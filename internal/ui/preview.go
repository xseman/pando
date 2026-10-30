package ui

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/db"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// lexerCache holds lexers.Match by file name, nil where none matches: chroma
// tries the patterns of every lexer it has, which was 41% of a keystroke
// once an edit renders its line again.
var lexerCache sync.Map

// matchLexer is lexers.Match(name), asked once per name.
func matchLexer(name string) chroma.Lexer {
	if l, ok := lexerCache.Load(name); ok {
		lx, _ := l.(chroma.Lexer)
		return lx
	}

	l := lexers.Match(name)
	if l == lexers.Markdown {
		l = mdLexer
	}

	lexerCache.Store(name, l)

	return l
}

// langLexers holds lexers.Get by a code block's language: for a name it has
// no lexer by, chroma tries every lexer's file patterns, ~1 ms.
var langLexers sync.Map

// langLexer is lexers.Get(lang), asked once per name; nil for "".
func langLexer(lang string) chroma.Lexer {
	if lang == "" {
		return nil
	}

	if l, ok := langLexers.Load(lang); ok {
		lx, _ := l.(chroma.Lexer)
		return lx
	}

	l := lexers.Get(lang)
	if l == lexers.Markdown {
		l = mdLexer
	}

	langLexers.Store(lang, l)

	return l
}

const (
	maxTokenize     = 20000 // bytes of a line chroma colors, VS Code's maxTokenizationLineLength
	maxPreviewBytes = 1 << 20
	maxPreviewLines = 5000
)

// pos is a text position; col is a rune index into the plain line and may
// sit past the line end to keep the column across short lines.
type pos struct{ line, col int }

// vrow is one screen row: runes [from, to) of a line.
type vrow struct{ line, from, to int }

// The kinds a preview can show. The zero value, "", is no preview at all;
// only pvFile is editable.
const (
	pvFile = "file" // a file on disk, or an unsaved untitled buffer
	pvDiff = "diff" // one entry's working-tree or index diff
	pvShow = "show" // a commit, as `git show` prints it
	pvRev  = "rev"  // one revision of a file out of its history
	pvCmp  = "cmp"  // one file of a comparison: rev is the range, entry the file
	// A pull request or an issue as gh shows it, rendered as Markdown; rev
	// is "pr/12" or "issue/5".
	pvGH     = "gh"
	pvGHDiff = "ghdiff" // a pull request's changes, gh pr diff; rev is "pr/12"
)

// preview shows a file, a working-tree diff, or `git show` output with a
// text cursor and a selection made with shift+arrows or a mouse drag.
type preview struct {
	kind   string // one of the pv* constants
	path   string // file: absolute; diff: repo-relative; "" for an untitled buffer
	name   string // untitled buffer: "Untitled-1", what the tab shows until it is saved
	draft  string // unsaved text waiting for onLoad to put it back; cleared there
	draftM time.Time
	root   string
	entry  git.Entry
	rev    string
	gh     *ghDoc        // pvGH: the page's state, which its bar and buttons follow; nil until loaded
	tbl    *table        // pvTable: the grid, which the tab's copies share; nil until loaded
	set    *settingsPage // pvSettings: its section and row, which the tab's copies share
	// cell is the table cell an untitled editor holds the value of: ^s puts
	// it back there as an edit, and no draft keeps it.
	cell *cellRef
	// pvGH: what a click on each row of the page does, the key of the hits the
	// pointer lights, the comments folded, and the box at the end of it, which
	// has the keyboard while composing.
	ghHits    [][]ghHit
	hot       string
	folded    map[string]bool
	compose   *textarea.Model
	composing bool
	bodies    map[ghBodyKey][2][]string // the page's Markdown bodies, laid out
	raw       string
	err       string
	lines     []string   // styled, one per source line
	plain     [][]rune   // the same lines without styles
	meta      []lineMeta // diff rows: line numbers and change kind
	keep      *place     // the cursor's place while a diff reloads, to find again (keepPlace)
	oldHalf   bool       // side by side, the cursor is on the old half: its keys, drags and copies stay there
	// A diff fetched with full context (unfolds) shows its changes with
	// ctxRows around them: all holds every row, open the gaps unfolded, gaps
	// the rows of all hidden behind each gap row shown. fixed is a diff git
	// gave three lines of context: too long to unfold, or a merge's combined
	// diff.
	all   diffRows
	open  []gapLines
	gaps  map[int]shownRow
	fixed bool
	// probs are the errors and warnings on each row, set as view draws; disk
	// is a working-tree diff's file as on disk, which their columns count in.
	probs map[int][]probSpan
	disk  []string
	// probTip is the problem under the pointer, its tip shown while the
	// pointer stays on the screen cell where it found it.
	probTip *tipAt
	guide   indentGuides // the file's indent guides, set as view draws
	// guideOf is the text guide's levels were counted in: a frame over the
	// same lines (an edit makes new ones) counts them again only for the
	// cursor's active guide.
	guideOf  [][]rune
	guideTab int // the tab_size its unit fell back on
	// rowsNow are the rows drawn in this frame, rowsWas in the last, top to
	// bottom: a row drawn from what it was drawn from there takes that row
	// (renderRow).
	rowsNow, rowsWas []rowDrawn
	// base is a file's staged text, which its gutter marks the lines against
	// (chg, for the text chgKey names), VS Code's quick diff; nil for none.
	// head is its committed text when that differs, its staged changes then
	// marked against it, dimmer.
	base, head []string
	baseGen    int
	changes    []change
	chg        []byte
	chgKey     gutterKey
	markHot    int        // the change under the pointer in the gutter, plus one; 0 for none
	gutHot     bool       // the pointer is over the gutter, which shows the fold marks
	fold       folds      // the file's folded blocks
	qd         *quickDiff // the quick diff open under a change, nil for none
	numW       int        // digits of the line-number gutter
	ready      bool
	wrap       bool
	blanks     string // render_whitespace, put on as the editor draws
	top        int    // first visible screen row
	hl         []rune // the word under the cursor, painted wherever else it stands
	comp       completion
	hov        hover   // the language server's hover, by the pointer or under the cursor
	sig        sigHelp // the parameter hints of the call being typed
	left       int     // horizontal scroll in cells when not wrapping
	wide       int     // cells of the widest line, measured with the rows while not wrapping
	cur        pos
	anchor     *pos // selection start; nil = no selection
	reveal     bool // center the cursor once the content loads
	// transient is VS Code's preview editor, its tab in italics: the next
	// editor opened transient takes its tab over, until pin keeps it.
	transient bool
	used      int // when the editor was last shown, Model.edUsed's count: editor_limit closes the lowest
	// Markdown files: md is 0 for source, 1 rendered, 2 source and rendered
	// side by side. While rendered, lines and plain hold the rendering and
	// src and srcPlain the source.
	md                            int
	src                           []string
	srcPlain                      [][]rune
	mdW                           int // width mdLines were laid out for
	mdLines                       []string
	mdPlain                       []string
	mdBlocks                      *mdBlocks      // mdLines by block, kept across edits
	mdShown                       int            // width of the rendering in lines; 0 = lines hold the source
	repo                          string         // rev: the file's repository
	revs                          []git.Revision // rev: the file's history, newest first
	revIdx                        int            // rev: the revision shown
	vis                           []vrow
	visKey                        string
	lineRow                       []int   // first screen row of each line
	buf                           *buffer // the editable text of a file; nil for diffs and revisions
	trunc                         bool    // the file was too big to load whole, so it stays read-only
	ind                           indent  // the file's own indentation: .editorconfig, its lines, the status bar's picker
	find                          filter  // ⌃f box over the file; hits are selected one at a time
	hits                          []findHit
	hit                           int
	findFrom                      pos             // where the search started, so typing keeps matching forward
	findCase, findWord, findRegex bool            // the find widget's toggles
	repl                          textinput.Model // the replace box under the query, shown while replOn
	replOn                        bool            // ⌃h or the chevron opened the replace box
	findPreserve                  bool            // replace keeps each match's case, VS Code's AB toggle
	rangeHi                       int             // 1-based line Go to Line shows before it goes there, 0 for none
	typed                         bool            // the last thing to move the cursor was an edit: no occurrence highlight
	smart                         *smartSel       // expand/shrink selection chain; nil until the first press
	conf                          []conflict      // <<<<<<< blocks in the text, VS Code's merge-conflict decorations
	vim                           vimState        // vim_mode: the mode this editor is in and the keys still pending
}

type previewMsg struct {
	key, raw     string
	mod          time.Time
	trunc        bool
	lines, plain []string
	meta         []lineMeta
	numW         int
	err          error
	repo         string
	revs         []git.Revision
	gh           *ghDoc
	cached       bool     // from Model.ghPages, not fetched
	flipped      bool     // a diff's changes moved to or from the index: the tab follows them
	fixed        bool     // too long to unfold, or a merge's combined diff: git's three lines of context
	disk         []string // a working-tree diff's file, for its problems' columns
	base, head   []string // a file's staged text and, when it differs, its committed text
	ind          indent   // a file's own indentation
}

// diffRows are a rendered diff's rows, every one of them.
type diffRows struct {
	lines, plain []string
	meta         []lineMeta
}

func (p *preview) id() string {
	id := fmt.Sprintf("%s|%s|%s|%s|%v|%s", p.kind, p.root, p.path, p.name, p.entry.Staged, p.rev)

	switch c := p.cell; {
	case p.tbl != nil: // a table is its server's: an edited connection or .env names another since
		id += "|" + p.tbl.db.Target
	case c != nil: // a cell's value: its server, table, row and column, whatever row number it shows
		id += fmt.Sprintf("|%s|%s.%s|%s|%p|%s", c.tb.db.Target, c.tb.schema, c.tb.name, c.key, c.add, c.name)
	}

	return id
}

// untitled reports an editor with no file behind it yet: it types and saves
// like any other, but ⌃s asks where to put it.
func (p *preview) untitled() bool { return p.kind == pvFile && p.path == "" }

// fromGH reports a pull request or an issue gh fetched: no file behind it.
func (p *preview) fromGH() bool { return p.kind == pvGH || p.kind == pvGHDiff }

// label is the header's name and dimmed context, VS Code style.
func (p *preview) label(ws string) (name, context string) {
	switch p.kind {
	case pvSettings:
		return p.name, ""
	case pvTable:
		context = p.path
		if rel, err := filepath.Rel(ws, p.path); err == nil && !strings.HasPrefix(rel, "..") {
			context = rel
		}

		if p.tbl != nil {
			context += "  " + p.tbl.status()
		}

		return p.name, context

	case pvFile:
		if p.untitled() {
			return p.name, ""
		}

		context = filepath.Dir(p.path)
		if rel, err := filepath.Rel(ws, context); err == nil && !strings.HasPrefix(rel, "..") {
			context = rel
		}

		if context == "." {
			context = ""
		}

		return filepath.Base(p.path), context

	case pvDiff:
		kind := "working tree diff"

		switch {
		case p.entry.Staged:
			kind = "index diff"
		case p.entry.Letter == 'U':
			kind = "untracked file"
		case p.entry.Letter == '!':
			kind = "merge conflict, against ours"
		}

		if dir := filepath.Dir(p.path); dir != "." {
			return filepath.Base(p.path), dir + " — " + kind
		}

		return filepath.Base(p.path), kind

	case pvShow:
		return "git show " + p.rev, filepath.Base(p.root)
	case pvCmp:
		if dir := filepath.Dir(p.path); dir != "." {
			return filepath.Base(p.path), dir + " — " + p.rev
		}

		return filepath.Base(p.path), p.rev

	case pvGH:
		kind, n, _ := strings.Cut(p.rev, "/")
		context = "description"

		if p.gh != nil {
			context = p.gh.Title
		}

		return map[string]string{"pr": "PR", "issue": "Issue"}[kind] + " #" + n, context

	case pvGHDiff: // its own tab name: the description's tab is PR #n too
		return "PR #" + strings.TrimPrefix(p.rev, "pr/") + " changes", "gh pr diff"

	case pvRev:
		if p.revIdx >= len(p.revs) {
			return filepath.Base(p.path), "history"
		}

		r := p.revs[p.revIdx]

		context = r.Short + " · " + r.When + " · " + r.Subject
		if r.Hash == "" {
			context = "uncommitted changes"
		}

		return filepath.Base(p.path), fmt.Sprintf("%s  %d/%d", context, p.revIdx+1, len(p.revs))
	}

	return p.path, ""
}

// sameFile reports two editors of the same file: its content and its
// revisions share one tab.
func sameFile(a, b preview) bool {
	fileish := func(p preview) bool { return p.kind == pvFile || p.kind == pvRev }
	return a.path != "" && a.path == b.path && fileish(a) && fileish(b)
}

// snapshot is an editor without its content: reopening reloads it, while the
// cursor, scroll and Markdown mode come back.
func (p *preview) snapshot() preview {
	return preview{
		kind: p.kind, path: p.path, name: p.name, draft: p.draft, draftM: p.draftM,
		root: p.root, entry: p.entry, rev: p.rev, gh: p.gh, folded: p.folded, compose: p.compose, tbl: p.tbl, cell: p.cell, set: p.set,
		repo: p.repo, revs: p.revs, revIdx: p.revIdx, md: p.md,
		cur: p.cur, top: p.top, left: p.left, buf: p.buf, trunc: p.trunc, used: p.used,
		transient: p.transient, open: p.open, ind: p.ind, fold: folds{shut: slices.Clone(p.fold.shut)},
	}
}

// setPreview opens p in the main area, remembering the editor it replaces in
// the tab strip and the navigation history.
func (m *Model) setPreview(p preview) tea.Cmd {
	var focus tea.Cmd
	if q := m.holder(p.id()); q != nil { // its tab is in another group: that one opens it
		focus = m.focusGroup(q)
	}

	m.saveSpot()

	m.edUsed++
	p.used = m.edUsed

	i := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == p.id() })

	t := slices.IndexFunc(m.editors, func(e preview) bool { return e.transient })
	if t >= 0 && m.editors[t].dirty() { // a table edited in the preview tab stays, as an edit pins a file
		m.editors[t].transient = false
		t = -1
	}

	switch {
	case i >= 0:
		p.transient = p.transient && m.editors[i].transient // opened pinned, it stays pinned

		if !p.reveal && p.cur == (pos{}) { // reopening: back to where the cursor was
			e := m.editors[i]
			p.cur, p.top, p.left, p.md = e.cur, e.top, e.left, e.md
		}

		if e := m.editors[i]; e.tbl != nil { // the table it has open: its page and its edits
			p.tbl = e.tbl
		}

		if e := m.editors[i]; p.buf == nil { // its text, unsaved edits and all, and the indentation picked for it
			p.buf, p.ind = e.buf, e.ind
		}

		m.editors[i], m.edIdx = p.snapshot(), i

	case m.edIdx >= 0 && m.edIdx < len(m.editors) && sameFile(m.editors[m.edIdx], p):
		m.renameTab(m.editors[m.edIdx].id(), p.id())
		m.editors[m.edIdx] = p.snapshot() // a revision of the open file stays in its tab

	case p.transient && t >= 0:
		m.renameTab(m.editors[t].id(), p.id())
		m.editors[t], m.edIdx = p.snapshot(), t

	default:
		m.editors = append(m.editors, p.snapshot())
		m.edIdx = len(m.editors) - 1
		m.limitEditors()
	}

	if !m.restoring {
		m.navPush(p.snapshot())
	}

	if p.kind == pvFile && !m.restoring {
		m.files = slices.Insert(slices.DeleteFunc(m.files, func(f string) bool { return f == p.path }), 0, p.path)
	}

	if m.st.Settings.Reveal && p.path != "" { // a tab picked or restored too: a session switched to shows its file
		switch p.kind {
		case pvFile:
			m.autoReveal(p.path)
		case pvDiff, pvCmp: // the file changed, as VS Code reveals a diff's modified side
			m.autoReveal(filepath.Join(p.root, p.path))
		}
	}

	m.pv = p
	m.pv.wrap = m.st.Settings.Wrap

	// The session over the editor area keeps its tab beside the file's, as
	// VS Code's editor terminals do.
	m.preview = true
	tabW = m.tabWidth() // drawn at its own width from the start: the cursor it comes back with counts in it
	cmd := m.pv.load(m)
	m.saveSpot() // an untitled buffer loads in place: the tab takes it now

	return tea.Batch(focus, cmd)
}

// edLimits are the values Settings cycles editor_limit through; 0 is none.
var edLimits = []int{0, 5, 10, 20}

func edLimitHint(n int) string {
	if n <= 0 {
		return "off"
	}

	return strconv.Itoa(n)
}

// limitEditors closes the least recently shown editors while the strip holds
// more than editor_limit, VS Code's workbench.editor.limit. An unsaved one
// counts but stays — its tab going would take its draft along — and so does
// the active one; with nothing else to close the strip stays over the limit.
func (m *Model) limitEditors() {
	limit := m.st.Settings.EdLimit

	for limit > 0 && len(m.editors) > limit {
		lru := -1
		for i, e := range m.editors {
			if i != m.edIdx && !e.dirty() && (lru < 0 || e.used < m.editors[lru].used) {
				lru = i
			}
		}

		if lru < 0 {
			return
		}

		m.editors = slices.Delete(m.editors, lru, lru+1)
		if m.edIdx > lru {
			m.edIdx--
		}
	}
}

// pin keeps the active editor's tab, VS Code's Keep Editor: an edit or a
// double click on the tab or on what opened it.
func (m *Model) pin() {
	m.pv.transient = false
	m.saveSpot()
}

// openFile opens path; transient opens it in the preview tab, as a click in
// Explorer, Search or Source Control does.
func (m *Model) openFile(path string, transient bool) tea.Cmd {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return flash("cannot preview "+path, true)
	}

	if m.hasDB && st.Mode().IsRegular() && db.Has(db.SQLite) && db.IsSQLite(path) { // its tables, in the Databases view; a FIFO would block reading its header
		return m.revealDB(path)
	}

	return m.setPreview(preview{kind: pvFile, path: path, transient: transient})
}

// saveSpot remembers where the cursor is in the active editor and in the
// history, so coming back lands in the same place.
func (m *Model) saveSpot() {
	if m.pv.kind == "" {
		return
	}

	spot := m.pv.snapshot()
	if m.edIdx >= 0 && m.edIdx < len(m.editors) && m.editors[m.edIdx].id() == spot.id() {
		m.editors[m.edIdx] = spot
	}

	if m.navAt >= 0 && m.navAt < len(m.nav) && m.nav[m.navAt].id() == spot.id() {
		m.nav[m.navAt] = spot
	}
}

// groupSpec is the active group's strip as state.json keeps it: the files,
// each with its cursor and scroll, and the sessions. Diffs and commits depend
// on git state that moves on, so they are not remembered; nor is a session
// behind the files.
func (m *Model) groupSpec() proto.Editors {
	m.saveSpot()

	e := proto.Editors{Sessions: m.stripSessions()}
	if m.showsSession() {
		e.Session = m.sess
	}

	before := make([]int, len(m.editors)+1) // how many of Open come before editor i
	for i, p := range m.editors {
		before[i] = len(e.Open)

		if p.kind != pvFile && p.kind != pvRev || p.cell != nil { // a table cell's value goes with its table
			continue
		}

		if i == m.edIdx {
			e.Active = len(e.Open)
		}

		e.Open = append(e.Open, proto.Editor{
			Path: p.path, Name: p.name,
			Line: p.cur.line, Col: p.cur.col, Top: p.top, MD: p.md,
		})
	}

	before[len(m.editors)] = len(e.Open)

	for _, id := range e.Sessions {
		if _, ok := m.sessAfter[id]; ok {
			if e.SessionAt == nil {
				e.SessionAt = map[string]int{}
			}

			e.SessionAt[id] = before[m.sessPos(id)]
		}
	}

	return e
}

// saveEditors tells the daemon what the workspace has open when that changed
// since the last save, as saveDraft does for the commit message.
func (m *Model) saveEditors() tea.Cmd {
	if m.ws == "" {
		return nil
	}

	e := m.gridSpec(m.grid)
	e.Max = m.maxed
	// Against what this TUI saved, not the daemon's copy: two TUIs on one
	// workspace would otherwise rewrite each other's strip every tick.
	if reflect.DeepEqual(e, m.savedEds) {
		return nil
	}

	m.savedEds = e
	if m.st.Editors == nil {
		m.st.Editors = map[string]proto.Editors{}
	}

	if e.Empty() {
		delete(m.st.Editors, m.ws)
	} else {
		m.st.Editors[m.ws] = e
	}

	return do("state.set", map[string]any{"editors": map[string]proto.Editors{m.ws: e}})
}

// restoreEditors reopens what the workspace had open the last time, its
// groups and their tabs, cursors and scroll included, like VS Code reopening
// a folder.
func (m *Model) restoreEditors() tea.Cmd {
	e := m.st.Editors[m.ws]
	m.savedEds, m.maxed = e, false
	m.grid = newPane()
	m.cur = m.grid

	if e.A != nil && e.B != nil {
		return m.restoreGrid(e)
	}

	return m.restoreGroup(e)
}

// restoreGroup reopens group e in the active one: its sessions' tabs, the one
// it showed, and its files. Files that are gone are skipped; the active one
// loads, the others load when their tab is shown.
func (m *Model) restoreGroup(e proto.Editors) tea.Cmd {
	if m.sessTabs == nil {
		m.sessTabs = map[string][]string{}
	}

	m.sessTabs[m.ws] = slices.DeleteFunc(slices.Clone(e.Sessions), func(id string) bool { return m.session(id) == nil })
	if m.session(e.Session) != nil {
		m.sess, m.term = e.Session, term{}
	}

	m.editors, m.edIdx = nil, -1
	active := -1

	for i, o := range e.Open {
		if o.Path != "" {
			if st, err := os.Stat(o.Path); err != nil || st.IsDir() {
				continue
			}
		} else if o.Name == "" { // neither a file nor an untitled buffer
			continue
		}

		if i == e.Active {
			active = len(m.editors)
		}

		m.editors = append(m.editors, preview{
			kind: pvFile, path: o.Path, name: o.Name,
			cur: pos{o.Line, o.Col}, top: o.Top, md: o.MD,
		})
	}

	for id, n := range e.SessionAt { // back among the files it was moved to
		if slices.Contains(m.sessTabs[m.ws], id) {
			m.moveSessTab(id, m.editors[:min(n, len(m.editors))])
		}
	}

	if len(m.editors) == 0 {
		return nil
	}

	if active < 0 {
		active = len(m.editors) - 1
	}

	m.edIdx = active

	return m.restore(m.editors[active])
}

// navPush records a visited editor, dropping anything ahead of it.
func (m *Model) navPush(p preview) {
	m.nav = append(m.nav[:min(m.navAt+1, len(m.nav))], p)
	if len(m.nav) > 50 {
		m.nav = slices.Delete(m.nav, 0, 1)
	}

	m.navAt = len(m.nav) - 1
}

