package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// The test binary doubles as pando, so a CLI call can autostart `pando serve`.
func TestMain(m *testing.M) {
	if os.Getenv("PANDO_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pando.lock")

	a, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	b, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()

	if err := lockFile(a); err != nil {
		t.Fatalf("first lock: %v", err)
	}

	if err := lockFile(b); err == nil {
		t.Fatal("second lock taken while the first holds")
	}

	_ = a.Close()

	if err := lockFile(b); err != nil {
		t.Fatalf("lock after the holder closed: %v", err)
	}
}

// cli points pando at scratch directories and returns them and a runner of
// the test binary as the pando CLI, which fails the test on an error. The
// daemon the first call starts is shut down at the end.
func cli(t *testing.T) (string, func(args ...string) string) {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("PANDO_TEST_MAIN", "1")
	t.Setenv("PANDO_RUNTIME_DIR", filepath.Join(tmp, "run"))
	t.Setenv("PANDO_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	t.Setenv("PANDO_DATA_DIR", filepath.Join(tmp, "data"))
	t.Cleanup(func() {
		_ = proto.Call("shutdown", nil, nil)
		for i := 0; i < 50 && proto.Call("ping", nil, nil) == nil; i++ {
			time.Sleep(50 * time.Millisecond)
		}

		time.Sleep(500 * time.Millisecond)
	})

	return tmp, func(args ...string) string {
		t.Helper()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, os.Args[0], args...).CombinedOutput()
		if err != nil {
			t.Fatalf("pando %s: %v\n%s", strings.Join(args, " "), err, out)
		}

		return string(out)
	}
}

// TestWindowsDaemon drives the CLI end to end: the first call autostarts the
// daemon on its socket, a session runs, a second daemon is refused, stop ends it.
func TestWindowsDaemon(t *testing.T) {
	tmp, pando := cli(t)

	var s proto.Session
	if err := json.Unmarshal([]byte(pando("session", "new", "--ws", tmp, "cmd", "/c", "echo E2E_OK & ping -n 120 127.0.0.1 >nul")), &s); err != nil {
		t.Fatal(err)
	}

	pando("session", "wait", s.ID, "--match", "E2E_OK", "--timeout", "15000")

	if got := pando("session", "read", s.ID); !strings.Contains(got, "E2E_OK") {
		t.Fatalf("session read: %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, os.Args[0], "serve").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "another pando daemon") {
		t.Fatalf("second serve: %v %q, want refused", err, out)
	}

	// A daemon started from inside a session is on a console of its own:
	// killing the session leaves it running.
	run2 := filepath.Join(tmp, "run2")
	ps := "$env:PANDO_RUNTIME_DIR='" + run2 + "'; $env:PANDO_CONFIG_DIR='" + filepath.Join(tmp, "cfg2") +
		"'; $env:PANDO_DATA_DIR='" + filepath.Join(tmp, "data2") + "'; & '" + os.Args[0] + "' session ls; Start-Sleep 600"

	var nested proto.Session
	if err := json.Unmarshal([]byte(pando("session", "new", "--ws", tmp, "--", "powershell", "-NoProfile", "-Command", ps)), &nested); err != nil {
		t.Fatal(err)
	}

	second := func() error {
		t.Setenv("PANDO_RUNTIME_DIR", run2)
		defer t.Setenv("PANDO_RUNTIME_DIR", filepath.Join(tmp, "run"))

		return proto.Call("ping", nil, nil)
	}
	t.Cleanup(func() {
		t.Setenv("PANDO_RUNTIME_DIR", run2)
		_ = proto.Call("shutdown", nil, nil)
	})

	for i := 0; i < 300 && second() != nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}

	if err := second(); err != nil {
		t.Fatalf("the daemon started in a session does not answer: %v", err)
	}

	pando("session", "kill", nested.ID)
	time.Sleep(time.Second)

	if err := second(); err != nil {
		t.Fatalf("the daemon started in a session went with it: %v", err)
	}

	pando("session", "kill", s.ID)

	if got := pando("session", "ls"); strings.Contains(got, s.ID) {
		t.Fatalf("killed session listed: %s", got)
	}

	pando("stop")

	for i := 0; i < 100 && proto.Call("ping", nil, nil) == nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}

	if proto.Call("ping", nil, nil) == nil {
		t.Fatal("daemon still answers after stop")
	}
}

// TestWindowsClaude runs `pando claude` against a fake claude.cmd: attaching
// to the job `claude --bg` names, or running claude as it is when it is too
// old for --bg, pando exiting with its code either way.
func TestWindowsClaude(t *testing.T) {
	dir := t.TempDir()
	fake := "@echo off\r\nif \"%1\"==\"--bg\" goto bg\r\nif \"%1\"==\"attach\" exit /b 5\r\nexit /b 7\r\n" +
		":bg\r\nif \"%2\"==\"old\" (echo error: unknown option '--bg' & exit /b 1)\r\n" +
		"echo   claude attach abc123   open in this terminal\r\nexit /b 0\r\n"

	if err := os.WriteFile(filepath.Join(dir, "claude.cmd"), []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}

	for args, want := range map[string]int{"new": 5, "old": 7} {
		cmd := exec.Command(os.Args[0], "claude", args)
		cmd.Env = append(os.Environ(), "PANDO_TEST_MAIN=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		out, err := cmd.CombinedOutput()

		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != want {
			t.Errorf("pando claude %s: %v, want exit %d\n%s", args, err, want, out)
		}
	}
}
