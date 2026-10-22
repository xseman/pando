package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// The bottom panel, VS Code's: tabs for the Terminal, the Problems and the
// Output, under the editor or in a column of its own.
// The Terminal's shells are a select in its strip, VS Code's, a + before it.

// panelTab is one of the panel's tabs.
type panelTab int

const (
	ptTerm panelTab = iota
	ptProblems
	ptOutput
)

var (
	panelTitles = []string{"Terminal", "Problems", "Output"}
	panelShort  = []string{"Term", "Probs", "Out"} // a strip too narrow for the titles
)

// panelChip is a tab's label in the panel's strip, x cells from its start.
type panelChip struct {
	tab   panelTab
	label string
	x, w  int
}

// panelChips are the panel's tabs as a strip w cells wide draws them, from
// x0: the Problems with their count; short titles where the full ones do
// not fit before the buttons.
func (m *Model) panelChips(x0, w int) []panelChip {
	chips := func(titles []string, gap int) ([]panelChip, int) {
		out, at := make([]panelChip, len(titles)), x0

		for i, t := range titles {
			if panelTab(i) == ptProblems {
				if errs, warns := m.diagCounts(); errs+warns > 0 {
					t += " " + strconv.Itoa(errs+warns)
				}
			}

			out[i] = panelChip{panelTab(i), t, at, ansi.StringWidth(t)}
			at += out[i].w + gap
		}

		return out, at - gap
	}

	if out, end := chips(panelTitles, 3); end <= w-m.termBtnW() {
		return out
	}

	out, _ := chips(panelShort, 2)

	return out
}

// panelStrip is the strip row: the tabs, the active one bold, then the
// buttons; x0 is where the tabs start, sx and sy where the row is on screen.
func (m *Model) panelStrip(w, x0, sx, sy int) string {
	var left []seg

	at := 0
	for _, c := range m.panelChips(x0, w) {
		left = append(left, sg(blank(c.x-at), plain))

		st := dim

		switch {
		case c.tab == m.ptab:
			st = bold
		case m.overChip(c, sx, sy):
			st = plain
		}

		left, at = append(left, sg(c.label, st)), c.x+c.w
	}

	btns, _ := m.actSegs(m.termButtons(), sx, sy)

	return row(w, nil, left, btns...)
}

// overChip reports the mouse on chip c of a strip on screen row sy, from sx.
func (m *Model) overChip(c panelChip, sx, sy int) bool {
	return m.modal == nil && m.mouseY == sy && m.mouseX >= sx+c.x && m.mouseX < sx+c.x+c.w
}

// panelRule is the line under the strip, of the strip at sx, sy: marked
// under the active tab, in a tint under the one the mouse is on, as the
// activity bar's.
func (m *Model) panelRule(w, x0, sx, sy int) string {
	var marks []ruleMark

	for _, c := range m.panelChips(x0, w) {
		if hot := m.overChip(c, sx, sy); c.tab == m.ptab || hot {
			marks = append(marks, ruleMark{c.x, c.w, c.tab == m.ptab, hot})
		}
	}

	if m.termRows() > 0 {
		if st, ok := m.sashStyle(termSash); ok {
			return ruleRow(w, marks, st)
		}
	}

	return ruleRow(w, marks)
}

// panelTabAt is the tab under cell x of a strip w cells wide, false for none.
func (m *Model) panelTabAt(x, x0, w int) (panelTab, bool) {
	for _, c := range m.panelChips(x0, w) {
		if x >= c.x && x < c.x+c.w {
			return c.tab, true
		}
	}

	return 0, false
}

// showPanel opens the panel on tab, where it is docked, with the keyboard
// in it; the Terminal starts a shell if it has none.
func (m *Model) showPanel(tab panelTab) tea.Cmd {
	m.ptab = tab

	var cmds []tea.Cmd

	if !m.termOpen() {
		cmds = append(cmds, m.setTermOpen(true, nil))
	}

	if m.termPos() == "bottom" {
		m.focus = onPanel
	} else {
		cmds = append(cmds, m.showView(viewTerm))
	}

	if tab == ptTerm {
		cmds = append(cmds, m.startTerm())
	}

	return tea.Batch(cmds...)
}

// panelStripClick is a left click at cell x of the bottom panel's strip, w
// wide, on screen row sy: a button runs, a tab shows; the rest of the row is
// the panel's sash.
func (m *Model) panelStripClick(x, sy, w int) tea.Cmd {
	if a, ok := hit(m.termButtons(), x); ok {
		return a.run(m)
	}

	if tab, ok := m.panelTabAt(x, panelTabX, w); ok {
		return m.pickPanelTab(tab)
	}

	m.drag = &drag{kind: dragTerm, y0: sy, h0: m.termRows()}

	return nil
}

// pickPanelTab shows tab, a click on its label: the Terminal's shell, or
// one started.
func (m *Model) pickPanelTab(tab panelTab) tea.Cmd {
	m.ptab = tab

	switch tab {
	case ptTerm:
		return m.startTerm()
	case ptProblems: // the list as the problems are now, which then follows them
		return m.showProblems()
	case ptOutput:
	}

	return nil
}

// shellName is what runs in shell s, as the Terminal's select names it.
func shellName(s proto.Session) string { return cmp.Or(s.Program, sessionName(s)) }

