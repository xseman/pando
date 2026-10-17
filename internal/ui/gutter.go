package ui

import (
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xseman/pando/internal/git"
)

// The editor's gutter marks its lines against the file's staged text, VS
// Code's quick diff: a bar on a line added and on one changed, a mark where
// lines were deleted, kept up as the text changes; a file with staged
// changes has those marked too, dimmer, against its committed text.

// stagedText is file path's staged text and, when that differs, its
// committed text (none for a file new to the index), split as newBuffer
// splits a file; a file with no staged text has neither.
func stagedText(path string) (base, head []string) {
	lines := func(s string) []string {
		return strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	}

	staged, err := git.FileAt("", path)
	if err != nil {
		return nil, nil
	}

	committed, err := git.FileAt("HEAD", path)

	switch {
	case err != nil:
		return lines(staged), []string{}
	case committed != staged:
		return lines(staged), lines(committed)
	}

	return lines(staged), nil
}

// gutterKey is the text a file's marks were worked out for: its buffer, its
// count of changes, and the staged text it was compared with.
type gutterKey struct {
	buf          *buffer
	edits, based int
}

// gutterMsg is a file's changes and the marks they give its lines, worked
// out off the update loop.
type gutterMsg struct {
	path    string
	key     gutterKey
	changes []change
	chg     []byte
}

// change is one change of the editor's text against the file's staged
// text, or for a staged one against its committed text: the original's
// lines [a0, a1) became the editor's [b0, b1), none for a deletion, which
// took them from before line b0.
type change struct {
	a0, a1, b0, b1 int
	staged         bool
}

// noteGutter works out the shown file's changes again when its text or its
// staged text changed since; a file with no staged text has none.
func (m *Model) noteGutter() tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || p.buf == nil {
		return nil
	}

	if p.base == nil {
		p.chg, p.changes, p.chgKey = nil, nil, gutterKey{}
		return nil
	}

	key := gutterKey{p.buf, p.buf.edits, p.baseGen}
	if key == p.chgKey {
		return nil
	}

	p.chgKey = key
	path, base, head, lines := p.path, p.base, p.head, p.rawLines()

	return func() tea.Msg {
		cs := diffChanges(base, lines)
		if head != nil { // the staged changes no unstaged one touches, VS Code's secondary quick diff
			unstaged := slices.Clone(cs)
			for _, s := range diffChanges(head, lines) {
				if !slices.ContainsFunc(unstaged, func(c change) bool { return s.b0 <= c.b1 && c.b0 <= s.b1 }) {
					s.staged = true
					cs = append(cs, s)
				}
			}

			slices.SortStableFunc(cs, func(x, y change) int { return x.b0 - y.b0 })
		}

		return gutterMsg{path, key, cs, changeMarks(len(lines), cs)}
	}
}

// changeAt is the change line's mark stands for, -1 for none: the one over
// it, or a deletion marked on it.
func (p *preview) changeAt(line int) int {
	return slices.IndexFunc(p.changes, func(c change) bool {
		return line >= c.b0 && line < c.b1 || c.b0 == c.b1 && line == max(c.b0-1, 0)
	})
}

// laneChange is the change under body cell (x, y) of the gutter's mark lane
// and the cells either side, -1 for none.
func (p *preview) laneChange(w, h, x, y int) int {
	rows, gw := p.rows(w), p.gutter()
	if y < 0 || y >= h || x < gw-3 || x >= gw || p.top+y >= len(rows) {
		return -1
	}

	return p.changeAt(rows[p.top+y].line)
}

// changeMark is line's mark in the gutter's lane, VS Code's: ┃ added, ┇
// modified (its hatched bar), ▾ where lines were deleted after it (▴ before
// the first), on a line's first row; a staged change's the same in VS
// Code's secondary shade; the change under the pointer wider, █ and ▒; a
// blank in st else.
func (p *preview) changeMark(line int, first bool, st lipgloss.Style) string {
	if line >= len(p.chg) || p.chg[line] == 0 {
		return st.Render(" ")
	}

	k, shade := p.chg[line], func(c color.Color) color.Color { return c }
	if k < 'a' { // staged: darker on a dark theme, lighter on a light one
		k, shade = k-'A'+'a', func(c color.Color) color.Color {
			return ifElse(lookDark, lipgloss.Darken(c, 0.5), lipgloss.Lighten(c, 0.7))
		}
	}

	hot := p.markHot > 0 && p.changeAt(line) == p.markHot-1

	switch {
	case k == 'a':
		return st.Foreground(shade(pal.diffAddMark)).Render(ifElse(hot, "█", "┃"))
	case k == 'm':
		return st.Foreground(shade(pal.diffModMark)).Render(ifElse(hot, "▒", "┇"))
	case first:
		return st.Foreground(shade(pal.diffDelMark)).Bold(hot).Render(ifElse(k == 'd', "▾", "▴"))
	}

	return st.Render(" ")
}

