package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A file's text folds by indentation, VS Code's indentation folding: a line
// heads a block when the next line that is not blank is indented deeper,
// and the block runs to the last line indented deeper than it, so a closing
// brace stays in view. The gutter shows ▾ on a header while the pointer is
// over it and ▸ on a folded one always, VS Code's showFoldingControls
// "mouseover"; a click on it, ctrl+shift+[ and ] or vim's z keys fold and
// unfold. Folds live with the editor's tab.

// folds are a file's folded blocks: the header lines folded, sorted, and the
// block each line heads, worked out for the text at key.
type folds struct {
	shut []int     // headers of the folded blocks, sorted
	end  []int32   // per line, the last line of the block it heads; -1 for none
	key  gutterKey // the text end was worked out for: buffer and its count of edits
	gen  int       // counts changes to shut, which the screen rows are cached by
}

// foldRanges is each line's block: the last line indented deeper than it
// before the next line that is not, -1 when the next line that is not
// blank is not deeper. Blank lines have none, and end no block.
func foldRanges(lines [][]rune) []int32 {
	end := make([]int32, len(lines))

	var stack []int

	last := -1 // the last line that is not blank
	shut := func(depth int) {
		for len(stack) > 0 && indentCells(lines[stack[len(stack)-1]]) >= depth {
			h := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			if last > h {
				end[h] = int32(last)
			}
		}
	}

	for i, l := range lines {
		end[i] = -1

		if blankLine(l) {
			continue
		}

		shut(indentCells(l))

		stack, last = append(stack, i), i
	}

	shut(0)

	return end
}

// indentCells is the blanks a line starts with; tabs are already spaces.
func indentCells(l []rune) int {
	n := 0
	for n < len(l) && l[n] == ' ' {
		n++
	}

	return n
}

func blankLine(l []rune) bool { return strings.TrimSpace(string(l)) == "" }

// foldable reports a text that folds: a file shown as text.
func (p *preview) foldable() bool { return p.kind == pvFile && p.md != 1 && p.buf != nil && p.ready }

// foldEnd is each line's block for the text as it is now, worked out again
// after an edit; a fold whose header no longer heads a block opens.
func (p *preview) foldEnd() []int32 {
	if !p.foldable() {
		return nil
	}

	if key := (gutterKey{buf: p.buf, edits: p.buf.edits}); key != p.fold.key || len(p.fold.end) != len(p.plain) {
		p.fold.end, p.fold.key = foldRanges(p.plain), key

		if kept := slices.DeleteFunc(p.fold.shut, func(h int) bool { return h >= len(p.fold.end) || p.fold.end[h] <= int32(h) }); len(kept) != len(p.fold.shut) {
			p.fold.shut = kept
		}

		p.fold.gen++
	}

	return p.fold.end
}

// shutEnd is the last line folded under line, -1 when it heads no fold.
func (p *preview) shutEnd(line int) int {
	if len(p.fold.shut) == 0 {
		return -1
	}

	end := p.foldEnd()
	if _, ok := slices.BinarySearch(p.fold.shut, line); !ok || line >= len(end) {
		return -1
	}

	return int(end[line])
}

// hiddenBy is the outermost fold line is hidden in, its header; -1 when the
// line shows.
func (p *preview) hiddenBy(line int) int {
	for _, h := range p.fold.shut {
		if e := p.shutEnd(h); h < line && line <= e {
			return h
		}
	}

	return -1
}

// setFold folds or unfolds the block line heads; false when it heads none.
func (p *preview) setFold(line int, shut bool) bool {
	end := p.foldEnd()
	if line < 0 || line >= len(end) || end[line] <= int32(line) {
		return false
	}

	i, in := slices.BinarySearch(p.fold.shut, line)

	switch {
	case shut && !in:
		p.fold.shut = slices.Insert(p.fold.shut, i, line)
		if h := p.hiddenBy(p.cur.line); h >= 0 { // the cursor folded away goes to the header, as VS Code's
			p.cur, p.anchor = pos{h, min(p.cur.col, p.lineLen(h))}, nil
		}

	case !shut && in:
		p.fold.shut = slices.Delete(p.fold.shut, i, i+1)
	default:
		return true
	}

	p.fold.gen++
	p.vis = nil

	return true
}

