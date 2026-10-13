package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// fakeGH is a gh on PATH that answers from the files in dir, "pr-list" for
// gh pr list, "notes" for the notifications, "graphql" for a conversation, and fails with the text of
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
api) [ "$2" = -X ] && exit 0; if [ "$2" = graphql ]; then f=graphql; else f=notes; fi ;;
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
	m.gh.open[ghSecKey(ghIssues)], m.gh.open[ghSecKey(ghNotes)] = true, true // folded by default

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

// TestGitHubOpensPullRequestsOnly: Issues and Notifications start folded, so
// gh is asked for the pull requests alone.
func TestGitHubOpensPullRequestsOnly(t *testing.T) {
	f := newFakeGH(t)
	f.answer(t, "pr-list", ghPRList)

	m := testModelSized(t, 100, 40)
	m.hasGH = true
	ghRun(t, m, m.showView(viewGitHub))

	if calls := f.calls(t); len(calls) != 1 || !strings.HasPrefix(calls[0], "pr list ") {
		t.Errorf("gh calls %q", calls)
	}

	for _, s := range []int{ghIssues, ghNotes} {
		if m.gh.open[ghSecKey(s)] {
			t.Errorf("%s starts unfolded", ghSections[s])
		}
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
	d.Timeline = []ghEntry{{kind: "comment", author: "bob", text: "looks good"}, {kind: "commit", author: "eve", oid: "abc1234", text: "fix: typo"}}

	// The header has the title and the state, which the page does not say again.
	md := ghMarkdown("pr", d)
	for _, want := range []string{
		"**eve** · feat → main", "Adds batch lookup.", "## Checks\n\n- ✓ test\n- ○ ci/jenkins\n", "comment bob", "looks good", "commit eve abc1234", "https://github.com/o/r/pull/12",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("no %q in\n%s", want, md)
		}
	}

	for _, not := range []string{"# feat: streets", "wants to merge", "checks pending"} {
		if strings.Contains(md, not) {
			t.Errorf("%q repeats the header in\n%s", not, md)
		}
	}

	if md := ghMarkdown("issue", ghDoc{State: "OPEN"}); !strings.Contains(md, "_No description provided._") || strings.Contains(md, "→") {
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

	if name, ctx := m.pv.label(m.ws); m.pv.kind != pvGH || name != "PR #14" || ctx != "release v0.4.0" {
		t.Fatalf("editor %q %q (%s)", name, ctx, m.pv.kind)
	}

	if text := ansi.Strip(strings.Join(m.pv.lines, "\n")); !strings.Contains(text, "The release.") || strings.Contains(text, "#14") {
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

	// Only a header with an open pane above it is a sash.
	m.Update(tea.MouseMotionMsg{X: x, Y: top + prs.head})

	if m.sashAt != noSash || strings.Contains(m.gh.lines(m, 40, h)[prs.head], "⇕") {
		t.Errorf("the first header is a sash: %v", m.sashAt)
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	m.sashSince = time.Now().Add(-sashDelay)

	if m.sashAt != paneSash(viewGitHub, "Notifications") || !strings.Contains(ansi.Strip(m.gh.lines(m, 40, h)[notes.head]), "━") || !strings.HasSuffix(strings.TrimRight(ansi.Strip(m.gh.lines(m, 40, h)[notes.head]), " "), "⇕") {
		t.Errorf("the Notifications header is a sash, lit once rested on: %v", m.sashAt)
	}

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

// ghPage opens pull request 14 in the editor, answering as gh view with the
// JSON of doc, and returns the model and the gh log.
func ghPage(t *testing.T, doc string) (*Model, fakeGH) {
	t.Helper()

	m, f, load := ghModel(t)
	f.answer(t, "pr-view", doc)
	f.answer(t, "graphql", ghConversation(strings.Repeat("a", 40)))
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#14")

	_, cmd := m.Update(keyMsg("enter"))
	ghRun(t, m, cmd)

	if m.pv.kind != pvGH || m.pv.gh == nil {
		t.Fatalf("no page: kind %s, doc %v", m.pv.kind, m.pv.gh)
	}

	// The icons of a terminal with the font: one cell a button, as the header's room is made for.
	applyLook("vscode", true, "nerd", nil)
	t.Cleanup(func() { applyLook("vscode", true, "ascii", nil) })

	m.focus = onMain

	return m, f
}

// ghSettle runs cmd and what the answers ask for next, once: an action's
// result, then the fetches it started.
func ghSettle(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()

	for _, msg := range runAll(cmd) {
		_, next := m.Update(msg)
		ghRun(t, m, next)
	}
}

const ghOpenPR = `{"number":14,"title":"release v0.4.0","author":{"login":"ann"},"body":"The release.","state":"OPEN",
"headRefName":"release","baseRefName":"main","url":"https://github.com/o/r/pull/14","isDraft":false,
"reviewRequests":[{"login":"dodkes"}],"latestReviews":[{"author":{"login":"eve"},"state":"APPROVED"}],
"assignees":[{"login":"ann"}],"labels":[{"name":"bug"}],"reviewDecision":"REVIEW_REQUIRED",
"statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]}`

// ghHeaderText is the header row of the page as plain text.
func ghHeaderText(m *Model) string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")[m.stripH()]
}

// ghButton is the header button whose tip starts with tip.
func ghButton(t *testing.T, m *Model, tip string) rowAction {
	t.Helper()

	_, btns, _ := m.pv.ghHeader(m, m.mainW())
	for _, b := range btns {
		if strings.HasPrefix(b.tip, tip) {
			return b
		}
	}

	t.Fatalf("no %q button in %+v", tip, btns)

	return rowAction{}
}

// TestGitHubPageHeader: a pull request's page has its state and buttons at
// the right end of the header row, no rows of its own; a button is a click,
// a key and a menu entry, and what the state forbids is not offered.
func TestGitHubPageHeader(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	if m.headH() != 1 {
		t.Fatalf("header %d rows", m.headH())
	}

	head := ghHeaderText(m)
	for _, want := range []string{"PR #14", "release v0.4.0", "● Open", "✓ 1/1", "Merge", "▾"} {
		if !strings.Contains(head, want) {
			t.Errorf("no %q in the header\n%s", want, head)
		}
	}

	if text := ansi.Strip(strings.Join(m.pv.lines, "\n")); !strings.Contains(text, "ann · release → main") || !strings.Contains(text, "● dodkes") || !strings.Contains(text, "✓ eve") {
		t.Errorf("no author, branches and reviewers in\n%s", text)
	}

	checkWidths(t, m)

	// A click on the close button asks first; gh is called once it is confirmed, and the page is fetched again.
	before := len(f.calls(t))
	b := ghButton(t, m, "Close Pull Request")
	_, cmd := m.Update(tea.MouseClickMsg{X: m.mainX() + b.x, Y: m.stripH(), Button: tea.MouseLeft})
	ghSettle(t, m, cmd)

	if m.modal == nil || m.modal.title != "Close pull request #14?" || len(f.calls(t)) != before {
		t.Fatalf("no confirmation: modal %v, calls %q", m.modal != nil, f.calls(t)[before:])
	}

	ghSettle(t, m, m.modal.items[0].run(m))
	m.modal = nil

	calls := f.calls(t)[before:]
	if !slices.Contains(calls, "pr close 14") || !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "pr view 14 ") }) {
		t.Errorf("gh calls %q", calls)
	}

	// Resting on a button names it and its key in a tip by the pointer, as
	// every other button does.
	m.Update(tea.MouseMotionMsg{X: m.mainX() + b.x, Y: m.stripH()})
	elapse(m, tipDelay)

	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "│ Close Pull Request  x │") {
		t.Errorf("no tip by the pointer:\n%s", out)
	}

	m.mouseX, m.mouseY = 0, 0

	// M asks how to merge, as the list's menu does.
	press(m, "M")

	if m.modal == nil || !strings.Contains(m.modal.title, "Merge pull request #14 into main") || len(m.modal.items) != 4 {
		t.Fatalf("merge dialog %v", m.modal != nil)
	}

	m.modal = nil

	// A closed one is reopened, and cannot be merged.
	m.pv.gh.State = "CLOSED"

	head = ghHeaderText(m)
	if !strings.Contains(head, "● Closed") || !strings.Contains(head, "Reopen") || strings.Contains(head, "Merge") {
		t.Errorf("closed pull request\n%s", head)
	}

	m.pv.gh.State, m.pv.gh.IsDraft = "OPEN", true

	if head = ghHeaderText(m); !strings.Contains(head, "● Draft") || !strings.Contains(head, "Ready") || strings.Contains(head, "Merge") {
		t.Errorf("draft pull request\n%s", head)
	}
}

