package ui

import (
	"cmp"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xseman/pando/internal/proto"
)

// group is one editor group, VS Code's: a strip of files and sessions and
// the one it shows. The active group's live in Model (pv, editors, edIdx,
// preview, sess, term and the workspace's sessTabs); the others wait here
// until they are drawn, fetched for or given the keyboard.
type group struct {
	pv      preview
	editors []preview
	edIdx   int
	preview bool
	sess    string
	term    term
	tabs    []string
}

// pane is the editor area's grid, VS Code's: a group, or two panes side by
// side (row) or one over the other, the first taking ratio of the room.
type pane struct {
	g     *group
	a, b  *pane
	up    *pane
	row   bool
	ratio float64
}

// box is a place on screen, in content cells.
type box struct{ x, y, w, h int }

// The least room a group keeps: with less, the focused group is drawn alone.
const (
	minGroupW = 20
	minGroupH = 5
)

// Where a tab goes against a group: into it, or into a new group on that side.
const (
	sideInto = iota
	sideLeft
	sideRight
	sideUp
	sideDown
)

// offMain is the focus while a group without the keyboard is drawn: its
// cursor and its active tab's mark stay unlit.
const offMain = -3

func newPane() *pane { return &pane{g: &group{edIdx: -1}} }

// leaves are the groups under p, left to right and top to bottom.
func (p *pane) leaves() []*pane {
	if p.g != nil {
		return []*pane{p}
	}

	return append(p.a.leaves(), p.b.leaves()...)
}

// halves are the rooms of p's two panes in b, a cell between them for the
// divider.
func (p *pane) halves(b box) (box, box) {
	if p.row {
		w := min(max(int(float64(b.w-1)*p.ratio+0.5), 1), b.w-2)
		return box{b.x, b.y, w, b.h}, box{b.x + w + 1, b.y, b.w - w - 1, b.h}
	}

	h := min(max(int(float64(b.h-1)*p.ratio+0.5), 1), b.h-2)

	return box{b.x, b.y, b.w, h}, box{b.x, b.y + h + 1, b.w, b.h - h - 1}
}

// even shares the room under p evenly, VS Code's distribute: the groups in a
// row as wide as each other, those in a column as tall.
func (p *pane) even() {
	if p.g != nil {
		return
	}

	p.ratio = float64(len(p.a.units(p.row))) / float64(len(p.units(p.row)))
	p.a.even()
	p.b.even()
}

// units are the panes p puts side by side (row) or one over the other, left
// to right or top to bottom.
func (p *pane) units(row bool) []*pane {
	if p.g != nil || p.row != row {
		return []*pane{p}
	}

	return append(p.a.units(row), p.b.units(row)...)
}

// evenPair is a double click on split p's divider, VS Code's on a sash: the
// two panes beside it share their room evenly, the others keeping theirs.
func (m *Model) evenPair(p *pane) tea.Cmd {
	a, size := m.area(), map[*pane]int{}

	for _, u := range p.units(p.row) {
		b := u.boxIn(a)
		size[u] = ifElse(p.row, b.w, b.h)
	}

	l, r := p.a.units(p.row), p.b.units(p.row)
	pl, pr := l[len(l)-1], r[0]
	size[pl], size[pr] = (size[pl]+size[pr])/2, (size[pl]+size[pr]+1)/2

	var (
		cells func(q *pane) int
		fit   func(q *pane)
	)

	cells = func(q *pane) int { // q's units and a divider between each two
		if q.g != nil || q.row != p.row {
			return size[q]
		}

		return cells(q.a) + 1 + cells(q.b)
	}

	fit = func(q *pane) {
		if q.g != nil || q.row != p.row {
			return
		}

		q.ratio = float64(cells(q.a)) / float64(cells(q)-1)
		fit(q.a)
		fit(q.b)
	}

	fit(p)

	return tea.Batch(m.fetchScreen(), m.saveEditors())
}

// fits reports every group under p with its least room in b.
func (p *pane) fits(b box) bool {
	if p.g != nil {
		return b.w >= minGroupW && b.h >= minGroupH
	}

	ba, bb := p.halves(b)

	return p.a.fits(ba) && p.b.fits(bb)
}

// boxIn is p's room when the grid has a.
func (p *pane) boxIn(a box) box {
	if p.up == nil {
		return a
	}

	x, y := p.up.halves(p.up.boxIn(a))

	return ifElse(p.up.a == p, x, y)
}

