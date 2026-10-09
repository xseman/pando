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
// Terminal's shell with the keyboard in the panel, the session's tab with a
// session in view, which keeps its own tab first, else the active editor.
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

	tab := func(id string) proto.Session {
		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: tabAgent, Parent: "s1"}, Status: "idle"}
	}

	shell := func(id string) proto.Session {
		s := termSession(m.ws, id)
		s.Parent = "s1"

		return s
	}

	m.Update(sessionsMsg{m.sessions[0], tab("t1"), tab("t2"), shell("p1"), shell("p2")})
	m.Update(focusSessionMsg("s1"))

	ids := func(ss []proto.Session) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	press(m, "ctrl+shift+pgdown")

	if got := ids(m.tabsOf("s1")); got != "s1 t1 t2" {
		t.Fatalf("the session's own tab does not move: %s", got)
	}

	m.Update(focusSessionMsg("t1"))
	m.focus = onMain
	press(m, "ctrl+shift+pgup")

	if got := ids(m.tabsOf("s1")); got != "s1 t2 t1" {
		t.Fatalf("⌃⇧pgup wraps the first tab past the last, behind the session's own: %s", got)
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
// there, and a release off the strip's row leaves it.
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

	// The left half of the first tab: before it. Let go off the row, nothing moves.
	tabs = m.editorTabs(m.mainW())
	m.Update(tea.MouseClickMsg{X: x0 + tabs[2].x + 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x0 + tabs[0].x + 1, Y: 0, Button: tea.MouseLeft})

	if m.drag.slot != 0 || markAt(m.editorStrip(m.mainW())) != 0 {
		t.Fatalf("the slot before the first tab: %d", m.drag.slot)
	}

	m.Update(tea.MouseReleaseMsg{X: x0 + tabs[0].x + 1, Y: 5, Button: tea.MouseLeft})

	if got := names(); got != "README.md a-much-longer-name.txt .env" || m.drag != nil {
		t.Fatalf("a release off the strip drops nothing: %s", got)
	}

	click(m, x0+m.editorTabs(m.mainW())[0].x+1, 0, tea.MouseLeft)

	if m.drag != nil || m.edIdx != 0 || names() != "README.md a-much-longer-name.txt .env" {
		t.Fatalf("a click that never moved only activates: drag %+v, %s", m.drag, names())
	}
}

// TestDragSessionTab reorders a session's tabs by a drag: the
// session's own tab stays first and never picks up, a drop over it lands
// right after it, and the release tells the daemon.
func TestDragSessionTab(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	tab := func(id string) proto.Session {
		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: tabAgent, Parent: "s1", Cmd: []string{"/bin/" + id}}, Status: "idle"}
	}

	m.Update(sessionsMsg{m.sessions[0], tab("t1"), tab("t2"), tab("t3")})
	m.Update(focusSessionMsg("t1"))

	order := func() string {
		var out []string
		for _, s := range m.tabsOf("s1") {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	at := func(id string) (x, w int) {
		for _, t := range m.sessionTabs(m.sessW()) {
			if t.id == id {
				return m.mainX() + t.x, t.w
			}
		}

		t.Fatalf("no tab %s on the strip", id)

		return 0, 0
	}

	x, _ := at("s1")
	click(m, x+1, 0, tea.MouseLeft)

	if m.sess != "s1" || m.drag != nil {
		t.Fatalf("the session's own tab shows but does not pick up: %q, %+v", m.sess, m.drag)
	}

	x, _ = at("t3")
	m.Update(tea.MouseClickMsg{X: x + 1, Y: 0, Button: tea.MouseLeft})

	x, _ = at("s1")
	m.Update(tea.MouseMotionMsg{X: x + 1, Y: 0, Button: tea.MouseLeft})

	x1, _ := at("t1")
	if got := markAt(m.sessionStrip(m.sessW())); m.drag.slot != 0 || got != x1-m.mainX()-tabGap {
		t.Fatalf("over the session's own tab the mark sits before t1: slot %d, column %d", m.drag.slot, got)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x + 1, Y: 0, Button: tea.MouseLeft})
	if got := order(); got != "s1 t3 t1 t2" || cmd == nil || m.drag != nil || m.sess != "t3" {
		t.Fatalf("the release puts t3 first after s1 and tells the daemon: %s, cmd %v, showing %q", got, cmd != nil, m.sess)
	}
}

// TestDragTerminalTab reorders the Terminal panel's shells by a drag on its
// title row, which is otherwise the panel's sash.
func TestDragTerminalTab(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	m.sessions = append(m.sessions, termSession(m.ws, "p1"), termSession(m.ws, "p2"))
	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()
	checkWidths(t, m)

	order := func() string {
		var out []string
		for _, s := range m.termSessions() {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	tabs := m.termTabs(m.termStripW())
	y, left := m.mainH(), m.mainX()+1

	m.Update(tea.MouseClickMsg{X: left + tabs[0].x + 1, Y: y, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragStrip || m.focus != onPanel {
		t.Fatalf("a press on a shell's tab picks it up, not the sash: %+v", m.drag)
	}

	m.Update(tea.MouseMotionMsg{X: left + tabs[1].x + tabs[1].w - 1, Y: y, Button: tea.MouseLeft})

	if got := markAt(m.termPanelLines(m.mainW(), m.termRows())[0]); got != 1+tabs[1].x+tabs[1].w {
		t.Fatalf("the mark sits after p2 in the panel's title row: %d", got)
	}

	m.Update(tea.MouseReleaseMsg{X: left + tabs[1].x + tabs[1].w - 1, Y: y, Button: tea.MouseLeft})

	if got := order(); got != "p2 p1" || m.drag != nil || m.st.Settings.TermH != 8 {
		t.Fatalf("p1 moved past p2, the panel kept its height: %s, %d", got, m.st.Settings.TermH)
	}
}

// TestPlusButtonHovers paints the + of the session and Terminal strips on
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
		{stripSession, m.sessionTabs(m.sessW())},
		{stripTerm, m.termTabs(m.termStripW())},
	} {
		plus := c.tabs[len(c.tabs)-1]
		if !plus.plus || m.tabAt(c.strip, plus.x+1) != plusTab || m.tabAt(c.strip, plus.x+plus.w) != "" {
			t.Fatalf("strip %d: tabAt over the +: %+v", c.strip, plus)
		}

		for _, over := range []string{"", plusTab} {
			segs := tabSegs(c.tabs, over)

			if got := segs[len(segs)-1].ownBg; got != (over == plusTab) {
				t.Fatalf("strip %d, over %q: the + paints its own background: %v", c.strip, over, got)
			}
		}
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
