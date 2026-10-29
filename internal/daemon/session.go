package daemon

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/xseman/pando/internal/proto"
)

const (
	busyWindow   = 1500 * time.Millisecond // output this recent = running
	longBurst    = 3 * time.Second         // a burst this long ending unseen = attention
	viewedWindow = 2 * time.Second
	redrawWindow = 500 * time.Millisecond // output this soon after a resize or an input = the app's redraw or echo, no activity
)

// session is one process on a terminal of its own (a pty, a pseudo console
// on Windows) plus the emulator holding its screen. The plain Emulator is
// guarded by mu (not SafeEmulator) so scrollback reads are consistent with
// writes; its output pipe is drained lock-free.
type session struct {
	spec proto.SessionSpec
	cmd  *exec.Cmd
	pty  *term
	done chan struct{}

	mu         sync.Mutex
	emu        *vt.Emulator
	lastOutput time.Time
	busySince  time.Time
	lastViewed time.Time
	resized    time.Time // when a client last resized it
	inputAt    time.Time // its last input, which the app echoes or redraws for
	active     time.Time // its last input or change of status: what Updated reports
	touched    time.Time // its last input, or the use a restart carried over (used)
	exited     bool
	asleep     bool // put to sleep, or going to: the ticker leaves it be (sleepSession)
	exitCode   int
	attention  bool
	title      string
	program    string // the foreground program, as last reported
	sentTitle  string // the title as last reported, status glyphs aside
	// held is set while the user has scrolled the app's own view, a wheel
	// up sent to an app that takes the mouse: what its screen shows is
	// history, and a scrolled app may draw nothing while it works, so the
	// status stays as it was until a key, or output the app writes on its own.
	// ponytail: a turn that ends while scrolled shows running until then; the
	// view the app holds is not something a terminal tells.
	held bool
	// attached is set while the foreground attaches to a background job
	// (claude attach), which sets no title of its own: the shell's, the
	// command line, would stand in for it. jobName, the job's own name,
	// is reported instead; jobPID is the job's process, where its tools run.
	attached      bool
	jobName       string
	jobPID        int
	cursorVisible bool
	pushed        int      // lines scrolled into scrollback since it started: Screen.Pushed
	back          []string // the scrollback, rendered, oldest first: at most scrollMax rows
	cursorStyle   int      // DECSCUSR the app sent last: one for both screens, as in xterm
	mouse         map[int]bool
	status        string
	screenAt      time.Time        // lastOutput when the screen was last classified
	screenProg    string           // the program it was classified for
	seen          string           // screenState of it: blocked, running, idle or ""
	body          string           // aboveInput of it
	still         bool             // it is an idle prompt with the same body as before
	commands      []int            // the shell commands under the foreground, as last seen
	command       bool             // one of them was there the tick before too
	subagents     []proto.Subagent // what its agent runs now (setSubagents)

	started  time.Time  // set before the session is registered, read-only after
	fallback [][]string // the shells left to try if this one fails at once
}

func spawn(spec proto.SessionSpec, cols, rows int, onOutput, onExit func()) (*session, error) {
	cmd := exec.Command(spec.Cmd[0], spec.Cmd[1:]...)
	cmd.Dir = spec.Workspace
	cmd.Env = append(append(proto.WithoutNoColor(os.Environ()), spec.Env...),
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"PANDO_SESSION="+spec.ID, "PANDO_RUNTIME_DIR="+proto.Dir())

	f, err := startTerm(cmd, cols, rows)
	if err != nil {
		return nil, err
	}

	s := &session{
		spec: spec, cmd: cmd, pty: f, done: make(chan struct{}),
		emu: vt.NewEmulator(cols, rows), cursorVisible: true, mouse: map[int]bool{}, status: "running",
	}
	s.keepBack()
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
	s.emu.RegisterCsiHandler(ansi.Command(0, ' ', 'q'), func(p ansi.Params) bool {
		if n, _, _ := p.Param(0, 0); n <= 6 {
			s.cursorStyle = n // as sent: the emulator reads 0, the terminal's own, as 1
		}

		return false // the emulator keeps its own
	})
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
				s.drain(func() { _, _ = s.emu.Write(osc.ascii(buf[:n])) }) // the emulator absorbs anything
				s.mu.Unlock()
				onOutput()
			}

			if err != nil {
				break
			}
		}

		code := f.wait()

		s.mu.Lock()
		s.exited, s.exitCode = true, code
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

		s.pty.signal(syscall.SIGHUP)

		if fg > 0 && fg != pid {
			_ = kill(-fg, syscall.SIGHUP)
		}

		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
			s.pty.signal(syscall.SIGKILL)

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

