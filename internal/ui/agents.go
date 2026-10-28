package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

const (
	agProject = iota
	agWorkspace
	agSession
	agSub  // a subagent the session above runs: seen, never selected; a click goes to its session
	agGap  // the row before an open heading, drawn as a rule over it; blank before the folded and as the padding that sinks them; nothing selects it
	agTime // a heading of no project: Pinned, No Sessions, Group by Time's Today, Yesterday, …, or Group by State's
)

// pinnedHead is the heading over the pinned sessions, an agTime row.
const pinnedHead = "Pinned"

// bareHead is the heading over the projects with no session, an agTime row
// last in the tree.
const bareHead = "No Sessions"

type agRow struct {
	kind    int
	project string
	ws      proto.Workspace
	s       proto.Session
	when    string // agTime: the heading
	pinned  bool   // a row under Pinned: a session's copy, or its worktree's
	sub     proto.Subagent
}

// agents is the Project → Workspace → Session tree.
type agents struct {
	l         list
	collapsed map[string]bool
	folded    []string // the collapsed keys in the order the folded list shows them, the one folded last first
	ptop      int      // the Pinned pane's first row in view; l.top is the tree's
}

type newSessionMsg proto.Session

func (a *agents) rows(m *Model) []agRow {
	q, sessions := m.query(viewAgents), m.listedSessions()

	var out []agRow

	if m.byWorkspace() {
		out = a.treeRows(m, sessions, q)
	} else {
		out = a.groupRows(m, sessions, q)
	}
	// Pinned is a pane of its own over the tree, its border the gap.
	pins := a.pinnedRows(m, sessions, q)
	if q == "" {
		return append(pins, a.pinFolded(m, a.withGaps(withSubagents(out)), a.pane(pins, m.bodyH(viewAgents)).lines)...)
	}

	return append(matching(q, pins), a.withGaps(withSubagents(matching(q, out)))...)
}

// withSubagents puts the subagents a session runs under its row in the tree;
// Pinned's copy goes without.
func withSubagents(rows []agRow) []agRow {
	out := make([]agRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
		if r.kind != agSession || r.pinned {
			continue
		}

		for _, sub := range r.s.Subagents {
			out = append(out, agRow{kind: agSub, project: r.project, ws: r.ws, s: r.s, sub: sub})
		}
	}

	return out
}

// inert reports a row nothing selects: a gap, or a subagent's.
func (r agRow) inert() bool { return r.kind == agGap || r.kind == agSub }

// moreSessions reports whether another session follows row i's in its
// worktree, past the subagents under it.
func moreSessions(rows []agRow, i int) bool {
	j := i + 1
	for j < len(rows) && rows[j].kind == agSub {
		j++
	}

	return j < len(rows) && rows[j].kind == agSession
}

// matching are the rows that match q, plus the heading and worktree they sit
// under.
func matching(q string, out []agRow) []agRow {
	keep := make([]bool, len(out))
	project, ws := -1, -1

	for i, r := range out {
		switch r.kind {
		case agProject, agTime:
			project, ws = i, -1
		case agWorkspace:
			ws = i
		}

		if fuzzy(q, agLabel(r)) < 0 {
			continue
		}

		keep[i] = true
		if project >= 0 {
			keep[project] = true
		}

		if r.kind == agSession && ws >= 0 {
			keep[ws] = true
		}
	}

	var shown []agRow

	for i, r := range out {
		if keep[i] {
			shown = append(shown, r)
		}
	}

	return shown
}

// treeRows are Group by Workspace's rows: project, worktree, session. A
// project whose only worktree is its own checkout shows it on its heading
// (merged), its sessions straight under that. A folded project sinks below
// the open ones, to the top of the folded (shownProjects), so the spaces in
// use stay together at the top; unfolded it goes last among the open
// (unfold). The projects with no session go after all, under bareHead,
// open and folded the same way.
func (a *agents) treeRows(m *Model, sessions []proto.Session, q string) []agRow {
	var (
		open, bare         []agRow
		folded, bareFolded [][]agRow
	)

	busy := m.busyProjects()
	add := func(p string, rows []agRow) {
		switch {
		case len(rows) == 0:
		case !busy[p] && a.collapsed[p]:
			bareFolded = append(bareFolded, rows)
		case !busy[p]:
			bare = append(bare, rows...)
		case a.collapsed[p]:
			folded = append(folded, rows)
		default:
			open = append(open, rows...)
		}
	}

	known := map[string]bool{}

	for _, p := range m.projects() {
		var wts []proto.Workspace

		for _, w := range m.wss {
			if w.Project == p {
				wts, known[w.Path] = append(wts, w), true
			}
		}

		head := agRow{kind: agProject, project: p}
		if len(wts) == 1 && wts[0].Main && (!a.collapsed[p] || q != "") {
			head.ws = wts[0]
		}

		rows := []agRow{head}

		for _, w := range wts {
			if a.collapsed[p] && q == "" {
				break
			}

			if !head.merged() {
				rows = append(rows, agRow{kind: agWorkspace, project: p, ws: w})
			}

			for _, s := range sessions {
				if s.Workspace == w.Path {
					rows = append(rows, agRow{kind: agSession, project: p, ws: w, s: s})
				}
			}
		}

		add(p, rows)
	}

	var other []agRow

	for _, s := range sessions { // sessions outside every known worktree, under "other"
		if known[s.Workspace] {
			continue
		}

		if other == nil {
			other = []agRow{{kind: agProject}}
		}

		if !a.collapsed[""] || q != "" {
			other = append(other, agRow{kind: agSession, ws: proto.Workspace{Path: s.Workspace}, s: s})
		}
	}

	add("", other)

	byRank := func(x, y []agRow) int { return cmp.Compare(a.foldRank(x[0].project), a.foldRank(y[0].project)) }
	slices.SortStableFunc(folded, byRank)
	slices.SortStableFunc(bareFolded, byRank)

	open = append(open, slices.Concat(folded...)...)
	if bare == nil && bareFolded == nil {
		return open
	}

	open = append(open, agRow{kind: agTime, when: bareHead})
	if a.collapsed[timeKey(bareHead)] && q == "" {
		return open
	}

	return append(append(open, bare...), slices.Concat(bareFolded...)...)
}

// merged reports an open project heading that holds its own checkout, its
// only worktree, in place of a row of the checkout's.
func (r agRow) merged() bool { return r.kind == agProject && r.ws.Path != "" }

// shows reports r the tree's row of worktree path: the worktree's own, or
// its project's heading that holds it. Pinned's copy is not.
func (r agRow) shows(path string) bool {
	return (r.kind == agWorkspace || r.merged()) && r.ws.Path == path && !r.pinned
}

// busyProjects are the projects with a session, listed or left out by the
// Filter; "" for one outside every known worktree.
func (m *Model) busyProjects() map[string]bool {
	busy := map[string]bool{}
	for _, s := range m.mainSessions() {
		busy[m.projectOf(s.Workspace)] = true
	}

	return busy
}

// shownProjects are the projects in the order Spaces lists them: the open
// ones in the project Sort's order, then the folded ones, the one folded last
// first; the same again for the projects with no session, under bareHead.
func (a *agents) shownProjects(m *Model) []string {
	busy := m.busyProjects()

	var out []string

	for _, bare := range []bool{false, true} {
		in := slices.DeleteFunc(m.projects(), func(p string) bool { return busy[p] == bare })
		open := slices.DeleteFunc(slices.Clone(in), func(p string) bool { return a.collapsed[p] })
		folded := slices.DeleteFunc(in, func(p string) bool { return !a.collapsed[p] })
		slices.SortStableFunc(folded, func(x, y string) int { return cmp.Compare(a.foldRank(x), a.foldRank(y)) })
		out = append(append(out, open...), folded...)
	}

	return out
}

// apart reports projects x and p listed apart, which no move crosses: one
// folded and one open, or one with sessions (busy) and one under bareHead.
func (a *agents) apart(busy map[string]bool, x, p string) bool {
	return a.collapsed[x] != a.collapsed[p] || busy[x] != busy[p]
}

// foldRank is folded heading k's place in the folded list; one the list
// misses goes after the rest, in its saved order.
func (a *agents) foldRank(k string) int {
	if i := slices.Index(a.folded, k); i >= 0 {
		return i
	}

	return len(a.folded)
}

// fold shuts heading k, on top of the folded, or opens it.
func (a *agents) fold(k string, shut bool) {
	if a.collapsed == nil {
		a.collapsed = map[string]bool{}
	}

	a.collapsed[k] = shut

	a.folded = slices.DeleteFunc(a.folded, func(x string) bool { return x == k })
	if shut {
		a.folded = slices.Insert(a.folded, 0, k)
	}
}

// withGaps puts a row before every open heading but the first, drawn as a
// rule over it, VS Code's border over a pane; and a blank one before the
// folded that follow an open one, so a run of them stays a tight list. A
// heading with nothing under it, a project with no session, has nothing to
// set apart either. bareHead is a strip with a rule under it too, over the
// projects it holds, so it reads as none of them.
func (a *agents) withGaps(rows []agRow) []agRow {
	out := make([]agRow, 0, len(rows)+4)
	for i, r := range rows {
		lone := r.kind == agProject && rows[max(i-1, 0)].kind == agProject
		if (r.kind == agProject || r.kind == agTime) && i > 0 && rows[i-1].when != bareHead && !lone && (!a.collapsed[foldKey(r)] || rows[i-1].kind != r.kind) {
			out = append(out, agRow{kind: agGap})
		}

		out = append(out, r)
		if r.when == bareHead && i+1 < len(rows) {
			out = append(out, agRow{kind: agGap})
		}
	}

	return out
}

// pinFolded pads the tree with blank rows before its first folded project, or
// a folded bareHead, so the folded sit on the panel's bottom edge as VS
// Code's collapsed views do. A tree taller than the panel scrolls as it is;
// all folded, they all sink. The Pinned pane takes the panel's first above
// lines.
func (a *agents) pinFolded(m *Model, rows []agRow, above int) []agRow {
	k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agProject && a.collapsed[r.project] || r.when == bareHead })
	if k >= 0 && rows[k].when == bareHead && !a.collapsed[timeKey(bareHead)] { // open, it is no folded list
		k = -1
	}

	pad := m.bodyH(viewAgents) - above - len(rows)
	if !m.byWorkspace() || k < 0 || pad <= 0 {
		return rows
	}

	gaps := make([]agRow, pad)
	for i := range gaps {
		gaps[i].kind = agGap
	}

	return slices.Insert(rows, k, gaps...)
}

// listedSessions are the sessions Spaces shows: the Filter's states left out,
// in the Sort's order. Created keeps the order they were made in; Updated,
// and grouping by Time or State, puts the newest first.
func (m *Model) listedSessions() []proto.Session {
	st := m.st.Settings

	var out []proto.Session

	for _, s := range m.mainSessions() {
		if !slices.Contains(st.SpHide, sessionState(s)) {
			out = append(out, s)
		}
	}

	if st.SpSort == "updated" || !m.byWorkspace() {
		slices.SortStableFunc(out, func(a, b proto.Session) int { return m.sessionTime(b).Compare(m.sessionTime(a)) })
	}

	return out
}

// byWorkspace reports Spaces grouped by Workspace, the project tree, not by
// Time or State.
func (m *Model) byWorkspace() bool {
	g := m.st.Settings.SpGroup

	return g != "time" && g != "state"
}

// projects are the projects in the project Sort's order: their saved one, or
// by Updated the one whose sessions changed last first, the rest saved.
func (m *Model) projects() []string {
	out := slices.Clone(m.st.Projects)
	if m.st.Settings.SpProj != "updated" {
		return out
	}

	last := map[string]time.Time{}

	for _, s := range m.mainSessions() {
		p := m.projectOf(s.Workspace)
		if t := s.Updated; t.After(last[p]) {
			last[p] = t
		}
	}

	slices.SortStableFunc(out, func(a, b string) int { return last[b].Compare(last[a]) })

	return out
}

// sessionTime is what the Sort orders s by, and Group by Time files it under.
func (m *Model) sessionTime(s proto.Session) time.Time {
	if m.st.Settings.SpSort == "updated" {
		return s.Updated
	}

	return s.Created
}

// heads are the headings Group by Time or State lists, in their order:
// timeBuckets, or the states the most demanding first.
func (m *Model) heads() []string {
	if m.st.Settings.SpGroup != "state" {
		return timeBuckets
	}

	out := make([]string, len(sessionStates))
	for i, st := range sessionStates {
		out[i] = st.label
	}

	return out
}

// headOf is the heading of heads session s goes under at moment now.
func (m *Model) headOf(s proto.Session, now time.Time) string {
	if m.st.Settings.SpGroup != "state" {
		return timeBucket(m.sessionTime(s), now)
	}

	state := sessionState(s)

	return sessionStates[slices.IndexFunc(sessionStates, func(x struct{ state, label string }) bool { return x.state == state })].label
}

// timeBuckets are Group by Time's headings, newest first, as VS Code's.
var timeBuckets = []string{"Today", "Yesterday", "Last 7 Days", "Last 30 Days", "Older"}

// timeBucket files t under one of timeBuckets by calendar day; a session
// older than the created field is Older.
func timeBucket(t, now time.Time) string {
	if t.IsZero() {
		return "Older"
	}

	y, mo, d := now.Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())

	switch {
	case !t.Before(day):
		return "Today"
	case !t.Before(day.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(day.AddDate(0, 0, -6)):
		return "Last 7 Days"
	case !t.Before(day.AddDate(0, 0, -29)):
		return "Last 30 Days"
	}

	return "Older"
}

// groupRows are Group by Time's and Group by State's rows: a heading per
// bucket that has sessions (heads), folded like a project, and the sessions
// under it newest first.
func (a *agents) groupRows(m *Model, sessions []proto.Session, q string) []agRow {
	var out []agRow

	now := time.Now()

	for _, b := range m.heads() {
		head := false

		for _, s := range sessions {
			if m.headOf(s, now) != b {
				continue
			}

			if !head {
				out, head = append(out, agRow{kind: agTime, when: b}), true
			}

			if a.collapsed[timeKey(b)] && q == "" {
				continue
			}

			ws := proto.Workspace{Path: s.Workspace}
			if w := m.workspace(s.Workspace); w != nil {
				ws = *w
			}

			out = append(out, agRow{kind: agSession, ws: ws, s: s})
		}
	}

	return out
}

// pinnedRows are the Pinned heading and the pinned sessions under it, under
// their worktree as in the tree, so a branch shows once and whole: the
// worktrees in the order their first session was pinned, the sessions in
// the order they were. Each stays in the tree too.
func (a *agents) pinnedRows(m *Model, sessions []proto.Session, q string) []agRow {
	var (
		wss []proto.Workspace
		by  = map[string][]agRow{}
	)

	for _, id := range m.st.Pinned {
		i := slices.IndexFunc(sessions, func(s proto.Session) bool { return s.ID == id })
		if i < 0 {
			continue
		}

		ws := proto.Workspace{Path: sessions[i].Workspace}
		if w := m.workspace(ws.Path); w != nil {
			ws = *w
		}

		if by[ws.Path] == nil {
			wss = append(wss, ws)
		}

		by[ws.Path] = append(by[ws.Path], agRow{kind: agSession, ws: ws, s: sessions[i], pinned: true})
	}

	if wss == nil {
		return nil
	}

	out := []agRow{{kind: agTime, when: pinnedHead}}
	if a.collapsed[timeKey(pinnedHead)] && q == "" {
		return out
	}

	for _, ws := range wss {
		out = append(append(out, agRow{kind: agWorkspace, project: ws.Project, ws: ws, pinned: true}), by[ws.Path]...)
	}

	return out
}

// togglePin pins session id under Pinned, or unpins it, dropping the
// sessions gone since; Spaces keeps its selection on the row it was on.
func (m *Model) togglePin(id string) tea.Cmd {
	sel := m.ag.selected(m)

	pins := slices.DeleteFunc(append([]string{}, m.st.Pinned...), func(p string) bool { return p == id || m.session(p) == nil }) // not nil: null would keep the list saved before
	if !slices.Contains(m.st.Pinned, id) {
		pins = append(pins, id)
	}

	m.st.Pinned = pins

	if sel != nil {
		m.ag.follow(m, sel)
		m.ag.snap(m) // a pin adds rows above it
	}

	return do("state.set", map[string]any{"spaces_pinned": pins})
}

