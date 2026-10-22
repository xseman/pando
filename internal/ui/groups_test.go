package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// groupsModel has README.md and .env open, .env in front, the keyboard in
// the editor.
func groupsModel(t *testing.T) *Model {
	t.Helper()

	m := testModelSized(t, 160, 40)
	drainInputs(m)

	m.sess, m.focus = "", onMain
	for _, f := range []string{"README.md", ".env"} {
		fire(m, m.openFile(filepath.Join(m.ws, f), false))
	}

	return m
}

// TestSplitEditor: ⌃\ moves the active tab into a new group on the right,
// which takes the keyboard; both groups draw their strips side by side, a
// click gives the left one the keyboard back, and ⌃2 the right one.
func TestSplitEditor(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`)

	ps := m.grid.leaves()
	if len(ps) != 2 || !m.grid.row || m.cur != ps[1] || m.pv.path != filepath.Join(m.ws, ".env") || len(m.editors) != 1 {
		t.Fatalf("split right: %d groups, active %v, shows %q, %d tabs", len(ps), m.cur == ps[1], m.pv.path, len(m.editors))
	}

	if g := ps[0].g; len(g.editors) != 1 || filepath.Base(g.pv.path) != "README.md" {
		t.Fatalf("the left group keeps README.md: %+v", g.editors)
	}

	out := checkWidths(t, m)
	if !strings.Contains(out, "README.md") || !strings.Contains(out, ".env") {
		t.Fatalf("both strips on screen:\n%s", out)
	}

	fire(m, m.pv.load(m)) // as the split's command reads it

	if c := m.View().Cursor; c == nil || c.X < m.mainX() {
		t.Fatalf("the cursor is in the right group, from column %d: %+v", m.mainX(), c)
	}

	left := ps[0].boxIn(m.area())
	click(m, left.x+5, left.y+5, tea.MouseLeft)

	if m.cur != ps[0] || filepath.Base(m.pv.path) != "README.md" {
		t.Fatalf("a click in the left group gives it the keyboard: %q", m.pv.path)
	}

	press(m, "ctrl+2")

	if m.cur != ps[1] || m.focus != onMain {
		t.Fatal("ctrl+2 focuses the second group")
	}

	fire(m, m.splitEditor(sideRight))

	if len(m.grid.leaves()) != 2 || !strings.Contains(m.msg, "nothing to split") {
		t.Fatalf("a group of one tab does not split: %d groups, flash %q", len(m.grid.leaves()), m.msg)
	}
}

// TestDragTabToGroup drops a tab on another group's middle, which closes the
// group it left empty, and on an edge, which splits that group there.
func TestDragTabToGroup(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`) // .env alone on the right

	ps := m.grid.leaves()
	left, right := ps[0].boxIn(m.area()), ps[1].boxIn(m.area())
	tab := m.editorTabs(m.mainW())[0]

	m.Update(tea.MouseClickMsg{X: right.x + tab.x + 1, Y: right.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: left.x + left.w/2, Y: left.y + left.h/2, Button: tea.MouseLeft})

	out := checkWidths(t, m)
	if !strings.Contains(out, "Move Here") {
		t.Fatalf("the drop target says where it goes:\n%s", out)
	}

	m.Update(tea.MouseReleaseMsg{X: left.x + left.w/2, Y: left.y + left.h/2, Button: tea.MouseLeft})

	if m.grid.g == nil || len(m.editors) != 2 || filepath.Base(m.pv.path) != ".env" {
		t.Fatalf("one group again, .env in front: %v, %d tabs, %q", m.grid.g != nil, len(m.editors), m.pv.path)
	}

	// Onto the bottom edge: a group under it.
	a := m.area()
	tab = m.editorTabs(m.mainW())[1]
	m.Update(tea.MouseClickMsg{X: a.x + tab.x + 1, Y: a.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: a.x + a.w/2, Y: a.y + a.h - 2, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: a.x + a.w/2, Y: a.y + a.h - 2, Button: tea.MouseLeft})

	if ps := m.grid.leaves(); len(ps) != 2 || m.grid.row || m.cur != ps[1] || m.mainY() == 0 {
		t.Fatalf("split down: %d groups, row %v, below %v", len(ps), m.grid.row, m.mainY())
	}

	checkWidths(t, m)
}

