package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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

		if pre, suf, ok := textSides(x.st, x.s); ok {
			b.WriteString(pre)
			b.WriteString(x.s)
			b.WriteString(suf)

			return
		}

		b.WriteString(x.st.Render(x.s))
	}

	used := 0
	for i, x := range left {
		rem := w - rw - used
		if rem <= 0 {
			break
		}

		// A segment that fills the row while more text follows ends in … too:
		// the cut shows, whichever segment it falls on.
		if sw := strWidth(x.s); sw > rem || sw == rem && slices.ContainsFunc(left[i+1:], func(s seg) bool { return strings.TrimSpace(s.s) != "" }) {
			x.s = ansi.Truncate(x.s, rem-1, "") + "…"
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
	k, ok := textKey(st)
	if !ok {
		pre, suf, _ = strings.Cut(st.Render("\x00"), "\x00")
		return pre, suf
	}

	return sidesOf(k, st)
}

// renderText is st.Render(s) byte for byte, through textSides where it can.
func renderText(st lipgloss.Style, s string) string {
	if pre, suf, ok := textSides(st, s); ok {
		return pre + s + suf
	}

	return st.Render(s)
}

// textSides are renderSides(st) where pre+s+suf is st.Render(s) byte for
// byte (FuzzRenderText holds it): s one line with no tab, st colors and
// attributes only, and none that Render styles a rune at a time (underline,
// strikethrough). Render passes over every property a style has, a frame's
// costliest call; the sides are kept per style.
func textSides(st lipgloss.Style, s string) (pre, suf string, ok bool) {
	if strings.ContainsAny(s, "\t\n") {
		return "", "", false
	}

	k, ok := textKey(st)
	if !ok || k.ul != lipgloss.UnderlineNone || k.strike || st.GetUnderlineSpaces() || st.GetStrikethroughSpaces() {
		return "", "", false
	}

	pre, suf = sidesOf(k, st)

	return pre, suf, true
}

// styleKey is what Render styles text by: colors and attributes.
type styleKey struct {
	bold, italic, strike, reverse, blink, faint bool
	ul                                          lipgloss.Underline
	fg, bg, ulColor                             color.Color
}

// textKey is st's styleKey; ok is false for a style that lays text out
// (width, padding, margins, borders, a transform, a link, a value).
func textKey(st lipgloss.Style) (k styleKey, ok bool) {
	pt, pr, pb, pl := st.GetPadding()
	mt, mr, mb, ml := st.GetMargin()
	border, bt, br, bb, bl := st.GetBorder()

	if link, _ := st.GetHyperlink(); link != "" || st.Value() != "" || st.GetTransform() != nil ||
		st.GetWidth()|st.GetHeight()|st.GetMaxWidth()|st.GetMaxHeight()|pt|pr|pb|pl|mt|mr|mb|ml != 0 ||
		bt || br || bb || bl || border != (lipgloss.Border{}) {
		return k, false
	}

	return styleKey{
		st.GetBold(), st.GetItalic(), st.GetStrikethrough(), st.GetReverse(), st.GetBlink(), st.GetFaint(),
		st.GetUnderlineStyle(), st.GetForeground(), st.GetBackground(), st.GetUnderlineColor(),
	}, true
}

// sides holds the escapes either side of text by styleKey.
var sides sync.Map

func sidesOf(k styleKey, st lipgloss.Style) (pre, suf string) {
	if v, ok := sides.Load(k); ok {
		s, _ := v.([2]string)
		return s[0], s[1]
	}

	pre, suf, _ = strings.Cut(st.Render("\x00"), "\x00")
	sides.Store(k, [2]string{pre, suf})

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

// headCells is ansi.Truncate(s, n, ""), its styles closed, reading s only as
// far as the cut: Truncate reads on to the end, a line of a minified bundle
// megabytes past what a row shows.
func headCells(s string, n int) string {
	if h := cutHead(s, n); len(h) < len(s) {
		return h + "\x1b[m"
	}

	return s
}

// cutHead is the head of s at most n cells wide, cut between graphemes, its
// styles left open; s itself when it fits.
func cutHead(s string, n int) string {
	w := 0

	for i, state := 0, ansi.NormalState; i < len(s); {
		if k, cells := plainRun(s[i:]); k > 0 && state == ansi.NormalState {
			if w+cells <= n {
				w, i = w+cells, i+k
				continue
			}

			for j := range s[i : i+k] { // one cell a character: cut at the (n-w)th
				if w == n {
					return s[:i+j]
				}

				w++
			}
		}

		_, sw, k, next := ansi.DecodeSequence(s[i:], state, nil)
		if sw > 0 && s[i] < utf8.RuneSelf { // a byte at a time: cut where StringWidth does, 0🏽 one cluster
			var seq string

			seq, sw = ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			k = len(seq)
		}

		if sw > 0 && w+sw > n {
			return s[:i]
		}

		w, i, state = w+sw, i+k, next
	}

	return s
}

// cutTail is ansi.TruncateLeft(s, n, "") byte for byte: s past its first n
// cells, the escapes among them kept, a cluster the cut splits kept whole.
// plainRun's characters are counted without segmenting them, which
// TruncateLeft does to each, its loop otherwise.
func cutTail(s string, n int) string {
	if n <= 0 {
		return s
	}

	var b strings.Builder

	w, pstate := 0, parser.GroundState

	for i := 0; i < len(s); {
		if pstate == parser.GroundState { // inside an escape there is no text to run
			k, cells := plainRun(s[i:])
			if k > 0 && w+cells <= n {
				w, i = w+cells, i+k
				continue
			}

			for j := range s[i : i+k] { // one cell a character: the rest from the (n-w)th
				if w == n {
					return b.String() + s[i+j:]
				}

				w++
			}
		}

		state, action := parser.Table.Transition(pstate, s[i])
		if state == parser.Utf8State {
			cluster, cw := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			if w += cw; w > n {
				return b.String() + s[i:]
			}

			i += len(cluster)

			continue
		}

		switch action {
		case parser.PrintAction:
			w++
			if w > n {
				return b.String() + s[i:]
			}

			i++

			continue

		case parser.ExecuteAction: // a control among the cells cut goes with them
			i++
			continue
		}

		b.WriteByte(s[i])
		i++
		pstate = state
	}

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

	whole, safe := frame, true // safe: nothing replaced yet, nothing copied: most rows come back as they are
	keep := func(s string) {
		if !safe {
			b.WriteString(s)
		}
	}

	for state := ansi.NormalState; frame != ""; {
		// A plain run is one cell a character either way.
		if n, _ := plainRun(frame); n > 0 && state == ansi.NormalState {
			keep(frame[:n])
			frame = frame[n:]

			continue
		}

		if n := escLen(frame); state == ansi.NormalState && len(frame) > 1 && frame[0] == ansi.ESC && strings.IndexByte("[]_P^X", frame[1]) >= 0 && n < len(frame) { // a whole escape, a row's SGRs most of all: no width to either
			keep(frame[:n])
			frame = frame[n:]

			continue
		}

		seq, w, n, next := ansi.DecodeSequence(frame, state, nil)
		if w == 0 && frame[0] == ansi.ESC { // an escape: no width to either
			keep(seq)

			frame, state = frame[n:], next

			continue
		}

		if w > 0 && frame[0] < utf8.RuneSelf { // DecodeSequence takes ASCII a byte at a time: cut where StringWidth does, which keeps 1️⃣ one cluster
			seq, w = ansi.FirstGraphemeCluster(frame, ansi.GraphemeWidth)
			n = len(seq)
		}

		at := len(whole) - len(frame) // where seq starts
		frame, state = frame[n:], next

		if ansi.WcWidth.StringWidth(seq) == w { // escapes too: 0 either way
			keep(seq)
			continue
		}

		if safe { // the first cluster to replace: what came before it as it was
			safe = false

			b.Grow(len(whole))
			b.WriteString(whole[:at])
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

	if safe {
		return whole
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
		if s[i] >= ' ' && s[i] < 0x7f { // a stretch of printable ASCII, a cell a byte
			j := i + 1
			for j < len(s) && s[j] >= ' ' && s[j] < 0x7f {
				j++
			}

			last, i, cells = j-1, j, cells+j-i

			continue
		}

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

	side, field := edge.Background(pal.inputBg), max(w-6, 1)

	in.SetStyles(inputStyles(dark))
	in.SetWidth(field)
	text := ansi.Truncate(realCaret(in.View()), field, "")
	text = underBg(" "+text+blank(field-ansi.StringWidth(text))+" ", pal.inputBg) // the input leaves its caret cell, and the cells after its resets, bare

	return []string{frameLine(w, "▁", edge), " " + side.Render("▏") + text + side.Render("▕") + " ", frameLine(w, "▔", edge)}
}

// frameLine is the row above (▁) or below (▔) an input box whose ▏ edge
// sits at column 1 and whose ▕ one cell before the row's end: with the edges
// it frames the box in one thin line, as VS Code's input.border does.
func frameLine(w int, glyph string, st lipgloss.Style) string {
	return blank(1) + renderText(st, strings.Repeat(glyph, max(w-2, 0))) + blank(min(w, 1))
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

// chevron is a tree node's twistie and the space after it.
func chevron(open bool) string {
	if open {
		return icTwistyD.s() + " "
	}

	return icTwistyR.s() + " "
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

	slider, track := renderText(lipgloss.NewStyle().Background(s.slider(b)), " "), renderText(fg(pal.rulerBorder), "▏")
	if pal.trackBg != nil {
		track = renderText(lipgloss.NewStyle().Background(pal.trackBg), " ")
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

			out[i] = renderText(fg(set.cols[top]).Background(s.slider(b)), "▐")

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
	half := func(c color.Color, n int) string { return renderText(fg(c), strings.Repeat("▄", max(n, 0))) }
	x, sw := b.thumb()

	return half(track, x) + half(s.slider(b), sw) + half(track, b.h-x-sw)
}

// withBar fits lines to w-1 columns and puts b's column after them, h rows.
func (m *Model) withBar(lines []string, w int, b vbar, s barState) []string {
	return m.withCells(lines, w, b.cells(s))
}

// withCells puts a bar's cells in the last of w columns, beside lines.
func (m *Model) withCells(lines []string, w int, cells []string) []string {
	out := make([]string, len(cells))

	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}

		out[i] = m.fit(line, max(w-1, 0), cells[i])
	}

	return out
}

// memo holds a pure function's answers for this frame and the last: most
// of a frame's rows are the last one's, and measuring or cutting a row
// costs about as much as drawing it.
type memo[K comparable, V any] struct{ now, was map[K]V }

// get is f's answer for k, the one the last frame got where it asked.
func (c *memo[K, V]) get(k K, f func() V) V {
	v, ok := c.now[k]
	if ok {
		return v
	}

	if v, ok = c.was[k]; !ok {
		v = f()
	}

	if c.now == nil {
		c.now = map[K]V{}
	}

	c.now[k] = v

	return v
}

// frame starts a frame: the answers of the one before the last go, their
// map kept for this frame's.
func (c *memo[K, V]) frame() {
	c.was, c.now = c.now, c.was
	clear(c.now)
}

// fitKey is what fitWith fits: s to w cells, then tail.
type fitKey struct {
	s, tail string
	w       int
}

// fit is fitWith(s, w, tail), remembered for the frame and the next.
func (m *Model) fit(s string, w int, tail string) string {
	return m.fits.get(fitKey{s, tail, w}, func() string { return fitWith(s, w, tail) })
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
	inline string // dimmed text right after the label
	detail string // a muted second row under the label, shown while it is selected
	search string // what the filter matches instead of the label
	group  string // the heading it is filed under; a prefixed picker's only after ":"
	// segs and right draw the row in their own colors in place of label and
	// hint, which stay its plain text: quick open's Sessions tree.
	segs, right []seg
	file        string // a file's path: the row draws its icon, and in the name view its name before its folder

	// The flags last, in one word: quick open copies a worktree's files.
	styled bool  // label carries its own styles (keycaps, headings)
	always bool  // listed whatever the filter: on top while browsing, after a query's matches
	cont   bool  // the detail row of the selected item above it, placed by focus
	sep    bool  // a rule between groups, or with a label a group's muted heading; run stays nil so it is skipped
	lead   bool  // the row a fresh list selects, over its first
	sess   bool  // a session's row in quick open's Sessions tree, or a project's with none: what All counts
	words  bool  // the label lights the query's words, as text found does, not its letters
	dimTo  uint8 // the label's first bytes drawn dim: a line's number
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
func matchItem(q string, it *item) int {
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
	build     func(m *Model) []item  // rebuilds items after run when keep is set
	back      func(m *Model) tea.Cmd // a submenu's way back to its parent: ← and its first row
	disp      []item                 // rows as shown: filtered, ranked, grouped in tree mode
	input     textinput.Model
	filter    bool
	lineQuery bool // a trailing ":12:5" is a position, not part of the match
	submit    func(m *Model, value string) tea.Cmd
	raw       bool // submit gets the value as typed, spaces kept: a table cell's
	selected  bool // its value is selected, as VS Code's input box: a key or a paste replaces it, a move keeps it
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
	views    bool                             // offer ^t through quickViews, quick open's
	treeable bool                             // the tree among them
	fileView int                              // the quickViews one shown
	// tabs are a picker's kinds, drawn in place of the title: tab and
	// shift+tab or a click go to another through setTab. Quick open's.
	tabs   []string
	tab    int
	setTab func(m *Model, i int) tea.Cmd
	alt    func(m *Model) tea.Cmd // alt+⏎
	// keys are a picker's own keys, which act on its selection: the
	// prompts' ctrl+e and shift+del.
	keys  map[string]func(m *Model) tea.Cmd
	note  string // dim at the right end of the title row
	hints string // keys in the bottom edge, the last dropped where they do not fit
	// caps are the most rows a group shows under a query, in the order the
	// items list the groups; the rest wait behind a "+ N more" row that goes
	// to the tab named like the group.
	caps    map[string]int
	dialog  bool   // a question: the title as its message, the items as a row of buttons
	hover   int    // a dialog's button under the pointer, -1 for none
	pattern string // the query as the labels show it matched
	reveal  bool   // the selection scrolls into view at the next draw: refilter knows no height
	l       list
	x, y    int     // anchor; x < 0 centers
	form    formBox // a form the box is instead of a list: New or Edit Connection, the indentation
	// area is the screen columns [from, to) the box stays in: a table's keep
	// to the editor area. Zero is the screen.
	area [2]int
	w    int // the widest the box has been: it does not narrow while open
	rows int // a picker's list rows: it does not shrink while open either
	// hits and groups are refilter's, kept for the next key.
	hits      []match
	groups    []matchGroup
	gen       int                     // counts the lists refilter and focus made: what drawn hangs on with the rest of boxKey
	drawn     drawnBox                // a list box as last drawn
	rowsDrawn memo[boxRowKey, string] // its item rows, this draw's and the last's: a move redraws two
	caret     [2]string               // the input line as drawn and with the terminal's caret
	roles     map[roleKey]*roles      // the escapes its rows' styles are drawn with
	icons     map[iconKey]part        // and its files' icons
	tabsDrawn struct {                // the tabs as drawn
		tab, look int
		s         string
	}
	// itemsW is the widest of wOf's rows: items, measured once per slice
	// (a rebuild makes a new one), not on every frame.
	wOf    []item
	itemsW int
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

// dimmer is the frame a centered modal shows under it, one piece of a row
// at a time: halfway to the terminal's background, every color, a palette
// one at its usual shade, and the terminal's own text color. Where the
// terminal did not tell its colors (tmux without a client) black or white
// and a grey stand in. A piece is dimmed once while it stays as it was, kept
// until the next dimmed frame: a session's rows under quick open move a row
// for each of its screens, the panels beside them stay.
func (m *Model) dimmer() func(string) string {
	m.dims.frame()

	fg, bg := lipgloss.Color(ifElse(m.dark, "#cccccc", "#333333")), lipgloss.Color(ifElse(m.dark, "#000000", "#ffffff"))
	if m.fg != "" {
		fg = lipgloss.Color(m.fg)
	}

	if m.bg != "" {
		bg = lipgloss.Color(m.bg)
	}

	// The colors halfway, kept while the terminal's stay and they are few: a
	// session can paint in thousands.
	if m.halves.to != [2]string{m.fg, m.bg} || m.halves.dark != m.dark || len(m.halves.of) > 4096 {
		m.halves.to, m.halves.dark, m.halves.of = [2]string{m.fg, m.bg}, m.dark, map[color.Color]string{}
	}

	half := m.halves.of
	blend := func(c color.Color) string {
		h, ok := half[c]
		if !ok {
			r, g, b, _ := lipgloss.Blend1D(3, c, bg)[1].RGBA() // 16 bits a channel, as color.RGBAModel reads them
			h = strconv.Itoa(int(r>>8)) + ";" + strconv.Itoa(int(g>>8)) + ";" + strconv.Itoa(int(b>>8))
			half[c] = h
		}

		return h
	}

	text := blend(fg)

	return func(s string) string {
		if s == "" {
			return s
		}

		return m.dims.get([3]string{s, m.fg, m.bg}, func() string { return dimLine(s, text, blend) })
	}
}

// dimLine is ln with each color through blend, which gives a color's "r;g;b",
// and the terminal's own text color as text: every SGR is rewritten where it
// stands, the text and every other escape copied.
func dimLine(ln, text string, blend func(color.Color) string) string {
	var b strings.Builder

	b.Grow(len(ln) + len(ln)/2)              // colors grow into "38;2;r;g;b"
	b.WriteString("\x1b[38;2;" + text + "m") // a row starts in the default colors

	for i := 0; i < len(ln); {
		j := strings.IndexByte(ln[i:], ansi.ESC)
		if j < 0 {
			b.WriteString(ln[i:])
			break
		}

		b.WriteString(ln[i : i+j])
		i += j

		n := escLen(ln[i:])
		seq := ln[i : i+n]
		i += n

		if n < 3 || seq[1] != '[' || seq[n-1] != 'm' || strings.ContainsAny(seq[2:3], "<=>?") {
			b.WriteString(seq)
			continue
		}

		dimSGR(&b, seq[2:n-1], text, blend)
	}

	return b.String()
}

// escLen is how long the escape s starts with is: a CSI to its final byte,
// an OSC, DCS, APC, PM or SOS to its terminator, any other two bytes.
func escLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}

	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			switch {
			case s[i] >= 0x40 && s[i] <= 0x7e:
				return i + 1
			case s[i] < 0x20 || s[i] > 0x7e: // no CSI: what is left is not one
				return len(s)
			}
		}

	case ']', 'P', '_', '^', 'X':
		for i := 2; i < len(s); i++ {
			switch {
			case s[i] == ansi.BEL && s[1] == ']':
				return i + 1
			case s[i] == ansi.ESC && i+1 < len(s) && s[i+1] == '\\':
				return i + 2
			}
		}

	default:
		return 2
	}

	return len(s)
}

// dimSGR writes the SGR whose parameters are params with its colors through
// blend, and text for the default text color wherever it resets to it. It
// reads params in place: a row has an SGR every few cells.
func dimSGR(b *strings.Builder, params, text string, blend func(color.Color) string) {
	b.WriteString("\x1b[")

	first, reset := true, false // reset: the default text color is the last one set
	put := func(a, z string) {  // a parameter, in two parts: no slice for each
		if !first {
			b.WriteByte(';')
		}

		first = false

		b.WriteString(a)
		b.WriteString(z)
	}

	for i := 0; i <= len(params); {
		p, next := sgrParam(params, i)
		code, sub, colon := strings.Cut(p, ":")
		n := sgrNum(code)

		switch {
		case n == 0:
			put("0", "")

			reset = true

		case n == 39:
			put("38;2;", text)

			reset = false

		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			put("38;2;", blend(ansi.BasicColor(ifElse(n >= 90, n-90+8, n-30))))

			reset = false

		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			put("48;2;", blend(ansi.BasicColor(ifElse(n >= 100, n-100+8, n-40))))

		case n == 38 || n == 48 || n == 58:
			var (
				ps [5]string
				k  int
			)

			to := next // past the color's parameters

			if colon { // 38:5:n, 38:2:r:g:b or 38:2::r:g:b
				for f := range strings.SplitSeq(sub, ":") {
					if k < len(ps) {
						ps[k] = f
					}

					k++
				}
			} else {
				for j := next; k < 4 && j <= len(params); k++ {
					ps[k], j = sgrParam(params, j)
				}
			}

			c, used := sgrColor(ps[:min(k, len(ps))])
			if !colon && c != nil {
				for range used {
					_, to = sgrParam(params, to)
				}
			}

			switch {
			case c == nil || n == 58: // the underline's color is not dimmed
				put(params[i:min(to-1, len(params))], "")
			case n == 38:
				put("38;2;", blend(c))

				reset = false

			default:
				put("48;2;", blend(c))
			}

			next = to

		default:
			put(p, "")
		}

		i = next
	}

	if reset {
		put("38;2;", text)
	}

	b.WriteByte('m')
}

// sgrParam is the parameter of an SGR's params that starts at i, and where
// the one after it starts: past the end for the last.
func sgrParam(params string, i int) (p string, next int) {
	if j := strings.IndexByte(params[i:], ';'); j >= 0 {
		return params[i : i+j], i + j + 1
	}

	return params[i:], len(params) + 1
}

// sgrNum is an SGR parameter's number, 0 for none, -1 for one that is not.
func sgrNum(s string) int {
	if s == "" {
		return 0
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}

	return n
}

// sgrColor is the color an SGR's 38 or 48 sets with the parameters after it,
// and how many of them it takes: 5;n or 2;r;g;b, a colon form's 2 with an
// empty color space first. Nil for any other.
func sgrColor(ps []string) (color.Color, int) {
	num := func(s string) (uint8, bool) {
		v, err := strconv.Atoi(s)
		return uint8(v), err == nil && v >= 0 //nolint:gosec // over 255 wraps, as Ultraviolet reads it
	}

	switch {
	case len(ps) >= 2 && ps[0] == "5":
		if v, ok := num(ps[1]); ok {
			return ansi.IndexedColor(v), 2
		}

	case len(ps) >= 5 && ps[0] == "2" && ps[1] == "": // 2::r:g:b
		ps = ps[1:]
		fallthrough

	case len(ps) >= 4 && ps[0] == "2":
		r, ok1 := num(ps[1])
		g, ok2 := num(ps[2])
		bl, ok3 := num(ps[3])

		if ok1 && ok2 && ok3 {
			return color.RGBA{r, g, bl, 0xff}, 4
		}
	}

	return nil, 0
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
	// A completion's rest in the placeholder's color: bubbles' ANSI 240 passes for typed text on a light theme.
	st.Focused.Suggestion, st.Blurred.Suggestion = dim, dim
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

	// The hits and the groups they fall in are kept for the next key: quick
	// open matches every file of a worktree on each.
	hits, groups := md.hits[:0], md.groups[:0]
	gi := -1 // the group of the last hit: items come a group at a time

	for i := range md.items {
		it := &md.items[i]
		if it.always {
			continue
		}

		sc := 0
		if q != "" {
			if sc = matchItem(q, it); sc < 0 {
				continue
			}
		}

		if gi < 0 || groups[gi].name != it.group {
			gi = slices.IndexFunc(groups, func(g matchGroup) bool { return g.name == it.group })
			if gi < 0 {
				gi = len(groups)
				groups = append(groups, matchGroup{name: it.group, best: -1, cap: md.caps[it.group]})
			}
		}

		g := &groups[gi]

		g.count++
		if sc > g.best {
			g.best, g.bestAt = sc, i
		}

		hits = append(hits, match{i: i, score: sc, g: gi})
	}

	md.hits, md.groups = hits, groups

	byScore := func(a, b match) int { // the best first, a tie as the items list them
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.i, b.i))
	}

	switch {
	case q != "" && (md.prefix != "" || md.tree):
		slices.SortFunc(hits, byScore)
	case q != "":
		// A query ranks within each group and orders the groups by their best
		// match, or as the items list them where groups are capped.
		order := make([]int, len(groups))
		for k := range order {
			order[k] = k
		}

		if md.caps == nil {
			slices.SortFunc(order, func(a, b int) int {
				return cmp.Or(cmp.Compare(groups[b].best, groups[a].best), cmp.Compare(groups[a].bestAt, groups[b].bestAt))
			})
		}

		for r, k := range order {
			groups[k].rank = r
		}

		slices.SortFunc(hits, func(a, b match) int { return cmp.Or(cmp.Compare(groups[a.g].rank, groups[b.g].rank), byScore(a, b)) })

	case md.tree: // browsing: alphabetical, like a file tree; a group first, in its own order
		slices.SortStableFunc(hits, func(a, b match) int {
			ia, ib := &md.items[a.i], &md.items[b.i]
			switch {
			case ia.group != ib.group:
				return cmp.Compare(ib.group, ia.group) // "recently opened" before "files"
			case ia.group == recentGroup:
				return 0
			}

			return compareFold(ia.label, ib.label)
		})
	}

	var always []item

	for i := range md.items {
		if md.items[i].always {
			always = append(always, md.items[i])
		}
	}

	// A query's matches come first: what ⏎ runs is the best of them.
	last := q != "" && len(hits) > 0

	rows := len(always) + 2*len(groups) + 2 // what the list takes: the rows shown of each group, its heading and gap
	for _, g := range groups {
		rows += ifElse(q != "" && g.cap > 0, min(g.count, g.cap+1), g.count)
	}

	md.disp = slices.Grow(md.disp[:0], rows)
	if !last {
		md.disp = append(md.disp, always...)
	}

	switch {
	case md.prefix != "":
		if grouped {
			slices.SortStableFunc(hits, func(a, b match) int { return strings.Compare(md.items[a.i].group, md.items[b.i].group) })
		}

		for i, h := range hits {
			it := md.items[h.i]
			switch {
			case grouped && (i == 0 || md.items[hits[i-1].i].group != it.group):
				md.disp = append(md.disp, heading(fmt.Sprintf("%s (%d)", it.group, groups[h.g].count)))
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

			groups[dir] = append(groups[dir], item{label: "  " + filepath.Base(it.label), hint: it.hint, run: it.run, file: it.label})
		}

		if len(md.disp) > 0 && len(order) > 0 { // after the recent files
			md.disp = append(md.disp, item{}, item{label: "files", sep: true})
		}

		for _, dir := range order {
			md.disp = append(md.disp, item{label: dir + "/"})
			md.disp = append(md.disp, groups[dir]...)
		}

	default:
		group, shown := -1, 0

		for _, h := range hits {
			g := &groups[h.g]
			if h.g != group {
				shown, group = 0, h.g

				if g.name != "" {
					if len(md.disp) > 0 {
						md.disp = append(md.disp, item{}) // breathing room between groups
					}

					md.disp = append(md.disp, item{label: g.name, sep: true})
				}
			}

			if q != "" && g.cap > 0 && shown >= g.cap {
				if shown == g.cap {
					md.disp = append(md.disp, md.more(g.name, g.count-g.cap))
				}

				shown++

				continue
			}

			shown++

			md.disp = append(md.disp, md.items[h.i])
		}
	}

	if last && len(always) > 0 {
		md.disp = append(md.disp, item{})
		md.disp = append(md.disp, always...)
	}

	if q != "" && len(md.disp) == 0 {
		md.disp = append(md.disp, item{label: "No matching results", sep: true})
	}

	md.gen++
	md.l = list{sel: -1}
	md.step(1, 0)

	md.l.top = 0 // step knows no height here and scrolled past a heading over the selection
	if md.l.sel < 0 && len(md.disp) > 0 {
		md.l.sel = 0 // informational menus still scroll from the top
	}

	if i := slices.IndexFunc(md.disp, func(it item) bool { return it.lead }); i >= 0 && !last { // a match ranked above it stays the one ⏎ runs
		md.focus(i)
		md.reveal = true
	}
}

// match is an item a query matched: its index, its score, its matchGroup.
type match struct{ i, score, g int }

// matchGroup is a group the matches fall in: how many, the best score and the
// first item with it, where the group goes, and its cap under a query.
type matchGroup struct {
	name                           string
	count, best, bestAt, rank, cap int
}

// compareFold is strings.Compare of a and b lowered, without lowering a
// copy of either where both are ASCII.
func compareFold(a, b string) int {
	if !isASCII(a) || !isASCII(b) {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}

	for i := range min(len(a), len(b)) {
		if c := cmp.Compare(lowerASCII(a[i]), lowerASCII(b[i])); c != 0 {
			return c
		}
	}

	return cmp.Compare(len(a), len(b))
}

// more is a capped group's last row: how many rows it leaves out, and ⏎
// shows them all in the tab named like the group, the query kept.
func (md *modal) more(group string, n int) item {
	it := item{label: fmt.Sprintf("+ %d more", n)}

	if i := slices.IndexFunc(md.tabs, func(t string) bool { return strings.EqualFold(t, group) }); i >= 0 {
		it.hint = md.tabs[i] + " ⇥"
		it.run = func(m *Model) tea.Cmd {
			m.modal = md // choosing closed it

			return md.setTab(m, i)
		}
	}

	return it
}

// focus selects row i and moves the detail row under it: only the selected
// item shows its detail, so a list of refs or remotes stays one row each.
func (md *modal) focus(i int) {
	md.gen++

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

// jump moves to the first or last actionable row, or a page of rows up or
// down: the furthest actionable one at most a page away. A list with none
// to land on scrolls instead.
func (md *modal) jump(s string, rows int) {
	from, top, d := md.l.sel, md.l.top, ifElse(s == "home" || s == "pgup", -1, 1)
	page := ifElse(s == "pgup" || s == "pgdown", max(rows-1, 1), len(md.disp))

	target := min(max(from+d*page, 0), len(md.disp)-1) + d
	md.l.sel = target
	md.step(-d, rows)

	if md.l.sel == target { // nothing to land on
		md.l.sel, md.l.top = from, top
		md.l.wheel(d*page, len(md.disp), rows)

		return
	}

	// On the first or last actionable row the headings and notes past it come into view.
	runs := func(it item) bool { return it.run != nil }
	switch i := md.l.sel; {
	case d < 0 && !slices.ContainsFunc(md.disp[:i], runs):
		md.l.top = min(md.l.top, max(0, i-rows+1))
	case d > 0 && !slices.ContainsFunc(md.disp[i+1:], runs):
		md.l.top = max(md.l.top, min(i, len(md.disp)-rows))
	}
}

func (md *modal) hasInput() bool { return md.filter || md.submit != nil }

// dims reports whether the frame under the box goes dim: a box in the middle
// of the screen, not one at the thing it acts on or atop an area's body, nor
// one that previews in that frame (Go to Line, a theme).
func (md *modal) dims() bool {
	return md.x < 0 && md.form == nil && (md.dialog || md.area[1] == 0) && md.still()
}

// still reports whether a key leaves the frame under the box as it was: no
// move or change hook (a theme's preview, Go to Line's) and no item run with
// the box kept open. Quick open's change hook only ever swaps it for
// another box.
func (md *modal) still() bool {
	return md.move == nil && (md.change == nil || md.tabs != nil) && !md.keep
}

// rect returns the box position and size, and how many item rows fit.
func (md *modal) rect(m *Model) (x, y, w, h, rows int) {
	x0, x1 := 0, m.w
	if md.area[1] > 0 {
		x0, x1 = md.area[0], md.area[1]
	}
	// A box kept to an area covers it whole rather than leave a cell of what
	// is under it showing at its sides.
	cover := func(w int) int {
		if md.area[1] > 0 && x1-x0-w < 4 {
			return x1 - x0
		}

		return w
	}

	if md.form != nil { // at the top of the area's body, as tall as it is: no widget above is cut
		fw, fh, _ := md.form.size()
		w, h = cover(min(fw, x1-x0-2)), fh

		return x0 + (x1-x0-w)/2, m.mainY() + m.headH() + m.stripH(), w, h, 0
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

		w = cover(min(max(min(w+4, 64), row+4), x1-x0-2)) // a cell's gap each side of the buttons
		msg, _, hintH := md.dialogBody(w - 2)
		h = len(msg) + hintH + 4
		// ponytail: taller than the terminal clips the bottom, buttons and all; scroll the message if one ever does.
		if md.x < 0 {
			return max(x0+(x1-x0-w)/2, x0), max((m.h-h)/2, 0), w, h, 0
		}

		y = md.y // anchored as a menu: under its line, else above it
		if y+h > m.h {
			y = md.y - 1 - h
		}

		return max(min(md.x, x1-w), x0), max(y, 0), w, h, 0
	}

	switch {
	case md.tabs != nil: // one width whatever a tab lists, a line of text included
		w = min(max(m.w*2/3, 60), 120)
	case len(md.items) == 0 || len(md.wOf) != len(md.items) || &md.wOf[0] != &md.items[0]:
		md.wOf, md.itemsW = md.items, 0
		for _, it := range md.items {
			md.itemsW = max(md.itemsW, ansi.StringWidth(it.label)+ansi.StringWidth(it.inline)+ansi.StringWidth(it.hint)+6+b2i(it.hint != "")) // two blanks before a hint
		}
	}

	w = max(w, ansi.StringWidth(md.title)+6, md.itemsW)

	if md.hasInput() {
		w = max(w, 60)
		if md.x >= 0 { // a box opened at a place in the text stays out of the way
			w = max(min(w, 30), ansi.StringWidth(md.input.Value())+8)
		}
	}

	md.w = max(md.w, w)
	w = cover(min(md.w, x1-x0-2))
	maxRows := max(m.h-6, 1)

	rows = min(len(md.disp), maxRows)
	if md.filter { // at least 8 rows, at most half the screen; it does not shrink while typing, so it stays put
		half := min(max(m.h/2-3, 8), maxRows)
		md.rows = min(max(md.rows, rows, 8, ifElse(md.tabs != nil, half, 0)), half)
		rows = md.rows
	}

	if md.submit != nil {
		rows = 0
	}

	h = rows + 2
	if md.hasInput() {
		h++
	}

	x = min(md.x, x1-w)

	switch {
	case md.x < 0 && md.area[1] > 0 && !md.dialog: // as the form: lower, it would cut the grid's boxes
		x, y = x0+(x1-x0-w)/2, m.mainY()+m.headH()+m.stripH()

	case md.x < 0:
		x, y = x0+(x1-x0-w)/2, max((m.h-h)/2, 0)

	case md.y+h <= m.h:
		y = md.y
	case !md.hasInput() && md.y-1-h >= 0: // a menu opens above the line it was opened under, as the suggestions do
		y = md.y - 1 - h
	default: // a text box stays as near its text as it fits
		y = m.h - h
	}

	return max(x, x0), max(y, 0), w, h, rows
}

// toggle is the view switch at the right end of the title row: ^t and the
// views the tab offers, the one shown lit.
func (md *modal) toggle() (segs []seg, width int) {
	if !md.views {
		return nil, 0
	}

	segs = []seg{sg(" ^t ", dim)}

	for i, v := range quickViews {
		switch {
		case i == qvTree && !md.treeable:
			continue
		case i > 0:
			segs = append(segs, sg("·", dim))
		}

		segs = append(segs, sg(v, ifElse(i == md.shownView(), fg(pal.headerAccent), dim)))
	}

	segs = append(segs, sg(" ", dim))
	for _, s := range segs {
		width += ansi.StringWidth(s.s)
	}

	return segs, width
}

// shownView is the view the rows are in: the tree where the tab has none is
// the name view.
func (md *modal) shownView() int {
	if md.fileView == qvTree && !md.treeable {
		return qvName
	}

	return md.fileView
}

// cycleView is ^t: the next view the tab offers, kept in the settings.
func (md *modal) cycleView(m *Model) tea.Cmd {
	md.fileView = (md.shownView() + 1) % len(quickViews)
	if md.fileView == qvTree && !md.treeable {
		md.fileView = 0
	}

	md.tree = md.fileView == qvTree
	md.refilter()

	return m.setSettings(map[string]any{"quick_open_view": quickViews[md.fileView]})
}

// label is a row's label, marked where the query matched it; a file's has
// its icon after any indent, and in the name view its name before its folder.
func (md *modal) label(dst []seg, it *item, st, note lipgloss.Style) []seg {
	if it.words { // a line found: its number dim, the query's words lit in the text
		n := int(it.dimTo)
		dst = append(dst, sg(it.label[:n], note))

		return appendRuns(dst, it.label[n:], wordRuns(md.input.Value(), it.label[n:]), st)
	}

	var hits []int
	if !it.always { // a row listed whatever the query matched nothing
		hits = fuzzyHits(md.pattern, it.label)
	}

	if it.file == "" {
		return appendMarked(dst, it.label, hits, st)
	}

	icon := iconSeg(it.file, false, false)

	slash := strings.LastIndexByte(it.label, '/')
	if md.shownView() != qvName || slash < 0 {
		in := len(it.label) - len(strings.TrimLeft(it.label, " ")) // a tree's file sits under its folder

		var after []int

		for _, h := range hits {
			if h >= in {
				after = append(after, h-in)
			}
		}

		return appendMarked(append(dst, sg(it.label[:in], st), icon), it.label[in:], after, st)
	}

	var inDir, inName []int

	for _, h := range hits {
		if h < slash {
			inDir = append(inDir, h)
		} else if h > slash {
			inName = append(inName, h-slash-1)
		}
	}

	dst = appendMarked(append(dst, icon), it.label[slash+1:], inName, st)

	return appendMarked(append(dst, sg("  ", note)), it.label[:slash], inDir, note)
}

// boxKey is what a list box's rows hang on.
type boxKey struct {
	gen, sel, top, pos, x, y, w, rows, tab, view, look int
	in, title, note, hints                             string
	selected, views, treeable                          bool
}

// drawnBox is a box's rows as drawn, its caret taken out and where it was.
type drawnBox struct {
	key    boxKey
	lines  []string
	cx, cy int
	caret  bool
}

// box is the box drawn at x, y, w cells wide: a list box once while nothing
// it shows changes, as under a session that streams while quick open waits
// for a key.
func (md *modal) box(m *Model) (d *drawnBox, x, y, w int) {
	keyOf := func() boxKey {
		x, y, w, _, rows := md.rect(m)

		return boxKey{
			md.gen, md.l.sel, md.l.top, md.input.Position(), x, y, w, rows, md.tab, md.fileView, lookGen,
			md.input.Value(), md.title, md.note, md.hints, md.selected, md.views, md.treeable,
		}
	}

	list := md.form == nil && !md.dialog && !md.reveal
	if list && md.drawn.lines != nil && md.drawn.key == keyOf() {
		return &md.drawn, md.drawn.key.x, md.drawn.key.y, md.drawn.key.w
	}

	lines, x, y := md.lines(m)
	cx, cy, caret := takeCaret(lines)

	if !m.graphemes { // made safe once while it stays, as the frame under it is
		for i, l := range lines {
			lines[i] = wcSafe(l)
		}
	}

	md.drawn = drawnBox{keyOf(), lines, cx, cy, caret}
	if !list { // a form or a dialog is drawn anew
		md.drawn.key.gen = -1
	}

	return &md.drawn, x, y, md.drawn.key.w
}

// lines are the box's rows at x, y.
func (md *modal) lines(m *Model) ([]string, int, int) {
	x, y, w, _, rows := md.rect(m)
	iw := w - 2

	if md.reveal {
		md.l.snap(rows)
		md.reveal = false
	}

	if md.form != nil {
		return md.form.lines(w), x, y
	}

	if md.dialog {
		return strings.Split(md.dialogView(iw), "\n"), x, y
	}

	title := ""
	if md.title != "" {
		title = bold.Render(" " + md.title + " ")
	}

	if md.tabs != nil {
		if md.tabsDrawn.tab != md.tab || md.tabsDrawn.look != lookGen || md.tabsDrawn.s == "" {
			var b strings.Builder

			segs, _ := choices(md.tabs, md.tab, true)
			for _, s := range segs {
				b.WriteString(s.st.Render(s.s))
			}

			md.tabsDrawn.tab, md.tabsDrawn.look, md.tabsDrawn.s = md.tab, lookGen, " "+b.String()
		}

		title = md.tabsDrawn.s
	}

	var rb strings.Builder

	segs, tw := md.toggle()
	for _, s := range segs {
		rb.WriteString(s.st.Render(s.s))
	}

	right := rb.String()
	// Before the view switch: Text's count, left out where it would cut the tabs.
	if nw := ansi.StringWidth(md.note) + 2; md.note != "" && (md.tabs == nil || ansi.StringWidth(title)+tw+nw <= iw) {
		right, tw = dim.Render(" "+md.note+" ")+right, tw+nw
	}

	title = ansi.Truncate(title, max(iw-tw, 0), "…")

	// A menu at the pointer has square corners, as an editor's context menu;
	// a centered picker keeps the round ones.
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	if md.x >= 0 && !md.hasInput() {
		tl, tr, bl, br = "┌", "┐", "└", "┘"
	}

	top := dim.Render(tl) + title + dim.Render(strings.Repeat("─", max(iw-tw-ansi.StringWidth(title), 0))) + right
	lines := []string{top + dim.Render(tr)}
	side := dim.Render("│")

	if md.hasInput() {
		md.input.SetWidth(iw - 3)

		in := md.input
		if md.selected { // as the editor's selection
			st := in.Styles()
			st.Focused.Text = st.Focused.Text.Background(pal.textSelBg)
			in.SetStyles(st)
		}

		if v := in.View(); md.caret[0] != v { // the caret found once while the line stays
			md.caret = [2]string{v, realCaret(v)}
		}

		lines = append(lines, side+fit(md.caret[1], iw)+side)
	}

	md.rowsDrawn.frame()

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
			if it.hint != "" {
				return row(rw, nil, []seg{sg(" "+it.label, dim)}, sg(it.hint+" ", dim))
			}

			if it.label != "" {
				return fit(" "+dim.Render(it.label), rw)
			}

			return dim.Render(strings.Repeat("─", rw))
		}

		if it.styled {
			return fit(" "+it.label, rw)
		}

		if it.segs != nil {
			var bg color.Color
			if i == md.l.sel && it.run != nil {
				bg = pal.selBg
			}

			return row(rw, bg, append([]seg{sg(" ", plain)}, it.segs...), append(append([]seg{sg(" ", plain)}, it.right...), sg(" ", plain))...)
		}

		k := boxRowKey{it.label, it.hint, it.inline, it.file, ifElse(it.words, md.input.Value(), md.pattern), rw, md.shownView(), lookGen, i == md.l.sel, it.run != nil, it.always, it.words}

		return md.rowsDrawn.get(k, func() string { return md.itemRow(&it, k.sel, rw) })
	}) {
		lines = append(lines, side+r+side)
	}

	edge := strings.Repeat("─", iw)
	if md.hints != "" && iw > 6 { // the keys in the bottom edge, as the preview's footer drops them
		h := ansi.Truncate(fitHints(md.hints, iw-4), iw-4, "…")
		edge = "─ " + h + " " + strings.Repeat("─", iw-3-ansi.StringWidth(h))
	}

	lines = append(lines, dim.Render(bl+edge+br))

	return lines, x, y
}

