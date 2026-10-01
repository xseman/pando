package ui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// The GitHub view's sections, VS Code's panes.
const (
	ghPRs = iota
	ghIssues
	ghNotes
)

var ghSections = []string{"Pull Requests", "Issues", "Notifications"}

// ghQuery is a folder of pull requests or issues, one of VS Code's GitHub
// Pull Requests queries. fetch names the gh call behind it: Local Pull
// Request Branches and All Open share one.
type ghQuery struct {
	sec   int
	label string
	fetch string
	args  []string
	local bool // only the pull requests with a local branch
}

var ghQueries = []ghQuery{
	{sec: ghPRs, label: "Local Pull Request Branches", fetch: "open", local: true},
	{sec: ghPRs, label: "Waiting For My Review", fetch: "review", args: []string{"--search", "review-requested:@me"}},
	{sec: ghPRs, label: "Assigned To Me", fetch: "assigned", args: []string{"--assignee", "@me"}},
	{sec: ghPRs, label: "Created By Me", fetch: "created", args: []string{"--author", "@me"}},
	{sec: ghPRs, label: "All Open", fetch: "open"},
	{sec: ghIssues, label: "My Issues", fetch: "my", args: []string{"--assignee", "@me"}},
	{sec: ghIssues, label: "Created Issues", fetch: "authored", args: []string{"--author", "@me"}},
	{sec: ghIssues, label: "Recent Issues", fetch: "recent", args: []string{"--search", "sort:updated-desc"}},
}

// ghNotesKey is the Notifications section's gh call.
const ghNotesKey = "notes"

// ghOpenLimit is how many open pull requests All Open lists, and so how far
// Local Pull Request Branches looks.
// ponytail: a local branch whose pull request is older is missed; ask gh
// per branch (--head) if that matters.
const ghOpenLimit = 50

// ghRes is one gh call's answer.
type ghRes struct {
	items  []git.Item
	notes  []git.Note
	local  map[string]bool // "open": the project's local branches
	err    error
	loaded bool // an answer is here; busy with it is a refresh on the way
	busy   bool
}

type ghMsg struct {
	project, key string
	gen          int
	res          ghRes
}

// ghDoneMsg is an action on GitHub that finished: what to say, and the lists
// to fetch again.
type ghDoneMsg struct {
	text string
	err  error
}

// ghView is VS Code's GitHub Pull Requests view over gh: pull requests and
// issues in folders of queries, and the repository's notifications. It is
// there only where gh is installed (Model.hasGH), and fetches a folder once,
// when it is unfolded and on screen; only ^r fetches it again.
type ghView struct {
	l        list
	project  string          // whose answers res holds: a project's worktrees share a repository
	open     map[string]bool // what is unfolded, by row key
	res      map[string]*ghRes
	gen      int             // a refresh or another project drops answers on the way
	checking map[string]bool // the branches a checkout is making a worktree for
	tops     map[int]int     // per section: its pane's first row in view
	// h is per section its pane's height as dragged, 0 for an even share.
	// ponytail: heights and folds last as long as the TUI; a github_panes
	// setting, as git_panes keeps the drawers', if they should outlast it.
	h map[int]int
}

func (g *ghView) init() {
	g.l, g.res, g.checking = list{sel: -1}, map[string]*ghRes{}, map[string]bool{}
	g.tops, g.h = map[int]int{}, map[int]int{}
	g.open = map[string]bool{ghSecKey(ghPRs): true, ghSecKey(ghIssues): true, ghSecKey(ghNotes): true, "q:All Open": true, "q:My Issues": true}
}

func ghSecKey(s int) string { return "s:" + ghSections[s] }

// The kinds of rows, and of the note a row can be instead of a result.
const (
	ghrSection = iota
	ghrQuery
	ghrItem
	ghrNote
	ghrInfo
)

const (
	ghErrNone = iota // not an error: Loading…, No pull requests
	ghErrNotGitHub
	ghErrNoRemote
	ghErrAuth
	ghErrOther
)

// ghRow is one row of the view: a section, a query, a pull request or an
// issue, a notification, or a line in place of results.
type ghRow struct {
	kind int
	sec  int
	q    *ghQuery  // a query, and the one an item is listed under
	item *git.Item // an item
	note *git.Note // a notification
	text string    // an info row's text
	err  int       // an info row's error class, which decides what ⏎ does
	key  string    // what folding and the selection hold on to
	sash bool      // a section header whose drag resizes: it lights as a sash
}

// ghErrKind classes gh's stderr. The first match wins: the message for a
// remote on another host also says to run gh auth login.
func ghErrKind(stderr string) int {
	s := strings.ToLower(stderr)

	switch {
	case strings.Contains(s, "point to a known github host"):
		return ghErrNotGitHub
	case strings.Contains(s, "no git remotes"):
		return ghErrNoRemote
	case strings.Contains(s, "to get started with github cli"), strings.Contains(s, "gh auth login"):
		return ghErrAuth
	}

	return ghErrOther
}

// ghErrLine is the line of gh's (or git's) error worth a flash: git's
// fatal: line when there is one, else the first.
func ghErrLine(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "fatal: ") {
			return l
		}
	}

	return lines[0]
}

