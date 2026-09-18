package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Merge conflicts in an editor, VS Code's merge-conflict extension: a file
// with <<<<<<< / ======= / >>>>>>> blocks (and diff3's |||||||) gets its
// current and incoming lines tinted, the header carries the CodeLens actions
// Accept Current Change | Accept Incoming Change | Accept Both Changes, and
// the menu adds Accept All and next / previous conflict. It works on the text
// alone: git need not know about the conflict.

type conflict struct {
	start, split, end int   // the <<<<<<<, ======= and >>>>>>> lines
	ancestors         []int // the ||||||| lines of a diff3 block
}

func (c conflict) contains(line int) bool { return line >= c.start && line <= c.end }

// afterCurrent is the first line past the current block.
func (c conflict) afterCurrent() int {
	if len(c.ancestors) > 0 {
		return c.ancestors[0]
	}

	return c.split
}

const (
	acceptCurrent = iota
	acceptIncoming
	acceptBoth
)

var (
	acceptLabels    = [...]string{"Accept Current Change", "Accept Incoming Change", "Accept Both Changes"}
	acceptAllLabels = [...]string{"Accept All Current", "Accept All Incoming", "Accept All Both"}
)

func startsWith(l []rune, marker string) bool {
	return len(l) >= len(marker) && string(l[:len(marker)]) == marker
}

// parseConflicts scans lines the way VS Code's MergeConflictParser does: a
// second <<<<<<< inside a block stops the scan, ======= must be the whole
// line, a >>>>>>> without a ======= drops its block.
func parseConflicts(lines [][]rune) []conflict {
	var (
		out []conflict
		cur *conflict
	)

	for i, l := range lines {
		switch {
		case startsWith(l, "<<<<<<<"):
			if cur != nil {
				return out
			}

			cur = &conflict{start: i, split: -1}

		case cur == nil:
		case startsWith(l, "|||||||") && cur.split < 0:
			cur.ancestors = append(cur.ancestors, i)
		case string(l) == "=======" && cur.split < 0:
			cur.split = i
		case startsWith(l, ">>>>>>>"):
			if cur.split >= 0 {
				cur.end = i
				out = append(out, *cur)
			}

			cur = nil
		}
	}

	return out
}

// scanConflicts refreshes the editor's conflicts from its text.
func (p *preview) scanConflicts() {
	p.conf = nil
	if p.kind == pvFile && p.md != 1 {
		p.conf = parseConflicts(p.plain)
	}
}

// conflictAt is the conflict holding line, or the one the cursor is in.
func (p *preview) conflictAt(line int) (conflict, bool) {
	i, _ := slices.BinarySearchFunc(p.conf, line, func(c conflict, line int) int { return cmp.Compare(c.end, line) })
	if i < len(p.conf) && p.conf[i].contains(line) {
		return p.conf[i], true
	}

	return conflict{}, false
}

// conflictBg is VS Code's merge decoration for line: header and content of
// the current, common ancestor and incoming blocks; the splitter has none.
func (p *preview) conflictBg(line int) color.Color {
	c, ok := p.conflictAt(line)
	if !ok {
		return nil
	}

	switch {
	case line == c.start:
		return pal.mergeCurrentHeadBg
	case line == c.split:
		return nil
	case line == c.end:
		return pal.mergeIncomingHeadBg
	case line > c.split:
		return pal.mergeIncomingBg
	case line < c.afterCurrent():
		return pal.mergeCurrentBg
	case slices.Contains(c.ancestors, line):
		return pal.mergeCommonHeadBg
	}

	return pal.mergeCommonBg
}

// conflictSuffix is what a marker line shows after its text: "(Current
// Change)" and the three actions on <<<<<<<, "(Incoming Change)" on >>>>>>>.
// x positions in acts count cells from the line's start.
func (p *preview) conflictSuffix(line int) (plain []rune, styled string, acts []rowAction) {
	c, ok := p.conflictAt(line)
	if !ok || (line != c.start && line != c.end) {
		return nil, "", nil
	}

	if line == c.end {
		s := " (Incoming Change)"
		return []rune(s), dim.Render(s), nil
	}

	note := " (Current Change)  "

	var b, sb strings.Builder
	b.WriteString(note)
	sb.WriteString(dim.Render(note))

	link := fg(pal.headerAccent)
	x := cellsOf(p.plain[line]) + ansi.StringWidth(note)

	for i, label := range acceptLabels {
		if i > 0 {
			b.WriteString(" | ")
			sb.WriteString(dim.Render(" | "))

			x += 3
		}

		which := i

		acts = append(acts, rowAction{
			g: glyph{"", label, label, ""}, x: x, w: ansi.StringWidth(label),
			run: func(m *Model) tea.Cmd { return m.pv.acceptConflict(m, c, which) },
		})
		b.WriteString(label)
		sb.WriteString(link.Render(label))
		x += ansi.StringWidth(label)
	}

	return []rune(b.String()), sb.String(), acts
}

