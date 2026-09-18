package ui

import (
	"strings"
	"testing"
)

// vimModel is an editor with vim_mode on, in normal mode at the file's start.
func vimModel(t *testing.T, text string) *Model {
	t.Helper()
	m, _ := editorModel(t, "a.txt", text)
	m.st.Settings.Vim = true
	m.pv.setCursor(m, pos{0, 0})

	return m
}

// TestVimOff is the setting's default: letters type, as they always have.
func TestVimOff(t *testing.T) {
	m, _ := editorModel(t, "a.txt", "one\n")
	m.pv.setCursor(m, pos{0, 0})
	press(m, "i", "j")

	if got := m.pv.text(); got != "ijone\n" {
		t.Fatalf("letters type while vim_mode is off: %q", got)
	}

	if m.pv.vimOn(m) {
		t.Fatal("vim_mode is off by default")
	}
}

// TestVimModes walks the way in and out of insert mode.
func TestVimModes(t *testing.T) {
	m := vimModel(t, "one\ntwo\n")
	press(m, "j", "k") // letters move instead of typing

	if got := m.pv.text(); got != "one\ntwo\n" || m.pv.vim.mode != vimNormal {
		t.Fatalf("normal mode types nothing: %q mode %d", got, m.pv.vim.mode)
	}

	press(m, "i", "h", "i")

	if got := m.pv.text(); got != "hione\ntwo\n" || m.pv.vim.mode != vimInsert {
		t.Fatalf("i inserts: %q mode %d", got, m.pv.vim.mode)
	}

	press(m, "esc")

	if m.pv.vim.mode != vimNormal {
		t.Fatalf("esc leaves insert mode: %d", m.pv.vim.mode)
	}

	press(m, "A", "!")

	if got := m.pv.text(); got != "hione!\ntwo\n" {
		t.Fatalf("A appends at the line end: %q", got)
	}

	press(m, "esc", "o", "x")

	if got := m.pv.text(); got != "hione!\nx\ntwo\n" {
		t.Fatalf("o opens the line below: %q", got)
	}

	press(m, "esc", "O", "y")

	if got := m.pv.text(); got != "hione!\ny\nx\ntwo\n" {
		t.Fatalf("O opens the line above: %q", got)
	}
	// The header names the mode and the editor is still an editor.
	press(m, "esc")

	if out := checkWidths(t, m); !strings.Contains(out, "NORMAL") {
		t.Fatal("the header names the mode")
	}
}

// TestVimMotions covers the motions that are not plain arrows.
func TestVimMotions(t *testing.T) {
	m := vimModel(t, "foo bar.baz\nsecond line\nthird\n")
	press(m, "w")

	if c := m.pv.at(); c != (pos{0, 4}) {
		t.Fatalf("w to the next word: %+v", c)
	}

	press(m, "e")

	if c := m.pv.at(); c != (pos{0, 6}) {
		t.Fatalf("e to the word end: %+v", c)
	}

	press(m, "w") // punctuation is a word of its own

	if c := m.pv.at(); c != (pos{0, 7}) {
		t.Fatalf("w onto the dot: %+v", c)
	}

	press(m, "b")

	if c := m.pv.at(); c != (pos{0, 4}) {
		t.Fatalf("b back a word: %+v", c)
	}

	press(m, "$") // normal mode stands on a rune, never past the line end

	if c := m.pv.at(); c != (pos{0, 10}) {
		t.Fatalf("$ to the last rune: %+v", c)
	}

	press(m, "0")

	if c := m.pv.at(); c != (pos{0, 0}) {
		t.Fatalf("0 to the line start: %+v", c)
	}

	press(m, "G")

	if c := m.pv.at(); c.line != 2 {
		t.Fatalf("G to the last line: %+v", c)
	}

	press(m, "g", "g")

	if c := m.pv.at(); c != (pos{0, 0}) {
		t.Fatalf("gg to the file start: %+v", c)
	}

	press(m, "2", "j")

	if c := m.pv.at(); c.line != 2 {
		t.Fatalf("a count repeats the motion: %+v", c)
	}

	press(m, "2", "G")

	if c := m.pv.at(); c.line != 1 {
		t.Fatalf("2G goes to line 2: %+v", c)
	}
}

