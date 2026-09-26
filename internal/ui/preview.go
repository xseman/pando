package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

const (
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
	raw    string
	err    string
	lines  []string   // styled, one per source line
	plain  [][]rune   // the same lines without styles
	meta   []lineMeta // diff rows: line numbers and change kind
	numW   int        // digits of the line-number gutter
	ready  bool
	wrap   bool
	top    int    // first visible screen row
	hl     []rune // the word under the cursor, painted wherever else it stands
	comp   completion
	left   int // horizontal scroll in cells when not wrapping
	wide   int // cells of the widest line, measured with the rows while not wrapping
	cur    pos
	anchor *pos // selection start; nil = no selection
	reveal bool // center the cursor once the content loads
	// Markdown files: md is 0 for source, 1 rendered, 2 source and rendered
	// side by side. While rendered, lines and plain hold the rendering and
	// src and srcPlain the source.
	md                            int
	src                           []string
	srcPlain                      [][]rune
	mdW                           int // width mdLines were laid out for
	mdLines                       []string
	mdPlain                       []string
	mdShown                       int            // width of the rendering in lines; 0 = lines hold the source
	repo                          string         // rev: the file's repository
	revs                          []git.Revision // rev: the file's history, newest first
	revIdx                        int            // rev: the revision shown
	vis                           []vrow
	visKey                        string
	lineRow                       []int   // first screen row of each line
	buf                           *buffer // the editable text of a file; nil for diffs and revisions
	trunc                         bool    // the file was too big to load whole, so it stays read-only
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
}

func (p *preview) id() string {
	return fmt.Sprintf("%s|%s|%s|%s|%v|%s", p.kind, p.root, p.path, p.name, p.entry.Staged, p.rev)
}

// untitled reports an editor with no file behind it yet: it types and saves
// like any other, but ⌃s asks where to put it.
func (p *preview) untitled() bool { return p.kind == pvFile && p.path == "" }

// label is the header's name and dimmed context, VS Code style.
func (p *preview) label(ws string) (name, context string) {
	switch p.kind {
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
		root: p.root, entry: p.entry, rev: p.rev,
		repo: p.repo, revs: p.revs, revIdx: p.revIdx, md: p.md,
		cur: p.cur, top: p.top, left: p.left, buf: p.buf, trunc: p.trunc,
	}
}

// setPreview opens p in the main area, remembering the editor it replaces in
// the tab strip and the navigation history.
func (m *Model) setPreview(p preview) tea.Cmd {
	m.saveSpot()

	i := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == p.id() })
	switch {
	case i >= 0:
		if !p.reveal && p.cur == (pos{}) { // reopening: back to where the cursor was
			e := m.editors[i]
			p.cur, p.top, p.left, p.md = e.cur, e.top, e.left, e.md
		}

		m.editors[i], m.edIdx = p.snapshot(), i

	case m.edIdx >= 0 && m.edIdx < len(m.editors) && sameFile(m.editors[m.edIdx], p):
		m.editors[m.edIdx] = p.snapshot() // a revision of the open file stays in its tab
	default:
		// ponytail: 20 editors, oldest first out; VS Code closes by its own MRU.
		// Unsaved text is skipped over: dropping the tab drops its draft too.
		if len(m.editors) >= 20 {
			if i := slices.IndexFunc(m.editors, func(e preview) bool { return !e.dirty() }); i >= 0 {
				m.editors = slices.Delete(m.editors, i, i+1)
				if m.edIdx > i {
					m.edIdx--
				}
			}
		}

		m.editors = append(m.editors, p.snapshot())
		m.edIdx = len(m.editors) - 1
	}

	if !m.restoring {
		m.navPush(p.snapshot())
	}

	m.pv = p
	m.pv.wrap = m.st.Settings.Wrap

	var dock tea.Cmd

	if m.showsSession() && !m.restoring { // the session has the editor area: it docks so the file opens beside it
		focus := m.focus
		dock = m.splitTo(viewSession, m.sessSide())
		m.focus = focus // docking does not move the keyboard off what opened the file
	}

	m.preview = true
	cmd := m.pv.load(m)
	m.saveSpot() // an untitled buffer loads in place: the tab takes it now

	return tea.Batch(dock, cmd)
}

func (m *Model) openFile(path string) tea.Cmd {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return flash("cannot preview "+path, true)
	}

	return m.setPreview(preview{kind: pvFile, path: path})
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

