//go:build unix

package daemon

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// defaultShell is what a shell session opens unless told otherwise.
func defaultShell() string { return cmp.Or(os.Getenv("SHELL"), "/bin/sh") }

// fallbackShells are what a shell session falls back on when every shell it
// was set to fails.
var fallbackShells = []string{"bash", "/bin/sh"}

// execLine runs resume command line argv, written for a shell to read, as a
// session's own process: env takes the KEY=VALUE before the program.
func execLine(argv []string) []string {
	return []string{"/bin/sh", "-c", "exec env " + strings.Join(argv, " ")}
}

// kill is kill(2): a negative pid is a process group.
var kill = syscall.Kill

// term is a session's pty, the master side of its process's terminal.
type term struct {
	*os.File
	cmd *exec.Cmd
}

func startTerm(cmd *exec.Cmd, cols, rows int) (*term, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}

	return &term{File: f, cmd: cmd}, nil
}

// signal sends sig to the session's process group.
func (t *term) signal(sig syscall.Signal) { _ = syscall.Kill(-t.cmd.Process.Pid, sig) }

func (t *term) resize(cols, rows int) error {
	return pty.Setsize(t.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// wait waits for the process to exit and returns its exit code, -1 when it
// did not exit by itself.
func (t *term) wait() int {
	err := t.cmd.Wait()

	code := t.cmd.ProcessState.ExitCode()
	if err != nil && code == 0 {
		code = -1
	}

	return code
}

// foreground is the program the session's terminal is running right now and
// the leader of its process group: the agent the user started in the shell,
// or the shell itself. The pid is 0 when the terminal is gone.
// ponytail: /proc and one ioctl, no process tree walk.
func (s *session) foreground() (int, string) {
	// Through the raw conn, not Fd(): the reader may be closing the pty.
	rc, err := s.pty.SyscallConn()
	if err != nil {
		return 0, ""
	}

	var pgrp int

	cerr := rc.Control(func(fd uintptr) { pgrp, err = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) })
	if cerr != nil || err != nil || pgrp <= 0 {
		return 0, ""
	}

	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pgrp))
	if err != nil {
		return 0, ""
	}

	argv := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")

	return pgrp, programOf(argv)
}

// runtimes run a script whose name is the program the user means.
var runtimes = []string{"node", "bun", "deno", "python", "python3", "ruby", "perl", "sh", "bash"}

// programOf names the program an argv runs: node cli.js is "cli".
func programOf(argv []string) string {
	if len(argv) == 0 || argv[0] == "" {
		return ""
	}

	name := filepath.Base(argv[0])
	if len(argv) > 1 && slices.Contains(runtimes, name) && !strings.HasPrefix(argv[1], "-") {
		name = filepath.Base(argv[1])
	}

	return strings.TrimSuffix(name, filepath.Ext(name))
}
