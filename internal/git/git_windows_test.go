package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowsPaths: paths git prints with forward slashes come back in the
// OS's form, the same directories.
func TestWindowsPaths(t *testing.T) {
	root := repo(t)
	mustGit(t, root, "commit", "-q", "--allow-empty", "-m", "init")

	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, root, "worktree", "add", "-q", "-b", "b", wt)

	same := func(what, got, want string) {
		t.Helper()

		if strings.Contains(got, "/") {
			t.Errorf("%s %q has a forward slash", what, got)
		}

		a, err1 := os.Stat(got)
		b, err2 := os.Stat(want)

		if err1 != nil || err2 != nil || !os.SameFile(a, b) {
			t.Errorf("%s %q is not %q (%v, %v)", what, got, want, err1, err2)
		}

		if got != want {
			t.Logf("%s %q, as a string not %q", what, got, want)
		}
	}

	got, err := Root(wt)
	must(t, "Root", err)
	same("Root", got, wt)

	got, err = MainRoot(wt)
	must(t, "MainRoot", err)
	same("MainRoot", got, root)

	wts, err := Worktrees(root)
	must(t, "Worktrees", err)

	if len(wts) != 2 {
		t.Fatalf("Worktrees: %+v", wts)
	}

	same("Worktrees[0]", wts[0].Path, root)
	same("Worktrees[1]", wts[1].Path, wt)
}
