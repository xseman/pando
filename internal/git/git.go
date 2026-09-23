// Package git wraps the git CLI. No libgit2: every call is `git -C root ...`.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Error is a command that failed: the git call itself, or the gh call behind
// PublishGitHub. Stderr is what it printed; Code is its exit status, or -1
// when it never ran or a signal killed it.
type Error struct {
	Args   []string // the arguments, without the leading -C dir
	Stderr string
	Code   int
	err    error // the *exec.ExitError, or why the command could not run
}

// Error is the command's stderr, or the exec error when it printed nothing.
func (e *Error) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}

	return e.err.Error()
}

// Unwrap returns the exec error, so errors.Is finds context.DeadlineExceeded
// after the 60 s timeout and exec.ErrNotFound when git is missing.
func (e *Error) Unwrap() error { return e.err }

// newError describes a failed command; ctxErr, when set, is the expired
// context that killed it.
func newError(args []string, stderr string, ctxErr, err error) *Error {
	e := &Error{Args: args, Stderr: strings.TrimSpace(stderr), Code: -1, err: err}

	var exit *exec.ExitError
	if errors.As(err, &exit) {
		e.Code = exit.ExitCode()
	}

	if ctxErr != nil {
		e.err = fmt.Errorf("%w: %w", ctxErr, err)
	}

	return e
}

// ExitCode is the exit status behind err: 0 when there is no error, -1 when
// the command never ran, was killed, or did not come from this package.
func ExitCode(err error) int {
	var e *Error
	switch {
	case err == nil:
		return 0
	case errors.As(err, &e):
		return e.Code
	}

	return -1
}

// Run executes `git -C dir args...` and returns its stdout; when git fails
// the error is an *Error carrying its stderr and exit status, and stdout is
// still returned.
func Run(dir string, args ...string) (string, error) { return run(dir, nil, args...) }

// timeout bounds one git or claude invocation.
const timeout = 60 * time.Second

func run(dir string, stdin io.Reader, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = stdin

	var out, errb bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), newError(args, errb.String(), ctx.Err(), err)
	}

	return out.String(), nil
}

