package ui

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/xseman/pando/internal/proto"
)

// fetchLab plays a daemon: a screen is read when it is asked for and handed
// to the model whenever the schedule says, so any reply order can be tried.
type fetchLab struct {
	t       *testing.T
	m       *Model
	mu      sync.Mutex
	gen     map[string]int
	calls   int
	pending []screenMsg
	trace   []string

	onScreen func(proto.ScreenParams)
}

func (l *fetchLab) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}

		go func() {
			defer func() { _ = c.Close() }()

			var req proto.Request
			if json.NewDecoder(c).Decode(&req) != nil {
				return
			}

			var res any

			if req.Method == "session.screen" {
				var p proto.ScreenParams

				_ = json.Unmarshal(req.Params, &p)

				l.mu.Lock()

				l.calls++
				if l.onScreen != nil {
					l.onScreen(p)
				}

				res = proto.Screen{Lines: []string{fmt.Sprintf("%s#%d", p.ID, l.gen[p.ID])}}
				l.mu.Unlock()
			}

			b, _ := json.Marshal(res)
			_ = json.NewEncoder(c).Encode(proto.Response{Result: b})
		}()
	}
}

func (l *fetchLab) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				l.run(c)
			}

		case screenMsg:
			l.pending = append(l.pending, msg)
		}

	case <-time.After(300 * time.Millisecond): // a timer or a wait: nothing to do with screens
	}
}

func (l *fetchLab) deliver(i int) {
	msg := l.pending[i]
	l.pending = append(l.pending[:i], l.pending[i+1:]...)
	l.trace = append(l.trace, "reply "+msg.id+" "+strings.Join(msg.scr.Lines, ""))
	l.run(l.m.onScreen(msg))
}

func (l *fetchLab) output(id string) {
	l.mu.Lock()
	l.gen[id]++
	l.mu.Unlock()
	l.trace = append(l.trace, "output "+id)
	l.run(l.m.onEvent(proto.Event{Kind: "screen", ID: id}))
}

// shown is what every terminal on screen holds, and whether one is stuck.
func (l *fetchLab) check() (bad []string) {
	m := l.m
	see := func(name, id string, t *term) {
		if t.fetching || t.again {
			bad = append(bad, fmt.Sprintf("%s %s stuck: fetching=%v again=%v", name, id, t.fetching, t.again))
		}

		want := fmt.Sprintf("%s#%d", id, l.gen[id])
		if len(t.scr.Lines) == 0 || t.scr.Lines[0] != want {
			bad = append(bad, fmt.Sprintf("%s shows %q, want %q", name, t.scr.Lines, want))
		}
	}

	if m.tv.id != "" && m.termShowing() {
		see("panel", m.tv.id, &m.tv.term)
	}

	if !m.tiled() {
		if m.showsSession() {
			see("main", m.sess, &m.term)
		}

		return bad
	}

	m.eachGroup(func() {
		if m.showsSession() {
			see("group", m.sess, &m.term)
		}
	})

	return bad
}

func fetchModel(t *testing.T, groups int) *fetchLab {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "fs") //nolint:usetesting // a unix socket path caps at ~104 bytes
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("PANDO_RUNTIME_DIR", dir)

	ln, err := net.Listen("unix", filepath.Join(dir, "pando.sock"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	m := testModelSized(t, 220, 56)
	drainInputs(m)

	sess := func(id string) proto.Session {
		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: "shell"}, Status: "idle"}
	}
	m.sessions = []proto.Session{sess("s1"), sess("s2"), sess("s3"), sess("s4"), termSession(m.ws, "t1"), termSession(m.ws, "t2")}
	m.st.Settings.TermOpen = true
	m.st.Settings.GitDeco = false
	m.tv.id = "t1"
	m.sess, m.focus = "s1", onMain
	m.sessTabs = map[string][]string{m.ws: {"s1"}}

	p := m.cur
	for i := 2; i <= groups; i++ {
		p = m.splitPane(p, sideRight)
		p.g.sess = fmt.Sprintf("s%d", i)
		p.g.tabs = []string{p.g.sess}
	}

	l := &fetchLab{t: t, m: m, gen: map[string]int{}}

	go l.serve(ln)

	return l
}

func TestFetchesSettle(t *testing.T) {
	for _, groups := range []int{1, 2, 3} {
		for seed := uint64(1); seed <= 150; seed++ {
			t.Run(fmt.Sprintf("g%d-s%d", groups, seed), func(t *testing.T) { settles(t, groups, seed, false) })
		}
	}
}

func TestFetchesSettleSwitching(t *testing.T) {
	for _, groups := range []int{1, 3} {
		for seed := uint64(1); seed <= 150; seed++ {
			t.Run(fmt.Sprintf("g%d-s%d", groups, seed), func(t *testing.T) { settles(t, groups, seed, true) })
		}
	}
}

func settles(t *testing.T, groups int, seed uint64, switches bool) {
	t.Helper()

	l := fetchModel(t, groups)
	m := l.m
	r := rand.New(rand.NewPCG(seed, 7))
	ids := []string{"s1", "s2", "s3", "s4", "t1", "t2"}

	l.run(m.fetchScreen())

	for range 40 {
		switch c := r.IntN(100); {
		case c < 55:
			if len(l.pending) > 0 {
				l.deliver(r.IntN(len(l.pending)))
			}

		case c < 80:
			l.output(ids[r.IntN(len(ids))])
		case c < 90:
			if switches {
				l.trace = append(l.trace, "cycleTerm")
				l.run(m.cycleTerm(1))
			}

		default:
			if switches {
				id := ids[r.IntN(4)]
				l.trace = append(l.trace, "switchSession "+id)
				l.run(m.switchSession(id))
			}
		}
	}

	replies := 0
	for len(l.pending) > 0 {
		replies++
		if replies > 200 {
			t.Fatalf("I1: no end of replies with nothing printed\n%s", strings.Join(l.trace[max(len(l.trace)-20, 0):], "\n"))
		}

		l.deliver(r.IntN(len(l.pending)))
	}

	if bad := l.check(); len(bad) > 0 {
		t.Fatalf("%s\n--- trace\n%s", strings.Join(bad, "\n"), strings.Join(l.trace, "\n"))
	}
}