// ghLead is the tips of the header's buttons in order.
func ghLead(m *Model) []string {
	_, btns, _ := m.pv.ghHeader(m, m.mainW())

	out := make([]string, len(btns))
	for i, b := range btns {
		out[i] = strings.Fields(b.tip)[0]
	}

	return out
}

// TestGitHubPagePrimary: one button leads the header, by the page's state,
// and a leading one of the first group has the ▾ with that group's menu.
func TestGitHubPagePrimary(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	lead := func(want string, more bool) {
		t.Helper()

		got := ghLead(m)
		if got[0] != want {
			t.Errorf("leads with %q, want %q: %q", got[0], want, got)
		}

		if (got[1] == "More") != more {
			t.Errorf("%s: ▾ %v, want %v", want, got[1] == "More", more)
		}
	}

	lead("Merge", true)

	m.pv.gh.Rollup[0].Conclusion = "FAILURE" // a failing check: the worktree leads, merge follows the changes

	lead("Checkout", true)

	if got := ghLead(m); !slices.Equal(got[:4], []string{"Checkout", "More", "Open", "Merge"}) {
		t.Errorf("buttons %q", got)
	}

	m.pv.gh.IsDraft = true

	lead("Mark", true)

	m.pv.gh.IsDraft, m.pv.gh.State = false, "CLOSED"

	lead("Reopen", false)

	// The ▾ lists the first group: what to do with the code, not the comment.
	m.pv.gh.State = "OPEN"
	m.pv.gh.Rollup[0].Conclusion = "SUCCESS"

	ghButton(t, m, "More").run(m)

	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.label)
	}

	if want := []string{"Checkout in New Worktree", "Open Changes", "Open Changed Files", "Merge Pull Request…", "Convert to Draft"}; !slices.Equal(labels, want) {
		t.Errorf("the ▾ menu %q, want %q", labels, want)
	}
}

