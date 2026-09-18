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

// peek is VS Code's references widget under the editor: the selected
// location on the left, every location grouped by file on the right.
type peek struct {
	title  string
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

// peekLoc is one location with the line it sits on, the match in runes.
type peekLoc struct {
	loc  lsp.Location
	text string
	a, b int
}

// peekRow is a file header (loc -1) or one of its locations.
type peekRow struct{ file, loc int }

// openPeek shows locations under the editor, the first one selected.
// ponytail: it reads every result file on the update loop, so a reference
// search over hundreds of files stalls the TUI; move the reads into a
// tea.Cmd that hands back the built rows if that ever bites.
func (m *Model) openPeek(title string, locs []lsp.Location) {
	byPath := map[string][]lsp.Location{}
	for _, l := range locs {
		byPath[l.Path] = append(byPath[l.Path], l)
	}

	pk := &peek{title: title, closed: map[string]bool{}}

	for _, path := range slices.Sorted(maps.Keys(byPath)) {
		ls := byPath[path]
		slices.SortFunc(ls, func(a, b lsp.Location) int { return cmp.Or(a.Line-b.Line, a.Col-b.Col) })

		lines := fileLines(path)
		f := peekFile{path: path}

		for _, l := range ls {
			raw := ""
			if l.Line < len(lines) {
				raw = lines[l.Line]
			}

			a, b := displayCol(raw, lsp.RuneCol(raw, l.Col)), len([]rune(expandTabs(raw)))
			if l.EndLine == l.Line {
				b = displayCol(raw, lsp.RuneCol(raw, l.EndCol))
			}

			text := strings.TrimLeft(expandTabs(raw), " ")
			trim := len([]rune(expandTabs(raw))) - len([]rune(text))
			f.locs = append(f.locs, peekLoc{loc: l, text: text, a: max(a-trim, 0), b: max(b-trim, 0)})
		}

		pk.files = append(pk.files, f)
	}

	pk.build()
	pk.l.sel = min(1, len(pk.rows)-1)
	m.pk, m.focus = pk, onMain
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

// peekH is the height of the widget, 0 when it is closed.
func (m *Model) peekH() int {
	if m.pk == nil || !m.showsPreview() {
		return 0
	}

	return min(max(m.mainH()*45/100, 6), max(m.mainH()-6, 1))
}

func (pk *peek) view(m *Model, w, h int) []string {
	if h < 2 {
		return nil
	}

	name, dir := "", ""
	if loc, ok := pk.shown(); ok {
		name, dir = filepath.Base(loc.loc.Path), filepath.Dir(rel(m.ws, loc.loc.Path))
	}

	head := row(w, pal.sectionBg, []seg{
		sg(" "+name, bold), sg("  "+dir, dim),
		sg(fmt.Sprintf("  — %s (%d) ", pk.title, pk.count()), dim),
	}, sg(" "+icClose.s()+" ", dim))
	body := h - 1
	lw := max(w*3/5, 20)
	rw := max(w-lw-1, 10)
	left := pk.source(m, lw, body)
	pk.l.clamp(len(pk.rows), body)
	right := pk.l.render(rw, body, len(pk.rows), func(i, rww int) string { return pk.renderRow(m, i, rww) })

	out := []string{head}
	for i := range body {
		out = append(out, fit(left[i], lw)+dim.Render("│")+fit(right[i], rw))
	}

	return out
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

	hl := lipgloss.NewStyle().Background(pal.matchBg)

	return ansi.Cut(styled, 0, a) + "\x1b[m" + hl.Render(string(plain[a:b])) + ansi.Cut(styled, b, w)
}

func (pk *peek) renderRow(m *Model, i, w int) string {
	r := pk.rows[i]
	f := pk.files[r.file]
	bg, base := m.rowColors(viewFiles, i == pk.l.sel, false)

	if r.loc < 0 {
		name := filepath.Base(f.path)

		return row(w, bg, []seg{
			sg(" "+chevron(!pk.closed[f.path]), dim), iconSeg(name, false, false), sg(name, base),
			sg(" "+filepath.Dir(rel(m.ws, f.path)), dim),
		}, badge(strconv.Itoa(len(f.locs))), sg(" ", plain))
	}

	l := f.locs[r.loc]
	text := []rune(l.text)
	a, b := max(min(l.a, len(text)), 0), max(min(l.b, len(text)), 0)

	segs := []seg{sg("    ", plain)}
	if a < b {
		segs = append(segs, sg(string(text[:a]), base),
			sgOwn(string(text[a:b]), base.Background(pal.matchBg)), sg(string(text[b:]), base))
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
	h, n := max(m.peekH()-1, 1), len(pk.rows)

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
		if _, ok := pk.selected(); ok {
			m.pk = nil // ⏎ takes you there, o keeps the list open
		}

		return cmd

	case "o", "space":
		return pk.open(m)
	case "esc", "q", "tab":
		m.pk = nil
	}

	return nil
}

func (pk *peek) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()

	h := max(m.peekH()-1, 1)
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		pk.l.wheel(wheelDelta(mo), len(pk.rows), h)
		return nil
	}

	if _, click := msg.(tea.MouseClickMsg); !click || mo.Button != tea.MouseLeft {
		return nil
	}

	w := m.mainW()
	if y == 0 { // the header: only ✕ does anything
		if x >= w-3 {
			m.pk = nil
		}

		return nil
	}

	lw := max(w*3/5, 20)
	if x <= lw { // the source preview is not clickable
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
