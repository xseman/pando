package ui

import (
	"fmt"
	"image/color"
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

// labels are the modal's rows, without their colors.
func labels(m *Model) []string {
	var out []string
	for _, it := range m.modal.disp {
		out = append(out, ansi.Strip(it.label))
	}

	return out
}

// send delivers a message and the message its command produces, once.
func send(m *Model, msg tea.Msg) {
	if _, cmd := m.Update(msg); cmd != nil {
		m.Update(cmd())
	}
}

// fire delivers the message cmd produces, if there is one.
func fire(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}

// termSession is an idle bash terminal for the panel.
func termSession(ws, id string) proto.Session {
	return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: ws, Agent: termAgent, Cmd: []string{"/bin/bash"}}, Status: "idle"}
}

func TestBranchPicker(t *testing.T) {
	m := testModelSized(t, 120, 40)

	root := m.ws
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "Ann"},
		{"add", "-A"},
		{"commit", "-qm", "init"},
		{"branch", "feat"},
		{"-c", "tag.gpgSign=false", "tag", "v1"},
	} {
		mustGit(t, root, a...)
	}

	head := func() string {
		out, _ := git.Run(root, "rev-parse", "--abbrev-ref", "HEAD")
		return strings.TrimSpace(out)
	}

	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "main"}}})
	press(m, "2")
	send(m, keyMsg("B"))
	got := labels(m)
	// 3 actions, then each kind under its heading, every ref with a detail row.
	if m.modal == nil || m.modal.title != "Select a branch or tag to checkout" || len(got) != 13 ||
		!strings.HasSuffix(got[0], "Create new branch…") || !strings.Contains(got[4], "branches") ||
		!strings.HasPrefix(got[6], "Ann • ") || !strings.Contains(got[10], "tags") {
		t.Fatalf("picker %v", got)
	}

	feat := slices.IndexFunc(got, func(l string) bool { return strings.HasSuffix(l, " feat") })
	x, y, _, _, _ := m.modal.rect(m)
	m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 2 + feat + 1}) // over feat's detail row

	if m.modal.l.sel != feat {
		t.Fatalf("a detail row selects its item: sel=%d want %d", m.modal.l.sel, feat)
	}

	checkWidths(t, m)
	send(m, keyMsg("enter"))

	if head() != "feat" {
		t.Fatalf("checkout feat: HEAD is %s (%s)", head(), m.msg)
	}

	// Create new branch takes the typed name and stays on top of the filter.
	send(m, keyMsg("B"))
	press(m, "t", "o", "p", "i", "c")

	if got := labels(m); !strings.HasSuffix(got[0], "Create new branch…") {
		t.Fatalf("filtered picker %v", got)
	}

	send(m, keyMsg("enter"))

	if head() != "topic" {
		t.Fatalf("create branch: HEAD is %s (%s)", head(), m.msg)
	}

	// A branch checked out in another worktree opens that worktree.
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, root, "worktree", "add", "-q", "-b", "side", wt)
	m.wss = append(m.wss, proto.Workspace{Path: wt, Project: root, Branch: "side"})
	send(m, keyMsg("B"))
	press(m, "s", "i", "d", "e")
	m.modal.l.sel = slices.IndexFunc(m.modal.disp, func(it item) bool { return strings.HasSuffix(it.label, " side") })
	send(m, keyMsg("enter"))

	if m.ws != wt {
		t.Fatalf("worktree branch: ws=%s msg=%q", m.ws, m.msg)
	}
}

func TestCommandPalette(t *testing.T) {
	m := testModel(t)
	press(m, "ctrl+shift+p")

	if m.modal == nil || m.modal.title != "Command Palette" || !slices.Contains(labels(m), "View: Quick Open…") ||
		!slices.Contains(labels(m), "Preferences: Color theme") || slices.Contains(labels(m), "Editor: Stage Selected Ranges") {
		t.Fatalf("palette %v", labels(m))
	}

	m = diffModel(t, 100, 20)
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})

	if !slices.Contains(labels(m), "Editor: Stage Selected Ranges") {
		t.Fatalf("a diff adds its line commands: %v", labels(m))
	}

	checkWidths(t, m)
	choose(t, m, "View: Show Search")

	if v, _ := m.viewOn(m.colOf(viewSearch)); v != viewSearch || !m.sr.query.Focused() {
		t.Fatal("the chosen command runs")
	}

	press(m, "ctrl+shift+p")

	if labels(m)[0] != "View: Show Search" {
		t.Fatalf("the last command comes first: %v", labels(m)[:3])
	}
}

func TestAgentNavigator(t *testing.T) {
	m := testModel(t)
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "running", Title: "fix"})
	press(m, "alt+t") // ⌃t stays free for VS Code's Go to Symbol in Workspace

	if got := labels(m); !slices.Equal(got, []string{"◐ claude · fix", "○ shell", "⌂ main"}) {
		t.Fatalf("navigator %q", got)
	}

	press(m, "@", "i", "d", "l", "e")

	if got := labels(m); !slices.Equal(got, []string{"○ shell"}) {
		t.Fatalf("@idle %q", got)
	}

	press(m, "enter")

	if m.sess != "s1" || m.focus != onMain {
		t.Fatalf("enter switches to the session: sess=%q focus=%d", m.sess, m.focus)
	}
}

func TestAgentsTreeAndSounds(t *testing.T) {
	m := testModel(t)
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "blocked", Attention: true})
	press(m, "3")

	out := checkWidths(t, m)
	for _, want := range []string{"▾ × ", "├─ ○ shell", "└─ × claude", "   ⌂ main"} { // the project carries its most demanding session
		if !strings.Contains(out, want) {
			t.Fatalf("agents tree lacks %q:\n%s", want, out)
		}
	}
	// A background session that starts waiting plays the request cue once,
	// one that finishes unseen the done cue; the visible session is silent.
	var played []string

	playSound = func(path string) { played = append(played, path) }
	m.st.Settings.SessHi = "off" // no pulse ticker beside the sound in the batch
	m.st.Settings.Sounds, m.st.Settings.SoundReq, m.st.Settings.SoundDone = true, "req", "done"
	m.sess = ""
	m.sessions[1].Status, m.sessions[1].Attention = "running", false
	next := slices.Clone(m.sessions)
	next[1].Status, next[1].Attention = "blocked", true
	send(m, sessionsMsg(next))
	m.soundAt = time.Time{}
	next = slices.Clone(next)
	next[0].Attention = true // the shell rang its bell
	send(m, sessionsMsg(next))
	m.soundAt = time.Time{}
	m.sess = "s2"
	next = slices.Clone(next)
	next[1].Status = "idle"
	send(m, sessionsMsg(next)) // s2 is on screen, and going idle is not a cue anyway

	if !slices.Equal(played, []string{"req", "done"}) {
		t.Fatalf("sounds played %q", played)
	}
}

// TestSessionHighlight tints a session that waits (×), finished unseen (✓)
// or failed (✕) in its symbol's color, on its Spaces row, its tab and a
// folded project holding it. The tint pulses between two shades until the
// session is clicked, and stays steady while the state lasts; a new state
// pulses again. "steady" never pulses, "off" drops it, and the session on
// screen needs none.
func TestSessionHighlight(t *testing.T) {
	m := testModel(t)
	m.st.Settings.SessHi = "tint"
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "b", Workspace: m.ws, Agent: "claude"}, Status: "blocked"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "d", Workspace: m.ws, Agent: "codex"}, Status: "idle", Attention: true},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "r", Workspace: m.ws, Agent: "gemini"}, Status: "running", Attention: true})
	press(m, "3")

	shades := func() (full, soft bool) {
		checkWidths(t, m)
		out := m.View().Content // with its colors

		return strings.Contains(out, bgParams(pal.blockedBg)), strings.Contains(out, bgParams(pal.blockedSoftBg))
	}

	// A new state pulses: the ticker runs and flips the shade.
	if m.blink() == nil || !m.blinking || m.blink() != nil {
		t.Fatal("a pulse starts one ticker")
	}

	for i, full := range []bool{true, false, true} {
		m.Update(blinkMsg{}) // not send: the next tick would sleep and flip it back

		if f, s := shades(); f != full || s == full {
			t.Fatalf("pulse %d: full %v soft %v, want full %v", i, f, s, full)
		}
	}

	if !strings.Contains(m.View().Content, bgParams(pal.doneBg)) && !strings.Contains(m.View().Content, bgParams(pal.doneSoftBg)) {
		t.Fatal("a done session is tinted in the attention color")
	}

	if tint, _ := sessionTint(m.sessions[3]); tint != nil {
		t.Fatal("a running session is not waiting for anyone")
	}

	var tabBgs []color.Color
	for _, tab := range m.tabsFor(100, m.spaceSessions(), "") {
		tabBgs = append(tabBgs, tab.bg)
	}

	if tabBgs[1] == nil || tabBgs[2] == nil || tabBgs[0] != nil || tabBgs[3] != nil {
		t.Fatalf("tab tints = %v", tabBgs)
	}

	// A click stops b's pulse; its tint stays while it waits.
	m.Update(focusSessionMsg("b"))
	m.Update(focusSessionMsg("s1"))

	if m.pulses(m.sessions[1]) || m.highlight(m.sessions[1]) != pal.blockedBg {
		t.Fatalf("clicked b: pulses %v, tint %v", m.pulses(m.sessions[1]), m.highlight(m.sessions[1]))
	}

	if !m.pulses(m.sessions[2]) {
		t.Fatal("d, never clicked, still pulses")
	}

	// A state it comes to afterwards is news again.
	m.sessions[1].Status = "exited"
	if !m.pulses(m.sessions[1]) {
		t.Fatal("b failing after the click pulses again")
	}

	// The session on screen needs no tint, and is seen as it changes.
	m.Update(focusSessionMsg("d"))
	send(m, sessionsMsg(slices.Clone(m.sessions)))

	if m.highlight(m.sessions[2]) != nil || m.pulses(m.sessions[2]) {
		t.Fatal("the session on screen is neither tinted nor news")
	}

	m.Update(focusSessionMsg("s1"))

	// A folded project carries the tint of what it hides, unless selected.
	other := t.TempDir()
	m.st.Projects = append(m.st.Projects, other)
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Branch: "main", Main: true})
	m.ag.collapsed = map[string]bool{m.ws: true}
	m.ag.l.sel = len(m.ag.rows(m)) - 1

	if f, s := shades(); !f && !s {
		t.Fatal("a folded project hides its failed session's tint")
	}

	m.ag.collapsed = nil

	// steady tints without the pulse, off drops the tint.
	m.st.Settings.SessHi = "steady"
	if m.pulses(m.sessions[1]) || m.highlight(m.sessions[1]) != pal.blockedBg {
		t.Fatal("steady tints without a pulse")
	}

	m.st.Settings.SessHi = "off"

	if f, s := shades(); f || s {
		t.Fatalf("off: full %v soft %v", f, s)
	}

	m.sessions = m.sessions[:1] // nothing left to pulse
	m.Update(blinkMsg{})

	if m.blinking || m.blinkOn {
		t.Fatal("the ticker stops with nothing to pulse")
	}
}

// TestModalOwnsThePointer: with a context menu open, moving over it hovers
// its items only; the list under it keeps the row the right click was on,
// rather than lighting up whatever row the pointer crosses behind the menu.
func TestModalOwnsThePointer(t *testing.T) {
	m := testModelSized(t, 100, 30)
	other := t.TempDir()
	m.st.Projects = append(m.st.Projects, other)
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Branch: "main", Main: true})
	press(m, "3")

	top := m.bodyTop(viewAgents)
	m.Update(tea.MouseClickMsg{X: 5, Y: top + 1, Button: tea.MouseRight})

	if m.modal == nil {
		t.Fatal("a right click on a row opens its menu")
	}

	before := m.hoverRow(viewAgents)
	m.Update(tea.MouseMotionMsg{X: 8, Y: top + 4}) // over the menu, level with another row

	if got := m.hoverRow(viewAgents); got != before || got != 1 {
		t.Fatalf("the list under the menu hovers row %d, want the right-clicked %d", got, before)
	}

	m.modal = nil
	m.Update(tea.MouseMotionMsg{X: 5, Y: top + 4})

	if got := m.hoverRow(viewAgents); got != 4 {
		t.Fatalf("with the menu gone the pointer hovers again: row %d", got)
	}
}

