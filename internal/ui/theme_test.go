package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestColorOverrides(t *testing.T) {
	defer applyLook("vscode", true, "ascii", nil)

	applyLook("vscode", true, "ascii", map[string]string{"accent": "#ff0000", "ok": "2", "badge_bg": "nope", "unknown": "#000000"})

	if pal.accent != hex("#ff0000") || pal.ok != lipgloss.Color("2") || pal.badgeBg != vscodeDark.badgeBg || vscodeDark.accent == hex("#ff0000") {
		t.Fatalf("overrides: accent=%v ok=%v badge=%v", pal.accent, pal.ok, pal.badgeBg)
	}

	if n := reflect.TypeOf(palette{}).NumField() - 1; len(pal.colorKeys()) != n { // every color, not the light flag
		t.Fatalf("%d color keys for %d palette colors", len(pal.colorKeys()), n)
	}
}

// TestPickerKeepsItsTop centers a picker at half the screen at most, 8 rows
// at least, and keeps it where it opened while typing narrows the list.
func TestPickerKeepsItsTop(t *testing.T) {
	m := testModelSized(t, 100, 40)
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"a.go"}}, qoAll)
	_, y1, _, h1, rows1 := m.modal.rect(m)

	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("f%02d.go", i)
	}

	m.quickOpen(indexMsg{ws: m.ws, files: files}, qoAll)

	_, y2, _, h2, _ := m.modal.rect(m) // quick open is half the screen, whatever it lists
	if y1 != y2 || h1 != h2 || rows1 != 17 || y1 != (m.h-h1)/2 {
		t.Fatalf("one result: y=%d h=%d rows=%d; thirty: y=%d h=%d", y1, h1, rows1, y2, h2)
	}

	m.modal = newPicker("Pick", []item{{label: "one", run: func(*Model) tea.Cmd { return nil }}, {label: "two", run: func(*Model) tea.Cmd { return nil }}})
	if _, y, _, h, rows := m.modal.rect(m); rows != 8 || y != (m.h-h)/2 {
		t.Fatalf("two items: y=%d h=%d rows=%d", y, h, rows)
	}

	m.modal = newPicker("Pick", slices.Repeat([]item{{label: "one", run: func(*Model) tea.Cmd { return nil }}}, 30))
	_, y3, _, h3, rows3 := m.modal.rect(m)

	press(m, "x") // nothing left

	if _, y, _, h, _ := m.modal.rect(m); rows3 != 17 || y != y3 || h != h3 {
		t.Fatalf("thirty: y=%d h=%d rows=%d; typed: y=%d h=%d", y3, h3, rows3, y, h)
	}

	checkWidths(t, m)
}

func TestEmojiIcons(t *testing.T) {
	m := gitModel(t)

	defer applyLook("vscode", true, "ascii", nil)

	m.setSettings(map[string]any{"icons": "emoji"})

	if iconsNerd || !iconsEmoji || !strings.Contains(m.View().Content, icGit.emoji) {
		t.Fatal("emoji icons in the activity bar")
	}

	if got := iconSeg("main.rs", false, false).s; got != "🦀 " {
		t.Fatalf("rust file icon: %q", got)
	}

	if got := iconSeg("src", true, true).s; got != "📂 " {
		t.Fatalf("open directory icon: %q", got)
	}

	if got := icSearch.short(); got != icSearch.emoji {
		t.Fatalf("short glyph in emoji mode: %q", got)
	}

	checkWidths(t, m)
	m.hidden = [2]bool{true, true}

	if out := checkWidths(t, m); !strings.Contains(out, icFiles.emoji) {
		t.Fatal("a hidden side keeps its emoji chips")
	}

	applyLook("vscode", true, "ascii", nil)

	if got := icFiles.short(); got != "F" || iconSeg("a.go", false, false).s != "" {
		t.Fatalf("ascii icons: short=%q", got)
	}
}
