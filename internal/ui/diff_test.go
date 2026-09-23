package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// Ported from herdr-sidebar's diffview tests. No line continuations: they
// would eat the leading space that marks context lines.
const sampleDiff = "diff --git a/src/app.ts b/src/app.ts\n" +
	"index 111..222 100644\n" +
	"--- a/src/app.ts\n" +
	"+++ b/src/app.ts\n" +
	"@@ -1,4 +1,5 @@\n" +
	" import { search } from \"./search\";\n" +
	"-console.log(\"scm-playground up\");\n" +
	"+console.log(\"scm oh yeah\");\n" +
	"+// TODO: debounce input\n" +
	" search(\"hello\");\n"

// diffModel is a w×h model showing sampleDiff as the working-tree diff of
// src/app.ts, focused on it.
func diffModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := testModelSized(t, w, h)
	m.pv = preview{kind: pvDiff, root: m.ws, path: "src/app.ts", entry: git.Entry{Path: "src/app.ts", Letter: 'M'}}
	m.preview, m.focus = true, onMain
	lines, plain, meta, numW := render(pvDiff, "src/app.ts", sampleDiff, true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: sampleDiff, lines: lines, plain: plain, meta: meta, numW: numW})

	return m
}

// bgParams is the SGR parameter text lipgloss emits for background c.
func bgParams(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func fgParams(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestDiffGuttersTintsAndWordRanges(t *testing.T) {
	applyLook("vscode", true, "ascii", nil)

	lines := parseDiff(sampleDiff)

	var kb strings.Builder
	for _, l := range lines {
		kb.WriteByte(l.kind)
	}

	kinds := kb.String()
	if kinds != "cdaac" {
		t.Fatalf("kinds = %q", kinds)
	}

	want := []lineMeta{{1, 1, 'c'}, {2, 0, 'd'}, {0, 2, 'a'}, {0, 3, 'a'}, {3, 4, 'c'}}
	for i, w := range want {
		if lines[i].lineMeta != w {
			t.Errorf("row %d = %+v, want %+v", i, lines[i].lineMeta, w)
		}
	}

	ranges := wordRanges(lines)
	if _, ok := ranges[3]; ok || len(ranges) != 2 {
		t.Fatalf("only the paired del/add get word ranges: %v", ranges)
	}

	styled, plainLines, meta, numW := renderDiff(lines, "src/app.ts", true)
	if plainLines[1] != `console.log("scm-playground up");` || numW != 2 {
		t.Fatalf("plain = %q, numW = %d", plainLines[1], numW)
	}

	if !strings.Contains(styled[1], bgParams(pal.diffDelWordBg)) || !strings.Contains(styled[1], bgParams(pal.diffDelBg)) {
		t.Errorf("deletion lacks row or word tint: %q", styled[1])
	}

	if strings.Contains(styled[3], bgParams(pal.diffAddWordBg)) || !strings.Contains(styled[3], bgParams(pal.diffAddBg)) {
		t.Errorf("unpaired addition must have the row tint only: %q", styled[3])
	}

	if strings.Contains(styled[0], "48;2;") {
		t.Errorf("context rows are not tinted: %q", styled[0])
	}

	if g := diffGutter(meta[0], numW, nil); !strings.HasPrefix(ansi.Strip(g), " 1  1  ") {
		t.Errorf("context gutter = %q", ansi.Strip(g))
	}

	if g := ansi.Strip(diffGutter(meta[1], numW, diffBg('d'))); g != " 2    - " {
		t.Errorf("deletion gutter = %q", g)
	}
}

func TestDiffDeletedDashesKeepGutters(t *testing.T) {
	// A deleted "-- comment" looks like a "--- a/file" header unless the parser is hunk-aware.
	lines := parseDiff("diff --git a/q.sql b/q.sql\nindex 111..222 100644\n--- a/q.sql\n+++ b/q.sql\n" +
		"@@ -1,3 +1,2 @@\n SELECT 1;\n--- trailing comment\n SELECT 2;\n")
	if len(lines) != 3 || lines[1].text != "-- trailing comment" || lines[1].kind != 'd' {
		t.Fatalf("rows = %+v", lines)
	}

	if lines[2].a != 3 || lines[2].b != 2 {
		t.Fatalf("old side must not skip: %+v", lines[2])
	}
}

func TestDiffHunksBinaryAndShow(t *testing.T) {
	if lines := parseDiff("@@ -1,1 +1,1 @@\n ctx\n@@ -9,1 +9,1 @@\n ctx2\n"); len(lines) != 3 || lines[1].kind != 'h' || lines[2].a != 9 {
		t.Fatalf("hunks = %+v", lines)
	}

	if lines := parseDiff("Binary files a/x.bin and b/x.bin differ\n"); len(lines) != 1 || lines[0].kind != 'p' {
		t.Fatalf("binary = %+v", lines)
	}

	show := "commit abc123\nAuthor: t <t@t>\n\n    Fix it\n\n a.go | 1 +\n" +
		"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n x\n+y\n" +
		"diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -3 +3 @@\n-old\n+new\n"
	lines := parseDiff(show)

	var kb strings.Builder
	for _, l := range lines {
		kb.WriteByte(l.kind)
	}

	kinds := kb.String()
	// Indented commit message lines stay plain: they are outside any hunk.
	if kinds != "ppppppfcafda" {
		t.Fatalf("show kinds = %q", kinds)
	}

	if lines[6].text != "a.go" || lines[9].text != "b.go" {
		t.Fatalf("file rows = %q %q", lines[6].text, lines[9].text)
	}

	styled, _, _, _ := renderDiff(lines, "", true)
	if !strings.Contains(styled[0], "commit abc123") || !strings.Contains(styled[11], bgParams(pal.diffAddBg)) {
		t.Fatalf("show render: %q / %q", styled[0], styled[11])
	}
}

// Side by side selects and stages the same lines inline does.
func TestSplitDiffSelection(t *testing.T) {
	applyLook("vscode", true, "ascii", nil)

	m := diffModel(t, 130, 20)
	press(m, "s")

	if !m.pv.split(m) {
		t.Fatal("s shows the diff side by side")
	}

	// Down the rows, then a selection over both changed lines.
	press(m, "down", "shift+down", "shift+down")

	a, b, ok := m.pv.selection()
	if !ok || a.line != 1 || b.line != 3 {
		t.Fatalf("selection = %+v..%+v (%v)", a, b, ok)
	}

	dels, adds := m.pv.changedLines()
	if len(dels) != 1 || !dels[2] || len(adds) != 2 || !adds[2] || !adds[3] {
		t.Fatalf("selected changes: dels=%v adds=%v", dels, adds)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.Contains(m.View().Content, bgParams(pal.textSelBg)) {
		t.Fatal("the selected rows are tinted")
	}

	if !strings.Contains(out[len(out)-2], "stage/revert lines") {
		t.Fatalf("footer = %q", out[len(out)-2])
	}

	if !slices.ContainsFunc(m.pv.items(m), func(it item) bool { return it.label == "Stage Selected Ranges" }) {
		t.Fatal("the menu offers the line actions side by side too")
	}

	// A click picks the row under the mouse; its half decides which line.
	lw := (m.pvW() - 1) / 2
	click(m, m.mainX()+lw+2, 2, tea.MouseLeft)

	if at := m.pv.at(); at.line != 2 || m.pv.anchor != nil {
		t.Fatalf("clicking the new half selects line 2, got %+v", at)
	}

	click(m, m.mainX()+1, 2, tea.MouseLeft)

	if at := m.pv.at(); at.line != 1 {
		t.Fatalf("clicking the old half selects line 1, got %+v", at)
	}
}

func TestOpenFileFromDiff(t *testing.T) {
	m := diffModel(t, 120, 20)
	mustWrite(t, filepath.Join(m.ws, "src", "app.ts"), "a\nb\nc\nd\ne\n")

	// Row 3 is the second addition: new line 3 of the file.
	press(m, "down", "down", "down", "O")

	if m.pv.kind != pvFile || !strings.HasSuffix(m.pv.path, "src/app.ts") || m.pv.cur.line != 2 {
		t.Fatalf("O opens the file at the row's line: kind=%q path=%q line=%d", m.pv.kind, m.pv.path, m.pv.cur.line)
	}
}

func TestHorizontalWheel(t *testing.T) {
	for _, c := range []struct {
		mo   tea.Mouse
		want int
		ok   bool
	}{
		{tea.Mouse{Button: tea.MouseWheelRight}, 8, true},
		{tea.Mouse{Button: tea.MouseWheelLeft}, -8, true},
		{tea.Mouse{Button: tea.MouseWheelDown, Mod: tea.ModShift}, 8, true},
		{tea.Mouse{Button: tea.MouseWheelDown}, 0, false},
	} {
		if got, ok := wheelX(c.mo); got != c.want || ok != c.ok {
			t.Errorf("wheelX(%+v) = %d, %v; want %d, %v", c.mo, got, ok, c.want, c.ok)
		}
	}
}
