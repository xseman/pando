package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// panelModel is a model with two shells and the panel open under the
// editor on the Terminal.
func panelModel(t *testing.T) *Model {
	t.Helper()

	m := testModelSized(t, 120, 30)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"), termSession(m.ws, "t2"))
	drainInputs(m)
	press(m, "ctrl+j")

	return m
}

// at is where text shows on screen, its first cell.
func at(t *testing.T, m *Model, text string) (int, int) {
	t.Helper()

	for y, l := range strings.Split(checkWidths(t, m), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}

	t.Fatalf("%q is not on screen", text)

	return 0, 0
}

// The panel's tabs switch what it shows; on a list's tab the keys are
// pando's, not the shell's; the shells are a select, + before it.
func TestPanelTabs(t *testing.T) {
	m := panelModel(t)

	x, y := at(t, m, "Problems")
	_, cmd := m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	fire(m, cmd)

	if m.ptab != ptProblems || !strings.Contains(checkWidths(t, m), "No problems have been detected") {
		t.Fatalf("the Problems tab: %d", m.ptab)
	}

	press(m, "x")

	if len(m.inputs) != 0 || m.termFocused() {
		t.Fatal("a key on the Problems tab went to the shell")
	}

	x, y = at(t, m, "Terminal")
	click(m, x+1, y, tea.MouseLeft)

	if m.ptab != ptTerm {
		t.Fatalf("back to the Terminal: %d", m.ptab)
	}

	// The select: its list shows another shell, or asks to kill the one
	// shown; + starts one.
	x, y = at(t, m, "1: bash ▾")
	click(m, x+1, y, tea.MouseLeft)
	choose(t, m, "2: bash")

	if m.tv.id != "t2" {
		t.Fatalf("2: bash picked shows %q", m.tv.id)
	}

	x, y = at(t, m, "2: bash ▾")
	click(m, x+1, y, tea.MouseLeft)
	choose(t, m, "Kill Terminal…")

	if m.modal == nil || !strings.Contains(m.modal.title, "Kill") {
		t.Fatalf("Kill Terminal… asks first: %+v", m.modal)
	}

	m.modal = nil

	if _, cmd := m.Update(tea.MouseClickMsg{X: x - 2, Y: y, Button: tea.MouseLeft}); cmd == nil {
		t.Fatal("+ starts no shell")
	}

	// ⋮ opens the tab's menu, then where the panel goes.
	x, y = at(t, m, icEllipsis.s())
	click(m, x, y, tea.MouseLeft)

	if m.modal == nil || !slices.Contains(labels(m), "Move Terminal to Left") {
		t.Fatalf("⋮: %+v", m.modal)
	}

	m.modal = nil
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	checkWidths(t, m)
}

// The last shell gone shuts the panel on the Terminal, not on another tab.
func TestPanelStaysForItsLists(t *testing.T) {
	m := panelModel(t)
	m.ptab = ptOutput
	m.Update(sessionsMsg(m.mainSessions()))

	if !m.termOpen() {
		t.Fatal("the last shell gone shuts the panel on the Output tab")
	}
}

