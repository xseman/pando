package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}

type seg struct {
	s     string
	st    lipgloss.Style
	ownBg bool // keep the segment's background under a row highlight (badges)
}

func sg(s string, st lipgloss.Style) seg    { return seg{s: s, st: st} }
func sgOwn(s string, st lipgloss.Style) seg { return seg{s: s, st: st, ownBg: true} }

// row paints left segments and right-aligned segments into exactly w cells.
// A non-nil bg is applied to every segment so inner styles don't break it.
func row(w int, bg color.Color, left []seg, right ...seg) string {
	rw := 0
	for _, r := range right {
		rw += strWidth(r.s)
	}

	if rw > w {
		right, rw = nil, 0
	}

	var b strings.Builder

	paint := func(x seg) {
		if bg != nil && !x.ownBg {
			x.st = x.st.Background(bg)
		}

		b.WriteString(x.st.Render(x.s))
	}

	used := 0
	for _, x := range left {
		rem := w - rw - used
		if rem <= 0 {
			break
		}

		if strWidth(x.s) > rem {
			x.s = ansi.Truncate(x.s, rem, "…")
		}

		paint(x)
		used += strWidth(x.s)
	}

	if gap := w - rw - used; gap > 0 {
		paint(sg(strings.Repeat(" ", gap), plain))
	}

	for _, x := range right {
		paint(x)
	}

	return b.String()
}

// renderSides are the escapes st.Render puts either side of a line of text
// with no tab, for st of colors and text attributes only (no width, padding,
// border, or spaces styled apart): pre+text+suf is that text rendered,
// without the pass over every property Render makes.
func renderSides(st lipgloss.Style) (pre, suf string) {
	pre, suf, _ = strings.Cut(st.Render("\x00"), "\x00")

	return pre, suf
}

// fit truncates or pads a pre-styled line to exactly w cells.
func fit(s string, w int) string { return fitWith(s, w, "") }

// fitWith is fit(s, w)+tail in one allocation.
func fitWith(s string, w int, tail string) string {
	if w <= 0 {
		return tail
	}

	// Truncate counts some clusters narrower than StringWidth does (1️⃣):
	// cut a cell shorter until the row fits.
	sw := strWidth(s)
	for lim := w; sw > w; lim-- {
		s = ansi.Truncate(s, lim, "")
		sw = strWidth(s)
	}

	var b strings.Builder

	b.Grow(len(s) + len("\x1b[m") + w - sw + len(tail))
	b.WriteString(s)
	b.WriteString("\x1b[m")

	for range w - sw {
		b.WriteByte(' ')
	}

	b.WriteString(tail)

	return b.String()
}

// strWidth is ansi.StringWidth, which segments every character into its
// grapheme cluster, with plainRun's characters counted without that.
func strWidth(s string) int {
	pstate, w := parser.GroundState, 0

	for i := 0; i < len(s); i++ {
		if pstate == parser.GroundState {
			if n, cells := plainRun(s[i:]); n > 0 {
				w += cells
				i += n - 1

				continue
			}
		}

		state, action := parser.Table.Transition(pstate, s[i])
		if action == parser.PrintAction || state == parser.Utf8State {
			cluster, cw := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			w += cw
			i += len(cluster) - 1
			pstate = parser.GroundState

			continue
		}

		pstate = state
	}

	return w
}

func blank(w int) string { return strings.Repeat(" ", max(w, 0)) }

// wcSafe rewrites each cluster of frame that wcwidth measures other than
// its grapheme width, the width every row is laid out in, as its first rune
// padded to that width: ⚠️ (2 cells, 1 to wcwidth) becomes "⚠ ", 👍🏽 (2, 4)
// "👍". Until the terminal answers mode 2027 Bubble Tea places cells by
// wcwidth, while tmux draws ⚠️ wide: the renderer's idea of the rest of the
// row is then a cell off, and its next partial redraw lands there.
func wcSafe(frame string) string {
	var b strings.Builder

	b.Grow(len(frame))

	for state := ansi.NormalState; frame != ""; {
		// A plain run is one cell a character either way.
		if n, _ := plainRun(frame); n > 0 && state == ansi.NormalState {
			b.WriteString(frame[:n])
			frame = frame[n:]

			continue
		}

		seq, w, n, next := ansi.DecodeSequence(frame, state, nil)
		if w > 0 && frame[0] < utf8.RuneSelf { // DecodeSequence takes ASCII a byte at a time: cut where StringWidth does, which keeps 1️⃣ one cluster
			seq, w = ansi.FirstGraphemeCluster(frame, ansi.GraphemeWidth)
			n = len(seq)
		}

		frame, state = frame[n:], next

		if ansi.WcWidth.StringWidth(seq) == w { // escapes too: 0 either way
			b.WriteString(seq)
			continue
		}

		r, _ := utf8.DecodeRuneInString(seq)
		base := string(r)

		// A lone regional indicator has no safe rune, and a zero-width one
		// (U+06DD prepends) would join the next cluster.
		bw := ansi.WcWidth.StringWidth(base)
		if bw == 0 || bw > w || bw != ansi.StringWidth(base) {
			base, bw = "", 0
		}

		b.WriteString(base + blank(w-bw))
	}

	return b.String()
}

// plainRun measures the text s starts with whose every character is one
// cell to wcwidth and to its grapheme width alike, and a cluster of its own
// while the next one is such too, or ASCII, or the end: printable ASCII, and
// box drawing, block elements and geometric shapes (U+2500–U+25FC; ◽◾ are
// wide). n is its length in bytes and cells in characters, less the last one
// when other text follows, which a mark there would join.
func plainRun(s string) (n, cells int) {
	last, i := 0, 0
	for i < len(s) {
		r, size := rune(s[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(s[i:])
		}

		if (r < ' ' || r >= 0x7f) && (r < 0x2500 || r > 0x25fc) {
			break
		}

		last, i, cells = i, i+size, cells+1
	}

	if i < len(s) && s[i] >= utf8.RuneSelf && cells > 0 {
		return last, cells - 1
	}

	return i, cells
}

// framedBox is a text box w cells wide in one thin frame, VS Code's input
// with input.border, in the accent while it has the keyboard: three rows.
func framedBox(in *textinput.Model, w int, focused, dark bool) []string {
	edge := fg(pal.inputBorder)
	if focused {
		edge = fg(pal.accent)
	}

	box, side, field := lipgloss.NewStyle().Background(pal.inputBg), edge.Background(pal.inputBg), max(w-6, 1)

	in.SetStyles(inputStyles(dark))
	in.SetWidth(field)
	text := ansi.Truncate(realCaret(in.View()), field, "")
	text += box.Render(blank(field - ansi.StringWidth(text)))

	return []string{frameLine(w, "▁", edge), " " + side.Render("▏") + box.Render(" "+text+" ") + side.Render("▕") + " ", frameLine(w, "▔", edge)}
}

// frameLine is the row above (▁) or below (▔) an input box whose ▏ edge
// sits at column 1 and whose ▕ one cell before the row's end: with the edges
// it frames the box in one thin line, as VS Code's input.border does.
func frameLine(w int, glyph string, st lipgloss.Style) string {
	return blank(1) + st.Render(strings.Repeat(glyph, max(w-2, 0))) + blank(min(w, 1))
}

// hoverBox puts pre-styled lines in a box by the mouse, under the pointer, or
// over it when there is no room below: VS Code's hover.
func (m *Model) hoverBox(lines []string) (box []string, x, y int) {
	iw := 0
	for _, l := range lines {
		iw = max(iw, ansi.StringWidth(l)+2)
	}

	side := dim.Render("│")
	box = append(box, dim.Render("╭"+strings.Repeat("─", iw)+"╮"))

	for _, l := range lines {
		box = append(box, side+row(iw, nil, []seg{sg(" "+l, plain)})+side)
	}

	box = append(box, dim.Render("╰"+strings.Repeat("─", iw)+"╯"))

	x, y = max(min(m.mouseX+2, m.w-iw-2), 0), m.mouseY+1
	if y+len(box) > m.panelH() { // above the pointer instead, over the frame's bottom edge from the status bar
		y = max(min(m.mouseY, m.panelH())-len(box), 0)
	}

	return box, x, y
}

// center places s in the middle of w cells.
func center(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return ansi.Truncate(s, max(w, 0), "")
	}

	l := (w - sw) / 2

	return blank(l) + s + blank(w-sw-l)
}

