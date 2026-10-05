package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH puts a gh on PATH that runs script with sh in the directory gh was
// started in, after logging its arguments, one call a line, to the returned
// file.
func fakeGH(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "gh")
	mustWrite(t, bin, "#!/bin/sh\necho \"$*\" >> '"+log+"'\n"+script+"\n")
	must(t, "chmod gh", os.Chmod(bin, 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return log
}

// fixture is the absolute path of testdata/gh/name: gh runs in another
// directory.
func fixture(t *testing.T, name string) string {
	t.Helper()

	p, err := filepath.Abs(filepath.Join("testdata", "gh", name))
	must(t, "fixture "+name, err)

	return p
}

// calls reads the fake gh's log: the arguments of each call.
func calls(t *testing.T, log string) []string {
	t.Helper()

	b, err := os.ReadFile(log)
	must(t, "read gh log", err)

	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestPullRequests(t *testing.T) {
	log := fakeGH(t, "cat '"+fixture(t, "prs.json")+"'")

	prs, err := PullRequests(t.TempDir(), 50, "--search", "review-requested:@me")
	must(t, "PullRequests", err)

	if got := calls(t, log); len(got) != 1 || got[0] != "pr list --limit 50 --json "+prFields+" --search review-requested:@me" {
		t.Fatalf("gh called with %q", got)
	}

	if len(prs) != 4 {
		t.Fatalf("got %d pull requests: %+v", len(prs), prs)
	}

	p := prs[1]
	if p.Number != 13 || p.Title != "fix: employees see suppliers" || p.Author.Login != "bob" || !p.IsDraft ||
		!p.IsCrossRepository || p.HeadRefName != "main" || p.BaseRefName != "main" || p.HeadRefOid != strings.Repeat("1", 40) ||
		p.URL != "https://github.com/o/r/pull/13" {
		t.Errorf("decoded %+v", p)
	}

	for i, want := range []string{"pass", "pending", "fail", ""} {
		if got := prs[i].Checks(); got != want {
			t.Errorf("#%d checks = %q, want %q", prs[i].Number, got, want)
		}
	}
}

func TestChecks(t *testing.T) {
	run := func(status, conclusion string) Check {
		return Check{Typename: "CheckRun", Status: status, Conclusion: conclusion}
	}
	ctx := func(state string) Check { return Check{Typename: "StatusContext", State: state} }

	for _, c := range []struct {
		name   string
		checks []Check
		want   string
	}{
		{"none", nil, ""},
		{"passed", []Check{run("COMPLETED", "SUCCESS"), run("COMPLETED", "SKIPPED"), run("COMPLETED", "NEUTRAL")}, "pass"},
		{"queued", []Check{run("COMPLETED", "SUCCESS"), run("QUEUED", "")}, "pending"},
		{"failed while another runs", []Check{run("IN_PROGRESS", ""), run("COMPLETED", "TIMED_OUT")}, "fail"},
		{"cancelled", []Check{run("COMPLETED", "CANCELLED")}, "fail"},
		// A status has no Status field: it must not read as a run still going.
		{"status passed", []Check{ctx("SUCCESS")}, "pass"},
		{"status pending", []Check{ctx("PENDING"), run("COMPLETED", "SUCCESS")}, "pending"},
		{"status expected", []Check{ctx("EXPECTED")}, "pending"},
		{"status error", []Check{run("COMPLETED", "SUCCESS"), ctx("ERROR")}, "fail"},
	} {
		if got := (Item{Rollup: c.checks}).Checks(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIssues(t *testing.T) {
	log := fakeGH(t, "cat '"+fixture(t, "prs.json")+"'") // an issue decodes from the same fields

	items, err := Issues(t.TempDir(), "--assignee", "@me")
	must(t, "Issues", err)

	if got := calls(t, log); len(got) != 1 || got[0] != "issue list --json "+issueFields+" --assignee @me" {
		t.Fatalf("gh called with %q", got)
	}

	if len(items) != 4 || items[0].Number != 14 || items[0].Author.Login != "ann" {
		t.Errorf("decoded %+v", items)
	}
}

func TestNotifications(t *testing.T) {
	log := fakeGH(t, "cat '"+fixture(t, "notifications.json")+"'")

	notes, err := Notifications(t.TempDir())
	must(t, "Notifications", err)

	if got := calls(t, log); len(got) != 1 || got[0] != "api repos/{owner}/{repo}/notifications" {
		t.Fatalf("gh called with %q", got)
	}

	want := []struct {
		id, typ, url string
		number       int
	}{
		{"101", "PullRequest", "https://github.com/o/r/pull/258", 258},
		{"102", "Issue", "https://github.com/o/r/issues/7", 7},
		{"103", "Release", "https://github.com/o/r", 0}, // …/releases/99 is not a number to show
		{"104", "Discussion", "https://github.com/o/r", 0},
	}
	if len(notes) != len(want) {
		t.Fatalf("got %d notes: %+v", len(notes), notes)
	}

	for i, w := range want {
		n := notes[i]
		if n.ID != w.id || n.Type != w.typ || n.Number != w.number || n.URL() != w.url || !n.Unread {
			t.Errorf("note %d = %+v (url %s), want %+v", i, n, n.URL(), w)
		}
	}

	if notes[0].Title != "feat: streets by batch ids" || notes[0].Reason != "review_requested" || notes[0].Updated.Day() != 30 {
		t.Errorf("decoded %+v", notes[0])
	}
}

func TestGHError(t *testing.T) {
	fakeGH(t, "echo 'To get started with GitHub CLI, please run:  gh auth login' >&2\nexit 4")

	_, err := PullRequests(t.TempDir(), 50)
	if err == nil {
		t.Fatal("PullRequests succeeded without a login")
	}

	if ExitCode(err) != 4 || err.Error() != "To get started with GitHub CLI, please run:  gh auth login" {
		t.Errorf("error %q, exit %d", err, ExitCode(err))
	}
}

// TestCheckoutPR runs CheckoutPR in real worktrees against a gh that does
// what gh pr checkout does: a new branch tracks the pull request's head, an
// existing one only fast-forwards.
func TestCheckoutPR(t *testing.T) {
	origin := repo(t)
	mustWrite(t, filepath.Join(origin, "a.txt"), "a\n")
	mustGit(t, origin, "add", "-A")
	mustGit(t, origin, "commit", "-qm", "init")
	mustGit(t, origin, "switch", "-qc", "feat")
	mustWrite(t, filepath.Join(origin, "a.txt"), "feat\n")
	mustGit(t, origin, "commit", "-qam", "feat")
	mustGit(t, origin, "switch", "-q", "main")

	project := filepath.Join(t.TempDir(), "project")
	mustGit(t, origin, "clone", "-q", origin, project)
	mustGit(t, project, "config", "user.email", "t@t")
	mustGit(t, project, "config", "user.name", "t")

	log := fakeGH(t, `b=$5
git fetch -q origin "+refs/heads/feat:refs/remotes/origin/feat" || exit 1
if git rev-parse -q --verify "refs/heads/$b" >/dev/null; then
	git checkout -q "$b" && git merge -q --ff-only origin/feat
else
	git checkout -q -b "$b" --track origin/feat
fi`)

	branchExists := func(name string) bool {
		_, err := Run(project, "rev-parse", "-q", "--verify", "refs/heads/"+name)
		return err == nil
	}

	// A new branch: the worktree was made on a throwaway one, which goes.
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, project, "worktree", "add", "-q", "-b", "worktree/tmp", wt)
	must(t, "CheckoutPR", CheckoutPR(wt, 7, "feat", "worktree/tmp"))

	if up, err := Run(wt, "rev-parse", "--abbrev-ref", "@{u}"); err != nil || strings.TrimSpace(up) != "origin/feat" {
		t.Errorf("upstream = %q, %v", up, err)
	}

	if branchExists("worktree/tmp") {
		t.Error("the throwaway branch is still there")
	}

	// A branch of the user's own that has diverged keeps its commit.
	mustGit(t, project, "branch", "mine", "main")

	mine := filepath.Join(t.TempDir(), "mine")
	mustGit(t, project, "worktree", "add", "-q", mine, "mine")
	mustGit(t, mine, "commit", "-q", "--allow-empty", "-m", "local work")

	head, _ := Run(mine, "rev-parse", "HEAD")
	if err := CheckoutPR(mine, 7, "mine", ""); err == nil {
		t.Error("a diverged branch was checked out over")
	}

	if now, _ := Run(mine, "rev-parse", "HEAD"); now != head {
		t.Errorf("HEAD moved from %s to %s", head, now)
	}

	// A failed checkout leaves the throwaway branch: the worktree is on it.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	mustGit(t, project, "worktree", "add", "-q", "-b", "worktree/tmp2", wt2)

	if err := CheckoutPR(wt2, 7, "mine", "worktree/tmp2"); err == nil { // mine is checked out elsewhere
		t.Error("checked out a branch another worktree has")
	}

	if !branchExists("worktree/tmp2") {
		t.Error("the throwaway branch went with the checkout failing")
	}

	for _, c := range calls(t, log) {
		if strings.Contains(c, "--force") {
			t.Errorf("gh %s: --force resets a local branch", c)
		}
	}
}

// TestFetchPR fetches a pull request's head from the remote of its
// repository, not another, and compares it from where it left its base.
func TestFetchPR(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "a\n")
	must(t, "stage", StageAll(root))
	must(t, "commit", Commit(root, "one"))

	srv := filepath.Join(t.TempDir(), "owner", "repo")
	mustGit(t, root, "clone", "-q", "--bare", root, srv)
	mustGit(t, root, "switch", "-q", "-c", "feature")
	mustWrite(t, filepath.Join(root, "b.txt"), "b\n")
	must(t, "stage", StageAll(root))
	must(t, "commit", Commit(root, "two"))
	mustGit(t, root, "push", "-q", srv, "feature:refs/pull/7/head")

	head, _ := Run(root, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)

	mustGit(t, root, "switch", "-q", "main")
	mustGit(t, root, "branch", "-q", "-D", "feature") // only the remote has it now
	mustGit(t, root, "remote", "add", "fork", srv+"2")
	mustGit(t, root, "remote", "add", "up", srv)
	fakeGH(t, `echo '{"baseRefName":"main","headRefOid":"`+head+`","url":"https://github.com/Owner/repo/pull/7"}'`)

	spec, err := FetchPR(root, 7)
	if err != nil || spec != "up/main..."+head {
		t.Fatalf("spec %q, %v", spec, err)
	}

	if es, err := Compare(root, spec); err != nil || len(es) != 1 || es[0].Path != "b.txt" {
		t.Fatalf("pull request files: %v %+v", err, es)
	}
}
