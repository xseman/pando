package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// What a modal costs: quick open over a large worktree, the palette, the
// frame dimmed under a box while a session streams under it.

// benchFiles are n paths shaped like a large monorepo's.
func benchFiles(n int) []string {
	files := make([]string, n)
	for i := range files {
		files[i] = fmt.Sprintf("packages/Pkg%03d/src/components/Sub%02d/Widget%04d.tsx", i/200, i/20%10, i)
	}

	return files
}

// quickModel is benchModel with quick open over 20 000 files on tab.
func quickModel(tb testing.TB, tab int) *Model {
	tb.Helper()

	m := benchModel(tb)
	m.quickOpen(indexMsg{ws: m.ws, files: benchFiles(20000)}, tab)
	_ = m.View()

	return m
}

// BenchmarkQuickOpenOpen is ctrl+p on a 20 000 file worktree: the lists, the
// commands, the first frame over the dimmed screen.
func BenchmarkQuickOpenOpen(b *testing.B) {
	m := benchModel(b)
	msg := indexMsg{ws: m.ws, files: benchFiles(20000)}

	b.ReportAllocs()

	for b.Loop() {
		m.modal = nil
		_ = m.View()

		m.quickOpen(msg, qoAll)
		_ = m.View()
	}
}

// BenchmarkQuickOpenType types a letter into a query and takes it back,
// each drawn: what every key in quick open costs.
func BenchmarkQuickOpenType(b *testing.B) {
	m := quickModel(b, qoAll)
	press(m, "w", "i", "d")
	_ = m.View()

	b.ReportAllocs()

	for b.Loop() {
		m.Update(keyMsg("g"))
		_ = m.View()
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		_ = m.View()
	}
}

// BenchmarkQuickOpenBrowse types the first letter and takes it back: All
// goes from its browsing lists to a query's and back.
func BenchmarkQuickOpenBrowse(b *testing.B) {
	m := quickModel(b, qoAll)

	b.ReportAllocs()

	for b.Loop() {
		m.Update(keyMsg("w"))
		_ = m.View()
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		_ = m.View()
	}
}

// BenchmarkQuickOpenText lists a full batch of lines found, as the search's
// last batch arrives, and draws it.
func BenchmarkQuickOpenText(b *testing.B) {
	m := quickModel(b, qoText)
	press(m, "n", "e")

	q := quickOf(b, m)

	var found []searchFile

	for f := range 40 {
		sf := searchFile{path: fmt.Sprintf("packages/Pkg%03d/src/needle%02d.ts", f, f), count: 5}
		for l := range 5 {
			text := fmt.Sprintf("\t\tconst needle%d = useNeedle(props.needle, %d) // the needle in line %d", l, f, l)
			i := strings.Index(text, "needle")
			sf.lines = append(sf.lines, searchLine{line: 10*l + 3, text: text, ranges: [][2]int{{i, i + 6}}})
		}

		found = append(found, sf)
	}

	b.ReportAllocs()

	for b.Loop() {
		q.onText(m, searchMsg{gen: q.gen, files: found})
		_ = m.View()
	}
}

// BenchmarkPaletteType types into quick open's Actions, ctrl+shift+p.
func BenchmarkPaletteType(b *testing.B) {
	m := quickModel(b, qoActions)

	b.ReportAllocs()

	for b.Loop() {
		m.Update(keyMsg("g"))
		_ = m.View()
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		_ = m.View()
	}
}

// BenchmarkPaletteTypeOn types a command's name a letter at a time, each
// drawn, then clears it: every key a list no row of it was drawn for.
func BenchmarkPaletteTypeOn(b *testing.B) {
	m := quickModel(b, qoActions)

	b.ReportAllocs()

	for b.Loop() {
		for _, k := range []string{"s", "p", "l", "i", "t", "ctrl+u"} {
			m.Update(keyMsg(k))
			_ = m.View()
		}
	}
}

// BenchmarkModalStreaming is a busy session under quick open: each screen it
// sends draws the frame anew, dimmed.
func BenchmarkModalStreaming(b *testing.B) {
	m := quickModel(b, qoAll)
	scr := m.term.scr
	i := 0

	b.ReportAllocs()

	for b.Loop() {
		i++

		scr.Lines = append(scr.Lines[1:], scr.Lines[0]) // the screen scrolls a line

		m.Update(eventMsg{Kind: "screen", ID: "s1"})
		m.Update(screenMsg{"s1", scr, nil, 0})
		_ = m.View()
	}
}

// BenchmarkDialogOpen opens a question over the screen and draws it.
func BenchmarkDialogOpen(b *testing.B) {
	m := benchModel(b)

	b.ReportAllocs()

	for b.Loop() {
		m.modal = nil
		_ = m.View()

		_ = m.confirmQuit()
		_ = m.View()
	}
}