// project is the project the workspace in view belongs to, the repository
// the view lists.
func (g *ghView) projectOf(m *Model) string {
	if w := m.workspace(m.ws); w != nil && w.Project != "" {
		return w.Project
	}

	return m.ws
}

// need lists the gh calls the unfolded sections and queries show.
func (g *ghView) need() []string {
	var keys []string

	for s := range ghSections {
		switch {
		case !g.open[ghSecKey(s)]:
		case s == ghNotes:
			keys = append(keys, ghNotesKey)
		default:
			for _, q := range ghQueries {
				if q.sec == s && g.open["q:"+q.label] && !slices.Contains(keys, q.fetch) {
					keys = append(keys, q.fetch)
				}
			}
		}
	}

	return keys
}

// sync drops the answers of a project left behind, shown or not: they list
// another repository's numbers.
func (g *ghView) sync(m *Model) {
	if p := g.projectOf(m); p != g.project {
		g.project, g.res, g.l, g.tops = p, map[string]*ghRes{}, list{sel: -1}, map[int]int{}
		g.gen++
	}
}

// ensure starts the gh calls the view shows and has not asked for yet.
func (g *ghView) ensure(m *Model) tea.Cmd {
	if !m.hasGH {
		return nil
	}

	g.sync(m)

	var cmds []tea.Cmd

	for _, k := range g.need() {
		if g.res[k] == nil {
			g.res[k] = &ghRes{busy: true}
			cmds = append(cmds, g.fetch(m, k))
		}
	}

	return tea.Batch(cmds...)
}

// shown is ensure while the view is on screen. The tick asks it too: the
// view comes on screen in more ways than showView (^b, a rail, a resize).
func (g *ghView) shown(m *Model) tea.Cmd {
	if !m.shown(viewGitHub) {
		return nil
	}

	return g.ensure(m)
}

// refresh asks gh again for everything the view shows; the old lists stay
// up until the answers replace them.
func (g *ghView) refresh(m *Model) tea.Cmd {
	g.gen++
	old := g.res
	g.res = map[string]*ghRes{}

	cmd := g.ensure(m)
	for k, r := range g.res {
		if o := old[k]; o != nil && o.loaded && o.err == nil {
			r.items, r.notes, r.local, r.loaded = o.items, o.notes, o.local, true
		}
	}

	return cmd
}

func (g *ghView) fetch(m *Model, key string) tea.Cmd {
	dir, project, gen := m.ws, g.project, g.gen
	return func() tea.Msg { return ghMsg{project: project, key: key, gen: gen, res: ghLoad(dir, project, key)} }
}

// ghLoad runs the gh call behind key in dir; project's branches come along
// with the open pull requests, for Local Pull Request Branches.
func ghLoad(dir, project, key string) ghRes {
	r := ghRes{loaded: true}
	if key == ghNotesKey {
		r.notes, r.err = git.Notifications(dir)
		return r
	}

	q := ghQueries[slices.IndexFunc(ghQueries, func(q ghQuery) bool { return q.fetch == key })]
	if q.sec == ghIssues {
		r.items, r.err = git.Issues(dir, q.args...)
		return r
	}

	limit := 30 // gh's own default
	if key == "open" {
		limit = ghOpenLimit
	}

	if r.items, r.err = git.PullRequests(dir, limit, q.args...); r.err == nil && key == "open" {
		out, _ := git.Run(project, "for-each-ref", "--format=%(refname:short)", "refs/heads") // none: Local lists nothing

		r.local = map[string]bool{}
		for b := range strings.FieldsSeq(out) {
			r.local[b] = true
		}
	}

	return r
}

func (g *ghView) onMsg(m *Model, msg ghMsg) {
	if msg.project != g.project || msg.gen != g.gen {
		return
	}

	key := g.selKey(m)
	r := msg.res
	g.res[msg.key] = &r
	g.follow(m, key)
}

// loading reports a gh call on the way, which the Loading… rows shimmer for.
func (g *ghView) loading() bool {
	for _, r := range g.res {
		if r.busy {
			return true
		}
	}

	return false
}

func (g *ghView) selKey(m *Model) string {
	if rows := g.rows(m); g.l.sel >= 0 && g.l.sel < len(rows) {
		return rows[g.l.sel].key
	}

	return ""
}

// follow keeps the selection on the row it was on when the rows change
// around it, as agents.follow does for a session.
func (g *ghView) follow(m *Model, key string) {
	if key == "" {
		return
	}

	if i := slices.IndexFunc(g.rows(m), func(r ghRow) bool { return r.key == key }); i >= 0 {
		g.l.sel = i
	}
}

// repoErr is an answer that leaves the view nothing to list at all: no
// GitHub remote, or no login. It shows alone, as VS Code's welcome view.
func (g *ghView) repoErr() *ghRow {
	for _, k := range slices.Sorted(maps.Keys(g.res)) {
		if r := g.res[k]; r.err != nil {
			if kind := ghErrKind(r.err.Error()); kind != ghErrOther {
				text := map[int]string{
					ghErrNotGitHub: "Not a GitHub repository",
					ghErrNoRemote:  "No remote · Publish to GitHub…",
					ghErrAuth:      "Sign in to GitHub…",
				}[kind]

				return &ghRow{kind: ghrInfo, err: kind, text: text, key: "error"}
			}
		}
	}

	return nil
}

