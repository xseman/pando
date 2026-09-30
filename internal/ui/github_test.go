package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// fakeGH is a gh on PATH that answers from the files in dir, "pr-list" for
// gh pr list, "notes" for the notifications, and fails with the text of
// "fail" when that file is there. It logs each call's arguments to log.
type fakeGH struct{ dir, log string }

func newFakeGH(t *testing.T) fakeGH {
	t.Helper()

	f := fakeGH{dir: t.TempDir()}
	f.log = filepath.Join(f.dir, "calls")
	bin := filepath.Join(f.dir, "bin", "gh")
	mustWrite(t, bin, `#!/bin/sh
echo "$*" >> '`+f.log+`'
d='`+f.dir+`'
if [ -f "$d/fail" ]; then cat "$d/fail" >&2; exit 1; fi
case "$1" in
api) [ "$2" = -X ] && exit 0; f=notes ;;
*) f="$1-$2" ;;
esac
if [ -f "$d/$f" ]; then cat "$d/$f"; else echo '[]'; fi
`)

	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatalf("chmod gh: %v", err)
	}

	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	return f
}

func (f fakeGH) answer(t *testing.T, name, content string) {
	t.Helper()
	mustWrite(t, filepath.Join(f.dir, name), content)
}

func (f fakeGH) calls(t *testing.T) []string {
	t.Helper()

	b, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}

	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

const (
	ghPRList = `[
{"number":14,"title":"release v0.4.0","author":{"login":"ann"},"url":"https://github.com/o/r/pull/14","isDraft":false,"headRefName":"release","baseRefName":"main","headRefOid":"5299565","isCrossRepository":false,
 "statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]},
{"number":13,"title":"fix: employees see suppliers","author":{"login":"bob"},"url":"https://github.com/o/r/pull/13","isDraft":true,"headRefName":"main","baseRefName":"main","headRefOid":"1111111","isCrossRepository":true,
 "statusCheckRollup":[{"__typename":"StatusContext","context":"ci/jenkins","state":"PENDING"}]},
{"number":12,"title":"feat: streets by batch ids","author":{"login":"eve"},"url":"https://github.com/o/r/pull/12","isDraft":false,"headRefName":"feat","baseRefName":"main","headRefOid":"2222222","isCrossRepository":false,
 "statusCheckRollup":[{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"}]}
]`
	ghIssueList = `[{"number":88,"title":"Search ignores .gitignore","author":{"login":"ann"},"url":"https://github.com/o/r/issues/88"}]`
	ghNoteList  = `[{"id":"101","unread":true,"reason":"review_requested","updated_at":"2026-09-30T10:00:00Z",
 "subject":{"title":"feat: SSD-621 alter deduction","url":"https://api.github.com/repos/o/r/pulls/258","type":"PullRequest"},
 "repository":{"full_name":"o/r","html_url":"https://github.com/o/r"}}]`
)

// ghModel is testModel with gh installed and answering with the lists above,
// the GitHub view shown and focused, and the command showing it returned:
// the gh calls, not made yet, for ghRun.
func ghModel(t *testing.T) (*Model, fakeGH, tea.Cmd) {
	t.Helper()

	f := newFakeGH(t)
	f.answer(t, "pr-list", ghPRList)
	f.answer(t, "issue-list", ghIssueList)
	f.answer(t, "notes", ghNoteList)

	m := testModelSized(t, 100, 40)
	m.hasGH = true

	return m, f, m.showView(viewGitHub)
}

// ghRun runs the gh calls the view is waiting for and hands it the answers.
func ghRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()

	for _, msg := range runAll(cmd) {
		m.Update(msg)
	}
}

// ghRowText is the rows of the view as plain text, runs of blanks as one.
func ghRowText(m *Model) []string {
	var out []string
	for _, r := range m.gh.rows(m) {
		out = append(out, strings.Join(strings.Fields(ansi.Strip(m.gh.renderRow(m, r, 80, false, false))), " "))
	}

	return out
}

func ghSelect(t *testing.T, m *Model, key string) {
	t.Helper()

	i := slices.IndexFunc(m.gh.rows(m), func(r ghRow) bool { return r.key == key })
	if i < 0 {
		t.Fatalf("no row %s in %q", key, ghRowText(m))
	}

	m.gh.l.sel = i
}