// Root returns the top-level of the repository containing dir.
func Root(dir string) (string, error) {
	out, err := Run(dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// MainRoot returns the main worktree of the repository containing dir, so a
// linked worktree resolves to the project it belongs to.
func MainRoot(dir string) (string, error) {
	out, err := Run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}

	if common := strings.TrimSpace(out); filepath.Base(common) == ".git" {
		return filepath.Dir(common), nil
	}

	return Root(dir)
}

// Entry is one path from git status, with the porcelain letter that put it
// in its section.
type Entry struct {
	Path   string // relative to repo root
	Orig   string // rename source
	Letter byte   // M A D R C U(ntracked) !(conflict)
	Staged bool
	XY     string // conflicts only: the porcelain pair (UU AA DD AU UA DU UD)
}

// Status is one parse of git status: the branch with its tracking counts,
// the paths grouped as VS Code groups them, and any operation in progress.
type Status struct {
	Branch    string
	Upstream  string
	Ahead     int
	Behind    int
	Staged    []Entry
	Changes   []Entry
	Conflicts []Entry // VS Code's Merge Changes group
	Op        string  // "merge", "rebase" or "cherry-pick" while one is in progress
	// MergeMsg is the message git prepared for the merge or cherry-pick in
	// progress, comments dropped: VS Code's message box value then.
	MergeMsg string
}

// Stat runs `git status --porcelain --branch` over root and adds the
// operation in progress.
func Stat(root string) (Status, error) {
	out, err := Run(root, "status", "--porcelain", "-z", "--branch", "--renames", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}

	s := parseStatus(out)
	s.Op, s.MergeMsg = operation(root)

	return s, nil
}

// operation names the merge, rebase or cherry-pick in progress, as VS Code
// reads MERGE_HEAD, rebase-merge / rebase-apply and CHERRY_PICK_HEAD, with
// the MERGE_MSG git wrote for it. The files live in the worktree's own git
// dir, so linked worktrees work.
func operation(root string) (op, msg string) {
	out, err := Run(root, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return "", ""
	}

	dir := strings.TrimSpace(out)

	op = operationIn(dir)
	if op == "merge" || op == "cherry-pick" {
		b, _ := os.ReadFile(filepath.Join(dir, "MERGE_MSG")) // none: no message to offer
		msg = mergeMessage(string(b))
	}

	return op, msg
}

// mergeMessage is a MERGE_MSG as `git commit` would clean it up: comment
// lines ("# Conflicts:") dropped, surrounding blank lines trimmed.
func mergeMessage(s string) string {
	var lines []string

	for l := range strings.SplitSeq(s, "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, strings.TrimRight(l, " \t\r"))
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func operationIn(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("MERGE_HEAD"):
		return "merge"
	case exists("rebase-merge"), exists("rebase-apply"):
		return "rebase"
	case exists("CHERRY_PICK_HEAD"):
		return "cherry-pick"
	}

	return ""
}

// ConflictText describes a conflict's porcelain pair the way VS Code's
// tooltip does ("Conflict: Both Modified").
func ConflictText(xy string) string {
	switch xy {
	case "UU":
		return "both modified"
	case "AA":
		return "both added"
	case "DD":
		return "both deleted"
	case "AU":
		return "added by us"
	case "UA":
		return "added by them"
	case "DU":
		return "deleted by us"
	case "UD":
		return "deleted by them"
	}

	return "conflict"
}

// conflictMarkers is VS Code's test for an unresolved file.
var conflictMarkers = regexp.MustCompile(`(?m)^<{7}\s|^={7}$|^>{7}\s`)

// HasConflictMarkers reports whether the working tree file still holds
// <<<<<<< / ======= / >>>>>>> lines. A missing file has none.
func HasConflictMarkers(root, path string) bool {
	b, err := os.ReadFile(filepath.Join(root, path))
	return err == nil && conflictMarkers.Match(b)
}

// parseStatus parses `git status --porcelain -z --branch`.
func parseStatus(raw string) Status {
	var s Status

	parts := strings.Split(raw, "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			// "XY path": shorter than that it carries no path, and an entry
			// with an empty one would name the repository root itself.
			continue
		}

		if strings.HasPrefix(p, "## ") {
			parseBranch(&s, p[3:])
			continue
		}

		x, y, path := p[0], p[1], p[3:]

		var orig string

		if x == 'R' || x == 'C' {
			i++
			if i < len(parts) {
				orig = parts[i]
			}
		}

		switch {
		case x == '?':
			s.Changes = append(s.Changes, Entry{Path: path, Letter: 'U'})
		case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
			s.Conflicts = append(s.Conflicts, Entry{Path: path, Letter: '!', XY: string([]byte{x, y})})
		default:
			if x != ' ' {
				s.Staged = append(s.Staged, Entry{Path: path, Orig: orig, Letter: x, Staged: true})
			}

			if y != ' ' {
				s.Changes = append(s.Changes, Entry{Path: path, Letter: y})
			}
		}
	}

	return s
}

func parseBranch(s *Status, h string) {
	// "main...origin/main [ahead 1, behind 2]" | "No commits yet on main" | "HEAD (no branch)"
	if b, ok := strings.CutPrefix(h, "No commits yet on "); ok {
		s.Branch = b
		return
	}

	head, track, _ := strings.Cut(h, " [")
	s.Branch, s.Upstream, _ = strings.Cut(head, "...")

	for f := range strings.SplitSeq(strings.TrimSuffix(track, "]"), ", ") {
		if n, ok := strings.CutPrefix(f, "ahead "); ok {
			s.Ahead, _ = strconv.Atoi(n)
		}

		if n, ok := strings.CutPrefix(f, "behind "); ok {
			s.Behind, _ = strconv.Atoi(n)
		}
	}
}

// Decorations maps absolute paths to status letters; directories containing
// changes map to '*'. Ignored top-level entries map to 'I'.
func Decorations(root string, st Status) map[string]byte {
	d := map[string]byte{}

	for _, e := range slices.Concat(st.Staged, st.Changes, st.Conflicts) {
		abs := filepath.Join(root, e.Path)
		if prev, ok := d[abs]; !ok || prev == 'I' || e.Letter == '!' {
			d[abs] = e.Letter
		}

		for dir := filepath.Dir(abs); len(dir) > len(root); dir = filepath.Dir(dir) {
			if _, ok := d[dir]; !ok {
				d[dir] = '*'
			}
		}
	}

	if out, err := Run(root, "status", "--porcelain", "-z", "--ignored=traditional", "--untracked-files=normal"); err == nil {
		for p := range strings.SplitSeq(out, "\x00") {
			if rel, ok := strings.CutPrefix(p, "!! "); ok {
				d[filepath.Join(root, strings.TrimSuffix(rel, "/"))] = 'I'
			}
		}
	}

	return d
}

// Discover returns the repo containing dir plus child repos up to two levels down.
func Discover(dir string) []string {
	var roots []string
	if r, err := Root(dir); err == nil {
		roots = append(roots, r)
	}

	var (
		child []string
		walk  func(d string, depth int)
	)

	walk = func(d string, depth int) {
		if depth == 0 {
			return
		}

		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			n := e.Name()
			if !e.IsDir() || strings.HasPrefix(n, ".") || n == "node_modules" || n == "target" || n == "vendor" {
				continue
			}

			p := filepath.Join(d, n)
			if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
				child = append(child, p)
			}

			walk(p, depth-1)
		}
	}
	walk(dir, 2)
	slices.Sort(child)

	for _, c := range child {
		if r, err := Root(c); err == nil && !slices.Contains(roots, r) {
			roots = append(roots, r)
		}
	}

	return roots
}