func chevron(open bool) string {
	if open {
		return "▾ "
	}

	return "▸ "
}

// guides is a tree's active indent guide, VS Code's: lit at level lit on the
// rows after act through end, the ones under it. The zero value lights none.
type guides struct{ act, end, lit int }

// activeGuide is the guide lit among n rows depth(i) levels deep: the
// selection's when it has rows under it, else its parent's; none at the root.
func activeGuide(n, sel int, depth func(int) int) guides {
	if sel < 0 || sel >= n {
		return guides{}
	}

	act := sel
	if d := depth(sel); sel+1 == n || depth(sel+1) <= d { // nothing under it: its parent
		act = -1

		for j := sel - 1; j >= 0; j-- {
			if depth(j) < d {
				act = j
				break
			}
		}
	}

	if act < 0 {
		return guides{}
	}

	end, lit := act, depth(act)
	for end+1 < n && depth(end+1) > lit {
		end++
	}

	return guides{act, end, lit}
}

// at is row i's indent guides, depth of them: a │ under each of its
// ancestors' chevrons in the whitespace color, the active one in
// indent_guide's.
func (g guides) at(i, depth int) []seg {
	rule := func(n int) seg { return sg(strings.Repeat("│ ", n), fg(pal.whitespace)) }
	if i <= g.act || i > g.end {
		return []seg{rule(depth)}
	}

	return []seg{rule(g.lit), sg("│ ", fg(pal.indentGuide)), rule(depth - g.lit - 1)}
}

// list is a windowed selection: the wheel scrolls the view only, keys move
// the selection and snap the view to it. sel -1 = nothing selected yet.
// bar is the mouse on its scrollbar, which draws the slider held or hovered.
type list struct {
	sel, top int
	bar      barState
}

func (l *list) clamp(n, h int) {
	if l.sel >= n {
		l.sel = n - 1
	}

	l.top = max(min(l.top, n-h), 0)
}

func (l *list) move(d, n, h int) {
	if n == 0 {
		return
	}

	if l.sel < 0 {
		l.sel = 0
	} else {
		l.sel = max(0, min(n-1, l.sel+d))
	}

	l.snap(h)
}

func (l *list) snap(h int) {
	if l.sel < l.top {
		l.top = l.sel
	}

	if h > 0 && l.sel >= l.top+h {
		l.top = l.sel - h + 1
	}
}

func (l *list) wheel(d, n, h int) { l.top = max(0, min(l.top+d, n-h)) }

func (l *list) at(y, n int) int {
	if i := l.top + y; y >= 0 && i < n {
		return i
	}

	return -1
}

// render draws rows top..top+h with a right-edge scrollbar on overflow.
func (l *list) render(w, h, n int, rowFn func(i, w int) string) []string {
	return l.renderBar(w, h, n, 0, rowFn)
}

// renderBar is render with the scrollbar kept off the first skip lines, rows
// that stay put while the rest scrolls; those get the bar's column too. The
// bar is the editor's: a slider over a solid track.
func (l *list) renderBar(w, h, n, skip int, rowFn func(i, w int) string) []string {
	out := make([]string, max(h, 0))
	bar := n > h && w > 1
	rw := w

	var cells []string

	if bar { // the geometry a click on it reads back (listBar)
		rw = w - 1
		cells = vbar{n - skip, h - skip, l.top}.cells(l.bar)
	}

	for y := range out {
		lw := rw
		if y < skip {
			lw = w
		}

		line := blank(lw)
		if i := l.top + y; i < n {
			line = rowFn(i, lw)
		}

		if bar && y >= skip {
			line += cells[y-skip]
		}

		out[y] = line
	}

	return out
}

// vbar is the geometry of VS Code's editor scrollbar: n rows in all, h of
// them in view from top. The editor and the terminals keep their last column
// for it, as VS Code keeps the scrollbar's width off the text. The editor's
// horizontal bar is the same geometry turned on its side: n cells of the
// widest line, h of them in view from p.left.
type vbar struct{ n, h, top int }

// on reports rows out of view, which is when the slider shows.
func (b vbar) on() bool { return b.h > 0 && b.n > b.h }

// thumb is the slider's first row and its height.
func (b vbar) thumb() (y, sh int) {
	sh = max(1, b.h*b.h/max(b.n, 1))
	y = max(min(b.top, b.n-b.h), 0) * (b.h - sh) / max(b.n-b.h, 1)

	return y, sh
}

// topAt is the first top that puts the slider's first row at row y,
// clamped to what can scroll: thumb and topAt round the same way.
func (b vbar) topAt(y int) int {
	_, sh := b.thumb()

	span := b.h - sh
	if span <= 0 {
		return 0
	}

	y = max(min(y, span), 0)

	return (y*(b.n-b.h) + span - 1) / span
}

// cells are the bar's column, one cell a row: the slider shaded over the
// rows in view, the track a solid column (scrollbar_track; a thin border
// like the overview ruler's without one), and blanks when everything fits.
// A background, not a block glyph: those seam at fractional scaling in VTE.
func (b vbar) cells(s barState) []string {
	out := make([]string, max(b.h, 0))
	if !b.on() {
		for i := range out {
			out[i] = " "
		}

		return out
	}

	slider, track := lipgloss.NewStyle().Background(s.slider(b)).Render(" "), fg(pal.rulerBorder).Render("▏")
	if pal.trackBg != nil {
		track = lipgloss.NewStyle().Background(pal.trackBg).Render(" ")
	}

	ty, sh := b.thumb()
	for i := range out {
		if i >= ty && i < ty+sh {
			out[i] = slider
		} else {
			out[i] = track
		}
	}

	return out
}

// markSet is how marked draws a kind of row, 1, 2 or both: its color, and
// its rank where kinds share a half.
type markSet struct {
	cols [4]color.Color
	rank [4]int
}

// diffMarks are a diff's: red deletions over green additions over blue
// changed lines. probMarks are a file's problems: red errors over yellow
// warnings.
func diffMarks() markSet {
	return markSet{[4]color.Color{nil, pal.diffDelMark, pal.diffAddMark, pal.diffModMark}, [4]int{0, 3, 2, 1}}
}

func probMarks() markSet {
	return markSet{[4]color.Color{nil, pal.errc, pal.warn, pal.errc}, [4]int{0, 3, 1, 3}}
}