// areaH is the editor area's rows, above the bottom panel.
func (m *Model) areaH() int { return max(m.panelH()-m.termRows(), 0) }

// area is the editor area: main's columns above the bottom panel.
func (m *Model) area() box { _, c := m.layout(); return box{c.x, 0, c.w, m.areaH()} }

// tiled reports the groups drawn side by side: more than one, none
// maximized, each with its least room.
func (m *Model) tiled() bool { return m.grid.g == nil && !m.maxed && m.grid.fits(m.area()) }

// boxOf is where group p is drawn: its room in the grid, or the whole area.
func (m *Model) boxOf(p *pane) box {
	if !m.tiled() {
		return m.area()
	}

	return p.boxIn(m.area())
}

// mainY is the active group's first row; mainX, mainW and mainH its other
// edges.
func (m *Model) mainY() int { return m.boxOf(m.cur).y }
func (m *Model) mainX() int { return m.boxOf(m.cur).x }
func (m *Model) mainW() int { return m.boxOf(m.cur).w }
func (m *Model) mainH() int { return m.boxOf(m.cur).h }

// stash keeps the active group's state in its pane.
func (m *Model) stash() {
	g := m.cur.g
	g.pv, g.editors, g.edIdx, g.preview, g.sess, g.term, g.tabs = m.pv, m.editors, m.edIdx, m.preview, m.sess, m.term, m.sessTabs[m.ws]
}

// load makes group p the active one: its state in Model.
func (m *Model) load(p *pane) {
	g := p.g
	m.cur, m.pv, m.editors, m.edIdx, m.preview, m.sess, m.term = p, g.pv, g.editors, g.edIdx, g.preview, g.sess, g.term

	if m.sessTabs == nil {
		m.sessTabs = map[string][]string{}
	}

	m.sessTabs[m.ws] = g.tabs
}

// within runs f with group p's state in Model, as if it had the keyboard:
// to draw it, or to fetch its session's screen.
func (m *Model) within(p *pane, f func()) {
	was := m.cur
	if p == was {
		f()
		return
	}

	m.stash()
	m.load(p)
	f()
	m.stash()
	m.load(was)
}

// eachGroup runs f with every group in Model in turn.
func (m *Model) eachGroup(f func()) {
	for _, p := range m.grid.leaves() {
		m.within(p, f)
	}
}

// focusGroup gives group p the keyboard.
func (m *Model) focusGroup(p *pane) tea.Cmd {
	m.focus = onMain
	if p == m.cur {
		return nil
	}

	m.saveSpot()
	m.stash()
	m.load(p)
	m.refs = nil // the References inline were the other group's

	if m.showsSession() { // its frame stops pulsing
		m.acknowledge(m.sess)
	}

	return tea.Batch(m.syncTabs(), m.fetchScreen()) // tabs as wide as its file has them
}

// focusGroupN is ⌃1…⌃9: group n, counted left to right and top to bottom.
func (m *Model) focusGroupN(n int) tea.Cmd {
	ps := m.grid.leaves()
	if n >= len(ps) {
		return nil
	}

	return tea.Batch(m.focusGroup(ps[n]), m.saveEditors())
}

// holds reports tab id, a file's or a session's, in g: a session behind its
// files without a tab of its own is not one.
func (g *group) holds(id string) bool {
	shown := g.sess == id && (g.pv.kind == "" || !g.preview)
	return shown || slices.Contains(g.tabs, id) || slices.ContainsFunc(g.editors, func(e preview) bool { return e.id() == id })
}

// holder is the group other than the active one with tab id, nil for none:
// a file and a session have their tab in one group.
func (m *Model) holder(id string) *pane {
	for _, p := range m.grid.leaves() {
		if p != m.cur && p.g.holds(id) {
			return p
		}
	}

	return nil
}

// empty reports the active group without a tab.
func (m *Model) empty() bool {
	return len(m.editors) == 0 && m.pv.kind == "" && m.sess == "" && len(m.sessTabs[m.ws]) == 0
}

// replace puts pane n where p is in the grid.
func (m *Model) replace(p, n *pane) {
	switch u := p.up; {
	case u == nil:
		m.grid = n
	case u.a == p:
		u.a = n
	default:
		u.b = n
	}

	n.up = p.up
}

