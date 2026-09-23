package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	raw := "## main...origin/main [ahead 2, behind 1]\x00" +
		"M  staged.go\x00" +
		" M changed.go\x00" +
		"MM both.go\x00" +
		"R  new.go\x00old.go\x00" +
		"?? untracked.txt\x00" +
		"UU conflict.go\x00" +
		" D gone.go\x00"

	s := parseStatus(raw)
	if s.Branch != "main" || s.Upstream != "origin/main" || s.Ahead != 2 || s.Behind != 1 {
		t.Fatalf("branch: %+v", s)
	}

	want := []Entry{{Path: "staged.go", Letter: 'M', Staged: true}, {Path: "both.go", Letter: 'M', Staged: true}, {Path: "new.go", Orig: "old.go", Letter: 'R', Staged: true}}
	if len(s.Staged) != len(want) {
		t.Fatalf("staged: %+v", s.Staged)
	}

	for i := range want {
		if s.Staged[i] != want[i] {
			t.Errorf("staged[%d] = %+v, want %+v", i, s.Staged[i], want[i])
		}
	}

	var letters strings.Builder
	for _, e := range s.Changes {
		letters.WriteByte(e.Letter)
	}

	if letters.String() != "MMUD" {
		t.Errorf("changes letters = %q (%+v)", letters.String(), s.Changes)
	}

	if len(s.Conflicts) != 1 || s.Conflicts[0] != (Entry{Path: "conflict.go", Letter: '!', XY: "UU"}) {
		t.Errorf("conflicts = %+v", s.Conflicts)
	}

	for xy, want := range map[string]string{"UU": "both modified", "DU": "deleted by us", "UD": "deleted by them", "AA": "both added", "DD": "both deleted", "AU": "added by us", "UA": "added by them"} {
		if got := ConflictText(xy); got != want {
			t.Errorf("ConflictText(%s) = %q", xy, got)
		}
	}

	if b := parseStatus("## No commits yet on dev\x00").Branch; b != "dev" {
		t.Errorf("unborn branch = %q", b)
	}
}

// repo makes an empty repository with an identity, or fails the test.
func repo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		mustGit(t, dir, args...)
	}

	return dir
}