// TestAgentsTreeGaps keeps a blank row between two projects, herdr's gap
// between spaces: it renders empty, ↑↓ steps over it and a click on it is
// ignored, so the tree still walks one project row at a time. Folded
// projects have nothing to separate, so they stack without one.
func TestAgentsTreeGaps(t *testing.T) {
	m := testModel(t)
	other := t.TempDir()
	m.st.Projects = append(m.st.Projects, other)
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Branch: "main", Main: true})
	press(m, "3")
	rows := m.ag.rows(m)

	gap := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agGap })
	if gap <= 0 || rows[gap+1].kind != agProject {
		t.Fatalf("a blank row sits before the second project: %d of %d", gap, len(rows))
	}

	if n := len(slices.DeleteFunc(slices.Clone(rows), func(r agRow) bool { return r.kind != agGap })); n != 1 {
		t.Fatalf("one gap for two projects, got %d", n)
	}

	lines := strings.Split(checkWidths(t, m), "\n")

	top := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(ansi.Strip(l), " SPACES") }) + 1
	if top <= 0 {
		t.Fatalf("no Spaces header:\n%s", strings.Join(lines, "\n"))
	}

	if body := ansi.Cut(lines[top+gap], 0, 30); strings.TrimSpace(ansi.Strip(body)) != "" {
		t.Fatalf("the gap row draws something: %q", body)
	}
	// ↓ from the last row of the first project lands on the second project.
	m.ag.l.sel = gap - 1
	press(m, "down")

	if m.ag.l.sel != gap+1 {
		t.Fatalf("down over the gap: sel = %d, want %d", m.ag.l.sel, gap+1)
	}

	press(m, "up")

	if m.ag.l.sel != gap-1 {
		t.Fatalf("up over the gap: sel = %d, want %d", m.ag.l.sel, gap-1)
	}

	if r := m.ag.selected(m); r == nil || r.kind == agGap {
		t.Fatalf("the selection never rests on a gap: %+v", r)
	}
	// A click on the blank row keeps the selection where it was.
	cs, _ := m.layout()
	click(m, cs[0].x+1, top+gap, tea.MouseLeft)

	if m.ag.l.sel != gap-1 {
		t.Fatalf("click on the gap moved the selection to %d", m.ag.l.sel)
	}
	// Two folded projects are two rows, no gap between them.
	m.ag.collapsed = map[string]bool{m.ws: true, other: true}

	folded := m.ag.rows(m)
	if len(folded) != 2 || folded[0].kind != agProject || folded[1].kind != agProject {
		t.Fatalf("folded projects stack without a gap: %+v", folded)
	}
	// Unfolding the first one puts the gap back before the second.
	m.ag.collapsed[m.ws] = false
	mixed := m.ag.rows(m)

	last := mixed[len(mixed)-1]
	if last.kind != agProject || mixed[len(mixed)-2].kind != agGap {
		t.Fatalf("a gap follows an unfolded project: %+v", mixed)
	}
}

// TestSpacesViewOptions drives VS Code's view menu: Group by Time files
// sessions under day headings, the Sort orders them, the Filter leaves states
// out and stays open, and Collapse All Groups folds every heading.
func TestSpacesViewOptions(t *testing.T) {
	m := testModel(t)
	now := time.Now()
	m.sessions[0].Created, m.sessions[0].Updated = now.Add(-time.Minute), now.Add(-time.Minute)
	m.sessions = append(m.sessions, proto.Session{
		SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "shell", Created: now.AddDate(0, 0, -3)},
		Status:      "exited", Updated: now,
	})
	press(m, "3")

	choose := func(label string) {
		t.Helper()

		i := slices.IndexFunc(m.modal.disp, func(it item) bool { return strings.TrimSpace(strings.TrimPrefix(it.label, "✓")) == label })
		if i < 0 {
			t.Fatalf("no %q in %+v", label, m.modal.disp)
		}

		m.modal.choose(m, i)
	}
	ids := func() (out []string) {
		for _, r := range m.ag.rows(m) {
			switch r.kind {
			case agTime:
				out = append(out, r.when)
			case agSession:
				out = append(out, r.s.ID)
			}
		}

		return out
	}

	press(m, "o")

	if m.modal == nil || !slices.ContainsFunc(m.modal.disp, func(it item) bool { return it.label == "✓ Sort by Created" }) {
		t.Fatalf("the view menu checks the default sort: %+v", m.modal)
	}

	choose("Group by Time")

	if got := ids(); !slices.Equal(got, []string{"Today", "s1", "Last 7 Days", "s2"}) {
		t.Fatalf("by time, created: %v", got)
	}

	press(m, "o")
	choose("Sort by Updated")

	if got := ids(); !slices.Equal(got, []string{"Today", "s2", "s1"}) {
		t.Fatalf("by time, updated: %v", got)
	}

	checkWidths(t, m)

	press(m, "o")
	choose("Group by Workspace")

	if got := ids(); !slices.Equal(got, []string{"s2", "s1"}) {
		t.Fatalf("the tree sorted by updated: %v", got)
	}

	press(m, "o")
	choose("Filter")

	md := m.modal

	choose("Exited")

	if m.modal != md || !slices.Equal(m.st.Settings.SpHide, []string{"exited"}) {
		t.Fatalf("the filter stays open and hides exited: %v", m.st.Settings.SpHide)
	}

	if got := ids(); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("exited left out: %v", got)
	}

	choose("Show All")

	m.modal = nil
	m.st.Settings.SpGroup = "time"

	press(m, "o")
	choose("Collapse All Groups")

	if got := ids(); !slices.Equal(got, []string{"Today"}) {
		t.Fatalf("collapsed: %v", got)
	}
}

func TestTimeBucket(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-9 * time.Hour), "Today"},
		{now.Add(-11 * time.Hour), "Yesterday"},
		{now.AddDate(0, 0, -6), "Last 7 Days"},
		{now.AddDate(0, 0, -7), "Last 30 Days"},
		{now.AddDate(0, 0, -30), "Older"},
		{time.Time{}, "Older"},
	} {
		if got := timeBucket(c.t, now); got != c.want {
			t.Errorf("%v: %s, want %s", c.t, got, c.want)
		}
	}
}

// TestSpacesDragReorder drags a project down the Spaces list: the tree
// reorders under the pointer, the release saves the order, and a press that
// never moves is still the click that folds the project.
func TestSpacesDragReorder(t *testing.T) {
	m := testModel(t)
	other := t.TempDir()
	m.st.Projects = append(m.st.Projects, other)
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Branch: "main", Main: true})
	first := m.ws
	press(m, "3")
	m.ag.collapsed = map[string]bool{first: true, other: true}
	checkWidths(t, m) // the list settles its scroll offset when it draws
	cs, _ := m.layout()
	x, top := cs[m.colOf(viewAgents)].x+1, m.bodyTop(viewAgents)
	m.Update(tea.MouseClickMsg{X: x, Y: top, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragRow || m.drag.proj != first {
		t.Fatalf("a press on a project row starts a drag: %+v", m.drag)
	}

	if !m.ag.collapsed[first] {
		t.Fatal("the press must not unfold the project: the fold waits for the release")
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: top + 1, Button: tea.MouseLeft})

	if m.st.Projects[0] != other || m.st.Projects[1] != first {
		t.Fatalf("the project follows the pointer: %v", m.st.Projects)
	}

	if r := m.ag.selected(m); r == nil || r.project != first {
		t.Fatalf("the moved row stays selected: %+v", r)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: top + 1, Button: tea.MouseLeft})
	if cmd == nil || m.drag != nil {
		t.Fatalf("the release saves the order: cmd %v drag %+v", cmd != nil, m.drag)
	}
	// A press and release on the same row is a click, and folds.
	was := m.ag.collapsed[other]
	click(m, x, top, tea.MouseLeft)

	if m.ag.collapsed[other] == was {
		t.Fatal("a click that never moved folds the project")
	}
	// alt+↑↓ does the same from the keyboard.
	m.ag.l.sel = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agProject && r.project == first })
	press(m, "alt+up")

	if m.st.Projects[0] != first {
		t.Fatalf("alt+up moves the project: %v", m.st.Projects)
	}

	press(m, "alt+up")

	if m.st.Projects[0] != first {
		t.Fatalf("alt+up at the top does nothing: %v", m.st.Projects)
	}
}

// TestSpacesDragSession drags a session down its worktree: it follows the
// pointer with its tab, a sessions event mid-drag does not undo it, the
// release names the session whose place it took, and a press that never
// moves still opens it.
func TestSpacesDragSession(t *testing.T) {
	m := testModel(t)
	sess := func(id, parent string) proto.Session {
		agent := "shell"
		if parent != "" {
			agent = tabAgent
		}

		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: agent, Parent: parent}, Status: "idle"}
	}
	daemon := sessionsMsg{sess("s1", ""), sess("s2", ""), sess("s2t", "s2"), sess("s3", "")}
	m.Update(daemon)
	press(m, "3")
	checkWidths(t, m)
	cs, _ := m.layout()
	x, top := cs[m.colOf(viewAgents)].x+1, m.bodyTop(viewAgents)
	ids := func() string {
		var out []string
		for _, s := range m.sessions {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	m.Update(tea.MouseClickMsg{X: x, Y: top + 2, Button: tea.MouseLeft}) // project, worktree, s1

	if m.drag == nil || m.drag.sess != "s1" {
		t.Fatalf("a press on a session row starts a drag: %+v", m.drag)
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: top + 3, Button: tea.MouseLeft})

	if got := ids(); got != "s2 s2t s1 s3" || m.drag.to != "s2" {
		t.Fatalf("the session follows the pointer past s2 and its tab: %q, to %q", got, m.drag.to)
	}

	if r := m.ag.selected(m); r == nil || r.s.ID != "s1" {
		t.Fatalf("the moved row stays selected: %+v", r)
	}

	m.Update(daemon)

	if got := ids(); got != "s2 s2t s1 s3" {
		t.Fatalf("a sessions event mid-drag keeps the held session in place: %q", got)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: top + 3, Button: tea.MouseLeft})
	if cmd == nil || m.drag != nil {
		t.Fatalf("the release saves the order: cmd %v drag %+v", cmd != nil, m.drag)
	}

	press(m, "alt+up")

	if got := ids(); got != "s1 s2 s2t s3" {
		t.Fatalf("alt+up moves the session back: %q", got)
	}

	press(m, "3")
	click(m, x, top+4, tea.MouseLeft)

	if m.drag != nil || m.focus != onMain || m.sess != "s3" {
		t.Fatalf("a click that never moved opens the session: drag %+v, sess %q", m.drag, m.sess)
	}
	// Sorted by Updated the clock orders sessions: a press opens at once.
	m.st.Settings.SpSort = "updated"
	press(m, "3")
	m.Update(tea.MouseClickMsg{X: x, Y: top + 2, Button: tea.MouseLeft})

	if m.drag != nil {
		t.Fatalf("no drag when sorted by Updated: %+v", m.drag)
	}
}

// TestSpacesDragWorktree drags a project's checkout below its linked
// worktree: each takes its sessions along, a workspaces event mid-drag does
// not undo it, and a press that never moves still switches to the worktree.
func TestSpacesDragWorktree(t *testing.T) {
	m := testModel(t)
	root, wt := m.ws, t.TempDir()
	daemon := workspacesMsg{m.wss[0], {Path: wt, Project: root, Branch: "feat"}}
	m.Update(daemon)
	m.Update(sessionsMsg{
		{SessionSpec: proto.SessionSpec{ID: "s1", Workspace: root, Agent: "shell"}, Status: "idle"},
		{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: wt, Agent: "shell"}, Status: "idle"},
	})
	press(m, "3")
	checkWidths(t, m)
	cs, _ := m.layout()
	x, top := cs[m.colOf(viewAgents)].x+1, m.bodyTop(viewAgents)
	tree := func() string {
		var out []string

		for _, r := range m.ag.rows(m) {
			switch r.kind {
			case agWorkspace:
				out = append(out, r.ws.Branch)
			case agSession:
				out = append(out, r.s.ID)
			}
		}

		return strings.Join(out, " ")
	}

	m.Update(tea.MouseClickMsg{X: x, Y: top + 1, Button: tea.MouseLeft}) // project, main

	if m.drag == nil || m.drag.ws != root {
		t.Fatalf("a press on a worktree row starts a drag: %+v", m.drag)
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: top + 3, Button: tea.MouseLeft})

	if got := tree(); got != "feat s2 main s1" || m.drag.to != wt {
		t.Fatalf("the worktree follows the pointer with its session: %q, to %q", got, m.drag.to)
	}

	m.Update(daemon)

	if got := tree(); got != "feat s2 main s1" {
		t.Fatalf("a workspaces event mid-drag keeps the held worktree in place: %q", got)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: top + 3, Button: tea.MouseLeft})
	if cmd == nil || m.drag != nil {
		t.Fatalf("the release saves the order: cmd %v drag %+v", cmd != nil, m.drag)
	}

	press(m, "alt+up")

	if got := tree(); got != "main s1 feat s2" {
		t.Fatalf("alt+up moves the worktree back: %q", got)
	}

	press(m, "3")
	click(m, x, top+3, tea.MouseLeft)

	if m.drag != nil || m.ws != wt {
		t.Fatalf("a click that never moved switches to the worktree: drag %+v, ws %q", m.drag, m.ws)
	}
}

