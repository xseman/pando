package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// markAt is the screen column of the drop mark in line, -1 for none.
func markAt(line string) int {
	for i, r := range []rune(ansi.Strip(line)) {
		if r == '│' {
			return i
		}
	}

	return -1
}

// TestMoveTab moves the focused strip's tab with ⌃⇧pgup ⌃⇧pgdn, wrapping
// at either end as herdr's move_tab_previous and move_tab_next do: the
// Terminal's shell with the keyboard in the panel, else the active editor.
func TestMoveTab(t *testing.T) {
	m := testModelSized(t, 120, 24)
	drainInputs(m)

	m.focus = onMain
	for _, f := range []string{"README.md", ".env"} {
		fire(m, m.openFile(filepath.Join(m.ws, f), false))
	}

	names := func() string {
		var out []string
		for _, e := range m.editors {
			out = append(out, filepath.Base(e.path))
		}

		return strings.Join(out, " ")
	}

	press(m, "ctrl+shift+pgdown")

	if got := names(); got != ".env README.md" || m.edIdx != 0 || !strings.HasSuffix(m.pv.path, ".env") {
		t.Fatalf("⌃⇧pgdn wraps the last editor to the front, still active: %s, edIdx %d", got, m.edIdx)
	}

	if e := m.st.Editors[m.ws]; e.Active != 0 || len(e.Open) != 2 {
		t.Fatalf("the new order is saved: %+v", e)
	}

	press(m, "ctrl+shift+pgup")

	if got := names(); got != "README.md .env" || m.edIdx != 1 {
		t.Fatalf("⌃⇧pgup wraps the first editor to the end: %s, edIdx %d", got, m.edIdx)
	}

	if !slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == "view.moveTabLeft" }) {
		t.Fatal("Move Tab Left is a command [keys] can bind")
	}

	shell := func(id string) proto.Session {
		s := termSession(m.ws, id)
		s.Parent = "s1"

		return s
	}

	m.Update(sessionsMsg{m.sessions[0], shell("p1"), shell("p2")})
	m.Update(focusSessionMsg("s1"))

	ids := func(ss []proto.Session) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()
	m.focus = onPanel
	press(m, "ctrl+shift+pgdown")

	if got := ids(m.termSessions()); got != "p2 p1" || m.tv.id != "p1" {
		t.Fatalf("⌃⇧pgdn in the Terminal moves its shell: %s, showing %s", got, m.tv.id)
	}
}

// TestDragEditorTab drags an editor along the strip as herdr drags a tab:
// the strip stays put and marks where it would land, the release moves it
// there, and a release off the editor area leaves it.
func TestDragEditorTab(t *testing.T) {
	m := testModelSized(t, 120, 24)
	drainInputs(m)

	m.focus = onMain
	mustWrite(t, filepath.Join(m.ws, "a-much-longer-name.txt"), "x\n")

	for _, f := range []string{"README.md", ".env", "a-much-longer-name.txt"} {
		fire(m, m.openFile(filepath.Join(m.ws, f), false))
	}

	names := func() string {
		var out []string
		for _, e := range m.editors {
			out = append(out, filepath.Base(e.path))
		}

		return strings.Join(out, " ")
	}

	tabs := m.editorTabs(m.mainW())
	x0 := m.mainX()
	m.Update(tea.MouseClickMsg{X: x0 + tabs[1].x + 1, Y: 0, Button: tea.MouseLeft}) // .env

	if m.drag == nil || m.drag.kind != dragStrip || m.edIdx != 1 || markAt(m.editorStrip(m.mainW())) >= 0 {
		t.Fatalf("a press on a tab shows it and picks it up, unmarked: drag %+v, edIdx %d", m.drag, m.edIdx)
	}

	// The right half of the last tab: the slot after it.
	end := x0 + tabs[2].x + tabs[2].w - 2
	m.Update(tea.MouseMotionMsg{X: end, Y: 0, Button: tea.MouseLeft})

	if got := names(); got != "README.md .env a-much-longer-name.txt" || m.drag.slot != 3 {
		t.Fatalf("the strip stays put while the tab is held: %s, slot %d", got, m.drag.slot)
	}

	checkWidths(t, m)

	if got := markAt(m.editorStrip(m.mainW())); got != tabs[2].x+tabs[2].w {
		t.Fatalf("the mark sits in the hairline after the last tab: %d, want %d", got, tabs[2].x+tabs[2].w)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: end, Y: 0, Button: tea.MouseLeft})
	if got := names(); got != "README.md a-much-longer-name.txt .env" || m.edIdx != 2 || cmd == nil || m.drag != nil {
		t.Fatalf("the release moves it to the end and saves: %s, edIdx %d, cmd %v", got, m.edIdx, cmd != nil)
	}

	if m.st.Editors[m.ws].Active != 2 || markAt(m.editorStrip(m.mainW())) >= 0 {
		t.Fatalf("the saved strip keeps it active, the mark is gone: %+v", m.st.Editors[m.ws])
	}

	// The left half of the first tab: before it. Let go off the editor area, nothing moves.
	tabs = m.editorTabs(m.mainW())
	m.Update(tea.MouseClickMsg{X: x0 + tabs[2].x + 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x0 + tabs[0].x + 1, Y: 0, Button: tea.MouseLeft})

	if m.drag.slot != 0 || markAt(m.editorStrip(m.mainW())) != 0 {
		t.Fatalf("the slot before the first tab: %d", m.drag.slot)
	}

	m.Update(tea.MouseReleaseMsg{X: x0 - 3, Y: 5, Button: tea.MouseLeft})

	if got := names(); got != "README.md a-much-longer-name.txt .env" || m.drag != nil || m.grid.g == nil {
		t.Fatalf("a release off the editor area drops nothing: %s", got)
	}

	click(m, x0+m.editorTabs(m.mainW())[0].x+1, 0, tea.MouseLeft)

	if m.drag != nil || m.edIdx != 0 || names() != "README.md a-much-longer-name.txt .env" {
		t.Fatalf("a click that never moved only activates: drag %+v, %s", m.drag, names())
	}
}

