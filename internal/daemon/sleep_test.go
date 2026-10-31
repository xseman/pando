package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// jobAgent sets up myagent, a stand-in claude whose sessions attach to
// background jobs: ws/myagent is the attach client, and what resumes it,
// printing MYAGENT and its arguments; ws/stopjob is its [stop_job], which
// adds the job it was given to ws/stopped and ends the job's process.
func jobAgent(t *testing.T, d *Daemon, ws string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	conversations["myagent"] = claudeSource{}

	t.Cleanup(func() { delete(conversations, "myagent") })

	mustWrite(t, filepath.Join(ws, "myagent"), "#!/bin/sh\necho MYAGENT \"$@\"\nsleep 300\n")
	mustWrite(t, filepath.Join(ws, "stopjob"), "#!/bin/sh\necho \"$@\" >> stopped\nkill \"$(cat job-$1.pid)\"\n")

	for _, f := range []string{"myagent", "stopjob"} {
		if err := os.Chmod(filepath.Join(ws, f), 0o755); err != nil { // the test runs them
			t.Fatalf("chmod %s: %v", f, err)
		}
	}

	d.mu.Lock()
	d.state.ResumeID = map[string][]string{"myagent": {"./myagent", "resumed", "{id}"}}
	d.state.ResumeJob = map[string][]string{"myagent": {"./myagent", "reattached", "{id}"}}
	d.state.StopJob = map[string][]string{"myagent": {"./stopjob", "{id}"}}
	cfgErr := d.saveConfig() // a restart reads them back
	d.mu.Unlock()

	if cfgErr != nil {
		t.Fatal(cfgErr)
	}
}

// jobSession starts a session in ws attached to background job job, whose
// conversation is conv-JOB: the job is a process of its own that a sessions
// entry of kind bg names, as Claude Code's daemon runs one. A parent makes
// it a shell of that session's Terminal panel. It returns the session's id
// and a channel closed when the job's process ends.
func jobSession(t *testing.T, d *Daemon, ws, parent, job string) (string, chan struct{}) {
	t.Helper()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}, "parent": parent}, &s)

	bg := exec.Command("sleep", "60")
	if err := bg.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = bg.Process.Kill() })

	gone := make(chan struct{})

	go func() { _ = bg.Wait(); close(gone) }()

	mustWrite(t, filepath.Join(ws, "job-"+job+".pid"), strconv.Itoa(bg.Process.Pid))
	fakeClaudeProc(t, claudeProcess{PID: bg.Process.Pid, SessionID: "conv-" + job, Kind: "bg", JobID: job, Name: "Job " + job})
	mustWrite(t, filepath.Join(claudeDir(), "projects", "-ws", "conv-"+job+".jsonl"), "{}\n")

	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "./myagent attach " + job + "\r"}, nil)
	waitFor(t, "the client to attach", func() bool { return strings.Contains(readScreen(t, s.ID), "MYAGENT attach") })
	waitFor(t, "the job to be recognised", func() bool {
		c := remembered(d, d.sessions[s.ID]).Conversation
		return c != nil && c.Job == job
	})

	return s.ID, gone
}

// readScreen is session id's screen as text.
func readScreen(t *testing.T, id string) string {
	t.Helper()

	var text string
	call(t, "session.read", proto.ReadParams{ID: id}, &text)

	return text
}

// TestKillStopsBackgroundJob: a killed session's job goes with it, through
// [stop_job], instead of running on in Claude Code's daemon with nothing in
// pando to reach it; so does the job a shell of its Terminal panel attached to.
func TestKillStopsBackgroundJob(t *testing.T) {
	needProc(t)

	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()
	jobAgent(t, d, ws)
	id, gone := jobSession(t, d, ws, "", "aaaa1111")
	_, panelGone := jobSession(t, d, ws, id, "bbbb2222")

	call(t, "session.kill", map[string]any{"id": id}, nil)

	for job, gone := range map[string]chan struct{}{"aaaa1111": gone, "bbbb2222": panelGone} {
		select {
		case <-gone:
		case <-time.After(5 * time.Second): // reaped by the test's own Wait, a moment after
			t.Fatalf("job %s still runs", job)
		}
	}

	if got := strings.Fields(mustRead(t, filepath.Join(ws, "stopped"))); !slices.Equal(got, []string{"aaaa1111", "bbbb2222"}) {
		t.Fatalf("stop_job got %q", got)
	}
}

