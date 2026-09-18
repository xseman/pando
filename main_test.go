//go:build unix

package main

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// The test binary doubles as pando, so the TUI can autostart `pando serve`.
func TestMain(m *testing.M) {
	if os.Getenv("PANDO_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// mustWrite writes a file and the directories above it, or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	_ = os.MkdirAll(filepath.Dir(path), 0o755) // a failing WriteFile names the path
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustGit runs a git command in root, or fails the test naming the command.
func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()

	if _, err := git.Run(root, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

// TestE2E drives the real TUI in a PTY, rendered by pando's own emulator.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	// Both are t.TempDir: /tmp/TestE2E<digits>/001 keeps the socket under it
	// well inside the ~104-byte sun_path cap, and removal is a cleanup, so the
	// shutdown registered below runs before either directory goes.
	run, tmp := t.TempDir(), t.TempDir()
	repo := filepath.Join(tmp, "repo")
	mustWrite(t, filepath.Join(repo, "README.md"), "# e2e\n")
	mustWrite(t, filepath.Join(repo, "src", "a.go"), "package a\n")

	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"add", "-A"}, {"commit", "-qm", "init"}} {
		mustGit(t, repo, args...)
	}

	mustWrite(t, filepath.Join(repo, "README.md"), "# e2e changed\n")
	// Pin the defaults that depend on installed fonts or move click targets.
	mustWrite(t, filepath.Join(tmp, "cfg", "config.toml"), "icons = \"ascii\"\npanel_borders = false\n")

	env := []string{
		"PANDO_TEST_MAIN=1", "PANDO_RUNTIME_DIR=" + run, "PANDO_CONFIG_DIR=" + filepath.Join(tmp, "cfg"),
		"PANDO_DATA_DIR=" + filepath.Join(tmp, "data"), "SHELL=/bin/sh", "PS1=pando$ ", "HISTFILE=/dev/null",
		"TERM=xterm-256color", "HOME=" + tmp, "PATH=" + os.Getenv("PATH"),
	}
	t.Setenv("PANDO_RUNTIME_DIR", run)
	// Registered after TempDir, so it runs first: the daemon and its sessions
	// must be gone before the directory is removed.
	t.Cleanup(func() {
		_ = proto.Call("shutdown", nil, nil) // no daemon left to stop is the goal
		for i := 0; i < 50 && proto.Call("ping", nil, nil) == nil; i++ {
			time.Sleep(50 * time.Millisecond)
		}

		time.Sleep(300 * time.Millisecond) // sessions exit after the listener closes
	})

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(exe)
	cmd.Dir, cmd.Env = repo, env

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	var mu sync.Mutex

	emu := vt.NewEmulator(100, 30)
	go func() { _, _ = io.Copy(f, emu) }() // answers the TUI's terminal queries
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := f.Read(buf)

			mu.Lock()
			_, _ = emu.Write(buf[:n]) // the emulator absorbs anything
			mu.Unlock()

			if err != nil {
				return
			}
		}
	}()

	screen := func() string { mu.Lock(); defer mu.Unlock(); return emu.String() }
	wait := func(what string) {
		t.Helper()

		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if strings.Contains(screen(), what) {
				return
			}
		}

		t.Fatalf("screen never showed %q:\n%s", what, screen())
	}
	send := func(s string) {
		t.Helper()

		if _, err := f.WriteString(s); err != nil {
			t.Fatalf("write %q to the pty: %v", s, err)
		}

		time.Sleep(150 * time.Millisecond)
	}

	wait("README.md")
	wait("M") // git decoration arrives asynchronously
	send("2")
	wait("Changes")
	send("j") // the Changes header; keys skip the message box and buttons
	send("j")
	send("\r") // stage README.md
	wait("Staged Changes")
	send("c")
	send("e2e commit")
	send("\r")
	wait("committed")

	if out, _ := exec.Command("git", "-C", repo, "log", "--oneline", "-1").Output(); !strings.Contains(string(out), "e2e commit") {
		t.Fatalf("commit missing: %s", out)
	}

	send("3")
	send("n") // a shell in the workspace, no agent to pick first
	wait("pando$")
	send("echo E2E_$((40+2)) Upper\r")
	wait("E2E_42 Upper")

	// Right click the Spaces chip in the activity bar (row 1, columns 16-23) and move it right.
	send("\x1b[<2;18;1M\x1b[<2;18;1m")
	wait("Move to Right Sidebar")
	send("\r")

	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if lines := strings.Split(screen(), "\n"); len(lines) > 0 && strings.HasSuffix(strings.TrimRight(lines[0], " "), "SPACES") {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("Spaces did not move to the right sidebar:\n%s", screen())
		}
	}

	send("\x10") // ctrl+p quick open, focus is on the right sidebar
	wait("Go to File")
	send("a.go\r")
	wait("package a")

	send("q") // preview has no focus: q asks before quitting
	wait("Close pando?")
	send("\r") // Close: the TUI exits, the daemon keeps the session

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("tui exit: %v", err)
		}

	case <-time.After(5 * time.Second):
		t.Fatalf("tui did not quit:\n%s", screen())
	}

	var ss []proto.Session
	if err := proto.Call("session.list", nil, &ss); err != nil || len(ss) != 1 || ss[0].Agent != "shell" {
		t.Fatalf("session survives the TUI: %+v %v", ss, err)
	}

	var text string
	if err := proto.Call("session.read", map[string]any{"id": ss[0].ID}, &text); err != nil {
		t.Fatalf("session.read: %v", err)
	}

	if !strings.Contains(text, "E2E_42") {
		t.Fatalf("session output: %q", text)
	}
}

