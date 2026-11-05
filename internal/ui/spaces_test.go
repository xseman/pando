package ui

import (
	"fmt"
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
// a click on the branch or the name switches to it, on the chevron folds it.
// A linked worktree splits them again.
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

	if m.ws != other || m.ag.collapsed[other] {
		t.Fatalf("a click on the name switches to its checkout, as enter, folding nothing: ws %q", m.ws)
	}

	click(m, m.colRect(i).x+m.barW(i)+1, m.bodyTop(viewAgents)+o, tea.MouseLeft)

	if !m.ag.collapsed[other] || slices.ContainsFunc(m.ag.rows(m), func(r agRow) bool { return r.project == other && r.merged() }) {
		t.Fatal("a click on the chevron folds it, the branch with it")
	}

	m.wss = append(m.wss, proto.Workspace{Path: t.TempDir(), Project: m.wss[0].Project, Branch: "feat"})

	rows = m.ag.rows(m)
	if rows[0].merged() || rows[1].kind != agWorkspace || rows[1].ws.Path != m.wss[0].Path {
		t.Fatalf("a linked worktree gives the checkout its row back: %+v", rows[:2])
	}

	head := m.colRect(i).x + m.barW(i) + 5
	click(m, head, m.bodyTop(viewAgents), tea.MouseLeft)

	if m.ag.collapsed[m.wss[0].Project] || m.ws != m.wss[0].Path {
		t.Fatalf("a click on its name switches to its first worktree, folding nothing: ws %q", m.ws)
	}

	feat := m.wss[len(m.wss)-1].Path
	m.switchWorkspace(feat)
	click(m, head, m.bodyTop(viewAgents), tea.MouseLeft)

	if m.ws != feat || m.ag.l.sel != 0 {
		t.Fatalf("a click on the project in view selects it, staying on its worktree: ws %q, sel %d", m.ws, m.ag.l.sel)
	}

	m.ag.fold(other, true)
	o = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agProject && r.project == other })
	click(m, head, m.bodyTop(viewAgents)+o, tea.MouseLeft)

	if m.ws != other || m.ag.collapsed[other] {
		t.Fatalf("a click on a folded project's name switches to it, opening it: ws %q", m.ws)
	}
}

