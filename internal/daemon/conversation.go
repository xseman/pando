package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// conversationSource reads which conversation an agent's process has open,
// from what the agent itself publishes about its running processes.
type conversationSource interface {
	// open is the conversation process pid has open, "" when it does not say.
	open(pid int) string
	// holder is a live process that has conversation id open, 0 when none.
	holder(id string) int
	// saved reports whether conversation id has anything to continue.
	saved(id string) bool
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

type claudeSource struct{}

// claudeProcess is the part of Claude Code's sessions/<pid>.json pando reads:
// sessionId follows /clear and /resume inside the running process.
type claudeProcess struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	ProcStart string `json:"procStart"` // /proc/<pid>/stat starttime, so a reused pid does not count
}

// readClaude reads one sessions/<pid>.json; ok when its process still runs.
func readClaude(path string) (claudeProcess, bool) {
	var p claudeProcess

	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &p) != nil || p.PID <= 0 || p.SessionID == "" {
		return p, false
	}

	return p, p.ProcStart == startTime(p.PID)
}

func (claudeSource) open(pid int) string {
	dir := envOf(pid, "CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = claudeDir()
	}

	p, ok := readClaude(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"))
	if !ok || p.PID != pid {
		return ""
	}

	return p.SessionID
}

func (claudeSource) holder(id string) int {
	paths, _ := filepath.Glob(filepath.Join(claudeDir(), "sessions", "*.json"))
	for _, path := range paths {
		if p, ok := readClaude(path); ok && p.SessionID == id {
			return p.PID
		}
	}

	return 0
}

// saved looks for the transcript under any project: claude writes it with
// the first message, and `--resume` of a conversation without one fails.
func (claudeSource) saved(id string) bool {
	m, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", "*", id+".jsonl"))

	return len(m) > 0
}

// startTime is field 22 of /proc/<pid>/stat, when the process started in
// clock ticks after boot; "" when it is not running.
func startTime(pid int) string {
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
	if len(f) < 20 {
		return ""
	}

	return f[19] // fields 3.. follow the name
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