// TestGroupDivider drags the divider between two groups, each keeping its
// least room.
func TestGroupDivider(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`)

	l := m.grid.leaves()[0].boxIn(m.area())
	x := l.x + l.w // the divider

	if s := m.sashUnder(x, 10); s != gridSash(m.grid) {
		t.Fatalf("the divider is a sash: %+v", s)
	}

	m.Update(tea.MouseClickMsg{X: x, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x - 30, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x - 30, Y: 10, Button: tea.MouseLeft})

	if l2 := m.grid.leaves()[0].boxIn(m.area()); l2.w != l.w-30 {
		t.Fatalf("dragged 30 cells left: %d, was %d", l2.w, l.w)
	}

	m.Update(tea.MouseClickMsg{X: x - 30, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 0, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 0, Y: 10, Button: tea.MouseLeft})

	if l2 := m.grid.leaves()[0].boxIn(m.area()); l2.w != minGroupW {
		t.Fatalf("it stops at the least room: %d", l2.w)
	}

	checkWidths(t, m)
}

// TestGroupsRestore saves the groups with the workspace's editors and opens
// them again, the focused one with the keyboard; a narrow screen draws the
// focused group alone, and Maximize Group does the same on purpose.
func TestGroupsRestore(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`)
	fire(m, m.saveEditors())

	e := m.st.Editors[m.ws]
	if e.A == nil || e.B == nil || !e.Row || !e.B.Focus || len(e.A.Open) != 1 || len(e.B.Open) != 1 {
		t.Fatalf("saved grid: %+v", e)
	}

	m.restoreEditors()

	if ps := m.grid.leaves(); len(ps) != 2 || m.cur != ps[1] || filepath.Base(m.pv.path) != ".env" || filepath.Base(ps[0].g.pv.path) != "README.md" {
		t.Fatalf("restored: %d groups, front %q", len(ps), m.pv.path)
	}

	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})

	if m.tiled() || m.mainW() != m.area().w {
		t.Fatalf("too narrow for two: the focused group alone, %d of %d", m.mainW(), m.area().w)
	}

	checkWidths(t, m)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	fire(m, m.toggleMaxGroup())

	if m.tiled() || !m.maxed || !m.st.Editors[m.ws].Max {
		t.Fatal("maximized: the focused group alone, saved")
	}

	if strip := ansi.Strip(m.editorStrip(m.mainW())); !strings.Contains(strip, icRestore.s()) {
		t.Fatalf("the strip restores: %q", strip)
	}
}

// TestSessionInGroup opens a session in a group of its own beside a file:
// both draw, Spaces showing it again keeps it there, and its tab dropped on
// the file's group brings it back.
func TestSessionInGroup(t *testing.T) {
	m := groupsModel(t)
	m.switchSession("s1")

	if !m.showsSession() || len(m.stripSessions()) != 1 {
		t.Fatal("the session over the files, its tab in the strip")
	}

	fire(m, m.moveEditor(sideRight))

	ps := m.grid.leaves()
	if len(ps) != 2 || m.cur != ps[1] || m.sess != "s1" || !m.showsSession() || ps[0].g.sess != "" {
		t.Fatalf("the session in a group on the right: %d groups, sess %q, left's %q", len(ps), m.sess, ps[0].g.sess)
	}

	_ = m.focusGroup(ps[0])
	m.switchSession("s1")

	if m.cur != ps[1] || !m.showsSession() || ps[0].g.sess != "" || ps[0].g.pv.kind == "" {
		t.Fatalf("showing the session again gives its group the keyboard, the left one keeps its file: %q", ps[0].g.sess)
	}

	checkWidths(t, m)
	fire(m, m.moveEditor(sideLeft))

	if m.grid.g == nil || m.sess != "s1" || len(m.editors) != 2 {
		t.Fatalf("back beside the files: grid %v, sess %q, %d tabs", m.grid.g != nil, m.sess, len(m.editors))
	}
}

// TestSessionBehindFiles: a session the files opened in front of, with no tab
// of its own, is not that group's; picked, it opens in the focused group and
// leaves the other.
func TestSessionBehindFiles(t *testing.T) {
	m := groupsModel(t)
	m.sess, m.preview = "s1", true // as pando starts: the worktree's session behind its files
	press(m, `ctrl+\`)

	ps := m.grid.leaves()
	if ps[0].g.sess != "s1" || m.holder("s1") != nil {
		t.Fatalf("behind README.md, tabless: in no group, %q", ps[0].g.sess)
	}

	if e := m.gridSpec(m.grid); e.A.Session != "" {
		t.Fatalf("not saved as shown: %+v", e.A)
	}

	m.switchSession("s1")

	if m.cur != ps[1] || !m.showsSession() || ps[0].g.sess != "" {
		t.Fatalf("in the focused group, gone from the other: right %v, left's %q", m.cur == ps[1], ps[0].g.sess)
	}
}
