package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

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
		m.st.Settings.DiffView = ifElse(split, "split", "inline")
		m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
		m.preview, m.focus = true, onMain
		loadDiff(m, pvDiff, longDiff(200, 50, 120))

		if m.pv.split(m) != split {
			t.Fatalf("split = %v", m.pv.split(m))
		}

		rows := changes(m.pv.meta)
		first, second := rows[0][0], rows[1][0]

		if m.pv.at().line != first {
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

			if got := m.pv.at().line; got != step.want {
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

	if dels, adds := m.pv.changedLines(); !dels[151] || !adds[151] || len(dels)+len(adds) != 2 {
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
