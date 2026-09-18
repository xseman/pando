package ui

import (
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

	if got := labels(m); !slices.Equal(got, []string{"◐ claude · fix", "○ shell", "⎇ main"}) {
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
	for _, want := range []string{"▾ × ", "├─ ○ shell", "└─ × claude", "   main"} { // the project carries its most demanding session
		if !strings.Contains(out, want) {
			t.Fatalf("agents tree lacks %q:\n%s", want, out)
		}
	}
	// A background session that starts waiting plays the request cue once,
	// one that finishes unseen the done cue; the visible session is silent.
	var played []string

	playSound = func(path string) { played = append(played, path) }
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

	if m.termOpen() {
		t.Fatal("ctrl+j closes it again")
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

	if m.tv.wide || m.tv.left != 0 || m.termCols() != w {
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
	sh := proto.Session{SessionSpec: proto.SessionSpec{ID: "s1", Agent: "shell", Cmd: []string{"bash"}}, Program: "bash", Title: "~/w/n/cloudflare-iac"}
	claude := sh
	claude.Program, claude.Title = "claude", "✳ Fix the geoblock rule"
	bare := claude
	bare.Title = ""
	named := claude
	named.Name = "deploy"

	panel := proto.Session{SessionSpec: proto.SessionSpec{Agent: termAgent, Cmd: []string{"/bin/zsh"}}, Program: "zsh"}
	for want, s := range map[string]proto.Session{"shell": sh, "Fix the geoblock rule": claude, "claude": bare, "deploy": named, "zsh": panel} {
		if got := sessionName(s); got != want {
			t.Errorf("sessionName(%+v) = %q, want %q", s, got, want)
		}
	}

	if titleAfter(claude) != "" || titleAfter(sh) != " · ~/w/n/cloudflare-iac" {
		t.Fatalf("titles after names: %q %q", titleAfter(claude), titleAfter(sh))
	}

	m := testModelSized(t, 120, 30)
	claude.Workspace = m.ws
	m.sessions = []proto.Session{claude}
	m.focus = onMain
	m.switchSession("s1")

	tabs := m.sessionTabs(m.mainW())
	if !strings.Contains(tabs[0].label, "Fix the geoblock rule") {
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
