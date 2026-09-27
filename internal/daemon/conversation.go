package daemon

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// conversationSource reads which conversation an agent's process has open,
// from what the agent itself publishes about its running processes.
type conversationSource interface {
	// open is the conversation process pid has open, "" when it does not
	// say; job is the background job it runs in when pid only attaches to it.
	open(pid int) (id, job string)
	// holder is a live process that has conversation c open, 0 when none;
	// job is set when that process is a background job, which outlives pando
	// and is attached to again rather than resumed.
	holder(c proto.Conversation) (pid int, job string)
	// saved reports whether conversation c has anything to continue.
	saved(c proto.Conversation) bool
}

// conversations are the agents whose open conversation pando can tell, by
// program; any other agent resumes its latest one through [resume].
// ponytail: claude only; codex keeps its rollout file open, readable from /proc/<pid>/fd.
var conversations = map[string]conversationSource{"claude": claudeSource{}}

// claudeDir is Claude Code's config directory, where it keeps
// sessions/<pid>.json for every running process.
func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}

	h, _ := os.UserHomeDir()

	return filepath.Join(h, ".claude")
}

// claudeDirIn is the config directory env names, else the daemon's.
func claudeDirIn(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok && v != "" {
			return v
		}
	}

	return claudeDir()
}

type claudeSource struct{}

// claudeProcess is the part of Claude Code's sessions/<pid>.json pando reads:
// sessionId follows /clear and /resume inside the running process.
type claudeProcess struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	ProcStart string `json:"procStart"` // /proc/<pid>/stat starttime, so a reused pid does not count
	Kind      string `json:"kind"`      // "interactive", or "bg" for a background session its daemon runs
	JobID     string `json:"jobId"`     // a background session's job, what `claude attach` takes
}

// bgJob is the background job p runs, "" for a process in a terminal.
func (p claudeProcess) bgJob() string {
	if p.Kind != "bg" {
		return ""
	}

	return p.JobID
}

// readClaude reads one sessions/<pid>.json; ok when its process still runs.
// Claude rewrites the file in place rather than renaming a new one over it,
// so a read can land between the truncate and the write: one that does not
// parse is read again before the process counts as gone. Taken for gone, a
// background job would be resumed by id, which claude refuses while the job
// holds the conversation, rather than attached to.
func readClaude(path string) (claudeProcess, bool) {
	var p claudeProcess

	for try := 0; ; try++ {
		b, err := os.ReadFile(path)
		if err != nil {
			return p, false
		}

		p = claudeProcess{}
		if json.Unmarshal(b, &p) == nil {
			break
		}

		if try == tornRetries {
			return p, false
		}

		time.Sleep(tornWait)
	}

	if p.PID <= 0 || p.SessionID == "" {
		return p, false
	}

	return p, p.ProcStart == startTime(p.PID)
}

// tornRetries and tornWait bound the rereads of a sessions file caught half
// written: claude writes a few hundred bytes, so one wait is plenty.
const (
	tornRetries = 5
	tornWait    = 10 * time.Millisecond
)

// open reads the conversation of a claude in the terminal from its own
// sessions/<pid>.json. `claude attach JOB` writes none: its conversation is
// the background session whose job id JOB begins.
func (claudeSource) open(pid int) (id, job string) {
	dir := envOf(pid, "CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = claudeDir()
	}

	if p, ok := readClaude(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json")); ok && p.PID == pid {
		return p.SessionID, p.bgJob()
	}

	argv := cmdline(pid)

	i := slices.Index(argv, "attach")
	if i < 1 || i+1 >= len(argv) || argv[i+1] == "" {
		return "", ""
	}

	var found []claudeProcess

	for _, p := range claudeProcesses(dir) {
		if j := p.bgJob(); j != "" && strings.HasPrefix(j, argv[i+1]) {
			found = append(found, p)
		}
	}

	if len(found) != 1 { // none, or a prefix too short to say which
		return "", ""
	}

	return found[0].SessionID, found[0].JobID
}