// TestPlusButtonHovers paints the + of the Terminal strip on
// hover_bg while the pointer is over it.
func TestPlusButtonHovers(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	m.sessions = append(m.sessions, termSession(m.ws, "p1"))
	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()

	for _, c := range []struct {
		strip int
		tabs  []sessTab
	}{
		{stripTerm, m.termTabs(m.termStripW())},
	} {
		plus := c.tabs[len(c.tabs)-1]
		in, _ := m.tabUnder(c.strip, plus.x+1)
		past, _ := m.tabUnder(c.strip, plus.x+plus.w)

		if !plus.plus || in != plusTab || past != "" {
			t.Fatalf("strip %d: tabUnder over the +: %+v", c.strip, plus)
		}

		for _, over := range []string{"", plusTab} {
			segs := tabSegs(c.tabs, over, "")

			if got := segs[len(segs)-1].ownBg; got != (over == plusTab) {
				t.Fatalf("strip %d, over %q: the + paints its own background: %v", c.strip, over, got)
			}
		}
	}
}

// TestCloseHovers raises the ✕ of the active tab while the pointer is on the
// cells where a click closes it, and nowhere else.
func TestCloseHovers(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	m.sessions = append(m.sessions, termSession(m.ws, "p1"))
	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()

	var tab sessTab

	for _, c := range m.termTabs(m.termStripW()) {
		if c.active {
			tab = c
		}
	}

	if tab.id != "p1" {
		t.Fatalf("no active tab: %+v", tab)
	}

	for x, want := range map[int]bool{tab.x + tab.w - 4: false, tab.x + tab.w - 3: true, tab.x + tab.w - 1: true, tab.x + tab.w: false} {
		if _, on := m.tabUnder(stripTerm, x); on != want {
			t.Fatalf("column %d of the tab at %d, %d wide: on ✕ %v", x, tab.x, tab.w, on)
		}
	}

	m.setOver(stripTerm, tab.x+tab.w-1)

	if m.overX != "p1" {
		t.Fatalf("overX %q", m.overX)
	}

	hot := tabSegs([]sessTab{tab}, m.overTab, m.overX)
	if got := hot[1]; got.s != " "+icClose.s()+" " || got.st.GetBackground() != keycapHot().GetBackground() {
		t.Fatalf("the ✕ is not raised: %+v", hot)
	}

	if cold := tabSegs([]sessTab{tab}, m.overTab, ""); len(cold) != len(hot)-1 {
		t.Fatalf("a tab without the ✕ under the mouse is one chip: %d vs %d segments", len(cold), len(hot))
	}
}

// TestCloseOnHover: a tab other than the active one shows its ✕ only while
// the pointer is on it, and a click there closes that tab.
func TestCloseOnHover(t *testing.T) {
	m := groupsModel(t) // .env in front
	tab := m.editorTabs(m.mainW())[0]

	if !strings.HasSuffix(tab.label, tabClose(false)) {
		t.Fatalf("README.md shows no ✕ while the pointer is elsewhere: %q", tab.label)
	}

	x, y := m.mainX()+tab.x+tab.w-2, m.mainY()
	m.Update(tea.MouseMotionMsg{X: x, Y: y})

	if strip := ansi.Strip(m.editorStrip(m.mainW())); strings.Count(strip, icClose.s()) != 2 || m.overX == "" {
		t.Fatalf("hovered, README.md shows its ✕ too: %q", strip)
	}

	click(m, x, y, tea.MouseLeft)

	if len(m.editors) != 1 || filepath.Base(m.pv.path) != ".env" {
		t.Fatalf("the click closed README.md and left .env in front: %d tabs, %q", len(m.editors), m.pv.path)
	}
}

