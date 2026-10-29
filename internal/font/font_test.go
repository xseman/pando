package font

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	_ = os.MkdirAll(filepath.Dir(path), 0o755) // a failing WriteFile names the path
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNerdFontIn(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Hack", "Hack-Regular.ttf"), "")

	if nerdFontIn(dir, filepath.Join(dir, "missing")) {
		t.Fatal("Hack-Regular.ttf counted as a Nerd Font")
	}

	for _, name := range []string{"JetBrainsMonoNerdFontMono-Regular.ttf", "Hack Regular Nerd Font Complete.ttf"} {
		other := t.TempDir()
		mustWrite(t, filepath.Join(other, "nerd", name), "")

		if !nerdFontIn(dir, other) {
			t.Errorf("%s not found", name)
		}
	}
}

func TestOfferSkipsSSHAndTerminalsWithTheIcons(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Install is for Linux and macOS")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	for _, v := range []string{"SSH_CONNECTION", "SSH_TTY", "KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR"} {
		t.Setenv(v, "")
	}

	if Installed() {
		t.Skip("this machine has a Nerd Font installed system-wide")
	}

	if !Offer() {
		t.Fatal("no offer in a local terminal without a Nerd Font")
	}

	t.Setenv("SSH_TTY", "/dev/pts/1")

	if Offer() {
		t.Error("an offer over SSH")
	}

	t.Setenv("SSH_TTY", "")
	t.Setenv("KITTY_WINDOW_ID", "1")

	if Offer() {
		t.Error("an offer in kitty")
	}

	t.Setenv("KITTY_WINDOW_ID", "")
	mustWrite(t, filepath.Join(home, "data", "fonts", "SymbolsNerdFontMono-Regular.ttf"), "")

	if Offer() {
		t.Error("an offer with a Nerd Font installed")
	}
}
