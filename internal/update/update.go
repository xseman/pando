// Package update checks GitHub for a newer pando and replaces this binary
// with it. Releases publish one bare binary per platform plus a
// CHECKSUMS.txt; nothing is installed that the checksum file does not cover.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are published to.
const Repo = "xseman/pando"

// Dev is the version a build without -ldflags reports. It is never offered an
// update, so a working copy is not nagged about the release it is ahead of.
const Dev = "dev"

// Version is the release this binary was built from, set at link time with
// `-X github.com/xseman/pando/internal/update.Version=1.2.3`.
var Version = Dev

// API is the latest-release endpoint. The tests point it at a local server.
var API = "https://api.github.com/repos/" + Repo + "/releases/latest"

// client gives every request a deadline: a hung mirror must not wedge the
// daemon's check goroutine.
var client = &http.Client{Timeout: 30 * time.Second}

// Asset is the name this platform's binary has in a release.
func Asset() string { return "pando-" + runtime.GOOS + "-" + runtime.GOARCH }

// Release is the newest published pando: its version, where this platform's
// binary is, and what that binary must hash to.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes,omitempty"`
}

// Newer reports whether r is a release above this build. A Dev build is
// behind every release, so Newer is false for it: only an explicit
// `pando update` replaces a build that was not made from a tag.
func (r Release) Newer() bool { return Version != Dev && Compare(r.Version, Version) > 0 }

// Compare orders two dotted versions as -1, 0 or +1. A pre-release suffix
// ("1.2.3-rc1") is cut off, and a field that is not a number counts as 0, so
// Dev sorts below every release.
func Compare(a, b string) int {
	as := strings.Split(cut(a), ".")

	bs := strings.Split(cut(b), ".")
	for i := range max(len(as), len(bs)) {
		switch x, y := field(as, i), field(bs, i); {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}

	return 0
}

func cut(v string) string {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")

	return v
}

func field(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}

	n, _ := strconv.Atoi(parts[i]) // a non-numeric field counts as 0

	return n
}

// Check asks GitHub for the latest release and the checksum its
// CHECKSUMS.txt records for this platform's binary.
func Check(ctx context.Context) (Release, error) {
	var body struct {
		Tag    string `json:"tag_name"`
		Notes  string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := getJSON(ctx, API, &body); err != nil {
		return Release{}, err
	}

	rel := Release{Version: cut(body.Tag), Notes: body.Notes}
	sums := ""

	for _, a := range body.Assets {
		switch a.Name {
		case Asset():
			rel.URL = a.URL
		case "CHECKSUMS.txt":
			sums = a.URL
		}
	}

	if rel.Version == "" {
		return Release{}, errors.New("latest release has no tag")
	}

	if rel.URL == "" {
		return Release{}, fmt.Errorf("release %s has no %s", body.Tag, Asset())
	}

	if sums == "" {
		return Release{}, fmt.Errorf("release %s has no CHECKSUMS.txt", body.Tag)
	}

	b, err := get(ctx, sums)
	if err != nil {
		return Release{}, err
	}

	if rel.SHA256 = checksum(string(b), Asset()); rel.SHA256 == "" {
		return Release{}, fmt.Errorf("CHECKSUMS.txt of %s does not cover %s", body.Tag, Asset())
	}

	return rel, nil
}

// checksum reads sha256sum's output: one "HASH  NAME" line per file.
func checksum(sums, name string) string {
	for line := range strings.Lines(sums) {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err == nil {
				return strings.ToLower(f[0])
			}
		}
	}

	return ""
}

// Download fetches the release binary into dir and returns the path of the
// file, which is left there only when its SHA-256 matches. progress, when it
// is not nil, is called as bytes arrive; total is 0 when the server sends no
// length.
func Download(ctx context.Context, rel Release, dir string, progress func(done, total int64)) (string, error) {
	req, err := request(ctx, rel.URL)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", rel.URL, resp.Status)
	}

	f, err := os.CreateTemp(dir, ".pando-update-*")
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	sum := sha256.New()
	if err := copyProgress(f, io.TeeReader(resp.Body, sum), resp.ContentLength, progress); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}

	if got := hex.EncodeToString(sum.Sum(nil)); got != rel.SHA256 {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("checksum of %s is %s, want %s", Asset(), got, rel.SHA256)
	}

	return f.Name(), nil
}

// copyProgress is io.Copy with a callback, reporting the first and the last
// chunk and at most one update every 100 ms in between.
func copyProgress(dst io.Writer, src io.Reader, total int64, progress func(done, total int64)) error {
	if progress == nil {
		progress = func(int64, int64) {}
	}

	buf := make([]byte, 64*1024)
	done, last := int64(0), time.Time{}

	progress(0, total)

	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}

			done += int64(n)
			if time.Since(last) > 100*time.Millisecond {
				progress(done, total)

				last = time.Now()
			}
		}

		if errors.Is(err, io.EOF) {
			progress(done, total)
			return nil
		}

		if err != nil {
			return err
		}
	}
}

// Install downloads rel and puts it where this executable is. The running
// process keeps the binary it started from — the new one runs at the next
// start, which is why the caller tells the user to restart.
func Install(ctx context.Context, rel Release, progress func(done, total int64)) error {
	exe, err := Path()
	if err != nil {
		return err
	}

	return install(ctx, rel, exe, progress)
}

func install(ctx context.Context, rel Release, dest string, progress func(done, total int64)) error {
	tmp, err := Download(ctx, rel, filepath.Dir(dest), progress)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }() // a successful rename leaves nothing to remove

	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// Rename, never write in place: the kernel refuses to open a running
	// binary for writing (ETXTBSY), and a rename is atomic for anyone
	// starting pando while this runs.
	return os.Rename(tmp, dest)
}

// Path is this executable with its symlinks resolved: the file an update
// replaces. It is a variable so a test can aim an install at a scratch file
// instead of the binary running it.
var Path = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	if p, err := filepath.EvalSymlinks(exe); err == nil {
		return p, nil
	}

	return exe, nil
}

func request(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "pando/"+Version) // the GitHub API rejects requests without one

	return req, nil
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := request(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func getJSON(ctx context.Context, url string, v any) error {
	b, err := get(ctx, url)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}

	return nil
}