// splitPane puts a new group on side of p, every group then sharing the
// room evenly, and returns it.
func (m *Model) splitPane(p *pane, side int) *pane {
	q := newPane()
	s := &pane{row: side == sideLeft || side == sideRight, ratio: 0.5}
	m.replace(p, s)

	s.a, s.b = p, q
	if side == sideLeft || side == sideUp {
		s.a, s.b = q, p
	}

	p.up, q.up = s, s

	m.grid.even()

	return q
}

// dropPane takes group p out of the grid, the others sharing its room
// evenly.
func (m *Model) dropPane(p *pane) {
	if s := p.up; s != nil {
		m.replace(s, ifElse(s.a == p, s.b, s.a))
		m.grid.even()
	}
}

// prune closes the groups left without a tab, VS Code's closeEmptyGroups:
// the active one's neighbour takes the keyboard. The last group stays.
func (m *Model) prune() (cmd tea.Cmd) {
	for _, p := range m.grid.leaves() {
		if m.grid.g != nil {
			break
		}

		g := p.g
		if p == m.cur && !m.empty() || p != m.cur && (len(g.editors) > 0 || g.pv.kind != "" || g.sess != "" || len(g.tabs) > 0) {
			continue
		}

		next := ifElse(p.up.a == p, p.up.b, p.up.a).leaves()[0]
		m.dropPane(p)

		if p == m.cur {
			m.load(next)
			cmd = m.syncTabs()
		}
	}

	m.maxed = m.maxed && m.grid.g == nil

	return cmd
}

// idleGroup reports the active group one of several and showing nothing: the
// tab of a session that ended keeps it from prune.
func (m *Model) idleGroup() bool {
	return m.grid.g == nil && len(m.editors) == 0 && m.pv.kind == "" && !m.showsSession() && len(m.stripSessions()) == 0
}

// closeGroup takes the active group out of the grid, its neighbour taking
// the keyboard; a session behind it runs on, in Spaces.
func (m *Model) closeGroup() tea.Cmd {
	p := m.cur
	next := ifElse(p.up.a == p, p.up.b, p.up.a).leaves()[0]
	m.dropPane(p)
	m.load(next)
	m.maxed = m.maxed && m.grid.g == nil

	return tea.Batch(m.syncTabs(), m.fetchScreen(), m.saveEditors())
}

// moveTabTo moves the active group's tab id, a file's or a session's, into
// group dst or into a new group on side of it, and gives that group the
// keyboard: VS Code's Move Editor into Group, and a tab dropped on a group.
func (m *Model) moveTabTo(id string, dst *pane, side int) tea.Cmd {
	src, file := m.cur, slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == id })
	if side == sideInto && dst == src || file < 0 && m.sess != id && !slices.Contains(m.sessTabs[m.ws], id) {
		return nil
	}

	if side != sideInto {
		dst = m.splitPane(dst, side)
		if !m.grid.fits(m.area()) {
			m.dropPane(dst)
			return flash("no room for another group", true)
		}
	}

	var (
		e    preview
		took tea.Cmd
	)

	if file >= 0 {
		m.saveSpot()

		e = m.editors[file] // before it goes
		took = m.dropEditor(file)
	} else {
		took = m.closeSessTab(id)
	}

	m.stash()
	m.load(dst)

	var put tea.Cmd
	if file >= 0 {
		put = m.restore(e)
	} else {
		m.term, m.sess, m.preview = term{}, id, false
		m.openSessTab(id)
	}

	m.focus = onMain

	return tea.Batch(took, put, m.prune(), m.fetchScreen(), m.saveEditors())
}

// moveEditor is VS Code's Move Editor into Left/Right/Above/Below Group: the
// active tab goes to the group on that side, or to a new one there.
func (m *Model) moveEditor(side int) tea.Cmd {
	id := m.sess
	if m.showsPreview() {
		id = m.pv.id()
	}

	if id == "" {
		return nil
	}

	if n := m.neighbour(side); n != nil {
		return m.moveTabTo(id, n, sideInto)
	}

	return m.moveTabTo(id, m.cur, side)
}

// splitEditor is ⌃\: the active tab into a new group on side of its own,
// VS Code's Split Editor, which moves it here since a tab has one group.
func (m *Model) splitEditor(side int) tea.Cmd {
	if len(m.editors)+len(m.stripSessions()) < 2 {
		return flash("one tab: nothing to split off", false)
	}

	id := m.sess
	if m.showsPreview() {
		id = m.pv.id()
	}

	return m.moveTabTo(id, m.cur, side)
}

