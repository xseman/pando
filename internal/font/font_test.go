package font

import (
	"os"
	"path/filepath"
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