func TestStatusBar(t *testing.T) {
	m := gitModel(t)
	// The active session is not counted: you are looking at it.
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "running", Attention: true})
	lines := strings.Split(checkWidths(t, m), "\n")

	status := lines[len(lines)-1]
	if !strings.Contains(status, "⎇ main*") || !strings.Contains(status, "! 1") {
		t.Fatalf("status bar = %q", status)
	}

	if _, cmd := m.Update(tea.MouseClickMsg{X: 3, Y: m.h - 1, Button: tea.MouseLeft}); cmd == nil {
		t.Fatal("clicking the branch loads the refs")
	}

	_, zones := m.statusLine(m.w)
	click(m, zones[len(zones)-1].x, m.h-1, tea.MouseLeft)

	if m.modal == nil || m.modal.title != "Go to Agent or Worktree" {
		t.Fatalf("clicking the agent count opens the navigator: %+v", m.modal)
	}
}

func TestConfigKeys(t *testing.T) {
	if id := commandID("Git: Switch Branch…"); id != "git.switchBranch" {
		t.Fatalf("command id = %q", id)
	}

	m := testModel(t)
	m.st.Settings.Keys = map[string]string{"b": "", "ctrl+g": "view.showSearch"}
	press(m, "b")

	if m.hidden[0] {
		t.Fatal(`an empty binding unbinds the key`)
	}

	press(m, "ctrl+g")

	if v, _ := m.viewOn(m.colOf(viewSearch)); v != viewSearch || !m.sr.query.Focused() {
		t.Fatal("a bound key runs its command")
	}
	// While typing, keys are text.
	press(m, "b")

	if m.sr.query.Value() != "b" {
		t.Fatalf("query = %q", m.sr.query.Value())
	}
}

func TestDrawerVisibility(t *testing.T) {
	m := gitModel(t)
	if got := len(m.drawers()); got != 5 {
		t.Fatalf("drawers shown by default = %d", got)
	}

	if out := checkWidths(t, m); strings.Contains(out, "Worktrees") || !strings.Contains(out, "Commits") {
		t.Fatalf("default drawers:\n%s", out)
	}
	// A right click on a drawer header offers to hide it, or to toggle any other.
	_, ds := m.scm.geometry(m, m.scm.paneH(m))
	click(m, 3, m.bodyTop(viewGit)+ds[0].head, tea.MouseRight)

	if m.modal == nil || m.modal.disp[0].label != "Hide 'Graph'" {
		t.Fatalf("drawer menu = %v", labels(m))
	}

	choose(t, m, "Hide 'Graph'")

	if !slices.Equal(m.st.Settings.Drawers, []string{"Commits", "Branches", "Remotes", "Stashes"}) {
		t.Fatalf("after hiding: %v", m.st.Settings.Drawers)
	}

	m.scm.drawerMenu(m, "", 0, 0)
	choose(t, m, "  Worktrees")

	if !slices.Contains(m.st.Settings.Drawers, "Worktrees") {
		t.Fatalf("after showing: %v", m.st.Settings.Drawers)
	}

	checkWidths(t, m)
}

func TestQuitConfirmation(t *testing.T) {
	m := testModel(t)
	// A filter is what esc clears first; only then does it offer to quit.
	m.startFilter(viewFiles)
	m.filters[viewFiles].editing = false
	press(m, "esc")

	if m.modal != nil || m.filters[viewFiles].on {
		t.Fatalf("esc clears the filter first: modal=%v on=%v", m.modal, m.filters[viewFiles].on)
	}

	press(m, "esc")

	if m.modal == nil || m.modal.title != "Close pando?" {
		t.Fatalf("esc asks before closing: %+v", m.modal)
	}
	// Cancel goes back to work, Close quits.
	press(m, "down", "enter")

	if m.modal != nil {
		t.Fatal("cancel closes the popup")
	}

	press(m, "q")

	if m.modal == nil {
		t.Fatal("q asks too")
	}

	if m.modal.title != "Close pando?" {
		t.Fatalf("nothing unsaved, nothing to warn about: %q", m.modal.title)
	}

	if cmd := m.modal.items[0].run(m); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("Close quits")
	}
}