func TestGitHubViewNeedsGH(t *testing.T) {
	m := testModel(t)
	has := func() bool {
		return slices.ContainsFunc(m.cols(), func(c col) bool { return slices.Contains(c.views, viewGitHub) })
	}

	if has() || hasCommand(m, "view.showGithub") {
		t.Fatal("a GitHub view without gh")
	}

	m.focus = 0
	press(m, "6")

	if m.shown(viewGitHub) {
		t.Fatal("6 showed a view that is not there")
	}

	m.hasGH = true
	if !has() || !hasCommand(m, "view.showGithub") {
		t.Fatal("no GitHub view with gh")
	}

	press(m, "6")

	if !m.shown(viewGitHub) || !m.focused(viewGitHub) {
		t.Fatal("6 does not show the GitHub view")
	}
}

func TestGitHubViewLists(t *testing.T) {
	m, f, load := ghModel(t)
	ghRun(t, m, load)

	// What starts unfolded is what gh is asked for: All Open, My Issues and
	// the notifications; the folded queries wait for their turn.
	calls := f.calls(t)
	if len(calls) != 3 || !strings.HasPrefix(calls[0], "pr list --limit 50 ") ||
		!strings.HasPrefix(calls[1], "issue list ") || !strings.HasSuffix(calls[1], "--assignee @me") ||
		calls[2] != "api repos/{owner}/{repo}/notifications" {
		t.Fatalf("gh calls %q", calls)
	}

	checkWidths(t, m)

	// Checks on the right: ✓ passed, ⇅ running, ✕ failed. A draft is faint.
	rows := ghRowText(m)
	for _, want := range []string{
		"▾ PULL REQUESTS", "▸ Waiting For My Review", "▾ All Open 3", "▾ ISSUES", "# Search ignores .gitignore #88 @ann",
		"▾ NOTIFICATIONS 1", "PR feat: SSD-621 alter deduction review requested",
	} {
		if !slices.Contains(rows, want) {
			t.Errorf("no row %q in %q", want, rows)
		}
	}

	for _, want := range []string{"#14 @ann ✓", "#13 @bob ⇅", "#12 @eve ✕"} {
		if !slices.ContainsFunc(rows, func(r string) bool { return strings.HasSuffix(r, want) }) {
			t.Errorf("no row ending %q in %q", want, rows)
		}
	}

	// Unfolding a query asks gh for it, and only it.
	ghSelect(t, m, "q:Waiting For My Review")

	_, cmd := m.Update(keyMsg("enter"))
	ghRun(t, m, cmd)

	if calls := f.calls(t); len(calls) != 4 || !strings.HasSuffix(calls[3], "--search review-requested:@me") {
		t.Fatalf("gh calls after unfolding %q", calls)
	}

	m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	checkWidths(t, m)
}