// navGo steps back (-1) or forward (1) through the visited editors.
func (m *Model) navGo(d int) tea.Cmd {
	i := m.navAt + d
	if i < 0 || i >= len(m.nav) {
		return flash("no more history", false)
	}

	m.saveSpot()
	m.navAt = i

	return m.restore(m.nav[i])
}

// restore reopens a remembered editor without recording it as a new step.
func (m *Model) restore(p preview) tea.Cmd {
	m.restoring = true
	defer func() { m.restoring = false }()

	return m.setPreview(p)
}

// showEditor activates tab i of the strip.
func (m *Model) showEditor(i int) tea.Cmd {
	if i < 0 || i >= len(m.editors) {
		return nil
	}

	if i == m.edIdx { // in front of the session shown over it, if one is
		m.preview = true
		return nil
	}

	m.saveSpot()

	return m.restore(m.editors[i])
}

// cycleEditor moves d tabs along the strip, wrapping around.
func (m *Model) cycleEditor(d int) tea.Cmd {
	if len(m.editors) < 2 {
		return nil
	}

	return m.showEditor((m.edIdx + d + len(m.editors)) % len(m.editors))
}

// closeEditor closes tab i; its neighbour takes over, or the preview closes.
// A dialog's choice finds the tab again: a submit closing another meanwhile
// moves it.
func (m *Model) closeEditor(i int) tea.Cmd {
	if i < 0 || i >= len(m.editors) {
		return m.dropEditor(i)
	}

	id := m.editors[i].id()
	again := func(m *Model, then func(i int) tea.Cmd) tea.Cmd {
		if i < len(m.editors) && m.editors[i].id() == id { // where it was: Save As can open a file twice
			return then(i)
		}

		if i := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == id }); i >= 0 {
			return then(i)
		}

		return nil
	}

	if m.editors[i].dirty() && m.editors[i].tbl != nil {
		tb := m.editors[i].tbl
		if tb.sending { // its edits are being written: nothing left to discard
			return flash(onItsWay, true)
		}

		m.modal = newDialog(fmt.Sprintf("Close %s with %s not submitted?", tb.name, plural(len(tb.edits), "change")),
			item{label: "Submit and close", run: func(*Model) tea.Cmd { return tb.submit(true) }},
			item{label: "Discard and close", run: func(m *Model) tea.Cmd { // Go Back may bring the tab back: its edits go
				tb.discard()
				return again(m, m.dropEditor)
			}},
			cancelItem())
		m.modal.area = m.editorArea()

		return nil
	}

	if m.editors[i].dirty() {
		name, _ := m.editors[i].label(m.ws)
		m.modal = newDialog("Close "+name+" without saving?",
			item{label: "Save and close", run: func(m *Model) tea.Cmd {
				return again(m, func(i int) tea.Cmd {
					// An untitled buffer has nowhere to go yet: ask where, and
					// drop the tab only once that save went through.
					switch {
					case m.editors[i].cell != nil: // putCell closes it itself
						return m.saveEditor(i)
					case m.editors[i].untitled(): // saved, it has another id
						return m.saveAsPrompt(i, func(m *Model) tea.Cmd { return m.dropEditor(i) })
					}

					return tea.Batch(m.saveEditor(i), m.dropEditor(i))
				})
			}},
			item{label: "Close anyway", run: func(m *Model) tea.Cmd {
				return again(m, func(i int) tea.Cmd { return tea.Batch(m.forgetDraft(m.editors[i]), m.dropEditor(i)) })
			}},
			cancelItem())

		return nil
	}

	return m.dropEditor(i)
}

// saveEditor saves tab i, which is not always the one on screen: a middle
// click closes a tab the preview is not showing.
func (m *Model) saveEditor(i int) tea.Cmd {
	if i < 0 || i >= len(m.editors) {
		return nil
	}

	if m.editors[i].id() == m.pv.id() {
		return m.pv.save(m, false)
	}

	e := &m.editors[i]
	if e.cell != nil {
		return m.putCell(e)
	}

	if err := e.buf.save(e.path, false); err != nil {
		return flash(err.Error(), true)
	}

	return tea.Batch(m.refreshGit(), flash("saved "+filepath.Base(e.path), false))
}

func (m *Model) dropEditor(i int) tea.Cmd {
	if i < 0 || i >= len(m.editors) {
		return m.pv.closeNow(m)
	}

	m.editors = slices.Delete(m.editors, i, i+1)
	if len(m.editors) == 0 {
		m.edIdx = -1
		return m.pv.closeNow(m)
	}

	m.edIdx = min(i, len(m.editors)-1)

	return m.restore(m.editors[m.edIdx])
}

// closeOtherEditors keeps the active editor, and those with unsaved text.
func (m *Model) closeOtherEditors() tea.Cmd {
	kept, active, dirty := m.editors[:0:0], 0, 0
	for i, e := range m.editors {
		switch {
		case i == m.edIdx:
			active = len(kept)
		case e.dirty():
			dirty++
		default:
			continue
		}

		kept = append(kept, e)
	}

	m.editors, m.edIdx = kept, active

	if dirty > 0 {
		return flash(fmt.Sprintf("kept %s with unsaved changes", plural(dirty, "editor")), false)
	}

	return nil
}

// closeEditors closes every editor but those with unsaved text, which would
// lose their drafts with their tabs — the rule closeOtherEditors follows.
func (m *Model) closeEditors() tea.Cmd {
	kept, dirty := m.editors[:0:0], 0
	for _, e := range m.editors {
		if e.dirty() {
			kept = append(kept, e)
			dirty++
		}
	}

	m.editors, m.edIdx = kept, len(kept)-1
	if dirty == 0 {
		return m.pv.closeNow(m)
	}

	return tea.Batch(m.restore(m.editors[m.edIdx]),
		flash(fmt.Sprintf("kept %s with unsaved changes", plural(dirty, "editor")), false))
}

// pathHeader reports a file whose header holds its path, as VS Code's
// breadcrumbs, rather than the ✕, the name and its context.
func (p *preview) pathHeader() bool { return p.kind == pvFile && !p.untitled() }

// crumb is a part of the path in the header: a folder, or the file last. A
// click lists dir, the folder holding it, with path, its own entry, selected.
type crumb struct {
	x, w      int
	dir, path string
}

// crumbFrom is how many leading folders of parts give way to a … for the
// path to fit room.
func crumbFrom(parts []string, room int) int {
	from := 0
	for len(parts)-from > 1 && ansi.StringWidth(strings.Join(parts[from:], " › "))+4*b2i(from > 0) > room {
		from++
	}

	return from
}

// crumbs draws the file's path, the folders muted and the file lit, and says
// where each part is.
func (p *preview) crumbs(m *Model, w int) (left []seg, hits []crumb) {
	name, context := p.label(m.ws)

	room := w - ansi.StringWidth(name) - 8
	for _, a := range p.buttons(m, w) {
		room -= a.w
	}

	var parts []string

	for _, s := range strings.Split(filepath.ToSlash(context), "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}

	dir := filepath.Dir(p.path)
	abs := make([]string, len(parts))

	for i, cur := len(parts)-1, dir; i >= 0; i, cur = i-1, filepath.Dir(cur) {
		abs[i] = cur
	}

	left, x, from := []seg{sg(" ", plain)}, 1, crumbFrom(parts, room)
	if from > 0 {
		left, x = append(left, sg("… › ", dim)), x+4
	}

	for i := from; i < len(parts); i++ {
		w := ansi.StringWidth(parts[i])
		hits = append(hits, crumb{x: x, w: w, dir: filepath.Dir(abs[i]), path: abs[i]})
		left, x = append(left, sg(parts[i]+" › ", dim)), x+w+3
	}

	icon := iconSeg(name, false, false)
	hits = append(hits, crumb{x: x, w: ansi.StringWidth(icon.s + name), dir: dir, path: p.path})

	return append(left, icon, sg(name, bold)), hits
}

// crumbClick opens the picker of the part of the path at header column x,
// screen column sx and row sy; false when none is there.
func (p *preview) crumbClick(m *Model, x, sx, sy int) bool {
	if !p.pathHeader() || !m.st.Settings.Crumbs {
		return false
	}

	_, hits := p.crumbs(m, m.mainW())

	i := slices.IndexFunc(hits, func(c crumb) bool { return x >= c.x && x < c.x+c.w })
	if i < 0 {
		return false
	}

	m.crumbMenu(hits[i], sx-(x-hits[i].x), sy+1)

	return true
}

// crumbMenu is VS Code's breadcrumb picker: the folder's entries under the
// part clicked, its own entry selected, folders opening in place. x, y is
// where it hangs on the screen.
func (m *Model) crumbMenu(c crumb, x, y int) {
	open := map[string]bool{}
	sel := 0
	md := &modal{x: x, y: y, keep: true}

	md.build = func(m *Model) []item {
		ex := explorer{expanded: open}
		ex.walk(m, c.dir, 0)

		items := make([]item, len(ex.nodes))
		for i, n := range ex.nodes {
			mark := "  "
			if n.dir {
				mark = map[bool]string{true: "▾ ", false: "▸ "}[open[n.path]]
			}

			if n.path == c.path {
				sel = i
			}

			items[i] = item{label: strings.Repeat("  ", n.depth) + mark + n.name, run: func(m *Model) tea.Cmd {
				if n.dir {
					open[n.path] = !open[n.path]
					return nil
				}

				m.modal = nil

				return m.openFile(n.path, false)
			}}
		}

		return items
	}

	md.items = md.build(m)
	md.refilter()
	md.focus(sel)
	m.modal = md
}

// tabDirty reports unsaved changes in editor i; the active one is m.pv, ahead
// of its snapshot in the strip.
func (m *Model) tabDirty(i int) bool {
	if i == m.edIdx {
		return m.pv.dirty()
	}

	return m.editors[i].dirty()
}

// edTab is one tab of the editor area's strip: an open file (i its editor),
// or a session over the editor area (i -1, id the session).
type edTab struct {
	i, x, w int
	label   string // " ", the icon, the name, the ✕ or ● mark
	icon    seg    // the icon in the label, drawn in its own color
	active  bool
	id      string      // a session's
	tip     string      // a session's whole name, when the label cuts it
	bg      color.Color // a session's session_highlight tint, else its glyph's (tabTint)
}

// editorTabs lays the strip out: the open files, then the sessions opened
// over the editor area, a session moved among the files where it went;
// tabs on the left give way until the active one fits.
func (m *Model) editorTabs(w int) []edTab {
	sessOn := m.showsSession()
	w -= ifElse(sessOn, m.sessBtnW(), m.stripBtnW()) + tabPlusW

	var items []edTab

	for i, e := range m.editors {
		name, _ := e.label(m.ws)

		var icon seg

		switch {
		case e.kind == pvTable:
			icon = sg(icTable.s()+" ", dim)
		case e.kind == pvSettings:
			icon = sg(icGear.s()+" ", dim)
		case e.kind != pvShow && !e.fromGH():
			icon = iconSeg(name, false, false)
		}

		active := i == m.edIdx && !sessOn
		label := " " + icon.s + name + " " + editorMark(active, e.id() == m.overTab, m.tabDirty(i))
		items = append(items, edTab{i: i, w: ansi.StringWidth(label), label: label, icon: icon, active: active})
	}

	pos := map[string]int{} // a session before the file at its sessPos

	for _, id := range m.stripSessions() {
		pos[id] = m.sessPos(id)
		s := *m.session(id)
		active := sessOn && id == m.sess
		label, tip := m.sessLabel(s, active || id == m.overTab, w-3)
		g, c := m.tabGlyph(s)
		items = append(items, edTab{
			i: -1, id: id, w: ansi.StringWidth(label), label: label, tip: tip, active: active,
			icon: ifElse(g == "", seg{}, sg(g, fg(c))), bg: cmp.Or(m.highlight(s), ifElse(g == "", nil, m.tabTint(c))),
		})
	}

	key := func(t edTab) int {
		if t.i >= 0 {
			return 2*t.i + 1
		}

		return 2 * pos[t.id]
	}

	slices.SortStableFunc(items, func(a, b edTab) int { return cmp.Compare(key(a), key(b)) })

	at, total := -1, 0

	for k, t := range items {
		if t.active {
			at = k
		}

		total += t.w + tabGap
	}

	start := 0
	for total > w && start < at {
		total -= items[start].w + tabGap
		start++
	}

	var out []edTab

	x := 0
	for _, t := range items[start:] {
		if x+t.w+tabGap > w {
			break
		}

		t.x = x
		out = append(out, t)
		x += t.w + tabGap
	}

	return out
}

// stripSessions are the sessions of the workspace in view opened over the
// editor area, in the order they were opened, the group's own among them:
// one a worktree switch or a restart showed without a tab keeps one behind
// the files too.
func (m *Model) stripSessions() []string {
	var out []string

	for _, id := range m.sessTabs[m.ws] {
		if m.session(id) != nil {
			out = append(out, id)
		}
	}

	if m.sess != "" && !slices.Contains(out, m.sess) {
		out = append(out, m.sess)
	}

	return out
}

// openSessTab puts session id's tab in its workspace's strip, once.
func (m *Model) openSessTab(id string) {
	s := m.session(id)
	if s == nil {
		return
	}

	if m.sessTabs == nil {
		m.sessTabs = map[string][]string{}
	}

	if !slices.Contains(m.sessTabs[s.Workspace], id) { // a new tab follows the files
		m.sessTabs[s.Workspace] = append(m.sessTabs[s.Workspace], id)
		delete(m.sessAfter, id)
	}
}

// closeSessTab is a session tab's ✕: the tab leaves the strip, the session
// running on, and the tab before it shows.
func (m *Model) closeSessTab(id string) tea.Cmd {
	if m.sess != id {
		m.sessTabs[m.ws] = slices.DeleteFunc(slices.Clone(m.sessTabs[m.ws]), func(r string) bool { return r == id })
		return nil
	}

	cmd := m.hideSession()
	if rest := m.stripSessions(); m.pv.kind == "" && len(rest) > 0 { // no file to show: the session before it
		return tea.Batch(cmd, m.switchSession(rest[len(rest)-1]))
	}

	return cmd
}

func (m *Model) editorStrip(w int) string {
	var segs []seg

	tabs := m.editorTabs(w)
	for _, t := range tabs {
		var chip []seg

		switch {
		case t.i < 0: // on its glyph's tint, apart from the files', the active one too
			chip = tabSegs([]sessTab{{id: t.id, label: t.label, active: t.active, bg: t.bg}}, m.overTab, m.overX)
			if t.active && t.bg != nil {
				chip[0].st = chip[0].st.Background(t.bg)
			}

		default:
			var bg color.Color
			if m.editors[t.i].id() == m.overTab {
				bg = pal.hoverBg
			}

			chip = tabChip(t.label, t.active, bg)
			if m.editors[t.i].id() == m.overX {
				chip, m.tip = closeHot(chip), ifElse(t.active, "Close  ^w", "Close")
			}

			if m.editors[t.i].transient {
				chip[0].st = chip[0].st.Italic(true).Bold(false)
			}
		}

		if t.icon.s != "" { // the icon keeps its color on the chip's background
			rest := strings.TrimPrefix(chip[0].s, " "+t.icon.s)
			icon := sgOwn(t.icon.s, t.icon.st.Background(chip[0].st.GetBackground()))
			chip = append([]seg{sgOwn(" ", chip[0].st), icon, sgOwn(rest, chip[0].st)}, chip[1:]...)
		}

		segs = append(segs, chip...)
	}

	plus, _ := m.actSegs(tabPlus(tabs), m.mainX(), m.mainY())
	segs = append(segs, plus...)

	var right []seg

	switch {
	case m.showsSession():
		if hint, st := m.sessionHint(); hint != "" && ansi.StringWidth(hint) <= w-m.sessBtnW()-edTabsEnd(tabs)-tabPlusW {
			right = append(right, sg(hint, st))
		}

		btns, _ := m.actSegs(m.sessionButtons(), m.mainX(), m.mainY())
		right = append(right, btns...)

		m.noteSessTabs(tabs)

	case m.buttonsUp():
		right, _ = m.pv.buttonSegs(m, w)
	}

	return m.stripMark(row(w, nil, segs, right...), stripEditor, 0)
}

// edTabsEnd is the column after the strip's last tab.
func edTabsEnd(tabs []edTab) int {
	if len(tabs) == 0 {
		return 0
	}

	t := tabs[len(tabs)-1]

	return t.x + t.w + tabGap
}

// tabPlusW is the room the strip's + takes after its tabs.
const tabPlusW = 3

// tabPlus is the strip's +, right after its last tab.
func tabPlus(tabs []edTab) []rowAction {
	return []rowAction{{g: icAdd, tip: "Open in a New Tab…  M-n", x: edTabsEnd(tabs), w: tabPlusW, run: (*Model).newTabMenu}}
}

// newTabMenu is the + beside the tabs, under it for a click and for a key: a
// file by quick open, a session of the space in view, or a new session in it.
func (m *Model) newTabMenu() tea.Cmd {
	items := []item{{label: "Open File…", hint: "^p", run: func(m *Model) tea.Cmd { return m.loadIndex(true) }}}
	if len(m.spaceSessions()) > 0 {
		items = append(items, item{label: "Open Session…", run: func(m *Model) tea.Cmd {
			m.modal = newPicker("Open Session", m.sessionItems(m.spaceSessions()))
			return nil
		}})
	}

	x := m.mainX() + tabPlus(m.editorTabs(m.mainW()))[0].x
	m.modal = newMenu("", x, m.mainY()+1, append(items, item{label: "New Session…", run: func(m *Model) tea.Cmd { return m.harnessPicker(m.ws) }})...)

	return nil
}

// noteSessTabs notes the tip of the session tab under the mouse: a ✕'s, or
// a cut name whole.
func (m *Model) noteSessTabs(tabs []edTab) {
	if m.mouseY != m.mainY() || m.mouseX < m.mainX() || m.mouseX >= m.mainX()+m.mainW() {
		return
	}

	for _, t := range tabs {
		switch {
		case t.i >= 0:
		case t.id != "" && t.id == m.overX:
			m.tip = "Close (keeps running)"
		case t.id != "" && t.id == m.overTab && t.tip != "":
			m.tip = t.tip
		}
	}
}

// buttonSegs draws the buttons, the one under the mouse and a lit toggle in
// their colors, and returns their width.
func (p *preview) buttonSegs(m *Model, w int) (segs []seg, bw int) {
	return m.actSegs(p.buttons(m, w), m.mainX(), m.mainY())
}

// actSegs draws a strip's buttons, the one under the mouse (the strip's row y
// on the screen, its left edge x0) and a lit toggle in their colors, and
// returns their width.
func (m *Model) actSegs(acts []rowAction, x0, y int) (segs []seg, bw int) {
	mx := m.mouseX - x0

	for _, a := range acts {
		st := dim

		switch {
		case m.mouseY == y && mx >= a.x && mx < a.x+a.w:
			st, m.tip = keycapHot(), a.tip
		case a.on:
			st = fg(pal.headerAccent) // lit: the accent color, no block
		}

		segs, bw = append(segs, sg(" "+a.g.s()+" ", st)), bw+a.w
	}

	return segs, bw
}

// editorRule is the line under the editor strip.
func (m *Model) editorRule(w int) string {
	var marks []ruleMark

	for _, t := range m.editorTabs(w) { // a group without the keyboard leaves its active tab unlit
		marks = append(marks, ruleMark{t.x, t.w, t.active && m.focus != offMain, m.tabID(t) == m.overTab})
	}

	if st, _, ok := m.ownFrame(); ok { // the top of the group's frame
		return markedRule(w, marks, "━", st)
	}

	return ruleRow(w, marks)
}

// tabID is tab t's id: its editor's, or its session's.
func (m *Model) tabID(t edTab) string {
	if t.i >= 0 {
		return m.editors[t.i].id()
	}

	return t.id
}

// stripMouse handles a click on the editor area's strip: the middle button
// and a tab's ✕ close, anything else activates, and the left button
// picks a file's tab up to drag it along the strip (a double click pins it);
// past the tabs a session's strip is its title.
func (m *Model) stripMouse(x int, mo tea.Mouse) tea.Cmd {
	button := mo.Button

	switch {
	case m.showsSession():
		if a, ok := hit(m.sessionButtons(), x); ok && button == tea.MouseLeft {
			return a.run(m)
		}

	case m.buttonsUp():
		if a, ok := hit(m.pv.buttons(m, m.mainW()), x); ok && button == tea.MouseLeft {
			return a.run(m)
		}
	}

	tabs := m.editorTabs(m.mainW())
	if a, ok := hit(tabPlus(tabs), x); ok && button == tea.MouseLeft {
		return a.run(m)
	}

	for _, t := range tabs {
		if x < t.x || x >= t.x+t.w {
			continue
		}

		if t.i < 0 {
			return m.sessTabClick(t, x, mo)
		}

		if button == tea.MouseRight {
			return nil
		}

		if button == tea.MouseMiddle || x >= t.x+t.w-tabCloseW {
			return m.closeEditor(t.i)
		}

		if button == tea.MouseLeft {
			m.grabTab(stripEditor, m.editors[t.i].id(), x)
		}

		cmd := m.showEditor(t.i)
		if button == tea.MouseLeft && m.clicks.double(x, 0) {
			m.pin()
		}

		return cmd
	}

	if m.showsSession() && mo.Button == tea.MouseRight { // the empty strip is the session's title
		return m.sessionTitleMenu()
	}

	return nil
}

// sessTabClick is a click on session tab t at strip column x: the right
// button opens its menu, the middle one or the ✕ closes it; else it shows.
func (m *Model) sessTabClick(t edTab, x int, mo tea.Mouse) tea.Cmd {
	switch {
	case mo.Button == tea.MouseRight:
		return m.sessionMenu(t.id)
	case mo.Button == tea.MouseMiddle || x >= t.x+t.w-tabCloseW:
		return m.closeSessTab(t.id)
	case mo.Button == tea.MouseLeft: // held, it goes to another group
		m.grabTab(stripEditor, t.id, x)
	}

	return m.switchSession(t.id)
}

// gotoLine moves the cursor to line and col (0-based), centering the view.
// Go to Line, a search hit, a symbol and a definition all land here, and all
// of them are the cursor being moved somewhere on purpose.
func (p *preview) gotoLine(m *Model, line, col int) {
	p.typed = false
	p.anchor, p.cur = nil, pos{max(min(line, p.lastLine()), 0), max(col, 0)}
	w, h := m.pvW(), m.pvH()
	p.top = max(p.cursorRow(w)-h/2, 0)
	p.follow(w, h)
}

// openFileAt previews path with the cursor on line (0-based) and n runes
// from col selected, as a search result opens.
func (m *Model) openFileAt(path string, line, col, n int, transient bool) tea.Cmd {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return flash("cannot preview "+path, true)
	}

	p := preview{kind: pvFile, path: path, cur: pos{line, col + n}, reveal: true, transient: transient}
	if n > 0 {
		p.anchor = &pos{line, col}
	}

	return m.setPreview(p)
}

