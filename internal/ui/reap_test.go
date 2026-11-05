//go:build unix

package ui

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestStartReaped leaves no zombie behind a sound or a link opened: a child
// nobody waits for stays in the process table until pando exits.
func TestStartReaped(t *testing.T) {
	cmd := exec.Command("true")
	if err := startReaped(cmd); err != nil {
		t.Fatal(err)
	}

	pid := cmd.Process.Pid
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) { // a zombie still takes a signal
			return
		}
	}

	t.Fatalf("pid %d was never reaped", pid)
}