func (g *ghView) rows(m *Model) []ghRow {
	if r := g.repoErr(); r != nil {
		return []ghRow{*r}
	}

	filter := strings.ToLower(m.query(viewGitHub))

	var out []ghRow

	for _, s := range g.order() {
		out = append(out, ghRow{kind: ghrSection, sec: s, text: ghSections[s], key: ghSecKey(s)})

		switch {
		case !g.open[ghSecKey(s)]:
		case s == ghNotes:
			out = append(out, g.results(nil, filter)...)
		default:
			for i := range ghQueries {
				q := &ghQueries[i]
				if q.sec != s {
					continue
				}

				out = append(out, ghRow{kind: ghrQuery, sec: s, q: q, key: "q:" + q.label})
				if g.open["q:"+q.label] {
					out = append(out, g.results(q, filter)...)
				}
			}
		}
	}

	return out
}

// listed is what q shows of its answer, nil while there is none; q nil is
// Notifications.
func (g *ghView) listed(q *ghQuery, filter string) (items []*git.Item, notes []*git.Note, ok bool) {
	key := ghNotesKey
	if q != nil {
		key = q.fetch
	}

	r := g.res[key]
	if r == nil || !r.loaded || r.err != nil {
		return nil, nil, false
	}

	match := func(title string, n int) bool {
		return filter == "" || strings.Contains(strings.ToLower(title), filter) || "#"+strconv.Itoa(n) == filter
	}

	for i := range r.notes {
		if n := &r.notes[i]; match(n.Title, n.Number) {
			notes = append(notes, n)
		}
	}

	local := q != nil && q.local

	for i := range r.items {
		it := &r.items[i]
		if match(it.Title, it.Number) && (!local || r.local[prBranch(*it)]) {
			items = append(items, it)
		}
	}

	return items, notes, true
}

// results are q's rows under its folder: its pull requests or issues, the
// notifications for q nil, or a line saying why there are none.
func (g *ghView) results(q *ghQuery, filter string) []ghRow {
	key, sec, label := ghNotesKey, ghNotes, "notes"
	if q != nil {
		key, sec, label = q.fetch, q.sec, q.label
	}

	info := func(text string, kind int) []ghRow {
		return []ghRow{{kind: ghrInfo, sec: sec, q: q, text: text, err: kind, key: "i:" + label}}
	}

	items, notes, ok := g.listed(q, filter)

	switch r := g.res[key]; {
	case r == nil || !r.loaded:
		return info("Loading…", ghErrNone)
	case r.err != nil:
		return info("gh: "+ghErrLine(r.err), ghErrOther)
	case !ok, len(items)+len(notes) == 0:
		return info("No "+strings.ToLower(ghSections[sec])+map[bool]string{true: " match", false: ""}[filter != ""], ghErrNone)
	}

	out := make([]ghRow, 0, len(items)+len(notes))
	for _, it := range items {
		out = append(out, ghRow{kind: ghrItem, sec: sec, q: q, item: it, key: "i:" + label + "#" + strconv.Itoa(it.Number)})
	}

	for _, n := range notes {
		out = append(out, ghRow{kind: ghrNote, sec: sec, note: n, key: "n:" + n.ID})
	}

	return out
}

// order is the sections top to bottom: the open ones sharing the height,
// the folded ones pinned under them at the bottom, each in its own order.
func (g *ghView) order() []int {
	var shut, open []int

	for s := range ghSections {
		if g.open[ghSecKey(s)] {
			open = append(open, s)
		} else {
			shut = append(shut, s)
		}
	}

	return append(open, shut...)
}

// ghPane is a section as laid out in the view: its header row, and for an
// open one the body under it, h rows of the rows [start, end).
type ghPane struct {
	sec, head, body, h, start, end int
	open                           bool
}

// panes lays the sections out over h rows as VS Code stacks its panes: every
// header keeps its row, and the open sections share what is left, each
// above the last at the height it was dragged to (an even share until
// then), the last one taking the rest.
func (g *ghView) panes(rows []ghRow, h int) []ghPane {
	var ps []ghPane

	for i, r := range rows {
		if r.kind == ghrSection {
			ps = append(ps, ghPane{sec: r.sec, start: i + 1, end: i + 1, open: g.open[r.key]})
		} else if len(ps) > 0 {
			ps[len(ps)-1].end = i + 1
		}
	}

	open := 0
	for _, p := range ps {
		open += b2i(p.open)
	}

	left := max(h-len(ps), 0) // the body rows the open sections share
	share := left / max(open, 1)
	y := 0

	for i := range ps {
		p := &ps[i]
		p.head, p.body = y, y+1

		if p.open {
			open--
			p.h = left // the last open one takes what is left

			if open > 0 {
				p.h = max(min(cmp.Or(g.h[p.sec], share), left-open), 1)
			}

			p.h = max(p.h, 0)
			left -= p.h
		}

		y = p.body + p.h
	}

	return ps
}

// paneOf is the pane holding row i, header or body.
func paneOf(ps []ghPane, i int) *ghPane {
	for j := range ps {
		if i >= ps[j].start-1 && i < ps[j].end {
			return &ps[j]
		}
	}

	return nil
}