func (m *Model) openDiff(root string, e git.Entry, transient bool) tea.Cmd {
	return m.setPreview(preview{kind: pvDiff, root: root, path: e.Path, entry: e, transient: transient})
}

// openCompare opens what file e of a Compare drawer changed across spec.
func (m *Model) openCompare(root, spec string, e git.Entry, transient bool) tea.Cmd {
	return m.setPreview(preview{kind: pvCmp, root: root, path: e.Path, rev: spec, entry: e, transient: transient})
}

func (m *Model) openShow(root, rev string) tea.Cmd {
	return m.setPreview(preview{kind: pvShow, root: root, rev: rev})
}

func (p *preview) load(m *Model) tea.Cmd {
	switch {
	case p.kind == "":
		return nil
	case p.kind == pvTable && p.tbl != nil && p.tbl.gen == 0: // read once: a tab shown again keeps its page and edits
		return p.tbl.load(0, false)
	case p.kind == pvTable:
		return nil
	case p.kind == pvSettings: // nothing to read: the settings are the model's
		p.ready = true
		return nil
	}
	// An untitled buffer has nothing on disk: it is its own source, so the
	// view is rebuilt from it rather than fetched. A restored draft arrives
	// here as text and becomes the buffer, dirty, with the mtime it was taken
	// at so a foreign write is still caught on save.
	if p.untitled() {
		if p.buf == nil {
			p.buf = newBuffer(p.draft, p.draftM)
			p.buf.dirty = p.draft != "" && p.cell == nil // a cell's value is what it holds, not an edit
		}

		p.draft, p.err, p.ready = "", "", true
		p.refreshAll(m)
		p.cur.line = min(p.cur.line, p.lastLine())
		p.revealCursor(m)

		return nil
	}

	p.closeComp()

	// A page or a diff from GitHub seen before shows at once, as it was; the
	// fetch below brings the news behind it, unless it was fetched within
	// ghTTL. ^r and every action still fetch.
	if c, ok := m.ghPages[p.id()]; ok && p.fromGH() && !p.ready {
		msg := previewMsg{key: p.id(), raw: c.raw, gh: c.doc, cached: true}
		msg.lines, msg.plain, msg.meta, msg.numW = render(p.kind, p.path, c.raw, m.dark)
		p.onLoad(m, msg)

		if time.Since(c.at) < ghTTL {
			return nil
		}
	}

	cp, dark, key, full := *p, m.dark, p.id(), m.st.Settings.DiffFull
	if p.draft != "" {
		cp.raw = "" // force a render: the draft, not the file, is what shows
	}

	return func() tea.Msg {
		msg := previewMsg{key: key}

		if cp.kind == pvRev && cp.revs == nil {
			root, err := git.Root(filepath.Dir(cp.path))
			if err == nil {
				rel, _ := filepath.Rel(root, cp.path)

				cp.revs, err = git.Revisions(root, rel)
				if err == nil && len(cp.revs) == 0 {
					err = fmt.Errorf("%s has no git history", filepath.Base(cp.path))
				}
			}

			if err != nil {
				msg.err = err
				return msg
			}

			cp.repo, msg.repo, msg.revs = root, root, cp.revs
		}

		var raw string

		var err error

		switch {
		case cp.kind == pvGH:
			kind, n, _ := strings.Cut(cp.rev, "/")
			raw, msg.gh, err = ghDocument(cp.root, kind, n)

		case cp.unfolds():
			raw, err = cp.fetch(true)
			if err == nil && !foldable(raw) {
				msg.fixed = true // git's three lines, or with z the whole file cut short

				if !full || cp.kind == pvShow {
					raw, err = cp.fetch(false)
				}
			}

		default:
			raw, err = cp.fetch(full)
		}

		if cp.kind == pvDiff && raw == "(no changes)" && cp.entry.Letter != 'U' && cp.entry.Letter != '!' {
			e := cp.entry
			e.Staged = !e.Staged

			if out, oerr := git.Diff(cp.root, e, full); oerr == nil && out != "" {
				raw, msg.flipped = out, true
			}
		}

		msg.raw, msg.err = raw, err

		if cp.kind == pvFile {
			if st, serr := os.Stat(cp.path); serr == nil {
				msg.mod = st.ModTime()
			}

			msg.base, msg.head = stagedText(cp.path)

			msg.trunc = strings.Contains(raw, "\n[truncated at 1 MiB]") ||
				strings.Count(raw, "\n") >= maxPreviewLines
		}

		if err == nil && raw != cp.raw {
			msg.lines, msg.plain, msg.meta, msg.numW = render(cp.kind, cp.path, raw, dark)

			if cp.kind == pvFile {
				msg.ind = fileIndent(cp.path, raw)
			}

			if cp.kind == pvDiff && cp.diagPath() != "" {
				msg.disk = fileLines(cp.diagPath())
			}
		}

		return msg
	}
}

// reloadIfLive refreshes files and diffs; `git show` output never changes.
func (p *preview) reloadIfLive(m *Model) tea.Cmd {
	if p.dirty() { // unsaved edits are the truth, not what is on disk
		return nil
	}

	if p.kind == pvDiff && p.ready { // an agent's edit above the cursor moves the rows under it
		p.keepPlace(m)
	}

	if p.kind == pvFile || p.kind == pvDiff {
		return p.load(m)
	}

	return nil
}

// dirty reports unsaved edits in this editor.
func (p *preview) dirty() bool {
	return p.buf != nil && p.buf.dirty || p.tbl != nil && len(p.tbl.edits) > 0
}

// tableBox reports a table whose WHERE or ORDER BY box has the keyboard.
func (p *preview) tableBox() bool { return p.kind == pvTable && p.tbl != nil && p.tbl.box > 0 }

// editable reports a file that can be typed into: not a diff, a revision, a
// rendering or a file too big to hold whole.
func (p *preview) editable() bool {
	return p.kind == pvFile && p.ready && p.buf != nil && !p.trunc && p.md != 1
}

func (p *preview) fetch(full bool) (string, error) {
	switch p.kind {
	case pvFile:
		f, err := os.Open(p.path)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()

		buf := make([]byte, maxPreviewBytes+1)
		n, _ := f.Read(buf)

		buf = buf[:n]
		if bytes.IndexByte(buf[:min(n, 8000)], 0) >= 0 { // a card instead, drawn as an error is: no text, no editing
			size := int64(n)
			if st, err := f.Stat(); err == nil {
				size = st.Size()
			}

			return "", errors.New(strings.Join(binaryCard(p.path, buf, size), "\n"))
		}

		s := string(buf)
		if n > maxPreviewBytes {
			s = s[:maxPreviewBytes] + "\n[truncated at 1 MiB]"
		}

		return s, nil

	case pvDiff:
		out, err := git.Diff(p.root, p.entry, full)
		if err == nil && out == "" {
			out = "(no changes)"
		}

		return out, err

	case pvShow:
		if full {
			return git.Run(p.root, "show", "--stat", "--patch", "--unified=1000000000", p.rev)
		}

		return git.Run(p.root, "show", "--stat", "--patch", p.rev)

	case pvCmp:
		out, err := git.CompareDiff(p.root, p.rev, p.entry, full)
		if err == nil && out == "" {
			out = "(no changes)"
		}

		return out, err

	case pvGHDiff:
		return git.GH(p.root, "pr", "diff", strings.TrimPrefix(p.rev, "pr/"))
	case pvRev:
		if p.revIdx >= len(p.revs) {
			return "", errors.New("no such revision")
		}

		out, err := git.RevisionDiff(p.repo, p.revs[p.revIdx], full)
		if err == nil && out == "" {
			out = "(no changes)"
		}

		return out, err
	}

	return "", nil
}

// render returns the styled lines, the same lines as plain text, and for
// diffs the per-row line numbers and change kinds.
func render(kind, path, raw string, dark bool) (styled, plainLines []string, meta []lineMeta, numW int) {
	raw = expandTabs(strings.ReplaceAll(raw, "\r", ""))
	if kind != pvFile && kind != pvGH {
		lines := parseDiff(raw)
		if len(lines) > maxPreviewLines {
			lines = lines[:maxPreviewLines]
		}

		return renderDiff(lines, path, dark)
	}

	plainLines = strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(plainLines) > maxPreviewLines {
		plainLines = append(plainLines[:maxPreviewLines:maxPreviewLines], fmt.Sprintf("[truncated at %d lines]", maxPreviewLines))
	}

	styled = slices.Clone(plainLines)

	// An untitled buffer stays plain until it is saved: edits render a line at
	// a time, and a guess per line would colour each its own way.
	lexer := matchLexer(filepath.Base(path))
	if lexer == nil && path != "" {
		lexer = lexers.Analyse(raw)
	}

	if lexer != nil {
		var b strings.Builder

		it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(tokenizable(plainLines), "\n"))
		if err == nil && formatters.Get("terminal16m").Format(&b, syntaxStyle(dark), refine(lexer, it)) == nil {
			if hl := strings.Split(b.String(), "\n"); len(hl) >= len(plainLines) {
				styled = untokenized(hl[:len(plainLines)], plainLines)
			}
		}
	}

	return styled, plainLines, nil, len(strconv.Itoa(len(plainLines)))
}

func (p *preview) onLoad(m *Model, msg previewMsg) {
	if msg.key != p.id() {
		return
	}

	if msg.revs != nil {
		p.revs, p.repo = msg.revs, msg.repo
	}

	if msg.flipped { // and so do the tab and the history that hold it
		for _, l := range [][]preview{m.editors, m.nav} {
			for i := range l {
				if l[i].id() == p.id() {
					l[i].entry.Staged = !l[i].entry.Staged
				}
			}
		}

		p.entry.Staged = !p.entry.Staged
	}

	if msg.err != nil {
		p.err, p.raw, p.keep = msg.err.Error(), "", nil
		return
	}

	p.err = ""
	if p.kind == pvFile && (!slices.Equal(p.base, msg.base) || !slices.Equal(p.head, msg.head) || (p.head == nil) != (msg.head == nil)) {
		p.base, p.head, p.baseGen = msg.base, msg.head, p.baseGen+1
	}

	if msg.gh != nil {
		p.gh, p.bodies = msg.gh, nil
	}

	if p.fromGH() && !msg.cached {
		if m.ghPages == nil {
			m.ghPages = map[string]ghCached{}
		}

		// ponytail: pages stay for the life of the TUI; drop the oldest if memory ever shows.
		m.ghPages[p.id()] = ghCached{raw: msg.raw, doc: msg.gh, at: time.Now()}
	}

	if msg.lines == nil { // the content is unchanged: nothing moved, the cursor may have
		p.keep = nil
		p.revealCursor(m)

		return
	}

	if p.keep != nil && p.cur != p.keep.was { // moved while it loaded: the cursor stays where it went
		p.keep = nil
	}

	opened := !p.ready && p.cur == (pos{}) && p.top == 0 // not a reload, nor a tab coming back to where it was
	p.raw, p.lines, p.meta, p.numW, p.ready, p.vis = msg.raw, msg.lines, msg.meta, msg.numW, true, nil

	p.plain = make([][]rune, len(msg.plain))
	for i, l := range msg.plain {
		p.plain[i] = []rune(l)
	}

	p.disk = msg.disk

	if p.fixed = p.fixed || msg.fixed; p.unfolds() && msg.meta != nil {
		p.all = diffRows{msg.lines, msg.plain, msg.meta}
		p.compact(m)
	} else { // git's own rows, a diff grown too long to fold among them: no gaps of an earlier load
		p.all, p.gaps = diffRows{}, nil
	}

	if p.kind == pvFile {
		if p.trunc = msg.trunc; !p.ind.picked {
			p.ind = msg.ind
		}

		switch {
		case p.dirty(): // unsaved edits survive a reload, a tab switch, a reopen
			p.draft = ""
			p.refreshAll(m)

		case p.draft != "": // and a restart: the draft the daemon kept comes back
			p.buf = newBuffer(p.draft, p.draftM)
			p.buf.dirty, p.draft = true, ""
			p.refreshAll(m)

		default:
			p.buf = newBuffer(msg.raw, msg.mod)
		}
	}

	p.src, p.srcPlain, p.mdLines, p.mdShown = p.lines, p.plain, nil, 0
	p.rehit() // the content is new; so are its matches
	p.syncMarkdown(m)
	p.cur.line = min(p.cur.line, p.lastLine())
	p.revealCursor(m)

	switch cs := changes(p.meta); {
	case p.keep != nil && p.meta != nil: // the rows moved: the same lines, on the same screen row
		p.findAgain(m)

	case opened && p.fileDiff() && len(cs) > 0: // VS Code opens a diff on its first change
		p.setLine(m, cs[0][0])

		if p.row(m) >= m.pvH() {
			p.scrollTo(m, m.pvH()/2)
		}
	}

	if p.anchor != nil {
		a := pos{min(p.anchor.line, p.lastLine()), p.anchor.col}
		p.anchor = &a
	}
}

// revealCursor centres a cursor that came from a search hit, a jump or a
// language server.
func (p *preview) revealCursor(m *Model) {
	if !p.reveal || !p.ready {
		return
	}

	p.reveal = false
	w, h := m.pvW(), m.pvH()
	p.cur.line = min(p.cur.line, p.lastLine())
	p.top = max(p.cursorRow(w)-h/2, 0)
	p.follow(w, h)
}

func (p *preview) lastLine() int { return max(len(p.plain)-1, 0) }

func (p *preview) lineLen(i int) int {
	if i >= 0 && i < len(p.plain) {
		return len(p.plain[i])
	}

	return 0
}

// at is the cursor with its column clamped to the line.
func (p *preview) at() pos { return pos{p.cur.line, min(p.cur.col, p.lineLen(p.cur.line))} }

// lineNumSt is a file's line number: faint gray, the cursor's line in the
// active color.
func lineNumSt(active bool) lipgloss.Style {
	c := pal.lineNumber
	if active {
		c = pal.lineNumberActive
	}

	if c == nil {
		return plain
	}

	return fg(c)
}

// numPad is the blank before a file's line numbers, VS Code's margin left of
// them, so the numbers do not sit against the sidebar.
const numPad = 1

// gutter is the width of the line-number column: ` n ┃ ` for files, the
// change mark in a lane of its own as VS Code's, `old new ± ` for diffs.
func (p *preview) gutter() int {
	switch {
	case p.kind == pvFile && p.md != 1:
		return numPad + p.numW + 3
	case p.meta != nil:
		return 2*p.numW + 4
	}

	return 0
}

// underlay is the color under a row's text, the whole width, nil for none:
// the line Go to Line is about to go to, a conflict block, a GitHub page's
// cursor (its row, as a list's), the cursor's line in a file, faintly.
func (p *preview) underlay(line int) color.Color {
	switch {
	case line == p.rangeHi-1:
		return pal.rangeHiBg
	case p.conflictBg(line) != nil:
		return p.conflictBg(line)
	case p.kind == pvGH && line == p.cur.line && !p.composing:
		return pal.hoverBg
	case p.lineHi(line):
		return pal.lineHiBg
	}

	return nil
}

// lineHi reports line drawn on the current line's background: the cursor's
// in a file shown as text, while nothing is selected.
func (p *preview) lineHi(line int) bool {
	if pal.lineHiBg == nil || p.kind != pvFile || p.md == 1 || line != p.cur.line {
		return false
	}

	_, _, sel := p.selection()

	return !sel
}

// rowBg is a diff row's tint, or a merge conflict's, nil for other rows.
func (p *preview) rowBg(line int) color.Color {
	if line < len(p.meta) {
		return diffBg(p.meta[line].kind)
	}

	return p.conflictBg(line)
}

func (p *preview) gutterText(line int, first bool) string {
	gw, bg := p.gutter(), p.rowBg(line)

	if bg == nil && p.lineHi(line) {
		bg = pal.lineHiBg
	}

	if gw == 0 {
		return ""
	}

	k := numKey{diff: p.meta != nil, bg: bg}
	if !k.diff { // VS Code's editorLineNumber colors, the cursor's line lit
		k.fg = ifElse(line == p.cur.line, pal.lineNumberActive, pal.lineNumber)
	}

	var st lipgloss.Style // a change mark's, built only for a row that has one
	if line < len(p.chg) && p.chg[line] != 0 {
		st = k.style()
	}

	pre, suf := numSides(k)

	// A file's change mark leads, in the margin before the numbers (numPad).
	switch sp := pre + " " + suf; {
	case !first && p.meta == nil: // a wrapped line's later rows keep its bar
		return p.changeMark(line, false, st, sp) + pre + blank(gw-numPad) + suf
	case !first:
		return pre + blank(gw) + suf
	case p.meta == nil:
		num := strconv.Itoa(line + 1) // %*d, without fmt on every row of every frame
		return p.changeMark(line, true, st, sp) + pre + blank(p.numW-len(num)) + num + " " + suf + p.foldMark(line, pre, suf, sp) + sp
	}

	return diffGutter(p.meta[line], p.numW, bg)
}

// numKey is what a gutter's number is drawn in: dim in a diff, else the
// line number color fg (nil: plain), on bg.
type numKey struct {
	diff   bool
	fg, bg color.Color
}

func (k numKey) style() lipgloss.Style {
	st := dim

	switch {
	case k.diff:
	case k.fg == nil:
		st = plain
	default:
		st = fg(k.fg)
	}

	if k.bg != nil {
		st = st.Background(k.bg)
	}

	return st
}

// numStyles holds renderSides of each numKey: the rows of a frame share a
// few, and a style built per row cost a tenth of the frame.
var numStyles sync.Map

func numSides(k numKey) (pre, suf string) {
	if v, ok := numStyles.Load(k); ok {
		s, _ := v.([2]string)
		return s[0], s[1]
	}

	pre, suf = renderSides(k.style())
	numStyles.Store(k, [2]string{pre, suf})

	return pre, suf
}

func runeCells(r rune) int {
	if r < utf8.RuneSelf {
		return 1 // tabs are already spaces
	}

	return ansi.StringWidth(string(r))
}

// tokenizable is lines with each one past maxTokenize blanked: such a line
// stays plain, as VS Code's editor.maxTokenizationLineLength leaves it;
// chroma takes a second over one of a minified bundle's, and again on each
// key typed in it.
func tokenizable(lines []string) []string {
	if !slices.ContainsFunc(lines, func(l string) bool { return len(l) > maxTokenize }) {
		return lines
	}

	out := slices.Clone(lines)
	for i, l := range out {
		if len(l) > maxTokenize {
			out[i] = ""
		}
	}

	return out
}

// untokenized puts the lines tokenizable blanked back into the colored rows
// of lines, plain.
func untokenized(rows, lines []string) []string {
	for i, l := range lines {
		if len(l) > maxTokenize {
			rows[i] = l
		}
	}

	return rows
}

// cutRow is the styled text of a line's runes lo to hi, off cells into it,
// its styles closed: the runes themselves for a line that has no styles, as a
// line too long to color has none.
func cutRow(styled string, line []rune, lo, hi, off int) string {
	if !strings.Contains(styled, "\x1b") && utf8.RuneCountInString(styled) == len(line) {
		return string(line[lo:hi])
	}

	return ansi.TruncateLeft(headCells(styled, off+cellsOf(line[lo:hi])), off, "")
}

// runesIn is how many of rs's runes start within its first n cells.
func runesIn(rs []rune, n int) int {
	c := 0

	for i, r := range rs {
		if c >= n {
			return i
		}

		c += runeCells(r)
	}

	return len(rs)
}

func cellsOf(rs []rune) int {
	n := 0
	for _, r := range rs {
		n += runeCells(r)
	}

	return n
}

// rows maps lines to screen rows for a main area w cells wide.
func (p *preview) rows(w int) []vrow {
	tw := max(w-p.gutter(), 1)

	zone, zh := -1, 0 // the quick diff's rows go under this line
	if p.qd != nil {
		zone, zh = p.zoneLine(), p.qd.h
	}

	key := fmt.Sprint(tw*b2i(p.wrap), p.wrap, zone, zh, p.fold.gen) // the width breaks only wrapped lines
	if p.vis != nil && p.visKey == key {
		return p.vis
	}

	p.vis, p.visKey, p.lineRow, p.wide = p.vis[:0], key, make([]int, len(p.plain)), 0
	for i := 0; i < len(p.plain); i++ {
		l := p.plain[i]
		p.lineRow[i] = len(p.vis)
		from, cells := 0, 0

		if !p.wrap {
			p.wide = max(p.wide, cellsOf(l))
		} else {
			for j, r := range l {
				c := runeCells(r)
				if cells+c > tw && j > from {
					p.vis = append(p.vis, vrow{i, from, j})
					from, cells = j, 0
				}

				cells += c
			}
		}

		p.vis = append(p.vis, vrow{i, from, len(l)})
		if i == zone {
			for k := range zh {
				p.vis = append(p.vis, vrow{-1, k, 0})
			}
		}

		if e := p.shutEnd(i); e > i { // a folded block: its lines take its header's row
			for j := i + 1; j <= e; j++ {
				p.lineRow[j] = p.lineRow[i]
			}

			i = e
		}
	}

	return p.vis
}

func (p *preview) cursorRow(w int) int {
	vis, c := p.rows(w), p.at()
	if c.line >= len(p.lineRow) {
		return 0
	}

	k := p.lineRow[c.line]
	for k+1 < len(vis) && vis[k+1].line == c.line && vis[k+1].from <= c.col {
		k++
	}

	return k
}

// colX is the cell offset of col within screen row vr's text area.
func (p *preview) colX(vr vrow, col int) int {
	if p.wrap {
		return cellsOf(p.plain[vr.line][vr.from:col])
	}

	return cellsOf(p.plain[vr.line][:col]) - p.left
}

// follow scrolls so the cursor stays on screen.
func (p *preview) follow(w, h int) {
	if len(p.plain) == 0 {
		return
	}

	p.cur.line = max(min(p.cur.line, p.lastLine()), 0) // edits can shorten the file
	p.unfoldAt(p.cur.line)                             // a jump into a folded block shows it

	if k := p.cursorRow(w); k < p.top {
		p.top = k
	} else if k >= p.top+h {
		p.top = k - h + 1
	}

	if !p.wrap {
		c, tw := p.at(), max(w-p.gutter(), 1)

		x := cellsOf(p.plain[c.line][:c.col])
		if x < p.left {
			p.left = x
		} else if x >= p.left+tw {
			p.left = x - tw + 1
		}
	}
}

// cursor is the terminal cursor position inside the preview body.
func (p *preview) cursor(w, h int) (x, y int, ok bool) {
	if !p.ready || p.err != "" || len(p.plain) == 0 || p.md == 1 || p.empty() { // a rendering is read, not edited: no cursor
		return 0, 0, false
	}

	k := p.cursorRow(w)
	x = p.gutter() + p.colX(p.rows(w)[k], p.at().col)
	y = k - p.top

	return x, y, y >= 0 && y < h && x >= p.gutter() && x < w
}

