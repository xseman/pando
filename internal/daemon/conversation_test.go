package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// sleeper is a process that runs until the test ends, to stand for a claude.
func sleeper(t *testing.T) int {
	t.Helper()

	c := exec.Command("sleep", "60")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })

	return c.Process.Pid
}

// Claude rewrites sessions/<pid>.json in place: a read between the truncate
// and the write sees half a file, which is not the process being gone.
func TestReadClaudeTornWrite(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	pid := sleeper(t)
	fakeClaudeProc(t, claudeProcess{PID: pid, SessionID: "conv", Kind: "bg", JobID: "job1"})

	path := filepath.Join(claudeDir(), "sessions", strconv.Itoa(pid)+".json")
	whole := mustRead(t, path)
	mustWrite(t, path, whole[:len(whole)/2])

	done := make(chan struct{})

	go func() { // the write lands while readClaude is still at it
		time.Sleep(tornWait + tornWait/2)

		_ = os.WriteFile(path, []byte(whole), 0o600)

		close(done)
	}()

	p, ok := readClaude(path)

	<-done

	if !ok || p.SessionID != "conv" || p.bgJob() != "job1" {
		t.Fatalf("a half-written file read again: ok %v %+v", ok, p)
	}
	// One that never parses is given up on, not waited for.
	mustWrite(t, path, whole[:len(whole)/2])

	start := time.Now()

	if _, ok := readClaude(path); ok {
		t.Fatal("a file that never parses read as a live process")
	}

	if d := time.Since(start); d > time.Second { // tornRetries waits, with room for a loaded machine
		t.Fatalf("gave up after %v", d)
	}
}

// A background job is what a restart attaches to, even when a process left
// of the session before it still shows the same conversation and its file
// sorts first.
func TestHolderPrefersBackgroundJob(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	a, b := sleeper(t), sleeper(t)
	if strconv.Itoa(a) > strconv.Itoa(b) {
		a, b = b, a // a's file is read first
	}

	fakeClaudeProc(t, claudeProcess{PID: a, SessionID: "conv", Kind: "interactive"})
	fakeClaudeProc(t, claudeProcess{PID: b, SessionID: "conv", Kind: "bg", JobID: "job1"})

	if pid, job := (claudeSource{}).holder(proto.Conversation{Agent: "claude", ID: "conv"}); pid != b || job != "job1" {
		t.Fatalf("holder %d %q, want the job %d", pid, job, b)
	}
	// Without a job, the other process is the holder.
	fakeClaudeProc(t, claudeProcess{PID: b, SessionID: "other", Kind: "bg", JobID: "job1"})

	if pid, job := (claudeSource{}).holder(proto.Conversation{Agent: "claude", ID: "conv"}); pid != a || job != "" {
		t.Fatalf("holder %d %q, want %d", pid, job, a)
	}
}

// A file left by a claude that exited is skipped, whatever it holds.
func TestClaudeProcessesSkipsTheGone(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	gone := c.Process.Pid

	b, err := json.Marshal(claudeProcess{PID: gone, SessionID: "conv", Kind: "bg", JobID: "job1"})
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(claudeDir(), "sessions", strconv.Itoa(gone)+".json"), string(b))

	if ps := claudeProcesses(claudeDir()); len(ps) != 0 {
		t.Fatalf("a gone process listed: %+v", ps)
	}
}

// An agent started in a config of its own (CLAUDE_CONFIG_DIR, CODEX_HOME…)
// resumes in it, whether or not pando can tell its conversation: the
// variables [resume_env] names go before the command.
func TestResumeCarriesAgentEnv(t *testing.T) {
	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()
	agent := filepath.Join(ws, "myagent")
	mustWrite(t, agent, "#!/bin/sh\necho AGENT UP\nsleep 300\n")

	if err := os.Chmod(agent, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", agent, err)
	}

	d.mu.Lock()
	d.state.Resume = map[string][]string{"myagent": {"myagent", "--last"}}
	d.state.ResumeEnv = map[string][]string{"myagent": {"MYAGENT_HOME", "MYAGENT_UNSET"}}
	d.mu.Unlock()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "MYAGENT_HOME='/tmp/my home' ./myagent\r"}, nil)

	sess := d.sessions[s.ID]

	var got proto.SessionSpec

	waitFor(t, "the agent to be recognised", func() bool { got = remembered(d, sess); return got.Resume != nil })

	if want := []string{"MYAGENT_HOME='/tmp/my home'", "myagent", "--last"}; !reflect.DeepEqual(got.Resume, want) {
		t.Fatalf("resume %q, want %q", got.Resume, want)
	}
}

// claude_background continues a claude conversation as a background session
// through pando itself; other programs, and claude with it off, as they are.
func TestByIDInBackground(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	d := &Daemon{}
	d.state.ResumeID = map[string][]string{"claude": {"claude", "--resume", "{id}"}, "other": {"other", "{id}"}}
	d.state.Settings.ClaudeBg = true

	if got := d.byID("claude", "c1"); !reflect.DeepEqual(got, []string{shellWord(exe), "claude", "--resume", "c1"}) {
		t.Fatalf("in the background: %q", got)
	}

	if got := d.byID("other", "c1"); !reflect.DeepEqual(got, []string{"other", "c1"}) {
		t.Fatalf("another program: %q", got)
	}

	d.state.Settings.ClaudeBg = false

	if got := d.byID("claude", "c1"); strings.Join(got, " ") != "claude --resume c1" {
		t.Fatalf("off: %q", got)
	}
}
