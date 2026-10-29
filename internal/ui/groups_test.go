package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
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

// TestSplitEven: each split shares the area out evenly again, however many
// groups there are, and a group that closes gives its room to all of them.
func TestSplitEven(t *testing.T) {
	m := groupsModel(t)
	fire(m, m.openFile(filepath.Join(m.ws, "src", "deep", "x.go"), false))

	widths := func() []int {
		var w []int
		for _, p := range m.grid.leaves() {
			w = append(w, p.boxIn(m.area()).w)
		}

		return w
	}

	press(m, `ctrl+\`) // x.go on the right
	fire(m, m.focusGroupN(0))
	press(m, `ctrl+\`) // .env between README.md and x.go

	w := widths()
	if len(w) != 3 || slices.Max(w)-slices.Min(w) > 1 {
		t.Fatalf("three groups as wide as each other: %v", w)
	}

	fire(m, m.closeEditor(0)) // .env, the middle group's only tab
	fire(m, m.prune())

	if w := widths(); len(w) != 2 || slices.Max(w)-slices.Min(w) > 1 {
		t.Fatalf("two groups left, as wide as each other: %v", w)
	}

	checkWidths(t, m)
}

// TestEvenPair: a double click on a divider shares the room of the two
// groups beside it evenly, the third keeping its width.
func TestEvenPair(t *testing.T) {
	m := groupsModel(t)
	fire(m, m.openFile(filepath.Join(m.ws, "src", "deep", "x.go"), false))
	press(m, `ctrl+\`)
	fire(m, m.focusGroupN(0))
	press(m, `ctrl+\`) // README.md | .env | x.go

	widths := func() []int {
		var w []int
		for _, p := range m.grid.leaves() {
			w = append(w, p.boxIn(m.area()).w)
		}

		return w
	}

	mid := m.grid.leaves()[1].boxIn(m.area())
	x := mid.x + mid.w // between .env and x.go

	m.Update(tea.MouseClickMsg{X: x, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x + 20, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x + 20, Y: 10, Button: tea.MouseLeft})

	was := widths()

	for range 2 {
		m.Update(tea.MouseClickMsg{X: x + 20, Y: 10, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: x + 20, Y: 10, Button: tea.MouseLeft})
	}

	if w := widths(); w[0] != was[0] || w[1]-w[2] > 1 || w[2]-w[1] > 1 || w[1]+w[2] != was[1]+was[2] {
		t.Fatalf("the two beside the divider even, README.md's kept: %v, was %v", w, was)
	}

	checkWidths(t, m)
}

// TestGroupFrame: the active group's rule under its strip and the dividers
// beside it, a column's edge too, are drawn in a shade of the accent; a click
// moves the frame, and a group drawn alone has none.
func TestGroupFrame(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`) // .env on the right, with the keyboard

	ps := m.grid.leaves()
	l, r := ps[0].boxIn(m.area()), ps[1].boxIn(m.area())
	framedFrom := func(y int) int { // the first column of the frame in row y, -1 for none
		line := strings.Split(m.View().Content, "\n")[y]

		i := strings.Index(line, fgParams(pal.sashHover))
		if i < 0 {
			return -1
		}

		return ansi.StringWidth(line[:strings.LastIndex(line[:i], "\x1b")])
	}

	if x, d := framedFrom(r.y+1), framedFrom(r.y+5); x != r.x-1 || d != r.x-1 {
		t.Fatalf("the right group framed from the divider: rule from %d, divider at %d, want %d", x, d, r.x-1)
	}

	click(m, l.x+5, l.y+5, tea.MouseLeft)

	if x := framedFrom(l.y + 1); x != l.x-1 {
		t.Fatalf("a click frames the left group from the Explorer's edge: rule from %d, want %d", x, l.x-1)
	}

	fire(m, m.toggleMaxGroup())
	checkWidths(t, m)

	if x := framedFrom(l.y + 1); x != -1 {
		t.Fatalf("a group drawn alone has no frame: from %d", x)
	}
}