// resumeWith types a command into the session once its shell is listening, so
// a restarted daemon brings the agent back instead of an empty prompt. A
// leftover process still holding the conversation is stopped first.
func (s *session) resumeWith(argv []string, leftover int) {
	if len(argv) == 0 {
		return
	}

	time.AfterFunc(600*time.Millisecond, func() {
		hangUp(leftover)

		_, _ = s.pty.WriteString(strings.Join(argv, " ") + "\r") // best effort
	})
}

// hangUp stops leftover process pid, whose terminal is gone, and waits for
// it; 0 is none.
func hangUp(pid int) {
	if pid > 0 {
		_ = kill(pid, syscall.SIGHUP)
		waitGone(pid, 3*time.Second)
	}
}

// setResume records what would bring this session's agent back, the
// conversation that is, when known, and whether the agent is the session's
// own process; it reports a change worth saving.
func (s *session) setResume(argv []string, c *proto.Conversation, own bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	own = own && len(argv) > 0
	if slices.Equal(s.spec.Resume, argv) && sameConversation(s.spec.Conversation, c) && s.spec.ResumeExec == own {
		return false
	}

	s.spec.Resume, s.spec.Conversation, s.spec.ResumeExec = argv, c, own

	return true
}

// ownAgent is the session's own process and the agent it runs, while it
// runs and the session is an agent's, not a shell's: what foreground says
// where the terminal cannot tell.
func (s *session) ownAgent() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.exited || isShellAgent(s.spec.Agent) {
		return 0, ""
	}

	return s.cmd.Process.Pid, s.spec.Agent
}

// failedAtOnce reports a shell that exited with an error as soon as it
// started, with another shell left to try in its place.
func (s *session) failedAtOnce() bool {
	info := s.info()
	return len(s.fallback) > 0 && info.ExitCode != 0 && time.Since(s.started) < quickExit
}

// size is the emulator's width and height.
func (s *session) size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.emu.Width(), s.emu.Height()
}

// ownEnv is the part of env, KEY=VALUE agent process pid ran with, that the
// session's shell does not set the same way: what was typed before the
// agent's name, and has to be again. An agent that is the session's own
// process has no shell: what it got beyond the daemon's environment counts,
// such as the session's Env.
func (s *session) ownEnv(pid int, env []string) []string {
	var out []string

	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")

		set := envOf(s.cmd.Process.Pid, k)
		if pid == s.cmd.Process.Pid {
			set = os.Getenv(k)
		}

		if set != v {
			out = append(out, kv)
		}
	}

	return out
}

func sameConversation(a, b *proto.Conversation) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Agent == b.Agent && a.ID == b.ID && a.Job == b.Job && slices.Equal(a.Env, b.Env)
}

// notice prints lines dimmed on the session's screen, pando speaking rather
// than the program, and marks the session for attention.
func (s *session) notice(lines ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.drain(func() {
		for _, l := range lines {
			_, _ = s.emu.WriteString("\x1b[2mpando: " + l + "\x1b[0m\r\n") // the emulator absorbs anything
		}
	})

	s.attention = true
}

