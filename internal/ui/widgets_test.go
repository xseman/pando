package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

// TestRowShowsCut ends a row cut short in …, whichever segment the cut falls
// on, and keeps a row that only loses padding whole.
func TestRowShowsCut(t *testing.T) {
	for _, c := range []struct {
		left []seg
		want string
	}{
		{[]seg{sg("fmt.Println(s.", plain), sg("Total", plain), sg("())", plain)}, "fmt.Println(s.Tota…"},
		{[]seg{sg("fmt.Println(s.Total", plain), sg("())", plain)}, "fmt.Println(s.Tota…"},
		{[]seg{sg("fmt.Println(s.Total", plain), sg("  ", plain)}, "fmt.Println(s.Total"},
	} {
		if got := ansi.Strip(row(19, nil, c.left)); got != c.want {
			t.Errorf("row = %q, want %q", got, c.want)
		}
	}
}

// TestMenuOpensAbove opens a menu anchored under a line near the bottom
// above that line, as the suggestions do, rather than over it.
func TestMenuOpensAbove(t *testing.T) {
	m, _ := editorModel(t, "a.go", strings.Repeat("x := 1\n", 60))
	m.pv.setCursor(m, pos{59, 0})

	x, y := m.lightbulb(0)
	m.onActions(actionsMsg{acts: []lsp.Action{{Title: "Extract function"}, {Title: "Inline variable"}, {Title: "Rename"}, {Title: "Add comment"}}})

	_, my, _, h, _ := m.modal.rect(m)
	if y+h <= m.h {
		t.Fatalf("the menu fits under the line at %d,%d: nothing to test", x, y)
	}

	if line := y - 1; my < 0 || my+h > line {
		t.Fatalf("menu rows [%d, %d), want them above the line at %d", my, my+h, line)
	}
}

// TestPickerKeepsWidth keeps Go to Line's box as wide as its hint row once a
// number replaces that row with a shorter one.
func TestPickerKeepsWidth(t *testing.T) {
	m, _ := editorModel(t, "a.go", strings.Repeat("total := 1 + 2 + 3\n", 29))
	m.pv.setCursor(m, pos{17, 16})
	press(m, "ctrl+g")

	_, _, w, _, _ := m.modal.rect(m)

	press(m, "1")

	if _, _, w1, _, _ := m.modal.rect(m); w1 != w || w <= 60 {
		t.Fatalf("width %d after typing, %d before", w1, w)
	}
}

// TestModalSuggestionColor draws a prompt's completion in the placeholder's
// color, not bubbles' grey, which passes for typed text on a light theme.
func TestModalSuggestionColor(t *testing.T) {
	in := modalInput()
	in.ShowSuggestions = true
	in.SetValue("docs/")
	in.CursorEnd()
	in.SetSuggestions([]string{"docs/notes.md"})

	if v := in.View(); !strings.Contains(v, fgParams(pal.descFg)) {
		t.Fatalf("suggestion not in the description color: %q", v)
	}
}

// TestFramedBoxFilled paints every cell inside the frame with the box's
// background: the unfocused caret, the cells after the text, the last one.
func TestFramedBoxFilled(t *testing.T) {
	for _, focused := range []bool{true, false} {
		in := modalInput()
		in.SetValue("status = 'new'")
		in.SetCursor(3)

		if !focused {
			in.Blur()
		}

		line := framedBox(&in, 30, focused, true)[1]
		line = strings.ReplaceAll(line, caretMark, "")

		buf := uv.NewScreenBuffer(30, 1)
		uv.NewStyledString(line).Draw(buf, buf.Bounds())

		for x := 2; x < 28; x++ { // between ▏ and ▕
			if c := buf.CellAt(x, 0); c == nil || c.Style.Bg == nil {
				t.Errorf("focused=%v: cell %d has no background in %q", focused, x, line)
			}
		}
	}
}