// I4: output in one pane asks for that pane's screen alone.
func TestOutputFetchesItsTerminal(t *testing.T) {
	for _, groups := range []int{1, 3} {
		l := fetchModel(t, groups)
		l.run(l.m.fetchScreen())

		for len(l.pending) > 0 {
			l.deliver(0)
		}

		for _, id := range []string{"s1", "t1"} {
			before := l.calls
			l.output(id)

			if n := l.calls - before; n != 1 {
				t.Fatalf("groups %d: output in %s asked for %d screens", groups, id, n)
			}

			for len(l.pending) > 0 {
				l.deliver(0)
			}
		}
	}
}

// One session in two groups, as a saved layout can hold it.
func TestSameSessionTwoGroups(t *testing.T) {
	l := fetchModel(t, 2)

	m := l.m
	for _, p := range m.grid.leaves() {
		p.g.sess = "s1"
	}

	m.sess = "s1"
	l.run(m.fetchScreen())
	t.Logf("flights: %d tiled=%v", len(l.pending), m.tiled())

	for len(l.pending) > 0 {
		l.deliver(0)
	}
}

// A line of a TUI wider than this one stays inside its group.
func TestForeignWidthFits(t *testing.T) {
	l := fetchModel(t, 2)
	m := l.m
	l.run(m.fetchScreen())

	wide := strings.Repeat("x", 400)

	for len(l.pending) > 0 {
		msg := l.pending[0]
		l.pending = l.pending[1:]
		msg.scr.Lines = []string{wide, wide}
		msg.scr.Cols = 400
		l.run(m.onScreen(msg))
	}

	m.still = false
	checkWidths(t, m)
}

// pumpScreens plays a daemon that answers in order: it hands every screen in
// flight back to the model and counts them, up to limit. Nothing dials: the
// fetching flags say what the model would have sent.
func pumpScreens(m *Model, flying func() map[string]bool, seq func(id string) int, limit int) int {
	var queue []string

	inQueue := map[string]bool{}
	enqueue := func() {
		f := flying()
		for _, id := range slices.Sorted(maps.Keys(f)) {
			if f[id] && !inQueue[id] {
				inQueue[id] = true
				queue = append(queue, id)
			}
		}
	}

	enqueue()

	n := 0
	for ; n < limit && len(queue) > 0; n++ {
		id := queue[0]
		queue = queue[1:]
		inQueue[id] = false

		m.onScreen(screenMsg{id: id, scr: proto.Screen{}, seq: seq(id)})
		enqueue()
	}

	return n
}

// A second fetch while both screens fly must bring each back once more at
// most: nothing printed, so nothing is left to ask for.
func TestFetchScreenSettlesSessionAndPanel(t *testing.T) {
	m := testModel(t)
	m.sess, m.tv.id = "s1", "t1"
	m.st.Settings.TermOpen = true

	if !m.termShowing() || !m.showsSession() {
		t.Fatalf("two terminals on screen: panel %v, session %v", m.termShowing(), m.showsSession())
	}

	m.fetchScreen()
	m.fetchScreen() // an event, a key, a tick: anything while they fly

	flying := func() map[string]bool { return map[string]bool{"s1": m.term.fetching, "t1": m.tv.fetching} }
	seq := func(id string) int {
		if id == "t1" {
			return m.tv.seq
		}

		return m.term.seq
	}

	if n := pumpScreens(m, flying, seq, 1000); n > 4 {
		t.Fatalf("%d screens asked for with nothing printed: the terminals keep each other fetching", n)
	}
}

// Three groups and the panel, the layout the loop was seen on.
func TestFetchScreenSettlesGroups(t *testing.T) {
	m := testModel(t)
	m.st.Settings.TermOpen = true
	m.w = 220
	m.Update(tea.WindowSizeMsg{Width: 220, Height: 56})

	a := m.cur
	b := m.splitPane(a, sideRight)
	c := m.splitPane(b, sideRight)

	for p, id := range map[*pane]string{a: "s1", b: "s2", c: "s3"} {
		p.g.sess = id
	}

	m.sess = "s1"
	m.tv.id = "t1"

	if !m.tiled() {
		t.Fatal("not tiled")
	}

	m.fetchScreen()
	m.fetchScreen()

	flying := func() map[string]bool {
		out := map[string]bool{"t1": m.tv.fetching}
		for _, p := range m.grid.leaves() {
			f := p.g.term.fetching
			if p == m.cur {
				f = m.term.fetching
			}

			out[p.g.sess] = f
		}

		return out
	}

	seq := func(id string) int {
		if id == "t1" {
			return m.tv.seq
		}

		for _, p := range m.grid.leaves() {
			if p != m.cur && p.g.sess == id {
				return p.g.term.seq
			}
		}

		return m.term.seq
	}

	if n := pumpScreens(m, flying, seq, 1000); n > 8 {
		t.Fatalf("%d screens asked for with nothing printed: the terminals keep each other fetching", n)
	}
}
