package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/lsp"
)

// marksOf is the marks now's changes against base give it, as text, a dot
// for an unchanged line.
func marksOf(base, now string) string {
	out := []byte{}
	for _, k := range changeMarks(len(strings.Fields(now)), diffChanges(strings.Fields(base), strings.Fields(now))) {
		out = append(out, ifElse(k == 0, byte('.'), k))
	}

	return string(out)
}

// TestLineChanges marks lines as VS Code's quick diff does: added where a
// change only added, modified where it also removed, a deletion on the line
// before it, or on the first.
func TestLineChanges(t *testing.T) {
	for _, c := range []struct{ base, now, want string }{
		{"a b c", "a b c", "..."},
		{"a b c", "a x b c", ".a.."},
		{"a b c", "a x c", ".m."},
		{"a b c d", "a x d", ".m."},
		{"a b c", "a x y c", ".mm."},
		{"a b c", "a c", "d."},
		{"a b c", "b c", "u."},
		{"a b c", "a b", ".d"},
		{"a b c d e", "a B c new d", ".m.ad"},
		{"", "x y", "aa"},
	} {
		if got := marksOf(c.base, c.now); got != c.want {
			t.Errorf("%q → %q: %s, want %s", c.base, c.now, got, c.want)
		}
	}
}

// FuzzEditScript: the script spells b out of a, and the changes it makes
// leave the lines between them the same in both, in order.
func FuzzEditScript(f *testing.F) {
	f.Add("a\nb\nc", "a\nx\nc\nd")
	f.Add("", "x")
	f.Add("a\na\na", "a")
	f.Fuzz(func(t *testing.T, x, y string) {
		a, b := strings.Split(x, "\n"), strings.Split(y, "\n")

		ops, ok := editScript(a, b)
		if !ok {
			return // past maxEdits
		}

		i, j := 0, 0

		for _, op := range ops {
			switch op {
			case '=':
				if i >= len(a) || j >= len(b) || a[i] != b[j] {
					t.Fatalf("%q kept unequal lines at %d, %d: %q", ops, i, j, ops)
				}

				i, j = i+1, j+1

			case '-':
				i++
			case '+':
				j++
			}
		}

		if i != len(a) || j != len(b) {
			t.Fatalf("%q ends at %d, %d of %d, %d", ops, i, j, len(a), len(b))
		}

		i, j = 0, 0
		for _, c := range diffChanges(a, b) {
			if c.a0 < i || c.b0 < j || c.a1 < c.a0 || c.b1 < c.b0 || c.a0-i != c.b0-j || !slices.Equal(a[i:c.a0], b[j:c.b0]) {
				t.Fatalf("change %+v after %d, %d", c, i, j)
			}

			i, j = c.a1, c.b1
		}

		if !slices.Equal(a[i:], b[j:]) {
			t.Fatalf("the lines after the last change differ: %q, %q", a[i:], b[j:])
		}
	})
}

