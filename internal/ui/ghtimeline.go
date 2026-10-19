package ui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xseman/pando/internal/git"
)

// The conversation of a pull request or an issue, as GitHub's page lists it:
// commits, comments, reviews with their threads on the code, and the events
// between them, in the order they happened. gh pr view has no events, so one
// GraphQL call (gh api graphql) fetches all of it.

// ghEntry is one item of the conversation.
type ghEntry struct {
	kind     string // commit, comment, review or event
	id       string // what a fold is kept by
	author   string
	when     time.Time
	text     string // a comment's or review's body, an event's sentence, a commit's headline
	state    string // a review's: APPROVED, CHANGES_REQUESTED, COMMENTED; a commit's checks: pass, fail, pending
	oid      string // a commit's
	url      string
	verified bool
	edited   bool
	threads  []ghThread // a review's comments on the code
}

// ghThread is a conversation on a line of a file: its first comment and the
// replies.
type ghThread struct {
	path     string
	line     int
	comments []ghEntry
}

// ghEventFields are the timeline items an issue has as well as a pull request.
// A fragment on a type the union does not hold is an error, so the pull
// request's own (commits, reviews) are in its query alone.
const ghEventFields = `
...on IssueComment{id author{login} body createdAt url lastEditedAt}
...on AssignedEvent{actor{login} createdAt assignee{...on User{login}}}
...on LabeledEvent{actor{login} createdAt label{name}}
...on ClosedEvent{actor{login} createdAt}
...on ReopenedEvent{actor{login} createdAt}
...on RenamedTitleEvent{actor{login} createdAt previousTitle currentTitle}`

const ghPRQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){pullRequest(number:$n){timelineItems(first:100){nodes{__typename` + ghEventFields + `
...on PullRequestCommit{commit{oid messageHeadline committedDate authors(first:1){nodes{name user{login}}} statusCheckRollup{state} signature{isValid}}}
...on PullRequestReview{id author{login} state body submittedAt url comments(first:50){nodes{id path line originalLine body createdAt url author{login} replyTo{id}}}}
...on ReviewRequestedEvent{actor{login} createdAt requestedReviewer{...on User{login} ...on Team{slug}}}
...on ReadyForReviewEvent{actor{login} createdAt}
...on ConvertToDraftEvent{actor{login} createdAt}
...on MergedEvent{actor{login} createdAt}
...on HeadRefForcePushedEvent{actor{login} createdAt}
}}}}}`

const ghIssueQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){issue(number:$n){timelineItems(first:100){nodes{__typename` + ghEventFields + `
}}}}}`

type ghLogin struct{ Login string }

// ghNode is a timeline item as GraphQL answers: every type's fields, the ones
// it has set.
type ghNode struct {
	Typename      string `json:"__typename"`
	ID            string
	Author        *ghLogin
	Actor         *ghLogin
	Body, State   string
	URL           string
	CreatedAt     time.Time
	SubmittedAt   time.Time
	LastEditedAt  *time.Time
	PreviousTitle string
	CurrentTitle  string
	Assignee      *ghLogin
	Label         *struct{ Name string }
	Requested     *struct{ Login, Slug string } `json:"requestedReviewer"`
	Commit        *struct {
		Oid, MessageHeadline string
		CommittedDate        time.Time
		Authors              struct {
			Nodes []struct {
				Name string
				User *ghLogin
			}
		}
		StatusCheckRollup *struct{ State string }
		Signature         *struct{ IsValid bool }
	}
	Comments struct {
		Nodes []struct {
			ID, Path, Body, URL string
			Line, OriginalLine  int
			CreatedAt           time.Time
			Author              *ghLogin
			ReplyTo             *struct{ ID string }
		}
	}
}

// ghTimeline fetches the conversation of pull request or issue n.
func ghTimeline(dir, kind, n string) ([]ghEntry, error) {
	query, field := ghPRQuery, "pullRequest"
	if kind == "issue" {
		query, field = ghIssueQuery, "issue"
	}

	out, err := git.GH(dir, "api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-F", "n="+n, "-f", "query="+query)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			Repository map[string]struct {
				TimelineItems struct{ Nodes []ghNode }
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, err
	}

	return ghEntries(res.Data.Repository[field].TimelineItems.Nodes), nil
}

