package ui

import (
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

const (
	agProject = iota
	agWorkspace
	agSession
	agGap // the blank row between two projects; nothing selects it
)

type agRow struct {
	kind    int
	project string
	ws      proto.Workspace
	s       proto.Session
}

// agents is the Project → Workspace → Session tree.
type agents struct {
	l         list
	collapsed map[string]bool
}

type newSessionMsg proto.Session

func (a *agents) rows(m *Model) []agRow {
	q := m.query(viewAgents)

	var out []agRow

	known := map[string]bool{}

	for _, p := range m.st.Projects {
		out = append(out, agRow{kind: agProject, project: p})
		for _, w := range m.wss {
			if w.Project != p {
				continue
			}

			known[w.Path] = true

			if a.collapsed[p] && q == "" {
				continue
			}

			out = append(out, agRow{kind: agWorkspace, project: p, ws: w})
			for _, s := range m.agentSessions() {
				if s.Workspace == w.Path {
					out = append(out, agRow{kind: agSession, project: p, ws: w, s: s})
				}
			}
		}
	}

	header := false

	for _, s := range m.agentSessions() { // sessions outside every known worktree, under "other"
		if known[s.Workspace] {
			continue
		}

		if !header {
			out, header = append(out, agRow{kind: agProject}), true
		}

		if !a.collapsed[""] || q != "" {
			out = append(out, agRow{kind: agSession, ws: proto.Workspace{Path: s.Workspace}, s: s})
		}
	}

	if q == "" {
		return withGaps(out)
	}
	// Keep matching rows plus the project and worktree they sit under.
	keep := make([]bool, len(out))
	project, ws := -1, -1

	for i, r := range out {
		switch r.kind {
		case agProject:
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

	return withGaps(shown)
}

// withGaps puts a blank row before a project, herdr's gap between two spaces,
// but only where the project above it unfolded: the gap separates rows that
// belong to something, so a run of folded projects stays a tight list.
func withGaps(rows []agRow) []agRow {
	out := make([]agRow, 0, len(rows)+4)
	for i, r := range rows {
		if r.kind == agProject && i > 0 && rows[i-1].kind != agProject {
			out = append(out, agRow{kind: agGap})
		}

		out = append(out, r)
	}

	return out
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
	case agGap:
		return ""
	}

	return r.s.Agent + " " + sessionName(r.s) + " " + r.s.Title
}

// sessionGlyph is herdr's status symbol set: × blocked on a permission or a
// question, ◐ working, ✓ done (idle, unseen since it finished), ○ idle, and
// ✕ for a process that exited with a code.
func sessionGlyph(s proto.Session) (string, color.Color) {
	switch {
	case s.Status == "blocked":
		return "× ", pal.errc
	case s.Status == "running":
		return "◐ ", pal.warn
	case s.Status == "exited":
		return "✕ ", pal.ignored
	case s.Attention:
		return "✓ ", pal.attention
	}

	return "○ ", pal.ok
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
	a.l.clamp(len(rows), h)

	hover := m.hoverRow(viewAgents)
	all := m.agentSessions()

	return a.l.render(w, h, len(rows), func(i, rw int) string {
		r := rows[i]
		if r.kind == agGap {
			return blank(rw)
		}

		bg, base := m.rowColors(viewAgents, i == a.l.sel, i-a.l.top == hover)

		switch r.kind {
		case agProject:
			open := !a.collapsed[r.project]

			var ss []proto.Session

			for _, s := range all {
				if w := m.workspace(s.Workspace); (w != nil && w.Project == r.project) || (w == nil && r.project == "") {
					ss = append(ss, s)
				}
			}

			glyph, c := groupGlyph(ss)

			return row(rw, bg, []seg{sg(" "+chevron(open), dim), sg(glyph, fg(c)), sg(agLabel(r), base.Bold(true))})

		case agWorkspace:
			nameSt := dim
			if r.ws.Path == m.ws {
				nameSt = base.Foreground(pal.headerAccent).Bold(true)
			}

			name := r.ws.Branch
			if name == "" {
				name = filepath.Base(r.ws.Path)
			}

			n := 0

			for _, s := range all {
				if s.Workspace == r.ws.Path {
					n++
				}
			}

			var right []seg
			if n > 0 {
				right = append(right, sg(fmt.Sprintf("%d ", n), dim))
			}

			return row(rw, bg, []seg{sg("   "+name, nameSt)}, right...)
		}
		// herdr's tree: sessions hang off their branch, the last one on └─.
		conn := "   ├─ "
		if i+1 == len(rows) || rows[i+1].kind != agSession {
			conn = "   └─ "
		}

		glyph, c := sessionGlyph(r.s)
		label := sessionName(r.s) + titleAfter(r.s)

		nameSt := base
		if r.s.ID == m.sess {
			nameSt = nameSt.Bold(true).Underline(true)
		}

		status := r.s.Status
		if r.s.Status == "exited" {
			status = fmt.Sprintf("exit %d", r.s.ExitCode)
		}

		return row(rw, bg, []seg{sg(conn, dim), sg(glyph, fg(c)), sg(label, nameSt)}, sg(" "+status+" ", dim))
	})
}

// reveal selects workspace path's row, its project unfolded, as a click on it
// would: whatever switches the workspace shows where it went. A selection
// already inside that workspace, a clicked session, stays.
func (a *agents) reveal(m *Model, path string) {
	if r := a.selected(m); r != nil && r.kind != agProject && r.ws.Path == path {
		return
	}

	if w := m.workspace(path); w != nil && a.collapsed[w.Project] {
		a.collapsed[w.Project] = false
	}

	rows := a.rows(m)
	if i := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agWorkspace && r.ws.Path == path }); i >= 0 {
		a.l.sel = i
		a.l.snap(m.bodyH(viewAgents))
	}
}