// reveal scrolls the selected row's pane to it.
func (g *ghView) reveal(m *Model) {
	rows := g.rows(m)
	if p := paneOf(g.panes(rows, m.bodyH(viewGitHub)), g.l.sel); p != nil && g.l.sel >= p.start && p.h > 0 {
		l := list{sel: g.l.sel - p.start, top: g.tops[p.sec]}
		l.snap(p.h)
		g.tops[p.sec] = l.top
	}
}

func (g *ghView) lines(m *Model, w, h int) []string {
	rows := g.rows(m)
	g.l.sel = min(g.l.sel, len(rows)-1)
	hover := m.hoverRow(viewGitHub)

	if len(rows) == 1 && rows[0].kind == ghrInfo { // the view's one message: no sections
		return []string{g.renderRow(m, rows[0], w, g.l.sel == 0, hover == 0)}
	}

	var out []string

	ps := g.panes(rows, h)
	for _, p := range ps {
		head := rows[p.start-1]
		head.sash = g.resizes(ps, p.sec)

		out = append(out, g.renderRow(m, head, w, g.l.sel == p.start-1, hover == p.head))
		if p.h == 0 {
			continue
		}

		n := p.end - p.start
		l := list{top: g.tops[p.sec], held: m.barActive(ghBarID(p.sec))}
		l.clamp(n, p.h)
		g.tops[p.sec] = l.top

		out = append(out, l.render(w, p.h, n, func(i, rw int) string {
			return g.renderRow(m, rows[p.start+i], rw, p.start+i == g.l.sel, p.body+i-l.top == hover)
		})...)
	}

	return out
}

func ghBarID(sec int) string { return "list:github:" + ghSections[sec] }

// bar is the scrollbar beside row y of the view: its pane's, drawn as lines
// draws it.
func (g *ghView) bar(m *Model, y int) (listBar, bool) {
	ps := g.panes(g.rows(m), m.bodyH(viewGitHub))

	j := slices.IndexFunc(ps, func(p ghPane) bool { return y >= p.body && y < p.body+p.h })
	if j < 0 {
		return listBar{}, false
	}

	sec := ps[j].sec
	b := listBar{id: ghBarID(sec), row: y - ps[j].body, geo: func() vbar {
		for _, p := range g.panes(g.rows(m), m.bodyH(viewGitHub)) {
			if p.sec == sec {
				return vbar{p.end - p.start, p.h, g.tops[sec]}
			}
		}

		return vbar{}
	}, to: func(top int) tea.Cmd { g.tops[sec] = top; return nil }}

	return b, b.geo().on()
}

func (g *ghView) renderRow(m *Model, r ghRow, w int, selected, hovered bool) string {
	bg, base := m.rowColors(viewGitHub, selected, hovered)

	count := func(q *ghQuery) []seg { // none for none: the row under it says so
		if items, notes, _ := g.listed(q, strings.ToLower(m.query(viewGitHub))); len(items)+len(notes) > 0 {
			return []seg{badge(strconv.Itoa(len(items) + len(notes))), sg(" ", plain)}
		}

		return nil
	}

	switch r.kind {
	case ghrSection:
		if bg == nil {
			bg = pal.sectionBg
		}

		var right []seg
		if r.sec == ghNotes {
			right = count(nil)
		}

		left := []seg{sg(" "+chevron(g.open[r.key]), base.Bold(true)), sg(strings.ToUpper(r.text), base.Bold(true))}
		if r.sash { // the header is the resize handle, as a Source Control drawer's
			right = append(right, sg("⇕ ", dim))
			left = append(left, m.sashRule(paneSash(viewGitHub, r.text), w, left, right)...)
		}

		return row(w, bg, left, right...)

	case ghrQuery:
		return row(w, bg, []seg{sg("   "+chevron(g.open[r.key]), dim), sg(r.q.label, base)}, count(r.q)...)
	case ghrItem:
		return g.itemRow(m, r, w, bg, base)
	case ghrNote:
		return g.noteRow(r, w, bg, base)
	}

	indent := "      "
	if r.key == "error" {
		indent = " "
	}

	st := dim

	switch r.err {
	case ghErrOther:
		st = fg(pal.errc)
	case ghErrNoRemote, ghErrAuth:
		st = fg(pal.accent)
	}

	if r.text == "Loading…" {
		return row(w, bg, append([]seg{sg(indent, plain)}, m.shimmer(r.text, dim, bold)...))
	}

	return row(w, bg, []seg{sg(indent+r.text, st)})
}

