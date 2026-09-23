package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xseman/pando/internal/proto"
)

// TestShellCandidates is the order a shell session tries: the shell setting,
// the agent's preset, the shell preset, the login shell, then bash and sh,
// each once.
func TestShellCandidates(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/zsh")

	d := &Daemon{state: proto.State{
		Settings: proto.Settings{Shell: "fish -l"},
		Agents:   map[string][]string{"terminal": {"/bin/fish"}, "shell": {"bash"}},
	}}

	got := d.shellCandidates("terminal")
	want := [][]string{{"fish", "-l"}, {"/bin/fish"}, {"bash"}, {"/usr/bin/zsh"}, {"/bin/sh"}}

	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}

	d.state.Settings.Shell = ""
	if got := d.shellCandidates("tab"); !slices.Equal(got[0], []string{"bash"}) {
		t.Fatalf("no setting: the shell preset first, got %q", got)
	}
}

// TestShellFallback: a shell setting naming nothing installed, or a shell
// that fails as soon as it starts, gives way to the next shell - the
// session opens all the same and says why on its screen.
func TestShellFallback(t *testing.T) {
	boot := start(t)

	d := boot()
	defer d.Close()

	ws := t.TempDir()
	broken := filepath.Join(ws, "broken-shell")
	mustWrite(t, broken, "#!/bin/sh\nexit 3\n")

	if err := os.Chmod(broken, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", broken, err)
	}

	screen := func(id string) string {
		t.Helper()

		var scr proto.Screen
		call(t, "session.screen", proto.ScreenParams{ID: id, Cols: 100, Rows: 8}, &scr)

		return strings.Join(scr.Lines, "\n")
	}

	for _, c := range []struct{ shell, says string }{
		{"no-such-shell-pando", "no-such-shell-pando did not start"},
		{broken, "broken-shell exited as soon as it started"},
	} {
		call(t, "state.set", map[string]any{"settings": map[string]any{"shell": c.shell}}, nil)

		var s proto.Session
		call(t, "session.new", map[string]any{"workspace": ws, "agent": "terminal"}, &s)

		var got proto.Session

		waitFor(t, "a shell that runs", func() bool {
			call(t, "session.get", map[string]string{"id": s.ID}, &got)
			return got.Status != "exited" && got.Cmd[0] != c.shell && strings.Contains(screen(s.ID), c.says)
		})

		if got.ID != s.ID {
			t.Fatalf("%s: the session changed its id: %+v", c.shell, got)
		}
	}
}
