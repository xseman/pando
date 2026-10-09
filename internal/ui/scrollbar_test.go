package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
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
	// The pointer on the slider shades it, as VS Code's; on the track it does not.
	m.Update(tea.MouseMotionMsg{X: x, Y: y0})

	if !strings.Contains(m.View().Content, bgParams(pal.sliderHoverBg)) {
		t.Fatal("the slider under the pointer is drawn hovered")
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: y0 + h - 1})

	if strings.Contains(m.View().Content, bgParams(pal.sliderHoverBg)) {
		t.Fatal("the pointer on the track leaves the slider at rest")
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

	click(m, m.mainX()+acts[len(acts)-1].x+1, 0, tea.MouseLeft) // in the tab strip

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

	x, y0 := m.mainX()+m.mainW()-1, m.stripH()
	m.Update(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})

	if m.term.scroll != 200 {
		t.Fatalf("a click at the top of the track goes to the oldest line: scroll %d", m.term.scroll)
	}

	m.Update(tea.MouseReleaseMsg{X: x, Y: y0, Button: tea.MouseLeft})

	if m.drag != nil {
		t.Fatal("the release ends the drag")
	}
}

// TestSessionPageKeys pages a shell's scrollback with pgup and pgdn, shift
// or not, as the wheel scrolls it; on the alternate screen the keys are the
// app's, as a fullscreen claude pages its own history.
func TestSessionPageKeys(t *testing.T) {
	m := testModel(t)
	m.switchSession("s1")
	m.focus = onMain

	h := m.sessH()
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: make([]string, h), Scrollback: 2*h + 1}

	press(m, "pgup")

	if m.term.scroll != h {
		t.Fatalf("pgup goes back a page: scroll %d, want %d", m.term.scroll, h)
	}

	press(m, "shift+pgup", "pgup")

	if m.term.scroll != 2*h+1 {
		t.Fatalf("paging back stops at the oldest line: scroll %d", m.term.scroll)
	}

	press(m, "pgdown", "shift+pgdown", "pgdown")

	if m.term.scroll != 0 {
		t.Fatalf("paging on stops at the live screen: scroll %d", m.term.scroll)
	}

	select {
	case in := <-m.inputs:
		t.Fatalf("a page key reached the shell: %+v", in)
	default:
	}

	m.term.scr = proto.Screen{Lines: make([]string, h), AltScreen: true}
	press(m, "pgup")

	if in := sent(t, m); len(in.Keys) != 1 || in.Keys[0].Code != tea.KeyPgUp || m.term.scroll != 0 {
		t.Fatalf("pgup on the alternate screen goes to the app: %+v, scroll %d", in, m.term.scroll)
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

// A sidebar list's scrollbar is the editor's to the mouse: the slider drags,
// a click on the track jumps it there, and the pointer over it lights no row.
func TestSourceControlScrollbar(t *testing.T) {
	m := testModelSized(t, 100, 24)
	root := m.ws

	var changes []git.Entry
	for i := range 60 {
		changes = append(changes, git.Entry{Path: fmt.Sprintf("f%02d.go", i), Letter: 'M'})
	}

	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "main", Changes: changes}}})
	press(m, "2")
	checkWidths(t, m)

	i := m.colOf(viewGit)
	x := m.colRect(i).x + m.colRect(i).w - 1 // the bar's column
	top := m.bodyTop(viewGit)

	ch, _ := m.scm.geometry(m, m.scm.paneH(m))
	b, ok := m.listBar(viewGit, ch-1)

	if !ok {
		t.Fatalf("no bar beside the last row of %d changes in %d rows", m.scm.changesEnd(), ch)
	}

	y0 := top + ch - 1 - b.row // the bar's first row on screen
	// The pointer over it: no row takes the hover.
	m.Update(tea.MouseMotionMsg{X: x, Y: y0 + 2})
	checkWidths(t, m)

	if m.hoverRow(viewGit) != -1 || m.scm.hovRow != -1 {
		t.Fatalf("hover over the scrollbar lit row %d (%d)", m.hoverRow(viewGit), m.scm.hovRow)
	}
	// The slider at the top of the bar shades under it.
	m.Update(tea.MouseMotionMsg{X: x, Y: y0})
	checkWidths(t, m)

	if !strings.Contains(m.View().Content, fgParams(pal.sliderHoverBg)) {
		t.Fatal("the slider under the pointer is drawn hovered")
	}

	m.Update(tea.MouseMotionMsg{X: x - 3, Y: y0 + 2})
	checkWidths(t, m)

	if strings.Contains(m.View().Content, fgParams(pal.sliderHoverBg)) {
		t.Fatal("off the bar the slider rests")
	}

	if m.scm.hovRow < 0 {
		t.Fatal("beside the bar the row lights again")
	}
	// A click low on the track jumps the slider there and holds it.
	bar := b.geo()
	m.Update(tea.MouseClickMsg{X: x, Y: y0 + bar.h - 1, Button: tea.MouseLeft})

	if m.scm.tops[""] == 0 || !m.barActive(b.id) {
		t.Fatalf("track click: top %d, held %v", m.scm.tops[""], m.barActive(b.id))
	}

	if out := checkWidths(t, m); !strings.Contains(out, plain.Render("┃")) {
		t.Fatal("the held slider does not light")
	}
	// Dragged to the top and let go.
	m.Update(tea.MouseMotionMsg{X: x, Y: y0 - 5, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y0 - 5, Button: tea.MouseLeft})

	if m.scm.tops[""] != 0 || m.drag != nil {
		t.Fatalf("dragged up: top %d, drag %+v", m.scm.tops[""], m.drag)
	}
	// The wheel over the bar still scrolls the list.
	m.Update(tea.MouseWheelMsg{X: x, Y: y0 + 2, Button: tea.MouseWheelDown})

	if m.scm.tops[""] == 0 {
		t.Fatal("the wheel over the bar does not scroll")
	}
}

// Explorer's bar drags the same way, and a click on a row beside it still
// selects the row.
func TestExplorerScrollbar(t *testing.T) {
	m := testModelSized(t, 100, 20)
	for i := range 40 {
		mustWrite(t, filepath.Join(m.ws, fmt.Sprintf("f%02d.txt", i)), "x\n")
	}

	m.ex.setRoot(m, m.ws)
	press(m, "1")

	i := m.colOf(viewFiles)
	x := m.colRect(i).x + m.colRect(i).w - 1
	y0 := m.bodyTop(viewFiles)

	b, ok := m.listBar(viewFiles, 0)
	if !ok {
		t.Fatalf("no bar over %d files in %d rows", len(m.ex.nodes), m.bodyH(viewFiles))
	}

	m.Update(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y0 + b.geo().h, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y0 + b.geo().h, Button: tea.MouseLeft})

	if bar := b.geo(); m.ex.l.top != bar.n-bar.h {
		t.Fatalf("dragged to the bottom: top %d of %d", m.ex.l.top, bar.n-bar.h)
	}

	sel := m.ex.l.sel
	m.Update(tea.MouseClickMsg{X: x - 5, Y: y0 + 1, Button: tea.MouseLeft})

	if m.ex.l.sel == sel || m.ex.l.sel != m.ex.l.top+1 {
		t.Fatalf("a click beside the bar selects row %d, got %d", m.ex.l.top+1, m.ex.l.sel)
	}

	checkWidths(t, m)
}
