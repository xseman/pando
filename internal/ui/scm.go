package ui

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

const (
	rowRepo = iota
	rowMsg
	rowGap
	rowCommit
	rowSection
	rowDir
	rowFile
	rowDrawer
	rowLine
)

type scmRow struct {
	kind  int
	root  string // repository of the row; drawer rows belong to the active one
	title string // section or drawer title
	entry git.Entry
	text  string // section: file count; dir: label; line: text
	path  string // dir: repo-relative path
	line  int    // message box: the box's visual line this row draws
	depth int
}

// widget reports rows that are clicked rather than selected: the message
// box and the action button. Keyboard navigation skips them.
func (r scmRow) widget() bool { return r.kind >= rowMsg && r.kind <= rowCommit }

// scmView is VS Code's Source Control: per repository a message box, the
// action button and the changes, all as list rows; history drawers sit at
// the bottom and act on the active repository.
type scmView struct {
	repos    []string
	status   map[string]git.Status
	repo     int // active repository: the message box input and drawers belong to it
	input    textarea.Model
	draft    string          // last draft sent to the daemon for the active repo
	closed   map[string]bool // collapsed repos, sections ("root|Changes") and tree dirs
	drawers  map[string][]string
	history  string // repo-relative file File History follows
	rows     []scmRow
	heads    []int          // row index of each drawer header, in m.drawers() order
	sel      int            // selected row, -1 = none
	tops     map[string]int // scroll offset per pane, "" = changes
	busy     string
	busyRoot string
	frame    int              // scramble frame of the message box while ✦ writes a message
	last     *git.SuggestOpts // the last suggestion's request, for Regenerate
	lastMsg  string           // and what it came back with
	hovRow   int              // row under the mouse in the frame being drawn, -1 = none
}

// suggestTickMsg advances the message box's scramble while a suggestion runs.
type suggestTickMsg struct{}

func suggestTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return suggestTickMsg{} })
}

type scmMsg struct {
	root, text string
	err        error
	clear      bool
	publish    bool   // a commit was to sync but the branch has no upstream: publish it
	message    string // suggestion to put in the input
	worktree   string // a checkout found the branch in this worktree: open it
}

// modalMsg opens a menu built in a tea.Cmd, such as the "stage with merge
// conflicts?" question that first scans the file for markers.
type modalMsg struct{ menu *modal }

// stageMsg runs a staging command the marker scan cleared.
type stageMsg struct {
	root string
	run  func(*Model) tea.Cmd
}

type refsMsg struct {
	root string
	refs []git.Ref
	err  error
}

// remotesMsg answers publish: the remotes to pick from, and whether gh can
// publish to GitHub when there is none.
type remotesMsg struct {
	root    string
	remotes []git.Remote
	gh      bool
	login   string // the gh user, "" when not signed in
	err     error
}

type drawerMsg struct {
	root, title string
	lines       []string
}

const defaultPaneH = 8

func (s *scmView) init() {
	s.input = newMessageArea()
	s.styleInput(true)
	s.reset()
}

// maxMsgLines is how tall the message box grows, VS Code's scm.inputMaxLineCount.
const maxMsgLines = 10

// styleInput paints the message box text on the input background.
func (s *scmView) styleInput(dark bool) { s.input.SetStyles(areaStyles(dark)) }

// msgLines is how many rows root's message box takes: the active one grows
// with its text, the others show their draft's first line.
func (s *scmView) msgLines(root string) int {
	if root == s.root() {
		return max(s.input.Height(), 1)
	}

	return 1
}

// fit rebuilds the rows when the message box grew or shrank.
func (s *scmView) fit(m *Model) {
	n := 0

	for _, r := range s.rows {
		if r.kind == rowMsg && r.root == s.root() {
			n++
		}
	}

	if n != s.msgLines(s.root()) {
		s.build(m)
	}
}

func (s *scmView) reset() {
	s.repos, s.status, s.repo = nil, map[string]git.Status{}, 0
	s.closed, s.drawers, s.tops = map[string]bool{}, map[string][]string{}, map[string]int{}
	s.rows, s.heads, s.sel, s.draft, s.history = nil, nil, -1, "", ""
	s.last, s.lastMsg = nil, ""
	s.input.Reset()
	s.input.Blur()
}

func (s *scmView) root() string {
	if s.repo < len(s.repos) {
		return s.repos[s.repo]
	}

	return ""
}

// summary names the repository and branch for the view header.
func (s *scmView) summary() string {
	if len(s.repos) != 1 {
		return ""
	}

	return filepath.Base(s.repos[0]) + " · " + headLabel(s.status[s.repos[0]])
}

// headLabel is the branch with a * for any change and, as in VS Code, a !
// while a merge, rebase or cherry-pick is in progress.
func headLabel(st git.Status) string {
	label := st.Branch
	if len(st.Staged)+len(st.Changes)+len(st.Conflicts) > 0 {
		label += "*"
	}

	if st.Op != "" {
		label += "!"
	}

	return label
}

// conflictPaths are the Merge Changes entries' paths, or those under dir;
// uu are the ones that can still hold conflict markers.
func conflictPaths(st git.Status, dir string) (uu, all []string) {
	for _, e := range st.Conflicts {
		if dir != "" && e.Path != dir && !strings.HasPrefix(e.Path, dir+"/") {
			continue
		}

		all = append(all, e.Path)
		if e.XY == "UU" || e.XY == "AA" {
			uu = append(uu, e.Path)
		}
	}

	return uu, all
}

func paths(entries []git.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}

	return out
}

// onGit takes the daemon's git status and rebuilds the view.
func (s *scmView) onGit(m *Model, msg gitMsg) {
	prev := s.root()
	s.repos, s.status = msg.repos, msg.status

	s.repo = 0
	for i, r := range s.repos {
		if r == prev {
			s.repo = i
		}
	}

	if s.root() != prev {
		s.loadDraft(m)
	}

	s.build(m)
}

// loadDraft fills the message box with the active repository's saved draft.
func (s *scmView) loadDraft(m *Model) {
	s.draft = m.st.Drafts[s.root()]
	s.input.SetValue(s.draft)
}

// saveDraft persists the message box when it changed since the last save.
func (s *scmView) saveDraft(m *Model) tea.Cmd {
	root, v := s.root(), s.input.Value()
	if root == "" || v == s.draft {
		return nil
	}

	s.draft = v

	if m.st.Drafts == nil {
		m.st.Drafts = map[string]string{}
	}

	m.st.Drafts[root] = v

	return do("state.set", map[string]any{"drafts": map[string]string{root: v}})
}

// setRepo makes root the active repository.
func (s *scmView) setRepo(m *Model, root string) tea.Cmd {
	if root == "" || root == s.root() {
		return nil
	}

	cmd := s.saveDraft(m)
	for i, r := range s.repos {
		if r == root {
			s.repo = i
		}
	}

	s.last, s.lastMsg = nil, "" // Regenerate belongs to the repository it wrote for

	s.drawers, s.history = map[string][]string{}, ""
	for k := range s.tops {
		if k != "" {
			delete(s.tops, k)
		}
	}

	s.loadDraft(m)
	s.build(m)

	return tea.Batch(cmd, s.loadDrawers(m))
}

// pane is a drawer's open state and height, from the shared settings.
func (m *Model) pane(title string) proto.Pane {
	p := m.st.Settings.GitPanes[title]
	if p.H <= 0 {
		p.H = defaultPaneH
	}

	return p
}

func (m *Model) setPane(title string, p proto.Pane) tea.Cmd {
	return m.setSettings(map[string]any{"git_panes": map[string]proto.Pane{title: p}})
}

func sectionKey(root, title string) string { return root + "|" + title }

func (s *scmView) build(m *Model) {
	q := m.query(viewGit)
	multi := len(s.repos) > 1

	s.rows, s.heads = s.rows[:0], s.heads[:0]
	for _, root := range s.repos {
		st := s.status[root]
		if multi {
			s.rows = append(s.rows, scmRow{kind: rowRepo, root: root})
			if s.closed[root] {
				continue
			}
		}
		// Blank rows around the message box and the button, like VS Code's padding.
		s.rows = append(s.rows, scmRow{kind: rowGap, root: root})
		for i := range s.msgLines(root) {
			s.rows = append(s.rows, scmRow{kind: rowMsg, root: root, line: i})
		}

		s.rows = append(s.rows, scmRow{kind: rowGap, root: root}, scmRow{kind: rowCommit, root: root}, scmRow{kind: rowGap, root: root})
		// Changes always shows, like VS Code; a filter hides sections without matches.
		section := func(title string, entries []git.Entry, always bool) {
			if q != "" {
				entries = slices.DeleteFunc(slices.Clone(entries), func(e git.Entry) bool { return fuzzy(q, e.Path) < 0 })
			}

			if len(entries) == 0 && (q != "" || !always) {
				return
			}

			s.rows = append(s.rows, scmRow{kind: rowSection, root: root, title: title, text: strconv.Itoa(len(entries))})
			switch {
			case s.closed[sectionKey(root, title)]:
			case m.st.Settings.GitTree:
				s.rows = append(s.rows, treeRows(root, title, entries, s.closed)...)
			default:
				for _, e := range entries {
					s.rows = append(s.rows, scmRow{kind: rowFile, root: root, title: title, entry: e})
				}
			}
		}
		tracked, untracked := splitUntracked(st.Changes)
		section("Merge Changes", st.Conflicts, false)
		section("Staged Changes", st.Staged, false)
		section("Changes", tracked, true)
		section("Untracked Changes", untracked, false)
	}

	lq := strings.ToLower(q)

	for _, d := range m.drawers() {
		s.heads = append(s.heads, len(s.rows))

		s.rows = append(s.rows, scmRow{kind: rowDrawer, title: d.Title})
		if !m.pane(d.Title).Open {
			continue
		}

		for _, l := range s.drawers[d.Title] {
			if lq == "" || strings.Contains(strings.ToLower(l), lq) {
				s.rows = append(s.rows, scmRow{kind: rowLine, title: d.Title, text: l})
			}
		}
	}

	if s.sel >= len(s.rows) || (s.sel >= 0 && s.rows[s.sel].widget()) {
		s.sel = -1
	}
}