// TestGitHubPageEdits: reviewers, assignees, labels and the title are
// prompts that send only what changed.
func TestGitHubPageEdits(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	run := func(label, value string) {
		t.Helper()

		i := slices.IndexFunc(m.pv.ghActs(), func(a ghAct) bool { return a.label == label })
		if i < 0 {
			t.Fatalf("no %q in the page's commands", label)
		}

		m.pv.ghActs()[i].run(m)

		if m.modal == nil {
			t.Fatalf("%s asked nothing", label)
		}

		m.modal.input.SetValue(value)

		_, cmd := m.Update(keyMsg("enter"))
		ghRun(t, m, cmd)
	}

	run("Edit Labels…", "bug, docs")
	run("Edit Reviewers…", "eve, @ann")
	run("Edit Assignees…", "ann")
	run("Edit Title…", "release v0.5.0")

	calls := f.calls(t)
	for _, want := range []string{
		"pr edit 14 --add-label docs",
		"pr edit 14 --add-reviewer ann --remove-reviewer dodkes",
		"pr edit 14 --title release v0.5.0",
	} {
		if !slices.Contains(calls, want) {
			t.Errorf("no %q in %q", want, calls)
		}
	}

	if slices.Contains(calls, "pr edit 14 --add-assignee ann") {
		t.Errorf("assignees sent unchanged: %q", calls)
	}
}

// TestGitHubComment: c puts the cursor in the box at the end of the page, ^s
// sends what is in it; a refusal keeps the text.
func TestGitHubComment(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	page := ghPlainPage(m)
	if !strings.Contains(page, "Leave a comment") || !strings.Contains(page, "Comment ") {
		t.Fatalf("no box at the end of the page:\n%s", page)
	}

	press(m, "c")

	if !m.pv.composing || m.pv.kind != pvGH {
		t.Fatalf("composing %v, kind %s", m.pv.composing, m.pv.kind)
	}

	press(m, "a", "enter", "b")

	if got := m.pv.compose.Value(); got != "a\nb" {
		t.Fatalf("typed %q", got)
	}

	if h := m.pv.hints(m); !strings.Contains(h, "^s send") {
		t.Errorf("hints %q", h)
	}

	if page = ghPlainPage(m); !strings.Contains(page, "│ a ") || !strings.Contains(page, "│ b ") {
		t.Errorf("the text is not in the box:\n%s", page)
	}

	f.answer(t, "fail", "HTTP 403: forbidden")

	_, cmd := m.Update(keyMsg("ctrl+s"))
	ghSettle(t, m, cmd)

	if m.pv.compose.Value() != "a\nb" || !m.pv.composing || !strings.Contains(m.msg, "forbidden") {
		t.Fatalf("after a refusal: %q, composing %v, flash %q", m.pv.compose.Value(), m.pv.composing, m.msg)
	}

	mustRemove(t, filepath.Join(f.dir, "fail"))

	_, cmd = m.Update(keyMsg("ctrl+s"))
	ghSettle(t, m, cmd)

	// The log has a call a line: the new line of the text breaks it in two.
	if !slices.Contains(f.calls(t), "pr comment 14 --body a") || !slices.Contains(f.calls(t), "b") {
		t.Errorf("gh calls %q", f.calls(t))
	}

	if m.pv.compose.Value() != "" || m.pv.composing || m.pv.kind != pvGH {
		t.Errorf("after sending: %q, composing %v, kind %s", m.pv.compose.Value(), m.pv.composing, m.pv.kind)
	}
}