// blockAt is the innermost block holding line, its header; -1 for none.
func (p *preview) blockAt(line int) int {
	end := p.foldEnd()
	for h := min(line, len(end)-1); h >= 0; h-- {
		if int(end[h]) >= line && end[h] > int32(h) {
			return h
		}
	}

	return -1
}

// foldCmd runs a fold command at the cursor: "fold" the innermost block
// holding it, "unfold" the one it heads, "toggle" either, "all" every
// block, "none" none.
func (m *Model) foldCmd(what string) tea.Cmd {
	p := &m.pv
	if !p.foldable() {
		return flash("open a file first", true)
	}

	line := p.at().line

	switch what {
	case "fold":
		p.setFold(p.blockAt(line), true)

	case "unfold":
		p.setFold(line, false)
	case "toggle":
		if p.shutEnd(line) > line {
			p.setFold(line, false)
		} else {
			p.setFold(p.blockAt(line), true)
		}

	case "all":
		for h, e := range p.foldEnd() {
			if e > int32(h) {
				p.setFold(h, true)
			}
		}

	case "none":
		p.fold.shut, p.vis = nil, nil
		p.fold.gen++
	}

	p.follow(m.pvW(), m.pvH())

	return nil
}

// unfoldAt opens every fold that hides line: a jump, a find hit, Go to Line
// landing in one shows it.
func (p *preview) unfoldAt(line int) {
	for h := p.hiddenBy(line); h >= 0; h = p.hiddenBy(line) {
		p.setFold(h, false)
	}
}

// visibleLine is line moved out of a fold: onto its header going up, past
// its end going down, the header again at the file's end.
func (p *preview) visibleLine(line, d int) int {
	h := p.hiddenBy(line)
	if h < 0 {
		return line
	}

	if e := p.shutEnd(h); d > 0 && e < p.lastLine() {
		return e + 1
	}

	return h
}

// foldClick folds or unfolds the block whose mark is at body cell (x, y),
// in the fold lane after the numbers; false when there is none there.
func (p *preview) foldClick(w, h, x, y int) bool {
	vis := p.rows(w)
	if x != p.gutter()-2 || y < 0 || y >= h || p.top+y >= len(vis) {
		return false
	}

	line := vis[p.top+y].line

	return line >= 0 && p.setFold(line, p.shutEnd(line) <= line)
}

// shiftFolds keeps folds on their lines after an edit replaced lines
// [from, oldTo) with [from, newTo): those below move along.
func (p *preview) shiftFolds(from, oldTo, newTo int) {
	if len(p.fold.shut) == 0 || oldTo == newTo {
		return
	}

	for i, h := range p.fold.shut {
		if h >= oldTo {
			p.fold.shut[i] = h + newTo - oldTo
		} else if h > from && h >= newTo { // its header went with the edit
			p.fold.shut[i] = -1
		}
	}

	p.fold.shut = slices.DeleteFunc(p.fold.shut, func(h int) bool { return h < 0 })
	p.fold.gen++
}

// foldMark is line's mark in the gutter's fold lane, between pre and suf:
// ▸ on a folded block, ▾ on one that folds while the pointer is over the
// gutter, none else.
func (p *preview) foldMark(line int, pre, suf, none string) string {
	switch {
	case p.shutEnd(line) > line:
		return pre + "▸" + suf
	case p.gutHot && p.foldable():
		if end := p.foldEnd(); line < len(end) && end[line] > int32(line) {
			return pre + "▾" + suf
		}
	}

	return none
}