// tick recomputes status; returns true when anything a client shows changed.
// An agent's screen decides blocked (an approval prompt waits in silence) and
// running (a long tool call prints nothing); output the app writes on its own
// says running over any screen, as an agent streaming its answer takes its
// spinner down; a shell command under the agent keeps it running too. While
// the user has scrolled the app's own view (held) the status stays.
func (s *session) tick(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, prevAttn := s.status, s.attention

	viewed := now.Sub(s.lastViewed) < viewedWindow

	prog := s.program
	if prog == "" && !isShellAgent(s.spec.Agent) { // the terminal cannot tell what runs (no /proc): an agent's session runs the agent, as ownAgent
		prog = s.spec.Agent
	}

	if s.emu != nil && (s.screenAt != s.lastOutput || s.screenProg != prog) {
		s.screenAt, s.screenProg = s.lastOutput, prog
		lines := strings.Split(s.emu.String(), "\n")
		s.seen = screenState(prog, s.title, lines)

		body := aboveInput(lines)
		s.still = s.seen == "idle" && body == s.body
		s.body = body
	}

	// live is output the app wrote on its own: not its redraw for a resize,
	// nor its echo of an input, both of which an idle app does too, nor a
	// redraw under an idle prompt (claude's "← N agents" footer, in every
	// session at once), which leaves the text above it as it was.
	live := now.Sub(s.lastOutput) < busyWindow && s.lastOutput.Sub(s.resized) > redrawWindow && s.lastOutput.Sub(s.inputAt) > redrawWindow && !s.still
	if live { // it draws for itself again: its screen is its own
		s.held = false
	}

	switch {
	case s.asleep:
		s.status = "asleep"
	case s.exited:
		s.status = "exited"
	case s.seen == "blocked":
		s.status = "blocked"
		if prev != "blocked" && !viewed {
			s.attention = true
		}

	case s.held: // the screen shows the view the user scrolled to
	// A program that has printed nothing yet has not drawn its prompt: it
	// starts busy, so session new --wait waits for it.
	case s.seen == "running", s.command, s.seen == "" && s.lastOutput.IsZero(), live:
		s.status = "running"
	case now.Sub(s.inputAt) < busyWindow: // what the app draws now answers the input, not what it does
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

	if prev != s.status && now.Sub(s.started) >= startGrace { // within it, its agent coming back
		s.active = now
	}

	return prev != s.status || prevAttn != s.attention
}

func (s *session) info() proto.Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.status
	switch {
	case s.asleep:
		status = "asleep"
	case s.exited:
		status = "exited" // the process is gone; tick would say so within half a second
	}

	updated := s.active
	if updated.IsZero() {
		updated = s.spec.Created
	}

	subagents := s.subagents
	if status == "asleep" || status == "exited" { // nothing runs them: the ticker no longer looks
		subagents = nil
	}

	return proto.Session{
		SessionSpec: s.spec, Status: status, ExitCode: s.exitCode, Attention: s.attention, Shell: status == "running" && s.command,
		Title: s.shownTitle(), Program: s.program, Updated: updated, Subagents: subagents,
	}
}