// posAt is the text position under body cell (x, y).
func (p *preview) posAt(w, x, y int) pos {
	vis := p.rows(w)
	if len(vis) == 0 {
		return pos{}
	}

	vr := vis[max(0, min(p.top+y, len(vis)-1))]
	if vr.line < 0 { // on the quick diff's zone: the end of the line it stands under
		return pos{p.zoneLine(), len(p.plain[p.zoneLine()])}
	}

	line, col, acc, cx := p.plain[vr.line], vr.from, 0, max(x-p.gutter(), 0)
	if !p.wrap {
		col, cx = 0, cx+p.left
	}

	for col < vr.to {
		c := runeCells(line[col])
		if acc+c > cx {
			break
		}

		acc += c
		col++
	}

	return pos{vr.line, col}
}

// selection returns the ordered, clamped selection; ok is false when empty.
// Vim's V selects whole lines with their line breaks, what its y and d take.
func (p *preview) selection() (a, b pos, ok bool) {
	if p.anchor == nil {
		return pos{}, pos{}, false
	}

	a, b = pos{p.anchor.line, min(p.anchor.col, p.lineLen(p.anchor.line))}, p.at()
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		a, b = b, a
	}

	if p.vim.mode == vimLine {
		a, b = pos{a.line, 0}, pos{b.line + 1, 0}
		if b.line > p.lastLine() { // the last line has no break to take
			b = pos{p.lastLine(), p.lineLen(p.lastLine())}
		}
	}

	return a, b, a != b
}

// selCount is the characters selected, as VS Code's status bar counts them:
// a tab is one, and so is each line break.
func (p *preview) selCount() int {
	a, b, ok := p.selection()
	if !ok {
		return 0
	}

	a, b = p.rawPos(a), p.rawPos(b)
	line := func(i int) int {
		if p.buf != nil {
			return len(p.buf.line(i))
		}

		return utf8.RuneCountInString(p.rowText(i))
	}

	if a.line == b.line {
		return b.col - a.col
	}

	n := line(a.line) - a.col + 1
	for i := a.line + 1; i < b.line; i++ {
		n += line(i) + 1
	}

	return n + b.col
}

// rowText is row i's text, a diff's gap the rows it hides: a selection
// over a gap holds them, as VS Code's over a collapsed region does.
func (p *preview) rowText(i int) string {
	if g := p.gaps[i]; g.n > 0 {
		return strings.Join(p.all.plain[g.i:g.i+g.n], "\n")
	}

	return string(p.plain[i])
}

func (p *preview) selectedText() string {
	a, b, ok := p.selection()
	if !ok {
		return ""
	}

	if a.line == b.line {
		return string(p.plain[a.line][a.col:b.col])
	}

	var sb strings.Builder
	sb.WriteString(string([]rune(p.rowText(a.line))[a.col:]))

	for i := a.line + 1; i < b.line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(p.rowText(i))
	}

	sb.WriteByte('\n')
	sb.WriteString(string(p.plain[b.line][:b.col]))

	return sb.String()
}

// smartSel is VS Code's smart select state: the chain of ranges around the
// selection a press started from, and where in it the last press stopped.
type smartSel struct {
	ranges [][2]pos // [0] is the selection the chain grew from; innermost first, end exclusive
	idx    int
	sel    [2]pos // what the last press selected; anything else means the user moved on
}

func posLE(a, b pos) bool { return a.line < b.line || (a.line == b.line && a.col <= b.col) }

func contains(r, s [2]pos) bool { return posLE(r[0], s[0]) && posLE(s[1], r[1]) }

// expandSel grows (d > 0) or shrinks the selection one step along the chain
// word → trimmed line → line → inside each bracket pair and with it → file.
// The chain is computed once; a cursor that moved on its own starts a new one.
func (p *preview) expandSel(m *Model, d int) tea.Cmd {
	if !p.ready || len(p.plain) == 0 || p.split(m) {
		return nil
	}

	a, b, ok := p.selection()
	if !ok {
		a, b = p.at(), p.at()
	}

	sel := [2]pos{a, b}
	if p.smart == nil || p.smart.sel != sel {
		p.smart = &smartSel{ranges: p.selRanges(sel)}
	}

	s := p.smart
	s.idx = max(min(s.idx+d, len(s.ranges)-1), 0)
	r := s.ranges[s.idx]

	p.anchor = nil
	if r[0] != r[1] {
		p.anchor = &r[0]
	}

	p.cur, s.sel = r[1], r
	p.follow(m.pvW(), m.pvH())

	return nil
}

// selRanges is VS Code's provideSelectionRanges without a language server:
// the word, the bracket pairs, the file, with the trimmed and the full line
// slipped in wherever the next range spans other lines.
func (p *preview) selRanges(sel [2]pos) [][2]pos {
	c := sel[1]

	var rs [][2]pos
	if a, b := wordRange(p.plain[c.line], c.col); a < b {
		rs = append(rs, [2]pos{{c.line, a}, {c.line, b}})
	}

	rs = append(rs, p.bracketRanges(sel)...)
	last := p.lastLine()
	rs = append(rs, [2]pos{{}, {last, p.lineLen(last)}})
	rs = slices.DeleteFunc(rs, func(r [2]pos) bool { return r == sel || !contains(r, sel) })
	// the candidates nest and arrive innermost first, so no sort is needed
	out := [][2]pos{sel}
	for _, r := range rs {
		prev := out[len(out)-1]
		if prev[0].line != r[0].line || prev[1].line != r[1].line {
			l0, l1 := p.plain[prev[0].line], p.plain[prev[1].line]

			e := len(l1)
			for e > 0 && unicode.IsSpace(l1[e-1]) {
				e--
			}

			trimmed := [2]pos{{prev[0].line, len(indentOf(l0))}, {prev[1].line, e}}

			full := [2]pos{{prev[0].line, 0}, {prev[1].line, len(l1)}}
			for _, l := range [][2]pos{trimmed, full} {
				if q := out[len(out)-1]; contains(l, q) && l != q && contains(r, l) && l != r {
					out = append(out, l)
				}
			}
		}

		if r != out[len(out)-1] {
			out = append(out, r)
		}
	}

	return out
}

var opener = map[rune]rune{')': '(', ']': '[', '}': '{'}

// bracketRanges are the ()[]{} pairs around sel, innermost first, each as its
// inside and then with the brackets: one pass over the text with a stack.
// ponytail: no lexer, so a bracket inside a string or a comment pairs like any
// other; the pass covers at most maxPreviewLines, once per chain.
func (p *preview) bracketRanges(sel [2]pos) [][2]pos {
	var (
		stack []pos
		out   [][2]pos
	)

	for l, line := range p.plain {
		for c, r := range line {
			switch r {
			case '(', '[', '{':
				stack = append(stack, pos{l, c})
			case ')', ']', '}':
				if len(stack) == 0 {
					continue
				}

				o := stack[len(stack)-1]
				if p.plain[o.line][o.col] != opener[r] {
					continue // unbalanced: skip the closer
				}

				stack = stack[:len(stack)-1]

				if incl := ([2]pos{o, {l, c + 1}}); contains(incl, sel) {
					out = append(out, [2]pos{{o.line, o.col + 1}, {l, c}}, incl)
				}
			}
		}
	}

	return out // a nested pair closes before its parent, so this is innermost first
}

// hlWord is the identifier under the cursor. VS Code paints every occurrence
// of it while nothing is selected, which is what shows a variable's or a
// method's other uses without asking a language server.
//
// It lights up when the cursor moves onto a name the file already uses, never
// while that name is being typed: VS Code cancels its word highlighter on
// every edit, and a word that stands nowhere else has nothing to show.
func (p *preview) hlWord() []rune {
	if _, _, ok := p.selection(); ok || !p.ready || len(p.plain) == 0 || p.typed {
		return nil
	}

	c := p.at()
	if c.line < len(p.meta) && p.meta[c.line].a == 0 && p.meta[c.line].b == 0 { // a diff's header or hunk row names no code
		return nil
	}

	a, b := wordRange(p.plain[c.line], c.col)
	if b-a < 2 { // a single letter matches half the file; VS Code is no keener
		return nil
	}

	w := p.plain[c.line][a:b]
	if !p.standsElsewhere(w, c.line, a) {
		return nil
	}

	return w
}

// standsElsewhere reports word standing as a whole word somewhere other than
// at line/col. It stops at the first one, so a name that is used answers at
// once and only a name used nowhere else pays for the whole file.
func (p *preview) standsElsewhere(word []rune, line, col int) bool {
	for li, l := range p.plain {
		for i := 0; i+len(word) <= len(l); i++ {
			if l[i] != word[0] || !slices.Equal(l[i:i+len(word)], word) {
				continue
			}

			j := i + len(word)
			if (i > 0 && isWordRune(l[i-1])) || (j < len(l) && isWordRune(l[j])) {
				continue // part of a longer word, not this one
			}

			if li != line || i != col {
				return true
			}

			i = j - 1
		}
	}

	return false
}

// wordSpans is where p.hl stands on line's runes lo to hi, as cell ranges
// from lo; a match the window's edge cuts shows its part inside it.
func (p *preview) wordSpans(line []rune, lo, hi int) [][2]int {
	if len(p.hl) == 0 {
		return nil
	}

	var spans [][2]int

	at, c := lo, 0 // c is the cells of line[lo:at], counted on as the scan moves: a minified line holds thousands of matches
	for i := max(lo-len(p.hl)+1, 0); i < hi && i+len(p.hl) <= len(line); i++ {
		if !slices.Equal(line[i:i+len(p.hl)], p.hl) {
			continue
		}

		j := i + len(p.hl)
		if (i > 0 && isWordRune(line[i-1])) || (j < len(line) && isWordRune(line[j])) {
			continue // part of a longer word, not this one
		}

		a, z := max(i, lo), min(j, hi)
		c += cellsOf(line[at:a])
		w := cellsOf(line[a:z])
		spans = append(spans, [2]int{c, c + w})
		at, c, i = z, c+w, j-1
	}

	return spans
}

// Whitespace markers, VS Code's editor.renderWhitespace: a space shows as ·,
// a tab as → on the first of the four cells it takes.
const spaceMark, tabMark = "·", "→"

// blankMarks is what render_whitespace draws over runes lo to hi of display
// line i: per rune, a marker, or "" to leave the rune alone. nil when nothing
// shows.
func (p *preview) blankMarks(i int, line []rune, lo, hi int) []string {
	if p.blanks == "" || p.blanks == "none" || p.kind != pvFile || p.md == 1 {
		return nil
	}

	marks, seen := make([]string, hi-lo), false

	for j, r := range line[lo:hi] {
		if r == ' ' {
			marks[j], seen = spaceMark, true
		}
	}

	if !seen {
		return nil
	}

	// The display holds tabW spaces for a tab; the buffer still has the tab.
	if p.buf != nil && i < len(p.buf.lines) {
		d := 0

		for _, r := range p.buf.line(i) {
			if d >= hi {
				break
			}

			if r != '\t' {
				d++
				continue
			}

			for k := max(d, lo); k < min(d+tabW, hi); k++ {
				marks[k-lo] = ifElse(k == d, tabMark, " ")
			}

			d += tabW
		}
	}

	first, last := 0, len(line) // the text between the leading and trailing blanks
	for first < len(line) && line[first] == ' ' {
		first++
	}

	for last > first && line[last-1] == ' ' {
		last--
	}

	a, z, sel := p.selection()

	for jj := range marks {
		j, keep := lo+jj, true

		switch p.blanks {
		case "trailing":
			keep = j >= last
		case "boundary": // all but a single space between words
			keep = marks[jj] != spaceMark || j < first || j >= last || line[j-1] == ' ' || line[j+1] == ' '
		case "selection":
			keep = sel && (i > a.line || i == a.line && j >= a.col) && (i < z.line || i == z.line && j < z.col)
		}

		if !keep {
			marks[jj] = ""
		}
	}

	return marks
}

// indentGuides are a file's indent guides, VS Code's: how many each line
// has, one every unit cells, and the one lit around the cursor.
type indentGuides struct {
	lv   []int
	unit int
	act  guides
	bar  [2]string // a guide, then the active one, off the selection
}

// indentGuides are the shown file's guides, none for a diff or rendered
// Markdown. A line has as many as its indentation takes units, rounded up;
// a blank line takes them from the lines around it, as VS Code's
// getLinesIndentGuides does. The unit is the step most lines indent by, as
// VS Code's detectIndentation guesses it, else tab_size.
func (p *preview) indentGuides() indentGuides {
	if p.kind != pvFile || p.md == 1 || p.meta != nil {
		return indentGuides{}
	}

	bar := [2]string{renderText(fg(pal.whitespace), "│"), renderText(fg(pal.indentGuide), "│")}
	if lv := p.guide.lv; sameLines(p.guideOf, p.plain) && p.guideTab == tabW && len(lv) == len(p.plain) {
		return indentGuides{lv, p.guide.unit, activeGuide(len(lv), p.at().line, func(i int) int { return lv[i] }), bar}
	}

	p.guideOf, p.guideTab = p.plain, tabW

	in, steps, prev := make([]int, len(p.plain)), map[int]int{}, 0 // leading blanks, -1 for a blank line
	for i, l := range p.plain {
		n := 0
		for n < len(l) && l[n] == ' ' {
			n++
		}

		in[i] = ifElse(n == len(l), -1, n)
		if in[i] < 0 {
			continue
		}

		if d := max(in[i]-prev, prev-in[i]); d > 0 && d <= 8 {
			steps[d]++
		}

		prev = in[i]
	}

	unit := tabW
	for d, n := range steps {
		if n > steps[unit] || n == steps[unit] && d < unit {
			unit = d
		}
	}

	lv := make([]int, len(in))
	for i := 0; i < len(in); {
		if in[i] >= 0 {
			lv[i] = (in[i] + unit - 1) / unit
			i++

			continue
		}

		j := i // a run of blank lines, i to j
		for j < len(in) && in[j] < 0 {
			j++
		}

		n := 0 // at the top or the bottom of the file: none

		switch above, below := ifElse(i > 0, in[max(i-1, 0)], -1), ifElse(j < len(in), in[min(j, len(in)-1)], -1); {
		case above < 0 || below < 0:
		case above < below: // inside the block below the line above
			n = 1 + above/unit
		case above == below: // between two blocks
			n = (below + unit - 1) / unit
		default: // inside the block that ends below
			n = 1 + below/unit
		}

		for ; i < j; i++ {
			lv[i] = n
		}
	}

	return indentGuides{lv, unit, activeGuide(len(lv), p.at().line, func(i int) int { return lv[i] }), bar}
}

// sameLines reports a and b to be one slice: the lines are never changed in
// place, an edit puts new ones in (buffer.go's splice).
func sameLines(a, b [][]rune) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// guideRoom clears render_whitespace's marks off row vr's guide cells, as
// VS Code draws both: a tab's arrow moves to the tab's next cell, a space's
// dot gives way.
func (p *preview) guideRoom(vr vrow, marks []string, lo int) []string {
	if vr.line >= len(p.guide.lv) || vr.from > 0 {
		return marks
	}

	for k := 1; k < p.guide.lv[vr.line]; k++ { // the top level has none (paintGuides); marks start at rune lo, indentation a cell each
		if c := k*p.guide.unit - lo; c >= 0 && c < len(marks) {
			if marks[c] == tabMark && c+1 < len(marks) && marks[c+1] == " " {
				marks[c+1] = tabMark
			}

			marks[c] = ""
		}
	}

	return marks
}

// paintGuides draws a row's indent guides over body, its cells start to
// end: lv levels unit cells apart, a │ at each past the first, level lit in
// indent_guide's color, a selected one (s0 to s1) on the selection; the top
// level's would be a rule down the code's left edge. bar is the guide, then
// the lit one.
func paintGuides(body string, lv, unit, lit int, bar [2]string, start, end, s0, s1 int) string {
	if lv < 2 {
		return body
	}

	bw := strWidth(body)

	for k := 1; k < lv; k++ {
		c := k * unit
		if c < start || c >= end {
			continue
		}

		bar := bar[b2i(k == lit)]
		if c >= s0 && c < s1 {
			bar = renderText(fg(ifElse(k == lit, pal.indentGuide, pal.whitespace)).Background(pal.textSelBg), "│")
		}

		x := c - start
		if bw <= x { // a blank line's guides run past its text
			body, bw = body+blank(x-bw+1), x+1
		}

		body = cutHead(body, x) + "\x1b[m" + bar + ansi.TruncateLeft(body, x+1, "")
	}

	return body
}

// markBlanks draws marks over a styled line's blanks in st; the text around
// keeps its own style.
func markBlanks(styled string, line []rune, marks []string, st lipgloss.Style) string {
	if marks == nil {
		return styled
	}

	var (
		b       strings.Builder
		run     strings.Builder
		at, c   int // cells written, and where rune j starts
		runFrom = -1
	)

	flush := func() {
		if runFrom < 0 {
			return
		}

		b.WriteString(ansi.Cut(styled, at, runFrom))
		b.WriteString("\x1b[m")
		b.WriteString(renderText(st, run.String()))

		at, runFrom = c, -1

		run.Reset()
	}

	for j, r := range line {
		if j >= len(marks) || marks[j] == "" { // past the marks: a folded header's ⋯ after the text
			flush()
		} else {
			if runFrom < 0 {
				runFrom = c
			}

			run.WriteString(marks[j])
		}

		c += runeCells(r)
	}

	flush()
	b.WriteString(ansi.Cut(styled, at, c))

	return b.String()
}

// paintSpans gives cell ranges of a styled line a background, clipped to the
// visible window.
// ponytail: the painted text loses its syntax colors, the same trade the
// selection makes; keeping them means re-emitting the background after every
// reset chroma writes inside the span.
func paintSpans(styled string, spans []cellSpan, start, end int) string {
	if len(spans) == 0 {
		return ansi.Cut(styled, start, end)
	}

	var b strings.Builder

	at := start
	for _, sp := range spans {
		c0, c1 := max(sp.from, start), min(sp.to, end)
		if c1 <= c0 || c0 < at {
			continue
		}

		b.WriteString(ansi.Cut(styled, at, c0))
		b.WriteString("\x1b[m")
		b.WriteString(renderText(lipgloss.NewStyle().Background(sp.bg), ansi.Strip(ansi.Cut(styled, c0, c1))))
		at = c1
	}

	b.WriteString(ansi.Cut(styled, at, end))

	return b.String()
}

func (p *preview) renderRow(vr vrow, w int) string {
	if p.gaps[vr.line].n > 0 { // a gap's label, which is no text: find and copy read the rows it hides (rowText)
		return p.gutterText(vr.line, true) + p.lines[vr.line]
	}

	in := p.rowIn(vr, w)

	// The last frame drew its rows top to bottom, so they are in order.
	k, ok := slices.BinarySearchFunc(p.rowsWas, vr, func(d rowDrawn, vr vrow) int {
		return cmp.Or(d.in.line-vr.line, d.in.from-vr.from)
	})
	if !ok || !p.rowsWas[k].in.same(&in) {
		p.rowsNow = append(p.rowsNow, rowDrawn{in, in.draw()})
	} else {
		p.rowsNow = append(p.rowsNow, p.rowsWas[k])
	}

	return p.rowsNow[len(p.rowsNow)-1].out
}

// rowIn is everything row vr of the text is drawn from: draw reads nothing
// else, so a row whose rowIn the last frame had takes the row it drew.
type rowIn struct {
	rowKey
	full  []rune   // the line, the same runes until an edit puts new ones in
	marks []string // render_whitespace's, the guides' cells left clear
	extra []rune   // a conflict marker's note and actions
	spans []cellSpan
	probs []probSpan
}

// rowKey is the comparable part of a rowIn.
type rowKey struct {
	line, from, to, tw, left int
	wrap                     bool
	styled                   string // the line's colors
	gutter                   string // drawn whole by gutterText
	extraStyled              string
	marker, shut             bool // one color for a conflict marker; a fold's ⋯ after the text
	outside, brk             bool // no selection on the row; its line break selected
	aCol, zCol               int  // the selection's ends on the row's line, -1 off it
	lv, unit, lit            int  // the row's guide levels, their step, the one lit
	bar                      [2]string
	under, bg                color.Color // the row's background, or a diff's tint to its end
	gen                      int         // lookGen: the palette it is drawn in
}

// rowDrawn is a row as drawn from in.
type rowDrawn struct {
	in  rowIn
	out string
}

// same reports whether in and o draw the same row; the line is compared by
// its runes' address, which an edit changes.
func (in *rowIn) same(o *rowIn) bool {
	return in.rowKey == o.rowKey && len(in.full) == len(o.full) && (len(in.full) == 0 || &in.full[0] == &o.full[0]) &&
		slices.Equal(in.marks, o.marks) && slices.Equal(in.extra, o.extra) && slices.Equal(in.spans, o.spans) && slices.Equal(in.probs, o.probs)
}

// window is the runes lo to hi of the row's line it shows, off cells into
// the line. A row costs its width, not its line's, a minified one
// megabytes: wrapped, the window is the row's runes; unwrapped, those from
// the left edge to the right.
func (in *rowIn) window() (lo, hi, off int) {
	lo, hi = in.from, in.to
	if !in.wrap {
		lo, hi = runesIn(in.full, in.left), runesIn(in.full, in.left+in.tw)
	}

	return lo, hi, cellsOf(in.full[:lo])
}

// rowIn reads what row vr, w cells wide, is drawn from.
func (p *preview) rowIn(vr vrow, w int) rowIn {
	full := p.plain[vr.line]
	in := rowIn{rowKey: rowKey{
		line: vr.line, from: vr.from, to: vr.to, tw: max(w-p.gutter(), 1), left: p.left, wrap: p.wrap,
		styled: p.lines[vr.line], gutter: p.gutterText(vr.line, vr.from == 0), marker: p.marker(vr.line),
		under: p.underlay(vr.line), gen: lookGen,
	}, full: full, probs: p.probs[vr.line]}

	if in.under == nil {
		in.bg = p.rowBg(vr.line)
	}

	lo, hi, off := in.window()
	last := hi == len(full) // the row ends the line: a fold's ⋯ or a conflict's note may follow it

	if g := p.guide; vr.line < len(g.lv) && vr.from == 0 { // a wrapped line's later rows start with its text and have none
		in.lv, in.unit, in.bar = g.lv[vr.line], g.unit, g.bar
		if vr.line > g.act.act && vr.line <= g.act.end {
			in.lit = g.act.lit
		}
	}

	in.marks = p.guideRoom(vr, p.blankMarks(vr.line, full, lo, hi), lo)

	a, z, ok := p.selection()

	hits := p.findSpans(vr.line, full, lo, hi)
	if ok && p.hitSelected(a, z) {
		ok = false // the current match shows in find's color, not as a selection
	}

	in.outside = !ok || vr.line < a.line || vr.line > z.line
	in.shut = last && p.shutEnd(vr.line) > vr.line

	if in.outside && last && len(p.conf) > 0 {
		in.extra, in.extraStyled, _ = p.conflictSuffix(vr.line, in.tw)
	}

	if !in.outside {
		in.aCol, in.zCol, in.brk = ifElse(vr.line == a.line, a.col, -1), ifElse(vr.line == z.line, z.col, -1), vr.line < z.line
		return in
	}

	in.spans = hits
	if in.spans == nil {
		for _, sp := range p.wordSpans(full, lo, hi) {
			in.spans = append(in.spans, cellSpan{sp[0], sp[1], pal.wordHiBg})
		}
	}

	if p.hot != "" && vr.line < len(p.ghHits) {
		for _, h := range p.ghHits[vr.line] {
			if h.key == p.hot && h.lit1 > h.lit0 {
				in.spans = append(in.spans, cellSpan{h.lit0 - off, h.lit1 - off, pal.hoverBg})
			}
		}
	}

	return in
}