// TestSpacesFoldedCount counts the sessions a folded project hides on its
// heading; open, its rows say it.
func TestSpacesFoldedCount(t *testing.T) {
	m := testModel(t)
	other := addProject(t, m, "dev", "s2", "s3")
	press(m, "3")

	m.ag.collapsed[other] = true
	if out := checkWidths(t, m); !strings.Contains(out, "▸ ○ "+filepath.Base(other)) || !strings.Contains(out, " 2 ") {
		t.Fatalf("the folded heading counts its sessions:\n%s", out)
	}

	y := m.bodyTop(viewAgents) + slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.project == other })
	x := m.colRect(m.colOf(viewAgents)).x + m.barW(m.colOf(viewAgents))
	row := func() string { return strings.Split(m.View().Content, "\n")[y] }

	m.Update(tea.MouseMotionMsg{X: x + 5, Y: y})

	if strings.Contains(row(), keycapHot().Background(pal.twistieHoverBg).Render(" "+chevron(false))) {
		t.Fatalf("the chevron is raised with the pointer on the name: %q", row())
	}

	m.Update(tea.MouseMotionMsg{X: x + 1, Y: y})

	if !strings.Contains(row(), keycapHot().Background(pal.twistieHoverBg).Render(" "+chevron(false))) {
		t.Fatalf("the chevron under the pointer is not a button: %q", row())
	}

	m.ag.collapsed[other] = false
	if out := checkWidths(t, m); strings.Contains(out, " 2 ") {
		t.Fatalf("an open heading counts nothing:\n%s", out)
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
// dim caption of the projects not on top that counts them, folds like a
// project and sinks to the bottom edge; each is one dimmed row with no fold,
// folded or not, ⏎ on it switches into it, nothing moves across the heading,
// and a switch into one opens it.
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

	want := []string{filepath.Base(m.ws), "s1", "", bareHead, filepath.Base(b1), filepath.Base(b2)} // nothing under them to set apart
	if shape = slices.Compact(shape); !slices.Equal(shape, want) {                                  // the blank, then the padding that sinks them
		t.Fatalf("rows %q want %q", shape, want)
	}

	head := slices.IndexFunc(rows, func(r agRow) bool { return r.when == bareHead })
	lines := m.ag.lines(m, 40, m.bodyH(viewAgents))

	if l := strings.TrimRight(ansi.Strip(lines[head]), " "); !strings.HasPrefix(l, " ▾ other projects") || !strings.HasSuffix(l, " 2") {
		t.Fatalf("the caption counts its projects: %q", l)
	}

	if l := ansi.Strip(lines[head-1]); strings.TrimSpace(l) != "" {
		t.Fatalf("a blank over the caption, no rule: %q", l)
	}

	m.ag.l.sel = head + 1
	press(m, "alt+up")

	if !slices.Equal(m.st.Projects, []string{m.ws, b1, b2}) {
		t.Fatalf("alt+up does not take a project out from under the heading: %v", m.st.Projects)
	}

	first := m.ws
	m.ag.fold(b1, true)

	rows = m.ag.rows(m)
	if l := ansi.Strip(m.ag.lines(m, 40, m.bodyH(viewAgents))[head+1]); !strings.HasPrefix(l, "     "+filepath.Base(b1)+" ") || rows[head+2].project != b2 {
		t.Fatalf("folded too, a row in the project order, no chevron, no glyph, no branch: %q", l)
	}

	m.ag.l.sel = head + 1
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

	if lines := m.ag.lines(m, 40, m.bodyH(viewAgents)); strings.TrimSpace(ansi.Strip(lines[len(lines)-2])) != "" {
		t.Fatalf("folded, blanks over it, no rule:\n%s", strings.Join(lines, "\n"))
	}

	m.ag.reveal(m, b2)

	if r := m.ag.selected(m); m.ag.collapsed[timeKey(bareHead)] || r == nil || !r.shows(b2) {
		t.Fatalf("a switch into one opens the heading and selects it: %+v", r)
	}

	m.ag.fold(m.wss[0].Project, true)

	rows = m.ag.rows(m)
	if h := slices.IndexFunc(rows, func(r agRow) bool { return r.when == bareHead }); h < 1 || strings.TrimSpace(ansi.Strip(m.ag.lines(m, 40, m.bodyH(viewAgents))[h-1])) != "" {
		t.Fatalf("a folded project over the caption leaves a blank over it: %+v", rows)
	}

	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: b2, Agent: "shell"}, Status: "idle"})

	if got := m.ag.topProjects(m); !slices.Equal(got, []string{b2}) { // the folded and the bare stay under the caption
		t.Fatalf("a session takes its project up on top: %v", got)
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

// TestSpacesUnfoldRunning opens a folded project when one of its sessions
// goes to work; folded again while it works, it stays shut.
func TestSpacesUnfoldRunning(t *testing.T) {
	m := testModel(t)
	other := addProject(t, m, "dev", "s2", "s3")
	press(m, "3")
	m.ag.fold(other, true)

	set := func(id, status string) {
		next := slices.Clone(m.sessions)
		next[slices.IndexFunc(next, func(s proto.Session) bool { return s.ID == id })].Status = status
		m.Update(sessionsMsg(next))
	}

	set("s2", "blocked")

	if !m.ag.collapsed[other] {
		t.Fatal("only a session going to work opens its folded project, not one waiting for an answer")
	}

	set("s2", "running")

	if m.ag.collapsed[other] || slices.Contains(m.ag.folded, other) {
		t.Fatalf("a session at work opens its folded project: folded %v", m.ag.folded)
	}

	m.ag.fold(other, true)
	set("s2", "running")

	if !m.ag.collapsed[other] {
		t.Fatal("folded again while its session works, the project stays shut")
	}

	set("s3", "running")

	if m.ag.collapsed[other] {
		t.Fatal("another session going to work opens it again")
	}

	m.ag.fold(other, true)
	set("s3", "idle")
	m.Update(reconnectMsg{})
	set("s3", "running")

	if !m.ag.collapsed[other] {
		t.Fatal("an agent a restarted daemon resumes opens nothing")
	}
}

// TestSessionsTailRecentFirst lists the three projects under Other Projects
// used last first, newest on top, a blank row under them, then the rest
// A–Z; the filter lists them all A–Z.
func TestSessionsTailRecentFirst(t *testing.T) {
	m := testModelSized(t, 100, 40)

	var ps []string

	for i := range 5 {
		p := addProject(t, m, "main")
		ps = append(ps, p)
		m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: fmt.Sprint("s", i), Workspace: p, Agent: "claude"}, Status: "asleep", Updated: time.Now().Add(time.Duration(i) * time.Minute)})
	}

	ps = append(ps, addProject(t, m, "main")) // no session: never recent
	press(m, "3")

	tail := func() []string {
		rows := m.ag.rows(m)

		var got []string

		for _, r := range rows[slices.IndexFunc(rows, func(r agRow) bool { return r.when == bareHead })+1:] {
			got = append(got, r.project)
		}

		return got
	}

	if got, want := tail(), []string{ps[4], ps[3], ps[2], "", ps[0], ps[1], ps[5]}; !slices.Equal(got, want) { // TempDirs sort by their number
		t.Fatalf("tail %q want %q", got, want)
	}

	rows, lines := m.ag.rows(m), m.ag.lines(m, 40, m.bodyH(viewAgents))
	if gap := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agGap && r.tail }); strings.TrimSpace(ansi.Strip(lines[gap])) != "" {
		t.Fatalf("a blank under the recent, no rule: %q", ansi.Strip(lines[gap]))
	}

	press(m, "/")
	typeText(m, "main") // every project's branch

	if got := tail(); slices.Contains(got, "") {
		t.Fatalf("the filter lists no recent: %q", got)
	}
}

