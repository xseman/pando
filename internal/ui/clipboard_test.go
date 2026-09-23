package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestMain keeps the tests off the desktop's clipboard: a copy a test makes
// would otherwise land where the person running them pastes.
func TestMain(m *testing.M) {
	for _, k := range []string{"WAYLAND_DISPLAY", "DISPLAY"} {
		_ = os.Unsetenv(k) // unset on a machine without them already
	}

	os.Exit(m.Run())
}

// fakeClipboard puts a wl-copy and wl-paste on PATH that keep the clipboard
// in a file, and returns that file.
func fakeClipboard(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	board := filepath.Join(dir, "board")
	mustWrite(t, board, "")
	// Like wl-copy, it stays behind to serve the clipboard once it has read it.
	mustWrite(t, filepath.Join(dir, "wl-copy"), "#!/bin/sh\ncat > '"+board+"'\nsleep 30 </dev/null >/dev/null 2>&1 &\n")
	mustWrite(t, filepath.Join(dir, "wl-paste"), "#!/bin/sh\ncat '"+board+"'\n")

	for _, tool := range []string{"wl-copy", "wl-paste"} {
		if err := os.Chmod(filepath.Join(dir, tool), 0o755); err != nil { // the test runs them
			t.Fatalf("chmod %s: %v", tool, err)
		}
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")) // ahead of the real ones
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")

	return board
}

// runAll runs cmd and every command a batch holds, the way the program would,
// and returns the messages they produce.
func runAll(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runAll(c)...)
		}

		return out
	}

	return []tea.Msg{msg}
}

// TestClipboardIsTheDesktops is one clipboard everywhere: a copy from an
// editor or a terminal selection goes to the desktop's tool, not only to the
// terminal as OSC 52 (which VTE drops), and ctrl+v in a file reads it back.
func TestClipboardIsTheDesktops(t *testing.T) {
	board := fakeClipboard(t)

	m, _ := editorModel(t, "a.go", "package x\n")
	m.pv.anchor, m.pv.cur = &pos{0, 0}, pos{0, 7}

	_, cmd := m.Update(keyMsg("ctrl+c"))

	var flashed string

	start := time.Now()

	for _, msg := range runAll(cmd) {
		if f, ok := msg.(flashMsg); ok {
			flashed = f.text
		}
	}

	if got := mustRead(t, board); got != "package" || flashed != "copied 7 characters" {
		t.Fatalf("copy: board %q, flash %q", got, flashed)
	}

	if d := time.Since(start); d > time.Second { // the tool left running holds nothing of ours
		t.Fatalf("the copy waited %v for the tool that stays behind", d)
	}

	// What another app copied pastes into the file with ctrl+v.
	mustWrite(t, board, "func ")

	m.pv.anchor, m.pv.cur = nil, pos{1, 0}

	_, cmd = m.Update(keyMsg("ctrl+v"))
	for _, msg := range runAll(cmd) {
		m.Update(msg)
	}

	if got := strings.Join(runesLines(m.pv.plain), "\n"); !strings.Contains(got, "func ") {
		t.Fatalf("ctrl+v pasted nothing: %q", got)
	}

	// Without a tool the copy still goes out as OSC 52, and says so.
	t.Setenv("WAYLAND_DISPLAY", "")

	for _, msg := range runAll(setClipboard("x", "copied x")) {
		if f, ok := msg.(flashMsg); ok && (!strings.Contains(f.text, "OSC 52 only") || f.err) {
			t.Fatalf("no tool: %+v", f)
		}
	}
}

func runesLines(plain [][]rune) []string {
	out := make([]string, len(plain))
	for i, l := range plain {
		out[i] = string(l)
	}

	return out
}