// mustMkdir creates a directory tree, or fails the test where it went wrong.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// mustWrite writes a file and the directories above it, or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustGit runs a git command in root, or fails the test naming the command.
func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()

	if _, err := Run(root, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

// must fails the test naming op when a call it is not testing returns an error.
func must(t *testing.T, op string, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
}

func TestWorkflow(t *testing.T) {
	root := repo(t)
	if _, err := Suggest(root); err == nil {
		t.Error("Suggest on a clean tree")
	}

	mustWrite(t, filepath.Join(root, "a", "b.txt"), "hi\n")
	mustWrite(t, filepath.Join(root, ".gitignore"), "build/\n")
	mustWrite(t, filepath.Join(root, "build", "x"), "x")

	st, err := Stat(root)
	must(t, "stat before stage", err)

	if len(st.Changes) != 2 || len(st.Staged) != 0 {
		t.Fatalf("before stage: %+v", st)
	}

	deco := Decorations(root, st)
	if deco[filepath.Join(root, "a")] != '*' || deco[filepath.Join(root, "a", "b.txt")] != 'U' || deco[filepath.Join(root, "build")] != 'I' {
		t.Errorf("decorations: %v", deco)
	}

	if d, _ := Diff(root, st.Changes[0]); d == "" {
		t.Error("untracked diff empty")
	}

	// Unstage on an unborn HEAD must not fail.
	must(t, "stage all", StageAll(root))

	if err := Unstage(root, Entry{Path: "a/b.txt"}); err != nil {
		t.Fatalf("unstage on unborn HEAD: %v", err)
	}

	st, _ = Stat(root)
	if len(st.Staged) != 1 { // .gitignore stays staged
		t.Fatalf("after unstage: %+v", st)
	}

	must(t, "stage all", StageAll(root))
	must(t, "commit init", Commit(root, "init"))
	mustWrite(t, filepath.Join(root, "a", "b.txt"), "changed\n")

	st, _ = Stat(root)
	if len(st.Changes) != 1 || st.Changes[0].Letter != 'M' {
		t.Fatalf("modified: %+v", st)
	}

	must(t, "discard b.txt", Discard(root, st.Changes[0]))

	if st, _ = Stat(root); len(st.Changes) != 0 {
		t.Fatalf("after discard: %+v", st)
	}

	if lines := Lines(root, Drawers[1].Args...); len(lines) != 1 {
		t.Errorf("commits drawer: %v", lines)
	}

	if files := ListFiles(root, 100); len(files) != 2 {
		t.Errorf("ListFiles = %v", files)
	}

	// Worktrees + discovery of a nested child repo.
	wt := filepath.Join(t.TempDir(), "feat")
	must(t, "add worktree feat", AddWorktree(root, wt, "feat"))

	wts, err := Worktrees(root)
	if err != nil || len(wts) != 2 || wts[1].Branch != "feat" {
		t.Fatalf("worktrees: %+v %v", wts, err)
	}

	if err := RemoveWorktree(root, wt); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	mustGit(t, root, "init", "-q", "libs/child")
	mustMkdir(t, filepath.Join(root, "tools", ".git")) // no repository: it resolves to root, once

	if got := Discover(root); len(got) != 2 {
		t.Errorf("Discover = %v", got)
	}
}

func TestSectionOperations(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "t.txt"), "v1\n")
	must(t, "stage all", StageAll(root))
	must(t, "commit init", Commit(root, "init"))
	mustWrite(t, filepath.Join(root, "t.txt"), "v2\n")
	mustWrite(t, filepath.Join(root, "u1.txt"), "u\n")
	mustWrite(t, filepath.Join(root, "u2.txt"), "u\n")
	must(t, "stage tracked", StageTracked(root))

	if st, _ := Stat(root); len(st.Staged) != 1 || len(st.Changes) != 2 {
		t.Fatalf("stage tracked only: %+v", st)
	}

	must(t, "stage untracked files", StageFiles(root, []string{"u1.txt", "u2.txt"}))

	if st, _ := Stat(root); len(st.Staged) != 3 || len(st.Changes) != 0 {
		t.Fatalf("stage untracked files: %+v", st)
	}

	must(t, "unstage all", UnstageAll(root))
	must(t, "discard tracked", DiscardTracked(root))

	if st, _ := Stat(root); len(st.Changes) != 2 || st.Changes[0].Letter != 'U' {
		t.Fatalf("discard tracked keeps untracked files: %+v", st)
	}
}

func TestMergeConflict(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "base\n")
	mustWrite(t, filepath.Join(root, "gone.txt"), "base\n")

	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "init"}, {"switch", "-q", "-c", "feat"}} {
		mustGit(t, root, args...)
	}

	mustWrite(t, filepath.Join(root, "a.txt"), "theirs\n")

	for _, args := range [][]string{{"rm", "-q", "gone.txt"}, {"commit", "-q", "-am", "feat"}, {"switch", "-q", "main"}} {
		mustGit(t, root, args...)
	}

	mustWrite(t, filepath.Join(root, "a.txt"), "ours\n")
	mustWrite(t, filepath.Join(root, "gone.txt"), "ours\n")
	mustGit(t, root, "commit", "-q", "-am", "main")

	if st, _ := Stat(root); st.Op != "" {
		t.Fatalf("op before merge = %q", st.Op)
	}

	if _, err := Run(root, "merge", "feat"); err == nil {
		t.Fatal("merge should conflict")
	}

	st, err := Stat(root)
	must(t, "stat during merge", err)

	if st.Op != "merge" || len(st.Conflicts) != 2 || len(st.Changes) != 0 || len(st.Staged) != 0 {
		t.Fatalf("conflicted status: %+v", st)
	}

	// The message git prepared, "# Conflicts:" and the rest of its comments gone.
	if st.MergeMsg != "Merge branch 'feat'" {
		t.Fatalf("merge message = %q", st.MergeMsg)
	}

	if st.Conflicts[0] != (Entry{Path: "a.txt", Letter: '!', XY: "UU"}) || st.Conflicts[1] != (Entry{Path: "gone.txt", Letter: '!', XY: "UD"}) {
		t.Fatalf("conflicts: %+v", st.Conflicts)
	}

	if !HasConflictMarkers(root, "a.txt") || HasConflictMarkers(root, "gone.txt") || HasConflictMarkers(root, "missing.txt") {
		t.Fatal("conflict markers")
	}

	if d, err := Diff(root, st.Conflicts[0]); err != nil || !strings.Contains(d, "+<<<<<<<") || strings.Contains(d, "++<<<<<<<") {
		t.Fatalf("diff --ours: %v\n%s", err, d)
	}

	if deco := Decorations(root, st); deco[filepath.Join(root, "a.txt")] != '!' {
		t.Fatalf("decorations: %v", deco)
	}

	mustWrite(t, filepath.Join(root, "a.txt"), "resolved\n")

	if HasConflictMarkers(root, "a.txt") {
		t.Fatal("markers after resolving")
	}

	must(t, "stage resolved a.txt", Stage(root, st.Conflicts[0]))
	must(t, "remove gone.txt", Remove(root, st.Conflicts[1]))

	if st, _ = Stat(root); len(st.Conflicts) != 0 || len(st.Staged) != 2 || st.Op != "merge" {
		t.Fatalf("after resolving: %+v", st)
	}

	// An empty message box commits git's own message, as VS Code's does.
	must(t, "continue merge", Continue(root, st.Op, ""))

	if st, _ = Stat(root); st.Op != "" || st.MergeMsg != "" || len(st.Staged)+len(st.Changes) != 0 {
		t.Fatalf("after continue: %+v", st)
	}

	if out, _ := Run(root, "log", "-1", "--format=%B"); strings.TrimSpace(out) != "Merge branch 'feat'" {
		t.Fatalf("merge commit: %q", out)
	}
}