// draw is the row in shows: its gutter, then its window on the line, in
// the line's colors under the selection, find's hits or the word under the
// cursor, the guides and the problems' squiggles.
func (in *rowIn) draw() string {
	lo, hi, off := in.window()
	line, styled := in.full[lo:hi], in.styled

	if lo > 0 || hi < len(in.full) {
		styled = cutRow(styled, in.full, lo, hi, off)
	}

	last := hi == len(in.full)

	if in.marker { // one color, not the lexer's guess at << and ==
		styled = string(line)
	}

	styled = markBlanks(styled, line, in.marks, fg(pal.whitespace))

	start, end := 0, cellsOf(line)
	if !in.wrap { // the edge may split a wide rune, which the window leaves out
		start = max(in.left-off, 0)
		end = start + in.tw
	}

	if in.shut { // a folded block's header ends in ⋯, VS Code's placeholder
		line = append(line[:len(line):len(line)], ' ', '⋯')
		styled += renderText(fg(pal.whitespace), " ⋯")

		if in.wrap {
			end = min(start+cellsOf(line), start+in.tw)
		}
	}

	if in.extra != nil { // a conflict marker's note and actions, VS Code's decoration after-text and CodeLens in one row
		line = append(line[:len(line):len(line)], in.extra...)
		styled += in.extraStyled

		if in.wrap {
			end = min(start+cellsOf(line), start+in.tw)
		}
	}

	var b strings.Builder

	c0, c1 := 0, 0 // the row's selected cells

	if in.outside {
		b.WriteString(paintSpans(styled, in.spans, start, end))
	} else {
		sel := lipgloss.NewStyle().Background(pal.textSelBg)

		s0, s1 := 0, len(line) // in the window's runes
		if in.aCol >= 0 {
			s0 = min(max(in.aCol-lo, 0), len(line))
		}

		if in.zCol >= 0 {
			s1 = min(max(in.zCol-lo, 0), len(line))
		}

		c0 = max(start, min(cellsOf(line[:s0]), end))
		c1 = max(start, min(cellsOf(line[:s1]), end))
		b.WriteString(ansi.Cut(styled, start, c0))
		b.WriteString("\x1b[m")
		// Selected text drops its colors, the whitespace markers keep theirs.
		b.WriteString(ansi.Cut(markBlanks(renderText(sel, string(line)), line, in.marks, sel.Foreground(pal.whitespace)), c0, c1))
		b.WriteString(ansi.Cut(styled, c1, end))
		// A selected line break shows as one selected cell after the text.
		if lineCells := cellsOf(line); in.brk && last && lineCells >= start && lineCells-start < in.tw {
			b.WriteString(renderText(sel, " "))
		}
	}

	body := paintProbs(paintGuides(b.String(), in.lv, in.unit, in.lit, in.bar, off+start, off+end, off+c0, off+c1), in.probs, off+start)
	if in.under != nil {
		body = underBg(body+blank(max(in.tw-ansi.StringWidth(body), 0)), in.under)
	} else if in.bg != nil { // tinted diff rows fill the width, like an editor
		if used := ansi.StringWidth(body); used < in.tw {
			body += renderText(lipgloss.NewStyle().Background(in.bg), blank(in.tw-used))
		}
	}

	return in.gutter + body
}

// splitMinW is the narrowest main area that shows a diff side by side.
const splitMinW = 90

// split reports whether the diff shows side by side: the setting is on and
// both halves stay readable.
func (p *preview) split(m *Model) bool {
	return p.meta != nil && m.st.Settings.DiffView == "split" && m.mainW() >= splitMinW
}

// splitRow is one side-by-side row: old and new line indexes, -1 = an empty
// side. Full rows (commit text, file names, hunk gaps) span both halves.
type splitRow struct {
	l, r int
	full bool
}

// splitRows pairs each run of deletions with the additions right after it.
func splitRows(meta []lineMeta) []splitRow {
	var out []splitRow

	for i := 0; i < len(meta); {
		switch meta[i].kind {
		case 'c':
			out = append(out, splitRow{l: i, r: i})
			i++

		case 'd', 'a':
			del := i
			for i < len(meta) && meta[i].kind == 'd' {
				i++
			}

			add := i
			for i < len(meta) && meta[i].kind == 'a' {
				i++
			}

			for k := range max(add-del, i-add) {
				sr := splitRow{l: -1, r: -1}
				if del+k < add {
					sr.l = del + k
				}

				if add+k < i {
					sr.r = add + k
				}

				out = append(out, sr)
			}

		default:
			out = append(out, splitRow{l: i, r: i, full: true})
			i++
		}
	}

	return out
}

// splitIndex is the side-by-side row meta line n is drawn on.
func splitIndex(rows []splitRow, n int) int {
	return slices.IndexFunc(rows, func(sr splitRow) bool { return sr.l == n || sr.r == n })
}

// splitPos is the meta line under split body cell (x, y): the half the mouse
// is over, or the line itself on a row that spans both.
func (p *preview) splitPos(w, x, y int) (int, bool) {
	rows := splitRows(p.meta)
	if len(rows) == 0 {
		return 0, false
	}

	sr := rows[max(0, min(p.top+y, len(rows)-1))]

	n := sr.r
	if sr.full || x <= (w-1)/2 {
		n = sr.l
	}

	if n < 0 {
		n = max(sr.l, sr.r)
	}

	return n, n >= 0
}

// selectTo moves the cursor of a side-by-side diff, where there are no
// columns: a selection is always whole lines, so the cursor sits at the end
// of its own. A line of the other half's change takes the line beside it,
// or the other half when it stands alone, as a pure deletion does.
func (p *preview) selectTo(line int, extend bool) {
	if line < len(p.meta) && p.otherHalf(line) {
		rows := splitRows(p.meta)
		if n := p.halfLine(rows[splitIndex(rows, line)]); n >= 0 {
			line = n
		} else {
			p.oldHalf = !p.oldHalf
		}
	}

	if extend {
		if p.anchor == nil {
			a := p.at()
			p.anchor = &a
		}
	} else {
		p.anchor = nil
	}

	p.cur = pos{line, p.lineLen(line)}
}

// otherHalf reports meta line i a change the cursor's half does not show.
func (p *preview) otherHalf(i int) bool { return p.meta[i].kind == ifElse(p.oldHalf, byte('a'), 'd') }

// halfLine is the line row sr shows on the cursor's half, -1 for none.
func (p *preview) halfLine(sr splitRow) int {
	if sr.full {
		return sr.l
	}

	return ifElse(p.oldHalf, sr.l, sr.r)
}

// splitRange is the rows a side-by-side selection spans, the anchor's to
// the cursor's, or the cursor's alone: both halves of them stage and revert,
// as VS Code's do for what a selection in either editor touches.
func (p *preview) splitRange(rows []splitRow) (a, z int) {
	c := max(splitIndex(rows, p.at().line), 0)
	if _, _, ok := p.selection(); !ok {
		return c, c
	}

	a = max(splitIndex(rows, p.anchor.line), 0)

	return min(a, c), max(a, c)
}

// halfText is a side-by-side selection's text: the lines of its rows on the
// cursor's half, as the editor on that side copies them; "" for none.
func (p *preview) halfText() string {
	if _, _, ok := p.selection(); !ok {
		return ""
	}

	rows := splitRows(p.meta)
	a, z := p.splitRange(rows)

	var out []string

	for _, sr := range rows[a : z+1] {
		if i := p.halfLine(sr); i >= 0 {
			out = append(out, p.rowText(i))
		}
	}

	return strings.Join(out, "\n")
}

// splitMove is the line key takes the cursor of a side-by-side diff to: by
// rows on its half, past the rows that half leaves empty, as VS Code's
// editors move; false for a key that is no move.
func (p *preview) splitMove(key string, h int) (int, bool) {
	rows := splitRows(p.meta)
	r := max(splitIndex(rows, p.at().line), 0)

	d, ok := map[string]int{
		"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -h, "b": -h, "pgdown": h, "f": h, "space": h,
		"g": -len(rows), "ctrl+home": -len(rows), "G": len(rows), "ctrl+end": len(rows),
	}[key]
	if !ok || len(rows) == 0 {
		return 0, false
	}

	step := ifElse(d < 0, -1, 1)
	for t := max(0, min(r+d, len(rows)-1)); t >= 0 && t < len(rows) && t != r; t += step {
		if n := p.halfLine(rows[t]); n >= 0 {
			return n, true
		}
	}

	return p.at().line, true // nothing further on this half
}

// followSplit scrolls a side-by-side diff so the cursor's row is visible.
func (p *preview) followSplit(h int) {
	rows := splitRows(p.meta)

	i := splitIndex(rows, p.at().line)
	if i < 0 {
		return
	}

	p.top = max(0, min(max(min(p.top, i), i-h+1), max(len(rows)-h, 0)))
}

func (p *preview) splitBody(m *Model, w, h int) []string {
	rows := splitRows(p.meta)
	p.top = max(0, min(p.top, len(rows)-h))
	lw := (w - 1) / 2

	var body []string

	sa, sz := p.splitRange(rows)

	hy := p.hoverY(m, w, h)

	for k, sr := range rows[p.top:min(p.top+h, len(rows))] {
		sel := p.top+k >= sa && p.top+k <= sz // drawn on the cursor's half

		switch {
		case sr.full && p.meta[sr.l].kind == 'h':
			body = append(body, renderText(gapSt(k == hy), center(strings.TrimSpace("⋯ "+ansi.Strip(p.lines[sr.l])), w)))
		case sr.full:
			body = append(body, ansi.Cut(p.lines[sr.l], p.left, p.left+w))
		default:
			body = append(body, p.splitHalf(sr.l, true, sel && p.oldHalf, lw)+renderText(dim, "│")+p.splitHalf(sr.r, false, sel && !p.oldHalf, w-1-lw))
		}
	}

	return body
}

// splitHalf is one side of a split row: its line number, mark and code,
// tinted to the half's edge, or selected.
func (p *preview) splitHalf(i int, old, sel bool, w int) string {
	if i < 0 {
		return blank(w)
	}

	md, bg := p.meta[i], p.rowBg(i)
	if sel {
		bg = pal.textSelBg
	}

	n, mark, mc := md.b, " ", pal.diffAddMark
	if old {
		n = md.a
	}

	switch md.kind {
	case 'a':
		mark = "+"
	case 'd':
		mark, mc = "-", pal.diffDelMark
	}

	num := blank(p.numW)
	if n > 0 {
		num = fmt.Sprintf("%*d", p.numW, n)
	}

	st, ms := dim, fg(mc)
	if bg != nil {
		st, ms = st.Background(bg), ms.Background(bg)
	}

	tw := max(w-p.numW-3, 0)

	body := ansi.Cut(p.lines[i], p.left, p.left+tw)
	if sel { // selected text drops its own colors, as it does inline
		body = lipgloss.NewStyle().Background(bg).Render(ansi.Strip(body))
	}

	if !old { // the new side is the file, its problems squiggled, selected too
		body = paintProbs(body, p.probs[i], p.left)
	}

	body += "\x1b[m"

	if used := ansi.StringWidth(body); used < tw {
		pad := blank(tw - used)
		if bg != nil {
			pad = lipgloss.NewStyle().Background(bg).Render(pad)
		}

		body += pad
	}

	return st.Render(num+" ") + ms.Render(mark+" ") + body
}

