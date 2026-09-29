package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/xseman/pando/internal/proto"
)

// The tab strips Move Tab Left/Right moves a tab along, as herdr's
// move_tab_previous and move_tab_next do.
const (
	stripEditor  = iota + 1 // the editors over the main area
	stripSession            // the tabs of the session in view; the session's own stays first
	stripTerm               // the Terminal's shells
)

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

// stripMove puts tab id where tab to sits now in the model's own order; the
// daemon hears of it in stripSave.
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
