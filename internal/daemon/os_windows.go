package daemon

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultShell is PowerShell: Windows has no $SHELL, and Git Bash's is an
// MSYS path no Windows process can start.
func defaultShell() string { return "powershell" }

// fallbackShells are what a shell session falls back on when every shell it
// was set to fails.
var fallbackShells = []string{"powershell", "cmd"}

// kill stands in for kill(2) on one process: signal 0 asks whether pid runs,
// any other ends it. Windows has no process groups; a session's term signals
// what is on its console.
func kill(pid int, sig syscall.Signal) error {
	access := uint32(windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if sig != 0 {
		access |= windows.PROCESS_TERMINATE
	}

	h, err := windows.OpenProcess(access, false, uint32(pid))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return syscall.EPERM
	}

	if err != nil {
		return syscall.ESRCH
	}

	defer func() { _ = windows.CloseHandle(h) }()

	if ev, _ := windows.WaitForSingleObject(h, 0); ev == windows.WAIT_OBJECT_0 {
		return syscall.ESRCH // exited; a handle open elsewhere keeps it listed
	}

	if sig == 0 {
		return nil
	}

	return windows.TerminateProcess(h, 1)
}

// term is a session's pseudo console (ConPTY). Unlike a pty it outlives its
// process, so the process is waited for here and the console closed then:
// that is what ends the output the session reads.
type term struct {
	in, out *os.File
	cmd     *exec.Cmd
	exited  chan struct{}
	code    int // set before exited closes

	mu sync.Mutex
	pc windows.Handle // 0 once closed: a closed console's handle is freed memory
}

// startTerm starts cmd on a new pseudo console of cols×rows and sets
// cmd.Process. exec.Cmd cannot give a process a pseudo console, so the
// process is created here, from cmd's Path, Args, Dir and Env.
func startTerm(cmd *exec.Cmd, cols, rows int) (*term, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}

	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}

	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		_ = windows.CloseHandle(inW)

		return nil, err
	}

	t := &term{in: os.NewFile(uintptr(inW), "conin"), out: os.NewFile(uintptr(outR), "conout"), cmd: cmd, exited: make(chan struct{})}

	err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inR, outW, 0, &t.pc)
	_ = windows.CloseHandle(inR) // the console holds its own copies, if it started
	_ = windows.CloseHandle(outW)

	if err == nil {
		err = t.spawn(cmd)
	}

	if err != nil {
		_ = t.Close()
		return nil, err
	}

	go func() {
		t.code = -1
		if st, err := cmd.Process.Wait(); err == nil {
			t.code = st.ExitCode()
		}

		close(t.exited)
		t.closeConsole()
	}()

	return t, nil
}

// spawn creates cmd's process on the console.
func (t *term) spawn(cmd *exec.Cmd) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()

	// The attribute's value is the console handle itself, not a pointer to
	// it; read as a pointer, not converted, so vet sees no uintptr turned
	// into one.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&t.pc)), unsafe.Sizeof(t.pc)); err != nil {
		return err
	}

	name, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}

	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}

	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return err
		}
	}

	env := utf16.Encode([]rune(strings.Join(cmd.Environ(), "\x00") + "\x00\x00"))

	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// No standard handles: without the flag a console process takes the
	// daemon's, its log, instead of the console's.
	si.Flags = windows.STARTF_USESTDHANDLES

	var pi windows.ProcessInformation

	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(name, line, nil, nil, false, flags, &env[0], dir, &si.StartupInfo, &pi); err != nil {
		return err
	}

	defer func() { _ = windows.CloseHandle(pi.Thread); _ = windows.CloseHandle(pi.Process) }()

	// Opened while pi.Process is still open, so the pid cannot be reused.
	if cmd.Process, err = os.FindProcess(int(pi.ProcessId)); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
	}

	return err
}

func (t *term) Read(p []byte) (int, error) { return t.out.Read(p) }

func (t *term) Write(p []byte) (int, error) { return t.in.Write(p) }

func (t *term) WriteString(s string) (int, error) { return t.in.WriteString(s) }

func (t *term) Close() error {
	t.closeConsole()

	return errors.Join(t.in.Close(), t.out.Close())
}

// signal is the session's hangup or kill. A hangup closes the console, which
// asks every console process on it to exit (CTRL_CLOSE_EVENT), as closing a
// terminal's tab does; the close returns only once they have, seconds on
// Windows 10, so it does not hold up the kill. A kill ends the session's
// process. What runs on a console of its own outlives both, as a detached
// process outlives a pty: a browser the session opened, a daemon it started.
func (t *term) signal(sig syscall.Signal) {
	if sig == syscall.SIGHUP {
		go t.closeConsole()
		return
	}

	_ = t.cmd.Process.Kill()
}

func (t *term) resize(cols, rows int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.pc == 0 {
		return os.ErrClosed
	}

	return windows.ResizePseudoConsole(t.pc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// closeConsole closes the pseudo console, which ends the console processes
// still on it as closing a window would (CTRL_CLOSE_EVENT). It may wait for
// the output to be read, so it runs outside mu.
func (t *term) closeConsole() {
	t.mu.Lock()
	pc := t.pc
	t.pc = 0
	t.mu.Unlock()

	if pc != 0 {
		windows.ClosePseudoConsole(pc)
	}
}

// wait waits for the process to exit and returns its exit code, -1 when it
// could not be had.
func (t *term) wait() int {
	<-t.exited

	return t.code
}

// foreground is unknown on Windows: a console has no foreground process
// group to ask.
// ponytail: no process tree walk; the screen and output timing decide, as on macOS.
func (s *session) foreground() (int, string) { return 0, "" }