func TestDragTabToOtherSide(t *testing.T) {
	m := testModelSized(t, 120, 30)
	m.Update(tea.MouseClickMsg{X: tabAt(m, viewSearch), Y: 0, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragTab {
		t.Fatalf("pressing a tab picks it up: %+v", m.drag)
	}
	// Still just a click until the mouse actually moves.
	if m.drag.drop != nil {
		t.Fatal("no drop target before the tab moves")
	}

	_, c := m.layout()
	inner := c.x + c.w + 2 // just past main, the right sidebar's half toward it
	m.Update(tea.MouseMotionMsg{X: inner, Y: 10, Button: tea.MouseLeft})

	if m.drag.drop == nil || m.drag.drop.side != 1 {
		t.Fatalf("drop target = %+v", m.drag.drop)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "┌") || !strings.Contains(out, "New Column") == m.drag.drop.own {
		t.Fatal("the rectangle says where the tab would land")
	}

	m.Update(tea.MouseReleaseMsg{X: inner, Y: 10, Button: tea.MouseLeft})

	if m.drag != nil || !m.cols()[m.colOf(viewSearch)].right {
		t.Fatalf("dropped on the right: left=%v right=%v", m.st.Settings.Left, m.st.Settings.Right)
	}
	// And back: a plain click on a chip still only shows the view.
	click(m, tabAt(m, viewFiles), 0, tea.MouseLeft)

	if v, _ := m.viewOn(m.colOf(viewFiles)); v != viewFiles || m.cols()[m.colOf(viewFiles)].right {
		t.Fatal("a click that never moved is a click")
	}
}

func TestTerminalPanel(t *testing.T) {
	m := testModelSized(t, 130, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drainInputs(m)

	// Shut by default; ⌃` opens it under the editor and focuses it. Most
	// terminals send NUL for ⌃`, which arrives as ctrl+space.
	if m.termRows() != 0 || m.mainH() != m.panelH() {
		t.Fatal("the terminal starts shut")
	}

	press(m, "ctrl+space")

	if !m.termOpen() {
		t.Fatal("ctrl+space (NUL, what ⌃` sends) opens the terminal")
	}

	press(m, "ctrl+`")
	m.focus = 0 // closing left the focus in main's session, where 5 is the app's
	press(m, "5")

	if m.termRows() == 0 || m.focus != onPanel || m.mainH() != m.panelH()-m.termRows() {
		t.Fatalf("5 opens the panel under the editor: rows=%d focus=%d", m.termRows(), m.focus)
	}

	if m.tv.id != "t1" {
		t.Fatalf("the panel shows the running shell, got %q", m.tv.id)
	}

	if m.colOf(viewTerm) >= 0 {
		t.Fatal("a bottom panel is no sidebar column")
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.Contains(out[m.mainH()], "bash") {
		t.Fatalf("the panel's tabs sit on its first row: %q", out[m.mainH()])
	}
	// Terminal sessions stay out of the Agents view and the main strip.
	if len(m.agentSessions()) != 1 || m.agentSessions()[0].ID == "t1" {
		t.Fatalf("agent sessions = %+v", m.agentSessions())
	}

	// The strip is drawn and clicked with one width: + sits where it is drawn.
	tabs := m.termTabs(m.termStripW())

	plus := tabs[len(tabs)-1]
	if !plus.plus || plus.x == 0 {
		t.Fatalf("strip tabs = %+v", tabs)
	}

	c := m.mainX()
	if cmd := m.mouse(tea.MouseClickMsg{X: c + 1 + plus.x, Y: m.mainH(), Button: tea.MouseLeft}); cmd == nil {
		t.Fatal("clicking + in the bottom panel opens a shell")
	}

	// ⌃⇧↑ gives it the whole editor area, ⌃⇧↓ hands it back.
	press(m, "ctrl+shift+up")

	if m.termRows() != m.panelH() || m.mainH() != 0 {
		t.Fatalf("maximized rows=%d mainH=%d", m.termRows(), m.mainH())
	}

	checkWidths(t, m)
	press(m, "ctrl+shift+down")

	if m.termRows() == m.panelH() {
		t.Fatal("⌃⇧↓ restores the panel")
	}

	// Pressing it again closes it, whatever has the focus.
	m.focus = 0
	press(m, "ctrl+`")

	if m.termRows() != 0 || m.st.Settings.TermOpen {
		t.Fatalf("⌃` closes the panel: rows=%d open=%v", m.termRows(), m.st.Settings.TermOpen)
	}

	// When its last shell goes, the panel goes with it.
	press(m, "5")

	if !m.termOpen() || m.tv.id != "t1" {
		t.Fatalf("reopened: open=%v id=%q", m.termOpen(), m.tv.id)
	}

	m.Update(sessionsMsg(m.agentSessions()))

	if m.termOpen() || m.termRows() != 0 {
		t.Fatal("the last terminal tab closing shuts the panel")
	}

	m.sessions = append(m.sessions, termSession(m.ws, "t1"))

	// Moved to a side it becomes a column of its own, never a tab of another.
	m.moveTerminal("right")

	i := m.colOf(viewTerm)
	if i < 0 || !m.cols()[i].right || len(m.cols()[i].views) != 1 {
		t.Fatalf("a side terminal stands alone: %v", m.cols())
	}

	if m.termRows() != 0 {
		t.Fatal("no bottom panel while it is docked to a side")
	}

	checkWidths(t, m)
	press(m, "ctrl+`")

	if m.colOf(viewTerm) >= 0 || m.st.Settings.TermOpen {
		t.Fatal("⌃` closes a docked terminal too")
	}
}

func TestVerticalActivityBar(t *testing.T) {
	m := testModelSized(t, 120, 30)

	m.st.Settings.ActBar = "side"
	if m.barH(0) != 0 || m.barW(0) != actW() {
		t.Fatalf("side bar: barH=%d barW=%d", m.barH(0), m.barW(0))
	}

	out := strings.Split(checkWidths(t, m), "\n")
	raw := strings.Split(m.View().Content, "\n") // checkWidths strips the colors
	// The chips are the top bar's, stacked: one actH-row block per view, the
	// active one marked with the border down the strip's outer edge.
	if !strings.HasPrefix(out[0], "▎") || !strings.Contains(raw[0], fgParams(pal.headerAccent)) {
		t.Fatalf("the active chip is bordered on the outer edge:\n%s", out[0])
	}

	if got := strings.TrimSpace(ansi.Cut(out[0], 1, actW())); got != strings.TrimSpace(m.vertTabs(0)[0].label) {
		t.Fatalf("its icon sits beside the border: %q", got)
	}

	if strip := ansi.Cut(out[actH-1], 0, actW()); strings.TrimSpace(strip) != "" {
		t.Fatalf("the rest of the block is air: %q", strip)
	}

	if tabs := m.vertTabs(0); len(tabs) != 4 || tabs[1].x != actH || tabs[1].v != viewGit {
		t.Fatalf("vertical tabs = %+v", tabs)
	}

	click(m, 1, actH, tea.MouseLeft)

	if v, _ := m.viewOn(0); v != viewGit {
		t.Fatalf("clicking an icon shows its view, got %v", v)
	}
	// The view body starts after the icons, so a click there still hits rows.
	click(m, actW()+2, m.bodyTop(viewGit), tea.MouseLeft)
	checkWidths(t, m)
	// The mouse over a chip lights that chip only: the row beside it in the
	// body keeps its own background.
	m.mouseAt, m.mouseX, m.mouseY = time.Now(), m.colRect(0).x+1, m.bodyTop(viewGit)+2
	if got := m.hoverRow(viewGit); got != -1 {
		t.Fatalf("hover over the strip leaks into the body: row %d", got)
	}

	m.mouseX = m.colRect(0).x + actW() + 2
	if got := m.hoverRow(viewGit); got != 2 {
		t.Fatalf("hover over the body: row %d", got)
	}
	// Header buttons are laid out over the body and hit-tested there too:
	// hovering the last one (fold) marks it hot, clicking it folds the sidebar.
	rc := m.colRect(0)
	m.mouseAt, m.mouseY = time.Now(), m.barH(0)
	acts := m.headerActions(0, viewGit, rc.w)
	fold := acts[len(acts)-1]

	m.mouseX = rc.x + actW() + fold.x + 1
	if acts = m.headerActions(0, viewGit, rc.w); !acts[len(acts)-1].hot {
		t.Fatalf("the fold button is hot under the mouse: %+v", acts)
	}

	click(m, m.mouseX, m.barH(0), tea.MouseLeft)

	if !m.hidden[0] {
		t.Fatal("clicking the fold button where it is drawn folds the sidebar")
	}

	m.hidden[0] = false
	// A right sidebar keeps its icons on the right edge.
	m.moveView(viewSearch, 1)
	i := m.colOf(viewSearch)
	rc = m.colRect(i)
	click(m, rc.x+rc.w-2, 0, tea.MouseLeft)

	if v, _ := m.viewOn(i); v != viewSearch {
		t.Fatalf("right sidebar icons sit on its right edge, got %v", v)
	}

	checkWidths(t, m)
}

func TestDropIntoOwnColumn(t *testing.T) {
	m := testModelSized(t, 140, 30)
	// Dropped on the half of the sidebar toward main, the tab joins it.
	rc := m.colRect(0)
	m.Update(tea.MouseClickMsg{X: tabAt(m, viewSearch), Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: rc.x + rc.w - 2, Y: 10, Button: tea.MouseLeft})

	if m.drag.drop == nil || m.drag.drop.own {
		t.Fatalf("the inner half keeps it a tab: %+v", m.drag.drop)
	}
	// Dropped past it, toward the screen edge, it gets a column of its own.
	m.Update(tea.MouseMotionMsg{X: rc.x + 1, Y: 10, Button: tea.MouseLeft})

	if m.drag.drop == nil || !m.drag.drop.own || m.drag.drop.side != 0 {
		t.Fatalf("the outer half opens a column: %+v", m.drag.drop)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "New Column") {
		t.Fatal("the rectangle names the drop")
	}

	m.Update(tea.MouseReleaseMsg{X: rc.x + 1, Y: 10, Button: tea.MouseLeft})

	cs := m.cols()
	if i := m.colOf(viewSearch); i != 0 || len(cs[i].views) != 1 || cs[i].right {
		t.Fatalf("Search stands in its own left column: %v", cs)
	}

	if len(cs) < 2 {
		t.Fatalf("the other views keep their column: %v", cs)
	}

	checkWidths(t, m)
}

// TestDragTabReorders drags a chip along its own activity bar: the drop sets
// the tab's place, and the rectangle marks the gap between icons, not the
// whole column.
func TestDragTabReorders(t *testing.T) {
	m := testModelSized(t, 140, 30)
	rc := m.colRect(0)

	views := slices.Clone(m.colViews(0))
	if len(views) < 3 {
		t.Fatalf("views = %v", views)
	}

	last := m.tabs(0)[len(views)-1]
	m.Update(tea.MouseClickMsg{X: tabAt(m, views[0]), Y: 0, Button: tea.MouseLeft})
	// Over the bar, even on the outer half, the mouse picks a slot, not a new column.
	m.Update(tea.MouseMotionMsg{X: rc.x + last.x + last.w - 1, Y: 0, Button: tea.MouseLeft})

	d := m.drag.drop
	if d == nil || d.own || d.col != 0 || d.at != len(views)-1 {
		t.Fatalf("drop = %+v", d)
	}

	if d.h != actH || d.rc.w >= rc.w/2 {
		t.Fatalf("a small box over the bar, not the column: %+v", d)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.Contains(out[0], "┌") || !strings.Contains(out[actH-1], "└") || strings.Contains(out[actH], "└") {
		t.Fatalf("the box spans the bar rows only:\n%s\n%s\n%s", out[0], out[actH-1], out[actH])
	}

	m.Update(tea.MouseReleaseMsg{X: rc.x + last.x + last.w - 1, Y: 0, Button: tea.MouseLeft})

	got := m.colViews(0)
	if want := slices.Concat(views[1:], views[:1]); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	// Dropped where it already is, nothing is saved.
	m.Update(tea.MouseClickMsg{X: tabAt(m, views[1]), Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: tabAt(m, views[1]) + 2, Y: 1, Button: tea.MouseLeft})

	if d := m.drag.drop; d == nil || d.col != 0 || d.at != 0 {
		t.Fatalf("drop = %+v", d)
	}

	m.Update(tea.MouseReleaseMsg{X: tabAt(m, views[1]) + 2, Y: 1, Button: tea.MouseLeft})

	if !slices.Equal(m.colViews(0), got) {
		t.Fatalf("order = %v, want %v", m.colViews(0), got)
	}
	// Below the bar the outer half still opens a column.
	m.Update(tea.MouseClickMsg{X: tabAt(m, got[0]), Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: rc.x + 1, Y: 10, Button: tea.MouseLeft})

	if d := m.drag.drop; d == nil || !d.own || d.h != 0 {
		t.Fatalf("drop = %+v", d)
	}

	m.drag = nil
}

// TestDragChipDownSideBar reorders icons of the vertical activity bar: one
// row of motion is a drag there, and the chip takes the block it is let go
// on.
func TestDragChipDownSideBar(t *testing.T) {
	m := testModelSized(t, 140, 30)
	m.st.Settings.ActBar = "side"
	rc := m.colRect(0)
	views := slices.Clone(m.colViews(0))
	m.Update(tea.MouseClickMsg{X: rc.x + 1, Y: 0, Button: tea.MouseLeft})

	if m.drag == nil || !m.drag.fine {
		t.Fatalf("drag = %+v", m.drag)
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + 1, Y: actH, Button: tea.MouseLeft})

	d := m.drag.drop
	if d == nil || d.col != 0 || d.at != 1 || d.rc.w != actW() || d.h != actH {
		t.Fatalf("drop = %+v", d)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.HasPrefix(out[actH], "┌") || !strings.HasPrefix(out[2*actH-1], "└") {
		t.Fatalf("a box around the target block:\n%s\n%s", out[actH], out[2*actH-1])
	}

	m.Update(tea.MouseReleaseMsg{X: rc.x + 1, Y: actH, Button: tea.MouseLeft})

	if got := m.colViews(0); got[0] != views[1] || got[1] != views[0] {
		t.Fatalf("order = %v, from %v", got, views)
	}
	// From another column, dragged by its title, the chip joins at the row
	// under the mouse.
	m.moveView(viewSearch, 1)
	i := m.colOf(viewSearch)
	m.Update(tea.MouseClickMsg{X: m.colRect(i).x + 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: rc.x + 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: rc.x + 1, Y: 0, Button: tea.MouseLeft})

	if got := m.colViews(0); got[0] != viewSearch {
		t.Fatalf("order = %v", got)
	}

	checkWidths(t, m)
}

// Going to a worktree from the navigator shows its session and focuses it,
// the way picking the session itself does.
func TestNavigatorFocusesSession(t *testing.T) {
	m := testModelSized(t, 130, 30)
	api := filepath.Join(t.TempDir(), "api")
	mustMkdir(t, api)
	m.wss = append(m.wss, proto.Workspace{Path: api, Project: api, Branch: "main", Main: true})
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: api, Agent: "shell"}, Status: "idle"})
	m.focus = 0
	m.agentNavigator()
	m.modal.input.SetValue("api")
	m.modal.refilter()
	press(m, "enter")

	if m.sess != "s2" || m.ws != api || m.focus != onMain {
		t.Fatalf("after the navigator: sess=%q ws=%q focus=%d", m.sess, m.ws, m.focus)
	}
}

// The VS Code defaults, on the keys VS Code uses.
func TestVSCodeKeys(t *testing.T) {
	m := testModelSized(t, 130, 30)
	press(m, "ctrl+b")

	if !m.hidden[0] || !m.hidden[1] {
		t.Fatal("ctrl+b folds both sidebars")
	}

	press(m, "ctrl+b")
	press(m, "ctrl+shift+g")

	if v, _ := m.viewOn(m.focus); v != viewGit {
		t.Fatalf("ctrl+shift+g shows Source Control, got %v", v)
	}

	press(m, "ctrl+shift+e")

	if v, _ := m.viewOn(m.focus); v != viewFiles {
		t.Fatalf("ctrl+shift+e shows Explorer, got %v", v)
	}

	press(m, "ctrl+shift+h")

	if v, _ := m.viewOn(m.focus); v != viewSearch || !m.sr.showReplace {
		t.Fatalf("ctrl+shift+h opens Search with replace: view=%v replace=%v", v, m.sr.showReplace)
	}

	press(m, "ctrl+,")

	if m.modal == nil || m.modal.title != "Settings" {
		t.Fatalf("ctrl+, opens settings: %+v", m.modal)
	}

	m.modal = nil
	press(m, "ctrl+1")

	if m.focus != onMain {
		t.Fatalf("ctrl+1 focuses the editor, got %d", m.focus)
	}

	press(m, "ctrl+0")

	if m.focus != 0 {
		t.Fatalf("ctrl+0 focuses the sidebar, got %d", m.focus)
	}

	press(m, "ctrl+j")

	if !m.termOpen() {
		t.Fatal("ctrl+j opens the terminal panel")
	}

	press(m, "ctrl+j")

	if !m.termOpen() || m.keyContext() != ctxTerminal {
		t.Fatal("ctrl+j inside the panel is the shell's line feed")
	}

	press(m, "ctrl+`")

	if m.termOpen() {
		t.Fatal("ctrl+` closes it from inside")
	}
}

func TestSessionColorsFallBackToTheTheme(t *testing.T) {
	m := testModel(t)

	m.dark, m.fg, m.bg = true, "", ""
	if fg, bg := m.sessionColors(); bg != "#1e1e1e" || fg != "#d4d4d4" {
		t.Fatalf("a dark pando hands a dark background: %s on %s", fg, bg)
	}

	m.dark = false
	if _, bg := m.sessionColors(); bg != "#ffffff" {
		t.Fatalf("a light one hands white: %s", bg)
	}
	// What the terminal actually answered always wins.
	m.fg, m.bg = "#abcdef", "#123456"
	if fg, bg := m.sessionColors(); fg != "#abcdef" || bg != "#123456" {
		t.Fatalf("the terminal's own colors win: %s on %s", fg, bg)
	}
}

// runCommand runs the command config.toml calls id, as a [keys] binding does.
func runCommand(t *testing.T, m *Model, id string) {
	t.Helper()

	for _, it := range m.commands() {
		if commandID(it.label) == id {
			it.run(m)
			return
		}
	}

	t.Fatalf("no command %s", id)
}

func hasCommand(m *Model, id string) bool {
	return slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == id })
}