// boxRowKey is what an item's row in a box hangs on.
type boxRowKey struct {
	label, hint, inline, file, pattern string
	rw, view, look                     int
	sel, runs, always, words           bool
}

// itemRow is an item's row in the box, rw cells wide: its label marked where
// the query matched it, its hint at the right, lit when sel. Its pieces take
// their styles' escapes from roleSides, worked out once for the box, not for
// each piece: a box draws a few dozen rows for each key.
func (md *modal) itemRow(it *item, sel bool, rw int) string {
	r := md.roleSides(sel && it.run != nil, it.run != nil)
	if !r.ok || strings.ContainsAny(it.label+it.hint+it.inline, "\t\n") { // a piece row draws through Render
		return md.itemRowSegs(it, sel, rw)
	}

	left := md.labelParts(append(make([]part, 0, 8), part{" ", r.st}), it, r, sel && it.run != nil)
	if left == nil {
		return md.itemRowSegs(it, sel, rw)
	}

	if it.inline != "" {
		left = append(left, part{" " + it.inline, r.note})
	}

	hint := it.hint + " "
	if it.hint != "" {
		hint = " " + hint // a label cut short keeps a cell from it
	}

	return rowParts(rw, left, r.gap, part{hint, r.note})
}

// part is a piece of a row and its style's escapes either side of it.
type part struct {
	s     string
	sides [2]string
}

