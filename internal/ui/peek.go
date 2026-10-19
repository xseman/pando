package ui

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

// peek is a list of locations, VS Code's references widget: the selected
// location on the left, every location grouped by file on the right. The
// panel's Problems tab has one; the References open inline, in the editor
// under the cursor's line.
type peek struct {
	title  string
	inline bool // in the editor, under the cursor's line: the References
	hov    int  // inline, the row under the mouse; -1 none
	w, h   int  // the size it was last drawn at, which keys and clicks go by
	files  []peekFile
	rows   []peekRow
	l      list
	closed map[string]bool
	src    []string // styled lines of the file shown on the left
	srcOf  string
}

type peekFile struct {
	path string
	locs []peekLoc
}

// peekLoc is one location with the line it sits on, the match in runes; a
// problem's has its message and severity, which its row shows instead.
type peekLoc struct {
	loc  lsp.Location
	text string
	a, b int
	msg  string
	sev  int
}

// peekRow is a file header (loc -1) or one of its locations.
type peekRow struct{ file, loc int }

// openPeek shows locations inline under the cursor's line, the first one
// selected.
// ponytail: it reads every result file on the update loop, so a reference
// search over hundreds of files stalls the TUI; move the reads into a
// tea.Cmd that hands back the built rows if that ever bites (the problems
// list does: showProblems).
func (m *Model) openPeek(title string, locs []lsp.Location) tea.Cmd {
	return m.showPeek(buildPeek(title, locs, nil))
}

// showPeek opens pk focused, its first location selected: the Problems in
// the panel's tab, the References inline, the editor scrolled to make room
// under the cursor's line.
func (m *Model) showPeek(pk *peek) tea.Cmd {
	pk.build()
	pk.l.sel = min(1, len(pk.rows)-1)

	if pk.title == "Problems" {
		m.probs = pk
		return m.showPanel(ptProblems)
	}

	pk.inline, pk.hov = true, -1
	m.refs, m.focus = pk, onMain

	if k, room := m.pv.cursorRow(m.pvW()), m.pvH()-m.peekH()-1; k-m.pv.top > room {
		m.pv.top = k - room
	}

	return nil
}

// peekH is the height of the References inline, 0 while they are closed.
func (m *Model) peekH() int {
	if m.refs == nil || !m.showsPreview() {
		return 0
	}

	return min(max(m.pvH()*45/100, 6), max(m.pvH()-2, 1))
}

// peekRow is the editor's text row the References open under: the cursor's,
// kept where the widget fits below it.
func (m *Model) peekRow() int {
	return max(min(m.pv.cursorRow(m.pvW())-m.pv.top, m.pvH()-m.peekH()-1), 0)
}

// withPeek is the editor's text rows, h of them, with the References
// inline under the cursor's line, w wide: what they cover is under them.
func (m *Model) withPeek(text []string, w, h int) []string {
	for len(text) < h {
		text = append(text, "")
	}

	r, ph := m.peekRow(), m.peekH()

	return append(append(slices.Clip(text[:r+1]), m.refs.view(m, w, ph)...), text[r+1:h-ph]...)
}

// buildPeek groups locations by file with the line each sits on, read from
// disk; probs, when set, are each location's problem, by index.
func buildPeek(title string, locs []lsp.Location, probs []lsp.Diagnostic) *peek {
	byPath := map[string][]int{}
	for i, l := range locs {
		byPath[l.Path] = append(byPath[l.Path], i)
	}

	pk := &peek{title: title, closed: map[string]bool{}}

	for _, path := range slices.Sorted(maps.Keys(byPath)) {
		is := byPath[path]
		slices.SortStableFunc(is, func(i, j int) int {
			worse := 0 // problems: the worst first, as VS Code's Problems lists them
			if probs != nil {
				worse = probs[i].Severity - probs[j].Severity
			}

			return cmp.Or(worse, locs[i].Line-locs[j].Line, locs[i].Col-locs[j].Col)
		})

		lines := fileLines(path)
		f := peekFile{path: path}

		for _, i := range is {
			l, raw := locs[i], ""
			if l.Line < len(lines) {
				raw = lines[l.Line]
			}

			a, b := displayCol(raw, lsp.RuneCol(raw, l.Col)), len([]rune(expandTabs(raw)))
			if l.EndLine == l.Line {
				b = displayCol(raw, lsp.RuneCol(raw, l.EndCol))
			}

			text := strings.TrimLeft(expandTabs(raw), " ")
			trim := len([]rune(expandTabs(raw))) - len([]rune(text))
			loc := peekLoc{loc: l, text: text, a: max(a-trim, 0), b: max(b-trim, 0)}

			if probs != nil {
				loc.msg, loc.sev = firstLine(probs[i].Message), probs[i].Severity
			}

			f.locs = append(f.locs, loc)
		}

		pk.files = append(pk.files, f)
	}

	return pk
}

