package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/git"
)

// quickDiff is VS Code's quick diff, a zone in the editor under the change
// it shows, scrolling with the text: one change of the file, its original
// lines over its new ones as a diff with a little context, stepped through
// with the others, staged or reverted from here. Its rows are the editor's
// (rows), vrow{line: -1, from: k}: a header, the diff, a rule.
type quickDiff struct {
	i     int // the change shown, in the preview's changes
	top   int // the diff's first row shown
	h     int // the zone's rows, header and rule too
	lines []string
	meta  []lineMeta
	numW  int
}

// quickCtx are the unchanged lines shown either side of a change.
const quickCtx = 3

// changeStagedMsg ends a Stage Change.
type changeStagedMsg struct{ err error }

// showChange opens the quick diff on change i under it, the editor at it,
// or closes it when it shows i already: a second click on a mark.
func (m *Model) showChange(i int) tea.Cmd {
	p := &m.pv
	if i < 0 || i >= len(p.changes) {
		return flash("no changes", false)
	}

	if p.qd != nil && p.qd.i == i {
		p.closeQuickDiff()
		return nil
	}

	p.qd = &quickDiff{i: i}
	p.qd.build(m)
	p.gotoLine(m, p.changes[i].b0, 0)
	p.revealZone(m)

	return nil
}

// closeQuickDiff takes the zone out of the editor.
func (p *preview) closeQuickDiff() {
	p.qd, p.vis = nil, nil
}

// zoneLine is the line the quick diff stands under: its change's last, or
// the line its deleted lines went after.
func (p *preview) zoneLine() int {
	c := p.changes[p.qd.i]
	return ifElse(c.b1 > c.b0, c.b1-1, max(c.b0-1, 0))
}

// revealZone scrolls the zone into view under its change, its change's first
// line kept on screen.
func (p *preview) revealZone(m *Model) {
	rows, h := p.rows(m.pvW()), m.pvH()

	end := slices.IndexFunc(rows, func(vr vrow) bool { return vr.line < 0 }) + p.qd.h - 1
	if end >= p.top+h {
		p.top = min(end-h+1, p.lineRow[p.changes[p.qd.i].b0])
	}
}

// followQuickDiff keeps an open quick diff on the changes as they are now,
// closing it with the last.
func (m *Model) followQuickDiff() {
	p := &m.pv
	if p.qd == nil {
		return
	}

	if len(p.changes) == 0 {
		p.closeQuickDiff()
		return
	}

	top := p.qd.top
	p.qd.i = min(p.qd.i, len(p.changes)-1)
	p.qd.build(m)
	p.qd.top = min(top, max(len(p.qd.lines)-1, 0))
}

// build renders change q.i as a diff, its context from the editor, its old
// lines from the text it is against, its new ones from the editor, and
// sizes the zone: the whole diff, up to half the editor's height.
func (q *quickDiff) build(m *Model) {
	p := &m.pv
	c := p.changes[q.i]
	orig, now := ifElse(c.staged, p.head, p.base), p.rawLines()
	from, to := max(c.b0-quickCtx, 0), min(c.b1+quickCtx, len(now))

	var b strings.Builder

	fmt.Fprintf(&b, "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -%d,%d +%d,%d @@\n",
		from+c.a0-c.b0+1, c.b0-from+c.a1-c.a0+to-c.b1, from+1, to-from)

	for _, run := range []struct {
		mark  string
		lines []string
	}{{" ", now[from:c.b0]}, {"-", orig[c.a0:c.a1]}, {"+", now[c.b0:c.b1]}, {" ", now[c.b1:to]}} {
		for _, l := range run.lines {
			b.WriteString(run.mark + l + "\n")
		}
	}

	q.lines, _, q.meta, q.numW = render(pvDiff, p.path, b.String(), m.dark)
	q.top = 0
	q.h = min(len(q.lines), max(m.pvH()/2-2, 3)) + 2
	p.vis = nil // the editor's rows make room again
}

// actions are the header's buttons, VS Code's: Stage and Revert for a
// change not yet staged, the next and previous change, close; placed for a
// header w cells wide.
func (q *quickDiff) actions(m *Model, w int) []rowAction {
	var acts []rowAction

	if !m.pv.changes[q.i].staged {
		acts = append(acts,
			rowAction{g: icAdd, text: "Stage", run: (*Model).stageChange},
			rowAction{g: icDiscard, text: "Revert", run: (*Model).revertChange})
	}

	acts = append(acts,
		rowAction{g: icDown, tip: "Next Change  alt+F3", run: func(m *Model) tea.Cmd { return m.pv.qd.step(m, 1) }},
		rowAction{g: icUp, tip: "Previous Change  ⇧alt+F3", run: func(m *Model) tea.Cmd { return m.pv.qd.step(m, -1) }},
		rowAction{g: icClose, tip: "Close  esc", run: func(m *Model) tea.Cmd { m.pv.closeQuickDiff(); return nil }})

	x := w
	for i := len(acts) - 1; i >= 0; i-- {
		acts[i].w = ansi.StringWidth(acts[i].label())
		x -= acts[i].w
		acts[i].x = x
	}

	return acts
}

