package daemon

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/xseman/pando/internal/proto"
)

func TestScreenState(t *testing.T) {
	cases := []struct {
		program, title string
		lines          []string
		want           string
	}{
		{"claude", "Claude Code", []string{"⏺ Done.", "", "✻ Worked for 5s", "", "───", "❯ ", "───"}, "idle"},
		{"claude", "Claude Code", []string{"✻ Orbiting… (2s · ↓ 66 tokens)", "", "───", "❯ ", "───"}, "running"},
		{"claude", "Claude Code", []string{"✶ Reading 1 file…", "❯ "}, "running"},
		{"claude", "Claude Code", []string{"✻ Cooking… (12s · esc to interrupt)", "❯ "}, "running"},
		{"claude", "Claude Code", []string{"✻ Wandering… (thinking)", "", "───", "❯ ", "───"}, "running"},
		{"claude", "Claude Code", []string{"✻ Clauding… (thinking with high effort)", "❯ "}, "running"},
		{"claude", "Claude Code", []string{"Bash command", "  rm -rf build", "Do you want to proceed?", "❯ 1. Yes", "  2. No", "esc to cancel"}, "blocked"},
		// A column 30 wide wraps the options until the question leaves the tail
		// and ❯ 1. Yes reads as the prompt; the footer still says blocked.
		{"claude", "Claude Code", []string{
			"Do you want to proceed?", "❯ 1. Yes", "  2. Yes, and don't ask again", "     for go vet, \"echo \\\"vet",
			"     exit: $?\\\"\", go run,", "     and \"echo \\\"run exit:", "     $?\\\"\" commands in", "     /tmp/pando-demo",
			"  3. Yes, and switch to auto", "     mode · auto mode handles", "     these prompts for you", "  4. No",
			"Esc to cancel · Tab to amend",
		}, "blocked"},
		// Narrower still, the footer wraps too.
		{"claude", "Claude Code", []string{"  3. Yes, and switch to", "     auto mode", "  4. No", "", "Esc to cancel · Tab to", "amend"}, "blocked"},
		// A subagent still runs under the idle prompt: output timing decides.
		{"claude", "Claude Code", []string{"───", "❯ ", "───", "  ⏵⏵ auto mode on", "  ● main", "  ◯ deep-task  Checking the decoder   8m 33s · ↓ 127.6k tokens"}, ""},
		{"claude", "Claude Code", []string{"❯ ", "───", "  ◯ docs-r2  ▰▰▰▱▱  50/67 · 25m33s · ↓ 8.6m tokens"}, ""},
		{"claude", "Claude Code", []string{"❯ ", "───", "  ◯ foo"}, "idle"},
		{"codex", "Action Required · codex", []string{"Allow command?"}, "blocked"},
		{"codex", "codex", []string{"• Working (3s • esc to interrupt)"}, "running"},
		{"gemini", "", []string{"│ Apply this change?", "│ ● Yes, allow once"}, "blocked"},
		{"opencode", "", []string{"△ Permission required"}, "blocked"},
		{"opencode", "", []string{"esc to interrupt"}, "running"},
		{"bash", "", []string{"$ "}, ""},
		{"claude", "Claude Code", []string{"plain output, no prompt"}, ""}, // unrecognised: output timing decides
	}
	for _, c := range cases {
		if got := screenState(c.program, c.title, c.lines); got != c.want {
			t.Errorf("%s %q %q = %q, want %q", c.program, c.title, c.lines, got, c.want)
		}
	}
}

func TestBlockedNeedsAttention(t *testing.T) {
	now := time.Now()

	s := &session{status: "running", seen: "blocked", lastOutput: now}
	if !s.tick(now) || s.status != "blocked" || !s.attention {
		t.Fatalf("blocked screen: %+v", s)
	}

	s = &session{status: "running", seen: "blocked", lastOutput: now, lastViewed: now}
	s.tick(now)

	if s.status != "blocked" || s.attention {
		t.Fatalf("viewed blocked session must not need attention: %+v", s)
	}
	// The prompt says idle once the output stops.
	s = &session{status: "running", seen: "idle", busySince: now.Add(-5 * time.Second), lastOutput: now}
	s.tick(now.Add(busyWindow))

	if s.status != "idle" || !s.attention {
		t.Fatalf("idle prompt after a burst: %+v", s)
	}
}

// An agent streaming its answer takes its spinner down, claude's prompt still
// under the text: output it writes on its own says running over the prompt,
// its echo of a key does not, and the prompt says idle once it stops.
func TestOutputOverIdlePrompt(t *testing.T) {
	now := time.Now()

	s := &session{status: "idle", seen: "idle", lastOutput: now}
	s.tick(now)

	if s.status != "running" {
		t.Fatalf("streaming under an idle prompt: %q, want running", s.status)
	}

	s.tick(now.Add(busyWindow))

	if s.status != "idle" {
		t.Fatalf("the stream done: %q, want idle", s.status)
	}

	s = &session{status: "idle", seen: "idle", inputAt: now.Add(-100 * time.Millisecond), lastOutput: now}
	s.tick(now)

	if s.status != "idle" {
		t.Fatalf("the echo of a key: %q, want idle", s.status)
	}
	// A key typed while the answer streams: its echo neither starts nor ends
	// a turn.
	s = &session{status: "running", seen: "idle", inputAt: now.Add(-100 * time.Millisecond), lastOutput: now}
	s.tick(now)

	if s.status != "running" {
		t.Fatalf("a key mid-stream: %q, want running", s.status)
	}
}

