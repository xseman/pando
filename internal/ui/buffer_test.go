package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func TestBufferSplice(t *testing.T) {
	b := newBuffer("one\ntwo\nthree\n", time.Time{})
	if len(b.lines) != 3 || b.text() != "one\ntwo\nthree" || !b.endNL {
		t.Fatalf("lines = %d, text = %q", len(b.lines), b.text())
	}
	// Insert inside a line, then across lines.
	if end := b.apply(pos{1, 3}, pos{1, 3}, "!", pos{1, 3}); end != (pos{1, 4}) {
		t.Fatalf("insert end = %+v", end)
	}

	if b.text() != "one\ntwo!\nthree" {
		t.Fatalf("after insert: %q", b.text())
	}

	if end := b.apply(pos{0, 1}, pos{2, 2}, "X\nY", pos{0, 1}); end != (pos{1, 1}) {
		t.Fatalf("multiline end = %+v", end)
	}

	if b.text() != "oX\nYree" {
		t.Fatalf("after multiline: %q", b.text())
	}

	if got := b.textIn(pos{0, 1}, pos{1, 1}); got != "X\nY" {
		t.Fatalf("textIn = %q", got)
	}
	// Undo walks back to the original, redo forward again.
	for range 2 {
		if _, ok := b.undo(); !ok {
			t.Fatal("undo ran out early")
		}
	}

	if b.text() != "one\ntwo\nthree" {
		t.Fatalf("after undo: %q", b.text())
	}

	if _, ok := b.undo(); ok {
		t.Fatal("nothing left to undo")
	}

	if _, ok := b.redo(); !ok || b.text() != "one\ntwo!\nthree" {
		t.Fatalf("after redo: %q", b.text())
	}
}

func TestBufferTypingCoalesces(t *testing.T) {
	b := newBuffer("ab\n", time.Time{})

	at := pos{0, 2}
	for _, r := range "cde" {
		at = b.apply(at, at, string(r), at)
	}

	if b.text() != "abcde" || len(b.undos) != 1 {
		t.Fatalf("typing: %q with %d undo steps", b.text(), len(b.undos))
	}

	if p, _ := b.undo(); b.text() != "ab" || p != (pos{0, 2}) {
		t.Fatalf("one undo takes the whole word back: %q at %+v", b.text(), p)
	}
	// A newline breaks the run, so it undoes on its own.
	b.apply(pos{0, 2}, pos{0, 2}, "x", pos{0, 2})
	b.apply(pos{0, 3}, pos{0, 3}, "\n", pos{0, 3})
	b.apply(pos{1, 0}, pos{1, 0}, "y", pos{1, 0})
	b.undo()

	if b.text() != "abx\n" {
		t.Fatalf("after undoing y: %q", b.text())
	}
}

func TestBufferSave(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	b := newBuffer("one\n", st.ModTime())
	b.apply(pos{0, 3}, pos{0, 3}, "!", pos{0, 3})

	if err := b.save(path, false); err != nil {
		t.Fatal(err)
	}

	if got := mustRead(t, path); got != "one!\n" || b.dirty {
		t.Fatalf("saved %q, dirty=%v", got, b.dirty)
	}

	if st, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	} else if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 600", st.Mode().Perm())
	}
	// Someone else writes: the save refuses until it is forced.
	touch(t, path)
	b.apply(pos{0, 4}, pos{0, 4}, "?", pos{0, 4})

	if err := b.save(path, false); !errors.Is(err, errChangedOnDisk) {
		t.Fatalf("save over a changed file = %v, want %v", err, errChangedOnDisk)
	}

	if err := b.save(path, true); err != nil {
		t.Fatal(err)
	}

	if got := mustRead(t, path); got != "one!?\n" {
		t.Fatalf("forced save wrote %q", got)
	}
}

