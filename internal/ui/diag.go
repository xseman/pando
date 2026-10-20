package ui

import (
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

// maxDiagFiles caps the changed files diagnostics keep open on a server, the
// editors' files first.
const maxDiagFiles = 50

// diagBox is where the language servers' readers leave diagnostics for the
// UI: each file's latest list, which waitDiags hands over in one message.
// A reader never blocks on it, and no list is lost to a full channel.
type diagBox struct {
	mu   sync.Mutex
	got  map[string][]lsp.Diagnostic
	wake chan struct{}
}

// newDiagBox is an empty box, its wake signal ready.
func newDiagBox() *diagBox {
	return &diagBox{got: map[string][]lsp.Diagnostic{}, wake: make(chan struct{}, 1)}
}

// put is the servers' lsp.Sink.
func (b *diagBox) put(path string, ds []lsp.Diagnostic) {
	b.mu.Lock()
	b.got[path] = ds
	b.mu.Unlock()

	select {
	case b.wake <- struct{}{}:
	default: // a wake is pending: it takes this list too
	}
}

// diagMsg is the files whose diagnostics changed, each with its whole list.
type diagMsg map[string][]lsp.Diagnostic

// waitDiags waits for the next diagnostics, as waitEvent for the daemon's
// events; Update re-arms it.
func (b *diagBox) waitDiags() tea.Cmd {
	return func() tea.Msg {
		<-b.wake

		b.mu.Lock()
		defer b.mu.Unlock()

		got := b.got
		b.got = map[string][]lsp.Diagnostic{}

		return diagMsg(got)
	}
}

// onDiags takes the servers' latest lists: Explorer, Source Control and an
// open Problems list follow, and the box is waited on again.
func (m *Model) onDiags(msg diagMsg) tea.Cmd {
	if m.st.Settings.Diags {
		for path, ds := range msg {
			if len(ds) == 0 {
				delete(m.diags, path)
			} else {
				m.diags[path] = ds
			}
		}

		m.decorateDiags()

		if m.problemsOpen() {
			return tea.Batch(m.diagIn.waitDiags(), m.problems(true))
		}
	}

	return m.diagIn.waitDiags()
}

// dropDiags forgets every problem: diagnostics turned off, or another
// workspace.
func (m *Model) dropDiags() {
	m.diags = map[string][]lsp.Diagnostic{}
	m.decorateDiags()
}

type (
	// diagTickMsg is the shown editor's sync coming due, unless a later edit
	// moved it on.
	diagTickMsg int
	// diagFailMsg is a server that would not start for diagnostics, for a
	// file; diagOKMsg one that took a file's text.
	diagFailMsg struct {
		server, path string
		err          error
	}
	diagOKMsg string
	// diagSyncedMsg ends a sync of the files diagnostics are for.
	diagSyncedMsg struct {
		fails      []diagFailMsg
		ok, forgot []string
	}
)

// editSeen is the text the server last got for the shown editor: its file,
// buffer and count of changes.
type editSeen struct {
	path  string
	buf   *buffer
	edits int
}

// noteDiags follows lsp_diagnostics turned on or off, here or anywhere: off,
// the problems go and so do the servers, so turned on again each starts
// fresh and reports again on every file, a broken one nobody opened too.
func (m *Model) noteDiags() {
	on := m.st.Settings.Diags
	if on == m.diagOn {
		return
	}

	m.diagOn, m.diagSeen = on, editSeen{}
	if on {
		m.lsps.reset(m.ws)
		return
	}

	m.dropDiags()
	clear(m.diagFailed)
	m.lsps.reset("") // no workspace's: a sync still running starts no server (errOtherRoot)

	m.probs = nil
}

// noteEdits arms the sync of the shown editor's text when it changed: a
// load, an edit, an undo, a format and a reload from disk all land here, and
// the server gets the text 300 ms after the last of them.
func (m *Model) noteEdits() tea.Cmd {
	p := &m.pv
	if !m.st.Settings.Diags || p.kind != pvFile || p.buf == nil || p.trunc || p.path == "" {
		return nil
	}

	seen := editSeen{p.path, p.buf, p.buf.edits}
	if seen == m.diagSeen {
		return nil
	}

	m.diagSeen, m.diagSeq = seen, m.diagSeq+1
	seq := m.diagSeq

	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return diagTickMsg(seq) })
}

