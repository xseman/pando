package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// detectIndent reads tabs or spaces off the lines, and the spaces of a level
// off the step most lines take.
func TestDetectIndent(t *testing.T) {
	for _, c := range []struct {
		text string
		want indent
	}{
		{"func a() {\n\tx()\n\tif y {\n\t\tz()\n\t}\n}\n", indent{style: "tab"}},
		{"a:\n  b:\n    c: 1\n  d: 2\n", indent{style: "space", size: 2}},
		{"def a():\n    if b:\n        c()\n    d()\n", indent{style: "space", size: 4}},
		{"/**\n * a doc comment\n */\nfunc a() {}\n", indent{}},             // one space is no indent
		{"no\nindent\nhere\n", indent{}},                                    // nothing to tell
		{"a\n\tb\n  c\n\td\n", indent{style: "tab"}},                        // more lines start with a tab
		{"a {\n    b\n\n    c\n}\n", indent{style: "space", size: 4}},       // a blank line breaks no step
		{"a\r\n  b\r\n    c\r\n  \r\n", indent{style: "space", size: 2}},    // CRLF, a line of blanks
		{"x\n      deep\nx\n      deep\n", indent{style: "space", size: 6}}, // the step it takes
	} {
		if got := detectIndent(c.text); got != c.want {
			t.Errorf("detectIndent(%q) = %+v, want %+v", c.text, got, c.want)
		}
	}
}

// An .editorconfig's sections match by its globs, the nearest file and the
// last section winning, up to the one with root = true.
func TestEditorConfig(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".editorconfig"), "indent_style = tab\n[*]\nindent_style = space\nindent_size = 8\n")
	mustWrite(t, filepath.Join(dir, "repo", ".editorconfig"), `root = true

# defaults
[*]
indent_style = space
indent_size = 4

; Makefiles need tabs
[{Makefile,*.mk}]
indent_style = tab
tab_width = 8

[*.{yml,yaml}]
indent_size = 2

[lib/**.js]
indent_size = 3

[/top.txt]
indent_size = 5

[*.go]
indent_style = tab
indent_size = tab
tab_width = 6

[*.[ch]]
INDENT_STYLE = TAB
indent_size = unset
`)
	mustWrite(t, filepath.Join(dir, "repo", "sub", ".editorconfig"), "[*.yml]\nindent_size = 6\n")

	for path, want := range map[string]indent{
		"repo/a.txt":          {style: "space", size: 4},
		"repo/Makefile":       {style: "tab", size: 8},
		"repo/x/rules.mk":     {style: "tab", size: 8},
		"repo/ci.yaml":        {style: "space", size: 2},
		"repo/sub/a.yml":      {style: "space", size: 6}, // the nearest file wins
		"repo/lib/a/b/c.js":   {style: "space", size: 3}, // ** crosses directories
		"repo/x/lib/c.js":     {style: "space", size: 4}, // a slash anchors it to the file's directory
		"repo/top.txt":        {style: "space", size: 5},
		"repo/x/top.txt":      {style: "space", size: 4},
		"repo/main.go":        {style: "tab", size: 6}, // indent_size = tab: a tab's width
		"repo/a.c":            {style: "tab"},          // keys and values in any case, unset
		"other.txt":           {style: "space", size: 8},
		"repo/no/such/a.yaml": {style: "space", size: 2},
	} {
		if got := editorConfig(filepath.Join(dir, path)); got != want {
			t.Errorf("%s: %+v, want %+v", path, got, want)
		}
	}
}

// fileIndent puts .editorconfig over what the lines show, key by key.
func TestFileIndent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".editorconfig"), "[*.py]\nindent_size = 4\n[*.go]\nindent_style = tab\n")

	if got := fileIndent(filepath.Join(dir, "a.py"), "a:\n  b\n"); got != (indent{"space", 4, false}) {
		t.Errorf("the size its .editorconfig says, the style its lines: %+v", got)
	}

	if got := fileIndent(filepath.Join(dir, "a.go"), "a\n  b\n"); got != (indent{style: "tab"}) {
		t.Errorf("tabs, as wide as tab_size: %+v", got)
	}

	if got := fileIndent(filepath.Join(dir, "a.txt"), "a\n   b\n"); got != (indent{"space", 3, false}) {
		t.Errorf("no section: its lines alone: %+v", got)
	}
}