// Stage runs `git add -A` on e, and on its rename source when it has one.
func Stage(root string, e Entry) error {
	args := []string{"add", "-A", "--", e.Path}
	if e.Orig != "" {
		args = append(args, e.Orig)
	}

	_, err := Run(root, args...)

	return err
}

// Unstage runs `git reset` on e, or `git rm --cached` while the repository
// has no commit to reset to.
func Unstage(root string, e Entry) error {
	args := []string{"reset", "-q", "--", e.Path}
	if e.Orig != "" {
		args = append(args, e.Orig)
	}

	if !hasHead(root) {
		args = []string{"rm", "--cached", "-q", "-r", "--", e.Path}
	}

	_, err := Run(root, args...)

	return err
}

// StageAll runs `git add -A` over the whole worktree.
func StageAll(root string) error { _, err := Run(root, "add", "-A"); return err }

// StageTracked stages modifications and deletions of tracked files only.
func StageTracked(root string) error { _, err := Run(root, "add", "-u"); return err }

// StageFiles stages the given repo-relative paths in one call.
func StageFiles(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	_, err := Run(root, append([]string{"add", "-A", "--"}, paths...)...)

	return err
}

// DiscardTracked reverts every unstaged change to tracked files.
func DiscardTracked(root string) error { _, err := Run(root, "checkout", "--", "."); return err }

// UnstageAll runs `git reset`, or `git rm --cached -r` while the repository
// has no commit to reset to.
func UnstageAll(root string) error {
	if !hasHead(root) {
		_, err := Run(root, "rm", "--cached", "-q", "-r", ".")
		return err
	}

	_, err := Run(root, "reset", "-q")

	return err
}

// Remove deletes a path from the index and the working tree: VS Code's
// "Delete File" answer to a deletion conflict.
func Remove(root string, e Entry) error {
	_, err := Run(root, "rm", "-q", "--", e.Path)
	return err
}

// Discard reverts an unstaged change; untracked files are deleted.
func Discard(root string, e Entry) error {
	if e.Letter == 'U' {
		return os.RemoveAll(filepath.Join(root, e.Path))
	}

	_, err := Run(root, "checkout", "--", e.Path)

	return err
}

// LineOp moves selected diff lines between the working tree, the index and HEAD.
type LineOp int

// Where ApplyLines moves the selected lines.
const (
	StageLines   LineOp = iota // working tree changes into the index
	UnstageLines               // index changes back to HEAD
	RevertLines                // working tree changes back to the index
)