// selected is the highlighted row, nil when nothing is selected.
func (a *agents) selected(m *Model) *agRow {
	if rows := a.rows(m); a.l.sel >= 0 && a.l.sel < len(rows) && rows[a.l.sel].kind != agGap {
		return &rows[a.l.sel]
	}

	return nil
}

// step moves the selection by d rows and walks on over the blank separators,
// so ↑↓ never parks on one. A gap always sits between two rows, so the walk
// ends inside the list.
func (a *agents) step(rows []agRow, d, h int) {
	n := len(rows)
	a.l.move(d, n, h)

	for a.l.sel >= 0 && a.l.sel < n && rows[a.l.sel].kind == agGap {
		a.l.move(d, n, h)
	}
}

func (a *agents) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := a.rows(m)
	h, n := m.bodyH(viewAgents), len(rows)

	var r *agRow
	if a.l.sel >= 0 && a.l.sel < n && rows[a.l.sel].kind != agGap {
		r = &rows[a.l.sel]
	}

	switch k.String() {
	case "up", "k":
		a.step(rows, -1, h)
	case "down", "j":
		a.step(rows, 1, h)
	case "alt+up", "alt+down":
		if r != nil && r.project != "" {
			return a.shiftProject(m, r.project, map[bool]int{true: -1, false: 1}[k.String() == "alt+up"])
		}

	case "g", "home":
		a.l.move(-n, n, h)
	case "G", "end":
		a.l.move(n, n, h)
	case "enter", "space", "l":
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

	case "x", "d", "delete":
		return a.remove(m, r)
	case "r":
		return tea.Batch(loadState(), loadWorkspaces(), loadSessions())
	case "m":
		return a.menu(m, 0, h/2)
	}

	return nil
}

func (a *agents) activate(m *Model, r *agRow) tea.Cmd {
	if r == nil {
		return nil
	}

	if a.collapsed == nil {
		a.collapsed = map[string]bool{}
	}

	switch r.kind {
	case agProject:
		a.collapsed[r.project] = !a.collapsed[r.project]
	case agWorkspace:
		return tea.Batch(m.switchWorkspace(r.ws.Path), m.refreshGit(), m.fetchScreen())
	case agSession:
		if r.s.ID == m.sess && (m.showsSession() || m.shown(viewSession)) { // a second click puts it away
			return m.hideSession()
		}

		return m.openSession(r.s.ID)
	}

	return nil
}

// workspaceFor picks the workspace a new session goes into.
func (m *Model) workspaceFor(r *agRow) string {
	if r == nil || r.kind == agGap || (r.kind == agProject && r.project == "") {
		return m.ws
	}

	if r.kind == agProject {
		for _, w := range m.wss {
			if w.Project == r.project && w.Main {
				return w.Path
			}
		}

		return r.project
	}

	return r.ws.Path
}

