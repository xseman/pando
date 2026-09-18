package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.3", "1.10.0", -1},
		{"0.1.0", Dev, 1},
		{Dev, "0.1.0", -1},
		{"1.2.3-rc1", "1.2.3", 0}, // a pre-release suffix is cut off
	} {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNewerIgnoresDevBuilds(t *testing.T) {
	restore := Version

	t.Cleanup(func() { Version = restore })

	Version = Dev
	if (Release{Version: "9.9.9"}).Newer() {
		t.Error("a dev build must not be offered an update")
	}

	Version = "0.1.0"
	if !(Release{Version: "0.2.0"}).Newer() {
		t.Error("0.2.0 is newer than 0.1.0")
	}

	if (Release{Version: "0.1.0"}).Newer() {
		t.Error("a release is not newer than itself")
	}
}

func TestChecksum(t *testing.T) {
	sums := "aaaa  pando-other\n" +
		strings.Repeat("b", 64) + "  " + Asset() + "\n" +
		strings.Repeat("c", 64) + "  CHECKSUMS.txt\n"
	if got := checksum(sums, Asset()); got != strings.Repeat("b", 64) {
		t.Errorf("checksum = %q", got)
	}

	if got := checksum(sums, "pando-nowhere"); got != "" {
		t.Errorf("checksum of a file not listed = %q, want empty", got)
	}

	if got := checksum("zz"+strings.Repeat("b", 62)+"  "+Asset(), Asset()); got != "" {
		t.Errorf("checksum that is not hex = %q, want empty", got)
	}
}

// FuzzChecksum holds the parser to its contract: whatever it is fed, it
// returns either nothing or a lowercase hex digest that the input contains
// on a line naming the wanted file.
func FuzzChecksum(f *testing.F) {
	f.Add(strings.Repeat("b", 64)+"  pando-linux-amd64\n", "pando-linux-amd64")
	f.Add("  \t\n\n", "x")
	f.Add(strings.Repeat("B", 64)+" *x", "x")
	f.Fuzz(func(t *testing.T, sums, name string) {
		got := checksum(sums, name)
		if got == "" {
			return
		}

		if len(got) != 64 {
			t.Fatalf("checksum(%q, %q) = %q, want 64 characters", sums, name, got)
		}

		if _, err := hex.DecodeString(got); err != nil {
			t.Fatalf("checksum(%q, %q) = %q, not hex", sums, name, got)
		}

		if got != strings.ToLower(got) {
			t.Fatalf("checksum(%q, %q) = %q, want lowercase", sums, name, got)
		}

		if !strings.Contains(strings.ToLower(sums), got) {
			t.Fatalf("checksum(%q, %q) = %q, which is not in the input", sums, name, got)
		}
	})
}

// release serves a GitHub-shaped latest release: the tag, this platform's
// binary and the CHECKSUMS.txt covering it.
func release(t *testing.T, tag string, binary []byte, sums string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if sums == "" {
		sum := sha256.Sum256(binary)
		sums = hex.EncodeToString(sum[:]) + "  " + Asset() + "\n"
	}

	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"body":"notes","assets":[
			{"name":%q,"browser_download_url":"%s/bin"},
			{"name":"CHECKSUMS.txt","browser_download_url":"%s/sums"}]}`,
			tag, Asset(), srv.URL, srv.URL)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(binary) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sums)) })

	restore := API
	API = srv.URL + "/latest"

	t.Cleanup(func() { API = restore })

	return srv
}

func TestCheck(t *testing.T) {
	release(t, "v1.4.0", []byte("#!/bin/sh\necho pando\n"), "")

	rel, err := Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if rel.Version != "1.4.0" {
		t.Errorf("version %q, want 1.4.0", rel.Version)
	}

	if rel.Notes != "notes" {
		t.Errorf("notes %q", rel.Notes)
	}

	if len(rel.SHA256) != 64 {
		t.Errorf("sha256 %q", rel.SHA256)
	}
}

func TestCheckWithoutChecksums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"x"}]}`, Asset())
	}))
	t.Cleanup(srv.Close)

	restore := API
	API = srv.URL

	t.Cleanup(func() { API = restore })

	if _, err := Check(t.Context()); err == nil || !strings.Contains(err.Error(), "CHECKSUMS.txt") {
		t.Fatalf("err = %v, want one naming CHECKSUMS.txt", err)
	}
}

func TestInstallReplacesTheBinary(t *testing.T) {
	body := []byte(strings.Repeat("pando!", 5000))
	release(t, "v2.0.0", body, "")

	rel, err := Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "pando")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var last int64

	if err := install(t.Context(), rel, dest, func(done, _ int64) { last = done }); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("binary was not replaced: %d bytes", len(got))
	}

	if last != int64(len(body)) {
		t.Errorf("progress ended at %d, want %d", last, len(body))
	}

	if st, err := os.Stat(dest); err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, %v", st.Mode().Perm(), err)
	}
	// Nothing is left beside it: a partial download must not become a file
	// the user finds in ~/.local/bin.
	ents, err := os.ReadDir(filepath.Dir(dest))
	if err != nil || len(ents) != 1 {
		t.Errorf("%d files left in the install directory, want 1 (%v)", len(ents), err)
	}
}

func TestDownloadRefusesAWrongChecksum(t *testing.T) {
	release(t, "v2.0.0", []byte("tampered"), strings.Repeat("a", 64)+"  "+Asset()+"\n")

	rel, err := Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if _, err := Download(t.Context(), rel, dir, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}

	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("%d files left behind, want none", len(ents))
	}
}
