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
	m.Update(sessionsMsg(m.agentSessions()))

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
