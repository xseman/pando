package ui

import (
	"fmt"
	"image/color"
	"slices"
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

// TestEditorHScrollbar is VS Code's horizontal scrollbar: with word wrap off,
// the default, a line wider than the editor puts a bar under the text that a
// click or a drag pans, and turning wrap on (⌥z or the header's toggle)
// takes the bar away.
func TestEditorHScrollbar(t *testing.T) {
	short, _ := editorModel(t, "short.go", "package x\n")
	if short.pv.wrap || short.hbarH() != 0 {
		t.Fatalf("wrap starts off and a file that fits has no bar: wrap %v, bar %d", short.pv.wrap, short.hbarH())
	}

	m, _ := editorModel(t, "wide.go", "package x\n\nvar s = \""+strings.Repeat("x", 400)+"\"\n")
	checkWidths(t, m)

	if m.hbarH() != 1 || m.pvH() != short.pvH()-1 {
		t.Fatalf("a wide line takes a row for the bar: bar %d, text rows %d of %d", m.hbarH(), m.pvH(), short.pvH())
	}

	h, g := m.pvH(), m.pv.gutter()
	y, x0 := 1+m.stripH()+h, m.mainX()+g

	row := m.editorLines(m.mainW())[y]
	if !strings.Contains(row, underParams(pal.sliderBg)) || !strings.Contains(row, underParams(pal.rulerBorder)) || strings.TrimSpace(ansi.Strip(row)) != "" {
		t.Fatalf("the row under the text is the bar: %q", ansi.Strip(row))
	}

	hb := m.pv.hbar(m, m.pvW())
	m.Update(tea.MouseClickMsg{X: x0 + hb.h - 1, Y: y, Button: tea.MouseLeft})

	if want := hb.n - hb.h; m.pv.left != want {
		t.Fatalf("a click at the right end of the track pans to the end: left %d, want %d", m.pv.left, want)
	}

	if !m.barActive("editor-h") || m.barActive("editor") {
		t.Fatal("the horizontal slider is the one held")
	}

	if !strings.Contains(m.View().Content, underParams(pal.sliderActiveBg)) {
		t.Fatal("the held slider is drawn active")
	}

	m.Update(tea.MouseMotionMsg{X: x0 - 20, Y: y + 3, Button: tea.MouseLeft})

	if m.pv.left != 0 {
		t.Fatalf("dragging past the left end pans to the start: left %d", m.pv.left)
	}

	m.Update(tea.MouseReleaseMsg{X: x0, Y: y, Button: tea.MouseLeft})

	if m.drag != nil || m.pv.cur != (pos{}) {
		t.Fatalf("the release ends the drag and leaves the cursor: drag %v cursor %+v", m.drag, m.pv.cur)
	}

	press(m, "down", "down", "end")

	if want := hb.n - hb.h; m.pv.left != want {
		t.Fatalf("the cursor at the end of the widest line brings the bar to its end: left %d, want %d", m.pv.left, want)
	}

	m.pv.left = 10_000 // a shift+wheel runs on; the view stops it at the widest line
	m.View()

	if want := hb.n - hb.h; m.pv.left != want {
		t.Fatalf("panning stops at the widest line: left %d, want %d", m.pv.left, want)
	}

	press(m, "alt+z")

	if !m.pv.wrap || !m.st.Settings.Wrap || m.pv.left != 0 || m.hbarH() != 0 || m.pvH() != short.pvH() {
		t.Fatalf("⌥z sets word_wrap, and the bar goes: wrap %v setting %v left %d bar %d", m.pv.wrap, m.st.Settings.Wrap, m.pv.left, m.hbarH())
	}

	checkWidths(t, m)

	acts := m.pv.buttons(m, m.mainW())
	if a := acts[len(acts)-1]; a.g != icWrap || !a.on {
		t.Fatalf("the header's last button is the lit wrap toggle: %+v", a)
	}

	click(m, m.mainX()+acts[len(acts)-1].x+1, 0, tea.MouseLeft)

	if m.pv.wrap || m.st.Settings.Wrap || m.hbarH() != 1 {
		t.Fatalf("the header toggle unwraps: wrap %v setting %v bar %d", m.pv.wrap, m.st.Settings.Wrap, m.hbarH())
	}

	i := slices.IndexFunc(settingsItems(m), func(it item) bool { return it.label == "Word wrap" })
	if i < 0 || settingsItems(m)[i].hint != "off" {
		t.Fatal("Settings lists word wrap, off")
	}

	settingsItems(m)[i].run(m)

	if !m.pv.wrap || settingsItems(m)[i].hint != "on" {
		t.Fatalf("the Settings toggle wraps the open editor: wrap %v", m.pv.wrap)
	}

	m.Update(stateMsg(proto.State{})) // another TUI, or config.toml, turned it off

	if m.pv.wrap || m.hbarH() != 1 {
		t.Fatalf("a state event carries word_wrap to the editor: wrap %v", m.pv.wrap)
	}
}

// underParams is the SGR that colors an underline c, the horizontal bar's line.
func underParams(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("58;2;%d;%d;%d", r>>8, g>>8, b>>8)
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

// TestSessionAltScreen hides the bar over an app on the alternate screen,
// which scrolls itself, and gives it the wheel: as mouse events when it asked
// for them, as arrows (xterm's alternate scroll) when it did not.
func TestSessionAltScreen(t *testing.T) {
	m := testModel(t)
	m.switchSession("s1")
	m.focus = onMain

	h := m.sessH()
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: make([]string, h), AltScreen: true}

	if strings.Contains(m.View().Content, bgParams(pal.sliderBg)) {
		t.Fatal("the alternate screen has no scrollback to show a slider for")
	}

	x, y := m.mainX()+5, 1+m.stripH()+2
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})

	if in := sent(t, m); len(in.Keys) != 3 || in.Keys[0].Code != tea.KeyUp || m.term.scroll != 0 {
		t.Fatalf("the wheel on an app without the mouse: %+v, scroll %d", in, m.term.scroll)
	}

	m.term.scr.Mouse = true
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})

	if in := sent(t, m); in.Mouse == nil || in.Mouse.Kind != "wheel" {
		t.Fatalf("the wheel on an app with the mouse: %+v", in)
	}
}