// TestGitHubCommentBox: the box's right edge is one column on every row, its
// buttons' row too, and the terminal's cursor, not a drawn one, sits where the
// typing goes.
func TestGitHubCommentBox(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)
	press(m, "c", "x")

	page := strings.Split(ghPlainPage(m), "\n")

	top := slices.IndexFunc(page, func(l string) bool { return strings.Contains(l, "╭─ Leave a comment") })
	if top < 0 {
		t.Fatalf("no box:\n%s", strings.Join(page, "\n"))
	}

	var edges []int

	for _, l := range page[top:] {
		edges = append(edges, ansi.StringWidth(l[:strings.LastIndexAny(l, "╮│╯")]))
		if strings.Contains(l, "╯") {
			break
		}
	}

	if slices.Min(edges) != slices.Max(edges) {
		t.Errorf("the right edge wanders %v:\n%s", edges, strings.Join(page[top:top+len(edges)], "\n"))
	}

	v := m.View()
	if strings.Contains(v.Content, caretMark) || slices.ContainsFunc(strings.Split(v.Content, "\n"), func(l string) bool { return realCaret(l) != l }) {
		t.Fatalf("the drawn caret is left:\n%s", ansi.Strip(v.Content))
	}

	screen := strings.Split(ansi.Strip(v.Content), "\n")

	y := slices.IndexFunc(screen, func(l string) bool { return strings.Contains(l, "│ x") })
	if y < 0 {
		t.Fatalf("the box is not on screen:\n%s", strings.Join(screen, "\n"))
	}

	l, _, _ := strings.Cut(screen[y], "│ x")
	if x := ansi.StringWidth(l) + 3; v.Cursor == nil || v.Cursor.X != x || v.Cursor.Y != y {
		t.Fatalf("the cursor is after the x at %d,%d: %+v", x, y, v.Cursor)
	}
}

// TestGitHubCommentButtons: the buttons under the box ask before they close
// the pull request, with the comment in the box when there is one, and
// Comment sends it.
func TestGitHubCommentButtons(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	press(m, "c", "o", "k")
	press(m, "esc")

	if m.pv.composing || m.pv.compose.Value() != "ok" {
		t.Fatalf("esc: composing %v, text %q", m.pv.composing, m.pv.compose.Value())
	}

	click := func(label string) tea.Cmd {
		t.Helper()

		i := ghRowOf(t, m, "Comment ")
		row := string(m.pv.plain[i])

		at := strings.Index(row, label)
		if at < 0 {
			t.Fatalf("no %q in %q", label, row)
		}

		return ghClickRow(t, m, i, len([]rune(row[:at])))
	}

	before := len(f.calls(t))
	ghSettle(t, m, click("Close pull request with comment"))

	if m.modal == nil || m.modal.title != "Close pull request #14 and send your comment?" || len(f.calls(t)) != before {
		t.Fatalf("no confirmation: %v, calls %q", m.modal != nil, f.calls(t)[before:])
	}

	ghSettle(t, m, m.modal.items[0].run(m))
	m.modal = nil

	if !slices.Contains(f.calls(t), "pr close 14 --comment ok") {
		t.Errorf("gh calls %q", f.calls(t))
	}

	if m.pv.compose.Value() != "" {
		t.Errorf("the box keeps %q after it was sent", m.pv.compose.Value())
	}

	press(m, "c", "h", "i")
	press(m, "esc")
	ghSettle(t, m, click("Comment"))

	if !slices.Contains(f.calls(t), "pr comment 14 --body hi") {
		t.Errorf("gh calls %q", f.calls(t))
	}
}

// TestGitHubConfirms: what changes the page asks first, and a merge says when
// its checks are failing.
func TestGitHubConfirms(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	ask := func(label, title string) {
		t.Helper()

		before := len(f.calls(t))

		i := slices.IndexFunc(m.pv.ghActs(), func(a ghAct) bool { return a.label == label })
		if i < 0 {
			t.Fatalf("no %q", label)
		}

		m.pv.ghActs()[i].run(m)

		if m.modal == nil || !strings.HasPrefix(m.modal.title, title) || len(f.calls(t)) != before {
			t.Fatalf("%s: modal %v", label, m.modal != nil)
		}

		m.modal = nil
	}

	ask("Convert to Draft", "Convert pull request #14 to a draft?")
	ask("Merge Pull Request…", "Merge pull request #14 into main?")

	m.pv.gh.Rollup[0].Conclusion = "FAILURE"

	ask("Merge Pull Request…", "Merge pull request #14 into main? Its checks are failing.")

	m.pv.gh.IsDraft = true

	ask("Mark Ready for Review", "Mark pull request #14 ready for review?")

	m.pv.gh.IsDraft, m.pv.gh.State = false, "CLOSED"

	ask("Reopen Pull Request", "Reopen pull request #14?")
}