// newSession opens a shell in the workspace at once: agents are started from
// inside it, the way a terminal works. Pick a preset with newAgentSession.
func (a *agents) newSession(m *Model, r *agRow) tea.Cmd {
	return m.newShell(m.workspaceFor(r), "shell")
}

// newShell starts agent's preset in ws, or the user's shell without one.
func (m *Model) newShell(ws, agent string) tea.Cmd {
	if cmd := m.st.Agents[agent]; len(cmd) > 0 {
		return m.newSession(ws, agent, nil)
	}

	return m.newSession(ws, agent, []string{cmp.Or(os.Getenv("SHELL"), "/bin/sh")})
}

func (a *agents) newAgentSession(m *Model, r *agRow) tea.Cmd {
	ws := m.workspaceFor(r)
	names := slices.Sorted(maps.Keys(m.st.Agents))

	items := make([]item, len(names))
	for i, name := range names {
		items[i] = item{label: name, hint: strings.Join(m.st.Agents[name], " "), run: func(m *Model) tea.Cmd {
			return m.newSession(ws, name, nil)
		}}
	}

	m.modal = newPicker("New session in "+filepath.Base(ws), items)

	return nil
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

	m.modal = newPrompt("New worktree branch in "+filepath.Base(project), "", func(_ *Model, branch string) tea.Cmd {
		if branch == "" {
			return nil
		}

		return func() tea.Msg {
			var w proto.Workspace
			if err := proto.Call("workspace.new", map[string]string{"project": project, "branch": branch}, &w); err != nil {
				return flashMsg{err.Error(), true}
			}

			return newWorkspaceMsg(w)
		}
	})

	return nil
}

type newWorkspaceMsg proto.Workspace

func (a *agents) remove(m *Model, r *agRow) tea.Cmd {
	if r == nil {
		return nil
	}

	var (
		title, method string
		params        any
	)

	switch r.kind {
	case agSession:
		title, method, params = "Kill "+sessionName(r.s)+" session?", "session.kill", map[string]string{"id": r.s.ID}
	case agWorkspace:
		if r.ws.Main {
			return flash("the main worktree cannot be removed", true)
		}

		title, method, params = "Remove worktree "+r.ws.Branch+"?", "workspace.remove", map[string]string{"path": r.ws.Path}

	case agProject:
		if r.project == "" {
			return nil
		}

		title, method, params = "Remove project "+filepath.Base(r.project)+" from pando?", "project.remove", map[string]string{"path": r.project}
	}

	m.modal = newMenu(title, -1, 0,
		item{label: "Yes", run: func(*Model) tea.Cmd { return do(method, params) }},
		cancelItem())

	return nil
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

	items := []item{
		{label: "Go to Agent or Worktree…", hint: "M-t", run: func(m *Model) tea.Cmd { return m.agentNavigator() }},
		{label: "New Session", hint: "n", run: func(m *Model) tea.Cmd { return a.newSession(m, r) }},
		{label: "New Agent Session…", run: func(m *Model) tea.Cmd { return a.newAgentSession(m, r) }},
		{label: "New Worktree…", hint: "w", run: func(m *Model) tea.Cmd { return a.newWorktree(m, r) }},
		{label: "Add Project…", hint: "a", run: func(m *Model) tea.Cmd { return a.key(m, tea.KeyPressMsg{Code: 'a', Text: "a"}) }},
		{label: "Open Project…", run: func(m *Model) tea.Cmd { return m.projectPicker() }},
	}
	if r != nil && r.project != "" && len(m.st.Projects) > 1 {
		// What a drag does, for the keyboard: the project keeps its place in
		// config, so the order survives a restart either way.
		items = append(items,
			item{label: "Move Project Up", hint: "M-↑", run: func(m *Model) tea.Cmd { return a.shiftProject(m, r.project, -1) }},
			item{label: "Move Project Down", hint: "M-↓", run: func(m *Model) tea.Cmd { return a.shiftProject(m, r.project, 1) }})
	}

	if r != nil && r.kind == agSession {
		id := r.s.ID

		items = append(items, item{label: "Rename Session…", hint: "R", run: func(m *Model) tea.Cmd { return m.renameSession(id) }})
	}

	if r != nil {
		items = append(items, item{label: "Remove / Kill…", hint: "x", run: func(m *Model) tea.Cmd { return a.remove(m, r) }})
	}

	return items
}

