package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
	"golang.org/x/sys/windows"
)

// detached starts a process on a hidden console of its own, none of the
// test's: what Setsid is on unix.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

// TestWindowsSessionExitCode: what the process prints reaches the screen, and
// its exit, which a pseudo console does not report, ends the session with
// its code.
func TestWindowsSessionExitCode(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"cmd", "/c", "echo READY & ping -n 2 127.0.0.1 >nul & exit 3"}}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: "READY", Timeout: 15000}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Until: []string{"exited"}, Timeout: 15000}, &s)

	if s.Status != "exited" || s.ExitCode != 3 {
		t.Fatalf("got status %q code %d, want exited 3", s.Status, s.ExitCode)
	}
}

func TestWindowsSessionClosesOnCleanExit(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"cmd", "/c", "exit 0"}}, &s)
	waitFor(t, "a clean exit to close the session", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 0
	})
}

// TestWindowsSessionInput types into an interactive cmd and reads the answer.
func TestWindowsSessionInput(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"cmd"}}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `>\s*$`, Timeout: 15000}, &s)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "set /a 6*7"}, nil)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "\r"}, nil)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `(?m)^42\s*$`, Timeout: 15000}, &s)
}

// TestWindowsSessionResize: a screen asked for at another size resizes the
// pseudo console, which the program sees.
func TestWindowsSessionResize(t *testing.T) {
	d := start(t)()
	defer d.Close()

	loop := `while ($true) { $s = $Host.UI.RawUI.WindowSize; Write-Host ('SIZE ' + $s.Width + 'x' + $s.Height); Start-Sleep -Milliseconds 200 }`

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"powershell", "-NoProfile", "-Command", loop}}, &s)

	var scr proto.Screen

	waitFor(t, "the program to see 50x12", func() bool {
		call(t, "session.screen", proto.ScreenParams{ID: s.ID, Cols: 50, Rows: 12}, &scr)

		return strings.Contains(strings.Join(scr.Lines, "\n"), "SIZE 50x12")
	})
}

// TestWindowsKillSession: killing a session ends its process and what that
// process started on the console.
func TestWindowsKillSession(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"cmd", "/c", "ping -n 600 127.0.0.1 >nul"}}, &s)

	d.mu.Lock()
	pid := d.sessions[s.ID].cmd.Process.Pid
	d.mu.Unlock()

	var child int

	waitFor(t, "ping to start under cmd", func() bool {
		q := "(Get-CimInstance Win32_Process -Filter 'ParentProcessId=" + strconv.Itoa(pid) + "').ProcessId"
		out, _ := exec.Command("powershell", "-NoProfile", "-Command", q).Output()
		child, _ = strconv.Atoi(strings.TrimSpace(string(out)))

		return child > 0
	})

	began := time.Now()
	call(t, "session.kill", map[string]string{"id": s.ID}, nil)
	t.Logf("session.kill took %v", time.Since(began))

	for _, p := range []int{pid, child} {
		waitFor(t, "process "+strconv.Itoa(p)+" to be gone", func() bool { return errors.Is(kill(p, 0), syscall.ESRCH) })
	}

	var ss []proto.Session
	if call(t, "session.list", nil, &ss); len(ss) != 0 {
		t.Fatalf("killed session still listed: %+v", ss)
	}
}

func TestWindowsKill(t *testing.T) {
	if err := kill(os.Getpid(), 0); err != nil {
		t.Fatalf("kill(self, 0) = %v, want nil", err)
	}

	done := exec.Command("cmd", "/c", "exit 0")
	if err := done.Run(); err != nil {
		t.Fatal(err)
	}

	if err := kill(done.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(exited, 0) = %v, want ESRCH", err)
	}

	c := exec.Command("ping", "-n", "600", "127.0.0.1")
	c.SysProcAttr = detached()

	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	if err := kill(c.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}

	if err := c.Wait(); err == nil {
		t.Fatal("killed ping exited cleanly")
	}

	if err := kill(c.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(killed, 0) = %v, want ESRCH", err)
	}
}

// TestWindowsShellSession: a shell session with nothing set opens PowerShell.
func TestWindowsShellSession(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "agent": "shell"}, &s)

	if !regexp.MustCompile(`(?i)powershell`).MatchString(strings.Join(s.Cmd, " ")) {
		t.Fatalf("shell session runs %q, want powershell", s.Cmd)
	}

	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `PS .*>`, Timeout: 20000}, &s)
}

// pingPid is the pid of the ping running to address, 0 while there is none.
func pingPid(address string) int {
	q := `(Get-CimInstance Win32_Process -Filter "Name='PING.EXE' and CommandLine like '%` + address + `%'").ProcessId`
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", q).Output()
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))

	return pid
}