// splitUntracked separates untracked files from other unstaged changes.
func splitUntracked(changes []git.Entry) (tracked, untracked []git.Entry) {
	for _, e := range changes {
		if e.Letter == 'U' {
			untracked = append(untracked, e)
		} else {
			tracked = append(tracked, e)
		}
	}

	return tracked, untracked
}

func untrackedPaths(st git.Status) []string {
	_, untracked := splitUntracked(st.Changes)

	paths := make([]string, len(untracked))
	for i, e := range untracked {
		paths[i] = e.Path
	}

	return paths
}

// rowAction is a hover button on a section, folder or file row.
type rowAction struct {
	g    glyph
	x, w int
	run  func(m *Model) tea.Cmd
}

// hit is the action under column x, if any.
func hit(acts []rowAction, x int) (rowAction, bool) {
	for _, a := range acts {
		if x >= a.x && x < a.x+a.w {
			return a, true
		}
	}

	return rowAction{}, false
}

// layoutRight places acts side by side ending at x, each pad cells wider
// than its glyph.
func layoutRight(acts []rowAction, x, pad int) {
	for i := len(acts) - 1; i >= 0; i-- {
		acts[i].w = pad + ansi.StringWidth(acts[i].g.s())
		x -= acts[i].w
		acts[i].x = x
	}
}

// actions are row r's hover buttons, VS Code's inline stage, unstage and
// discard, right-aligned before the row's count badge or status letter.
func (s *scmView) actions(r scmRow, w int) []rowAction {
	root := r.root
	git1 := func(label string, fn func(root string) error) func(*Model) tea.Cmd {
		return func(*Model) tea.Cmd {
			return s.run(root, label, func(root string) scmMsg { return scmMsg{err: fn(root)} })
		}
	}

	var acts []rowAction

	add := func(g glyph, run func(*Model) tea.Cmd) { acts = append(acts, rowAction{g: g, run: run}) }
	tail := 3 // " M "

	switch r.kind {
	case rowSection:
		tail = len(r.text) + 3 // " 12 " and a space
		switch r.title {
		case "Merge Changes":
			add(icAdd, func(m *Model) tea.Cmd { return s.stageConflicts(m, root, "") })
		case "Staged Changes":
			add(icRemove, git1("unstaging", git.UnstageAll))
		case "Changes":
			add(icDiscard, func(m *Model) tea.Cmd { return s.confirmDiscardAll(m, root) })
			add(icAdd, s.stageTracked(root))

		case "Untracked Changes":
			add(icAdd, s.stageUntracked(root))
		}

	case rowDir:
		tail = 1

		dir := git.Entry{Path: r.path}
		switch r.title {
		case "Merge Changes":
			add(icAdd, func(m *Model) tea.Cmd { return s.stageConflicts(m, root, dir.Path) })
		case "Staged Changes":
			add(icRemove, git1("unstaging", func(root string) error { return git.Unstage(root, dir) }))
		default:
			add(icAdd, git1("staging", func(root string) error { return git.Stage(root, dir) }))
		}

	case rowFile:
		e := r.entry
		if e.Letter != 'D' && e.XY != "DD" {
			add(icSource, func(m *Model) tea.Cmd { return m.openFile(filepath.Join(root, e.Path)) })
		}

		switch {
		case e.Letter == '!': // VS Code's merge group: stage only, no discard
			add(icAdd, func(m *Model) tea.Cmd { return s.stageConflict(m, root, e) })
		case e.Staged:
			add(icRemove, git1("unstaging", func(root string) error { return git.Unstage(root, e) }))
		default:
			add(icDiscard, func(m *Model) tea.Cmd { return s.confirmDiscard(m, root, e) })
			add(icAdd, git1("staging", func(root string) error { return git.Stage(root, e) }))
		}
	}

	layoutRight(acts, w-tail, 2) // " g ": a space either side

	return acts
}

// mouseCol is the mouse's column in the view's rows, as clicks count it: a
// vertical activity bar on the left comes off.
func (s *scmView) mouseCol(m *Model) int {
	i := m.colOf(viewGit)

	mx := m.mouseX - m.colRect(i).x
	if m.side(i) == 0 {
		mx -= m.barW(i)
	}

	return mx
}

// actionSegs draws row r's hover buttons, the one under the mouse raised.
func (s *scmView) actionSegs(m *Model, r scmRow, w int) []seg {
	mx := s.mouseCol(m)

	var out []seg

	for _, a := range s.actions(r, w) {
		if mx >= a.x && mx < a.x+a.w {
			out = append(out, sgOwn(" "+a.g.s()+" ", keycapHot()))

			continue
		}

		out = append(out, sg(" "+a.g.s()+" ", plain))
	}

	return out
}

// stageTracked is the Changes section's +. `git add -u` would also record
// unmerged paths as resolved, so with conflicts the section's own paths go.
func (s *scmView) stageTracked(root string) func(*Model) tea.Cmd {
	return func(*Model) tea.Cmd {
		st := s.status[root]
		if len(st.Conflicts) == 0 {
			return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageTracked(root)} })
		}

		tracked, _ := splitUntracked(st.Changes)
		ps := paths(tracked)

		return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageFiles(root, ps)} })
	}
}

// stageAll is Stage All: like VS Code's it leaves Merge Changes alone.
func (s *scmView) stageAll(root string) tea.Cmd {
	st := s.status[root]
	if len(st.Conflicts) == 0 {
		return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageAll(root)} })
	}

	ps := paths(st.Changes)

	return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageFiles(root, ps)} })
}

// stageConflict marks one Merge Changes entry resolved (git add), asking
// first as VS Code does: a file that still holds conflict markers, or a
// deletion conflict, where the answer is keep the file or delete it.
func (s *scmView) stageConflict(m *Model, root string, e git.Entry) tea.Cmd {
	stage := func(_ *Model) tea.Cmd {
		return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.Stage(root, e)} })
	}
	cancel := cancelItem()

	switch e.XY {
	case "UD", "DU":
		who, keep := "them and modified by us", "Keep Our Version"
		if e.XY == "DU" {
			who, keep = "us and modified by them", "Keep Their Version"
		}

		m.modal = newMenu(`File "`+e.Path+`" was deleted by `+who+`. What would you like to do?`, -1, 0,
			item{label: keep, run: stage},
			item{label: "Delete File", run: func(_ *Model) tea.Cmd {
				return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.Remove(root, e)} })
			}}, cancel)

		return nil

	case "UU", "AA":
		return func() tea.Msg {
			if !git.HasConflictMarkers(root, e.Path) {
				return stageMsg{root: root, run: stage}
			}

			return modalMsg{newMenu("Stage "+path.Base(e.Path)+" with merge conflicts?", -1, 0, item{label: "Stage", run: stage}, cancel)}
		}
	}

	return stage(m)
}

// stageConflicts is Stage All Merge Changes, or the Merge Changes folder
// dir: one question when any file still holds conflict markers. Deletion
// conflicts keep whatever the working tree has.
func (s *scmView) stageConflicts(_ *Model, root, dir string) tea.Cmd {
	uu, all := conflictPaths(s.status[root], dir)
	if len(all) == 0 {
		return flash("no merge conflicts", false)
	}

	stage := func(_ *Model) tea.Cmd {
		return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageFiles(root, all)} })
	}

	return func() tea.Msg {
		var unresolved []string

		for _, p := range uu {
			if git.HasConflictMarkers(root, p) {
				unresolved = append(unresolved, p)
			}
		}

		if len(unresolved) == 0 {
			return stageMsg{root: root, run: stage}
		}

		title := "Stage " + path.Base(unresolved[0]) + " with merge conflicts?"
		if len(unresolved) > 1 {
			title = fmt.Sprintf("Stage %d files with merge conflicts?", len(unresolved))
		}

		return modalMsg{newMenu(title, -1, 0, item{label: "Stage", run: stage},
			cancelItem())}
	}
}

func (s *scmView) stageUntracked(root string) func(*Model) tea.Cmd {
	return func(*Model) tea.Cmd {
		paths := untrackedPaths(s.status[root])
		if len(paths) == 0 {
			return flash("no untracked files", false)
		}

		return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.StageFiles(root, paths)} })
	}
}

