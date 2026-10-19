package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The status bar blames the cursor's line once it rests: the author and
// when, the commit a click away; a line typed since is not committed yet,
// and a narrow bar drops the item first.
func TestStatusBarBlame(t *testing.T) {
	m := testModelSized(t, 140, 30)
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "Ann Example"},
	} {
		mustGit(t, m.ws, a...)
	}

	path := filepath.Join(m.ws, "a.txt")
	mustWrite(t, path, "one\ntwo\n")
	mustGit(t, m.ws, "add", "a.txt")
	mustGit(t, m.ws, "commit", "-qm", "add a")

	for _, msg := range runAll(m.openFileAt(path, 1, 0, 0, false)) {
		if pm, ok := msg.(previewMsg); ok {
			m.Update(pm)
		}
	}

	m.focus = onMain
	status := func() string { s, _ := m.statusLine(m.w); return ansi.Strip(s) }
	rest := func() { // the cursor rests: Update noted its line, the tick comes, then git's answer
		t.Helper()

		m.Update(tea.MouseMotionMsg{X: 0, Y: m.h - 1})
		_, cmd := m.Update(blameTickMsg{m.blame.seq})

		for _, msg := range runAll(cmd) {
			m.Update(msg)
		}
	}

	rest()

	if s := status(); !strings.Contains(s, "Ann Example, now") {
		t.Fatalf("no blame: %q", s)
	}

	s := status()
	i := strings.Index(s, "Ann Example")
	m.statusMouse(ansi.StringWidth(s[:i]) + 1)

	if m.pv.kind != pvShow || m.pv.rev != m.blame.got.b.Hash {
		t.Fatalf("a click opens %s %s, want the commit", m.pv.kind, m.pv.rev)
	}

	for _, msg := range runAll(m.navGo(-1)) { // Go Back to the file
		if pm, ok := msg.(previewMsg); ok {
			m.Update(pm)
		}
	}

	m.focus = onMain
	m.pv.gotoLine(m, 1, 0)
	press(m, "x")
	rest()

	if s := status(); !strings.Contains(s, "Not Committed Yet") {
		t.Fatalf("an edited line: %q", s)
	}

	m.w = 50

	if s := status(); strings.Contains(s, "Not Committed Yet") || !strings.Contains(s, "Ln 2") {
		t.Fatalf("a narrow bar: %q", s)
	}
}