// The Output keeps every error the status bar flashes, a line of each of
// its lines; ctrl+shift+u shows it; it keeps outputMax lines, following the
// newest.
func TestPanelOutput(t *testing.T) {
	m := panelModel(t)
	m.flash("git push: rejected\nhint: fetch first", true)
	m.flash("saved a.go", false)
	m.focus = 0
	press(m, "ctrl+shift+u")

	out := checkWidths(t, m)
	if m.ptab != ptOutput || m.focus != onPanel || !strings.Contains(out, "pando git push: rejected") || !strings.Contains(out, "pando hint: fetch first") || len(m.out.lines) != 2 {
		t.Fatalf("the Output, tab %d, %d lines:\n%s", m.ptab, len(m.out.lines), out)
	}

	x, y := at(t, m, icEllipsis.s())
	click(m, x, y, tea.MouseLeft)

	if m.modal == nil || !slices.Contains(labels(m), "Clear Output") {
		t.Fatalf("the Output's ⋮: %+v", m.modal)
	}

	m.modal = nil

	x, y = at(t, m, "pando git push: rejected") // above the selected line
	m.Update(tea.MouseMotionMsg{X: x, Y: y})

	if !slices.ContainsFunc(m.out.view(m, m.mainW(), m.out.h), func(l string) bool {
		return strings.Contains(ansi.Strip(l), "git push: rejected") && strings.Contains(l, bgParams(pal.hoverBg))
	}) {
		t.Fatal("the Output's line under the mouse lights")
	}

	m.out.add(outputLine{src: "lsp:x", msg: "\tindented\x1b[31m red\x1b[m\r"})
	checkWidths(t, m)

	if got := m.out.lines[len(m.out.lines)-1].msg; got != "    indented red" {
		t.Fatalf("a line with a tab and escapes: %q", got)
	}

	// A line wider than the panel: a bar under the lines pans them.
	m.out.add(outputLine{src: "lsp:x", msg: strings.Repeat("w", 300) + "END"})
	m.focus = onPanel

	_, _, w, h, _ := m.panelOrigin()
	if body := m.panelBody(w, h); !strings.Contains(body[h-1], "▄") || strings.Contains(strings.Join(body, ""), "END") {
		t.Fatalf("a wide line: no bar, or no cut:\n%s", strings.Join(body, "\n"))
	}

	press(m, "G")

	for range 40 {
		press(m, "right")
	}

	if body := m.panelBody(w, h); m.out.left == 0 || !strings.Contains(strings.Join(body, ""), "END") {
		t.Fatalf("→ pans to the end: left %d", m.out.left)
	}

	x0, y0, _, _, _ := m.panelOrigin()
	m.Update(tea.MouseWheelMsg{X: x0 + 5, Y: y0 + 1, Button: tea.MouseWheelUp, Mod: tea.ModShift})
	left := m.out.left
	click(m, x0, y0+h-1, tea.MouseLeft) // the bar's left end

	if left >= m.out.wide-m.out.w || m.out.left != 0 {
		t.Fatalf("shift+wheel pans back (%d), a click at the bar's left end to the start (%d)", left, m.out.left)
	}

	var o outputLog
	for range outputMax + 100 {
		o.add(outputLine{msg: "x"})
	}

	if len(o.lines) > outputMax || o.l.sel != len(o.lines)-1 {
		t.Fatalf("%d lines, the selection on %d", len(o.lines), o.l.sel)
	}
}

// A language server's stderr reaches the Output.
func TestOutputFromServer(t *testing.T) {
	m, _ := serverModel(t, "a.go", "package a\n\nfunc B() { A(1, 2) }\n")
	m.pv.gotoLine(m, 2, 11)

	_, cmd := m.Update(keyMsg("ctrl+k"))
	deliver(m, cmd)

	done := make(chan tea.Msg, 1)
	go func() { done <- m.logIn.waitLogs()() }()

	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(5 * time.Second):
		t.Fatal("no output from the server")
	}

	if !slices.ContainsFunc(m.out.lines, func(l outputLine) bool { return strings.HasPrefix(l.src, "lsp:") && l.msg == "fake server ready" }) {
		t.Fatalf("output %+v", m.out.lines)
	}
}

// In a column the panel keeps its tabs.
func TestPanelColumn(t *testing.T) {
	m := panelModel(t)
	m.moveTerminal("right")

	x, y := at(t, m, "Out") // a narrow column's short titles
	click(m, x+1, y, tea.MouseLeft)

	if m.ptab != ptOutput || !strings.Contains(checkWidths(t, m), "Language servers' output") {
		t.Fatalf("the column's Output: %d", m.ptab)
	}
}

