package git

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

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

// GH runs gh in dir, which picks the repository from dir's remotes, with
// prompts off; the error is an *Error whose text is gh's stderr.
func GH(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_SPINNER_DISABLED=1")

	var out, errb bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), newError(args, errb.String(), ctx.Err(), err)
	}

	return out.String(), nil
}

// ghJSON decodes what gh prints into v.
func ghJSON(dir string, v any, args ...string) error {
	out, err := GH(dir, args...)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(out), v)
}

// Item is a pull request or an issue as gh lists it.
type Item struct {
	Number            int
	Title             string
	Author            struct{ Login string }
	URL               string
	IsDraft           bool    // pull requests only
	HeadRefName       string  // pull requests only
	BaseRefName       string  // pull requests only
	HeadRefOid        string  // pull requests only: a merge must still find this head
	IsCrossRepository bool    // pull requests only: the head is in a fork
	Rollup            []Check `json:"statusCheckRollup"`
}

// Check is one entry of a status check rollup: a CheckRun has a Name, a
// Status and a Conclusion, a StatusContext a Context and a State.
type Check struct {
	Typename                  string `json:"__typename"`
	Name, Context             string
	Status, Conclusion, State string
}

const (
	prFields    = "number,title,author,url,isDraft,headRefName,baseRefName,headRefOid,isCrossRepository,statusCheckRollup"
	issueFields = "number,title,author,url"
)

// PullRequests lists open pull requests, newest first; args narrow them
// (`--author @me`, `--search review-requested:@me`).
func PullRequests(dir string, limit int, args ...string) ([]Item, error) {
	var items []Item

	err := ghJSON(dir, &items, append([]string{"pr", "list", "--limit", strconv.Itoa(limit), "--json", prFields}, args...)...)

	return items, err
}

// Issues lists open issues; args narrow them as for PullRequests.
func Issues(dir string, args ...string) ([]Item, error) {
	var items []Item

	err := ghJSON(dir, &items, append([]string{"issue", "list", "--json", issueFields}, args...)...)

	return items, err
}

// Checks sums up a pull request's status checks as VS Code's list shows
// them: "fail" when any failed, else "pending" while any runs, else "pass";
// "" when it has none.
func (i Item) Checks() string {
	if len(i.Rollup) == 0 {
		return ""
	}

	state := "pass"

	for _, c := range i.Rollup {
		if c.Typename == "StatusContext" {
			switch c.State {
			case "FAILURE", "ERROR":
				return "fail"
			case "PENDING", "EXPECTED":
				state = "pending"
			}

			continue
		}

		switch { // a CheckRun
		case c.Status != "COMPLETED":
			state = "pending"
		case c.Conclusion == "FAILURE", c.Conclusion == "TIMED_OUT", c.Conclusion == "CANCELLED",
			c.Conclusion == "ACTION_REQUIRED", c.Conclusion == "STARTUP_FAILURE":
			return "fail"
		}
	}

	return state
}

// Note is a notification thread of the repository.
type Note struct {
	ID, Title, Reason string
	Type              string // PullRequest, Issue, Release, …
	Repo              string // the repository's web page
	Number            int    // the pull request's or the issue's; 0 for other types
	Unread            bool
	Updated           time.Time
}

// Notifications lists the repository's unread notification threads, as
// GitHub's inbox does.
func Notifications(dir string) ([]Note, error) {
	var raw []struct {
		ID      string
		Unread  bool
		Reason  string
		Updated time.Time `json:"updated_at"`
		Subject struct{ Title, URL, Type string }
		Repo    struct {
			URL string `json:"html_url"`
		} `json:"repository"`
	}
	if err := ghJSON(dir, &raw, "api", "repos/{owner}/{repo}/notifications"); err != nil {
		return nil, err
	}

	notes := make([]Note, len(raw))
	for i, r := range raw {
		n := Note{ID: r.ID, Title: r.Subject.Title, Reason: r.Reason, Type: r.Subject.Type, Repo: r.Repo.URL, Unread: r.Unread, Updated: r.Updated}
		if n.Type == "PullRequest" || n.Type == "Issue" {
			n.Number, _ = strconv.Atoi(path.Base(r.Subject.URL)) // the API's …/pulls/258
		}

		notes[i] = n
	}

	return notes, nil
}

// URL is the notification's page: its pull request or issue, else the
// repository.
func (n Note) URL() string {
	switch {
	case n.Number == 0:
		return n.Repo
	case n.Type == "PullRequest":
		return n.Repo + "/pull/" + strconv.Itoa(n.Number)
	}

	return n.Repo + "/issues/" + strconv.Itoa(n.Number)
}

// CheckoutPR checks out pull request n in the worktree dir as branch, with
// gh, which sets up where the branch pulls from. It never passes --force,
// which resets a local branch that already exists: such a branch only
// fast-forwards, and gh refuses when it has diverged. temp, when set, is the
// throwaway branch dir was created on, deleted once branch took its place.
func CheckoutPR(dir string, n int, branch, temp string) error {
	if _, err := GH(dir, "pr", "checkout", strconv.Itoa(n), "--branch", branch); err != nil {
		return err
	}

	if temp == "" {
		return nil
	}

	_, err := Run(dir, "branch", "-D", temp)

	return err
}
