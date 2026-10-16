package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// changes are a diff's runs of added and deleted lines, [from, to) in meta
// lines. A deletion and the additions that replace it are one change, as
// VS Code's Go to Next Change counts them.
func changes(meta []lineMeta) [][2]int {
	var out [][2]int

	for i := 0; i < len(meta); {
		if !changed(meta[i].kind) {
			i++
			continue
		}

		from := i
		for i < len(meta) && changed(meta[i].kind) {
			i++
		}

		out = append(out, [2]int{from, i})
	}

	return out
}

func changed(kind byte) bool { return kind == 'a' || kind == 'd' }

// stepTo is the first of starts after line c (d 1) or the last before it
// (d -1), around the ends as VS Code's next change goes; ok is false when
// there are none.
func stepTo(starts []int, c, d int) (int, bool) {
	if len(starts) == 0 {
		return 0, false
	}

	if d > 0 {
		if i := slices.IndexFunc(starts, func(s int) bool { return s > c }); i >= 0 {
			return starts[i], true
		}

		return starts[0], true
	}

	for _, s := range slices.Backward(starts) {
		if s < c {
			return s, true
		}
	}

	return starts[len(starts)-1], true
}

// gotoChange is VS Code's Go to Next Change (d 1) or Previous Change (d -1)
// in a diff: the cursor goes to the change's first line, in the middle of
// the view.
func (p *preview) gotoChange(m *Model, d int) tea.Cmd {
	var starts []int
	for _, c := range changes(p.meta) {
		starts = append(starts, c[0])
	}

	n, ok := stepTo(starts, p.at().line, d)
	if !ok {
		return flash("no changes", false)
	}

	p.showLine(m, n, false)

	return nil
}

// showLine puts the cursor on meta line n with its row in the middle of the
// view, or on the top row with top set, in either diff layout.
func (p *preview) showLine(m *Model, n int, top bool) {
	p.setLine(m, n)
	p.scrollTo(m, ifElse(top, 0, m.pvH()/2))
}

// setLine puts the cursor on meta line n, the whole line as a diff's cursor
// is, without a selection.
func (p *preview) setLine(m *Model, n int) {
	p.typed, p.anchor = false, nil
	if p.split(m) {
		p.selectTo(n, false)
	} else {
		p.cur = pos{max(min(n, p.lastLine()), 0), 0}
	}
}

// row is the cursor's row among the view's rows, wrapped or side by side.
func (p *preview) row(m *Model) int {
	if p.split(m) {
		return splitIndex(splitRows(p.meta), p.at().line)
	}

	return p.cursorRow(m.pvW())
}

// scrollTo scrolls so the cursor's row is off rows below the top of the
// view; the cursor stays.
func (p *preview) scrollTo(m *Model, off int) {
	if !p.split(m) {
		p.follow(m.pvW(), m.pvH()) // and sideways
	}

	p.top = max(p.row(m)-max(min(off, m.pvH()-1), 0), 0)
}

// place is where a diff's cursor was by the file's line numbers, for a load
// whose rows moved to find again: a reload after an agent's edit above it,
// or the other context mode (the whole file, or only the changes).
type place struct {
	at, from          lineMeta // the cursor's line and, with sel, the selection's other end
	sel               bool
	col, fromCol, off int // off: the cursor's row on screen
	was               pos // the cursor then: moved since, it stays where it went (onLoad)
}

// lineNo is the file's line number meta line n of a diff shows, as VS Code's
// status bar counts: the new side's, a deletion's old one; n+1 anywhere
// else.
func (p *preview) lineNo(n int) int {
	if n < len(p.meta) {
		if l := p.meta[n]; l.b > 0 || l.a > 0 {
			return ifElse(l.b > 0, l.b, l.a)
		}
	}

	return n + 1
}

// keepPlace notes the cursor's place for the next load (onLoad).
func (p *preview) keepPlace(m *Model) {
	c := p.at()
	if c.line >= len(p.meta) {
		return
	}

	k := place{at: p.meta[c.line], col: c.col, off: p.row(m) - p.top, was: p.cur}
	if a := p.anchor; a != nil && a.line < len(p.meta) {
		k.from, k.sel, k.fromCol = p.meta[a.line], true, a.col
	}

	p.keep = &k
}

// findAgain puts the cursor and the selection back where keepPlace noted
// them, by line number, on the same screen row.
func (p *preview) findAgain(m *Model) {
	k := *p.keep
	p.keep = nil

	n := findPlace(p.meta, k.at)
	p.cur, p.anchor = pos{n, min(k.col, p.lineLen(n))}, nil

	if k.sel {
		f := findPlace(p.meta, k.from)
		p.anchor = &pos{f, min(k.fromCol, p.lineLen(f))}
	}

	p.scrollTo(m, k.off)
}