// treeRows groups entries by directory like VS Code's tree view, compacting
// single-child directory chains ("src/pkg") into one row.
func treeRows(root, section string, entries []git.Entry, closed map[string]bool) []scmRow {
	byPath := make(map[string]git.Entry, len(entries))

	paths := make([]string, len(entries))
	for i, e := range entries {
		byPath[e.Path], paths[i] = e, e.Path
	}

	var (
		out  []scmRow
		walk func(t *ptree, dir string, depth int)
	)

	walk = func(t *ptree, dir string, depth int) {
		for _, name := range t.dirNames() {
			child, label, p := t.dirs[name], name, path.Join(dir, name)
			for len(child.files) == 0 && len(child.dirs) == 1 {
				for n, c := range child.dirs {
					label, p, child = label+"/"+n, path.Join(p, n), c
				}
			}

			out = append(out, scmRow{kind: rowDir, root: root, title: section, text: label, path: p, depth: depth})
			if !closed[sectionKey(root, section)+"/"+p] {
				walk(child, p, depth+1)
			}
		}

		for _, f := range t.sortedFiles() {
			out = append(out, scmRow{kind: rowFile, root: root, title: section, entry: byPath[f], depth: depth})
		}
	}
	walk(buildTree(paths), "", 0)

	return out
}

// drawerGeom is a drawer's rows within the pane area.
type drawerGeom struct {
	head, body, h int
	sep           bool // a rule above the header
}

func (s *scmView) paneH(m *Model) int { return m.bodyH(viewGit) }

// geometry lays out a pane area of height h: drawers sit at the bottom in a
// fixed order and the changes pane takes the rest, keeping at least 3 rows.
func (s *scmView) geometry(m *Model, h int) (changesH int, ds []drawerGeom) {
	drawers := m.drawers()
	ds = make([]drawerGeom, len(drawers))
	budget := h - 3

	for i := range ds {
		// Rules separate the drawers from the changes and an open drawer from the next header.
		ds[i].sep = i == 0 || m.pane(drawers[i-1].Title).Open
		budget -= 1 + b2i(ds[i].sep) // every drawer keeps its header row
	}

	used := 0

	for i, d := range drawers {
		// ponytail: earlier drawers win the height budget; VS Code shares it proportionally.
		if p := m.pane(d.Title); p.Open {
			ds[i].h = max(min(p.H, budget), 0)
			budget -= ds[i].h
		}

		used += 1 + b2i(ds[i].sep) + ds[i].h
	}

	changesH = max(h-used, 0)

	y := changesH
	for i := range ds {
		y += b2i(ds[i].sep)
		ds[i].head, ds[i].body = y, y+1
		y += 1 + ds[i].h
	}

	return changesH, ds
}

// changesEnd is the row index where the drawers begin.
func (s *scmView) changesEnd() int {
	if len(s.heads) > 0 {
		return s.heads[0]
	}

	return len(s.rows)
}

// drawerEnd is the row index after drawer j's lines.
func (s *scmView) drawerEnd(j int) int {
	if j+1 < len(s.heads) {
		return s.heads[j+1]
	}

	return len(s.rows)
}

// segment locates row i: its pane key, the pane's first row, row count and
// visible height. A drawer header reports h == 0.
func (s *scmView) segment(m *Model, i int) (key string, start, n, h int) {
	ch, ds := s.geometry(m, s.paneH(m))
	if i < s.changesEnd() {
		return "", 0, s.changesEnd(), ch
	}

	for j := len(s.heads) - 1; j >= 0; j-- {
		if i == s.heads[j] {
			return m.drawers()[j].Title, i, 0, 0
		}

		if i > s.heads[j] {
			return m.drawers()[j].Title, s.heads[j] + 1, s.drawerEnd(j) - s.heads[j] - 1, ds[j].h
		}
	}

	return "", 0, s.changesEnd(), ch
}

// nearest is the closest selectable row from t, looking in direction dir first.
func (s *scmView) nearest(t, dir int) int {
	for _, d := range []int{dir, -dir} {
		for j := t; j >= 0 && j < len(s.rows); j += d {
			if !s.rows[j].widget() {
				return j
			}
		}
	}

	return -1
}

func (s *scmView) move(m *Model, d int) tea.Cmd {
	if len(s.rows) == 0 || d == 0 {
		return nil
	}

	dir := 1
	if d < 0 {
		dir = -1
	}

	t := s.nearest(max(0, min(len(s.rows)-1, s.sel+d)), dir)
	if t < 0 {
		return nil
	}

	s.sel = t
	s.snap(m, t)

	return s.onSelect(m)
}

// snap scrolls row i's pane so the row is visible.
func (s *scmView) snap(m *Model, i int) {
	key, start, n, h := s.segment(m, i)
	if h == 0 {
		return
	}

	if key == "" && slices.Contains(s.pinned(s.tops[key], h), i) {
		return // on the sticky rows already
	}

	l := list{sel: i - start, top: s.tops[key]}
	l.snap(h)
	l.clamp(n, h)

	for key == "" && l.top > 0 && s.under(i, l.top, h) {
		l.top-- // not under the sticky rows either
	}

	s.tops[key] = l.top
}

// pinned are the rows drawn over the top of the changes pane once it is
// scrolled down: the head of the repository the first shown row is in
// (message box, the action button and the gaps between) and the header of its
// section, as VS Code's sticky scroll pins a tree's parents. The rows beneath
// scroll on unseen, so scrolling by one hides exactly one row.
// ponytail: tree folders do not stick; several repositories hand over with a
// jump when the next one's own rows reach the top.
func (s *scmView) pinned(top, h int) []int {
	var p []int
	for end := s.changesEnd(); top+len(p) < end; {
		next := s.ancestors(top + len(p))
		if len(next) < len(p) || slices.Equal(next, p) {
			break
		}

		p = next
	}

	if 2*len(p) > h { // a short pane scrolls as a plain list
		return nil
	}

	return p
}

// ancestors are the rows above row c that belong over it: its repository's
// head rows and, for a change, its section header.
func (s *scmView) ancestors(c int) []int {
	r := s.rows[c]

	first := c
	for first > 0 && s.rows[first-1].root == r.root {
		first--
	}

	var out []int
	for i := first; i < c && s.rows[i].kind != rowSection && (s.rows[i].kind == rowRepo || s.rows[i].kind == rowGap || s.rows[i].widget()); i++ {
		out = append(out, i)
	}

	if r.kind == rowFile || r.kind == rowDir {
		for i := c - 1; i >= first; i-- {
			if s.rows[i].kind == rowSection {
				out = append(out, i)
				break
			}
		}
	}

	return out
}

// spans reports the rows drawn edge to edge: the message box, the buttons and
// the gaps between them, which the scrollbar never runs beside.
// buttonOf is the action button row i belongs to: i itself, or a blank row
// right above or below it, where the button draws its taller edges. -1 when
// i is not part of a button.
func (s *scmView) buttonOf(i int) int {
	for _, j := range []int{i, i + 1, i - 1} {
		if j >= 0 && j < len(s.rows) && s.rows[j].kind == rowCommit && (j == i || s.rows[i].kind == rowGap) {
			return j
		}
	}

	return -1
}

func (s *scmView) spans(i int) bool { return s.rows[i].widget() || s.rows[i].kind == rowGap }

// under reports row i hidden beneath the sticky rows of a pane scrolled down.
func (s *scmView) under(i, top, h int) bool {
	p := s.pinned(top, h)
	return i >= top && i < top+len(p) && !slices.Contains(p, i)
}

// onSelect makes the selected row's repository active and points File
// History at a selected file.
func (s *scmView) onSelect(m *Model) tea.Cmd {
	r := s.selected()
	if r == nil {
		return nil
	}

	kind, file := r.kind, r.entry.Path

	cmd := s.setRepo(m, r.root)
	if kind != rowFile || file == s.history {
		return cmd
	}

	s.history = file

	if !m.pane("File History").Open {
		return cmd
	}

	return tea.Batch(cmd, s.loadDrawers(m))
}

func (s *scmView) loadDrawers(m *Model) tea.Cmd {
	root := s.root()
	if root == "" || !m.shown(viewGit) {
		return nil
	}

	var cmds []tea.Cmd

	for _, d := range m.drawers() {
		if !m.pane(d.Title).Open {
			continue
		}

		title, args, file := d.Title, d.Args, s.history

		cmds = append(cmds, func() tea.Msg {
			switch {
			case args != nil:
				return drawerMsg{root, title, git.Lines(root, args...)}
			case file == "":
				return drawerMsg{root, title, []string{"select a file to follow its history"}}
			}

			return drawerMsg{root, title, git.FileHistory(root, file)}
		})
	}

	return tea.Batch(cmds...)
}

func (s *scmView) onMsg(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case drawerMsg:
		if msg.root == s.root() {
			if len(msg.lines) > 300 {
				msg.lines = msg.lines[:300]
			}

			s.drawers[msg.title] = msg.lines
			s.build(m)
		}

	case modalMsg:
		m.modal = msg.menu
	case stageMsg:
		return msg.run(m)
	case suggestTickMsg:
		if s.busy != "suggesting" {
			return nil
		}

		s.frame++

		return suggestTick()

	case scmMsg:
		s.busy, s.busyRoot = "", ""

		var switched tea.Cmd
		if msg.worktree != "" && m.workspace(msg.worktree) != nil {
			switched = m.switchWorkspace(msg.worktree)
			msg.text = "opened the worktree of " + filepath.Base(msg.worktree)
		}

		if msg.message != "" && msg.root == s.root() {
			s.lastMsg = msg.message
			s.input.SetValue(msg.message)
			s.input.CursorEnd()
		}

		if msg.clear && msg.root == s.root() {
			s.input.Reset()
			s.last, s.lastMsg = nil, ""
		}

		s.fit(m)

		switch {
		case msg.err != nil:
			m.flash(msg.err.Error(), true)
		case msg.text != "":
			m.flash(msg.text, false)
		}

		var publish tea.Cmd
		if msg.publish && msg.root == s.root() {
			publish = s.publish(m)
		}

		return tea.Batch(switched, publish, m.refreshGit(), s.loadDrawers(m), s.saveDraft(m), m.pv.reloadIfLive(m))
	}

	return nil
}

