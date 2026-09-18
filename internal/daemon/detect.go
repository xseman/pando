package daemon

import (
	"regexp"
	"slices"
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
	titleBlocked     *regexp.Regexp
	idlePrompt       bool // a ❯ prompt line with nothing above says idle
}

const screenLines = 12

var promptLine = regexp.MustCompile(`(?m)^\s*❯`)

var agentRules = map[string]rules{
	"claude": {
		blocked: []string{
			"do you want to proceed?", "requests your input", "waiting for permission",
			"do you want to allow", "enter to confirm", "enter to select",
		},
		running: []string{"esc to interrupt", "background agents to finish", "mcp tasks still running"},
		// "✻ Orbiting… (5s · ↓ 66 tokens)"; a finished turn says "✻ Worked for 5s", no ellipsis.
		runningLine: regexp.MustCompile(`(?m)^\s*[*·✢✳✶✻✽]\s+\S.*…(?:\s+\(\d+[smh]|\s*$)`),
		idlePrompt:  true,
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
	case r.idlePrompt && promptLine.MatchString(text):
		return "idle"
	}

	return ""
}