// ghConversation is gh api graphql's answer for a pull request with a
// commit, a review request, a comment, an approval with a thread on a file,
// and a review that only replied to it.
func ghConversation(oid string) string {
	return `{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[
{"__typename":"PullRequestCommit","commit":{"oid":"` + oid + `","messageHeadline":"refactor: lokalita davok","committedDate":"2026-10-08T10:00:00Z",
 "authors":{"nodes":[{"name":"Ann","user":{"login":"ann"}}]},"statusCheckRollup":{"state":"SUCCESS"},"signature":{"isValid":true}}},
{"__typename":"ReviewRequestedEvent","actor":{"login":"ann"},"createdAt":"2026-10-08T11:00:00Z","requestedReviewer":{"login":"dodkes"}},
{"__typename":"IssueComment","id":"IC_1","author":{"login":"bob"},"body":"Looks **good** to me","createdAt":"2026-10-08T12:00:00Z","url":"https://github.com/o/r/pull/14#issuecomment-1","lastEditedAt":null},
{"__typename":"PullRequestReview","id":"PRR_1","author":{"login":"dodkes"},"state":"APPROVED","body":"Schvalujem to","submittedAt":"2026-10-08T13:00:00Z","url":"https://github.com/o/r/pull/14#pullrequestreview-1",
 "comments":{"nodes":[{"id":"C_1","path":"a.go","line":12,"originalLine":12,"body":"why this?","createdAt":"2026-10-08T13:00:00Z","url":"u","author":{"login":"dodkes"},"replyTo":null}]}},
{"__typename":"PullRequestReview","id":"PRR_2","author":{"login":"ann"},"state":"COMMENTED","body":"","submittedAt":"2026-10-08T14:00:00Z","url":"u",
 "comments":{"nodes":[{"id":"C_2","path":"a.go","line":12,"originalLine":12,"body":"because it is faster","createdAt":"2026-10-08T14:00:00Z","url":"u","author":{"login":"ann"},"replyTo":{"id":"C_1"}}]}}
]}}}}}`
}

// TestGHEntries: the conversation is the commits, comments, reviews and events
// in order; a reply joins the thread it answers and a review of only replies
// has no row of its own.
func TestGHEntries(t *testing.T) {
	var res struct {
		Data struct {
			Repository map[string]struct {
				TimelineItems struct{ Nodes []ghNode }
			}
		}
	}
	if err := json.Unmarshal([]byte(ghConversation("abc1234567")), &res); err != nil {
		t.Fatal(err)
	}

	es := ghEntries(res.Data.Repository["pullRequest"].TimelineItems.Nodes)

	var kinds []string
	for _, e := range es {
		kinds = append(kinds, e.kind)
	}

	if want := []string{"commit", "event", "comment", "review"}; !slices.Equal(kinds, want) {
		t.Fatalf("kinds %q, want %q", kinds, want)
	}

	if c := es[0]; c.author != "ann" || !c.verified || c.state != "pass" || c.oid != "abc1234567" {
		t.Errorf("commit %+v", c)
	}

	if e := es[1]; e.author != "ann" || e.text != "requested a review from dodkes" {
		t.Errorf("event %+v", e)
	}

	r := es[3]
	if r.state != "APPROVED" || len(r.threads) != 1 || len(r.threads[0].comments) != 2 || r.threads[0].comments[1].text != "because it is faster" {
		t.Errorf("review %+v", r)
	}
}

// ghPlainPage is the page's rows as plain text.
func ghPlainPage(m *Model) string {
	m.pv.syncMarkdown(m)

	lines := make([]string, len(m.pv.plain))
	for i, l := range m.pv.plain {
		lines[i] = strings.TrimRight(string(l), " ")
	}

	return strings.Join(lines, "\n")
}

// ghRowOf is the index of the first row of the page with text in it.
func ghRowOf(t *testing.T, m *Model, text string) int {
	t.Helper()

	m.pv.syncMarkdown(m)

	for i, l := range m.pv.plain {
		if strings.Contains(string(l), text) {
			return i
		}
	}

	t.Fatalf("no row with %q in\n%s", text, ghPlainPage(m))

	return -1
}