// TestVimOperators is d, c and y with a motion, doubled, and with a count.
func TestVimOperators(t *testing.T) {
	m := vimModel(t, "one\ntwo\nthree\nfour\n")
	press(m, "d", "d")

	if got := m.pv.text(); got != "two\nthree\nfour\n" {
		t.Fatalf("dd deletes the line: %q", got)
	}

	press(m, "u")

	if got := m.pv.text(); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("u undoes it: %q", got)
	}

	press(m, "g", "g", "2", "d", "d") // undo leaves the cursor at the restored text

	if got := m.pv.text(); got != "three\nfour\n" {
		t.Fatalf("2dd deletes two lines: %q", got)
	}
	// The deleted lines are the register: p puts them back under the cursor.
	press(m, "p")

	if got := m.pv.text(); got != "three\none\ntwo\nfour\n" {
		t.Fatalf("p pastes the lines below: %q", got)
	}

	press(m, "g", "g", "y", "y", "G", "p")

	if got := m.pv.text(); got != "three\none\ntwo\nfour\nthree\n" {
		t.Fatalf("yy then p at the end of the file: %q", got)
	}

	press(m, "g", "g", "x")

	if got := m.pv.text(); got != "hree\none\ntwo\nfour\nthree\n" {
		t.Fatalf("x deletes the rune under the cursor: %q", got)
	}

	press(m, "d", "$")

	if got := m.pv.text(); got != "\none\ntwo\nfour\nthree\n" {
		t.Fatalf("d$ deletes to the line end: %q", got)
	}

	press(m, "j", "c", "w", "ONE")

	if got := m.pv.text(); got != "\nONE\ntwo\nfour\nthree\n" || m.pv.vim.mode != vimInsert {
		t.Fatalf("cw changes the word and inserts: %q mode %d", got, m.pv.vim.mode)
	}

	press(m, "esc", "j", "0", "d", "w")

	if got := m.pv.text(); got != "\nONE\n\nfour\nthree\n" {
		t.Fatalf("dw deletes the word: %q", got)
	}

	press(m, "j", "J")

	if got := m.pv.text(); got != "\nONE\n\nfour three\n" {
		t.Fatalf("J joins the line below: %q", got)
	}
}

// TestVimVisual selects with v and V and runs an operator over the selection.
func TestVimVisual(t *testing.T) {
	m := vimModel(t, "abcdef\nsecond\nthird\n")
	press(m, "v", "l", "l", "d") // v is inclusive: three runes go

	if got := m.pv.text(); got != "def\nsecond\nthird\n" {
		t.Fatalf("v then d: %q", got)
	}

	if m.pv.vim.mode != vimNormal || m.pv.anchor != nil {
		t.Fatalf("d leaves visual mode: mode %d anchor %v", m.pv.vim.mode, m.pv.anchor)
	}

	press(m, "V", "j", "y") // two lines yanked

	if got := m.pv.text(); got != "def\nsecond\nthird\n" {
		t.Fatalf("V then y changes nothing: %q", got)
	}

	press(m, "G", "p")

	if got := m.pv.text(); got != "def\nsecond\nthird\ndef\nsecond\n" {
		t.Fatalf("the line-wise yank pastes as lines: %q", got)
	}

	press(m, "g", "g", "V", "d")

	if got := m.pv.text(); got != "second\nthird\ndef\nsecond\n" {
		t.Fatalf("V then d deletes the line: %q", got)
	}

	press(m, "v", "l", "esc")

	if m.pv.vim.mode != vimNormal || m.pv.anchor != nil {
		t.Fatalf("esc drops the selection: mode %d", m.pv.vim.mode)
	}
}

// TestVimKeepsEditorKeys: the editor's own keys work in either mode, and a
// read-only preview is untouched by the setting.
func TestVimKeepsEditorKeys(t *testing.T) {
	m := vimModel(t, "one\ntwo\n")
	press(m, "ctrl+f")

	if !m.pv.find.editing {
		t.Fatal("ctrl+f still opens find in normal mode")
	}

	press(m, "esc")
	press(m, "/")

	if !m.pv.find.editing {
		t.Fatal("/ opens find")
	}

	press(m, "esc")
	press(m, "down")

	if m.pv.at().line != 1 {
		t.Fatal("the arrows still move")
	}

	press(m, "d", "d", "ctrl+z")

	if got := m.pv.text(); got != "one\ntwo\n" {
		t.Fatalf("ctrl+z undoes a vim edit: %q", got)
	}
	// A diff keeps its own single letters whatever the setting says.
	m.pv = preview{kind: pvDiff, ready: true}
	if m.pv.vimOn(m) {
		t.Fatal("vim mode never claims a read-only preview")
	}
}