// scrollSplit applies a navigation key to a side-by-side diff, which has no
// text cursor; ok is false for other keys.
func (p *preview) scrollSplit(key string, rows, h int) bool {
	switch key {
	case "up", "k":
		p.top--
	case "down", "j":
		p.top++
	case "pgup", "b":
		p.top -= h
	case "pgdown", "f", "space":
		p.top += h
	case "g", "home":
		p.top = 0
	case "G", "end":
		p.top = rows
	case "left", "h":
		p.left = max(p.left-8, 0)
	case "right", "l":
		p.left += 8
	default:
		return false
	}

	p.top = max(0, min(p.top, rows-h))

	return true
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// side reports a Markdown file shown as source and rendering side by side.
func (p *preview) side(m *Model) bool { return p.md == 2 && m.mainW() >= splitMinW }

// scrollOnly reports the two-column views, which scroll by rows: side by side
// Markdown has no cursor, a split diff a whole row.
func (p *preview) scrollOnly(m *Model) bool { return p.split(m) || p.side(m) }

func (p *preview) scrollRows(m *Model) int {
	if p.side(m) {
		w := m.pvW()
		styled, _ := p.markdown(m, w-1-(w-1)/2-1)

		return max(len(p.src), len(styled))
	}

	return len(splitRows(p.meta))
}

// markdown is the file rendered w cells wide, cached while w stays.
func (p *preview) markdown(m *Model, w int) (styled, plain []string) {
	if p.mdW != w || p.mdLines == nil {
		if p.kind == pvGH && p.gh != nil { // a page of GitHub is laid out from its document, not rendered
			p.mdLines, p.mdPlain = p.ghLayout(m, w)
		} else {
			if p.mdBlocks == nil {
				p.mdBlocks = &mdBlocks{}
			}

			p.mdLines, p.mdPlain = p.mdBlocks.render(p.text(), w, m.dark)
		}

		p.mdW = w
	}

	return p.mdLines, p.mdPlain
}

// mdWidth is the width Markdown renders in on its own within w cells:
// markdown_width, glow's -w, when the panel is wider; 0 fills the panel.
// Beside its source it takes its whole half, so its lines stay level with the
// source's rather than running longer.
func mdWidth(m *Model, w int) int {
	if n := m.st.Settings.MDWidth; n > 0 {
		return min(w, n)
	}

	return w
}

// syncMarkdown puts the rendering, laid out for the current width, in place
// of the source while md == 1.
func (p *preview) syncMarkdown(m *Model) {
	w := mdWidth(m, m.pvW())
	if p.kind == pvGH { // a page of GitHub has the whole panel: its sidebar needs the room
		w = m.pvW()
	}

	if p.md != 1 || !p.ready || p.mdShown == w {
		return
	}

	styled, plainLines := p.markdown(m, w)

	p.lines, p.plain = styled, make([][]rune, len(plainLines))
	for i, l := range plainLines {
		p.plain[i] = []rune(l)
	}

	p.mdShown, p.vis, p.anchor = w, nil, nil
	p.cur.line = min(p.cur.line, p.lastLine())
}

// setMarkdown switches a Markdown file between source (0), rendering (1)
// and both side by side (2).
func (p *preview) setMarkdown(m *Model, md int) {
	p.md, p.top, p.left, p.anchor, p.cur, p.vis = md, 0, 0, nil, pos{}, nil
	p.lines, p.plain, p.mdShown = p.src, p.srcPlain, 0
	p.syncMarkdown(m)
}

// toggleMarkdown switches a Markdown file to mode, or back to source from
// it, and says why side by side may not show.
func (p *preview) toggleMarkdown(m *Model, mode int) tea.Cmd {
	if p.md == mode {
		mode = 0
	}

	p.setMarkdown(m, mode)

	if mode == 2 && !p.side(m) {
		return splitFlash()
	}

	return nil
}

// splitFlash says why a side by side layout stays inline.
func splitFlash() tea.Cmd {
	return flash(fmt.Sprintf("side by side needs a %d column wide main area", splitMinW), false)
}

// sideBody is the Markdown source beside its rendering; both scroll together.
func (p *preview) sideBody(m *Model, w, h int) []string {
	lw := (w - 1) / 2
	rw := w - 1 - lw
	styled, _ := p.markdown(m, rw-1)
	n := max(len(p.src), len(styled))
	p.top = max(0, min(p.top, n-h))
	gw := numPad + p.numW + 1

	var body []string

	for i := p.top; i < min(p.top+h, n); i++ {
		left, right := "", ""
		if i < len(p.src) {
			left = renderText(lineNumSt(false), blank(numPad)+fmt.Sprintf("%*d ", p.numW, i+1)) + ansi.Cut(p.src[i], p.left, p.left+lw-gw)
		}

		if i < len(styled) {
			right = " " + styled[i]
		}

		body = append(body, fit(left, lw)+renderText(dim, "│")+fit(right, rw))
	}

	return body
}

// buttons are the preview header's actions, right-aligned: Markdown
// rendering, the diff's context and layout, and the file's revisions.
func (p *preview) buttons(m *Model, w int) []rowAction {
	acts := m.maxAct() // first, so the others keep their places

	add := func(g glyph, tip string, k tea.KeyPressMsg) {
		acts = append(acts, rowAction{g: g, tip: tip + "  " + k.Text, run: func(m *Model) tea.Cmd { return m.pv.key(m, k) }})
	}

	if p.kind == pvTable && p.tbl != nil {
		if p.tbl.total > tablePage {
			add(icPrevRev, "Previous Page", tea.KeyPressMsg{Code: '[', Text: "["})
			add(icNextRev, "Next Page", tea.KeyPressMsg{Code: ']', Text: "]"})
		}

		add(icRefresh, "Reload", tea.KeyPressMsg{Code: 'r', Text: "r"})

		if len(p.tbl.edits) > 0 {
			acts = append(acts, rowAction{g: icCheck, tip: "Submit Changes  ^s", on: true, run: func(m *Model) tea.Cmd { return m.pv.save(m, false) }})
		}

		layoutRight(acts, w, 2)

		return acts
	}

	if p.kind == pvFile && isMarkdown(p.path) { // direct: in a file a letter is text
		acts = append(acts,
			rowAction{g: ifElse(p.md == 1, icSource, icPreview), tip: ifElse(p.md == 1, "Show Source", "Open Preview") + "  ^⇧v", run: func(m *Model) tea.Cmd { return m.toggleRendered() }},
			rowAction{g: ifElse(p.md == 2, icInline, icSplit), tip: ifElse(p.md == 2, "Close Preview", "Open Preview to the Side") + "  M-v", run: func(m *Model) tea.Cmd { return m.pv.toggleMarkdown(m, 2) }})
	}

	if p.meta != nil { // VS Code's diff editor starts its actions with these
		add(icUp, "Previous Change", tea.KeyPressMsg{Code: '[', Text: "["})
		add(icDown, "Next Change", tea.KeyPressMsg{Code: ']', Text: "]"})
	}

	if p.fileDiff() { // the whole file or the changes
		full := m.st.Settings.DiffFull
		add(ifElse(full, icFold, icUnfold), ifElse(full, "Show Only the Changes", "Show the Whole File"), tea.KeyPressMsg{Code: 'z', Text: "z"})
	}

	if p.meta != nil { // the file itself, then inline ↔ side by side
		acts = append(acts, rowAction{g: icSource, tip: "Open File  O", run: func(m *Model) tea.Cmd { return m.pv.openHere(m) }})
		split := m.st.Settings.DiffView == "split"
		add(ifElse(split, icInline, icSplit), ifElse(split, "Inline View", "Side by Side View"), tea.KeyPressMsg{Code: 's', Text: "s"})
	}

	if p.kind == pvFile || p.kind == pvRev {
		acts = append(acts, rowAction{g: icPrevRev, tip: "Open Changes with Previous Revision", run: func(m *Model) tea.Cmd { return m.pv.stepRev(m, 1) }})
	}

	if p.kind == pvRev {
		acts = append(acts, rowAction{g: icNextRev, tip: "Open Changes with Next Revision", run: func(m *Model) tea.Cmd { return m.pv.stepRev(m, -1) }})

		add(icHistory, "Open Changes with Revision…", tea.KeyPressMsg{Code: 'H', Text: "H"})
	}

	if !p.scrollOnly(m) && p.kind != pvSettings { // last, so it keeps its place whatever else the kind shows
		acts = append(acts, rowAction{g: icWrap, tip: "Toggle Word Wrap  M-z", on: p.wrap, run: func(m *Model) tea.Cmd { return m.toggleWrap() }})
	}

	layoutRight(acts, w, 2)

	return acts
}

func (p *preview) hints(m *Model) string {
	switch {
	case m.modal != nil: // the keys go to the box over it
		return " esc close"
	case p.gaps[p.at().line].n > 0:
		return fmt.Sprintf(" ⏎ show %d hidden lines  ↑↓ move  ] [ change%s  q close", p.gaps[p.at().line].n, ifElse(p.fileDiff(), "  z whole file", ""))
	case p.kind == pvTable && p.tbl != nil:
		return p.tbl.hints()
	case p.kind == pvSettings:
		return settingsHints()
	case m.peekH() > 0: // the References inline have the keys
		return " ↑↓ move  ←→ fold  ⏎ open  o open and stay  esc close"
	case p.empty(), !p.ready && p.kind != pvTable: // nothing yet to move to or act on
		return " q close"
	case p.split(m) && p.kind == pvRev: // one file's history: nothing to stage, no other file
		return " ↑↓ move  ] [ change  ⇧ select  s inline  q close"
	case p.split(m) && p.kind != pvDiff: // a commit's or a pull request's: nothing to stage
		return " ↑↓ move  ] [ change  } { file  ⇧ select  s inline  q close"
	case p.split(m):
		return " ↑↓ move  ] [ change  } { file  ⇧ select  " + p.stageHint() + "  O file  s inline  q close"
	case p.side(m):
		return " ↑↓ scroll  p rendered  s source only  q close"
	case p.composing:
		return " ^s send  esc leave the box  ⏎ new line"
	case p.kind == pvGH: // rendered only: no source to toggle to
		return " ↑↓ move  ⇧ select  y copy  m menu  q close"

	case p.md == 1:
		return " ↑↓ move  ⇧ select  y copy  p source  s side by side  q close"
	case p.vimOn(m) && (p.vim.mode == vimChar || p.vim.mode == vimLine): // what vimVisual takes
		return " y yank  d delete  c change  o other end  esc normal  ^w close"
	case p.vimOn(m) && p.vim.mode != vimInsert:
		return " i insert  v visual  w b e  dd yy p  u undo  / find  : line  ^s save  ^w close"
	case p.vimOn(m):
		return " esc normal  ^f find  ^z undo  ^s save  ^w close"
	case p.untitled():
		// No path, so no language server and no history: only what works.
		return " ^f find  ^h replace  ^z undo  ^s save as…  ^w close"
	case p.editable():
		save := "  ^s save"
		if !p.dirty() {
			save = ""
		}

		if h := p.conflictHint(); h != "" {
			return h + save + "  ^w close"
		}

		keys := "  F12 def  ⇧F12 refs  ^. fix  F2 rename"
		if _, argv := m.lspCommand(p.path); len(argv) == 0 { // no server would answer them
			keys = ""
		}

		if isMarkdown(p.path) {
			keys += "  ^⇧v rendered  M-v side by side"
		}

		return " ^f find  ^h replace" + save + keys + "  ^w close" // save before what a narrow row drops first

	case p.kind == pvFile:
		return " ↑↓ move  ^f find  F12 def  ⇧F12 refs  ^. fix  F2 rename  q close"
	case p.kind == pvRev:
		return " ↑↓ move  ] [ change  H history  s split  ⇧ select  y copy  q close"
	case p.kind == pvDiff:
		return " ↑↓ move  ] [ change  } { file  ⇧ select  " + p.stageHint() + "  O file  s split  q close"
	case p.meta != nil:
		return " ↑↓ move  ] [ change  } { file  ⇧ select  s split  y copy  q close"
	case isMarkdown(p.path):
		return " ↑↓ move  ⇧ select  p rendered  s side by side  M-← history  q close"
	}

	return " ↑↓ move  ⇧ select  y copy  w wrap  M-← history  q close"
}

// empty reports a diff with nothing in it: no row to move to or act on.
func (p *preview) empty() bool { return p.kind == pvDiff && p.raw == "(no changes)" }

// stageHint is what m does on a diff: a staged one's menu only unstages.
func (p *preview) stageHint() string {
	return ifElse(p.entry.Staged, "m unstage lines", "m stage/revert lines")
}

// fitHints drops whole hints, two spaces apart, until hints fits w cells,
// rather than cut one: from the end, but the closing one stays last.
func fitHints(hints string, w int) string {
	hs := strings.Split(hints, "  ")
	for len(hs) > 1 && ansi.StringWidth(strings.Join(hs, "  ")) > w {
		i := len(hs) - 1
		if strings.HasSuffix(hs[i], " close") && i > 1 {
			i--
		}

		hs = slices.Delete(hs, i, i+1)
	}

	return strings.Join(hs, "  ")
}

// stepRev walks the open file's history like GitLens: d = 1 opens the
// changes of the next older revision, -1 of the next newer one; newer than
// the newest is the file itself.
func (p *preview) stepRev(m *Model, d int) tea.Cmd {
	switch {
	case p.untitled():
		return flash("save the file first", true)
	case p.kind == pvFile && d > 0:
		return m.setPreview(preview{kind: pvRev, path: p.path, rev: "0"})
	case p.kind != pvRev || p.revs == nil:
		return nil
	case p.revIdx+d < 0:
		return m.setPreview(preview{kind: pvFile, path: p.path})
	case p.revIdx+d >= len(p.revs):
		return flash("no older revision", false)
	}

	return m.revAt(p.revIdx + d)
}

func (m *Model) revAt(i int) tea.Cmd {
	return m.setPreview(preview{kind: pvRev, path: m.pv.path, repo: m.pv.repo, revs: m.pv.revs, revIdx: i, rev: strconv.Itoa(i)})
}

// pickRev lists the file's revisions to jump to, like GitLens' Open Changes with Revision.
func (p *preview) pickRev(m *Model) tea.Cmd {
	if p.kind != pvRev || p.revs == nil {
		return nil
	}

	items := make([]item, len(p.revs))
	for i, r := range p.revs {
		label := r.Short + "  " + r.Subject
		if r.Hash == "" {
			label = "Uncommitted changes"
		}

		items[i] = item{label: label, hint: r.When, run: func(m *Model) tea.Cmd { return m.revAt(i) }}
	}

	m.modal = newPicker("Open Changes with Revision", items)

	return nil
}

func (p *preview) view(m *Model, w, h int) (header string, body []string, footer string) {
	p.rowsWas, p.rowsNow = p.rowsNow, p.rowsWas[:0]
	p.syncMarkdown(m)
	name, context := p.label(m.ws)

	nameSt := bold
	if m.focus == onMain {
		nameSt = accent
	}

	var left []seg

	switch {
	case p.pathHeader() && m.st.Settings.Crumbs:
		left, _ = p.crumbs(m, w)
	case p.pathHeader(): // breadcrumbs off: the buttons only
	default:
		left = []seg{sg(" "+icClose.s()+" ", dim)}

		switch {
		case p.kind == pvTable:
			left = append(left, sg(icTable.s()+" ", dim))
		case p.kind == pvSettings:
			left = append(left, sg(icGear.s()+" ", dim))
		case p.kind != pvShow && !p.fromGH():
			left = append(left, iconSeg(name, false, false))
		}

		if p.dirty() {
			name += " ●"
		}

		left = append(left, sg(name, nameSt), sg("  "+context, dim))
	}

	// The cursor and the selection are the status bar's, as VS Code shows
	// them; the header keeps vim's mode, while it leaves room for the name.
	var candidates []string
	if p.vimOn(m) && !p.scrollOnly(m) {
		candidates = []string{" " + p.vimLabel() + " "}
	}

	right, bw := []seg{{}}, 0

	if p.kind == pvGH && p.gh != nil {
		var gs []seg

		gs, _, bw = p.ghHeader(m, w)
		right = append(right, gs...)
	} else if !m.buttonsUp() {
		var bs []seg

		bs, bw = p.buttonSegs(m, w)
		right = append(right, bs...)
	}

	for _, s := range candidates {
		if ansi.StringWidth(s) <= w-ansi.StringWidth(name)-bw-6 {
			right[0] = sg(s, dim)
			break
		}
	}

	header = row(w, nil, left, right...)

	p.probs = m.probsOf(p)
	if t := p.probTip; t != nil && m.mouseX == t.x && m.mouseY == t.y && !p.hoverShown(m) { // VS Code's hover over a squiggle; a server's hover holds it
		m.tip = t.text
	}

	footer = row(w, nil, []seg{sg(fitHints(p.hints(m), w), dim)})
	if pr := p.probAtCursor(); pr != nil { // VS Code's hover, where the keyboard is: the problem under the cursor
		g, st := sevMark(pr.sev)
		footer = row(w, nil, []seg{sg(" "+g+" ", st), sg(pr.msg, dim)})
	}

	if p.kind == pvTable && p.tbl != nil {
		return header, p.tbl.body(m, w, h), footer
	}

	if p.kind == pvSettings && p.set != nil {
		return header, p.set.body(m, w, h), footer
	}

	if p.err != "" { // a card's first line, the file's name, in the text's color
		lines := strings.Split(p.err, "\n")

		body = []string{""}
		for i, l := range lines {
			body = append(body, ifElse(i == 0 && len(lines) > 1, plain, dim).Render("  "+l))
		}

		return header, body, footer
	}

	if !p.ready {
		return header, nil, footer
	}

	tw := w - 1 // the text; the scrollbar has the last column, as VS Code's editor.scrollbar does
	vb := m.barState("editor")

	switch {
	case p.split(m):
		body = p.splitBody(m, tw, h)
		kinds, _ := p.barChanges(m)

		return header, m.withCells(body, w, vbar{p.scrollRows(m), h, p.top}.marked(vb, kinds, diffMarks())), footer

	case p.side(m):
		body = p.sideBody(m, tw, h)
		return header, m.withBar(body, w, vbar{p.scrollRows(m), h, p.top}, vb), footer
	}

	if hot := p.ghHot(m, tw, h); hot != p.hot {
		was := p.hot
		p.hot = hot

		if ghFramed(was) || ghFramed(hot) {
			p.mdLines, p.mdShown = nil, 0
			p.syncMarkdown(m)
		}
	}

	p.hl, p.blanks, p.guide = p.hlWord(), m.st.Settings.Blanks, p.indentGuides()
	vis, hb := p.rows(tw), p.hbar(m, tw)

	p.top = max(0, min(p.top, len(vis)-h))
	p.left = max(0, min(p.left, hb.n-hb.h)) // a shift+wheel pans no further than the widest line
	hb.top = p.left

	hy := p.hoverY(m, tw, h)

	for k := p.top; k < len(vis) && k < p.top+h; k++ {
		if vis[k].line < 0 { // the quick diff's zone
			body = append(body, p.qd.row(m, vis[k].from, tw, m.mainY()+m.headH()+m.stripH()+k-p.top))
			continue
		}

		if k-p.top == hy && p.gaps[vis[k].line].n > 0 {
			body = append(body, renderText(gapSt(true), pad(ansi.Strip(p.renderRow(vis[k], tw)), tw, false)))
			continue
		}

		body = append(body, p.renderRow(vis[k], tw))
	}

	kinds, set := p.barProbs(vis), probMarks() // a file's problems; a diff's changes
	if p.meta != nil {
		kinds, _ = p.barChanges(m)
		set = diffMarks()
	}

	bar := vbar{len(vis), h, p.top}

	cells := bar.marked(vb, kinds, set)
	if under := p.fileBarChanges(vis); p.meta == nil && under != nil { // a file's changes under its problems, VS Code's ruler's lanes in one
		bare, chg := bar.cells(vb), bar.marked(vb, under, diffMarks())
		for i := range cells {
			if cells[i] == bare[i] {
				cells[i] = chg[i]
			}
		}
	}

	body = m.withCells(body, w, cells)
	if hb.on() { // under the text, the gutter and the vertical bar's corner left blank
		body = append(body, blank(p.gutter())+hb.hcells(m.barState("editor-h"))+" ")
	}

	return header, body, footer
}

// hbar is the editor's horizontal scrollbar for a view w cells wide: the
// widest line against the text area, from p.left. It shows only while a line
// runs past the right edge, so never while wrapping, and the two-column views
// have none.
func (p *preview) hbar(m *Model, w int) vbar {
	if p.wrap || !p.ready || p.err != "" || len(p.plain) == 0 || p.scrollOnly(m) {
		return vbar{}
	}

	p.rows(w)

	return vbar{p.wide + 1, max(w-p.gutter(), 1), p.left} // +1: the cursor stands past the line end
}

// fileDiff reports a diff of one file, which diff_full shows whole; a
// commit's or a pull request's spans many files, an untracked file's is
// whole already.
func (p *preview) fileDiff() bool {
	return p.meta != nil && p.entry.Letter != 'U' && (p.kind == pvDiff || p.kind == pvCmp || p.kind == pvRev)
}

// foldable reports a full-context diff a view can fold: short enough to
// show whole, and no merge's combined diff, whose -x/+x columns read as
// unchanged rows.
func foldable(raw string) bool {
	return strings.Count(raw, "\n") <= maxPreviewLines && !strings.Contains("\n"+raw, "\ndiff --cc ")
}

// unfolds reports a diff with gaps to unfold, fetched with full context: one
// file's (not an untracked one, whole already) or a commit's.
func (p *preview) unfolds() bool {
	return !p.fixed && p.entry.Letter != 'U' && (p.kind == pvDiff || p.kind == pvCmp || p.kind == pvRev || p.kind == pvShow)
}

// folds reports a diff shown from all, folded or whole.
func (p *preview) folds() bool { return p.unfolds() && p.all.meta != nil }

// compact shows the rows of all: a file's diff whole with diff_full, else
// the changes with the unchanged runs between them folded into gaps.
func (p *preview) compact(m *Model) {
	rows := shownRows(p.all.meta, p.open, m.st.Settings.DiffFull && p.fileDiff())
	p.lines, p.plain, p.meta, p.gaps = make([]string, len(rows)), make([][]rune, len(rows)), make([]lineMeta, len(rows)), map[int]shownRow{}

	for k, r := range rows {
		if r.n > 0 { // the gap: its label, no text, and the first hidden row's numbers, which unfold it
			l := p.all.meta[r.i]
			p.lines[k], p.meta[k], p.gaps[k] = dim.Render(fmt.Sprintf("%d hidden lines", r.n)), lineMeta{a: l.a, b: l.b, kind: 'h'}, r

			continue
		}

		p.lines[k], p.plain[k], p.meta[k] = p.all.lines[r.i], []rune(p.all.plain[r.i]), p.all.meta[r.i]
	}

	p.vis = nil
}

// refold shows what is folded now: the rows move under the cursor and the
// find hits.
func (p *preview) refold(m *Model) {
	p.compact(m)
	p.rehit()
	p.rangeHi, p.cur.line = 0, min(p.cur.line, p.lastLine())

	if p.anchor != nil {
		a := pos{min(p.anchor.line, p.lastLine()), p.anchor.col}
		p.anchor = &a
	}
}

// hoverY is the body row under the mouse, -1 when it is off the body.
func (p *preview) hoverY(m *Model, w, h int) int {
	x, y := m.mouseX-m.mainX(), m.mouseY-m.mainY()-m.headH()-m.stripH()
	if x < 0 || x >= w || y < 0 || y >= h {
		return -1
	}

	return y
}

// gapSt draws a gap's label, lit across the row under the mouse: a click
// unfolds it.
func gapSt(hot bool) lipgloss.Style {
	if hot {
		return plain.Background(pal.hoverBg)
	}

	return dim
}

// unfold shows the rows behind the gap on meta line n, the cursor on the
// first of them; false when n is no gap that unfolds.
func (p *preview) unfold(m *Model, n int) bool {
	if p.gaps[n].n == 0 {
		return false
	}

	file := 0
	for _, l := range p.meta[:n] {
		file += b2i(l.kind == 'f')
	}

	p.open = append(p.open, gapLines{file, p.meta[n].a, p.meta[n].a + p.gaps[n].n - 1}) // a gap's rows are unchanged: one old line each
	p.refold(m)
	p.setLine(m, n)

	return true
}

// unfoldLine unfolds the gap hiding new line b, if one does.
func (p *preview) unfoldLine(m *Model, b int) {
	for k, l := range p.meta { // in order: a commit's first file with the line, as findPlace finds it
		if g := p.gaps[k]; g.n > 0 && b >= l.b && b < l.b+g.n {
			p.unfold(m, k)
			return
		}
	}
}

// toggleDiffFull is VS Code's Toggle Collapse Unchanged Regions: a file's
// diff shows the whole file, or the changes with three lines around them.
func (m *Model) toggleDiffFull() tea.Cmd {
	m.pv.keepPlace(m) // the rows change under the cursor; its line is found again
	cmd := m.setSettings(map[string]any{"diff_full": !m.st.Settings.DiffFull})

	return tea.Batch(cmd, m.pv.refull(m))
}

// refull shows a file's diff whole or folded as diff_full says now: folded
// again where it has every row, fetched again where git cut it short.
func (p *preview) refull(m *Model) tea.Cmd {
	if !p.folds() {
		return p.load(m)
	}

	p.open = nil
	p.refold(m)

	if p.keep != nil {
		p.findAgain(m)
	}

	return nil
}

// toggleWrap is VS Code's Toggle Word Wrap. It flips the word_wrap setting,
// so every editor and every attached TUI follows, as `s` does for diff_view.
func (m *Model) toggleWrap() tea.Cmd {
	return m.setSettings(map[string]any{"word_wrap": !m.st.Settings.Wrap})
}

// syncWrap puts the word_wrap setting on the open editor; off, long lines
// scroll sideways under the horizontal bar.
func (m *Model) syncWrap() {
	if p := &m.pv; p.wrap != m.st.Settings.Wrap {
		p.wrap, p.left, p.vis = m.st.Settings.Wrap, 0, nil
		p.follow(m.pvW(), m.pvH())
	}
}

// bar is the editor's scrollbar as the view draws it.
func (p *preview) bar(m *Model) vbar {
	if p.scrollOnly(m) {
		return vbar{p.scrollRows(m), m.pvH(), p.top}
	}

	return vbar{len(p.rows(m.pvW())), m.pvH(), p.top}
}

// newFind is the ⌃f box: a text input with the preview's own placeholder.
func newFind() filter {
	f := newFilter()
	f.input.Prompt = ""
	f.input.Placeholder = "Find"

	return f
}

// startFind opens the ⌃f box, prefilled with the selection like VS Code.
func (p *preview) startFind(m *Model) tea.Cmd {
	if !p.ready || len(p.plain) == 0 {
		return nil
	}

	if p.find.input.Placeholder == "" {
		p.find = newFind()
		p.repl = textinput.New()
		p.repl.Prompt, p.repl.Placeholder = "", "Replace"
	}

	if t := p.selectedText(); t != "" && !strings.Contains(t, "\n") {
		p.find.input.SetValue(t)
	}

	p.find.on, p.find.editing = true, true
	p.findFrom = p.at()
	p.find.input.CursorEnd()
	p.refind(m)
	p.repl.Blur()

	return p.find.input.Focus()
}

// startReplace is VS Code's ⌃h: the find widget with the replace box open.
// The caret goes to the replacement once there is a query to replace, as it
// does there. A read-only editor has nothing to replace into.
func (p *preview) startReplace(m *Model) tea.Cmd {
	if !p.editable() {
		return flash("read-only: nothing to replace", true)
	}

	cmd := p.startFind(m)
	if !p.find.editing { // nothing to find in
		return nil
	}

	p.replOn = true
	if p.find.input.Value() != "" {
		return p.focusReplace(true)
	}

	return cmd
}

// toggleReplace opens or closes the replace box under the query; opening it
// takes the caret there.
func (p *preview) toggleReplace() tea.Cmd {
	if !p.editable() {
		return flash("read-only: nothing to replace", true)
	}

	if p.replOn = !p.replOn; !p.replOn {
		return p.focusReplace(false)
	}

	return p.focusReplace(true)
}

// focusReplace moves the caret between the query and the replacement.
func (p *preview) focusReplace(on bool) tea.Cmd {
	if on && p.replOn {
		p.find.input.Blur()
		return p.repl.Focus()
	}

	p.repl.Blur()

	return p.find.input.Focus()
}

// closeFind hides the box but keeps the matches: F3 walks them afterwards,
// as it does in VS Code.
func (p *preview) closeFind() {
	p.find.on, p.find.editing = false, false
	p.find.input.Blur()
	p.repl.Blur()
}

// findHit is one match: where it starts and the column it ends at; hidden
// is one in the rows a diff's gap at.line hides, which selected unfolds.
type findHit struct {
	at     pos
	end    int
	hidden bool
}

// refind recollects the matches and selects the one at or after the cursor.
// rehit is the variant that stays on the current match.
func (p *preview) refind(m *Model) {
	from := p.findFrom
	p.hit = 0
	p.collectHits()

	if len(p.hits) == 0 {
		return
	}

	p.hit = max(slices.IndexFunc(p.hits, func(h findHit) bool {
		return h.at.line > from.line || (h.at.line == from.line && h.at.col >= from.col)
	}), 0)
	p.selectHit(m)
}

// rehit recollects the matches after the text changed under them, staying on
// the same one and leaving the cursor where it is.
func (p *preview) rehit() {
	i := p.hit
	p.collectHits()
	p.hit = max(min(i, len(p.hits)-1), 0)
	p.scanConflicts() // every caller has new text
}

// findRe is the query as a regexp under the widget's toggles; nil for an
// empty query or a regular expression still being typed.
func (p *preview) findRe() *regexp.Regexp {
	q := p.find.input.Value()
	if q == "" {
		return nil
	}

	return matcher(searchOpts{query: q, caseSens: p.findCase, word: p.findWord, regex: p.findRegex})
}

// collectHits finds the query's matches in the text as it is now.
func (p *preview) collectHits() {
	p.hits = nil

	re := p.findRe()
	if re == nil {
		return
	}

	for i, line := range p.plain {
		rows := []string{string(line)}

		g, hidden := p.gaps[i] // a gap's label: the rows it hides, found as VS Code finds in them
		if hidden {
			rows = p.all.plain[g.i : g.i+g.n]
		}

		for _, text := range rows {
			for _, r := range re.FindAllStringIndex(text, -1) {
				if r[1] > r[0] {
					a := utf8.RuneCountInString(text[:r[0]])
					p.hits = append(p.hits, findHit{pos{i, a}, a + utf8.RuneCountInString(text[r[0]:r[1]]), hidden})
				}
			}
		}
	}
}

// findGo moves to the next match in direction d and selects it.
func (p *preview) findGo(m *Model, d int) tea.Cmd {
	if len(p.hits) == 0 {
		return nil
	}

	p.hit = (p.hit + d + len(p.hits)) % len(p.hits)
	p.selectHit(m)

	return nil
}

func (p *preview) selectHit(m *Model) {
	if h := p.hits[min(p.hit, len(p.hits)-1)]; h.hidden {
		p.unfold(m, h.at.line) // the same matches after, the rows shown
	}

	h := p.hits[min(p.hit, len(p.hits)-1)]
	a := h.at
	p.anchor, p.cur = &a, pos{h.at.line, h.end}
	p.top = max(p.cursorRow(m.pvW())-m.pvH()/2, 0)
	p.follow(m.pvW(), m.pvH())
}

// hitSelected reports whether the selection a..z is the current match.
func (p *preview) hitSelected(a, z pos) bool {
	if !p.find.on || p.hit >= len(p.hits) {
		return false
	}

	h := p.hits[p.hit]

	return a == h.at && z == pos{h.at.line, h.end}
}

// replacement is what match h becomes: the replace box's text, $1 expanded
// in regex mode, the match's case kept when AB is on. It reads the buffer's
// own line, so a tab inside the match stays a tab. ok is false when the
// text under the hit no longer matches.
func (p *preview) replacement(h findHit) (string, bool) {
	re := p.findRe()
	if re == nil || p.buf == nil {
		return "", false
	}

	raw := string(p.buf.line(h.at.line))
	start := docCol(raw, h.at.col)
	with := p.repl.Value()

	for _, mi := range re.FindAllStringSubmatchIndex(raw, -1) {
		if utf8.RuneCountInString(raw[:mi[0]]) != start {
			continue
		}

		out := replText(raw, re, with, p.findRegex, mi)
		if p.findPreserve {
			out = keepCase(raw[mi[0]:mi[1]], out)
		}

		return out, true
	}

	return "", false
}

// replaceOne is Replace (⏎ in the replace box): the current match becomes
// the replacement and the next one is selected. When the selection has left
// the match, it only goes back to it, as VS Code does.
func (p *preview) replaceOne(m *Model) tea.Cmd {
	if !p.editable() || len(p.hits) == 0 {
		return nil
	}

	h := p.hits[min(p.hit, len(p.hits)-1)]

	a, z, ok := p.selection()
	if !ok || !p.hitSelected(a, z) {
		p.selectHit(m)
		return nil
	}

	with, ok := p.replacement(h)
	if !ok {
		return nil
	}

	p.edit(m, h.at, pos{h.at.line, h.end}, with)
	p.findFrom = p.at() // search on from the end of the new text
	p.refind(m)

	return nil
}

// replaceAll rewrites every match in one undo step and reports how many.
func (p *preview) replaceAll(m *Model) tea.Cmd {
	re := p.findRe()
	if !p.editable() || re == nil || len(p.hits) == 0 {
		return nil
	}

	first, last := p.hits[0].at.line, p.hits[len(p.hits)-1].at.line
	lines := make([]string, 0, last-first+1)
	n := 0

	for i := first; i <= last; i++ {
		out, k := replaceLine(string(p.buf.line(i)), re, p.repl.Value(), p.findRegex, p.findPreserve)
		lines = append(lines, out)
		n += k
	}

	p.editRaw(m, pos{first, 0}, pos{last, len(p.buf.line(last))}, strings.Join(lines, "\n"))
	p.findFrom = p.at()
	p.refind(m)

	return flash("replaced "+plural(n, "match"), false)
}

// findSpans are the matches on line i while the find widget is open, the
// current one in a stronger color; nil when the line has none.
func (p *preview) findSpans(i int, line []rune, lo, hi int) []cellSpan {
	if !p.find.on {
		return nil
	}

	var spans []cellSpan

	k, _ := slices.BinarySearchFunc(p.hits, i, func(h findHit, line int) int { return h.at.line - line })
	for ; k < len(p.hits) && p.hits[k].at.line == i && !p.hits[k].hidden; k++ {
		h, bg := p.hits[k], pal.findMatchBg
		if k == p.hit {
			bg = pal.findHitBg
		}

		a, z := min(max(h.at.col, lo), hi), min(max(h.end, lo), hi) // clipped to the window, its cells from lo
		spans = append(spans, cellSpan{cellsOf(line[lo:a]), cellsOf(line[lo:z]), bg})
	}

	return spans
}

// cellSpan is a range of cells painted with a background.
type cellSpan struct {
	from, to int
	bg       color.Color
}

// underBg lays a background under styled text, through the resets that end its
// own colors.
func underBg(styled string, bg color.Color) string {
	on, _, _ := strings.Cut(lipgloss.NewStyle().Background(bg).Render("x"), "x")
	return on + strings.NewReplacer("\x1b[m", "\x1b[m"+on, "\x1b[0m", "\x1b[0m"+on).Replace(styled) + "\x1b[m"
}

func (p *preview) findStatus() string {
	switch {
	case p.find.input.Value() == "":
		return ""
	case len(p.hits) == 0:
		return "No results"
	}

	return fmt.Sprintf("%d of %d", p.hit+1, len(p.hits))
}

// toggleFind flips match case, whole word or regular expression, VS Code's
// ⌥c ⌥w ⌥r, and searches again.
func (p *preview) toggleFind(m *Model, key string) {
	switch key {
	case "alt+c":
		p.findCase = !p.findCase
	case "alt+w":
		p.findWord = !p.findWord
	case "alt+r":
		p.findRegex = !p.findRegex
	case "alt+p": // preserve case changes the replacement only
		p.findPreserve = !p.findPreserve
		return
	}

	p.refind(m)
}

// findW is the widest the find widget gets; findStatusW fits "123 of 456".
const findW, findStatusW = 60, 12

// findRect is where the find widget sits in the editor body: its first column
// and its width. It covers the body's top findH rows at the right edge, as
// VS Code's floats at the top right.
func (p *preview) findRect(m *Model) (x0, bw int, ok bool) {
	w := m.pvW()
	bw = min(findW, w-2)

	return w - bw - 1, bw, p.find.on && p.ready && bw >= 36 && m.pvH() >= p.findH()
}

// findH is the widget's height in rows: the frame around the query, plus a
// row for the replace box while it is open.
func (p *preview) findH() int { return 3 + b2i(p.replOn) }

// findActs are the widget's buttons after the query, placed for a widget bw
// wide: match case, whole word and regex, then previous, next and close.
// field is the query's width; the replace box below it is as wide.
func (p *preview) findActs(bw int) (field int, acts []rowAction) {
	acts = []rowAction{
		{g: icCase, tip: "Match Case  M-c", run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+c"); return nil }},
		{g: icWord, tip: "Match Whole Word  M-w", run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+w"); return nil }},
		{g: icRegex, tip: "Use Regular Expression  M-r", run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+r"); return nil }},
		{g: icUp, tip: "Previous Match  ⇧⏎", run: func(m *Model) tea.Cmd { return m.pv.findGo(m, -1) }},
		{g: icDown, tip: "Next Match  ⏎", run: func(m *Model) tea.Cmd { return m.pv.findGo(m, 1) }},
		{g: icClose, tip: "Close  esc", run: func(m *Model) tea.Cmd { m.pv.closeFind(); return nil }},
	}

	return findLayout(acts, bw), acts
}

// noteTip notes the tip of the button of acts under the mouse, acts laid out
// from screen column x0 on row y.
func (m *Model) noteTip(acts []rowAction, x0, y int) {
	if a, ok := hit(acts, m.mouseX-x0); ok && m.mouseY == y {
		m.tip = a.tip
	}
}

// findLayout places a find widget's six buttons (three toggles, previous,
// next, close) for a widget bw wide and returns the query's width.
func findLayout(acts []rowAction, bw int) (field int) {
	used := 0

	for i := range acts {
		acts[i].w = 1 + ansi.StringWidth(acts[i].g.s())
		used += acts[i].w
	}
	// "│ ▸ " query, toggles, "  " status, arrows, " │"
	field = bw - used - findStatusW - 8
	x := 4 + field

	for i := range acts {
		if i == 3 {
			x += 2 + findStatusW
		}

		acts[i].x = x
		x += acts[i].w
	}

	return field
}

// replActs are the replace row's buttons, under the query's toggles:
// preserve case, then replace and replace all.
func (p *preview) replActs(field int) []rowAction {
	acts := []rowAction{
		{g: icPreserve, tip: "Preserve Case  M-p", run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+p"); return nil }},
		{g: icReplace, tip: "Replace  ⏎", run: func(m *Model) tea.Cmd { return m.pv.replaceOne(m) }},
		{g: icReplaceAll, tip: "Replace All  ^M-⏎", run: func(m *Model) tea.Cmd { return m.pv.replaceAll(m) }},
	}
	x := 4 + field

	for i := range acts {
		acts[i].w = 1 + ansi.StringWidth(acts[i].g.s())
		if i == 1 {
			x += 2
		}

		acts[i].x = x
		x += acts[i].w
	}

	return acts
}

// findBox draws the find widget: the query, its toggles, "n of m" and the
// buttons, framed in the accent color while it has the keyboard. With the
// replace box open a second row holds the replacement, AB and the two
// replace buttons; the chevron before the query opens and closes it.
func (p *preview) findBox(m *Model) (box []string, x, y int, ok bool) {
	x0, bw, ok := p.findRect(m)
	if !ok || !m.showsPreview() {
		return nil, 0, 0, false
	}

	field, acts := p.findActs(bw)
	edge := findEdge(p.find.editing)
	x, y = m.mainX()+x0, m.mainY()+m.headH()+m.stripH()
	m.noteTip(acts, x, y+1)

	if p.editable() { // the chevron before the query
		m.noteTip([]rowAction{{x: 1, w: 3, tip: "Toggle Replace  ^h"}}, x, y+1) // where a click takes it
	}

	if p.replOn {
		m.noteTip(p.replActs(field), x, y+2)
	}

	lead := "  "
	if p.editable() { // the chevron opens the replace box; a read-only file has none
		lead = dim.Render(strings.TrimSuffix(chevron(p.replOn), " ")) + " "
	}

	rule := strings.Repeat("─", bw-2)
	box = []string{edge.Render("╭" + rule + "╮"), findRow(m, field, acts, findLook{
		input: &p.find.input, editing: p.find.editing, lead: lead,
		on: [3]bool{p.findCase, p.findWord, p.findRegex}, status: p.findStatus(),
		miss: len(p.hits) == 0 && p.find.input.Value() != "",
	})}

	if p.replOn {
		onStyle := lipgloss.NewStyle().Background(pal.accent).Foreground(pal.buttonFg)

		var r strings.Builder
		r.WriteString(edge.Render("│") + "   " + findInput(m, &p.repl, field))

		for i, a := range p.replActs(field) {
			st := dim
			if i == 0 && p.findPreserve {
				st = onStyle
			}

			if i == 1 {
				r.WriteString("  ")
			}

			r.WriteString(" " + st.Render(a.g.s()))
		}

		r.WriteString(blank(bw - 1 - ansi.StringWidth(r.String())))
		r.WriteString(edge.Render("│"))
		box = append(box, r.String())
	}

	return append(box, edge.Render("╰"+rule+"╯")), x, y, true
}

// findLook is what one find widget shows on its query row.
type findLook struct {
	input   *textinput.Model
	editing bool
	lead    string  // what stands before the query: the replace chevron, or blanks
	on      [3]bool // match case, whole word, regular expression
	status  string  // "n of m"
	miss    bool    // a query without a match: the status in the error color
}

// findEdge is a find widget's frame, in the accent color while it has the keyboard.
func findEdge(editing bool) lipgloss.Style {
	if editing {
		return fg(pal.accent)
	}

	return dim
}

// findInput draws a find widget's text box field cells wide.
func findInput(m *Model, t *textinput.Model, field int) string {
	t.SetStyles(inputStyles(m.dark))
	t.SetWidth(max(field, 1))

	text := realCaret(t.View())
	if pad := field - ansi.StringWidth(text); pad > 0 {
		return text + lipgloss.NewStyle().Background(pal.inputBg).Render(blank(pad))
	}

	return ansi.Truncate(text, field, "")
}

// findRow is a find widget's query row: the box, the toggles lit while on,
// "n of m" and the buttons findLayout placed, inside the frame's sides.
func findRow(m *Model, field int, acts []rowAction, l findLook) string {
	edge := findEdge(l.editing)

	status := dim
	if l.miss {
		status = fg(pal.errc)
	}

	onStyle := lipgloss.NewStyle().Background(pal.accent).Foreground(pal.buttonFg)

	var b strings.Builder
	b.WriteString(edge.Render("│") + " " + l.lead + findInput(m, l.input, field))

	for i, a := range acts {
		if i == 3 {
			b.WriteString("  " + status.Render(fit(l.status, findStatusW)))
		}

		st := dim
		if i < 3 && l.on[i] {
			st = onStyle
		}

		b.WriteString(" " + st.Render(a.g.s()))
	}

	b.WriteString(" " + edge.Render("│"))

	return b.String()
}

// findKey handles the box while it has the focus; ok is false for other keys.
func (p *preview) findKey(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !p.find.editing {
		return nil, false
	}

	switch k.String() {
	case "esc":
		p.closeFind()
		return nil, true

	case "ctrl+f": // back to the query from the replacement; from the query, closes
		if p.repl.Focused() {
			return p.focusReplace(false), true
		}

		p.closeFind()

		return nil, true

	case "ctrl+h":
		return p.toggleReplace(), true
	case "tab", "shift+tab":
		return p.focusReplace(!p.repl.Focused()), true
	case "enter":
		if p.repl.Focused() {
			return p.replaceOne(m), true
		}

		return p.findGo(m, 1), true

	case "down", "ctrl+n":
		return p.findGo(m, 1), true
	case "shift+enter", "up":
		return p.findGo(m, -1), true
	case "ctrl+shift+1", "ctrl+!": // VS Code's Replace
		return p.replaceOne(m), true
	case "ctrl+alt+enter": // VS Code's Replace All
		return p.replaceAll(m), true
	case "alt+c", "alt+w", "alt+r", "alt+p":
		p.toggleFind(m, k.String())
		return nil, true
	}

	if p.repl.Focused() {
		var cmd tea.Cmd

		p.repl, cmd = p.repl.Update(k)

		return cmd, true
	}

	before := p.find.input.Value()

	var cmd tea.Cmd

	p.find.input, cmd = p.find.input.Update(k)
	if p.find.input.Value() != before {
		p.refind(m)
	}

	return cmd, true
}

// openHere opens the file a diff shows, on the line under the cursor: the new
// line number, or the old one on a deletion.
func (p *preview) openHere(m *Model) tea.Cmd {
	if p.meta == nil || p.path == "" {
		return nil
	}

	path := p.path
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.root, path)
	}

	line := 0
	if c := p.at().line; c < len(p.meta) {
		line = max(p.meta[c].b, p.meta[c].a)
	}

	return m.openFileAt(path, max(line-1, 0), 0, 0, false)
}

// items are the preview's commands. Diffs of tracked files stage, unstage
// or revert the selected lines, like VS Code's Stage Selected Ranges.
func (p *preview) items(m *Model) []item {
	switch {
	case p.kind == pvTable && p.tbl != nil:
		return p.tbl.items()
	case p.kind == pvSettings:
		return nil
	}

	key := func(k rune) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.pv.key(m, tea.KeyPressMsg{Code: k, Text: string(k)}) }
	}
	edit, file := p.editable(), p.kind == pvFile && !p.untitled()

	var review, code, format, history, clip, find, more []item

	if p.kind == pvDiff && p.meta != nil && p.entry.Letter != 'U' && p.entry.Letter != '!' {
		switch {
		case p.entry.Staged:
			review = append(review, item{label: "Unstage Selected Ranges", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.UnstageLines) }})
		default:
			review = append(review,
				item{label: "Stage Selected Ranges", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.StageLines) }},
				item{label: "Revert Selected Ranges…", run: func(m *Model) tea.Cmd { return m.pv.confirmRevert(m) }})
		}
	}

	if p.kind == pvGH && p.gh != nil {
		review = append(review, p.ghItems()...)
	}

	review = append(review, p.conflictItems()...)

	if _, argv := m.lspCommand(p.path); p.kind == pvFile && len(argv) > 0 {
		code = append(code,
			item{label: "Go to Definition", hint: "f12", run: func(m *Model) tea.Cmd { return m.lspGo(false) }},
			item{label: "Find References", hint: "⇧f12", run: func(m *Model) tea.Cmd { return m.lspGo(true) }})
	}

	if file {
		code = append(code, item{label: "Go to Symbol…", hint: "^⇧o", run: func(m *Model) tea.Cmd { return m.gotoSymbol("@") }})
	}

	if _, argv := m.lspCommand(p.path); p.kind == pvFile && len(argv) > 0 {
		code = append(code,
			item{label: "Rename Symbol…", hint: "F2", run: func(m *Model) tea.Cmd { return m.lspRename() }},
			item{label: "Quick Fix…", hint: "^.", run: func(m *Model) tea.Cmd { return m.lspActions() }},
			item{label: "Show Hover", hint: "^k", run: func(m *Model) tea.Cmd { return m.showHover() }})
	}

	if edit {
		format = []item{
			{label: "Format Document", hint: "^⇧i", run: func(m *Model) tea.Cmd { return m.pv.formatDoc(m) }},
			{label: "Toggle Line Comment", hint: "^/", run: func(m *Model) tea.Cmd { return m.pv.toggleComment(m) }},
		}
		history = []item{
			{label: "Undo", hint: "^z", run: func(m *Model) tea.Cmd { return m.pv.history(m, true) }},
			{label: "Redo", hint: "^y", run: func(m *Model) tea.Cmd { return m.pv.history(m, false) }},
		}
		clip = append(clip, item{label: "Cut", hint: "^x", run: editorChord('x', tea.ModCtrl)})
	}

	clip = append(clip, item{label: "Copy", hint: "^c", run: editorChord('c', tea.ModCtrl)})
	if edit {
		clip = append(clip, item{label: "Paste", hint: "^v", run: func(m *Model) tea.Cmd { m.focus = onMain; return pasteClipboard }})
	}

	find = append(find, item{label: "Find", hint: "^f", run: editorChord('f', tea.ModCtrl)})
	if edit {
		find = append(find, item{label: "Replace", hint: "^h", run: editorChord('h', tea.ModCtrl)})
	}

	find = append(find, item{label: "Go to Line…", hint: "^g", run: func(m *Model) tea.Cmd { return m.gotoLineQuery(":") }})

	if edit {
		more = append(more,
			item{label: "Save File", hint: "^s", run: func(m *Model) tea.Cmd { return m.pv.save(m, false) }},
			item{label: "Save As…", run: func(m *Model) tea.Cmd { return m.saveAsPrompt(m.edIdx, nil) }})
	}

	if p.meta != nil {
		more = append(more, item{label: "Open File at This Line", hint: "O", run: func(m *Model) tea.Cmd { return m.pv.openHere(m) }})
	}

	if p.fileDiff() {
		more = append(more, item{label: "Toggle Collapse Unchanged Regions", hint: "z", run: key('z')})
	}

	if p.kind == pvFile && isMarkdown(p.path) {
		more = append(more,
			item{label: "Toggle Rendered Markdown", hint: "^⇧v", run: func(m *Model) tea.Cmd { return m.toggleRendered() }},
			item{label: "Markdown Side by Side", hint: "M-v", run: editorChord('v', tea.ModAlt)})
	}

	if p.kind != pvShow && !p.fromGH() && !p.untitled() {
		more = append(more, item{label: "Edit in $EDITOR", hint: "e", run: key('e')})
	}

	// VS Code's groups: going somewhere in the code, changing it, history,
	// the clipboard, the selection, finding; the file's own last.
	return menuGroups(review, code, format, history, clip, []item{{label: "Select All", hint: "^a", run: editorChord('a', tea.ModCtrl)}}, find, more)
}