// ghClickRow clicks the page at column col of its row i, which the test has
// scrolled to.
func ghClickRow(t *testing.T, m *Model, i, col int) tea.Cmd {
	t.Helper()

	y := m.stripH() + m.headH() + i - m.pv.top
	_, cmd := m.Update(tea.MouseClickMsg{X: m.mainX() + col, Y: y, Button: tea.MouseLeft})

	return cmd
}

// TestGitHubConversation: the page is GitHub's: a box with its author and
// time across the top for each comment, a rail with the commits, reviews and
// events on it, and what was said on a file under its review.
func TestGitHubConversation(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	page := ghPlainPage(m)
	for _, want := range []string{
		"●─╭─ ann commented", "╭─ bob commented", "Looks good to me", "◦ refactor: lokalita davok", "✓ Verified aaaaaaa",
		"· ann requested a review from dodkes", "✓ dodkes approved these changes", "left a comment", "Schvalujem to",
		"a.go:12", "dodkes", "why this?", "because it is faster", "The release.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q in\n%s", want, page)
		}
	}

	// The rail is one column: every row of the page starts with a blank, and the box under it is whole.
	for i, l := range m.pv.plain {
		if s := string(l); strings.ContainsAny(s, "╭╰") && !strings.HasPrefix(s, " ") {
			t.Errorf("row %d breaks the rail: %q", i, s)
		}
	}

	checkWidths(t, m)
}

// TestGitHubConversationClicks: a commit shows, a header folds its comment,
// the ⋯ lists what to do with it and quoting starts a reply.
func TestGitHubConversationClicks(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	// Fold bob's comment: its text goes, the header stays, and again brings it back.
	i := ghRowOf(t, m, "bob commented")
	ghClickRow(t, m, i, 5)

	page := ghPlainPage(m)
	if strings.Contains(page, "Looks good to me") || !strings.Contains(page, "▸ bob commented") {
		t.Fatalf("folded:\n%s", page)
	}

	ghClickRow(t, m, i, 5)

	if page = ghPlainPage(m); !strings.Contains(page, "Looks good to me") {
		t.Fatalf("unfolded:\n%s", page)
	}

	// The ⋯ at the end of the header.
	row := string(m.pv.plain[i])

	at := strings.Index(row, "⋯")
	if at < 0 {
		t.Fatalf("no ⋯ in %q", row)
	}

	ghClickRow(t, m, i, len([]rune(row[:at])))

	if m.modal == nil {
		t.Fatal("⋯ lists nothing")
	}

	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.label)
	}

	if want := []string{"Quote Reply", "Fold", "Open on GitHub", "Copy Link"}; !slices.Equal(labels, want) {
		t.Fatalf("menu %q, want %q", labels, want)
	}

	m.modal.items[0].run(m)
	m.modal = nil

	if !m.pv.composing || strings.TrimSpace(m.pv.compose.Value()) != "> Looks **good** to me" {
		t.Errorf("quote reply: composing %v, %q", m.pv.composing, m.pv.compose.Value())
	}
}

// TestGitHubCommitClick: a commit of the page that the repository has is shown;
// one it has not is asked for first.
func TestGitHubCommitClick(t *testing.T) {
	m, f, load := ghModel(t)
	mustGit(t, m.ws, "init", "-q")
	mustGit(t, m.ws, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", "first")

	head, err := git.Run(m.ws, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	f.answer(t, "pr-view", ghOpenPR)
	f.answer(t, "graphql", ghConversation(strings.TrimSpace(head)))
	ghRun(t, m, load)
	ghSelect(t, m, "i:All Open#14")

	_, cmd := m.Update(keyMsg("enter"))
	ghRun(t, m, cmd)

	i := ghRowOf(t, m, "◦ refactor")
	cmd = ghClickRow(t, m, i, 8)

	var shown ghShownMsg

	for _, msg := range runAll(cmd) {
		if s, ok := msg.(ghShownMsg); ok {
			shown = s
		}
	}

	if shown.err != nil || shown.oid != strings.TrimSpace(head) {
		t.Fatalf("shown %+v", shown)
	}

	m.Update(shown)

	if m.pv.kind != pvShow || m.pv.rev != shown.oid {
		t.Errorf("after the click: %s %s", m.pv.kind, m.pv.rev)
	}
}

// TestGitHubCommitHover: the pointer on a commit's row tints the row, which a
// click shows, title to hash, and only while it is there.
func TestGitHubCommitHover(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	y := m.stripH() + m.headH() + ghRowOf(t, m, "◦ refactor") - m.pv.top
	bgOf := func(text string) string {
		l := strings.Split(m.View().Content, "\n")[y]

		x := strings.Index(ansi.Strip(l), text)
		if x < 0 {
			t.Fatalf("no %q on row %d: %q", text, y, ansi.Strip(l))
		}

		buf := uv.NewScreenBuffer(ansi.StringWidth(l), 1)
		uv.NewStyledString(l).Draw(buf, buf.Bounds())

		return fmt.Sprint(buf.CellAt(ansi.StringWidth(ansi.Strip(l)[:x]), 0).Style.Bg)
	}
	hashBg := func() string { return bgOf("aaaaaaa") }

	lit := fmt.Sprint(pal.hoverBg)
	if bg := hashBg(); bg == lit {
		t.Fatalf("lit before the pointer came: %s", bg)
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 8, Y: y})

	if bg, title := hashBg(), bgOf("refactor"); bg != lit || title != lit {
		t.Errorf("the row under the pointer has %s at its hash, %s at its title, want %s", bg, title, lit)
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 8, Y: y + 1})

	if bg := hashBg(); bg == lit {
		t.Errorf("still lit after the pointer left")
	}
}