// A file opens with its own indentation: the status bar says it, tab
// inserts it, a tab draws at its width; a file with none follows the
// settings, and the picker's choice outlives a reload.
func TestFileIndentation(t *testing.T) {
	t.Cleanup(func() { tabW = 4 })

	m := testModelSized(t, 120, 30)
	mustWrite(t, filepath.Join(m.ws, ".editorconfig"), "[*.yml]\nindent_style = space\nindent_size = 2\n")

	yml, goFile := filepath.Join(m.ws, "a.yml"), filepath.Join(m.ws, "a.go")
	mustWrite(t, yml, "a:\nb\n")
	mustWrite(t, goFile, "func a() {\n\tb()\n}\n")

	open := func(path string) {
		t.Helper()

		for _, msg := range runAll(m.openFileAt(path, 0, 0, 0, true)) {
			if pm, ok := msg.(previewMsg); ok {
				m.Update(pm)
			}
		}

		if !m.pv.ready || m.pv.path != path {
			t.Fatalf("%s did not open", path)
		}

		m.focus = onMain
	}
	status := func() string { s, _ := m.statusLine(m.w); return ansi.Strip(s) }

	open(yml)

	if s := status(); !strings.Contains(s, "Spaces: 2") || tabW != 2 {
		t.Fatalf("a.yml: %q, tab width %d", s, tabW)
	}

	m.pv.gotoLine(m, 1, 0)
	press(m, "tab")

	if got := string(m.pv.buf.line(1)); got != "  b" {
		t.Fatalf("tab in a.yml: %q", got)
	}

	open(goFile)

	if s := status(); !strings.Contains(s, "Tab Size: 4") || tabW != 4 || string(m.pv.plain[1]) != "    b()" {
		t.Fatalf("a.go: %q, tab width %d, %q", s, tabW, string(m.pv.plain[1]))
	}

	click := func(label string) {
		t.Helper()

		s := status()
		i := strings.Index(s, label)
		m.statusMouse(ansi.StringWidth(s[:i]) + 1)
	}
	click("Tab Size")
	choose(t, m, "Indent Using Tabs")
	choose(t, m, "8")

	if tabW != 8 || string(m.pv.plain[1]) != "        b()" {
		t.Fatalf("picked 8: tab width %d, %q", tabW, string(m.pv.plain[1]))
	}

	mustWrite(t, goFile, "func a() {\n\tc()\n}\n") // an agent's edit reloads it
	fire(m, m.pv.load(m))

	if s := status(); !strings.Contains(s, "Tab Size: 8") || string(m.pv.plain[1]) != "        c()" {
		t.Fatalf("after a reload: %q, %q", s, string(m.pv.plain[1]))
	}

	open(yml) // back to a.yml: its own again

	if s := status(); !strings.Contains(s, "Spaces: 2") || tabW != 2 {
		t.Fatalf("a.yml again: %q, tab width %d", s, tabW)
	}

	checkWidths(t, m)
}

// FuzzEditorConfigGlob checks a glob with nothing special in it matches its
// own name, at any depth, and nothing else; and that no glob panics. One
// that is not UTF-8 makes no regexp, and so matches nothing.
func FuzzEditorConfigGlob(f *testing.F) {
	for _, s := range [][2]string{{"*.go", "a/b.go"}, {"{a,b}", "a"}, {"[!x]y", "zy"}, {"Makefile", "x/Makefile"}, {"**/*.js", "lib/a.js"}, {"{a", "x"}, {"[", "["}, {`\*`, "*"}} {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, glob, name string) {
		re := ecGlob(glob)
		if glob == "" || strings.ContainsAny(glob, `*?[]{},\/`) || !utf8.ValidString(glob) { // a regexp takes UTF-8 only
			return
		}

		if re == nil {
			t.Fatalf("%q: a plain name makes no regexp", glob)
		}

		want := name == glob || strings.HasSuffix(name, "/"+glob)
		if got := re.MatchString(name); got != want {
			t.Fatalf("%q matches %q: %v, want %v", glob, name, got, want)
		}
	})
}

// FuzzDetectIndent checks a guess is one an editor can take: tabs with the
// settings' width, or spaces 2 to 8 wide; and none for a text whose lines
// start with neither.
func FuzzDetectIndent(f *testing.F) {
	for _, s := range []string{"a\n\tb\n", "a\n  b\n    c\n", " * x\n", "\t \n  \t\r\n", ""} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		got := detectIndent(text)

		switch got.style {
		case "tab":
			if got.size != 0 {
				t.Fatalf("%q: tabs %d wide", text, got.size)
			}

		case "space":
			if got.size != 0 && (got.size < 2 || got.size > 8) {
				t.Fatalf("%q: a level of %d spaces", text, got.size)
			}

		case "":
			if got.size != 0 {
				t.Fatalf("%q: a size with no style", text)
			}

		default:
			t.Fatalf("%q: style %q", text, got.style)
		}

		indented := false
		for line := range strings.Lines(text) {
			indented = indented || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ")
		}

		if !indented && got != (indent{}) {
			t.Fatalf("%q: no line is indented, yet %+v", text, got)
		}
	})
}
