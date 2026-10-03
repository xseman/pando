package ui

import (
	"image/color"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// The tab strips a tab moves along, by a drag or by Move Tab Left/Right, as
// herdr's tabs do.
const (
	stripEditor  = iota + 1 // the editors over the main area
	stripSession            // the tabs of the session in view; the session's own stays first
	stripTerm               // the Terminal's shells
)

// stripTab is a tab that can move, where its strip draws it.
type stripTab struct {
	id   string
	x, w int
}

// stripTabs are strip s's movable tabs on screen, in strip columns; the
// geometry the renderer and the click hit tests use.
func (m *Model) stripTabs(s int) []stripTab {
	var out []stripTab

	switch s {
	case stripEditor:
		for _, t := range m.editorTabs(m.mainW()) {
			out = append(out, stripTab{id: m.editors[t.i].id(), x: t.x, w: t.w})
		}

		return out

	case stripSession:
		for _, t := range m.sessionTabs(m.sessW()) {
			if !t.plus && t.id != m.rootOf(m.sess) {
				out = append(out, stripTab{id: t.id, x: t.x, w: t.w})
			}
		}

		return out
	}

	for _, t := range m.termTabs(m.termStripW()) {
		if !t.plus {
			out = append(out, stripTab{id: t.id, x: t.x, w: t.w})
		}
	}

	return out
}

// tabAt is the id of strip s's tab at strip column x, the session's own
// included; "" over its + or past its tabs.
func (m *Model) tabAt(s, x int) string {
	if s == stripEditor {
		for _, t := range m.editorTabs(m.mainW()) {
			if x >= t.x && x < t.x+t.w {
				return m.editors[t.i].id()
			}
		}

		return ""
	}

	tabs := m.termTabs(m.termStripW())
	if s == stripSession {
		tabs = m.sessionTabs(m.sessW())
	}

	for _, t := range tabs {
		if !t.plus && x >= t.x && x < t.x+t.w {
			return t.id
		}
	}

	return ""
}

// stripOrder is every movable tab of strip s, shown or not, in its order.
func (m *Model) stripOrder(s int) []string {
	var ss []proto.Session

	switch s {
	case stripEditor:
		out := make([]string, len(m.editors))
		for i, e := range m.editors {
			out[i] = e.id()
		}

		return out

	case stripSession:
		root := m.rootOf(m.sess)
		ss = slices.DeleteFunc(m.tabsOf(root), func(s proto.Session) bool { return s.ID == root })

	default:
		ss = m.termSessions()
	}

	out := make([]string, len(ss))
	for i, x := range ss {
		out[i] = x.ID
	}

	return out
}

// stripActive is the tab strip s shows, "" for none or the session's own.
func (m *Model) stripActive(s int) string {
	switch s {
	case stripEditor:
		if m.edIdx >= 0 && m.edIdx < len(m.editors) {
			return m.editors[m.edIdx].id()
		}

		return ""

	case stripSession:
		if m.sess == m.rootOf(m.sess) {
			return ""
		}

		return m.sess
	}

	return m.tv.id
}

// focusedStrip is the strip Move Tab Left/Right acts on: the Terminal's with
// the keyboard in it, the session's when it has it or fills the main area,
// else the editors'.
func (m *Model) focusedStrip() int {
	switch {
	case m.termFocused():
		return stripTerm
	case m.sessFocused() || m.showsSession():
		return stripSession
	}

	return stripEditor
}

// stripMove puts tab id where tab to sits now in the model's own order, so a
// strip follows the pointer; the daemon hears of it in stripSave.
func (m *Model) stripMove(s int, id, to string) {
	if s != stripEditor {
		m.ag.moveSession(m, id, to)
		return
	}

	ids := m.stripOrder(s)

	i, j := slices.Index(ids, id), slices.Index(ids, to)
	if i < 0 || j < 0 || i == j {
		return
	}

	active := m.stripActive(s)
	e := m.editors[i]
	m.editors = slices.Insert(slices.Delete(m.editors, i, i+1), j, e)
	m.edIdx = slices.IndexFunc(m.editors, func(p preview) bool { return p.id() == active })
}

// stripSave keeps where tab id went: the workspace's editors in state.json,
// a session's tabs and shells through session.move onto to.
func (m *Model) stripSave(s int, id, to string) tea.Cmd {
	switch {
	case s == stripEditor:
		return m.saveEditors()
	case to == "":
		return nil
	}

	return do("session.move", proto.SessionMoveParams{ID: id, To: to})
}

// moveTab moves the focused strip's tab d places, wrapping at either end as
// herdr's move_tab_previous and move_tab_next do.
func (m *Model) moveTab(d int) tea.Cmd {
	s := m.focusedStrip()
	ids, id := m.stripOrder(s), m.stripActive(s)

	i := slices.Index(ids, id)
	if i < 0 || len(ids) < 2 {
		return nil
	}

	to := ids[(i+d+len(ids))%len(ids)]
	m.stripMove(s, id, to)

	return m.stripSave(s, id, to)
}

// grabTab picks tab id of strip s up at strip column x, the mouse's column
// and row being m.mouseX and m.mouseY: nothing moves until it is let go.
func (m *Model) grabTab(s int, id string, x int) {
	m.drag = &drag{kind: dragStrip, strip: s, tab: id, org: m.mouseX - x, y0: m.mouseY, slot: -1}
}

// dragStripTo follows the held tab with a mark where it would land, herdr's
// drop indicator, and moves it there once it is let go on its strip's row.
func (m *Model) dragStripTo(d *drag, x, y int, release bool) tea.Cmd {
	d.moved = true

	d.slot = -1
	if y == d.y0 {
		d.slot = m.stripSlot(d.strip, x-d.org)
	}

	if !release {
		return nil
	}

	m.drag = nil
	order := m.stripOrder(d.strip)

	i := slices.Index(order, d.tab)
	if i < 0 || d.slot < 0 {
		return nil
	}

	j := d.slot
	if i < j { // the slot counts the held tab, which leaves its place
		j--
	}

	if j = min(j, len(order)-1); j == i {
		return nil
	}

	m.stripMove(d.strip, d.tab, order[j])

	return m.stripSave(d.strip, d.tab, order[j])
}

// stripSlot is where a tab let go at strip column x lands, herdr's
// tab_drop_index_at: the index in stripOrder it goes before, the left half
// of a tab before it and the right half after, past the last tab the end.
func (m *Model) stripSlot(s, x int) int {
	tabs, order, last := m.stripTabs(s), m.stripOrder(s), -1
	for _, t := range tabs {
		i := slices.Index(order, t.id)
		if x < t.x+t.w/2 {
			return i
		}

		last = i
	}

	return last + 1
}

// slotX is the strip column slot k is marked in: the hairline before the
// tab it goes before, or after the last one; -1 off screen.
func (m *Model) slotX(s, k int) int {
	tabs, order := m.stripTabs(s), m.stripOrder(s)
	for n, t := range tabs {
		switch i := slices.Index(order, t.id); {
		case i == k:
			return max(t.x-tabGap, 0)
		case i+1 == k && n == len(tabs)-1:
			return t.x + t.w
		}
	}

	return -1
}

// stripMark draws the drop mark of a tab held over strip s into its row,
// whose tabs start at column off on bg.
func (m *Model) stripMark(line string, s, off int, bg color.Color) string {
	d := m.drag
	if d == nil || d.kind != dragStrip || d.strip != s || d.slot < 0 {
		return line
	}

	x := m.slotX(s, d.slot)
	if x < 0 {
		return line
	}

	st := fg(pal.accent)
	if bg != nil {
		st = st.Background(bg)
	}

	x += off

	return ansi.Cut(line, 0, x) + "\x1b[m" + st.Render("│") + "\x1b[m" + ansi.Cut(line, x+1, ansi.StringWidth(line))
}