// marked are the bar's cells with marks on them, as VS Code's overview
// ruler draws a diff's changes (diffMarks) or a file's problems
// (probMarks). kinds holds each of the b.n rows' bits; a run of marked rows
// is one mark of the kinds in it. Each cell is barSlots marks from top to
// bottom, and row r lands on slot r*slots*h/n (slotOf), so no mark is too
// short to show; where marks share a slot the higher rank shows. Off the
// slider a cell is the block glyph and the two colors, of the track and its
// marks, that draw its slots best (markCell); on the slider a mark is the
// cell's right half (▐) in its own color, so the slider sinks into it and
// still shows on the left, at rest, hovered and dragged.
func (b vbar) marked(s barState, kinds []byte, set markSet) []string {
	out := b.cells(s)
	if !b.on() || len(kinds) != b.n {
		return out
	}

	slot := make([]byte, barSlots*b.h)

	for r := 0; r < len(kinds); {
		if kinds[r] == 0 {
			r++
			continue
		}

		from, k := r, byte(0)
		for ; r < len(kinds) && kinds[r] != 0; r++ {
			k |= kinds[r]
		}

		for i := from; i < r; i++ {
			if at := &slot[b.slotOf(i)]; set.rank[k] > set.rank[*at] {
				*at = k
			}
		}
	}

	ty, sh := b.thumb()

	for i := range b.h {
		cell := [barSlots]byte(slot[barSlots*i : barSlots*(i+1)])
		if cell == ([barSlots]byte{}) {
			continue
		}

		if i >= ty && i < ty+sh {
			top := byte(0)
			for _, k := range cell {
				if set.rank[k] > set.rank[top] {
					top = k
				}
			}

			out[i] = fg(set.cols[top]).Background(s.slider(b)).Render("▐")

			continue
		}

		out[i] = markCell(cell, set)
	}

	return out
}

// barSlots are the marks a scrollbar cell holds, top to bottom: the eighths
// the block glyphs draw.
const barSlots = 8

// barGlyphs are the block glyphs a marked cell can be, with how many of its
// slots the foreground fills, from the top (up) or from the bottom.
var barGlyphs = []struct {
	g  string
	n  int
	up bool
}{{" ", 0, false}, {"▁", 1, false}, {"▂", 2, false}, {"▃", 3, false}, {"▄", 4, false}, {"▅", 5, false}, {"▆", 6, false}, {"▇", 7, false}, {"█", 8, false}, {"▔", 1, true}, {"▀", 4, true}}

// markCell is the glyph and the two colors, each the track's (kind 0) or a
// mark's, that show the cell's slots best: a mark drawn as another or the
// track drawn as a mark costs 1, a mark drawn as track 3, a mark missing
// from the cell altogether 100. Without a track color (the terminal theme)
// the track is the terminal's own background and never a foreground.
func markCell(cell [barSlots]byte, set markSet) string {
	var have [4]bool
	for _, k := range cell {
		have[k] = true
	}

	type pick struct {
		g     string
		f, bg byte
	}

	var best pick

	bestCost := -1

	for f := range byte(4) {
		for bg := range byte(4) {
			if f != 0 && !have[f] || bg != 0 && !have[bg] || f == bg || f == 0 && pal.trackBg == nil {
				continue
			}

			for _, gl := range barGlyphs {
				var shown [4]bool

				cost := 0

				for j, want := range cell {
					got := bg
					if gl.up && j < gl.n || !gl.up && j >= barSlots-gl.n {
						got = f
					}

					shown[got] = true

					switch {
					case got == want:
					case want != 0 && got == 0:
						cost += 3
					default:
						cost++
					}
				}

				for k := 1; k < 4; k++ {
					if have[k] && !shown[k] {
						cost += 100
					}
				}

				if bestCost < 0 || cost < bestCost {
					best, bestCost = pick{gl.g, f, bg}, cost
				}
			}
		}
	}

	return paintCell(best.g, best.f, best.bg, set)
}

// paintCell draws glyph g in kind f's color over kind bg's, kind 0 the
// track's.
func paintCell(g string, f, bg byte, set markSet) string {
	col := func(k byte) color.Color { return ifElse(k == 0, pal.trackBg, set.cols[k]) }

	st := lipgloss.NewStyle()
	if c := col(bg); c != nil {
		st = st.Background(c)
	}

	if g != " " {
		st = st.Foreground(col(f))
	}

	return st.Render(g)
}

// slotOf is the slot row r of the content is marked on: r*slots*h/n, kept
// on the slider while r is in view and off it while not, as thumb scales the
// slider so its ends meet the track's.
func (b vbar) slotOf(r int) int {
	i := r * barSlots * b.h / b.n
	ty, sh := b.thumb()
	lo, hi := barSlots*ty, barSlots*(ty+sh)-1

	switch {
	case r >= b.top && r < b.top+b.h:
		return max(min(i, hi), lo)
	case r < b.top:
		return max(min(i, lo-1), 0)
	}

	return min(max(i, hi+1), barSlots*b.h-1)
}

// hcells is the bar laid along a row, b.h cells: a lower half block (▄)
// a cell, in the slider's color over the columns in view and the track's
// (scrollbar_track, else the ruler's) elsewhere; blanks when it all fits.
func (b vbar) hcells(s barState) string {
	if !b.on() {
		return blank(max(b.h, 0))
	}

	track := ifElse(pal.trackBg != nil, pal.trackBg, pal.rulerBorder)
	half := func(c color.Color, n int) string { return fg(c).Render(strings.Repeat("▄", max(n, 0))) }
	x, sw := b.thumb()

	return half(track, x) + half(s.slider(b), sw) + half(track, b.h-x-sw)
}

// withBar fits lines to w-1 columns and puts b's column after them, h rows.
func withBar(lines []string, w int, b vbar, s barState) []string {
	return withCells(lines, w, b.cells(s))
}

// withCells puts a bar's cells in the last of w columns, beside lines.
func withCells(lines []string, w int, cells []string) []string {
	out := make([]string, len(cells))

	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}

		out[i] = fitWith(line, max(w-1, 0), cells[i])
	}

	return out
}

// scrollDrag follows a drag on a scrollbar: geo reads the bar as it is, to
// scrolls to a top, and off is where on the slider it was picked up.
type scrollDrag struct {
	id    string // the bar's owner: "editor", "editor-h", "session" or "terminal"
	geo   func() vbar
	to    func(top int) tea.Cmd
	y0    int // the screen row of the bar's first row, or column when horiz
	off   int
	horiz bool // the bar runs along a row: the mouse's column moves it
}

// barClick starts dragging bar b from a left click at its row y, which is
// screen row y0+y: on the slider it is grabbed where it was hit, on the track
// the slider jumps to put its middle there first, as VS Code's does.
func (m *Model) barClick(id string, y, y0 int, geo func() vbar, to func(top int) tea.Cmd) tea.Cmd {
	b := geo()
	if !b.on() {
		return nil
	}

	ty, sh := b.thumb()

	var cmd tea.Cmd

	off := y - ty
	if off < 0 || off >= sh {
		off = sh / 2
		cmd = to(b.topAt(y - off))
	}

	m.drag = &drag{kind: dragScroll, bar: &scrollDrag{id: id, geo: geo, to: to, y0: y0, off: off}}

	return cmd
}

// hbarClick is barClick for a bar along a row: x is the click's column on it,
// x0 the screen column of the bar's first cell.
func (m *Model) hbarClick(id string, x, x0 int, geo func() vbar, to func(left int) tea.Cmd) tea.Cmd {
	cmd := m.barClick(id, x, x0, geo, to)
	if m.barActive(id) {
		m.drag.bar.horiz = true
	}

	return cmd
}

// barDragTo moves the dragged slider with the mouse at screen cell (x, y).
func (m *Model) barDragTo(d *scrollDrag, x, y int, release bool) tea.Cmd {
	if release {
		m.drag = nil
		return nil
	}

	if d.horiz {
		y = x
	}

	return d.to(d.geo().topAt(y - d.y0 - d.off))
}