// TestSleepWakeBackgroundJob is claude as pando runs it by default: a
// session attached to a background job. Asleep, neither the attach nor the
// job runs, the screen stays, input is refused and a wait stops at it; woken,
// the conversation is resumed by id, the job being gone, and a wait begun
// while it slept follows it into the new process.
func TestSleepWakeBackgroundJob(t *testing.T) {
	needProc(t)

	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()
	jobAgent(t, d, ws)
	id, gone := jobSession(t, d, ws, "", "aaaa1111")
	shell := d.sessions[id].cmd.Process.Pid

	call(t, "session.sleep", map[string]any{"id": id}, nil)

	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the job still runs")
	}

	if kill(shell, 0) == nil {
		t.Fatal("the session's shell still runs")
	}

	var got proto.Session
	call(t, "session.get", map[string]any{"id": id}, &got)

	if got.Status != "asleep" || !got.Asleep || got.Conversation == nil || got.Conversation.Job != "aaaa1111" {
		t.Fatalf("asleep: %+v", got)
	}

	if scr := readScreen(t, id); !strings.Contains(scr, "MYAGENT attach aaaa1111") || !strings.Contains(scr, asleepLine) {
		t.Fatalf("screen while asleep:\n%s", scr)
	}

	if err := proto.Call("session.input", proto.InputParams{ID: id, Text: "x"}, nil); err == nil || !strings.Contains(err.Error(), "asleep") {
		t.Fatalf("input while asleep: %v", err)
	}

	var settled proto.Session
	call(t, "session.wait", proto.WaitParams{ID: id, Timeout: 1000}, &settled)

	if settled.Status != "asleep" {
		t.Fatalf("wait stopped at %q", settled.Status)
	}

	if st := mustRead(t, d.statePath); !strings.Contains(st, `"asleep": true`) {
		t.Fatalf("state.json does not keep it asleep:\n%s", st)
	}

	waited := make(chan proto.Session)

	go func() {
		var s proto.Session

		_ = proto.Call("session.wait", proto.WaitParams{ID: id, Match: "MYAGENT resumed", Timeout: 15000}, &s)
		waited <- s
	}()

	var woke proto.Session
	call(t, "session.wake", map[string]any{"id": id}, &woke)

	if woke.ID != id || woke.Status == "asleep" || woke.Program != "myagent" {
		t.Fatalf("woken: %+v", woke)
	}

	if w := <-waited; w.ID != id {
		t.Fatalf("the wait begun asleep returned %+v", w)
	}

	if scr := readScreen(t, id); !strings.Contains(scr, "MYAGENT resumed conv-aaaa1111") {
		t.Fatalf("screen after waking:\n%s", scr)
	}

	var ss []proto.Session
	call(t, "session.list", nil, &ss)

	if len(ss) != 1 {
		t.Fatalf("sessions after waking: %+v", ss)
	}
}

// TestSleepSurvivesRestart: a restarted daemon lists an asleep session with
// nothing running behind it and the use it had, and wakes it as the running
// daemon would.
func TestSleepSurvivesRestart(t *testing.T) {
	needProc(t)

	boot := start(t)
	d := boot()
	ws := t.TempDir()
	jobAgent(t, d, ws)

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "./myagent\r"}, nil)
	waitFor(t, "the agent to start", func() bool { return strings.Contains(readScreen(t, s.ID), "MYAGENT") })

	sess := d.sessions[s.ID]

	var pid int

	waitFor(t, "the agent to hold the terminal", func() bool {
		var prog string

		pid, prog = sess.foreground()

		return prog == "myagent"
	})

	fakeClaude(t, pid, "conv-one")
	mustWrite(t, filepath.Join(claudeDir(), "projects", "-ws", "conv-one.jsonl"), "{}\n")
	waitFor(t, "the conversation to be recognised", func() bool { return remembered(d, sess).Conversation != nil })

	call(t, "session.sleep", map[string]any{"id": s.ID}, nil)

	used := d.sessions[s.ID].used()

	d.Close()

	d = boot()
	defer d.Close()

	var got proto.Session
	call(t, "session.get", map[string]any{"id": s.ID}, &got)

	if dm := d.sessions[s.ID]; got.Status != "asleep" || dm.cmd != nil || !dm.used().Equal(used) {
		t.Fatalf("after a restart: %+v, cmd %v, used %v want %v", got, dm.cmd, dm.used(), used)
	}

	if scr := readScreen(t, s.ID); !strings.Contains(scr, asleepLine) {
		t.Fatalf("screen after a restart:\n%s", scr)
	}

	var woke proto.Session
	call(t, "session.wake", map[string]any{"id": s.ID}, &woke)

	if woke.Program != "myagent" || !strings.Contains(readScreen(t, s.ID), "MYAGENT resumed conv-one") {
		t.Fatalf("woken: %+v\n%s", woke, readScreen(t, s.ID))
	}
}