// ApplyLines stages, unstages or reverts the selected changes of e's diff:
// deletions by old line number, additions by new line number. Like VS Code
// it rebuilds the whole target file from a full-context diff instead of
// editing patches, so there are no hunk offsets to get wrong.
func ApplyLines(root string, e Entry, op LineOp, dels, adds map[int]bool) error {
	args := []string{"diff", "--no-ext-diff", "--no-color", "--unified=1000000000"}
	if op == UnstageLines {
		args = append(args, "--cached")
	}

	diff, err := Run(root, append(args, "--", e.Path)...)
	if err != nil {
		return err
	}

	content, err := pickLines(diff, dels, adds, op == StageLines)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}

	if op != RevertLines {
		return writeIndex(root, e.Path, content)
	}
	// ponytail: written as git sees it, without smudge filters (CRLF, LFS).
	path, mode := filepath.Join(root, e.Path), os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}

	return os.WriteFile(path, []byte(content), mode)
}

// pickLines rebuilds one side of a single-hunk, full-context diff: fromOld
// starts from the old version and applies the selected changes, otherwise it
// starts from the new version and undoes them.
func pickLines(diff string, dels, adds map[int]bool, fromOld bool) (string, error) {
	var b strings.Builder

	oldNo, newNo, hunks := 0, 0, 0
	kept, noEOL := false, -1

	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if strings.HasPrefix(line, "@@") {
			f := strings.Fields(line)

			hunks++
			if hunks > 1 || len(f) < 3 {
				return "", errors.New("unexpected diff hunks")
			}

			oldNo, _ = strconv.Atoi(strings.SplitN(f[1][1:], ",", 2)[0])

			newNo, _ = strconv.Atoi(strings.SplitN(f[2][1:], ",", 2)[0])
			if oldNo > 1 || newNo > 1 {
				return "", errors.New("diff does not start at the first line")
			}

			continue
		}

		if hunks == 0 {
			continue
		}

		if line == "" { // diff.suppressBlankEmpty
			line = " "
		}

		switch line[0] {
		case ' ':
			kept = true
			oldNo++
			newNo++

		case '-':
			kept = dels[oldNo] != fromOld
			oldNo++

		case '+':
			kept = adds[newNo] == fromOld
			newNo++

		case '\\': // "\ No newline at end of file" belongs to the line before
			if kept {
				noEOL = b.Len()
			}

			continue

		default:
			continue
		}

		if kept {
			b.WriteString(line[1:])
			b.WriteByte('\n')
		}
	}

	if hunks == 0 {
		return "", errors.New("no text changes")
	}

	s := b.String()
	if noEOL == len(s) && s != "" {
		s = s[:len(s)-1]
	}

	return s, nil
}

// writeIndex stores content as path's staged version, keeping its file mode.
func writeIndex(root, path, content string) error {
	mode := "100644"
	if out, _ := Run(root, "ls-files", "-s", "--", path); len(strings.Fields(out)) > 0 {
		mode = strings.Fields(out)[0]
	}

	sha, err := run(root, strings.NewReader(content), "hash-object", "-w", "--no-filters", "--stdin")
	if err != nil {
		return err
	}

	_, err = Run(root, "update-index", "--add", "--cacheinfo", mode+","+strings.TrimSpace(sha)+","+path)

	return err
}

// Ref is a branch, remote branch or tag with its latest commit.
type Ref struct {
	Name, Kind, When, Author, Hash, Subject string // Kind: branch, remote or tag
	Head                                    bool   // the checked-out branch
}

// Refs lists branches, remote branches and tags, most recent first.
func Refs(root string) ([]Ref, error) {
	out, err := Run(root, "for-each-ref", "--sort=-creatordate",
		"--format=%(refname)%00%(refname:short)%00%(creatordate:relative)%00%(authorname)%00%(objectname:short)%00%(subject)%00%(HEAD)",
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}

	var refs []Ref

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 7 || strings.HasSuffix(f[0], "/HEAD") {
			continue
		}

		r := Ref{Name: f[1], Kind: "tag", When: f[2], Author: f[3], Hash: f[4], Subject: f[5], Head: f[6] == "*"}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			r.Kind = "branch"
		case strings.HasPrefix(f[0], "refs/remotes/"):
			r.Kind = "remote"
		}

		refs = append(refs, r)
	}

	return refs, nil
}

