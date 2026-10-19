package ui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

func TestChangesAndStepTo(t *testing.T) {
	kinds := "ccdaacchacdc"

	meta := make([]lineMeta, len(kinds))
	for i := range kinds {
		meta[i].kind = kinds[i]
	}

	if got, want := changes(meta), [][2]int{{2, 5}, {8, 9}, {10, 11}}; !slices.Equal(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}

	starts := []int{2, 8, 10}
	for _, c := range []struct{ line, d, want int }{
		{0, 1, 2}, {2, 1, 8}, {3, 1, 8}, {10, 1, 2}, // inside a change the next one is after it
		{0, -1, 10}, {2, -1, 10}, {3, -1, 2}, {9, -1, 8}, // inside one, its own start first
	} {
		if got, ok := stepTo(starts, c.line, c.d); !ok || got != c.want {
			t.Errorf("stepTo(%d, %d) = %d, want %d", c.line, c.d, got, c.want)
		}
	}

	if _, ok := stepTo(nil, 0, 1); ok {
		t.Fatal("no changes, nowhere to go")
	}
}

// longDiff is a file's whole-file diff of n lines with a line changed at each
// of at.
func longDiff(n int, at ...int) string {
	ops := map[int]string{}
	for _, i := range at {
		ops[i] = "m"
	}

	return opsDiff(n, ops)
}

func loadDiff(m *Model, kind, raw string) {
	lines, plain, meta, numW := render(kind, "f.go", raw, true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: raw, lines: lines, plain: plain, meta: meta, numW: numW})
}

// TestGotoChange steps through a diff's changes as VS Code's ⌥F5 and ⇧⌥F5
// do, around both ends, each one in the middle of the view, inline and side
// by side.
func TestGotoChange(t *testing.T) {
	for _, split := range []bool{false, true} {
		m := testModelSized(t, 160, 30)
		m.st.Settings.DiffFull = true // the whole file, as z shows it
		m.st.Settings.DiffView = ifElse(split, "split", "inline")
		m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
		m.preview, m.focus = true, onMain
		loadDiff(m, pvDiff, longDiff(200, 50, 120))

		if m.pv.split(m) != split {
			t.Fatalf("split = %v", m.pv.split(m))
		}

		rows := changes(m.pv.meta)
		first, second := rows[0][0], rows[1][0]

		// on reports the cursor on line n's row: side by side, on its new half
		on := func(n int) bool {
			sr := splitRows(m.pv.meta)
			return m.pv.at().line == n || split && splitIndex(sr, n) == splitIndex(sr, m.pv.at().line)
		}

		if !on(first) {
			t.Fatalf("split %v: the diff opens on its first change, line %d; the cursor is on %d", split, first, m.pv.at().line)
		}

		row := func() int {
			if split {
				return splitIndex(splitRows(m.pv.meta), m.pv.at().line) - m.pv.top
			}

			return m.pv.cursorRow(m.pvW()) - m.pv.top
		}

		for _, step := range []struct {
			key  string
			want int
		}{{"]", second}, {"]", first}, {"[", second}, {"[", first}, {"alt+f5", second}, {"alt+shift+f5", first}} {
			press(m, step.key)

			if got := m.pv.at().line; !on(step.want) {
				t.Fatalf("split %v: %s went to line %d, want %d", split, step.key, got, step.want)
			}

			if r := row(); r != m.pvH()/2 {
				t.Fatalf("split %v: the change is on row %d of %d, want the middle", split, r, m.pvH())
			}

			checkWidths(t, m)
		}
	}
}

