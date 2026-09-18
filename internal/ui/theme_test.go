package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestPickerKeepsItsTop(t *testing.T) {
	m := testModelSized(t, 100, 40)
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"a.go"}})
	_, y1, _, h1, rows1 := m.modal.rect(m)

	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("f%02d.go", i)
	}

	m.quickOpen(indexMsg{ws: m.ws, files: files})

	_, y2, _, h2, _ := m.modal.rect(m)
	if y1 != y2 || rows1 != 8 || h2 <= h1 {
		t.Fatalf("one result: y=%d h=%d rows=%d; thirty: y=%d h=%d", y1, h1, rows1, y2, h2)
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

	if got := chipCell(icFiles.short(), railW); ansi.StringWidth(got) != railW || ansi.Strip(got) != icFiles.emoji {
		t.Fatalf("emoji fills the rail cell: %q", got)
	}

	if got := icSearch.short(); got != icSearch.emoji {
		t.Fatalf("short glyph in emoji mode: %q", got)
	}

	checkWidths(t, m)
	m.hidden = [2]bool{true, true}
	checkWidths(t, m)
	applyLook("vscode", true, "ascii", nil)

	if got := icFiles.short(); got != "F" || iconSeg("a.go", false, false).s != "" {
		t.Fatalf("ascii icons: short=%q", got)
	}
}
