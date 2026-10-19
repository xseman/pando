package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Settings is a tab: sections as chips, groups under their headings,
// checkboxes and dropdowns, each change applied at once; opened again it
// is the same tab.
func TestSettingsTab(t *testing.T) {
	m := testModelSized(t, 120, 30)
	press(m, "ctrl+,")

	out := checkWidths(t, m)
	for _, want := range []string{"Appearance  Explorer  Source Control  Editor  Sessions", "Theme ─", "Color theme", "❱ vscode", "▼", "Tooltips", "[x]"} {
		if m.pv.kind != pvSettings || !strings.Contains(out, want) {
			t.Fatalf("no %q in the Settings tab:\n%s", want, out)
		}
	}

	if strings.Count(out, "Settings") != 1 { // the tab names them; no header row repeats it
		t.Fatalf("Settings named more than once:\n%s", out)
	}

	// The keyboard: down to Tooltips, space flips it; tab to Explorer, ⏎ flips Hidden files.
	press(m, "down", "down", "down", "down", "space")

	if m.st.Settings.Tips {
		t.Fatal("space on Tooltips leaves it on")
	}

	press(m, "tab", "enter")

	if !m.st.Settings.Hidden || !strings.Contains(checkWidths(t, m), "Hidden files") {
		t.Fatal("⏎ on Hidden files leaves it off")
	}

	press(m, "down", "down", "right") // Sort files by: name → type

	if m.st.Settings.ExSort != "type" {
		t.Fatalf("→ on a dropdown: %q", m.st.Settings.ExSort)
	}

	// The mouse: the Editor chip, then Vim mode's checkbox.
	at := func(text string) (int, int) {
		t.Helper()

		for y, l := range strings.Split(checkWidths(t, m), "\n") {
			if i := strings.Index(l, text); i >= 0 {
				return ansi.StringWidth(l[:i]), y
			}
		}

		t.Fatalf("%q is not on screen", text)

		return 0, 0
	}
	x, y := at("Editor  Sessions")
	click(m, x+1, y, tea.MouseLeft)

	x, y = at("Vim mode")
	click(m, x-settingsLabelX+settingsCtrlX+1, y, tea.MouseLeft)

	if !m.st.Settings.Vim {
		t.Fatal("a click on Vim mode's checkbox leaves it off")
	}

	// A dropdown opens its values under it, the current one ticked.
	press(m, "shift+tab", "shift+tab", "shift+tab", "enter") // Appearance, Color theme

	if m.modal == nil || !slices.Contains(labels(m), "vscode-light") || m.modal.disp[0].hint != "✓" {
		t.Fatalf("the theme's dropdown: %+v", m.modal)
	}

	choose(t, m, "vscode-light")

	if m.st.Settings.Theme != "vscode-light" || !strings.Contains(checkWidths(t, m), "❱ vscode-light") {
		t.Fatalf("theme %q", m.st.Settings.Theme)
	}

	// Opened again, right under its row, on the value it has.
	press(m, "enter")

	row := slices.IndexFunc(strings.Split(checkWidths(t, m), "\n"), func(l string) bool { return strings.Contains(l, "Color theme") })
	if m.modal == nil || m.modal.y != row+1 || m.modal.disp[m.modal.l.sel].label != "vscode-light" {
		t.Fatalf("reopened at y %d under row %d, on %+v", m.modal.y, row, m.modal.disp[m.modal.l.sel])
	}

	m.modal = nil

	// Opened again it is the same tab; the palette still lists every setting.
	n := len(m.editors)
	press(m, "ctrl+,")

	if len(m.editors) != n || m.pv.kind != pvSettings {
		t.Fatalf("ctrl+, again: %d tabs, was %d", len(m.editors), n)
	}

	runCommand(t, m, "preferences.wordWrap")

	if !m.st.Settings.Wrap {
		t.Fatal("Preferences: Word wrap from the palette")
	}

	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	checkWidths(t, m)

	press(m, "q")

	if m.pv.kind == pvSettings {
		t.Fatal("q closes the Settings tab")
	}
}

// The row and the section chip under the mouse light, the chip marked on the
// rule under it in the hover tint.
func TestSettingsHover(t *testing.T) {
	m := testModelSized(t, 120, 30)
	press(m, "ctrl+,")

	top := m.headH() + m.stripH()
	_, xs := m.pv.set.chips(-1)
	m.Update(tea.MouseMotionMsg{X: m.mainX() + xs[1] + 1, Y: top})

	body := m.pv.set.body(m, m.mainW(), m.pvH())
	if !strings.Contains(body[1], fgParams(pal.sashHover)) {
		t.Fatalf("the chip under the mouse is not marked: %q", body[1])
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 10, Y: top + settingsTopH + 2}) // Icons, under Color theme

	body = m.pv.set.body(m, m.mainW(), m.pvH())
	if !strings.Contains(body[settingsTopH+2], bgParams(pal.hoverBg)) || strings.Contains(body[settingsTopH+3], bgParams(pal.hoverBg)) {
		t.Fatalf("the row under the mouse, alone:\n%s", strings.Join(body, "\n"))
	}
}