// stepChange is the change after (d 1) or before (d -1) line, around the
// ends; -1 for none.
func (p *preview) stepChange(line, d int) int {
	n := len(p.changes)
	if n == 0 {
		return -1
	}

	if d > 0 {
		return max(slices.IndexFunc(p.changes, func(c change) bool { return c.b0 > line }), 0)
	}

	for i := n - 1; i >= 0; i-- {
		if p.changes[i].b0 < line {
			return i
		}
	}

	return n - 1
}

// gotoFileChange is VS Code's Go to Next Change (alt+F5) and Previous in a
// file: the cursor to the change's first line, mid view.
func (m *Model) gotoFileChange(d int) tea.Cmd {
	p := &m.pv

	i := p.stepChange(p.at().line, d)
	if i < 0 {
		return flash("no changes", false)
	}

	p.gotoLine(m, p.changes[i].b0, 0)

	return nil
}

// maxEdits bounds the diff the gutter works out.
// ponytail: past it the changed middle shows as one modification.
const maxEdits = 1000

// diffChanges are now's changes against base, VS Code's quick diff's: each
// run of removed and added lines between kept ones, found by Myers'
// algorithm between their common prefix and suffix; past maxEdits that
// middle is one change.
func diffChanges(base, now []string) []change {
	pre := 0
	for pre < len(base) && pre < len(now) && base[pre] == now[pre] {
		pre++
	}

	suf := 0
	for suf < len(base)-pre && suf < len(now)-pre && base[len(base)-1-suf] == now[len(now)-1-suf] {
		suf++
	}

	ops, ok := editScript(base[pre:len(base)-suf], now[pre:len(now)-suf])
	if !ok {
		return []change{{a0: pre, a1: len(base) - suf, b0: pre, b1: len(now) - suf}}
	}

	var out []change

	for k, i, j := 0, pre, pre; k < len(ops); {
		if ops[k] == '=' {
			k, i, j = k+1, i+1, j+1
			continue
		}

		c := change{a0: i, b0: j}
		for ; k < len(ops) && ops[k] != '='; k++ {
			i, j = i+b2i(ops[k] == '-'), j+b2i(ops[k] == '+')
		}

		c.a1, c.b1 = i, j
		out = append(out, c)
	}

	return out
}

// changeMarks are the marks changes give n lines, as VS Code draws them: 'a'
// on the lines of a change that only added some, 'm' on those of one that
// also removed some, 'd' on the line lines were deleted after, 'u' on the
// first when they were deleted before it; upper case for a staged change, 0
// on the rest.
func changeMarks(n int, cs []change) []byte {
	out := make([]byte, n)

	for _, c := range cs {
		up := ifElse(c.staged, byte('a'-'A'), 0)

		switch {
		case c.b1 > c.b0:
			for j := c.b0; j < c.b1; j++ {
				out[j] = ifElse(c.a1 == c.a0, byte('a'), 'm') - up
			}

		case c.b0 > 0 && out[c.b0-1] == 0:
			out[c.b0-1] = 'd' - up
		case c.b0 == 0 && n > 0 && out[0] == 0:
			out[0] = 'u' - up
		}
	}

	return out
}

// editScript is a shortest way from a to b, by Myers' algorithm: '=' keeps
// a line, '-' removes one of a, '+' adds one of b; false past maxEdits.
func editScript(a, b []string) ([]byte, bool) {
	n, m := len(a), len(b)
	off := n + m + 1
	v := make([]int, 2*off+1) // the furthest x on each diagonal k = x-y, at off+k

	var trace [][]int // v before each step d, diagonals -d-1 to d+1

	for d := 0; d <= n+m; d++ {
		if d > maxEdits {
			return nil, false
		}

		trace = append(trace, slices.Clone(v[off-d-1:off+d+2]))

		for k := -d; k <= d; k += 2 {
			x := v[off+k-1] + 1 // from the left: one of a removed
			if k == -d || k != d && v[off+k-1] < v[off+k+1] {
				x = v[off+k+1] // from above: one of b added
			}

			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}

			v[off+k] = x

			if x >= n && y >= m {
				return backtrack(trace, n, m), true
			}
		}
	}

	return nil, false
}

// backtrack walks editScript's steps back from the end to the start.
func backtrack(trace [][]int, x, y int) []byte {
	var ops []byte

	for d := len(trace) - 1; d >= 0; d-- {
		at := func(k int) int { return trace[d][k+d+1] }

		k := x - y

		pk := k - 1
		if k == -d || k != d && at(k-1) < at(k+1) {
			pk = k + 1
		}

		px := at(pk)
		py := px - pk

		for x > px && y > py {
			ops, x, y = append(ops, '='), x-1, y-1
		}

		if d > 0 {
			ops = append(ops, ifElse(x == px, byte('+'), '-'))
		}

		x, y = px, py
	}

	slices.Reverse(ops)

	return ops
}