// barActive reports bar id's slider held by the mouse.
func (m *Model) barActive(id string) bool {
	return m.drag != nil && m.drag.kind == dragScroll && m.drag.bar.id == id
}

// barPoint is the scrollbar cell under the pointer: its bar's id and the row
// along it, or the column for a bar along a row. Each bar's hit test notes
// it as the mouse passes, which is all a hover needs.
type barPoint struct {
	id string
	at int
}

// barState is the mouse on a scrollbar: holding its slider, or over the bar
// at row at.
type barState struct {
	held, over bool
	at         int
}

// barState is bar id's state, read where the bar is drawn: the slider may
// have scrolled away from under a resting pointer since.
func (m *Model) barState(id string) barState {
	return barState{held: m.barActive(id), over: m.overBar.id == id, at: m.overBar.at}
}

// slider is the slider's color on bar b: scrollbar_slider_active while held,
// scrollbar_slider_hover while the pointer rests on it, as VS Code's slider
// shades under the pointer.
func (s barState) slider(b vbar) color.Color {
	ty, sh := b.thumb()

	switch {
	case s.held:
		return pal.sliderActiveBg
	case s.over && s.at >= ty && s.at < ty+sh:
		return pal.sliderHoverBg
	}

	return pal.sliderBg
}

// ptree groups slash-separated relative paths by directory.
type ptree struct {
	dirs  map[string]*ptree
	files []string // full relative paths of the files directly in this directory
}

func buildTree(paths []string) *ptree {
	root := &ptree{}
	for _, p := range paths {
		t := root

		parts := strings.Split(p, "/")
		for _, d := range parts[:len(parts)-1] {
			if t.dirs == nil {
				t.dirs = map[string]*ptree{}
			}

			if t.dirs[d] == nil {
				t.dirs[d] = &ptree{}
			}

			t = t.dirs[d]
		}

		t.files = append(t.files, p)
	}

	return root
}

// byLowerBase orders paths by base name, case-insensitively.
func byLowerBase(a, b string) int {
	return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
}

func (t *ptree) dirNames() []string {
	names := slices.Collect(maps.Keys(t.dirs))
	slices.SortFunc(names, byLowerBase)

	return names
}

// sortedFiles orders the directory's files by base name.
func (t *ptree) sortedFiles() []string {
	files := slices.Clone(t.files)
	slices.SortFunc(files, byLowerBase)

	return files
}

// filter is a view's Ctrl+F query line, shown under the view header.
type filter struct {
	input   textinput.Model
	on      bool
	editing bool
}

func newFilter() filter {
	f := filter{input: textinput.New()}
	f.input.Prompt = "/ "
	f.input.Placeholder = "filter · esc clears"

	return f
}

// item is one modal entry; run is nil for informational rows.
type item struct {
	label  string
	hint   string
	run    func(m *Model) tea.Cmd
	styled bool   // label carries its own styles (keycaps, headings)
	inline string // dimmed text right after the label
	detail string // a muted second row under the label, shown while it is selected
	search string // what the filter matches instead of the label
	always bool   // listed whatever the filter: on top while browsing, after a query's matches
	group  string // the heading it is filed under; a prefixed picker's only after ":"
	cont   bool   // the detail row of the selected item above it, placed by focus
	sep    bool   // a rule between groups, or with a label a group's muted heading; run stays nil so it is skipped
}

// cancelItem is the last entry of a menu or a confirmation: it closes the
// overlay and does nothing else.
func cancelItem() item {
	return item{label: "Cancel", run: func(*Model) tea.Cmd { return nil }}
}

// separator is VS Code's menu rule between groups of commands.
func separator() item { return item{sep: true} }

// menuGroups joins groups of items with a rule between them; an empty group
// leaves no rule.
func menuGroups(groups ...[]item) []item {
	var out []item

	for _, g := range groups {
		if len(g) == 0 {
			continue
		}

		if len(out) > 0 {
			out = append(out, separator())
		}

		out = append(out, g...)
	}

	return out
}

// matchItem scores an item for a picker query: words starting with @ or !
// must appear verbatim in its search text, the rest match fuzzily.
func matchItem(q string, it item) int {
	if it.search == "" {
		return fuzzy(q, it.label)
	}

	lower, rest := strings.ToLower(it.search), []string{}

	for _, w := range strings.Fields(q) {
		if len(w) > 1 && (w[0] == '@' || w[0] == '!') {
			if !strings.Contains(lower, strings.ToLower(w)) {
				return -1
			}

			continue
		}

		rest = append(rest, w)
	}

	return fuzzy(strings.Join(rest, " "), it.search)
}

// recentGroup is the heading of the files quick open lists first.
const recentGroup = "recently opened"

// modal is the single overlay: a menu (items), a filterable picker
// (items + filter), or a prompt (input + submit).
type modal struct {
	title     string
	items     []item
	build     func(m *Model) []item // rebuilds items after run when keep is set
	disp      []item                // rows as shown: filtered, ranked, grouped in tree mode
	input     textinput.Model
	filter    bool
	lineQuery bool // a trailing ":12:5" is a position, not part of the match
	submit    func(m *Model, value string) tea.Cmd
	raw       bool // submit gets the value as typed, spaces kept: a table cell's
	keep      bool
	tree      bool // group picker results under their directory
	// prefix starts every query of a picker that lists one kind of thing, "@"
	// for symbols; "@:" after it groups the items under their group, as VS
	// Code's quick access does.
	prefix   string
	change   func(m *Model, v string) tea.Cmd // after the query changed, before refiltering
	move     func(m *Model)                   // after the selection moved
	cancel   func(m *Model)                   // closed without choosing
	complete func(v string) []string          // a prompt's suggestions for what is typed: the rest shows dimmed, tab or → takes it
	treeable bool                             // offer the list/tree toggle
	dialog   bool                             // a question: the title as its message, the items as a row of buttons
	hover    int                              // a dialog's button under the pointer, -1 for none
	pattern  string                           // the query as the labels show it matched
	l        list
	x, y     int     // anchor; x < 0 centers
	form     formBox // a form the box is instead of a list: New or Edit Connection, the indentation
	// area is the screen columns [from, to) the box stays in: a table's keep
	// to the editor area, off a session docked beside it. Zero is the screen.
	area [2]int
}

// choices draws options in a row, a segmented control: the chosen one bold
// on a background, the selection's while the row has the keyboard, the
// rest dim. xs are where each starts, for a click.
func choices(opts []string, sel int, focused bool) (segs []seg, xs []int) {
	at := 0

	for i, o := range opts {
		st := dim
		if i == sel {
			st = bold.Background(ifElse(focused, pal.selBg, pal.hoverBg))
			if focused && pal.selFg != nil {
				st = st.Foreground(pal.selFg)
			}
		}

		segs, xs = append(segs, sgOwn(" "+o+" ", st)), append(xs, at)
		at += ansi.StringWidth(o) + 2
	}

	return segs, xs
}

// formBox is a form a modal holds instead of a list, drawing itself and
// taking its keys, clicks and pastes.
type formBox interface {
	// size is the box's width and rows, its frame included, and the rows it
	// is placed for: a form that grows keeps its top where its tallest has it.
	size() (w, h, placed int)
	lines(w int) []string
	key(m *Model, k tea.KeyPressMsg) tea.Cmd
	click(m *Model, r, cx int) tea.Cmd // row r inside the frame, any of them; cell cx past it
	paste(msg tea.PasteMsg) tea.Cmd
}