func (pk *peek) build() {
	pk.rows = pk.rows[:0]
	for i, f := range pk.files {
		pk.rows = append(pk.rows, peekRow{i, -1})
		if !pk.closed[f.path] {
			for j := range f.locs {
				pk.rows = append(pk.rows, peekRow{i, j})
			}
		}
	}
}

// follow takes over the open list old, built again from what it lists now:
// its closed files, its scroll, its selected location, or the row it sat on
// when that is gone.
func (pk *peek) follow(old *peek) {
	pk.closed, pk.l = old.closed, old.l
	pk.build()

	if at, ok := old.selected(); ok {
		if k := slices.IndexFunc(pk.rows, func(r peekRow) bool { return r.loc >= 0 && pk.files[r.file].locs[r.loc].loc == at.loc }); k >= 0 {
			pk.l.sel = k
		}
	}

	pk.l.sel = min(pk.l.sel, len(pk.rows)-1)
}

func (pk *peek) count() int {
	n := 0
	for _, f := range pk.files {
		n += len(f.locs)
	}

	return n
}

// selected is the location under the cursor, false on a file header.
func (pk *peek) selected() (peekLoc, bool) {
	if pk.l.sel < 0 || pk.l.sel >= len(pk.rows) {
		return peekLoc{}, false
	}

	r := pk.rows[pk.l.sel]
	if r.loc < 0 {
		return peekLoc{}, false
	}

	return pk.files[r.file].locs[r.loc], true
}

// shown is the location the left pane and the header show: the selected
// one, or the first of the file whose header is selected.
func (pk *peek) shown() (peekLoc, bool) {
	if loc, ok := pk.selected(); ok {
		return loc, true
	}

	if pk.l.sel < 0 || pk.l.sel >= len(pk.rows) {
		return peekLoc{}, false
	}

	if f := pk.files[pk.rows[pk.l.sel].file]; len(f.locs) > 0 {
		l := f.locs[0]
		l.a, l.b = 0, 0 // a header selects no match, so highlight nothing

		return l, true
	}

	return peekLoc{}, false
}

// peekSourceW is the narrowest panel that shows the source beside the list.
const peekSourceW = 100

func (pk *peek) view(m *Model, w, h int) []string {
	pk.w, pk.h = w, h
	if h < 2 {
		return make([]string, max(h, 0))
	}

	name, dir := "", ""
	if loc, ok := pk.shown(); ok {
		name, dir = filepath.Base(loc.loc.Path), filepath.Dir(rel(m.ws, loc.loc.Path))
		dir = ifElse(dir == ".", "", dir) // a file at the root: no folder
	}

	head := row(w, pal.sectionBg, []seg{
		sg(" "+name, bold), sg("  "+dir, dim),
		sg(fmt.Sprintf("  — %s (%d) ", pk.title, pk.count()), dim),
	}, sg(" "+icClose.s()+" ", dim))
	body := h - 1
	lw := pk.sourceW()
	rw := max(w-lw-b2i(lw > 0), 10)
	left := pk.source(m, lw, body)
	pk.l.clamp(len(pk.rows), body)

	hover := pk.hov
	if x, y, ok := m.panelHover(); ok && x >= w-rw && !pk.inline {
		hover = pk.l.at(y-1, len(pk.rows))
	}

	level := func(i int) int { return ifElse(pk.rows[i].loc >= 0, 1, 0) } // a hit under its file
	ag := activeGuide(len(pk.rows), pk.l.sel, level)
	right := pk.l.render(rw, body, len(pk.rows), func(i, rww int) string {
		return pk.renderRow(m, i, rww, i == hover, ag.at(i, level(i)))
	})

	out := []string{head}

	for i := range body {
		if lw == 0 { // too narrow for the source: the list alone
			out = append(out, fit(right[i], rw))
			continue
		}

		out = append(out, fit(left[i], lw)+dim.Render("│")+fit(right[i], rw))
	}

	return out
}

