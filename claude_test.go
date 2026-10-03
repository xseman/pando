package main

import (
	"bytes"
	"regexp"
	"testing"
)

func TestBackgroundID(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"started", "backgrounded · \x1b[36m8e0e5ee8\x1b[39m\x1b[2m (idle — send a prompt to start)\x1b[22m\n" +
			"\x1b[2m  claude attach 8e0e5ee8    open in this terminal\x1b[22m\n", "8e0e5ee8"},
		{"a warning first", "warning: --bg manages the session id; ignoring --session-id\nbackgrounded · 8adf21fc (idle)\n", "8adf21fc"},
		{"a copy of a running one", "note: session aaaa1111 is already running in the background, so this started a copy as bbbb2222. `claude attach aaaa1111` opens the original.\n" +
			"backgrounded · bbbb2222\n  claude attach bbbb2222    open in this terminal\n", "bbbb2222"},
		{"only the hint", "  claude attach cccc3333    open in this terminal\n", "cccc3333"},
		{"an error", "error: unknown option '--bg'\n", ""},
		{"nothing", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := backgroundID([]byte(tc.out)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// FuzzBackgroundID: whatever claude prints, an id found is hex and printed
// there, so `claude attach` is never handed something claude did not say.
func FuzzBackgroundID(f *testing.F) {
	f.Add([]byte("backgrounded · \x1b[36m8e0e5ee8\x1b[39m (idle)\n"))
	f.Add([]byte("  claude attach cccc3333    open in this terminal\n"))
	f.Add([]byte("backgrounded · \x1b[3"))

	hex := regexp.MustCompile(`^[0-9a-f]+$`)

	f.Fuzz(func(t *testing.T, out []byte) {
		id := backgroundID(out)
		if id == "" {
			return
		}

		if !hex.MatchString(id) || !bytes.Contains(ansiSeq.ReplaceAll(out, nil), []byte(id)) {
			t.Fatalf("id %q from %q", id, out)
		}
	})
}
