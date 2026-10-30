package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// foldRanges folds a block to its last line indented deeper, its closing
// line in view; blank lines inside belong to it, trailing ones do not.
func TestFoldRanges(t *testing.T) {
	src := []string{
		"func a() {", //  0: to 4
		"  if b {",   //  1: to 2
		"    c()",    //  2
		"",           //  3: blank, inside
		"  d()",      //  4
		"}",          //  5
		"",           //  6
		"x",          //  7: nothing deeper
		"y:",         //  8: to 9
		"  z",        //  9
		"",           // 10
	}

	lines := make([][]rune, len(src))
	for i, l := range src {
		lines[i] = []rune(l)
	}

	want := []int32{4, 2, -1, -1, -1, -1, -1, -1, 9, -1, -1}
	if got := foldRanges(lines); !slices.Equal(got, want) {
		t.Fatalf("foldRanges = %v, want %v", got, want)
	}
}

// FuzzFoldRanges checks every block lies after its header, ends on a line
// that is not blank, and nests in or keeps apart from every other.
func FuzzFoldRanges(f *testing.F) {
	for _, s := range []string{"a\n  b\nc", "a\n  b\n\n  c\n", "\n\n  x\ny\n    z\n  w", "a\n\tb", ""} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		var lines [][]rune
		for _, l := range strings.Split(s, "\n") {
			lines = append(lines, []rune(l))
		}

		end := foldRanges(lines)
		for i, e := range end {
			if e < 0 {
				continue
			}

			if int(e) <= i || int(e) >= len(lines) || blankLine(lines[e]) {
				t.Fatalf("%q: line %d folds to %d", s, i, e)
			}

			for j := i + 1; j <= int(e); j++ {
				if end[j] > e {
					t.Fatalf("%q: block %d-%d overlaps %d-%d", s, i, e, j, end[j])
				}
			}
		}
	})
}

const foldSrc = "package a\n\nfunc A() {\n\tif x {\n\t\ty()\n\t}\n}\n\nfunc B() {}\n"

// The fold marks show while the pointer is over the gutter, ▸ on a folded
// block always; a click on one folds its block behind ⋯ and again unfolds
// it; the cursor steps over a folded block, and a jump into one opens it.
func TestFileFolding(t *testing.T) {
	m, _ := editorModel(t, "a.go", foldSrc)
	out := func() string { return checkWidths(t, m) }
	gutterX := m.mainX() + m.pv.gutter() - 2
	rowOf := func(text string) int { // the screen row text shows on
		t.Helper()

		for y, l := range strings.Split(out(), "\n") {
			if strings.Contains(l, text) {
				return y
			}
		}

		t.Fatalf("%q is not on screen", text)

		return 0
	}

	if strings.Contains(out(), "▾") {
		t.Fatal("fold marks show with the pointer away")
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 1, Y: rowOf("func A")})

	if o := out(); strings.Count(o, "▾") != 2 { // func A and if x; func B {} folds nothing
		t.Fatalf("hovering the gutter: %d marks\n%s", strings.Count(o, "▾"), o)
	}

	click(m, gutterX, rowOf("func A"), tea.MouseLeft)

	o := out()
	if !strings.Contains(o, "func A() { ⋯") || !strings.Contains(o, "▸") || strings.Contains(o, "y()") || !strings.Contains(o, "func B") {
		t.Fatalf("func A folded:\n%s", o)
	}

	// The cursor steps over it, both ways.
	m.focus = onMain
	m.pv.gotoLine(m, 2, 0)
	press(m, "down")

	if m.pv.cur.line != 6 { // the closing brace, which the block leaves in view
		t.Fatalf("down from the header went to line %d", m.pv.cur.line)
	}

	press(m, "up")

	if m.pv.cur.line != 2 {
		t.Fatalf("up onto the fold went to line %d", m.pv.cur.line)
	}

	// Go to Line into it opens it.
	m.pv.gotoLine(m, 4, 0)

	if m.pv.shutEnd(2) > 2 || !strings.Contains(out(), "y()") {
		t.Fatal("a jump into the fold leaves it shut")
	}

	// An edit above moves it along; ctrl+shift+[ and ] fold and unfold.
	m.pv.gotoLine(m, 4, 0)
	press(m, "ctrl+shift+[")

	if m.pv.shutEnd(3) != 4 || m.pv.cur.line != 3 {
		t.Fatalf("fold at the cursor: shut %v, cursor %d", m.pv.fold.shut, m.pv.cur.line)
	}

	m.pv.gotoLine(m, 0, 0)
	press(m, "end", "enter", "x")

	if !slices.Equal(m.pv.fold.shut, []int{4}) || !strings.Contains(out(), "if x { ⋯") {
		t.Fatalf("after a line above: shut %v\n%s", m.pv.fold.shut, out())
	}

	m.pv.gotoLine(m, 4, 0)
	press(m, "ctrl+shift+]")

	if len(m.pv.fold.shut) != 0 {
		t.Fatalf("unfold left %v", m.pv.fold.shut)
	}

	// Fold All, and wrapped lines fold too.
	runCommand(t, m, "editor.foldAll")
	m.toggleWrap()

	if o := out(); strings.Contains(o, "if x") || !strings.Contains(o, "func A() { ⋯") {
		t.Fatalf("fold all, wrapped:\n%s", o)
	}

	runCommand(t, m, "editor.unfoldAll")

	if o := out(); !strings.Contains(o, "y()") {
		t.Fatalf("unfold all:\n%s", o)
	}
}

// Vim's z keys fold: zc, zo, za, zM, zR.
func TestVimFolds(t *testing.T) {
	m, _ := editorModel(t, "a.go", foldSrc)
	m.st.Settings.Vim = true
	m.pv.gotoLine(m, 4, 0)
	m.pv.vim.mode = vimNormal

	press(m, "z", "c")

	if !slices.Equal(m.pv.fold.shut, []int{3}) {
		t.Fatalf("zc: %v", m.pv.fold.shut)
	}

	press(m, "z", "o", "z", "M")

	if !slices.Equal(m.pv.fold.shut, []int{2, 3}) {
		t.Fatalf("zo zM: %v", m.pv.fold.shut)
	}

	press(m, "z", "R")

	if len(m.pv.fold.shut) != 0 || !strings.Contains(ansi.Strip(m.View().Content), "y()") {
		t.Fatalf("zR: %v", m.pv.fold.shut)
	}
}

// TestFoldSelectedBlanks: a folded header's ⋯ follows its whitespace marks,
// selected too, rather than read past them.
func TestFoldSelectedBlanks(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nfunc A() {\n\tif x {\n\t\ty()\n\t}\n}\n\nvar b = 1\n")
	m.st.Settings.Blanks = "all"
	m.pv.cur = pos{2, 0}
	press(m, "ctrl+shift+[")

	m.pv.cur = pos{0, 0}
	press(m, "shift+down", "shift+down", "shift+down")

	if out := checkWidths(t, m); !strings.Contains(out, "func·A()·{ ⋯") {
		t.Fatalf("the selected header, its marks and its ⋯:\n%s", out)
	}
}
