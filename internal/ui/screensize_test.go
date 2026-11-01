package ui

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/xseman/pando/internal/proto"
)

// sizes is a daemon's idea of each session's size, 120x40 to start.
type sizes struct {
	at   map[string][2]int
	sent int
}

func (s *sizes) serve(p proto.ScreenParams) {
	if p.Cols > 0 && p.Rows > 0 {
		s.at[p.ID] = [2]int{p.Cols, p.Rows}
		s.sent++
	}
}

func (s *sizes) of(id string) [2]int {
	if z, ok := s.at[id]; ok {
		return z
	}

	return [2]int{120, 40}
}

func drain(l *fetchLab, r *rand.Rand, errs bool) {
	for len(l.pending) > 0 {
		i := r.Intn(len(l.pending))
		if errs && r.Intn(4) == 0 {
			l.pending[i].err = errors.New("dial: connection refused")
		}

		l.deliver(i)
	}
}

// wrong lists the terminals on screen at a size their session does not have.
func wrong(l *fetchLab, s *sizes) (bad []string) {
	m := l.m
	if m.tv.id != "" && m.termShowing() {
		_, h := m.termBody()
		if want := [2]int{m.termCols(), h}; h > 0 && s.of(m.tv.id) != want {
			bad = append(bad, fmt.Sprintf("panel %s at %v, room %v", m.tv.id, s.of(m.tv.id), want))
		}
	}

	check := func() {
		if m.showsSession() {
			if want := [2]int{max(m.mainW()-1, 1), m.sessH()}; s.of(m.sess) != want {
				bad = append(bad, fmt.Sprintf("group %s at %v, room %v", m.sess, s.of(m.sess), want))
			}
		}
	}
	if m.tiled() {
		m.eachGroup(check)
	} else {
		check()
	}

	return bad
}

func walk(t *testing.T, seed int64, errs bool) string {
	t.Helper()

	r := rand.New(rand.NewSource(seed))
	l := fetchModel(t, 1+r.Intn(3))
	m := l.m
	s := &sizes{at: map[string][2]int{}}
	l.onScreen = s.serve
	l.run(m.fetchScreen())

	ops := []func() (string, tea.Cmd){
		func() (string, tea.Cmd) {
			id := fmt.Sprintf("s%d", 1+r.Intn(4))
			return "switch " + id, m.switchSession(id)
		},
		func() (string, tea.Cmd) { return "max", m.toggleMaxGroup() },
		func() (string, tea.Cmd) { return "split", m.splitEditor(sideRight) },
		func() (string, tea.Cmd) { return "move", m.moveEditor(sideRight) },
		func() (string, tea.Cmd) { return "moveL", m.moveEditor(sideLeft) },
		func() (string, tea.Cmd) { return "hide", m.hideSession() },
		func() (string, tea.Cmd) { return "term", m.toggleTerminal() },
		func() (string, tea.Cmd) { return "cycle", m.cycleTerm(1) },
		func() (string, tea.Cmd) { return "sidebars", m.toggleSidebars() },
		func() (string, tea.Cmd) { return "wide", m.toggleTermWide() },
		func() (string, tea.Cmd) {
			if m.grid.g != nil {
				return "nogroup", nil
			}

			return "close", m.closeGroup()
		},
		func() (string, tea.Cmd) {
			w, h := 120+r.Intn(120), 30+r.Intn(30)
			_, c := m.Update(tea.WindowSizeMsg{Width: w, Height: h})

			return fmt.Sprintf("win %dx%d", w, h), c
		},
		func() (string, tea.Cmd) {
			id := fmt.Sprintf("s%d", 1+r.Intn(4))

			l.mu.Lock()
			l.gen[id]++
			l.mu.Unlock()

			return "out " + id, m.onEvent(proto.Event{Kind: "screen", ID: id})
		},
	}

	var log string

	var logSb118 strings.Builder

	var logSb124 strings.Builder

	for range 80 {
		name, cmd := ops[r.Intn(len(ops))]()
		logSb118.WriteString(name + "; ")

		l.run(cmd)

		var logSb122 strings.Builder

		for n := r.Intn(3); n > 0 && len(l.pending) > 0; n-- {
			i := r.Intn(len(l.pending))
			if errs && r.Intn(4) == 0 {
				l.pending[i].err = errors.New("dial: connection refused")

				logSb122.WriteString("ERR; ")
			}

			l.deliver(i)
		}

		logSb124.WriteString(logSb122.String())
	}

	log += logSb124.String()

	log += logSb118.String()

	drain(l, r, false)
	// Every session on screen prints: all a base fetch would need.
	for _, id := range []string{"s1", "s2", "s3", "s4", "t1", "t2"} {
		l.output(id)
	}

	drain(l, r, false)

	bad := append(wrong(l, s), l.check()...)
	if len(bad) > 0 {
		return fmt.Sprint(bad, " after ", log)
	}

	return ""
}

