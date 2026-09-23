package daemon

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/xseman/pando/internal/proto"
	"golang.org/x/sys/unix"
)

const (
	busyWindow   = 1500 * time.Millisecond // output this recent = running
	longBurst    = 3 * time.Second         // a burst this long ending unseen = attention
	viewedWindow = 2 * time.Second
)

// session is one PTY process plus the emulator holding its screen. The
// plain Emulator is guarded by mu (not SafeEmulator) so scrollback reads are
// consistent with writes; its output pipe is drained lock-free.
type session struct {
	spec proto.SessionSpec
	cmd  *exec.Cmd
	pty  *os.File
	done chan struct{}

	mu            sync.Mutex
	emu           *vt.Emulator
	lastOutput    time.Time
	busySince     time.Time
	lastViewed    time.Time
	exited        bool
	exitCode      int
	attention     bool
	title         string
	program       string // the foreground program, as last reported
	sentTitle     string // the title as last reported, status glyphs aside
	cursorVisible bool
	mouse         map[int]bool
	status        string
	screenAt      time.Time // lastOutput when the screen was last classified
	screenProg    string    // the program it was classified for
	seen          string    // screenState of it: blocked, running, idle or ""
}

func spawn(spec proto.SessionSpec, cols, rows int, onOutput, onExit func()) (*session, error) {
	cmd := exec.Command(spec.Cmd[0], spec.Cmd[1:]...)
	cmd.Dir = spec.Workspace
	cmd.Env = append(proto.WithoutNoColor(os.Environ()),
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"PANDO_SESSION="+spec.ID, "PANDO_RUNTIME_DIR="+proto.Dir())

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}

	s := &session{
		spec: spec, cmd: cmd, pty: f, done: make(chan struct{}),
		emu: vt.NewEmulator(cols, rows), cursorVisible: true, mouse: map[int]bool{}, status: "running",
	}
	s.emu.SetScrollbackSize(10000)
	// Answer OSC 10/11 color queries with the attached terminal's colors so
	// apps pick a readable theme (vt defaults to white on black).
	if c := ansi.XParseColor(spec.FG); c != nil {
		s.emu.SetDefaultForegroundColor(c)
	}

	if c := ansi.XParseColor(spec.BG); c != nil {
		s.emu.SetDefaultBackgroundColor(c)
	}
	// Callbacks run inside emu.Write, i.e. with s.mu already held.
	notify := func([]byte) bool { s.attention = true; return true }
	s.emu.RegisterOscHandler(9, notify)
	s.emu.RegisterOscHandler(777, notify)
	s.emu.SetCallbacks(vt.Callbacks{
		Bell:             func() { s.attention = true },
		Title:            func(t string) { s.title = strings.TrimSpace(t) },
		CursorVisibility: func(v bool) { s.cursorVisible = v },
		EnableMode:       func(m ansi.Mode) { s.setMode(m, true) },
		DisableMode:      func(m ansi.Mode) { s.setMode(m, false) },
	})

	go func() { _, _ = io.Copy(f, s.emu) }() // emulator replies + encoded keys -> app
	go func() {
		buf := make([]byte, 32*1024)

		var osc oscFilter

		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.mu.Lock()

				now := time.Now()
				if now.Sub(s.lastOutput) > busyWindow {
					s.busySince = now
				}

				s.lastOutput = now
				_, _ = s.emu.Write(osc.ascii(buf[:n])) // the emulator absorbs anything
				s.mu.Unlock()
				onOutput()
			}

			if err != nil {
				break
			}
		}

		err := cmd.Wait()

		s.mu.Lock()
		s.exited = true

		s.exitCode = cmd.ProcessState.ExitCode()
		if err != nil && s.exitCode == 0 {
			s.exitCode = -1
		}
		s.mu.Unlock()

		_ = f.Close()

		close(s.done)
		onExit()
	}()

	return s, nil
}

// oscFilter keeps OSC strings ASCII: the vt parser leaves an OSC on the first
// multibyte rune and prints the rest of the title on screen (claude's
// "✳ Claude Code" title garbled its banner). Non-ASCII runes are dropped;
// nothing outside OSC changes.
// ponytail: drop when charmbracelet/x/ansi parses UTF-8 inside OSC.
type oscFilter struct{ inOSC, esc bool }