// onDiagTick hands the server the shown editor's text.
func (m *Model) onDiagTick(seq int) tea.Cmd {
	p := &m.pv
	if seq != m.diagSeq || !m.st.Settings.Diags || p.kind != pvFile || p.buf == nil || p.path == "" {
		return nil
	}

	path, text, root, pool, cfg := p.path, p.text(), m.ws, &m.lsps, m.st.Settings.LSP

	return func() tea.Msg {
		lang, argv := serverFor(cfg, path) // it stats its way to a TypeScript: not on the update loop
		if len(argv) == 0 {
			return nil
		}

		c, err := pool.background(root, path, lang, argv)
		if err != nil {
			return diagFailMsg{argv[0], path, err}
		}

		_ = c.Sync(path, text) // a server gone says so on the next sync

		return diagOKMsg(path)
	}
}

// warnDiag says once per server and TUI that it would not start, and keeps
// it for the status bar while its file shows: a file with no problems shown
// is not a file without problems.
func (m *Model) warnDiag(f diagFailMsg) {
	if errors.Is(f.err, errOtherRoot) || !m.st.Settings.Diags {
		return
	}

	m.diagFailed[f.path] = f

	if !m.diagWarned[f.server] {
		m.diagWarned[f.server] = true
		m.flash("diagnostics: "+f.err.Error(), true)
	}
}

// diagFile is a file diagnostics are for, with its unsaved text when an
// editor has some.
type diagFile struct {
	path, text string
	dirty      bool
}

// diagFiles are the files diagnostics are for: the editors' files, then the
// changed files of Source Control in the workspace, deleted ones aside, at
// most maxDiagFiles.
func (m *Model) diagFiles() []diagFile {
	var out []diagFile

	seen := map[string]bool{}
	add := func(path, text string, dirty bool) {
		if path != "" && !seen[path] && len(out) < maxDiagFiles {
			seen[path] = true
			out = append(out, diagFile{path, text, dirty})
		}
	}

	for _, e := range append([]preview{m.pv}, m.editors...) {
		if e.kind == pvFile && !e.trunc {
			add(e.path, e.unsaved(), e.dirty())
		}
	}

	for _, root := range slices.Sorted(maps.Keys(m.scm.status)) {
		st := m.scm.status[root]
		for _, e := range slices.Concat(st.Conflicts, st.Staged, st.Changes) {
			if path := filepath.Join(root, e.Path); e.Letter != 'D' && strings.HasPrefix(path, m.ws+string(filepath.Separator)) {
				add(path, "", false)
			}
		}
	}

	return out
}

// syncDiags keeps the servers to the files diagnostics are for, on each git
// refresh: each one's text (an editor's, else what is on disk when it moved),
// and a document neither open nor changed closed. It starts no server that
// died or would not start, and one sync runs at a time.
func (m *Model) syncDiags() tea.Cmd {
	if !m.st.Settings.Diags || m.diagBusy {
		return nil
	}

	m.diagBusy = true
	files, root, pool, cfg := m.diagFiles(), m.ws, &m.lsps, m.st.Settings.LSP

	return func() tea.Msg {
		var msg diagSyncedMsg

		keep := map[*lsp.Client]map[string]bool{}

		for _, f := range files {
			lang, argv := serverFor(cfg, f.path)
			if len(argv) == 0 {
				continue
			}

			c, err := pool.background(root, f.path, lang, argv)
			if err != nil {
				msg.fails = append(msg.fails, diagFailMsg{argv[0], f.path, err})
				continue
			}

			msg.ok = append(msg.ok, f.path)

			if f.dirty {
				_ = c.Sync(f.path, f.text) // a server gone says so on the next sync
			} else {
				_ = c.SyncFile(f.path) // a file gone meanwhile is the next refresh's
			}

			if keep[c] == nil {
				keep[c] = map[string]bool{}
			}

			keep[c][f.path] = true
		}

		for _, c := range pool.clients() {
			for _, path := range c.Synced() {
				if !keep[c][path] {
					c.Forget(path)
					msg.forgot = append(msg.forgot, path)
				}
			}
		}

		return msg
	}
}

// onDiagSynced ends a sync pass: a file synced is no failure, a failure
// warns, a file forgotten takes its problems along, an open list too.
func (m *Model) onDiagSynced(msg diagSyncedMsg) tea.Cmd {
	m.diagBusy = false

	for _, path := range msg.ok {
		delete(m.diagFailed, path)
	}

	for _, f := range msg.fails {
		m.warnDiag(f)
	}

	for _, path := range msg.forgot {
		delete(m.diags, path)
	}

	m.decorateDiags()

	if len(msg.forgot) > 0 && m.problemsOpen() {
		return m.problems(true)
	}

	return nil
}