func (s *scmView) lines(m *Model, w, h int) []string {
	if len(s.repos) == 0 {
		return []string{row(w, nil, []seg{sg(" No git repository in this workspace", dim)})}
	}

	hover := m.hoverRow(viewGit)
	ch, ds := s.geometry(m, h)

	out := s.renderPane(m, w, ch, "", 0, s.changesEnd(), hover)
	for j := range s.heads {
		if ds[j].sep {
			out = append(out, fg(pal.inputBorder).Render(strings.Repeat("─", w)))
		}

		out = append(out, s.renderRow(m, s.heads[j], w, hover == len(out)))
		y := len(out)
		out = append(out, s.renderPane(m, w, ds[j].h, m.drawers()[j].Title, s.heads[j]+1, s.drawerEnd(j), hover-y)...)
	}

	return out
}

func (s *scmView) renderPane(m *Model, w, h int, key string, start, end, hover int) []string {
	if h <= 0 {
		return nil
	}

	n := end - start
	l := list{sel: s.sel - start, top: s.tops[key]}
	l.clamp(n, h)
	s.tops[key] = l.top

	var pin []int

	skip := 0

	if key == "" {
		pin = s.pinned(l.top, h)
		skip = len(s.pinned(max(l.top, 1), h)) // the scrollbar runs beside the changes only
	}

	bar := n > h && w > 1

	s.hovRow = -1
	if hover >= 0 && hover < h && l.top+hover < n {
		s.hovRow = start + l.top + hover
		if hover < len(pin) {
			s.hovRow = pin[hover]
		}
	}

	return l.renderBar(w, h, n, skip, func(i, rw int) string {
		r := start + i
		if y := i - l.top; y < len(pin) {
			r = pin[y]
		}

		if bar && rw == w && !s.spans(r) { // the section header keeps its count over the files' letters
			return s.renderRow(m, r, w-1, i-l.top == hover) + " "
		}

		return s.renderRow(m, r, rw, i-l.top == hover)
	})
}

func (s *scmView) renderRow(m *Model, i, w int, hovered bool) string {
	r := s.rows[i]
	switch r.kind {
	case rowMsg:
		return s.messageRow(m, r.root, r.line, w, hovered)
	case rowGap, rowCommit:
		b := s.buttonOf(i)
		if b < 0 {
			return blank(w)
		}

		edge := map[int]string{b - 1: "▁", b: "", b + 1: "▔"}[i]

		return s.commitRow(r.root, w, s.hovRow >= 0 && s.buttonOf(s.hovRow) == b, s.mouseCol(m), edge)
	}

	bg, base := m.rowColors(viewGit, i == s.sel && !s.input.Focused(), hovered)

	indent := strings.Repeat("  ", r.depth)
	switch r.kind {
	case rowRepo:
		st := s.status[r.root]

		name := base.Bold(true)
		if r.root != s.root() {
			name = name.Faint(true)
		}

		branch := icBranch.s() + " " + headLabel(st)
		if st.Ahead+st.Behind > 0 {
			branch += fmt.Sprintf(" %d↑ %d↓", st.Ahead, st.Behind)
		}

		return row(w, bg, []seg{sg(" "+chevron(!s.closed[r.root]), base.Bold(true)), iconSeg("", true, false), sg(filepath.Base(r.root), name)},
			sg(" "+branch, dim), sg(" ⇅ ✓ ", dim))

	case rowSection:
		if bg == nil {
			bg = pal.sectionBg
		}

		var right []seg
		if hovered {
			right = s.actionSegs(m, r, w)
		}

		right = append(right, badge(r.text), sg(" ", plain))

		return row(w, bg, []seg{sg(" "+chevron(!s.closed[sectionKey(r.root, r.title)]), base.Bold(true)), sg(r.title, base.Bold(true))}, right...)

	case rowDir:
		open := !s.closed[sectionKey(r.root, r.title)+"/"+r.path]

		var right []seg
		if hovered {
			right = append(s.actionSegs(m, r, w), sg(" ", plain))
		}

		return row(w, bg, []seg{sg("   "+indent+chevron(open), dim), iconSeg(path.Base(r.path), true, open), sg(r.text, base)}, right...)

	case rowFile:
		name, dir := path.Base(r.entry.Path), path.Dir(r.entry.Path)
		if dir == "." || m.st.Settings.GitTree {
			dir = ""
		}

		c := statusColor(r.entry.Letter)
		nameSt := base.Foreground(c)

		switch r.entry.XY {
		case "", "UU", "AA", "AU", "UA":
			if r.entry.Letter == 'D' {
				nameSt = nameSt.Strikethrough(true)
			}

		default: // deleted on one side or both
			nameSt = nameSt.Strikethrough(true)
		}

		if r.entry.XY != "" && !m.st.Settings.GitTree {
			// VS Code's tooltip ("Conflict: Both Modified"), dimmed after the path.
			dir = strings.TrimSpace(dir + " · " + git.ConflictText(r.entry.XY))
		}

		var right []seg
		if hovered {
			right = s.actionSegs(m, r, w)
		}

		right = append(right, sg(" "+string(r.entry.Letter)+" ", base.Foreground(c)))

		return row(w, bg, []seg{sg("   "+indent, plain), iconSeg(name, false, false), sg(name, nameSt), sg(" "+dir, dim)}, right...)

	case rowDrawer:
		p := m.pane(r.title)

		left := []seg{sg(" "+chevron(p.Open), base.Bold(true)), sg(r.title, base.Bold(true))}
		if r.title == "File History" && s.history != "" {
			left = append(left, sg("  "+path.Base(s.history), dim))
		}

		var right []seg
		if p.Open {
			right = []seg{sg("⇕ ", dim)} // the header is the resize handle
		}

		return row(w, bg, left, right...)
	}

	st := base
	if r.title == "Branches" && strings.HasPrefix(r.text, "*") {
		st = st.Bold(true)
	}

	return row(w, bg, []seg{sg("   "+r.text, st)})
}

// commitPlaceholder is the message box's hint: during a merge or
// cherry-pick the message git prepared, which Continue commits when the box
// is left empty, as VS Code fills it in; otherwise how to commit.
func commitPlaceholder(st git.Status, width int) string {
	if first, _, _ := strings.Cut(st.MergeMsg, "\n"); first != "" {
		return ansi.Truncate(first, width, "…")
	}

	for _, t := range []string{`Message (⏎ to commit on "` + st.Branch + `")`, "Message (⏎ to commit)", "Message"} {
		if ansi.StringWidth(t) <= width {
			return t
		}
	}

	return ""
}

// edge is the message box border: VS Code's input border, accent on focus.
func (s *scmView) edge(root string) lipgloss.Style {
	if root == s.root() && s.input.Focused() {
		return fg(pal.accent)
	}

	return fg(pal.inputBorder)
}

// messageRow is line of a repository's message box like VS Code's input:
// a tinted field between thin edges, inset one column, with the ∨ of
// suggestMenu at the right end of its first line behind a ▏, split off like
// the Commit button's, and raised under the mouse like the rows' hover
// buttons. The ▏ draws on its cell's left edge, so the raised button starts
// at the line, not half a cell past it as it would behind a │.
func (s *scmView) messageRow(m *Model, root string, line, w int, hovered bool) string {
	active := root == s.root()
	box := lipgloss.NewStyle().Background(pal.inputBg)
	edge := s.edge(root).Background(pal.inputBg)

	suggesting := s.busy == "suggesting" && s.busyRoot == root

	sparkle := icChevron.s() // the ∨ opens suggestMenu
	if suggesting {
		sparkle = []string{"✦", "✧", "·", "✧"}[s.frame/3%4] // the star twinkles
	}

	if line > 0 {
		sparkle = blank(ansi.StringWidth(sparkle))
	}

	field := w - 7 - ansi.StringWidth(sparkle) // the text gets a space before it
	if field < 1 {
		return blank(w)
	}

	st := s.status[root]

	var text string

	switch {
	case suggesting && line == 0:
		text = scramble(suggestPhrase(field), s.frame, box)
	case suggesting:
	case active:
		s.styleInput(m.dark)
		s.input.Placeholder = commitPlaceholder(st, field)
		s.input.SetWidth(field)

		if lines := strings.Split(s.input.View(), "\n"); line < len(lines) {
			text = lines[line]
		}

	case m.st.Drafts[root] != "":
		first, _, more := strings.Cut(m.st.Drafts[root], "\n")
		if more {
			first += " …"
		}

		text = box.Render(ansi.Truncate(first, field, "…"))

	default:
		text = dim.Italic(true).Background(pal.inputBg).Render(commitPlaceholder(st, field))
	}

	if pad := field - ansi.StringWidth(text); pad > 0 {
		text += box.Render(blank(pad))
	} else {
		text = ansi.Truncate(text, field, "")
	}

	btn, sep, end := box, edge, edge
	if mx := s.mouseCol(m); hovered && line == 0 && !suggesting && mx >= w-4 && mx < w-1 { // click's hit box
		// The raised button runs into the edge: ▕ only draws the cell's right
		// sliver, so its own background would leave a gap before the border.
		btn, sep, end = keycapHot(), sep.Background(pal.keycapBg), end.Background(pal.keycapBg)
	}

	bar := sep.Render("▏") // splits the ∨ off as the Commit button does
	if line > 0 {
		bar = box.Render(" ")
	}

	return " " + edge.Render("▏") + box.Render(" ") + text + box.Render(" ") + bar + btn.Render(sparkle) + end.Render("▕") + " "
}