// TestDragTabToGroup carries a tab under the pointer and drops it on another
// group's middle, which closes the group it left empty, and on an edge, which
// splits that group there.
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

	if ghost := ansi.Strip(strings.Split(out, "\n")[left.y+left.h/2+1]); !strings.Contains(ghost, ".env") {
		t.Fatalf("the tab follows the pointer, a row under it: %q", ghost)
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

// TestMotionFrames drags a tab faster than motionGap: a motion that soon
// after the last frame gets that frame again, the one redraw it asks for
// draws the drag where the pointer stopped, and a release draws at once.
func TestMotionFrames(t *testing.T) {
	m := groupsModel(t)
	a := m.area()
	tab := m.editorTabs(m.mainW())[0]

	defer func(g time.Duration) { motionGap = g }(motionGap)

	motionGap = time.Hour

	m.Update(tea.MouseClickMsg{X: a.x + tab.x + 1, Y: a.y, Button: tea.MouseLeft})
	before := checkWidths(t, m)

	_, cmd := m.Update(tea.MouseMotionMsg{X: a.x + a.w/2, Y: a.y + a.h/2, Button: tea.MouseLeft})
	if out := checkWidths(t, m); out != before || cmd == nil || !m.redrawDue {
		t.Fatalf("a motion inside the gap keeps the frame and asks for a redraw: same %v, cmd %v", out == before, cmd != nil)
	}

	m.Update(tea.MouseMotionMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if out := checkWidths(t, m); out != before {
		t.Fatal("the next motion keeps it too")
	}

	m.Update(redrawMsg{})

	if out := checkWidths(t, m); !strings.Contains(out, "Split Right") || m.redrawDue {
		t.Fatalf("the redraw draws the drag where the pointer stopped:\n%s", out)
	}

	m.Update(tea.MouseReleaseMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if out := checkWidths(t, m); strings.Contains(out, "Split Right") || len(m.grid.leaves()) != 2 {
		t.Fatalf("the release draws at once, split: %d groups\n%s", len(m.grid.leaves()), out)
	}
}

// TestDragBase: a drag's motions, and the redraw motion asks for, draw over
// the frame under the drag as drawn once; any other message draws it anew.
func TestDragBase(t *testing.T) {
	m := groupsModel(t)
	a := m.area()
	tab := m.editorTabs(m.mainW())[0]

	m.Update(tea.MouseClickMsg{X: a.x + tab.x + 1, Y: a.y, Button: tea.MouseLeft})
	checkWidths(t, m)

	b := m.base
	for _, msg := range []tea.Msg{tea.MouseMotionMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft}, redrawMsg{}} {
		m.Update(msg)

		if out := checkWidths(t, m); m.base != b || !strings.Contains(out, "Split Right") {
			t.Fatalf("%T keeps the base and draws the drop box over it:\n%s", msg, out)
		}
	}

	m.Update(spinMsg{})
	checkWidths(t, m)

	if m.base == b {
		t.Fatal("any other message draws the base anew")
	}
}

// TestDragOnlyTab: a group's only tab held over that group offers no split,
// which would leave nothing behind.
func TestDragOnlyTab(t *testing.T) {
	m := groupsModel(t)
	fire(m, m.closeEditor(0)) // .env alone

	a, tab := m.area(), m.editorTabs(m.mainW())[0]
	m.Update(tea.MouseClickMsg{X: a.x + tab.x + 1, Y: a.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if out := checkWidths(t, m); strings.Contains(out, "Split Right") || m.drag.drop != nil {
		t.Fatalf("no drop target over its own edge:\n%s", out)
	}

	m.Update(tea.MouseReleaseMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if m.grid.g == nil || len(m.editors) != 1 {
		t.Fatalf("still one group with .env: %v, %d tabs", m.grid.g != nil, len(m.editors))
	}
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
	checkWidths(t, m) // held, it draws lit in place of the frames

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

// TestGroupAlert: a group whose session waits for an answer is framed in the
// error's color, the Explorer's edge beside it too; without the keyboard the
// frame pulses until the group is focused, then stays steady.
func TestGroupAlert(t *testing.T) {
	m := groupsModel(t)
	m.st.Settings.SessHi = "tint"
	m.switchSession("s1")
	fire(m, m.moveEditor(sideLeft)) // s1 on the left, beside the Explorer

	ps := m.grid.leaves()
	l := ps[0].boxIn(m.area())
	_ = m.focusGroup(ps[1])

	ss := slices.Clone(m.sessions)
	for i := range ss {
		if ss[i].ID == "s1" {
			ss[i].Status = "blocked"
		}
	}

	m.Update(sessionsMsg(ss))

	red := fgParams(pal.errc)
	framed := func() (edge, div bool) {
		rows := strings.Split(m.View().Content, "\n")
		at := func(x int) bool {
			pre := ansi.Cut(rows[5], 0, x+1)
			i := strings.LastIndex(pre, "\x1b[3")

			return i >= 0 && strings.HasPrefix(pre[i+2:], red)
		}

		return at(l.x - 1), at(l.x + l.w)
	}

	if !m.blinking {
		t.Fatal("a group without the keyboard pulses")
	}

	for i, on := range []bool{true, false} {
		m.Update(blinkMsg{})

		if e, d := framed(); e != on || d != on {
			t.Fatalf("beat %d: the Explorer's edge red %v, the divider red %v, want %v", i, e, d, on)
		}
	}

	_ = m.focusGroup(ps[0])
	m.Update(blinkMsg{})

	if e, d := framed(); !e || !d || m.blinking {
		t.Fatalf("focused, the frame stays red, steady: edge %v, divider %v, pulsing %v", e, d, m.blinking)
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

// TestCloseIdleGroup: a group whose session ended shows nothing; ⌃w or ⌃d
// closes it, the other group taking the keyboard.
func TestCloseIdleGroup(t *testing.T) {
	m := groupsModel(t)
	m.switchSession("s1")
	fire(m, m.moveEditor(sideRight))
	m.Update(sessionsMsg{})

	if m.grid.g != nil || !m.idleGroup() {
		t.Fatalf("the ended session's group stays, showing nothing: grid %v, kind %q, sess %q, strip %v", m.grid.g != nil, m.pv.kind, m.sess, m.stripSessions())
	}

	press(m, "ctrl+d")

	if m.grid.g == nil || m.cur != m.grid || filepath.Base(m.pv.path) != ".env" {
		t.Fatalf("⌃d closes it, the files' group left: %q", m.pv.path)
	}
}

// TestSplitSessions: a worktree's Split Sessions in Spaces puts each of its
// sessions in a group of its own, side by side and as wide as each other,
// the files of both groups open before in the first, which takes the keyboard.
func TestSplitSessions(t *testing.T) {
	m := groupsModel(t)
	press(m, `ctrl+\`) // .env in a group of its own

	var ss []proto.Session
	for _, id := range []string{"s1", "s2", "s3"} {
		ss = append(ss, proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: "shell", Name: id}, Status: "idle"})
	}

	m.Update(sessionsMsg(ss))
	press(m, "3")

	rows := m.ag.rows(m)
	i := slices.IndexFunc(rows, func(r agRow) bool { return r.shows(m.ws) })
	acts := m.ag.actions(m, rows[i], m.bodyW(viewAgents))

	j := slices.IndexFunc(acts, func(a rowAction) bool { return a.g == icSplitAll })
	if j < 0 {
		t.Fatalf("the worktree with three sessions has Split Sessions: %+v", acts)
	}

	fire(m, acts[j].run(m))

	ps := m.grid.leaves()

	var w []int

	for n, p := range ps {
		if p.g.sess != ss[n].ID {
			t.Fatalf("group %d shows %q", n, p.g.sess)
		}

		w = append(w, p.boxIn(m.area()).w)
	}

	if len(ps) != 3 || !m.grid.row || m.cur != ps[0] || !m.showsSession() || len(m.editors) != 2 || slices.Max(w)-slices.Min(w) > 1 {
		t.Fatalf("three groups side by side, the first with the keyboard %v, the files and s1 in it: %d files, widths %v", m.cur == ps[0], len(m.editors), w)
	}

	checkWidths(t, m)
}

// TestDragFileFromExplorer carries a file from Explorer over the editor area
// as a tab held off its strip: onto an edge it opens in a new group there.
func TestDragFileFromExplorer(t *testing.T) {
	m := groupsModel(t)
	x, top := m.colRect(m.colOf(viewFiles)).x+4, m.bodyTop(viewFiles) // row 0 is src, row 1 README.md
	a := m.area()

	m.Update(tea.MouseClickMsg{X: x, Y: top + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	out := checkWidths(t, m)
	if !strings.Contains(out, "Split Right") {
		t.Fatalf("the drop target says where it goes:\n%s", out)
	}

	if ghost := ansi.Strip(strings.Split(out, "\n")[a.y+a.h/2+1]); !strings.Contains(ghost, "▎") || !strings.Contains(ghost, "README.md") {
		t.Fatalf("its tab follows the pointer, a row under it: %q", ghost)
	}

	m.Update(tea.MouseReleaseMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if ps := m.grid.leaves(); len(ps) != 2 || m.cur != ps[1] || filepath.Base(m.pv.path) != "README.md" || len(m.editors) != 1 {
		t.Fatalf("README.md alone on the right: %d groups, %q, %d tabs", len(ps), m.pv.path, len(m.editors))
	}

	checkWidths(t, m)
}

// TestDragSessionFromSpaces carries a session of the worktree in view from
// Spaces into a new group; one of another project's stays in Spaces.
func TestDragSessionFromSpaces(t *testing.T) {
	m := groupsModel(t)
	other := addProject(t, m, "dev", "s9")
	m.focus = m.colOf(viewFiles)
	press(m, "3") // Spaces in Explorer's place

	x, top := m.colRect(m.colOf(viewAgents)).x+4, m.bodyTop(viewAgents)
	a := m.area()
	o := slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agSession && r.s.ID == "s9" })

	m.Update(tea.MouseClickMsg{X: x, Y: top + o, Button: tea.MouseLeft})

	if m.drag != nil || m.ws != other {
		t.Fatalf("another project's session switches on the press, no drag: %+v, ws %q", m.drag, m.ws)
	}

	m.Update(tea.MouseReleaseMsg{X: x, Y: top + o, Button: tea.MouseLeft})
	fire(m, m.switchWorkspace(m.wss[0].Path))

	m.Update(tea.MouseClickMsg{X: x, Y: top + 1, Button: tea.MouseLeft}) // s1, under the heading holding its checkout
	m.Update(tea.MouseMotionMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	out := checkWidths(t, m)
	if !strings.Contains(out, "Split Right") || m.drag.to != "" {
		t.Fatalf("the drop target over the editor area, no slot in Spaces:\n%s", out)
	}

	if ghost := ansi.Strip(strings.Split(out, "\n")[a.y+a.h/2+1]); !strings.Contains(ghost, "▎") || !strings.Contains(ghost, sessionName(*m.session("s1"))) {
		t.Fatalf("its tab follows the pointer, a row under it: %q", ghost)
	}

	m.Update(tea.MouseReleaseMsg{X: a.x + a.w - 2, Y: a.y + a.h/2, Button: tea.MouseLeft})

	if ps := m.grid.leaves(); len(ps) != 2 || m.cur != ps[1] || m.sess != "s1" || !m.showsSession() {
		t.Fatalf("s1 in a group on the right: %d groups, session %q", len(ps), m.sess)
	}

	// One that moves among its worktree's opens on the release instead, here
	// in a group of its own under the right one.
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "shell"}, Status: "idle"})
	o = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agSession && r.s.ID == "s2" })
	r := m.boxOf(m.cur)

	m.Update(tea.MouseClickMsg{X: x, Y: top + o, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragRow || m.sess != "s1" {
		t.Fatalf("a row drag, nothing opened yet: %+v, session %q", m.drag, m.sess)
	}

	m.Update(tea.MouseMotionMsg{X: r.x + r.w/2, Y: r.y + r.h - 2, Button: tea.MouseLeft})

	if ghost := ansi.Strip(strings.Split(checkWidths(t, m), "\n")[r.y+r.h-1]); !strings.Contains(ghost, sessionName(*m.session("s2"))) {
		t.Fatalf("the tab it would get follows the pointer: %q", ghost)
	}

	m.Update(tea.MouseReleaseMsg{X: r.x + r.w/2, Y: r.y + r.h - 2, Button: tea.MouseLeft})

	if ps := m.grid.leaves(); len(ps) != 3 || m.cur != ps[2] || m.sess != "s2" {
		t.Fatalf("s2 under s1: %d groups, session %q", len(ps), m.sess)
	}

	checkWidths(t, m)
}
