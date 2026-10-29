package font

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release serves a zip of files as the nerd-fonts release, under sum (the
// zip's own hash when empty), and points HOME and the XDG directories at a
// scratch home it returns.
func release(t *testing.T, files map[string]string, sum string) string {
	t.Helper()

	var b bytes.Buffer

	zw := zip.NewWriter(&b)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = w.Write([]byte(content)) // a bytes.Buffer never fails
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if sum == "" {
		h := sha256.Sum256(b.Bytes())
		sum = hex.EncodeToString(h[:])
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b.Bytes()) }))
	t.Cleanup(srv.Close)

	url, sha := zipURL, zipSHA
	zipURL, zipSHA = srv.URL+"/NerdFontsSymbolsOnly.zip", sum

	t.Cleanup(func() { zipURL, zipSHA = url, sha })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	return home
}

func TestInstallPutsTheMonoFontWhereItIsFound(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Install is for Linux and macOS")
	}

	home := release(t, map[string]string{
		"SymbolsNerdFontMono-Regular.ttf": "mono", "SymbolsNerdFont-Regular.ttf": "wide",
		"LICENSE": "MIT", "README.md": "readme", "10-nerd-font-symbols.conf": "<fontconfig/>",
	}, "")

	if nerdFontIn(dirs()[0]) {
		t.Fatal("a scratch home has a Nerd Font before the install")
	}

	if err := Install(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !nerdFontIn(dirs()[0]) {
		t.Errorf("no Nerd Font in %s after the install", dirs()[0])
	}

	ents, _ := os.ReadDir(dirs()[0])

	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}

	if got := strings.Join(names, " "); got != "SymbolsNerdFont-LICENSE SymbolsNerdFontMono-Regular.ttf" {
		t.Errorf("installed %q, want the mono font and its license only", got)
	}

	conf := filepath.Join(home, "config", "fontconfig", "conf.d", "10-nerd-font-symbols.conf")
	if _, err := os.Stat(conf); (err == nil) != (runtime.GOOS == "linux") {
		t.Errorf("fontconfig rule: %v, want it on Linux only", err)
	}
}

func TestInstallRefusesAWrongChecksum(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Install is for Linux and macOS")
	}

	release(t, map[string]string{"SymbolsNerdFontMono-Regular.ttf": "tampered"}, strings.Repeat("a", 64))

	if err := Install(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}

	if nerdFontIn(dirs()[0]) {
		t.Error("a font that failed its checksum was installed")
	}
}