// TestStepFileInCommit goes from file to file of a commit's diff: each
// file's name on the top row and the cursor on its first change; back from
// inside a file is the file before it.
func TestStepFileInCommit(t *testing.T) {
	m := testModelSized(t, 100, 20)
	m.pv = preview{kind: pvShow, root: m.ws, rev: "abc"}
	m.preview, m.focus = true, onMain

	raw := strings.Replace(longDiff(30, 3), "f.go", "g.go", 4) + longDiff(30, 10) + strings.Replace(longDiff(30, 20), "f.go", "h.go", 4)
	loadDiff(m, pvShow, raw)

	var names []int

	for i, l := range m.pv.meta {
		if l.kind == 'f' {
			names = append(names, i)
		}
	}

	if len(names) != 3 {
		t.Fatalf("file rows = %v", names)
	}

	first := func(name int) int { // the file's first change
		return changes(m.pv.meta)[slices.IndexFunc(changes(m.pv.meta), func(c [2]int) bool { return c[0] > name })][0]
	}

	for _, step := range []struct {
		key  string
		file int
	}{{"}", 1}, {"}", 2}, {"}", 0}, {"{", 2}, {"{", 1}} {
		press(m, step.key)

		name := names[step.file]

		top := m.pv.lineRow[name] // the name on the top row, unless the change is too far under it: then the change mid view
		if m.pv.lineRow[first(name)]-top >= m.pvH() {
			top = m.pv.lineRow[first(name)] - m.pvH()/2
		}

		if got := m.pv.at().line; got != first(name) || m.pv.top != top {
			t.Fatalf("%s: cursor on line %d, top %d; want file %d's first change, line %d, top %d", step.key, got, m.pv.top, step.file, first(name), top)
		}
	}

	checkWidths(t, m)
}

// TestStepFileInSourceControl opens the next changed file's diff from a
// file's diff and selects it in Source Control, around the ends.
func TestStepFileInSourceControl(t *testing.T) {
	m := gitModel(t)

	var files []scmRow

	for _, r := range m.scm.rows {
		if r.kind == rowFile {
			files = append(files, r)
		}
	}

	first := files[0]
	m.pv = preview{kind: pvDiff, root: first.root, path: first.entry.Path, entry: first.entry}
	m.preview, m.focus = true, onMain

	for _, want := range append(files[1:], files[0]) {
		press(m, "}")

		if m.pv.kind != pvDiff || m.pv.path != want.entry.Path {
			t.Fatalf("} opened %s %q, want the diff of %q", m.pv.kind, m.pv.path, want.entry.Path)
		}

		if r := m.scm.selected(); r == nil || r.entry.Path != want.entry.Path {
			t.Fatalf("Source Control selects %v, want %q", r, want.entry.Path)
		}
	}

	press(m, "{")

	if last := files[len(files)-1]; m.pv.path != last.entry.Path {
		t.Fatalf("{ from the first file opened %q, want the last, %q", m.pv.path, last.entry.Path)
	}
}

// opsDiff is a file's whole-file diff of n lines: at line i an "a" adds a
// line after it, a "d" deletes it, an "m" replaces it.
func opsDiff(n int, ops map[int]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1,%d +1,%d @@\n", n, n)

	for i := range n {
		switch ops[i] {
		case "a":
			fmt.Fprintf(&b, " line %d\n+added %d\n", i, i)
		case "d":
			fmt.Fprintf(&b, "-line %d\n", i)
		case "m":
			fmt.Fprintf(&b, "-line %d\n+changed %d\n", i, i)
		default:
			fmt.Fprintf(&b, " line %d\n", i)
		}
	}

	return b.String()
}