// roles are the escapes of the styles a box's rows are drawn in, on a row
// lit or not: its text, the text lit where the query matched, the notes and
// their lit matches, the blanks. ok is false where one has no plain sides.
type roles struct {
	st, hi, note, noteHi, gap [2]string
	lit                       bool
	ok                        bool
}

// roleSides are the roles for a row lit or not, and of an item that runs or
// is dimmed, worked out once while the palette stays.
func (md *modal) roleSides(lit, runs bool) *roles {
	k := roleKey{lit, runs, lookGen}
	if r, ok := md.roles[k]; ok {
		return r
	}

	st, note := plain, dim
	if !runs {
		st = dim
	}

	var bg color.Color
	if lit {
		bg = pal.selBg
		if pal.selFg != nil {
			st = st.Foreground(pal.selFg)
		}
	}

	r := &roles{lit: lit, ok: true}
	for _, x := range []struct {
		to *[2]string
		st lipgloss.Style
	}{
		{&r.st, st},
		{&r.hi, st.Foreground(pal.headerAccent).Bold(true)},
		{&r.note, note},
		{&r.noteHi, note.Foreground(pal.headerAccent).Bold(true)},
		{&r.gap, plain},
	} {
		if bg != nil {
			x.st = x.st.Background(bg)
		}

		pre, suf, ok := textSides(x.st, "x")
		*x.to, r.ok = [2]string{pre, suf}, r.ok && ok
	}

	if md.roles == nil {
		md.roles = map[roleKey]*roles{}
	}

	md.roles[k] = r

	return r
}

