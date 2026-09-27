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
		" Title ", "", "Some em and bold text with  code ", "and a link.", "",
		"• one", "• two", "  ◦ nested", "", "1. first", "2. second", "", "▎ quote", "", " func main() {}", "",
		"a │ bb", "──┼───", "1 │ 22", "", strings.Repeat("─", 36),
	}
	for i, l := range want { // glow's margin, two cells either side
		want[i] = "  " + l
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
	// The title and inline code are chips: their pads take their background.
	if !strings.Contains(styled[0], bgParams(pal.accent)) || !strings.Contains(styled[2], bgParams(pal.mdCodeBg)) ||
		!strings.Contains(styled[2], fgParams(pal.mdCode)) {
		t.Errorf("chips: %q / %q", styled[0], styled[2])
	}

	if !strings.Contains(styled[2], "with \x1b[") { // the space after "with" is plain, the chip starts after it
		t.Errorf("the space before a chip is the chip's: %q", styled[2])
	}

	long := strings.Repeat("word ", 30) + strings.Repeat("x", 50) + " `a long code span` # not a heading"

	for _, w := range []int{12, 20, 40} {
		for _, l := range func() []string { s, _ := renderMarkdown("# "+long+"\n\n"+long, w, true); return s }() {
			if ansi.StringWidth(l) > w {
				t.Fatalf("wrapped line %q is wider than %d", ansi.Strip(l), w)
			}
		}
	}
	// A narrow panel has no room to spare for margins.
	if _, p := renderMarkdown("hello", 20, true); p[0] != "hello" {
		t.Errorf("margins at 20 cells: %q", p[0])
	}
}

// Headings below the title keep their #s, as glow shows them.
func TestMarkdownHeadings(t *testing.T) {
	_, p := renderMarkdown("## Two\n\n### Three *em*\n", 60, true)

	if want := []string{"  ## Two", "  ", "  ### Three em"}; !slices.Equal(p, want) {
		t.Fatalf("headings %q, want %q", p, want)
	}
}

// A cell too wide for its column wraps within it instead of being cut, the
// row growing to its tallest cell.
func TestMarkdownTableWraps(t *testing.T) {
	src := "| k | text |\n|---|---|\n| a | the quick brown fox jumps over the lazy dog |\n| b | short |\n"
	styled, p := renderMarkdown(src, 30, true)

	want := []string{
		"  k │ text", "  ─" + "─┼─" + strings.Repeat("─", 22), // the text column takes what the key leaves
		"  a │ the quick brown fox", "    │ jumps over the lazy", "    │ dog",
		"  b │ short",
	}

	for i := range p {
		p[i] = strings.TrimRight(p[i], " ")
	}

	if !slices.Equal(p, want) {
		t.Fatalf("table:\n%s\nwant:\n%s", strings.Join(p, "\n"), strings.Join(want, "\n"))
	}

	for _, l := range styled {
		if ansi.StringWidth(l) > 30 {
			t.Fatalf("row %q wider than 30", ansi.Strip(l))
		}
	}

	if strings.Contains(strings.Join(p, ""), "…") {
		t.Fatal("a cell is cut instead of wrapped")
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
	if m.pv.gutter() != 0 || !strings.HasPrefix(ansi.Cut(out[1], m.mainX(), m.w), "   Title ") || !strings.Contains(out[len(out)-2], "p source") {
		t.Fatalf("rendered:\n%s", strings.Join(out, "\n"))
	}

	press(m, "ctrl+shift+v", "alt+v") // back to source, then side by side
	out = strings.Split(checkWidths(t, m), "\n")

	c, lw := m.mainX(), (m.pvW()-1)/2
	if l, r := ansi.Cut(out[1], c, c+lw), ansi.Cut(out[1], c+lw+1, m.w); !strings.Contains(l, "# Title") || !strings.HasPrefix(r, "    Title ") || m.View().Cursor != nil {
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

// markdown_width caps the rendering, glow's -w: a wide panel wraps it there
// and leaves the rest blank, and a new width lays it out again at once.
func TestMarkdownWidth(t *testing.T) {
	m := testModelSized(t, 160, 20)
	path := filepath.Join(m.ws, "README.md")
	src := "# Title\n\n" + strings.Repeat("lorem ipsum dolor sit amet ", 20) + "\n"
	m.pv = preview{kind: pvFile, path: path}
	m.preview, m.focus = true, onMain
	lines, plainLines, meta, numW := render(pvFile, path, src, true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: src, lines: lines, plain: plainLines, meta: meta, numW: numW})
	m.st.Settings.MDWidth = 60
	press(m, "ctrl+shift+v")
	checkWidths(t, m)

	widest := func() int {
		n := 0
		for _, l := range m.pv.plain {
			n = max(n, len([]rune(strings.TrimRight(string(l), " "))))
		}

		return n
	}

	if w := widest(); w > 60-mdMargin || w < 50 {
		t.Fatalf("widest line %d cells, want it wrapped just inside 60", w)
	}

	st := m.st
	st.Settings.MDWidth = 0
	m.Update(stateMsg(st))
	checkWidths(t, m)

	if w := widest(); w <= 60 {
		t.Fatalf("markdown_width 0 fills the %d-cell panel, widest line %d", m.pvW(), w)
	}
	// Beside its source the rendering takes its whole half, uncapped, so the
	// two sides keep their lines level.
	st.Settings.MDWidth = 40
	m.Update(stateMsg(st))
	press(m, "alt+v")
	checkWidths(t, m)

	half := m.pvW() - 1 - (m.pvW()-1)/2 - 1
	if m.pv.mdW != half {
		t.Fatalf("side by side lays out %d cells, want the half's %d", m.pv.mdW, half)
	}
}

// Code in rendered Markdown, blocks and inline alike, sits on md_code_bg: a
// soft grey on the light theme, where the input background is all but white.
func TestMarkdownCodeBackground(t *testing.T) {
	t.Cleanup(func() { applyLook("vscode", true, "ascii", nil) })
	applyLook("vscode-light", false, "ascii", nil)

	styled, _ := renderMarkdown("Run `go test` now.\n\n```go\nfunc main() {}\n```\n", 60, false)

	grey := bgParams(pal.mdCodeBg)
	if !strings.Contains(styled[0], grey) || !strings.Contains(styled[2], grey) {
		t.Fatalf("inline code %q, block %q: want the grey %s", styled[0], styled[2], grey)
	}

	if r, g, b, _ := pal.mdCodeBg.RGBA(); r>>8 > 0xf0 || r>>8 < 0xe0 || r != g || b < g {
		t.Fatalf("md_code_bg %s is not a soft grey", hexColor(pal.mdCodeBg))
	}
}