// editorModel is a file preview with a real file behind it, ready to type in.
func editorModel(t *testing.T, name, text string) (*Model, string) {
	t.Helper()
	m := testModelSized(t, 120, 30)
	path := filepath.Join(m.ws, name)
	mustWrite(t, path, text)
	st, _ := os.Stat(path)
	lines, plain, meta, numW := render(pvFile, path, text, true)
	m.pv = preview{kind: pvFile, path: path}
	m.preview, m.focus = true, onMain
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: text, mod: st.ModTime(), lines: lines, plain: plain, meta: meta, numW: numW})

	return m, path
}

func TestEditorTyping(t *testing.T) {
	m, path := editorModel(t, "a.go", "package a\n\nfunc A() {}\n")
	if !m.pv.editable() {
		t.Fatal("a file preview is an editor")
	}
	// Letters type instead of running commands, and the tab is marked dirty.
	m.pv.cur = pos{2, 12}
	press(m, "/", "/", " ", "h", "i")

	if got := string(m.pv.buf.line(2)); got != "func A() {}// hi" {
		t.Fatalf("typed line = %q", got)
	}

	if !m.pv.dirty() || !strings.Contains(checkWidths(t, m), "●") {
		t.Fatalf("dirty=%v, the tab shows a dot", m.pv.dirty())
	}
	// The highlighter ran again, so the view matches the buffer.
	if string(m.pv.plain[2]) != "func A() {}// hi" {
		t.Fatalf("view line = %q", string(m.pv.plain[2]))
	}
	// ⏎ keeps the indent, ⌫ joins lines back.
	m.pv.cur = pos{2, 0}
	press(m, "tab", "x", "enter", "y")

	if got := m.pv.buf.text(); got != "package a\n\n\tx\n\tyfunc A() {}// hi" {
		t.Fatalf("after enter: %q", got)
	}

	press(m, "backspace", "backspace")

	if got := string(m.pv.buf.line(3)); got != "func A() {}// hi" { // 'y' then the indent tab
		t.Fatalf("after backspace: %q", got)
	}
	// ⌃z walks the typing back, ⌃s writes the file.
	for range 20 {
		if _, ok := m.pv.buf.undo(); !ok {
			break
		}
	}

	m.pv.refreshAll(m)

	if got := m.pv.buf.text(); got != "package a\n\nfunc A() {}" {
		t.Fatalf("undone to %q", got)
	}

	press(m, "ctrl+s")

	if b, _ := os.ReadFile(path); string(b) != "package a\n\nfunc A() {}\n" || m.pv.dirty() {
		t.Fatalf("saved %q dirty=%v", b, m.pv.dirty())
	}
}

func TestEditorLineOps(t *testing.T) {
	m, _ := editorModel(t, "b.py", "one\ntwo\nthree\n")
	m.pv.cur = pos{1, 0}
	press(m, "ctrl+d") // duplicate

	if got := m.pv.buf.text(); got != "one\ntwo\ntwo\nthree" {
		t.Fatalf("duplicate: %q", got)
	}

	press(m, "alt+down") // move it down

	if got := m.pv.buf.text(); got != "one\ntwo\nthree\ntwo" {
		t.Fatalf("move down: %q", got)
	}

	m.pv.cur = pos{3, 0}
	press(m, "ctrl+shift+k") // delete the line

	if got := m.pv.buf.text(); got != "one\ntwo\nthree" {
		t.Fatalf("delete line: %q", got)
	}
	// ⌃/ comments with the language's marker and toggles back.
	m.pv.cur = pos{0, 0}
	press(m, "ctrl+/")

	if got := string(m.pv.buf.line(0)); got != "# one" {
		t.Fatalf("commented = %q", got)
	}

	press(m, "ctrl+/")

	if got := string(m.pv.buf.line(0)); got != "one" {
		t.Fatalf("uncommented = %q", got)
	}
}