// TestGitHubLocalBranches lists the pull requests whose branch is local, by
// the name they check out as: a fork's head named main is not the local main.
func TestGitHubLocalBranches(t *testing.T) {
	m, _, load := ghModel(t)
	mustGit(t, m.ws, "init", "-q", "-b", "main")
	mustGit(t, m.ws, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	mustGit(t, m.ws, "branch", "feat")

	m.gh.open["q:Local Pull Request Branches"] = true
	ghRun(t, m, load)

	var local []int

	rows := m.gh.rows(m)
	for i, r := range rows {
		if r.kind == ghrItem && r.q.local {
			local = append(local, rows[i].item.Number)
		}
	}

	if !slices.Equal(local, []int{12}) {
		t.Errorf("local pull requests %v, want [12]", local)
	}
}

func TestGHErrKind(t *testing.T) {
	for _, c := range []struct {
		stderr string
		want   int
	}{
		// A remote on another host names gh auth login too: it is no login matter.
		{"none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login`", ghErrNotGitHub},
		{"To get started with GitHub CLI, please run:  gh auth login\nAlternatively, populate the GH_TOKEN environment variable with a GitHub API authentication token.", ghErrAuth},
		{"HTTP 401: Bad credentials (https://api.github.com/graphql)\nTry authenticating with:  gh auth login -h github.com", ghErrAuth},
		{"No git remotes found", ghErrNoRemote},
		{"HTTP 502: Bad Gateway", ghErrOther},
	} {
		if got := ghErrKind(c.stderr); got != c.want {
			t.Errorf("ghErrKind(%q) = %d, want %d", c.stderr, got, c.want)
		}
	}
}

func TestGitHubSignIn(t *testing.T) {
	m, f, load := ghModel(t)
	f.answer(t, "fail", "To get started with GitHub CLI, please run:  gh auth login")
	ghRun(t, m, load)

	// Every list failed the same way: the view says it once.
	if rows := ghRowText(m); !slices.Equal(rows, []string{"Sign in to GitHub…"}) {
		t.Fatalf("rows %q", rows)
	}

	checkWidths(t, m)

	f.answer(t, "fail", "none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login`")
	ghRun(t, m, m.gh.refresh(m))

	if rows := ghRowText(m); !slices.Equal(rows, []string{"Not a GitHub repository"}) {
		t.Fatalf("rows on a GitLab remote %q", rows)
	}

	m.gh.l.sel = 0
	if _, cmd := m.Update(keyMsg("enter")); cmd != nil {
		t.Error("⏎ on a repository elsewhere does something")
	}

	// A failure of one list stays under it.
	mustRemove(t, filepath.Join(f.dir, "fail"))
	ghRun(t, m, m.gh.refresh(m))
	f.answer(t, "fail", "HTTP 502: Bad Gateway")

	m.gh.open["q:Created By Me"] = true
	ghRun(t, m, m.gh.ensure(m))

	rows := ghRowText(m)
	if i := slices.Index(rows, "▾ Created By Me"); i < 0 || i+1 >= len(rows) || rows[i+1] != "gh: HTTP 502: Bad Gateway" || !slices.Contains(rows, "PR release v0.4.0 #14 @ann ✓") {
		t.Errorf("rows %q", rows)
	}
}

// TestGitHubAnswersInFlight drops the answers a refresh or another project
// made stale, and keeps the selection on its row when a list reorders.
func TestGitHubAnswersInFlight(t *testing.T) {
	m, f, load := ghModel(t)
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#13")

	stale := runAll(m.gh.refresh(m))
	m.gh.refresh(m)

	f.answer(t, "pr-list", `[{"number":13,"title":"fix: employees see suppliers","author":{"login":"bob"}},{"number":14,"title":"release v0.4.0","author":{"login":"ann"}}]`)

	for _, msg := range stale {
		m.Update(msg)
	}

	if r := m.gh.res["open"]; !r.busy {
		t.Fatal("an answer to an earlier refresh was taken")
	}

	ghRun(t, m, m.gh.refresh(m))

	if r := m.gh.selected(m); r == nil || r.key != "i:All Open#13" || m.gh.res["open"].items[0].Number != 13 {
		t.Errorf("selection %+v after #13 moved up", r)
	}

	m.Update(ghMsg{project: "/elsewhere", key: "open", gen: m.gh.gen, res: ghRes{loaded: true}})

	if len(m.gh.res["open"].items) != 2 {
		t.Error("another project's answer replaced the list")
	}
}

func TestGHMarkdown(t *testing.T) {
	d := ghDoc{Body: "Adds batch lookup.", State: "OPEN"}
	d.Number, d.Title, d.Author.Login, d.HeadRefName, d.BaseRefName, d.URL = 12, "feat: streets", "eve", "feat", "main", "https://github.com/o/r/pull/12"
	d.Rollup = []git.Check{{Typename: "CheckRun", Name: "test", Status: "COMPLETED", Conclusion: "SUCCESS"}, {Typename: "StatusContext", Context: "ci/jenkins", State: "PENDING"}}
	d.Comments = append(d.Comments, struct {
		Author    struct{ Login string }
		Body      string
		CreatedAt time.Time
	}{Body: "looks good"})
	d.Comments[0].Author.Login = "bob"

	md := ghMarkdown("pr", d)
	for _, want := range []string{
		"# feat: streets #12\n", "**@eve** wants to merge `feat` into `main` · Open · checks pending", "Adds batch lookup.",
		"## Checks\n\n- ✓ test\n- ○ ci/jenkins\n", "## Comments", "**@bob**", "looks good", "https://github.com/o/r/pull/12",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("no %q in\n%s", want, md)
		}
	}

	if md := ghMarkdown("issue", ghDoc{State: "OPEN"}); !strings.Contains(md, "opened this issue · Open") || !strings.Contains(md, "_No description provided._") {
		t.Errorf("issue page\n%s", md)
	}
}