// FuzzMergeMessage checks the cleanup is git's: every line that does not
// start with "#" is kept, in order, and nothing else; no blank edges. " #" is
// text to git, not a comment.
func FuzzMergeMessage(f *testing.F) {
	for _, s := range []string{
		"Merge branch 'develop' of gitlab.nike.sk:web/nike-web into feature/DEV-43231\n\n# Conflicts:\n#\tsrc/a.ts\n",
		"fix: x\n\nbody\r\n", "", "#", "\n\n", "# only\n# comments", "a\n#b\nc",
	} {
		f.Add(s)
	}

	lines := func(s string, comments bool) []string {
		var out []string

		for l := range strings.SplitSeq(s, "\n") {
			if !comments && strings.HasPrefix(l, "#") {
				continue
			}

			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}

		return out
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := mergeMessage(s)
		if strings.TrimSpace(got) != got {
			t.Fatalf("mergeMessage(%q) = %q: blank edges", s, got)
		}

		if kept, want := lines(got, true), lines(s, false); !slices.Equal(kept, want) {
			t.Fatalf("mergeMessage(%q) = %q: lines %q, want %q", s, got, kept, want)
		}
	})
}

func TestApplyLines(t *testing.T) {
	root := repo(t)
	f := filepath.Join(root, "f.txt")
	mustWrite(t, f, "1\n2\n3\n4\n5\n6\n7\n8\n9\n")
	mustWrite(t, filepath.Join(root, "g.txt"), "a\nb")
	must(t, "stage all", StageAll(root))
	must(t, "commit init", Commit(root, "init"))
	mustWrite(t, f, "1\nTWO\n3\n4\nFIVE\n6\n7\n8\nNINE\n")

	e := Entry{Path: "f.txt", Letter: 'M'}
	one := func(n int) map[int]bool { return map[int]bool{n: true} }

	if err := ApplyLines(root, e, StageLines, one(5), one(5)); err != nil {
		t.Fatalf("stage line 5: %v", err)
	}

	if got, _ := Run(root, "show", ":f.txt"); got != "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n" {
		t.Fatalf("stage line 5: index = %q", got)
	}

	if err := ApplyLines(root, e, RevertLines, one(9), one(9)); err != nil {
		t.Fatalf("revert line 9: %v", err)
	}

	if b, _ := os.ReadFile(f); string(b) != "1\nTWO\n3\n4\nFIVE\n6\n7\n8\n9\n" {
		t.Fatalf("revert line 9: file = %q", b)
	}

	if err := ApplyLines(root, Entry{Path: "f.txt", Letter: 'M', Staged: true}, UnstageLines, one(5), one(5)); err != nil {
		t.Fatalf("unstage line 5: %v", err)
	}

	if out, _ := Run(root, "diff", "--cached"); out != "" {
		t.Fatalf("unstage line 5 leaves %q", out)
	}

	// Files without a final newline keep it that way.
	mustWrite(t, filepath.Join(root, "g.txt"), "a\nB\nc")

	if err := ApplyLines(root, Entry{Path: "g.txt", Letter: 'M'}, StageLines, one(2), map[int]bool{2: true, 3: true}); err != nil {
		t.Fatalf("no newline at EOF: %v", err)
	}

	if got, _ := Run(root, "show", ":g.txt"); got != "a\nB\nc" {
		t.Fatalf("no newline at EOF: index = %q", got)
	}

	if err := ApplyLines(root, Entry{Path: "g.txt", Letter: 'M'}, StageLines, one(1), nil); err == nil {
		t.Fatal("a file without unstaged changes must fail")
	}
}