// termSelect is the Terminal's select, VS Code's: the shell shown, as
// "N: name ▾", "N ▾" in a strip too narrow for the name; "" with none.
func (m *Model) termSelect() string {
	ts := m.termSessions()

	i := slices.IndexFunc(ts, func(s proto.Session) bool { return s.ID == m.tv.id })
	if i < 0 {
		return ""
	}

	if m.termW() < 60 {
		return fmt.Sprintf("%d ▾", i+1)
	}

	return fmt.Sprintf("%d: %s ▾", i+1, ansi.Truncate(shellName(ts[i]), 16, "…"))
}

// termPick is the select's list at the pointer: every shell, the one shown
// checked, then a new one and killing the one shown.
func (m *Model) termPick() tea.Cmd {
	var items []item

	for i, s := range m.termSessions() {
		id := s.ID
		items = append(items, item{
			label: fmt.Sprintf("%d: %s", i+1, shellName(s)), hint: ifElse(id == m.tv.id, "✓", ""),
			run: func(m *Model) tea.Cmd { m.tv.id, m.tv.scroll = id, 0; return m.fetchScreen() },
		})
	}

	return m.menuOf(menuGroups(items, []item{
		{label: "New Terminal", hint: "^⇧`", run: func(m *Model) tea.Cmd { return m.newTerm() }},
		{label: "Kill Terminal…", run: func(m *Model) tea.Cmd { return m.confirmKill(m.tv.id) }},
	}), m.mouseX, m.mouseY+1)
}

// panelOrigin is the screen cell of the panel body's top left, and its size;
// ok is false while the panel is not shown.
func (m *Model) panelOrigin() (x, y, w, h int, ok bool) {
	if n := m.termRows(); n > 0 {
		return m.area().x, m.areaH() + panelTopH, m.area().w, n - panelTopH, true
	}

	if !m.shown(viewTerm) {
		return 0, 0, 0, 0, false
	}

	rc := m.colRect(m.colOf(viewTerm))

	return rc.x, m.bodyTop(viewTerm) + termStripH, rc.w, m.bodyH(viewTerm) - termStripH, true
}

// panelHover is the panel body's cell under the mouse; ok is false while it
// is elsewhere, or a modal is over it.
func (m *Model) panelHover() (x, y int, ok bool) {
	x0, y0, w, h, shown := m.panelOrigin()
	x, y = m.mouseX-x0, m.mouseY-y0

	return x, y, shown && m.modal == nil && x >= 0 && x < w && y >= 0 && y < h
}

// panelBody is the panel's body for its tab, w by h.
func (m *Model) panelBody(w, h int) []string {
	switch m.ptab {
	case ptProblems:
		return m.peekBody(m.probs, "No problems have been detected in the workspace.", w, h)
	case ptOutput:
		return m.out.view(m, w, h)
	case ptTerm:
	}

	return m.termScreen(w, h)
}

// peekBody is a peek's list in the panel, or what says there is none.
func (m *Model) peekBody(pk *peek, none string, w, h int) []string {
	if pk == nil {
		out := []string{row(w, nil, []seg{sg("  "+none, dim)})}
		for len(out) < h {
			out = append(out, blank(w))
		}

		return out[:max(h, 0)]
	}

	return pk.view(m, w, h)
}

// panelMouse is a mouse event at body cell (x, y) of the panel.
func (m *Model) panelMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	switch m.ptab {
	case ptProblems:
		if m.probs != nil {
			return m.probs.mouse(m, msg, x, y)
		}

		return nil

	case ptOutput:
		return m.out.mouse(m, msg, x, y)
	case ptTerm:
	}

	return m.termPanelMouse(msg, x, y)
}

// panelKey is a key with the keyboard in the panel: the shell's on the
// Terminal, the list's on the others.
func (m *Model) panelKey(k tea.KeyPressMsg) tea.Cmd {
	switch m.ptab {
	case ptTerm:
		return m.termPanelKey(k)
	case ptOutput:
		return m.out.key(m, k)
	case ptProblems:
		if m.probs != nil {
			return m.probs.key(m, k)
		}
	}

	if s := k.String(); s == "esc" || s == "q" || s == "tab" {
		m.focus = onMain
	}

	return nil
}

// panelMenu is the strip's ⋮: what the tab offers, then where the panel
// goes.
func (m *Model) panelMenu() tea.Cmd {
	var tab []item

	switch m.ptab {
	case ptTerm:
		if m.session(m.tv.id) != nil {
			tab = append(tab, item{label: "Clear", run: func(m *Model) tea.Cmd { return m.clearTerm(m.tv.id) }})
		}

	case ptOutput:
		tab = append(tab,
			item{label: "Copy All", run: func(m *Model) tea.Cmd { return setClipboard(m.out.text(), "copied the output") }},
			item{label: "Clear Output", run: func(m *Model) tea.Cmd { m.out = outputLog{}; return nil }})

	case ptProblems:
		if m.probs != nil {
			tab = append(tab, item{label: "Clear", run: func(m *Model) tea.Cmd { m.probs = nil; return nil }})
		}
	}

	return m.menuOf(menuGroups(tab, m.terminalItems()), m.mouseX, m.mouseY+1)
}