// editorChord runs a key in the editor, as its key would with the keyboard
// there.
func editorChord(code rune, mod tea.KeyMod) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		m.focus = onMain
		return m.pv.key(m, tea.KeyPressMsg{Code: code, Mod: mod})
	}
}

// keyItems are the editor's commands for the palette and [keys] alone, most
// with keys of their own: the context menu would only grow long with them.
func (p *preview) keyItems(_ *Model) []item {
	if p.kind == pvTable || p.kind == pvSettings { // its items hold its keys
		return nil
	}

	chord := editorChord

	items := []item{
		{label: "Toggle Word Wrap", hint: "M-z", run: chord('z', tea.ModAlt)},
		{label: "Change Indentation…", run: func(m *Model) tea.Cmd { return m.indentPicker() }}, // the status bar's Tab Size
		{label: "Expand Selection", hint: "M-⇧→", run: chord(tea.KeyRight, tea.ModAlt|tea.ModShift)},
		{label: "Shrink Selection", hint: "M-⇧←", run: chord(tea.KeyLeft, tea.ModAlt|tea.ModShift)},
	}
	if len(p.changes) > 0 && p.meta == nil {
		items = append(items,
			item{label: "Go to Next Change", hint: "M-F5", run: func(m *Model) tea.Cmd { return m.gotoFileChange(1) }},
			item{label: "Go to Previous Change", hint: "⇧M-F5", run: func(m *Model) tea.Cmd { return m.gotoFileChange(-1) }},
			item{label: "Show Next Change", hint: "M-F3", run: func(m *Model) tea.Cmd { return m.showChange(m.pv.stepChange(m.pv.at().line, 1)) }},
			item{label: "Show Previous Change", hint: "⇧M-F3", run: func(m *Model) tea.Cmd { return m.showChange(m.pv.stepChange(m.pv.at().line, -1)) }})
	}

	if p.foldable() {
		items = append(items,
			item{label: "Fold", hint: "^⇧[", run: func(m *Model) tea.Cmd { return m.foldCmd("fold") }},
			item{label: "Unfold", hint: "^⇧]", run: func(m *Model) tea.Cmd { return m.foldCmd("unfold") }},
			item{label: "Toggle Fold", run: func(m *Model) tea.Cmd { return m.foldCmd("toggle") }},
			item{label: "Fold All", run: func(m *Model) tea.Cmd { return m.foldCmd("all") }},
			item{label: "Unfold All", run: func(m *Model) tea.Cmd { return m.foldCmd("none") }})
	}

	if p.diagPath() != "" {
		items = append(items,
			item{label: "Go to Next Problem", hint: "F8", run: func(m *Model) tea.Cmd { return m.gotoProblem(1) }},
			item{label: "Go to Previous Problem", hint: "⇧F8", run: func(m *Model) tea.Cmd { return m.gotoProblem(-1) }})
	}

	if p.meta != nil { // the palette's, as VS Code keeps them off the diff's context menu
		items = append(items,
			item{label: "Go to Next Change", hint: "]", run: chord(']', 0)},
			item{label: "Go to Previous Change", hint: "[", run: chord('[', 0)})
	}

	if p.kind == pvDiff || p.kind == pvCmp || p.kind == pvShow || p.kind == pvGHDiff {
		items = append(items,
			item{label: "Go to Next File", hint: "}", run: chord('}', 0)},
			item{label: "Go to Previous File", hint: "{", run: chord('{', 0)})
	}

	if p.editable() {
		items = append(items,
			item{label: "Duplicate Line", hint: "^d", run: chord('d', tea.ModCtrl)},
			item{label: "Copy Line Up", hint: "M-⇧↑", run: chord(tea.KeyUp, tea.ModAlt|tea.ModShift)},
			item{label: "Copy Line Down", hint: "M-⇧↓", run: chord(tea.KeyDown, tea.ModAlt|tea.ModShift)},
			item{label: "Delete Line", hint: "^⇧k", run: chord('k', tea.ModCtrl|tea.ModShift)},
			item{label: "Move Line Up", hint: "M-↑", run: chord(tea.KeyUp, tea.ModAlt)},
			item{label: "Move Line Down", hint: "M-↓", run: chord(tea.KeyDown, tea.ModAlt)},
			item{label: "Sort Lines Ascending", run: func(m *Model) tea.Cmd {
				m.focus = onMain
				return m.pv.sortLines(m)
			}},
			item{label: "Trigger Suggest", hint: "^space", run: func(m *Model) tea.Cmd {
				m.focus = onMain
				return m.pv.suggest(m, "", true)
			}})
	}

	return items
}

// menu opens the preview's context menu at x, y.
func (p *preview) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(p.items(m), x, y) }

// menuAt is where a menu opened by key goes: under the cursor's row, as VS
// Code opens its context menu at the caret (rect flips it above when it does
// not fit); centred while no cursor shows.
func (p *preview) menuAt(m *Model) (x, y int) {
	if p.split(m) {
		if i := splitIndex(splitRows(p.meta), p.at().line) - p.top; i >= 0 && i < m.pvH() {
			return m.mainX(), m.mainY() + m.headH() + m.stripH() + i + 1
		}
	} else if _, _, ok := p.cursor(m.pvW(), m.pvH()); ok {
		return m.lightbulb(0)
	}

	return -1, 0
}

// changedLines are the selected diff changes: deletions by old line number,
// additions by new line number. A selection ending at column 0 leaves that
// line out; side by side, a selection is its rows, both halves of them;
// without a selection the change under the cursor counts, its deletion with
// its additions, so staging one never splits a line's edit.
func (p *preview) changedLines(m *Model) (dels, adds map[int]bool) {
	dels, adds = map[int]bool{}, map[int]bool{}
	note := func(i int) {
		switch md := p.meta[i]; md.kind {
		case 'd':
			dels[md.a] = true
		case 'a':
			adds[md.b] = true
		}
	}

	a, b, ok := p.selection()
	if p.split(m) { // its cursor is a row: with nothing selected, that row
		rows := splitRows(p.meta)
		ra, rz := p.splitRange(rows)

		for _, sr := range rows[ra : rz+1] {
			for _, i := range []int{sr.l, sr.r} {
				if i >= 0 {
					note(i)
				}
			}
		}

		return dels, adds
	}

	if !ok { // the change under the cursor, its deletion with its additions, as VS Code's
		a, b = p.at(), p.at()

		cs := changes(p.meta)
		if i := slices.IndexFunc(cs, func(c [2]int) bool { return a.line >= c[0] && a.line < c[1] }); i >= 0 {
			a.line, b.line = cs[i][0], cs[i][1]-1
		}
	} else if b.col == 0 && b.line > a.line {
		b.line--
	}

	for i := a.line; i <= b.line && i < len(p.meta); i++ {
		note(i)
	}

	return dels, adds
}