// A click on the Problems tab lists the problems there are.
func TestProblemsTabLists(t *testing.T) {
	m, _ := diagModel(t)
	m.sessions = append(m.sessions, termSession(m.ws, "t1"))
	drainInputs(m)
	press(m, "ctrl+j")

	x, y := at(t, m, "Problems")
	_, cmd := m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	fire(m, cmd)

	if m.probs == nil || m.probs.count() != 3 || m.probs.sourceW() != 0 || !strings.Contains(checkWidths(t, m), "undefined: undefined") {
		t.Fatalf("the Problems tab: %+v", m.probs)
	}

	// A row under the mouse lights, as the sidebar's lists do.
	_, y0, w, h, _ := m.panelOrigin()
	m.Update(tea.MouseMotionMsg{X: m.mainX() + w - 5, Y: y0 + 3}) // the row under the selected one

	if body := m.panelBody(w, h); !strings.Contains(body[3], bgParams(pal.hoverBg)) || strings.Contains(body[4], bgParams(pal.hoverBg)) {
		t.Fatalf("the problem under the mouse lights, alone:\n%s", strings.Join(body, "\n"))
	}
}

// The Terminal's select, VS Code's, names the shell shown; its list picks
// another, the one shown checked.
func TestTermSelect(t *testing.T) {
	m := panelModel(t)
	m.sessions[len(m.sessions)-2].Program = "fish"
	m.sessions[len(m.sessions)-1].Program = "claude"
	m.tv.id = m.sessions[len(m.sessions)-2].ID

	x, y := at(t, m, "1: fish ▾")
	click(m, x+1, y, tea.MouseLeft)

	if m.modal == nil || !slices.Contains(labels(m), "2: claude") || m.modal.disp[0].hint != "✓" {
		t.Fatalf("the select's list: %v", m.modal)
	}

	choose(t, m, "2: claude")

	if m.tv.id != m.sessions[len(m.sessions)-1].ID || !strings.Contains(checkWidths(t, m), "2: claude ▾") {
		t.Fatalf("picked claude: showing %q", m.tv.id)
	}
}

// A tab under the mouse is marked on the rule under it, in the hover tint.
func TestPanelTabHover(t *testing.T) {
	m := panelModel(t)
	x, y := at(t, m, "Output")
	m.Update(tea.MouseMotionMsg{X: x + 1, Y: y})

	if rule := strings.Split(m.View().Content, "\n")[y+1]; !strings.Contains(rule, fgParams(pal.sashHover)) {
		t.Fatalf("the rule under the hovered tab: %q", rule)
	}
}

// framedPanel is a model with the panel under the editor, 8 rows tall.
func framedPanel(t *testing.T, borders bool) *Model {
	t.Helper()
	m := testModelSized(t, 100, 30)
	m.setSettings(map[string]any{"panel_borders": borders})
	m.st.Settings.TermH = 8
	m.moveTerminal("bottom")

	return m
}

// dragTo presses at (x, y), drags to (tx, ty) and lets go, cells of the panels.
func dragTo(m *Model, x, y, tx, ty int) {
	b := m.bord()
	m.Update(tea.MouseClickMsg{X: x, Y: y + b, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: tx, Y: ty + b, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: tx, Y: ty + b, Button: tea.MouseLeft})
}