func TestRevisions(t *testing.T) {
	root := repo(t)

	f := filepath.Join(root, "f.txt")
	for i, subject := range []string{"first", "second", "third"} {
		mustWrite(t, f, strings.Repeat("v\n", i+1))
		must(t, "stage all", StageAll(root))
		must(t, "commit "+subject, Commit(root, subject))
	}

	if revs, _ := Revisions(root, "f.txt"); len(revs) != 3 || revs[0].Subject != "third" || revs[0].Hash == "" {
		t.Fatalf("clean file: %+v", revs)
	}

	mustWrite(t, f, "v\nv\nv\nmore\n")

	revs, err := Revisions(root, "f.txt")
	if err != nil || len(revs) != 4 || revs[0].Hash != "" || revs[3].Subject != "first" || revs[1].Path != "f.txt" {
		t.Fatalf("revisions: %v %+v", err, revs)
	}

	if d, _ := RevisionDiff(root, revs[0]); !strings.Contains(d, "+more") {
		t.Fatalf("uncommitted diff: %q", d)
	}

	if d, _ := RevisionDiff(root, revs[3]); !strings.Contains(d, "+v") || strings.Contains(d, "more") {
		t.Fatalf("root commit diff: %q", d)
	}
}

func TestRefs(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "f"), "1\n")
	must(t, "stage all", StageAll(root))
	must(t, "commit one", Commit(root, "one"))

	bare := filepath.Join(t.TempDir(), "o.git")
	for _, a := range [][]string{
		{"-c", "tag.gpgSign=false", "tag", "v1"},
		{"branch", "feat"},
		{"clone", "-q", "--bare", ".", bare},
		{"remote", "add", "origin", bare},
		{"fetch", "-q", "origin"},
	} {
		mustGit(t, root, a...)
	}

	refs, err := Refs(root)

	got := map[string]string{}
	for _, r := range refs {
		got[r.Name] = r.Kind
		if r.Name == "main" && (!r.Head || r.Subject != "one" || r.Hash == "") {
			t.Errorf("main = %+v", r)
		}
	}

	want := map[string]string{"main": "branch", "feat": "branch", "origin/main": "remote", "origin/feat": "remote", "v1": "tag"}
	if err != nil || len(got) != len(want) {
		t.Fatalf("refs %v %v", got, err)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	head := func() string { out, _ := Run(root, "rev-parse", "--abbrev-ref", "HEAD"); return strings.TrimSpace(out) }
	if err := CreateBranch(root, "new", "v1"); err != nil || head() != "new" {
		t.Fatalf("create branch: %v %s", err, head())
	}

	if err := Checkout(root, Ref{Name: "v1", Kind: "tag"}, false); err != nil || head() != "HEAD" {
		t.Fatalf("tag checks out detached: %v %s", err, head())
	}

	if err := Checkout(root, Ref{Name: "feat", Kind: "branch"}, false); err != nil || head() != "feat" {
		t.Fatalf("branch: %v %s", err, head())
	}
}