// roleKey is what a box's roles hang on.
type roleKey struct {
	lit, runs bool
	look      int
}

// labelParts is label in parts: nil where a piece has no plain sides, for
// itemRowSegs to draw the row instead.
func (md *modal) labelParts(dst []part, it *item, r *roles, lit bool) []part {
	if it.words { // a line found: its number dim, the query's words lit in the text
		n := int(it.dimTo)
		dst = append(dst, part{it.label[:n], r.note})

		return appendRunParts(dst, it.label[n:], wordRuns(md.input.Value(), it.label[n:]), r.st, r.hi)
	}

	var hits []int
	if !it.always { // a row listed whatever the query matched nothing
		hits = fuzzyHits(md.pattern, it.label)
	}

	if it.file == "" {
		return appendHitParts(dst, it.label, hits, r.st, r.hi)
	}

	icon, ok := md.iconPart(it.file, lit)
	if !ok {
		return nil
	}

	slash := strings.LastIndexByte(it.label, '/')
	if md.shownView() != qvName || slash < 0 {
		in := len(it.label) - len(strings.TrimLeft(it.label, " ")) // a tree's file sits under its folder

		var after []int

		for _, h := range hits {
			if h >= in {
				after = append(after, h-in)
			}
		}

		return appendHitParts(append(dst, part{it.label[:in], r.st}, icon), it.label[in:], after, r.st, r.hi)
	}

	var inDir, inName []int

	for _, h := range hits {
		if h < slash {
			inDir = append(inDir, h)
		} else if h > slash {
			inName = append(inName, h-slash-1)
		}
	}

	dst = appendHitParts(append(dst, icon), it.label[slash+1:], inName, r.st, r.hi)

	return appendHitParts(append(dst, part{"  ", r.note}), it.label[:slash], inDir, r.note, r.noteHi)
}