// TestEditorRule draws a rule under the editor strip, lit under the active tab.
func TestEditorRule(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	fire(m, m.openFile(filepath.Join(m.ws, "README.md"), false))
	checkWidths(t, m)

	rule := m.editorRule(m.mainW())
	if got, want := ansi.Strip(rule), strings.Repeat("─", m.mainW()); got != want {
		t.Fatalf("the rule spans the strip: %q", got)
	}

	if !strings.Contains(rule, "\x1b[") {
		t.Fatalf("the active tab's mark is colored: %q", rule)
	}
}

// TestSessionTabTint sets a session's tab apart from the files' tabs beside
// it: its glyph in Spaces' color, on a much softer shade of it, the active
// tab too.
func TestSessionTabTint(t *testing.T) {
	m := testModel(t)
	drainInputs(m)
	fire(m, m.openFile(filepath.Join(m.ws, "README.md"), false))
	m.switchSession("s1")

	g, c := m.tabGlyph(*m.session("s1"))
	tint := m.tabTint(c)

	if g == "" || tint == nil || hexColor(tint) == hexColor(c) {
		t.Fatalf("glyph %q in %v on %v", g, c, tint)
	}

	strip := m.editorStrip(m.mainW())
	if strings.Count(strip, bgParams(tint)) < 2 || !strings.Contains(strip, fgParams(c)) {
		t.Fatalf("the session's tab is not on its glyph's tint: %q", strip)
	}
}

// TestSessionStripHint puts what the session header used to say at the right
// end of its strip, and leaves no header row under it.
func TestSessionStripHint(t *testing.T) {
	m := testModel(t)
	drainInputs(m)
	m.switchSession("s1")

	if m.headH() != 0 || m.sessH() != m.mainH()-m.stripH() {
		t.Fatalf("a session has no header row: head %d, body %d", m.headH(), m.sessH())
	}

	m.sessions[0].Status, m.sessions[0].ExitCode = "exited", 3
	checkWidths(t, m)

	if strip := ansi.Strip(m.editorStrip(m.mainW())); !strings.Contains(strip, "exited 3") {
		t.Fatalf("the strip says it exited: %q", strip)
	}

	m.sessions[0].Status = "idle"
	m.term.scroll = 7

	if strip := ansi.Strip(m.editorStrip(m.mainW())); !strings.Contains(strip, "scrollback -7") {
		t.Fatalf("the strip says how far back it is: %q", strip)
	}

	if title := m.mainTitle(); strings.Contains(title, "shell") || title == "" {
		t.Fatalf("the frame says where, the tab what: %q", title)
	}
}

// TestTerminalStripCarriesTheButtons gives the Terminal panel what the editor
// has: its buttons at the right end of the strip, always shown, and a rule under it.
func TestTerminalStripCarriesTheButtons(t *testing.T) {
	m := testModelSized(t, 120, 30)
	drainInputs(m)

	m.sessions = append(m.sessions, termSession(m.ws, "p1"))
	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()
	m.focus = onPanel

	lines := m.termPanelLines(m.mainW(), m.termRows())
	if got, want := ansi.Strip(lines[0]), strings.Repeat("─", m.mainW()); got != want {
		t.Fatalf("the border over the strip: %q", got)
	}

	if strip := ansi.Strip(lines[1]); !strings.Contains(strip, icMax.s()) || !strings.Contains(strip, icClose.s()) {
		t.Fatalf("the buttons are always there: %q", strip)
	}

	if got, want := ansi.Strip(lines[2]), strings.Repeat("─", m.mainW()); got != want {
		t.Fatalf("the rule under the strip: %q", got)
	}

	if _, h := m.termBody(); h != m.termRows()-panelTopH {
		t.Fatalf("the screen is under the border, strip and rule: %d of %d rows", h, m.termRows())
	}

	checkWidths(t, m)

	// The button after ⋮ maximizes the panel, and then restores it.
	maxBtn := func() rowAction {
		acts := m.termButtons()
		return acts[slices.IndexFunc(acts, func(a rowAction) bool { return a.g == icEllipsis })+1]
	}

	click(m, m.mainX()+maxBtn().x+1, m.mainH()+1, tea.MouseLeft)

	if !m.termMax || maxBtn().g != icRestore {
		t.Fatalf("maximized: %v", m.termMax)
	}

	click(m, m.mainX()+maxBtn().x+1, m.mainH()+1, tea.MouseLeft)

	if m.termMax {
		t.Fatal("restored")
	}

	// The last one closes the panel.
	acts := m.termButtons()
	click(m, m.mainX()+acts[len(acts)-1].x+1, m.mainH()+1, tea.MouseLeft)

	if m.termOpen() {
		t.Fatal("the panel closed")
	}
}