func (l *ghLogin) name() string {
	if l == nil {
		return "ghost" // GitHub's name for a user that is gone
	}

	return l.Login
}

// ghEntries turns the nodes into entries; replies to a review's comment on the
// code join its thread, wherever the reply's review stands.
func ghEntries(nodes []ghNode) []ghEntry {
	var out []ghEntry

	threads := map[string]*ghThread{} // by the first comment's id, pointing into the review entries

	for _, n := range nodes {
		switch n.Typename {
		case "PullRequestCommit":
			c := n.Commit
			e := ghEntry{kind: "commit", id: c.Oid, oid: c.Oid, text: c.MessageHeadline, when: c.CommittedDate, verified: c.Signature != nil && c.Signature.IsValid}

			if len(c.Authors.Nodes) > 0 {
				a := c.Authors.Nodes[0]
				e.author = a.Name

				if a.User != nil {
					e.author = a.User.Login
				}
			}

			if c.StatusCheckRollup != nil {
				e.state = map[string]string{"SUCCESS": "pass", "FAILURE": "fail", "ERROR": "fail", "PENDING": "pending", "EXPECTED": "pending"}[c.StatusCheckRollup.State]
			}

			out = append(out, e)

		case "IssueComment":
			out = append(out, ghEntry{kind: "comment", id: n.ID, author: n.Author.name(), when: n.CreatedAt, text: n.Body, url: n.URL, edited: n.LastEditedAt != nil})

		case "PullRequestReview":
			e := ghEntry{kind: "review", id: n.ID, author: n.Author.name(), when: n.SubmittedAt, text: n.Body, state: n.State, url: n.URL}

			for _, c := range n.Comments.Nodes {
				ce := ghEntry{kind: "comment", id: c.ID, author: c.Author.name(), when: c.CreatedAt, text: c.Body, url: c.URL}
				if c.ReplyTo != nil {
					if t := threads[c.ReplyTo.ID]; t != nil {
						t.comments = append(t.comments, ce)
					}

					continue
				}

				e.threads = append(e.threads, ghThread{path: c.Path, line: cmp.Or(c.Line, c.OriginalLine), comments: []ghEntry{ce}})
			}

			if e.state == "COMMENTED" && strings.TrimSpace(e.text) == "" && len(e.threads) == 0 { // only replies, which joined their threads
				continue
			}

			out = append(out, e)
			// Pointers into the entry just added, which no append of this loop moves.
			for i := range out[len(out)-1].threads {
				t := &out[len(out)-1].threads[i]
				threads[t.comments[0].id] = t
			}

		default:
			if s := ghEvent(n); s != "" {
				out = append(out, ghEntry{kind: "event", author: n.Actor.name(), when: n.CreatedAt, text: s})
			}
		}
	}

	return out
}

// ghEvent is the sentence that follows the actor's name.
func ghEvent(n ghNode) string {
	switch n.Typename {
	case "ReviewRequestedEvent":
		if r := n.Requested; r != nil {
			return "requested a review from " + cmp.Or(r.Login, r.Slug)
		}

	case "AssignedEvent":
		if n.Assignee != nil {
			return "assigned " + n.Assignee.Login
		}

	case "LabeledEvent":
		if n.Label != nil {
			return "added the " + n.Label.Name + " label"
		}

	case "ReadyForReviewEvent":
		return "marked this as ready for review"
	case "ConvertToDraftEvent":
		return "marked this as a draft"
	case "ClosedEvent":
		return "closed this"
	case "ReopenedEvent":
		return "reopened this"
	case "MergedEvent":
		return "merged this"
	case "HeadRefForcePushedEvent":
		return "force-pushed the branch"
	case "RenamedTitleEvent":
		return fmt.Sprintf("changed the title from %q to %q", n.PreviousTitle, n.CurrentTitle)
	}

	return ""
}

// ghAgo is when as GitHub's page says it, a day or a week at the coarsest.
func ghAgo(when time.Time) string {
	d := time.Since(when)

	switch {
	case when.IsZero():
		return ""
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " days ago"
	}

	return "on " + when.Format("2 Jan 2006")
}

// ghQuote is text as a reply quotes it.
func ghQuote(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}

	return strings.Join(lines, "\n") + "\n\n"
}
