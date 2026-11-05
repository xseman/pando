package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/git"
)

// TestReloadSkipsUnchanged leaves an open file be while neither it nor git
// changed: the tick would read it and run git twice for the same text.
func TestReloadSkipsUnchanged(t *testing.T) {
	m := testModel(t)
	path := filepath.Join(m.ws, "a.go")
	mustWrite(t, path, "package a\n")
	fire(m, m.openFile(path, false))

	if cmd := m.pv.reloadIfLive(m); cmd != nil {
		t.Fatal("an unchanged file reads again")
	}

	m.gitGen++ // a refresh found the index or the status moved

	if m.pv.reloadIfLive(m) == nil {
		t.Fatal("a git change does not read the file again")
	}

	fire(m, m.pv.reloadIfLive(m))
	mustWrite(t, path, "package a // edited\n")

	if m.pv.reloadIfLive(m) == nil {
		t.Fatal("a file edited on disk does not read again")
	}
}

// TestGitBacksOff stretches the wait between refreshes while each finds what
// the last one did, and comes back to every tick on input or a change.
func TestGitBacksOff(t *testing.T) {
	m := testModel(t)
	st := map[string]git.Status{m.ws: {Branch: "main"}}
	seen := func(status map[string]git.Status) bool {
		return m.onGitSeen(gitMsg{ws: m.ws, status: status, index: map[string]string{m.ws: "1"}})
	}

	if !seen(st) {
		t.Fatal("the first refresh is no change")
	}

	gen := m.gitGen

	var waits []time.Duration

	for range 6 {
		if seen(st) {
			t.Fatal("the same status again counts as a change")
		}

		waits = append(waits, time.Until(m.gitNext).Round(time.Second))
	}

	if want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 16 * time.Second, 16 * time.Second}; !slices.Equal(waits, want) {
		t.Fatalf("waits %v, want %v", waits, want)
	}

	if m.gitGen != gen {
		t.Fatal("refreshes that found nothing new moved gitGen")
	}

	m.gitAwake()

	if m.gitIdle != 0 || !m.gitNext.IsZero() {
		t.Fatal("input leaves git backed off")
	}

	if !seen(map[string]git.Status{m.ws: {Branch: "feat"}}) || m.gitGen != gen+1 {
		t.Fatal("a new status is no change")
	}
}

// TestGitDiscoverCached finds the workspace's repositories once per
// reposEvery: a git call per repository per tick is what Discover costs.
func TestGitDiscoverCached(t *testing.T) {
	m := testModel(t)
	mustGit(t, m.ws, "init", "-q")

	msg := gitRefresh(t, m)
	if !msg.found || len(msg.repos) != 1 {
		t.Fatalf("first refresh: found %v, repos %v", msg.found, msg.repos)
	}

	m.Update(msg)

	if msg = gitRefresh(t, m); msg.found {
		t.Fatal("the next refresh looked for the repositories again")
	}

	m.Update(msg)
	m.gitReposAt = time.Now().Add(-reposEvery - time.Second)

	if msg = gitRefresh(t, m); !msg.found {
		t.Fatal("past reposEvery the repositories are not looked for again")
	}
}

// gitRefresh runs the refresh the tick would and returns what it found.
func gitRefresh(t *testing.T, m *Model) gitMsg {
	t.Helper()

	msg, ok := m.refreshGit()().(gitMsg)
	if !ok {
		t.Fatal("a refresh brought no gitMsg")
	}

	return msg
}

// TestFetchSizeZero reads a file whose size says 0 whole: /proc and /sys
// files do, and their text comes only as it is read.
func TestFetchSizeZero(t *testing.T) {
	if _, err := os.Stat("/proc/self/status"); err != nil {
		t.Skip("no /proc here")
	}

	p := preview{kind: pvFile, path: "/proc/self/status"}

	s, err := p.fetch(false)
	if err != nil || !strings.Contains(s, "Name:") {
		t.Fatalf("read %q, %v", s, err)
	}
}