// suggestPhrase is what the scramble settles on, the longest that fits.
func suggestPhrase(width int) string {
	for _, t := range []string{"Generating commit message", "Generating message", "Generating", "…"} {
		if ansi.StringWidth(t) <= width {
			return t
		}
	}

	return ""
}

// scrambleGlyphs is the noise a scrambled character cycles through.
const scrambleGlyphs = `!#$%&*+-/<=>?@[\]^_{|}~01`

// scramble renders phrase at frame of a loop that mixes it out of ASCII
// noise: characters settle one by one (each a few frames off its neighbour),
// the phrase holds with dots counting up, then it dissolves back into noise.
// Settled characters take the accent, noise stays faint.
func scramble(phrase string, frame int, box lipgloss.Style) string {
	rs := []rune(phrase)
	n := len(rs)

	const jitter, hold, rest = 6, 24, 6

	period := 2*(n+jitter) + hold + rest
	p := frame % period
	dissolve := n + jitter + hold

	// hash is a stable pseudo-random number per character and frame, so a
	// frame renders the same twice and tests can pin it.
	hash := func(i, f int) int {
		h := uint32(i)*2654435761 ^ uint32(f)*40503 //nolint:gosec // wraparound is the point
		h ^= h >> 13
		h *= 0x5bd1e995
		h ^= h >> 15

		return int(h >> 1)
	}

	on := fg(pal.accent).Background(pal.inputBg).Bold(true)
	off := dim.Background(pal.inputBg)

	var b strings.Builder

	for i, r := range rs {
		at := i + hash(i, 0)%jitter
		settled := p >= at && p < dissolve+at

		switch {
		case settled:
			b.WriteString(on.Render(string(r)))
		case r == ' ' && hash(i, p)%3 == 0: // gaps keep the noise from reading as one word
			b.WriteString(box.Render(" "))
		default:
			b.WriteString(off.Render(string(scrambleGlyphs[hash(i, p)%len(scrambleGlyphs)])))
		}
	}

	if p >= n+jitter && p < dissolve {
		b.WriteString(on.Render(strings.Repeat(".", (p-n-jitter)/6%4)))
	}

	return b.String()
}

// Actions of the button under the message box, VS Code's SCM action button.
const (
	actCommit  = "commit"
	actPublish = "publish"
	actSync    = "sync"
)

// action picks what the button under the message box does, in VS Code's
// order: Commit while there is anything to commit or an operation to
// continue, else Publish Branch without an upstream, else Sync Changes when
// the branch is ahead or behind, else a Commit that has nothing to do.
func (s *scmView) action(root string) string {
	st := s.status[root]
	if s.busy != "" && s.busyRoot == root {
		return map[string]string{"publishing": actPublish, "syncing": actSync}[s.busy]
	}

	switch {
	case st.Op != "" || len(st.Staged)+len(st.Changes)+len(st.Conflicts) > 0:
		return actCommit
	case st.Branch != "" && st.Upstream == "":
		return actPublish
	case st.Upstream != "" && st.Ahead+st.Behind > 0:
		return actSync
	}

	return actCommit
}

// commitRow is VS Code's action button, inset like the message box: the
// split ✓ Commit with a separator and the ∨ menu, or Publish Branch / Sync
// Changes once nothing is left to commit. The hovered half of the button
// darkens, the label or the ∨ at mouse column mx; an inactive repository's
// button, or a Commit with nothing to commit, is muted. A non-empty edge
// draws the blank row above (▁) or below (▔) the button as a sliver of its
// colors, so it stands a few pixels taller than one cell.
func (s *scmView) commitRow(root string, w int, hovered bool, mx int, edge string) string {
	st, act, busy := s.status[root], s.action(root), s.busyRoot == root && s.busy != ""

	var text string

	switch {
	case act == actPublish && busy:
		text = icPublish.s() + " Publishing…"
	case act == actPublish:
		text = icPublish.s() + " Publish Branch"
	case act == actSync && busy:
		text = icSync.s() + " Syncing…"
	case act == actSync:
		text = icSync.s() + " Sync Changes"
		if st.Behind > 0 {
			text += fmt.Sprintf(" %d↓", st.Behind)
		}

		if st.Ahead > 0 {
			text += fmt.Sprintf(" %d↑", st.Ahead)
		}

	case st.Op != "" && busy:
		text = "Continuing…"
	case st.Op != "":
		text = icCheck.s() + " Continue" // VS Code's button while a merge or rebase waits
	case busy && s.busy == "committing":
		text = "Committing…"
	default:
		text = icCheck.s() + " Commit"
	}

	idle := act == actCommit && st.Op == "" && len(st.Staged)+len(st.Changes)+len(st.Conflicts) == 0
	bgc, fgc, sep := pal.buttonBg, pal.buttonFg, pal.buttonSep
	muted := root != s.root() || idle

	if muted {
		bgc, fgc, sep = pal.mutedButtonBg, pal.mutedButtonFg, pal.mutedButtonFg
	}

	style := lipgloss.NewStyle().Background(bgc).Foreground(fgc)
	onMenu := act == actCommit && w >= 12 && mx >= w-4 && mx < w-1 // click's hit box
	label, menu := style, style

	switch {
	case !hovered || muted:
	case onMenu:
		menu = menu.Background(pal.buttonHoverBg)
	default:
		label = label.Background(pal.buttonHoverBg)
	}

	// part is n cells of the button in style st: its text, or on an edge row
	// the sliver in the button's color.
	part := func(st lipgloss.Style, n int, str string) string {
		if edge != "" {
			return lipgloss.NewStyle().Foreground(st.GetBackground()).Render(strings.Repeat(edge, n))
		}

		return st.Render(str)
	}

	if w < 12 {
		return part(label, w, center(text, w))
	}

	if act != actCommit { // Publish and Sync have no menu, as in VS Code
		return " " + part(label, w-2, center(text, w-2)) + " "
	}

	bar := menu.Foreground(sep).Render("▏") // on the ∨'s left edge, where its hover starts
	if edge != "" {
		bar = part(menu, 1, "")
	}

	return " " + part(label, w-5, center(text, w-5)) + bar + part(menu, 2, icChevron.s()+" ") + " "
}

// press runs the action button of the active repository.
func (s *scmView) press(m *Model) tea.Cmd {
	switch s.action(s.root()) {
	case actPublish:
		return s.publish(m)
	case actSync:
		return s.sync(m)
	}

	return s.commit(m)
}

func (s *scmView) toggleTree(m *Model) tea.Cmd {
	return m.setSettings(map[string]any{"git_tree": !m.st.Settings.GitTree})
}

// collapseAll folds every tree directory, or every section in list mode.
func (s *scmView) collapseAll(m *Model) {
	for _, r := range s.rows {
		switch {
		case r.kind == rowDir:
			s.closed[sectionKey(r.root, r.title)+"/"+r.path] = true
		case r.kind == rowSection && !m.st.Settings.GitTree:
			s.closed[sectionKey(r.root, r.title)] = true
		}
	}

	s.build(m)
}

func (s *scmView) run(root, label string, fn func(root string) scmMsg) tea.Cmd {
	if root == "" || s.busy != "" {
		return nil
	}

	s.busy, s.busyRoot = label, root

	return func() tea.Msg { msg := fn(root); msg.root = root; return msg }
}

func (s *scmView) commit(_ *Model) tea.Cmd { return s.commitWith(false, false) }

// commitWith commits the active repository's message, optionally amending
// the last commit or syncing afterwards.
func (s *scmView) commitWith(amend, sync bool) tea.Cmd {
	root, msg := s.root(), strings.TrimSpace(s.input.Value())

	st := s.status[root]
	switch {
	case len(st.Conflicts) > 0:
		return flash("resolve the merge conflicts first", true)
	case amend && st.Op != "":
		return flash("cannot amend during a "+st.Op, true)
	case amend:
	case msg == "" && st.Op != "rebase" && st.MergeMsg == "": // Continue commits git's own message
		s.input.Focus()
		return flash("commit message is empty", true)

	case len(st.Staged) == 0 && st.Op == "":
		return flash("nothing staged (a stages all)", true)
	}

	s.input.Blur()

	return s.run(root, "committing", func(root string) scmMsg {
		commit := git.Commit

		switch {
		case amend:
			commit = git.Amend
		case st.Op != "":
			commit = func(root, msg string) error { return git.Continue(root, st.Op, msg) }
		}

		if err := commit(root, msg); err != nil {
			return scmMsg{err: err}
		}

		text := map[bool]string{false: "committed", true: "amended"}[amend]
		if st.Op != "" {
			text = st.Op + " continued"
		}

		if sync && st.Upstream == "" { // VS Code's Sync offers Publish Branch instead
			rs, err := git.Remotes(root)
			if err != nil {
				return scmMsg{err: err, clear: true}
			}

			if len(rs) != 1 {
				return scmMsg{text: text, clear: true, publish: true}
			}

			if err := git.Publish(root, rs[0].Name, ""); err != nil {
				return scmMsg{err: err, clear: true}
			}

			return scmMsg{text: text + " and published to " + rs[0].Name, clear: true}
		}

		if sync {
			if err := git.Sync(root); err != nil {
				return scmMsg{err: err, clear: true}
			}

			text += " and synced"
		}

		return scmMsg{text: text, clear: true}
	})
}