func TestGitHubOpensInTheEditor(t *testing.T) {
	m, f, load := ghModel(t)
	f.answer(t, "pr-view", `{"number":14,"title":"release v0.4.0","author":{"login":"ann"},"body":"The release.","state":"OPEN","headRefName":"release","baseRefName":"main","url":"https://github.com/o/r/pull/14"}`)
	f.answer(t, "pr-diff", "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\n")
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#14")

	_, cmd := m.Update(keyMsg("enter"))
	ghRun(t, m, cmd)

	if name, ctx := m.pv.label(m.ws); m.pv.kind != pvGH || name != "PR #14" || ctx != "description" {
		t.Fatalf("editor %q %q (%s)", name, ctx, m.pv.kind)
	}

	if text := ansi.Strip(strings.Join(m.pv.lines, "\n")); !strings.Contains(text, "release v0.4.0 #14") || !strings.Contains(text, "The release.") || strings.Contains(text, "# release") {
		t.Errorf("rendered page\n%s", text)
	}

	m.focus = m.colOf(viewGitHub)
	_, cmd = m.Update(keyMsg("d"))
	ghRun(t, m, cmd)

	if m.pv.kind != pvGHDiff || m.pv.meta == nil || !slices.Contains(f.calls(t), "pr diff 14") {
		t.Errorf("changes: kind %s, meta %v, calls %q", m.pv.kind, m.pv.meta != nil, f.calls(t))
	}

	checkWidths(t, m)
}

func TestPRBranch(t *testing.T) {
	if b := prBranch(git.Item{Number: 12, HeadRefName: "feat"}); b != "feat" {
		t.Errorf("same repository: %s", b)
	}

	if b := prBranch(git.Item{Number: 13, HeadRefName: "main", IsCrossRepository: true}); b != "pr/13" {
		t.Errorf("a fork's main: %s", b)
	}

	if s := slug("Search ignores .gitignore — rovnako pomenované ulice, and a title far too long to keep"); s != "search-ignores-gitignore-rovnako-pomenov" {
		t.Errorf("slug %q", s)
	}
}

// TestGitHubWorktrees: w on a pull request whose branch a worktree has goes
// there without gh; on an issue it offers a branch named after it.
func TestGitHubWorktrees(t *testing.T) {
	m, f, load := ghModel(t)
	ghRun(t, m, load)

	wt := t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: wt, Project: m.ws, Branch: "feat"})
	before := len(f.calls(t))

	if rows := ghRowText(m); !slices.ContainsFunc(rows, func(r string) bool { return r == "PR feat: streets by batch ids #12 @eve ⑂ ✕" }) {
		t.Errorf("no worktree mark in %q", rows)
	}

	ghSelect(t, m, "i:All Open#12")
	press(m, "w")

	if m.ws != wt || len(f.calls(t)) != before {
		t.Fatalf("w went to %s, gh calls %q", m.ws, f.calls(t)[before:])
	}

	m.switchWorkspace(m.wss[0].Path)
	m.focus = m.colOf(viewGitHub)
	ghSelect(t, m, "i:My Issues#88")
	press(m, "w")

	if m.modal == nil || m.modal.input.Value() != "issue/88-search-ignores-gitignore" {
		t.Fatalf("prompt %+v", m.modal)
	}
}

func TestGitHubMergeAndNotifications(t *testing.T) {
	m, f, load := ghModel(t)
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#14")

	i := slices.IndexFunc(m.gh.items(m), func(it item) bool { return it.label == "Merge Pull Request…" })
	m.gh.items(m)[i].run(m)

	if m.modal == nil || !strings.Contains(m.modal.title, "into main") || len(m.modal.items) != 4 {
		t.Fatalf("merge dialog %+v", m.modal)
	}

	squash := slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Squash and Merge" })
	for _, msg := range runAll(m.modal.items[squash].run(m)) {
		if done, ok := msg.(ghDoneMsg); !ok || done.err != nil || done.text != "merged #14" {
			t.Errorf("merge answered %#v", msg)
		}
	}

	if !slices.Contains(f.calls(t), "pr merge 14 --squash --match-head-commit 5299565") {
		t.Errorf("gh calls %q", f.calls(t))
	}

	m.modal = nil
	ghSelect(t, m, "n:101")

	_, cmd := m.Update(keyMsg("x"))
	ghRun(t, m, cmd)

	if slices.ContainsFunc(m.gh.rows(m), func(r ghRow) bool { return r.kind == ghrNote }) || !slices.Contains(f.calls(t), "api -X DELETE notifications/threads/101") {
		t.Errorf("after Mark as Done: rows %q, calls %q", ghRowText(m), f.calls(t))
	}
}

// TestGHTerminal opens the panel on a shell of its own for what gh asks, not
// on the one ensureTerm would start: that would make two.
func TestGHTerminal(t *testing.T) {
	m := testModel(t)

	if m.termOpen() {
		t.Fatal("the panel starts open")
	}

	if cmd := m.ghTerminal("gh auth login"); cmd == nil || !m.termOpen() || m.focus != onPanel || m.tv.id != "" {
		t.Errorf("panel open %v, focus %d, attached %q", m.termOpen(), m.focus, m.tv.id)
	}
}