// inputStyles paints a text input on the input box background.
func inputStyles(dark bool) textinput.Styles {
	st := textinput.DefaultStyles(dark)
	// The prompt and the placeholder in VS Code's input.placeholderForeground:
	// bubbles' ANSI 7 and 240 vanish, or pass for typed text, on a light theme.
	for _, ss := range []*textinput.StyleState{&st.Focused, &st.Blurred} {
		ss.Text, ss.Placeholder, ss.Prompt = ss.Text.Background(pal.inputBg), fg(pal.descFg).Background(pal.inputBg), fg(pal.descFg).Background(pal.inputBg)
	}

	st.Blurred.Text = st.Focused.Text // VS Code's input keeps its text's color without focus, bubbles' greys it

	st.Cursor.Color = nil   // the text's color: the default, ANSI 7, is near white on a light theme
	st.Cursor.Blink = false // realCaret: a blink would take the terminal's cursor away

	return st
}

// caretMark is where realCaret puts the terminal's cursor: an APC string,
// zero cells wide, that View takes out again.
const caretMark = "\x1b_pando:caret\x1b\\"

// realCaret turns the caret a text box draws, a reverse-video cell, into the
// terminal's cursor, so the terminal decides its shape, blink and color: the
// cell takes its neighbour's style and caretMark goes before it. The boxes
// keep drawing the caret because bubbles' own real cursor counts runes from
// the start of the value, not cells from where the box has scrolled to.
func realCaret(line string) string {
	w := ansi.StringWidth(line)
	buf := uv.NewScreenBuffer(w, 1)
	buf.Method = ansi.GraphemeWidth // as ansi.StringWidth counts
	uv.NewStyledString(line).Draw(buf, buf.Bounds())

	for x := range w {
		c := buf.CellAt(x, 0)
		if c == nil || c.Width == 0 || c.Style.Attrs&uv.AttrReverse == 0 {
			continue
		}

		st := uv.Style{}
		if next := buf.CellAt(x+c.Width, 0); next != nil {
			st = next.Style
		} else if x > 0 {
			st = buf.CellAt(x-1, 0).Style
		}

		// The reset ends the reverse video the cut carries over.
		return ansi.Cut(line, 0, x) + caretMark + ansi.ResetStyle + st.Styled(c.Content) + ansi.Cut(line, x+c.Width, w)
	}

	return line
}

// takeCaret takes every caretMark out of lines and reports the cell the
// first one stood at.
func takeCaret(lines []string) (x, y int, ok bool) {
	for i, l := range lines {
		j := strings.Index(l, caretMark)
		if j < 0 {
			continue
		}

		if !ok {
			x, y, ok = ansi.StringWidth(l[:j]), i, true
		}

		lines[i] = strings.ReplaceAll(l, caretMark, "")
	}

	return x, y, ok
}

// newMessageArea is the commit message box: VS Code's grows with its text up
// to scm.inputMaxLineCount (maxMsgLines) and scrolls beyond. Enter commits,
// so a new line is shift+enter, or alt+enter where the terminal cannot tell
// shift+enter from enter.
func newMessageArea() textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.DynamicHeight, ta.MinHeight, ta.MaxHeight = true, 1, maxMsgLines
	ta.MaxContentHeight = 1 << 16 // MaxHeight alone would cap the text at maxMsgLines lines
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"))
	// ctrl+a selects all as in the editor, not readline's line start.
	ta.KeyMap.LineStart = key.NewBinding(key.WithKeys("home"))
	ta.KeyMap.SelectAll = key.NewBinding(key.WithKeys("ctrl+a"))

	return ta
}

// areaStyles is inputStyles for a textarea: no cursor-line tint, the rows
// past the text painted on the input background too, the editor's selection.
func areaStyles(dark bool) textarea.Styles {
	in, st := inputStyles(dark), textarea.DefaultStyles(dark)
	for _, p := range []struct {
		a *textarea.StyleState
		i textinput.StyleState
	}{{&st.Focused, in.Focused}, {&st.Blurred, in.Blurred}} {
		p.a.Text, p.a.CursorLine, p.a.Placeholder, p.a.Prompt = p.i.Text, p.i.Text, p.i.Placeholder, p.i.Prompt
		p.a.EndOfBuffer = lipgloss.NewStyle().Background(pal.inputBg)
		p.a.Selection = lipgloss.NewStyle().Background(pal.textSelBg) // the editor's
	}

	st.Cursor.Color, st.Cursor.Blink = nil, false // as inputStyles'

	return st
}

func newMenu(title string, x, y int, items ...item) *modal {
	md := &modal{title: title, items: items, x: x, y: y}
	md.refilter()

	return md
}

// newDialog is VS Code's dialog: msg in the body over a row of buttons,
// items[0] the primary one, cancelItem last when there is one. Its items
// carry no detail: refilter would add rows and shift what buttons returns.
func newDialog(msg string, items ...item) *modal {
	md := newMenu(msg, -1, 0, items...)
	md.dialog, md.hover = true, -1

	return md
}

// buttons are the dialog's item indices in the row's order, VS Code's on
// Linux and macOS: Cancel moves next to the primary button, then the row
// reverses, so the primary one is rightmost with Cancel before it.
func (md *modal) buttons() []int {
	order := make([]int, len(md.items))
	for i := range order {
		order[i] = i
	}

	if n := len(order); n > 2 && md.items[n-1].label == "Cancel" {
		order = slices.Insert(order[:n-1], 1, n-1)
	}

	slices.Reverse(order)

	return order
}

// buttonRow lays the buttons out right-aligned in iw cells: each one's item,
// label and first column, in the row's order.
func (md *modal) buttonRow(iw int) (order []int, labels []string, xs []int) {
	order = md.buttons()
	total := 2 * (len(order) - 1)

	for _, i := range order {
		labels = append(labels, md.items[i].label)
		total += ansi.StringWidth(md.items[i].label) + 2
	}
	// ponytail: a row too wide cuts every label to an equal share; wrap the row if dialogs grow buttons.
	if total > iw-1 {
		each := max((iw-1-2*(len(order)-1))/len(order)-2, 1)
		total = 2 * (len(order) - 1)

		for k, l := range labels {
			labels[k] = ansi.Truncate(l, each, "…")
			total += ansi.StringWidth(labels[k]) + 2
		}
	}

	x := max(iw-1-total, 0)
	for _, l := range labels {
		xs = append(xs, x)
		x += ansi.StringWidth(l) + 4
	}

	return order, labels, xs
}

// dialogBody is the dialog's message and every item's hint, wrapped to the
// box's inner width iw; hintH is the tallest hint, so the box keeps its
// height while the focus moves.
func (md *modal) dialogBody(iw int) (msg []string, hints [][]string, hintH int) {
	wrap := func(s string) []string { return strings.Split(ansi.Wrap(s, max(iw-2, 1), ""), "\n") }

	msg = wrap(md.title)

	for _, it := range md.items {
		var h []string
		if it.hint != "" {
			h = wrap(it.hint)
		}

		hints = append(hints, h)
		hintH = max(hintH, len(h))
	}

	return msg, hints, hintH
}

func newPicker(title string, items []item) *modal {
	md := &modal{title: title, items: items, filter: true, x: -1, input: modalInput()}
	md.refilter()

	return md
}

func newPrompt(title, value string, submit func(m *Model, v string) tea.Cmd) *modal {
	md := &modal{title: title, submit: submit, x: -1, input: modalInput()}
	md.input.SetValue(value)
	md.input.CursorEnd()

	return md
}

func modalInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	st := in.Styles()
	st.Cursor.Blink = false // as inputStyles'
	in.SetStyles(st)
	in.Focus()

	return in
}