// TestSessionsSwitchIntoTail clicks into a project asleep under Other
// Projects: its session goes on screen, the project on top, and the
// selection on its row there; a click on an asleep session opened in place
// keeps the selection on that session.
func TestSessionsSwitchIntoTail(t *testing.T) {
	m := testModelSized(t, 100, 40)
	p, q := addProject(t, m, "main"), addProject(t, m, "main")
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "p1", Workspace: p, Agent: "claude"}, Status: "asleep"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "q1", Workspace: q, Agent: "claude"}, Status: "asleep"})
	press(m, "3")

	clickRow := func(pred func(agRow) bool, dx int) {
		t.Helper()

		i := slices.IndexFunc(m.ag.rows(m), pred)
		if i < 0 {
			t.Fatal("no such row")
		}

		col := m.colOf(viewAgents)
		click(m, m.colRect(col).x+m.barW(col)+dx, m.bodyTop(viewAgents)+i-m.ag.l.top, tea.MouseLeft)
	}

	clickRow(func(r agRow) bool { return r.kind == agProject && r.project == p }, 8) // its name, not the chevron

	if r := m.ag.selected(m); m.sess != "p1" || r == nil || !r.shows(p) || r.tail {
		t.Fatalf("a switch into it selects its row on top: sess %q, %+v", m.sess, r)
	}

	clickRow(func(r agRow) bool { return r.kind == agProject && r.project == q }, 1) // the chevron: open in place
	clickRow(func(r agRow) bool { return r.kind == agSession && r.s.ID == "q1" }, 8)

	if r := m.ag.selected(m); m.sess != "q1" || r == nil || r.kind != agSession || r.s.ID != "q1" || r.tail {
		t.Fatalf("the session clicked stays selected, on top: sess %q, %+v", m.sess, r)
	}
}