func (a *agents) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(a.items(m), x, y) }

func (a *agents) mouse(m *Model, msg tea.MouseMsg, y int) tea.Cmd {
	mo := msg.Mouse()
	rows := a.rows(m)
	h := m.bodyH(viewAgents)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		a.l.wheel(wheelDelta(mo), len(rows), h)
	case tea.MouseClickMsg:
		i := a.l.at(y, len(rows))
		if i < 0 || rows[i].kind == agGap {
			return nil
		}

		a.l.sel = i
		if mo.Button == tea.MouseRight {
			return a.menu(m, mo.X, mo.Y)
		}
		// A project row drags up and down the list; a press that never leaves
		// its row is the click that folds it, so the fold waits for the release.
		if p := rows[i].project; mo.Button == tea.MouseLeft && rows[i].kind == agProject && p != "" && len(m.st.Projects) > 1 {
			m.drag = &drag{kind: dragRow, proj: p, from: slices.Index(m.st.Projects, p), y0: mo.Y}
			return nil
		}

		return a.activate(m, &rows[i])
	}

	return nil
}

// dragRowTo moves the dragged project to wherever the pointer is: the tree
// reorders under it row by row, and the release tells the daemon the index it
// landed on. A press that never left its row folds the project instead.
func (a *agents) dragRowTo(m *Model, d *drag, y int, release bool) tea.Cmd {
	if y != d.y0 {
		d.moved = true
	}

	if d.moved {
		if over := a.projectAt(m, m.hoverRow(viewAgents)); over != "" && over != d.proj {
			a.moveProject(m, d.proj, over)
		}
	}

	if !release {
		return nil
	}

	m.drag = nil
	if !d.moved {
		return a.activate(m, a.selected(m))
	}

	to := slices.Index(m.st.Projects, d.proj)
	if to < 0 || to == d.from {
		return nil
	}

	return do("project.move", proto.MoveParams{Path: d.proj, To: to})
}

// shiftProject moves a project d places and saves it at once: the menu's and
// alt+↑↓'s half of the drag.
func (a *agents) shiftProject(m *Model, path string, d int) tea.Cmd {
	i := slices.Index(m.st.Projects, path)

	to := i + d
	if i < 0 || to < 0 || to >= len(m.st.Projects) {
		return nil
	}

	a.moveProject(m, path, m.st.Projects[to])

	return do("project.move", proto.MoveParams{Path: path, To: to})
}

// projectAt is the project the list row at screen row y belongs to, "" off
// the list, on a blank row or on the "other" group, which is not a project.
func (a *agents) projectAt(m *Model, y int) string {
	if y < 0 {
		return ""
	}

	rows := a.rows(m)

	i := a.l.at(y, len(rows))
	if i < 0 {
		return ""
	}

	return rows[i].project
}

// moveProject puts from where to sits now and keeps the moved row selected.
// It reorders the model's own copy so the tree follows the pointer; the
// daemon is told once, on release.
func (a *agents) moveProject(m *Model, from, to string) {
	i, j := slices.Index(m.st.Projects, from), slices.Index(m.st.Projects, to)
	if i < 0 || j < 0 || i == j {
		return
	}

	rest := slices.Delete(slices.Clone(m.st.Projects), i, i+1)
	m.st.Projects = slices.Insert(rest, j, from)

	rows := a.rows(m)
	if k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agProject && r.project == from }); k >= 0 {
		a.l.sel = k
		a.l.snap(m.bodyH(viewAgents))
	}
}

