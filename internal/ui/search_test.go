package ui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSearchEngines(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "Hello world\nnothing\nsay hello hello\n")
	mustWrite(t, filepath.Join(root, "sub", "b.go"), "\thello()\n")
	mustGit(t, root, "init", "-q")

	engines := []string{"git", "grep"}
	if _, err := exec.LookPath("rg"); err == nil {
		engines = append(engines, "rg")
	}

	for _, e := range engines {
		for _, c := range []struct {
			o    searchOpts
			want string
		}{
			{searchOpts{query: "hello"}, "a.txt:3 sub/b.go:1"},
			{searchOpts{query: "hello", caseSens: true}, "a.txt:2 sub/b.go:1"},
			{searchOpts{query: "hell", word: true}, ""},
			{searchOpts{query: `hel+o\(`, regex: true}, "sub/b.go:1"},
			{searchOpts{query: "("}, "sub/b.go:1"},
			{searchOpts{query: "hello", include: "*.go"}, "sub/b.go:1"},
		} {
			files, _, err := runSearch(context.Background(), root, c.o, e, nil)

			var got []string
			for _, f := range files {
				got = append(got, fmt.Sprintf("%s:%d", f.path, f.count))
			}

			if err != nil || strings.Join(got, " ") != c.want {
				t.Errorf("%s %+v = %q (%v), want %q", e, c.o, got, err, c.want)
			}
		}

		files, _, _ := runSearch(context.Background(), root, searchOpts{query: "hello"}, e, nil)
		if len(files) == 0 || len(files[0].lines) != 2 || !slices.Equal(files[0].lines[1].ranges, [][2]int{{4, 9}, {10, 15}}) || files[0].lines[1].line != 3 {
			t.Errorf("%s: match ranges %+v", e, files)
		}

		if _, _, err := runSearch(context.Background(), root, searchOpts{query: "(", regex: true}, e, nil); err == nil {
			t.Errorf("%s: an invalid regex reports an error", e)
		}
	}
}

func TestSearchView(t *testing.T) {
	m := testModel(t)
	press(m, "4")

	if v, _ := m.viewOn(m.colOf(viewSearch)); v != viewSearch || !m.sr.query.Focused() {
		t.Fatal("4 opens Search with the caret in the query")
	}

	press(m, "p", "a", "c", "k")

	if !m.sr.busy {
		t.Fatal("typing schedules a search")
	}

	searchNow(m)

	out := checkWidths(t, m)
	if !strings.Contains(out, "1 result in 1 file") || !strings.Contains(out, "package x") || !strings.Contains(out, "x.go") {
		t.Fatalf("results:\n%s", out)
	}

	press(m, "enter", "enter") // from the query to the first match, then open it

	if m.pv.kind != pvFile || !strings.HasSuffix(m.pv.path, "x.go") || m.pv.anchor == nil || *m.pv.anchor != (pos{0, 0}) || m.pv.cur != (pos{0, 4}) {
		t.Fatalf("open match: kind=%q path=%q anchor=%v cur=%v", m.pv.kind, m.pv.path, m.pv.anchor, m.pv.cur)
	}

	rc := m.colRect(m.colOf(viewSearch))
	click(m, rc.x+m.sr.toggles(rc.w, &m.sr.query)[2].x+1, m.bodyTop(viewSearch), tea.MouseLeft) // the query row, the first

	if !m.sr.regex || !m.sr.busy {
		t.Fatal("clicking .* turns on regex and searches again")
	}

	checkWidths(t, m)
}

func TestReplaceLine(t *testing.T) {
	re := matcher(searchOpts{query: `([a-z])(\d)`, regex: true, caseSens: true})
	if out, n := replaceLine("a1 b2", re, "$2$1", true, false); out != "1a 2b" || n != 2 {
		t.Fatalf("regex replace = %q (%d)", out, n)
	}

	if out, n := replaceLine("a1 b2", re, "$2", false, false); out != "$2 $2" || n != 2 {
		t.Fatalf("literal replace = %q (%d)", out, n)
	}
}

func TestSearchReplace(t *testing.T) {
	m := testModel(t)
	path := filepath.Join(m.ws, "a.txt")
	mustWrite(t, path, "foo bar\nfoo foo\n")
	press(m, "4")
	press(m, "f", "o", "o")
	searchNow(m)
	press(m, "ctrl+h")

	if !m.sr.replace.Focused() {
		t.Fatal("ctrl+h opens the replace box")
	}

	press(m, "b", "a", "z")

	if out := checkWidths(t, m); !strings.Contains(out, "baz") {
		t.Fatalf("replace preview:\n%s", out)
	}

	press(m, "enter", "up") // to the file header
	send(m, keyMsg("r"))

	if b := mustRead(t, path); b != "baz bar\nbaz baz\n" {
		t.Fatalf("after replace: %q", b)
	}
	// A file that changed since the search is refused, not guessed at.
	mustWrite(t, path, "nothing\n")

	f := searchFile{path: "a.txt", lines: []searchLine{{line: 1, text: "foo bar"}}}
	if _, err := replaceFile(path, f, matcher(searchOpts{query: "foo"}), "x", false, false); err == nil {
		t.Fatal("a changed file must be refused")
	}
}