// Checkout switches to a branch, a remote branch as a new tracking branch,
// or anything else detached.
func Checkout(root string, r Ref, detach bool) error {
	args := []string{"switch", r.Name}
	switch {
	case detach || r.Kind == "tag":
		args = []string{"switch", "--detach", r.Name}
	case r.Kind == "remote":
		args = []string{"switch", "--track", r.Name}
	}

	_, err := Run(root, args...)

	return err
}

// CreateBranch creates a branch at from (HEAD when "") and switches to it.
func CreateBranch(root, name, from string) error {
	args := []string{"switch", "-c", name}
	if from != "" {
		args = append(args, from)
	}

	_, err := Run(root, args...)

	return err
}

// Revision is a commit that changed a file; Hash "" stands for the file's
// uncommitted changes. Path is the file's name in that commit.
type Revision struct{ Hash, Short, When, Subject, Path string }

// Revisions lists path's revisions newest first, following renames: its
// uncommitted changes when there are any, then every commit that touched it.
// It returns revisions or an error, never both.
func Revisions(root, path string) ([]Revision, error) {
	var revs []Revision

	if !hasHead(root) {
		// An unborn branch has no HEAD to diff or log against and nothing
		// committed, so the file is uncommitted in full; a directory that is
		// no repository at all still has to report git's error.
		if _, err := Run(root, "rev-parse", "--git-dir"); err != nil {
			return nil, err
		}

		return []Revision{{Subject: "Uncommitted changes", Path: path}}, nil
	}

	if _, err := Run(root, "diff", "--quiet", "HEAD", "--", path); ExitCode(err) == 1 {
		revs = append(revs, Revision{Subject: "Uncommitted changes", Path: path})
	}

	out, err := Run(root, "log", "--follow", "--format=%x00%H%x1f%h%x1f%ar%x1f%s", "--name-only", "--", path)
	if err != nil {
		return nil, err
	}

	for _, rec := range strings.Split(out, "\x00")[1:] {
		head, names, _ := strings.Cut(rec, "\n")

		f := strings.Split(head, "\x1f")
		if len(f) < 4 {
			continue
		}

		name := path
		if n := strings.Fields(names); len(n) > 0 {
			name = n[0]
		}

		revs = append(revs, Revision{Hash: f[0], Short: f[1], When: f[2], Subject: f[3], Path: name})
	}

	return revs, nil
}

