package ui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// The tab strips a tab moves along, by a drag or by Move Tab Left/Right, as
// herdr's tabs do.
const (
	stripEditor = iota + 1 // the files and sessions over the main area
	stripTerm              // the Terminal's shells
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

	if s == stripEditor {
		for _, t := range m.editorTabs(m.mainW()) {
			out = append(out, stripTab{id: m.tabID(t), x: t.x, w: t.w})
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

// ruleMark is a tab's span under a strip: lit when the tab is active, tinted
// when the mouse is on it.
type ruleMark struct {
	x, w        int
	active, hot bool
}

// ruleRow is the line under a strip, as the activity bar has under its icons:
// a faint rule, in the accent color under the active tab and in a tint of it
// under the one the mouse is on. A lit rule (a strip that is a sash while it
// lights or drags) is drawn heavy in that style between the marks.
func ruleRow(w int, marks []ruleMark, lit ...lipgloss.Style) string {
	if len(lit) > 0 {
		return markedRule(w, marks, "━", lit[0])
	}

	return markedRule(w, marks, "─", fg(pal.rulerBorder))
}

// markedRule is ruleRow's line: glyph in st between the marks.
func markedRule(w int, marks []ruleMark, glyph string, st lipgloss.Style) string {
	rule := func(n int) seg { return sg(strings.Repeat(glyph, max(n, 0)), st) }

	var segs []seg

	cx := 0

	for _, k := range marks {
		c := markColor(k.active, k.hot)
		if c == nil {
			continue
		}

		segs = append(segs, rule(k.x-cx), sg(strings.Repeat("─", k.w), fg(c)))
		cx = k.x + k.w
	}

	return row(w, nil, append(segs, rule(w-cx)))
}

// plusTab is tabUnder's answer over a strip's + button, which has no session.
const plusTab = "+"

// tabUnder is the id of strip s's tab at strip column x, the session's own
// included; plusTab over the terminal strip's +, "" past its tabs. It
// says too whether x is on its ✕, which a tab under the mouse shows, the
// cells where a click closes it.
func (m *Model) tabUnder(s, x int) (id string, onClose bool) {
	if s == stripEditor {
		for _, t := range m.editorTabs(m.mainW()) {
			if x >= t.x && x < t.x+t.w {
				return m.tabID(t), x >= t.x+t.w-tabCloseW
			}
		}

		return "", false
	}

	for _, t := range m.termTabs(m.termStripW()) {
		if x >= t.x && x < t.x+t.w {
			if t.plus {
				return plusTab, false
			}

			return t.id, x >= t.x+t.w-tabCloseW
		}
	}

	return "", false
}

// setOver notes the tab of strip s under column x as hovered, and its ✕ when
// the pointer is on it.
func (m *Model) setOver(s, x int) {
	id, onClose := m.tabUnder(s, x)
	m.overTab = id

	if onClose {
		m.overX = id
	}
}

// stripOrder is every movable tab of strip s, shown or not, in its order.
func (m *Model) stripOrder(s int) []string {
	var ss []proto.Session

	if s == stripEditor { // the files, a session before the file at its sessPos
		var out []string

		key := map[string]int{}

		for i, e := range m.editors {
			out = append(out, e.id())
			key[e.id()] = 2*i + 1
		}

		for _, id := range m.stripSessions() {
			out = append(out, id)
			key[id] = 2 * m.sessPos(id)
		}

		slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(key[a], key[b]) })

		return out
	}

	ss = m.termSessions()

	out := make([]string, len(ss))
	for i, x := range ss {
		out[i] = x.ID
	}

	return out
}

// stripActive is the tab strip s shows, "" for none or the session's own.
func (m *Model) stripActive(s int) string {
	switch {
	case s != stripEditor:
		return m.tv.id
	case m.showsSession():
		return m.sess
	case m.edIdx >= 0 && m.edIdx < len(m.editors):
		return m.editors[m.edIdx].id()
	}

	return ""
}

// focusedStrip is the strip Move Tab Left/Right acts on: the Terminal's with
// the keyboard in it, else the editor area's.
func (m *Model) focusedStrip() int {
	if m.termFocused() {
		return stripTerm
	}

	return stripEditor
}

// sessPos is where session id's tab sits among the editor strip's files: the
// index of the file it goes before, right after the last still open of the
// files it was moved behind, first with none of them. A tab never moved
// follows the files, the ones opened later too.
func (m *Model) sessPos(id string) int {
	after, ok := m.sessAfter[id]
	if !ok {
		return len(m.editors)
	}

	pos := 0

	for i, e := range m.editors {
		if slices.Contains(after, e.id()) {
			pos = i + 1
		}
	}

	return pos
}

// moveSessTab puts session id's tab after files, the editors before it now.
func (m *Model) moveSessTab(id string, files []preview) {
	if m.sessAfter == nil {
		m.sessAfter = map[string][]string{}
	}

	after := make([]string, len(files))
	for i, e := range files {
		after[i] = e.id()
	}

	m.sessAfter[id] = after
}

// renameTab keeps the session tabs next to an editor whose tab now shows
// another file, as the preview tab does.
func (m *Model) renameTab(from, to string) {
	for _, after := range m.sessAfter {
		if i := slices.Index(after, from); i >= 0 {
			after[i] = to
		}
	}
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

	ids = slices.Insert(slices.Delete(ids, i, i+1), j, id)

	active := ""
	if m.edIdx >= 0 && m.edIdx < len(m.editors) { // the file in front, or behind the session
		active = m.editors[m.edIdx].id()
	}

	// The files in their new order, each session after the ones before it.
	var (
		eds  []preview
		tabs []string
	)

	for _, x := range ids {
		if k := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == x }); k >= 0 {
			eds = append(eds, m.editors[k])
			continue
		}

		tabs = append(tabs, x)
		m.moveSessTab(x, eds)
	}

	for _, x := range tabs { // the ones after every file follow the files opened next
		if len(m.sessAfter[x]) == len(eds) {
			delete(m.sessAfter, x)
		}
	}

	if m.sessTabs == nil {
		m.sessTabs = map[string][]string{}
	}

	m.editors, m.sessTabs[m.ws] = eds, tabs
	m.edIdx = slices.IndexFunc(m.editors, func(p preview) bool { return p.id() == active })
}

