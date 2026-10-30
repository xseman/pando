package ui

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/lsp"
)

// TestFrameMemos drives editors through random keys, clicks, wheel turns,
// problems and settings, and checks each frame against one drawn with
// nothing kept from the last: a row (renderRow) or a fitted line (fits)
// taken from there must be what drawing it anew gives.
func TestFrameMemos(t *testing.T) {
	var src strings.Builder

	src.WriteString("package a\n\nvar long = []int{" + strings.Repeat("1, 2, ", 60) + "}\n\n")

	for i := range 30 { // words of their own: a row the cursor's word does not touch
		fmt.Fprintf(&src, "func F%d(a, b int) int {\n\tif a > b {  \n\t\t// q%d\n\n\t\treturn a\n\t}\n    return b\n}\n\n", i, i)
	}

	for _, c := range []struct {
		name string
		mk   func(t *testing.T) *Model
	}{
		{"go", func(t *testing.T) *Model {
			t.Helper()
			m, _ := editorModel(t, "a.go", src.String())

			return m
		}},
		{"conflict", func(t *testing.T) *Model {
			t.Helper()
			m, _ := editorModel(t, "c.go", "package c\n\n<<<<<<< HEAD\n\tours := 1\n=======\n\ttheirs := 2\n>>>>>>> feat\n\nfunc f() {}\n"+src.String())

			return m
		}},
		{"diff", func(t *testing.T) *Model {
			t.Helper()
			return diffModel(t, 120, 30)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.mk(t)
			rng := rand.New(rand.NewPCG(7, uint64(len(c.name))))
			keys := []string{
				"down", "down", "up", "left", "right", "pgdown", "pgup", "home", "end", "ctrl+home", "ctrl+end",
				"shift+down", "shift+right", "shift+end", "shift+up", "x", "enter", "backspace", "tab", "space",
				"ctrl+z", "alt+z", "ctrl+shift+[", "ctrl+shift+]", "alt+down", "ctrl+/", "esc", "a", "b",
			}

			for i := range 600 {
				a := m.area()

				var op string

				switch r := rng.IntN(32); {
				case r < 15:
					op = keys[rng.IntN(len(keys))]
					press(m, op)

				case r < 17:
					x, y := a.x+rng.IntN(a.w), a.y+rng.IntN(a.h)
					op = fmt.Sprintf("click %d,%d", x, y)
					click(m, x, y, tea.MouseLeft)

				case r < 18:
					x, y := a.x+rng.IntN(8), a.y+rng.IntN(a.h) // over the gutter: its fold marks show
					op = fmt.Sprintf("hover %d,%d", x, y)
					m.Update(tea.MouseMotionMsg{X: x, Y: y})

				case r < 19:
					op = "wheel"

					m.Update(tea.MouseWheelMsg{X: a.x + a.w/2, Y: a.y + a.h/2, Button: []tea.MouseButton{tea.MouseWheelDown, tea.MouseWheelUp}[rng.IntN(2)]})

				case r < 20:
					m.st.Settings.Blanks = []string{"none", "all", "boundary", "selection", "trailing"}[rng.IntN(5)]
					op = "blanks " + m.st.Settings.Blanks

				case r < 21:
					line := min(m.pv.top+rng.IntN(20), max(len(m.pv.plain)-1, 0)) // in view
					m.diags[m.pv.diagPath()] = []lsp.Diagnostic{{Line: line, Col: 1, EndLine: line, EndCol: 4, Severity: 1 + rng.IntN(2), Message: "m"}}
					op = fmt.Sprintf("problem on %d", line)

				case r < 22:
					op = "find"

					press(m, "ctrl+f")
					press(m, "a", "enter")

				case r < 23:
					op = "find off"

					press(m, "esc")

				case r < 24:
					w := 90 + 20*rng.IntN(3)
					op = fmt.Sprintf("width %d", w)
					m.Update(tea.WindowSizeMsg{Width: w, Height: 30})

				case r < 25:
					op = "palette"

					press(m, "ctrl+shift+p", "g")

				case r < 26:
					c := m.colRect(rng.IntN(len(m.cols()))) // the activity bar: its chips and the gear
					x, y := c.x+rng.IntN(max(c.w, 1)), rng.IntN(actH+1)
					op = fmt.Sprintf("bar hover %d,%d", x, y)
					m.Update(tea.MouseMotionMsg{X: x, Y: y})

				case r < 27:
					op = []string{"ctrl+shift+e", "ctrl+shift+g", "ctrl+shift+f", "ctrl+0"}[rng.IntN(4)]
					press(m, op)

				case r < 28:
					x, y := rng.IntN(m.w), rng.IntN(m.h) // anywhere: a sidebar's rows light under it
					op = fmt.Sprintf("move %d,%d", x, y)
					m.Update(tea.MouseMotionMsg{X: x, Y: y})

				case r < 30:
					k := []string{"shift+right", "shift+left"}[rng.IntN(2)]
					op = "extend " + k

					press(m, k) // a selection grows on its line over two frames
					m.View()
					press(m, k)

				default:
					op = "pan"

					m.Update(tea.MouseWheelMsg{X: a.x + a.w/2, Y: a.y + a.h/2, Button: []tea.MouseButton{tea.MouseWheelRight, tea.MouseWheelLeft}[rng.IntN(2)]})
				}

				got := m.View().Content
				dropMemos(m)

				if want := m.View().Content; got != want {
					g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
					for y := range min(len(g), len(w)) {
						if g[y] != w[y] {
							t.Fatalf("step %d (%s), row %d:\nkept %q\nnew  %q", i, op, y, ansi.Strip(g[y]), ansi.Strip(w[y]))
						}
					}

					t.Fatalf("step %d (%s): %d rows kept, %d new", i, op, len(g), len(w))
				}
			}
		})
	}
}

