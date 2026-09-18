package proto

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestParseKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Key
	}{
		{"esc", Key{Code: uv.KeyEscape}},
		{"ESC", Key{Code: uv.KeyEscape}},
		{"enter", Key{Code: uv.KeyEnter}},
		{"f7", Key{Code: uv.KeyF7}},
		// A printable key with no ctrl or alt carries its text: session.input
		// sends that through the emulator unencoded.
		{"c", Key{Code: 'c', Text: "c"}},
		{"+", Key{Code: '+', Text: "+"}},
		{"ctrl+c", Key{Code: 'c', Mod: int(uv.ModCtrl)}},
		{"ctrl++", Key{Code: '+', Mod: int(uv.ModCtrl)}},
		{"shift+tab", Key{Code: uv.KeyTab, Mod: int(uv.ModShift)}},
		{"ctrl+alt+delete", Key{Code: uv.KeyDelete, Mod: int(uv.ModCtrl | uv.ModAlt)}},
		{" ctrl+c ", Key{Code: 'c', Mod: int(uv.ModCtrl)}},
	} {
		got, err := ParseKey(tc.name)
		if err != nil {
			t.Errorf("ParseKey(%q): %v", tc.name, err)
			continue
		}

		if got != tc.want {
			t.Errorf("ParseKey(%q) = %+v, want %+v", tc.name, got, tc.want)
		}
	}

	for _, bad := range []string{"", "  ", "nope", "ctrl+nope", "ctrl"} {
		if k, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) = %+v, want an error", bad, k)
		}
	}
}