// itemRow is a pull request or an issue: its icon, its title, its number and
// author, and on the right its worktree and its checks.
func (g *ghView) itemRow(m *Model, r ghRow, w int, bg color.Color, base lipgloss.Style) string {
	it := r.item
	icon, ist, title := icPR, fg(pal.ok), base

	switch {
	case r.sec == ghIssues:
		icon = icIssue
	case it.IsDraft:
		icon, ist, title = icPRDraft, fg(pal.ignored), dim
	}

	left := []seg{sg("      ", plain), sg(icon.s()+" ", ist), sg(it.Title, title), sg(fmt.Sprintf(" #%d @%s", it.Number, it.Author.Login), dim)}

	var right []seg

	if r.sec == ghPRs {
		if w := g.worktree(m, *it); w != nil {
			wt := icLinkedWt
			if w.Main {
				wt = icMainWt
			}

			right = append(right, sg(wt.s()+" ", dim))
		}

		switch it.Checks() {
		case "pass":
			right = append(right, sg(icCheckAll.s()+" ", fg(pal.ok)))
		case "fail":
			right = append(right, sg(icClose.s()+" ", fg(pal.errc)))
		case "pending":
			right = append(right, sg(icSync.s()+" ", fg(pal.warn)))
		}
	}

	return row(w, bg, left, append(right, sg(" ", plain))...)
}

// noteRow is a notification: its kind's icon, its title, and why it came;
// one read here is faint until the list is fetched again.
func (g *ghView) noteRow(r ghRow, w int, bg color.Color, base lipgloss.Style) string {
	n := r.note

	icon := icBell

	switch n.Type {
	case "PullRequest":
		icon = icPR
	case "Issue":
		icon = icIssue
	}

	st := base
	if !n.Unread {
		st = dim
	}

	return row(w, bg, []seg{sg("    ", plain), sg(icon.s()+" ", fg(pal.accent)), sg(n.Title, st), sg("  "+strings.ReplaceAll(n.Reason, "_", " "), dim)})
}

// prBranch is the local branch a pull request checks out as: its own name,
// or pr/<number> for one from a fork, whose name (often main) would clash.
func prBranch(it git.Item) string {
	if it.IsCrossRepository {
		return "pr/" + strconv.Itoa(it.Number)
	}

	return it.HeadRefName
}

// worktree is the project's worktree the pull request is checked out in.
func (g *ghView) worktree(m *Model, it git.Item) *proto.Workspace {
	b := prBranch(it)
	for i := range m.wss {
		if w := &m.wss[i]; w.Project == g.projectOf(m) && w.Branch == b {
			return w
		}
	}

	return nil
}

func (g *ghView) selected(m *Model) *ghRow {
	if rows := g.rows(m); g.l.sel >= 0 && g.l.sel < len(rows) {
		return &rows[g.l.sel]
	}

	return nil
}

// toggle folds or unfolds a section or a query, fetching what it shows. A
// section moves as it folds, down among the pinned ones or up among the
// open: the selection goes with its row.
func (g *ghView) toggle(m *Model, r *ghRow, open bool) tea.Cmd {
	if r == nil || r.kind != ghrSection && r.kind != ghrQuery {
		return nil
	}

	key := g.selKey(m)
	g.open[r.key] = open
	g.follow(m, key)
	g.reveal(m)

	return g.ensure(m)
}

func (g *ghView) collapseAll() {
	for k := range g.open {
		g.open[k] = false
	}

	g.l, g.tops = list{sel: -1}, map[int]int{}
}

// url is the row's page on GitHub.
func (r *ghRow) url() string {
	switch {
	case r == nil:
		return ""
	case r.item != nil:
		return r.item.URL
	case r.note != nil:
		return r.note.URL()
	}

	return ""
}

// activate is ⏎ or a click: fold a section or a query, open a pull request
// or an issue in the editor, open a notification and mark it read, or do
// what an info row offers.
func (g *ghView) activate(m *Model, r *ghRow) tea.Cmd {
	switch {
	case r == nil:
		return nil
	case r.kind == ghrSection, r.kind == ghrQuery:
		return g.toggle(m, r, !g.open[r.key])
	case r.item != nil:
		return m.openGH(map[bool]string{true: "issue", false: "pr"}[r.sec == ghIssues], r.item.Number)
	case r.note != nil:
		read := g.markRead(m, r.note)
		switch {
		case r.note.Number == 0:
			return tea.Batch(read, openURL(r.note.URL()))
		case r.note.Type == "PullRequest":
			return tea.Batch(read, m.openGH("pr", r.note.Number))
		}

		return tea.Batch(read, m.openGH("issue", r.note.Number))
	}

	switch r.err {
	case ghErrNoRemote:
		return m.scm.publish(m)
	case ghErrAuth:
		return m.ghTerminal("gh auth login")
	case ghErrOther:
		return g.refresh(m)
	}

	return nil
}

func openURL(url string) tea.Cmd {
	if url == "" {
		return nil
	}

	if err := openExternal(url); err != nil {
		return flash("open: "+err.Error(), true)
	}

	return nil
}

// markRead marks a notification read on GitHub; it stays listed, faint,
// until the next fetch, which asks for unread ones only.
func (g *ghView) markRead(m *Model, n *git.Note) tea.Cmd {
	if !n.Unread {
		return nil
	}

	n.Unread = false
	dir, id := m.ws, n.ID

	return func() tea.Msg {
		if _, err := git.GH(dir, "api", "-X", "PATCH", "notifications/threads/"+id); err != nil {
			return flashMsg{"gh: " + ghErrLine(err), true}
		}

		return nil
	}
}

