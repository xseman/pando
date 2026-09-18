package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const sampleMD = "# Title\n\nSome *em* and **bold** text with `code` and a [link](http://x).\n\n" +
	"- one\n- two\n  - nested\n\n1. first\n2. second\n\n> quote\n\n```go\nfunc main() {}\n```\n\n" +
	"| a | bb |\n|---|---:|\n| 1 | 22 |\n\n---\n"

func TestMarkdownRender(t *testing.T) {
	styled, plainLines := renderMarkdown(sampleMD, 40, true)

	want := []string{
		"Title", strings.Repeat("─", 40), "", "Some em and bold text with code and a", "link.", "",
		"• one", "• two", "  ◦ nested", "", "1. first", "2. second", "", "▎ quote", "", " func main() {}", "",
		"a │ bb", "──┼───", "1 │ 22", "", strings.Repeat("─", 40),
	}
	if !slices.Equal(plainLines, want) {
		t.Fatalf("rendered:\n%s\nwant:\n%s", strings.Join(plainLines, "\n"), strings.Join(want, "\n"))
	}

	if len(styled) != len(plainLines) {
		t.Fatalf("%d styled lines for %d plain ones", len(styled), len(plainLines))
	}

	for i, l := range styled {
		if ansi.StringWidth(l) > 40 || !strings.HasPrefix(ansi.Strip(l), plainLines[i]) {
			t.Errorf("line %d: %q vs %q", i, ansi.Strip(l), plainLines[i])
		}
	}

	long := strings.Repeat("word ", 30) + strings.Repeat("x", 50)

	for _, l := range func() []string { s, _ := renderMarkdown(long, 20, true); return s }() {
		if ansi.StringWidth(l) > 20 {
			t.Fatalf("wrapped line %q is wider than 20", ansi.Strip(l))
		}
	}
}

func TestMarkdownPreview(t *testing.T) {
	m := testModelSized(t, 130, 20)
	path := filepath.Join(m.ws, "README.md")
	m.pv = preview{kind: pvFile, path: path}
	m.preview, m.focus = true, onMain
	lines, plainLines, meta, numW := render(pvFile, path, sampleMD, true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: sampleMD, lines: lines, plain: plainLines, meta: meta, numW: numW})

	if out := checkWidths(t, m); !strings.Contains(out, "# Title") {
		t.Fatalf("Markdown opens as source:\n%s", out)
	}

	press(m, "ctrl+shift+v") // a source file types letters now; ⌃⇧v renders it

	out := strings.Split(checkWidths(t, m), "\n")
	if m.pv.gutter() != 0 || !strings.HasPrefix(ansi.Cut(out[1], m.mainX(), m.w), "Title") || !strings.Contains(out[len(out)-2], "p source") {
		t.Fatalf("rendered:\n%s", strings.Join(out, "\n"))
	}

	press(m, "ctrl+shift+v", "alt+v") // back to source, then side by side
	out = strings.Split(checkWidths(t, m), "\n")

	c, lw := m.mainX(), (m.mainW()-1)/2
	if l, r := ansi.Cut(out[1], c, c+lw), ansi.Cut(out[1], c+lw+1, m.w); !strings.Contains(l, "# Title") || !strings.HasPrefix(r, " Title") || m.View().Cursor != nil {
		t.Fatalf("side by side:\n%q\n%q", l, r)
	}

	press(m, "alt+v") // side by side off, so the button has something to toggle
	click(m, m.mainX()+m.pv.buttons(m, m.mainW())[0].x+1, 0, tea.MouseLeft)

	if m.pv.md != 1 {
		t.Fatalf("the first header button shows the rendering: md = %d", m.pv.md)
	}
}

func TestFileRevisions(t *testing.T) {
	m := testModel(t)

	root := m.ws
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		mustGit(t, root, a...)
	}

	f := filepath.Join(root, "notes.txt")
	for _, v := range []string{"one", "two"} {
		mustWrite(t, f, v+"\n")
		mustGit(t, root, "add", "notes.txt")
		mustGit(t, root, "commit", "-qm", "set "+v)
	}

	mustWrite(t, f, "three\n")
	m.pv, m.preview, m.focus = preview{kind: pvFile, path: f}, true, onMain
	check := func(context, body string) {
		t.Helper()

		out := checkWidths(t, m)
		if _, ctx := m.pv.label(m.ws); m.pv.kind != pvRev || !strings.Contains(ctx, context) || !strings.Contains(out, body) {
			t.Fatalf("kind=%s label=%q, want %q and %q in\n%s", m.pv.kind, ctx, context, body, out)
		}
	}

	fire(m, m.pv.stepRev(m, 1)) // the header's ← button; revisions have no key
	check("uncommitted changes  1/3", "+ three")
	fire(m, m.pv.stepRev(m, 1))
	check("set two  2/3", "+ two")
	fire(m, m.pv.stepRev(m, 1))
	fire(m, m.pv.stepRev(m, 1)) // there is nothing older
	check("set one  3/3", "+ one")
	m.pv.pickRev(m)
	choose(t, m, "Uncommitted changes")
	fire(m, m.pv.load(m))
	check("1/3", "three")
	fire(m, m.pv.stepRev(m, -1))

	if m.pv.kind != pvFile || m.pv.path != f {
		t.Fatalf("newer than the newest is the file: %+v", m.pv.kind)
	}
}