// sourceW is the width of the source beside the list: inline the larger
// part, as VS Code's peek has it; none for the panel's problems, whose
// messages VS Code's Problems give the whole width; none too narrow.
func (pk *peek) sourceW() int {
	if pk.inline {
		return ifElse(pk.w < peekSourceW*3/5, 0, pk.w*3/5)
	}

	if pk.w < peekSourceW || len(pk.files) > 0 && len(pk.files[0].locs) > 0 && pk.files[0].locs[0].msg != "" {
		return 0
	}

	return pk.w * 2 / 5
}

// source is the selected location's file around it, its match highlighted.
// ponytail: the read and the chroma render happen here, on the render path,
// cached by path alone, so arrow-key navigation across files pays for both;
// load it in a tea.Cmd keyed on the shown path and draw the old lines until
// it lands.
func (pk *peek) source(m *Model, w, h int) []string {
	out := make([]string, h)

	loc, ok := pk.shown()
	if !ok {
		return out
	}

	if pk.srcOf != loc.loc.Path {
		b := fileLines(loc.loc.Path)
		styled, _, _, _ := render(pvFile, loc.loc.Path, strings.Join(b, "\n"), m.dark)
		pk.src, pk.srcOf = styled, loc.loc.Path
	}

	numW := len(strconv.Itoa(len(pk.src)))

	top := max(min(loc.loc.Line-h/3, len(pk.src)-h), 0)
	for i := range h {
		n := top + i
		if n >= len(pk.src) {
			break
		}

		text := ansi.Cut(pk.src[n], 0, max(w-numW-1, 1))
		if n == loc.loc.Line {
			text = highlight(pk.src[n], loc.a, loc.b, max(w-numW-1, 1))
		}

		out[i] = dim.Render(fmt.Sprintf("%*d ", numW, n+1)) + text
	}

	return out
}

// highlight paints runes [a, b) of a styled line with the match colour.
func highlight(styled string, a, b, w int) string {
	plain := []rune(ansi.Strip(styled))

	a, b = max(min(a, len(plain)), 0), max(min(b, len(plain)), 0)
	if a >= b {
		return ansi.Cut(styled, 0, w)
	}

	hl := matchStyle(lipgloss.NewStyle())

	return ansi.Cut(styled, 0, a) + "\x1b[m" + hl.Render(string(plain[a:b])) + ansi.Cut(styled, b, w)
}

// renderRow draws row i, a hit behind its indent guides; hovered lights it.
func (pk *peek) renderRow(m *Model, i, w int, hovered bool, guides []seg) string {
	r := pk.rows[i]
	f := pk.files[r.file]
	bg, base := m.rowColors(viewFiles, i == pk.l.sel, hovered)

	if r.loc < 0 {
		name := filepath.Base(f.path)

		return row(w, bg, []seg{
			sg(" "+chevron(!pk.closed[f.path]), dim), iconSeg(name, false, false), sg(name, base),
			sg(" "+ifElse(filepath.Dir(rel(m.ws, f.path)) == ".", "", filepath.Dir(rel(m.ws, f.path))), dim), // a file at the root: no folder
		}, badge(strconv.Itoa(len(f.locs))), sg(" ", plain))
	}

	lead := append(append([]seg{sg(" ", plain)}, guides...), sg(" ", plain))

	l := f.locs[r.loc]
	if l.msg != "" { // a problem: its severity and message, VS Code's Problems row
		g, st := sevMark(l.sev)

		return row(w, bg, append(lead, sg(g+" ", st), sg(l.msg, base)),
			sg(fmt.Sprintf(" %d:%d ", l.loc.Line+1, l.loc.Col+1), dim))
	}

	text := []rune(l.text)
	a, b := max(min(l.a, len(text)), 0), max(min(l.b, len(text)), 0)

	segs := lead
	if a < b {
		segs = append(segs, sg(string(text[:a]), base),
			sgOwn(string(text[a:b]), matchStyle(base)), sg(string(text[b:]), base))
	} else {
		segs = append(segs, sg(l.text, base))
	}

	return row(w, bg, segs, sg(fmt.Sprintf(" %d ", l.loc.Line+1), dim))
}