// TestGutterMarks draws the marks in the editor's gutter, in its first
// column before the line numbers: a thin bar ▎ added or modified, its color
// telling which, ▾ deleted; and works them out again after an edit.
func TestGutterMarks(t *testing.T) {
	m, _ := editorModel(t, "a.go", "a\nB\nc\nnew\nd\n")
	m.pv.base, m.pv.baseGen = []string{"a", "b", "c", "d", "e"}, 1

	for _, msg := range runAll(m.noteGutter()) {
		m.Update(msg)
	}

	out := m.View().Content
	for _, want := range []string{"▎2   B", "▎4   new", "▾5   d"} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Fatalf("no %q in:\n%s", want, ansi.Strip(out))
		}
	}

	for _, c := range []string{fgParams(pal.diffModMark), fgParams(pal.diffAddMark), fgParams(pal.diffDelMark)} {
		if !strings.Contains(out, c) {
			t.Fatalf("no mark in %s", c)
		}
	}

	m.pv.buf.apply(pos{1, 0}, pos{1, 1}, "b", pos{1, 0})

	for _, msg := range runAll(m.noteGutter()) {
		m.Update(msg)
	}

	if m.pv.chg[1] != 0 {
		t.Fatalf("back to the staged text, no mark: %q", m.pv.chg)
	}

	// Staged changes, against the committed text, show where none since, in
	// VS Code's secondary shade.
	m.pv.base, m.pv.head, m.pv.baseGen = []string{"a", "b", "c", "new", "d"}, []string{"a", "b", "c", "d"}, 2

	for _, msg := range runAll(m.noteGutter()) {
		m.Update(msg)
	}

	if got := string(m.pv.chg); got != "\x00\x00\x00A\x00" {
		t.Fatalf("staged marks: %q", got)
	}

	if out := m.View().Content; !strings.Contains(ansi.Strip(out), "▎4   new") || !strings.Contains(out, fgParams(lipgloss.Darken(pal.diffAddMark, 0.5))) {
		t.Fatalf("a staged line's bar in VS Code's darker shade:\n%s", out)
	}
}

// TestQuickDiff opens VS Code's quick diff under the editor from a click on
// a change's mark, which widens under the pointer: the change as a diff,
// the next one with ↓ and the editor at it, Revert putting the staged lines
// back in the editor, Stage writing a change into the index.
func TestQuickDiff(t *testing.T) {
	m, path := editorModel(t, "a.txt", "a\nB\nc\nd\nnew\ne\n")
	mustGit(t, m.ws, "init", "-q")
	mustWrite(t, path, "a\nb\nc\nd\ne\n")
	mustGit(t, m.ws, "add", "a.txt")
	mustWrite(t, path, "a\nB\nc\nd\nnew\ne\n")

	m.pv.base, m.pv.baseGen = []string{"a", "b", "c", "d", "e"}, 1
	gutter := func() { // what the program does with the command Update returned
		m.pv.chgKey = gutterKey{}
		for _, msg := range runAll(m.noteGutter()) {
			m.Update(msg)
		}
	}
	gutter()

	x, y := m.mainX(), 1+m.stripH()+1 // the mark of line 2, B, in the gutter's first column
	m.Update(tea.MouseMotionMsg{X: x, Y: y})

	if !strings.Contains(ansi.Strip(m.View().Content), "▌2   B") {
		t.Fatalf("the change under the pointer widens:\n%s", ansi.Strip(m.View().Content))
	}

	click(m, x, y, tea.MouseLeft)

	if m.pv.qd == nil || m.pv.qd.i != 0 {
		t.Fatal("a click on the mark opens the quick diff on its change")
	}

	var kinds strings.Builder
	for _, l := range m.pv.qd.meta {
		kinds.WriteByte(l.kind)
	}

	out := ansi.Strip(checkWidths(t, m))
	if kinds.String() != "cdaccc" || !strings.Contains(out, "1 of 2 changes  Git Local Changes (Working Tree)") || !strings.Contains(out, "Stage") {
		t.Fatalf("the change as a diff, %q:\n%s", kinds.String(), out)
	}

	// The zone stands under its change's line, its buttons raised under the
	// mouse as every button is.
	if strings.Contains(m.View().Content, bgParams(pal.keycapBg)+"m") {
		t.Fatal("no button raised before the mouse is on one")
	}

	for y, l := range strings.Split(out, "\n") {
		if x := strings.Index(l, "Stage"); x >= 0 && strings.Contains(l, "1 of 2 changes") {
			if !strings.Contains(strings.Split(out, "\n")[y-1], "▌2   B") {
				t.Fatalf("the zone right under its change:\n%s", out)
			}

			m.Update(tea.MouseMotionMsg{X: ansi.StringWidth(l[:x]), Y: y})
		}
	}

	if !strings.Contains(m.View().Content, bgParams(pal.keycapBg)+"m") {
		t.Fatal("the button under the mouse is raised")
	}

	press(m, "alt+f3")

	if m.pv.qd.i != 1 || m.pv.at().line != 4 {
		t.Fatalf("↓ shows the next change, the editor at it: %d, line %d", m.pv.qd.i, m.pv.at().line)
	}

	press(m, "alt+shift+f3")
	m.revertChange()
	gutter()

	if got := m.pv.buf.text(); got != "a\nb\nc\nd\nnew\ne" || len(m.pv.changes) != 1 || m.pv.qd == nil || m.pv.qd.i != 0 {
		t.Fatalf("Revert puts the staged line back, the peek on the change left: %q, %+v", got, m.pv.changes)
	}

	for _, msg := range runAll(m.stageChange()) {
		if staged, ok := msg.(changeStagedMsg); ok && staged.err != nil {
			t.Fatal(staged.err)
		}
	}

	if got, _ := git.Run(m.ws, "show", ":a.txt"); got != "a\nb\nc\nd\nnew\ne\n" {
		t.Fatalf("Stage writes the change into the index: %q", got)
	}
}