// agentNavigator is a quick pick of every session and worktree, like
// herdr-navigator: blocked sessions first, then running, done, idle and
// exited ones. "@blocked", "@running", "@done", "@idle", "@exited",
// "@worktree" narrow by kind and "!claude" by agent.
func (m *Model) agentNavigator() tea.Cmd {
	ss := m.agentSessions()
	slices.SortStableFunc(ss, func(a, b proto.Session) int { return sessionRank(a) - sessionRank(b) })

	var items []item

	for _, s := range ss {
		glyph, _ := sessionGlyph(s)

		status := s.Status
		if s.Attention && status == "idle" {
			status = "done"
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
		name, n := w.Branch, 0
		if name == "" {
			name = filepath.Base(w.Path)
		}

		for _, s := range m.agentSessions() {
			n += b2i(s.Workspace == w.Path)
		}

		path := w.Path
		items = append(items, item{
			label: icBranch.s() + " " + name, hint: filepath.Base(w.Project) + " · " + plural(n, "session"),
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

// addProjectPrompt asks for a directory and adds it as a project, completing
// the path as fish does: the rest shows dimmed, tab or → takes it.
func (m *Model) addProjectPrompt() tea.Cmd {
	defer func() {
		md := m.modal
		md.complete, md.input.ShowSuggestions = completeDir, true
		md.input.SetSuggestions(completeDir(md.input.Value()))
	}()

	m.modal = newPrompt("Add project (directory)", m.ws, func(_ *Model, v string) tea.Cmd {
		return func() tea.Msg {
			var root string
			if err := proto.Call("project.add", map[string]string{"path": expandHome(v)}, &root); err != nil {
				return flashMsg{err.Error(), true}
			}

			return flashMsg{"added " + root, false}
		}
	})

	return nil
}

// completeDir lists the directories a typed path can go on to: "~/gi" gives
// "~/github.com/". Dotted ones only once a dot is typed.
func completeDir(v string) []string { return completeEntries(v, true) }

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
}

type screenMsg struct {
	id  string
	scr proto.Screen
	err error
}

// fetchScreen refreshes both terminals: the main area's session and the
// Terminal panel's shell.
func (m *Model) fetchScreen() tea.Cmd { return tea.Batch(m.fetchMain(), m.fetchTerm()) }

func (m *Model) fetchMain() tea.Cmd {
	t := &m.term
	docked := m.sessDocked()

	shown := m.showsSession() || docked && m.shown(viewSession)
	if m.sess == "" || m.w == 0 || !shown {
		return nil
	}

	if t.fetching {
		t.again = true
		return nil
	}

	t.fetching = true

	p := proto.ScreenParams{ID: m.sess, Cols: m.mainW(), Rows: m.sessH(), Scroll: t.scroll}
	if docked { // its column: the tab strip above the screen
		p.Cols, p.Rows = m.sessW(), max(m.bodyH(viewSession)-1, 1)
	}

	return func() tea.Msg {
		var scr proto.Screen

		err := proto.Call("session.screen", p, &scr)

		return screenMsg{p.ID, scr, err}
	}
}

// onScreen routes a screen to whichever terminal asked for it.
func (m *Model) onScreen(msg screenMsg) tea.Cmd {
	for _, t := range []struct {
		t    *term
		want string
	}{{&m.term, m.sess}, {&m.tv.term, m.tv.id}} {
		if msg.id != t.want {
			continue
		}

		t.t.fetching = false
		if msg.err == nil {
			t.t.id, t.t.scr = msg.id, msg.scr
			t.t.scroll = min(t.t.scroll, msg.scr.Scrollback)
		}

		if t.t.again {
			t.t.again = false
			return m.fetchScreen()
		}
	}

	return nil
}

// sessTab is one chip of the session strip; the last one is the + that opens
// another shell.
type sessTab struct {
	id     string
	x, w   int
	label  string
	active bool
	plus   bool
}

// sessionTabs are the agent sessions, in the order [ and ] cycle them, with a
// + to start one more.
func (m *Model) sessionTabs(w int) []sessTab {
	return m.tabsFor(w, m.agentSessions(), m.sess)
}

// agentSessions are the sessions the Agents view and the main area show; the
// Terminal panel keeps its own.
func (m *Model) agentSessions() []proto.Session {
	return slices.DeleteFunc(slices.Clone(m.sessions), func(s proto.Session) bool { return s.Agent == termAgent })
}

// termSessions are the shells of the Terminal panel.
func (m *Model) termSessions() []proto.Session {
	return slices.DeleteFunc(slices.Clone(m.sessions), func(s proto.Session) bool { return s.Agent != termAgent })
}

func (m *Model) tabsFor(w int, sessions []proto.Session, active string) []sessTab {
	var out []sessTab

	x := 0

	for _, s := range sessions {
		glyph, _ := sessionGlyph(s)

		label := " " + glyph + " " + sessionName(s)
		if s.Workspace != m.ws {
			label += " · " + m.branchOf(s.Workspace)
		}

		if s.ID == active {
			label += " " + icClose.s()
		}

		label += " "

		tw := ansi.StringWidth(label)
		if x+tw > w-3 {
			break
		}

		out = append(out, sessTab{id: s.ID, x: x, w: tw, label: label, active: s.ID == active})
		x += tw
	}

	return append(out, sessTab{x: x, w: 3, label: " + ", plus: true})
}

// branchOf names a workspace the way its tab shows it.
func (m *Model) branchOf(path string) string {
	if w := m.workspace(path); w != nil && w.Branch != "" {
		return w.Branch
	}

	return filepath.Base(path)
}

// shells are the programs a session waits in; anything else running in one
// names its tab.
var shells = []string{"bash", "zsh", "fish", "sh", "dash", "ksh", "tcsh", "nu", "elvish", "xonsh"}

// sessionName is what a session's tab says: the name it was renamed to, else
// what runs in it, in the words of its terminal title (Claude Code titles the
// task it works on), else its agent, or its shell's program.
func sessionName(s proto.Session) string {
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

	return ansi.Truncate(name, 32, "…")
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
	m.modal = newMenu("", m.mouseX, m.mouseY,
		item{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
		item{label: "Kill Session…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }})

	return nil
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

func (m *Model) sessionStrip(w int) string { return row(w, nil, tabSegs(m.sessionTabs(w))) }

func tabSegs(tabs []sessTab) []seg {
	var segs []seg

	for _, t := range tabs {
		st := dim

		switch {
		case t.plus:
			st = fg(pal.headerAccent)
		case t.active:
			st = selStyle()
		}

		segs = append(segs, sg(t.label, st))
	}

	return segs
}

// sessionStripMouse switches, closes or opens a session from the strip.
func (m *Model) sessionStripMouse(x int, button tea.MouseButton) tea.Cmd {
	for _, t := range m.sessionTabs(m.sessW()) {
		if x < t.x || x >= t.x+t.w {
			continue
		}

		switch {
		case t.plus:
			return m.ag.newSession(m, nil)
		case button == tea.MouseRight:
			return m.sessionMenu(t.id)
		case button == tea.MouseMiddle || (t.active && x >= t.x+t.w-2):
			return m.confirmKill(t.id)
		}

		return m.switchSession(t.id)
	}

	return nil
}

func (t *term) view(m *Model, w, _ int) (string, []string) {
	s := m.session(m.sess)
	if s == nil {
		return blank(w), nil
	}

	glyph, c := sessionGlyph(*s)

	ws := filepath.Base(s.Workspace)
	if wsp := m.workspace(s.Workspace); wsp != nil && wsp.Branch != "" {
		ws = wsp.Branch
	}

	st := accent
	if m.focus != onMain {
		st = bold
	}

	left := []seg{sg(" "+glyph, fg(c)), sg(sessionName(*s), st), sg(" · "+ws, dim)}
	if t := titleAfter(*s); t != "" {
		left = append(left, sg(t, dim))
	}

	var right []seg

	switch {
	case s.Status == "exited":
		right = append(right, sg(fmt.Sprintf(" exited %d · x in Agents to close ", s.ExitCode), fg(pal.errc)))
	case t.scroll > 0:
		right = append(right, sg(fmt.Sprintf(" scrollback -%d ", t.scroll), fg(pal.warn)))
	case m.focus != onMain:
		right = append(right, sg(" ^] focus ", dim))
	case m.anyRailed():
		right = append(right, sg(" ^] sidebar ", dim))
	}

	if t.id != m.sess {
		return row(w, nil, left, right...), nil
	}

	return row(w, nil, left, right...), t.scr.Lines
}

func (t *term) key(m *Model, id string, k tea.KeyPressMsg) tea.Cmd {
	key := k.Key()

	if s := m.session(id); id == "" || (s != nil && s.Status == "exited") {
		return nil
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
		return nil // ponytail: hover motion is not forwarded; apps using any-event mouse mode miss it
	}

	if t.scr.Mouse && t.scroll == 0 {
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

	if w, ok := msg.(tea.MouseWheelMsg); ok {
		prev := t.scroll
		if w.Button == tea.MouseWheelUp {
			t.scroll = min(t.scroll+3, t.scr.Scrollback)
		} else {
			t.scroll = max(t.scroll-3, 0)
		}

		if t.scroll != prev {
			return m.fetchScreen()
		}
	}

	return nil
}

func (m *Model) onNewSession(s proto.Session) tea.Cmd {
	if m.session(s.ID) == nil {
		m.sessions = append(m.sessions, s)
	}

	if s.Agent == termAgent { // it belongs to the Terminal panel
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

// openSession shows a session and gives it the keyboard, in its column when it
// is docked beside the editor.
func (m *Model) openSession(id string) tea.Cmd {
	m.focus = onMain

	cmd := m.switchSession(id)
	if m.sessDocked() {
		return tea.Batch(cmd, m.showView(viewSession))
	}

	return cmd
}

// sessFocused reports the keyboard in the docked session's column.
func (m *Model) sessFocused() bool {
	if m.focus < 0 {
		return false
	}

	v, ok := m.viewOn(m.focus)

	return ok && v == viewSession
}

// sessW is the width the session is drawn at: the editor area's, or its column's.
func (m *Model) sessW() int {
	if i := m.colOf(viewSession); i >= 0 {
		return max(m.colRect(i).w, 1)
	}

	return m.mainW()
}

// sessionLines are the docked session: its tabs, then its screen.
func (m *Model) sessionLines(w, h int) []string {
	_, lines := m.term.view(m, w, h-1)
	out := []string{m.sessionStrip(w)}

	for i := range max(h-1, 0) {
		if i < len(lines) {
			out = append(out, fit(lines[i], w))
		} else {
			out = append(out, blank(w))
		}
	}

	return out
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
	if m.tv.wide {
		return max(w, wideCols)
	}

	return w
}

// termFocused reports the keyboard in the Terminal, under the editor or in a column.
func (m *Model) termFocused() bool {
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
	if t.wide { // keep the cursor in view while typing
		w, _ := m.termBody()
		t.left = max(min(t.left, t.scr.CursorX), t.scr.CursorX-w+1, 0)
	}

	return t.key(m, t.id, k)
}

// termPanelMouse is the Terminal's mouse: a right click opens its menu, a
// sideways wheel pans a shell wider than the panel, the rest goes to the shell.
func (m *Model) termPanelMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	t, mo := &m.tv, msg.Mouse()
	if _, click := msg.(tea.MouseClickMsg); click && mo.Button == tea.MouseRight && y >= 0 {
		return m.termMenu(mo.X, mo.Y)
	}

	if _, wheel := msg.(tea.MouseWheelMsg); wheel && t.wide {
		if dx, ok := wheelX(mo); ok {
			w, _ := m.termBody()
			t.left = max(0, min(t.left+dx, m.termCols()-w))

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

	m.modal = newMenu("", x, y,
		item{label: "Copy All", run: func(*Model) tea.Cmd { return tea.Batch(copyTerminal(id), flash("copied the terminal", false)) }},
		item{label: "Paste", run: func(m *Model) tea.Cmd { return m.pasteInto(id) }},
		item{label: "Clear", run: func(m *Model) tea.Cmd { return m.clearTerm(id) }},
		item{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
		item{label: "Kill Terminal…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }},
		item{label: "Toggle Size to Content Width", hint: "M-z", run: func(m *Model) tea.Cmd { return m.toggleTermWide() }})

	return nil
}

// copyTerminal puts a shell's text, its scrollback included, on the clipboard.
func copyTerminal(id string) tea.Cmd {
	return func() tea.Msg {
		var text string
		if err := proto.Call("session.read", map[string]any{"id": id, "scrollback": true}, &text); err != nil {
			return flashMsg{"copy: " + err.Error(), true}
		}

		return tea.SetClipboard(text)()
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

// clipboardText reads the system clipboard with the first tool that answers.
// ponytail: wl-paste, xclip, xsel or pbpaste; an OSC 52 read is left out,
// most terminals refuse it.
func clipboardText() (string, error) {
	for _, argv := range [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-o", "-selection", "clipboard"}, {"xsel", "-ob"}, {"pbpaste"}} {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}

		if out, err := exec.Command(argv[0], argv[1:]...).Output(); err == nil {
			return string(out), nil
		}
	}

	return "", errors.New("no clipboard tool answered (wl-paste, xclip, xsel)")
}

// clearTerm clears a shell's screen with ⌃l.
// ponytail: the scrollback stays, where VS Code's Clear drops it too; that
// needs a reset call into the daemon's emulator.
func (m *Model) clearTerm(id string) tea.Cmd {
	m.tv.scroll = 0
	m.inputs <- proto.InputParams{ID: id, Text: "\x0c"}

	return m.fetchScreen()
}

func (m *Model) termBody() (w, h int) {
	if n := m.termRows(); n > 0 { // the panel under the editor
		return max(m.mainW(), 1), max(n-1, 1) // -1: the tab strip
	}

	rc := m.colRect(m.colOf(viewTerm))

	return max(rc.w, 1), max(m.bodyH(viewTerm)-1, 1)
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

// termPanelLines are the bottom panel: a title row with its tabs, then the
// terminal screen, spanning the editor area like VS Code's panel.
func (m *Model) termPanelLines(w, h int) []string {
	st := dim
	if m.focus == onPanel {
		st = accent
	}

	head := row(w, pal.sectionBg, []seg{sg(" "+icTerminal.s()+" ", st)},
		sg(" "+icClose.s()+" ", dim))
	if tabs := m.termTabs(m.termStripW()); len(tabs) > 0 {
		head = row(w, pal.sectionBg, append([]seg{sg(" ", plain)}, tabSegs(tabs)...), sg(" "+icClose.s()+" ", dim))
	}

	return append([]string{head}, m.termScreen(w, h-1)...)
}

// termLines are the panel: its tab strip, then the terminal screen.
func (m *Model) termLines(w, h int) []string {
	return append([]string{row(w, nil, tabSegs(m.termTabs(w)))}, m.termScreen(w, h-1)...)
}

// termScreen is the shell's screen, padded to h rows.
func (m *Model) termScreen(w, h int) []string {
	t := &m.tv

	out := make([]string, 0, max(h, 0))
	for i := range max(h, 0) {
		if t.id == t.term.id && i < len(t.scr.Lines) {
			line := t.scr.Lines[i]
			if t.wide {
				line = ansi.Cut(line, t.left, t.left+w)
			}

			out = append(out, fit(line, w))
		} else {
			out = append(out, blank(w))
		}
	}

	return out
}

func (m *Model) termTabs(w int) []sessTab { return m.tabsFor(w, m.termSessions(), m.tv.id) }

// termStripW is the room the tab strip has: the bottom panel keeps its ✕ and
// a margin, a column gives it its whole width. Rendering and hit tests share it.
func (m *Model) termStripW() int {
	if m.termRows() > 0 {
		return max(m.mainW()-10, 1)
	}

	return m.colRect(m.colOf(viewTerm)).w
}

// termStripMouse switches, closes or opens a shell from the panel's strip.
func (m *Model) termStripMouse(x int, button tea.MouseButton) tea.Cmd {
	for _, t := range m.termTabs(m.termStripW()) {
		if x < t.x || x >= t.x+t.w {
			continue
		}

		switch {
		case t.plus:
			return m.newTerm()
		case button == tea.MouseRight:
			return m.sessionMenu(t.id)
		case button == tea.MouseMiddle || (t.active && x >= t.x+t.w-2):
			return m.confirmKill(t.id)
		}

		m.tv.id, m.tv.scroll = t.id, 0

		return m.fetchScreen()
	}

	return nil
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

func (m *Model) newTerm() tea.Cmd { return m.newShell(m.ws, termAgent) }

// toggleTerminal is ⌃`: it opens the terminal where it is docked and focuses
// it, and closes it when it is already open, wherever the focus is.
func (m *Model) toggleTerminal() tea.Cmd {
	if m.termOpen() {
		m.termMax = false
		cmd := m.setSettings(map[string]any{"terminal_open": false})
		m.fixFocus()

		return tea.Batch(cmd, m.fetchScreen())
	}

	cmd := m.setSettings(map[string]any{"terminal_open": true})
	if m.termPos() == "bottom" {
		m.focus = onPanel
	} else {
		cmd = tea.Batch(cmd, m.showView(viewTerm))
	}

	return tea.Batch(cmd, m.startTerm())
}

// moveTerminal parks the panel under the editor or in a sidebar of its own.
func (m *Model) moveTerminal(pos string) tea.Cmd {
	cmds := []tea.Cmd{m.setSettings(map[string]any{"terminal_position": pos, "terminal_open": true})}

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
	if m.session(m.tv.id) == nil {
		m.tv.id, m.tv.term = "", term{}
		if ss := m.termSessions(); len(ss) > 0 {
			m.tv.id = ss[0].ID
		}
	}

	if m.tv.id == "" {
		return m.newTerm()
	}

	return m.fetchScreen()
}