// TestWindowsKillLeavesHiddenChildren: a child started on a console of its
// own is not on the session's console, so killing the session leaves it, as
// closing a Windows Terminal tab does.
func TestWindowsKillLeavesHiddenChildren(t *testing.T) {
	d := start(t)()
	defer d.Close()

	address := "127.0.0." + strconv.Itoa(2+os.Getpid()%250)
	ps := "Start-Process -WindowStyle Hidden ping -ArgumentList '-n','600','" + address + "'; Start-Sleep 600"

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"powershell", "-NoProfile", "-Command", ps}}, &s)

	var child int

	waitFor(t, "the hidden ping to start", func() bool { child = pingPid(address); return child > 0 })
	t.Cleanup(func() { _ = kill(child, syscall.SIGKILL) })
	call(t, "session.kill", map[string]string{"id": s.ID}, nil)
	time.Sleep(time.Second)

	if err := kill(child, 0); err != nil {
		t.Fatalf("the hidden ping went with its session: %v", err)
	}
}

// TestWindowsSessionEndLeavesHiddenChildren: what a session leaves running
// when its process exits by itself stays, as a detached process outlives a
// pty: a browser it opened, a daemon it started.
func TestWindowsSessionEndLeavesHiddenChildren(t *testing.T) {
	d := start(t)()
	defer d.Close()

	address := "127.0.1." + strconv.Itoa(2+os.Getpid()%250)
	ps := "Start-Process -WindowStyle Hidden ping -ArgumentList '-n','600','" + address + "'; Start-Sleep 3"

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"powershell", "-NoProfile", "-Command", ps}}, &s)

	var child int

	waitFor(t, "the hidden ping to start", func() bool { child = pingPid(address); return child > 0 })
	t.Cleanup(func() { _ = kill(child, syscall.SIGKILL) })
	waitFor(t, "the session to close", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 0
	})
	time.Sleep(time.Second)

	if err := kill(child, 0); err != nil {
		t.Fatalf("the hidden ping went with its session: %v", err)
	}
}

// TestWindowsWorktreeLoop is pando's own loop: a project, a worktree for a
// branch, a session in it, then the worktree gone from disk once the session
// is killed.
func TestWindowsWorktreeLoop(t *testing.T) {
	d := start(t)()
	defer d.Close()

	repo := t.TempDir()
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		mustGit(t, repo, a...)
	}

	var root string
	call(t, "project.add", map[string]string{"path": repo}, &root)

	var ws proto.Workspace
	call(t, "workspace.new", map[string]string{"project": root, "branch": "feat/x"}, &ws)

	if st, err := os.Stat(ws.Path); err != nil || !st.IsDir() || strings.Contains(ws.Path, "/") {
		t.Fatalf("worktree %q: %v", ws.Path, err)
	}

	// The session is asked for in the form the data directory was given in,
	// short names and all; it is the worktree's all the same.
	given := filepath.Join(d.dataDir, "worktrees", filepath.Base(root), "feat-x")

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": given, "cmd": []string{"cmd", "/c", "cd & ping -n 600 127.0.0.1 >nul"}}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: regexp.QuoteMeta(filepath.Base(ws.Path)), Timeout: 15000}, &s)

	if err := proto.Call("workspace.remove", map[string]string{"path": ws.Path}, nil); err == nil {
		t.Fatalf("removed the worktree under session %s (workspace %q, worktree %q)", s.ID, s.Workspace, ws.Path)
	}

	call(t, "session.kill", map[string]string{"id": s.ID}, nil)
	call(t, "workspace.remove", map[string]string{"path": ws.Path}, nil)

	if _, err := os.Stat(ws.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the worktree is still on disk: %v", err)
	}

	var list []proto.Workspace
	if call(t, "workspace.list", nil, &list); len(list) != 1 {
		t.Fatalf("workspaces = %+v", list)
	}
}

// TestWindowsRespawn: a restarted daemon brings its sessions back, on a new
// pseudo console each.
func TestWindowsRespawn(t *testing.T) {
	boot := start(t)
	d := boot()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"cmd"}}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `>\s*$`, Timeout: 15000}, &s)
	d.Close()

	d = boot()
	defer d.Close()

	var ss []proto.Session
	if call(t, "session.list", nil, &ss); len(ss) != 1 || ss[0].ID != s.ID {
		t.Fatalf("after a restart: %+v", ss)
	}

	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `>\s*$`, Timeout: 15000}, &s)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "set /a 6*7"}, nil)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "\r"}, nil)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: `(?m)^42\s*$`, Timeout: 15000}, &s)
}

