package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// Bubble Tea calls View after every Update, so what these cost times the
// message rate (mouse motion, a busy session's screens) is how much of the
// event loop is left for keys. go test -run '^$' -bench . ./internal/ui

// benchModel is a framed 220x55 window, the session s1 docked beside the
// editor area on a busy screen whose rows are padded to its width, as the
// emulator pads a row it cleared.
func benchModel(tb testing.TB) *Model {
	tb.Helper()

	m := testModelSized(tb, 220, 55)
	drainInputs(m)

	m.st.Settings.Borders = true
	m.Update(tea.WindowSizeMsg{Width: 220, Height: 55})
	m.Update(focusSessionMsg("s1"))

	cols := m.mainW() - 1
	lines := make([]string, m.sessH())

	for y := range lines {
		l := fmt.Sprintf("\x1b[38;5;%dm● line %d\x1b[0m \x1b[1mThinking about the problem\x1b[0m 13:38:11.784016023 src/deep/x.go:1", y%200+16, y)
		lines[y] = l + strings.Repeat(" ", max(cols-ansi.StringWidth(l), 0))
	}

	m.term.id, m.term.scr = "s1", proto.Screen{Lines: lines, Cols: cols, CursorVisible: true, Scrollback: 5000}

	return m
}

func BenchmarkViewSession(b *testing.B) {
	m := benchModel(b)

	b.ReportAllocs()

	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkStreaming is one turn of a busy session: the daemon's screen
// event, then the screen it asked for, each drawn as Bubble Tea draws after
// every message.
func BenchmarkStreaming(b *testing.B) {
	m := benchModel(b)
	scr := m.term.scr

	b.ReportAllocs()

	for b.Loop() {
		m.Update(eventMsg{Kind: "screen", ID: "s1"})
		_ = m.View()

		m.term.fetching = true
		m.Update(screenMsg{"s1", scr, nil})
		_ = m.View()
	}
}

// largeFile is an editable Go file of 4900 lines, near the 5000 past which
// a file opens read-only, with the cursor at line 4000.
func largeFile(tb testing.TB) *Model {
	tb.Helper()

	var b strings.Builder

	b.WriteString("package big\n\n")

	for i := range 2449 {
		fmt.Fprintf(&b, "func F%d(a, b int) int { return a*%d + b // comment %d\n}\n", i, i, i)
	}

	m, _ := editorModel(tb, "big.go", b.String())
	drainInputs(m)
	m.pv.cur = pos{4000, 5}

	return m
}

// BenchmarkTypeLargeFile types a letter into largeFile and deletes it, each
// drawn: the line rendered again, the suggestions from the file's words.
func BenchmarkTypeLargeFile(b *testing.B) {
	m := largeFile(b)

	b.ReportAllocs()

	for b.Loop() {
		m.Update(keyMsg("x"))
		_ = m.View()
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		_ = m.View()
	}
}

// largeMarkdown is an editable changelog of 4900 lines open beside its
// rendering, the cursor at line 4000.
func largeMarkdown(tb testing.TB) *Model {
	tb.Helper()

	var b strings.Builder

	for i := range 700 {
		fmt.Fprintf(&b, "## v1.%d.0\n\n- Fixed *the* `thing` %d, see [#%d](https://x.y/%d)\n- Added **more** of it\n- Changed a b c\n\n", i, i, i, i)
	}

	m, _ := editorModel(tb, "CHANGELOG.md", b.String())
	drainInputs(m)
	m.Update(tea.WindowSizeMsg{Width: 220, Height: 55})
	m.pv.setMarkdown(m, 2)
	m.pv.cur = pos{4000, 5}

	return m
}

// BenchmarkTypeMarkdown types a letter into largeMarkdown and deletes it,
// each drawn: the line highlighted again, the file parsed again and the one
// block it changed laid out again.
func BenchmarkTypeMarkdown(b *testing.B) {
	m := largeMarkdown(b)

	b.ReportAllocs()

	for b.Loop() {
		m.Update(keyMsg("x"))
		_ = m.View()
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		_ = m.View()
	}
}

// BenchmarkViewLongLine draws a minified bundle, ten lines of 50 000 cells,
// scrolled into them: a row costs what it shows, not its line.
func BenchmarkViewLongLine(b *testing.B) {
	m, _ := editorModel(b, "min.js", strings.Repeat(strings.Repeat("var foo = bar(1); ", 2800)+"\n", 10))
	m.pv.left = 20000

	b.ReportAllocs()

	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkViewFile draws this package's largest source file, highlighted,
// beside the session.
func BenchmarkViewFile(b *testing.B) {
	m := benchModel(b)

	openApp(b, m)

	b.ReportAllocs()

	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkHoverSession looks for the link under the pointer, as each mouse
// motion over a session does.
func BenchmarkHoverSession(b *testing.B) {
	m := benchModel(b)

	b.ReportAllocs()

	for b.Loop() {
		_ = m.term.linkAt(m, "s1", 70, 25)
	}
}

// openApp opens this package's app.go in m, highlighted.
func openApp(tb testing.TB, m *Model) {
	tb.Helper()

	src, err := os.ReadFile("app.go")
	if err != nil {
		tb.Fatalf("read app.go: %v", err)
	}

	path := filepath.Join(m.ws, "app.go")
	mustWrite(tb, path, string(src))
	fire(m, m.openFile(path, false))
}

// TestAllocBudgets holds View and hover to their allocations: unlike time,
// the count is the same on every machine for a Go version, so CI catches a
// regression (make test runs it without -race). A budget sits ~15% over the
// count; a change that beats it lowers it, one that has to raise it says why.
func TestAllocBudgets(t *testing.T) {
	if raceOn {
		t.Skip("-race drops sync.Pool items: the counts vary")
	}

	session, file, large, md := benchModel(t), benchModel(t), largeFile(t), largeMarkdown(t)
	openApp(t, file)

	for _, c := range []struct {
		name   string
		budget float64
		f      func()
	}{
		{"View, session", 370, func() { _ = session.View() }},
		{"View, file", 715, func() { _ = file.View() }},
		{"hover", 40, func() { _ = session.term.linkAt(session, "s1", 70, 25) }},
		{"typing in a large file", 3350, func() {
			large.Update(keyMsg("x"))
			_ = large.View()
			large.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			_ = large.View()
		}},
		{"typing in a large Markdown file", 76600, func() {
			md.Update(keyMsg("x"))
			_ = md.View()
			md.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			_ = md.View()
		}},
		{"streaming turn", 420, func() {
			session.Update(eventMsg{Kind: "screen", ID: "s1"})
			_ = session.View()
			session.Update(screenMsg{"s1", session.term.scr, nil})
			_ = session.View()
		}},
	} {
		if n := testing.AllocsPerRun(10, c.f); n > c.budget {
			t.Errorf("%s: %.0f allocations, budget %.0f", c.name, n, c.budget)
		}
	}
}