// pinPane is the Pinned pane over the tree: a strip, the sessions, and a
// heavy border under them, set apart from the tree's rules. n is the rows it holds (the heading, then the worktrees and
// sessions), h those in view, lines the view lines it takes. Zero without
// pins.
type pinPane struct{ n, h, lines int }

// pane is the Pinned pane of rows in a view h lines high: as tall as its
// rows, at most a third of the view, past that it scrolls.
func (a *agents) pane(rows []agRow, h int) pinPane {
	if len(rows) == 0 || rows[0].kind != agTime || rows[0].when != pinnedHead {
		return pinPane{}
	}

	p := pinPane{n: 1, lines: 2} // the strip and the border
	for p.n < len(rows) && rows[p.n].pinned {
		p.n++
	}

	p.h = max(min(p.n-1, h/3), 0)
	p.lines += p.h // the sessions

	return p
}

// rowAt is the row of rows on line y of the view, -1 on none: the rule, the
// border, past the end.
func (a *agents) rowAt(m *Model, rows []agRow, y int) int {
	p := a.pane(rows, m.bodyH(viewAgents))

	switch {
	case y < 0:
		return -1
	case y == 0 && p.n > 0:
		return 0
	case y < p.lines:
		if i := y + a.ptop; y >= 1 && y < 1+p.h && i < p.n {
			return i
		}

		return -1
	}

	if s := a.sticky(m, rows); y == p.lines && s >= 0 {
		return s
	}

	if i := a.l.at(y-p.lines, len(rows)-p.n); i >= 0 {
		return p.n + i
	}

	return -1
}

// sticky is the heading drawn over the tree's first line once the tree
// scrolls past it, as VS Code's sticky scroll pins a tree's parents: the
// heading of the row on the line below, -1 when that heading is in view, the
// tree is at its top or too short, or a row drags. The row the heading
// covers scrolls on unseen.
func (a *agents) sticky(m *Model, rows []agRow) int {
	h := m.bodyH(viewAgents)
	p := a.pane(rows, h)

	top := p.n + a.l.top
	if a.l.top == 0 || m.drag != nil || h-p.lines < 4 || top+1 >= len(rows) {
		return -1
	}

	for i := top + 1; i >= p.n; i-- {
		if rows[i].kind == agProject || rows[i].kind == agTime {
			if i >= top { // in view on its own
				return -1
			}

			return i
		}
	}

	return -1
}

// snap scrolls the selection into view: the Pinned pane's or the tree's.
func (a *agents) snap(m *Model) {
	rows, h := a.rows(m), m.bodyH(viewAgents)
	p := a.pane(rows, h)

	switch {
	case a.l.sel < 0:
	case a.l.sel < p.n: // the heading is the strip, always in view
		l := list{sel: a.l.sel - 1, top: a.ptop}
		l.snap(p.h)
		a.ptop = max(l.top, 0)

	default:
		l := list{sel: a.l.sel - p.n, top: a.l.top}
		l.snap(h - p.lines)
		a.l.top = l.top

		for a.l.top > 0 && a.l.top == a.l.sel-p.n { // not under the sticky heading either
			if a.sticky(m, rows) < 0 {
				break
			}

			a.l.top--
		}
	}
}

// bar is the scrollbar beside line y of Spaces: the Pinned pane's or the tree's.
func (a *agents) bar(m *Model, y int) (listBar, bool) {
	h := m.bodyH(viewAgents)
	geo := func() (pinPane, int) {
		rows := a.rows(m)

		return a.pane(rows, h), len(rows)
	}

	p, _ := geo()
	b := listBar{id: "list:agents", row: y - p.lines, geo: func() vbar {
		p, n := geo()

		return vbar{n - p.n, h - p.lines, a.l.top}
	}, to: func(top int) tea.Cmd { a.l.top = top; return nil }}

	if y >= 1 && y < 1+p.h {
		b = listBar{id: "list:agents:pinned", row: y - 1, geo: func() vbar {
			p, _ := geo()

			return vbar{p.n - 1, p.h, a.ptop}
		}, to: func(top int) tea.Cmd { a.ptop = top; return nil }}
	}

	g := b.geo()

	return b, g.on() && b.row >= 0 && b.row < g.h
}

// pinLabel is the menu item that pins session id, or unpins it.
func (m *Model) pinLabel(id string) string {
	if slices.Contains(m.st.Pinned, id) {
		return "Unpin Session"
	}

	return "Pin Session"
}

// timeKey is the key in collapsed of a heading of no project (agTime),
// apart from every project path, which is absolute.
func timeKey(bucket string) string { return "time:" + bucket }

// foldKey is the collapsed key of a heading row.
func foldKey(r agRow) string {
	if r.kind == agTime {
		return timeKey(r.when)
	}

	return r.project
}

// collapseAll folds every heading the grouping shows: projects, or times.
func (a *agents) collapseAll(m *Model) tea.Cmd {
	var shut []string // the open ones, over the folded in the order they were listed

	for _, r := range a.rows(m) {
		if (r.kind == agProject || r.kind == agTime) && !a.collapsed[foldKey(r)] {
			shut = append(shut, foldKey(r))
		}
	}

	for _, k := range slices.Backward(shut) {
		a.fold(k, true)
	}

	return a.saveFolds()
}

// unfold opens heading k. A project sorted manually goes last among the open
// ones and is saved there: it does not go back to where it was.
func (a *agents) unfold(m *Model, k string) tea.Cmd {
	a.fold(k, false)

	i := slices.Index(m.st.Projects, k)
	if i < 0 || m.st.Settings.SpProj == "updated" {
		return a.saveFolds()
	}

	m.st.Projects = append(slices.Delete(slices.Clone(m.st.Projects), i, i+1), k)

	return tea.Batch(a.saveFolds(), do("project.move", proto.MoveParams{Path: k, To: len(m.st.Projects) - 1}))
}

// saveFolds sends the folded headings to state.json, so a restart keeps
// them shut.
func (a *agents) saveFolds() tea.Cmd {
	folded := append([]string{}, a.folded...) // not nil: null would keep the list saved before

	return do("state.set", map[string]any{"spaces_folded": folded})
}

func agLabel(r agRow) string {
	switch r.kind {
	case agProject:
		if r.project == "" {
			return "other"
		}

		return filepath.Base(r.project)

	case agWorkspace:
		return r.ws.Branch + " " + filepath.Base(r.ws.Path)
	case agTime:
		return r.when
	case agGap:
		return ""
	case agSub:
		return r.sub.Type + " · " + r.sub.Description
	}

	return r.s.Agent + " " + sessionName(r.s) + " " + r.s.Title
}

// runsAt is session row r's status grouped by Time or State, with no project
// tree to say where it runs: its branch before it; by State its project too,
// and since when in place of the state its heading says.
func (m *Model) runsAt(r agRow, status string) string {
	where := r.ws.Branch
	if where == "" {
		where = filepath.Base(r.ws.Path)
	}

	if m.st.Settings.SpGroup != "state" {
		return where + " · " + status
	}

	if r.ws.Project != "" {
		where = filepath.Base(r.ws.Project) + " · " + where
	}

	if t := shortAgo(r.s.Updated, time.Now()); t != "" && r.s.Status != "exited" {
		status = t
	}

	return where + " · " + status
}

// shortAgo is how long before now t was, in its largest whole unit: now,
// 12m, 3h, 6d, 5w; "" for no t.
func shortAgo(t, now time.Time) string {
	d := now.Sub(t)

	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}

	return strconv.Itoa(int(d/(7*24*time.Hour))) + "w"
}

// sessionGlyph is herdr's status symbol set: × blocked on a permission or a
// question, ◐ working (in the accent's blue on a shell command), ✓ done
// (idle, unseen since it finished), ○ idle, and ✕ for a process that exited
// with a code; ◌ asleep, nothing running until it is opened.
func sessionGlyph(s proto.Session) (string, color.Color) {
	switch {
	case s.Status == "asleep":
		return "◌ ", pal.ignored
	case s.Status == "blocked":
		return "× ", pal.errc
	case s.Shell:
		return "◐ ", pal.accent
	case s.Status == "running":
		return "◐ ", pal.warn
	case s.Status == "exited":
		return "✕ ", pal.ignored
	case s.Attention:
		return "✓ ", pal.attention
	}

	return "○ ", pal.ok
}

// spinStep is how long a working session's ◐ holds each quarter turn: a
// turn a second, every spinner on screen in step.
const spinStep = 250 * time.Millisecond

// spinGlyph is glyph g at moment t: ◐ turned a quarter per spinStep, any
// other glyph as it is.
func spinGlyph(g string, t time.Time) string {
	rest, ok := strings.CutPrefix(g, "◐")
	if !ok {
		return g
	}

	return []string{"◐", "◓", "◑", "◒"}[t.UnixMilli()/spinStep.Milliseconds()%4] + rest
}

// spin is glyph g of sessions ss as drawn now: ◐ turns while one of them
// works, with animations on.
func (m *Model) spin(g string, ss ...proto.Session) string {
	if !m.fx.on || !slices.ContainsFunc(ss, works) {
		return g
	}

	return spinGlyph(g, time.Now())
}

// works reports s running for real: an agent by its screen, a plain shell,
// which has no screen to read, only while a program other than the shell is
// in front; a prompt's echo or redraw reads as running for a moment too.
func works(s proto.Session) bool {
	if g, _ := sessionGlyph(s); g != "◐ " {
		return false
	}

	plain := s.Agent == "shell"

	return !plain || s.Program != "" && !slices.Contains(shells, s.Program)
}

// spinning reports a session whose ◐ turns.
func (m *Model) spinning() bool { return m.fx.on && slices.ContainsFunc(m.mainSessions(), works) }

// spinLater sends the next spinMsg while a session works, one at a time, on
// the clock's spinStep boundaries, so no quarter turn is skipped.
func (m *Model) spinLater() tea.Cmd {
	if m.spinWake || !m.spinning() {
		return nil
	}

	m.spinWake = true

	return tea.Every(spinStep, func(time.Time) tea.Msg { return spinMsg{} })
}

// sessionState names what a session's symbol says: blocked, running,
// asleep, exited, done (finished unseen) or idle.
func sessionState(s proto.Session) string {
	switch {
	case s.Status == "blocked", s.Status == "running", s.Status == "asleep", s.Status == "exited":
		return s.Status
	case s.Attention:
		return "done"
	}

	return "idle"
}

// sessionTint is the background session_highlight gives s in its symbol's
// color: waiting for an answer (×) or ended with an error (✕) in the error's,
// finished unseen (✓) in the attention's; nil for any other state. soft is
// the pulse's other shade.
func sessionTint(s proto.Session) (tint, soft color.Color) {
	switch sessionState(s) {
	case "blocked", "exited":
		return pal.blockedBg, pal.blockedSoftBg
	case "done":
		return pal.doneBg, pal.doneSoftBg
	}

	return nil, nil
}

// highlight is the background session s's row and tab get: its tint, which
// pulses while the state it came to is one nobody clicked it for yet; nil
// when session_highlight is off or s is on screen.
func (m *Model) highlight(s proto.Session) color.Color {
	tint, soft := sessionTint(s)

	switch {
	case tint == nil, m.st.Settings.SessHi == "off", m.inView(s.ID):
		return nil
	case m.pulses(s) && !m.blinkOn:
		return soft
	}

	return tint
}

// pulses reports a tinted session whose state is news: session_highlight
// pulses ("tint", or the older "blink"), and nobody clicked it since.
func (m *Model) pulses(s proto.Session) bool {
	tint, _ := sessionTint(s)

	return tint != nil && m.st.Settings.SessHi != "steady" && m.st.Settings.SessHi != "off" && m.seen[s.ID] != sessionState(s)
}

// acknowledge marks the states of sessions ids as seen: a click on one, or
// having it on screen, stops its pulse; the tint stays while the state lasts.
func (m *Model) acknowledge(ids ...string) {
	if m.seen == nil {
		m.seen = map[string]string{}
	}

	for _, id := range ids {
		if s := m.session(id); s != nil {
			m.seen[id] = sessionState(*s)
		}
	}
}

// inView reports session id shown right now, in any group on screen.
func (m *Model) inView(id string) (in bool) {
	if !m.tiled() {
		return id == m.sess && m.showsSession()
	}

	m.eachGroup(func() { in = in || id == m.sess && m.showsSession() })

	return in
}

// wantsBlink reports a session out of view whose tint pulses.
func (m *Model) wantsBlink() bool {
	return slices.ContainsFunc(m.mainSessions(), func(s proto.Session) bool { return m.pulses(s) && !m.inView(s.ID) })
}

// blink starts the blink ticker when a tint wants one and none runs; the
// ticker stops itself when nothing is left to blink.
func (m *Model) blink() tea.Cmd {
	if m.blinking || !m.wantsBlink() {
		return nil
	}

	m.blinking = true

	return blinkTick()
}

func blinkTick() tea.Cmd {
	return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return blinkMsg{} })
}

// sessionRank orders states by how much they want the user: blocked first.
func sessionRank(s proto.Session) int {
	switch {
	case s.Status == "blocked":
		return 0
	case s.Status == "running":
		return 1
	case s.Attention:
		return 2
	case s.Status == "idle":
		return 3
	case s.Status == "asleep":
		return 5
	}

	return 4
}

// groupGlyph is a project's symbol: its most demanding session, · for none.
func groupGlyph(ss []proto.Session) (string, color.Color) {
	if len(ss) == 0 {
		return "· ", pal.ignored
	}

	return sessionGlyph(slices.MinFunc(ss, func(a, b proto.Session) int { return sessionRank(a) - sessionRank(b) }))
}