// TestGitHubCommentBoxHover: the pointer on the comment box lights its frame
// in the accent, as the keyboard in it does: a click puts the cursor there.
func TestGitHubCommentBoxHover(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)
	m.pv.top = len(m.pv.rows(m.pvW())) // the end of the page, with the box
	m.View()

	screen := strings.Split(ansi.Strip(m.View().Content), "\n")

	y := slices.IndexFunc(screen, func(l string) bool { return strings.Contains(l, "╭─ Leave a comment") })
	if y < 0 {
		t.Fatalf("no box on screen:\n%s", strings.Join(screen, "\n"))
	}

	lit := func() bool {
		return strings.Contains(strings.Split(m.View().Content, "\n")[y], fgParams(pal.headerAccent))
	}
	if lit() {
		t.Fatalf("lit before the pointer came")
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 20, Y: y + 1})

	if !lit() {
		t.Errorf("the frame under the pointer is not lit")
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 20, Y: y - 2})

	if lit() {
		t.Errorf("still lit after the pointer left")
	}
}

// TestGitHubPageDetails: a comment typed and left outlives the page; the
// page's cursor is its tinted row, not the terminal's; a box's top lights its
// frame under the pointer, and its ⋯ as a button; a page from memory says how
// old it is.
func TestGitHubPageDetails(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	press(m, "c", "h", "i", "esc")
	m.dropEditor(m.edIdx)
	m.openGH("pr", 14)

	if got := m.pv.composer(m).Value(); got != "hi" {
		t.Errorf("the draft after the page closed: %q", got)
	}

	if c := m.View().Cursor; c != nil {
		t.Errorf("a terminal cursor on the page: %+v", c)
	}

	m.pv.top, m.pv.cur.line = 0, 0
	screen := strings.Split(ansi.Strip(m.View().Content), "\n")

	y := slices.IndexFunc(screen, func(l string) bool { return strings.Contains(l, "╭─ bob commented") })
	if y < 0 {
		t.Fatalf("no box on screen:\n%s", strings.Join(screen, "\n"))
	}

	row := func() string { return strings.Split(m.View().Content, "\n")[y] }
	if strings.Contains(row(), fgParams(pal.headerAccent)) {
		t.Fatalf("lit before the pointer came: %q", row())
	}

	m.Update(tea.MouseMotionMsg{X: m.mainX() + 10, Y: y})

	if !strings.Contains(row(), fgParams(pal.headerAccent)) {
		t.Errorf("the top under the pointer is not lit: %q", row())
	}

	left, _, _ := strings.Cut(screen[y], "⋯")
	m.Update(tea.MouseMotionMsg{X: ansi.StringWidth(left), Y: y})

	if !strings.Contains(row(), keycapHot().Render(" ⋯ ")) {
		t.Errorf("the ⋯ under the pointer is not a button: %q", row())
	}

	c := m.ghPages[m.pv.id()]
	c.at = time.Now().Add(-3 * time.Minute)
	m.ghPages[m.pv.id()] = c

	if head := ansi.Strip(strings.Split(m.View().Content, "\n")[m.stripH()]); !strings.Contains(head, "3 minutes ago") {
		t.Errorf("the header does not say how old the page is: %q", head)
	}
}