func (f *oscFilter) ascii(b []byte) []byte {
	out := b[:0]
	for _, c := range b {
		switch {
		case f.esc:
			f.esc = false
			f.inOSC = c == ']' || (f.inOSC && c != '\\')

		case c == 0x1b:
			f.esc = true
		case f.inOSC && c == 0x07:
			f.inOSC = false
		case f.inOSC && c >= 0x80: // any byte of a multibyte rune
			continue
		}

		out = append(out, c)
	}

	return out
}

func (s *session) setMode(m ansi.Mode, on bool) {
	if d, ok := m.(ansi.DECMode); ok {
		switch d {
		case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
			s.mouse[int(d)] = on
		}
	}
}

func (s *session) kill() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()

	if !exited {
		// An agent started from the shell is a job with a process group of
		// its own; it goes too, and is waited for, so a daemon started next
		// does not find its conversation still open.
		pid := s.cmd.Process.Pid
		fg, _ := s.foreground()

		_ = syscall.Kill(-pid, syscall.SIGHUP)
		if fg > 0 && fg != pid {
			_ = syscall.Kill(-fg, syscall.SIGHUP)
		}

		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)

			<-s.done
		}

		if fg > 0 && fg != pid {
			waitGone(-fg, 2*time.Second)
		}
	}
	// Ends the io.Copy reader. Not emu.Close(): it writes an unsynchronized
	// flag that emu.Read checks, a data race with that reader.
	c, ok := s.emu.InputPipe().(io.Closer)
	if !ok {
		// vt types the pipe as io.Writer; today it is an *io.PipeWriter. A
		// panic here would take every other session down with it, so say what
		// leaked instead: the copier goroutine outlives the session.
		fmt.Fprintln(os.Stderr, "session", s.spec.ID, "input pipe is not an io.Closer; its output copier leaks")
		return
	}

	_ = c.Close()
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

// resumeWith types a command into the session once its shell is listening, so
// a restarted daemon brings the agent back instead of an empty prompt. A
// leftover process still holding the conversation is stopped first.
func (s *session) resumeWith(argv []string, leftover int) {
	if len(argv) == 0 {
		return
	}

	time.AfterFunc(600*time.Millisecond, func() {
		if leftover > 0 {
			_ = syscall.Kill(leftover, syscall.SIGHUP) // its terminal is gone
			waitGone(leftover, 3*time.Second)
		}

		_, _ = s.pty.WriteString(strings.Join(argv, " ") + "\r") // best effort
	})
}

// setResume records what would bring this session's agent back and the
// conversation that is, when known; it reports a change worth saving.
func (s *session) setResume(argv []string, c *proto.Conversation) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if slices.Equal(s.spec.Resume, argv) && ptrEqual(s.spec.Conversation, c) {
		return false
	}

	s.spec.Resume, s.spec.Conversation = argv, c

	return true
}

func ptrEqual[T comparable](a, b *T) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// notice prints lines dimmed on the session's screen, pando speaking rather
// than the program, and marks the session for attention.
func (s *session) notice(lines ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, l := range lines {
		_, _ = s.emu.WriteString("\x1b[2mpando: " + l + "\x1b[0m\r\n") // the emulator absorbs anything
	}

	s.attention = true
}

// tick recomputes status; returns true when anything a client shows changed.
// An agent's screen decides when its rules recognise it (an approval prompt
// waits in silence; a long tool call prints nothing), output timing otherwise.
func (s *session) tick(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, prevAttn := s.status, s.attention

	viewed := now.Sub(s.lastViewed) < viewedWindow
	if s.emu != nil && (s.screenAt != s.lastOutput || s.screenProg != s.program) {
		s.screenAt, s.screenProg = s.lastOutput, s.program
		s.seen = screenState(s.program, s.title, strings.Split(s.emu.String(), "\n"))
	}

	switch {
	case s.exited:
		s.status = "exited"
	case s.seen == "blocked":
		s.status = "blocked"
		if prev != "blocked" && !viewed {
			s.attention = true
		}

	case s.seen == "running", s.seen != "idle" && now.Sub(s.lastOutput) < busyWindow:
		s.status = "running"
	default:
		s.status = "idle"
		if prev == "running" && s.lastOutput.Sub(s.busySince) >= longBurst && !viewed {
			s.attention = true
		}
	}

	if s.status == "exited" && prev != "exited" && !viewed {
		s.attention = true
	}

	if viewed {
		s.attention = false
	}

	return prev != s.status || prevAttn != s.attention
}

