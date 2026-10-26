package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
)

func TestCommentOf(t *testing.T) {
	slash, hash, dash := commentSyntax{line: "//"}, commentSyntax{line: "#"}, commentSyntax{line: "--"}
	html := commentSyntax{open: "<!--", close: "-->"}

	for _, tc := range []struct {
		path, text string
		want       commentSyntax
		ok         bool
	}{
		{"main.go", "", slash, true},
		{"go.mod", "", slash, true},        // AMPL's *.mod to chroma
		{"Program.fs", "", slash, true},    // Forth's first
		{"AppDelegate.m", "", slash, true}, // Mathematica's first
		{"top.v", "", slash, true},         // Coq's first
		{"kernel.cu", "", slash, true},
		{"start.s", "", commentSyntax{open: "/*", close: "*/"}, true},
		{"templates/_helpers.tpl", "", commentSyntax{open: "{{/*", close: "*/}}"}, true}, // Smarty's first
		{"page.html.tmpl", "", commentSyntax{open: "{{/*", close: "*/}}"}, true},         // Cheetah's first
		{"src/App.tsx", "", slash, true},
		{"x.py", "", hash, true},
		{"Makefile", "", hash, true},
		{"Dockerfile", "", hash, true},
		{".gitignore", "", hash, true},
		{"requirements.txt", "", hash, true},
		{"a.yaml", "", hash, true},
		{"q.sql", "", dash, true},
		{"init.lua", "", dash, true},
		{"core.clj", "", commentSyntax{line: ";;"}, true},
		{"setup.ini", "", commentSyntax{line: ";"}, true},
		{"paper.tex", "", commentSyntax{line: "%"}, true},
		{"run.bat", "", commentSyntax{line: "@REM"}, true},
		{".vimrc", "", commentSyntax{line: `"`}, true},
		{"README.md", "", html, true},
		{"index.html", "", html, true},
		{"a.css", "", commentSyntax{open: "/*", close: "*/"}, true},
		{"m.ml", "", commentSyntax{open: "(*", close: "*)"}, true},
		{"page.j2", "", commentSyntax{open: "{#", close: "#}"}, true},
		{"deploy", "#!/bin/bash\necho hi\n", hash, true},            // chroma's guess
		{"serve", "#!/usr/bin/env python3\nprint(1)\n", hash, true}, // chroma has none
		{"notes.txt", "", commentSyntax{}, false},
		{"LICENSE", "Copyright\n", commentSyntax{}, false},
		{"", "", commentSyntax{}, false}, // an untitled buffer
	} {
		if got, ok := commentOf(tc.path, tc.text); got != tc.want || ok != tc.ok {
			t.Errorf("commentOf(%q) = %+v %v, want %+v %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

// TestCommentTokensCoverChroma: every language chroma colours has its comment
// syntax, or is known to have none, so a misspelt name in the table fails here
// and so does a lexer a chroma update brings.
func TestCommentTokensCoverChroma(t *testing.T) {
	none := []string{
		"Bash Session", "BNF", "Brainfuck", "Core", "CSV", "Diff", "Gemfile.lock", "Gemtext", "Hexdump",
		"HTTP", "Natural", "NDISASM", "Newspeak", "plaintext", "Plutus Core", "SNBT", "Snobol", "Tal",
		"TypoScriptCssData", "TypoScriptHtmlData", "WebVTT",
	}

	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		name := l.Config().Name
		if _, ok := commentTokens[name]; ok == slices.Contains(none, name) {
			t.Errorf("%q: in the table %v, listed as without comments %v", name, ok, !ok)
		}
	}
}

func TestCommentIn(t *testing.T) {
	html := commentSyntax{open: "<!--", close: "-->"}

	for _, tc := range []struct {
		name, before string
		want         commentSyntax
	}{
		{"markup", "<div>", html},
		{"a script", "<template>\n</template>\n<script setup lang=\"ts\">", commentSyntax{line: "//"}},
		{"a style", "<STYLE>\na {}", commentSyntax{open: "/*", close: "*/"}},
		{"past the script", "<script>\nx()\n</script>", html},
		{"a one-line script", `<script src="a.js"></script>`, html},
		{"a Go fence", "# Title\n```go\nfunc f() {", commentSyntax{line: "//"}},
		{"a shell fence", "~~~sh", commentSyntax{line: "#"}},
		{"past the fence", "```go\nx := 1\n```", html},
		{"a fence without a language", "```", html},
		{"past an HTML fence", "```html\n<script>\n```", html},
	} {
		if got := commentIn(html, tc.before+"\nx\ny", strings.Count(tc.before, "\n")+1); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}

	if c := (commentSyntax{line: "#"}); commentIn(c, "<script>\nx", 1) != c { // only markup has elements
		t.Error("a # file took a <script>")
	}
}

func TestToggleComments(t *testing.T) {
	slash, html := commentSyntax{line: "//"}, commentSyntax{open: "<!--", close: "-->"}

	for _, tc := range []struct {
		name     string
		c        commentSyntax
		in, want string
		at0, by0 int // the first line's change
	}{
		{"shallowest indent", slash, "\tif x {\n\t\ty()\n\t}", "\t// if x {\n\t// \ty()\n\t// }", 1, 3},
		{"blank lines stay", slash, "a\n\n  \nb", "// a\n\n  \n// b", 0, 3},
		{"one mixed line comments all", slash, "// a\nb", "// // a\n// b", 0, 3},
		{"uncomment", slash, "  // a\n  //b\n\n  // c", "  a\n  b\n\n  c", 2, -3},
		{"a blank line alone", slash, "", "// ", 0, 3},
		{"wide token", commentSyntax{line: "⍝"}, "x", "⍝ x", 0, 2},
		{"block", html, "  <p>\n  </p>", "  <!-- <p>\n  </p> -->", 2, 5},
		{"block back", html, "  <!-- <p>\n  </p> -->  ", "  <p>\n  </p>  ", 2, -5},
		{"block on a blank line", html, "", "<!--  -->", 0, 5},
		{"a token that only opens", html, "<!-->", "<!-- <!--> -->", 0, 5},
		{"a line's own comment comes out", commentSyntax{open: "/*", close: "*/"}, "  color: red; /* old */", "  color: red; old", 14, -3},
		{"in runes", html, "- položka <!-- x --> y", "- položka x y", 10, -5},
		{"an empty block", html, "<!---->", "", 0, -4},
	} {
		out := toggleComments(strings.Split(tc.in, "\n"), tc.c)

		text := make([]string, 0, len(out))
		for _, e := range out {
			text = append(text, e.text)
		}

		if got := strings.Join(text, "\n"); got != tc.want || out[0].at != tc.at0 || out[0].by != tc.by0 {
			t.Errorf("%s: %q at %d by %d, want %q at %d by %d", tc.name, got, out[0].at, out[0].by, tc.want, tc.at0, tc.by0)
		}
	}
}

// FuzzToggleComments: a toggle that comments lines out is undone by the next.
func FuzzToggleComments(f *testing.F) {
	for _, s := range []string{"a\n\tb\n", "// a\n\n  //b", "", "  \n\t", "<!-- x -->", "<!-->", "\"\"", "x \t"} {
		f.Add(s, uint8(0))
		f.Add(s, uint8(2))
	}

	syntaxes := []commentSyntax{{line: "//"}, {line: "⍝"}, {open: "<!--", close: "-->"}, {open: `"`, close: `"`}}

	f.Fuzz(func(t *testing.T, text string, which uint8) {
		c := syntaxes[int(which)%len(syntaxes)]
		lines := strings.Split(text, "\n")

		texts := func(out []commentEdit) []string {
			s := make([]string, 0, len(out))
			for _, e := range out {
				s = append(s, e.text)
			}

			return s
		}

		once := texts(toggleComments(lines, c))
		if len(once) != len(lines) {
			t.Fatalf("%q: %d lines became %d", text, len(lines), len(once))
		}

		if len(strings.Join(once, "\n")) <= len(text) { // it took comments out
			return
		}

		if c.line == "" && len(lines) == 1 && strings.Contains(text, c.close) { // a stray close: the block ends there
			return
		}

		if back := texts(toggleComments(once, c)); !slices.Equal(back, lines) {
			t.Fatalf("%+v %q: commented %q, back %q", c, text, once, back)
		}
	})
}

// TestEditorCommentLines: ⌃/ over a selection comments every line it touches
// at the shallowest indent, keeps the selection, and a second ⌃/ undoes it.
func TestEditorCommentLines(t *testing.T) {
	m, _ := editorModel(t, "a.go", "func f() {\n\tif x {\n\t\ty()\n\t}\n}\n")
	m.pv.cur = pos{1, 0}
	press(m, "shift+down", "shift+down", "shift+down") // ends at column 0 of the closing brace

	press(m, "ctrl+/")

	if got := m.pv.buf.text(); got != "func f() {\n\t// if x {\n\t// \ty()\n\t// }\n}" {
		t.Fatalf("commented: %q", got)
	}

	if a, z, ok := m.pv.selection(); !ok || a != (pos{1, 0}) || z != (pos{4, 0}) {
		t.Fatalf("selection %v..%v %v", a, z, ok)
	}

	press(m, "ctrl+/")

	if got := m.pv.buf.text(); got != "func f() {\n\tif x {\n\t\ty()\n\t}\n}" {
		t.Fatalf("uncommented: %q", got)
	}
	// Without a selection the cursor moves with its text.
	m.pv.anchor, m.pv.cur = nil, pos{4, 1}
	press(m, "ctrl+/")

	if got := string(m.pv.buf.line(4)); got != "// }" || m.pv.cur != (pos{4, 4}) {
		t.Fatalf("line %q cur %v", got, m.pv.cur)
	}
}