// stripSave keeps where tab id went: the workspace's editors in state.json,
// the Terminal's shells through session.move onto to.
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
	m.drag = &drag{kind: dragStrip, strip: s, tab: id, org: m.mouseX - x, x0: m.mouseX, y0: m.mouseY, slot: -1}
}

// dragGhost is the held tab lifted off its strip and carried by the pointer,
// as VS Code drags a tab's image: the cell it was picked up by stays under
// the pointer, a row lower so the drop mark on the strip stays in sight.
func (m *Model) dragGhost() (box []string, x, y int, ok bool) {
	d := m.drag
	if d == nil || d.kind != dragStrip || !d.moved {
		return nil, 0, 0, false
	}

	var label string

	tx := 0

	if d.strip == stripEditor {
		for _, t := range m.editorTabs(m.mainW()) {
			if t.id == d.tab || t.i >= 0 && m.editors[t.i].id() == d.tab {
				label, tx = t.label, t.x
			}
		}
	} else {
		for _, t := range m.termTabs(m.termStripW()) {
			if t.id == d.tab {
				label, tx = t.label, t.x
			}
		}
	}

	if label == "" {
		return nil, 0, 0, false
	}

	st := lipgloss.NewStyle().Background(pal.selBg).Bold(true)
	if pal.selFg != nil {
		st = st.Foreground(pal.selFg)
	}

	chip := renderText(fg(pal.accent).Background(pal.selBg), "▎") + renderText(st, label)
	w := ansi.StringWidth(chip)

	x, y = max(min(m.mouseX-(d.x0-d.org-tx), m.w-w), 0), m.mouseY+1
	if y >= m.panelH() {
		y = m.mouseY - 1
	}

	return []string{chip}, x, y, true
}

// dragStripTo follows the held tab with a mark where it would land, herdr's
// drop indicator, and moves it there once it is let go on its strip's row.
// An editor's tab held over the editor area off its strip goes to a group
// instead: the one under it, or a new one at the edge it is near.
func (m *Model) dragStripTo(d *drag, x, y int, release bool) tea.Cmd {
	d.moved = true

	d.slot, d.drop = -1, nil

	switch {
	case y == d.y0 && (d.strip != stripEditor || x >= m.mainX() && x < m.mainX()+m.mainW()):
		d.slot = m.stripSlot(d.strip, x-d.org)
	case d.strip == stripEditor:
		var t dropTarget
		if d.dst, d.side, t = m.gridDrop(x, y); d.dst != nil {
			d.drop = &t
		}
	}

	if !release {
		return nil
	}

	m.drag = nil
	if d.drop != nil {
		return m.moveTabTo(d.tab, d.dst, d.side)
	}

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
// whose tabs start at column off.
func (m *Model) stripMark(line string, s, off int) string {
	d := m.drag
	if d == nil || d.kind != dragStrip || d.strip != s || d.slot < 0 {
		return line
	}

	x := m.slotX(s, d.slot)
	if x < 0 {
		return line
	}

	x += off

	return ansi.Cut(line, 0, x) + "\x1b[m" + fg(pal.accent).Render("│") + "\x1b[m" + ansi.Cut(line, x+1, ansi.StringWidth(line))
}
