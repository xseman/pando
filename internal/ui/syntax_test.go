package ui

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// colorOf is the foreground the styled line gives word, "" for none.
func colorOf(t *testing.T, styled []string, word string) string {
	t.Helper()

	for _, l := range styled {
		i := strings.Index(l, word)
		if i < 0 {
			continue
		}

		esc := l[:i]
		if j := strings.LastIndex(esc, "\x1b["); j >= 0 {
			return esc[j:]
		}

		return ""
	}

	t.Fatalf("%q is not in the rendering", word)

	return ""
}

// A light terminal's code takes VS Code's Light+ colors: control flow
// magenta, keywords blue, strings dark red, comments green; in JavaScript
// a call or a definition brown, a class teal. A dark one keeps github-dark.
func TestLightPlusColors(t *testing.T) {
	js := "import { LitElement } from \"lit\";\n// a comment\nclass AppModal extends LitElement {\n  createRenderRoot() {\n    return this.MAX_SIZE + 42;\n  }\n}\n"
	goSrc := "package a\n\nfunc A() int {\n\tfmt.Println(\"x\")\n\treturn 1\n}\n"

	for _, c := range []struct {
		path, src, word, hex string
	}{
		{"a.js", js, "import", "#af00db"},
		{"a.js", js, "from", "#af00db"},
		{"a.js", js, "return", "#af00db"},
		{"a.js", js, "class", "#0000ff"},
		{"a.js", js, "extends", "#0000ff"},
		{"a.js", js, `"lit"`, "#a31515"},
		{"a.js", js, "// a comment", "#008000"},
		{"a.js", js, "createRenderRoot", "#795e26"},
		{"a.js", js, "AppModal", "#267f99"},
		{"a.js", js, "MAX_SIZE", "#0070c1"},
		{"a.js", js, "42", "#098658"},
		{"a.go", goSrc, "func", "#0000ff"},
		{"a.go", goSrc, "return", "#af00db"},
		{"a.go", goSrc, "int", "#267f99"},
		{"a.go", goSrc, "Println", "#795e26"},
	} {
		styled, _, _, _ := render(pvFile, c.path, c.src, false)
		if got := colorOf(t, styled, c.word); !strings.Contains(got, fgParams(hex(c.hex))) {
			t.Errorf("%s %q: %q, want %s", c.path, c.word, got, c.hex)
		}
	}

	styled, _, _, _ := render(pvFile, "a.go", goSrc, true)
	if got := colorOf(t, styled, "func"); !strings.Contains(got, fgParams(hex("#ff7b72"))) {
		t.Errorf("a dark terminal's keyword: %q, want github-dark's", got)
	}
}

// Every Light+ color reads on white at 4.5:1, and at 3:1 on the light
// theme's find highlights.
func TestLightPlusContrast(t *testing.T) {
	applyLook("vscode-light", false, "ascii", nil)
	t.Cleanup(func() { applyLook("vscode", true, "ascii", nil) })

	for _, tt := range lightPlus.Types() {
		e := lightPlus.Get(tt)
		if !e.Colour.IsSet() {
			continue
		}

		c := hex(e.Colour.String())
		for bg, least := range map[color.Color]float64{hex("#ffffff"): 4.5, pal.findHitBg: 3, pal.findMatchBg: 3, pal.matchBg: 3} {
			if r := contrast(c, bg); r < least {
				t.Errorf("%v %s on %v: %.2f, want %.1f", tt, e.Colour, bg, r, least)
			}
		}
	}
}

// refine changes types only: the same tokens, the same text.
func TestRefineKeepsText(t *testing.T) {
	src := "import x from 'y'\nfoo(Bar, BAZ)\nif (a) { return b }\n"
	lexer := lexers.Get("javascript")

	it, _ := chroma.Coalesce(lexer).Tokenise(nil, src)
	before := it.Tokens()

	it, _ = chroma.Coalesce(lexer).Tokenise(nil, src)
	after := refine(lexer, it).Tokens()

	if len(before) != len(after) {
		t.Fatalf("%d tokens, then %d", len(before), len(after))
	}

	var b strings.Builder
	for _, tk := range after {
		b.WriteString(tk.Value)
	}

	if b.String() != src {
		t.Fatalf("text %q", b.String())
	}
}

// FuzzRefine checks refine keeps the tokens and the text, whatever the
// source, in a language it retypes names in and one it does not.
func FuzzRefine(f *testing.F) {
	for _, s := range []string{"import a from 'b'", "f(X, Y_Z)", "return if else", "", "(", "from"} {
		f.Add(s, true)
		f.Add(s, false)
	}

	f.Fuzz(func(t *testing.T, src string, js bool) {
		lexer := lexers.Get(ifElse(js, "typescript", "go"))

		it, err := chroma.Coalesce(lexer).Tokenise(nil, src)
		if err != nil {
			return
		}

		before := it.Tokens()

		it, _ = chroma.Coalesce(lexer).Tokenise(nil, src)
		after := refine(lexer, it).Tokens()

		if len(before) != len(after) {
			t.Fatalf("%q: %d tokens, then %d", src, len(before), len(after))
		}

		for i := range before {
			if before[i].Value != after[i].Value {
				t.Fatalf("%q: token %d %q, then %q", src, i, before[i].Value, after[i].Value)
			}
		}
	})
}

// luminance is c's relative luminance, 0 for black to 1 for white.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		f := float64(v) / 0xffff
		if f <= 0.04045 {
			return f / 12.92
		}

		return math.Pow((f+0.055)/1.055, 2.4)
	}

	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is WCAG's contrast ratio of a and b, 1 to 21.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)

	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}