func (a *agents) lines(m *Model, w, h int) []string {
	rows := a.rows(m)
	p := a.pane(rows, h)

	slot, mark, band := a.dropSlot(m, rows) // the drop mark takes rows of its own, in the tree
	n := len(rows) - p.n + mark             // the tree's, under the Pinned pane
	a.l.sel = min(a.l.sel, len(rows)-1)
	a.l.top = max(min(a.l.top, n-(h-p.lines)), 0)

	hover, mx := m.hoverRow(viewAgents), m.mouseCol(viewAgents)
	a.l.bar = m.barState("list:agents")
	every := m.mainSessions() // their tabs too, which roll up

	draw := func(i, y, rw int) string { // row i on line y
		r := rows[i]
		if r.kind == agGap { // the rule over an open heading or under bareHead, or a blank
			if h := i + 1; h < len(rows) && (rows[h].kind == agProject || rows[h].kind == agTime) && (!a.collapsed[foldKey(rows[h])] || m.query(viewAgents) != "") || i > 0 && rows[i-1].when == bareHead {
				return ruleRow(rw, nil)
			}

			return blank(rw)
		}

		bg, base := m.rowColors(viewAgents, i == a.l.sel, y == hover)

		var plus, kill []seg // the + under the pointer, or on the selection while Spaces has the keyboard; a worktree's ✕ on hover only

		pointed := y == hover || i == a.l.sel && m.focused(viewAgents)
		if pointed && r.kind != agSession { // a session's is its pin, in the gutter
			acts := a.actions(m, r, rw)
			if act, ok := hit(acts, mx); ok && y == hover && act.tip != "" {
				m.tip = act.tip
			}

			if n := len(acts); n > 0 && acts[n-1].g == icClose {
				if y == hover {
					kill = drawActions(acts[n-1:], mx)
				}

				acts = acts[:n-1]
			}

			plus = drawActions(acts, mx)
		}

		switch r.kind {
		case agTime:
			open := !a.collapsed[timeKey(r.when)]

			var ss []proto.Session

			now := time.Now()
			for _, s := range m.listedSessions() {
				in := m.headOf(s, now) == r.when
				if r.when == pinnedHead {
					in = slices.Contains(m.st.Pinned, s.ID)
				}

				if in {
					ss = append(ss, s)
				}
			}

			glyph, c := groupGlyph(ss)
			count := func(n int) []seg { return []seg{sg(strconv.Itoa(n)+" ", dim)} }

			label, right := r.when, []seg(nil)

			switch {
			case r.when == pinnedHead: // the pane's strip
				label, right = strings.ToUpper(r.when), count(len(ss))
			case r.when == bareHead: // a strip as Pinned's, not one more project
				busy := m.busyProjects()
				label, right = strings.ToUpper(r.when), count(len(slices.DeleteFunc(m.projects(), func(p string) bool { return busy[p] })))

			case m.st.Settings.SpGroup == "state":
				right = count(len(ss))
			}

			return row(rw, bg, []seg{sg(" "+chevron(open), dim), sg(m.spin(glyph, ss...), fg(c)), sg(label, base.Bold(true))}, right...)

		case agProject:
			open := !a.collapsed[r.project]

			var ss []proto.Session

			for _, s := range every {
				if w := m.workspace(s.Workspace); (w != nil && w.Project == r.project) || (w == nil && r.project == "") {
					ss = append(ss, s)
				}
			}

			glyph, c := groupGlyph(ss)

			if bg == nil && !open { // folded, its sessions cannot show their own
				for _, s := range ss {
					if hl := m.highlight(s); hl != nil {
						bg = hl
						break
					}
				}
			}

			left := []seg{sg(" "+chevron(open), dim), sg(m.spin(glyph, ss...), fg(c)), sg(agLabel(r), base.Bold(true))}
			if r.merged() {
				left = append(append(left, sg("  ", plain)), m.wsSegs(r.ws, base)...)
			}

			return row(rw, bg, append(left, plus...))

		case agWorkspace:
			left := append([]seg{sg("   ", plain)}, m.wsSegs(r.ws, base)...)

			right := wsRight(m, r, rw)
			if kill != nil {
				right = strings.TrimSuffix(right, " ")
			}

			return row(rw, bg, append(left, plus...), append([]seg{sg(right, dim)}, kill...)...)

		case agSub: // under its session's glyph, on the tree's guide while sessions follow
			guide := "   "
			if moreSessions(rows, i) {
				guide = "│  "
			}

			if label := agLabel(r); y == hover && 8+ansi.StringWidth(label) > rw {
				m.tip = label
			}

			return row(rw, bg, []seg{sg("   "+guide+"↳ ", dim), sg(r.sub.Type, base), sg(" · "+r.sub.Description, dim)})
		}
		// herdr's tree: sessions hang off their branch, the last one on └─.
		conn := "├─ "
		if !moreSessions(rows, i) {
			conn = "└─ "
		}
		// The gutter before it holds the pin: lit on a pinned session, dim
		// on the row under the pointer (not the selection), raised with its
		// tip under the pointer. The row under the pointer ends in its ✕.
		acts := a.actions(m, r, rw)
		pin := acts[0]
		_, onPin := hit([]rowAction{pin}, mx)
		onPin = onPin && y == hover
		_, onKill := hit(acts[1:], mx)
		onKill = onKill && y == hover

		gutter := sg("   ", dim)

		switch {
		case onPin:
			gutter, m.tip = sgOwn(pin.label(), keycapHot()), pin.tip
		case pin.on:
			gutter = sg(pin.label(), fg(pal.headerAccent))
		case y == hover:
			gutter = sg(pin.label(), dim)
		}

		glyph, c := groupGlyph([]proto.Session{r.s})

		// The session on screen: the accent's bar in the gutter's first cell
		// and its name in the accent, as VS Code marks the active item. One
		// that needs you says so in its status, in its symbol's color, bold
		// on the pulse's beat; no row is tinted, so none reads as selected.
		nameSt, lead := base, []seg{gutter}
		if m.sess == r.s.ID {
			nameSt = nameSt.Foreground(pal.headerAccent).Bold(true)
			gutter.s = strings.TrimPrefix(gutter.s, " ") // the pin's own blank before it
			lead = []seg{sg("▎", fg(pal.headerAccent)), gutter}
		}

		status, statusSt := r.s.Status, dim
		switch state := sessionState(r.s); {
		case state == "blocked":
			status = "input"
		case state == "done":
			status = "done"
		case r.s.Status == "exited":
			status = fmt.Sprintf("exit %d", r.s.ExitCode)
		case r.s.Shell:
			status = "shell"
		case state == "idle" && !r.s.Updated.IsZero(): // its glyph says idle: since when
			status = shortAgo(r.s.Updated, time.Now())
		}

		if tint, _ := sessionTint(r.s); tint != nil && m.st.Settings.SessHi != "off" {
			statusSt = fg(c).Bold(m.pulses(r.s) && m.blinkOn)
		}

		if !m.byWorkspace() && !r.pinned { // no project tree to say where it runs
			status = m.runsAt(r, status)
		}

		// A name the row cuts, at 32 cells or at the column's edge, shows
		// whole in a tip.
		g, after, right := m.spin(glyph, r.s), titleAfter(r.s), " "+status+" "

		if y == hover {
			right, kill = " "+status, drawActions(acts[1:], mx)
		}

		if onKill {
			m.tip = acts[1].tip
		}

		if full := fullName(r.s); y == hover && !onPin && !onKill && (full != sessionName(r.s) || 3+ansi.StringWidth(conn+g+full+after+right) > rw) {
			m.tip = full + after
		}

		left := append(append(lead, sg(conn, dim), sg(g, fg(c))), m.fx.segs("sess:"+r.s.ID, sessionName(r.s), nameSt)...)

		return row(rw, bg, append(left, sg(after, nameSt)), append([]seg{sg(right, statusSt)}, kill...)...)
	}

	var out []string

	if p.n > 0 {
		out = append(out, draw(0, 0, w))

		if p.h > 0 {
			body := list{top: a.ptop, bar: m.barState("list:agents:pinned")}
			body.clamp(p.n-1, p.h)
			a.ptop = body.top

			out = append(out, body.render(w, p.h, p.n-1, func(i, rw int) string { return draw(1+i, 1+i-body.top, rw) })...)
		}

		out = append(out, ruleRow(w, nil, fg(pal.tabBorder))) // its border: heavy, a shade over the tree's rules
	}

	if slot >= 0 {
		slot -= p.n
	}

	stick := a.sticky(m, rows)

	return append(out, a.l.render(w, h-p.lines, n, func(i, rw int) string {
		y := p.lines + i - a.l.top

		switch {
		case i == a.l.top && stick >= 0:
			return draw(stick, y, rw)
		case slot < 0 || i < slot:
			return draw(p.n+i, y, rw)
		case i == slot+band: // a band in the selection's tint, thick enough to see while dragging
			return row(rw, pal.selBg, nil)
		case i < slot+mark:
			return blank(rw)
		}

		return draw(p.n+i-mark, y, rw)
	})...)
}

// reveal selects workspace path's row, its project unfolded (and No Sessions
// over it), as a click on it would: whatever switches the workspace shows
// where it went. A selection already inside that workspace, a clicked
// session, stays.
func (a *agents) reveal(m *Model, path string) tea.Cmd {
	if r := a.selected(m); r != nil && r.kind != agProject && r.ws.Path == path {
		return nil
	}

	var saved tea.Cmd

	if w := m.workspace(path); w != nil && a.collapsed[w.Project] {
		saved = a.unfold(m, w.Project)
	}

	if w := m.workspace(path); w != nil && !m.busyProjects()[w.Project] && a.collapsed[timeKey(bareHead)] {
		saved = tea.Batch(saved, a.unfold(m, timeKey(bareHead)))
	}

	rows := a.rows(m)
	if i := slices.IndexFunc(rows, func(r agRow) bool { return r.shows(path) }); i >= 0 {
		a.l.sel = i
		a.snap(m)
	}

	return saved
}

// selected is the highlighted row, nil when nothing is selected.
func (a *agents) selected(m *Model) *agRow {
	if rows := a.rows(m); a.l.sel >= 0 && a.l.sel < len(rows) && !rows[a.l.sel].inert() {
		return &rows[a.l.sel]
	}

	return nil
}

// follow keeps the selection on row r, the one selected before the rows
// changed: sorted by Updated, a session that changes moves up the list, and
// the highlight goes with it rather than onto whatever took its place.
func (a *agents) follow(m *Model, r *agRow) {
	if r == nil {
		return
	}

	same := func(o agRow) bool { // a session is itself under any heading, as Group by Time moves it
		if r.kind == agSession {
			return o.kind == agSession && o.s.ID == r.s.ID && o.pinned == r.pinned
		}

		return o.kind == r.kind && o.project == r.project && (o.ws.Path == r.ws.Path || r.kind == agProject) && o.when == r.when && o.pinned == r.pinned // a heading holds its checkout only open
	}

	rows := a.rows(m)

	i := slices.IndexFunc(rows, same)
	if i < 0 && r.pinned { // unpinned: onto the tree's copy
		tree := *r
		tree.pinned, r = false, &tree
		i = slices.IndexFunc(rows, same)
	}

	if i >= 0 {
		a.l.sel = i
	}
}

// step moves the selection by d rows and walks on over the blank separators
// one row at a time, so ↑↓ and a page never park on one. A walk that meets the
// end of the list turns back: with every project folded, the padding that
// pins them down opens the list.
func (a *agents) step(m *Model, rows []agRow, d int) {
	n, s := len(rows), min(max(d, -1), 1)
	if n == 0 {
		return
	}

	if a.l.sel < 0 { // none selected yet: the first
		a.l.sel = 0
	} else {
		a.l.sel = max(0, min(n-1, a.l.sel+d))
	}

	for a.l.sel < n && rows[a.l.sel].inert() {
		if (s < 0 && a.l.sel == 0) || (s > 0 && a.l.sel == n-1) {
			s = -s
		}

		a.l.sel = max(0, min(n-1, a.l.sel+s))
	}

	a.snap(m) // in the Pinned pane or the tree, wherever it landed
}

func (a *agents) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := a.rows(m)
	h, n := m.bodyH(viewAgents), len(rows)

	var r *agRow
	if a.l.sel >= 0 && a.l.sel < n && !rows[a.l.sel].inert() {
		r = &rows[a.l.sel]
	}

	switch k.String() {
	case "up", "k":
		a.step(m, rows, -1)
	case "down", "j":
		a.step(m, rows, 1)
	case "pgup":
		a.step(m, rows, -h)
	case "pgdown":
		a.step(m, rows, h)
	case "alt+up", "alt+down":
		d := map[bool]int{true: -1, false: 1}[k.String() == "alt+up"]

		if r != nil && r.pinned { // Pinned keeps the order they were pinned in
			return nil
		}

		if r != nil && r.kind == agSession {
			return a.shiftSession(m, r.s.ID, d)
		}

		if r != nil && r.kind == agWorkspace {
			return a.shiftWorkspace(m, r.ws.Path, d)
		}

		if r != nil && r.project != "" {
			return a.shiftProject(m, r.project, d)
		}

	case "g", "home":
		a.step(m, rows, -n)
	case "G", "end":
		a.step(m, rows, n)
	case "enter", "space", "l":
		if r != nil && r.merged() && k.String() == "enter" { // its checkout's row's ⏎; space and l fold
			return a.activate(m, &agRow{kind: agWorkspace, project: r.project, ws: r.ws})
		}

		return a.activate(m, r)

	case "n":
		return a.newSession(m, r)
	case "w":
		return a.newWorktree(m, r)
	case "a":
		return m.addProjectPrompt()
	case "R":
		if r != nil && r.kind == agSession {
			return m.renameSession(r.s.ID)
		}

	case "p":
		if r != nil && r.kind == agSession {
			return m.togglePin(r.s.ID)
		}

	case "x", "d", "delete":
		return a.remove(m, r)
	case "z":
		if r != nil && r.kind == agSession {
			return m.sleepSession(r.s.ID)
		}

	case "r":
		return tea.Batch(loadState(), loadWorkspaces(), loadSessions())
	case "m":
		return a.menu(m, 0, h/2)
	case "o":
		return a.viewMenu(m, 0, h/2)
	}

	return nil
}

func (a *agents) activate(m *Model, r *agRow) tea.Cmd {
	if r == nil {
		return nil
	}

	switch r.kind {
	case agProject, agTime:
		var saved tea.Cmd

		if k := foldKey(*r); a.collapsed[k] {
			saved = a.unfold(m, k)
		} else {
			a.fold(k, true)
			saved = a.saveFolds()
		}

		a.follow(m, r) // a folded project sinks below the open ones, an unfolded one goes last among them
		a.snap(m)

		return saved

	case agWorkspace:
		return tea.Batch(m.switchWorkspace(r.ws.Path), m.refreshGit(), m.fetchScreen())
	case agSession: // the one on screen too: it keeps its tab and takes the keyboard, as VS Code's open file does
		return m.openSession(r.s.ID)
	}

	return nil
}

// workspaceFor picks the workspace a new session from row r goes into: a
// worktree's or a session's own, a project heading's that holds its
// checkout, else the one in view.
func (m *Model) workspaceFor(r *agRow) string {
	if r == nil || r.kind != agWorkspace && r.kind != agSession && !r.merged() {
		return m.ws
	}

	return r.ws.Path
}

// newSession is n and a row's +: a project asks which of its worktrees, or a
// new one, the session goes into; any other row, and a project heading that
// holds its checkout, asks only for the harness.
func (a *agents) newSession(m *Model, r *agRow) tea.Cmd {
	if r != nil && r.kind == agProject && r.project != "" && !r.merged() {
		return m.worktreePicker(r.project, false)
	}

	return m.harnessPicker(m.workspaceFor(r))
}

// resume is the header's Resume: in row r's worktree, as newSession's; a
// project asks which of its worktrees.
func (a *agents) resume(m *Model, r *agRow) tea.Cmd {
	if r != nil && r.kind == agProject && r.project != "" && !r.merged() {
		return m.worktreePicker(r.project, true)
	}

	return m.resumePicker(m.workspaceFor(r))
}

// worktreePicker asks which of project's worktrees a new session goes into,
// or a new one, then which harness it runs. With resume it asks where
// Resume runs (resumePicker), offering no new one: it would have nothing to
// resume.
func (m *Model) worktreePicker(project string, resume bool) tea.Cmd {
	title, then := "New session in ", (*Model).harnessPicker
	items := []item{{label: "New Worktree…", run: func(m *Model) tea.Cmd {
		m.promptWorktree(project, git.RandomBranch(), true)

		return nil
	}}}

	if resume {
		title, then, items = "Resume in ", (*Model).resumePicker, nil
	}

	for _, w := range m.wss {
		if w.Project != project {
			continue
		}

		path := w.Path
		items = append(items, item{
			label: wtGlyph(w).s() + " " + wsName(w), hint: plural(m.sessionCount(path), "session"),
			run: func(m *Model) tea.Cmd { return then(m, path) },
		})
	}

	m.modal = newPicker(title+filepath.Base(project), items)

	return nil
}

// harnessPicker asks what a new session in ws runs: an installed agent, or
// a terminal, the shell a tab opens. The session ends when it exits. An
// agent with profiles asks for the profile next.
func (m *Model) harnessPicker(ws string) tea.Cmd {
	var items []item

	for _, name := range m.harnesses() {
		items = append(items, item{label: name, hint: strings.Join(m.st.Agents[name], " "), run: func(m *Model) tea.Cmd {
			if m.profilePicker(ws, name, nil) {
				return nil
			}

			return m.newSession(ws, name, nil)
		}})
	}

	items = append(items, item{label: "terminal", hint: cmp.Or(m.st.Settings.Shell, strings.Join(m.st.Agents["shell"], " ")), run: func(m *Model) tea.Cmd {
		return m.newSession(ws, "shell", nil) // the daemon picks the shell
	}})

	m.modal = newPicker("New session in "+filepath.Base(ws), items)

	return nil
}