// rel is path inside the workspace, or the path itself when outside it.
func rel(ws, path string) string {
	if r, err := filepath.Rel(ws, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}

	return path
}

// open jumps to the selected location; a file header folds instead.
func (pk *peek) open(m *Model) tea.Cmd {
	if pk.l.sel < 0 || pk.l.sel >= len(pk.rows) {
		return nil
	}

	r := pk.rows[pk.l.sel]
	if r.loc < 0 {
		path := pk.files[r.file].path
		pk.closed[path] = !pk.closed[path]
		pk.build()

		return nil
	}

	return m.openLocation(pk.files[r.file].locs[r.loc].loc)
}

func (pk *peek) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	h, n := max(pk.h-1, 1), len(pk.rows)

	switch k.String() {
	case "up", "k":
		pk.l.move(-1, n, h)
	case "down", "j":
		pk.l.move(1, n, h)
	case "pgup", "b":
		pk.l.move(-h, n, h)
	case "pgdown", "f":
		pk.l.move(h, n, h)
	case "g", "home":
		pk.l.move(-n, n, h)
	case "G", "end":
		pk.l.move(n, n, h)
	case "left", "h":
		if r := pk.rows[max(pk.l.sel, 0)]; r.loc < 0 {
			pk.closed[pk.files[r.file].path] = true
			pk.build()
		} else {
			pk.l.sel = slices.IndexFunc(pk.rows, func(x peekRow) bool { return x.file == r.file && x.loc < 0 })
		}

	case "right", "l":
		if r := pk.rows[max(pk.l.sel, 0)]; r.loc < 0 {
			pk.closed[pk.files[r.file].path] = false
			pk.build()
		}

	case "enter":
		cmd := pk.open(m)
		if _, ok := pk.selected(); ok { // ⏎ takes you there, o keeps the keyboard in the list
			m.focus = onMain
			pk.close(m)
		}

		return cmd

	case "o", "space":
		return pk.open(m)
	case "esc", "q", "tab":
		m.focus = onMain
		pk.close(m)
	}

	return nil
}

// close shuts the References inline; the panel's list stays.
func (pk *peek) close(m *Model) {
	if pk.inline {
		m.refs = nil
	}
}

func (pk *peek) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()

	h := max(pk.h-1, 1)
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		pk.l.wheel(wheelDelta(mo), len(pk.rows), h)
		return nil
	}

	if _, click := msg.(tea.MouseClickMsg); !click || mo.Button != tea.MouseLeft {
		if pk.hov = -1; x > pk.sourceW() { // inline, the hit under the mouse lights
			pk.hov = pk.l.at(y-1, len(pk.rows))
		}

		return nil
	}

	if y == 0 { // the header: only ✕ does anything, which clears the list
		if x >= pk.w-3 {
			if m.probs == pk {
				m.probs = nil
			} else {
				m.refs = nil
			}
		}

		return nil
	}

	if lw := pk.sourceW(); lw > 0 && x <= lw { // the source preview is not clickable
		return nil
	}

	i := pk.l.at(y-1, len(pk.rows))
	if i < 0 {
		return nil
	}

	double := m.clicks.double(mo.X, mo.Y) && i == pk.l.sel

	pk.l.sel = i
	if double {
		return pk.open(m)
	}

	return nil
}