// problemsOpen reports a Problems list in the panel, which follows the problems.
func (m *Model) problemsOpen() bool { return m.probs != nil }

// sevMark is a severity's glyph and color, VS Code's: ✖ error, ⚠ warning,
// ℹ information and hint.
func sevMark(sev int) (string, lipgloss.Style) {
	switch sev {
	case 1:
		return "✖", fg(pal.errc)
	case 2:
		return "⚠", fg(pal.warn)
	}

	return "ℹ", dim
}

func sevColor(sev int) color.Color { return ifElse(sev == 1, pal.errc, pal.warn) }

// firstLine is a message's first line, its tabs spaces: one row's worth.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.ReplaceAll(line, "\t", " ") // a tab is no width a row can count
}

// diagCounts are the errors and warnings of every file.
func (m *Model) diagCounts() (errs, warns int) {
	for _, ds := range m.diags {
		for _, d := range ds {
			errs, warns = errs+b2i(d.Severity == 1), warns+b2i(d.Severity == 2)
		}
	}

	return errs, warns
}

// decorateDiags is each file's worst problem and its count, its folders'
// worst too, as VS Code's Explorer colors them: what Explorer and Source
// Control draw.
func (m *Model) decorateDiags() {
	deco := map[string]probDeco{}

	for path, ds := range m.diags {
		d := probDeco{}

		for _, x := range ds {
			if x.Severity <= 2 {
				d.n++
				d.sev = min(cmp.Or(d.sev, x.Severity), x.Severity)
			}
		}

		if d.n == 0 {
			continue
		}

		deco[path] = d

		for dir := filepath.Dir(path); len(dir) >= len(m.ws) && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if o := deco[dir]; o.sev == 0 || d.sev < o.sev {
				deco[dir] = probDeco{sev: d.sev}
			}
		}
	}

	m.probDeco = deco
}

// probDeco is a file's or folder's worst problem, 1 error or 2 warning, and
// a file's count of them.
type probDeco struct{ sev, n int }

// probSpan is a problem's cells on one row of the view: [from, to) of its
// plain text.
type probSpan struct {
	from, to, sev int
	msg           string
}

// probsOf are the errors and warnings of what p shows, by row: a file's, or
// a working-tree diff's on its new side, its rows found by line number.
func (m *Model) probsOf(p *preview) map[int][]probSpan {
	ds := m.diags[p.diagPath()]
	if len(ds) == 0 || p.md == 1 {
		return nil
	}

	row := map[int]int{} // file line → row

	for i, l := range p.meta {
		if l.b > 0 && l.kind != 'h' {
			row[l.b-1] = i
		}
	}

	out := map[int][]probSpan{}

	for _, d := range ds {
		if d.Severity > 2 {
			continue
		}

		d = p.inText(d)
		for line := d.Line; line <= max(d.EndLine, d.Line); line++ {
			i, ok := line, line < len(p.plain)
			if p.meta != nil {
				i, ok = row[line]
			}

			if !ok {
				continue
			}

			raw, w := p.rawText(line), cellsOf(p.plain[i])
			from, to := 0, w

			if line == d.Line {
				from = p.cellAt(i, raw, d.Col)
			}

			if line == d.EndLine {
				to = p.cellAt(i, raw, d.EndCol)
			}

			if to <= from { // a point: the word there, else one cell, at the end the last
				to = from + max(wordCells(p.plain[i], from), 1)
				if from >= w && w > 0 {
					from, to = cellsOf(p.plain[i][:len(p.plain[i])-1]), w
				}
			}

			out[i] = append(out[i], probSpan{from, to, d.Severity, firstLine(d.Message) + ifElse(d.Source != "", "  "+d.Source, "")})
		}
	}

	return out
}

// diagPath is the file p's diagnostics are of: a file's own, a working-tree
// diff's (unstaged, or an untracked file's), "" for anything else.
func (p *preview) diagPath() string {
	switch {
	case p.kind == pvFile:
		return p.path
	case p.kind == pvDiff && !p.entry.Staged && p.entry.Letter != '!':
		return filepath.Join(p.root, p.path)
	}

	return ""
}

// rawText is file line n as the server has it, tabs and all: the buffer's,
// or for a diff the file on disk's.
func (p *preview) rawText(n int) string {
	if p.buf != nil {
		return p.rawLine(n)
	}

	if n < len(p.disk) {
		return p.disk[n]
	}

	return ""
}