// TestSleepKeepsName: an asleep session keeps the title and program that name
// it, from the moment it says asleep, though the program clears its title on
// its way out, and over a restart.
func TestSleepKeepsName(t *testing.T) {
	needProc(t)

	boot := start(t)
	d := boot()

	script := `trap 'printf "\033]0;\007"; sleep 0.5; exit' HUP; printf '\033]0;the task\007'; while :; do sleep 0.1; done`

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"sh", "-c", script}}, &s)
	waitFor(t, "the title", func() bool {
		call(t, "session.get", map[string]any{"id": s.ID}, &s)
		return s.Title == "the task"
	})

	slept := make(chan error, 1)
	id := s.ID // the loop below decodes into s while this runs

	go func() { slept <- proto.Call("session.sleep", map[string]any{"id": id}, nil) }()

	for going := true; going; {
		select {
		case err := <-slept:
			if err != nil {
				t.Fatalf("session.sleep: %v", err)
			}

			going = false

		default:
		}

		call(t, "session.get", map[string]any{"id": s.ID}, &s)

		if s.Status == "asleep" && (s.Title != "the task" || s.Program != "sh") {
			t.Fatalf("going to sleep: %+v", s)
		}
	}

	if s.Status != "asleep" {
		t.Fatalf("asleep: %+v", s)
	}

	d.Close()

	d = boot()
	defer d.Close()

	call(t, "session.get", map[string]any{"id": s.ID}, &s)

	if s.Status != "asleep" || s.Title != "the task" || s.Program != "sh" {
		t.Fatalf("after a restart: %+v", s)
	}
}

// TestDormantConversationTitle: a session that went to sleep before the spec
// kept its title takes it from its conversation; one that kept none stays so.
func TestDormantConversationTitle(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mustWrite(t, filepath.Join(claudeDir(), "projects", "-ws", "conv-dormant.jsonl"),
		`{"type":"ai-title","aiTitle":"the task","sessionId":"conv-dormant"}`+"\n")

	s := dormant(proto.SessionSpec{
		ID: "a", Agent: "shell", Cmd: []string{"sh"}, Asleep: true,
		Conversation: &proto.Conversation{Agent: "claude", ID: "conv-dormant"},
	}, 0, 0)

	if i := s.info(); i.Title != "the task" || i.Program != "claude" {
		t.Fatalf("dormant: %+v", i)
	}

	spec := s.info().SessionSpec
	spec.Title = ""

	if i := dormant(spec, 0, 0).info(); i.Title != "" || i.Program != "claude" {
		t.Fatalf("dormant, no title kept: %+v", i)
	}
}