// TestKeyCommands gives the actions that only had a key a command of their
// own, so the palette finds them and [keys] binds them.
func TestKeyCommands(t *testing.T) {
	m, _ := editorModel(t, "a.go", "one\ntwo\nthree\n")
	m.focus = 0
	runCommand(t, m, "view.focusEditor")

	if m.focus != onMain {
		t.Fatalf("focus editor: %d", m.focus)
	}

	runCommand(t, m, "view.focusSidebar")

	if m.focus != 0 {
		t.Fatalf("focus sidebar: %d", m.focus)
	}

	lines := func() string { return m.pv.text() }

	m.pv.setCursor(m, pos{0, 0})
	runCommand(t, m, "editor.duplicateLine")

	if got := lines(); got != "one\none\ntwo\nthree\n" || m.focus != onMain {
		t.Fatalf("duplicate: %q focus %d", got, m.focus)
	}

	if !hasCommand(m, "editor.copyLineUp") || !hasCommand(m, "editor.expandSelection") {
		t.Fatal("copy line, expand selection commands")
	}

	runCommand(t, m, "editor.deleteLine")
	runCommand(t, m, "editor.moveLineDown")

	if got := lines(); got != "one\nthree\ntwo\n" { // the duplicate took the cursor, as in VS Code
		t.Fatalf("delete, then move down: %q", got)
	}

	wrap := m.pv.wrap
	runCommand(t, m, "editor.toggleWordWrap")

	if m.pv.wrap == wrap {
		t.Fatal("toggle word wrap")
	}

	runCommand(t, m, "editor.triggerSuggest")

	if !m.pv.comp.on {
		t.Fatal("trigger suggest")
	}

	m.pv.closeComp()
	runCommand(t, m, "editor.find")

	if !m.pv.find.editing {
		t.Fatal("find")
	}

	press(m, "esc")
	runCommand(t, m, "editor.selectAll")
	runCommand(t, m, "editor.cut")

	if got := lines(); strings.TrimSpace(got) != "" {
		t.Fatalf("select all, cut: %q", got)
	}

	// [keys] comes before the built-in chords: ctrl+b can be unbound.
	m.st.Settings.Keys = map[string]string{"ctrl+b": ""}
	press(m, "ctrl+b")

	if m.hidden[0] || m.hidden[1] {
		t.Fatal("an unbound ctrl+b still folded the sidebars")
	}

	m.st.Settings.Keys = nil

	// Explorer's Collapse All has C; Source Control's commit variants are commands.
	m.focus = 0
	m.ex.expanded = map[string]bool{filepath.Join(m.ws, "src"): true}
	press(m, "C")

	if len(m.ex.expanded) != 0 || !hasCommand(m, "explorer.collapseAll") {
		t.Fatalf("C collapses the explorer: %v", m.ex.expanded)
	}

	g := gitModel(t)
	for _, id := range []string{"git.commitSync", "git.commitAmend", "git.collapseAll"} {
		if !hasCommand(g, id) {
			t.Fatalf("no %s", id)
		}
	}

	var help []string
	for _, it := range helpModal().items {
		help = append(help, ansi.Strip(it.label))
	}

	if !slices.Contains(help, "Search") {
		t.Fatalf("the keys help has no Search section: %q", help)
	}
}

func TestCloseOtherEditors(t *testing.T) {
	m := testModelSized(t, 100, 24)

	m.focus = onMain
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		p := filepath.Join(m.ws, f)
		mustWrite(t, p, f+"\n")
		fire(m, m.openFile(p))
	}

	send(m, keyMsg("alt+2"))
	press(m, "x") // b.txt has unsaved text
	send(m, keyMsg("alt+3"))

	if len(m.editors) != 3 || !strings.HasSuffix(m.pv.path, "c.txt") {
		t.Fatalf("editors=%d at %s", len(m.editors), m.pv.path)
	}

	runCommand(t, m, "view.closeOtherEditors")

	if len(m.editors) != 2 || m.edIdx != 1 || !strings.HasSuffix(m.editors[0].path, "b.txt") || !strings.HasSuffix(m.pv.path, "c.txt") {
		t.Fatalf("editors=%d active=%d at %s", len(m.editors), m.edIdx, m.pv.path)
	}

	runCommand(t, m, "view.previousEditor")

	if !strings.HasSuffix(m.pv.path, "b.txt") {
		t.Fatalf("previous editor: %s", m.pv.path)
	}
}

func TestTerminalCommands(t *testing.T) {
	m := testModelSized(t, 130, 30)
	for _, id := range []string{"t1", "t2", "t3"} {
		m.sessions = append(m.sessions, termSession(m.ws, id))
	}

	drainInputs(m)
	runCommand(t, m, "view.focusTerminal")

	if !m.termOpen() || m.focus != onPanel {
		t.Fatalf("focus terminal: open=%v focus=%d", m.termOpen(), m.focus)
	}

	first := m.tv.id
	runCommand(t, m, "view.nextTerminal")

	if m.tv.id == first || m.tv.id == "" {
		t.Fatalf("next terminal stayed on %q", m.tv.id)
	}

	runCommand(t, m, "view.previousTerminal")
	runCommand(t, m, "view.previousTerminal")

	if last := m.termSessions()[2].ID; m.tv.id != last {
		t.Fatalf("previous from the first terminal: %q, want the last, %q", m.tv.id, last)
	}

	if !hasCommand(m, "view.newTerminal") || !hasCommand(m, "view.killTerminal") {
		t.Fatal("new and kill terminal commands")
	}

	m.focus = 0
	runCommand(t, m, "view.focusTerminal")

	if m.focus != onPanel {
		t.Fatalf("focus an open terminal: %d", m.focus)
	}
}

// TestLastWorkspace reopens the last workspace when pando is started with no
// directory, opens the directory it is given, and remembers the one open.
func TestLastWorkspace(t *testing.T) {
	app := filepath.Join(t.TempDir(), "app")
	wss := []proto.Workspace{{Path: app}}

	st := proto.State{LastWorkspace: app}
	if ws, ok := lastWorkspace("", st, wss); !ok || ws != app {
		t.Fatalf("no directory: %q %v", ws, ok)
	}

	if _, ok := lastWorkspace(t.TempDir(), st, wss); ok {
		t.Fatal("a directory given, that one is added and opens")
	}

	if _, ok := lastWorkspace("", proto.State{LastWorkspace: "/gone"}, wss); ok {
		t.Fatal("a workspace that is gone")
	}

	if _, ok := lastWorkspace("", proto.State{}, wss); ok {
		t.Fatal("the first run has none to reopen")
	}

	g := testModel(t)
	if g.saveWorkspace() == nil || g.saveWorkspace() != nil {
		t.Fatal("the open workspace is sent once")
	}
}

// TestTerminalResize drags the bottom panel's title row, VS Code's sash: up
// grows it, the height is saved on release, and it stops short of the top.
func TestTerminalResize(t *testing.T) {
	m := testModelSized(t, 130, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drainInputs(m)
	press(m, "ctrl+j")
	rows, y := m.termRows(), m.mainH()
	_, c := m.layout()
	x := c.x + c.w - 8 // past the tabs, before the ✕
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y - 4, Button: tea.MouseLeft})

	if m.termRows() != rows+4 || m.mainH() != y-4 {
		t.Fatalf("dragged up 4: rows %d → %d", rows, m.termRows())
	}

	m.Update(tea.MouseReleaseMsg{X: x, Y: y - 4, Button: tea.MouseLeft})

	if m.drag != nil || m.st.Settings.TermH != rows+4 {
		t.Fatalf("release: drag %v, saved height %d", m.drag, m.st.Settings.TermH)
	}

	m.Update(tea.MouseClickMsg{X: x, Y: y - 4, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: 0, Button: tea.MouseLeft})

	if m.termRows() != m.panelH()-3 || m.st.Settings.TermH != m.panelH()-3 {
		t.Fatalf("dragged to the top: %d rows of %d", m.termRows(), m.panelH())
	}

	checkWidths(t, m)
}

// TestStatusProjects lights the status bar's buttons under the mouse and opens
// the project switcher from the project name.
func TestStatusProjects(t *testing.T) {
	m := gitModel(t)
	other, g0 := t.TempDir(), m.ws
	m.st.Projects = []string{m.ws, other}
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: other, Main: true})
	_, zones := m.statusLine(m.w)
	branch, project := zones[0], zones[1]

	m.mouseX, m.mouseY = branch.x+1, m.h-1
	if s, _ := m.statusLine(m.w); !strings.Contains(s, bgParams(pal.keycapBg)) {
		t.Fatal("the branch under the mouse is not lit")
	}

	m.mouseY = 0
	if s, _ := m.statusLine(m.w); strings.Contains(s, bgParams(pal.keycapBg)) {
		t.Fatal("lit without the mouse over it")
	}

	click(m, project.x+1, m.h-1, tea.MouseLeft)

	if m.modal == nil || m.modal.title != "Open Project" {
		t.Fatalf("clicking the project: %+v", m.modal)
	}

	rows := labels(m)
	if len(rows) != 3 || rows[2] != "+ Add Project…" || !slices.Contains(rows, filepath.Base(other)) {
		t.Fatalf("rows %q", rows)
	}

	choose(t, m, filepath.Base(other))

	if m.ws != other || m.modal != nil {
		t.Fatalf("switched to %s", m.ws)
	}
	// Agents selects the project's worktree, unfolded, as a click on it does.
	if r := m.ag.selected(m); r == nil || r.kind != agWorkspace || r.ws.Path != other {
		t.Fatalf("agents selection after the switch: %+v", r)
	}

	m.ag.collapsed = map[string]bool{g0: true}
	m.ag.l.sel = 0
	m.switchWorkspace(g0)

	if r := m.ag.selected(m); m.ag.collapsed[g0] || r == nil || r.ws.Path != g0 {
		t.Fatalf("a folded project unfolds to show its worktree: %+v", r)
	}
	// A session row picked in the tree stays picked when its workspace opens.
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s9", Workspace: other, Agent: "claude"}, Status: "idle"})
	tree := m.ag.rows(m)
	i := slices.IndexFunc(tree, func(r agRow) bool { return r.kind == agSession && r.s.ID == "s9" })
	m.ag.l.sel = i
	m.ag.activate(m, &tree[i])

	if m.ag.l.sel != i || m.ws != other {
		t.Fatalf("session click: sel %d want %d, ws %s", m.ag.l.sel, i, m.ws)
	}
	// A typed name picks the project, not Add Project….
	m.projectPicker()

	for _, r := range filepath.Base(g0) {
		press(m, string(r))
	}

	press(m, "enter")

	if m.modal != nil || m.ws != g0 {
		t.Fatalf("typed %s: ws %s, modal %+v", filepath.Base(g0), m.ws, m.modal)
	}

	m.projectPicker()
	m.modal.choose(m, len(m.modal.disp)-1)

	if m.modal == nil || m.modal.title != "Add project (directory)" {
		t.Fatalf("add project: %+v", m.modal)
	}
}