// resumePicker is a worktree's Resume: a session in ws runs an installed
// agent's resumeCmd. It asks which agent when more than one can, then, as a
// new session does, the profile, whose config holds the conversations.
func (m *Model) resumePicker(ws string) tea.Cmd {
	var items []item

	for _, name := range m.harnesses() {
		cmd := m.resumeCmd(name)
		if cmd == nil {
			continue
		}

		items = append(items, item{label: name, hint: strings.Join(cmd, " "), run: func(m *Model) tea.Cmd {
			if m.profilePicker(ws, name, cmd) {
				return nil
			}

			return m.newSession(ws, name, cmd)
		}})
	}

	switch len(items) {
	case 0:
		return flash("no installed agent can resume", true)
	case 1:
		return items[0].run(m)
	}

	m.modal = newPicker("Resume in "+filepath.Base(ws), items)

	return nil
}

// resumeCmd is what Resume runs for agent: its [resume_id] without the id,
// claude's picker of the directory's conversations, else its [resume], which
// continues the last one. Nil when it has neither.
func (m *Model) resumeCmd(agent string) []string {
	if t := m.st.ResumeID[agent]; len(t) > 0 {
		return slices.DeleteFunc(slices.Clone(t), func(a string) bool { return a == "{id}" })
	}

	return m.st.Resume[agent]
}

// profilePicker asks which profile a new session of agent in ws runs with:
// its default, which sets nothing, its [profiles], then each config pando saw
// it run in that none of them sets. The session runs cmd, nil for the agent's
// preset. It reports whether there was a choice.
func (m *Model) profilePicker(ws, agent string, cmd []string) bool {
	run := func(env []string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.spawn(ws, agent, "", cmd, "", env) }
	}

	items := []item{{label: "default", hint: cmp.Or(strings.Join(cmd, " "), strings.Join(m.st.Agents[agent], " ")), run: run(nil)}}

	var set [][]string

	profiles := m.st.Settings.Profiles[agent]
	for _, name := range slices.Sorted(maps.Keys(profiles)) {
		env := homeEnv(profiles[name])
		set = append(set, env)
		items = append(items, item{label: name, hint: strings.Join(profiles[name], " "), run: run(env)})
	}

	for _, env := range m.st.Seen[agent] {
		if !slices.ContainsFunc(set, func(e []string) bool { return slices.Equal(e, env) }) {
			items = append(items, item{label: strings.Join(env, " "), hint: "seen", run: run(env)})
		}
	}

	if len(items) == 1 {
		return false
	}

	m.modal = newPicker(agent+" profile", items)

	return true
}

// homeEnv is KEY=VALUE env with a leading ~ in each value expanded, as a
// shell does: no shell runs between a profile and the agent.
func homeEnv(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[i] = k + "=" + expandHome(v)
	}

	return out
}

// harnesses are the [agents] presets a session can run: the installed ones,
// the shells of tabs and the Terminal panel aside. None installed, none.
func (m *Model) harnesses() []string {
	var out []string

	for _, name := range slices.Sorted(maps.Keys(m.st.Agents)) {
		argv := m.st.Agents[name]
		if len(argv) == 0 || slices.Contains([]string{"shell", termAgent}, name) {
			continue
		}

		if _, err := exec.LookPath(argv[0]); err == nil {
			out = append(out, name)
		}
	}

	return out
}

// sessionCount is how many sessions worktree path holds, their tabs aside.
func (m *Model) sessionCount(path string) int {
	n := 0
	for _, s := range m.mainSessions() {
		n += b2i(s.Workspace == path)
	}

	return n
}

func (a *agents) newWorktree(m *Model, r *agRow) tea.Cmd {
	project := ""
	if r != nil {
		project = r.project
	}

	if project == "" {
		if w := m.workspace(m.ws); w != nil {
			project = w.Project
		}
	}

	if project == "" {
		return flash("select a project first", true)
	}

	m.promptWorktree(project, git.RandomBranch(), false)

	return nil
}

// promptWorktree asks for the branch of a new worktree of project, offering
// branch; ⏎ creates it and switches there, then with session asks for the
// harness of a session in it.
func (m *Model) promptWorktree(project, branch string, session bool) {
	m.modal = newPrompt("New worktree branch in "+filepath.Base(project), branch, func(_ *Model, branch string) tea.Cmd {
		return func() tea.Msg { // an emptied name gets a random one from the daemon
			var w proto.Workspace
			if err := proto.Call("workspace.new", map[string]string{"project": project, "branch": branch}, &w); err != nil {
				return flashMsg{err.Error(), true}
			}

			if session {
				return sessionWorktreeMsg(w)
			}

			return newWorkspaceMsg(w)
		}
	})
}

type newWorkspaceMsg proto.Workspace

// sessionWorktreeMsg is a worktree made for a new session: the harness
// picker follows it.
type sessionWorktreeMsg proto.Workspace

func (a *agents) remove(m *Model, r *agRow) tea.Cmd {
	if r == nil {
		return nil
	}

	var (
		title, label, method string
		params               any
		kill                 []string
	)

	switch {
	case r.kind == agSession:
		title, label = "Kill "+sessionName(r.s)+" session?", "Kill Session"
		method, params = "session.kill", map[string]string{"id": r.s.ID}

	case r.kind == agWorkspace && !r.ws.Main:
		kill = m.sessionsIn(r.ws.Path)
		title = fmt.Sprintf("Delete worktree %s? The checkout %s is removed, the branch stays.%s",
			cmp.Or(r.ws.Branch, filepath.Base(r.ws.Path)), r.ws.Path, killedText(len(kill)))
		label, method, params = "Delete Worktree", "workspace.remove", map[string]string{"path": r.ws.Path}

	case r.project != "": // the project row, or its own checkout: never deleted, only closed
		var paths []string

		for _, w := range m.wss {
			if w.Project == r.project {
				paths = append(paths, w.Path)
			}
		}

		kill = m.sessionsIn(paths...)
		title = fmt.Sprintf("Close project %s? It leaves Spaces, nothing on disk changes.%s",
			filepath.Base(r.project), killedText(len(kill)))
		label, method, params = "Close Project", "project.remove", map[string]string{"path": r.project}

	default:
		return nil
	}

	names := map[string]string{}

	switch r.kind {
	case agSession:
		names["sess:"+r.s.ID] = sessionName(r.s)
	case agWorkspace:
		names["ws:"+r.ws.Path] = wsName(r.ws)
	}

	for _, id := range kill {
		if s := m.session(id); s != nil {
			names["sess:"+id] = sessionName(*s)
		}
	}

	m.modal = newDialog(title,
		item{label: label, run: func(m *Model) tea.Cmd { return m.dissolve(names, killThen(kill, method, params)) }},
		cancelItem())

	return nil
}

// dissolve runs cmd once names, the keys and text of what it removes, have
// dissolved into noise on screen. The names stay blank a while after, until
// the daemon's list drops their rows.
func (m *Model) dissolve(names map[string]string, cmd tea.Cmd) tea.Cmd {
	if !m.fx.on || len(names) == 0 {
		return cmd
	}

	const hold = 30 // frames

	d := 0

	for key, name := range names {
		a := anim{kind: fxMorph, from: name, dur: morphDur(name, "")}
		d = max(d, a.dur)
		a.dur += hold
		m.fx.run(key, a)
	}

	// ponytail: quitting pando within the dissolve leaves what it removes in
	// place; run these on quit if that bites.
	return func() tea.Msg {
		time.Sleep(time.Duration(d) * fxFrame)
		return cmd()
	}
}

// wsSegs are worktree w's glyph and name on a row whose text is base: the
// project's own checkout, which only closes, in the accent and plain text; a
// linked worktree, which can be deleted, dimmed; the one in view in the
// header's accent.
func (m *Model) wsSegs(w proto.Workspace, base lipgloss.Style) []seg {
	glyphSt, nameSt := dim, dim
	if w.Main {
		glyphSt, nameSt = fg(pal.accent), base
	}

	if w.Path == m.ws {
		nameSt = base.Foreground(pal.headerAccent).Bold(true)
	}

	return append([]seg{sg(wtGlyph(w).s()+" ", glyphSt)}, m.fx.segs("ws:"+w.Path, wsName(w), nameSt)...)
}

// wtGlyph marks a project's own checkout apart from its linked worktrees.
func wtGlyph(w proto.Workspace) glyph {
	if w.Main {
		return icMainWt
	}

	return icLinkedWt
}

// removeLabel names what x does to row r: a session is killed, a linked
// worktree deleted, a project and its own checkout only closed.
func removeLabel(r *agRow) string {
	switch {
	case r == nil:
		return ""
	case r.kind == agSession:
		return "Kill Session…"
	case r.kind == agWorkspace && !r.ws.Main:
		return "Delete Worktree…"
	case r.project != "":
		return "Close Project…"
	}

	return ""
}

// wsRight is worktree row r's right end, rw cells wide: its session count,
// or on a pinned row its project, where the branch leaves room: a branch is
// in more than one.
func wsRight(m *Model, r agRow, rw int) string {
	if r.pinned {
		if p := filepath.Base(r.project) + " "; r.project != "" && 6+ansi.StringWidth(wsName(r.ws)+p) <= rw {
			return p
		}

		return ""
	}

	if n := m.sessionCount(r.ws.Path); n > 0 {
		return m.fx.text("wsn:"+r.ws.Path, strconv.Itoa(n)) + " "
	}

	return ""
}

// actions are row r's hover buttons, right after its name: a project's New
// Worktree (then its harness), a worktree's New Session and Resume, a
// project heading holding its checkout all three, as glyphs with a tip; a
// session's are its pin, in the gutter before it, and its ✕ at the row's
// right end, x's kill, as a linked worktree's ✕ is x's delete, last after
// its session count. On a row w cells wide a
// button that would run into the row's right end is left out, whole.
func (a *agents) actions(m *Model, r agRow, w int) []rowAction {
	if r.kind == agSession {
		pin := rowAction{g: icPin, tip: m.pinLabel(r.s.ID) + "  p", on: slices.Contains(m.st.Pinned, r.s.ID), w: 3, run: func(m *Model) tea.Cmd { return m.togglePin(r.s.ID) }}
		if pin.on {
			pin.g = icPinned
		}

		return []rowAction{pin, a.killAction(r, w)}
	}

	head := " " + chevron(!a.collapsed[r.project]) + "· " + agLabel(r) // any status glyph is a cell and a space

	acts := []rowAction{{g: icAdd, text: "New Session", run: func(m *Model) tea.Cmd { return a.newSession(m, &r) }}}
	resume := rowAction{g: icResume, text: "Resume", run: func(m *Model) tea.Cmd { return m.resumePicker(r.ws.Path) }}
	worktree := rowAction{g: icWorktree, text: "New Worktree", run: func(m *Model) tea.Cmd {
		m.promptWorktree(r.project, git.RandomBranch(), true)
		return nil
	}}

	switch {
	case r.kind == agWorkspace:
		head = "   " + wtGlyph(r.ws).s() + " " + wsName(r.ws)

		acts = append(acts, resume)

	case r.merged(): // its checkout's, then its own, glyphs alone
		head += "  " + wtGlyph(r.ws).s() + " " + wsName(r.ws)

		acts = append(acts, resume, worktree)
		for i, tip := range []string{"New Session  n", "Resume Session", "New Worktree  w"} {
			acts[i].text, acts[i].tip = "", tip
		}

	case r.kind == agProject && r.project != "" && !a.collapsed[r.project]: // folded, it shows nothing
		acts[0] = worktree

	default:
		return nil
	}

	var kill []rowAction

	if r.kind == agWorkspace {
		right := wsRight(m, r, w)
		if !r.ws.Main {
			kill = []rowAction{a.killAction(r, w)}
			right = strings.TrimSuffix(right, " ")
			w -= kill[0].w
		}

		w -= ansi.StringWidth(right)
	}

	x := ansi.StringWidth(head)
	for i := range acts {
		acts[i].x, acts[i].w = x, ansi.StringWidth(acts[i].label())
		if x += acts[i].w; x > w {
			return append(acts[:i], kill...)
		}
	}

	return append(acts, kill...)
}

// killAction is row r's ✕ at the right end of a row w cells wide: x on it.
func (a *agents) killAction(r agRow, w int) rowAction {
	kill := rowAction{g: icClose, tip: removeLabel(&r) + "  x", run: func(m *Model) tea.Cmd { return a.remove(m, &r) }}
	kill.w = ansi.StringWidth(kill.label())
	kill.x = w - kill.w

	return kill
}

// sessionsIn are the sessions to kill to empty workspaces paths. A Terminal
// panel's shell whose session is killed too is left out, it goes with it; one
// whose session lives in another workspace is killed on its own.
func (m *Model) sessionsIn(paths ...string) []string {
	in := func(s proto.Session) bool { return slices.Contains(paths, s.Workspace) }

	var ids []string

	for _, s := range m.sessions {
		if p := m.session(s.Parent); in(s) && (p == nil || !in(*p)) {
			ids = append(ids, s.ID)
		}
	}

	return ids
}

func killedText(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " Its session is killed."
	}

	return fmt.Sprintf(" Its %d sessions are killed.", n)
}

// killThen kills sessions ids, then calls method: the daemon removes no
// workspace or project that still has a session in it.
func killThen(ids []string, method string, params any) tea.Cmd {
	return func() tea.Msg {
		for _, id := range ids {
			if err := proto.Call("session.kill", map[string]string{"id": id}, nil); err != nil {
				return flashMsg{"session.kill: " + err.Error(), true}
			}
		}

		return do(method, params)()
	}
}

// confirmKill asks before killing a session, as the Agents view does.
func (m *Model) confirmKill(id string) tea.Cmd {
	s := m.session(id)
	if s == nil {
		return nil
	}

	return m.ag.remove(m, &agRow{kind: agSession, s: *s})
}

// items are the Agents commands for the selected row.
func (a *agents) items(m *Model) []item {
	r := a.selected(m)

	create := []item{
		{label: "New Session…", hint: "n", run: func(m *Model) tea.Cmd { return a.newSession(m, r) }},
		{label: "New Worktree…", hint: "w", run: func(m *Model) tea.Cmd { return a.newWorktree(m, r) }},
		{label: "Add Project…", hint: "a", run: func(m *Model) tea.Cmd { return a.key(m, tea.KeyPressMsg{Code: 'a', Text: "a"}) }},
		{label: "Open Project…", run: func(m *Model) tea.Cmd { return m.projectPicker() }},
	}
	goTo := []item{
		{label: "Go to Agent or Worktree…", hint: "M-t", run: func(m *Model) tea.Cmd { return m.agentNavigator() }},
		{label: "View Options…", hint: "o", run: func(m *Model) tea.Cmd { return a.viewMenu(m, m.mouseX, m.mouseY) }},
	}

	if r != nil && (r.kind == agWorkspace || r.kind == agSession || r.merged()) { // a worktree to resume in
		ws := r.ws.Path
		create = slices.Insert(create, 1, item{label: "Resume Session…", run: func(m *Model) tea.Cmd { return m.resumePicker(ws) }})
	}

	var row, remove []item // what the selected row can do, and its end

	if r != nil && r.kind == agWorkspace && !r.pinned && len(m.worktreesOf(r.project)) > 1 {
		path := r.ws.Path

		row = append(row,
			item{label: "Move Worktree Up", hint: "M-↑", run: func(m *Model) tea.Cmd { return a.shiftWorkspace(m, path, -1) }},
			item{label: "Move Worktree Down", hint: "M-↓", run: func(m *Model) tea.Cmd { return a.shiftWorkspace(m, path, 1) }})
	}

	if r != nil && r.project != "" && r.kind == agProject && len(m.st.Projects) > 1 && a.projectMovable(m, r.project) {
		// What a drag does, for the keyboard: the project keeps its place in
		// config, so the order survives a restart either way.
		row = append(row,
			item{label: "Move Project Up", hint: "M-↑", run: func(m *Model) tea.Cmd { return a.shiftProject(m, r.project, -1) }},
			item{label: "Move Project Down", hint: "M-↓", run: func(m *Model) tea.Cmd { return a.shiftProject(m, r.project, 1) }})
	}

	if r != nil && r.kind == agSession {
		id := r.s.ID

		row = append(row,
			item{label: "Rename Session…", hint: "R", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
			item{label: m.pinLabel(id), hint: "p", run: func(m *Model) tea.Cmd { return m.togglePin(id) }})
		if canSleep(r.s) {
			row = append(row, item{label: "Sleep Session", hint: "z", run: func(m *Model) tea.Cmd { return m.sleepSession(id) }})
		}

		if !r.pinned && m.sessionsMovable() && len(m.siblings(id)) > 1 {
			row = append(row,
				item{label: "Move Session Up", hint: "M-↑", run: func(m *Model) tea.Cmd { return a.shiftSession(m, id, -1) }},
				item{label: "Move Session Down", hint: "M-↓", run: func(m *Model) tea.Cmd { return a.shiftSession(m, id, 1) }})
		}
	}

	if label := removeLabel(r); label != "" {
		remove = []item{{label: label, hint: "x", run: func(m *Model) tea.Cmd { return a.remove(m, r) }}}
	}

	return menuGroups(create, row, goTo, remove)
}

func (a *agents) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(a.items(m), x, y) }