// A wheel up sent to an app that takes the mouse scrolls its own view: claude
// then shows history under its prompt and draws nothing while it works. The
// status holds through it, until a key, or output the app writes on its own.
func TestScrolledViewHolds(t *testing.T) {
	now := time.Now()
	wheel := proto.InputParams{Mouse: &proto.Mouse{Kind: "wheel", Button: int(uv.MouseWheelUp)}}

	s := &session{status: "running", seen: "idle", emu: vt.NewEmulator(80, 6), lastOutput: now.Add(-time.Minute)}
	s.screenAt = s.lastOutput

	go func() { // what it sends the app, which a session's pty takes
		for buf := make([]byte, 64); ; {
			if _, err := s.emu.Read(buf); err != nil {
				return
			}
		}
	}()

	if err := s.input(wheel); err != nil || !s.held {
		t.Fatalf("a wheel up: held %v, %v", s.held, err)
	}

	s.tick(now)

	if s.status != "running" {
		t.Fatalf("scrolled up mid-turn: %q, want running", s.status)
	}

	_ = s.input(proto.InputParams{Keys: []proto.Key{{Code: uv.KeyEnd, Mod: int(uv.ModCtrl)}}})
	s.tick(now.Add(2 * busyWindow)) // past the redraw the key brings

	if s.held || s.status != "idle" {
		t.Fatalf("a key back to the live view: held %v, %q, want idle", s.held, s.status)
	}

	_ = s.input(wheel)
	s.lastOutput = now.Add(time.Second) // the app follows its stream again
	s.screenAt = s.lastOutput

	s.tick(now.Add(time.Second))

	if s.held || s.status != "running" {
		t.Fatalf("output of its own: held %v, %q, want running", s.held, s.status)
	}
}

// A subagent's row ticks under claude's idle prompt: running while its clock
// prints, idle once the output stops.
func TestBackgroundAgentRuns(t *testing.T) {
	now := time.Now()

	s := &session{status: "idle", program: "claude", emu: vt.NewEmulator(80, 6), lastOutput: now}
	_, _ = s.emu.WriteString("❯ \r\n───\r\n  ● main\r\n  ◯ deep-task  Checking the decoder   8m 33s")

	s.tick(now)

	if s.status != "running" {
		t.Fatalf("a ticking subagent row: %q, want running", s.status)
	}

	s.tick(now.Add(2 * busyWindow))

	if s.status != "idle" {
		t.Fatalf("a row gone quiet: %q, want idle", s.status)
	}
}

func TestShellCommand(t *testing.T) {
	cases := []struct {
		argv []string
		want bool
	}{
		{[]string{"/bin/bash", "-c", "source ~/.claude/shell-snapshots/snapshot-bash-1.sh && eval 'sleep 60'"}, true},
		{[]string{"bash", "-lc", "cargo test"}, true},
		{[]string{"/usr/bin/zsh", "-c", "ls"}, true},
		{[]string{"-sh", "-c", "make"}, true},
		{[]string{"sh", "-e", "-c", "make"}, true},
		{[]string{"bash"}, false},                               // an interactive shell
		{[]string{"bash", "script.sh", "-c"}, false},            // a script's own flag
		{[]string{"bash", "--noprofile", "--norc"}, false},      // long flags only
		{[]string{"npm", "exec", "figma-developer-mcp"}, false}, // an MCP server
		{[]string{"node", "-c", "x"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := shellCommand(c.argv); got != c.want {
			t.Errorf("shellCommand(%q) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// A background shell leaves claude at its idle prompt: the command under it
// keeps the session running once it outlasts a tick, and idle after.
func TestCommandKeepsRunning(t *testing.T) {
	now := time.Now()
	s := &session{status: "idle", seen: "idle", screenProg: "claude", program: "claude"}

	s.setCommands([]int{42})
	s.tick(now)

	if s.status != "idle" {
		t.Fatalf("a command seen once: %q, want idle", s.status)
	}

	s.setCommands([]int{42})
	s.tick(now)

	if s.status != "running" || !s.info().Shell {
		t.Fatalf("a command seen twice: %q shell=%v, want running on a shell", s.status, s.info().Shell)
	}

	s.seen = "blocked"
	s.tick(now)

	if s.status != "blocked" || s.info().Shell {
		t.Fatalf("a prompt over a running command: %q shell=%v, want blocked", s.status, s.info().Shell)
	}

	s.seen = "idle"
	s.setCommands(nil)
	s.tick(now)

	if s.status != "idle" {
		t.Fatalf("the command done: %q, want idle", s.status)
	}
}

func TestCommandsUnder(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	if _, err := os.Stat("/proc/" + self + "/task/" + self + "/children"); err != nil {
		t.Skip("no /proc/<pid>/task/<tid>/children here")
	}

	sh := exec.Command("sh", "-c", "sleep 30; true") // the list keeps sh from exec'ing sleep
	plain := exec.Command("sleep", "30")

	for _, c := range []*exec.Cmd{sh, plain} {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	}

	var got []int

	for range 100 { // until both have exec'd
		got = commandsUnder(os.Getpid())
		if slices.Contains(got, sh.Process.Pid) && len(cmdline(plain.Process.Pid)) > 0 && cmdline(plain.Process.Pid)[0] == "sleep" {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if !slices.Contains(got, sh.Process.Pid) || slices.Contains(got, plain.Process.Pid) {
		t.Fatalf("commandsUnder = %v, want %d (sh -c) and not %d (sleep)", got, sh.Process.Pid, plain.Process.Pid)
	}
}
