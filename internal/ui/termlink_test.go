package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// linkText is a link as the corpus writes it: the text it covers, then its
// path or URL and its line and column.
func linkText(s string, l termLink) string {
	text := ansi.Cut(s, l.from, l.to)
	switch {
	case l.url != "" && l.line > 0:
		return fmt.Sprintf("%s → %s %d:%d", text, l.url, l.line, l.col)
	case l.url != "":
		return text + " → " + l.url
	}

	return fmt.Sprintf("%s → %s %d:%d", text, l.path, l.line, l.col)
}

// TestTextLinks is what the terminals' tools print: each line and the links
// VS Code's terminal finds in it.
func TestTextLinks(t *testing.T) {
	for _, c := range []struct {
		line string
		want []string
	}{
		{"    app_test.go:42: expected 1, got 2", []string{"app_test.go:42 → app_test.go 42:0"}},
		{"internal/ui/app.go:123:45: undefined: foo", []string{"internal/ui/app.go:123:45 → internal/ui/app.go 123:45"}},
		{"src/app.ts(12,5): error TS2304: Cannot find name 'x'.", []string{"src/app.ts(12,5) → src/app.ts 12:5"}},
		{"src/app.ts:12:5 - error TS2304", []string{"src/app.ts:12:5 → src/app.ts 12:5"}},
		{`  File "/usr/lib/python3.12/json/__init__.py", line 293, in load`, []string{`/usr/lib/python3.12/json/__init__.py", line 293 → /usr/lib/python3.12/json/__init__.py 293:0`}},
		{`  File "/run/x/dir with space/x y.py", line 12, in f`, []string{`/run/x/dir with space/x y.py", line 12 → /run/x/dir with space/x y.py 12:0`}},
		{`"src/a b.ts":3:4 - error`, []string{`src/a b.ts":3:4 → src/a b.ts 3:4`}},
		{`echo "edited a.go and b c"`, []string{"a.go → a.go 0:0"}},
		{"internal/ui/app.go:123:\treturn x", []string{"internal/ui/app.go:123 → internal/ui/app.go 123:0"}},
		{"⏺ Update(internal/ui/termlink.go)", []string{"internal/ui/termlink.go → internal/ui/termlink.go 0:0"}},
		{"• Edited internal/ui/app.go (+12 -3)", []string{"internal/ui/app.go → internal/ui/app.go 0:0"}},
		{"+++ b/internal/ui/app.go", []string{"b/internal/ui/app.go → b/internal/ui/app.go 0:0"}},
		{"--- a/x.go", []string{"a/x.go → a/x.go 0:0"}},
		{"see https://github.com/xseman/pando/pull/12.", []string{"https://github.com/xseman/pando/pull/12 → https://github.com/xseman/pando/pull/12"}},
		{"(https://en.wikipedia.org/wiki/Pando_(tree))", []string{"https://en.wikipedia.org/wiki/Pando_(tree) → https://en.wikipedia.org/wiki/Pando_(tree)"}},
		{"serving on http://localhost:3000/", []string{"http://localhost:3000/ → http://localhost:3000/"}},
		{"[app.go](internal/ui/app.go#L12)", []string{"app.go → app.go 0:0", "internal/ui/app.go#L12 → internal/ui/app.go 12:0"}},
		{`C:\Users\me\x.go:12:3: oops`, []string{`C:\Users\me\x.go:12:3 → C:\Users\me\x.go 12:3`}},
		{"  --> src/main.rs:2:5", []string{"src/main.rs:2:5 → src/main.rs 2:5"}},
		{"    at Object.<anonymous> (/home/u/x.js:10:15)", []string{"/home/u/x.js:10:15 → /home/u/x.js 10:15"}},
		{"Makefile:12: *** missing separator.  Stop.", []string{"Makefile:12 → Makefile 12:0"}},
		{"edited README.md: two lines", []string{"README.md → README.md 0:0"}},
		{"日本 src/a.go:1 ok", []string{"src/a.go:1 → src/a.go 1:0"}},
		{"src/u\u0308ber.go:3 decomposed", []string{"src/u\u0308ber.go:3 → src/u\u0308ber.go 3:0"}},
		{"~/notes/todo.md and ../up.txt", []string{"~/notes/todo.md → ~/notes/todo.md 0:0", "../up.txt → ../up.txt 0:0"}},
		{"open file:///tmp/x.txt now", []string{"file:///tmp/x.txt → file:///tmp/x.txt"}},
		{"    at file:///home/u/a.mjs:12:5", []string{"file:///home/u/a.mjs:12:5 → file:///home/u/a.mjs 12:5"}},
		{"listening on port:8080, 127.0.0.1:80, line:12 done", []string{"port:8080 → port 8080:0", "line:12 → line 12:0"}}, // no such files: linkAt drops them
		{"--- ... +++ 1.5 2/3", nil},
	} {
		var got []string
		for _, l := range textLinks(c.line) {
			got = append(got, linkText(c.line, l))
		}

		if !slices.Equal(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.line, got, c.want)
		}
	}
}