// holder prefers a background job to any other process with c open: the
// job is what outlives pando, and a process left of a session before the
// restart that still shows c must not turn the attach into a resume.
func (claudeSource) holder(c proto.Conversation) (pid int, job string) {
	for _, p := range claudeProcesses(claudeDirIn(c.Env)) {
		if p.SessionID != c.ID {
			continue
		}

		if j := p.bgJob(); j != "" {
			return p.PID, j
		}

		pid = cmp.Or(pid, p.PID)
	}

	return pid, ""
}

// claudeProcesses are the live processes dir/sessions describes. A file
// named for a pid that is gone is skipped unread: claude leaves them behind.
func claudeProcesses(dir string) []claudeProcess {
	paths, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))

	var out []claudeProcess

	for _, path := range paths {
		if pid, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".json")); err == nil && startTime(pid) == "" {
			continue
		}

		if p, ok := readClaude(path); ok {
			out = append(out, p)
		}
	}

	return out
}

// saved looks for the transcript under any project: claude writes it with
// the first message, and `--resume` of a conversation without one fails.
func (claudeSource) saved(c proto.Conversation) bool {
	m, _ := filepath.Glob(filepath.Join(claudeDirIn(c.Env), "projects", "*", c.ID+".jsonl"))

	return len(m) > 0
}

// startTime is field 22 of /proc/<pid>/stat, when the process started in
// clock ticks after boot; "" when it is not running.
func startTime(pid int) string { return statField(pid, 22) }

// hasTerminal reports whether process pid still has a controlling terminal:
// field 7 of /proc/<pid>/stat, tty_nr, is 0 once the one it had is gone.
func hasTerminal(pid int) bool {
	tty := statField(pid, 7)
	return tty != "" && tty != "0"
}

// statField is field n (1-based, as proc(5) numbers them) of
// /proc/<pid>/stat, "" when the process is gone.
func statField(pid, n int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name, field 2, may hold spaces and parentheses.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return ""
	}

	f := strings.Fields(string(b[i+1:]))
	if n < 3 || len(f) < n-2 {
		return ""
	}

	return f[n-3] // fields 3.. follow the name
}

// cmdline is process pid's argv, nil when it is gone.
func cmdline(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return nil
	}

	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

// envOf is variable key in the environment process pid started with.
func envOf(pid int, key string) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return ""
	}

	for kv := range strings.SplitSeq(string(b), "\x00") {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}

	return ""
}

// agentEnv are keys as process pid has them set, KEY=VALUE: the variables
// that pick where an agent keeps its config and conversations ([resume_env]),
// which it resumes from only there.
func agentEnv(pid int, keys []string) []string {
	var out []string

	for _, k := range keys {
		if v := envOf(pid, k); v != "" {
			out = append(out, k+"="+v)
		}
	}

	return out
}

// withEnv puts env's KEY=VALUE before argv, as a shell line sets them for the
// one command; a value with anything but plain path characters is quoted.
func withEnv(env, argv []string) []string {
	if len(argv) == 0 {
		return argv
	}

	out := make([]string, 0, len(env)+len(argv))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out = append(out, k+"="+shellWord(v))
	}

	return append(out, argv...)
}

// shellWord is s as one word for bash, zsh and fish alike: bare when it is
// plain, else single-quoted, a quote inside closing and reopening the quotes.
func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:@%+,~") == "" && s[0] != '~' {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// withID is template t with {id} in each argument replaced by id.
func withID(t []string, id string) []string {
	out := make([]string, len(t))
	for i, a := range t {
		out[i] = strings.ReplaceAll(a, "{id}", id)
	}

	return out
}

// waitGone waits up to d for pid to be gone, then kills what is left of it;
// a negative pid is a process group, as for kill(2).
func waitGone(pid int, d time.Duration) {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
	}

	_ = syscall.Kill(pid, syscall.SIGKILL) // it had its chance
}
