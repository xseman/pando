package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// metaOf is diff rows of kinds, numbered as git numbers them.
func metaOf(kinds string) []lineMeta {
	meta, a, b := make([]lineMeta, len(kinds)), 1, 1

	for i, k := range []byte(kinds) {
		meta[i].kind = k

		switch k {
		case 'c':
			meta[i].a, meta[i].b = a, b
			a, b = a+1, b+1

		case 'd':
			meta[i].a, a = a, a+1
		case 'a':
			meta[i].b, b = b, b+1
		case 'f':
			a, b = 1, 1
		}
	}

	return meta
}

// foldedAs writes shownRows back as kinds, a gap as its count in brackets.
func foldedAs(meta []lineMeta, open []gapLines, all bool) string {
	var b strings.Builder

	for _, r := range shownRows(meta, open, all) {
		if r.n > 0 {
			fmt.Fprintf(&b, "[%d]", r.n)
		} else {
			b.WriteByte(meta[r.i].kind)
		}
	}

	return b.String()
}

func TestShownRows(t *testing.T) {
	for _, c := range []struct{ kinds, want string }{
		{"cccccccccc", "[10]"},                         // nothing changed: one gap
		{"cccccccda", "[4]cccda"},                      // before the first change, ctxRows stay
		{"daccccccccc", "daccc[6]"},                    // after the last
		{"daccccccccccda", "daccc[4]cccda"},            // between two
		{"dacccccccda", "dacccccccda"},                 // seven between: a gap of one shows the row
		{"dacccccda", "dacccccda"},                     // six: nothing hidden
		{"pfccccccdafccccccda", "pf[3]cccdaf[3]cccda"}, // a commit: each file on its own
		{"ccccccccdahcccda", "[5]cccdahcccda"},         // git's own gap row stays
	} {
		if got := foldedAs(metaOf(c.kinds), nil, false); got != c.want {
			t.Errorf("%s: shown %s, want %s", c.kinds, got, c.want)
		}
	}

	meta := metaOf("pfccccccdafccccccda")
	if got := foldedAs(meta, []gapLines{{2, 1, 3}}, false); got != "pf[3]cccdafccccccda" {
		t.Errorf("the second file's gap unfolds alone: %s", got)
	}

	if got := foldedAs(meta, nil, true); got != "pfccccccdafccccccda" {
		t.Errorf("all shows every row: %s", got)
	}
}

func TestFoldable(t *testing.T) {
	if !foldable("diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n") || foldable(strings.Repeat("\n", maxPreviewLines+1)) ||
		foldable("commit abc\n\ndiff --cc x.go\n@@@ -1,1 -1,1 +1,1 @@@\n") {
		t.Fatal("a short two-way diff folds; a long one or a merge's combined diff does not")
	}
}

// foldModel shows a 300-line file's diff with changes at lines 100 and 200,
// fetched with full context and folded.
func foldModel(t *testing.T) *Model {
	t.Helper()

	m := testModelSized(t, 160, 30)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	loadDiff(m, pvDiff, longDiff(300, 100, 200))

	return m
}

// gapRows are the meta lines of the view's gap rows.
func gapRows(m *Model) []int {
	var out []int

	for i := range m.pv.meta {
		if m.pv.gaps[i].n > 0 {
			out = append(out, i)
		}
	}

	return out
}

// TestUnfoldGap unfolds one gap in place by ⏎ or a click, keeps it unfolded
// through a reload and a tab coming back, and z shows the whole file then
// folds every gap again.
func TestUnfoldGap(t *testing.T) {
	m := foldModel(t)

	gaps := gapRows(m)
	if len(gaps) != 3 || m.pv.gaps[gaps[0]].n != 97 || m.pv.gaps[gaps[1]].n != 93 || m.pv.gaps[gaps[2]].n != 96 {
		t.Fatalf("gaps %v of %v", gaps, m.pv.gaps)
	}

	out := checkWidths(t, m)
	if !strings.Contains(out, "97 hidden lines") {
		t.Fatalf("the gap shows its count:\n%s", out)
	}

	m.pv.setLine(m, gaps[1])

	if !strings.Contains(ansi.Strip(m.View().Content), "⏎ show 93 hidden lines") {
		t.Fatal("on a gap, the footer says what ⏎ does")
	}

	rows := len(m.pv.meta)
	press(m, "enter")

	if len(m.pv.meta) != rows+92 || m.pv.gaps[gaps[1]].n != 0 || m.pv.meta[gaps[1]] != (lineMeta{105, 105, 'c'}) || m.pv.at().line != gaps[1] {
		t.Fatalf("⏎ unfolds the gap in place: %d rows, cursor %d on %+v", len(m.pv.meta), m.pv.at().line, m.pv.meta[m.pv.at().line])
	}

	// The 2 s reload and a snapshot of the tab keep it unfolded.
	m.pv.reloadIfLive(m)
	loadDiff(m, pvDiff, longDiff(300, 100, 200))

	if len(gapRows(m)) != 2 || m.pv.snapshot().open == nil {
		t.Fatalf("a reload keeps the gap unfolded: gaps %v", gapRows(m))
	}

	// A click on the first gap unfolds it too.
	x, y := m.mainX()+m.pv.gutter()+3, 1+m.stripH()+gapRows(m)[0]-m.pv.top
	click(m, x, y, tea.MouseLeft)

	if len(gapRows(m)) != 1 {
		t.Fatalf("a click unfolds the gap under it: gaps %v", gapRows(m))
	}

	press(m, "z")

	if !m.st.Settings.DiffFull || len(m.pv.meta) != 302 || len(gapRows(m)) != 0 {
		t.Fatalf("z shows the whole file at once: %d rows, gaps %v", len(m.pv.meta), gapRows(m))
	}

	m.pv.setLine(m, findPlace(m.pv.meta, lineMeta{b: 150}))
	press(m, "z")

	if m.st.Settings.DiffFull || len(gapRows(m)) != 3 || m.pv.open != nil {
		t.Fatalf("z again folds every gap: gaps %v, open %v", gapRows(m), m.pv.open)
	}

	if at := m.pv.at().line; at != gapRows(m)[1] {
		t.Fatalf("the cursor's hidden line leaves it on the gap hiding it: row %d, gaps %v", at, gapRows(m))
	}

	checkWidths(t, m)
}