// TestDragByTitle picks a view up by its header title, not only its tab.
func TestDragByTitle(t *testing.T) {
	m := testModelSized(t, 120, 30)
	i := m.colOf(viewFiles)
	v, _ := m.viewOn(i)
	m.Update(tea.MouseClickMsg{X: m.colRect(i).x + 2, Y: m.barH(i), Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragTab || m.drag.v != v {
		t.Fatalf("pressing the title picks the view up: %+v", m.drag)
	}

	_, c := m.layout()
	inner := c.x + c.w + 2
	m.Update(tea.MouseMotionMsg{X: inner, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: inner, Y: 10, Button: tea.MouseLeft})

	if m.drag != nil || !m.cols()[m.colOf(v)].right {
		t.Fatalf("dropped on the right: left=%v right=%v", m.st.Settings.Left, m.st.Settings.Right)
	}
}

// TestProjectPromptComplete completes the directory typed into Add project, as
// fish does.
func TestProjectPromptComplete(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"alpha", "beta", ".hidden"} {
		mustMkdir(t, filepath.Join(dir, d))
	}

	mustWrite(t, filepath.Join(dir, "afile"), "")

	if got := completeDir(dir + "/a"); !slices.Equal(got, []string{dir + "/alpha/"}) {
		t.Fatalf("a: %q", got)
	}

	if got := completeDir(dir + "/"); !slices.Equal(got, []string{dir + "/alpha/", dir + "/beta/"}) {
		t.Fatalf("all: %q", got)
	}

	if got := completeDir(dir + "/."); !slices.Equal(got, []string{dir + "/.hidden/"}) {
		t.Fatalf("dotted: %q", got)
	}

	m := testModel(t)
	m.addProjectPrompt()
	m.modal.input.SetValue("")

	for _, r := range dir + "/b" {
		press(m, string(r))
	}

	if got := m.modal.input.CurrentSuggestion(); got != dir+"/beta/" {
		t.Fatalf("suggestion %q", got)
	}

	if !strings.Contains(ansi.Strip(m.modal.input.View()), "eta/") {
		t.Fatalf("the rest is not shown: %q", ansi.Strip(m.modal.input.View()))
	}

	press(m, "tab")

	if m.modal == nil || m.modal.input.Value() != dir+"/beta/" {
		t.Fatalf("tab took %+v", m.modal)
	}
}

// TestTerminalMenu is the Terminal panel's right click menu and its size to
// content width, which pans sideways.
func TestTerminalMenu(t *testing.T) {
	m := testModelSized(t, 130, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drain := func() []proto.InputParams {
		var out []proto.InputParams
		for len(m.inputs) > 0 {
			out = append(out, <-m.inputs)
		}

		return out
	}

	press(m, "ctrl+j")
	drain()

	_, c := m.layout()
	y := m.mainH() + 3
	m.Update(tea.MouseClickMsg{X: c.x + 5, Y: y, Button: tea.MouseRight})

	if m.modal == nil || !slices.Equal(labels(m), []string{"Copy All", "Paste", "Clear", "Rename…", "Kill Terminal…", "Toggle Size to Content Width"}) {
		t.Fatalf("menu %+v", m.modal)
	}

	choose(t, m, "Clear")

	if in := drain(); len(in) != 1 || in[0].ID != "t1" || in[0].Text != "\x0c" {
		t.Fatalf("clear sent %+v", in)
	}
	// ⌥z: the shell runs wider than the panel, and a sideways wheel pans it.
	m.focus = onPanel
	press(m, "alt+z")

	if !m.tv.wide || m.termCols() != wideCols {
		t.Fatalf("alt+z: wide=%v cols=%d", m.tv.wide, m.termCols())
	}

	m.Update(tea.MouseWheelMsg{X: c.x + 5, Y: y, Button: tea.MouseWheelDown, Mod: tea.ModShift})

	if m.tv.left != 8 {
		t.Fatalf("shift+wheel: left %d", m.tv.left)
	}

	m.tv.term.id, m.tv.left = "t1", 100
	m.tv.scr = proto.Screen{Lines: []string{strings.Repeat("a", 100) + "XYZ" + strings.Repeat("b", 100)}}

	w, h := m.termBody()
	if got := m.termScreen(w, h)[0]; !strings.HasPrefix(got, "XYZbbb") {
		t.Fatalf("panned line %q", got)
	}

	press(m, "alt+z")

	if m.tv.wide || m.tv.left != 0 || m.termCols() != w-1 { // the scrollbar keeps its column
		t.Fatal("alt+z again fits the panel")
	}
	// A paste goes to the panel's shell when it has the keyboard.
	drain()
	m.Update(tea.PasteMsg{Content: "hi"})

	if in := drain(); len(in) != 1 || in[0].ID != "t1" || in[0].Paste != "hi" {
		t.Fatalf("paste sent %+v", in)
	}
}

// TestSessionNames names tabs by what runs in them, Claude Code's title
// included, and by a rename from the tab's right click or R in Agents.
func TestSessionNames(t *testing.T) {
	sh := proto.Session{SessionSpec: proto.SessionSpec{ID: "s1", Agent: "shell", Cmd: []string{"bash"}}, Program: "bash", Title: "~/c/shop"}
	claude := sh
	claude.Program, claude.Title = "claude", "✳ Add a Count method"
	bare := claude
	bare.Title = ""
	named := claude
	named.Name = "deploy"

	panel := proto.Session{SessionSpec: proto.SessionSpec{Agent: termAgent, Cmd: []string{"/bin/zsh"}}, Program: "zsh"}
	for want, s := range map[string]proto.Session{"shell": sh, "Add a Count method": claude, "claude": bare, "deploy": named, "zsh": panel} {
		if got := sessionName(s); got != want {
			t.Errorf("sessionName(%+v) = %q, want %q", s, got, want)
		}
	}

	if titleAfter(claude) != "" || titleAfter(sh) != " · ~/c/shop" {
		t.Fatalf("titles after names: %q %q", titleAfter(claude), titleAfter(sh))
	}

	m := testModelSized(t, 120, 30)
	claude.Workspace = m.ws
	m.sessions = []proto.Session{claude}
	m.focus = onMain
	m.switchSession("s1")

	tabs := m.sessionTabs(m.mainW())
	if !strings.Contains(tabs[0].label, "Add a Count method") {
		t.Fatalf("tab %q", tabs[0].label)
	}

	m.Update(tea.MouseClickMsg{X: m.mainX() + tabs[0].x + 1, Y: 0, Button: tea.MouseRight})

	if m.modal == nil || !slices.Equal(labels(m), []string{"Rename…", "Kill Session…"}) {
		t.Fatalf("tab menu %+v", m.modal)
	}

	m.modal.choose(m, 0)

	for _, r := range "deploy" {
		press(m, string(r))
	}

	press(m, "enter")

	if s := m.session("s1"); s.Name != "deploy" || !strings.Contains(m.sessionTabs(m.mainW())[0].label, "deploy") {
		t.Fatalf("renamed: %+v", s)
	}

	rows := m.ag.rows(m)
	m.ag.l.sel = slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession })
	m.ag.key(m, keyMsg("R"))

	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Rename session") || m.modal.input.Value() != "deploy" {
		t.Fatalf("R in Agents: %+v", m.modal)
	}
}

// TestWorkspaceBranchRefresh reloads the worktree list when the open worktree
// checked out another branch, once per branch.
func TestWorkspaceBranchRefresh(t *testing.T) {
	m := gitModel(t)
	m.wss[0].Branch = "feat/old"
	msg := gitMsg{ws: m.ws, repos: []string{m.ws}, status: map[string]git.Status{m.ws: {Branch: "main"}}}
	m.Update(msg)

	if m.branchSeen != "main" {
		t.Fatalf("branch seen %q", m.branchSeen)
	}

	m.wss[0].Branch = "main"
	m.branchSeen = ""
	m.Update(msg)

	if m.branchSeen != "" {
		t.Fatal("a list that already names the branch was reloaded")
	}
}

// TestActivityBarAir centers Nerd Font icons with two cells of air each side,
// caps the active chip above and below into a button, fills the one under the
// mouse the same way, and keeps the gear clickable at its width.
func TestActivityBarAir(t *testing.T) {
	m := testModelSized(t, 120, 30)

	applyLook("vscode", true, "nerd", nil)
	defer applyLook("vscode", true, "ascii", nil)

	tabs := m.tabs(0)
	if len(tabs) < 2 || !strings.HasPrefix(tabs[0].label, "  ") || !strings.HasSuffix(tabs[0].label, "  ") || tabs[0].w != 5 || tabs[1].x != tabs[0].x+tabs[0].w+1 {
		t.Fatalf("chips %+v", tabs)
	}

	active, _ := m.viewOn(0)

	other := tabs[1]
	if other.v == active {
		other = tabs[0]
	}

	rc := m.colRect(0)
	at := slices.IndexFunc(tabs, func(t tab) bool { return t.v == active })
	under := strings.Repeat("━", tabs[at].chipW)

	bar := m.activityBar(0, rc.w)
	if len(bar) != actH || !strings.Contains(bar[0], fgParams(pal.headerAccent)) ||
		strings.TrimSpace(ansi.Strip(bar[1])) != under {
		t.Fatalf("the active chip is underlined, nothing behind it: %q", bar)
	}
	// The one under the mouse takes a neutral line of its own.
	m.Update(tea.MouseMotionMsg{X: rc.x + other.x + 1, Y: 0})

	if bar = m.activityBar(0, rc.w); strings.Count(ansi.Strip(bar[1]), under) != 2 ||
		!strings.Contains(bar[1], fgParams(pal.inputBorder)) {
		t.Fatalf("the chip under the mouse is marked, not filled: %q", bar)
	}

	click(m, rc.x+rc.w-ansi.StringWidth(gearLabel()), 0, tea.MouseLeft)

	if m.modal == nil || m.modal.title != "Settings" {
		t.Fatalf("gear at its left edge: %+v", m.modal)
	}
	// A narrow sidebar tightens the chips rather than run into the gear.
	m.modal = nil
	m.setColWidth(0, 22)

	tabs = m.tabs(0)
	if last := tabs[len(tabs)-1]; tabs[0].w != 3 || last.x+last.w > m.colRect(0).w-ansi.StringWidth(gearLabel()) {
		t.Fatalf("narrow chips %+v in %d", tabs, m.colRect(0).w)
	}
}

// TestSessionDock drags the agent session by its title beside the editor, so a
// file shows next to it; widened over the editor it takes the editor area
// back; Close and a second click in Agents take it off screen, running on.
func TestSessionDock(t *testing.T) {
	m := testModelSized(t, 140, 30)
	m.sessions = []proto.Session{{SessionSpec: proto.SessionSpec{ID: "s1", Workspace: m.ws, Agent: "claude", Cmd: []string{"claude"}}, Status: "idle"}}
	f := filepath.Join(m.ws, "a.txt")
	mustWrite(t, f, "hello docked\n")
	fire(m, m.openFile(f))
	m.focus = onMain
	m.switchSession("s1")

	if !m.showsSession() || m.showsPreview() {
		t.Fatal("the session starts over the file")
	}

	_, c := m.layout()
	title := m.stripH() // the header under the session's tabs
	m.Update(tea.MouseClickMsg{X: c.x + 5, Y: title, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: c.x + c.w - 2, Y: 10, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.drop == nil || m.drag.drop.label != "Dock Right" {
		t.Fatalf("drop target %+v", m.drag)
	}

	m.Update(tea.MouseReleaseMsg{X: c.x + c.w - 2, Y: 10, Button: tea.MouseLeft})

	i := m.colOf(viewSession)
	if i < 0 || m.side(i) != 1 || !m.showsPreview() || m.showsSession() || !m.shown(viewSession) || !m.sessFocused() {
		t.Fatalf("docked right: col %d, preview %v, session over main %v", i, m.showsPreview(), m.showsSession())
	}

	if out := checkWidths(t, m); !strings.Contains(out, "hello docked") {
		t.Fatalf("the file shows beside the session:\n%s", out)
	}
	// Docked on the other side, the session and the editor keep their widths.
	_, ed := m.layout()
	m.splitTo(viewSession, 0)

	if j := m.colOf(viewSession); j < 0 || m.side(j) != 0 {
		t.Fatalf("docked left: col %d", j)
	}

	if _, c := m.layout(); c.w != ed.w {
		t.Fatalf("editor width %d after moving the session, was %d", c.w, ed.w)
	}

	m.splitTo(viewSession, 1)
	i = m.colOf(viewSession)
	// Widening it over the editor gives it the editor area.
	rc := m.colRect(i)
	_, c = m.layout()
	m.Update(tea.MouseClickMsg{X: rc.x - 1, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: c.x + 5, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: c.x + 5, Y: 10, Button: tea.MouseLeft})

	if m.sessDocked() || !m.showsSession() {
		t.Fatalf("stretched over the editor: docked %v, over main %v", m.sessDocked(), m.showsSession())
	}
	// Its title's right click: Close keeps the session running.
	_, c = m.layout()
	m.Update(tea.MouseClickMsg{X: c.x + 5, Y: m.stripH(), Button: tea.MouseRight})

	if m.modal == nil || labels(m)[0] != "Close (keeps running)" {
		t.Fatalf("title menu %+v", m.modal)
	}

	m.modal.choose(m, 0)

	if m.sess != "" || len(m.sessions) != 1 || !m.showsPreview() {
		t.Fatalf("closed: sess %q, sessions %d", m.sess, len(m.sessions))
	}
	// Agents: a click shows it, a second click on it puts it away again.
	rows := m.ag.rows(m)
	k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession })
	m.ag.activate(m, &rows[k])

	if m.sess != "s1" || !m.showsSession() {
		t.Fatal("a click in Agents shows the session")
	}

	m.ag.activate(m, &rows[k])

	if m.sess != "" {
		t.Fatal("a second click puts it away")
	}
	// A framed layout carries the title on the frame's top edge.
	m.st.Settings.Borders = true
	m.resize()
	m.ag.activate(m, &rows[k])
	_, c = m.layout()
	m.Update(tea.MouseClickMsg{X: c.x + 5, Y: 0, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.v != viewSession {
		t.Fatalf("frame title drag %+v", m.drag)
	}
}

// Closing a session tab hands focus to a neighbour; only the last one closing
// leaves the strip empty.
func TestClosingTabKeepsAnotherFocused(t *testing.T) {
	m := testModel(t)
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "idle"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s3", Workspace: m.ws, Agent: "claude"}, Status: "idle"})
	m.sess = "s2"
	send(m, sessionsMsg([]proto.Session{m.sessions[0], m.sessions[2]})) // s2 killed

	if m.sess != "s1" {
		t.Fatalf("closing s2 focuses the tab left of it, got %q", m.sess)
	}

	send(m, sessionsMsg([]proto.Session{m.sessions[1]})) // s1 killed, s3 left

	if m.sess != "s3" {
		t.Fatalf("closing the first tab falls through to the right, got %q", m.sess)
	}

	send(m, sessionsMsg(nil))

	if m.sess != "" {
		t.Fatalf("the last tab closing leaves no session, got %q", m.sess)
	}
}

// A session opens in a column of its own beside the editor, the default;
// moving it to the editor area is remembered as session_position.
func TestSessionOpensDocked(t *testing.T) {
	m := testModelSized(t, 140, 30)
	m.st.Settings.SessPos, m.sess = "", "" // the shipped default, nothing on screen
	rows := m.ag.rows(m)
	k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession })
	m.ag.activate(m, &rows[k])

	i := m.colOf(viewSession)
	if i < 0 || m.side(i) != 1 || m.showsSession() {
		t.Fatalf("docked right: col %d, over main %v", i, m.showsSession())
	}

	if _, c := m.layout(); m.colRect(i).w < c.w-2 || m.colRect(i).w > c.w+2 {
		t.Fatalf("half the editor area: session %d, editor %d", m.colRect(i).w, c.w)
	}

	checkWidths(t, m)
	m.undockSession()

	if m.sessDocked() || !m.showsSession() || m.st.Settings.SessPos != "editor" {
		t.Fatalf("moved to the editor area: docked %v, session_position %q", m.sessDocked(), m.st.Settings.SessPos)
	}

	m.splitTo(viewSession, 0)

	if m.side(m.colOf(viewSession)) != 0 || m.st.Settings.SessPos != "left" {
		t.Fatalf("docked left: session_position %q", m.st.Settings.SessPos)
	}
	// Stretched over the editor again, opening a file docks it back on its side.
	m.undockSession()
	fire(m, m.openFile(filepath.Join(m.ws, "README.md")))
	fire(m, m.pv.load(m))

	j := m.colOf(viewSession)
	if j < 0 || m.side(j) != 0 || m.showsSession() || !m.showsPreview() {
		t.Fatalf("a file docks it beside the editor: col %d, over main %v", j, m.showsSession())
	}

	if out := checkWidths(t, m); !strings.Contains(out, "# hi") {
		t.Fatalf("the file shows beside the session:\n%s", out)
	}
}