// iconPart is file's icon as a part, its sides worked out once for each kind
// of file while the palette stays.
func (md *modal) iconPart(file string, lit bool) (part, bool) {
	k := iconKey{kindOf(file, false, false), lit, lookGen}

	p, ok := md.icons[k]
	if !ok {
		x := iconSeg(file, false, false)
		if lit {
			x.st = x.st.Background(pal.selBg)
		}

		pre, suf, sides := textSides(x.st, "x")
		if !sides {
			return part{}, false
		}

		p = part{x.s, [2]string{pre, suf}}

		if md.icons == nil {
			md.icons = map[iconKey]part{}
		}

		md.icons[k] = p
	}

	return p, true
}

// iconKey is what an icon's part hangs on.
type iconKey struct {
	kind string
	lit  bool
	look int
}

// appendHitParts is appendMarked in parts: the bytes at, a run of them a
// part, in hi.
func appendHitParts(out []part, label string, at []int, st, hi [2]string) []part {
	last := 0

	for k := 0; k < len(at); {
		i, j := at[k], at[k]+1
		for k++; k < len(at) && at[k] == j; k++ {
			j++
		}

		if i > last {
			out = append(out, part{label[last:i], st})
		}

		out, last = append(out, part{label[i:j], hi}), j
	}

	return append(out, part{label[last:], st})
}