// TestModalCoversArea lets a box kept to a narrow editor area cover it whole
// rather than leave a cell of the screen under it at its sides.
func TestModalCoversArea(t *testing.T) {
	m := testModelSized(t, 120, 30)
	area := [2]int{40, 115}

	for _, md := range []*modal{
		{form: newConnForm(m, ""), x: -1, area: area},
		{title: "Pending SQL", items: []item{{label: strings.Repeat("x", 80)}}, x: -1, area: area},
	} {
		md.refilter()

		if x, _, w, _, _ := md.rect(m); x != area[0] || x+w != area[1] {
			t.Errorf("%q: box columns [%d, %d), want the area [%d, %d)", md.title, x, x+w, area[0], area[1])
		}
	}
}

// TestFitHints drops whole hints until a hint row fits, the closing one
// staying last, rather than cut one through.
func TestFitHints(t *testing.T) {
	h := " ↑↓ move  ⇧ select  m stage/revert lines  O file  s split  q close"
	if got := fitHints(h, 200); got != h {
		t.Fatalf("a row that fits changed: %q", got)
	}

	got := fitHints(h, 40)
	if ansi.StringWidth(got) > 40 || !strings.HasSuffix(got, "  q close") {
		t.Fatalf("cut: %q", got)
	}

	for part := range strings.SplitSeq(got, "  ") {
		if !strings.Contains(h, part+"  ") && !strings.HasSuffix(h, part) {
			t.Fatalf("a hint cut through: %q in %q", part, got)
		}
	}
}

// TestDialogButtonsGap leaves the same air between a dialog's border and its
// buttons on both sides when the buttons set its width.
func TestDialogButtonsGap(t *testing.T) {
	m := testModel(t)
	m.confirmQuit()

	for l := range strings.SplitSeq(ansi.Strip(checkWidths(t, m)), "\n") {
		first := strings.Index(l, "Save all and close")
		if first < 0 {
			continue
		}

		left, right, last := strings.Index(l, "│"), strings.LastIndex(l, "│"), strings.LastIndex(l, "Close")+len("Close")
		before := ansi.StringWidth(l[:first]) - ansi.StringWidth(l[:left]) - 1
		after := ansi.StringWidth(l[:right]) - ansi.StringWidth(l[:last])

		if before != after {
			t.Fatalf("%d cells before the buttons, %d after: %q", before, after, l)
		}

		return
	}

	t.Fatalf("no button row:\n%s", checkWidths(t, m))
}

// FuzzRenderText holds renderText to lipgloss: the text st.Render makes of
// s, byte for byte, for a style of any colors, attributes and layout.
func FuzzRenderText(f *testing.F) {
	for _, s := range []string{"", "x", "a b", "\t", "one\ntwo", "⚠️ wide 世界", "\x1b[1mbold\x1b[m", " "} {
		f.Add(uint32(0), uint8(0), uint8(0), s)
		f.Add(uint32(0b111111), uint8(3), uint8(5), s)
		f.Add(uint32(1<<12|1<<6), uint8(1), uint8(2), s)
		f.Add(uint32(1<<17|1<<18|1<<19|1<<20), uint8(1), uint8(1), s)
	}

	colors := []color.Color{nil, hex("#0069cc"), ansi16(3), lipgloss.Color("200"), lipgloss.NoColor{}}

	f.Fuzz(func(t *testing.T, flags uint32, fgc, bgc uint8, s string) {
		on := func(bit int) bool { return flags&(1<<bit) != 0 }
		st := lipgloss.NewStyle().Bold(on(0)).Italic(on(1)).Faint(on(2)).Reverse(on(3)).Blink(on(4)).Strikethrough(on(5)).
			Foreground(colors[int(fgc)%len(colors)]).Background(colors[int(bgc)%len(colors)])

		if on(6) {
			st = st.Underline(true)
		}

		if on(7) {
			st = st.UnderlineSpaces(on(8))
		}

		if on(9) {
			st = st.Width(int(fgc % 12))
		}

		if on(10) {
			st = st.Padding(0, int(bgc%3))
		}

		if on(11) {
			st = st.Margin(0, int(fgc%2))
		}

		if on(12) {
			st = st.Border(lipgloss.NormalBorder())
		}

		if on(13) {
			st = st.Inline(true)
		}

		if on(14) {
			st = st.MaxWidth(int(bgc % 6))
		}

		if on(15) {
			st = st.Hyperlink("https://x.y")
		}

		if on(16) {
			st = st.Transform(strings.ToUpper)
		}

		if on(17) {
			st = st.BorderStyle(lipgloss.RoundedBorder())
		}

		if on(18) {
			st = st.BorderTop(true)
		}

		if on(19) {
			st = st.PaddingLeft(int(fgc % 2)).MarginBottom(int(bgc % 2))
		}

		if on(20) {
			st = st.Height(int(bgc % 3))
		}

		if got, want := renderText(st, s), st.Render(s); got != want {
			t.Fatalf("flags %b on %q:\n got %q\nwant %q", flags, s, got, want)
		}
	})
}

