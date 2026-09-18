package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
	"github.com/xseman/pando/internal/update"
)

// serveRelease stands in for GitHub: one release holding this platform's
// binary and its checksums, with update.Path aimed at a scratch file so the
// install never touches the test binary.
func serveRelease(t *testing.T, tag, body string) string {
	t.Helper()

	sum := sha256.Sum256([]byte(body))
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"body":"what changed","assets":[
			{"name":%q,"browser_download_url":"%s/bin"},
			{"name":"CHECKSUMS.txt","browser_download_url":"%s/sums"}]}`,
			tag, update.Asset(), srv.URL, srv.URL)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), update.Asset())
	})

	exe := filepath.Join(t.TempDir(), "pando")
	mustWrite(t, exe, "old binary")

	api, path, version := update.API, update.Path, update.Version
	update.API = srv.URL + "/latest"
	update.Path = func() (string, error) { return exe, nil }
	update.Version = "0.1.0"

	t.Cleanup(func() { update.API, update.Path, update.Version = api, path, version })

	return exe
}

// subscribed returns an event stream the daemon is already known to fan out
// to: Subscribe returns as soon as the request is written, so a call made
// right after it can be answered before the subscriber is registered.
func subscribed(t *testing.T) <-chan proto.Event {
	t.Helper()

	events, err := proto.Subscribe()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; ; i++ {
		if i > 100 {
			t.Fatal("no focus event: the subscription never registered")
		}

		call(t, "focus", proto.FocusParams{Workspace: "/probe"}, nil)

		select {
		case ev := <-events:
			if ev.Kind == "focus" {
				return events
			}

		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestUpdateCheckAndInstall(t *testing.T) {
	boot := start(t)
	exe := serveRelease(t, "v0.2.0", "new binary")

	d := boot()
	defer d.Close()

	var u proto.Update
	call(t, "update.status", nil, &u)

	if u.State != "" || u.Current != "0.1.0" {
		t.Fatalf("status before any check = %+v", u)
	}

	events := subscribed(t)
	call(t, "update.check", nil, &u)

	if u.State != upAvailable || u.Latest != "0.2.0" || u.Notes != "what changed" {
		t.Fatalf("check = %+v", u)
	}

	call(t, "update.install", nil, nil)
	waitFor(t, "the update to install", func() bool {
		var s proto.Update
		call(t, "update.status", nil, &s)

		return s.State == upReady || s.State == upError
	})
	call(t, "update.status", nil, &u)

	if u.State != upReady {
		t.Fatalf("install ended as %+v", u)
	}

	if got := mustRead(t, exe); got != "new binary" {
		t.Fatalf("binary on disk = %q", got)
	}

	// Every step reached the subscribers, so a TUI can draw the progress
	// without polling.
	seen := map[string]bool{}

	for {
		ev := <-events
		if ev.Kind != "update" {
			continue
		}

		var p proto.Update
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			t.Fatal(err)
		}

		seen[p.State] = true
		if p.State == upReady {
			break
		}
	}

	for _, want := range []string{upChecking, upAvailable, upDownloading, upReady} {
		if !seen[want] {
			t.Errorf("no %q event, got %v", want, seen)
		}
	}
}

// A check that finds the release this build already is says so, and installs
// nothing.
func TestUpdateCheckWhenCurrent(t *testing.T) {
	boot := start(t)
	exe := serveRelease(t, "v0.1.0", "new binary")

	d := boot()
	defer d.Close()

	var u proto.Update
	call(t, "update.check", nil, &u)

	if u.State != upCurrent || u.Latest != "0.1.0" {
		t.Fatalf("check = %+v", u)
	}

	if got := mustRead(t, exe); got != "old binary" {
		t.Fatalf("binary was replaced: %q", got)
	}
}

func TestUpdateInstallWithoutCheck(t *testing.T) {
	boot := start(t)

	d := boot()
	defer d.Close()

	if err := proto.Call("update.install", nil, nil); err == nil {
		t.Fatal("install without a check must fail")
	}
}

// A check that cannot reach GitHub is remembered, not fatal: the next one
// retries.
func TestUpdateCheckOffline(t *testing.T) {
	boot := start(t)

	d := boot()
	defer d.Close()

	api := update.API
	update.API = "http://127.0.0.1:1/latest"

	t.Cleanup(func() { update.API = api })

	if err := proto.Call("update.check", nil, nil); err == nil {
		t.Fatal("a check that cannot connect must report the error")
	}

	var u proto.Update
	call(t, "update.status", nil, &u)

	if u.State != upError || u.Error == "" {
		t.Fatalf("status = %+v", u)
	}
}

// update_check off keeps the daemon's daily check from running at all; the
// explicit methods still work.
func TestUpdateCheckSetting(t *testing.T) {
	boot := start(t)

	d := boot()
	defer d.Close()

	var st proto.State
	call(t, "state.get", nil, &st)

	if !st.Settings.Updates {
		t.Fatal("update_check should default to on")
	}

	call(t, "state.set", map[string]any{"settings": map[string]any{"update_check": false}}, nil)
	call(t, "state.get", nil, &st)

	if st.Settings.Updates {
		t.Fatal("update_check stayed on")
	}

	if cfg := mustRead(t, filepath.Join(filepath.Dir(d.statePath), "config.toml")); !strings.Contains(cfg, "update_check = false") {
		t.Fatalf("config.toml did not keep the setting:\n%s", cfg)
	}
}
