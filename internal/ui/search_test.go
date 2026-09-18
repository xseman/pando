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
			files, _, err := runSearch(context.Background(), root, c.o, e)

			var got []string
			for _, f := range files {
				got = append(got, fmt.Sprintf("%s:%d", f.path, f.count))
			}

			if err != nil || strings.Join(got, " ") != c.want {
				t.Errorf("%s %+v = %q (%v), want %q", e, c.o, got, err, c.want)
			}
		}

		files, _, _ := runSearch(context.Background(), root, searchOpts{query: "hello"}, e)
		if len(files) == 0 || len(files[0].lines) != 2 || !slices.Equal(files[0].lines[1].ranges, [][2]int{{4, 9}, {10, 15}}) || files[0].lines[1].line != 3 {
			t.Errorf("%s: match ranges %+v", e, files)
		}

		if _, _, err := runSearch(context.Background(), root, searchOpts{query: "(", regex: true}, e); err == nil {
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

	m.Update(m.sr.onTick(m, m.sr.gen)())

	out := checkWidths(t, m)
	if !strings.Contains(out, "1 result in 1 file") || !strings.Contains(out, "package x") || !strings.Contains(out, "x.go") {
		t.Fatalf("results:\n%s", out)
	}

	press(m, "enter", "enter") // from the query to the first match, then open it

	if m.pv.kind != pvFile || !strings.HasSuffix(m.pv.path, "x.go") || m.pv.anchor == nil || *m.pv.anchor != (pos{0, 0}) || m.pv.cur != (pos{0, 4}) {
		t.Fatalf("open match: kind=%q path=%q anchor=%v cur=%v", m.pv.kind, m.pv.path, m.pv.anchor, m.pv.cur)
	}

	rc := m.colRect(m.colOf(viewSearch))
	click(m, rc.x+m.sr.toggles(rc.w, &m.sr.query)[2].x+1, m.bodyTop(viewSearch)+1, tea.MouseLeft) // the query row, under the blank one

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
	m.Update(m.sr.onTick(m, m.sr.gen)())
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
		files, _, err := runSearch(context.Background(), root, searchOpts{query: "hello", exclude: "*.txt"}, e)

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

	press(m, "4")

	if out := checkWidths(t, m); !strings.Contains(out, "deep") {
		t.Fatalf("tree view:\n%s", out)
	}
}