// TestPanelOwnFrame: with panel_borders the panel is framed apart from the
// editor, untitled, and only the two facing edges are its sash.
func TestPanelOwnFrame(t *testing.T) {
	m := framedPanel(t, true)
	lines := strings.Split(checkWidths(t, m), "\n")
	y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "│┌⎻⎻") })

	if y < 1 || !strings.Contains(lines[y-1], "│└──") || !strings.HasSuffix(lines[y-1], "─┘") || !strings.HasSuffix(lines[y], "⎻┐") || strings.Contains(lines[y], "Terminal") {
		t.Fatalf("the editor's frame closes, the panel's opens:\n%s", strings.Join(lines, "\n"))
	}

	if y != m.areaH()+2 || m.panelStripY() != m.areaH()+2 || !strings.Contains(lines[y+1], "Terminal   Problems") {
		t.Fatalf("the top edge on screen row %d, the editor %d rows, the strip on %d", y, m.areaH(), m.panelStripY())
	}

	if _, h := m.termBody(); h != m.termRows()-4 {
		t.Fatalf("body %d rows of %d", h, m.termRows())
	}

	x := m.area().x + 1 + m.panelChips(panelTabX, m.area().w)[ptProblems].x
	click(m, x, m.panelStripY()+1, tea.MouseLeft)

	if m.ptab != ptProblems {
		t.Fatalf("the tab under the click: %d", m.ptab)
	}

	for row, sash := range map[int]bool{m.areaH(): true, m.areaH() + 1: true, m.panelStripY(): false, m.panelStripY() + 1: false} {
		m.Update(tea.MouseClickMsg{X: m.area().x + 40, Y: row + 1, Button: tea.MouseLeft})

		if (m.drag != nil && m.drag.kind == dragTerm) != sash {
			t.Fatalf("row %d: sash %v, drag %+v", row, sash, m.drag)
		}

		m.Update(tea.MouseReleaseMsg{X: m.area().x + 40, Y: row + 1, Button: tea.MouseLeft})
	}
}

