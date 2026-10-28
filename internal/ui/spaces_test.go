package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// addProject adds a project whose only worktree is its own checkout on
// branch, with sessions ids in it.
func addProject(t *testing.T, m *Model, branch string, ids ...string) string {
	t.Helper()

	p := t.TempDir()
	m.st.Projects = append(m.st.Projects, p)
	m.wss = append(m.wss, proto.Workspace{Path: p, Project: p, Branch: branch, Main: true})

	for _, id := range ids {
		m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: p, Agent: "shell"}, Status: "idle"})
	}

	return p
}

// TestSpacesMergedCheckout shows a project's only worktree, its own
// checkout, on the project's heading: its sessions hang straight off it, its
// buttons are the checkout's and the project's, n asks only for the harness,
// and a click on the branch switches to it. A linked worktree splits them
// again.
func TestSpacesMergedCheckout(t *testing.T) {
	m := testModel(t)
	other := addProject(t, m, "dev", "s2")
	press(m, "3")

	rows := m.ag.rows(m)
	if slices.ContainsFunc(rows, func(r agRow) bool { return r.kind == agWorkspace }) || !rows[0].merged() || rows[1].s.ID != "s1" {
		t.Fatalf("no checkout rows, the sessions under the heading: %+v", rows)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "▾ ○ "+filepath.Base(other)+"  ⌂ dev") {
		t.Fatalf("the heading carries its branch:\n%s", out)
	}

	var tips []string
	for _, a := range m.ag.actions(m, rows[0], 40) {
		tips = append(tips, a.tip)
	}

	if want := []string{"New Session  n", "Resume Session", "New Worktree  w"}; !slices.Equal(tips, want) {
		t.Fatalf("the heading's buttons: %q want %q", tips, want)
	}

	m.ag.l.sel = 0
	press(m, "n")

	if m.modal == nil || m.modal.title != "New session in "+filepath.Base(m.ws) {
		t.Fatalf("n on the heading asks only for the harness: %+v", m.modal)
	}

	m.modal = nil
	o := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agProject && r.project == other })
	i := m.colOf(viewAgents)
	x := m.colRect(i).x + m.barW(i) + ansi.StringWidth(" ▾ ○ "+filepath.Base(other)+"  ")
	click(m, x+1, m.bodyTop(viewAgents)+o, tea.MouseLeft)

	if m.ws != other || m.ag.collapsed[other] {
		t.Fatalf("a click on the branch switches to it, folding nothing: ws %q", m.ws)
	}

	m.ag.l.sel = 0
	press(m, "enter")

	if m.ws != m.wss[0].Path || m.ag.collapsed[m.wss[0].Project] {
		t.Fatalf("enter on the heading switches to its checkout, as its row's did: ws %q", m.ws)
	}

	click(m, x-3, m.bodyTop(viewAgents)+o, tea.MouseLeft)

	if !m.ag.collapsed[other] || slices.ContainsFunc(m.ag.rows(m), func(r agRow) bool { return r.project == other && r.merged() }) {
		t.Fatal("a click on the name folds it, the branch with it")
	}

	m.wss = append(m.wss, proto.Workspace{Path: t.TempDir(), Project: m.wss[0].Project, Branch: "feat"})

	rows = m.ag.rows(m)
	if rows[0].merged() || rows[1].kind != agWorkspace || rows[1].ws.Path != m.wss[0].Path {
		t.Fatalf("a linked worktree gives the checkout its row back: %+v", rows[:2])
	}
}

// TestSpacesIdleAge says how long an idle session has been idle where it
// said idle; the other states keep their word.
func TestSpacesIdleAge(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{12 * time.Minute, "12m"},
		{3 * time.Hour, "3h"},
		{6 * 24 * time.Hour, "6d"},
		{15 * 24 * time.Hour, "2w"},
	} {
		if got := shortAgo(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v: %q, want %q", c.ago, got, c.want)
		}
	}

	if got := shortAgo(time.Time{}, now); got != "" {
		t.Fatalf("no time, no age: %q", got)
	}

	m := testModel(t)
	m.sessions[0].Updated = time.Now().Add(-3 * time.Hour)
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "blocked", Updated: time.Now()},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s3", Workspace: m.ws, Agent: "claude"}, Status: "idle"})
	press(m, "3")

	lines := m.ag.lines(m, 40, m.bodyH(viewAgents))
	for i, want := range map[int]string{1: " 3h ", 2: " input ", 3: " idle "} {
		if l := ansi.Strip(lines[i]); !strings.HasSuffix(l, want) {
			t.Fatalf("row %d ends in %q: %q", i, want, l)
		}
	}
}