// inText is d moved into the text when it is past the last line, which a
// file's final newline ends, as a truncated file's error is: to the end of
// the last line, as VS Code keeps a problem in its model.
func (p *preview) inText(d lsp.Diagnostic) lsp.Diagnostic {
	n := ifElse(p.meta == nil, len(p.plain), len(p.disk))
	if d.Line < n || n == 0 {
		return d
	}

	raw := p.rawText(n - 1)
	end := lsp.UTF16Col(raw, utf8.RuneCountInString(raw))
	d.Line, d.Col, d.EndLine, d.EndCol = n-1, end, n-1, end

	return d
}

// cellAt is the cell of row i a server's UTF-16 column on its raw line
// stands at: a tab is tab_size cells, a wide rune two.
func (p *preview) cellAt(i int, raw string, u16 int) int {
	d := displayCol(raw, lsp.RuneCol(raw, u16))

	return cellsOf(p.plain[i][:min(d, len(p.plain[i]))])
}

// wordCells are the cells of the word starting at cell from of line.
func wordCells(line []rune, from int) int {
	c := colAtCell(line, from)

	e := c
	for e < len(line) && isWordRune(line[e]) {
		e++
	}

	return cellsOf(line[c:e])
}

// squiggle draws a problem's range of styled text with VS Code's wavy
// underline in its color, the text's own colors kept: the underline goes
// on again after every SGR inside, which may reset it.
func squiggle(s string, c color.Color) string {
	r, g, b, _ := c.RGBA()
	on := fmt.Sprintf("\x1b[4:3m\x1b[58;2;%d;%d;%dm", r>>8, g>>8, b>>8)

	var out strings.Builder

	out.WriteString(on)

	var state byte

	for s != "" {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		if n == 0 {
			break
		}

		out.WriteString(seq)

		if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
			out.WriteString(on)
		}

		s, state = s[n:], next
	}

	out.WriteString("\x1b[4:0m\x1b[59m")

	return out.String()
}

// paintProbs squiggles the problems on row line of body, the text from cell
// start; the worse of two over the same cells wins, drawn last.
func paintProbs(body string, spans []probSpan, start int) string {
	spans = slices.Clone(spans)
	slices.SortStableFunc(spans, func(a, b probSpan) int { return b.sev - a.sev })

	for _, sp := range spans {
		a, b := max(sp.from-start, 0), sp.to-start
		if b <= a {
			continue
		}

		body = cutHead(body, a) + "\x1b[m" + squiggle(ansi.Cut(body, a, b), sevColor(sp.sev)) + ansi.TruncateLeft(body, b, "")
	}

	return body
}

// probAt is the problem at column col of row i, the worst there; nil for
// none.
func (p *preview) probAt(i, col int) *probSpan {
	x := cellsOf(p.plain[i][:min(col, len(p.plain[i]))])

	var best *probSpan

	for k, sp := range p.probs[i] {
		if x >= sp.from && x < sp.to && (best == nil || sp.sev < best.sev) {
			best = &p.probs[i][k]
		}
	}

	return best
}

// probAtCursor is the problem under the cursor, which the footer shows, or
// one ending there, as VS Code's ranges hold their end.
func (p *preview) probAtCursor() *probSpan {
	c := p.at()
	if c.line >= len(p.plain) {
		return nil
	}

	if pr := p.probAt(c.line, c.col); pr != nil || c.col == 0 {
		return pr
	}

	return p.probAt(c.line, c.col-1)
}

// gotoProblem is VS Code's Go to Next Problem (F8) and Previous (shift+F8)
// in the shown file or diff: the cursor to its start, mid view, around the
// ends; a problem in a diff's gap unfolds it.
func (m *Model) gotoProblem(d int) tea.Cmd {
	p := &m.pv

	ds := slices.DeleteFunc(slices.Clone(m.diags[p.diagPath()]), func(x lsp.Diagnostic) bool { return x.Severity > 2 })
	if len(ds) == 0 {
		return flash("no problems", false)
	}

	for k := range ds {
		ds[k] = p.inText(ds[k])
	}

	slices.SortFunc(ds, func(a, b lsp.Diagnostic) int { return cmp.Or(a.Line-b.Line, a.Col-b.Col) })

	c := p.at()
	raw := p.rawText(c.line)

	cl, cc := c.line, lsp.UTF16Col(raw, docCol(raw, c.col)) // the server's column: a tab is one
	if p.meta != nil {                                      // a diff's cursor is a whole file line: past its problems, or before them going back
		cl, cc = p.lineNo(c.line)-1, ifElse(d > 0, math.MaxInt, -1)
	}

	// the first past the cursor, or at it going back; the one before it then
	i := slices.IndexFunc(ds, func(x lsp.Diagnostic) bool {
		return x.Line > cl || x.Line == cl && (x.Col > cc || d < 0 && x.Col == cc)
	})
	if i < 0 {
		i = len(ds)
	}

	x := ds[(i+min(d, 0)+len(ds))%len(ds)]

	if p.meta == nil {
		raw = p.rawText(x.Line)
		p.gotoLine(m, x.Line, displayCol(raw, lsp.RuneCol(raw, x.Col)))
	} else {
		p.unfoldLine(m, x.Line+1)
		p.showLine(m, findPlace(p.meta, lineMeta{b: x.Line + 1}), false)
	}

	m.focus = onMain

	return nil
}