func (md *modal) refilter() {
	q := ""
	if md.filter {
		q = md.input.Value()
	}

	if md.lineQuery {
		q, _, _ = strings.Cut(q, ":")
	}

	grouped := false

	if md.prefix != "" {
		q = strings.TrimPrefix(q, md.prefix)
		q, grouped = strings.CutPrefix(q, ":")
		q = strings.TrimSpace(q)
	}

	md.pattern = strings.Join(slices.DeleteFunc(strings.Fields(q), func(w string) bool { // what the labels show matched: no @ or ! tokens
		return len(w) > 1 && (w[0] == '@' || w[0] == '!')
	}), " ")

	type hit struct{ i, score int }

	var hits []hit

	for i, it := range md.items {
		if s := matchItem(q, it); s >= 0 && !it.always {
			hits = append(hits, hit{i, s})
		}
	}

	switch {
	case q != "":
		slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.score, a.score) })
	case md.tree: // browsing: alphabetical, like a file tree; a group first, in its own order
		slices.SortStableFunc(hits, func(a, b hit) int {
			ia, ib := md.items[a.i], md.items[b.i]
			switch {
			case ia.group != ib.group:
				return cmp.Compare(ib.group, ia.group) // "recently opened" before "files"
			case ia.group == recentGroup:
				return 0
			}

			return strings.Compare(strings.ToLower(ia.label), strings.ToLower(ib.label))
		})
	}

	var always []item

	for _, it := range md.items {
		if it.always {
			always = append(always, it)
		}
	}

	// A query's matches come first: what ⏎ runs is the best of them.
	last := q != "" && len(hits) > 0

	md.disp = md.disp[:0]
	if !last {
		md.disp = append(md.disp, always...)
	}

	switch {
	case md.prefix != "":
		if grouped {
			slices.SortStableFunc(hits, func(a, b hit) int { return strings.Compare(md.items[a.i].group, md.items[b.i].group) })
		}

		count := map[string]int{}
		for _, h := range hits {
			count[md.items[h.i].group]++
		}

		for i, h := range hits {
			it := md.items[h.i]
			switch {
			case grouped && (i == 0 || md.items[hits[i-1].i].group != it.group):
				md.disp = append(md.disp, heading(fmt.Sprintf("%s (%d)", it.group, count[it.group])))
			case !grouped && i == 0:
				md.disp = append(md.disp, heading(fmt.Sprintf("symbols (%d)", len(hits))))
			}

			md.disp = append(md.disp, it)
		}

	case md.tree:
		groups := map[string][]item{}

		var order []string // directories by their best match

		for _, h := range hits {
			it := md.items[h.i]
			if it.group == recentGroup { // a few files, listed whole rather than by directory
				if len(md.disp) == 0 {
					md.disp = append(md.disp, item{label: recentGroup, sep: true})
				}

				md.disp = append(md.disp, it)

				continue
			}

			dir := filepath.Dir(it.label)
			if _, ok := groups[dir]; !ok {
				order = append(order, dir)
			}

			groups[dir] = append(groups[dir], item{label: "  " + filepath.Base(it.label), hint: it.hint, run: it.run})
		}

		if len(md.disp) > 0 && len(order) > 0 { // after the recent files
			md.disp = append(md.disp, item{}, item{label: "files", sep: true})
		}

		for _, dir := range order {
			md.disp = append(md.disp, item{label: dir + "/"})
			md.disp = append(md.disp, groups[dir]...)
		}

	default:
		if q != "" {
			// A query ranks within each group and orders the groups by their best match.
			rank := map[string]int{}
			for _, h := range hits {
				if _, ok := rank[md.items[h.i].group]; !ok {
					rank[md.items[h.i].group] = len(rank)
				}
			}

			slices.SortStableFunc(hits, func(a, b hit) int {
				return cmp.Compare(rank[md.items[a.i].group], rank[md.items[b.i].group])
			})
		}

		group := ""

		for _, h := range hits {
			it := md.items[h.i]
			if it.group != group {
				if group = it.group; group != "" {
					if len(md.disp) > 0 {
						md.disp = append(md.disp, item{}) // breathing room between groups
					}

					md.disp = append(md.disp, item{label: group, sep: true})
				}
			}

			md.disp = append(md.disp, it)
		}
	}

	if last && len(always) > 0 {
		md.disp = append(md.disp, item{})
		md.disp = append(md.disp, always...)
	}

	if q != "" && len(md.disp) == 0 {
		md.disp = append(md.disp, item{label: "No matching results", sep: true})
	}

	md.l = list{sel: -1}
	md.step(1, 0)

	md.l.top = 0 // step knows no height here and scrolled past a heading over the selection
	if md.l.sel < 0 && len(md.disp) > 0 {
		md.l.sel = 0 // informational menus still scroll from the top
	}
}

// focus selects row i and moves the detail row under it: only the selected
// item shows its detail, so a list of refs or remotes stays one row each.
func (md *modal) focus(i int) {
	if j := slices.IndexFunc(md.disp, func(it item) bool { return it.cont }); j >= 0 {
		md.disp = slices.Delete(md.disp, j, j+1)
		if i > j {
			i--
		}

		if j < md.l.top {
			md.l.top-- // the rows on screen stay put
		}
	}

	md.l.sel = i
	if i >= 0 && i < len(md.disp) && md.disp[i].detail != "" {
		md.disp = slices.Insert(md.disp, i+1, item{label: md.disp[i].detail, cont: true})
	}
}

// step moves to the next actionable row in direction d, scrolling when none is left.
func (md *modal) step(d, rows int) {
	for i := md.l.sel + d; i >= 0 && i < len(md.disp); i += d {
		if md.disp[i].run != nil {
			md.focus(i)
			md.l.snap(rows)

			if i = md.l.sel; i+1 < len(md.disp) && md.disp[i+1].cont && i+1 >= md.l.top+rows {
				md.l.top++ // keep the item's detail row in view
			}

			return
		}
	}

	md.l.wheel(d, len(md.disp), rows)
}

func (md *modal) hasInput() bool { return md.filter || md.submit != nil }

// rect returns the box position and size, and how many item rows fit.
func (md *modal) rect(m *Model) (x, y, w, h, rows int) {
	x0, x1 := 0, m.w
	if md.area[1] > 0 {
		x0, x1 = md.area[0], md.area[1]
	}

	if md.form != nil { // its top stays put as it grows: placed as if at its tallest
		fw, fh, placed := md.form.size()
		w, h = min(fw, x1-x0-2), fh

		return x0 + (x1-x0-w)/2, max((m.h-placed)/3, 0), w, h, 0
	}

	if md.dialog {
		_, labels, _ := md.buttonRow(1 << 16)
		row := 2 * (len(labels) - 1)

		for _, l := range labels {
			row += ansi.StringWidth(l) + 2
		}

		w = ansi.StringWidth(md.title)
		for _, it := range md.items {
			w = max(w, ansi.StringWidth(it.hint))
		}

		w = min(max(min(w+4, 64), row+3), x1-x0-2)
		msg, _, hintH := md.dialogBody(w - 2)
		h = len(msg) + hintH + 4
		// ponytail: taller than the terminal clips the bottom, buttons and all; scroll the message if one ever does.
		return max(x0+(x1-x0-w)/2, x0), max((m.h-h)/3, 0), w, h, 0
	}

	w = ansi.StringWidth(md.title) + 6
	for _, it := range md.items {
		w = max(w, ansi.StringWidth(it.label)+ansi.StringWidth(it.inline)+ansi.StringWidth(it.hint)+6)
	}

	if md.hasInput() {
		w = max(w, 60)
		if md.x >= 0 { // a box opened at a place in the text stays out of the way
			w = max(min(w, 30), ansi.StringWidth(md.input.Value())+8)
		}
	}

	w = min(w, x1-x0-2)
	maxRows := max(m.h-6, 1)

	rows = min(len(md.disp), maxRows)
	if md.filter {
		rows = max(rows, min(8, maxRows)) // a picker keeps a minimum height
	}

	if md.submit != nil {
		rows = 0
	}

	h = rows + 2
	if md.hasInput() {
		h++
	}

	if md.x < 0 {
		// A picker's top edge stays put while results change: it is placed as if full.
		top := h
		if md.filter {
			top = maxRows + 3
		}

		x, y = x0+(x1-x0-w)/2, max((m.h-top)/3, 0)
	} else {
		x, y = min(md.x, x1-w), min(md.y, m.h-h)
	}

	return max(x, x0), max(y, 0), w, h, rows
}

