package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// answer feeds the widget of t the text session.read would return for the
// search its last key asked for.
func answer(m *Model, t *term, move int, lines []string) {
	m.Update(termTextMsg{t: t, id: t.id, seq: t.find.seq, lines: lines, move: move})
}

// TestTerminalFind is VS Code's terminal find over a session: ⌃f opens the
// widget and keeps the keys from the shell, the query searches the
// scrollback and the screen, ⏎ walks up toward older output and ⇧⏎ down,
// the terminal scrolls to a match out of view, and esc closes it.
func TestTerminalFind(t *testing.T) {
	m := testModel(t)
	m.switchSession("s1")
	m.focus = onMain

	h := m.sessH()
	screen := make([]string, h)
	screen[2] = "build error: missing ;"
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: screen, Scrollback: 100}

	text := make([]string, 0, 100+h)

	for i := range 100 {
		line := fmt.Sprintf("line %d", i)
		if i == 10 || i == 50 {
			line = "an Error at " + line
		}

		text = append(text, line)
	}

	text = append(text, screen...)

	press(m, "ctrl+f")

	if !m.term.find.on || !m.term.find.editing || m.keyContext() != ctxInput {
		t.Fatalf("⌃f opens the widget with the keyboard: on %v editing %v", m.term.find.on, m.term.find.editing)
	}

	press(m, "e", "r", "r")

	select {
	case in := <-m.inputs:
		t.Fatalf("the query's keys reached the shell: %+v", in)
	default:
	}

	answer(m, &m.term, 2, text)

	if f := &m.term.find; len(f.hits) != 3 || f.hit != 2 || m.term.scroll != 0 {
		t.Fatalf("a new query selects the newest match, on screen: %d hits, hit %d, scroll %d", len(f.hits), f.hit, m.term.scroll)
	}

	view := checkWidths(t, m)
	if !strings.Contains(view, "3 of 3") || !strings.Contains(m.View().Content, bgParams(pal.findHitBg)) {
		t.Fatal("the widget counts the matches and the one on screen is painted")
	}

	press(m, "enter")
	answer(m, &m.term, -1, text)

	if f := &m.term.find; f.hit != 1 || m.term.scroll != 100-(50-h/2) {
		t.Fatalf("⏎ goes up to the older match and scrolls it to the middle: hit %d, scroll %d", f.hit, m.term.scroll)
	}

	press(m, "enter")
	answer(m, &m.term, -1, text)
	press(m, "enter")
	answer(m, &m.term, -1, text)

	if m.term.find.hit != 2 {
		t.Fatalf("⏎ past the oldest wraps to the newest: hit %d", m.term.find.hit)
	}

	press(m, "shift+enter")
	answer(m, &m.term, 1, text)

	if m.term.find.hit != 0 {
		t.Fatalf("⇧⏎ past the newest wraps to the oldest: hit %d", m.term.find.hit)
	}

	more := slices.Concat(text, []string{"fresh error"})
	answer(m, &m.term, 0, more) // a screen update searched again

	if f := &m.term.find; len(f.hits) != 4 || f.hit != 0 {
		t.Fatalf("new output adds its matches and keeps the selection: %d hits, hit %d", len(f.hits), f.hit)
	}

	press(m, "alt+c")
	answer(m, &m.term, 2, more)

	if f := &m.term.find; !f.caseSens || len(f.hits) != 2 {
		t.Fatalf("match case drops the Error lines: %v, %d hits", f.caseSens, len(f.hits))
	}

	// A click on the terminal gives the keys back to the shell with the
	// matches still painted; esc then closes the widget, the next reaches the app.
	x, y0 := m.mainX()+2, 1+m.stripH()
	m.Update(tea.MouseClickMsg{X: x, Y: y0 + 5, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y0 + 5, Button: tea.MouseLeft})

	if !m.term.find.on || m.term.find.editing || m.keyContext() != ctxTerminal {
		t.Fatalf("a click on the terminal leaves the widget open: on %v editing %v", m.term.find.on, m.term.find.editing)
	}

	press(m, "esc")

	if m.term.find.on || m.term.find.hits != nil {
		t.Fatal("esc closes the widget and its highlights")
	}

	press(m, "esc")

	select {
	case in := <-m.inputs:
		if len(in.Keys) != 1 || in.Keys[0].Code != tea.KeyEscape {
			t.Fatalf("the next esc is the app's: %+v", in)
		}

	case <-time.After(time.Second):
		t.Fatal("the next esc is the app's")
	}
}

// TestTerminalFindWidget clicks the widget over the Terminal panel: its
// buttons toggle and close, and it stays inside the panel's width.
func TestTerminalFindWidget(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	m.tv.id = "t1"
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "t1", Workspace: m.ws, Agent: termAgent}, Status: "idle"})
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 10
	m.resize()
	m.focus = onPanel

	w, h := m.termBody()
	m.tv.term.id = "t1"
	m.tv.scr = proto.Screen{Lines: make([]string, h)}

	press(m, "ctrl+f")

	if !m.tv.find.on {
		t.Fatal("⌃f in the Terminal opens its widget")
	}

	checkWidths(t, m)

	x0, bw, ok := m.tv.findRect(w, h)
	if !ok {
		t.Fatalf("the widget fits a panel %dx%d", w, h)
	}

	_, acts := m.tv.findActs(m.tv.id, bw)
	top := m.mainH() + 1 // the panel's tab row, then its body

	m.Update(tea.MouseClickMsg{X: m.mainX() + x0 + acts[1].x, Y: top + 1, Button: tea.MouseLeft})

	if !m.tv.find.word {
		t.Fatal("the whole word button toggles")
	}

	m.Update(tea.MouseClickMsg{X: m.mainX() + x0 + acts[5].x, Y: top + 1, Button: tea.MouseLeft})

	if m.tv.find.on {
		t.Fatal("the close button closes the widget")
	}
}
