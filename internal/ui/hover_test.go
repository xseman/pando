package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// serverModel is an editor on name with the test binary as its language
// server (fakeLSP), stopped when the test ends.
func serverModel(t *testing.T, name, text string) (*Model, string) {
	t.Helper()
	t.Setenv(fakeLSPEnv, "1")

	m, path := editorModel(t, name, text)
	m.st.Settings.LSP = map[string][]string{strings.TrimPrefix(name[strings.LastIndex(name, "."):], "."): {os.Args[0]}}

	t.Cleanup(func() { m.lsps.reset("") })

	return m, path
}

// deliver runs what the key or event started and hands Update the
// language server's answers among the messages.
func deliver(m *Model, cmd tea.Cmd) {
	for _, msg := range runAll(cmd) {
		switch msg.(type) {
		case hoverMsg, sigMsg, hoverTickMsg:
			_, next := m.Update(msg)
			deliver(m, next)
		}
	}
}

// screenAt is where text first shows on screen.
func screenAt(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()

	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}

	t.Fatalf("%q is not on screen", text)

	return 0, 0
}

func onScreen(m *Model, text string) bool {
	return strings.Contains(ansi.Strip(m.View().Content), text)
}

// ⌃k asks the server about the word at the cursor and shows its Markdown
// under it; the next key puts it away.
func TestShowHover(t *testing.T) {
	m, _ := serverModel(t, "a.go", "package a\n\nfunc B() { A(1, 2) }\n")
	m.pv.gotoLine(m, 2, 11) // on A

	_, cmd := m.Update(keyMsg("ctrl+k"))
	deliver(m, cmd)

	if !onScreen(m, "A adds them.") || !onScreen(m, "func A(x int, y int)") {
		t.Fatalf("no hover:\n%s", ansi.Strip(m.View().Content))
	}

	checkWidths(t, m)
	press(m, "right")

	if onScreen(m, "A adds them.") {
		t.Fatal("a key leaves the hover up")
	}
	// A file with no server says what to configure.
	m.st.Settings.LSP = nil
	m.pv.path = strings.TrimSuffix(m.pv.path, ".go") + ".zz"

	_, cmd = m.Update(keyMsg("ctrl+k"))
	if msg, _ := cmd().(flashMsg); !strings.Contains(msg.text, "[lsp]") {
		t.Fatalf("no server: %q", msg.text)
	}
}

// The pointer resting on a word shows its hover by the pointer, after the
// problem under it; moving along the word keeps it, off it puts it away.
func TestHoverPointer(t *testing.T) {
	m, _ := serverModel(t, "a.go", "package a\n\nfunc B() { A(1, 2) }\n")
	m.st.Settings.Tips = true
	x, y := screenAt(t, m, "A(1")

	_, cmd := m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if cmd == nil {
		t.Fatal("resting on a word asks nothing")
	}

	deliver(m, cmd)

	if !onScreen(m, "A adds them.") {
		t.Fatalf("no hover by the pointer:\n%s", ansi.Strip(m.View().Content))
	}

	checkWidths(t, m)

	m.Update(tea.MouseMotionMsg{X: x + 1, Y: y}) // onto "(": off the word

	if onScreen(m, "A adds them.") {
		t.Fatal("the hover stays with the pointer gone")
	}
	// Tips off: the pointer asks nothing.
	m.st.Settings.Tips = false
	if _, cmd := m.Update(tea.MouseMotionMsg{X: x, Y: y}); cmd != nil {
		t.Fatal("with tips off the pointer still asks")
	}
}

// Typing ( asks for the signature, its active parameter follows the
// commas, ) or esc puts it away.
func TestSignatureHelp(t *testing.T) {
	m, _ := serverModel(t, "a.go", "package a\n\nfunc B() {  }\n")
	m.pv.gotoLine(m, 2, 11)

	typ := func(keys ...string) {
		for _, k := range keys {
			_, cmd := m.Update(keyMsg(k))
			deliver(m, cmd)
		}
	}

	typ("A", "(")

	if s := m.pv.sig.sig; s == nil || s.Label[s.From:s.To] != "x int" || !onScreen(m, "A(x int, y int)") || !onScreen(m, "A adds them.") {
		t.Fatalf("after A(: %+v\n%s", s, ansi.Strip(m.View().Content))
	}

	checkWidths(t, m)
	typ("1", ",")

	if s := m.pv.sig.sig; s == nil || s.Label[s.From:s.To] != "y int" {
		t.Fatalf("after the comma: %+v", s)
	}

	typ("2", ")")

	if m.pv.sig.sig != nil || onScreen(m, "A(x int, y int)") {
		t.Fatal("the hints outlive the call")
	}

	typ("left", "left", ",")

	if m.pv.sig.sig == nil {
		t.Fatal("a comma in the call asks again")
	}

	typ("esc")

	if m.pv.sig.sig != nil {
		t.Fatal("esc leaves the hints up")
	}
}

// A file [format] has no tool for formats with its language server, with
// the file's own indentation.
func TestFormatWithServer(t *testing.T) {
	m, path := serverModel(t, "a.zz", "a  \n\tb\t\n")
	m.pv.ind = indent{style: "space", size: 2}

	if !m.canFormat(path) {
		t.Fatal("a server formats")
	}

	fire(m, m.pv.formatDoc(m))

	if got := m.pv.buf.text(); got != "a\n  b" {
		t.Fatalf("formatted %q (%s)", got, m.msg)
	}
}

// A language server that is not installed formats nothing: format_on_save
// saves quietly, as it did before servers formatted.
func TestFormatServerMissing(t *testing.T) {
	m, path := editorModel(t, "a.zz", "a  \n")
	m.st.Settings.LSP = map[string][]string{"zz": {"no-such-language-server"}}
	m.st.Settings.FmtSave = true

	if m.canFormat(path) {
		t.Fatal("a server not installed formats")
	}

	press(m, "x")

	for _, msg := range runAll(m.pv.save(m, false)) {
		if f, ok := msg.(flashMsg); ok && (f.err || f.text != "saved a.zz") {
			t.Fatalf("save said %q", f.text)
		}
	}
}