// suggestMenu is the message box's ∨: ways to have claude write the message.
// Rewrite needs text in the box, Regenerate a suggestion to redo.
func (s *scmView) suggestMenu(m *Model, x, y int) {
	with := func(o git.SuggestOpts) func(*Model) tea.Cmd {
		return func(*Model) tea.Cmd { return s.suggestWith(o) }
	}

	items := []item{
		{label: "Generate Commit Message", hint: "A", run: s.suggest},
		{label: "Generate with Description", run: with(git.SuggestOpts{Body: true})},
		{label: "Match Repository Style", run: with(git.SuggestOpts{Style: true})},
	}

	if cur := strings.TrimSpace(s.input.Value()); cur != "" {
		items = append(items, item{label: "Rewrite Current Message", run: with(git.SuggestOpts{Current: cur, Body: strings.Contains(cur, "\n")})})
	}

	if s.last != nil && s.lastMsg != "" {
		o := *s.last
		o.Avoid = s.lastMsg
		items = append(items, item{label: "Regenerate", run: with(o)})
	}

	m.modal = newMenu("", x, y, items...)
}

// commitMenu opens the Commit button's ∨ menu at x, y.
func (s *scmView) commitMenu(m *Model, x, y int) {
	m.modal = newMenu("", x, y,
		item{label: "Commit", hint: "C", run: s.commit},
		item{label: "Commit & Sync", run: func(*Model) tea.Cmd { return s.commitWith(false, true) }},
		item{label: "Commit (Amend)", run: func(*Model) tea.Cmd { return s.commitWith(true, false) }})
}

// suggest asks claude for a subject line.
func (s *scmView) suggest(_ *Model) tea.Cmd { return s.suggestWith(git.SuggestOpts{}) }

// suggestWith asks claude for a message shaped by o; the box scrambles until
// it lands.
func (s *scmView) suggestWith(o git.SuggestOpts) tea.Cmd {
	cmd := s.run(s.root(), "suggesting", func(root string) scmMsg {
		msg, err := git.Suggest(root, o)
		return scmMsg{message: msg, err: err}
	})
	if cmd == nil {
		return nil
	}

	o.Avoid = "" // Regenerate sets its own
	s.frame, s.last, s.lastMsg = 0, &o, ""

	return tea.Batch(cmd, suggestTick())
}

// sync pulls and pushes; a branch without an upstream is published instead,
// as VS Code's Sync offers.
func (s *scmView) sync(m *Model) tea.Cmd {
	if st := s.status[s.root()]; st.Branch != "" && st.Upstream == "" {
		return s.publish(m)
	}

	return s.run(s.root(), "syncing", func(root string) scmMsg { return scmMsg{text: "synced", err: git.Sync(root)} })
}

// publish is VS Code's Publish Branch: the only remote is pushed to, several
// are picked from (or a new one added), and without any the branch can go to
// a new GitHub repository through gh, as the GitHub extension offers.
func (s *scmView) publish(_ *Model) tea.Cmd {
	root := s.root()
	if root == "" {
		return flash("no git repository in this workspace", true)
	}

	if s.busy != "" {
		return nil
	}

	return func() tea.Msg {
		rs, err := git.Remotes(root)

		msg := remotesMsg{root: root, remotes: rs, err: err}
		if err == nil && len(rs) == 0 && git.HasGH() {
			msg.gh, msg.login = true, git.GHLogin()
		}

		return msg
	}
}

func (s *scmView) onRemotes(m *Model, msg remotesMsg) tea.Cmd {
	root, branch := msg.root, s.status[msg.root].Branch
	switch {
	case msg.err != nil:
		return flash(msg.err.Error(), true)
	case len(msg.remotes) == 1:
		return s.publishTo(root, msg.remotes[0].Name, "")
	case len(msg.remotes) == 0 && !msg.gh:
		return flash("your repository has no remotes configured to publish to (gh would publish to GitHub)", true)
	case len(msg.remotes) == 0:
		s.githubPicker(m, root, msg.login)
		return nil
	}

	items := make([]item, 0, len(msg.remotes)+1)
	for _, r := range msg.remotes {
		items = append(items, item{label: r.Name, detail: r.URL, run: func(_ *Model) tea.Cmd { return s.publishTo(root, r.Name, "") }})
	}

	items = append(items, item{label: icAdd.s() + " Add a new remote…", always: true, run: func(m *Model) tea.Cmd {
		return s.addRemote(m, root, branch)
	}})
	m.modal = newPicker(fmt.Sprintf("Pick a remote to publish the branch %q to", branch), items)

	return nil
}

func (s *scmView) publishTo(root, remote, url string) tea.Cmd {
	return s.run(root, "publishing", func(root string) scmMsg {
		return scmMsg{text: "published to " + remote, err: git.Publish(root, remote, url)}
	})
}

// addRemote asks for the URL and then the name, VS Code's Add Remote, and
// publishes branch to it.
func (s *scmView) addRemote(m *Model, root, branch string) tea.Cmd {
	m.modal = newPrompt("Remote URL", "", func(m *Model, url string) tea.Cmd {
		if url == "" {
			return nil
		}

		m.modal = newPrompt(fmt.Sprintf("Remote name to publish the branch %q to", branch), "", func(_ *Model, name string) tea.Cmd {
			if name == "" {
				return nil
			}

			return s.publishTo(root, name, url)
		})

		return nil
	})

	return nil
}

var repoNameRe = regexp.MustCompile(`[^a-zA-Z0-9_.]`)

// githubPicker is the GitHub extension's Publish to GitHub: the repository
// name is typed into the picker, prefilled with the folder's, and the two
// items pick its visibility.
func (s *scmView) githubPicker(m *Model, root, login string) {
	md := newPicker("Repository name to publish to GitHub", nil)
	md.input.SetValue(filepath.Base(root))
	md.input.CursorEnd()

	detail := "github.com"
	if login != "" {
		detail += "/" + login
	}

	pick := func(private bool) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			name := repoNameRe.ReplaceAllString(strings.TrimSpace(md.input.Value()), "-")
			if name == "" {
				m.modal = md
				return nil
			}

			return s.run(root, "publishing", func(root string) scmMsg {
				return scmMsg{text: "published to " + detail + "/" + name, err: git.PublishGitHub(root, name, private)}
			})
		}
	}
	md.items = []item{
		{label: "Publish to GitHub private repository", hint: detail, always: true, run: pick(true)},
		{label: "Publish to GitHub public repository", hint: detail, always: true, run: pick(false)},
	}
	md.refilter()
	m.modal = md
}

func (s *scmView) selected() *scmRow {
	if s.sel >= 0 && s.sel < len(s.rows) {
		return &s.rows[s.sel]
	}

	return nil
}

func (s *scmView) inputKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter":
		return s.commit(m)
	case "esc", "tab":
		s.input.Blur()
		return s.saveDraft(m)

	case "ctrl+c", "ctrl+shift+c", "ctrl+x": // the editor's copy and cut, on the selection only
		text := s.input.SelectedText()
		if text == "" {
			return nil
		}

		if k.String() == "ctrl+x" {
			s.input.DeleteSelection()
			s.fit(m)

			return setClipboard(text, "")
		}

		return setClipboard(text, fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)))
	}

	if s.busy == "suggesting" && s.busyRoot == s.root() {
		return nil // the suggestion replaces the box: typing now would be lost unseen
	}

	var cmd tea.Cmd

	s.input, cmd = s.input.Update(k)
	s.fit(m)

	return cmd
}

// focusMessage focuses the active repository's message box and scrolls it into view.
func (s *scmView) focusMessage(m *Model) tea.Cmd {
	for i, r := range s.rows {
		if r.kind == rowMsg && r.root == s.root() {
			s.snap(m, i)
		}
	}

	return s.input.Focus()
}

func (s *scmView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	r := s.selected()
	page, _ := s.geometry(m, s.paneH(m))
	page = max(page, 1)

	switch k.String() {
	case "up", "k":
		return s.move(m, -1)
	case "down", "j":
		return s.move(m, 1)
	case "g", "home":
		return s.move(m, -len(s.rows))
	case "G", "end":
		return s.move(m, len(s.rows))
	case "pgup":
		return s.move(m, -page)
	case "pgdown":
		return s.move(m, page)
	case "t":
		return s.toggleTree(m)
	case "l", "right":
		if r != nil && r.kind == rowDir && s.closed[sectionKey(r.root, r.title)+"/"+r.path] {
			return s.activate(m, r, false)
		}

	case "h", "left":
		if r != nil && r.kind == rowDir && !s.closed[sectionKey(r.root, r.title)+"/"+r.path] {
			return s.activate(m, r, false)
		}

	case "c":
		return s.focusMessage(m)
	case "C":
		return s.commit(m)
	case "A":
		return s.suggest(m)
	case "S":
		return s.sync(m)
	case "r":
		return tea.Batch(m.refreshGit(), s.loadDrawers(m))
	case "a":
		return s.stageAll(s.root())
	case "u":
		return s.run(s.root(), "unstaging", func(root string) scmMsg { return scmMsg{err: git.UnstageAll(root)} })
	case "U":
		return s.stageUntracked(s.root())(m)
	case "{", "}":
		if len(s.repos) > 1 {
			d := 1
			if k.String() == "{" {
				d = len(s.repos) - 1
			}

			return s.setRepo(m, s.repos[(s.repo+d)%len(s.repos)])
		}

	case "enter", "space":
		return s.activate(m, r, false)
	case "o":
		if r != nil && r.kind == rowFile {
			return m.openDiff(r.root, r.entry)
		}

	case "B":
		return s.branchPicker(m)
	case "O":
		if r != nil && r.kind == rowFile {
			return m.openFile(filepath.Join(r.root, r.entry.Path))
		}

	case "d":
		if r != nil && r.kind == rowFile && !r.entry.Staged && r.entry.Letter != '!' {
			return s.confirmDiscard(m, r.root, r.entry)
		}

	case "m":
		return s.menu(m, 0, m.bodyH(viewGit)/2)
	}

	return nil
}

