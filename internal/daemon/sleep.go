package daemon

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/xseman/pando/internal/proto"
)

// sleepUnit is what sleep_after counts in; tests shorten it.
var sleepUnit = time.Minute

// wakeWait bounds how long session.wake waits for the agent it resumes to
// come up in front and settle.
var wakeWait = 20 * time.Second

// asleepLine is what an asleep session's screen ends with.
const asleepLine = "asleep: open it, or pando session wake, to bring it back"

// dormant is session spec as a restarted daemon lists it while it is asleep:
// a screen with nothing running behind it, which session.wake replaces.
func dormant(spec proto.SessionSpec, cols, rows int) *session {
	if cols <= 0 || rows <= 0 {
		cols, rows = 120, 40
	}

	s := &session{
		spec: spec, done: make(chan struct{}), emu: vt.NewEmulator(cols, rows),
		cursorVisible: true, mouse: map[int]bool{}, status: "asleep", exited: true, asleep: true, touched: spec.Active, active: spec.Active,
	}
	s.keepBack()
	close(s.done)

	if c, ok := s.emu.InputPipe().(io.Closer); ok { // no app reads it: a reply the emulator writes fails at once
		_ = c.Close()
	}

	s.lull()

	return s
}

// lull writes asleepLine on the session's screen, dimmed as a notice is,
// without asking for attention. s.mu is held.
func (s *session) lull() {
	s.drain(func() { _, _ = s.emu.WriteString("\r\n\x1b[2mpando: " + asleepLine + "\x1b[0m\r\n") })
}

// isAsleep reports a session put to sleep, or going to sleep.
func (s *session) isAsleep() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.asleep
}

// procfs reports whether the system says what runs in a terminal (Linux):
// without it a woken agent cannot be told from its shell.
func procfs() bool {
	_, err := os.Stat("/proc/self/stat")
	return err == nil
}

// knownAgent reports whether program prog is an [agents] preset's that is
// not a shell: one whose conversation a session without a resume would lose.
func (d *Daemon) knownAgent(prog string) bool {
	_, ok := d.state.Agents[prog]
	return ok && !isShellAgent(prog)
}

// canSleep reports why session s cannot be put to sleep by hand, nil when it
// can: an agent pando cannot bring back would lose its conversation. What
// else runs in front, a dev server or an editor, is not brought back: the
// session wakes to its shell. It remembers what runs first, as the ticker
// does, so an agent started since its last tick counts. d.mu is held.
func (d *Daemon) canSleep(s *session) error {
	if !s.isAsleep() && !d.moving[s.spec.ID] {
		pid, prog := s.foreground()
		_ = s.setProgram(prog) // clients hear of it with the sleep
		d.remember(s, pid, prog)
	}

	info := s.info()

	switch {
	case s.isAsleep():
		return fmt.Errorf("session %s is asleep already", info.ID)
	case info.Status == "exited":
		return fmt.Errorf("session %s has exited: nothing runs to stop", info.ID)
	case d.moving[info.ID]:
		return fmt.Errorf("session %s is going to sleep or waking", info.ID)
	case info.Resume == nil && info.Conversation == nil && d.knownAgent(info.Program):
		return fmt.Errorf("%s cannot be resumed ([resume] has nothing for it): kill the session instead", info.Program)
	}

	return nil
}

// sleepy reports whether session s goes to sleep on its own at now: it, and
// each shell it is the parent of, which goes with it, is unused. Those shells
// never sleep on their own. d.mu is held.
func (d *Daemon) sleepy(s *session, now time.Time) bool {
	after := time.Duration(d.state.Settings.Sleep) * sleepUnit
	if after <= 0 || s.isAsleep() || d.moving[s.spec.ID] || s.info().Parent != "" || !d.unused(s, now, after) {
		return false
	}

	for _, c := range d.sessions {
		if c.info().Parent == s.spec.ID && !c.isAsleep() && !d.unused(c, now, after) {
			return false
		}
	}

	return true
}

// unused reports session s idle and unused for after, its finished turn seen
// (attention), with what runs in front one pando brings back: an agent with
// a resume, or its own shell at the prompt. A server or an editor, or
// nothing that says what runs (no /proc), keeps it awake. d.mu is held.
func (d *Daemon) unused(s *session, now time.Time, after time.Duration) bool {
	info := s.info()
	if info.Status != "idle" || info.Attention || now.Sub(s.used()) < after {
		return false
	}

	return info.Resume != nil || info.Program != "" && info.Program == programOf(info.Cmd) && !d.knownAgent(info.Program)
}

// startGrace is how long after a session starts its status changes are its
// agent coming back, not work: they do not count as use.
const startGrace = 30 * time.Second