// TestScrollbarChangeMarks marks a diff's changes on its scrollbar as VS
// Code's overview ruler does: green, red, or the modified color for both, on
// the slider too, side by side as well; a click on a mark off the slider
// goes to that change.
func TestScrollbarChangeMarks(t *testing.T) {
	applyLook("vscode", true, "ascii", nil)

	for _, split := range []bool{false, true} {
		m := testModelSized(t, 160, 30)
		m.st.Settings.DiffFull = true // the whole file, as z shows it
		m.st.Settings.DiffView = ifElse(split, "split", "inline")
		m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
		m.preview, m.focus = true, onMain
		loadDiff(m, pvDiff, opsDiff(300, map[int]string{2: "a", 150: "d", 290: "m"}))
		checkWidths(t, m)

		b, h := m.pv.bar(m), m.pvH()
		x, y0 := m.mainX()+m.mainW()-1, 1+m.stripH()
		marked := func(c string) bool { return strings.ContainsAny(ansi.Strip(c), "▁▂▃▄▅▆▇█▔▀▐") }
		cell := func(y int) string { // the bar's cell: its glyph and the last SGR before it, not those the cut carries along
			c := ansi.Cut(m.editorLines(m.mainW())[m.stripH()+1+y], m.mainW()-1, m.mainW())
			if g := strings.LastIndex(c, ansi.Strip(c)); g > 0 {
				c = c[strings.LastIndex(c[:g], "\x1b["):]
			}

			return c
		}

		kinds, lines := m.pv.barChanges(m)
		yOf := func(c byte) int { return slices.Index(kinds, c) * h / b.n }

		add, del := yOf(2), yOf(1)
		mod := slices.IndexFunc(lines, func(n int) bool { return m.pv.meta[n].kind == 'd' && m.pv.meta[n].a == 291 }) * h / b.n

		for _, c := range []struct {
			y   int
			col color.Color
		}{{add, pal.diffAddMark}, {del, pal.diffDelMark}, {mod, pal.diffModMark}} {
			if got := cell(c.y); !strings.Contains(got, fgParams(c.col)) && !strings.Contains(got, bgParams(c.col)) || !marked(got) {
				t.Fatalf("split %v: bar row %d = %q, want a mark in %v", split, c.y, got, c.col)
			}
		}

		if ty, _ := b.thumb(); ty != 0 || !strings.Contains(cell(add), bgParams(pal.sliderBg)) {
			t.Fatalf("split %v: the mark at the top is drawn on the slider: %q", split, cell(add))
		}

		if got := cell(del + 1); marked(got) {
			t.Fatalf("split %v: row %d has no change, got %q", split, del+1, got)
		}

		m.Update(tea.MouseClickMsg{X: x, Y: y0 + del, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: x, Y: y0 + del, Button: tea.MouseLeft})

		if l := m.pv.at().line; m.pv.meta[l].kind != 'd' || m.pv.meta[l].a != 151 {
			t.Fatalf("split %v: a click on the deletion's mark put the cursor on %+v", split, m.pv.meta[l])
		}

		checkWidths(t, m)
	}
}

// TestScrollbarMarksDense keeps a file changed all over legible: a cell is
// two marks, the rarer kind shows where changes share one, and clicks on a
// cell go through every change it holds.
func TestScrollbarMarksDense(t *testing.T) {
	applyLook("vscode", true, "ascii", nil)

	ops := map[int]string{}
	for i := 37; i < 2000; i += 37 {
		ops[i] = "m"
	}

	for i := 101; i < 2000; i += 101 {
		ops[i] = "d"
	}

	m := testModelSized(t, 160, 30)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, opsDiff(2000, ops))

	h, x, y0 := m.pvH(), m.mainX()+m.mainW()-1, 1+m.stripH()
	cells := m.pv.bar(m).marked(barState{}, func() []byte { k, _ := m.pv.barChanges(m); return k }(), diffMarks())

	var reds int

	for _, c := range cells[:h] {
		if strings.Contains(c, fgParams(pal.diffDelMark)) || strings.Contains(c, bgParams(pal.diffDelMark)) {
			reds++
		}
	}

	if reds < len(ops)/10 {
		t.Fatalf("the deletions among the modifications show red: %d cells of %d", reds, h)
	}

	for _, top := range []int{0, 900} { // the slider shows through the marks, wherever it is
		m.pv.top = top
		b := m.pv.bar(m)
		ty, sh := b.thumb()
		kinds, _ := m.pv.barChanges(m)

		for i, c := range b.marked(barState{}, kinds, diffMarks())[ty : ty+sh] {
			if !strings.Contains(c, bgParams(pal.sliderBg)) {
				t.Fatalf("top %d: slider cell %d hides the slider: %q", top, ty+i, c)
			}
		}
	}

	m.pv.top = 0

	// The last cell's changes, each a click away, then the first again.
	ty, sh := m.pv.bar(m).thumb()

	y := h - 1
	if y >= ty && y < ty+sh {
		t.Fatal("the slider is at the top")
	}

	var seen []int

	for range 4 {
		m.Update(tea.MouseClickMsg{X: x, Y: y0 + y, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: x, Y: y0 + y, Button: tea.MouseLeft})
		seen = append(seen, m.pv.at().line)
		m.pv.top = 0 // back to the top, so the slider is off the cell again
	}

	if seen[0] == seen[1] || !slices.Contains(seen[2:], seen[0]) {
		t.Fatalf("clicks on one cell go through its changes and around: %v", seen)
	}

	checkWidths(t, m)
}