// activate is ⏎ on a row (or a click when click is set): files stage or
// unstage (a click opens the diff), repos, sections and directories fold,
// drawers open, drawer lines open details.
func (s *scmView) activate(m *Model, r *scmRow, click bool) tea.Cmd {
	if r == nil {
		return nil
	}

	fold := func(key string) tea.Cmd {
		s.closed[key] = !s.closed[key]
		s.build(m)

		return nil
	}

	switch r.kind {
	case rowRepo:
		return fold(r.root)
	case rowSection:
		return fold(sectionKey(r.root, r.title))
	case rowDir:
		return fold(sectionKey(r.root, r.title) + "/" + r.path)
	case rowDrawer:
		p := m.pane(r.title)
		p.Open = !p.Open

		return tea.Batch(m.setPane(r.title, p), s.loadDrawers(m))

	case rowFile:
		e := r.entry
		if e.Letter == '!' {
			switch {
			case click && e.XY == "DD":
				return flash(e.Path+" was deleted on both sides", false)
			case click: // VS Code opens the conflicted file itself, not a diff
				return m.openFile(filepath.Join(r.root, e.Path))
			}

			return s.stageConflict(m, r.root, e)
		}

		if click {
			return m.openDiff(r.root, e)
		}

		if e.Staged {
			return s.run(r.root, "unstaging", func(root string) scmMsg { return scmMsg{err: git.Unstage(root, e)} })
		}

		return s.run(r.root, "staging", func(root string) scmMsg { return scmMsg{err: git.Stage(root, e)} })

	case rowLine:
		return s.lineAction(m, r)
	}

	return nil
}

// drawerMenu hides the drawer under the pointer or toggles any other, like
// a right click on a VS Code Source Control section.
func (s *scmView) drawerMenu(m *Model, title string, x, y int) tea.Cmd {
	toggle := func(name string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.toggleDrawer(name) }
	}

	var items []item
	if title != "" {
		items = append(items, item{label: "Hide '" + title + "'", run: toggle(title)}, item{})
	}

	shown := m.drawers()

	for _, d := range git.Drawers {
		mark := "  "
		if slices.ContainsFunc(shown, func(x git.Drawer) bool { return x.Title == d.Title }) {
			mark = icCheck.s() + " "
		}

		items = append(items, item{label: mark + d.Title, run: toggle(d.Title)})
	}

	return m.menuOf(items, x, y)
}

// branchPicker loads refs for VS Code's "Select a branch or tag to checkout".
func (s *scmView) branchPicker(_ *Model) tea.Cmd {
	root := s.root()
	if root == "" {
		return flash("no git repository in this workspace", true)
	}

	return func() tea.Msg {
		refs, err := git.Refs(root)
		return refsMsg{root, refs, err}
	}
}

func (s *scmView) onRefs(m *Model, msg refsMsg) tea.Cmd {
	if msg.err != nil {
		return flash(msg.err.Error(), true)
	}

	root, refs := msg.root, msg.refs
	md := s.refPicker(m, "Select a branch or tag to checkout", refs, func(_ *Model, r git.Ref) tea.Cmd {
		return s.checkout(root, refs, r, false)
	})
	actions := []item{
		{label: icAdd.s() + " Create new branch…", always: true, run: func(m *Model) tea.Cmd {
			return s.createBranch(m, root, md.input.Value(), "")
		}},
		{label: icAdd.s() + " Create new branch from…", always: true, run: func(m *Model) tea.Cmd {
			s.refPicker(m, "Select a ref to create the branch from", refs, func(m *Model, r git.Ref) tea.Cmd {
				return s.createBranch(m, root, "", r.Name)
			})

			return nil
		}},
		{label: icDetach.s() + " Checkout detached…", always: true, run: func(m *Model) tea.Cmd {
			s.refPicker(m, "Select a ref to checkout detached", refs, func(_ *Model, r git.Ref) tea.Cmd {
				return s.checkout(root, refs, r, true)
			})

			return nil
		}},
	}
	md.items = append(actions, md.items...)
	md.refilter()

	return nil
}

// refGroups orders the picker's refs the way VS Code groups them: local
// branches (the checked-out one first), then remotes, then tags.
var refGroups = map[string]string{"branch": "branches", "remote": "remote branches", "tag": "tags"}

// refPicker opens a picker of refs, each with its latest commit on a second row.
func (s *scmView) refPicker(m *Model, title string, refs []git.Ref, pick func(*Model, git.Ref) tea.Cmd) *modal {
	sorted := slices.Clone(refs)
	order := func(r git.Ref) int {
		switch {
		case r.Head:
			return 0
		case r.Kind == "branch":
			return 1
		case r.Kind == "remote":
			return 2
		}

		return 3
	}

	slices.SortStableFunc(sorted, func(a, b git.Ref) int { return cmp.Compare(order(a), order(b)) })

	items := make([]item, len(sorted))
	for i, r := range sorted {
		g, when := icBranch, r.When
		if r.Kind == "tag" {
			g = icTag
		}

		if r.Head {
			when = "current · " + when
		}

		items[i] = item{
			label: g.s() + " " + r.Name, inline: when, detail: r.Author + " • " + r.Hash + " • " + r.Subject,
			search: r.Name, group: refGroups[r.Kind], run: func(m *Model) tea.Cmd { return pick(m, r) },
		}
	}

	m.modal = newPicker(title, items)

	return m.modal
}

var worktreeRe = regexp.MustCompile(`(?:worktree at|checked out at) '([^']+)'`)

// checkout switches root to r. A remote branch with a local branch of the
// same name checks out the local one; a branch checked out in another
// worktree opens that worktree instead.
func (s *scmView) checkout(root string, refs []git.Ref, r git.Ref, detach bool) tea.Cmd {
	if _, local, ok := strings.Cut(r.Name, "/"); ok && r.Kind == "remote" && !detach {
		for _, x := range refs {
			if x.Kind == "branch" && x.Name == local {
				r = x
			}
		}
	}

	return s.run(root, "switching", func(root string) scmMsg {
		err := git.Checkout(root, r, detach)
		if sub := worktreeRe.FindStringSubmatch(fmt.Sprint(err)); err != nil && sub != nil {
			return scmMsg{text: r.Name + " is checked out in " + sub[1], worktree: sub[1]}
		}

		return scmMsg{text: "switched to " + r.Name, err: err}
	})
}

// createBranch asks for a name when there is none, then creates the branch
// at from (HEAD when "") and switches to it.
func (s *scmView) createBranch(m *Model, root, name, from string) tea.Cmd {
	if name = strings.TrimSpace(name); name == "" {
		title := "New branch name"
		if from != "" {
			title += " (from " + from + ")"
		}

		m.modal = newPrompt(title, "", func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}

			return s.createBranch(m, root, v, from)
		})

		return nil
	}

	return s.run(root, "creating a branch", func(root string) scmMsg {
		return scmMsg{text: "switched to a new branch " + name, err: git.CreateBranch(root, name, from)}
	})
}

var hashRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

func (s *scmView) lineAction(m *Model, r *scmRow) tea.Cmd {
	root := s.root()

	text := strings.TrimSpace(r.text)
	switch r.title {
	case "Graph", "Commits", "File History":
		if h := hashRe.FindString(r.text); h != "" {
			return m.openShow(root, h)
		}

	case "Stashes":
		ref, _, _ := strings.Cut(text, ":")
		return m.openShow(root, ref)

	case "Tags":
		return m.openShow(root, text)
	case "Branches":
		name := strings.Fields(strings.TrimPrefix(text, "*"))
		if len(name) == 0 || strings.HasPrefix(text, "*") {
			return nil
		}

		branch := name[0]
		m.modal = newMenu("Switch to "+branch+"?", -1, 0,
			item{label: "git switch " + branch, run: func(_ *Model) tea.Cmd {
				return s.run(root, "switching", func(root string) scmMsg {
					_, err := git.Run(root, "switch", strings.TrimPrefix(branch, "origin/"))
					return scmMsg{text: "switched to " + branch, err: err}
				})
			}},
			cancelItem())

	case "Worktrees":
		if f := strings.Fields(text); len(f) > 0 && m.workspace(f[0]) != nil {
			return tea.Batch(m.switchWorkspace(f[0]), m.refreshGit(), flash("workspace "+f[0], false))
		}
	}

	return nil
}

func (s *scmView) confirmDiscard(m *Model, root string, e git.Entry) tea.Cmd {
	m.modal = newMenu("Discard changes in "+e.Path+"?", -1, 0,
		item{label: "Discard", run: func(_ *Model) tea.Cmd {
			return s.run(root, "discarding", func(root string) scmMsg {
				return scmMsg{text: "discarded " + e.Path, err: git.Discard(root, e)}
			})
		}},
		cancelItem())

	return nil
}