// RevisionDiff is what revision r changed in its file; a merge compares
// with its first parent.
func RevisionDiff(root string, r Revision) (string, error) {
	switch {
	case r.Hash != "":
		return Run(root, "show", "--format=", "--no-color", "--no-ext-diff", "--diff-merges=first-parent", r.Hash, "--", r.Path)
	case !hasHead(root):
		// No HEAD to diff against, so the whole file reads as added.
		out, err := Run(root, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", r.Path)
		return out, noIndex(err)
	}

	return Run(root, "diff", "--no-color", "--no-ext-diff", "HEAD", "--", r.Path)
}

// noIndex drops the exit status 1 that `git diff --no-index` returns when the
// files differ, which is the diff that was asked for.
func noIndex(err error) error {
	if ExitCode(err) == 1 {
		return nil
	}

	return err
}

// hasHead reports whether root has a commit: an unborn branch has none.
func hasHead(root string) bool {
	_, err := Run(root, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// Commit runs `git commit -m msg`, committing what is staged.
func Commit(root, msg string) error { _, err := Run(root, "commit", "-q", "-m", msg); return err }

// Continue finishes the operation op (see operation) once its conflicts are
// staged: a merge or cherry-pick is a commit, with the message git prepared
// when msg is empty; a rebase goes on with the message it already has.
func Continue(root, op, msg string) error {
	switch {
	case op == "rebase":
		_, err := Run(root, "-c", "core.editor=true", "rebase", "--continue")
		return err

	case msg == "": // --no-edit alone would keep the "# Conflicts:" comments
		_, err := Run(root, "commit", "-q", "--no-edit", "--cleanup=strip")
		return err
	}

	return Commit(root, msg)
}

// Amend rewrites the last commit with the staged changes, keeping its
// message when msg is empty.
func Amend(root, msg string) error {
	args := []string{"commit", "-q", "--amend", "--no-edit"}
	if msg != "" {
		args = []string{"commit", "-q", "--amend", "-m", msg}
	}

	_, err := Run(root, args...)

	return err
}

// Sync runs `pull --rebase --autostash` then `push`, VS Code's Sync Changes.
func Sync(root string) error {
	if _, err := Run(root, "pull", "--rebase", "--autostash"); err != nil {
		return err
	}

	_, err := Run(root, "push")

	return err
}

// Publish pushes the current branch to remote and sets it as the upstream,
// VS Code's Publish Branch; url, when given, first adds the remote.
func Publish(root, remote, url string) error {
	if url != "" {
		if _, err := Run(root, "remote", "add", remote, url); err != nil {
			return err
		}
	}

	_, err := Run(root, "push", "-u", remote, "HEAD")

	return err
}

// Remote is a named remote with its push URL.
type Remote struct{ Name, URL string }

// Remotes lists the remotes with their push URL.
func Remotes(root string) ([]Remote, error) {
	out, err := Run(root, "remote", "-v")
	if err != nil {
		return nil, err
	}

	var rs []Remote

	for _, l := range strings.Split(out, "\n") {
		name, rest, ok := strings.Cut(l, "\t")
		if url, kind, _ := strings.Cut(rest, " "); ok && kind == "(push)" {
			rs = append(rs, Remote{name, url})
		}
	}

	return rs, nil
}

// HasGH reports whether gh is installed: it stands in for VS Code's GitHub
// extension, the publisher offered when a repository has no remote.
func HasGH() bool { _, err := exec.LookPath("gh"); return err == nil }

// GHLogin is the gh user, "" when it is not signed in.
func GHLogin() string {
	out, _ := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	return strings.TrimSpace(string(out))
}

// PublishGitHub creates the repository name on GitHub with gh, adds it as
// origin and pushes the current branch, the GitHub extension's Publish to
// GitHub.
func PublishGitHub(root, name string, private bool) error {
	vis := "--public"
	if private {
		vis = "--private"
	}

	args := []string{"repo", "create", name, vis, "--source", root, "--remote", "origin", "--push"}

	out, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return newError(args, string(out), nil, err)
	}

	return nil
}

// Diff returns the patch for one entry (untracked files diff against /dev/null).
func Diff(root string, e Entry) (string, error) {
	switch {
	case e.Letter == 'U':
		out, err := Run(root, "diff", "--no-index", "--", "/dev/null", e.Path)
		return out, noIndex(err)

	case e.Letter == '!':
		// The working tree against our side (stage 2): a plain two-way diff
		// where `git diff` alone would print a combined one.
		return Run(root, "diff", "--ours", "--", e.Path)
	case e.Staged:
		return Run(root, "diff", "--cached", "--", e.Path)
	default:
		return Run(root, "diff", "--", e.Path)
	}
}

// Drawer is a history pane under the changes; Args nil means File History,
// which follows the selected file.
type Drawer struct {
	Title string
	Args  []string
}

// Drawers are the history lists shown under the changes, one git command each.
var Drawers = []Drawer{
	{"Graph", []string{"log", "--graph", "--oneline", "--decorate=short", "-n", "200", "--all"}},
	{"Commits", []string{"log", "--format=%h %ad %s", "--date=short", "-n", "200"}},
	{"File History", nil},
	{"Branches", []string{"branch", "-a", "--format=%(HEAD) %(refname:short) %(upstream:track)"}},
	{"Worktrees", []string{"worktree", "list"}},
	{"Remotes", []string{"remote", "-v"}},
	{"Stashes", []string{"stash", "list"}},
	{"Tags", []string{"tag", "--sort=-creatordate"}},
}

// FileHistory lists the commits that touched path, following renames.
func FileHistory(root, path string) []string {
	return Lines(root, "log", "--format=%h %ad %s", "--date=short", "--follow", "-n", "200", "--", path)
}

// Lines runs git and splits its stdout into lines; a failure comes back as
// a single "error: …" line, for the drawers that only render text.
func Lines(root string, args ...string) []string {
	out, err := Run(root, args...)
	if err != nil {
		return []string{"error: " + err.Error()}
	}

	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}

	return strings.Split(out, "\n")
}

// Worktree is one checkout of a repository; Branch is "(detached)" when it
// has no branch.
type Worktree struct {
	Path   string
	Branch string
}

// Worktrees lists the repository's worktrees from `git worktree list
// --porcelain`, the main one first.
func Worktrees(root string) ([]Worktree, error) {
	out, err := Run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var wts []Worktree

	for block := range strings.SplitSeq(strings.TrimSpace(out), "\n\n") {
		var w Worktree

		for line := range strings.SplitSeq(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				w.Path = p
			}

			if b, ok := strings.CutPrefix(line, "branch "); ok {
				w.Branch = strings.TrimPrefix(b, "refs/heads/")
			}

			if line == "detached" {
				w.Branch = "(detached)"
			}
		}

		if w.Path != "" {
			wts = append(wts, w)
		}
	}

	return wts, nil
}