// TestPublish pushes a branch without an upstream and sets it, as VS Code's
// Publish Branch, adding the remote first when given a URL.
func TestPublish(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "f"), "1\n")
	must(t, "stage all", StageAll(root))
	must(t, "commit one", Commit(root, "one"))

	if rs, err := Remotes(root); err != nil || len(rs) != 0 {
		t.Fatalf("remotes of a fresh repo: %v %v", rs, err)
	}

	if err := Publish(root, "origin", ""); err == nil {
		t.Fatal("publish to a missing remote succeeded")
	}

	bare := filepath.Join(t.TempDir(), "o.git")
	mustGit(t, root, "init", "-q", "--bare", bare)
	mustGit(t, root, "checkout", "-q", "-b", "feat")

	if err := Publish(root, "origin", bare); err != nil {
		t.Fatalf("publish to a new remote: %v", err)
	}

	if rs, err := Remotes(root); err != nil || len(rs) != 1 || rs[0] != (Remote{"origin", bare}) {
		t.Fatalf("remotes after publish: %v %v", rs, err)
	}

	if st, _ := Stat(root); st.Branch != "feat" || st.Upstream != "origin/feat" || st.Ahead+st.Behind != 0 {
		t.Fatalf("after publish: %+v", st)
	}
}

func TestError(t *testing.T) {
	root := repo(t)
	f := filepath.Join(root, "f.txt")
	mustWrite(t, f, "x\n")
	must(t, "stage all", StageAll(root))
	must(t, "commit", Commit(root, "one"))

	_, err := Run(root, "cat-file", "-e", "deadbeef")

	var gerr *Error
	if !errors.As(err, &gerr) {
		t.Fatalf("a failed command must return *Error, got %T", err)
	}

	if gerr.Code != 128 || ExitCode(err) != 128 {
		t.Errorf("code = %d (%v)", gerr.Code, err)
	}

	if !strings.Contains(gerr.Stderr, "deadbeef") || gerr.Error() != gerr.Stderr {
		t.Errorf("stderr = %q, message = %q", gerr.Stderr, gerr.Error())
	}

	if len(gerr.Args) == 0 || gerr.Args[0] != "cat-file" {
		t.Errorf("args = %v", gerr.Args)
	}

	if _, err := Run(root, "diff", "--quiet", "HEAD", "--", "f.txt"); ExitCode(err) != 0 {
		t.Errorf("clean tree: %v", err)
	}
	// Nothing on stderr: the message stays the exec error's, as it always was.
	mustWrite(t, f, "y\n")

	_, err = Run(root, "diff", "--quiet", "HEAD", "--", "f.txt")
	if ExitCode(err) != 1 || err.Error() != "exit status 1" {
		t.Errorf("dirty tree: %v (%d)", err, ExitCode(err))
	}

	if ExitCode(nil) != 0 || ExitCode(errors.New("x")) != -1 {
		t.Error("ExitCode of a nil and of a foreign error")
	}
	// A command the timeout killed keeps the deadline for errors.Is.
	killed := newError([]string{"log"}, "", context.DeadlineExceeded, errors.New("signal: killed"))
	if !errors.Is(killed, context.DeadlineExceeded) || killed.Code != -1 {
		t.Errorf("timeout: %v (%d)", killed, killed.Code)
	}
}

// TestRevisionsNoisyStderr covers a git that prints a warning while still
// exiting 1: the uncommitted row must survive it.
func TestRevisionsNoisyStderr(t *testing.T) {
	root := repo(t)
	mustGit(t, root, "config", "core.autocrlf", "input")
	mustGit(t, root, "config", "core.safecrlf", "warn")
	f := filepath.Join(root, "crlf.txt")
	mustWrite(t, f, "one\r\n")
	must(t, "stage all", StageAll(root))
	must(t, "commit", Commit(root, "one"))
	mustWrite(t, f, "one\r\ntwo\r\n")

	_, err := Run(root, "diff", "--quiet", "HEAD", "--", "crlf.txt")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "CRLF") {
		t.Fatalf("this test needs git to warn on stderr and exit 1, got %v", err)
	}

	revs, err := Revisions(root, "crlf.txt")
	if err != nil || len(revs) != 2 || revs[0].Hash != "" || revs[0].Subject != "Uncommitted changes" {
		t.Fatalf("revisions: %v %+v", err, revs)
	}
}

func TestRevisionsUnborn(t *testing.T) {
	root := repo(t)
	mustWrite(t, filepath.Join(root, "new.txt"), "hello\n")

	revs, err := Revisions(root, "new.txt")
	if err != nil || len(revs) != 1 || revs[0].Hash != "" || revs[0].Subject != "Uncommitted changes" {
		t.Fatalf("unborn branch: %v %+v", err, revs)
	}

	if d, err := RevisionDiff(root, revs[0]); err != nil || !strings.Contains(d, "+hello") {
		t.Fatalf("unborn diff: %v %q", err, d)
	}

	if revs, err := Revisions(t.TempDir(), "new.txt"); err == nil || revs != nil {
		t.Errorf("outside a repository: %v %+v", err, revs)
	}
}

