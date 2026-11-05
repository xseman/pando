//go:build unix

package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// detached starts a process without a controlling terminal.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func TestProgramOf(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"/usr/bin/claude"}, "claude"},
		{[]string{"node", "/home/u/.local/bin/claude.js"}, "claude"},
		{[]string{"bash", "-l"}, "bash"},
		{[]string{}, ""},
	} {
		if got := programOf(c.argv); got != c.want {
			t.Errorf("programOf(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

// A job the shell left running in a process group of its own holds the
// terminal open; a kill gives up on it rather than hang, and so does stop.
func TestKillLeavesDetachedJob(t *testing.T) {
	d := start(t)()
	defer d.Close()

	dir := t.TempDir()
	pidf := filepath.Join(dir, "pid")

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": dir, "cmd": []string{"sh", "-c", "set -m; sleep 30 & echo $! > " + pidf + "; sleep 30"}}, &s)

	var pid int

	waitFor(t, "the job to start", func() bool {
		b, err := os.ReadFile(pidf)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))

		return err == nil && pid > 0
	})
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	done := make(chan error, 1)

	go func() { done <- proto.Call("session.kill", map[string]string{"id": s.ID}, nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}

	case <-time.After(15 * time.Second):
		t.Fatal("kill hung on a job holding the terminal")
	}
}