// FuzzCutTail holds cutTail to ansi.TruncateLeft, byte for byte, on any
// bytes: escapes cut through, controls, broken UTF-8.
func FuzzCutTail(f *testing.F) {
	for _, s := range []string{"plain text", "\x1b[38;2;1;2;3mred\x1b[m and \x1b[1mbold", "日本語 wide", "a⚠️b 👍🏽 x\u0301", "┌──┐ ▏box", "\x1b]8;;u\x07link\x1b]8;;\x07 tail", "a\tb\x1b[", ""} {
		f.Add(s, 4)
		f.Add(s, 0)
	}

	f.Fuzz(func(t *testing.T, s string, n int) {
		n = max(min(n, 64), -1)
		if got, want := cutTail(s, n), ansi.TruncateLeft(s, n, ""); got != want {
			t.Fatalf("cutTail(%q, %d) = %q, TruncateLeft %q", s, n, got, want)
		}
	})
}

// FuzzHeadCells holds headCells to what a cut is: the text's head, at most n
// cells as the rows are measured (strWidth, ansi.StringWidth), and not a
// grapheme shorter than fits. The text is a row's: printable, with the
// well-formed colors a lexer puts between its words.
func FuzzHeadCells(f *testing.F) {
	for _, s := range []string{"plain text", "red and bold", "日本語 wide", "a⚠️b 👍🏽 x\u0301", "┌──┐ ▏box", ""} {
		f.Add(s, 4, uint8(0b1011))
		f.Add(s, 0, uint8(0))
	}

	f.Fuzz(func(t *testing.T, text string, n int, colors uint8) {
		if !utf8.ValidString(text) || strings.ContainsFunc(text, unicode.IsControl) {
			return
		}

		var b strings.Builder

		for i, r := range text {
			if colors>>(i%8)&1 != 0 {
				b.WriteString([]string{"\x1b[38;2;1;2;3m", "\x1b[m", "\x1b[1;4m"}[i%3])
			}

			b.WriteRune(r)
		}

		s := b.String()
		n = max(min(n, 64), 0)

		got := headCells(s, n)
		head, all := ansi.Strip(got), ansi.Strip(s)

		rest, ok := strings.CutPrefix(all, head)
		if next, w := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth); !ok || strWidth(got) > n || next != "" && strWidth(got)+w <= n {
			t.Fatalf("headCells(%q, %d) = %q: %d cells, the next %q", s, n, got, strWidth(got), next)
		}
	})
}