// The full-context diffs ApplyLines feeds pickLines, captured from a
// repository built exactly like TestApplyLines builds one.
const (
	diffLines = "diff --git a/f.txt b/f.txt\n" +
		"index 0719398..ed5edd5 100644\n--- a/f.txt\n+++ b/f.txt\n" +
		"@@ -1,9 +1,9 @@\n 1\n-2\n+TWO\n 3\n 4\n-5\n+FIVE\n 6\n 7\n 8\n-9\n+NINE\n"
	diffNoEOL = "diff --git a/g.txt b/g.txt\n" +
		"index 0a207c0..36ef1ba 100644\n--- a/g.txt\n+++ b/g.txt\n" +
		"@@ -1,2 +1,3 @@\n a\n-b\n\\ No newline at end of file\n+B\n+c\n" +
		"\\ No newline at end of file\n"
)

// conflictTexts is every string ConflictText is allowed to return.
var conflictTexts = []string{
	"both modified", "both added", "both deleted", "added by us",
	"added by them", "deleted by us", "deleted by them", "conflict",
}

// checkEntry asserts what every entry parseStatus returns has to hold: a
// caller joins Path onto the repository root and hands it to git, or to
// os.RemoveAll, so an empty or invented path is a bug.
func checkEntry(t *testing.T, raw string, e Entry) {
	t.Helper()

	switch {
	case e.Path == "":
		t.Fatalf("empty path from %q: %+v", raw, e)
	case !strings.Contains(raw, e.Path):
		t.Fatalf("invented path %q from %q", e.Path, raw)
	case e.Orig != "" && !strings.Contains(raw, e.Orig):
		t.Fatalf("invented rename source %q from %q", e.Orig, raw)
	case strings.ContainsRune(e.Path, 0):
		t.Fatalf("path %q spans the NUL separator in %q", e.Path, raw)
	}
}

// FuzzParseStatus feeds parseStatus the porcelain -z shapes the tests above
// never see: truncated records, CRLF, quoting, non-UTF-8 paths.
func FuzzParseStatus(f *testing.F) {
	for _, raw := range []string{
		"",
		// The fixture TestParseStatus asserts on.
		"## main...origin/main [ahead 2, behind 1]\x00M  staged.go\x00 M changed.go\x00" +
			"MM both.go\x00R  new.go\x00old.go\x00?? untracked.txt\x00UU conflict.go\x00 D gone.go\x00",
		"## No commits yet on dev\x00",
		// A real conflicted status with a rename and the paths -z leaves
		// unquoted: a quote, a newline and a non-UTF-8 byte.
		"## main\x00UU a.txt\x00UD gone.txt\x00R  new.go\x00old.go\x00?? bad\xffname.txt\x00" +
			"?? has\nnewline.txt\x00?? has\"quote.txt\x00",
		// The quoted form of those paths, which only the non -z output uses.
		"## main\x00?? \"has\\nnewline.txt\"\x00?? \"has\\\"quote.txt\"\x00?? \"\\303\\251.txt\"\x00",
		"## main\x00M  a -> b.go\x00",                        // "->" inside a filename
		"## main\x00R  a -> b.go\x00c -> d.go\x00",           // and inside a rename
		"## main...origin/main [ahead 2, behind 1]\x00M  tr", // truncated last record
		"## main\x00R  no-source.go",                         // a rename missing its pair
		"## main\r\n\x00 M crlf.go\r\n\x00",
		"M  a\x00\x00?? b\x00",         // an empty record between two entries
		"?? \x00M  \x00R  \x00## \x00", // records with no path at all
		"## HEAD (no branch)\x00",
		"## main...origin/main [gone]\x00",
		"## m...o/m [ahead 99999999999999999999, behind -3]\x00",
		"## \xff\xfe\x00?? \xff\xfe.txt\x00\\ No newline at end of file\x00",
	} {
		f.Add(raw)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		s := parseStatus(raw)
		for _, e := range s.Staged {
			checkEntry(t, raw, e)

			if !e.Staged || e.Letter == ' ' {
				t.Fatalf("staged entry %+v from %q", e, raw)
			}
		}

		for _, e := range s.Changes {
			checkEntry(t, raw, e)

			if e.Staged || e.Letter == ' ' {
				t.Fatalf("changed entry %+v from %q", e, raw)
			}
		}

		for _, e := range s.Conflicts {
			checkEntry(t, raw, e)

			if e.Letter != '!' || len(e.XY) != 2 {
				t.Fatalf("conflict entry %+v from %q", e, raw)
			}

			if e.XY[0] != 'U' && e.XY[1] != 'U' && e.XY != "AA" && e.XY != "DD" {
				t.Fatalf("XY %q is no conflict, from %q", e.XY, raw)
			}
		}
		// A record yields at most two entries, one staged and one changed.
		if n := len(s.Staged) + len(s.Changes) + len(s.Conflicts); n > 2*(strings.Count(raw, "\x00")+1) {
			t.Fatalf("%d entries out of %q", n, raw)
		}

		if s.Branch != "" && !strings.Contains(raw, s.Branch) {
			t.Fatalf("invented branch %q from %q", s.Branch, raw)
		}

		if s.Upstream != "" && !strings.Contains(raw, s.Upstream) {
			t.Fatalf("invented upstream %q from %q", s.Upstream, raw)
		}
	})
}