// row is the zone's row k, w cells wide, on screen row y: the header, its
// button under the mouse raised as every button is, a row of the diff, or
// the rule closing it.
func (q *quickDiff) row(m *Model, k, w, y int) string {
	p := &m.pv

	switch k {
	case 0:
		c, mx := p.changes[q.i], ifElse(m.mouseY == y, m.mouseX-m.mainX(), -1)

		var right []seg

		for _, a := range q.actions(m, w) {
			if mx >= a.x && mx < a.x+a.w {
				right, m.tip = append(right, sgOwn(a.label(), keycapHot())), a.tip
				continue
			}

			right = append(right, sg(a.label(), ifElse(a.text != "", plain, dim)))
		}

		return row(w, pal.sectionBg, []seg{
			sg(" "+filepath.Base(p.path), bold),
			sg(fmt.Sprintf("  %d of %d changes", q.i+1, len(p.changes)), plain), // before the label, which a narrow header cuts
			sg("  "+ifElse(c.staged, "Git Local Changes (Index)", "Git Local Changes (Working Tree)"), dim),
		}, right...)

	case q.h - 1:
		return fg(pal.sectionBg).Render(strings.Repeat("▔", w))
	}

	i := q.top + k - 1
	if i >= len(q.lines) {
		return blank(w)
	}

	bg, gw := diffBg(q.meta[i].kind), 2*q.numW+4
	body := ansi.Cut(q.lines[i], 0, w-gw)

	if pad := w - gw - ansi.StringWidth(body); pad > 0 {
		body += lipgloss.NewStyle().Background(bg).Render(blank(pad))
	}

	return diffGutter(q.meta[i], q.numW, bg) + body
}

// step shows the change after (d 1) or before (d -1), around the ends, the
// editor at it.
func (q *quickDiff) step(m *Model, d int) tea.Cmd {
	n := len(m.pv.changes)
	q.i = (q.i + d + n) % n
	q.build(m)
	m.pv.gotoLine(m, m.pv.changes[q.i].b0, 0)
	m.pv.revealZone(m)

	return nil
}

// mouse is a click on the zone's row k, a header button, or the wheel over
// it: its diff scrolls while it runs past the zone, else the editor does.
func (q *quickDiff) mouse(m *Model, msg tea.MouseMsg, x, k int) (tea.Cmd, bool) {
	mo := msg.Mouse()

	switch msg.(type) {
	case tea.MouseWheelMsg:
		body := q.h - 2
		if len(q.lines) <= body {
			return nil, false
		}

		q.top = max(0, min(q.top+wheelDelta(mo), len(q.lines)-body))

	case tea.MouseClickMsg:
		if a, ok := hit(q.actions(m, m.pvW()), x); ok && k == 0 && mo.Button == tea.MouseLeft {
			return a.run(m), true
		}
	}

	return nil, true
}

// stageChange is VS Code's Stage Change: the shown change, as the editor
// has it, into the index; the file's staged text and git read again after.
func (m *Model) stageChange() tea.Cmd {
	p := &m.pv

	c := p.changes[p.qd.i]
	if c.staged {
		return nil
	}

	path, old, lines := p.path, slices.Clone(p.base[c.a0:c.a1]), slices.Clone(p.rawLines()[c.b0:c.b1])

	return func() tea.Msg { return changeStagedMsg{git.StageChange(path, c.a0, c.a1, old, lines)} }
}

// onChangeStaged reads the file's staged text and git again, or says why the
// change did not stage.
func (m *Model) onChangeStaged(msg changeStagedMsg) tea.Cmd {
	if msg.err != nil {
		return flash(msg.err.Error(), true)
	}

	return tea.Batch(m.pv.load(m), m.refreshGit(), flash("change staged", false))
}

// revertChange is VS Code's Revert Change: the shown change's lines back to
// the staged text's in the editor, one step for undo.
func (m *Model) revertChange() tea.Cmd {
	p := &m.pv

	c := p.changes[p.qd.i]
	if c.staged || !p.editable() {
		return nil
	}

	now := p.rawLines()

	text := strings.Join(slices.Concat(now[:c.b0], p.base[c.a0:c.a1], now[c.b1:]), "\n")
	if p.buf.endNL {
		text += "\n"
	}

	p.setText(m, text)

	return nil
}