// markDone takes a notification out of the list and out of GitHub's inbox.
func (g *ghView) markDone(m *Model, n *git.Note) tea.Cmd {
	dir, id := m.ws, n.ID // n points into the list, which the delete clears

	if r := g.res[ghNotesKey]; r != nil {
		r.notes = slices.DeleteFunc(r.notes, func(x git.Note) bool { return x.ID == id })
	}

	return func() tea.Msg {
		_, err := git.GH(dir, "api", "-X", "DELETE", "notifications/threads/"+id)
		if err == nil {
			return nil
		}

		return ghDoneMsg{err: err} // its refresh brings it back; so may one already on its way
	}
}

// onDone reports an action and fetches the lists again: a merged pull
// request leaves them, a failed Mark as Done comes back.
func (g *ghView) onDone(m *Model, msg ghDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.flash("gh: "+ghErrLine(msg.err), true)
	} else if msg.text != "" {
		m.flash(msg.text, false)
	}

	return g.refresh(m)
}

// merge asks how to merge the pull request, as VS Code's merge button does,
// and merges it only while its head is the commit the list showed: one
// pushed since would go in unseen.
func (g *ghView) merge(m *Model, it git.Item) tea.Cmd {
	dir, n := m.ws, strconv.Itoa(it.Number)
	how := func(method string) func(*Model) tea.Cmd {
		return func(*Model) tea.Cmd {
			return func() tea.Msg {
				_, err := git.GH(dir, "pr", "merge", n, "--"+method, "--match-head-commit", it.HeadRefOid)
				return ghDoneMsg{text: "merged #" + n, err: err}
			}
		}
	}

	m.modal = newDialog(fmt.Sprintf("Merge pull request #%d into %s?", it.Number, cmp.Or(it.BaseRefName, "its base")),
		item{label: "Create Merge Commit", run: how("merge")},
		item{label: "Squash and Merge", run: how("squash")},
		item{label: "Rebase and Merge", run: how("rebase")},
		cancelItem())

	return nil
}

// ghCheckoutMsg is how a pull request's checkout went: the worktree made
// for it, none when that failed, and the error.
type ghCheckoutMsg struct {
	branch string
	w      proto.Workspace
	err    error
}

// checkout opens the pull request in a worktree of its own: the one that
// has its branch already, else a new one that gh checks it out in. gh sets
// up where the branch pulls from only when it creates the branch, so a new
// worktree starts on a throwaway branch, deleted once gh made the real one;
// a local branch that exists is checked out as it is and only fast-forwards.
func (g *ghView) checkout(m *Model, it git.Item) tea.Cmd {
	n, branch, project := strconv.Itoa(it.Number), prBranch(it), g.projectOf(m)
	if w := g.worktree(m, it); w != nil {
		return tea.Batch(m.switchWorkspace(w.Path), flash("#"+n+" is checked out in "+w.Path, false))
	}

	if g.checking[branch] { // a second worktree would find the branch taken by the first
		return flash("#"+n+" is being checked out", false)
	}

	g.checking[branch] = true

	return func() tea.Msg {
		_, err := git.Run(project, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
		exists := err == nil

		ws := "" // the daemon names a throwaway branch
		if exists {
			ws = branch
		}

		var w proto.Workspace
		if err := proto.Call("workspace.new", map[string]string{"project": project, "branch": ws}, &w); err != nil {
			return ghCheckoutMsg{branch: branch, err: err}
		}

		temp := ""
		if !exists {
			temp = w.Branch
		}

		err = git.CheckoutPR(w.Path, it.Number, branch, temp)
		if err == nil {
			w.Branch = branch
		}

		return ghCheckoutMsg{branch: branch, w: w, err: err}
	}
}

func (m *Model) onGHCheckout(msg ghCheckoutMsg) tea.Cmd {
	delete(m.gh.checking, msg.branch)

	var cmd tea.Cmd
	if msg.w.Path != "" {
		cmd = m.onNewWorkspace(msg.w)
	}

	if msg.err != nil { // a worktree made stays, on its throwaway branch: x in Spaces deletes it
		m.flash("checkout: "+ghErrLine(msg.err), true)
	}

	return cmd
}

// startIssue is VS Code's Start Working on Issue: a new worktree on a
// branch named after the issue, the name up to you.
func (g *ghView) startIssue(m *Model, it git.Item) tea.Cmd {
	m.promptWorktree(g.projectOf(m), "issue/"+strconv.Itoa(it.Number)+"-"+slug(it.Title))
	return nil
}

// slug makes a branch name of a title: lowercase ASCII words joined by
// dashes, at most 40 characters.
func slug(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })
	s := strings.Join(words, "-")

	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}

	return s
}