// TestSessionsSelectionSinks keeps the selection on a project that sinks
// under Other Projects as its last session falls asleep: on its row there,
// not on the blank rows that pin the tail down.
func TestSessionsSelectionSinks(t *testing.T) {
	m := testModelSized(t, 100, 30)
	p := addProject(t, m, "main", "a1")
	press(m, "3")

	m.ag.l.sel = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agSession && r.s.ID == "a1" })
	next := slices.Clone(m.sessions)
	next[slices.IndexFunc(next, func(s proto.Session) bool { return s.ID == "a1" })].Status = "asleep"
	m.Update(sessionsMsg(next))

	if r := m.ag.selected(m); r == nil || r.kind != agProject || r.project != p || !r.tail {
		t.Fatalf("the selection goes with its project under the caption: %+v", r)
	}
}

// TestSessionsSinkScrolls puts every project but the selected one's to
// sleep in one update: they sink under the caption, and the tree scrolls to
// the selection, which stays on its session.
func TestSessionsSinkScrolls(t *testing.T) {
	m := testModelSized(t, 100, 16)
	for i := range 12 {
		addProject(t, m, "main", fmt.Sprintf("x%d", i))
	}

	press(m, "3", "end") // the last project's session, scrolled to
	checkWidths(t, m)

	next := slices.Clone(m.sessions)
	for i := range next {
		if next[i].ID != "x11" {
			next[i].Status = "asleep"
		}
	}

	m.Update(sessionsMsg(next))
	checkWidths(t, m)

	if r := m.ag.selected(m); r == nil || r.s.ID != "x11" || m.ag.l.sel < m.ag.l.top || m.ag.l.sel >= m.ag.l.top+m.bodyH(viewAgents) {
		t.Fatalf("the selection stays on its session, in view: sel %d top %d, %+v", m.ag.l.sel, m.ag.l.top, r)
	}
}