// splitSessions is Spaces' Split Sessions on worktree path: its sessions a
// group each, side by side and as wide as each other, the files of the
// groups it had in the first, as they were, unsaved edits too.
func (m *Model) splitSessions(path string) tea.Cmd {
	var ids []string

	for _, s := range m.mainSessions() {
		if s.Workspace == path {
			ids = append(ids, s.ID)
		}
	}

	if len(ids) < 2 {
		return flash("one session: nothing to split", false)
	}

	if len(ids)*(minGroupW+1)-1 > m.area().w {
		return flash("no room for a group each", true)
	}

	cmds := []tea.Cmd{m.switchWorkspace(path)}

	m.eachGroup(m.saveSpot)
	m.stash()

	first := m.cur
	g := first.g

	for _, p := range m.grid.leaves() {
		for _, e := range p.g.editors {
			if p != first && !slices.ContainsFunc(g.editors, func(o preview) bool { return o.id() == e.id() }) {
				g.editors = append(g.editors, e)
			}
		}
	}

	if g.sess != ids[0] {
		g.term = term{}
	}

	g.sess, g.tabs, g.preview = ids[0], []string{ids[0]}, false
	m.grid, first.up = first, nil

	for p, i := first, 1; i < len(ids); i++ {
		p = m.splitPane(p, sideRight)
		p.g.sess, p.g.tabs = ids[i], []string{ids[i]}
	}

	m.load(first)
	m.maxed, m.focus = false, onMain

	for _, id := range ids {
		cmds = append(cmds, m.wake(id))
	}

	return tea.Batch(append(cmds, m.syncTabs(), m.fetchScreen(), m.saveEditors())...)
}

// neighbour is the group on side of the active one, the nearest to its
// middle, nil at the area's edge.
func (m *Model) neighbour(side int) *pane {
	if !m.tiled() {
		return nil
	}

	a, me := m.area(), m.boxOf(m.cur)
	cx, cy := me.x+me.w/2, me.y+me.h/2

	var best *pane

	far := 1 << 30

	for _, p := range m.grid.leaves() {
		b := p.boxIn(a)

		var d int

		switch side {
		case sideLeft:
			d = me.x - (b.x + b.w)
		case sideRight:
			d = b.x - (me.x + me.w)
		case sideUp:
			d = me.y - (b.y + b.h)
		default:
			d = b.y - (me.y + me.h)
		}

		if p == m.cur || d < 0 {
			continue
		}

		d = d*1000 + max(cx-b.x-b.w/2, b.x+b.w/2-cx) + max(cy-b.y-b.h/2, b.y+b.h/2-cy)
		if d < far {
			best, far = p, d
		}
	}

	return best
}

// toggleMaxGroup is VS Code's Toggle Maximized Editor Group: the active
// group fills the editor area, the others waiting, or the grid comes back.
func (m *Model) toggleMaxGroup() tea.Cmd {
	if m.grid.g != nil {
		return nil
	}

	m.maxed, m.focus = !m.maxed, onMain

	return tea.Batch(m.fetchScreen(), m.saveEditors())
}

// paneAt is the group drawn at cell (x, y), nil off the grid.
func (m *Model) paneAt(x, y int) *pane {
	a := m.area()
	for _, p := range m.grid.leaves() {
		if b := p.boxIn(a); x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h {
			return p
		}
	}

	return nil
}

// splitAt is the split whose divider is at cell (x, y), nil for none.
func (m *Model) splitAt(x, y int) *pane {
	var at func(p *pane, b box) *pane

	at = func(p *pane, b box) *pane {
		if p.g != nil || x < b.x || x >= b.x+b.w || y < b.y || y >= b.y+b.h {
			return nil
		}

		ba, bb := p.halves(b)
		if p.row && x == ba.x+ba.w || !p.row && y == ba.y+ba.h {
			return p
		}

		return cmp.Or(at(p.a, ba), at(p.b, bb))
	}

	return at(m.grid, m.area())
}

// dragSplit follows a divider between groups: its split's ratio is where
// the pointer is, each side keeping its least room.
func (m *Model) dragSplit(d *drag, x, y int, release bool) tea.Cmd {
	p := d.split
	b := p.boxIn(m.area())

	at, n, least := x-b.x, b.w-1, minGroupW
	if !p.row {
		at, n, least = y-b.y, b.h-1, minGroupH
	}

	was := p.ratio
	if n > 0 {
		p.ratio = float64(min(max(at, least), n-least)) / float64(n)
	}

	if !m.grid.fits(m.area()) {
		p.ratio = was
	}

	if !release {
		return nil
	}

	m.drag = nil

	return tea.Batch(m.fetchScreen(), m.saveEditors())
}