// items are the selected row's commands and the view's own, for the context
// menu and the command palette.
func (g *ghView) items(m *Model) []item {
	r := g.selected(m)

	var out []item

	add := func(label, hint string, run func(*Model) tea.Cmd) {
		out = append(out, item{label: label, hint: hint, run: run})
	}
	link := func() {
		url := r.url()

		add("Open on GitHub", "o", func(*Model) tea.Cmd { return openURL(url) })
		add("Copy Link", "y", func(*Model) tea.Cmd { return setClipboard(url, "copied "+url) })
	}

	switch {
	case r == nil:
	case r.item != nil && r.sec == ghPRs:
		it := *r.item

		add("Open Description", "⏎", func(m *Model) tea.Cmd { return m.openGH("pr", it.Number) })
		add("Open Changes", "d", func(m *Model) tea.Cmd { return m.openGHDiff(it.Number) })
		link()

		out = append(out, separator())

		add("Checkout in New Worktree", "w", func(m *Model) tea.Cmd { return m.gh.checkout(m, it) })
		add("Merge Pull Request…", "", func(m *Model) tea.Cmd { return m.gh.merge(m, it) })

		out = append(out, separator())

	case r.item != nil:
		it := *r.item

		add("Open Description", "⏎", func(m *Model) tea.Cmd { return m.openGH("issue", it.Number) })
		link()

		out = append(out, separator())

		add("Start Working in New Worktree…", "w", func(m *Model) tea.Cmd { return m.gh.startIssue(m, it) })

		out = append(out, separator())

	case r.note != nil:
		n := r.note

		add("Open", "⏎", func(m *Model) tea.Cmd { return m.gh.activate(m, r) })
		link()
		add("Mark as Read", "", func(m *Model) tea.Cmd { return m.gh.markRead(m, n) })
		add("Mark as Done", "x", func(m *Model) tea.Cmd { return m.gh.markDone(m, n) })

		out = append(out, separator())
	}

	add("Create Pull Request", "", func(m *Model) tea.Cmd { return m.ghTerminal("gh pr create") })
	add("Refresh", "^r", func(m *Model) tea.Cmd { return m.gh.refresh(m) })
	add("Collapse All", "C", func(m *Model) tea.Cmd { m.gh.collapseAll(); return nil })

	return out
}

func (g *ghView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := g.rows(m)
	h, n := m.bodyH(viewGitHub), len(rows)
	r := g.selected(m)

	if d, ok := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -h, "pgdown": h, "g": -n, "home": -n, "G": n, "end": n}[k.String()]; ok {
		g.l.move(d, n, 0) // the panes scroll themselves, to where the selection went
		g.reveal(m)

		return nil
	}

	switch k.String() {
	case "enter", "space":
		return g.activate(m, r)
	case "l", "right":
		return g.toggle(m, r, true)
	case "h", "left":
		if r != nil && (r.kind == ghrSection || r.kind == ghrQuery) && g.open[r.key] {
			return g.toggle(m, r, false)
		}

		g.toParent(rows)
		g.reveal(m)

	case "o":
		return openURL(r.url())
	case "y":
		if url := r.url(); url != "" {
			return setClipboard(url, "copied "+url)
		}

	case "d":
		if r != nil && r.item != nil && r.sec == ghPRs {
			return m.openGHDiff(r.item.Number)
		}

	case "w":
		return g.work(m, r)
	case "x":
		if r != nil && r.note != nil {
			return g.markDone(m, r.note)
		}

	case "ctrl+r":
		return g.refresh(m)
	case "C":
		g.collapseAll()
	case "m":
		return m.menuOf(g.items(m), 0, h/2)
	}

	return nil
}

// work is w: a pull request checks out in a worktree, an issue starts one.
func (g *ghView) work(m *Model, r *ghRow) tea.Cmd {
	switch {
	case r == nil || r.item == nil:
		return nil
	case r.sec == ghPRs:
		return g.checkout(m, *r.item)
	}

	return g.startIssue(m, *r.item)
}

// toParent moves the selection up to the section or query the row is in.
func (g *ghView) toParent(rows []ghRow) {
	if g.l.sel < 0 || g.l.sel >= len(rows) {
		return
	}

	kind := rows[g.l.sel].kind
	for i := g.l.sel - 1; i >= 0; i-- {
		if rows[i].kind < kind && rows[i].kind <= ghrQuery {
			g.l.sel = i
			return
		}
	}
}

// at is the row at line y of the view and the pane it is in; a header is
// its pane's start-1. -1 where no row is.
func (g *ghView) at(rows []ghRow, ps []ghPane, y int) (int, *ghPane) {
	if len(rows) == 1 && rows[0].kind == ghrInfo {
		return map[bool]int{true: 0, false: -1}[y == 0], nil
	}

	for j := range ps {
		p := &ps[j]

		switch {
		case y == p.head:
			return p.start - 1, p
		case y >= p.body && y < p.body+p.h:
			if i := p.start + g.tops[p.sec] + y - p.body; i < p.end {
				return i, p
			}

			return -1, p
		}
	}

	return -1, nil
}

func (g *ghView) mouse(m *Model, msg tea.MouseMsg, y int) tea.Cmd {
	mo := msg.Mouse()
	rows := g.rows(m)
	ps := g.panes(rows, m.bodyH(viewGitHub))
	i, p := g.at(rows, ps, y)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		if p != nil && p.h > 0 {
			l := list{top: g.tops[p.sec]}
			l.wheel(wheelDelta(mo), p.end-p.start, p.h)
			g.tops[p.sec] = l.top
		}

	case tea.MouseClickMsg:
		if i < 0 {
			return nil
		}

		g.l.sel = i
		switch {
		case mo.Button == tea.MouseRight:
			return m.menuOf(g.items(m), mo.X, mo.Y)
		case mo.Button != tea.MouseLeft:
		case p != nil && i == p.start-1: // a header: a drag resizes, a click folds on release
			_, h := above(ps, p.sec)
			m.drag = &drag{kind: dragPane, v: viewGitHub, pane: ghSections[p.sec], y0: mo.Y, h0: h}

		default:
			return g.activate(m, &rows[i])
		}
	}

	return nil
}

