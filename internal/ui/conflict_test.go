package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func runes(lines ...string) [][]rune {
	out := make([][]rune, len(lines))
	for i, l := range lines {
		out[i] = []rune(l)
	}

	return out
}

// TestParseConflicts follows VS Code's MergeConflictParser: diff3 ancestors,
// a splitter that must be the whole line, a footer without a splitter, and a
// nested header that stops the scan.
func TestParseConflicts(t *testing.T) {
	cs := parseConflicts(runes("a", "<<<<<<< HEAD", "ours", "||||||| base", "old", "=======", "theirs", ">>>>>>> feat", "b",
		"<<<<<<< x", "======= not a splitter", "=======", ">>>>>>> y"))

	want := []conflict{{start: 1, split: 5, end: 7, ancestors: []int{3}}, {start: 9, split: 11, end: 12}}
	if len(cs) != 2 || cs[0].start != 1 || cs[0].split != 5 || cs[0].end != 7 || !slices.Equal(cs[0].ancestors, []int{3}) || cs[1].start != 9 || cs[1].split != 11 || cs[1].end != 12 {
		t.Fatalf("conflicts = %+v, want %+v", cs, want)
	}

	if cs := parseConflicts(runes("<<<<<<< a", "x", ">>>>>>> a", "<<<<<<< b", "=======", ">>>>>>> b")); len(cs) != 1 || cs[0].start != 3 {
		t.Fatalf("a footer without a splitter drops its block: %+v", cs)
	}

	if cs := parseConflicts(runes("<<<<<<< a", "=======", ">>>>>>> a", "<<<<<<< b", "<<<<<<< c", "=======", ">>>>>>> c")); len(cs) != 1 {
		t.Fatalf("a nested header stops the scan: %+v", cs)
	}
}

// TestMergeConflictEditor is the merge-conflict extension in the editor:
// tinted blocks, the actions on the header, accept by click and by command,
// one undo step, and next / previous.
func TestMergeConflictEditor(t *testing.T) {
	text := "top\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feat\nmid\n<<<<<<< HEAD\n\n=======\nt2\n>>>>>>> feat\nend\n"
	m, _ := editorModel(t, "a.go", text)

	p := &m.pv
	if len(p.conf) != 2 || p.conf[0].start != 1 || p.conf[1].end != 11 {
		t.Fatalf("conflicts = %+v", p.conf)
	}

	out := checkWidths(t, m)
	for _, want := range []string{"(Current Change)", "Accept Current Change | Accept Incoming Change | Accept", "(Incoming Change)", "2 merge conflicts"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}

	for _, c := range []string{bgParams(pal.mergeCurrentHeadBg), bgParams(pal.mergeCurrentBg), bgParams(pal.mergeIncomingBg), bgParams(pal.mergeIncomingHeadBg)} {
		if !strings.Contains(m.View().Content, c) {
			t.Fatalf("missing tint %s", c)
		}
	}

	if !hasCommand(m, "editor.acceptIncomingChange") || !hasCommand(m, "editor.acceptAllBoth") || !hasCommand(m, "editor.nextConflict") {
		t.Fatal("conflict commands in the palette")
	}

	// A click on Accept Incoming Change resolves the first block only.
	_, _, acts := p.conflictSuffix(1)
	row := p.lineRow[1] - p.top
	click(m, m.mainX()+p.gutter()+acts[acceptIncoming].x+1, 1+m.stripH()+row, tea.MouseLeft)

	if got := p.buf.text(); got != "top\ntheirs\nmid\n<<<<<<< HEAD\n\n=======\nt2\n>>>>>>> feat\nend" {
		t.Fatalf("after accept incoming:\n%s", got)
	}

	if len(p.conf) != 1 || p.conf[0].start != 3 {
		t.Fatalf("conflicts rescanned: %+v", p.conf)
	}

	press(m, "ctrl+z")

	if p.buf.text() != strings.TrimSuffix(text, "\n") || len(p.conf) != 2 {
		t.Fatalf("undo restores the conflict: %q %+v", p.buf.text(), p.conf)
	}

	// Accept Current on the second block drops its lone empty line, as VS Code does.
	p.cur = pos{8, 0}

	runCommand(t, m, "editor.acceptCurrentChange")

	if got := p.buf.text(); got != "top\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feat\nmid\nend" {
		t.Fatalf("after accept current on an empty side:\n%s", got)
	}

	press(m, "ctrl+z")

	// Outside a conflict the command says so; next / previous wrap around.
	p.cur = pos{0, 0}

	m.Update(m.pv.acceptAtCursor(m, acceptBoth)())

	if m.msg != "Editor cursor is not within a merge conflict" {
		t.Fatalf("outside a conflict: %q", m.msg)
	}

	runCommand(t, m, "editor.nextConflict")

	if p.cur.line != 1 {
		t.Fatalf("next from the top: line %d", p.cur.line)
	}

	runCommand(t, m, "editor.nextConflict")

	if p.cur.line != 7 {
		t.Fatalf("next: line %d", p.cur.line)
	}

	runCommand(t, m, "editor.nextConflict")

	if p.cur.line != 1 {
		t.Fatalf("next wraps: line %d", p.cur.line)
	}

	runCommand(t, m, "editor.previousConflict")

	if p.cur.line != 7 {
		t.Fatalf("previous wraps: line %d", p.cur.line)
	}

	// Accept All Both keeps both sides of every block in one undo step.
	runCommand(t, m, "editor.acceptAllBoth")

	if got := p.buf.text(); got != "top\nours\ntheirs\nmid\n\nt2\nend" || len(p.conf) != 0 {
		t.Fatalf("after accept all both:\n%s\n%+v", got, p.conf)
	}

	press(m, "ctrl+z")

	if len(p.conf) != 2 {
		t.Fatalf("one undo step: %+v", p.conf)
	}

	if strings.Contains(checkWidths(t, m), "merge conflicts") == false {
		t.Fatal("hint names the conflicts")
	}
}