// TestCenteredModalDims draws the frame under quick open halfway to the
// background, and leaves it as it was under a menu at the pointer.
func TestCenteredModalDims(t *testing.T) {
	m := testModel(t)
	m.fg, m.bg = "#cccccc", "#1e1e1e"

	corner := func() uv.Style { // the frame's top left, which no box covers
		t.Helper()

		checkWidths(t, m)

		line, _, _ := strings.Cut(m.View().Content, "\n")
		buf := uv.NewScreenBuffer(ansi.StringWidth(line), 1)
		uv.NewStyledString(line).Draw(buf, buf.Bounds())

		return buf.CellAt(0, 0).Style
	}

	before := corner()

	m.quickOpen(indexMsg{ws: m.ws, files: []string{"a.go"}}, qoAll)

	fg := before.Fg
	if fg == nil {
		fg = lipgloss.Color(m.fg)
	}

	want := color.RGBAModel.Convert(lipgloss.Blend1D(3, fg, lipgloss.Color(m.bg))[1])
	if got := corner(); got.Fg == nil || color.RGBAModel.Convert(got.Fg) != want {
		t.Fatalf("under quick open the corner is %v, want %v (from %v)", got.Fg, want, before.Fg)
	}

	m.modal = newMenu("", 5, 5, item{label: "Copy", run: func(*Model) tea.Cmd { return nil }})

	if got := corner(); fmt.Sprint(got.Fg) != fmt.Sprint(before.Fg) || got.Attrs != before.Attrs {
		t.Fatalf("under a menu at the pointer the corner is %v, want %v", got, before)
	}

	m.fg, m.bg, m.dark = "", "", true // tmux with no client to ask: black stands in
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"a.go"}}, qoAll)

	fg = before.Fg
	if fg == nil {
		fg = lipgloss.Color("#cccccc")
	}

	want = color.RGBAModel.Convert(lipgloss.Blend1D(3, fg, lipgloss.Color("#000000"))[1])
	if got := corner(); got.Fg == nil || color.RGBAModel.Convert(got.Fg) != want {
		t.Fatalf("with the terminal's colors unknown the corner is %v, want %v", got.Fg, want)
	}
}

// FuzzFuzzyFrom holds fuzzyFrom, which lowers an ASCII path a byte at a time,
// to the score and start of the plain form that lowers both strings first.
func FuzzFuzzyFrom(f *testing.F) {
	for _, c := range [][2]string{{"wid", "packages/Pkg001/Widget0001.tsx"}, {"RM", "README.md"}, {"ı", "İstanbul.go"}, {"é", "café/É.txt"}, {"", "x"}} {
		f.Add(c[0], c[1])
	}

	f.Fuzz(func(t *testing.T, pattern, s string) {
		best, from := fuzzyFrom(pattern, s)
		if wb, wf := fuzzyLowered(pattern, s); best != wb || from != wf {
			t.Fatalf("fuzzyFrom(%q, %q) = %d, %d; lowered first %d, %d", pattern, s, best, from, wb, wf)
		}
	})
}

// fuzzyLowered is fuzzyFrom as it was: both strings lowered, then matched.
func fuzzyLowered(pattern, s string) (best, from int) {
	if pattern == "" {
		return 0, -1
	}

	p, ls := strings.ToLower(pattern), strings.ToLower(s)
	best, from = -1, -1

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
			break
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

// TestDimLineCells holds dimLine, which rewrites a row's SGRs, to the cells
// the row dimmed one cell at a time comes to: the rows of real frames, a
// session's, a file's and quick open's.
func TestDimLineCells(t *testing.T) {
	m := benchModel(t)
	frames := []string{m.View().Content}

	openApp(t, m)
	frames = append(frames, m.View().Content)

	m.quickOpen(indexMsg{ws: m.ws, files: benchFiles(50)}, qoAll)
	frames = append(frames, m.View().Content)

	for _, f := range frames {
		for ln := range strings.SplitSeq(f, "\n") {
			checkDim(t, ln)
		}
	}

	for _, ln := range []string{
		"\x1b[31mred\x1b[39m plain \x1b[91;44mbright\x1b[0m", "\x1b[38:2::10:20:30mcolon\x1b[48:5:200m bg\x1b[m",
		"\x1b[38;5;9mpalette\x1b[7m reverse\x1b[27;49m", "\x1b[1;2;3;4mattrs\x1b[22;23;24m", "\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\ after",
		"\x1b[4:3;58;2;1;2;3mcurly\x1b[59m", "\x1b[38;2;300;0;0mbad\x1b[38;5mshort", "",
	} {
		checkDim(t, ln)
	}
}

// FuzzDimLine checks dimLine against dimCells on rows built from bytes:
// text, colors in every form, resets, attributes and links.
func FuzzDimLine(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})

	tokens := []string{
		"a", "é", "漢", " ", "\x1b[0m", "\x1b[m", "\x1b[39m", "\x1b[49m", "\x1b[1m", "\x1b[22m", "\x1b[7m",
		"\x1b[32m", "\x1b[97m", "\x1b[43m", "\x1b[105m", "\x1b[38;5;123m", "\x1b[48;5;4m", "\x1b[38;2;1;200;3m",
		"\x1b[48;2;9;9;9m", "\x1b[38:2::5:6:7m", "\x1b[38:5:17m", "\x1b[0;31;1m", "\x1b[31;0m", "\x1b]8;;u\x1b\\", "\x1b]8;;\x1b\\",
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var b strings.Builder
		for _, c := range data {
			b.WriteString(tokens[int(c)%len(tokens)])
		}

		checkDim(t, b.String())
	})
}