func TestSizesWalk(t *testing.T) {
	for seed := range int64(500) {
		if why := walk(t, seed, false); why != "" {
			t.Errorf("seed %d: %s", seed, why)
		}
	}
}

func TestSizesWalkErrors(t *testing.T) {
	for seed := range int64(500) {
		if why := walk(t, seed, true); why != "" {
			t.Errorf("seed %d: %.300s", seed, why)
			return
		}
	}
}

// A fetch that carried the size fails: the size never reached the session.
func TestFailedFetchSendsSizeAgain(t *testing.T) {
	l := fetchModel(t, 1)
	m := l.m
	s := &sizes{at: map[string][2]int{}}
	l.onScreen = func(proto.ScreenParams) {} // the call fails: nothing lands
	l.run(m.fetchScreen())

	for len(l.pending) > 0 {
		l.pending[0].err = errors.New("dial: connection refused")
		l.deliver(0)
	}

	l.onScreen = s.serve
	l.output("s1")
	l.output("t1")

	for len(l.pending) > 0 {
		l.deliver(0)
	}

	if bad := wrong(l, s); len(bad) > 0 {
		t.Fatal(bad)
	}
}

// session.wake swaps the session in at the size it read first: a size sent
// meanwhile went to the old one. wokeMsg's fetch is what base healed it with.
func TestWakeSendsSizeAgain(t *testing.T) {
	l := fetchModel(t, 1)
	m := l.m
	m.session("s2").Status = "asleep"
	s := &sizes{at: map[string][2]int{}}
	l.onScreen = s.serve
	l.run(m.fetchScreen())

	for len(l.pending) > 0 {
		l.deliver(0)
	}

	cmd := m.switchSession("s2") // Batch(fetchScreen, wake): one flight with the size
	l.run(cmd)

	for len(l.pending) > 0 {
		l.deliver(0)
	}

	delete(s.at, "s2") // the woken session: the emulator it had before the size came

	m.session("s2").Status = "idle"
	_, c := m.Update(wokeMsg{id: "s2"})
	l.run(c)
	l.output("s2")

	for len(l.pending) > 0 {
		l.deliver(0)
	}

	if bad := wrong(l, s); len(bad) > 0 {
		t.Fatal(bad)
	}
}

func drainAll(ls ...*fetchLab) {
	for _, l := range ls {
		for n := 0; len(l.pending) > 0 && n < 50; n++ {
			l.deliver(0)
		}
	}
}

// tuiPair: TUI a is settled; then another TUI of size 10x5 took s1.
func tuiPair(t *testing.T) (a, b *fetchLab, size *[2]int) {
	t.Helper()

	a = fetchModel(t, 1)
	size = &[2]int{}
	a.onScreen = func(p proto.ScreenParams) {
		if p.Cols > 0 && p.ID == "s1" {
			*size = [2]int{p.Cols, p.Rows}
		}
	}
	a.run(a.m.fetchScreen())
	drainAll(a)

	*size = [2]int{10, 5}

	return a, nil, size
}

// B quits. A alone keeps watching the agent print.
func TestDepartedTUILeavesNoSize(t *testing.T) {
	a, _, size := tuiPair(t)
	small := *size

	for range 20 {
		a.output("s1")
		drainAll(a)
	}

	a.m.st.Settings.Sleep = 60
	a.m.ticks = 14
	_, c := a.m.Update(tickMsg{})
	a.run(c)
	drainAll(a)

	if *size == small {
		t.Fatalf("A alone after 20 outputs: session still at departed B's %v", *size)
	}
}

// A pastes: as a key, the TUI acting should take the session.
func TestPasteTakesSize(t *testing.T) {
	a, _, size := tuiPair(t)
	small := *size

	a.m.focus = onMain
	go func() { <-a.m.inputs }()

	_, c := a.m.Update(tea.PasteMsg{Content: "x"})
	a.run(c)
	a.output("s1")
	drainAll(a)

	if *size == small {
		t.Fatalf("after paste still %v", *size)
	}
}

// Control: a key does.
func TestKeyTakesSize(t *testing.T) {
	a, _, size := tuiPair(t)
	small := *size

	a.m.focus = onMain
	go func() { <-a.m.inputs }()

	a.m.term.key(a.m, "s1", tea.KeyPressMsg{Code: 'x', Text: "x"})
	a.output("s1")
	drainAll(a)

	if *size == small {
		t.Fatalf("after key still %v", *size)
	}
}