// gridDrop is where the active group's tab held over the editor area at
// (x, y) would land: into the group under it, on its strip or its middle,
// or into a new group at the edge it is near, a quarter of the group's width
// or height; dst is nil where it would not move, and over its own group when
// it is the only tab there, which no split would leave anything behind.
func (m *Model) gridDrop(x, y int) (dst *pane, side int, t dropTarget) {
	dst = m.cur
	if m.tiled() {
		dst = m.paneAt(x, y)
	}

	if dst == nil {
		return nil, 0, t
	}

	b := m.boxOf(dst)
	if x < b.x || x >= b.x+b.w || y < b.y || y >= b.y+b.h {
		return nil, 0, t
	}

	qw, qh := max(b.w/4, 1), max(b.h/4, 1)

	switch {
	case y == b.y: // its strip
	case x < b.x+qw:
		side, b.w = sideLeft, b.w/2
	case x >= b.x+b.w-qw:
		side, b.x, b.w = sideRight, b.x+b.w-b.w/2, b.w/2
	case y < b.y+qh:
		side, b.h = sideUp, b.h/2
	case y >= b.y+b.h-qh:
		side, b.y, b.h = sideDown, b.y+b.h-b.h/2, b.h/2
	}

	if dst == m.cur && (side == sideInto || len(m.editors)+len(m.stripSessions()) < 2) {
		return nil, 0, t
	}

	label := []string{"Move Here", "Split Left", "Split Right", "Split Up", "Split Down"}[side]

	return dst, side, dropTarget{rc: rect{b.x, b.w}, y: b.y, h: b.h, fill: true, col: -1, label: label}
}

// maxAct is the strip's maximize button while there are groups to leave:
// VS Code's Maximize Group, Restore while it fills the area.
func (m *Model) maxAct() []rowAction {
	if m.grid.g != nil {
		return nil
	}

	return []rowAction{{g: ifElse(m.maxed, icRestore, icMax), tip: ifElse(m.maxed, "Restore Groups", "Maximize Group"), run: func(m *Model) tea.Cmd { return m.toggleMaxGroup() }}}
}

// areaLines draws the editor area: the grid, a divider between its groups,
// or the active group alone.
func (m *Model) areaLines(w int) []string {
	if !m.tiled() {
		return m.editorLines(w)
	}

	return m.paneLines(m.grid, m.area(), m.frames())
}

func (m *Model) paneLines(p *pane, b box, fs []groupFrame) []string {
	if p.g != nil {
		var out []string

		m.asDrawn(p, func() { out = m.editorLines(b.w) })

		return out[:b.h]
	}

	ba, bb := p.halves(b)
	a, c := m.paneLines(p.a, ba, fs), m.paneLines(p.b, bb, fs)

	st, lit := m.sashStyle(gridSash(p))
	if lit {
		fs = nil // a divider lit under the pointer or dragged shows that instead
	} else {
		st = fg(pal.panelBorder)
	}

	if p.row {
		x, glyph := ba.x+ba.w, ifElse(lit, "┃", "│")
		for i := range a {
			a[i] += frameRun(fs, x, ba.y+i, 1, glyph, st) + c[i]
		}

		return a
	}

	return append(append(a, frameRun(fs, b.x, ba.y+ba.h, b.w, ifElse(lit, "━", "─"), st)), c...)
}

// asDrawn runs f with group p in Model as paneLines draws it: a group
// without the keyboard has its focus offMain.
func (m *Model) asDrawn(p *pane, f func()) {
	focus := m.focus
	if p != m.cur {
		m.focus = offMain
	}

	m.within(p, f)
	m.focus = focus
}

// groupFrame is a group's frame while the grid is tiled: where the group is
// and its style.
type groupFrame struct {
	b     box
	st    lipgloss.Style
	alert bool
}

// alert is the color of the frame the session shown in the group in Model
// gives it, as session_highlight tints its tab: waiting for an answer or
// ended in the error's, finished unseen in the attention's; it pulses until
// the group has the keyboard. nil while nothing there needs you.
func (m *Model) alert() (c color.Color, pulse bool) {
	s := m.session(m.sess)
	if s == nil || !m.showsSession() || m.st.Settings.SessHi == "off" {
		return nil, false
	}

	switch sessionState(*s) {
	case "blocked", "exited":
		c = pal.errc
	case "done":
		c = pal.attention
	default:
		return nil, false
	}

	return c, m.pulses(*s)
}

