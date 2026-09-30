package daemon

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// rules classify an agent's screen the way herdr's detection manifests do,
// reduced to the phrases that decide the state: what the agent prints while
// it waits for a permission or an answer (blocked), while a turn runs
// (running), and at its prompt (idle). Substrings and line patterns match the
// lowercased text of the last screenLines non-empty lines. Titles only help
// with ASCII words: oscFilter drops the spinner glyphs agents put in them.
// ponytail: no regions or priorities — blocked wins over running wins over
// idle, and an unknown program falls back to output timing.
type rules struct {
	blocked, running []string
	runningLine      *regexp.Regexp // a status line only a running turn shows
	// backgroundLine is a row of background work (a subagent, a workflow)
	// listed under the prompt. Its clock ticks while the work runs, so the
	// prompt stops saying idle and output timing decides instead.
	backgroundLine *regexp.Regexp
	titleBlocked   *regexp.Regexp
	idlePrompt     bool // a ❯ prompt line with nothing above says idle
}

const screenLines = 12

var promptLine = regexp.MustCompile(`(?m)^\s*❯`)

var agentRules = map[string]rules{
	"claude": {
		// "esc to cancel" opens the permission prompt's last line, so it stays
		// in view, whole, when a narrow column wraps the question out of the
		// tail and the rest of that line onto the next.
		blocked: []string{
			"do you want to proceed?", "requests your input", "waiting for permission",
			"do you want to allow", "enter to confirm", "enter to select", "esc to cancel",
		},
		running: []string{"esc to interrupt", "background agents to finish", "mcp tasks still running"},
		// "✻ Orbiting… (5s · ↓ 66 tokens)" or "✻ Wandering… (thinking)"; a finished
		// turn says "✻ Worked for 5s", no ellipsis.
		runningLine: regexp.MustCompile(`(?m)^\s*[*·✢✳✶✻✽]\s+\S.*…(?:\s+\((?:\d+[smh]|thinking)|\s*$)`),
		// "◯ deep-task  Checking the decoder   8m 33s · ↓ 127.6k tokens"
		backgroundLine: regexp.MustCompile(`(?m)^\s*◯\s+\S.*\s\d+[hms](?:\s?\d+[ms])?\b`),
		idlePrompt:     true,
	},
	"codex": {
		titleBlocked: regexp.MustCompile(`Action Required`),
		blocked: []string{
			"press enter to confirm or esc to cancel", "enter to submit answer", "allow command?",
			"do you trust the contents of this directory", "[y/n]", "yes (y)",
		},
		running: []string{"esc to interrupt"},
	},
	"gemini": {
		blocked: []string{"apply this change", "allow execution", "do you want to proceed", "waiting for user confirmation"},
		running: []string{"esc to cancel"},
	},
	"opencode": {
		blocked: []string{"permission required", "esc dismiss"},
		running: []string{"esc to interrupt", "ctrl+c to interrupt"},
	},
}

// screenState is "blocked", "running" or "idle" when the program's rules
// recognise the screen, else "".
func screenState(program, title string, lines []string) string {
	r, ok := agentRules[program]
	if !ok {
		return ""
	}

	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < screenLines; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append(tail, lines[i])
		}
	}

	text := strings.ToLower(strings.Join(tail, "\n"))

	has := func(ps []string) bool {
		return slices.ContainsFunc(ps, func(p string) bool { return strings.Contains(text, p) })
	}
	switch {
	case r.titleBlocked != nil && r.titleBlocked.MatchString(title), has(r.blocked):
		return "blocked"
	case has(r.running), r.runningLine != nil && r.runningLine.MatchString(text):
		return "running"
	case r.backgroundLine != nil && r.backgroundLine.MatchString(text):
		return ""
	case r.idlePrompt && promptLine.MatchString(text):
		return "idle"
	}

	return ""
}

// shells are what agents run a tool's command line in: claude's Bash tool is
// `bash -c source …/shell-snapshots/… && eval …`, codex's `bash -lc`,
// gemini's and opencode's `bash -c`.
var shells = []string{"sh", "bash", "zsh", "fish", "dash", "ksh"}

// shellCommand reports an argv that runs a command line in a shell (`sh -c`,
// `bash -lc`), not an interactive shell or some other program: an MCP server
// is a child of the agent too, and runs for as long as it does.
func shellCommand(argv []string) bool {
	if len(argv) < 2 || !slices.Contains(shells, strings.TrimPrefix(filepath.Base(argv[0]), "-")) {
		return false
	}

	for _, a := range argv[1:] {
		if !strings.HasPrefix(a, "-") {
			return false
		}

		if !strings.HasPrefix(a, "--") && strings.ContainsRune(a, 'c') {
			return true
		}
	}

	return false
}

// commandsUnder are the shell commands process pid runs as its own children,
// over every thread's list: a runtime may fork from any of them. Grandchildren
// do not count, as an MCP server's `npm exec` runs one of its own.
// ponytail: Linux only, as foreground is; elsewhere nothing is found.
func commandsUnder(pid int) []int {
	if pid <= 0 {
		return nil
	}

	dir := "/proc/" + strconv.Itoa(pid) + "/task/"

	tasks, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []int

	for _, t := range tasks {
		b, err := os.ReadFile(dir + t.Name() + "/children")
		if err != nil {
			continue
		}

		for f := range strings.FieldsSeq(string(b)) {
			if c, err := strconv.Atoi(f); err == nil && shellCommand(cmdline(c)) {
				out = append(out, c)
			}
		}
	}

	return out
}