// TestParse pins the CLI forms the README and the tapes document: flags
// anywhere among the positionals, and `--` making everything after it positional.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		pos   []string
		flags string // the flags the form sets, name=value
	}{
		{[]string{"ls"}, nil, ""},
		{[]string{"new", "--agent", "claude"}, nil, "agent=claude"},
		{[]string{"new", "--agent", "claude", "--ws", "../repo-feat"}, nil, "agent=claude ws=../repo-feat"},
		{[]string{"new", "--ws", "."}, nil, "ws=."},
		{[]string{"new", "--ws", ".", "--", "npm", "run", "dev"}, []string{"npm", "run", "dev"}, "ws=."},
		{[]string{"new", "--ws=.", "--", "claude", "--continue"}, []string{"claude", "--continue"}, "ws=."},
		{[]string{"new", "--", "-v"}, []string{"-v"}, ""},
		{[]string{"send", "ID", "hello", "world", "--enter"}, []string{"ID", "hello", "world"}, "enter=true"},
		{[]string{"send", "ID", `claude --settings '{"theme": "dark"}'`, "--enter"}, []string{"ID", `claude --settings '{"theme": "dark"}'`}, "enter=true"},
		{[]string{"send", "ID", "--enter", "hello"}, []string{"ID", "hello"}, "enter=true"},
		{[]string{"send", "ID", "--", "--enter"}, []string{"ID", "--enter"}, ""},
		{[]string{"read", "ID", "--scrollback"}, []string{"ID"}, "scrollback=true"},
		{[]string{"read", "ID"}, []string{"ID"}, ""},
		{[]string{"rename", "ID", "my", "name"}, []string{"ID", "my", "name"}, ""},
		{[]string{"switch", "ID"}, []string{"ID"}, ""},
		{[]string{"kill", "ID"}, []string{"ID"}, ""},
		{[]string{"new", "feat-x", "--project", "."}, []string{"feat-x"}, "project=."},
		{[]string{"new", "feat/prices"}, []string{"feat/prices"}, ""},
		{[]string{"rm", "/p"}, []string{"/p"}, ""},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("agent", "", "")
		fs.String("ws", ".", "")
		fs.Bool("enter", false, "")
		fs.Bool("scrollback", false, "")
		fs.String("project", ".", "")
		pos, err := parse(fs, tc.args[1:])

		var set []string

		fs.Visit(func(f *flag.Flag) { set = append(set, f.Name+"="+f.Value.String()) })

		if flags := strings.Join(set, " "); err != nil || !slices.Equal(pos, tc.pos) || flags != tc.flags {
			t.Errorf("%q: pos %q flags %q err %v, want %q %q", tc.args, pos, flags, err, tc.pos, tc.flags)
		}
	}
}