// editorsSpec is the tab strip as state.json keeps it: the files, each with
// its cursor and scroll. Diffs and commits depend on git state that moves on,
// so they are not remembered.
func (m *Model) editorsSpec() proto.Editors {
	m.saveSpot()

	var e proto.Editors

	for i, p := range m.editors {
		if p.kind != pvFile && p.kind != pvRev {
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

	return e
}

// saveEditors tells the daemon what the workspace has open when that changed
// since the last save, as saveDraft does for the commit message.
func (m *Model) saveEditors() tea.Cmd {
	if m.ws == "" {
		return nil
	}

	e := m.editorsSpec()
	// Against what this TUI saved, not the daemon's copy: two TUIs on one
	// workspace would otherwise rewrite each other's strip every tick.
	if e.Active == m.savedEds.Active && slices.Equal(e.Open, m.savedEds.Open) {
		return nil
	}

	m.savedEds = e
	if m.st.Editors == nil {
		m.st.Editors = map[string]proto.Editors{}
	}

	if len(e.Open) == 0 {
		delete(m.st.Editors, m.ws)
	} else {
		m.st.Editors[m.ws] = e
	}

	return do("state.set", map[string]any{"editors": map[string]proto.Editors{m.ws: e}})
}

// restoreEditors reopens what the workspace had open the last time, cursors
// and scroll included, like VS Code reopening a folder. Files that are gone
// are skipped; the active one loads, the others load when their tab is shown.
func (m *Model) restoreEditors() tea.Cmd {
	e := m.st.Editors[m.ws]
	m.savedEds = e
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
	if i < 0 || i >= len(m.editors) || i == m.edIdx {
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
func (m *Model) closeEditor(i int) tea.Cmd {
	if i >= 0 && i < len(m.editors) && m.editors[i].dirty() {
		name, _ := m.editors[i].label(m.ws)
		m.modal = newMenu("Close "+name+" without saving?", -1, 0,
			item{label: "Save and close", run: func(m *Model) tea.Cmd {
				// An untitled buffer has nowhere to go yet: ask where, and
				// drop the tab only once that save went through.
				if m.editors[i].untitled() {
					return m.saveAsPrompt(i, func(m *Model) tea.Cmd { return m.dropEditor(i) })
				}

				return tea.Batch(m.saveEditor(i), m.dropEditor(i))
			}},
			item{label: "Close anyway", run: func(m *Model) tea.Cmd {
				return tea.Batch(m.forgetDraft(m.editors[i]), m.dropEditor(i))
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

// edTab is one tab of the editor strip.
type edTab struct {
	i, x, w int
	label   string
	active  bool
}

// editorTabs lays the strip out, dropping tabs on the left until the active
// one fits. A single editor still gets its tab.
func (m *Model) editorTabs(w int) []edTab {
	if len(m.editors) == 0 {
		return nil
	}

	names, width, total := make([]string, len(m.editors)), make([]int, len(m.editors)), 0
	for i, e := range m.editors {
		names[i], _ = e.label(m.ws)
		if e.dirty() {
			names[i] += " ●"
		}

		width[i] = ansi.StringWidth(" " + names[i] + " " + tabClose(false))
		total += width[i] + tabGap
	}

	start := 0
	for total > w && start < m.edIdx {
		total -= width[start] + tabGap
		start++
	}

	var out []edTab

	x := 0
	for i := start; i < len(names) && x+width[i]+tabGap <= w; i++ {
		label := " " + names[i] + " " + tabClose(i == m.edIdx)

		out = append(out, edTab{i: i, x: x, w: width[i], label: label, active: i == m.edIdx})
		x += width[i] + tabGap
	}

	return out
}

func (m *Model) editorStrip(w int) string {
	var segs []seg

	for _, t := range m.editorTabs(w) {
		segs = append(segs, tabChip(t.label, t.active, nil)...)
	}

	return row(w, nil, segs)
}

// stripMouse handles a click on the editor strip: the middle button and the
// active tab's ✕ close, anything else activates.
func (m *Model) stripMouse(x int, button tea.MouseButton) tea.Cmd {
	for _, t := range m.editorTabs(m.mainW()) {
		if x < t.x || x >= t.x+t.w {
			continue
		}

		if button == tea.MouseMiddle || (t.active && x >= t.x+t.w-2) {
			return m.closeEditor(t.i)
		}

		return m.showEditor(t.i)
	}

	return nil
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
func (m *Model) openFileAt(path string, line, col, n int) tea.Cmd {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return flash("cannot preview "+path, true)
	}

	p := preview{kind: pvFile, path: path, cur: pos{line, col + n}, reveal: true}
	if n > 0 {
		p.anchor = &pos{line, col}
	}

	return m.setPreview(p)
}

func (m *Model) openDiff(root string, e git.Entry) tea.Cmd {
	return m.setPreview(preview{kind: pvDiff, root: root, path: e.Path, entry: e})
}

func (m *Model) openShow(root, rev string) tea.Cmd {
	return m.setPreview(preview{kind: pvShow, root: root, rev: rev})
}

func (p *preview) load(m *Model) tea.Cmd {
	if p.kind == "" {
		return nil
	}
	// An untitled buffer has nothing on disk: it is its own source, so the
	// view is rebuilt from it rather than fetched. A restored draft arrives
	// here as text and becomes the buffer, dirty, with the mtime it was taken
	// at so a foreign write is still caught on save.
	if p.untitled() {
		if p.buf == nil {
			p.buf = newBuffer(p.draft, p.draftM)
			p.buf.dirty = p.draft != ""
		}

		p.draft, p.err, p.ready = "", "", true
		p.refreshAll(m)
		p.cur.line = min(p.cur.line, p.lastLine())
		p.revealCursor(m)

		return nil
	}

	p.closeComp()

	cp, dark, key := *p, m.dark, p.id()
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

		raw, err := cp.fetch()
		msg.raw, msg.err = raw, err

		if cp.kind == pvFile {
			if st, serr := os.Stat(cp.path); serr == nil {
				msg.mod = st.ModTime()
			}

			msg.trunc = strings.Contains(raw, "\n[truncated at 1 MiB]") ||
				strings.Count(raw, "\n") >= maxPreviewLines
		}

		if err == nil && raw != cp.raw {
			msg.lines, msg.plain, msg.meta, msg.numW = render(cp.kind, cp.path, raw, dark)
		}

		return msg
	}
}

// reloadIfLive refreshes files and diffs; `git show` output never changes.
func (p *preview) reloadIfLive(m *Model) tea.Cmd {
	if p.dirty() { // unsaved edits are the truth, not what is on disk
		return nil
	}

	if p.kind == pvFile || p.kind == pvDiff {
		return p.load(m)
	}

	return nil
}

// dirty reports unsaved edits in this editor.
func (p *preview) dirty() bool { return p.buf != nil && p.buf.dirty }

// editable reports a file that can be typed into: not a diff, a revision, a
// rendering or a file too big to hold whole.
func (p *preview) editable() bool {
	return p.kind == pvFile && p.ready && p.buf != nil && !p.trunc && p.md != 1
}

func (p *preview) fetch() (string, error) {
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
		if bytes.IndexByte(buf[:min(n, 8000)], 0) >= 0 {
			return "", errors.New("binary file")
		}

		s := string(buf)
		if n > maxPreviewBytes {
			s = s[:maxPreviewBytes] + "\n[truncated at 1 MiB]"
		}

		return s, nil

	case pvDiff:
		out, err := git.Diff(p.root, p.entry)
		if err == nil && out == "" {
			out = "(no changes)"
		}

		return out, err

	case pvShow:
		return git.Run(p.root, "show", "--stat", "--patch", p.rev)
	case pvRev:
		if p.revIdx >= len(p.revs) {
			return "", errors.New("no such revision")
		}

		out, err := git.RevisionDiff(p.repo, p.revs[p.revIdx])
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
	if kind != pvFile {
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

	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(raw)
	}

	if lexer != nil {
		style := styles.Get("github")
		if dark {
			style = styles.Get("github-dark")
		}

		var b strings.Builder

		it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(plainLines, "\n"))
		if err == nil && formatters.Get("terminal16m").Format(&b, style, it) == nil {
			if hl := strings.Split(b.String(), "\n"); len(hl) >= len(plainLines) {
				styled = hl[:len(plainLines)]
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

	if msg.err != nil {
		p.err, p.raw = msg.err.Error(), ""
		return
	}

	p.err = ""
	if msg.lines == nil {
		p.revealCursor(m) // the content is unchanged, the cursor may not be
		return
	}

	p.raw, p.lines, p.meta, p.numW, p.ready, p.vis = msg.raw, msg.lines, msg.meta, msg.numW, true, nil

	p.plain = make([][]rune, len(msg.plain))
	for i, l := range msg.plain {
		p.plain[i] = []rune(l)
	}

	if p.kind == pvFile {
		p.trunc = msg.trunc
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

// numPad is the blank before a file's line numbers, VS Code's margin left of
// them, so the numbers do not sit against the sidebar.
const numPad = 1

// gutter is the width of the line-number column: ` n ` for files, `old new ± ` for diffs.
func (p *preview) gutter() int {
	switch {
	case p.kind == pvFile && p.md != 1:
		return numPad + p.numW + 1
	case p.meta != nil:
		return 2*p.numW + 4
	}

	return 0
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

	st := dim
	if bg != nil {
		st = st.Background(bg)
	}

	switch {
	case gw == 0:
		return ""
	case !first:
		return st.Render(blank(gw))
	case p.meta == nil:
		return st.Render(blank(numPad) + fmt.Sprintf("%*d ", p.numW, line+1))
	}

	return diffGutter(p.meta[line], p.numW, bg)
}

func runeCells(r rune) int {
	if r < utf8.RuneSelf {
		return 1 // tabs are already spaces
	}

	return ansi.StringWidth(string(r))
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

	key := fmt.Sprint(tw, p.wrap)
	if p.vis != nil && p.visKey == key {
		return p.vis
	}

	p.vis, p.visKey, p.lineRow, p.wide = p.vis[:0], key, make([]int, len(p.plain)), 0
	for i, l := range p.plain {
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
	if !p.ready || p.err != "" || len(p.plain) == 0 {
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
func (p *preview) selection() (a, b pos, ok bool) {
	if p.anchor == nil {
		return pos{}, pos{}, false
	}

	a, b = pos{p.anchor.line, min(p.anchor.col, p.lineLen(p.anchor.line))}, p.at()
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		a, b = b, a
	}

	return a, b, a != b
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
	sb.WriteString(string(p.plain[a.line][a.col:]))

	for i := a.line + 1; i < b.line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(p.plain[i]))
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

// wordSpans is where p.hl stands on a line, as cell ranges.
func (p *preview) wordSpans(line []rune) [][2]int {
	if len(p.hl) == 0 || len(line) < len(p.hl) {
		return nil
	}

	var spans [][2]int

	for i := 0; i+len(p.hl) <= len(line); i++ {
		if !slices.Equal(line[i:i+len(p.hl)], p.hl) {
			continue
		}

		j := i + len(p.hl)
		if (i > 0 && isWordRune(line[i-1])) || (j < len(line) && isWordRune(line[j])) {
			continue // part of a longer word, not this one
		}

		spans = append(spans, [2]int{cellsOf(line[:i]), cellsOf(line[:j])})
		i = j - 1
	}

	return spans
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
		b.WriteString(lipgloss.NewStyle().Background(sp.bg).Render(ansi.Strip(ansi.Cut(styled, c0, c1))))
		at = c1
	}

	b.WriteString(ansi.Cut(styled, at, end))

	return b.String()
}

func (p *preview) renderRow(vr vrow, w int) string {
	tw := max(w-p.gutter(), 1)
	line, styled := p.plain[vr.line], p.lines[vr.line]
	start := cellsOf(line[:vr.from])

	end := start + cellsOf(line[vr.from:vr.to])
	if !p.wrap {
		start, end = p.left, p.left+tw
	}

	var b strings.Builder

	a, z, ok := p.selection()

	hits := p.findSpans(vr.line, line)
	if ok && p.hitSelected(a, z) {
		ok = false // the current match shows in find's color, not as a selection
	}

	outside := !ok || vr.line < a.line || vr.line > z.line
	if outside && vr.to == len(line) && len(p.conf) > 0 {
		// A conflict marker's note and actions follow its text, VS Code's
		// decoration after-text and CodeLens in one row.
		if extra, extraStyled, _ := p.conflictSuffix(vr.line); extra != nil {
			line = append(line[:len(line):len(line)], extra...)
			styled += extraStyled

			if p.wrap {
				end = min(start+cellsOf(line[vr.from:]), start+tw)
			}
		}
	}

	switch {
	case outside:
		spans := hits
		if spans == nil {
			for _, sp := range p.wordSpans(line) {
				spans = append(spans, cellSpan{sp[0], sp[1], pal.wordHiBg})
			}
		}

		b.WriteString(paintSpans(styled, spans, start, end))

	default:
		sel := lipgloss.NewStyle().Background(pal.textSelBg)

		s0, s1 := 0, len(line)
		if vr.line == a.line {
			s0 = a.col
		}

		if vr.line == z.line {
			s1 = z.col
		}

		c0 := max(start, min(cellsOf(line[:s0]), end))
		c1 := max(start, min(cellsOf(line[:s1]), end))
		b.WriteString(ansi.Cut(styled, start, c0))
		b.WriteString("\x1b[m")
		b.WriteString(sel.Render(ansi.Strip(ansi.Cut(styled, c0, c1))))
		b.WriteString(ansi.Cut(styled, c1, end))
		// A selected line break shows as one selected cell after the text.
		if lineCells := cellsOf(line); vr.line < z.line && vr.to == len(line) && lineCells >= start && lineCells-start < tw {
			b.WriteString(sel.Render(" "))
		}
	}

	body := b.String()
	if vr.line == p.rangeHi-1 { // the line Go to Line is about to go to, the whole width
		body = underBg(body+blank(max(tw-ansi.StringWidth(body), 0)), pal.rangeHiBg)
	} else if bg := p.conflictBg(vr.line); bg != nil { // a conflict block is tinted under its text
		body = underBg(body+blank(max(tw-ansi.StringWidth(body), 0)), bg)
	} else if bg := p.rowBg(vr.line); bg != nil { // tinted diff rows fill the width, like an editor
		if used := ansi.StringWidth(body); used < tw {
			body += lipgloss.NewStyle().Background(bg).Render(blank(tw - used))
		}
	}

	return p.gutterText(vr.line, vr.from == 0) + body
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
// of its own.
func (p *preview) selectTo(line int, extend bool) {
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

// rowSelected reports a meta line inside the selection, or the cursor's own
// line when there is none — the same lines stage/revert would apply to.
func (p *preview) rowSelected(i int) bool {
	a, b, ok := p.selection()
	if !ok {
		return i == p.at().line
	}

	if b.col == 0 && b.line > a.line {
		b.line--
	}

	return i >= a.line && i <= b.line
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

func (p *preview) splitBody(w, h int) []string {
	rows := splitRows(p.meta)
	p.top = max(0, min(p.top, len(rows)-h))
	lw := (w - 1) / 2

	var body []string

	for _, sr := range rows[p.top:min(p.top+h, len(rows))] {
		switch {
		case sr.full && p.meta[sr.l].kind == 'h':
			body = append(body, dim.Render(center("⋯", w)))
		case sr.full:
			body = append(body, ansi.Cut(p.lines[sr.l], p.left, p.left+w))
		default:
			body = append(body, p.splitHalf(sr.l, true, lw)+dim.Render("│")+p.splitHalf(sr.r, false, w-1-lw))
		}
	}

	return body
}

// splitHalf is one side of a split row: its line number, mark and code,
// tinted to the half's edge.
func (p *preview) splitHalf(i int, old bool, w int) string {
	if i < 0 {
		return blank(w)
	}

	md, bg, sel := p.meta[i], p.rowBg(i), p.rowSelected(i)
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

	body := ansi.Cut(p.lines[i], p.left, p.left+tw) + "\x1b[m"
	if sel { // selected text drops its own colors, as it does inline
		body = lipgloss.NewStyle().Background(bg).Render(ansi.Strip(ansi.Cut(p.lines[i], p.left, p.left+tw)))
	}

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

// scrollOnly reports the two-column views, which scroll instead of moving a cursor.
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
		p.mdLines, p.mdPlain = renderMarkdown(p.text(), w, m.dark)
		p.mdW = w
	}

	return p.mdLines, p.mdPlain
}

// syncMarkdown puts the rendering, laid out for the current width, in place
// of the source while md == 1.
func (p *preview) syncMarkdown(m *Model) {
	if p.md != 1 || !p.ready || p.mdShown == m.pvW() {
		return
	}

	styled, plainLines := p.markdown(m, m.pvW())

	p.lines, p.plain = styled, make([][]rune, len(plainLines))
	for i, l := range plainLines {
		p.plain[i] = []rune(l)
	}

	p.mdShown, p.vis, p.anchor = m.pvW(), nil, nil
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
			left = dim.Render(blank(numPad)+fmt.Sprintf("%*d ", p.numW, i+1)) + ansi.Cut(p.src[i], p.left, p.left+lw-gw)
		}

		if i < len(styled) {
			right = " " + styled[i]
		}

		body = append(body, fit(left, lw)+dim.Render("│")+fit(right, rw))
	}

	return body
}

// buttons are the preview header's actions, right-aligned: Markdown
// rendering, the diff layout, and the file's revisions.
func (p *preview) buttons(m *Model, w int) []rowAction {
	var acts []rowAction

	add := func(g glyph, k tea.KeyPressMsg) {
		acts = append(acts, rowAction{g: g, run: func(m *Model) tea.Cmd { return m.pv.key(m, k) }})
	}

	pick := func(on bool, off, onG glyph) glyph {
		if on {
			return onG
		}

		return off
	}
	if p.kind == pvFile && isMarkdown(p.path) { // direct: in a file a letter is text
		acts = append(acts,
			rowAction{g: pick(p.md == 1, icPreview, icSource), run: func(m *Model) tea.Cmd { return m.toggleRendered() }},
			rowAction{g: pick(p.md == 2, icSplit, icInline), run: func(m *Model) tea.Cmd { return m.pv.toggleMarkdown(m, 2) }})
	}

	if p.meta != nil { // the file itself, then inline ↔ side by side
		acts = append(acts, rowAction{g: icSource, run: func(m *Model) tea.Cmd { return m.pv.openHere(m) }})
		add(pick(m.st.Settings.DiffView == "split", icSplit, icInline), tea.KeyPressMsg{Code: 's', Text: "s"})
	}

	if p.kind == pvFile || p.kind == pvRev {
		acts = append(acts, rowAction{g: icPrevRev, run: func(m *Model) tea.Cmd { return m.pv.stepRev(m, 1) }})
	}

	if p.kind == pvRev {
		acts = append(acts, rowAction{g: icNextRev, run: func(m *Model) tea.Cmd { return m.pv.stepRev(m, -1) }})

		add(icHistory, tea.KeyPressMsg{Code: 'H', Text: "H"})
	}

	if !p.scrollOnly(m) { // last, so it keeps its place whatever else the kind shows
		acts = append(acts, rowAction{g: icWrap, on: p.wrap, run: func(m *Model) tea.Cmd { return m.toggleWrap() }})
	}

	layoutRight(acts, w, 2)

	return acts
}

func (p *preview) hints(m *Model) string {
	switch {
	case m.pk != nil:
		return " ↑↓ move  ←→ fold  ⏎ open  o open and stay  esc close"
	case p.split(m):
		return " ↑↓ move  ⇧ select  m stage/revert lines  O file  s inline  q close"
	case p.side(m):
		return " ↑↓ scroll  p rendered  s source only  q close"
	case p.md == 1:
		return " ↑↓ move  ⇧ select  y copy  p source  s side by side  q close"
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

		return " ^f find  ^h replace  F12 def  ⇧F12 refs  ^. fix  F2 rename" + save + "  ^w close"

	case p.kind == pvFile:
		return " ↑↓ move  ^f find  F12 def  ⇧F12 refs  ^. fix  F2 rename  q close"
	case p.kind == pvRev:
		return " ↑↓ move  H history  s split  ⇧ select  y copy  q close"
	case p.kind == pvDiff:
		return " ↑↓ move  ⇧ select  m stage/revert lines  O file  s split  q close"
	case p.meta != nil:
		return " ↑↓ move  ⇧ select  s split  y copy  q close"
	case isMarkdown(p.path):
		return " ↑↓ move  ⇧ select  p rendered  s side by side  M-← history  q close"
	}

	return " ↑↓ move  ⇧ select  y copy  w wrap  M-← history  q close"
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
	p.syncMarkdown(m)
	name, context := p.label(m.ws)

	nameSt := bold
	if m.focus == onMain {
		nameSt = accent
	}

	left := []seg{sg(" "+icClose.s()+" ", dim)}
	if p.kind != pvShow {
		left = append(left, iconSeg(name, false, false))
	}

	if p.dirty() {
		name += " ●"
	}

	left = append(left, sg(name, nameSt), sg("  "+context, dim))
	c := p.at()

	info := fmt.Sprintf(" Ln %d, Col %d ", c.line+1, c.col+1)
	if p.vimOn(m) {
		info = " " + p.vimLabel() + info
	}
	// Longest status that still leaves room for the name; a narrow area drops details.
	candidates := []string{info}
	if _, _, ok := p.selection(); ok {
		candidates = []string{fmt.Sprintf(" (%d selected)", utf8.RuneCountInString(p.selectedText())) + info, info}
	}

	if p.scrollOnly(m) {
		candidates = nil
	}

	right := []seg{{}}

	bw, mx := 0, m.mouseX-m.mainX()
	for _, a := range p.buttons(m, w) {
		st := dim

		switch {
		case m.mouseY == 0 && mx >= a.x && mx < a.x+a.w:
			st = keycapHot()
		case a.on:
			st = lipgloss.NewStyle().Background(pal.accent).Foreground(pal.buttonFg) // as find's toggles
		}

		right, bw = append(right, sg(" "+a.g.s()+" ", st)), bw+a.w
	}

	for _, s := range candidates {
		if ansi.StringWidth(s) <= w-ansi.StringWidth(name)-bw-6 {
			right[0] = sg(s, dim)
			break
		}
	}

	header = row(w, nil, left, right...)

	footer = row(w, nil, []seg{sg(p.hints(m), dim)})
	if p.err != "" {
		return header, []string{"", dim.Render("  " + p.err)}, footer
	}

	if !p.ready {
		return header, nil, footer
	}

	tw := w - 1 // the text; the scrollbar has the last column, as VS Code's editor.scrollbar does
	active := m.barActive("editor")

	switch {
	case p.split(m):
		body = p.splitBody(tw, h)
		return header, withBar(body, w, vbar{p.scrollRows(m), h, p.top}, active), footer

	case p.side(m):
		body = p.sideBody(m, tw, h)
		return header, withBar(body, w, vbar{p.scrollRows(m), h, p.top}, active), footer
	}

	p.hl = p.hlWord()
	vis, hb := p.rows(tw), p.hbar(m, tw)

	p.top = max(0, min(p.top, len(vis)-h))
	p.left = max(0, min(p.left, hb.n-hb.h)) // a shift+wheel pans no further than the widest line
	hb.top = p.left

	for k := p.top; k < len(vis) && k < p.top+h; k++ {
		body = append(body, p.renderRow(vis[k], tw))
	}

	body = withBar(body, w, vbar{len(vis), h, p.top}, active)
	if hb.on() { // under the text, the gutter and the vertical bar's corner left blank
		body = append(body, blank(p.gutter())+hb.hcells(m.barActive("editor-h"))+" ")
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
	if cmd == nil {
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

// findHit is one match: where it starts and the column it ends at.
type findHit struct {
	at  pos
	end int
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
		text := string(line)
		for _, r := range re.FindAllStringIndex(text, -1) {
			if r[1] > r[0] {
				a := utf8.RuneCountInString(text[:r[0]])
				p.hits = append(p.hits, findHit{pos{i, a}, a + utf8.RuneCountInString(text[r[0]:r[1]])})
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
func (p *preview) findSpans(i int, line []rune) []cellSpan {
	if !p.find.on {
		return nil
	}

	var spans []cellSpan

	k, _ := slices.BinarySearchFunc(p.hits, i, func(h findHit, line int) int { return h.at.line - line })
	for ; k < len(p.hits) && p.hits[k].at.line == i; k++ {
		h, bg := p.hits[k], pal.findMatchBg
		if k == p.hit {
			bg = pal.findHitBg
		}

		spans = append(spans, cellSpan{cellsOf(line[:min(h.at.col, len(line))]), cellsOf(line[:min(h.end, len(line))]), bg})
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
		{g: icCase, run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+c"); return nil }},
		{g: icWord, run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+w"); return nil }},
		{g: icRegex, run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+r"); return nil }},
		{g: icUp, run: func(m *Model) tea.Cmd { return m.pv.findGo(m, -1) }},
		{g: icDown, run: func(m *Model) tea.Cmd { return m.pv.findGo(m, 1) }},
		{g: icClose, run: func(m *Model) tea.Cmd { m.pv.closeFind(); return nil }},
	}

	return findLayout(acts, bw), acts
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
		{g: icPreserve, run: func(m *Model) tea.Cmd { m.pv.toggleFind(m, "alt+p"); return nil }},
		{g: icReplace, run: func(m *Model) tea.Cmd { return m.pv.replaceOne(m) }},
		{g: icReplaceAll, run: func(m *Model) tea.Cmd { return m.pv.replaceAll(m) }},
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

	return append(box, edge.Render("╰"+rule+"╯")), m.mainX() + x0, 1 + m.stripH(), true
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

	text := t.View()
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
	case "shift+enter", "up", "ctrl+p":
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

	return m.openFileAt(path, max(line-1, 0), 0, 0)
}

// items are the preview's commands. Diffs of tracked files stage, unstage
// or revert the selected lines, like VS Code's Stage Selected Ranges.
func (p *preview) items(m *Model) []item {
	key := func(k rune) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.pv.key(m, tea.KeyPressMsg{Code: k, Text: string(k)}) }
	}

	var items []item

	if p.kind == pvDiff && p.meta != nil && p.entry.Letter != 'U' && p.entry.Letter != '!' {
		switch {
		case p.entry.Staged:
			items = append(items, item{label: "Unstage Selected Ranges", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.UnstageLines) }})
		default:
			items = append(items,
				item{label: "Stage Selected Ranges", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.StageLines) }},
				item{label: "Revert Selected Ranges…", run: func(m *Model) tea.Cmd { return m.pv.confirmRevert(m) }})
		}
	}

	items = append(items, p.conflictItems()...)
	if p.kind == pvFile && p.editable() {
		items = append(items, item{label: "Format Document", hint: "^⇧i", run: func(m *Model) tea.Cmd { return m.pv.formatDoc(m) }})
	}

	if _, argv := m.lspCommand(p.path); p.kind == pvFile && len(argv) > 0 {
		items = append(items,
			item{label: "Go to Definition", hint: "f12", run: func(m *Model) tea.Cmd { return m.lspGo(false) }},
			item{label: "Find References", hint: "⇧f12", run: func(m *Model) tea.Cmd { return m.lspGo(true) }},
			item{label: "Quick Fix…", hint: "^.", run: func(m *Model) tea.Cmd { return m.lspActions() }},
			item{label: "Rename Symbol…", hint: "F2", run: func(m *Model) tea.Cmd { return m.lspRename() }})
	}

	if p.meta != nil {
		items = append(items, item{label: "Open File at This Line", hint: "O", run: func(m *Model) tea.Cmd { return m.pv.openHere(m) }})
	}

	if p.kind == pvFile && isMarkdown(p.path) {
		items = append(items,
			item{label: "Toggle Rendered Markdown", hint: "^⇧v", run: func(m *Model) tea.Cmd { return m.toggleRendered() }},
			item{label: "Markdown Side by Side", hint: "M-v", run: func(m *Model) tea.Cmd {
				return m.pv.key(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt})
			}})
	}

	if p.editable() {
		items = append(items,
			item{label: "Save File", hint: "^s", run: func(m *Model) tea.Cmd { return m.pv.save(m, false) }},
			item{label: "Save As…", run: func(m *Model) tea.Cmd { return m.saveAsPrompt(m.edIdx, nil) }},
			item{label: "Undo", hint: "^z", run: func(m *Model) tea.Cmd { return m.pv.history(m, true) }},
			item{label: "Redo", hint: "^y", run: func(m *Model) tea.Cmd { return m.pv.history(m, false) }},
			item{label: "Toggle Line Comment", hint: "^/", run: func(m *Model) tea.Cmd { return m.pv.toggleComment(m) }})
	}

	items = append(items, item{label: "Copy", hint: "^c", run: key('y')},
		item{label: "Go to Line…", hint: "^g", run: func(m *Model) tea.Cmd { return m.gotoLineQuery(":") }})
	if p.kind == pvFile && !p.untitled() {
		items = append(items, item{label: "Go to Symbol…", hint: "^⇧o", run: func(m *Model) tea.Cmd { return m.gotoSymbol("@") }})
	}

	if p.kind != pvShow && !p.untitled() {
		items = append(items, item{label: "Edit in $EDITOR", hint: "e", run: key('e')})
	}

	return items
}

// keyItems are the editor's commands that have keys of their own: the palette
// and [keys] list them, the context menu would only grow long with them.
func (p *preview) keyItems(_ *Model) []item {
	chord := func(code rune, mod tea.KeyMod) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			m.focus = onMain
			return m.pv.key(m, tea.KeyPressMsg{Code: code, Mod: mod})
		}
	}

	items := []item{
		{label: "Find", hint: "^f", run: chord('f', tea.ModCtrl)},
		{label: "Select All", hint: "^a", run: chord('a', tea.ModCtrl)},
		{label: "Toggle Word Wrap", hint: "M-z", run: chord('z', tea.ModAlt)},
		{label: "Expand Selection", hint: "M-⇧→", run: chord(tea.KeyRight, tea.ModAlt|tea.ModShift)},
		{label: "Shrink Selection", hint: "M-⇧←", run: chord(tea.KeyLeft, tea.ModAlt|tea.ModShift)},
	}
	if p.editable() {
		items = append(items,
			item{label: "Replace", hint: "^h", run: chord('h', tea.ModCtrl)},
			item{label: "Cut", hint: "^x", run: chord('x', tea.ModCtrl)},
			item{label: "Duplicate Line", hint: "^d", run: chord('d', tea.ModCtrl)},
			item{label: "Copy Line Up", hint: "M-⇧↑", run: chord(tea.KeyUp, tea.ModAlt|tea.ModShift)},
			item{label: "Copy Line Down", hint: "M-⇧↓", run: chord(tea.KeyDown, tea.ModAlt|tea.ModShift)},
			item{label: "Delete Line", hint: "^⇧k", run: chord('k', tea.ModCtrl|tea.ModShift)},
			item{label: "Move Line Up", hint: "M-↑", run: chord(tea.KeyUp, tea.ModAlt)},
			item{label: "Move Line Down", hint: "M-↓", run: chord(tea.KeyDown, tea.ModAlt)},
			item{label: "Trigger Suggest", hint: "^space", run: func(m *Model) tea.Cmd {
				m.focus = onMain
				return m.pv.suggest(m, "", true)
			}})
	}

	return items
}

// menu opens the preview's context menu at x, y.
func (p *preview) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(p.items(m), x, y) }

// changedLines are the selected diff changes: deletions by old line number,
// additions by new line number. A selection ending at column 0 leaves that
// line out; without a selection the cursor line counts.
func (p *preview) changedLines() (dels, adds map[int]bool) {
	dels, adds = map[int]bool{}, map[int]bool{}

	a, b, ok := p.selection()
	if !ok {
		a, b = p.at(), p.at()
	} else if b.col == 0 && b.line > a.line {
		b.line--
	}

	for i := a.line; i <= b.line && i < len(p.meta); i++ {
		switch md := p.meta[i]; md.kind {
		case 'd':
			dels[md.a] = true
		case 'a':
			adds[md.b] = true
		}
	}

	return dels, adds
}

func (p *preview) applyLines(m *Model, op git.LineOp) tea.Cmd {
	dels, adds := p.changedLines()

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
	m.modal = newMenu("Revert the selected changes in "+p.path+"?", -1, 0,
		item{label: "Revert", run: func(m *Model) tea.Cmd { return m.pv.applyLines(m, git.RevertLines) }},
		cancelItem())

	return nil
}

func (p *preview) close(m *Model) {
	*p = preview{wrap: p.wrap}
	m.preview = false
}

// moved applies a navigation key to the cursor; ok is false for other keys.
func (p *preview) moved(key string, h int) (pos, bool) {
	c, last := p.at(), p.lastLine()

	switch key {
	case "up", "k":
		return pos{max(p.cur.line-1, 0), p.cur.col}, true
	case "down", "j":
		return pos{min(p.cur.line+1, last), p.cur.col}, true
	case "left", "h":
		if c.col > 0 {
			return pos{c.line, c.col - 1}, true
		}

		if c.line > 0 {
			return pos{c.line - 1, p.lineLen(c.line - 1)}, true
		}

		return c, true

	case "right", "l":
		if c.col < p.lineLen(c.line) {
			return pos{c.line, c.col + 1}, true
		}

		if c.line < last {
			return pos{c.line + 1, 0}, true
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
		return pos{max(c.line-h, 0), p.cur.col}, true
	case "pgdown", "f", "space":
		return pos{min(c.line+h, last), p.cur.col}, true
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

	m.pk = nil
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
	if p.kind == "" {
		return nil
	}

	if cmd, ok := p.compKey(m, k); ok {
		return cmd
	}

	if cmd, ok := p.findKey(m, k); ok {
		return cmd
	}

	if cmd, ok := p.vimKey(m, k); ok {
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
		return tea.Batch(cmd, p.afterEdit(m, k))
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

		if np, ok := p.moved(strings.TrimPrefix(s, "shift+"), h); ok {
			p.selectTo(np.line, strings.HasPrefix(s, "shift+"))
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
		if p.anchor != nil {
			p.anchor = nil
			return nil
		}

		return p.closeAndRefocus(m)

	case "q":
		return m.closeEditor(m.edIdx)
	case "m", "shift+f10": // ⇧F10 is the context menu key an editor leaves free
		return p.menu(m, -1, 0)
	case "alt+shift+right": // VS Code's Expand Selection; ctrl+shift+→ selects a word
		return p.expandSel(m, 1)
	case "alt+shift+left":
		return p.expandSel(m, -1)
	case "ctrl+a":
		p.anchor = &pos{}
		p.cur = pos{p.lastLine(), p.lineLen(p.lastLine())}
		p.follow(w, h)

	case "y", "ctrl+c":
		if text := p.selectedText(); text != "" {
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

	if click && y == -1 && mo.Button == tea.MouseLeft {
		switch {
		case x < 3: // the ✕ in the header
			return m.closeEditor(m.edIdx)
		default:
			if a, ok := hit(p.buttons(m, m.mainW()), x); ok { // the header spans the scrollbar too
				return a.run(m)
			}
		}
	}

	if !p.ready || p.err != "" || len(p.plain) == 0 {
		return nil
	}

	if click && mo.Button == tea.MouseLeft && x >= w && y >= 0 && y < h { // the scrollbar
		return m.barClick("editor", y, mo.Y-y, func() vbar { return p.bar(m) }, func(top int) tea.Cmd {
			p.top = top
			return nil
		})
	}

	if click && mo.Button == tea.MouseLeft && y == h && x >= p.gutter() && x < w { // the horizontal bar under the text
		bx := x - p.gutter()

		return m.hbarClick("editor-h", bx, mo.X-bx, func() vbar { return p.hbar(m, m.pvW()) }, func(left int) tea.Cmd {
			p.left = left
			return nil
		})
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
				if n, ok := p.splitPos(w, x, y); ok && !p.rowSelected(n) {
					p.selectTo(n, false)
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
			if n, ok := p.splitPos(w, x, y); ok {
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
		if mo.Mod&tea.ModShift != 0 {
			if p.anchor == nil {
				a := p.at()
				p.anchor = &a
			}
		} else {
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
	x, y = x-c, y-1-m.stripH()
	if p.split(m) {
		if y < 0 {
			p.top, y = max(p.top-1, 0), 0
		}

		if y >= h {
			p.top, y = min(p.top+1, max(p.scrollRows(m)-h, 0)), h-1
		}

		if n, ok := p.splitPos(w, x, y); ok {
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