// TestSpacesNoSessions lists the projects with no session last, under a
// heading of their own that counts them, folds like a project and sinks to
// the bottom edge folded; each is one dimmed row with no fold, folded or not,
// ⏎ on it switches into it, nothing moves across the heading, and a switch
// into one opens it.
func TestSpacesNoSessions(t *testing.T) {
	m := testModel(t)
	b1, b2 := addProject(t, m, "main"), addProject(t, m, "main")
	press(m, "3")

	var shape []string

	rows := m.ag.rows(m)
	for _, r := range rows {
		switch r.kind {
		case agTime:
			shape = append(shape, r.when)
		case agProject:
			shape = append(shape, filepath.Base(r.project))
		case agGap:
			shape = append(shape, "")
		default:
			shape = append(shape, r.s.ID)
		}
	}

	want := []string{filepath.Base(m.ws), "s1", "", bareHead, "", filepath.Base(b1), filepath.Base(b2)} // a rule under the strip; nothing under them to set apart
	if !slices.Equal(shape, want) {
		t.Fatalf("rows %q want %q", shape, want)
	}

	head := slices.IndexFunc(rows, func(r agRow) bool { return r.when == bareHead })
	lines := m.ag.lines(m, 40, m.bodyH(viewAgents))

	if l := strings.TrimRight(ansi.Strip(lines[head]), " "); !strings.HasPrefix(l, " ▾ · NO SESSIONS") || !strings.HasSuffix(l, " 2") {
		t.Fatalf("the strip counts its projects: %q", l)
	}

	if l := ansi.Strip(lines[head+1]); strings.Trim(l, "─ ") != "" {
		t.Fatalf("a rule under the strip, over the projects it holds: %q", l)
	}

	m.ag.l.sel = head + 2
	press(m, "alt+up")

	if !slices.Equal(m.st.Projects, []string{m.ws, b1, b2}) {
		t.Fatalf("alt+up does not take a project out from under the heading: %v", m.st.Projects)
	}

	first := m.ws
	m.ag.fold(b1, true)

	rows = m.ag.rows(m)
	if l := ansi.Strip(m.ag.lines(m, 40, m.bodyH(viewAgents))[head+2]); !strings.HasPrefix(l, "   · "+filepath.Base(b1)+" ") || rows[head+3].project != b2 {
		t.Fatalf("folded too, a row in the project order, no chevron, no branch: %q", l)
	}

	m.ag.l.sel = head + 2
	press(m, "n")

	if m.modal == nil || m.modal.title != "New session in "+filepath.Base(b1) {
		t.Fatalf("n starts in its only checkout: %+v", m.modal)
	}

	m.modal = nil
	press(m, "enter")

	if m.ws != b1 {
		t.Fatalf("enter switches into it: ws %q", m.ws)
	}

	m.switchWorkspace(first)

	m.ag.activate(m, &rows[head])

	if rows := m.ag.rows(m); len(rows) != m.bodyH(viewAgents) || rows[len(rows)-1].when != bareHead {
		t.Fatalf("folded, the heading sits on the bottom edge: %d rows of %d", len(rows), m.bodyH(viewAgents))
	}

	if lines := m.ag.lines(m, 40, m.bodyH(viewAgents)); strings.Trim(ansi.Strip(lines[len(lines)-2]), "─ ") != "" || strings.Trim(ansi.Strip(lines[len(lines)-3]), " ") != "" {
		t.Fatalf("folded, a rule right over it, blank above:\n%s", strings.Join(lines, "\n"))
	}

	m.ag.reveal(m, b2)

	if r := m.ag.selected(m); m.ag.collapsed[timeKey(bareHead)] || r == nil || !r.shows(b2) {
		t.Fatalf("a switch into one opens the heading and selects it: %+v", r)
	}

	m.ag.fold(m.wss[0].Project, true)

	rows = m.ag.rows(m)
	if h := slices.IndexFunc(rows, func(r agRow) bool { return r.when == bareHead }); h < 1 || strings.Trim(ansi.Strip(m.ag.lines(m, 40, m.bodyH(viewAgents))[h-1]), "─ ") != "" {
		t.Fatalf("a folded project over the strip leaves the rule over it: %+v", rows)
	}

	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: b2, Agent: "shell"}, Status: "idle"})

	if got := m.ag.shownProjects(m); !slices.Equal(got, []string{b2, m.ws, b1}) { // the open busy, the folded, the bare
		t.Fatalf("a session takes its project up among the busy: %v", got)
	}
}