// quickOf types a letter into m's quick open and takes the state behind it
// from the search it starts.
func quickOf(tb testing.TB, m *Model) *quickOpenState {
	tb.Helper()

	_, cmd := m.Update(keyMsg("d"))
	for _, msg := range runAll(cmd) {
		if tick, ok := msg.(quickTickMsg); ok {
			tick.q.stop()
			tick.q.gen++

			return tick.q
		}
	}

	tb.Fatal("no search started")

	return nil
}

// TestModalBoxKept holds the box drawn once (modal.box) to one drawn anew,
// and the dimmed frame kept under it to a fresh one, after each thing that
// changes what quick open shows.
func TestModalBoxKept(t *testing.T) {
	m := benchModel(t)
	m.quickOpen(indexMsg{ws: m.ws, files: benchFiles(300)}, qoAll)

	press(m, "n", "e")
	q := quickOf(t, m) // types a "d"
	_ = m.View()

	found := func(text string) searchMsg {
		return searchMsg{gen: q.gen, files: []searchFile{{path: "a.go", count: 1, lines: []searchLine{{line: 3, text: text, ranges: [][2]int{{0, 2}}}}}}}
	}

	steps := []struct {
		name string
		do   func()
	}{
		{"type", func() { press(m, "backspace", "backspace", "backspace", "w", "i") }},
		{"down", func() {
			sel := m.modal.l.sel

			press(m, "down")

			if m.modal.l.sel == sel {
				t.Fatal("down moved nothing")
			}
		}},
		{"a batch of lines", func() { q.onText(m, found("wi one")) }},
		{"another batch", func() { q.onText(m, found("wi two")) }},
		{"page", func() { press(m, "pgdown") }},
		{"tab", func() { press(m, "tab") }},
		{"view", func() { press(m, "ctrl+t") }},
		{"wheel", func() {
			x, y, _, _, _ := m.modal.rect(m)
			m.Update(tea.MouseWheelMsg{X: x + 3, Y: y + 4, Button: tea.MouseWheelDown})
		}},
		{"motion", func() {
			x, y, _, _, _ := m.modal.rect(m)
			m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 5})
		}},
		{"resize", func() { m.Update(tea.WindowSizeMsg{Width: 180, Height: 40}) }},
		{"screen", func() { m.Update(screenMsg{"s1", m.term.scr, nil, 0}) }},
		{"paste", func() { m.Update(tea.PasteMsg{Content: "Widget"}) }},
	}

	for _, s := range steps {
		s.do()

		kept := m.View().Content

		m.modal.drawn, m.modal.rowsDrawn, m.base = drawnBox{}, memo[boxRowKey, string]{}, nil
		if fresh := m.View().Content; fresh != kept {
			t.Fatalf("after %s the kept box or frame differs from a fresh one:\n%s\n%s", s.name, ansi.Strip(kept), ansi.Strip(fresh))
		}
	}
}

// TestModalSafeWidths draws quick open over a session full of emoji on a
// terminal that measures text by wcwidth: the frame under it and the box are
// made safe apart, and every row still measures the screen's width both ways.
func TestModalSafeWidths(t *testing.T) {
	m := benchModel(t)
	m.graphemes = false

	for y := range m.term.scr.Lines {
		m.term.scr.Lines[y] = strings.Repeat("a⚠️b 👍🏽 1️⃣ 🇸🇰 日本 ", 12)
	}

	m.quickOpen(indexMsg{ws: m.ws, files: []string{"⚠️ warn.md", "👍🏽/日本.go", "plain.go"}}, qoAll)

	for _, k := range []string{"", "⚠", "down"} {
		if k != "" {
			press(m, k)
		}

		for i, l := range strings.Split(m.View().Content, "\n") {
			if w, wc := ansi.StringWidth(l), ansi.WcWidth.StringWidth(l); w != m.w || wc != m.w {
				t.Fatalf("after %q row %d is %d cells, %d by wcwidth, want %d: %q", k, i, w, wc, m.w, ansi.Strip(l))
			}
		}
	}
}

// BenchmarkQuickOpenHeld reports what quick open over 20 000 files holds on
// the heap while open: its lists, the rows it shows, the frame under it.
func BenchmarkQuickOpenHeld(b *testing.B) {
	m := benchModel(b)
	msg := indexMsg{ws: m.ws, files: benchFiles(20000)}

	var (
		ms   runtime.MemStats
		held uint64
	)

	for b.Loop() {
		m.modal, m.base = nil, nil
		_ = m.View()

		runtime.GC()
		runtime.ReadMemStats(&ms)
		before := ms.HeapAlloc

		m.quickOpen(msg, qoAll)
		press(m, "w", "i", "d")
		_ = m.View()

		runtime.GC()
		runtime.ReadMemStats(&ms)
		held = ms.HeapAlloc - before
	}

	b.ReportMetric(float64(held), "held-B")
}