// TestSleepGenericAgents: what comes back depends on what ran, the same way
// for any agent. One with a [resume] is continued by it; a shell wakes to a
// shell; whatever else ran in front, a server, is not brought back but the
// session sleeps; an agent pando cannot bring back is refused, its
// conversation would be lost; a session whose process exits cleanly when it
// is stopped stays listed.
func TestSleepGenericAgents(t *testing.T) {
	needProc(t)

	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()

	for _, name := range []string{"coder", "gem"} {
		mustWrite(t, filepath.Join(ws, name), "#!/bin/sh\necho "+strings.ToUpper(name)+" \"$@\"\nsleep 300\n")

		if err := os.Chmod(filepath.Join(ws, name), 0o755); err != nil { // the test runs it
			t.Fatalf("chmod %s: %v", name, err)
		}
	}

	d.mu.Lock()
	d.state.Resume = map[string][]string{"coder": {"./coder", "--last"}}
	d.state.Agents["gem"] = []string{"gem"}
	d.mu.Unlock()

	run := func(line, seen string) string {
		t.Helper()

		var s proto.Session
		call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)

		if line != "" {
			call(t, "session.input", proto.InputParams{ID: s.ID, Text: line + "\r"}, nil)
			waitFor(t, line+" to start", func() bool { return strings.Contains(readScreen(t, s.ID), seen) })
			waitFor(t, line+" to be in front", func() bool {
				_, prog := d.sessions[s.ID].foreground()
				return prog == strings.Fields(line)[0][2:] || prog == strings.Fields(line)[0]
			})
		}

		return s.ID
	}

	coder, shell, server, gem := run("./coder", "CODER"), run("", ""), run("sleep 300", ""), run("./gem", "GEM")

	waitFor(t, "coder's resume to be remembered", func() bool { return remembered(d, d.sessions[coder]).Resume != nil })
	remembered(d, d.sessions[gem])

	for _, id := range []string{coder, shell, server} {
		call(t, "session.sleep", map[string]any{"id": id}, nil)
	}

	if err := proto.Call("session.sleep", map[string]any{"id": gem}, nil); err == nil || !strings.Contains(err.Error(), "gem cannot be resumed") {
		t.Fatalf("sleep gem: %v", err)
	}

	for _, id := range []string{coder, shell, server} {
		var woke proto.Session
		call(t, "session.wake", map[string]any{"id": id}, &woke)

		if woke.Status == "asleep" || woke.Status == "exited" {
			t.Fatalf("woken: %+v", woke)
		}
	}

	waitFor(t, "coder to be continued", func() bool { return strings.Contains(readScreen(t, coder), "CODER --last") })

	if _, prog := d.sessions[server].foreground(); prog == "sleep" {
		t.Fatal("what ran in front came back")
	}

	// A process that ends cleanly on its hangup would close the session.
	mustWrite(t, filepath.Join(ws, "clean"), "#!/bin/sh\ntrap 'exit 0' HUP\nwhile :; do sleep 0.1; done\n")

	if err := os.Chmod(filepath.Join(ws, "clean"), 0o755); err != nil { // the session runs it
		t.Fatal(err)
	}

	var clean proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"./clean"}}, &clean)
	call(t, "session.sleep", map[string]any{"id": clean.ID}, nil)
	time.Sleep(200 * time.Millisecond) // past the exit callback

	var got proto.Session
	call(t, "session.get", map[string]any{"id": clean.ID}, &got)

	if got.Status != "asleep" {
		t.Fatalf("a clean exit on sleep: %+v", got)
	}
}

// TestAutoSleep puts a session to sleep once it has gone unused for
// sleep_after, its Terminal panel's shell with it; one in use, one working,
// one running something pando would not bring back, one whose panel works
// or runs a server, and one that rang for attention nobody gave stay awake.
func TestAutoSleep(t *testing.T) {
	needProc(t)

	unit := sleepUnit
	sleepUnit = 300 * time.Millisecond

	t.Cleanup(func() { sleepUnit = unit })

	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()
	newSession := func(parent, line string) string {
		t.Helper()

		var s proto.Session
		call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}, "parent": parent}, &s)

		if line != "" {
			call(t, "session.input", proto.InputParams{ID: s.ID, Text: line + "\r"}, nil)
		}

		return s.ID
	}

	idle := newSession("", "")
	panel := newSession(idle, "")
	busy := newSession("", "while :; do echo tick; sleep 0.2; done")
	server := newSession("", "sleep 300")
	busyPanel := newSession("", "")
	newSession(busyPanel, "while :; do echo tick; sleep 0.2; done")
	serverPanel := newSession("", "") // its panel's shell runs a quiet server: idle, yet not to be stopped
	newSession(serverPanel, "sleep 300")
	viewed := newSession("", "")
	rang := newSession("", `printf '\a'`)

	stop := make(chan struct{})
	defer close(stop)

	go func() { // a TUI showing it
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_ = proto.Call("session.screen", proto.ScreenParams{ID: viewed}, nil)
			}
		}
	}()

	d.mu.Lock()
	d.state.Settings.Sleep = 1
	d.mu.Unlock()

	status := func(id string) string {
		var s proto.Session
		call(t, "session.get", map[string]any{"id": id}, &s)

		return s.Status
	}

	waitFor(t, "the idle session to fall asleep", func() bool { return status(idle) == "asleep" && status(panel) == "asleep" })
	time.Sleep(time.Second) // long past sleep_after for the rest too

	for name, id := range map[string]string{"busy": busy, "server": server, "busy panel's": busyPanel, "server panel's": serverPanel, "viewed": viewed, "rang": rang} {
		if st := status(id); st == "asleep" {
			t.Errorf("the %s session fell asleep", name)
		}
	}
}
