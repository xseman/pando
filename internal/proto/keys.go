package proto

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// keyNames are the keys a caller names instead of typing: the ones an agent's
// TUI binds that have no printable character. uv keeps the same table for
// rendering a key, unexported, so this is its other direction.
var keyNames = map[string]rune{
	"enter":     uv.KeyEnter,
	"return":    uv.KeyEnter,
	"esc":       uv.KeyEscape,
	"escape":    uv.KeyEscape,
	"tab":       uv.KeyTab,
	"space":     uv.KeySpace,
	"backspace": uv.KeyBackspace,
	"delete":    uv.KeyDelete,
	"insert":    uv.KeyInsert,
	"up":        uv.KeyUp,
	"down":      uv.KeyDown,
	"left":      uv.KeyLeft,
	"right":     uv.KeyRight,
	"home":      uv.KeyHome,
	"end":       uv.KeyEnd,
	"pgup":      uv.KeyPgUp,
	"pgdown":    uv.KeyPgDown,
}

var keyMods = map[string]uv.KeyMod{
	"ctrl":  uv.ModCtrl,
	"alt":   uv.ModAlt,
	"shift": uv.ModShift,
	"meta":  uv.ModMeta,
	"super": uv.ModSuper,
}

func init() {
	for i, f := range []rune{
		uv.KeyF1, uv.KeyF2, uv.KeyF3, uv.KeyF4, uv.KeyF5, uv.KeyF6,
		uv.KeyF7, uv.KeyF8, uv.KeyF9, uv.KeyF10, uv.KeyF11, uv.KeyF12,
	} {
		keyNames[fmt.Sprintf("f%d", i+1)] = f
	}
}

// KeyNames are the names ParseKey accepts as the key itself, sorted. The
// modifier prefixes it accepts are ctrl, alt, shift, meta and super.
func KeyNames() []string { return slices.Sorted(maps.Keys(keyNames)) }

// ParseKey turns a name into the Key session.input delivers: a single
// character is itself, a name from KeyNames is that key, and any number of
// modifier prefixes joined with "+" apply to either ("ctrl+c", "shift+tab",
// "ctrl+alt+delete"). A literal "+" is a key like any other.
func ParseKey(name string) (Key, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Key{}, errors.New("empty key name")
	}

	var mod uv.KeyMod

	for {
		// i <= 0 leaves a literal "+"; the last byte leaves the "+" of "ctrl++".
		i := strings.IndexByte(name, '+')
		if i <= 0 || i == len(name)-1 {
			break
		}

		m, ok := keyMods[strings.ToLower(name[:i])]
		if !ok {
			break
		}

		mod, name = mod|m, name[i+1:]
	}

	if code, ok := keyNames[strings.ToLower(name)]; ok {
		return Key{Code: code, Mod: int(mod)}, nil
	}

	if r, n := utf8.DecodeRuneInString(name); n == len(name) && r != utf8.RuneError {
		k := Key{Code: r, Mod: int(mod)}
		// A printable key with no ctrl or alt is delivered as text, which is
		// what session.input sends through the emulator unencoded.
		if mod&^uv.ModShift == 0 {
			k.Text = name
		}

		return k, nil
	}

	return Key{}, fmt.Errorf("unknown key %q (try one of: %s)", name, strings.Join(KeyNames(), " "))
}