// compactDiff is the changes-only diff of longDiff(n, at...): three lines of
// context around each change.
func compactDiff(n int, at ...int) string {
	var b strings.Builder

	b.WriteString("diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n")

	for _, i := range at {
		from, to := max(i-3, 0), min(i+4, n)
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", from+1, to-from, from+1, to-from)

		for j := from; j < to; j++ {
			if j == i {
				fmt.Fprintf(&b, "-old %d\n+new %d\n", j, j)
			} else {
				fmt.Fprintf(&b, " line %d\n", j)
			}
		}
	}

	return b.String()
}

// TestDiffLayoutKeepsPlace keeps the cursor's line where it was through a
// switch to side by side and back, and finds it again, on the same screen
// row, in the other context mode, as VS Code's diff editor does.
func TestDiffLayoutKeepsPlace(t *testing.T) {
	m := testModelSized(t, 160, 30)
	m.st.Settings.DiffFull = true // the whole file, as z shows it
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, longDiff(200, 50, 120))

	press(m, "]", "down", "down") // from the first change, a context line two under the second

	line, off := m.pv.at().line, m.pv.row(m)-m.pv.top

	press(m, "s")

	if !m.pv.split(m) || m.pv.at().line != line || m.pv.row(m)-m.pv.top != off {
		t.Fatalf("side by side: line %d on row %d, want %d on row %d", m.pv.at().line, m.pv.row(m)-m.pv.top, line, off)
	}

	press(m, "s")

	if m.pv.split(m) || m.pv.at().line != line || m.pv.row(m)-m.pv.top != off {
		t.Fatalf("inline again: line %d on row %d, want %d on row %d", m.pv.at().line, m.pv.row(m)-m.pv.top, line, off)
	}

	was := m.pv.meta[line]
	m.pv.keepPlace(m) // what z does before it loads the other mode
	loadDiff(m, pvDiff, compactDiff(200, 50, 120))

	if got := m.pv.meta[m.pv.at().line]; got != was || m.pv.row(m)-m.pv.top != min(off, m.pv.row(m)) {
		t.Fatalf("changes only: cursor on %+v at row %d, want %+v at row %d", got, m.pv.row(m)-m.pv.top, was, off)
	}

	press(m, "G") // the last context line, then a line only the whole file has
	m.pv.keepPlace(m)
	loadDiff(m, pvDiff, longDiff(200, 50, 120))

	if got := m.pv.meta[m.pv.at().line]; got.b != 124 {
		t.Fatalf("whole file: cursor on %+v, want new line 124", got)
	}

	m.pv.setLine(m, 100)
	m.pv.keepPlace(m)
	loadDiff(m, pvDiff, compactDiff(200, 50, 120))

	if got := m.pv.meta[m.pv.at().line]; got.b != 118 {
		t.Fatalf("a line the changes leave out: cursor on %+v, want the next shown, new line 118", got)
	}
}

// TestDiffOpensOnFirstChange opens a file's diff on its first change, in
// the middle of the view when the whole file puts it out of sight; a reload
// and a commit's diff keep their place.
func TestDiffOpensOnFirstChange(t *testing.T) {
	m := testModelSized(t, 100, 20)
	m.st.Settings.DiffFull = true // the whole file, as z shows it
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, longDiff(300, 200))

	if l := m.pv.at().line; m.pv.meta[l].kind != 'd' || m.pv.row(m)-m.pv.top != m.pvH()/2 {
		t.Fatalf("the whole file opens on the change mid view: cursor on %+v at row %d", m.pv.meta[l], m.pv.row(m)-m.pv.top)
	}

	press(m, "up", "up")

	line := m.pv.at().line
	loadDiff(m, pvDiff, longDiff(300, 200)) // the 2 s reload of a live diff

	if m.pv.at().line != line {
		t.Fatalf("a reload keeps the cursor: line %d, want %d", m.pv.at().line, line)
	}

	m.pv = preview{kind: pvShow, root: m.ws, rev: "abc"}
	loadDiff(m, pvShow, "commit abc\n\n    the message\n\n"+longDiff(300, 200))

	if m.pv.at().line != 0 || m.pv.top != 0 {
		t.Fatalf("a commit opens on its message: line %d, top %d", m.pv.at().line, m.pv.top)
	}
}