// The session's column follows Spaces to the other sidebar, unless it has the
// editor area to itself.
func TestSessionFollowsSpaces(t *testing.T) {
	m := testModelSized(t, 140, 30)
	m.st.Settings.SessPos, m.sess = "", "" // the shipped default, nothing on screen
	rows := m.ag.rows(m)
	m.ag.activate(m, &rows[slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession })])

	if m.side(m.colOf(viewSession)) != 1 || m.side(m.colOf(viewAgents)) != 0 {
		t.Fatalf("session right, Spaces left to start: %d %d", m.side(m.colOf(viewSession)), m.side(m.colOf(viewAgents)))
	}

	m.moveView(viewAgents, 0) // Spaces stays left, the session joins it there

	if m.side(m.colOf(viewSession)) != 0 || m.st.Settings.SessPos != "left" {
		t.Fatalf("the session follows to the left: %d, position %q", m.side(m.colOf(viewSession)), m.st.Settings.SessPos)
	}

	m.moveView(viewAgents, 1)

	i, j := m.colOf(viewSession), m.colOf(viewAgents)
	if m.side(i) != 1 || m.side(j) != 1 || i > j || m.st.Settings.SessPos != "right" {
		t.Fatalf("both right, the session next to the editor: session %d, Spaces %d, position %q", i, j, m.st.Settings.SessPos)
	}

	if rc := m.colRect(i); rc.w < 20 {
		t.Fatalf("the session keeps its width: %+v", rc)
	}

	checkWidths(t, m)
	m.moveView(viewAgents, 0)

	if m.side(m.colOf(viewSession)) != 0 || m.st.Settings.SessPos != "left" {
		t.Fatalf("both left: session %d, position %q", m.side(m.colOf(viewSession)), m.st.Settings.SessPos)
	}

	checkWidths(t, m)
	// Over the editor area it has no column, so nothing follows.
	m.undockSession()
	m.moveView(viewAgents, 1)

	if m.sessDocked() || m.st.Settings.SessPos != "editor" {
		t.Fatalf("full screen stays full screen: docked %v, position %q", m.sessDocked(), m.st.Settings.SessPos)
	}
}

// The empty main area names what is missing there. A session docked to a side
// column is on screen, so claiming the workspace has none would be a lie.
func TestWelcomeNamesWhatIsMissing(t *testing.T) {
	m := testModelSized(t, 140, 30)
	m.st.Settings.SessPos, m.sess = "", ""

	if out := checkWidths(t, m); !strings.Contains(out, "No session in this workspace.") {
		t.Fatal("no session: the main area should say so")
	}

	rows := m.ag.rows(m)
	k := slices.IndexFunc(rows, func(r agRow) bool { return r.kind == agSession })
	m.ag.activate(m, &rows[k])

	if !m.sessDocked() || m.showsSession() {
		t.Fatalf("want the session in a column of its own: docked %v, over main %v", m.sessDocked(), m.showsSession())
	}

	out := checkWidths(t, m)
	if strings.Contains(out, "No session in this workspace.") {
		t.Error("the session is on screen beside the editor, so main must not deny it")
	}

	if !strings.Contains(out, "No editor open.") {
		t.Error("main holds no editor: say that instead")
	}
}

// ⌃` and ⌃space are one byte on a terminal that does not disambiguate keys,
// and in an editor that byte belongs to the suggestions. Say so once, so the
// panel not opening is not a silent mystery.
func TestAmbiguousCtrlSpaceHintsTheTerminalKey(t *testing.T) {
	m, _ := editorModel(t, "a.go", "package a\n\nfunc A() {}\n")

	if !m.ambiguous {
		t.Fatal("a terminal that never answered is ambiguous until it does")
	}

	press(m, "ctrl+space")

	if !strings.Contains(m.msg, "⌃j") {
		t.Errorf("first ⌃space said %q, want the ⌃j hint", m.msg)
	}

	m.msg = ""
	press(m, "ctrl+space")

	if m.msg != "" {
		t.Errorf("the hint repeated: %q", m.msg)
	}
	// A terminal that answers gets no hint at all: ⌃` reaches the panel there.
	m2, _ := editorModel(t, "b.go", "package b\n")
	m2.Update(tea.KeyboardEnhancementsMsg{})
	m2.ambiguous = false
	press(m2, "ctrl+space")

	if m2.msg != "" {
		t.Errorf("a disambiguating terminal was hinted at: %q", m2.msg)
	}
}

func TestTerminalPanelAttachesAtStart(t *testing.T) {
	// A panel saved open showed its tab strip over an empty body until it was
	// closed and opened again: nothing pointed it at the shell it had.
	root := t.TempDir()
	st := proto.State{Settings: proto.Settings{Width: 30, Icons: "ascii", TermOpen: true}, Projects: []string{root}}
	wss := []proto.Workspace{{Path: root, Project: root, Branch: "main", Main: true}}
	m := New(st, wss, []proto.Session{termSession(root, "t1")}, root, nil)

	if m.tv.id != "t1" {
		t.Fatalf("an open panel starts on the shell it has, got %q", m.tv.id)
	}

	if m.ensureTerm() != nil {
		t.Fatal("a panel with a shell starts no other")
	}

	m = New(st, wss, nil, root, nil)
	if m.tv.id != "" || m.ensureTerm() == nil {
		t.Fatalf("an open panel without a shell starts one, id=%q", m.tv.id)
	}

	st.Settings.TermOpen = false
	if m = New(st, wss, nil, root, nil); m.ensureTerm() != nil {
		t.Fatal("a shut panel starts nothing")
	}
}

func TestTerminalTabsBelongToTheSession(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	owned := func(id, parent string) proto.Session {
		s := termSession(m.ws, id)
		s.Parent = parent

		return s
	}

	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "shell"}, Status: "idle"},
		owned("t0", ""), owned("t1", "s1"), owned("t2", "s2"))
	m.st.Settings.TermOpen = true

	ids := func() []string {
		var out []string
		for _, s := range m.termSessions() {
			out = append(out, s.ID)
		}

		return out
	}

	m.Update(focusSessionMsg("s1"))

	if got := ids(); !slices.Equal(got, []string{"t1"}) || m.tv.id != "t1" {
		t.Fatalf("s1 in view: tabs %v, panel on %q", got, m.tv.id)
	}

	m.Update(focusSessionMsg("s2"))

	if got := ids(); !slices.Equal(got, []string{"t2"}) || m.tv.id != "t2" {
		t.Fatalf("s2 in view: tabs %v, panel on %q", got, m.tv.id)
	}
	// Another session's shell, opened by a script, does not take the panel.
	m.Update(newSessionMsg(owned("t3", "s1")))

	if m.tv.id != "t2" {
		t.Fatalf("a shell of s1 took the panel while s2 is in view: %q", m.tv.id)
	}

	m.sess = ""
	m.attachTerm()

	if !slices.Equal(ids(), []string{"t0"}) || m.tv.id != "t0" {
		t.Fatalf("no session in view: tabs %v, panel on %q", ids(), m.tv.id)
	}
	// A shell opened now belongs to the session in view.
	m.sess = "s2"
	if cmd := m.newTerm(); cmd == nil {
		t.Fatal("newTerm returns the spawn")
	}
}

func TestTerminalDragSelectsAndCopies(t *testing.T) {
	m := testModelSized(t, 120, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drainInputs(m)
	press(m, "ctrl+j")

	tv := &m.tv
	tv.term.id, tv.scr = "t1", proto.Screen{Lines: []string{"\x1b[31mhello world\x1b[m", "second line", "third"}}
	x, y := m.mainX(), m.mainH()+1 // the panel's first screen row

	m.Update(tea.MouseClickMsg{X: x + 6, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})

	if got := tv.selText(); got != "world\nsec" {
		t.Fatalf("selected %q", got)
	}

	if a, b := tv.selRange(0); a != 6 || b != wideCols {
		t.Fatalf("first row selects to its end: [%d, %d)", a, b)
	}

	if !strings.Contains(ansi.Strip(tv.mark(1, "second line")), "second line") || !strings.Contains(tv.mark(1, "second line"), "\x1b[7msec\x1b[0m") {
		t.Fatalf("row 1 marked %q", tv.mark(1, "second line"))
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})

	if tv.selecting || !tv.hasSel || cmd == nil {
		t.Fatalf("release copies and keeps the mark: selecting=%v sel=%v cmd=%v", tv.selecting, tv.hasSel, cmd != nil)
	}

	checkWidths(t, m)
	press(m, "a")

	if tv.hasSel {
		t.Fatal("a key clears the selection")
	}
	// A plain click selects nothing.
	click(m, x+1, y, tea.MouseLeft)

	if tv.hasSel {
		t.Fatal("a click without a drag leaves no selection")
	}
}

func TestSessionDragSelects(t *testing.T) {
	m := testModelSized(t, 120, 30)
	drainInputs(m)
	m.Update(focusSessionMsg("s1"))

	m.term.id, m.term.scr = "s1", proto.Screen{Lines: []string{"agent says hi"}}
	_, c := m.layout()
	y := m.stripH() + 1 // the session's title row, then its screen

	m.Update(tea.MouseClickMsg{X: c.x + 6, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: c.x + 9, Y: y, Button: tea.MouseLeft})

	if got := m.term.selText(); got != "says" {
		t.Fatalf("selected %q", got)
	}

	if !strings.Contains(checkWidths(t, m), "agent says hi") || !strings.Contains(m.View().Content, "\x1b[7msays\x1b[0m") {
		t.Fatal("the session over the editor draws the selection")
	}
}

