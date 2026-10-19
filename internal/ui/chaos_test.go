package ui

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// chaosKeys are the keys TestChaos presses: text, the editor's and the
// views' own, the commands, and keys that open and close modals.
var chaosKeys = func() []tea.KeyPressMsg {
	var ks []tea.KeyPressMsg
	for _, s := range strings.Fields("a b x z q y n m d s e w o p r u v H D R N O F C Y 1 2 3 4 5 ( ) , . / [ ] { } : @ ? > space " +
		"enter esc tab up down left right home end pgup pgdown f2 f3 f5 f8 f12 shift+up shift+down shift+left shift+right " +
		"shift+tab shift+f8 shift+f12 alt+up alt+down alt+z alt+v alt+shift+right alt+shift+left alt+, alt+. alt+t alt+enter " +
		"ctrl+p ctrl+f ctrl+h ctrl+g ctrl+s ctrl+z ctrl+y ctrl+a ctrl+c ctrl+w ctrl+n ctrl+b ctrl+k ctrl+j ctrl+d ctrl+/ " +
		"ctrl+. ctrl+space ctrl+shift+p ctrl+shift+f ctrl+shift+o ctrl+shift+m ctrl+shift+e ctrl+shift+g ctrl+0 ctrl+1 ctrl+] " +
		"ctrl+pgdown ctrl+pgup ctrl+up ctrl+down ctrl+tab ctrl+shift+[ ctrl+shift+] ctrl+{ ctrl+}") {
		ks = append(ks, keyMsg(s))
	}

	return append(ks, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyDelete})
}()

// TestChaos drives the model with random keys, clicks, drags, wheels,
// resizes and opened files, from a seed, as the chaos monkey of ttt does:
// after every event the model has not panicked and every row is the
// window's width. Commands are not run (they would reach a daemon, a
// timer or the desktop); an opened file's load is. A failure logs its
// seed and the events before it: PANDO_CHAOS_SEED=N replays it,
// PANDO_CHAOS_STEPS=N runs longer.
func TestChaos(t *testing.T) {
	seeds, steps := []uint64{1, 2, 3}, 400
	if testing.Short() {
		seeds, steps = seeds[:1], 150
	}

	if s := os.Getenv("PANDO_CHAOS_SEED"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("PANDO_CHAOS_SEED %q: %v", s, err)
		}

		seeds = []uint64{n}
	}

	if s := os.Getenv("PANDO_CHAOS_STEPS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("PANDO_CHAOS_STEPS %q: %v", s, err)
		}

		steps = n
	}
	// Nothing it does may reach the person's own pando or desktop: no
	// daemon to call, no xdg-open to open a file with.
	dir := t.TempDir()
	for _, k := range []string{"PANDO_RUNTIME_DIR", "PANDO_CONFIG_DIR", "PANDO_DATA_DIR"} {
		t.Setenv(k, filepath.Join(dir, k))
	}

	for _, tool := range []string{"xdg-open", "open"} {
		mustWrite(t, filepath.Join(dir, "bin", tool), "#!/bin/sh\n")

		if err := os.Chmod(filepath.Join(dir, "bin", tool), 0o755); err != nil { // the model runs it
			t.Fatalf("chmod %s: %v", tool, err)
		}
	}

	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, seed := range seeds {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) { chaos(t, seed, steps) })
	}
}

func chaos(t *testing.T, seed uint64, steps int) {
	t.Helper()

	r := rand.New(rand.NewPCG(seed, seed))
	m, path := editorModel(t, "a.go", "package a\n\nimport \"fmt\"\n\n// A says hi.\nfunc A(n int) {\n\tfor i := range n {\n\t\tfmt.Println(\"héllo 😀\", i)\n\t}\n}\n")
	mustWrite(t, filepath.Join(m.ws, "notes.md"), "# Notes\n\n- one\n- two\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nx := 1\n```\n")
	mustWrite(t, filepath.Join(m.ws, "wide.txt"), strings.Repeat("ｗｉｄｅ 漢字 ", 40)+"\n\ttab\n")
	files := []string{path, filepath.Join(m.ws, "notes.md"), filepath.Join(m.ws, "wide.txt"), filepath.Join(m.ws, "README.md")}

	var log []string

	defer func() {
		if p := recover(); p != nil {
			t.Errorf("panic: %v\n%s", p, debug.Stack())
		}

		if t.Failed() {
			t.Logf("seed %d, the last events (PANDO_CHAOS_SEED=%d replays them):\n  %s", seed, seed, strings.Join(log[max(len(log)-30, 0):], "\n  "))
		}
	}()

	for step := range steps {
		var msg tea.Msg

		switch n := r.IntN(100); {
		case n < 60:
			k := chaosKeys[r.IntN(len(chaosKeys))]
			msg = k
			log = append(log, fmt.Sprintf("%d key %s", step, k.String()))

		case n < 75:
			x, y := r.IntN(m.w), r.IntN(m.termH)
			b := []tea.MouseButton{tea.MouseLeft, tea.MouseLeft, tea.MouseRight}[r.IntN(3)]
			m.Update(tea.MouseClickMsg{X: x, Y: y, Button: b})
			msg = tea.MouseReleaseMsg{X: x, Y: y, Button: b}
			log = append(log, fmt.Sprintf("%d click %d,%d button %d", step, x, y, b))

		case n < 83:
			x, y := r.IntN(m.w), r.IntN(m.termH)
			msg = tea.MouseMotionMsg{X: x, Y: y, Button: []tea.MouseButton{tea.MouseNone, tea.MouseNone, tea.MouseLeft}[r.IntN(3)]}
			log = append(log, fmt.Sprintf("%d move %d,%d", step, x, y))

		case n < 90:
			x, y := r.IntN(m.w), r.IntN(m.termH)
			msg = tea.MouseWheelMsg{X: x, Y: y, Button: []tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft}[r.IntN(3)]}
			log = append(log, fmt.Sprintf("%d wheel %d,%d", step, x, y))

		case n < 95:
			w, h := 20+r.IntN(160), 6+r.IntN(50)
			msg = tea.WindowSizeMsg{Width: w, Height: h}
			log = append(log, fmt.Sprintf("%d resize %dx%d", step, w, h))

		default: // a file opened, as quick open would: its load delivered
			f := files[r.IntN(len(files))]
			log = append(log, fmt.Sprintf("%d open %s", step, filepath.Base(f)))

			for _, msg := range runAll(m.openFileAt(f, r.IntN(12), r.IntN(10), 0, r.IntN(2) == 0)) {
				if pm, ok := msg.(previewMsg); ok {
					m.Update(pm)
				}
			}

			msg = tea.FocusMsg{}
		}

		m.Update(msg)
		chaosWidths(t, m)

		if t.Failed() {
			return
		}
	}
}

// chaosWidths is checkWidths reporting with Errorf, so the events leading
// up to it are logged.
func chaosWidths(t *testing.T, m *Model) {
	t.Helper()

	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != m.termH {
		t.Errorf("view has %d lines, want %d", len(lines), m.termH)
		return
	}

	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Errorf("line %d width %d, want %d: %q", i, w, m.w, ansi.Strip(l))
			return
		}
	}
}