// TestSessionsFilter finds the rows holding every word typed, in any case and
// without accents, never by the agent's name, so "ssd" no longer finds "Less
// CSS": a project found brings all it holds, a project is found by its
// folder's name too, and one under Other Projects stays there, under the
// caption. ⏎ opens the first one found, a session before a heading, and
// clears the filter.
func TestSessionsFilter(t *testing.T) {
	for _, c := range []struct {
		q, s string
		want bool
	}{
		{"ssd", "PR 422 ssd.stengl.sk optimalizcia", true},
		{"ssd", "DEV-44532: PoC Less → natívne CSS", false},
		{"zmazane", "Zmazané pravidlá a konfigurácie", true},
		{"PRAVIDLA zmaz", "Zmazané pravidlá", true},
		{"pravidla web", "Zmazané pravidlá", false},
		{"claude", searchText(agRow{kind: agSession, s: proto.Session{SessionSpec: proto.SessionSpec{Agent: "claude"}, Program: "claude"}}), false},
		{"shell", searchText(agRow{kind: agSession, s: proto.Session{SessionSpec: proto.SessionSpec{Agent: "shell"}, Title: "Pyenv"}}), false},
		{"other", searchText(agRow{kind: agTime, when: bareHead}), false},
		{"master", searchText(agRow{kind: agProject, project: "/w/p", ws: proto.Workspace{Path: "/w/p", Branch: "master", Main: true}}), true},
	} {
		if got := hits(c.q, c.s); got != c.want {
			t.Errorf("hits(%q, %q) = %v", c.q, c.s, got)
		}
	}

	m := testModel(t)
	nike := filepath.Join(t.TempDir(), "nike")
	project := func(name string, ss ...proto.Session) {
		p := filepath.Join(nike, name)
		m.st.Projects = append(m.st.Projects, p)
		m.wss = append(m.wss, proto.Workspace{Path: p, Project: p, Branch: "main", Main: true})

		for _, s := range ss {
			s.Workspace = p
			m.sessions = append(m.sessions, s)
		}
	}
	sess := func(id, title, status string) proto.Session {
		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Agent: "claude"}, Title: title, Status: status}
	}

	project("ssd.stengl.sk", sess("pr", "PR 422 ssd.stengl.sk optimalizcia", "idle"), sess("map", "orchestrator_mapa", "idle"))
	project("super.dental", sess("less", "DEV-44532: PoC Less → natívne CSS", "idle"))
	project("frontend-deploy", sess("py", "Pyenv v shelli", "idle"))
	project("cloudflare-iac", sess("cf", "Zmazané pravidlá a konfigurácie", "asleep"))

	found := func() []string {
		var out []string

		for _, r := range m.ag.rows(m) {
			switch r.kind {
			case agProject:
				out = append(out, filepath.Base(r.project))
			case agSession:
				out = append(out, r.s.ID)
			case agTime:
				out = append(out, r.when)
			}
		}

		return out
	}
	search := func(q string) {
		if m.filters[viewAgents].on {
			press(m, "esc")
		}

		press(m, "/")
		typeText(m, q)
	}

	press(m, "3")
	search("ssd")

	if got := found(); !m.filters[viewAgents].on || !slices.Equal(got, []string{"ssd.stengl.sk", "pr", "map"}) {
		t.Fatalf("/ then ssd finds its project and all it holds, nothing else: %q", got)
	}

	if r := m.ag.selected(m); r == nil || r.s.ID != "pr" {
		t.Fatalf("the first session found is selected: %+v", r)
	}

	search("nike")

	if got := found(); slices.Contains(got, filepath.Base(m.ws)) || !slices.Contains(got, "frontend-deploy") {
		t.Fatalf("nike finds the projects in its folder: %q", got)
	}

	search("map")
	press(m, "esc")

	if r := m.ag.selected(m); m.filters[viewAgents].on || r == nil || r.s.ID != "map" {
		t.Fatalf("esc clears the filter, the row found still selected: %+v", r)
	}

	search("zmazane")

	if got := found(); !slices.Equal(got, []string{bareHead, "cloudflare-iac", "cf"}) {
		t.Fatalf("one asleep is found where it lists, under the caption: %q", got)
	}

	press(m, "enter")

	if f := m.filters[viewAgents]; f.on || m.sess != "cf" {
		t.Fatalf("⏎ opens the session found and clears the filter: on %v, sess %q", f.on, m.sess)
	}

	if r := m.ag.selected(m); r == nil || r.s.ID != "cf" || r.tail {
		t.Fatalf("the session opened stays selected, its project on top now: %+v", r)
	}
}