func (p *preview) applyLines(m *Model, op git.LineOp) tea.Cmd {
	dels, adds := p.changedLines(m)

	n := len(dels) + len(adds)
	if n == 0 {
		return flash("no changed lines selected", true)
	}

	verb := [...][2]string{{"staging", "staged"}, {"unstaging", "unstaged"}, {"reverting", "reverted"}}[op]
	e := p.entry
	p.anchor = nil

	return m.scm.run(p.root, verb[0], func(root string) scmMsg {
		return scmMsg{text: fmt.Sprintf("%s %s in %s", verb[1], plural(n, "changed line"), e.Path), err: git.ApplyLines(root, e, op, dels, adds)}
	})
}

func (p *preview) confirmRevert(m *Model) tea.Cmd {
	m.modal = newDialog("Revert the selected changes in "+p.path+"?",
		item{label: "Revert", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.RevertLines) }},
		cancelItem())
	m.modal.x, m.modal.y = p.menuAt(m) // off the lines it asks about, where the menu that asked was

	return nil
}

func (p *preview) close(m *Model) {
	*p = preview{wrap: p.wrap}
	m.preview = false
}

// moved applies a navigation key to the cursor; ok is false for other keys.
func (p *preview) moved(key string, h int) (pos, bool) {
	c, last := p.at(), p.lastLine()

	up, down := p.visibleLine(max(c.line-1, 0), -1), c.line // the lines either side, over folded blocks
	if e := p.shutEnd(c.line); e > c.line {
		down = e
	}

	down = min(down+1, last)

	switch key {
	case "up", "k":
		return pos{up, p.cur.col}, true
	case "down", "j":
		return pos{ifElse(p.hiddenBy(down) >= 0, c.line, down), p.cur.col}, true
	case "left", "h":
		if c.col > 0 {
			return pos{c.line, c.col - 1}, true
		}

		if c.line > 0 {
			return pos{up, p.lineLen(up)}, true
		}

		return c, true

	case "right", "l":
		if c.col < p.lineLen(c.line) {
			return pos{c.line, c.col + 1}, true
		}

		if down > c.line {
			return pos{down, 0}, true
		}

		return c, true

	case "ctrl+left": // VS Code's cursorWordStartLeft
		return p.wordLeft(c), true
	case "ctrl+right": // and cursorWordEndRight
		return p.wordRight(c), true
	case "home", "0":
		return pos{c.line, 0}, true
	case "end", "$":
		return pos{c.line, p.lineLen(c.line)}, true
	case "pgup", "b":
		return pos{p.visibleLine(max(c.line-h, 0), -1), p.cur.col}, true
	case "pgdown", "f", "space":
		return pos{p.visibleLine(min(c.line+h, last), -1), p.cur.col}, true
	case "g", "ctrl+home":
		return pos{}, true
	case "G", "ctrl+end":
		return pos{last, p.lineLen(last)}, true
	}

	return pos{}, false
}

// runeClass groups runes the way a word move walks them: blanks, word
// characters, and any other run of punctuation.
func runeClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case isWordRune(r):
		return 1
	}

	return 2
}

// wordAt is the run of one kind of character under c, as VS Code's double
// click selects it: a word, a run of punctuation or of blanks. At a line's
// end it is the run before; an empty line has none.
func (p *preview) wordAt(c pos) (a, b pos) {
	l := p.plain[c.line]

	i := min(c.col, len(l))
	if i == len(l) {
		if i == 0 {
			return c, c
		}

		i--
	}

	cls, j, k := runeClass(l[i]), i, i+1
	for j > 0 && runeClass(l[j-1]) == cls {
		j--
	}

	for k < len(l) && runeClass(l[k]) == cls {
		k++
	}

	return pos{c.line, j}, pos{c.line, k}
}

// wordLeft is the start of the word before c, past the blanks between; at a
// line's start it is the end of the line above.
func (p *preview) wordLeft(c pos) pos {
	if c.col == 0 {
		if c.line > 0 {
			return pos{c.line - 1, p.lineLen(c.line - 1)}
		}

		return c
	}

	l, i := p.plain[c.line], c.col
	for i > 0 && runeClass(l[i-1]) == 0 {
		i--
	}

	if i > 0 {
		cls := runeClass(l[i-1])
		for i > 0 && runeClass(l[i-1]) == cls {
			i--
		}
	}

	return pos{c.line, i}
}

// wordRight is the end of the word after c, past the blanks before it; at a
// line's end it is the start of the line below.
func (p *preview) wordRight(c pos) pos {
	n := p.lineLen(c.line)
	if c.col >= n {
		if c.line < p.lastLine() {
			return pos{c.line + 1, 0}
		}

		return c
	}

	l, i := p.plain[c.line], c.col
	for i < n && runeClass(l[i]) == 0 {
		i++
	}

	if i < n {
		cls := runeClass(l[i])
		for i < n && runeClass(l[i]) == cls {
			i++
		}
	}

	return pos{c.line, i}
}

// closeAndRefocus is esc and q in an editor: unsaved text goes through the
// same question ⌃w asks, so neither key drops it silently.
func (p *preview) closeAndRefocus(m *Model) tea.Cmd {
	if p.dirty() && m.edIdx >= 0 && m.edIdx < len(m.editors) && m.editors[m.edIdx].id() == p.id() {
		return m.closeEditor(m.edIdx)
	}

	return p.closeNow(m)
}

func (p *preview) closeNow(m *Model) tea.Cmd {
	p.close(m)

	if m.sess == "" {
		for i := range m.cols() {
			if m.colRect(i).w > 0 && !m.railed(i) {
				m.focus = i
				break
			}
		}
	}

	return m.fetchScreen()
}

func (p *preview) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch {
	case p.kind == "": // the welcome screen: the keys its text lists, as Spaces takes them
		switch k.String() {
		case "n":
			if m.ws != "" {
				return m.harnessPicker(m.ws)
			}

		case "w":
			return m.ag.newWorktree(m, nil)
		case "a":
			return m.addProjectPrompt()
		case "3":
			return m.showView(viewAgents)
		}

		return nil

	case p.kind == pvTable && p.tbl != nil:
		return p.tbl.key(m, k)
	case p.kind == pvSettings && p.set != nil:
		return p.set.key(m, k)
	}

	if p.composing {
		return p.composeKey(m, k)
	}

	p.hov = hover{seq: p.hov.seq + 1} // a key puts the hover away, as VS Code's

	if cmd, ok := p.compKey(m, k); ok {
		return cmd
	}

	if p.sigKey(k) {
		return nil
	}

	if cmd, ok := p.findKey(m, k); ok {
		return cmd
	}

	if cmd, ok := p.vimKey(m, k); ok {
		return cmd
	}

	if cmd, ok := p.ghKey(m, k); ok {
		return cmd
	}

	if k.String() == "ctrl+f" { // find in this file, as VS Code does in an editor
		return p.startFind(m)
	}

	if k.String() == "ctrl+h" && p.editable() { // find and replace, VS Code's ⌃h
		return p.startReplace(m)
	}
	// In a file the keyboard belongs to the text; navigation keeps the arrows,
	// and the commands single letters used to run live on ⌃ keys, the context
	// menu and the palette.
	if cmd, ok := p.editKey(m, k); ok {
		return tea.Batch(cmd, p.afterEdit(m, k), p.sigAfter(m, k))
	}

	p.typed = false // not an edit: the cursor moves on its own from here
	w, h := m.pvW(), m.pvH()
	p.syncMarkdown(m)

	s := k.String()

	switch {
	case p.split(m) && p.ready:
		switch s {
		case "left", "h", "right", "l":
			p.scrollSplit(s, p.scrollRows(m), h)
			return nil
		}

		key := strings.TrimPrefix(s, "shift+")

		n, ok := p.splitMove(key, h)
		if !ok { // home, end, a word: the line it is on
			np, moved := p.moved(key, h)
			n, ok = np.line, moved
		}

		if ok {
			p.selectTo(n, strings.HasPrefix(s, "shift+"))
			p.followSplit(h)

			return nil
		}

	case p.scrollOnly(m):
		if p.scrollSplit(s, p.scrollRows(m), h) {
			return nil
		}

	case p.ready && len(p.plain) > 0:
		if np, ok := p.moved(strings.Replace(s, "shift+", "", 1), h); ok {
			if strings.Contains(s, "shift+") {
				if p.anchor == nil {
					a := p.at()
					p.anchor = &a
				}
			} else {
				p.anchor = nil
			}

			p.cur = np
			p.follow(w, h)

			return nil
		}
	}

	switch s {
	case "esc":
		if p.qd != nil {
			p.closeQuickDiff()
			return nil
		}

		if p.anchor != nil {
			p.anchor = nil
			return nil
		}

		return p.closeAndRefocus(m)

	case "q":
		return m.closeEditor(m.edIdx)
	case "m", "shift+f10": // ⇧F10 is the context menu key an editor leaves free
		x, y := p.menuAt(m)

		return p.menu(m, x, y)

	case "alt+shift+right": // VS Code's Expand Selection; ctrl+shift+→ selects a word
		return p.expandSel(m, 1)
	case "alt+shift+left":
		return p.expandSel(m, -1)
	case "ctrl+a":
		p.anchor = &pos{}
		p.cur = pos{p.lastLine(), p.lineLen(p.lastLine())}
		p.follow(w, h)

	case "y", "ctrl+c":
		text := p.selectedText()
		if p.split(m) {
			text = p.halfText()
		}

		if text != "" {
			return setClipboard(text, fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)))
		}

		return setClipboard(p.text(), "copied preview")

	case "w", "alt+z": // ⌥z is VS Code's word wrap
		return m.toggleWrap()

	case "p":
		if p.kind == pvFile && isMarkdown(p.path) {
			return p.toggleMarkdown(m, 1)
		}

	case "H":
		return p.pickRev(m)
	case "f12":
		return m.lspGo(false)
	case "shift+f12":
		return m.lspGo(true)
	case "ctrl+k": // VS Code's ⌃k ⌃i, a chord pando has no room for
		return m.showHover()
	case "ctrl+shift+[", "ctrl+{": // VS Code's Fold and Unfold; a terminal without the kitty protocol sends neither
		return m.foldCmd("fold")
	case "ctrl+shift+]", "ctrl+}":
		return m.foldCmd("unfold")
	case "alt+v": // side by side; VS Code's ⌃k v is a chord pando has no room for
		if p.kind == pvFile && isMarkdown(p.path) {
			return p.toggleMarkdown(m, 2)
		}

	case "ctrl+up", "ctrl+down": // VS Code's scroll line up/down: the view moves, the cursor stays
		n := len(p.rows(w))
		if p.scrollOnly(m) {
			n = p.scrollRows(m)
		}

		p.top = max(0, min(p.top+map[bool]int{true: -1, false: 1}[s == "ctrl+up"], max(n-h, 0)))

		return nil

	case "ctrl+.", "alt+enter", ".": // ⌃. is VS Code's; alt+⏎ for the terminals that cannot send it
		return m.lspActions()
	case "f2":
		return m.lspRename()
	case "ctrl+shift+i": // VS Code's Format Document on Linux
		return p.formatDoc(m)
	case "f3", "n", "shift+f3", "N": // walk the matches with the box closed
		if len(p.hits) > 0 {
			return p.findGo(m, map[bool]int{true: -1, false: 1}[s == "shift+f3" || s == "N"])
		}

	case "O":
		return p.openHere(m)
	case "enter":
		if p.meta != nil && p.unfold(m, p.at().line) {
			return nil
		}

	case "]", "[": // and VS Code's ⌥F5 ⇧⌥F5, everywhere (keybindings)
		if p.meta != nil {
			return p.gotoChange(m, ifElse(s == "]", 1, -1))
		}

	case "}", "{":
		return m.stepFile(ifElse(s == "}", 1, -1))
	case "z":
		if p.fileDiff() {
			return m.toggleDiffFull()
		}

	case "s":
		if p.kind == pvFile && isMarkdown(p.path) {
			return p.toggleMarkdown(m, 2)
		}

		if p.meta == nil {
			return nil
		}

		cmd := m.toggleDiffView()
		if m.st.Settings.DiffView == "split" && !p.split(m) {
			return tea.Batch(cmd, splitFlash())
		}

		return cmd

	case "e":
		if p.kind == pvFile {
			return m.edit(p.path)
		}

		if p.kind == pvDiff {
			return m.edit(filepath.Join(p.root, p.path))
		}

	case "o":
		if p.kind == pvFile {
			if err := openExternal(p.path); err != nil {
				return flash(err.Error(), true)
			}
		}
	}

	return nil
}

func (p *preview) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	w, h, mo := m.pvW(), m.pvH(), msg.Mouse()

	_, click := msg.(tea.MouseClickMsg)
	if p.kind == "" { // the welcome screen: double click it and start writing
		if click && mo.Button == tea.MouseLeft && y >= 0 && m.clicks.double(mo.X, mo.Y) {
			return m.newUntitled()
		}

		return nil
	}

	p.syncMarkdown(m)

	if click {
		p.closeComp()
		p.closeSig()
		p.hov = hover{seq: p.hov.seq + 1}
		p.typed = false // the cursor is being put somewhere on purpose
	}

	if x0, bw, ok := p.findRect(m); ok && y >= 0 && y < p.findH() && x >= x0 && x < x0+bw { // over the find widget
		if !click || mo.Button != tea.MouseLeft {
			return nil
		}

		field, acts := p.findActs(bw)
		switch {
		case y == 1 && x-x0 >= 1 && x-x0 < 4 && p.editable(): // the chevron
			p.find.editing = true
			return p.toggleReplace()

		case y == 1:
			if a, ok := hit(acts, x-x0); ok {
				return a.run(m)
			}

		case y == 2 && p.replOn:
			if a, ok := hit(p.replActs(field), x-x0); ok {
				return a.run(m)
			}

			p.find.editing = true

			return p.focusReplace(true)
		}

		p.find.editing = true

		return p.focusReplace(false)
	}

	if click && y == -1 && m.headH() > 0 && mo.Button == tea.MouseLeft {
		switch {
		case x < 3 && !p.pathHeader(): // the ✕ in the header
			return m.closeEditor(m.edIdx)
		default:
			if a, ok := p.ghHit(m, x); ok {
				return a.run(m)
			}

			if p.crumbClick(m, x, mo.X, mo.Y) {
				return nil
			}

			if a, ok := hit(p.buttons(m, m.mainW()), x); ok && !m.buttonsUp() { // the header spans the scrollbar too
				return a.run(m)
			}
		}
	}

	if p.kind == pvTable && p.tbl != nil && y >= 0 {
		return p.tbl.mouse(m, msg, x, y)
	}

	if p.kind == pvSettings && p.set != nil && y >= 0 {
		return p.set.mouse(m, msg, x, y)
	}

	if !p.ready || p.err != "" || len(p.plain) == 0 {
		return nil
	}

	if x >= w && y >= 0 && y < h { // the scrollbar
		m.overBar = barPoint{"editor", y}

		if click && mo.Button == tea.MouseLeft {
			if p.markClick(m, y) {
				return nil
			}

			return m.barClick("editor", y, mo.Y-y, func() vbar { return p.bar(m) }, func(top int) tea.Cmd {
				p.top = top
				return nil
			})
		}
	}

	if y == h && x >= p.gutter() && x < w { // the horizontal bar under the text
		bx := x - p.gutter()
		m.overBar = barPoint{"editor-h", bx}

		if click && mo.Button == tea.MouseLeft {
			return m.hbarClick("editor-h", bx, mo.X-bx, func() vbar { return p.hbar(m, m.pvW()) }, func(left int) tea.Cmd {
				p.left = left
				return nil
			})
		}
	}

	if vis := p.rows(w); p.qd != nil && y >= 0 && y < h && p.top+y < len(vis) && vis[p.top+y].line < 0 {
		if cmd, ok := p.qd.mouse(m, msg, x, vis[p.top+y].from); ok {
			return cmd
		}
	}

	if p.kind == pvFile && p.md != 1 { // the gutter's change lane, VS Code's: the change under the pointer widens, a click shows it or closes it
		ci := p.laneChange(w, h, x, y)
		if _, move := msg.(tea.MouseMotionMsg); move {
			p.markHot, p.gutHot = ci+1, x >= 0 && x < p.gutter() && y >= 0 && y < h
		}

		if click && mo.Button == tea.MouseLeft && ci >= 0 {
			return m.showChange(ci)
		}

		if click && mo.Button == tea.MouseLeft && p.foldClick(w, h, x, y) {
			return nil
		}
	}

	if _, move := msg.(tea.MouseMotionMsg); move && mo.Button == tea.MouseNone && len(p.probs) > 0 && !p.scrollOnly(m) {
		p.probTip = nil

		if np := p.posAt(w, x, y); y >= 0 && y < h && x >= p.gutter() && np.line < len(p.plain) {
			if pr := p.probAt(np.line, np.col); pr != nil {
				g, _ := sevMark(pr.sev)
				p.probTip = &tipAt{g + " " + pr.msg, mo.X, mo.Y}
			}
		}
	}

	if _, move := msg.(tea.MouseMotionMsg); move && mo.Button == tea.MouseNone && p.kind == pvFile {
		np := p.posAt(w, x, y)
		if cmd := p.hoverMove(m, np, y >= 0 && y < h && x >= p.gutter() && !p.scrollOnly(m) && !p.split(m)); cmd != nil {
			return cmd
		}
	}

	switch msg.(type) {
	case tea.MouseWheelMsg:
		if dx, ok := wheelX(mo); ok { // a tilt wheel, or shift+wheel, pans
			p.left = max(p.left+dx, 0)
			return nil
		}

		n := len(p.rows(w))
		if p.scrollOnly(m) {
			n = p.scrollRows(m)
		}

		p.top = max(0, min(p.top+wheelDelta(mo), n-h))

	case tea.MouseClickMsg:
		if mo.Button == tea.MouseRight && y >= 0 && y < h {
			// Like an editor: a right click outside the selection moves the cursor there.
			switch {
			case p.split(m):
				rows := splitRows(p.meta)
				if a, z := p.splitRange(rows); a > p.top+y || z < p.top+y {
					p.oldHalf = x <= (w-1)/2
					if n, ok := p.splitPos(w, x, y); ok {
						p.selectTo(n, false)
					}
				}

			case !p.scrollOnly(m):
				np := p.posAt(w, x, y)
				if a, b, ok := p.selection(); !ok || np.line < a.line || np.line > b.line {
					p.anchor, p.cur = nil, np
				}
			}

			return p.menu(m, mo.X, mo.Y)
		}

		if mo.Button != tea.MouseLeft || y < 0 || y >= h {
			return nil
		}

		if p.split(m) {
			if n, ok := p.splitPos(w, x, y); ok && !p.unfold(m, n) {
				if mo.Mod&tea.ModShift == 0 { // a shift+click extends on the half it started
					p.oldHalf = x <= (w-1)/2
				}

				p.selectTo(n, mo.Mod&tea.ModShift != 0)

				m.drag = &drag{kind: dragSelect}
			}

			return nil
		}

		if p.scrollOnly(m) {
			return nil
		}

		if cmd, ok := p.conflictClick(m, w, x, y); ok {
			return cmd
		}

		np := p.posAt(w, x, y)
		if p.meta != nil && mo.Mod&tea.ModShift == 0 && p.unfold(m, np.line) {
			return nil
		}

		if cmd, ok := p.ghClick(m, np, mo); ok {
			return cmd
		}

		switch n := m.clicks.count(mo.X, mo.Y); {
		case mo.Mod&tea.ModShift != 0:
			if p.anchor == nil {
				a := p.at()
				p.anchor = &a
			}

		case n == 2: // VS Code's double click selects the word, the triple the line
			a, b := p.wordAt(np)
			p.anchor, p.cur = &a, b

			return nil

		case n == 3: // the cursor at the line's end, not on the next line
			a, b := pos{np.line, 0}, pos{np.line, p.lineLen(np.line)}
			p.anchor, p.cur = &a, b

			return nil

		default:
			p.anchor = nil
		}

		p.cur = np
		m.drag = &drag{kind: dragSelect}
	}

	return nil
}

// wheelDelta is the rows a wheel event scrolls a list: three, up or down.
func wheelDelta(mo tea.Mouse) int {
	if mo.Button == tea.MouseWheelUp {
		return -3
	}

	return 3
}

// wheelX is the horizontal step a wheel event asks for: a tilt wheel, or
// shift+wheel, which is how most terminals send sideways scrolling.
func wheelX(mo tea.Mouse) (int, bool) {
	switch {
	case mo.Button == tea.MouseWheelLeft:
		return -8, true
	case mo.Button == tea.MouseWheelRight:
		return 8, true
	case mo.Mod&tea.ModShift == 0:
		return 0, false
	case mo.Button == tea.MouseWheelUp:
		return -8, true
	case mo.Button == tea.MouseWheelDown:
		return 8, true
	}

	return 0, false
}

// dragTo extends the selection to the mouse, scrolling at the edges.
func (p *preview) dragTo(m *Model, x, y int, release bool) tea.Cmd {
	c, w, h := m.mainX(), m.pvW(), m.pvH()

	// The same translation the click went through (Model.mouse): the header,
	// then the editor strip above it. Dropping the strip would put every drag
	// a row below its click, so the first motion of a click would select.
	x, y = x-c, y-m.mainY()-m.headH()-m.stripH()
	if p.split(m) {
		if y < 0 {
			p.top, y = max(p.top-1, 0), 0
		}

		if y >= h {
			p.top, y = min(p.top+1, max(p.scrollRows(m)-h, 0)), h-1
		}

		if n, ok := p.splitPos(w, ifElse(p.oldHalf, 0, w-1), y); ok && !p.otherHalf(n) { // on the half it started
			p.selectTo(n, true)
		}

		if release {
			p.endDrag(m)
		}

		return nil
	}

	if y < 0 {
		p.top, y = max(p.top-1, 0), 0
	}

	if y >= h {
		p.top, y = min(p.top+1, max(len(p.rows(w))-h, 0)), h-1
	}

	if np := p.posAt(w, x, y); np != p.at() {
		if p.anchor == nil {
			a := p.at()
			p.anchor = &a
		}

		p.cur = np
	}

	if release {
		p.endDrag(m)
	}

	return nil
}

// endDrag ends a selection drag; one that selected nothing was a click.
func (p *preview) endDrag(m *Model) {
	m.drag = nil

	if _, _, ok := p.selection(); !ok {
		p.anchor = nil
	}
}