// setSubagents records the subagents program prog, process pid in front,
// runs now (subagentReaders); it reports a change clients show.
func (s *session) setSubagents(pid int, prog string, now time.Time) bool {
	if pid == 0 { // the terminal cannot tell what runs (no /proc), as remember
		pid, prog = s.ownAgent()
	}

	s.mu.Lock()
	c := s.spec.Conversation
	s.mu.Unlock()

	var subagents []proto.Subagent
	if read := subagentReaders[prog]; read != nil {
		subagents = read(pid, c, now) // files read outside s.mu, which info waits on
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if slices.Equal(s.subagents, subagents) {
		return false
	}

	s.subagents = subagents

	return true
}

// shownTitle is the title clients show: a background job's name while the
// foreground only attaches to it, the terminal's title otherwise. s.mu is held.
func (s *session) shownTitle() string {
	if s.attached {
		return s.jobName
	}

	return s.title
}

// setCommands records the shell commands running under the foreground
// program, or under the job it attaches to (worker). One seen on two ticks in
// a row keeps the session running whatever its screen says: claude sits at
// its idle prompt while a background shell works. A status line or hook
// command is gone by the next tick. It reports whether that changed.
func (s *session) setCommands(pids []int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	was := s.command
	s.command = slices.ContainsFunc(pids, func(p int) bool { return slices.Contains(s.commands, p) })
	s.commands = pids

	return was != s.command
}

// setJob records whether the foreground attaches to a background job, the
// job's name and its process, 0 when unknown.
func (s *session) setJob(attached bool, name string, pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attached, s.jobName, s.jobPID = attached, strings.TrimSpace(name), pid
}

// worker is the process whose shell commands are the session's work: the
// background job's while foreground fg only attaches to it, else fg. A
// `claude attach` runs no tools of its own; the job's process does.
func (s *session) worker(fg int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cmp.Or(s.jobPID, fg) // set by an attach alone
}

// setProgram records what runs in the foreground; it reports a change clients
// show, a new terminal title included. Leading status glyphs are ignored, so a
// spinner in the title does not flood them.
func (s *session) setProgram(p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	title := strings.TrimLeftFunc(s.shownTitle(), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	changed := p != s.program || title != s.sentTitle
	s.program, s.sentTitle = p, title

	return changed
}

func (s *session) screen(p proto.ScreenParams) proto.Screen {
	s.mu.Lock()
	defer s.mu.Unlock()

	// tick clears attention from lastViewed, and reports it so clients hear.
	s.lastViewed = time.Now()
	if p.Cols > 0 && p.Rows > 0 && (p.Cols != s.emu.Width() || p.Rows != s.emu.Height()) {
		s.drain(func() { s.emu.Resize(p.Cols, p.Rows) })
		s.resized = time.Now()

		if !s.exited {
			_ = s.pty.resize(p.Cols, p.Rows)
		}
	}

	lines := strings.Split(s.emu.Render(), "\n")
	// The alternate screen has no scrollback of its own: what the emulator
	// holds is the shell's from before the app, which it does not show.
	alt := s.emu.IsAltScreen()

	back := len(s.back)
	if alt {
		back = 0
	}

	scroll := p.Scroll
	if scroll > 0 && p.Pushed > 0 { // the page moves up with its text
		scroll += s.pushed - p.Pushed
	}

	scroll = min(max(scroll, 0), back)
	if scroll > 0 { // the page scroll rows up the scrollback and screen together: only its rows are rendered
		end := back + len(lines) - scroll
		page := make([]string, 0, len(lines))

		for i := max(end-len(lines), 0); i < end; i++ {
			if i < back {
				page = append(page, s.back[i])
			} else {
				page = append(page, lines[i-back])
			}
		}

		lines = page
	}

	pos := s.emu.CursorPosition()

	return proto.Screen{
		Lines: lines, CursorX: pos.X, CursorY: pos.Y,
		CursorVisible: s.cursorVisible && scroll == 0 && !s.exited, CursorStyle: s.cursorStyle,
		Mouse: slices.Contains(slices.Collect(maps.Values(s.mouse)), true), Scrollback: back, AltScreen: alt,
		Cols: s.emu.Width(), Cwd: s.cwd(), Pushed: s.pushed,
	}
}

// scrollMax is the scrollback lines a session keeps.
const scrollMax = 10000

// keepBack sets the emulator up for drain: its scrollback only holds rows
// until drain moves them into back, the alternate screen keeps none, and
// ED 3 clears back.
func (s *session) keepBack() {
	s.emu.SetScrollbackSize(scrollMax)
	noAltScrollback(s.emu)
	s.emu.RegisterCsiHandler('J', func(p ansi.Params) bool {
		if n, _, _ := p.Param(0, 0); n == 3 && !s.emu.IsAltScreen() {
			s.back = nil
		}

		return false // the emulator clears its own
	})
}

// drain runs f, which writes to the emulator or resizes it, and moves the
// rows it scrolled off the main screen into back, rendered: a row of vt's
// 112-byte cells takes some 80 times the bytes. Callers hold s.mu.
func (s *session) drain(f func()) {
	f()

	sb := s.emu.Scrollback()
	rows := sb.Lines()

	for _, l := range rows {
		s.back = append(s.back, l.Render())
	}

	s.pushed += len(rows)
	clear(rows) // sb keeps the array: let the cells go
	sb.Clear()

	if n := len(s.back) - scrollMax; n > 0 {
		s.back = slices.Delete(s.back, 0, n)
	}
}

// noAltScrollback drops the scrollback vt gives the alternate screen: what
// scrolls off a fullscreen app is never shown, yet would keep up to 10 000
// rows of cells, 100 MiB. vt has no setter for it, so this reaches the
// screen by reflection, and leaves it be if vt's layout changes
// (TestScrollbackMemory notices).
func noAltScrollback(e *vt.Emulator) {
	f := reflect.ValueOf(e).Elem().FieldByName("scrs")
	if f.Kind() != reflect.Array || f.Len() != 2 || f.Type().Elem() != reflect.TypeFor[vt.Screen]() {
		return
	}

	(*vt.Screen)(unsafe.Pointer(f.Index(1).UnsafeAddr())).SetScrollback(nil)
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
		for _, l := range s.back {
			b.WriteString(strings.TrimRight(ansi.Strip(l), " "))
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

// xtermFinal and xtermTilde are the special keys xterm sends with their
// modifiers: CSI 1;m <final> and CSI <n>;m ~.
var (
	xtermFinal = map[rune]byte{
		uv.KeyUp: 'A', uv.KeyDown: 'B', uv.KeyRight: 'C', uv.KeyLeft: 'D', uv.KeyHome: 'H', uv.KeyEnd: 'F',
		uv.KeyF1: 'P', uv.KeyF2: 'Q', uv.KeyF3: 'R', uv.KeyF4: 'S',
	}
	xtermTilde = map[rune]int{
		uv.KeyInsert: 2, uv.KeyDelete: 3, uv.KeyPgUp: 5, uv.KeyPgDown: 6,
		uv.KeyF5: 15, uv.KeyF6: 17, uv.KeyF7: 18, uv.KeyF8: 19, uv.KeyF9: 20, uv.KeyF10: 21, uv.KeyF11: 23, uv.KeyF12: 24,
	}
)

// xtermKey is xterm's encoding of a special key held with shift, ctrl or
// meta, which is how readline, zsh and agents read ctrl+← and shift+end: the
// modifier parameter is 1 plus shift 1, alt 2, ctrl 4, meta 8. ok is false
// for what vt's SendKey encodes itself: other keys, and none or alt alone.
func xtermKey(code rune, mod uv.KeyMod) (seq string, ok bool) {
	mod &= uv.ModShift | uv.ModAlt | uv.ModCtrl | uv.ModMeta
	if mod&^uv.ModAlt == 0 {
		return "", false
	}

	m := 1 + int(mod)

	if f, ok := xtermFinal[code]; ok {
		return "\x1b[1;" + strconv.Itoa(m) + string(f), true
	}

	if n, ok := xtermTilde[code]; ok {
		return "\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(m) + "~", true
	}

	return "", false
}

// input writes to the app. Text takes the emulator's pipe with the keys, the
// paste and the mouse: written straight to the PTY it could overtake keys the
// copier still held, as a ctrl+u landing after the next call's line.
func (s *session) input(p proto.InputParams) error {
	if s.isAsleep() {
		return errAsleep
	}

	if p.Text != "" {
		if _, err := io.WriteString(s.emu.InputPipe(), p.Text); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.active = time.Now()
	s.touched, s.inputAt = s.active, s.active

	if len(p.Keys) > 0 || p.Text != "" || p.Paste != "" { // typing takes an app back to its live view
		s.held = false
	}

	if m := p.Mouse; m != nil && m.Kind == "wheel" && uv.MouseButton(m.Button) == uv.MouseWheelUp {
		s.held = true
	}

	for _, k := range p.Keys {
		// vt's SendKey only emits printable keys without modifiers, which
		// drops shifted letters; plain text goes through the same pipe instead.
		seq, special := xtermKey(k.Code, uv.KeyMod(k.Mod))

		switch {
		case k.Text != "" && uv.KeyMod(k.Mod)&^uv.ModShift == 0:
			s.emu.SendText(k.Text)
		case special: // ctrl+←, shift+end, …: vt drops a special key with modifiers
			s.emu.SendText(seq)
		default:
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