// above is the open pane above section sec's and its height, -1 for none:
// that pane's lower edge is what dragging sec's header moves.
func above(ps []ghPane, sec int) (up, h int) {
	up, h = -1, -1

	for _, p := range ps {
		if p.sec == sec {
			break
		}

		if p.open {
			up, h = p.sec, p.h
		}
	}

	return up, h
}

// resizes reports whether section sec's header drags an edge: it is open,
// below another open one.
func (g *ghView) resizes(ps []ghPane, sec int) bool {
	up, _ := above(ps, sec)

	return up >= 0 && g.open[ghSecKey(sec)]
}

// dragPane moves the edge between an open section and the open one above
// it, VS Code's sash between two panes; a press that never moved folds or
// unfolds the section instead.
func (g *ghView) dragPane(m *Model, d *drag, y int, release bool) tea.Cmd {
	sec := slices.Index(ghSections, d.pane)
	if !release {
		d.moved = d.moved || y != d.y0
		if ps := g.panes(g.rows(m), m.bodyH(viewGitHub)); d.moved && g.resizes(ps, sec) {
			up, _ := above(ps, sec)
			g.h[up] = max(d.h0+y-d.y0, 1) // panes clamps it to what the others leave
		}

		return nil
	}

	m.drag = nil

	if d.moved {
		return nil
	}

	return g.toggle(m, &ghRow{kind: ghrSection, sec: sec, key: ghSecKey(sec)}, !g.open[ghSecKey(sec)])
}

// ghTerminal types line into a new shell of the Terminal panel, for what gh
// asks about as it goes (gh auth login, gh pr create). Typed rather than run
// as the shell's command, so a restarted daemon brings back a shell, not
// the command again.
func (m *Model) ghTerminal(line string) tea.Cmd {
	return tea.Batch(m.showTerminal(), m.spawn(m.ws, termAgent, m.rootOf(m.sess), nil, line))
}

// ghDoc is a pull request or an issue as gh view prints it.
type ghDoc struct {
	git.Item

	Body, State string
	Comments    []struct {
		Author    struct{ Login string }
		Body      string
		CreatedAt time.Time
	}
}

// ghDocument is a pull request's or an issue's page as Markdown, from gh.
func ghDocument(dir, kind, n string) (string, error) {
	fields := "number,title,author,url,body,state,comments"
	if kind == "pr" {
		fields += ",isDraft,headRefName,baseRefName,statusCheckRollup"
	}

	out, err := git.GH(dir, kind, "view", n, "--json", fields)
	if err != nil {
		return "", err
	}

	var d ghDoc
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return "", err
	}

	return ghMarkdown(kind, d), nil
}

// ghMarkdown lays a pull request or an issue out as GitHub's page does: the
// title, who opened it and where it merges, its text, its checks and its
// comments.
func ghMarkdown(kind string, d ghDoc) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s #%d\n\n", d.Title, d.Number)

	state := d.State
	if state != "" {
		state = state[:1] + strings.ToLower(state[1:])
	}

	if d.IsDraft {
		state = "Draft"
	}

	if kind == "pr" {
		fmt.Fprintf(&b, "**@%s** wants to merge `%s` into `%s` · %s", d.Author.Login, d.HeadRefName, d.BaseRefName, state)

		if c := d.Checks(); c != "" {
			b.WriteString(" · checks " + c)
		}
	} else {
		fmt.Fprintf(&b, "**@%s** opened this issue · %s", d.Author.Login, state)
	}

	body := strings.TrimSpace(d.Body)
	if body == "" {
		body = "_No description provided._"
	}

	b.WriteString("\n\n" + body + "\n")

	if len(d.Rollup) > 0 {
		b.WriteString("\n## Checks\n\n")

		for _, c := range d.Rollup {
			mark := map[string]string{"pass": "✓", "fail": "✗", "pending": "○"}[git.Item{Rollup: []git.Check{c}}.Checks()]
			fmt.Fprintf(&b, "- %s %s\n", mark, cmp.Or(c.Name, c.Context))
		}
	}

	if len(d.Comments) > 0 {
		b.WriteString("\n## Comments\n")

		for _, c := range d.Comments {
			fmt.Fprintf(&b, "\n**@%s** · %s\n\n%s\n", c.Author.Login, c.CreatedAt.Format(time.DateOnly), strings.TrimSpace(c.Body))
		}
	}

	b.WriteString("\n" + d.URL + "\n")

	return b.String()
}

// openGH opens pull request or issue n ("pr" or "issue") in the editor, its
// page rendered as Markdown.
func (m *Model) openGH(kind string, n int) tea.Cmd {
	return m.setPreview(preview{kind: pvGH, root: m.ws, rev: kind + "/" + strconv.Itoa(n), md: 1})
}

// openGHDiff opens pull request n's changes, gh pr diff, in the editor.
func (m *Model) openGHDiff(n int) tea.Cmd {
	return m.setPreview(preview{kind: pvGHDiff, root: m.ws, rev: "pr/" + strconv.Itoa(n)})
}