// toggle is the list/tree switch drawn at the right end of the title row.
func (md *modal) toggle() (label string, width int) {
	if !md.treeable {
		return "", 0
	}

	label = "[tree]"
	if md.tree {
		label = "[list]"
	}

	return label, ansi.StringWidth(" ^t "+label) + 1
}

func (md *modal) view(m *Model) (string, int, int) {
	x, y, w, _, rows := md.rect(m)
	iw := w - 2

	if md.form != nil {
		return strings.Join(md.form.lines(w), "\n"), x, y
	}

	if md.dialog {
		return md.dialogView(iw), x, y
	}

	title := ""
	if md.title != "" {
		title = " " + md.title + " "
	}

	label, tw := md.toggle()
	title = ansi.Truncate(title, max(iw-tw, 0), "…")

	// A menu at the pointer has square corners, as an editor's context menu;
	// a centered picker keeps the round ones.
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	if md.x >= 0 && !md.hasInput() {
		tl, tr, bl, br = "┌", "┐", "└", "┘"
	}

	top := dim.Render(tl) + bold.Render(title) + dim.Render(strings.Repeat("─", max(iw-tw-ansi.StringWidth(title), 0)))
	if tw > 0 {
		top += dim.Render(" ^t ") + fg(pal.headerAccent).Render(label) + " "
	}

	lines := []string{top + dim.Render(tr)}
	side := dim.Render("│")

	if md.hasInput() {
		md.input.SetWidth(iw - 3)
		lines = append(lines, side+fit(realCaret(md.input.View()), iw)+side)
	}

	for _, r := range md.l.render(iw, rows, len(md.disp), func(i, rw int) string {
		it := md.disp[i]
		if it.cont {
			var bg color.Color
			if i-1 == md.l.sel {
				bg = pal.selBg
			}

			return row(rw, bg, []seg{sg("   "+it.label, dim)})
		}

		if it.sep {
			if it.label != "" {
				return fit(" "+muted.Render(it.label), rw)
			}

			return dim.Render(strings.Repeat("─", rw))
		}

		if it.styled {
			return fit(" "+it.label, rw)
		}

		st, note := plain, muted
		if it.run == nil {
			st = dim
		}

		var bg color.Color
		if i == md.l.sel && it.run != nil {
			bg, note = pal.selBg, dim // the grey can be the selection's own color (the terminal theme's 8)
			if pal.selFg != nil {
				st = st.Foreground(pal.selFg)
			}
		}

		left := append([]seg{sg(" ", st)}, marked(it.label, fuzzyHits(md.pattern, it.label), st)...)
		if it.inline != "" {
			left = append(left, sg(" "+it.inline, note))
		}

		return row(rw, bg, left, sg(it.hint+" ", note))
	}) {
		lines = append(lines, side+r+side)
	}

	lines = append(lines, dim.Render(bl+strings.Repeat("─", iw)+br))

	return strings.Join(lines, "\n"), x, y
}

// dialogView draws the message, the focused button's hint and the buttons,
// the focused one in the button color: what ⏎ runs.
func (md *modal) dialogView(iw int) string {
	msg, hints, hintH := md.dialogBody(iw)
	side := dim.Render("│")
	lines := []string{dim.Render("╭" + strings.Repeat("─", iw) + "╮")}

	for _, l := range msg {
		lines = append(lines, side+fit(" "+bold.Render(l), iw)+side)
	}

	var hint []string
	if md.l.sel >= 0 && md.l.sel < len(hints) {
		hint = hints[md.l.sel]
	}

	for i := range hintH {
		l := ""
		if i < len(hint) {
			l = dim.Render(hint[i])
		}

		lines = append(lines, side+fit(" "+l, iw)+side)
	}

	var b strings.Builder

	order, labels, xs := md.buttonRow(iw)
	at := 0

	for k, i := range order {
		st := lipgloss.NewStyle().Background(pal.mutedButtonBg).Foreground(pal.mutedButtonFg)

		switch hot := i == md.hover; {
		case i == md.l.sel:
			st = lipgloss.NewStyle().Background(ifElse(hot, pal.buttonHoverBg, pal.buttonBg)).Foreground(pal.buttonFg)
		case hot:
			st = st.Background(pal.selBg)
		}

		b.WriteString(strings.Repeat(" ", max(xs[k]-at, 0)) + st.Render(" "+labels[k]+" "))
		at = xs[k] + ansi.StringWidth(labels[k]) + 2
	}

	lines = append(lines, side+strings.Repeat(" ", iw)+side, side+fit(b.String(), iw)+side,
		dim.Render("╰"+strings.Repeat("─", iw)+"╯"))

	return strings.Join(lines, "\n")
}

// dialogKey takes every key while a dialog shows: ←→ and tab walk the
// buttons, wrapping, ⏎ runs the focused one, esc cancels.
func (md *modal) dialogKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	order := md.buttons()
	at := slices.Index(order, md.l.sel)

	switch k.String() {
	case "left", "h", "shift+tab":
		md.l.sel = order[(at-1+len(order))%len(order)]
	case "right", "l", "tab":
		md.l.sel = order[(at+1)%len(order)]
	case "enter", "space":
		return md.choose(m, md.l.sel)
	case "esc", "ctrl+c", "q":
		md.dismiss(m)
	}

	return nil
}

func (md *modal) toggleTree(m *Model) tea.Cmd {
	md.tree = !md.tree
	md.refilter()

	return m.setSettings(map[string]any{"quick_open_tree": md.tree})
}

func (md *modal) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch {
	case md.form != nil:
		return md.form.key(m, k)
	case md.dialog:
		return md.dialogKey(m, k)
	}

	_, _, _, _, rows := md.rect(m)
	if md.complete != nil {
		switch s := k.String(); {
		case s == "tab" || s == "right" && md.input.Position() == len([]rune(md.input.Value())):
			if sug := md.input.CurrentSuggestion(); sug != "" && sug != md.input.Value() {
				md.input.SetValue(sug)
				md.input.CursorEnd()
				md.input.SetSuggestions(md.complete(sug))

				return nil
			}

			if s == "tab" {
				return nil
			}

		case s == "up" || s == "down": // through the other suggestions
			var cmd tea.Cmd

			md.input, cmd = md.input.Update(k)

			return cmd
		}
	}

	switch k.String() {
	case "esc", "ctrl+c":
		md.dismiss(m)
		return nil

	case "up", "ctrl+p", "shift+tab":
		md.step(-1, rows)
		md.moved(m)

		return nil

	case "down", "ctrl+n", "tab":
		md.step(1, rows)
		md.moved(m)

		return nil

	case "ctrl+t":
		if md.treeable {
			return md.toggleTree(m)
		}

	case "enter":
		if md.submit != nil {
			m.modal = nil
			if md.raw {
				return md.submit(m, md.input.Value())
			}

			return md.submit(m, strings.TrimSpace(md.input.Value()))
		}

		return md.choose(m, md.l.sel)
	}

	if !md.hasInput() {
		switch k.String() {
		case "k":
			md.step(-1, rows)
		case "j":
			md.step(1, rows)
		case "q":
			m.modal = nil
		}

		return nil
	}

	var cmd tea.Cmd

	before := md.input.Value()

	md.input, cmd = md.input.Update(k)
	if v := md.input.Value(); md.complete != nil && v != before {
		md.input.SetSuggestions(md.complete(v))
	}

	if v := md.input.Value(); md.filter && v != before {
		if md.change != nil {
			if c := md.change(m, v); c != nil || m.modal != md {
				return tea.Batch(cmd, c)
			}
		}

		md.refilter()
		md.moved(m)
	}

	return cmd
}

