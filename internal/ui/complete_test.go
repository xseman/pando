package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func compLabels(p *preview) []string {
	var out []string
	for _, it := range p.comp.items {
		out = append(out, it.label)
	}

	return out
}

// TestSuggestWords is the list without a language server: the file's own
// words, nearest first, filtered as you type.
func TestSuggestWords(t *testing.T) {
	m, _ := editorModel(t, "a.py", "totals = 2\n\ntotal = 1\ntax = 3\n\n")
	m.pv.setCursor(m, pos{4, 0})
	press(m, "t")

	if !m.pv.comp.on || !slices.Equal(compLabels(&m.pv), []string{"tax", "total", "totals"}) {
		t.Fatalf("after t: on=%v %v", m.pv.comp.on, compLabels(&m.pv))
	}

	press(m, "o")

	if got := compLabels(&m.pv); !slices.Equal(got, []string{"total", "totals"}) {
		t.Fatalf("after to: %v (total is nearer than totals)", got)
	}
	// The list is drawn under the cursor.
	if box, _, _, ok := m.pv.compBox(m); !ok || len(box) != 4 || !strings.Contains(ansi.Strip(box[1]), "total") {
		t.Fatalf("box = %q", box)
	}
	// ↓ ⏎ takes the second, in place of what was typed; one undo step.
	press(m, "down", "enter")

	if got := string(m.pv.buf.line(4)); got != "totals" || m.pv.comp.on {
		t.Fatalf("accepted %q, on=%v", got, m.pv.comp.on)
	}
	// Backspace refilters an open list; esc and a space close it.
	press(m, "enter", "t", "a")

	if got := compLabels(&m.pv); !slices.Equal(got, []string{"tax"}) {
		t.Fatalf("after ta: %v", got)
	}

	press(m, "backspace")

	if got := compLabels(&m.pv); !m.pv.comp.on || len(got) != 3 {
		t.Fatalf("backspace: on=%v %v", m.pv.comp.on, got)
	}

	press(m, "esc")

	if m.pv.comp.on || string(m.pv.buf.line(5)) != "t" {
		t.Fatalf("esc: on=%v line=%q", m.pv.comp.on, m.pv.buf.line(5))
	}

	press(m, "a", "space")

	if m.pv.comp.on {
		t.Fatal("a space left the list open")
	}
}

// TestSuggestProse keeps quiet in a Markdown file until ⌃space asks.
func TestSuggestProse(t *testing.T) {
	m, _ := editorModel(t, "notes.md", "aspen aspens\n\n")
	m.pv.setCursor(m, pos{1, 0})
	press(m, "a", "s")

	if m.pv.comp.on {
		t.Fatal("typing prose opened suggestions")
	}

	press(m, "ctrl+space")

	if !m.pv.comp.on || !slices.Equal(compLabels(&m.pv), []string{"aspen", "aspens"}) {
		t.Fatalf("ctrl+space: on=%v %v", m.pv.comp.on, compLabels(&m.pv))
	}

	press(m, "tab")

	if got := string(m.pv.buf.line(1)); got != "aspen" {
		t.Fatalf("tab accepted %q", got)
	}
}

// TestSuggestServer takes a server's answer over the words, drops a stale one,
// and lets the server's range decide what is replaced.
func TestSuggestServer(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nfunc f(s *S) {\n\ts.To\n}\n")
	m.pv.setCursor(m, pos{3, 8}) // after "s.To"; a tab shows as four cells

	cmd := m.pv.suggest(m, "", false)
	if cmd == nil || !m.pv.comp.on {
		t.Fatal("a Go file with gopls configured asks the server")
	}

	seq := m.pv.comp.seq
	m.pv.onComp(compMsg{seq: seq - 1, items: []compItem{{label: "Stale", filter: "Stale", text: "Stale", col: -1}}})

	if slices.Contains(compLabels(&m.pv), "Stale") {
		t.Fatal("an answer to an older request was used")
	}

	m.pv.onComp(compMsg{seq: seq, items: []compItem{
		{label: "Total", detail: "func() int", filter: "Total", text: "Total()", col: 3},
		{label: "Tax", filter: "Tax", text: "Tax", col: -1},
	}})

	if got := compLabels(&m.pv); !slices.Equal(got, []string{"Total"}) {
		t.Fatalf("server items filtered by To: %v", got)
	}

	press(m, "enter")

	if got := string(m.pv.buf.line(3)); got != "\ts.Total()" {
		t.Fatalf("accepted %q", got)
	}
}

// TestSuggestBoxFits keeps the list inside the editor body wherever the
// cursor's row is: under the line when there is room, above it near the
// bottom, never over the line itself.
func TestSuggestBoxFits(t *testing.T) {
	m, _ := editorModel(t, "a.py", strings.Repeat("alpha = 1\nalpine = 2\n", 40))
	m.pv.editRaw(m, pos{40, 0}, pos{40, len(m.pv.buf.line(40))}, "al")

	top, h := 1+m.stripH(), m.pvH() // body rows [top, top+h)
	for k := range h {
		m.pv.cur, m.pv.top = pos{40, 2}, 40-k
		m.pv.suggest(m, "", false)

		box, _, y, ok := m.pv.compBox(m)
		if !ok {
			t.Fatalf("cursor on body row %d: no box", k)
		}

		row := top + k
		if y < top || y+len(box) > top+h {
			t.Fatalf("cursor on body row %d: box rows [%d, %d) leave the body [%d, %d)", k, y, y+len(box), top, top+h)
		}

		if y <= row && row < y+len(box) {
			t.Fatalf("cursor on body row %d: box rows [%d, %d) cover it", k, y, y+len(box))
		}

		m.pv.closeComp()
	}
}