func (s *session) info() proto.Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.status
	if s.exited {
		status = "exited" // the process is gone; tick would say so within half a second
	}

	return proto.Session{SessionSpec: s.spec, Status: status, ExitCode: s.exitCode, Attention: s.attention, Title: s.title, Program: s.program}
}

// setProgram records what runs in the foreground; it reports a change clients
// show, a new terminal title included. Leading status glyphs are ignored, so a
// spinner in the title does not flood them.
func (s *session) setProgram(p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	title := strings.TrimLeftFunc(s.title, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	changed := p != s.program || title != s.sentTitle
	s.program, s.sentTitle = p, title

	return changed
}

func (s *session) screen(p proto.ScreenParams) proto.Screen {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastViewed, s.attention = time.Now(), false
	if p.Cols > 0 && p.Rows > 0 && (p.Cols != s.emu.Width() || p.Rows != s.emu.Height()) {
		s.emu.Resize(p.Cols, p.Rows)

		if !s.exited {
			_ = pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)})
		}
	}

	lines := strings.Split(s.emu.Render(), "\n")
	sb := s.emu.Scrollback()

	scroll := min(max(p.Scroll, 0), sb.Len())
	if scroll > 0 && !s.emu.IsAltScreen() {
		all := make([]string, 0, sb.Len()+len(lines))
		for _, l := range sb.Lines() {
			all = append(all, l.Render())
		}

		all = append(all, lines...)
		end := len(all) - scroll
		lines = all[max(end-len(lines), 0):end]
	}

	pos := s.emu.CursorPosition()

	return proto.Screen{
		Lines: lines, CursorX: pos.X, CursorY: pos.Y,
		CursorVisible: s.cursorVisible && scroll == 0 && !s.exited,
		Mouse:         slices.Contains(slices.Collect(maps.Values(s.mouse)), true), Scrollback: sb.Len(),
	}
}

// text renders the session as plain text: the screen, with everything the
// emulator still holds in front of it when scrollback is set. A positive
// lines keeps only the last that many, which is how a caller tails a long
// transcript without pulling all of it through the socket.
func (s *session) text(scrollback bool, lines int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder

	if scrollback {
		for _, l := range s.emu.Scrollback().Lines() {
			b.WriteString(strings.TrimRight(l.String(), " "))
			b.WriteByte('\n')
		}
	}

	b.WriteString(s.emu.String())

	out := strings.TrimRight(b.String(), "\n ")
	if lines <= 0 {
		return out
	}

	all := strings.Split(out, "\n")

	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

// name is the name a rename gave the session, "" for one still named by what
// runs in it.
func (s *session) name() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.spec.Name
}

func (s *session) input(p proto.InputParams) error {
	if p.Text != "" {
		if _, err := s.pty.WriteString(p.Text); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, k := range p.Keys {
		// vt's SendKey only emits printable keys without modifiers, which
		// drops shifted letters; plain text goes through the same pipe instead.
		if k.Text != "" && uv.KeyMod(k.Mod)&^uv.ModShift == 0 {
			s.emu.SendText(k.Text)
		} else {
			s.emu.SendKey(uv.KeyPressEvent{Code: k.Code, Mod: uv.KeyMod(k.Mod), Text: k.Text})
		}
	}

	if p.Paste != "" {
		s.emu.Paste(p.Paste)
	}

	if m := p.Mouse; m != nil {
		ev := uv.Mouse{X: m.X, Y: m.Y, Button: uv.MouseButton(m.Button), Mod: uv.KeyMod(m.Mod)}
		switch m.Kind {
		case "click":
			s.emu.SendMouse(uv.MouseClickEvent(ev))
		case "release":
			s.emu.SendMouse(uv.MouseReleaseEvent(ev))
		case "motion":
			s.emu.SendMouse(uv.MouseMotionEvent(ev))
		case "wheel":
			s.emu.SendMouse(uv.MouseWheelEvent(ev))
		}
	}

	return nil
}
