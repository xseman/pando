package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// TestVbarGeometry checks the slider for every small bar: it stays inside
// the track, sits at the ends at the ends of the content, and topAt takes the
// slider's own row back to the top that drew it.
func TestVbarGeometry(t *testing.T) {
	for h := 1; h <= 12; h++ {
		for n := h + 1; n <= 60; n++ {
			for top := 0; top <= n-h; top++ {
				b := vbar{n, h, top}
				y, sh := b.thumb()

				switch {
				case !b.on() || sh < 1 || y < 0 || y+sh > h:
					t.Fatalf("%+v: slider %d+%d outside the track", b, y, sh)
				case top == 0 && y != 0, top == n-h && y+sh != h:
					t.Fatalf("%+v: slider %d+%d does not reach its end", b, y, sh)
				}

				back := vbar{n, h, b.topAt(y)}
				if y2, _ := back.thumb(); y2 != y {
					t.Fatalf("%+v: topAt(%d) = %d draws the slider at %d", b, y, back.top, y2)
				}
			}
		}
	}

	if (vbar{5, 5, 0}).on() || (vbar{5, 0, 0}).on() {
		t.Fatal("a bar with nothing out of view has no slider")
	}

	if got := (vbar{100, 10, 0}).topAt(-5); got != 0 {
		t.Fatalf("above the track = %d", got)
	}

	if got := (vbar{100, 10, 0}).topAt(50); got != 90 {
		t.Fatalf("below the track = %d", got)
	}
}

// TestEditorScrollbar is VS Code's editor scrollbar: the last column of the
// editor, a shaded slider only when the file does not fit, and a click on the
// track or a drag of the slider scrolls.
func TestEditorScrollbar(t *testing.T) {
	short, _ := editorModel(t, "short.go", "package x\n")
	if strings.Contains(short.View().Content, bgParams(pal.sliderBg)) {
		t.Fatal("a file that fits has no slider")
	}

	var text strings.Builder
	for i := range 300 {
		fmt.Fprintf(&text, "line %d\n", i)
	}

	m, _ := editorModel(t, "long.go", text.String())
	checkWidths(t, m)

	if !strings.Contains(m.View().Content, bgParams(pal.sliderBg)) {
		t.Fatal("a long file shows the slider")
	}

	x, y0, h := m.mainX()+m.mainW()-1, 1+m.stripH(), m.pvH()

	// The text stops a column short: the bar owns the last one, the slider at
	// the top of it and the track's border below.
	lines := m.editorLines(m.mainW())
	if first, last := lines[m.stripH()+1], ansi.Strip(lines[m.stripH()+h]); !strings.Contains(first, bgParams(pal.sliderBg)) || !strings.HasSuffix(last, "▏") {
		t.Fatalf("body rows end in the bar: first %q, last %q", first, last)
	}

	m.Update(tea.MouseClickMsg{X: x, Y: y0 + h - 1, Button: tea.MouseLeft})

	if want := m.pv.bar(m).n - h; m.pv.top != want {
		t.Fatalf("a click at the bottom of the track scrolls to the end: top %d, want %d", m.pv.top, want)
	}

	if !m.barActive("editor") || !strings.Contains(m.View().Content, bgParams(pal.sliderActiveBg)) {
		t.Fatal("the held slider is drawn active")
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: y0 - 5, Button: tea.MouseLeft})

	if m.pv.top != 0 {
		t.Fatalf("dragging past the top scrolls to the start: top %d", m.pv.top)
	}

	m.Update(tea.MouseReleaseMsg{X: x, Y: y0, Button: tea.MouseLeft})

	if m.drag != nil || m.pv.cur.line != 0 {
		t.Fatalf("the release ends the drag and leaves the cursor: drag %v cursor %+v", m.drag, m.pv.cur)
	}
}

// TestSessionScrollbar gives a session's scrollback the same bar: its last
// column, and a click that scrolls back through the history.
func TestSessionScrollbar(t *testing.T) {
	m := testModel(t)
	drainInputs(m)
	m.Update(focusSessionMsg("s1"))

	h := m.sessH()
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: make([]string, h), Scrollback: 200}

	if !strings.Contains(m.View().Content, bgParams(pal.sliderBg)) {
		t.Fatal("a session with scrollback shows the slider")
	}

	x, y0 := m.mainX()+m.mainW()-1, 1+m.stripH()
	m.Update(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})

	if m.term.scroll != 200 {
		t.Fatalf("a click at the top of the track goes to the oldest line: scroll %d", m.term.scroll)
	}

	m.Update(tea.MouseReleaseMsg{X: x, Y: y0, Button: tea.MouseLeft})

	if m.drag != nil {
		t.Fatal("the release ends the drag")
	}
}