// checkDim fails where dimLine's ln and dimCells' differ in a cell.
func checkDim(t *testing.T, ln string) {
	t.Helper()

	fg, bg := lipgloss.Color("#cccccc"), lipgloss.Color("#1e1e1e")
	blendC := func(c color.Color) color.Color { return color.RGBAModel.Convert(lipgloss.Blend1D(3, c, bg)[1]) }
	blendS := func(c color.Color) string {
		r, g, b, _ := blendC(c).RGBA()
		return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
	}

	got, want := dimCellsOf(dimLine(ln, blendS(fg), blendS)), dimCellsOf(dimCells(ln, fg, blendC))
	if len(got) != len(want) {
		t.Fatalf("%q: %d cells, want %d", ln, len(got), len(want))
	}

	for x := range got {
		if got[x] != want[x] {
			t.Fatalf("%q: cell %d is %+v, want %+v", ln, x, got[x], want[x])
		}
	}
}

// shownCell is what a cell shows, its colors as RGBA.
type shownCell struct {
	content          string
	fg, bg, ul       color.RGBA
	attrs, underline uint8
	link             string
}

// dimCellsOf is ln's cells.
func dimCellsOf(ln string) []shownCell {
	w := strWidth(ln)
	buf := uv.NewScreenBuffer(max(w, 1), 1)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(ln).Draw(buf, buf.Bounds())

	rgba := func(c color.Color) color.RGBA {
		if c == nil {
			return color.RGBA{}
		}

		r, g, b, a := c.RGBA()

		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)} //nolint:gosec // 16 bits a channel, the high byte kept
	}

	out := make([]shownCell, w)
	for x := range w {
		c := buf.CellAt(x, 0)
		out[x] = shownCell{c.Content, rgba(c.Style.Fg), rgba(c.Style.Bg), rgba(c.Style.UnderlineColor), c.Style.Attrs, c.Style.Underline, c.Link.URL}
	}

	return out
}

// dimCells is how dimLine dims ln, a cell at a time: each cell's text, the
// terminal's own as fg, and its background where it has one, through blend.
func dimCells(ln string, fg color.Color, blend func(color.Color) color.Color) string {
	w := strWidth(ln)
	if w == 0 {
		return ln
	}

	buf := uv.NewScreenBuffer(w, 1)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(ln).Draw(buf, buf.Bounds())

	line := buf.Line(0)
	for x := range line {
		st := &line[x].Style
		st.Fg = blend(cmp.Or[color.Color](st.Fg, fg))

		if st.Bg != nil {
			st.Bg = blend(st.Bg)
		}
	}

	return line.Render()
}