// sessionStates are the states the Filter can leave out, in sessionRank's order.
var sessionStates = []struct{ state, label string }{
	{"blocked", "Blocked"}, {"running", "Working"}, {"done", "Done"}, {"idle", "Idle"}, {"exited", "Exited"}, {"asleep", "Asleep"},
}

// checked is a menu label with VS Code's check column: ✓ when on.
func checked(on bool, label string) string {
	if on {
		return "✓ " + label
	}

	return "  " + label
}

// viewMenu is VS Code's agent sessions view menu: Filter, Sort, Group, Collapse
// All Groups, and the projects' Sort. Sort and Group are settings, so every
// window lists the same way.
func (a *agents) viewMenu(m *Model, x, y int) tea.Cmd {
	st := m.st.Settings
	set := func(key, v string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.setSettings(map[string]any{key: v}) }
	}

	filter := "  Filter"
	if len(st.SpHide) > 0 {
		filter += fmt.Sprintf(" (%d hidden)", len(st.SpHide))
	}

	return m.menuOf([]item{
		{label: filter, hint: "›", run: func(m *Model) tea.Cmd { return a.filterMenu(m, x, y) }},
		{},
		{label: checked(st.SpSort != "updated", "Sort by Created"), run: set("spaces_sort", "created")},
		{label: checked(st.SpSort == "updated", "Sort by Updated"), run: set("spaces_sort", "updated")},
		{},
		{label: checked(st.SpProj != "updated", "Sort Projects Manually"), run: set("spaces_project_sort", "manual")},
		{label: checked(st.SpProj == "updated", "Sort Projects by Updated"), run: set("spaces_project_sort", "updated")},
		{},
		{label: checked(m.byWorkspace(), "Group by Workspace"), run: set("spaces_group", "workspace")},
		{label: checked(st.SpGroup == "time", "Group by Time"), run: set("spaces_group", "time")},
		{label: checked(st.SpGroup == "state", "Group by State"), run: set("spaces_group", "state")},
		{},
		{label: "  Collapse All Groups", run: a.collapseAll},
	}, x, y)
}

// filterMenu toggles which session states Spaces shows; it stays open, as
// VS Code's submenu does, until esc.
func (a *agents) filterMenu(m *Model, x, y int) tea.Cmd {
	build := func(m *Model) []item {
		var items []item

		for _, s := range sessionStates {
			hidden := slices.Contains(m.st.Settings.SpHide, s.state)
			items = append(items, item{label: checked(!hidden, s.label), run: func(m *Model) tea.Cmd {
				hide := slices.DeleteFunc(slices.Clone(m.st.Settings.SpHide), func(h string) bool { return h == s.state })
				if !hidden {
					hide = append(hide, s.state)
				}

				return m.setSettings(map[string]any{"spaces_hide": hide})
			}})
		}

		return append(items, item{}, item{label: "  Show All", run: func(m *Model) tea.Cmd {
			return m.setSettings(map[string]any{"spaces_hide": []string{}})
		}})
	}

	md := newMenu("Filter", x, y, build(m)...)
	md.build, md.keep = build, true
	m.modal = md

	return nil
}

func (a *agents) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()
	rows := a.rows(m)
	h := m.bodyH(viewAgents)

	p := a.pane(rows, h)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		if y >= 1 && y < 1+p.h { // over the pinned sessions
			l := list{top: a.ptop}
			l.wheel(wheelDelta(mo), p.n-1, p.h)
			a.ptop = l.top
		} else {
			a.l.wheel(wheelDelta(mo), len(rows)-p.n, h-p.lines)
		}

	case tea.MouseClickMsg:
		i := a.rowAt(m, rows, y)
		for i > 0 && rows[i].kind == agSub { // a subagent's row is its session's
			i--
		}

		if i < 0 || rows[i].kind == agGap {
			return nil
		}

		a.l.sel = i
		if mo.Button == tea.MouseRight {
			return a.menu(m, mo.X, mo.Y)
		}

		w := m.bodyW(viewAgents)
		if y >= 1 && y < 1+p.h && p.n-1 > p.h || y >= p.lines && len(rows)-p.n > h-p.lines {
			w-- // the Pinned pane's or the tree's scrollbar takes the last column, as lines draws it
		}

		if act, ok := hit(a.actions(m, rows[i], w), x); ok && mo.Button == tea.MouseLeft {
			return act.run(m)
		}

		if r := rows[i]; mo.Button == tea.MouseLeft && a.onBranch(r, x) { // the checkout's own row's click
			return a.activate(m, &agRow{kind: agWorkspace, project: r.project, ws: r.ws})
		}
		// A project row drags up and down the list; a press that never leaves
		// its row is the click that folds it, so the fold waits for the release.
		if p := rows[i].project; mo.Button == tea.MouseLeft && rows[i].kind == agProject && p != "" && len(m.st.Projects) > 1 && a.projectMovable(m, p) {
			m.drag = &drag{kind: dragRow, proj: p, y0: mo.Y}
			return nil
		}
		// A worktree drags the same way among its project's, taking its
		// sessions along, and a session among its worktree's; a press that
		// never leaves its row switches to it on the release.
		if w := rows[i].ws.Path; mo.Button == tea.MouseLeft && rows[i].kind == agWorkspace && !rows[i].pinned {
			if wts := m.worktreesOf(rows[i].project); len(wts) > 1 {
				m.drag = &drag{kind: dragRow, ws: w, y0: mo.Y}
				return nil
			}
		}

		if id := rows[i].s.ID; mo.Button == tea.MouseLeft && rows[i].kind == agSession && !rows[i].pinned && m.sessionsMovable() {
			if sibs := m.siblings(id); len(sibs) > 1 {
				m.drag = &drag{kind: dragRow, sess: id, y0: mo.Y}
				return nil
			}
		}

		return a.activate(m, &rows[i])
	}

	return nil
}

// onBranch reports column x on the ⌂ branch an open project heading
// holding its checkout shows after its name.
func (a *agents) onBranch(r agRow, x int) bool {
	if !r.merged() {
		return false
	}

	from := ansi.StringWidth(" " + chevron(true) + "· " + agLabel(r) + "  ")

	return x >= from && x < from+ansi.StringWidth(wtGlyph(r.ws).s()+" "+wsName(r.ws))
}

// dragRowTo marks where the dragged project, worktree or session would land,
// a tinted row between the rows (dropSlot), and the release moves it there and tells the
// daemon. Let go anywhere else it stays; a press that never left its row is a
// click instead.
func (a *agents) dragRowTo(m *Model, d *drag, y int, release bool) tea.Cmd {
	if y != d.y0 {
		d.moved = true
	}

	if d.moved {
		d.to = a.dropOn(m, d)
	}

	if !release {
		return nil
	}

	m.drag = nil
	if !d.moved {
		return a.activate(m, a.selected(m))
	}

	switch {
	case d.to == "":
		return nil
	case d.ws != "":
		a.moveWorkspace(m, d.ws, d.to)

		return do("workspace.move", proto.MoveParams{Path: d.ws, To: slices.Index(m.worktreesOf(m.projectOf(d.ws)), d.ws)})

	case d.sess != "":
		a.moveSession(m, d.sess, d.to)

		return do("session.move", proto.SessionMoveParams{ID: d.sess, To: d.to})
	}

	a.moveProject(m, d.proj, d.to)

	if a.collapsed[d.proj] { // the folded keep an order of their own, saved with them
		return a.saveFolds()
	}

	return do("project.move", proto.MoveParams{Path: d.proj, To: slices.Index(m.st.Projects, d.proj)})
}

// dropOn is the row under the pointer whose place drag d would take: one of
// its kind and among its own, a session's worktree's or a worktree's
// project's, a project folded or open as it is (projectDrop); "" for none
// or its own.
func (a *agents) dropOn(m *Model, d *drag) string {
	rows, y := a.rows(m), m.hoverRow(viewAgents)

	p := a.pane(rows, m.bodyH(viewAgents))
	if y >= 0 && y < p.lines { // nothing drops into Pinned
		return ""
	}

	i := p.n + a.l.top + y - p.lines // the tree's row on line y, the drop mark counted
	// Over the drop mark it stays; the rows below it sit lower by its rows.
	if at, mark, _ := a.dropSlot(m, rows); at >= 0 && y >= 0 && i >= at {
		if i < at+mark {
			return d.to
		}

		y, i = y-mark, i-mark
	}

	var (
		over string
		ok   bool
	)

	switch {
	case d.ws != "":
		over = a.workspaceAt(m, y)
		ok = slices.Contains(m.worktreesOf(m.projectOf(d.ws)), over)

	case d.sess != "":
		over = a.sessionAt(m, y)
		ok = slices.Contains(m.siblings(d.sess), over)

	case y < 0:
		return ""

	default:
		return a.projectDrop(m, d.proj, rows, i)
	}

	if !ok || over == cmp.Or(d.ws, d.sess, d.proj) {
		return ""
	}

	return over
}

// dropSlot is the row the drop mark goes before, the dragged row's new
// place, the rows the mark takes and which of them is the band: past the
// one it takes the place of, with what hangs off it, moving down; that one
// itself moving up. An open project's band sits apart as a project would,
// a blank row below it, or above it at the end. -1, 0, 0 with no drop.
func (a *agents) dropSlot(m *Model, rows []agRow) (at, h, band int) {
	d := m.drag
	if d == nil || d.kind != dragRow || d.to == "" {
		return -1, 0, 0
	}

	open := d.proj != "" && !a.collapsed[d.proj]

	key := func(r agRow) string {
		switch {
		case d.sess != "" && r.kind == agSession && !r.pinned:
			return r.s.ID
		case d.ws != "" && r.kind == agWorkspace && !r.pinned:
			return r.ws.Path
		case d.proj != "" && r.kind == agProject:
			return r.project
		}

		return ""
	}

	from := slices.IndexFunc(rows, func(r agRow) bool { return key(r) == cmp.Or(d.ws, d.sess, d.proj) })
	to := slices.IndexFunc(rows, func(r agRow) bool { return key(r) == d.to })

	switch {
	case from < 0 || to < 0:
		return -1, 0, 0
	case to < from && open:
		return to, 2, 0
	case to < from:
		return to, 1, 0
	}

	for k := rows[to].kind; to+1 < len(rows) && rows[to+1].kind > k && rows[to+1].kind <= agSub; { // the tree under it
		to++
	}

	switch {
	case !open:
		return to + 1, 1, 0
	case to+1 == len(rows):
		return to + 1, 2, 1
	case rows[to+1].kind == agGap:
		to++
	}

	return to + 1, 2, 0
}

// shiftProject moves a project d places and saves it at once: the menu's and
// alt+↑↓'s half of the drag.
func (a *agents) shiftProject(m *Model, path string, d int) tea.Cmd {
	if !a.projectMovable(m, path) {
		m.flash("sort projects manually to reorder them", false)
		return nil
	}

	order := a.shownProjects(m)
	i := slices.Index(order, path)

	j := i + d
	if i < 0 || j < 0 || j >= len(order) || a.apart(m.busyProjects(), order[j], path) { // nothing crosses into the folded, or under bareHead
		return nil
	}

	a.moveProject(m, path, order[j])

	if a.collapsed[path] {
		return a.saveFolds()
	}

	return do("project.move", proto.MoveParams{Path: path, To: slices.Index(m.st.Projects, path)})
}

// projectMovable says whether project p moves by hand: the folded keep an
// order of their own, the open sorted by Updated the clock's.
func (a *agents) projectMovable(m *Model, p string) bool {
	return a.collapsed[p] || m.st.Settings.SpProj != "updated"
}

// projectDrop is the project whose place dragged project p takes with the
// pointer on row i, one folded or open as p is: on a project's upper half p
// goes above it, on its lower half below, and a one-row project, a folded
// one, is taken whole. On a gap, or past the end, p goes where the gap is:
// above the project below it, else below the one above it. "" for none or
// p's own place.
func (a *agents) projectDrop(m *Model, p string, rows []agRow, i int) string {
	busy := m.busyProjects()
	order := slices.DeleteFunc(a.shownProjects(m), func(x string) bool { return a.apart(busy, x, p) })
	from := slices.Index(order, p)
	gap := -1 // p goes before order[gap], past the end at len(order)

	switch {
	case i >= len(rows) || rows[i].kind == agGap:
		below, above := i, min(i, len(rows))-1
		for below < len(rows) && rows[below].kind != agProject {
			below++
		}

		for above >= 0 && rows[above].kind != agProject {
			above--
		}

		if below < len(rows) && slices.Contains(order, rows[below].project) {
			gap = slices.Index(order, rows[below].project)
		} else if above >= 0 && slices.Contains(order, rows[above].project) {
			gap = slices.Index(order, rows[above].project) + 1
		}

	case rows[i].kind <= agSub:
		start, end := i, i
		for start > 0 && rows[start].kind != agProject {
			start--
		}

		for end+1 < len(rows) && rows[end+1].kind > agProject && rows[end+1].kind <= agSub {
			end++
		}

		gap = slices.Index(order, rows[start].project)
		if gap >= 0 && (end == start && gap > from || end > start && 2*(i-start) >= end-start+1) {
			gap++
		}
	}

	switch {
	case gap < 0 || from < 0 || gap == from || gap == from+1:
		return ""
	case gap < from:
		return order[gap]
	}

	return order[gap-1]
}

// moveProject puts from where to sits now and keeps the moved row selected.
// It reorders the model's own copy, which the daemon is then told of.
func (a *agents) moveProject(m *Model, from, to string) {
	order := &m.st.Projects
	if a.collapsed[from] { // among the folded: their own order
		order = &a.folded
	}

	i, j := slices.Index(*order, from), slices.Index(*order, to)
	if i < 0 || j < 0 || i == j {
		return
	}

	rest := slices.Delete(slices.Clone(*order), i, i+1)
	*order = slices.Insert(rest, j, from)

	rows := a.rows(m)
	if k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agProject && r.project == from }); k >= 0 {
		a.l.sel = k
		a.snap(m)
	}
}

// sessionsMovable says whether the Spaces tree lists sessions in the order
// session.move keeps: grouped by worktree and sorted by Created. Sorted by
// Updated or grouped by time or state, the order is the clock's.
func (m *Model) sessionsMovable() bool {
	st := m.st.Settings

	return st.SpSort != "updated" && m.byWorkspace()
}

// siblings are the ids of the sessions Spaces lists in session id's worktree,
// id among them, in their order.
func (m *Model) siblings(id string) []string {
	s := m.session(id)
	if s == nil {
		return nil
	}

	var out []string

	for _, x := range m.listedSessions() {
		if x.Workspace == s.Workspace {
			out = append(out, x.ID)
		}
	}

	return out
}

// sessionAt is the session the list row at screen row y shows, "" on any
// other row or off the list.
func (a *agents) sessionAt(m *Model, y int) string {
	if y < 0 {
		return ""
	}

	rows := a.rows(m)
	if i := a.rowAt(m, rows, y); i >= 0 && (rows[i].kind == agSession || rows[i].kind == agSub) && !rows[i].pinned { // a subagent's row is its session's
		return rows[i].s.ID
	}

	return ""
}

// worktreesOf are the paths of project's worktrees, in the tree's order.
func (m *Model) worktreesOf(project string) []string {
	var out []string

	for _, w := range m.wss {
		if w.Project == project {
			out = append(out, w.Path)
		}
	}

	return out
}