// dismiss closes the modal without choosing anything.
func (md *modal) dismiss(m *Model) {
	m.modal = nil
	if md.cancel != nil {
		md.cancel(m)
	}
}

func (md *modal) moved(m *Model) {
	if md.move != nil && md.l.sel >= 0 && md.l.sel < len(md.disp) && md.disp[md.l.sel].run != nil {
		md.move(m)
	}
}

// rowAt is the item at list row y; a detail row belongs to the item above.
func (md *modal) rowAt(y, rows int) int {
	i := md.l.at(y, len(md.disp))
	if i < 0 || y >= rows {
		return -1
	}

	if md.disp[i].cont && i > 0 {
		i--
	}

	return i
}

func (md *modal) choose(m *Model, i int) tea.Cmd {
	if i < 0 || i >= len(md.disp) {
		return nil
	}

	it := md.disp[i]
	if it.run == nil {
		return nil
	}

	if !md.keep {
		m.modal = nil
	}

	cmd := it.run(m)
	if md.keep && md.build != nil && m.modal == md {
		sel := md.l.sel
		md.items = md.build(m)
		md.refilter()
		md.focus(sel)
	}

	return cmd
}

func (md *modal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	mo := msg.Mouse()
	x, y, w, h, rows := md.rect(m)
	inside := mo.X >= x && mo.X < x+w && mo.Y >= y && mo.Y < y+h

	if wf, ok := md.form.(interface{ wheel(cx, d int) }); ok && inside {
		if _, wheel := msg.(tea.MouseWheelMsg); wheel {
			wf.wheel(mo.X-x-1, wheelDelta(mo))
			return nil
		}
	}

	if _, click := msg.(tea.MouseClickMsg); md.form != nil && click { // outside closes it, a row takes the keyboard
		if r := mo.Y - y - 1; !inside {
			md.dismiss(m)
		} else if r >= 0 {
			return md.form.click(m, r, mo.X-x-1)
		}

		return nil
	}

	if md.dialog { // a click runs a button or, outside, cancels; hover lights a button and leaves the focus, as VS Code's
		md.hover = -1

		order, labels, xs := md.buttonRow(w - 2)
		for k, i := range order {
			if bx := x + 1 + xs[k]; mo.Y == y+h-2 && mo.X >= bx && mo.X < bx+ansi.StringWidth(labels[k])+2 {
				md.hover = i
			}
		}

		if _, ok := msg.(tea.MouseClickMsg); !ok || mo.Y != y+h-2 && inside {
			return nil
		}

		if !inside {
			md.dismiss(m)
			return nil
		}

		if md.hover >= 0 {
			md.l.sel = md.hover
			return md.choose(m, md.hover)
		}

		return nil
	}

	top := y + 1
	if md.hasInput() {
		top++
	}

	switch msg.(type) {
	case tea.MouseMotionMsg: // the row under the mouse is the selection, like a GUI menu
		if i := md.rowAt(mo.Y-top, rows); inside && i >= 0 && md.disp[i].run != nil && i != md.l.sel {
			md.focus(i)
			md.moved(m)
		}

	case tea.MouseWheelMsg:
		md.l.wheel(wheelDelta(mo), len(md.disp), rows)
	case tea.MouseClickMsg:
		if !inside {
			md.dismiss(m)
			return nil
		}

		if _, tw := md.toggle(); tw > 0 && mo.Y == y && mo.X >= x+w-1-tw && mo.X < x+w-1 {
			return md.toggleTree(m)
		}

		if i := md.rowAt(mo.Y-top, rows); i >= 0 {
			md.l.sel = i
			return md.choose(m, i)
		}
	}

	return nil
}

// fuzzy scores a case-insensitive subsequence match; -1 = no match.
func fuzzy(pattern, s string) int {
	best, _ := fuzzyFrom(pattern, s)
	return best
}

// fuzzyFrom is fuzzy and where in s its best match starts, -1 for none.
func fuzzyFrom(pattern, s string) (best, from int) {
	if pattern == "" {
		return 0, -1
	}

	p, ls := strings.ToLower(pattern), strings.ToLower(s)
	best, from = -1, -1
	// ponytail: greedy match from every occurrence of the first rune, O(n·m); fine for paths.
	for start := strings.IndexByte(ls, p[0]); start >= 0; {
		score, pi, prev := 0, 0, -2
		for i := start; i < len(ls) && pi < len(p); i++ {
			if ls[i] != p[pi] {
				continue
			}

			score += 10
			if i == prev+1 {
				score += 15
			}

			if i == 0 || strings.IndexByte("/_-. ", ls[i-1]) >= 0 {
				score += 10
			}

			prev = i
			pi++
		}

		if pi < len(p) {
			break // later starts cannot match either
		}

		if sc := score*100 - len(s); sc > best {
			best, from = sc, start
		}

		next := strings.IndexByte(ls[start+1:], p[0])
		if next < 0 {
			break
		}

		start += next + 1
	}

	return best, from
}

// fuzzyHits are the bytes of s that pattern's best match takes, for a row to
// show them; none without a match, or where s is not plain ASCII there.
func fuzzyHits(pattern, s string) []int {
	_, at := fuzzyFrom(pattern, s)
	if at < 0 || len(strings.ToLower(s)) != len(s) {
		return nil
	}

	var out []int

	p, ls := strings.ToLower(pattern), strings.ToLower(s)
	for i, pi := at, 0; i < len(ls) && pi < len(p); i++ {
		if ls[i] == p[pi] {
			if s[i] >= utf8.RuneSelf { // a byte inside a rune
				return nil
			}

			out, pi = append(out, i), pi+1
		}
	}

	return out
}

// marked is label in st with the bytes at, a fuzzy match, in the accent:
// VS Code's list.highlightForeground.
func marked(label string, at []int, st lipgloss.Style) []seg {
	hi := st.Foreground(pal.headerAccent).Bold(true)

	var out []seg

	last := 0

	for _, i := range at {
		if i > last {
			out = append(out, sg(label[last:i], st))
		}

		out, last = append(out, sg(label[i:i+1], hi)), i+1
	}

	return append(out, sg(label[last:], st))
}

func openExternal(path string) error {
	for _, opener := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(opener); err == nil {
			return exec.Command(p, path).Start()
		}
	}

	return exec.ErrNotFound
}

// clicks counts clicks in a row on the same cell: double and triple clicks.
type clicks struct {
	x, y, n int
	at      time.Time
}

// count is which click in a row this one is: 1, 2, 3, then 1 again.
func (c *clicks) count(x, y int) int {
	if c.x == x && c.y == y && time.Since(c.at) < 400*time.Millisecond && c.n < 3 {
		c.n++
	} else {
		c.n = 1
	}

	c.x, c.y, c.at = x, y, time.Now()

	return c.n
}

func (c *clicks) double(x, y int) bool { return c.count(x, y) == 2 }
