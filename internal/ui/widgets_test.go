package ui

import (
	"image/color"
	"strings"
	"testing"

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
