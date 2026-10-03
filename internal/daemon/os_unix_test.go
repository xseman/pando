//go:build unix

package daemon

import (
	"syscall"
	"testing"
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