// ownFrame is the frame of the group in Model while the grid is tiled: its
// alert, gone on the pulse's off beat, else the accent's shade around the
// group with the keyboard.
func (m *Model) ownFrame() (st lipgloss.Style, alert, ok bool) {
	if !m.tiled() {
		return st, false, false
	}

	if c, pulse := m.alert(); c != nil {
		return fg(c), true, !pulse || m.blinkOn
	}

	return fg(pal.sashHover), false, m.focus != offMain
}

// frames are the frames of the groups drawn now, none unless tiled.
func (m *Model) frames() (fs []groupFrame) {
	if !m.tiled() {
		return nil
	}

	a := m.area()
	for _, p := range m.grid.leaves() {
		m.asDrawn(p, func() {
			if st, alert, ok := m.ownFrame(); ok {
				fs = append(fs, groupFrame{p.boxIn(a), st, alert})
			}
		})
	}

	return fs
}

// frameAt is the frame drawn at cell (x, y) of the editor area, beside a
// group: an alert's over the focused group's; -1 for none.
func frameAt(fs []groupFrame, x, y int) int {
	at := -1

	for i, f := range fs {
		b := f.b
		side := (x == b.x-1 || x == b.x+b.w) && y >= b.y && y < b.y+b.h
		edge := (y == b.y-1 || y == b.y+b.h) && x >= b.x && x < b.x+b.w

		if (side || edge) && (at < 0 || f.alert) {
			at = i
		}
	}

	return at
}

// frameRun is n cells of glyph from cell (x0, y), in st or in the frame of
// the group beside each.
func frameRun(fs []groupFrame, x0, y, n int, glyph string, st lipgloss.Style) string {
	var sb strings.Builder

	for x := x0; x < x0+n; {
		at, e := frameAt(fs, x, y), x+1
		for e < x0+n && frameAt(fs, e, y) == at {
			e++
		}

		s := st
		if at >= 0 {
			s = fs[at].st
		}

		sb.WriteString(renderText(s, strings.Repeat(glyph, e-x)))
		x = e
	}

	return sb.String()
}

// gridSpec is the grid under p as state.json keeps it.
func (m *Model) gridSpec(p *pane) proto.Editors {
	if p.g == nil {
		a, b := m.gridSpec(p.a), m.gridSpec(p.b)
		return proto.Editors{Row: p.row, Ratio: p.ratio, A: &a, B: &b}
	}

	var e proto.Editors

	m.within(p, func() { e = m.groupSpec() })
	e.Focus = p == m.cur && m.grid.g == nil

	return e
}

// restoreGrid opens the groups e keeps, each as restoreGroup opens one, and
// gives the one that had it the keyboard.
func (m *Model) restoreGrid(e proto.Editors) tea.Cmd {
	var (
		cmds  []tea.Cmd
		focus *pane
		build func(e proto.Editors) *pane
	)

	build = func(e proto.Editors) *pane {
		if e.A == nil || e.B == nil {
			p := newPane()
			m.cur = p
			m.load(p)

			cmds = append(cmds, m.restoreGroup(e))
			m.preview = m.sess == "" // a session shows in front of the files, as it does in one group
			m.stash()

			if e.Focus || focus == nil {
				focus = p
			}

			return p
		}

		p := &pane{row: e.Row, ratio: cmpRatio(e.Ratio)}
		p.a, p.b = build(*e.A), build(*e.B)
		p.a.up, p.b.up = p, p

		return p
	}

	m.grid = build(e)
	m.load(focus)
	m.maxed = e.Max && m.grid.g == nil

	return tea.Batch(append(cmds, m.prune())...)
}

// cmpRatio is a saved split's ratio, half when it has none that makes sense.
func cmpRatio(r float64) float64 {
	if r <= 0 || r >= 1 {
		return 0.5
	}

	return r
}

// forget drops the sessions gone from a group without the keyboard: its
// shown one hands over to its last tab still alive.
func (g *group) forget(alive func(string) bool) {
	g.tabs = slices.DeleteFunc(slices.Clone(g.tabs), func(id string) bool { return !alive(id) })
	if g.sess == "" || alive(g.sess) {
		return
	}

	g.sess, g.term = "", term{}
	if len(g.tabs) > 0 && g.pv.kind == "" {
		g.sess = g.tabs[len(g.tabs)-1]
	}
}