// TestGitHubOffScreen: a project switched to while the view is out of sight
// drops the lists it had, and the view fetches its own once it is back on
// screen, whichever way it came (here ^b, which is no showView).
func TestGitHubOffScreen(t *testing.T) {
	m, f, load := ghModel(t)
	ghRun(t, m, load)

	side := m.side(m.colOf(viewGitHub))
	m.hidden[side] = true

	other := t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Branch: "main", Main: true})
	m.switchWorkspace(other)

	if m.gh.project != other || slices.ContainsFunc(m.gh.rows(m), func(r ghRow) bool { return r.item != nil }) {
		t.Fatalf("after the switch: project %s, rows %q", m.gh.project, ghRowText(m))
	}

	before := len(f.calls(t))
	m.hidden[side] = false
	m.Update(tickMsg{}) // its commands would reach the daemon: only what it asked for is checked

	if r := m.gh.res["open"]; r == nil || !r.busy {
		t.Fatalf("the tick did not fetch for the view back on screen: %+v", m.gh.res)
	}

	if len(f.calls(t)) != before {
		t.Error("gh ran on the UI loop")
	}
}

// TestGitHubPanes: a folded section is pinned at the bottom, the open ones
// share the rest, and dragging an open one's header moves its edge with the
// one above, as VS Code's panes and Source Control's drawers do.
func TestGitHubPanes(t *testing.T) {
	m, _, load := ghModel(t)
	ghRun(t, m, load)

	h := m.bodyH(viewGitHub)
	heads := func() (out []string) {
		for _, p := range m.gh.panes(m.gh.rows(m), h) {
			out = append(out, ghSections[p.sec])
		}

		return out
	}
	pane := func(sec int) ghPane {
		for _, p := range m.gh.panes(m.gh.rows(m), h) {
			if p.sec == sec {
				return p
			}
		}

		t.Fatalf("no pane %s", ghSections[sec])

		return ghPane{}
	}

	used := 0
	for _, p := range m.gh.panes(m.gh.rows(m), h) {
		used += 1 + p.h
	}

	if used != h {
		t.Errorf("the panes fill %d of %d rows", used, h)
	}

	ghSelect(t, m, ghSecKey(ghIssues))
	press(m, "enter")

	if got := heads(); !slices.Equal(got, []string{"Pull Requests", "Notifications", "Issues"}) || m.gh.selKey(m) != ghSecKey(ghIssues) {
		t.Fatalf("a folded section is not pinned at the bottom: %v, selection %s", got, m.gh.selKey(m))
	}

	checkWidths(t, m)

	// Drag the Notifications header up two rows: Pull Requests above it
	// gives them up, Notifications (the last) takes them.
	x, top := m.colRect(m.colOf(viewGitHub)).x+2, m.bodyTop(viewGitHub)
	prs, notes := pane(ghPRs), pane(ghNotes)
	y := top + notes.head

	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y - 2, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y - 2, Button: tea.MouseLeft})

	if p, n := pane(ghPRs), pane(ghNotes); p.h != prs.h-2 || n.h != notes.h+2 || !m.gh.open[ghSecKey(ghNotes)] {
		t.Errorf("after the drag: pull requests %d→%d, notifications %d→%d", prs.h, p.h, notes.h, n.h)
	}

	// A press that does not move folds it: down among the pinned ones.
	y = top + pane(ghNotes).head
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if got := heads(); !slices.Equal(got, []string{"Pull Requests", "Issues", "Notifications"}) || pane(ghPRs).h != h-3 {
		t.Errorf("after a click: %v, pull requests %d rows of %d", got, pane(ghPRs).h, h)
	}

	checkWidths(t, m)
}

// TestGitHubCheckoutOnce: a second w while a checkout makes its worktree
// would make another one, whose checkout then fails on the taken branch.
func TestGitHubCheckoutOnce(t *testing.T) {
	m, _, load := ghModel(t)
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#13")

	_, first := m.Update(keyMsg("w"))
	_, second := m.Update(keyMsg("w"))

	if first == nil || !m.gh.checking["pr/13"] {
		t.Fatal("w started no checkout")
	}

	if msg, ok := second().(flashMsg); !ok || !strings.Contains(msg.text, "being checked out") {
		t.Errorf("a second w: %#v", msg)
	}

	m.Update(ghCheckoutMsg{branch: "pr/13", err: errors.New("workspace.new: no such project")})

	if m.gh.checking["pr/13"] || !strings.Contains(m.msg, "no such project") {
		t.Errorf("after a failed checkout: checking %v, flash %q", m.gh.checking, m.msg)
	}
}