// showProblems is VS Code's Problems (ctrl+shift+m): every file's problems
// in the list under the editor, grouped by file, worst first; built in a
// command, as it reads each file for its lines.
func (m *Model) showProblems() tea.Cmd { return m.problems(false) }

// problems builds the Problems list; live, again for the open one, which
// follows its problems as VS Code's panel does, and closes with the last.
func (m *Model) problems(live bool) tea.Cmd {
	type prob struct {
		path string
		d    lsp.Diagnostic
	}

	var all []prob

	for path, ds := range m.diags {
		for _, d := range ds {
			all = append(all, prob{path, d})
		}
	}

	if len(all) == 0 { // the panel says there are none
		m.probs = nil

		if live {
			return nil
		}

		return m.showPanel(ptProblems)
	}

	slices.SortFunc(all, func(a, b prob) int {
		return cmp.Or(strings.Compare(a.path, b.path), a.d.Severity-b.d.Severity, a.d.Line-b.d.Line, a.d.Col-b.d.Col)
	})

	all = all[:min(len(all), 1000)] // ponytail: a list past a thousand stops there

	locs, probs := make([]lsp.Location, len(all)), make([]lsp.Diagnostic, len(all))
	for i, x := range all {
		locs[i] = lsp.Location{Path: x.path, Line: x.d.Line, Col: x.d.Col, EndLine: x.d.EndLine, EndCol: x.d.EndCol}
		probs[i] = x.d
	}

	return func() tea.Msg { return problemsMsg{buildPeek("Problems", locs, probs), live} }
}

type problemsMsg struct {
	pk   *peek
	live bool
}

// probDecoFor is the problem decoration of a path for Explorer and Source
// Control: its color and, for a file, its count.
func (m *Model) probDecoFor(path string) (color.Color, string) {
	d, ok := m.probDeco[path]
	if !ok {
		return nil, ""
	}

	return sevColor(d.sev), ifElse(d.n > 0, strconv.Itoa(d.n), "")
}

// barProbs are a file's rows as its scrollbar counts them, wrapped: 1 on a
// row with an error, 2 with only warnings, for vbar.marked with probMarks.
func (p *preview) barProbs(vis []vrow) []byte {
	if len(p.probs) == 0 {
		return nil
	}

	kinds := make([]byte, len(vis))

	for r, vr := range vis {
		for _, sp := range p.probs[vr.line] {
			kinds[r] |= byte(sp.sev)
		}
	}

	return kinds
}

// markClick goes to what scrollbar row y marks: a diff's change, a file's
// problem, or under it a file's change; false when it marks nothing there.
func (p *preview) markClick(m *Model, y int) bool {
	if n, ok := p.markAt(m, y); ok {
		p.showLine(m, n, false)
		return true
	}

	return p.probMarkAt(m, y) || p.changeMarkAt(m, y)
}

// probMarkAt goes to the first problem marked on scrollbar row y of a file,
// off the slider; false when there is none there.
func (p *preview) probMarkAt(m *Model, y int) bool {
	if p.meta != nil || len(p.probs) == 0 {
		return false
	}

	vis, b := p.rows(m.pvW()), p.bar(m)
	if ty, sh := b.thumb(); !b.on() || len(vis) != b.n || y >= ty && y < ty+sh {
		return false
	}

	for r, vr := range vis {
		if sps := p.probs[vr.line]; len(sps) > 0 && b.slotOf(r)/barSlots == y {
			p.gotoLine(m, vr.line, colAtCell(p.plain[vr.line], sps[0].from))
			return true
		}
	}

	return false
}

// colAtCell is the column of line at cell x: a wide rune is two cells.
func colAtCell(line []rune, x int) int {
	at := 0
	for i, r := range line {
		if at >= x {
			return i
		}

		at += runeCells(r)
	}

	return len(line)
}

// tipAt is a tip for screen cell (x, y), shown while the pointer stays there.
type tipAt struct {
	text string
	x, y int
}