// TestFileScrollbarChanges marks a file's changes on its scrollbar as VS
// Code's overview ruler does, a problem over them, and a click on a change's
// mark goes to it.
func TestFileScrollbarChanges(t *testing.T) {
	var base, now []string
	for i := range 120 {
		base, now = append(base, fmt.Sprint("l", i)), append(now, fmt.Sprint("l", i))
	}

	now[90] = "changed"

	m, _ := editorModel(t, "a.txt", strings.Join(now, "\n")+"\n")
	m.pv.base, m.pv.baseGen, m.pv.chgKey = base, 1, gutterKey{}

	for _, msg := range runAll(m.noteGutter()) {
		m.Update(msg)
	}

	b, vis := m.pv.bar(m), m.pv.rows(m.pvW())
	y := b.slotOf(slices.IndexFunc(vis, func(vr vrow) bool { return vr.line == 90 })) / barSlots
	cell := func() string { // the scrollbar's cell on row y
		row := strings.Split(m.View().Content, "\n")[1+m.stripH()+y]
		return ansi.Cut(row, m.mainX()+m.pvW(), m.mainX()+m.pvW()+1)
	}

	if !strings.Contains(cell(), fgParams(pal.diffModMark)) {
		t.Fatalf("the change is marked on the scrollbar: %q", cell())
	}

	click(m, m.mainX()+m.pvW(), 1+m.stripH()+y, tea.MouseLeft)

	if m.pv.at().line != 90 {
		t.Fatalf("a click on its mark goes to the change: line %d", m.pv.at().line)
	}

	m.pv.gotoLine(m, 0, 0) // the slider off the mark again
	m.diags[m.pv.path] = []lsp.Diagnostic{{Line: 90, EndLine: 90, EndCol: 2, Severity: 1, Message: "x"}}
	m.st.Settings.Diags = true

	if !strings.Contains(cell(), fgParams(pal.errc)) {
		t.Fatalf("a problem shows over the change: %q", cell())
	}
}

// The cursor's line is lit faintly the whole width, its number too, as VS
// Code's editor.lineHighlightBackground; not while text is selected, nor in
// a diff.
func TestLineHighlight(t *testing.T) {
	m, _ := editorModel(t, "a.txt", "one\ntwo\nthree\n")
	m.pv.gotoLine(m, 1, 0)

	lit := func() (rows []string) {
		checkWidths(t, m)

		for _, l := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(l, bgParams(pal.lineHiBg)) {
				rows = append(rows, ansi.Strip(l))
			}
		}

		return rows
	}

	if rows := lit(); len(rows) != 1 || !strings.Contains(rows[0], "2   two") {
		t.Fatalf("lit rows %q", rows)
	}

	press(m, "shift+down")

	if rows := lit(); len(rows) != 0 {
		t.Fatalf("a selection keeps rows %v lit", rows)
	}
}