func TestPreserveCase(t *testing.T) {
	re := matcher(searchOpts{query: "foo"})
	for in, want := range map[string]string{"foo bar": "baz bar", "FOO bar": "BAZ bar", "Foo bar": "Baz bar"} {
		if out, _ := replaceLine(in, re, "baz", false, true); out != want {
			t.Errorf("replace %q = %q, want %q", in, out, want)
		}
	}
}

func TestSearchTreeAndExclude(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src", "deep"))
	mustWrite(t, filepath.Join(root, "src", "a.go"), "hello\n")
	mustWrite(t, filepath.Join(root, "src", "deep", "b.go"), "hello\n")
	mustWrite(t, filepath.Join(root, "skip.txt"), "hello\n")
	mustGit(t, root, "init", "-q")

	for _, e := range []string{"git", "grep"} {
		files, _, err := runSearch(context.Background(), root, searchOpts{query: "hello", exclude: "*.txt"}, e, nil)

		var got []string
		for _, f := range files {
			got = append(got, f.path)
		}

		if err != nil || !slices.Equal(got, []string{"src/a.go", "src/deep/b.go"}) {
			t.Errorf("%s exclude: %v %v", e, got, err)
		}
	}

	m := testModel(t)
	m.sr.ws = root
	m.sr.files = []searchFile{
		{path: "src/a.go", count: 1, lines: []searchLine{{line: 1, text: "hello"}}},
		{path: "src/deep/b.go", count: 2, lines: []searchLine{{line: 1, text: "hello"}}},
	}
	m.sr.tree = true

	var labels []string

	for _, r := range m.sr.rows() {
		switch {
		case r.file < 0:
			labels = append(labels, r.label+strconv.Itoa(r.count))
		case r.line < 0:
			labels = append(labels, filepath.Base(m.sr.files[r.file].path))
		}
	}

	if !slices.Equal(labels, []string{"src3", "deep2", "b.go", "a.go"}) {
		t.Fatalf("tree rows = %v", labels)
	}
	// Indent guides run under each open folder's and file's chevron; b.go's
	// is lit while its line is selected.
	m.sr.l.sel = 3

	lines := m.sr.lines(m, 40, 30)
	for name, want := range map[string]struct {
		lead string
		lit  int
	}{"src": {" ▾ ", -1}, "deep": {" │ ▾ ", -1}, "b.go": {" │ │ ▾ ", -1}, "hello": {" │ │ │   ", 2}, "a.go": {" │ ▾ ", -1}} {
		if got, lit := treeLead(t, lines, name); !strings.HasPrefix(got, want.lead) || strings.Count(got, "│") != strings.Count(want.lead, "│") || lit != want.lit {
			t.Errorf("%s: %q lit %d, want %q lit %d", name, got, lit, want.lead, want.lit)
		}
	}

	press(m, "4")

	if out := checkWidths(t, m); !strings.Contains(out, "deep") {
		t.Fatalf("tree view:\n%s", out)
	}
}