// TestSpacesPinProject pins a whole project under Pinned: its worktrees and
// every session in them, asleep too, a session pinned on its own shown once
// under it; its heading folds apart from the tree's, the Filter takes each
// part of Pinned alone, and it unpins to the tree's copy.
func TestSpacesPinProject(t *testing.T) {
	m := testModel(t)
	m.sessions[0].Name = "gamma"
	other := addProject(t, m, "main", "o1")
	feat := t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: feat, Project: other, Branch: "feat"})
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "o2", Workspace: feat, Agent: "shell", Name: "beta"}, Status: "idle"})
	m.Update(sessionsMsg(m.sessions))
	press(m, "3")

	shape := func() (out []string) {
		for _, r := range m.ag.rows(m) {
			l := ""

			switch r.kind {
			case agTime:
				l = r.when
			case agProject:
				l = filepath.Base(r.project)
			case agWorkspace:
				l = "ws:" + r.ws.Branch
			case agSession:
				l = r.s.ID
			}

			if r.pinned {
				l = "pin:" + l
			}

			out = append(out, l)
		}

		return out
	}
	sel := func(pinned bool, kind int, key string) {
		t.Helper()

		m.ag.l.sel = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool {
			return r.pinned == pinned && r.kind == kind && (r.project == key || r.s.ID == key)
		})
		if m.ag.l.sel < 0 {
			t.Fatalf("no row %v %d %q: %q", pinned, kind, key, shape())
		}
	}
	pinned := func() []string { return shape()[:slices.Index(shape(), filepath.Base(m.ws))] }
	o := "pin:" + filepath.Base(other)

	sel(false, agProject, other)
	press(m, "p")

	if want := []string{"Pinned", o, "pin:ws:main", "pin:o1", "pin:ws:feat", "pin:o2"}; !slices.Equal(m.st.Pinned, []string{other}) || !slices.Equal(pinned(), want) {
		t.Fatalf("p pins the project whole: %v, rows %q want %q", m.st.Pinned, shape(), want)
	}

	if r := m.ag.selected(m); r == nil || r.kind != agProject || r.project != other || r.pinned {
		t.Fatalf("the selection stays on the tree's project: %+v", r)
	}
	// A session of it pinned on its own shows once; one elsewhere after it.
	sel(false, agSession, "o2")
	press(m, "p")
	sel(false, agSession, "s1")
	press(m, "p")

	if want := []string{"Pinned", o, "pin:ws:main", "pin:o1", "pin:ws:feat", "pin:o2", "pin:ws:main", "pin:s1"}; !slices.Equal(pinned(), want) {
		t.Fatalf("o2 once, s1 after: %q want %q", shape(), want)
	}
	// Under its project a worktree counts its sessions, not names the project.
	for _, l := range m.ag.lines(m, 40, m.bodyH(viewAgents))[:8] {
		if l = strings.TrimRight(ansi.Strip(l), " "); strings.Contains(l, "feat") && (!strings.HasSuffix(l, " 1") || strings.Contains(l, filepath.Base(other))) {
			t.Fatalf("the pinned project's worktree: %q", l)
		}
	}
	// The Filter takes each part alone: s1's worktree is not the project's.
	press(m, "/")
	typeText(m, "gamma")

	if want := []string{"Pinned", "pin:ws:main", "pin:s1", filepath.Base(m.ws), "s1"}; !slices.Equal(shape(), want) {
		t.Fatalf("gamma under Pinned: %q want %q", shape(), want)
	}

	press(m, "esc")
	// Its heading folds in the pane alone; the tree's copy stays on top.
	sel(true, agProject, other)
	press(m, "space")

	if want := []string{"Pinned", o, "pin:ws:main", "pin:s1"}; !slices.Equal(pinned(), want) || !slices.Contains(m.ag.topProjects(m), other) {
		t.Fatalf("folded under Pinned: %q want %q, on top %q", shape(), want, m.ag.topProjects(m))
	}

	press(m, "space")

	labels := func() (out []string) {
		for _, it := range m.ag.items(m) {
			out = append(out, it.label)
		}

		return out
	}
	if ls := labels(); !slices.Contains(ls, "Unpin Project") || slices.Contains(ls, "Move Project Up") {
		t.Fatalf("the pinned project's menu: %q", ls)
	}
	// Asleep, it stays whole under Pinned and the tree's goes under bareHead.
	for i := range m.sessions[1:] {
		m.sessions[1+i].Status = "asleep"
	}

	if p := pinned(); !slices.Contains(p, "pin:o1") || !slices.Contains(p, "pin:o2") || slices.Contains(m.ag.topProjects(m), other) {
		t.Fatalf("asleep, Pinned lists all: %q", shape())
	}

	press(m, "p")

	if r := m.ag.selected(m); !slices.Equal(m.st.Pinned, []string{"o2", "s1"}) || r == nil || r.kind != agProject || r.project != other || r.pinned {
		t.Fatalf("unpinned under Pinned, onto the tree's copy: %v %+v", m.st.Pinned, r)
	}

	sel(false, agProject, m.ws)

	if ls := labels(); !slices.Contains(ls, "Pin Project") {
		t.Fatalf("the tree's project menu: %q", ls)
	}

	checkWidths(t, m)
}