func TestEditorCopyLines(t *testing.T) {
	m, _ := editorModel(t, "c.py", "one\ntwo\nthree\n")
	m.pv.cur = pos{1, 0}
	press(m, "alt+shift+down") // the cursor follows the copy

	if got := m.pv.buf.text(); got != "one\ntwo\ntwo\nthree" || m.pv.cur != (pos{2, 0}) {
		t.Fatalf("copy down: %q cur %v", got, m.pv.cur)
	}

	press(m, "alt+shift+up") // the cursor stays

	if got := m.pv.buf.text(); got != "one\ntwo\ntwo\ntwo\nthree" || m.pv.cur != (pos{2, 0}) {
		t.Fatalf("copy up: %q cur %v", got, m.pv.cur)
	}
	// A selection ending at column 0 leaves that line out; the selection lands on the copy.
	m.pv.anchor, m.pv.cur = &pos{0, 1}, pos{2, 0}
	press(m, "alt+shift+down")

	if got := m.pv.buf.text(); got != "one\ntwo\none\ntwo\ntwo\ntwo\nthree" {
		t.Fatalf("copy selection down: %q", got)
	}

	if m.pv.anchor == nil || *m.pv.anchor != (pos{2, 1}) || m.pv.cur != (pos{4, 0}) || m.pv.selectedText() != "ne\ntwo\n" {
		t.Fatalf("selection after copy: anchor %v cur %v %q", m.pv.anchor, m.pv.cur, m.pv.selectedText())
	}

	press(m, "ctrl+z") // one undo step

	if got := m.pv.buf.text(); got != "one\ntwo\ntwo\ntwo\nthree" {
		t.Fatalf("undo: %q", got)
	}
}

func TestEditorGuards(t *testing.T) {
	m, path := editorModel(t, "c.go", "package c\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0
	// A dirty editor is not overwritten by the periodic reload.
	press(m, "x")

	if cmd := m.pv.reloadIfLive(m); cmd != nil {
		t.Fatal("a dirty editor ignores the reload tick")
	}
	// Closing it asks first.
	if cmd := m.closeEditor(m.edIdx); cmd != nil || m.modal == nil {
		t.Fatalf("closing a dirty editor asks: %+v", m.modal)
	}

	m.modal = nil
	// A foreign write is not clobbered silently.
	mustWrite(t, path, "package c // theirs\n")
	touch(t, path)
	m.pv.save(m, false)

	if m.modal == nil || !strings.Contains(m.modal.title, "changed on disk") {
		t.Fatalf("save over a foreign write asks: %+v", m.modal)
	}
	// Diffs and revisions stay read-only.
	m.pv.kind = "diff"
	if m.pv.editable() {
		t.Fatal("a diff is not editable")
	}
}

// Unsaved text follows the editor: switching tabs, reopening, even the
// periodic reload leave it alone.
func TestEditorKeepsUnsavedText(t *testing.T) {
	m, path := editorModel(t, "d.go", "package d\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0
	press(m, "x")

	if !m.pv.dirty() {
		t.Fatal("typing makes it dirty")
	}
	// A reload of the same file keeps the buffer and redraws from it.
	st, _ := os.Stat(path)
	lines, plain, _, numW := render(pvFile, path, "package d\n", true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: "package d\n", mod: st.ModTime(), lines: lines, plain: plain, numW: numW})

	if !m.pv.dirty() || m.pv.buf.text() != "xpackage d" || string(m.pv.plain[0]) != "xpackage d" {
		t.Fatalf("reload lost the edit: dirty=%v text=%q view=%q", m.pv.dirty(), m.pv.buf.text(), string(m.pv.plain[0]))
	}
	// So does opening the same file again through the tab strip.
	snap := m.pv.snapshot()
	m.pv = preview{}
	m.pv = snap
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: "package d\n", mod: st.ModTime(), lines: lines, plain: plain, numW: numW})

	if m.pv.buf.text() != "xpackage d" {
		t.Fatalf("reopening lost the edit: %q", m.pv.buf.text())
	}
}