// used is when anyone last typed into the session, looked at it or saw its
// status change: what sleep_after counts from, and what Active keeps over a
// restart. A session nobody has used counts from its start.
func (s *session) used() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.touched
	if s.lastViewed.After(t) {
		t = s.lastViewed
	}

	if s.active.Sub(s.started) > startGrace && s.active.After(t) {
		t = s.active
	}

	return cmp.Or(t, s.started)
}

// sleepSession puts session id to sleep: its process and any background job
// it is attached to are stopped, its screen stays listed, and its spec says
// how to bring it back. The shells it is the parent of go to sleep with it.
// auto is the ticker's: only a session sleepy still goes.
func (d *Daemon) sleepSession(id string, auto bool) error {
	s, err := d.session(id)
	if err != nil {
		return err
	}

	d.mu.Lock()
	if auto && !d.sleepy(s, time.Now()) {
		d.mu.Unlock()
		return nil
	}

	if err := d.canSleep(s); err != nil {
		d.mu.Unlock()
		return err
	}
	// Under d.mu: the ticker, which holds it, either remembered the agent
	// before this or skips the session from now on, never sees it gone.
	id = s.spec.ID
	d.moving[id] = true

	s.mu.Lock()
	s.asleep = true
	s.mu.Unlock()
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.moving, id)
		d.mu.Unlock()
	}()

	s.kill()

	spec := s.info().SessionSpec
	d.stopJob(spec.Conversation, spec.Workspace)

	s.mu.Lock()
	s.spec.Asleep, s.status, s.attention = true, "asleep", false
	s.lull()
	s.mu.Unlock()

	d.mu.Lock()
	err = d.save()

	var children []string

	for _, cid := range d.order {
		if c := d.sessions[cid].info(); c.Parent == id && !c.Asleep {
			children = append(children, cid)
		}
	}
	d.mu.Unlock()

	d.broadcast(proto.Event{Kind: "sessions"})

	for _, cid := range children {
		_ = d.sleepSession(cid, false) // a shell that has exited meanwhile stays as it is
	}

	return err
}

// wakeSession brings asleep session id back, as a restart would: its shell
// and its agent continuing the conversation it had open. It returns once the
// agent is in front and at its prompt, or after wakeWait. The shells it is
// the parent of wake with it. A session awake already is returned as it is.
func (d *Daemon) wakeSession(id string) (proto.Session, error) {
	s, err := d.session(id)
	if err != nil {
		return proto.Session{}, err
	}

	d.mu.Lock()
	id = s.spec.ID

	if d.moving[id] {
		d.mu.Unlock()
		return s.info(), fmt.Errorf("session %s is going to sleep or waking", id)
	}

	if !s.isAsleep() {
		d.mu.Unlock()
		return s.info(), nil
	}

	d.moving[id] = true
	by := map[string]string{} // the conversations sessions awake continue

	for _, o := range d.sessions {
		if oi := o.info(); oi.Conversation != nil && !oi.Asleep && oi.ID != id {
			by[oi.Conversation.Agent+"/"+oi.Conversation.ID] = cmp.Or(oi.Name, oi.ID)
		}
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.moving, id)
		d.mu.Unlock()
	}()

	spec := s.info().SessionSpec
	spec.Asleep = false
	cols, rows := s.size()

	ns, resumed, err := d.respawn(spec, by, cols, rows)
	if err != nil {
		return s.info(), err
	}

	ns.mu.Lock()
	ns.lastViewed = time.Now() // waking it is using it: sleep_after starts over
	ns.mu.Unlock()

	d.mu.Lock()
	err = d.save()

	var children []string

	for _, cid := range d.order {
		if c := d.sessions[cid].info(); c.Parent == id && c.Asleep {
			children = append(children, cid)
		}
	}
	d.mu.Unlock()

	d.broadcast(proto.Event{Kind: "sessions"})

	for _, cid := range children {
		_, _ = d.wakeSession(cid) // its own error is on its screen
	}

	// Back in front is not ready: a claude takes a moment to draw its prompt,
	// and what is typed before is lost.
	if want := agentOf(spec, resumed); want != "" && procfs() {
		for deadline := time.Now().Add(wakeWait); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			if i := ns.info(); i.Status == "exited" || i.Program == want && (i.Status == "idle" || i.Status == "blocked") {
				break
			}
		}
	}

	return ns.info(), err
}

// agentOf is the program that runs in front of a woken session once argv,
// what brings its agent back, has started: "" when there is none to wait for.
func agentOf(spec proto.SessionSpec, argv []string) string {
	if len(argv) == 0 {
		return ""
	}

	if c := spec.Conversation; c != nil {
		return c.Agent
	}

	i := slices.IndexFunc(argv, func(a string) bool { return !strings.Contains(a, "=") }) // past the KEY=VALUE before it
	if i < 0 {
		return ""
	}

	return programOf(argv[i:])
}

// errAsleep is what input to an asleep session gets.
var errAsleep = errors.New("session is asleep: wake it first (pando session wake)")