// TestSessionSelectionScrolls keeps a selection on its text, as the editor
// does: scrolling back or new output moves it with the lines it covers, and a
// selection reaching past the screen is cut from the whole scrollback.
func TestSessionSelectionScrolls(t *testing.T) {
	m := testModelSized(t, 120, 30)
	drainInputs(m)
	m.Update(focusSessionMsg("s1"))

	screen := func(top, h int) []string { // lines top.. of a transcript "line N"
		out := make([]string, h)
		for i := range out {
			out[i] = fmt.Sprintf("line %d", top+i)
		}

		return out
	}

	h := m.sessH()
	m.term.id, m.term.scr = "s1", proto.Screen{Lines: screen(100, h), Scrollback: 100}
	_, c := m.layout()
	y := m.stripH() + 1

	m.Update(tea.MouseClickMsg{X: c.x, Y: y + 2, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: c.x + 7, Y: y + 3, Button: tea.MouseLeft})

	if got := m.term.selText(); got != "line 102\nline 103" {
		t.Fatalf("selected %q", got)
	}

	// Scrolled back three lines, the text sits three rows lower and so does the mark.
	m.term.scroll, m.term.scr.Lines = 3, screen(97, h)

	if a, b := m.term.selRange(2); b > a {
		t.Fatalf("the mark stayed on its screen row: [%d, %d)", a, b)
	}

	if a, b := m.term.selRange(5); b <= a || m.term.selText() != "line 102\nline 103" {
		t.Fatalf("the mark follows its text: row 5 [%d, %d), text %q", a, b, m.term.selText())
	}

	// Output pushes the screen up a line at the bottom: the mark goes up with it.
	m.term.scroll, m.term.scr = 0, proto.Screen{Lines: screen(101, h), Scrollback: 101}

	if a, b := m.term.selRange(1); b <= a || m.term.selText() != "line 102\nline 103" {
		t.Fatalf("new output carries the mark: row 1 [%d, %d), text %q", a, b, m.term.selText())
	}

	// Extended past the top of the screen, the copy reads what scrolled away.
	m.term.sel[0] = [2]int{0, 50}
	all := screen(0, 101+h)

	text := func(n int) (string, bool) {
		if n >= len(all) {
			return "", false
		}

		return all[n], true
	}

	if got := m.term.cutSel(text); !strings.HasPrefix(got, "line 50\n") || !strings.HasSuffix(got, "line 103") {
		t.Fatalf("the copy spans the scrollback: %q", got)
	}
}

// TestSpacesCloseOrDelete tells a project's own checkout from its linked
// worktrees: the checkout only closes the project, a worktree is deleted,
// its sessions killed first.
func TestSpacesCloseOrDelete(t *testing.T) {
	m := testModel(t)
	wt := t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: wt, Project: m.ws, Branch: "feat"})
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "w1", Workspace: wt, Agent: "shell"}, Status: "idle"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "t1", Workspace: wt, Agent: termAgent, Parent: "w1"}, Status: "idle"})
	press(m, "3")

	out := checkWidths(t, m)
	for _, want := range []string{"   ⌂ main", "   ⑂ feat"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Spaces lacks %q:\n%s", want, out)
		}
	}

	var project, main, linked agRow

	for _, r := range m.ag.rows(m) {
		switch {
		case r.kind == agProject:
			project = r
		case r.kind == agWorkspace && r.ws.Main:
			main = r
		case r.kind == agWorkspace:
			linked = r
		}
	}

	for _, c := range []struct {
		r            agRow
		label, title string
	}{
		{project, "Close Project…", "Close project " + filepath.Base(m.ws) + "? It leaves Spaces, nothing on disk changes. Its 2 sessions are killed."},
		{main, "Close Project…", "Close project " + filepath.Base(m.ws) + "? It leaves Spaces, nothing on disk changes. Its 2 sessions are killed."},
		{linked, "Delete Worktree…", "Delete worktree feat? The checkout " + wt + " is removed, the branch stays. Its session is killed."},
	} {
		if got := removeLabel(&c.r); got != c.label {
			t.Errorf("x on %s is %q, want %q", agLabel(c.r), got, c.label)
		}

		m.modal = nil
		m.ag.remove(m, &c.r)

		if m.modal == nil || m.modal.title != c.title || m.modal.items[0].label != strings.TrimSuffix(c.label, "…") {
			t.Errorf("x on %s asks %+v, want %q", agLabel(c.r), m.modal, c.title)
		}
	}

	// The Terminal panel's shell goes with its session, not on its own; one
	// opened there beside a session of another workspace is killed itself.
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "t2", Workspace: wt, Agent: termAgent, Parent: "s1"}, Status: "idle"})

	if got := m.sessionsIn(wt); !slices.Equal(got, []string{"w1", "t2"}) {
		t.Fatalf("sessions to kill in the worktree = %v", got)
	}
}

// TestSessionTabs is herdr's workspace and its tabs: every session in the
// Spaces tree has tabs of its own, which its strip shows and [ ] cycle. A
// tab is not a session of the tree; the session carries its tabs' state,
// going back to it lands on the tab it was on, and its Terminal panel is
// shared by its tabs.
func TestSessionTabs(t *testing.T) {
	m := testModel(t)
	drainInputs(m)

	other := t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: other, Project: m.ws, Branch: "feat"})
	m.sessions = append(m.sessions,
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "shell"}, Status: "idle"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "t1", Workspace: m.ws, Agent: tabAgent, Parent: "s1", Cmd: []string{"/bin/fish"}}, Status: "blocked"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "p1", Workspace: m.ws, Agent: termAgent, Parent: "s1"}, Status: "idle"},
		proto.Session{SessionSpec: proto.SessionSpec{ID: "o1", Workspace: other, Agent: "shell"}, Status: "idle"})
	m.Update(focusSessionMsg("s1"))

	ids := func(ss []proto.Session) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}

		return out
	}

	if got := ids(m.spaceSessions()); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Fatalf("the space lists its sessions, not their tabs: %v", got)
	}

	tabs := m.sessionTabs(100)
	if len(tabs) != 3 || tabs[0].id != "s1" || tabs[1].id != "t1" || !tabs[2].plus || !strings.Contains(tabs[1].label, "fish") {
		t.Fatalf("the strip holds s1's own tabs and +: %+v", tabs)
	}

	// The tree shows s1 once, carrying its blocked tab's state.
	m.showView(viewAgents)
	checkWidths(t, m)

	var tree []string

	for _, r := range m.ag.rows(m) {
		if r.kind == agSession {
			tree = append(tree, r.s.ID)
		}
	}

	lines := strings.Join(m.ag.lines(m, 30, 20), "\n")
	if !slices.Equal(tree, []string{"s1", "s2", "o1"}) || !strings.Contains(ansi.Strip(lines), "├─ × shell") {
		t.Fatalf("Spaces tree %v:\n%s", tree, ansi.Strip(lines))
	}

	m.cycleSession(1)

	if m.sess != "t1" || m.rootOf(m.sess) != "s1" {
		t.Fatalf("] goes to s1's next tab: %q", m.sess)
	}

	// The Terminal panel is the session's, whichever of its tabs is shown.
	if got := ids(m.termSessions()); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("s1's panel from its tab: %v", got)
	}

	m.cycleSession(1)

	if m.sess != "s1" {
		t.Fatalf("] wraps inside the session, got %q", m.sess)
	}

	m.cycleSession(-1)
	m.Update(focusSessionMsg("s2"))

	if got := m.lastTabOf("s1"); got != "t1" {
		t.Fatalf("back to s1 lands on the tab it was on: %q", got)
	}

	// Closing a tab hands the strip to its neighbour in the session.
	m.Update(focusSessionMsg("t1"))
	send(m, sessionsMsg(slices.DeleteFunc(slices.Clone(m.sessions), func(s proto.Session) bool { return s.ID == "t1" })))

	if m.sess != "s1" {
		t.Fatalf("the closed tab handed over to %q", m.sess)
	}

	m.Update(focusSessionMsg("o1"))

	if m.ws != other || len(m.sessionTabs(100)) != 2 {
		t.Fatalf("the other space's session has its own tabs only: ws=%q tabs=%d", m.ws, len(m.sessionTabs(100)))
	}
}

// TestSashHover lights a divider once the pointer has rested on it, VS
// Code's sash.hoverBorder after its hover delay, in a shade apart from the
// accent it drags in: a column's edge with and without panel borders, and
// the Terminal panel's title row.
func TestSashHover(t *testing.T) {
	for _, borders := range []bool{false, true} {
		m := testModelSized(t, 130, 30)
		m.sessions = append(m.sessions, termSession(m.ws, "t1"))
		drainInputs(m)
		m.st.Settings.Borders = borders
		m.resize()

		cs, _ := m.layout()
		x, y := cs[0].x+cs[0].w, 5 // the gap on the Explorer's editor side
		hover := fgParams(pal.sashHover)

		if _, cmd := m.Update(tea.MouseMotionMsg{X: x + m.bord(), Y: y + m.bord()}); m.sashAt != 0 || cmd == nil {
			t.Fatalf("borders %v: the pointer on the divider is noted and waits: sash %d", borders, m.sashAt)
		}

		if strings.Contains(m.View().Content, hover) {
			t.Fatalf("borders %v: a divider does not light before the delay", borders)
		}

		m.sashSince = time.Now().Add(-sashDelay)

		if view := m.View().Content; !strings.Contains(view, hover) || !strings.Contains(ansi.Strip(view), "┃") {
			t.Fatalf("borders %v: a divider the pointer rests on lights", borders)
		}

		m.Update(tea.MouseClickMsg{X: x + m.bord(), Y: y + m.bord(), Button: tea.MouseLeft})

		if st, ok := m.sashStyle(0); !ok || st.GetForeground() != pal.accent || strings.Contains(m.View().Content, hover) {
			t.Fatalf("borders %v: dragged, the divider is the accent, not the hover shade", borders)
		}

		m.Update(tea.MouseReleaseMsg{X: x + m.bord(), Y: y + m.bord(), Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: x + 10 + m.bord(), Y: y + m.bord()})

		if m.sashAt != noSash || strings.Contains(m.View().Content, hover) {
			t.Fatalf("borders %v: off the divider it goes out", borders)
		}

		checkWidths(t, m)
	}

	m := testModelSized(t, 130, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drainInputs(m)
	press(m, "ctrl+j")

	_, c := m.layout()
	x, y := c.x+c.w-8, m.mainH() // past the tabs, before the ✕
	m.Update(tea.MouseMotionMsg{X: x + m.bord(), Y: y + m.bord()})

	if m.sashAt != termSash {
		t.Fatalf("the Terminal's title row is a sash: %d", m.sashAt)
	}

	m.sashSince = time.Now().Add(-sashDelay)

	if head := ansi.Strip(m.termPanelLines(c.w, m.termRows())[0]); !strings.Contains(head, "━") {
		t.Fatalf("the rested-on title row draws a rule: %q", head)
	}

	checkWidths(t, m)
}

// TestTerminalPanelPerWorkspace shuts the panel in one worktree and finds it
// still open in the other, on the shell it showed there; a worktree that
// never had it takes the last state set.
func TestTerminalPanelPerWorkspace(t *testing.T) {
	m := testModel(t)
	root, wt, fresh := m.ws, t.TempDir(), t.TempDir()
	m.wss = append(m.wss, proto.Workspace{Path: wt, Project: root, Branch: "feat"}, proto.Workspace{Path: fresh, Project: root, Branch: "new"})
	m.sessions = []proto.Session{termSession(root, "t1"), termSession(root, "t2"), termSession(wt, "t3")}

	m.toggleTerminal()
	m.cycleTerm(1)

	if !m.termOpen() || m.tv.id != "t2" {
		t.Fatalf("opened on the second shell: open %v, on %q", m.termOpen(), m.tv.id)
	}

	m.switchWorkspace(wt)

	if !m.termOpen() || m.tv.id != "t3" {
		t.Fatalf("a worktree without a panel of its own takes the last state: open %v, on %q", m.termOpen(), m.tv.id)
	}

	m.toggleTerminal()
	m.switchWorkspace(root)

	if !m.termOpen() || m.tv.id != "t2" {
		t.Fatalf("shut elsewhere, the panel stays open here, on its shell: open %v, on %q", m.termOpen(), m.tv.id)
	}

	m.switchWorkspace(wt)

	if m.termOpen() {
		t.Fatal("the worktree it was shut in keeps it shut")
	}

	m.switchWorkspace(fresh)

	if m.termOpen() {
		t.Fatal("a worktree never visited follows the last state set: shut")
	}
}