// TestDiffReloadKeepsPlace keeps the cursor and the selection on their file
// lines and screen row when an agent's edit above them moves the diff's rows
// on the 2 s reload; a cursor moved meanwhile stays where it went.
func TestDiffReloadKeepsPlace(t *testing.T) {
	m := testModelSized(t, 100, 30)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, compactDiff(200, 50, 120))

	press(m, "]", "shift+down") // the second change, its deletion and its addition selected

	at, from, off := m.pv.meta[m.pv.at().line], m.pv.meta[m.pv.anchor.line], m.pv.row(m)-m.pv.top
	reloaded := func() {
		t.Helper()

		m.pv.reloadIfLive(m)

		if m.pv.keep == nil {
			t.Fatal("a live diff's reload notes the place")
		}

		loadDiff(m, pvDiff, compactDiff(200, 20, 50, 120)) // the agent changed line 20 too
	}

	reloaded()

	if got := m.pv.meta[m.pv.at().line]; got != at || m.pv.anchor == nil || m.pv.meta[m.pv.anchor.line] != from || m.pv.row(m)-m.pv.top != off {
		t.Fatalf("after the reload: cursor on %+v at row %d, anchor %v; want %+v at row %d, the selection from %+v",
			got, m.pv.row(m)-m.pv.top, m.pv.anchor, at, off, from)
	}

	m.pv.reloadIfLive(m)
	press(m, "up") // while it loads
	line := m.pv.at().line
	loadDiff(m, pvDiff, compactDiff(200, 20, 50, 121))

	if m.pv.at().line != line {
		t.Fatalf("a cursor moved during the load stays: line %d, want %d", m.pv.at().line, line)
	}
}

// TestSplitCursorRow stages and reverts the cursor's row in a side-by-side
// diff, whose cursor is a whole row, where the inline diff takes the whole
// change under its cursor.
func TestSplitCursorRow(t *testing.T) {
	m := testModelSized(t, 160, 30)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, "diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1,2 +1,5 @@\n a\n+x\n+y\n+z\n b\n")

	press(m, "]") // the change, its first line

	if _, adds := m.pv.changedLines(m); len(adds) != 3 {
		t.Fatalf("inline, the whole change: %v", adds)
	}

	m.st.Settings.DiffView = "split"

	if dels, adds := m.pv.changedLines(m); !m.pv.split(m) || len(dels) != 0 || len(adds) != 1 {
		t.Fatalf("side by side, the cursor's row: dels %v adds %v", dels, adds)
	}
}

// TestDiffCursorChange stages and reverts the whole change under a cursor
// with no selection, its deletion with its addition, as VS Code's Stage
// Selected Ranges does; the status bar and Go to Line count the file's
// lines, not the diff's rows.
func TestDiffCursorChange(t *testing.T) {
	m := testModelSized(t, 100, 30)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, compactDiff(200, 50, 150))

	press(m, "]") // the second change's deletion, line 151

	if dels, adds := m.pv.changedLines(m); !dels[151] || !adds[151] || len(dels)+len(adds) != 2 {
		t.Fatalf("the change under the cursor: dels %v adds %v, want line 151 both", dels, adds)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "Ln 151,") {
		t.Fatalf("the status bar counts the file's lines:\n%s", out)
	}

	_ = m.gotoLineQuery(":149") // the picker; its command is for a server's symbols
	press(m, "enter")

	if l := m.pv.meta[m.pv.at().line]; l.b != 149 {
		t.Fatalf("Go to Line 149 went to %+v", l)
	}
}

// TestDiffEdges: a commit with no file names to step, and Go to Line in a
// deleted file's diff, which has only old lines.
func TestDiffEdges(t *testing.T) {
	m := testModelSized(t, 100, 20)
	m.pv = preview{kind: pvShow, root: m.ws, rev: "abc"}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvShow, "commit abc\n\n    an empty merge\n")
	press(m, "{", "}") // nothing to step to, and no panic

	var b strings.Builder

	b.WriteString("diff --git a/f.go b/f.go\ndeleted file mode 100644\n--- a/f.go\n+++ /dev/null\n@@ -1,10 +0,0 @@\n")

	for i := range 10 {
		fmt.Fprintf(&b, "-line %d\n", i)
	}

	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'D'}}
	loadDiff(m, pvDiff, b.String())

	_ = m.gotoLineQuery(":6")
	press(m, "enter")

	if l := m.pv.meta[m.pv.at().line]; l.a != 6 {
		t.Fatalf("Go to Line 6 in a deleted file went to %+v", l)
	}

	if got := findPlace(m.pv.meta, lineMeta{a: 99}); got != len(m.pv.meta)-1 {
		t.Fatalf("a line past the end is the last row: %d", got)
	}
}