// FuzzConflictText checks the tooltip text is total: one clean line for any
// pair, whatever git or a broken parse hands it.
func FuzzConflictText(f *testing.F) {
	for _, xy := range []string{
		"UU", "AA", "DD", "AU", "UA", "DU", "UD", "", "M ", " M", "??",
		"uu", "UUU", "U", "U\x00", "U\n", "\xff\xfe", "A -> B",
	} {
		f.Add(xy)
	}

	f.Fuzz(func(t *testing.T, xy string) {
		got := ConflictText(xy)
		switch {
		case !slices.Contains(conflictTexts, got):
			t.Fatalf("ConflictText(%q) = %q, not one of %v", xy, got, conflictTexts)
		case len(xy) != 2 && got != "conflict":
			t.Fatalf("ConflictText(%q) = %q, want the generic text", xy, got)
		case strings.ContainsAny(got, "\n\x00") || strings.TrimSpace(got) != got:
			t.Fatalf("ConflictText(%q) = %q: the tooltip is one clean line", xy, got)
		}
	})
}

// hunkStarts reports the first hunk header's old and new line numbers the way
// pickLines reads them, so the oracles below know which numbers it can reach.
func hunkStarts(diff string) (int, int, bool) {
	for _, l := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(l, "@@") {
			continue
		}

		fields := strings.Fields(l)
		if len(fields) < 3 {
			return 0, 0, false
		}

		o, _ := strconv.Atoi(strings.SplitN(fields[1][1:], ",", 2)[0])
		n, _ := strconv.Atoi(strings.SplitN(fields[2][1:], ",", 2)[0])

		return o, n, true
	}

	return 0, 0, false
}

// countBody counts the context, removed and added lines of a single-hunk
// diff, classifying them the way pickLines does.
func countBody(diff string) (ctx, del, add int) {
	body := false

	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			body = true
		case !body || l == "\\" || (l != "" && l[0] == '\\'):
		case l == "" || l[0] == ' ':
			ctx++
		case l[0] == '-':
			del++
		case l[0] == '+':
			add++
		}
	}

	return ctx, del, add
}

// outLines counts the lines pickLines wrote; the last one may be unterminated.
func outLines(s string) int {
	if s == "" {
		return 0
	}

	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}

	return n
}

// checkPicked asserts pickLines only ever returns lines the diff already
// held, and drops the final newline only for a "\ No newline" marker.
func checkPicked(t *testing.T, diff, got string) {
	t.Helper()

	if len(got) > len(diff) {
		t.Fatalf("pickLines grew %d bytes into %d", len(diff), len(got))
	}

	if got != "" && !strings.HasSuffix(got, "\n") && !strings.Contains("\n"+diff, "\n\\") {
		t.Fatalf("pickLines dropped the final newline of %q with no marker in %q", got, diff)
	}

	body := map[string]bool{"": true} // a blank diff line is a blank context line

	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if l != "" && strings.IndexByte(" -+", l[0]) >= 0 {
			body[l[1:]] = true
		}
	}

	if got == "" {
		return
	}

	for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if !body[l] {
			t.Fatalf("pickLines invented %q out of %q", l, diff)
		}
	}
}