// TestOSC8Links takes a hyperlink's run of cells as one link, ahead of the
// text links it covers.
func TestOSC8Links(t *testing.T) {
	line := "go \x1b[1m" + ansi.SetHyperlink("file:///repo/internal/ui/app.go") + "app.go:12" + ansi.ResetHyperlink() + "\x1b[m and " +
		ansi.SetHyperlink("https://pando.dev", "id=1") + "日本" + ansi.ResetHyperlink() + " src/a.go:3"

	var got []string
	for _, l := range termLinks(line) {
		got = append(got, linkText(ansi.Strip(line), l))
	}

	want := []string{"app.go:12 → file:///repo/internal/ui/app.go", "日本 → https://pando.dev", "src/a.go:3 → src/a.go 3:0"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func FuzzTermLinks(f *testing.F) {
	for _, s := range []string{
		"internal/ui/app.go:12:5: x", "src/a.ts(1, 2)", `"x.py", line 3, column 4`, "see https://a.b/c).", "日本 a/b.go:1",
		"\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\ a.go:1", "\x1b[31mred/x.go\x1b[m:2", "C:\\a\\b.go:1:1", "a.go#L3C4",
	} {
		f.Add(s)
	}
	// Links lie inside the line, in order, apart; a path's link covers its
	// path and then its position, a URL's its URL.
	f.Fuzz(func(t *testing.T, s string) {
		plain := ansi.Strip(s)
		// The emulator renders text it decoded: only that has to line up, and
		// only where widths add up: two runes one grapheme (a mark, a joiner,
		// U+06DD) can straddle a link's edge, which counts cells by the width
		// of the text before it (textLinks).
		joined := func(s string) bool {
			rs := []rune(s)
			for i := 1; i < len(rs); i++ {
				if ansi.StringWidth(string(rs[i-1:i+1])) != ansi.StringWidth(string(rs[i-1]))+ansi.StringWidth(string(rs[i])) {
					return true
				}
			}

			return false
		}

		if !utf8.ValidString(s) || joined(plain) {
			termLinks(s)
			return
		}

		for _, ls := range [][]termLink{textLinks(plain), termLinks(s)} {
			at := 0

			for _, l := range ls {
				if l.from < at || l.to <= l.from || l.to > ansi.StringWidth(plain) || l.line < 0 || l.col < 0 {
					t.Fatalf("%q: link %+v out of place (after %d, width %d)", s, l, at, ansi.StringWidth(plain))
				}

				at = l.to
			}
		}

		visible := func(s string) string { // what takes cells: a cut carries the zero-width runes after its end along
			return strings.Map(func(r rune) rune { return ifElse(ansi.StringWidth(string(r)) == 0, -1, r) }, s)
		}

		// A link covers its path or URL, from its own cell, and its position
		// after it; a grapheme across its edge comes along whole.
		for _, l := range textLinks(plain) {
			text, target := visible(ansi.Cut(plain, l.from, l.to)), visible(l.url+l.path)

			i := strings.Index(text, target)
			if i < 0 || ansi.StringWidth(text[:i]) > 1 || (l.line > 0) != (i+len(target) < len(text)) {
				t.Fatalf("%q: %q at line %d covers %q", plain, l.url+l.path, l.line, text)
			}
		}
	})
}

// TestTerminalLinks underlines the link under the pointer with VS Code's tip,
// and a ctrl+click opens it: a file of the session's worktree at its line
// and column, a path printed from elsewhere through the one file it names.
func TestTerminalLinks(t *testing.T) {
	m := testModel(t)
	drainInputs(m)
	m.Update(focusSessionMsg("s1"))

	h := m.sessH()
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: make([]string, h)}
	m.term.scr.Lines[1] = "\x1b[31mok\x1b[m see src/deep/x.go:1:3 now"
	m.term.scr.Lines[2] = "--- FAIL: x_test.go:1: boom"
	m.term.scr.Lines[3] = "port:8080 and/or nowhere/gone.go done"

	at := func(text string) (int, int) { // the screen cell where text starts
		for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if i := strings.Index(l, text); i >= 0 {
				return ansi.StringWidth(l[:i]), y
			}
		}

		t.Fatalf("%q is not on screen", text)

		return 0, 0
	}

	x, y := at("src/deep/x.go")
	m.Update(tea.MouseMotionMsg{X: x + 3, Y: y})
	elapse(m, time.Second)

	out := m.View().Content
	if !strings.Contains(out, "\x1b[0;4msrc/deep/x.go:1:3") || !strings.Contains(ansi.Strip(out), "Open File in Editor") {
		t.Fatalf("the link under the pointer is underlined, with its tip:\n%q", out)
	}

	m.Update(tea.MouseMotionMsg{X: x - 3, Y: y})

	if strings.Contains(m.View().Content, "\x1b[0;4m") {
		t.Fatal("off the link it is drawn as before")
	}

	for _, c := range []struct {
		text   string
		search bool
	}{{"port:8080", false}, {"and/or", false}, {"nowhere/gone.go", true}} {
		gx, gy := at(c.text)
		if h := m.term.linkAt(m, "s1", gx-m.mainX(), gy-(y-1)); (h != nil) != c.search || h != nil && !h.search {
			t.Fatalf("%q: link %+v; want a search %v (a file's path, not a word)", c.text, h, c.search)
		}
	}

	m.Update(tea.MouseClickMsg{X: x + 3, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if m.pv.kind != pvFile || m.pv.path != filepath.Join(m.ws, "src", "deep", "x.go") || m.pv.cur != (pos{0, 2}) {
		t.Fatalf("ctrl+click opened %s %q at %+v", m.pv.kind, m.pv.path, m.pv.cur)
	}

	m.Update(focusSessionMsg("s1"))

	x, y = at("x_test.go")
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if cmd == nil {
		t.Fatal("a path printed from a subdirectory is looked for")
	}

	mustWrite(t, filepath.Join(m.ws, "src", "deep", "x_test.go"), "package x\n")
	m.Update(cmd())

	if m.pv.path != filepath.Join(m.ws, "src", "deep", "x_test.go") {
		t.Fatalf("the one file it names opens: %q", m.pv.path)
	}

	// A path with a directory but no line, printed from elsewhere (rg -l, a
	// git status from a subdirectory), is looked for too.
	m.Update(focusSessionMsg("s1"))
	m.term.scr.Lines[6] = "modified: ../../deep/x_test.go"

	x, y = at("../../deep/x_test.go")
	if _, cmd = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("a path with a directory is looked for")
	}

	m.pv = preview{}
	m.Update(cmd())

	if m.pv.path != filepath.Join(m.ws, "src", "deep", "x_test.go") {
		t.Fatalf("the one file it names opens: %q", m.pv.path)
	}

	// A path the terminal wrapped is one link, on every row it covers.
	m.Update(focusSessionMsg("s1"))

	long := "  at " + filepath.Join(m.ws, "src", "deep", "x.go") + ":1:2 in main"
	m.term.scr = proto.Screen{Lines: make([]string, h), Cols: 24}

	for i := 0; i*24 < len(long); i++ {
		m.term.scr.Lines[4+i] = long[i*24 : min((i+1)*24, len(long))]
	}

	x, y = at(m.term.scr.Lines[5])
	m.Update(tea.MouseMotionMsg{X: x + 2, Y: y})

	if out := m.View().Content; strings.Count(out, "\x1b[0;4m") < 3 || !strings.Contains(out, "\x1b[0;4m"+m.term.scr.Lines[5]) {
		t.Fatalf("the wrapped path is underlined on each of its rows:\n%q", out)
	}

	m.Update(tea.MouseClickMsg{X: x + 2, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if m.pv.path != filepath.Join(m.ws, "src", "deep", "x.go") || m.pv.cur != (pos{0, 1}) {
		t.Fatalf("ctrl+click on the wrapped path opened %q at %+v", m.pv.path, m.pv.cur)
	}
}

// TestTerminalLinkCwd resolves a relative path in the shell's directory
// first, where it was printed, then in the worktree; and a ctrl+click in an
// app that takes the mouse is pando's, its release too.
func TestTerminalLinkCwd(t *testing.T) {
	m := testModel(t)
	m.Update(focusSessionMsg("s1"))

	mustWrite(t, filepath.Join(m.ws, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(m.ws, "sub", "main.go"), "package sub\n\nfunc f() {}\n")

	h := m.sessH()
	m.term.id = "s1"
	m.term.scr = proto.Screen{Lines: make([]string, h), Cwd: filepath.Join(m.ws, "sub"), Mouse: true}
	m.term.scr.Lines[2] = "main.go:3:1: undefined: foo"

	var x, y int

	for i, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if j := strings.Index(l, "main.go:3"); j >= 0 {
			x, y = ansi.StringWidth(l[:j]), i
		}
	}

	m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	m.Update(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if m.pv.path != filepath.Join(m.ws, "sub", "main.go") || m.pv.cur.line != 2 {
		t.Fatalf("the shell's directory first: opened %q at %+v", m.pv.path, m.pv.cur)
	}

	m.Update(focusSessionMsg("s1"))
	m.term.scr.Cwd = "" // where the system does not say: the worktree's

	if l := m.term.linkAt(m, "s1", x+1-m.mainX(), 2); l == nil || l.target != filepath.Join(m.ws, "main.go") {
		t.Fatalf("without the shell's directory, the worktree's file: %+v", l)
	}

	// A compiler's column counts a tab as one character.
	m.Update(focusSessionMsg("s1"))
	mustWrite(t, filepath.Join(m.ws, "sub", "t.go"), "package sub\n\nfunc f() {\n\t\t_ = 1\n}\n")
	m.term.scr.Cwd, m.term.scr.Lines[2] = filepath.Join(m.ws, "sub"), "t.go:4:3: declared and not used"
	m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	m.Update(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if m.pv.path != filepath.Join(m.ws, "sub", "t.go") || m.pv.cur != (pos{3, 2 * tabW}) {
		t.Fatalf("t.go:4:3 opened %q at %+v, want the cell after two tabs", m.pv.path, m.pv.cur)
	}

	// A search leaves the session where it is, under the release.
	m.Update(focusSessionMsg("s1"))
	m.term.scr.Lines[2] = "other/main.go:3:1: undefined: foo"
	m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	m.Update(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})

	if !m.showsSession() {
		t.Fatal("the search keeps the session on screen")
	}

	select {
	case in := <-m.inputs:
		t.Fatalf("the app got the ctrl+click's %+v", in.Mouse)
	default:
	}

	// A release that went elsewhere (to the editor a link opened) does not
	// cost the app the next click's.
	m.term.followed = true
	m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft})

	if in := sent(t, m); in.Mouse == nil || in.Mouse.Kind != "click" {
		t.Fatalf("the plain click is the app's: %+v", in.Mouse)
	}

	if in := sent(t, m); in.Mouse == nil || in.Mouse.Kind != "release" {
		t.Fatalf("and so is its release: %+v", in.Mouse)
	}
}