// TestSpacesSticky pins the heading of the tree's first row over it once the
// tree scrolls past the heading: a click there is the heading's, and a
// selection under it scrolls back into view.
func TestSpacesSticky(t *testing.T) {
	m := testModelSized(t, 100, 12)
	for _, id := range []string{"s2", "s3", "s4"} {
		m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: "shell"}, Status: "idle"})
	}

	other := addProject(t, m, "main", "o1", "o2", "o3", "o4")
	addProject(t, m, "main", "p1", "p2", "p3", "p4") // taller than the view
	press(m, "3")
	checkWidths(t, m)

	h := m.bodyH(viewAgents)
	rows := m.ag.rows(m) // first, s1…s4, gap, other, o1…o4, …
	o := slices.IndexFunc(rows, func(r agRow) bool { return r.project == other && r.kind == agProject })

	m.ag.l.top = o + 1 // o1 under the first line, o2 below it
	if l := ansi.Strip(m.ag.lines(m, 40, h)[0]); !strings.Contains(l, filepath.Base(other)) || m.ag.rowAt(m, rows, 0) != o {
		t.Fatalf("the heading sticks over the first line: %q, row %d", l, m.ag.rowAt(m, rows, 0))
	}

	m.ag.l.top = o - 2 // s4 under the first line, the gap and the next heading below
	if l := ansi.Strip(m.ag.lines(m, 40, h)[0]); !strings.Contains(l, filepath.Base(m.ws)) || m.ag.rowAt(m, rows, 2) != o {
		t.Fatalf("the first project's heading sticks until the next one's gap: %q", l)
	}

	m.ag.l.top = o - 1
	if l := ansi.Strip(m.ag.lines(m, 40, h)[0]); strings.Trim(l, "─ ") != "" || m.ag.rowAt(m, rows, 1) != o {
		t.Fatalf("then the gap's rule, the next heading under it: %q", l)
	}

	m.ag.l.top = o
	if l := ansi.Strip(m.ag.lines(m, 40, h)[0]); !strings.Contains(l, filepath.Base(other)) || m.ag.sticky(m, rows) >= 0 {
		t.Fatalf("a heading on the first line is its own: %q", l)
	}

	m.ag.l.top, m.ag.l.sel = o+1, o+1
	m.ag.snap(m)

	if m.ag.l.top != o {
		t.Fatalf("the selection under the sticky heading scrolls into view: top %d", m.ag.l.top)
	}

	m.ag.l.top = o + 2
	click(m, m.colRect(m.colOf(viewAgents)).x+1, m.bodyTop(viewAgents), tea.MouseLeft)

	if !m.ag.collapsed[other] {
		t.Fatal("a click on the sticky heading folds its project")
	}
}

// TestSpacesGroupByState groups the sessions under their state, the most
// demanding first, each heading counting its own; a row says where it runs
// and since when, its heading the state.
func TestSpacesGroupByState(t *testing.T) {
	m := testModel(t)
	now := time.Now()
	m.sessions[0].Updated = now.Add(-3 * time.Hour)
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "blocked", Updated: now},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s3", Workspace: m.ws, Agent: "claude"}, Status: "idle", Attention: true, Updated: now})
	press(m, "3", "o")

	i := slices.IndexFunc(m.modal.disp, func(it item) bool { return strings.TrimSpace(it.label) == "Group by State" })
	if i < 0 {
		t.Fatalf("no Group by State in the view menu: %+v", m.modal.disp)
	}

	m.modal.choose(m, i)
	m.st.Settings.SpGroup = "state" // the daemon's state event, which the test has none of

	var heads []string

	rows := m.ag.rows(m)
	for _, r := range rows {
		if r.kind == agTime {
			heads = append(heads, r.when)
		}
	}

	if !slices.Equal(heads, []string{"Blocked", "Done", "Idle"}) || m.sessionsMovable() {
		t.Fatalf("headings %q, the most demanding first; no reorder", heads)
	}

	lines := m.ag.lines(m, 60, m.bodyH(viewAgents))
	if l := strings.TrimRight(ansi.Strip(lines[0]), " "); !strings.HasPrefix(l, " ▾ × Blocked") || !strings.HasSuffix(l, " 1") {
		t.Fatalf("a heading counts its sessions: %q", l)
	}

	s1 := slices.IndexFunc(rows, func(r agRow) bool { return r.s.ID == "s1" })
	if l := ansi.Strip(lines[s1]); !strings.HasSuffix(l, " "+filepath.Base(m.ws)+" · main · 3h ") {
		t.Fatalf("an idle row: its project, branch and age: %q", l)
	}

	checkWidths(t, m)
}