// projectOf is the project worktree path belongs to, "" for none.
func (m *Model) projectOf(path string) string {
	if w := m.workspace(path); w != nil {
		return w.Project
	}

	return ""
}

// workspaceAt is the worktree the list row at screen row y shows, "" on any
// other row or off the list.
func (a *agents) workspaceAt(m *Model, y int) string {
	if y < 0 {
		return ""
	}

	rows := a.rows(m)
	if i := a.rowAt(m, rows, y); i >= 0 && rows[i].kind == agWorkspace && !rows[i].pinned {
		return rows[i].ws.Path
	}

	return ""
}

// shiftWorkspace moves worktree path d places among its project's and saves
// it at once: the menu's and alt+↑↓'s half of the drag.
func (a *agents) shiftWorkspace(m *Model, path string, d int) tea.Cmd {
	wts := m.worktreesOf(m.projectOf(path))
	i := slices.Index(wts, path)

	to := i + d
	if i < 0 || to < 0 || to >= len(wts) {
		return nil
	}

	a.moveWorkspace(m, path, wts[to])

	return do("workspace.move", proto.MoveParams{Path: path, To: to})
}

// moveWorkspace puts worktree from where worktree to sits now, in the
// model's own list, and keeps the moved row selected; its sessions follow it
// in the tree. The daemon is told once, on release.
func (a *agents) moveWorkspace(m *Model, from, to string) {
	i := slices.IndexFunc(m.wss, func(w proto.Workspace) bool { return w.Path == from })
	j := slices.IndexFunc(m.wss, func(w proto.Workspace) bool { return w.Path == to })

	if i < 0 || j < 0 || i == j {
		return
	}

	w := m.wss[i]
	m.wss = slices.Insert(slices.Delete(slices.Clone(m.wss), i, i+1), j, w)

	rows := a.rows(m)
	if k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agWorkspace && r.ws.Path == from && !r.pinned }); k >= 0 {
		a.l.sel = k
		a.snap(m)
	}
}

// shiftSession moves session id d places among its worktree's and saves it
// at once: the menu's and alt+↑↓'s half of the drag.
func (a *agents) shiftSession(m *Model, id string, d int) tea.Cmd {
	if !m.sessionsMovable() {
		m.flash("sort by Created, grouped by Workspace, to reorder sessions", false)
		return nil
	}

	sibs := m.siblings(id)
	i := slices.Index(sibs, id)

	to := i + d
	if i < 0 || to < 0 || to >= len(sibs) {
		return nil
	}

	a.moveSession(m, id, sibs[to])

	return do("session.move", proto.SessionMoveParams{ID: id, To: sibs[to]})
}

// moveSession puts session id where session to sits in the model's own
// list, as session.move does in the daemon's: its shells go with it, and the
// moved row stays selected. The daemon is told once, on release.
func (a *agents) moveSession(m *Model, id, to string) {
	i := slices.IndexFunc(m.sessions, func(s proto.Session) bool { return s.ID == id })
	j := slices.IndexFunc(m.sessions, func(s proto.Session) bool { return s.ID == to })

	if i < 0 || j < 0 || i == j {
		return
	}

	var moved, rest []proto.Session

	for _, s := range m.sessions {
		if s.ID == id || s.Parent == id {
			moved = append(moved, s)
		} else {
			rest = append(rest, s)
		}
	}

	k := slices.IndexFunc(rest, func(s proto.Session) bool { return s.ID == to })
	if i < j { // past the target and the tabs that follow it
		k++
		for k < len(rest) && rest[k].Parent == to {
			k++
		}
	}

	m.sessions = slices.Insert(rest, k, moved...)

	rows := a.rows(m)
	if r := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession && r.s.ID == id && !r.pinned }); r >= 0 {
		a.l.sel = r
		a.snap(m)
	}
}

// agentNavigator is a quick pick of every session and worktree, like
// herdr-navigator: blocked sessions first, then running, done, idle and
// exited ones. "@blocked", "@running", "@shell", "@done", "@idle", "@exited",
// "@worktree" narrow by kind and "!claude" by agent.
func (m *Model) agentNavigator() tea.Cmd {
	ss := m.mainSessions()
	slices.SortStableFunc(ss, func(a, b proto.Session) int { return sessionRank(a) - sessionRank(b) })

	var items []item

	for _, s := range ss {
		glyph, _ := sessionGlyph(s)

		status := s.Status
		switch {
		case s.Attention && status == "idle":
			status = "done"
		case s.Shell:
			status = "shell"
		}

		where := filepath.Base(s.Workspace)
		if w := m.workspace(s.Workspace); w != nil && w.Branch != "" {
			where = w.Branch
		}

		label := glyph + sessionName(s) + titleAfter(s)
		id := s.ID
		items = append(items, item{
			label: label, hint: where + " · " + status,
			search: fmt.Sprintf("%s %s @%s !%s", label, where, status, s.Agent),
			run:    func(m *Model) tea.Cmd { m.focus = onMain; return m.switchSession(id) },
		})
	}

	for _, w := range m.wss {
		name, n := w.Branch, m.sessionCount(w.Path)
		if name == "" {
			name = filepath.Base(w.Path)
		}

		path := w.Path
		items = append(items, item{
			label: wtGlyph(w).s() + " " + name, hint: filepath.Base(w.Project) + " · " + plural(n, "session"),
			search: name + " " + filepath.Base(w.Project) + " @worktree",
			run: func(m *Model) tea.Cmd {
				switched := m.switchWorkspace(path)
				if m.sess != "" { // a session of that worktree is on screen now: talk to it
					m.focus = onMain
				}

				return tea.Batch(switched, m.refreshGit(), m.fetchScreen())
			},
		})
	}

	m.modal = newPicker("Go to Agent or Worktree", items)

	return nil
}

// addProjectPrompt asks for a folder in the folder picker and adds it as a
// project.
func (m *Model) addProjectPrompt() tea.Cmd {
	m.modal = newDirPicker(m, "Add Project", "Add", m.ws, func(_ *Model, dir string) tea.Cmd {
		return func() tea.Msg {
			var root string
			if err := proto.Call("project.add", map[string]string{"path": dir}, &root); err != nil {
				return flashMsg{err.Error(), true}
			}

			return flashMsg{"added " + root, false}
		}
	})

	return nil
}

// completeEntries lists what a typed path can go on to, directories with a
// trailing slash. With dirsOnly the files are left out.
// ponytail: one directory read per keystroke; cache it if a slow mount shows.
func completeEntries(v string, dirsOnly bool) []string {
	dir, base := filepath.Split(v)

	read := expandHome(dir)
	if dir == "" {
		read = "."
	}

	entries, err := os.ReadDir(read)
	if err != nil {
		return nil
	}

	var out []string

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) || strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}

		st, err := os.Stat(filepath.Join(read, name)) // links to directories too
		switch {
		case err == nil && st.IsDir():
			out = append(out, dir+name+"/")
		case !dirsOnly:
			out = append(out, dir+name)
		}
	}

	return out
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}

	return p
}

// term renders the active session's screen, fetched from the daemon.
type term struct {
	id              string
	scr             proto.Screen
	scroll          int
	fetching, again bool
	// sel is a drag over the screen: anchor and end, as column and line
	// (lineAt), so it stays on its text as the screen scrolls; selecting
	// while the button is down. The release copies it.
	sel       [2][2]int
	hasSel    bool
	selecting bool
	find      termFind   // ⌃f: VS Code's terminal find widget
	link      *linkHover // the link under the pointer, which ctrl+click follows
	followed  bool       // a ctrl+click followed a link: its release is not the app's either
}

// lineAt is the line on screen row y, counted as Screen.Pushed counts: the
// screen scrolled back by scroll starts at Pushed-scroll. A line keeps its
// number as output scrolls it, and as a full scrollback drops lines above
// it. The alternate screen has no scrollback.
func (t *term) lineAt(y int) int {
	if t.scr.AltScreen {
		return y
	}

	return t.pushed() - t.scroll + y
}

// readBase is the number lineAt gives line 0 of the text searched and copied:
// session.read's, the oldest scrollback line, or the screen's first for an app
// that scrolls itself, whose screen is all of it.
func (t *term) readBase() int {
	if t.ownScroll() {
		return t.lineAt(0)
	}

	return t.pushed() - t.scr.Scrollback
}

// pushed is Screen.Pushed, or for a daemon too old to send it the
// scrollback, which it equals until the scrollback is full.
func (t *term) pushed() int { return max(t.scr.Pushed, t.scr.Scrollback) }

// selEnds are the selection's ends in text order.
func (t *term) selEnds() (p, q [2]int) {
	p, q = t.sel[0], t.sel[1]
	if p[1] > q[1] || p[1] == q[1] && p[0] > q[0] {
		p, q = q, p
	}

	return p, q
}

// selCols is the selection's columns on line n, [a, b), none when b <= a.
func (t *term) selCols(n int) (a, b int) {
	p, q := t.selEnds()
	if !t.hasSel || n < p[1] || n > q[1] {
		return 0, 0
	}

	a, b = 0, wideCols
	if n == p[1] {
		a = p[0]
	}

	if n == q[1] {
		b = q[0] + 1
	}

	return a, b
}

// selRange is the selection's columns on screen row y.
func (t *term) selRange(y int) (a, b int) { return t.selCols(t.lineAt(y)) }

// mark draws the selection on screen row y as reverse video.
func (t *term) mark(y int, line string) string {
	a, b := t.selRange(y)
	if b <= a {
		return line
	}

	return ansi.Cut(line, 0, a) + "\x1b[7m" + ansi.Strip(ansi.Cut(line, a, b)) + "\x1b[0m" + ansi.Cut(line, b, wideCols)
}

// selText is the selected text on screen, lines trimmed on the right.
func (t *term) selText() string {
	top := t.lineAt(0)

	return t.cutSel(func(n int) (string, bool) {
		if n < top || n-top >= len(t.scr.Lines) {
			return "", false
		}

		return ansi.Strip(t.scr.Lines[n-top]), true
	})
}

// cutSel is the selection cut out of the plain lines text gives by number,
// lines trimmed on the right; lines text does not have are left out.
func (t *term) cutSel(text func(n int) (string, bool)) string {
	p, q := t.selEnds()

	var out []string

	for n := p[1]; t.hasSel && n <= q[1]; n++ {
		if line, ok := text(n); ok {
			a, b := t.selCols(n)
			out = append(out, strings.TrimRight(ansi.Cut(line, a, b), " "))
		}
	}

	return strings.Join(out, "\n")
}

// copySel puts the selection on the clipboard. Rows scrolled out of view
// since the drag began come from session.read, which holds them all.
func (t *term) copySel(id string) tea.Cmd {
	p, q := t.selEnds()
	if top := t.lineAt(0); p[1] >= top && q[1] < top+len(t.scr.Lines) || t.ownScroll() {
		text := t.selText()
		return setClipboard(text, fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)))
	}

	sel := term{sel: t.sel, hasSel: true} // as it is now: the mouse may start another
	base := t.readBase()

	return func() tea.Msg {
		var all string
		if err := proto.Call("session.read", proto.ReadParams{ID: id, Scrollback: true}, &all); err != nil {
			return flashMsg{"copy: " + err.Error(), true}
		}

		lines := strings.Split(all, "\n")
		text := sel.cutSel(func(n int) (string, bool) {
			if n -= base; n < 0 || n >= len(lines) {
				return "", false
			}

			return lines[n], true
		})

		return setClipboard(text, fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)))()
	}
}

type screenMsg struct {
	id  string
	scr proto.Screen
	err error
}

// fetchScreen refreshes the terminals: the session of every group on screen
// and the Terminal panel's shell.
func (m *Model) fetchScreen() tea.Cmd {
	cmds := []tea.Cmd{m.fetchTerm()}
	if !m.tiled() {
		return tea.Batch(append(cmds, m.fetchMain())...)
	}

	m.eachGroup(func() { cmds = append(cmds, m.fetchMain()) })

	return tea.Batch(cmds...)
}

func (m *Model) fetchMain() tea.Cmd {
	t := &m.term
	if m.sess == "" || m.w == 0 || !m.showsSession() {
		return nil
	}

	if t.fetching {
		t.again = true
		return nil
	}

	t.fetching = true

	// One column less than the view: the scrollbar has it.
	p := proto.ScreenParams{ID: m.sess, Cols: max(m.mainW()-1, 1), Rows: m.sessH(), Scroll: t.scroll}

	if t.id == m.sess { // the scroll counts back from this screen
		p.Pushed = t.scr.Pushed
	}

	return func() tea.Msg {
		var scr proto.Screen

		err := proto.Call("session.screen", p, &scr)

		return screenMsg{p.ID, scr, err}
	}
}

// onScreen routes a screen to whichever terminal asked for it.
func (m *Model) onScreen(msg screenMsg) tea.Cmd {
	for _, p := range m.grid.leaves() {
		if p != m.cur && p.g.sess == msg.id { // a group without the keyboard asked for it
			var cmd tea.Cmd

			m.within(p, func() { cmd = m.onScreen(msg) })

			return cmd
		}
	}

	for _, t := range []struct {
		t    *term
		want string
	}{{&m.term, m.sess}, {&m.tv.term, m.tv.id}} {
		if msg.id != t.want {
			continue
		}

		t.t.fetching = false

		var find tea.Cmd

		if msg.err == nil {
			if t.t.id == msg.id && t.t.scroll > 0 { // the page moved up with its text, as the daemon moved it
				t.t.scroll += msg.scr.Pushed - t.t.scr.Pushed
			}

			t.t.id, t.t.scr = msg.id, msg.scr
			if t.t.scroll = min(t.t.scroll, msg.scr.Scrollback); t.t.ownScroll() { // back on its own live view
				t.t.scroll = 0
			}

			find = t.t.refind(m)
		}

		if t.t.again {
			t.t.again = false
			return tea.Batch(find, m.fetchScreen())
		}

		if find != nil {
			return find
		}
	}

	return nil
}

// sessTab is one chip of the session strip or the Terminal's, whose last one
// is the + that opens another shell.
type sessTab struct {
	id     string
	x, w   int
	label  string
	tip    string // the session's whole name, when label cuts it
	active bool
	plus   bool
	bg     color.Color // session_highlight's tint
}

// spaceSessions are the sessions of the workspace in view.
func (m *Model) spaceSessions() []proto.Session {
	return slices.DeleteFunc(m.mainSessions(), func(s proto.Session) bool { return s.Workspace != m.ws })
}

// mainSessions are the sessions the Spaces tree lists and the main area
// shows: all but the Terminal panel's shells.
func (m *Model) mainSessions() []proto.Session {
	return slices.DeleteFunc(slices.Clone(m.sessions), func(s proto.Session) bool { return s.Agent == termAgent })
}

// termSessions are the shells of the Terminal panel: the shown session's own,
// or the workspace's when no session is shown.
func (m *Model) termSessions() []proto.Session {
	return slices.DeleteFunc(slices.Clone(m.sessions), func(s proto.Session) bool { return !m.termOwned(s) })
}

// termOwned reports a shell belonging to the Terminal panel as it stands:
// every session has shells of its own, and a shell opened with no session in
// view stays with its workspace.
func (m *Model) termOwned(s proto.Session) bool {
	if s.Agent != termAgent || s.Parent != m.sess {
		return false
	}

	return m.sess != "" || s.Workspace == m.ws
}

// attachTerm points the panel at a shell of the session in view — the one it
// had when that still fits, else the first — and reports whether it found one.
func (m *Model) attachTerm() bool {
	if s := m.session(m.tv.id); s != nil && m.termOwned(*s) {
		return true
	}

	m.tv.id, m.tv.term = "", term{}

	ss := m.termSessions()
	if len(ss) == 0 {
		return false
	}

	m.tv.id = ss[0].ID
	if tab := m.st.Terminals[m.ws].Tab; slices.ContainsFunc(ss, func(s proto.Session) bool { return s.ID == tab }) {
		m.tv.id = tab // the shell the workspace showed last
	}

	return true
}

// ensureTerm starts a shell for an open panel that has none to show: an open
// panel shows a shell, never an empty strip.
func (m *Model) ensureTerm() tea.Cmd {
	if m.attachTerm() || !m.termOpen() {
		return nil
	}

	return m.newTerm()
}