// TestPanelSashLightsWhole: rested on, both facing edges light all along, a
// group's frame giving way as it does to a divider that lights.
func TestPanelSashLightsWhole(t *testing.T) {
	m := groupsModel(t)
	m.setSettings(map[string]any{"panel_borders": true})
	press(m, `ctrl+\`)
	m.moveTerminal("bottom")
	m.focus = onMain

	if !m.tiled() {
		t.Fatal("the groups are not side by side")
	}

	m.Update(tea.MouseMotionMsg{X: m.area().x + 5, Y: m.areaH() + 1})
	m.sashSince = time.Now().Add(-sashDelay)

	lines := m.termPanelLines(m.area().w, m.termRows())
	under, over, rule := ansi.Strip(lines[0]), ansi.Strip(lines[1]), ansi.Strip(lines[3])

	if strings.Trim(under, "━") != "" || strings.Trim(over, "━") != "" || strings.Contains(rule, "━") {
		t.Fatalf("the edges light whole, the rule stays:\n%q\n%q\n%q", under, over, rule)
	}
}

// TestPanelMaximizedFrame: maximized, the panel takes the editor's frame and
// spends no row on edges of its own.
func TestPanelMaximizedFrame(t *testing.T) {
	m := framedPanel(t, true)
	m.maximizeTerminal(true)

	lines := strings.Split(checkWidths(t, m), "\n")
	if strings.Contains(lines[0], "Terminal") || strings.Contains(strings.Join(lines[1:len(lines)-2], ""), "│└") {
		t.Fatalf("maximized:\n%s", strings.Join(lines, "\n"))
	}

	if _, h := m.termBody(); h != m.panelH()-termStripH {
		t.Fatalf("body %d rows of %d", h, m.panelH())
	}
}

// TestPanelSharesSidebar: in a sidebar the panel is a tab beside the views;
// shut, the tab stays, the view shown before it comes back, and the tab opens
// it. A column of its own goes when it shuts, as it always has.
func TestPanelSharesSidebar(t *testing.T) {
	m := framedPanel(t, false)
	m.moveTerminal("left")

	if i := m.colOf(viewTerm); i < 0 || i != m.colOf(viewFiles) || m.termPos() != "left" || m.termRows() != 0 || !m.shown(viewTerm) {
		t.Fatalf("not a tab beside Explorer: %+v", m.cols())
	}

	m.showView(viewFiles)
	m.toggleTerminal()

	if !m.termOpen() || !m.shown(viewTerm) {
		t.Fatalf("behind Explorer, the toggle did not bring it up: open=%v %+v", m.termOpen(), m.cols())
	}

	m.showView(viewFiles)
	m.showView(viewTerm)
	m.toggleTerminal()

	if m.termOpen() || m.colOf(viewTerm) != m.colOf(viewFiles) || !m.shown(viewFiles) || m.shown(viewTerm) {
		t.Fatalf("shut: open=%v %+v", m.termOpen(), m.cols())
	}

	m.Update(tea.MouseClickMsg{X: tabAt(m, viewTerm), Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: tabAt(m, viewTerm), Y: 0, Button: tea.MouseLeft})

	if !m.termOpen() || !m.shown(viewTerm) {
		t.Fatalf("its tab did not open it: open=%v %+v", m.termOpen(), m.cols())
	}

	m.splitView(viewTerm)

	if vs := m.colViews(m.colOf(viewTerm)); !slices.Equal(vs, []view{viewTerm}) {
		t.Fatalf("own column: %v", vs)
	}

	m.toggleTerminal()

	if m.colOf(viewTerm) >= 0 {
		t.Fatalf("a shut panel kept its column: %+v", m.cols())
	}
}

// TestPanelDrags: any tab of the strip carries the panel to a sidebar's tabs
// or a column of its own, the tab there brings it back, and nothing else goes
// under the editor.
func TestPanelDrags(t *testing.T) {
	m := framedPanel(t, false)
	tab := func() (int, int) {
		return m.area().x + 1 + m.panelChips(panelTabX, m.area().w)[ptOutput].x, m.panelStripY()
	}

	x, y := tab()
	dragTo(m, x, y, x+20, y)

	if m.termPos() != "bottom" || m.ptab != ptOutput {
		t.Fatalf("a drag along its own strip moved it: %s, tab %d", m.termPos(), m.ptab)
	}

	x, y = tab()
	dragTo(m, x, y, tabAt(m, viewGit), 0)

	if m.termPos() != "left" || m.colOf(viewTerm) != m.colOf(viewFiles) || m.ptab != ptOutput || m.termRows() != 0 {
		t.Fatalf("dropped on the tabs: %s %+v tab %d", m.termPos(), m.cols(), m.ptab)
	}

	low := m.panelH() - 2
	dragTo(m, tabAt(m, viewFiles), 0, m.area().x+20, low)

	if m.colOf(viewFiles) < 0 || m.termPos() != "left" || m.dropAt(viewFiles, m.area().x+20, low).under {
		t.Fatalf("Explorer went under the editor: %s %+v", m.termPos(), m.cols())
	}

	dragTo(m, tabAt(m, viewTerm), 0, m.area().x+20, low)

	if m.termPos() != "bottom" || m.colOf(viewTerm) >= 0 || m.termRows() == 0 {
		t.Fatalf("not back under the editor: %s %+v", m.termPos(), m.cols())
	}

	x, y = tab()
	dragTo(m, x, y, 1, 10)

	if vs := m.colViews(m.colOf(viewTerm)); !slices.Equal(vs, []view{viewTerm}) || m.termPos() != "left" {
		t.Fatalf("dropped at the edge: %s %v", m.termPos(), vs)
	}
}

// TestPanelBesideSideBar: past a side activity bar the panel starts after the
// bar, its strip and its screen one with what is drawn.
func TestPanelBesideSideBar(t *testing.T) {
	m := framedPanel(t, false)
	m.setSettings(map[string]any{"activity_bar": "side"})
	m.moveTerminal("left")
	checkWidths(t, m)

	i := m.colOf(viewTerm)
	rc := m.colRect(i)

	if bw := m.barW(i); bw == 0 || m.panelRect() != (rect{rc.x + bw, rc.w - bw}) || m.termW() != rc.w-bw {
		t.Fatalf("panel %+v in column %+v", m.panelRect(), rc)
	}

	if w, _ := m.termBody(); w != rc.w-m.barW(i) {
		t.Fatalf("screen %d wide in %+v", w, rc)
	}

	x := m.panelRect().x + m.panelChips(panelTabX, m.termW())[ptOutput].x
	m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: 0, Button: tea.MouseLeft})

	if m.ptab != ptOutput {
		t.Fatalf("the tab under the click: %d", m.ptab)
	}
}