// TestGitHubSidebar: a wide panel has the reviewers, assignees, labels and
// checks in a column beside the conversation, with a gear that edits; a
// narrow one has them in rows above it.
func TestGitHubSidebar(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)

	if page := ghPlainPage(m); !strings.Contains(page, "Reviewers  ") || strings.Contains(page, "│ Reviewers") {
		t.Fatalf("narrow:\n%s", page)
	}

	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	i := ghRowOf(t, m, "│ Reviewers")
	row := string(m.pv.plain[i])

	for _, want := range []string{"Reviewers", "● dodkes", "✓ eve"} {
		if !strings.Contains(ghPlainPage(m), want) {
			t.Errorf("no %q beside the conversation", want)
		}
	}

	if !strings.Contains(ghPlainPage(m), "Assignees") || !strings.Contains(ghPlainPage(m), "Labels") || !strings.Contains(ghPlainPage(m), "✓ test") {
		t.Errorf("sidebar:\n%s", ghPlainPage(m))
	}

	checkWidths(t, m)

	// The gear on the Reviewers heading edits them.
	col := len([]rune(row)) - 1
	cmd := ghClickRow(t, m, i, col)

	if cmd != nil {
		ghRun(t, m, cmd)
	}

	if m.modal == nil || !strings.Contains(m.modal.title, "Reviewers") {
		t.Errorf("the gear: modal %+v", m.modal)
	}
}

// TestGitHubPageCached: a page seen before shows at once, from memory; gh is
// asked for the news only when it is older than ghTTL or an action came
// since. A list older than its TTL is fetched again behind itself, its rows
// staying up.
func TestGitHubPageCached(t *testing.T) {
	m, f := ghPage(t, ghOpenPR)

	m.dropEditor(m.edIdx)

	before := len(f.calls(t))

	m.openGH("pr", 14)

	if !m.pv.ready || m.pv.gh == nil || len(f.calls(t)) != before {
		t.Fatalf("not shown at once: ready %v, doc %v, calls %q", m.pv.ready, m.pv.gh != nil, f.calls(t)[before:])
	}

	r := m.gh.res["open"]
	r.at = time.Now().Add(-2 * ghTTL)

	if cmd := m.gh.ensure(m); cmd == nil || !r.busy || len(r.items) == 0 {
		t.Errorf("a stale list: cmd %v, busy %v, %d items", cmd != nil, r.busy, len(r.items))
	}

	id := m.pv.id()

	m.pv.ready = false
	if cmd := m.pv.load(m); cmd != nil || !m.pv.ready {
		t.Errorf("a page fetched a moment ago: fetched %v, ready %v", cmd != nil, m.pv.ready)
	}

	c := m.ghPages[id]
	c.at = time.Now().Add(-2 * ghTTL)
	m.ghPages[id] = c

	m.pv.ready = false
	if cmd := m.pv.load(m); cmd == nil || !m.pv.ready {
		t.Errorf("an old page: fetched %v, ready %v", cmd != nil, m.pv.ready)
	}

	m.ghPages[id] = ghCached{raw: c.raw, doc: c.doc, at: time.Now()}
	m.gh.onDone(m, ghDoneMsg{text: "done"})

	if !m.ghPages[id].at.IsZero() {
		t.Errorf("a page trusted after an action")
	}
}

// TestGitHubMentions: every box has the same frame, but a comment that tags
// the reader has another, and says so.
func TestGitHubMentions(t *testing.T) {
	m, _ := ghPage(t, ghOpenPR)
	m.pv.gh.Timeline = append(m.pv.gh.Timeline,
		ghEntry{kind: "comment", id: "IC_9", author: "eve", text: "cc @Bob, thanks"},
		ghEntry{kind: "comment", id: "IC_10", author: "eve", text: "cc @bobby and mail@bob.example"})

	frame := func(text string) string {
		t.Helper()

		m.pv.mdLines, m.pv.mdShown = nil, 0
		m.pv.syncMarkdown(m)

		for i, l := range m.pv.plain {
			if strings.Contains(string(l), text) {
				return m.pv.lines[i]
			}
		}

		t.Fatalf("no row with %q in\n%s", text, ghPlainPage(m))

		return ""
	}

	m.Update(ghUserMsg("bob"))

	if l := frame("eve commented"); !strings.Contains(l, fgParams(pal.warn)) || !strings.Contains(l, "mentions you") {
		t.Errorf("the comment that tags bob: %q", ansi.Strip(l))
	}

	// The description, bob's own comment and the one that names bobby keep the frame of the rest.
	for _, text := range []string{"ann commented", "bob commented"} {
		if l := frame(text); strings.Contains(l, fgParams(pal.warn)) || !strings.Contains(l, fgParams(pal.sashHover)) {
			t.Errorf("%s: %q", text, ansi.Strip(l))
		}
	}

	if page := ghPlainPage(m); strings.Count(page, "mentions you") != 1 {
		t.Errorf("mentions marked more than once:\n%s", page)
	}
}