// appendRunParts is appendRuns in parts: the byte ranges runs in hi.
func appendRunParts(out []part, label string, runs [][2]int, st, hi [2]string) []part {
	last := 0

	for _, r := range runs {
		if r[0] > last {
			out = append(out, part{label[last:r[0]], st})
		}

		out, last = append(out, part{label[r[0]:r[1]], hi}), r[1]
	}

	return append(out, part{label[last:], st})
}

// rowParts is row for parts: the same bytes, the styles' escapes taken as
// they are.
func rowParts(w int, left []part, gap [2]string, right part) string {
	rw := strWidth(right.s)
	if rw > w {
		right, rw = part{}, 0
	}

	var b strings.Builder

	paint := func(x part) {
		b.WriteString(x.sides[0])
		b.WriteString(x.s)
		b.WriteString(x.sides[1])
	}

	used := 0
	for i, x := range left {
		rem := w - rw - used
		if rem <= 0 {
			break
		}

		if sw := strWidth(x.s); sw > rem || sw == rem && slices.ContainsFunc(left[i+1:], func(p part) bool { return strings.TrimSpace(p.s) != "" }) {
			x.s = ansi.Truncate(x.s, rem-1, "") + "…"
		}

		paint(x)
		used += strWidth(x.s)
	}

	if g := w - rw - used; g > 0 {
		paint(part{strings.Repeat(" ", g), gap})
	}

	if right.s != "" {
		paint(right)
	}

	return b.String()
}