// conflictClick runs the action under a click on a <<<<<<< line, if any.
func (p *preview) conflictClick(m *Model, w, x, y int) (tea.Cmd, bool) {
	vis := p.rows(w)
	if len(p.conf) == 0 || p.top+y < 0 || p.top+y >= len(vis) {
		return nil, false
	}

	vr := vis[p.top+y]

	_, _, acts := p.conflictSuffix(vr.line)
	if acts == nil || vr.to != len(p.plain[vr.line]) {
		return nil, false
	}

	cx := x - p.gutter()
	if p.wrap {
		cx += cellsOf(p.plain[vr.line][:vr.from])
	} else {
		cx += p.left
	}

	if a, ok := hit(acts, cx); ok {
		return a.run(m), true
	}

	return nil, false
}

// resolved is the buffer's lines with the conflicts in cs replaced by the
// side which chose, as VS Code's DocumentMergeConflict.applyEdit does: the
// markers and the other side go, the kept content stays verbatim, and a lone
// empty line chosen from one side goes too.
func (p *preview) resolved(cs []conflict, which int) []string {
	var out []string

	next := 0
	for _, c := range cs {
		for _, l := range p.buf.lines[next:c.start] {
			out = append(out, string(l))
		}

		var keep [][]rune
		if which != acceptIncoming {
			keep = append(keep, p.buf.lines[c.start+1:c.afterCurrent()]...)
		}

		if which != acceptCurrent {
			keep = append(keep, p.buf.lines[c.split+1:c.end]...)
		}

		if which != acceptBoth && len(keep) == 1 && len(keep[0]) == 0 {
			keep = nil
		}

		for _, l := range keep {
			out = append(out, string(l))
		}

		next = c.end + 1
	}

	for _, l := range p.buf.lines[next:] {
		out = append(out, string(l))
	}

	return out
}

func (p *preview) applyResolved(m *Model, cs []conflict, which int) {
	lines := p.resolved(cs, which)

	text := strings.Join(lines, "\n")
	if p.buf.endNL || len(lines) == 0 {
		text += "\n"
	}

	p.setText(m, text)
}

// acceptConflict is one header's action, or the menu's on the conflict under
// the cursor.
func (p *preview) acceptConflict(m *Model, c conflict, which int) tea.Cmd {
	if !p.editable() {
		return nil
	}

	p.applyResolved(m, []conflict{c}, which)

	return flash(strings.ToLower(acceptLabels[which]), false)
}

// acceptAtCursor is VS Code's Accept Current / Incoming / Both command.
func (p *preview) acceptAtCursor(m *Model, which int) tea.Cmd {
	c, ok := p.conflictAt(p.cur.line)
	if !ok {
		return flash("Editor cursor is not within a merge conflict", true)
	}

	return p.acceptConflict(m, c, which)
}

// acceptAllConflicts is Accept All Current / Incoming / Both: one edit, one undo.
func (p *preview) acceptAllConflicts(m *Model, which int) tea.Cmd {
	if len(p.conf) == 0 {
		return flash("No merge conflicts found in this file", true)
	}

	if !p.editable() {
		return nil
	}

	n := len(p.conf)
	p.applyResolved(m, p.conf, which)

	return flash(fmt.Sprintf("%s: %d conflicts", strings.ToLower(acceptAllLabels[which]), n), false)
}

// gotoConflict moves to the next (d = 1) or previous conflict, wrapping
// around, as VS Code's Next / Previous Conflict do.
func (p *preview) gotoConflict(m *Model, d int) tea.Cmd {
	if len(p.conf) == 0 {
		return flash("No merge conflicts found in this file", true)
	}

	line := p.cur.line
	if len(p.conf) == 1 {
		if p.conf[0].contains(line) {
			return flash("No other merge conflicts within this file", false)
		}
	}

	var target *conflict

	if d > 0 {
		for i := range p.conf {
			if c := p.conf[i]; c.start > line && !c.contains(line) {
				target = &c
				break
			}
		}
	} else {
		for i := len(p.conf) - 1; i >= 0; i-- {
			if c := p.conf[i]; c.start < line && !c.contains(line) {
				target = &c
				break
			}
		}
	}

	if target == nil { // past the last one: wrap to the far end
		wrap := 0
		if d <= 0 {
			wrap = len(p.conf) - 1
		}

		target = &p.conf[wrap]
	}

	p.anchor = nil
	p.setCursor(m, pos{target.start, 0})

	return nil
}

// conflictItems are the merge-conflict commands for the context menu and the
// palette, present while the file holds conflicts.
func (p *preview) conflictItems() []item {
	if len(p.conf) == 0 || !p.editable() {
		return nil
	}

	var items []item

	for i, label := range acceptLabels {
		which := i

		items = append(items, item{label: label, run: func(m *Model) tea.Cmd { return m.pv.acceptAtCursor(m, which) }})
	}

	for i, label := range acceptAllLabels {
		which := i

		items = append(items, item{label: label, run: func(m *Model) tea.Cmd { return m.pv.acceptAllConflicts(m, which) }})
	}

	return append(items,
		item{label: "Next Conflict", run: func(m *Model) tea.Cmd { return m.pv.gotoConflict(m, 1) }},
		item{label: "Previous Conflict", run: func(m *Model) tea.Cmd { return m.pv.gotoConflict(m, -1) }})
}

// conflictHint is the footer while the file holds conflicts.
func (p *preview) conflictHint() string {
	n := len(p.conf)
	if n == 0 {
		return ""
	}

	s := fmt.Sprintf(" %d merge conflict", n)
	if n > 1 {
		s += "s"
	}

	return s + "  accept on <<<<<<< or in the menu"
}