func TestQuitWarnsAboutUnsavedFiles(t *testing.T) {
	m, path := editorModel(t, "e.go", "package e\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0
	press(m, "x")
	m.focus = 0
	press(m, "q")

	if m.modal == nil || !strings.Contains(m.modal.title, "1 file unsaved") {
		t.Fatalf("quit warns: %+v", m.modal)
	}
	// Save all writes them before going.
	m.saveAll()

	if b, _ := os.ReadFile(path); string(b) != "xpackage e\n" || m.pv.dirty() {
		t.Fatalf("save all wrote %q dirty=%v", b, m.pv.dirty())
	}
}

// TestWordHighlight is VS Code's occurrence highlight: the word under the
// cursor is painted wherever else it stands, and a selection turns it off.
func TestWordHighlight(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nvar total, totals int\n\nfunc f() { total = 1 }\n")
	m.pv.setCursor(m, pos{2, 5}) // inside "total"

	if got := string(m.pv.hlWord()); got != "total" {
		t.Fatalf("word under the cursor = %q", got)
	}

	m.pv.hl = m.pv.hlWord()
	// "totals" is a longer word, so it does not count as an occurrence.
	if spans := m.pv.wordSpans(m.pv.plain[2]); len(spans) != 1 || spans[0] != [2]int{4, 9} {
		t.Fatalf("spans on the declaration = %v", spans)
	}

	if spans := m.pv.wordSpans(m.pv.plain[4]); len(spans) != 1 {
		t.Fatalf("spans on the use = %v", spans)
	}
	// A selection replaces it, and a single letter is not worth painting.
	m.pv.anchor = &pos{2, 4}
	if m.pv.hlWord() != nil {
		t.Fatal("a selection still highlighted a word")
	}

	m.pv.anchor = nil
	m.pv.setCursor(m, pos{4, 13})
	m.pv.plain[4] = []rune("func f() { x = 1 }")
	m.pv.setCursor(m, pos{4, 11})

	if got := m.pv.hlWord(); got != nil {
		t.Fatalf("one letter highlighted: %q", string(got))
	}
}

// ctrl+up and ctrl+down scroll the view and leave the cursor alone.
func TestScrollLineKeys(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n"+strings.Repeat("// line\n", 200))
	m.pv.setCursor(m, pos{0, 0})
	cur, top := m.pv.cur, m.pv.top
	press(m, "ctrl+down")
	press(m, "ctrl+down")

	if m.pv.top != top+2 || m.pv.cur != cur {
		t.Fatalf("ctrl+down: top %d→%d, cursor %+v", top, m.pv.top, m.pv.cur)
	}

	press(m, "ctrl+up")

	if m.pv.top != top+1 {
		t.Fatalf("ctrl+up: top = %d", m.pv.top)
	}
}

// The highlight follows the cursor onto a name the file uses, and stays away
// while one is being typed — VS Code cancels its word highlighter on a change.
func TestWordHighlightNotWhileTyping(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nvar total int\n\nfunc f() { total = 1 }\n")
	m.pv.setCursor(m, pos{4, 15}) // inside "total", where it is used

	if got := string(m.pv.hlWord()); got != "total" {
		t.Fatalf("moving onto a used name highlights it: %q", got)
	}

	// Typing a new name does not light it up, however long it grows.
	m.pv.setCursor(m, pos{1, 0})

	for _, r := range []string{"d", "s", "a", "d"} {
		press(m, r)

		if got := m.pv.hlWord(); got != nil {
			t.Fatalf("highlighted %q while it was being typed", string(got))
		}
	}

	if string(m.pv.buf.line(1)) != "dsad" {
		t.Fatalf("typed %q", string(m.pv.buf.line(1)))
	}
	// It stands nowhere else, so moving over it lights nothing either.
	press(m, "left")

	if got := m.pv.hlWord(); got != nil {
		t.Fatalf("a name used once highlighted: %q", string(got))
	}
	// A name the file does use lights up as soon as the cursor lands on it.
	m.pv.setCursor(m, pos{2, 5})
	press(m, "right")

	if got := string(m.pv.hlWord()); got != "total" {
		t.Fatalf("after moving onto a used name: %q", got)
	}
	// Editing it drops the highlight again, undo included.
	press(m, "x")

	if m.pv.hlWord() != nil {
		t.Fatal("still highlighted after an edit")
	}

	press(m, "ctrl+z")

	if m.pv.hlWord() != nil {
		t.Fatal("still highlighted after an undo")
	}

	press(m, "left")

	if got := string(m.pv.hlWord()); got != "total" {
		t.Fatalf("back after the cursor moved: %q", got)
	}
}

// A jump is the cursor being moved on purpose, so the highlight comes back:
// go to line, a search hit, a symbol and a definition all go through gotoLine.
func TestWordHighlightAfterAJump(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nvar total int\n\nfunc f() { total = 1 }\n")
	press(m, "x") // an edit takes the highlight away

	if m.pv.hlWord() != nil {
		t.Fatal("highlighted right after an edit")
	}

	m.pv.gotoLine(m, 4, 15) // onto "total", where it is used

	if got := string(m.pv.hlWord()); got != "total" {
		t.Fatalf("after a jump: %q", got)
	}
}

// A click that the mouse nudges during is still a click: the drag maps the
// pointer through the same rows the click did, editor strip included.
func TestClickWithMotionDoesNotSelectTheNextLine(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nfunc A() {}\n\nfunc B() {}\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0

	if m.stripH() != 1 {
		t.Fatalf("an open editor draws a tab strip, got stripH %d", m.stripH())
	}

	x, y := m.mainX()+m.pv.numW+2, 1+m.stripH()+2 // the third line of the file
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})

	clicked := m.pv.cur.line
	if clicked != 2 {
		t.Fatalf("clicked line %d, want 2", clicked)
	}
	// The pointer reports where it already is before the button comes up.
	m.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if _, _, ok := m.pv.selection(); ok {
		a, b, _ := m.pv.selection()
		t.Errorf("a click selected %v-%v", a, b)
	}

	if m.pv.cur.line != clicked {
		t.Errorf("the cursor moved to line %d, want %d", m.pv.cur.line, clicked)
	}
}