// tabGlyph is session s's glyph in a tab and its color, as Spaces draws
// them. A shell's screen says nothing about an agent's state: a server
// printing logs would read as working. Only an agent shows a glyph, one
// started by hand in a terminal too; a shell gets "".
func (m *Model) tabGlyph(s proto.Session) (string, color.Color) {
	if s.Agent == termAgent && !m.runsAgent(s) {
		return "", nil
	}

	g, c := sessionGlyph(s)

	return m.spin(g, s), c
}

// tabTint is c 7% of the way from the terminal's background to it: a
// session tab's fill, its glyph's color much softer. nil for a color that is
// not RGB, an ANSI palette's.
func (m *Model) tabTint(c color.Color) color.Color {
	bg := hex(ifElse(pal.light, "#ffffff", "#000000"))
	if m.bg != "" {
		bg = hex(m.bg)
	}

	if !isRGB(c) || !isRGB(bg) {
		return nil
	}

	// Mixed in sRGB as VS Code flattens a translucent color: lipgloss's
	// Blend1D allocates its whole gradient, every frame.
	r1, g1, b1, _ := c.RGBA()
	r2, g2, b2, _ := bg.RGBA()
	mix := func(a, b uint32) uint8 { return uint8((7*a + 93*b) / 100 >> 8) }

	return color.RGBA{mix(r1, r2), mix(g1, g2), mix(b1, b2), 255}
}

// sessLabel is session s's tab label in a strip with room cells for tabs,
// its ✕ shown when closable, and its tip when the label cuts its name.
func (m *Model) sessLabel(s proto.Session, closable bool, room int) (label, tip string) {
	glyph, _ := m.tabGlyph(s)
	if glyph != "" {
		glyph += " "
	}

	name, end := m.fx.text("sess:"+s.ID, sessionName(s)), " "+tabClose(closable)
	// A name too long for the strip on its own is cut, so every tab, the
	// active one above all, can show; its tip has it whole.
	frame := ansi.StringWidth(" "+glyph+end) + tabGap
	if ansi.StringWidth(name) > room-frame {
		name = ansi.Truncate(name, max(room-frame, 1), "…")
	}

	if full := fullName(s); full != sessionName(s) || ansi.StringWidth(full) > room-frame {
		tip = full
	}

	return " " + glyph + name + end, tip
}

func (m *Model) tabsFor(w int, sessions []proto.Session, active string) []sessTab {
	labels, tips, widths, total, at := make([]string, len(sessions)), make([]string, len(sessions)), make([]int, len(sessions)), 0, -1

	for i, s := range sessions {
		if s.ID == active {
			at = i
		}

		labels[i], tips[i] = m.sessLabel(s, s.ID == active || s.ID == m.overTab, w)
		widths[i] = ansi.StringWidth(labels[i])
		total += widths[i] + tabGap
	}

	// As the editor strip does: the tabs before the active one give way
	// until it fits.
	start := 0
	for total > w && start < at {
		total -= widths[start] + tabGap
		start++
	}

	var out []sessTab

	x := 0

	for i := start; i < len(sessions) && x+widths[i]+tabGap <= w; i++ {
		s := sessions[i]
		out = append(out, sessTab{id: s.ID, x: x, w: widths[i], label: labels[i], tip: tips[i], active: s.ID == active, bg: m.highlight(s)})
		x += widths[i] + tabGap
	}

	return out
}

// shells are the programs a session waits in; anything else running in one
// names its tab.
var shells = []string{"bash", "zsh", "fish", "sh", "dash", "ksh", "tcsh", "nu", "elvish", "xonsh"}

// sessionName is what a session's tab says: its fullName, cut to 32 cells.
func sessionName(s proto.Session) string { return ansi.Truncate(fullName(s), 32, "…") }

// fullName is a session's whole name: the name it was renamed to, else what
// runs in it, in the words of its terminal title (Claude Code titles the task
// it works on), else its agent, or its shell's program.
func fullName(s proto.Session) string {
	name := s.Agent
	switch {
	case s.Name != "":
		name = s.Name
	case s.Program != "" && !slices.Contains(shells, s.Program):
		name = s.Program
		if t := cleanTitle(s.Title); t != "" {
			name = t
		}

	case s.Agent == termAgent && len(s.Cmd) > 0:
		name = filepath.Base(s.Cmd[0])
	}

	return name
}

// titleAfter is " · title" for a title the session's name does not already say.
func titleAfter(s proto.Session) string {
	t := cleanTitle(s.Title)
	if t == "" || strings.HasPrefix(t, strings.TrimSuffix(sessionName(s), "…")) {
		return ""
	}

	return " · " + s.Title
}