// RandomBranch names a new worktree's branch when none is given, as herdr
// does: worktree/<adjective>-<noun>-<4 hex digits>.
func RandomBranch() string {
	adjectives := []string{
		"brave", "calm", "clear", "green", "lucky", "quiet", "rapid", "silver",
		"bold", "bright", "gentle", "golden", "keen", "swift", "warm", "wild",
	}
	nouns := []string{
		"river", "cloud", "field", "forest", "harbor", "meadow", "stone", "valley",
		"brook", "canyon", "cedar", "dune", "grove", "lake", "ridge", "shore",
	}

	return fmt.Sprintf("worktree/%s-%s-%04x",
		adjectives[rand.IntN(len(adjectives))], nouns[rand.IntN(len(nouns))], rand.IntN(0x10000))
}

// AddWorktree creates path on branch, creating the branch when it does not exist.
func AddWorktree(root, path, branch string) error {
	if _, err := Run(root, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil {
		_, err = Run(root, "worktree", "add", path, branch)
		return err
	}

	_, err := Run(root, "worktree", "add", "-b", branch, path)

	return err
}

// RemoveWorktree runs `git worktree remove`, which refuses a worktree with
// changes in it.
func RemoveWorktree(root, path string) error {
	_, err := Run(root, "worktree", "remove", path)
	return err
}

// ListFiles returns repo-relative files honoring .gitignore, capped at limit.
// Outside a repo it walks the directory, skipping dot-dirs.
func ListFiles(dir string, limit int) []string {
	var files []string

	if out, err := Run(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard"); err == nil {
		for f := range strings.SplitSeq(out, "\x00") {
			if f != "" {
				files = append(files, f)
			}

			if len(files) >= limit {
				break
			}
		}

		return files
	}
	// The walk never fails: an unreadable directory is skipped, and hitting
	// the limit is how it stops early.
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(files) >= limit {
			return filepath.SkipDir
		}

		if d.IsDir() && p != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}

		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, rel)
		}

		return nil
	})

	return files
}

const suggestPrompt = "Write a git commit message for the diff on stdin: one imperative " +
	"subject line under 72 characters, no quotes, no trailing period. Reply with ONLY the message line."

// Suggest asks the local `claude` CLI for a subject line for the staged diff
// (or the unstaged one when nothing is staged).
func Suggest(root string) (string, error) {
	diff, _ := Run(root, "diff", "--cached", "--stat", "--patch")
	if strings.TrimSpace(diff) == "" {
		diff, _ = Run(root, "diff", "--stat", "--patch")
	}

	if strings.TrimSpace(diff) == "" {
		return "", errors.New("no changes to describe")
	}

	if len(diff) > 16*1024 {
		diff = diff[:16*1024] + "\n[diff truncated]"
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", "--strict-mcp-config", suggestPrompt)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(diff)

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	msg := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
	if msg == "" {
		return "", errors.New("empty suggestion")
	}

	return strings.Trim(msg, "\"`"), nil
}