// TestRenderWhitespace is render_whitespace, VS Code's modes: a space drawn
// as ·, a tab as → over the four cells it takes, in the whitespace color.
func TestRenderWhitespace(t *testing.T) {
	m, _ := editorModel(t, "w.txt", "\tif a  b c \n")
	line := m.pv.plain[0] // "    if a  b c "

	shown := func(mode string) string {
		t.Helper()

		m.pv.blanks = mode

		marks := m.pv.blankMarks(0, line)
		out := []rune(string(line))

		for j, mk := range marks {
			if mk != "" {
				out[j], _ = utf8.DecodeRuneInString(mk)
			}
		}

		return string(out)
	}

	for mode, want := range map[string]string{
		"none":      "    if a  b c ",
		"all":       "→   if·a··b·c·",
		"boundary":  "→   if a··b c·",
		"trailing":  "    if a  b c·",
		"selection": "    if a  b c ",
	} {
		if got := shown(mode); got != want {
			t.Errorf("%s: %q, want %q", mode, got, want)
		}
	}

	m.pv.anchor, m.pv.cur = &pos{0, 6}, pos{0, 10}

	if got := shown("selection"); got != "    if·a··b c " {
		t.Errorf("selection: %q", got)
	}

	m.st.Settings.Blanks = "all"
	m.pv.anchor = nil

	out := checkWidths(t, m)
	if !strings.Contains(out, "→   if·a··b·c·") {
		t.Fatalf("the editor draws the markers:\n%s", out)
	}

	if !strings.Contains(m.View().Content, fgParams(pal.whitespace)) {
		t.Fatal("markers are in the whitespace color")
	}

	m.st.Settings.Blanks = "selection"
	m.pv.anchor, m.pv.cur = &pos{0, 6}, pos{0, 10}

	if out := checkWidths(t, m); !strings.Contains(out, "if·a··b c") {
		t.Fatalf("a selection shows its blanks:\n%s", out)
	}

	if !strings.Contains(m.View().Content, bgParams(pal.textSelBg)+";"+fgParams(pal.whitespace)) &&
		!strings.Contains(m.View().Content, fgParams(pal.whitespace)+";"+bgParams(pal.textSelBg)) {
		t.Fatal("selected markers keep the whitespace color over the selection")
	}
}