// FuzzPickLines rebuilds one side of a diff with the selection the masks
// describe. Beyond not panicking it asserts three things: the result only
// holds lines the diff had, the two directions keep every line exactly once
// (context twice), and selecting everything one way equals selecting nothing
// the other, which is how ApplyLines stages and unstages the same hunk.
func FuzzPickLines(f *testing.F) {
	for _, diff := range []string{
		"", "\n", "@@", "@@ -1,1 +1,1 @@", diffLines, diffNoEOL,
		strings.ReplaceAll(diffLines, "\n", "\r\n"),            // CRLF
		diffLines[:len(diffLines)-9],                           // truncated last line
		strings.ReplaceAll(diffLines, "TWO", "T\x00O"),         // an embedded NUL
		strings.ReplaceAll(diffLines, "f.txt", "a -> b.txt"),   // "->" in the filename
		strings.ReplaceAll(diffLines, "f.txt", "\"q\\n.txt\""), // a quoted path
		strings.ReplaceAll(diffLines, "TWO", "T\xffO"),         // non-UTF-8 content
		// "\ No newline at end of file" where git never puts it.
		"@@ -1,2 +1,2 @@\n\\ No newline at end of file\n a\n-b\n+c\n",
		"@@ -1,2 +1,2 @@\n a\n\\ No newline at end of file\n-b\n+c\n\\\n",
		diffLines + diffNoEOL, // two hunks
		"@@ -2,9 +1,9 @@\n a\n",
		"@@ --5,9 +1,9 @@\n a\n-b\n+c\n",
		"@@ -1 +1 @@\n-a\n+b\n",
		"@@ -1,2 +1,2 @@\n\n-b\n+c\n", // a blank context line (diff.suppressBlankEmpty)
	} {
		for _, m := range []uint64{0, ^uint64(0), 0xaaaa, 0x5555} {
			f.Add(diff, m, m>>1)
		}
	}

	f.Fuzz(func(t *testing.T, diff string, delMask, addMask uint64) {
		n := strings.Count(diff, "\n") + 2

		oracle := n <= 2048 // the maps have to cover every line number reached
		if !oracle {
			n = 0
		}

		pick := func(mask uint64) map[int]bool {
			m := make(map[int]bool, n+1)
			for i := range n + 1 {
				m[i] = mask>>(i%64)&1 == 1
			}

			return m
		}
		dels, adds := pick(delMask), pick(addMask)

		var out [2]string

		for i, fromOld := range []bool{true, false} {
			got, err := pickLines(diff, dels, adds, fromOld)
			if err != nil {
				if got != "" {
					t.Fatalf("pickLines(fromOld=%v) = %q with error %v", fromOld, got, err)
				}

				return
			}

			checkPicked(t, diff, got)
			out[i] = got
		}

		o, nw, ok := hunkStarts(diff)
		if !oracle || !ok || o < 0 || nw < 0 {
			return
		}
		// Every removed and added line lands on exactly one side, context on
		// both. A "\ No newline" marker strips the last newline, which makes
		// an empty last line vanish, so lines are only countable without one.
		if ctx, del, add := countBody(diff); !strings.Contains("\n"+diff, "\n\\") {
			if got := outLines(out[0]) + outLines(out[1]); got != 2*ctx+del+add {
				t.Fatalf("%d lines out of %d context, %d removed, %d added in %q", got, ctx, del, add, diff)
			}
		}

		all, none := pick(^uint64(0)), map[int]bool{}
		for _, side := range []struct {
			fromOld bool
			a, b    map[int]bool
			c, d    map[int]bool
			what    string
		}{
			{true, all, all, none, none, "new"},
			{true, none, none, all, all, "old"},
		} {
			x, errx := pickLines(diff, side.a, side.b, side.fromOld)

			y, erry := pickLines(diff, side.c, side.d, !side.fromOld)
			if x != y || (errx == nil) != (erry == nil) {
				t.Fatalf("%s side: %q (%v) != %q (%v) for %q", side.what, x, errx, y, erry, diff)
			}
		}
	})
}