// itemRowSegs is itemRow through row, a style looked at for each piece: what
// draws a row whose style has no plain sides.
func (md *modal) itemRowSegs(it *item, sel bool, rw int) string {
	st, note := plain, dim
	if it.run == nil {
		st = dim
	}

	var bg color.Color
	if sel && it.run != nil {
		bg, note = pal.selBg, dim // the grey can be the selection's own color (the terminal theme's 8)
		if pal.selFg != nil {
			st = st.Foreground(pal.selFg)
		}
	}

	left := md.label(append(make([]seg, 0, 8), sg(" ", st)), it, st, note) // room for the usual row: no slice grown
	if it.inline != "" {
		left = append(left, sg(" "+it.inline, note))
	}

	hint := it.hint + " "
	if it.hint != "" {
		hint = " " + hint // a label cut short keeps a cell from it
	}

	return row(rw, bg, left, sg(hint, note))
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
			if s == "tab" && len(md.input.AvailableSuggestions()) == 0 { // none shown until typed or asked for
				md.input.SetSuggestions(md.complete(md.input.Value()))
			}

			if sug := md.input.CurrentSuggestion(); sug != "" && sug != md.input.Value() {
				md.input.SetValue(sug)
				md.input.CursorEnd()
				md.input.SetSuggestions(nil) // the next waits for typing or tab too

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

	switch s := k.String(); {
	case md.tabs != nil && (s == "tab" || s == "shift+tab"):
		return md.setTab(m, (md.tab+ifElse(s == "tab", 1, len(md.tabs)-1))%len(md.tabs))
	case md.alt != nil && s == "alt+enter":
		return md.alt(m)
	case md.keys[s] != nil:
		return md.keys[s](m)
	case md.submit == nil && (s == "home" || s == "end" || s == "pgup" || s == "pgdown"):
		md.jump(s, rows)
		md.moved(m)

		return nil
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
		if md.views {
			return md.cycleView(m)
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
		switch s := k.String(); {
		case s == "k":
			md.step(-1, rows)
		case s == "j":
			md.step(1, rows)
		case s == "q":
			m.modal = nil
		case (s == "left" || s == "h" || s == "backspace") && md.back != nil:
			return md.back(m)
		case (s == "right" || s == "l") && md.l.sel >= 0 && md.l.sel < len(md.disp) && md.disp[md.l.sel].hint == "›": // into a submenu
			return md.choose(m, md.l.sel)
		}

		return nil
	}

	var cmd tea.Cmd

	before := md.input.Value()
	if md.selected {
		md.selected = false

		switch s := k.String(); {
		case k.Text != "":
			md.input.SetValue("")
		case s == "backspace" || s == "delete":
			md.input.SetValue("")
			return md.typed(m, before)
		}
	}

	md.input, cmd = md.input.Update(k)

	return tea.Batch(cmd, md.typed(m, before))
}

// typed follows the query a key or a paste changed from before: the
// suggestions, the picker's change, the filter.
func (md *modal) typed(m *Model, before string) tea.Cmd {
	v := md.input.Value()
	if v == before {
		return nil
	}

	if md.complete != nil {
		md.input.SetSuggestions(md.complete(v))
	}

	if !md.filter {
		return nil
	}

	if md.change != nil {
		if c := md.change(m, v); c != nil || m.modal != md {
			return c
		}
	}

	md.refilter()
	md.moved(m)

	return nil
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

	md.l.sel = i // what a run shared by many rows reads, quick open's files
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
			return md.cycleView(m)
		}

		if _, xs := choices(md.tabs, md.tab, true); mo.Y == y { // a tab in the title row, after "╭ "
			for i, t := range md.tabs {
				if at := x + 2 + xs[i]; mo.X >= at && mo.X < at+ansi.StringWidth(t)+2 {
					return md.setTab(m, i)
				}
			}
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

	// An ASCII s is lowered a byte at a time as it is read, not copied: quick
	// open matches every path of a worktree on every key.
	p, ls := strings.ToLower(pattern), s
	if !isASCII(s) {
		ls = strings.ToLower(s)
	}

	upper := p[0] // p[0] as a capital too: IndexByte looks for each a word at a time
	if 'a' <= upper && upper <= 'z' {
		upper -= 'a' - 'A'
	}

	index := func(from int) int { // the next byte from from that is p[0], any case
		i := strings.IndexByte(ls[from:], p[0])
		if upper != p[0] {
			if j := strings.IndexByte(ls[from:], upper); j >= 0 && (i < 0 || j < i) {
				i = j
			}
		}

		if i < 0 {
			return -1
		}

		return from + i
	}

	best, from = -1, -1
	// ponytail: greedy match from every occurrence of the first rune, O(n·m); fine for paths.
	for start := index(0); start >= 0; {
		score, pi, prev := 0, 0, -2
		for i := start; i < len(ls) && pi < len(p); i++ {
			if lowerASCII(ls[i]) != p[pi] {
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

		start = index(start + 1)
	}

	return best, from
}

// isASCII reports whether s is all ASCII, eight bytes at a time: quick open
// asks it of every path for each key.
func isASCII(s string) bool {
	i := 0
	for ; i+8 <= len(s); i += 8 { // the compiler loads these as one word
		w := uint64(s[i]) | uint64(s[i+1])<<8 | uint64(s[i+2])<<16 | uint64(s[i+3])<<24 |
			uint64(s[i+4])<<32 | uint64(s[i+5])<<40 | uint64(s[i+6])<<48 | uint64(s[i+7])<<56
		if w&0x8080808080808080 != 0 {
			return false
		}
	}

	for ; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}

// lowerASCII is c in lower case where it is an ASCII capital.
func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
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

// appendMarked is label in st after out's segments, with the bytes at, a
// fuzzy match, in the accent: VS Code's list.highlightForeground. A row grows
// one slice.
func appendMarked(out []seg, label string, at []int, st lipgloss.Style) []seg {
	hi := st.Foreground(pal.headerAccent).Bold(true)
	last := 0

	for k := 0; k < len(at); {
		i, j := at[k], at[k]+1 // a run of hits is one segment: each costs the row a style
		for k++; k < len(at) && at[k] == j; k++ {
			j++
		}

		if i > last {
			out = append(out, sg(label[last:i], st))
		}

		out, last = append(out, sg(label[i:j], hi)), j
	}

	return append(out, sg(label[last:], st))
}

func openExternal(path string) error {
	for _, opener := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(opener); err == nil {
			return startReaped(exec.Command(p, path))
		}
	}

	return exec.ErrNotFound
}

// startReaped starts cmd and waits for it in the background: a child no one
// waits for stays a zombie until pando exits, one per sound or link opened.
func startReaped(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() { _ = cmd.Wait() }() // its exit status is of no use

	return nil
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