func (s *scmView) confirmDiscardAll(m *Model, root string) tea.Cmd {
	m.modal = newMenu("Discard all changes to tracked files in "+filepath.Base(root)+"?", -1, 0,
		item{label: "Discard All Changes", run: func(_ *Model) tea.Cmd {
			return s.run(root, "discarding", func(root string) scmMsg {
				return scmMsg{text: "discarded changes", err: git.DiscardTracked(root)}
			})
		}},
		cancelItem())

	return nil
}

// items are the Source Control commands for the selection.
func (s *scmView) items(m *Model) []item {
	r := s.selected()

	var items []item

	if r != nil && r.kind == rowFile {
		e, root, row := r.entry, r.root, *r

		items = append(items, item{label: "Open Changes", hint: "o", run: func(m *Model) tea.Cmd { return m.openDiff(root, e) }},
			item{label: "Open File", hint: "O", run: func(m *Model) tea.Cmd { return m.openFile(filepath.Join(root, e.Path)) }})

		switch {
		case e.Letter == '!':
			items = append(items, item{label: "Stage", hint: "⏎", run: func(m *Model) tea.Cmd { return s.stageConflict(m, root, e) }})
		case e.Staged:
			items = append(items, item{label: "Unstage", hint: "⏎", run: func(m *Model) tea.Cmd { return s.activate(m, &row, false) }})
		default:
			items = append(items, item{label: "Stage", hint: "⏎", run: func(m *Model) tea.Cmd { return s.activate(m, &row, false) }},
				item{label: "Discard Changes…", hint: "d", run: func(m *Model) tea.Cmd { return s.confirmDiscard(m, root, e) }})
		}

		if e.Letter != 'D' {
			items = append(items, item{label: "Edit in $EDITOR", run: func(m *Model) tea.Cmd { return m.edit(filepath.Join(root, e.Path)) }})
		}
	}

	if r != nil && r.kind == rowDir {
		dir, root := git.Entry{Path: r.path}, r.root
		switch r.title {
		case "Merge Changes":
			items = append(items, item{label: "Stage Folder", run: func(m *Model) tea.Cmd { return s.stageConflicts(m, root, dir.Path) }})
		case "Staged Changes":
			items = append(items, item{label: "Unstage Folder", run: func(_ *Model) tea.Cmd {
				return s.run(root, "unstaging", func(root string) scmMsg { return scmMsg{err: git.Unstage(root, dir)} })
			}})

		default:
			items = append(items, item{label: "Stage Folder", run: func(_ *Model) tea.Cmd {
				return s.run(root, "staging", func(root string) scmMsg { return scmMsg{err: git.Stage(root, dir)} })
			}})
		}
	}

	items = append(items, item{label: "Switch Branch…", hint: "B", run: s.branchPicker},
		item{label: "History Drawers…", run: func(m *Model) tea.Cmd { return s.drawerMenu(m, "", 0, m.bodyH(viewGit)/2) }})

	mode := "View as Tree"
	if m.st.Settings.GitTree {
		mode = "View as List"
	}

	items = append(items,
		item{label: "Commit", hint: "C", run: s.commit},
		item{label: "Commit & Sync", run: func(*Model) tea.Cmd { return s.commitWith(false, true) }},
		item{label: "Commit (Amend)", run: func(*Model) tea.Cmd { return s.commitWith(true, false) }},
		item{label: icSparkle.text + " Suggest Message", hint: "A", run: s.suggest},
		item{label: "Stage All", hint: "a", run: func(m *Model) tea.Cmd { return s.key(m, tea.KeyPressMsg{Code: 'a', Text: "a"}) }},
		item{label: "Unstage All", hint: "u", run: func(m *Model) tea.Cmd { return s.key(m, tea.KeyPressMsg{Code: 'u', Text: "u"}) }},
		item{label: "Stage Untracked", hint: "U", run: func(m *Model) tea.Cmd { return s.stageUntracked(s.root())(m) }},
		item{label: "Stage All Merge Changes", run: func(m *Model) tea.Cmd { return s.stageConflicts(m, s.root(), "") }},
		item{label: "Sync", hint: "S", run: s.sync},
		item{label: "Publish Branch", run: s.publish},
		item{label: mode, hint: "t", run: s.toggleTree},
		item{label: "Collapse All", run: func(m *Model) tea.Cmd { s.collapseAll(m); return nil }})

	return items
}

func (s *scmView) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(s.items(m), x, y) }

func (s *scmView) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()
	_, click := msg.(tea.MouseClickMsg)

	_, wheel := msg.(tea.MouseWheelMsg)
	if (!click && !wheel) || len(s.repos) == 0 {
		return nil
	}

	ch, ds := s.geometry(m, s.paneH(m))

	key, start, n, py, h := "", 0, s.changesEnd(), y, ch
	if y >= ch {
		key = "-"

		for j, d := range ds {
			if j >= len(s.heads) {
				break
			}

			switch {
			case y == d.head && !click:
				return nil
			case y == d.head:
				s.input.Blur()

				s.sel = s.heads[j]
				if mo.Button == tea.MouseRight {
					return s.drawerMenu(m, m.drawers()[j].Title, mo.X, mo.Y)
				}

				m.drag = &drag{kind: dragPane, pane: m.drawers()[j].Title, y0: mo.Y, h0: d.h}

				return nil

			case y >= d.body && y < d.body+d.h:
				key, start, n, py, h = m.drawers()[j].Title, s.heads[j]+1, s.drawerEnd(j)-s.heads[j]-1, y-d.body, d.h
			}
		}

		if key == "-" {
			return nil
		}
	}

	l := list{top: s.tops[key]}
	if wheel {
		l.wheel(wheelDelta(mo), n, h)
		s.tops[key] = l.top

		return nil
	}

	i := l.at(py, n)
	if i < 0 {
		return nil
	}

	i += start
	if pin := s.pinned(l.top, h); key == "" && py < len(pin) {
		i = pin[py] // a sticky row answers for itself
	}

	if mo.Button == tea.MouseRight {
		if s.rows[i].widget() {
			return nil
		}

		s.sel = i

		return tea.Batch(s.onSelect(m), s.menu(m, mo.X, mo.Y))
	}

	w := m.colRect(m.colOf(viewGit)).w
	if n > h && w > 1 && (key != "" || py >= len(s.pinned(max(l.top, 1), h)) || !s.spans(i)) {
		w-- // the pane's scrollbar takes the last column, except beside the message box and buttons
	}

	return s.click(m, i, x, w, mo)
}

// click handles a left click on row i at column x of a row w cells wide.
func (s *scmView) click(m *Model, i, x, w int, mo tea.Mouse) tea.Cmd {
	if b := s.buttonOf(i); b >= 0 {
		i = b // the button's taller edges are the button
	}

	r := s.rows[i]
	if a, ok := hit(s.actions(r, w), x); ok {
		s.sel = i
		s.input.Blur()

		return tea.Batch(s.setRepo(m, r.root), a.run(m))
	}

	switch r.kind {
	case rowRepo:
		cmd := s.setRepo(m, r.root)

		switch {
		case x < 3:
			s.closed[r.root] = !s.closed[r.root]
			s.build(m)

		case x >= w-3:
			return tea.Batch(cmd, s.commit(m))
		case x >= w-5:
			return tea.Batch(cmd, s.sync(m))
		}

		return cmd

	case rowMsg:
		cmd := s.setRepo(m, r.root)
		if r.line == 0 && x >= w-4 && x < w-1 {
			s.suggestMenu(m, mo.X, mo.Y)
			return cmd
		}
		// A press places the cursor and starts a selection a drag extends;
		// the text starts after " ▏ ".
		if mo.Button == tea.MouseLeft && s.busy != "suggesting" {
			s.input.BeginSelection(x-3, r.line)
			m.drag = &drag{kind: dragMsgSel, x0: mo.X - x + 3, y0: mo.Y - r.line}
		}

		return tea.Batch(cmd, s.input.Focus())

	case rowCommit:
		cmd := s.setRepo(m, r.root)
		if x >= w-4 && x < w-1 && s.action(r.root) == actCommit {
			s.commitMenu(m, mo.X, mo.Y)
			return cmd
		}

		return tea.Batch(cmd, s.press(m))

	case rowGap:
		return nil
	}

	s.sel = i
	s.input.Blur()
	cmd := s.onSelect(m)

	return tea.Batch(cmd, s.activate(m, &s.rows[i], true))
}

// dragPane resizes an open drawer by its header, or toggles it on a plain click.
func (s *scmView) dragPane(m *Model, d *drag, y int, release bool) tea.Cmd {
	p := m.pane(d.pane)
	if !release {
		if y != d.y0 {
			d.moved = true
		}

		if p.Open && d.moved {
			p.H = max(1, min(d.h0+d.y0-y, s.paneH(m)))
			if m.st.Settings.GitPanes == nil {
				m.st.Settings.GitPanes = map[string]proto.Pane{}
			}

			m.st.Settings.GitPanes[d.pane] = p
		}

		return nil
	}

	m.drag = nil

	switch {
	case !d.moved:
		p.Open = !p.Open
		return tea.Batch(m.setPane(d.pane, p), s.loadDrawers(m))

	case p.Open:
		return m.setPane(d.pane, p)
	}

	return nil
}