// TestDimPiecesCells dims a frame a panel's piece of a row at a time, as
// baseFrame does under a centered modal, and holds each row's cells to the
// row dimmed whole: no piece leans on the colors the one before it left set.
func TestDimPiecesCells(t *testing.T) {
	fg, bg := lipgloss.Color("#cccccc"), lipgloss.Color("#1e1e1e")
	blendS := func(c color.Color) string {
		r, g, b, _ := lipgloss.Blend1D(3, c, bg)[1].RGBA()
		return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
	}
	soften := func(s string) string {
		if s == "" {
			return s
		}

		return dimLine(s, blendS(fg), blendS)
	}

	m := benchModel(t)
	session := func() {}
	file := func() { openApp(t, m) }
	borderless := func() { m.st.Settings.Borders = false }

	for _, setUp := range []func(){session, file, borderless} {
		setUp()

		m.frame = nil
		_ = m.View()

		cs, c := m.layout()
		m.frame = &frame{m.cols(), cs, c}
		b := m.bord()

		plain, top, bot := m.baseFrame(cs, c, b, nil)
		pieces, ptop, pbot := m.baseFrame(cs, c, b, soften)
		m.frame = nil

		for i := range plain {
			if got, want := dimCellsOf(pieces[i]), dimCellsOf(soften(plain[i])); !slices.Equal(got, want) {
				t.Fatalf("row %d differs dimmed in pieces:\n%q\n%q", i, pieces[i], soften(plain[i]))
			}
		}

		if !slices.Equal(dimCellsOf(ptop), dimCellsOf(soften(top))) || !slices.Equal(dimCellsOf(pbot), dimCellsOf(soften(bot))) {
			t.Fatal("the frame's rules differ dimmed in pieces")
		}
	}
}

// TestItemRowParts holds itemRow, which draws a box's rows from its styles'
// escapes worked out once, to itemRowSegs, which looks at each piece's style
// as row does: byte for byte, lit or not, in every view and icon set, cut
// short or not.
func TestItemRowParts(t *testing.T) {
	defer applyLook("vscode", true, "ascii", nil)

	run := func(*Model) tea.Cmd { return nil }
	items := []item{
		{label: "View: Split Editor Right", hint: `^\`, run: run},
		{label: "Git: Commit", inline: "staged", run: run},
		{label: "dimmed, no run"},
		{label: "internal/ui/quickopen.go", file: "internal/ui/quickopen.go", run: run},
		{label: "README.md", file: "README.md", hint: "M", run: run},
		{label: "  widgets.go", file: "internal/ui/widgets.go", run: run},
		{label: "  12  the needle is here", words: true, dimTo: 6, always: true, run: run},
		{label: "needle at the end of a long line", hint: "internal/ui/x.go:12", words: true, always: true, run: run},
		{label: "+ 3 more", hint: "Files ⇥", always: true, run: run},
	}

	for _, icons := range []string{"ascii", "nerd", "emoji"} {
		applyLook("vscode", true, icons, nil)

		for _, view := range []int{qvPath, qvName, qvTree} {
			for _, q := range []string{"", "qo", "needle", "sp ed"} {
				md := newPicker("", items)
				md.input.SetValue(q)
				md.views, md.treeable, md.fileView = true, true, view
				md.refilter()
				md.pattern = q

				for _, it := range items {
					for _, sel := range []bool{false, true} {
						for _, rw := range []int{80, 24, 9, 3} {
							if got, want := md.itemRow(&it, sel, rw), md.itemRowSegs(&it, sel, rw); got != want {
								t.Fatalf("%s icons, view %d, %q, sel %v, %d cells, %q:\n got %q\nwant %q", icons, view, q, sel, rw, it.label, got, want)
							}
						}
					}
				}
			}
		}
	}
}

// view is md's box as one string at x, y.
func (md *modal) view(m *Model) (string, int, int) {
	lines, x, y := md.lines(m)

	return strings.Join(lines, "\n"), x, y
}