// TestUnfoldGapSplit: a click on a gap side by side unfolds it, its label in
// the middle of the row.
func TestUnfoldGapSplit(t *testing.T) {
	m := foldModel(t)
	m.st.Settings.DiffView = "split"

	if out := checkWidths(t, m); !strings.Contains(out, "⋯ 97 hidden lines") {
		t.Fatalf("the gap's label side by side:\n%s", out)
	}

	click(m, m.mainX()+20, 1+m.stripH(), tea.MouseLeft)

	if len(gapRows(m)) != 2 {
		t.Fatalf("a click side by side unfolds the gap: gaps %v", gapRows(m))
	}
}

// TestGotoLineUnfolds: Go to Line to a line hidden in a gap unfolds it and
// lands there.
func TestGotoLineUnfolds(t *testing.T) {
	m := foldModel(t)

	_ = m.gotoLineQuery(":150")
	press(m, "enter")

	if l := m.pv.meta[m.pv.at().line]; l.b != 150 || len(gapRows(m)) != 2 {
		t.Fatalf("Go to Line 150 went to %+v, gaps %v", l, gapRows(m))
	}
}

// TestFixedDiffKeepsGitsGaps: a diff grown too long to fold, fetched with
// git's three lines, keeps git's gap rows, which do not unfold, and none of
// the gaps folded before.
func TestFixedDiffKeepsGitsGaps(t *testing.T) {
	m := foldModel(t) // folded first: the gaps of that load go

	raw := compactDiff(300, 100, 200)
	lines, plain, meta, numW := render(pvDiff, "f.go", raw, true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: raw, lines: lines, plain: plain, meta: meta, numW: numW, fixed: true})

	if !m.pv.fixed || m.pv.unfolds() || len(m.pv.meta) != len(meta) || len(gapRows(m)) != 0 {
		t.Fatalf("a fixed diff shows git's rows: fixed %v, %d rows of %d", m.pv.fixed, len(m.pv.meta), len(meta))
	}

	gap := -1

	for i, l := range m.pv.meta {
		if l.kind == 'h' {
			gap = i
		}
	}

	m.pv.setLine(m, gap)
	press(m, "enter")

	if len(m.pv.meta) != len(meta) {
		t.Fatal("git's gap does not unfold")
	}
}

// TestUnfoldedGapKeepsOpen: an agent's change inside an unfolded gap leaves
// the rest of it unfolded, as VS Code keeps what was revealed.
func TestUnfoldedGapKeepsOpen(t *testing.T) {
	m := foldModel(t)
	m.pv.setLine(m, gapRows(m)[1]) // between the changes at 100 and 200
	press(m, "enter")

	m.pv.reloadIfLive(m)
	loadDiff(m, pvDiff, longDiff(300, 100, 150, 200)) // the agent changed line 150

	if len(gapRows(m)) != 2 {
		t.Fatalf("both halves stay unfolded: gaps %v", gapRows(m))
	}
}

// TestFindInGap finds in the rows a gap hides, as VS Code's find does in
// collapsed regions: they count, and the match selected unfolds its gap.
func TestFindInGap(t *testing.T) {
	m := foldModel(t)

	press(m, "ctrl+f")
	typeText(m, "line 150")

	if m.pv.findStatus() != "1 of 1" || len(gapRows(m)) != 2 {
		t.Fatalf("the hidden match counts and its gap unfolds: %q, gaps %v", m.pv.findStatus(), gapRows(m))
	}

	if c := m.pv.at(); string(m.pv.plain[c.line]) != "line 150" || m.pv.anchor == nil {
		t.Fatalf("the match is selected: %+v on %q", c, string(m.pv.plain[c.line]))
	}

	checkWidths(t, m)
}

// TestCopyOverGap copies the rows a gap hides with a selection over it, and
// counts them in the status bar's selection.
func TestCopyOverGap(t *testing.T) {
	m := foldModel(t)

	gap := gapRows(m)[1]
	m.pv.setLine(m, gap-1)
	press(m, "shift+down", "shift+down")

	got := m.pv.selectedText()
	if !strings.Contains(got, "line 103\nline 104\n") || !strings.HasSuffix(got, "line 196\n") || strings.Count(got, "\n") != 94 {
		t.Fatalf("the hidden rows are copied:\n%s", got)
	}

	if n := m.pv.selCount(); n != len([]rune(got)) {
		t.Fatalf("the count holds them too: %d for %d", n, len([]rune(got)))
	}
}