// cleanTitle drops the status glyphs a program puts before its terminal title.
func cleanTitle(t string) string {
	return strings.TrimSpace(strings.TrimLeftFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

// sessionMenu is a session tab's right click menu.
func (m *Model) sessionMenu(id string) tea.Cmd {
	end := []item{{label: "Kill Session…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }}}
	if s := m.session(id); s != nil && canSleep(*s) {
		end = slices.Insert(end, 0, item{label: "Sleep Session", run: func(m *Model) tea.Cmd { return m.sleepSession(id) }})
	}

	m.modal = newMenu("", m.mouseX, m.mouseY, menuGroups(
		[]item{{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }}}, end)...)

	return nil
}

// canSleep reports a session that can be put to sleep: one with a process
// to stop.
func canSleep(s proto.Session) bool { return s.Status != "asleep" && s.Status != "exited" }

// sleepSession puts session id to sleep, asking first while it works: the
// turn stops, the conversation is kept.
func (m *Model) sleepSession(id string) tea.Cmd {
	s := m.session(id)
	if s == nil || !canSleep(*s) {
		return nil
	}

	run := func(*Model) tea.Cmd { return do("session.sleep", map[string]string{"id": id}) }
	if s.Status != "running" {
		return run(m)
	}

	m.modal = newDialog("Put "+sessionName(*s)+" to sleep? It is working: the turn stops, the conversation is kept.",
		item{label: "Sleep Session", run: run}, cancelItem())

	return nil
}

// wokeMsg is session.wake's answer for session id.
type wokeMsg struct {
	id  string
	err error
}

// wake brings asleep session id back: its process starts again and its
// agent continues the conversation. One wake at a time; keys typed into the
// session meanwhile are dropped (term.key).
func (m *Model) wake(id string) tea.Cmd {
	if s := m.session(id); s == nil || s.Status != "asleep" || m.waking[id] {
		return nil
	}

	if m.waking == nil {
		m.waking = map[string]bool{}
	}

	m.waking[id] = true

	return func() tea.Msg {
		return wokeMsg{id, proto.Call("session.wake", map[string]string{"id": id}, nil)}
	}
}

// renameSession names a session's tab; an empty name goes back to naming it by
// what runs in it.
func (m *Model) renameSession(id string) tea.Cmd {
	s := m.session(id)
	if s == nil {
		return nil
	}

	m.modal = newPrompt("Rename session · empty names it by what runs", s.Name, func(m *Model, v string) tea.Cmd {
		if s := m.session(id); s != nil {
			s.Name = v // at once; the daemon's list follows
		}

		return do("session.rename", map[string]string{"id": id, "name": v})
	})

	return nil
}

// sessionButtons are the buttons at the right end of the session's strip, shown
// always as the editor's are: maximize or restore the group while there are others,
// and close.
func (m *Model) sessionButtons() []rowAction {
	acts := append(m.maxAct(), rowAction{g: icClose, tip: "Close (keeps running)", run: func(m *Model) tea.Cmd { return m.hideSession() }})
	layoutRight(acts, m.mainW(), 2)

	return acts
}

// sessBtnW is the room the buttons take at the right end of the strip, with a
// cell before them.
func (m *Model) sessBtnW() int {
	n := 1
	for _, a := range m.sessionButtons() {
		n += a.w
	}

	return n
}

// tabsEnd is the column after a strip's last tab, its + included.
func tabsEnd(tabs []sessTab) int {
	if len(tabs) == 0 {
		return 0
	}

	t := tabs[len(tabs)-1]

	return t.x + t.w
}

// sessionHint is what the session in view has to say at the right end of its
// strip: that it exited, how far it is scrolled back, or where the keyboard
// goes.
func (m *Model) sessionHint() (string, lipgloss.Style) {
	s := m.session(m.sess)
	if s == nil {
		return "", plain
	}

	switch {
	case m.waking[s.ID]:
		return " waking… ", fg(pal.warn)
	case s.Status == "asleep":
		return " asleep · any key wakes it ", dim
	case s.Status == "exited":
		return fmt.Sprintf(" exited %d · x in Agents to close ", s.ExitCode), fg(pal.errc)
	case m.term.scroll > 0:
		return fmt.Sprintf(" scrollback -%d ", m.term.scroll), fg(pal.warn)
	case m.focus == offMain: // another group has the keyboard: a click gives it this one
		return "", plain
	case m.focus != onMain:
		return " ^] focus ", dim
	case m.anyRailed():
		return " ^] sidebar ", dim
	}

	return "", plain
}

// runsAgent reports an agent in the foreground of terminal s, claude typed
// at its prompt: the daemon reads its screen as it
// does an agent session's, so its state is worth a glyph.
func (m *Model) runsAgent(s proto.Session) bool {
	for name, cmd := range m.st.Agents {
		if name != "shell" && name != "terminal" && len(cmd) > 0 && filepath.Base(cmd[0]) == s.Program {
			return true
		}
	}

	return false
}

// newTermTip is the tip of the Terminal strip's +.
const newTermTip = "New Terminal  ^⇧`"

// tabSegs draws a strip's tabs, tab over in hover_bg and the ✕ of tab overX
// raised.
func tabSegs(tabs []sessTab, over, overX string) []seg {
	var segs []seg

	for _, t := range tabs {
		if t.plus {
			if over == plusTab {
				segs = append(segs, sgOwn(t.label, fg(pal.headerAccent).Background(pal.hoverBg)))
				continue
			}

			segs = append(segs, sg(t.label, fg(pal.headerAccent)))

			continue
		}

		bg := t.bg
		if t.id == over {
			bg = pal.hoverBg
		}

		chip := tabChip(t.label, t.active, bg)
		if t.id == overX {
			chip = closeHot(chip)
		}

		segs = append(segs, chip...)
	}

	return segs
}

func (t *term) view(m *Model, w, _ int) []string {
	if m.session(m.sess) == nil || t.id != m.sess {
		return nil
	}

	lines := slices.Clone(t.scr.Lines)
	for i := range lines {
		lines[i] = t.mark(i, t.paintFind(i, t.paintLink(m, i, lines[i])))
	}

	return withBar(lines, w, t.bar(len(lines)), m.barState("session"))
}

// bar is the terminal's scrollbar: its scrollback above the h rows it shows;
// none for an app that scrolls itself.
func (t *term) bar(h int) vbar {
	if t.ownScroll() {
		return vbar{h, h, 0}
	}

	return vbar{t.scr.Scrollback + h, h, t.scr.Scrollback - t.scroll}
}

// ownScroll reports an app that scrolls itself: on the alternate screen,
// which keeps no scrollback, or taking the mouse, as claude's fullscreen view
// does, whose scrollback is what scrolled off its redraws, not its history.
// The wheel and pgup/pgdn go to it.
func (t *term) ownScroll() bool { return t.scr.AltScreen || t.scr.Mouse }

// barMouse starts a drag on terminal t's scrollbar (id names it) from a
// left click at row y of its screen, at screen row sy.
func (m *Model) barMouse(t *term, id string, y, sy int) tea.Cmd {
	geo := func() vbar { return t.bar(len(t.scr.Lines)) }

	return m.barClick(id, y, sy-y, geo, func(top int) tea.Cmd {
		prev := t.scroll
		if t.scroll = max(min(t.scr.Scrollback-top, t.scr.Scrollback), 0); t.scroll != prev {
			return m.fetchScreen()
		}

		return nil
	})
}

// key sends a key to the terminal's app. pgup and pgdn (shift too, as VS
// Code's) page the scrollback instead, as the wheel does, unless the app
// scrolls itself.
func (t *term) key(m *Model, id string, k tea.KeyPressMsg) tea.Cmd {
	if !t.ownScroll() {
		switch k.String() {
		case "pgup", "shift+pgup":
			return t.scrollBy(m, len(t.scr.Lines))
		case "pgdown", "shift+pgdown":
			return t.scrollBy(m, -len(t.scr.Lines))
		}
	}

	key := k.Key()
	t.hasSel = false

	s := m.session(id)
	if id == "" || (s != nil && s.Status == "exited") {
		return nil
	}

	// The key wakes it rather than reach a process not there; while it wakes,
	// keys are dropped: typed ahead of the agent, they would garble the
	// command that resumes it.
	if s != nil && (s.Status == "asleep" || m.waking[id]) {
		return m.wake(id)
	}

	t.scroll = 0

	m.inputs <- proto.InputParams{ID: id, Keys: []proto.Key{{Code: key.Code, Mod: int(key.Mod), Text: key.Text}}}

	return nil
}

func (t *term) mouse(m *Model, id string, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()

	if y < 0 {
		return nil
	}

	if _, motion := msg.(tea.MouseMotionMsg); motion && mo.Button == tea.MouseNone {
		if t.link = t.linkAt(m, id, x, y); t.link != nil {
			t.link.sx, t.link.sy = mo.X, mo.Y
		}

		return nil // ponytail: hover motion is not forwarded; apps using any-event mouse mode miss it
	}

	if _, click := msg.(tea.MouseClickMsg); click {
		t.followed = false // the last one's release may have gone to the editor it opened

		if mo.Button == tea.MouseLeft && mo.Mod&(tea.ModCtrl|tea.ModSuper) != 0 {
			if h := t.linkAt(m, id, x, y); h != nil {
				t.followed = true // ahead of the app, as VS Code's terminal follows a link in a mouse app too

				return m.followLink(*h)
			}
		}
	}

	if _, release := msg.(tea.MouseReleaseMsg); release && t.followed {
		t.followed = false
		return nil
	}

	if t.scr.Mouse && t.scroll == 0 && !t.selecting {
		pm := &proto.Mouse{X: x, Y: y, Button: int(mo.Button), Mod: int(mo.Mod)}

		switch msg.(type) {
		case tea.MouseClickMsg:
			pm.Kind = "click"
		case tea.MouseReleaseMsg:
			pm.Kind = "release"
		case tea.MouseMotionMsg:
			pm.Kind = "motion"
		case tea.MouseWheelMsg:
			pm.Kind = "wheel"
		}

		m.inputs <- proto.InputParams{ID: id, Mouse: pm}

		return nil
	}
	// A left drag selects; the release copies it. ponytail: no word or line
	// selection on double click, no shift-extend.
	if mo.Button == tea.MouseLeft {
		switch msg.(type) {
		case tea.MouseClickMsg:
			n := t.lineAt(y)
			t.sel, t.hasSel, t.selecting = [2][2]int{{x, n}, {x, n}}, true, true

		case tea.MouseMotionMsg:
			if t.selecting {
				t.sel[1] = [2]int{x, t.lineAt(y)}
			}

		case tea.MouseReleaseMsg:
			if !t.selecting {
				break
			}

			t.selecting = false
			if t.sel[0] == t.sel[1] {
				t.hasSel = false
				break
			}

			return t.copySel(id)
		}

		return nil
	}

	if w, ok := msg.(tea.MouseWheelMsg); ok {
		return t.wheel(m, id, w.Button == tea.MouseWheelUp)
	}

	return nil
}

// wheel is a wheel turn over a terminal the app did not take as a mouse
// event: three lines through the scrollback, or on the alternate screen,
// which has none, three arrows, as xterm's and VS Code's alternate scroll.
func (t *term) wheel(m *Model, id string, up bool) tea.Cmd {
	if t.scr.AltScreen {
		key := proto.Key{Code: tea.KeyDown}
		if up {
			key.Code = tea.KeyUp
		}

		m.inputs <- proto.InputParams{ID: id, Keys: []proto.Key{key, key, key}}

		return nil
	}

	if up {
		return t.scrollBy(m, 3)
	}

	return t.scrollBy(m, -3)
}

// scrollBy moves n lines up the scrollback, down for a negative n, held
// between the live screen and the oldest line.
func (t *term) scrollBy(m *Model, n int) tea.Cmd {
	prev := t.scroll
	if t.scroll = max(min(t.scroll+n, t.scr.Scrollback), 0); t.scroll != prev {
		return m.fetchScreen()
	}

	return nil
}

func (m *Model) onNewSession(s proto.Session) tea.Cmd {
	if m.session(s.ID) == nil {
		m.sessions = append(m.sessions, s)
	}

	if s.Agent == termAgent { // it belongs to a Terminal panel
		if !m.termOwned(s) { // another session's, opened from a script
			return loadSessions()
		}

		m.tv.id, m.tv.scroll = s.ID, 0
		if m.termRows() > 0 {
			m.focus = onPanel
			return tea.Batch(m.fetchScreen(), loadSessions())
		}

		return tea.Batch(m.showView(viewTerm), loadSessions())
	}

	m.clock++
	m.recent[viewAgents] = m.clock

	return tea.Batch(m.openSession(s.ID), loadSessions())
}

// openSession shows a session and gives it the keyboard.
func (m *Model) openSession(id string) tea.Cmd {
	m.focus = onMain
	return m.switchSession(id)
}

func (m *Model) onNewWorkspace(w proto.Workspace) tea.Cmd {
	if !slices.ContainsFunc(m.wss, func(x proto.Workspace) bool { return x.Path == w.Path }) {
		m.wss = append(m.wss, w)
	}

	switched := m.switchWorkspace(w.Path)
	m.flash("worktree "+w.Branch+" ready · n starts a session", false)

	return tea.Batch(switched, loadWorkspaces(), m.refreshGit())
}

// termAgent is the agent name of the Terminal panel's shells: they are
// ordinary sessions the daemon respawns, kept out of the Agents tree.
const termAgent = "terminal"

// termPanel is the Terminal view: shells of their own, with the tab strip the
// main area has. ⌃` opens it, and it docks and moves like any other view.
type termPanel struct {
	term
	id   string
	wide bool // Toggle Size to Content Width: the shell runs wider than the panel
	left int  // the first column shown while it does
}

// wideCols is the width the Terminal's shell gets with Toggle Size to Content
// Width on. ponytail: a fixed width where VS Code measures the longest line;
// the daemon's emulator cannot reflow what already wrapped.
const wideCols = 240

// termCols is the width the Terminal panel's shell runs at.
func (m *Model) termCols() int {
	w, _ := m.termBody()
	w = max(w-1, 1) // the scrollbar's column

	if m.tv.wide {
		return max(w, wideCols)
	}

	return w
}

// termFocused reports the keyboard in the Terminal, under the editor or in
// a column, on its Terminal tab: the panel's other tabs are lists.
func (m *Model) termFocused() bool {
	if m.ptab != ptTerm {
		return false
	}

	if m.focus == onPanel {
		return true
	}

	v, ok := m.viewOn(m.focus)

	return m.focus >= 0 && ok && v == viewTerm
}

// termPanelKey sends a key to the Terminal's shell; ⌥z toggles its size to
// content width instead, as in VS Code.
func (m *Model) termPanelKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "alt+z" {
		return m.toggleTermWide()
	}

	t := &m.tv
	if t.wide { // keep the cursor in view while typing; the scrollbar has a column
		w, _ := m.termBody()
		t.left = max(min(t.left, t.scr.CursorX), t.scr.CursorX-w+2, 0)
	}

	return t.key(m, t.id, k)
}

// termPanelMouse is the Terminal's mouse: a right click opens its menu, a
// sideways wheel pans a shell wider than the panel, the rest goes to the shell.
func (m *Model) termPanelMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	t, mo := &m.tv, msg.Mouse()
	w, _ := m.termBody()

	if cmd, ok := t.findMouse(m, t.id, msg, x, y, w); ok {
		return cmd
	}

	if _, click := msg.(tea.MouseClickMsg); click && mo.Button == tea.MouseRight && y >= 0 {
		return m.termMenu(mo.X, mo.Y)
	}

	if x == w-1 {
		m.overBar = barPoint{"terminal", y}

		if _, click := msg.(tea.MouseClickMsg); click && mo.Button == tea.MouseLeft && y >= 0 {
			return m.barMouse(&t.term, "terminal", y, mo.Y)
		}

		return nil
	}

	if _, wheel := msg.(tea.MouseWheelMsg); wheel && t.wide {
		if dx, ok := wheelX(mo); ok {
			t.left = max(0, min(t.left+dx, m.termCols()-w+1))

			return nil
		}
	}

	return t.mouse(m, t.id, msg, x+t.left, y)
}

func (m *Model) toggleTermWide() tea.Cmd {
	m.tv.wide, m.tv.left = !m.tv.wide, 0
	return m.fetchScreen()
}

// termMenu is the Terminal's right click menu, after VS Code's.
func (m *Model) termMenu(x, y int) tea.Cmd {
	id := m.tv.id
	if m.session(id) == nil {
		return nil
	}

	m.modal = newMenu("", x, y, menuGroups(
		[]item{
			{label: "Copy All", run: func(*Model) tea.Cmd { return copyTerminal(id) }},
			{label: "Paste", run: func(m *Model) tea.Cmd { return m.pasteInto(id) }},
		},
		[]item{{label: "Clear", run: func(m *Model) tea.Cmd { return m.clearTerm(id) }}},
		[]item{
			{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
			{label: "Kill Terminal…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }},
		},
		[]item{{label: "Toggle Size to Content Width", hint: "M-z", run: func(m *Model) tea.Cmd { return m.toggleTermWide() }}})...)

	return nil
}

// copyTerminal puts a shell's text, its scrollback included, on the clipboard.
func copyTerminal(id string) tea.Cmd {
	return func() tea.Msg {
		var text string
		if err := proto.Call("session.read", map[string]any{"id": id, "scrollback": true}, &text); err != nil {
			return flashMsg{"copy: " + err.Error(), true}
		}

		return setClipboard(text, "copied the terminal")()
	}
}

// pasteInto types the system clipboard into a session as a bracketed paste.
func (m *Model) pasteInto(id string) tea.Cmd {
	inputs := m.inputs

	return func() tea.Msg {
		text, err := clipboardText()
		if err != nil {
			return flashMsg{"paste: " + err.Error(), true}
		}

		inputs <- proto.InputParams{ID: id, Paste: text}

		return nil
	}
}

// clearTerm clears a shell's screen with ⌃l.
// ponytail: the scrollback stays, where VS Code's Clear drops it too; that
// needs a reset call into the daemon's emulator.
func (m *Model) clearTerm(id string) tea.Cmd {
	m.tv.scroll = 0
	m.inputs <- proto.InputParams{ID: id, Text: "\x0c"}

	return m.fetchScreen()
}

// termBody is the shell's screen: the panel's body past the column of
// shells.
func (m *Model) termBody() (w, h int) {
	if n := m.termRows(); n > 0 { // the panel under the editor
		return max(m.area().w, 1), max(n-panelTopH, 1) // the border, the tab strip and its rule
	}

	rc := m.colRect(m.colOf(viewTerm))

	return max(rc.w, 1), max(m.bodyH(viewTerm)-termStripH, 1)
}

// fetchTerm asks for the Terminal panel's screen, sized to its column.
func (m *Model) fetchTerm() tea.Cmd {
	t := &m.tv
	if t.id == "" || !m.termShowing() {
		return nil
	}

	if t.fetching {
		t.again = true
		return nil
	}

	t.fetching = true
	_, h := m.termBody()

	p := proto.ScreenParams{ID: t.id, Cols: m.termCols(), Rows: h, Scroll: t.scroll}
	if t.term.id == t.id {
		p.Pushed = t.scr.Pushed
	}

	return func() tea.Msg {
		var scr proto.Screen

		err := proto.Call("session.screen", p, &scr)

		return screenMsg{p.ID, scr, err}
	}
}

// termShowing reports the panel being drawn, in a column or under the editor.
func (m *Model) termShowing() bool {
	if m.termRows() > 0 {
		return true
	}

	return m.termOpen() && m.shown(viewTerm)
}

// termPanelLines are the bottom panel: a border, a strip of its tabs and
// buttons, the rule under it, then the tab's body, spanning the editor area
// like VS Code's panel. The border, the strip's empty part and the rule are
// the panel's sash: in the bottom panel the rule lights heavy while the
// mouse rests on the panel's edge or drags it.
func (m *Model) termPanelLines(w, h int) []string {
	top := ruleRow(w, nil)
	if st, ok := m.sashStyle(termSash); ok {
		top = ruleRow(w, nil, st)
	}

	strip := m.panelStrip(w, panelTabX, m.area().x, m.areaH()+1)

	return append([]string{top, strip, m.panelRule(w, panelTabX, m.area().x, m.areaH()+1)}, m.panelBody(w, h-panelTopH)...)
}

// termLines are the panel in a column: its strip, the rule, then the tab's
// body.
func (m *Model) termLines(w, h int) []string {
	i := m.colOf(viewTerm)
	strip := m.panelStrip(w, panelTabX, m.colRect(i).x, m.barH(i))

	return append([]string{strip, m.panelRule(w, panelTabX, m.colRect(i).x, m.barH(i))}, m.panelBody(w, h-termStripH)...)
}

// panelTabX is where the panel's tabs start in its strip.
const panelTabX = 1

// termW is the width of the Terminal's strip: the editor area's, or its column's.
func (m *Model) termW() int {
	if m.termRows() > 0 {
		return m.area().w
	}

	return m.colRect(m.colOf(viewTerm)).w
}

// termButtons are the buttons at the right end of the Terminal's strip, shown
// always as the editor's are: the panel under the editor maximizes and closes,
// a column's shell closes.
func (m *Model) termButtons() []rowAction {
	var acts []rowAction

	if m.ptab == ptTerm { // VS Code's: a new shell, and the shell shown as a select
		acts = append(acts, rowAction{g: icAdd, tip: newTermTip, run: func(m *Model) tea.Cmd { return m.newTerm() }})
		if sel := m.termSelect(); sel != "" {
			acts = append(acts, rowAction{g: glyph{"select", sel, sel, sel}, tip: "Terminals", run: func(m *Model) tea.Cmd { return m.termPick() }})
		}
	}

	acts = append(acts, rowAction{g: icEllipsis, tip: "More Actions…", run: func(m *Model) tea.Cmd { return m.panelMenu() }})

	if m.termRows() > 0 {
		acts = append(acts,
			rowAction{
				g: ifElse(m.termMax, icRestore, icMax), tip: ifElse(m.termMax, "Restore Panel  ^⇧↓", "Maximize Panel  ^⇧↑"),
				run: func(m *Model) tea.Cmd { return m.maximizeTerminal(!m.termMax) },
			},
			rowAction{g: icClose, tip: "Close Terminal  ^`", run: func(m *Model) tea.Cmd { return m.toggleTerminal() }})
	} else {
		acts = append(acts, rowAction{g: icClose, tip: "Kill Terminal…", run: func(m *Model) tea.Cmd { return m.confirmKill(m.tv.id) }})
	}

	layoutRight(acts, m.termW(), 2)

	return acts
}

// termBtnW is the room the buttons take at the right end of the strip, with a
// cell before them.
func (m *Model) termBtnW() int {
	n := 1
	for _, a := range m.termButtons() {
		n += a.w
	}

	return n
}

// termScreen is the shell's screen, padded to h rows.
func (m *Model) termScreen(w, h int) []string {
	t := &m.tv

	tw := max(w-1, 0) // the scrollbar has the last column

	out := make([]string, 0, max(h, 0))
	for i := range max(h, 0) {
		if t.id == t.term.id && i < len(t.scr.Lines) {
			line := t.mark(i, t.paintFind(i, t.paintLink(m, i, t.scr.Lines[i])))
			if t.wide {
				line = ansi.Cut(line, t.left, t.left+tw)
			}

			out = append(out, line)
		}
	}

	return withBar(out, w, t.bar(h), m.barState("terminal"))
}

func (m *Model) termTabs(w int) []sessTab {
	tabs, x := m.tabsFor(w-3, m.termSessions(), m.tv.id), 0 // the + keeps its place after the tabs
	if len(tabs) > 0 {
		x = tabsEnd(tabs) + tabGap
	}

	return append(tabs, sessTab{x: x, w: 3, label: " + ", plus: true})
}

// termStripW is the room the tab strip has: the bottom panel keeps its buttons
// and the space before its tabs, a column its buttons. Rendering and hit tests
// share it.
func (m *Model) termStripW() int {
	if m.termRows() > 0 {
		return max(m.area().w-1-m.termBtnW(), 1)
	}

	return max(m.colRect(m.colOf(viewTerm)).w-m.termBtnW(), 1)
}

// focusTerminal opens the terminal when it is shut and puts the keyboard in it.
func (m *Model) focusTerminal() tea.Cmd {
	switch {
	case !m.termOpen():
		return m.toggleTerminal()
	case m.termPos() != "bottom":
		return m.showView(viewTerm)
	}

	m.focus = onPanel

	return m.fetchScreen()
}

// cycleTerm shows the next or the previous shell in the terminal.
func (m *Model) cycleTerm(d int) tea.Cmd {
	ts := m.termSessions()
	if len(ts) == 0 {
		return nil
	}

	i := slices.IndexFunc(ts, func(s proto.Session) bool { return s.ID == m.tv.id })
	m.tv.id, m.tv.scroll = ts[((i+d)%len(ts)+len(ts))%len(ts)].ID, 0

	return tea.Batch(m.openTerminalPanel(), m.fetchScreen())
}

func (m *Model) newTerm() tea.Cmd {
	return m.spawn(m.ws, termAgent, m.sess, nil, "", nil) // the daemon picks the shell
}

// toggleTerminal is ⌃`: it opens the terminal where it is docked and focuses
// it, and closes it when it is already open, wherever the focus is.
func (m *Model) toggleTerminal() tea.Cmd {
	if m.termOpen() {
		m.termMax = false
		cmd := m.setTermOpen(false, nil)
		m.fixFocus()

		return tea.Batch(cmd, m.fetchScreen())
	}

	return tea.Batch(m.showTerminal(), m.startTerm())
}

// showTerminal opens the panel where it is docked and focuses it, leaving
// what it shows to the caller: startTerm's shell, or a new one.
func (m *Model) showTerminal() tea.Cmd {
	if m.termOpen() {
		return nil
	}

	cmd := m.setTermOpen(true, nil)
	if m.termPos() == "bottom" {
		m.focus = onPanel
		return cmd
	}

	return tea.Batch(cmd, m.showView(viewTerm))
}

// moveTerminal parks the panel under the editor or in a sidebar of its own.
func (m *Model) moveTerminal(pos string) tea.Cmd {
	cmds := []tea.Cmd{m.setTermOpen(true, map[string]any{"terminal_position": pos})}

	m.termMax = false
	if pos == "bottom" {
		m.focus = onPanel
	} else {
		cmds = append(cmds, m.splitTo(viewTerm, b2i(pos == "right")))
	}

	return tea.Batch(append(cmds, m.startTerm())...)
}

// maximizeTerminal is ⌃⇧↑: the bottom panel takes the whole editor area, and
// ⌃⇧↓ (or ⌃⇧↑ again) gives it back.
func (m *Model) maximizeTerminal(on bool) tea.Cmd {
	if m.termPos() != "bottom" || !m.termOpen() {
		return flash("the terminal panel is not under the editor", true)
	}

	m.termMax, m.focus = on, onPanel

	return m.fetchScreen()
}

// startTerm shows a shell in the panel: the one it had, another that is still
// running, or a new one.
func (m *Model) startTerm() tea.Cmd {
	if cmd := m.ensureTerm(); cmd != nil {
		return cmd
	}

	return m.fetchScreen()
}

// sessStripH is the rows over a session's screen: its tab and the rule.
const sessStripH = 2

// termStripH is the same over the Terminal's.
const termStripH = sessStripH

// panelTopH is the rows over the bottom panel's screen: a border, as VS Code
// draws over its panel, then the strip and its rule.
const panelTopH = termStripH + 1
