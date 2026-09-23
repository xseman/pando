package ui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// sent is the next input the model forwarded to a session.
func sent(t *testing.T, m *Model) proto.InputParams {
	t.Helper()

	select {
	case in := <-m.inputs:
		return in
	case <-time.After(time.Second):
		t.Fatal("nothing reached the session")
	}

	return proto.InputParams{}
}

// TestKeyContexts is VS Code's when clauses: a chord means what the focused
// part makes of it. A terminal takes every key but the few it gives up, an
// editable file types its keys, and pando's own chords work in its panels.
func TestKeyContexts(t *testing.T) {
	m := testModel(t)
	m.switchSession("s1")
	m.focus = onMain

	if c := m.keyContext(); c != ctxTerminal {
		t.Fatalf("a session on screen is a terminal: %b", c)
	}
	// ⌃←/⌃→ move the shell's cursor by words; they used to walk editor history.
	for _, key := range []string{"ctrl+left", "ctrl+right", "ctrl+p", "ctrl+s", "ctrl+enter", "5", "f1", "alt+,"} {
		if _, ok := m.bindingFor(ctxTerminal, key); ok {
			t.Errorf("%s is pando's in a terminal, not the shell's", key)
		}
	}

	press(m, "ctrl+left")

	if in := sent(t, m); len(in.Keys) != 1 || in.Keys[0].Code != tea.KeyLeft || in.Keys[0].Mod != int(tea.ModCtrl) || m.msg != "" {
		t.Fatalf("ctrl+left reaches the session: %+v, flash %q", in, m.msg)
	}
	// The ways out, and VS Code's commandsToSkipShell, still work from there.
	for _, key := range []string{"ctrl+]", "ctrl+shift+p", "ctrl+j", "ctrl+b", "alt+t", "ctrl+pgdown"} {
		if _, ok := m.bindingFor(ctxTerminal, key); !ok {
			t.Errorf("%s does not leave the terminal", key)
		}
	}

	// An editable file types what it can type: 5 is a digit, not the panel.
	ed, _ := editorModel(t, "a.go", "package x\n")
	if c := ed.keyContext(); c != ctxText {
		t.Fatalf("an editable file is text: %b", c)
	}

	open := ed.termOpen()
	press(ed, "5")

	if ed.termOpen() != open || string(ed.pv.plain[0]) != "5package x" {
		t.Fatalf("5 in a file: terminal toggled %v, line %q", ed.termOpen() != open, string(ed.pv.plain[0]))
	}

	// In a list 5 is still the panel, and Go Back is on VS Code's keys.
	ed.focus = 0
	if c := ed.keyContext(); c != ctxList {
		t.Fatalf("a sidebar is a list: %b", c)
	}

	for _, key := range []string{"5", "ctrl+alt+-", "alt+,", "ctrl+p"} {
		if _, ok := ed.bindingFor(ctxList, key); !ok {
			t.Errorf("%s is not bound in a list", key)
		}
	}

	if _, ok := ed.bindingFor(ctxInput, "ctrl+p"); ok {
		t.Error("a text box keeps ctrl+p")
	}

	// [keys] reaches neither a terminal nor a text box.
	m.st.Settings.Keys = map[string]string{"ctrl+left": "view.showSearch"}
	press(m, "ctrl+left")

	if in := sent(t, m); len(in.Keys) != 1 || in.Keys[0].Code != tea.KeyLeft {
		t.Fatalf("a [keys] chord took a terminal's key: %+v", in)
	}
}

// TestWordMoves is VS Code's ctrl+← and ctrl+→ in an editor: past the
// blanks, then over one run of word characters or of punctuation, and over
// a line break at the ends; with shift they select.
func TestWordMoves(t *testing.T) {
	m, _ := editorModel(t, "a.go", "foo.bar(x)  baz\nnext\n")

	var got []int

	for range 6 {
		press(m, "ctrl+right")
		got = append(got, m.pv.cur.col)
	}

	if want := []int{3, 4, 7, 8, 9, 10}; !slices.Equal(got, want) {
		t.Fatalf("ctrl+right stops at %v, want %v", got, want)
	}

	press(m, "ctrl+right", "ctrl+right") // to the end of baz, then over the line break

	if c := m.pv.at(); c != (pos{1, 0}) {
		t.Fatalf("ctrl+right at the line's end goes to the next line's start: %+v", c)
	}

	press(m, "ctrl+left")

	if c := m.pv.at(); c != (pos{0, 15}) {
		t.Fatalf("ctrl+left at the line's start goes to the line above's end: %+v", c)
	}

	press(m, "ctrl+left", "ctrl+shift+left") // to baz's start, then back over the blanks and the )

	if got := m.pv.selectedText(); got != ")  " {
		t.Fatalf("ctrl+shift+left selects the word before: %q", got)
	}

	if m.msg != "" {
		t.Fatalf("a word move flashed %q", m.msg)
	}
}