// TestMarksOnTheSlider: a change in view is marked on the slider, one out of
// view off it, however the two scale.
func TestMarksOnTheSlider(t *testing.T) {
	for _, b := range []vbar{{2000, 38, 1480}, {2000, 38, 0}, {2000, 38, 1962}, {301, 27, 140}} {
		ty, sh := b.thumb()

		for r := range b.n {
			on := b.slotOf(r)/barSlots >= ty && b.slotOf(r)/barSlots < ty+sh
			if in := r >= b.top && r < b.top+b.h; in != on && (in || ty > 0 && ty+sh < b.h) {
				t.Fatalf("%+v: row %d in view %v, marked on the slider %v (cell %d, slider %d+%d)", b, r, in, on, b.slotOf(r)/barSlots, ty, sh)
			}
		}
	}
}

// TestNextChangeFromTheList: VS Code's ⌥F5 goes to the open diff's next
// change with the keyboard still in Source Control, where opening a file
// leaves it; ] there stays the session's.
func TestNextChangeFromTheList(t *testing.T) {
	m := gitModel(t)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview = true
	loadDiff(m, pvDiff, compactDiff(200, 50, 150))

	if m.focus == onMain {
		t.Fatal("Source Control has the keyboard")
	}

	first := m.pv.at().line
	press(m, "alt+f5")

	if l := m.pv.meta[m.pv.at().line]; l.a != 151 || m.focus == onMain {
		t.Fatalf("alt+f5 from the list went to %+v (from line %d), focus %d", l, first, m.focus)
	}

	press(m, "alt+shift+f5")

	if m.pv.at().line != first {
		t.Fatalf("alt+shift+f5 went back to %d, want %d", m.pv.at().line, first)
	}
}

// A marked cell draws its eighths with one block glyph and two colors, the
// track's and its marks': a mark one slot high still shows, two marks in a
// cell both show, the higher rank where they share a slot; on the slider a
// mark keeps its own color, the slider sinking into it.
func TestMarkCell(t *testing.T) {
	applyLook("vscode", true, "ascii", nil)

	set := diffMarks()
	shows := func(c string, col color.Color) bool {
		return strings.Contains(c, fgParams(col)) || strings.Contains(c, bgParams(col))
	}

	for _, c := range []struct {
		cell [barSlots]byte
		want []color.Color
		g    string
	}{
		{[barSlots]byte{7: 2}, []color.Color{pal.diffAddMark}, "▁"},
		{[barSlots]byte{2, 2, 2, 2, 2, 2, 2, 2}, []color.Color{pal.diffAddMark}, ""},
		{[barSlots]byte{0: 1, 7: 2}, []color.Color{pal.diffDelMark, pal.diffAddMark}, ""},
		{[barSlots]byte{0, 0, 0, 0, 3, 3, 3, 3}, []color.Color{pal.diffModMark}, ""}, // ▄, or ▀ in the track's color over the mark's
	} {
		got := markCell(c.cell, set)
		for _, col := range c.want {
			if !shows(got, col) {
				t.Errorf("%v: %q lacks %v", c.cell, got, col)
			}
		}

		if c.g != "" && ansi.Strip(got) != c.g {
			t.Errorf("%v: glyph %q, want %q", c.cell, ansi.Strip(got), c.g)
		}
	}

	// One changed line in 5000 still takes a slot, on its own cell.
	kinds := make([]byte, 5000)
	kinds[4200] = 2

	b := vbar{5000, 30, 0}
	cells := b.marked(barState{}, kinds, set)

	marked := 0

	for i, c := range cells {
		if shows(c, pal.diffAddMark) {
			marked++

			if i != b.slotOf(4200)/barSlots {
				t.Errorf("the mark on cell %d, its slot on %d", i, b.slotOf(4200)/barSlots)
			}
		}
	}

	if marked != 1 {
		t.Fatalf("one changed line marks %d cells", marked)
	}

	// On the slider, the mark's own color over the slider's.
	kinds[4200], kinds[3] = 0, 3

	if c := b.marked(barState{}, kinds, set)[0]; !strings.Contains(c, fgParams(pal.diffModMark)) || !strings.Contains(c, bgParams(pal.sliderBg)) {
		t.Fatalf("a mark on the slider: %q", c)
	}
}