// findPlace is the row of meta for the line k was: the same row, else the
// first at or after its new line number (its old one for a deletion), else
// the last row: the file ends before it.
func findPlace(meta []lineMeta, k lineMeta) int {
	if i := slices.Index(meta, k); i >= 0 {
		return i
	}

	if i := slices.IndexFunc(meta, func(l lineMeta) bool {
		if k.b > 0 {
			return l.b >= k.b
		}

		return k.a > 0 && l.a >= k.a
	}); i >= 0 {
		return i
	}

	return max(len(meta)-1, 0)
}

// stepFile goes to the next (d 1) or previous (d -1) file: in a commit's or
// a pull request's diff to the next file's name, on the top row; in one
// file's, to the diff of the next file in Source Control (in the Compare
// drawer for a Compare diff), selected there too. Both go around the ends.
func (m *Model) stepFile(d int) tea.Cmd {
	p := &m.pv

	switch p.kind {
	case pvShow, pvGHDiff:
		var starts []int

		for i, l := range p.meta {
			if l.kind == 'f' {
				starts = append(starts, i)
			}
		}

		if len(starts) < 2 { // the file names show only when there are several
			return flash("one file", false)
		}

		c := p.at().line
		if i := slices.IndexFunc(starts, func(s int) bool { return s > c }); d < 0 && i != 0 { // from inside a file, back is the file before it
			c = starts[ifElse(i < 0, len(starts), i)-1]
		}

		n, _ := stepTo(starts, c, d)

		p.showLine(m, n, true)
		// The cursor goes on to the file's first change, under its name, as
		// VS Code's multi-file diff puts it in the file.
		next := slices.IndexFunc(starts, func(s int) bool { return s > n })
		for _, ch := range changes(p.meta) {
			if ch[0] > n && (next < 0 || ch[0] < starts[next]) {
				p.setLine(m, ch[0])

				if p.row(m) >= p.top+m.pvH() {
					p.scrollTo(m, m.pvH()/2)
				}

				break
			}
		}

		return nil

	case pvDiff, pvCmp:
	default:
		return nil
	}

	s, cmp := &m.scm, p.kind == pvCmp
	cur, files := -1, []int(nil)

	for i, r := range s.rows {
		if r.kind != rowFile || (r.title == cmpTitle) != cmp {
			continue
		}

		if r.entry.Path == p.path && (cmp || r.root == p.root && r.entry.Staged == p.entry.Staged) {
			cur = len(files)
		}

		files = append(files, i)
	}

	if len(files) == 0 {
		return nil
	}

	k := (cur + d + len(files)) % len(files)
	if cur < 0 {
		k = ifElse(d > 0, 0, len(files)-1)
	}

	i := files[k]
	s.sel = i
	s.snap(m, i)

	return tea.Batch(s.onSelect(m), s.activate(m, &s.rows[i], true))
}

// barChanges are the diff's rows as its scrollbar counts them, wrapped or
// side by side: each row's change bits for vbar.marked and the meta line it
// shows, the deletion's of a side-by-side pair. Nil for anything but a diff.
func (p *preview) barChanges(m *Model) (kinds []byte, lines []int) {
	if p.meta == nil {
		return nil, nil
	}

	bit := func(n int) byte {
		switch {
		case n < 0:
			return 0
		case p.meta[n].kind == 'd':
			return 1
		case p.meta[n].kind == 'a':
			return 2
		}

		return 0
	}

	if p.split(m) {
		for _, sr := range splitRows(p.meta) {
			kinds = append(kinds, bit(sr.l)|bit(sr.r))
			lines = append(lines, ifElse(sr.l >= 0, sr.l, sr.r))
		}

		return kinds, lines
	}

	for _, vr := range p.rows(m.pvW()) {
		kinds, lines = append(kinds, bit(vr.line)), append(lines, vr.line)
	}

	return kinds, lines
}

// markAt is the first line of a change marked on scrollbar row y, off the
// slider, which a click goes to as VS Code's overview ruler does. A cell
// holds every change whose rows land on it: from the cursor on one of them,
// the click goes to the next, so each is a click away.
func (p *preview) markAt(m *Model, y int) (int, bool) {
	b := p.bar(m)
	if ty, sh := b.thumb(); !b.on() || y >= ty && y < ty+sh {
		return 0, false
	}

	kinds, lines := p.barChanges(m)
	cs := changes(p.meta)

	var starts []int

	for r, k := range kinds {
		if k == 0 || b.halfOf(r)/2 != y {
			continue
		}

		i, ok := slices.BinarySearchFunc(cs, lines[r], func(c [2]int, n int) int {
			return ifElse(c[1] <= n, -1, ifElse(c[0] > n, 1, 0))
		})
		if ok && !slices.Contains(starts, cs[i][0]) {
			starts = append(starts, cs[i][0])
		}
	}

	if len(starts) == 0 {
		return 0, false
	}

	if i := slices.Index(starts, p.at().line); i >= 0 {
		return starts[(i+1)%len(starts)], true
	}

	return starts[0], true
}