// TestFrameMemosExplorer is TestFrameMemos on the Explorer: rows the keys,
// the pointer, Source Control's letters, problems and a cut file change.
func TestFrameMemosExplorer(t *testing.T) {
	m := testModelSized(t, 120, 30)

	var paths []string

	for i := range 30 {
		p := filepath.Join(m.ws, fmt.Sprintf("d%d", i%4), fmt.Sprintf("s%d", i%3), fmt.Sprintf("f%d.go", i))
		mustWrite(t, p, "package f\n")
		paths = append(paths, p)
	}

	m.ex.rebuild(m)
	m.st.Settings.Diags = true
	press(m, "ctrl+0")

	rng := rand.New(rand.NewPCG(3, 9))
	keys := []string{"down", "down", "up", "pgdown", "pgup", "home", "end", "enter", "right", "left", "ctrl+x", "esc"}

	for i := range 800 {
		var op string

		switch r := rng.IntN(10); {
		case r < 6:
			op = keys[rng.IntN(len(keys))]
			press(m, op)

		case r < 7:
			x, y := rng.IntN(m.colRect(0).w), rng.IntN(m.h)
			op = fmt.Sprintf("move %d,%d", x, y)
			m.Update(tea.MouseMotionMsg{X: x, Y: y})

		case r < 8:
			p := paths[rng.IntN(len(paths))]
			m.ex.deco = map[string]byte{p: "MAUDI*"[rng.IntN(6)], filepath.Dir(p): '*'}
			op = "letters on " + p

		case r < 9:
			p := paths[rng.IntN(len(paths))]
			m.Update(diagMsg{p: slices.Repeat([]lsp.Diagnostic{{Severity: 1 + rng.IntN(2)}}, rng.IntN(3))})
			op = "problems in " + p

		default:
			op = "focus"

			press(m, []string{"ctrl+0", "tab"}[rng.IntN(2)])
		}

		got := m.View().Content
		dropMemos(m)

		if want := m.View().Content; got != want {
			t.Fatalf("step %d (%s): a kept row differs from one drawn anew", i, op)
		}
	}
}

// dropMemos forgets every row and line kept from the frames drawn before.
func dropMemos(m *Model) {
	m.pv.rowsNow, m.pv.rowsWas, m.bars = nil, nil, nil
	m.fits, m.cuts, m.ex.rows = memo[fitKey, string]{}, memo[cutKey, [2]string]{}, memo[exRow, string]{}
}

// TestFrameMemosLook: rows and bars kept from a frame in another look are
// drawn again in the new one, though only colors nothing else in their keys
// carries changed, from [colors]: the editor's selection, Explorer's
// selected row without the keyboard, the activity bar's rule.
func TestFrameMemosLook(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nfunc A() {\n\tif x {\n\t\ty()\n\t}\n}\n")

	defer applyLook("vscode", true, "ascii", nil)

	applyLook("vscode", true, "ascii", nil)
	press(m, "shift+down", "shift+down")
	m.ex.l.sel = 0
	m.View()
	applyLook("vscode", true, "ascii", map[string]string{"text_sel_bg": "#ff0000", "sel_unfocused_bg": "#0000ff", "overview_ruler_border": "#00ff00"})

	got := m.View().Content
	dropMemos(m)

	if want := m.View().Content; got != want {
		t.Fatal("a row or bar kept from the last look keeps its old colors")
	}
}