// TestSearchPage pages the results with pgup pgdn, scrolling the list.
func TestSearchPage(t *testing.T) {
	m := testModelSized(t, 100, 12)
	for i := range 20 {
		m.sr.files = append(m.sr.files, searchFile{path: fmt.Sprintf("f%02d.go", i), count: 1, lines: []searchLine{{line: 1, text: "hello"}}})
	}

	m.sr.l.sel = 0
	m.sr.key(m, tea.KeyPressMsg{Code: tea.KeyPgDown})

	if m.sr.l.sel == 0 || m.sr.l.top == 0 {
		t.Fatalf("pgdown: sel %d, top %d", m.sr.l.sel, m.sr.l.top)
	}

	m.sr.key(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	m.sr.key(m, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if m.sr.l.sel != 0 || m.sr.l.top != 0 {
		t.Fatalf("pgup back to the top: sel %d, top %d", m.sr.l.sel, m.sr.l.top)
	}
}

// TestSearchPreserveCaseKey toggles Preserve Case with alt+p while a box of
// Search's has the keyboard, as its button's tip says.
func TestSearchPreserveCaseKey(t *testing.T) {
	m := testModel(t)
	press(m, "4", "ctrl+h", "alt+p")

	if !m.sr.editing() || !m.sr.preserveCase {
		t.Fatalf("alt+p in the query box: editing %v, preserve case %v", m.sr.editing(), m.sr.preserveCase)
	}
}

// searchNow runs the search the query waits for and hands Update every
// batch, the last included.
func searchNow(m *Model) {
	cmd := m.sr.onTick(m, m.sr.gen)
	for {
		msg, _ := cmd().(searchMsg)
		m.Update(msg)

		if !msg.partial {
			return
		}

		cmd = waitSearch(msg.ch)
	}
}

// runSearch hands over what it found so far while the engine runs, sorted
// as the end result is, each batch a part of the next, the last one whole.
func TestSearchBatches(t *testing.T) {
	root := t.TempDir()
	for i := range 400 {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("d%03d", i/50), fmt.Sprintf("f%03d.txt", i)), strings.Repeat("needle\n", 3)) // 1200 matches, under maxSearchMatches
	}

	var batches [][]searchFile

	files, _, err := runSearch(context.Background(), root, searchOpts{query: "needle"}, "grep", func(fs []searchFile) { batches = append(batches, fs) })
	if err != nil || len(files) != 400 {
		t.Fatalf("%d files (%v)", len(files), err)
	}

	if len(batches) == 0 || len(batches[0]) == 0 {
		t.Fatal("the first match makes no batch")
	}

	for _, b := range batches {
		if !slices.IsSortedFunc(b, func(a, c searchFile) int { return strings.Compare(a.path, c.path) }) || len(b) > len(files) {
			t.Fatalf("a batch of %d, unsorted or too many", len(b))
		}
	}
}

// A batch while the search runs keeps the selection on its line and the
// search busy; the last one ends it.
func TestSearchKeepsSelection(t *testing.T) {
	m := testModel(t)
	press(m, "4", "x")

	gen, ch := m.sr.gen, make(chan searchMsg, 1)
	b := searchFile{path: "b.go", count: 1, lines: []searchLine{{line: 7, text: "x"}}}
	c := searchFile{path: "c.go", count: 1, lines: []searchLine{{line: 3, text: "x"}}}

	m.Update(searchMsg{gen: gen, files: []searchFile{c}, partial: true, ch: ch})
	m.sr.l.sel = 1 // c.go's line 3

	a := searchFile{path: "a.go", count: 1, lines: []searchLine{{line: 1, text: "x"}}}
	if _, cmd := m.Update(searchMsg{gen: gen, files: []searchFile{a, b, c}, partial: true, ch: ch}); cmd == nil || !m.sr.busy {
		t.Fatal("a batch while the search runs stops waiting")
	}

	if r := m.sr.rows()[m.sr.l.sel]; m.sr.files[r.file].path != "c.go" || r.line != 0 {
		t.Fatalf("the selection moved to %+v", r)
	}

	m.Update(searchMsg{gen: gen, files: []searchFile{a, b, c}, ch: ch})

	if m.sr.busy || !strings.Contains(checkWidths(t, m), "3: x") {
		t.Fatalf("the last batch: busy %v\n%s", m.sr.busy, checkWidths(t, m))
	}
}

// The rule under the boxes shows the files to include and exclude; the
// summary's button turns the list into a tree; a match shows its line.
func TestSearchHeader(t *testing.T) {
	m := testModel(t)
	press(m, "4", "p", "a", "c", "k")
	searchNow(m)

	rc, top := m.colRect(m.colOf(viewSearch)), m.bodyTop(viewSearch)

	if out := checkWidths(t, m); !strings.Contains(out, "❱ pack") || !strings.Contains(out, "1: package x") {
		t.Fatalf("header and match:\n%s", out)
	}

	click(m, rc.x+rc.w-2, top+m.sr.ruleY(), tea.MouseLeft)

	if out := checkWidths(t, m); !m.sr.showDetails || !strings.Contains(out, "include ❱") || !strings.Contains(out, "exclude ❱") {
		t.Fatalf("the rule's ▾ opens include and exclude:\n%s", out)
	}

	click(m, rc.x+m.sr.ruleY()+12, top+m.sr.ruleY()+1, tea.MouseLeft)

	if !m.sr.include.Focused() {
		t.Fatal("a click on include gives it the keyboard")
	}

	click(m, rc.x+rc.w-3, top+m.sr.headH()-2, tea.MouseLeft)

	if !m.sr.tree {
		t.Fatal("the summary's button turns the list into a tree")
	}
}
