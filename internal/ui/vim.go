package ui

import (
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// ponytail: the keys vim users reach for first, no more — no ex commands
// (⌃s saves, ⌃w closes), macros, marks, named registers, "." or text objects.
// Each one is its own case in vimAction or vimOperate when it is missed.

// The modes `vim_mode` puts an editor in. The zero value is normal, so an
// editor opens there and a reopened tab comes back there.
const (
	vimNormal = iota
	vimInsert
	vimChar // visual, character-wise
	vimLine // visual, line-wise
)

// vimState is one editor's vim state: the mode, and the keys of a command
// that is not complete yet — "2d" waiting for its motion, "g" for its second g.
type vimState struct {
	mode int
	pend string
}

// vimOn reports an editor taking vim keys: the setting is on and the text is
// editable, so a diff, a revision and a rendering keep their own letters.
func (p *preview) vimOn(m *Model) bool { return m.st.Settings.Vim && p.editable() }

// vimLabel is the mode shown in the header, with the keys still pending.
func (p *preview) vimLabel() string {
	name := [...]string{"NORMAL", "INSERT", "VISUAL", "V-LINE"}[p.vim.mode]
	if p.vim.pend != "" {
		return name + " " + p.vim.pend
	}

	return name
}

// vimKey runs the editor's vim keys; ok is false when the key belongs to the
// editor itself. Insert mode claims nothing but esc, so typing, suggestions
// and every ctrl key work exactly as they do with the setting off.
func (p *preview) vimKey(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !p.vimOn(m) || len(p.plain) == 0 {
		return nil, false
	}

	switch s := k.String(); {
	case p.vim.mode == vimInsert:
		if s == "esc" {
			p.vimEsc(m)
			return nil, true
		}

		return nil, false

	case s == "esc":
		if p.vim.pend == "" && p.vim.mode == vimNormal {
			return nil, false // nothing to leave: esc still closes the editor
		}

		p.vimEsc(m)

		return nil, true

	case s == "ctrl+r":
		return p.history(m, false), true
	case k.Mod&^tea.ModShift != 0 || k.Text == "":
		return nil, false // arrows, ^s, ^f, F12 … keep working in every mode
	}

	return p.vimRun(m, k.Text), true
}

// vimEsc goes back to normal mode and drops the selection and the half-typed
// command with it. Leaving insert mode steps the cursor back onto a rune, as
// vim does.
func (p *preview) vimEsc(m *Model) {
	p.vim = vimState{}
	p.anchor = nil
	p.cur = p.vimClamp(p.at())
	p.follow(m.pvW(), m.pvH())
}

// vimClamp keeps the normal-mode cursor on a rune rather than past the line
// end, where only insert and visual mode may sit.
func (p *preview) vimClamp(c pos) pos {
	if p.vim.mode == vimNormal && c.col > 0 && c.col >= p.lineLen(c.line) {
		c.col = max(p.lineLen(c.line)-1, 0)
	}

	return c
}

// vimRun takes one typed character, appends it to whatever was pending and
// runs the command once it is whole.
func (p *preview) vimRun(m *Model, key string) tea.Cmd {
	cmd := p.vim.pend + key
	p.vim.pend = ""

	n, rest := vimCount(cmd)
	if rest == "" { // digits so far, the command follows
		p.vim.pend = cmd
		return nil
	}

	if p.vim.mode != vimNormal {
		return p.vimVisual(m, n, rest)
	}

	if op := rest[:1]; strings.ContainsAny(op, "dcy") {
		if len(rest) == 1 {
			p.vim.pend = cmd
			return nil
		}

		return p.vimOperate(m, op, rest[1:], max(n, 1))
	}

	return p.vimAction(m, rest, n)
}

// vimCount splits a leading repeat count off a command; 0 is no count at all,
// as "0" on its own is the motion to the line start.
func vimCount(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' && (i != 0 || s[i] != '0') {
		i++
	}

	n, _ := strconv.Atoi(s[:i])

	return n, s[i:]
}

// vimAction runs a normal-mode command that is not an operator: a motion, a
// way into insert mode, or an edit of its own.
func (p *preview) vimAction(m *Model, key string, n int) tea.Cmd {
	c := p.at()

	switch key {
	case "g": // gg, or the g of dgg
		p.vim.pend = key
		return nil

	case "i", "a", "I", "A":
		switch key {
		case "a":
			p.cur = pos{c.line, min(c.col+1, p.lineLen(c.line))}
		case "I":
			p.cur = pos{c.line, len(indentOf(p.plain[c.line]))}
		case "A":
			p.cur = pos{c.line, p.lineLen(c.line)}
		}

		p.vimInsert(m)

		return nil

	case "o", "O":
		at, text := pos{c.line, p.lineLen(c.line)}, "\n"+indentOf(p.plain[c.line])
		if key == "O" {
			at, text = pos{c.line, 0}, indentOf(p.plain[c.line])+"\n"
		}

		cmd := p.edit(m, at, at, text)
		if key == "O" { // the new line holds the indent alone, so its end is the caret
			p.setCursor(m, pos{c.line, p.lineLen(c.line)})
		}

		p.vimInsert(m)

		return cmd

	case "x", "D", "C":
		z := pos{c.line, min(c.col+max(n, 1), p.lineLen(c.line))}
		if key != "x" {
			z = pos{c.line, p.lineLen(c.line)}
		}

		m.vimReg, m.vimRegLine = p.textIn(c, z), false

		cmd := p.edit(m, c, z, "")
		if key == "C" {
			p.vimInsert(m)
		}

		return cmd

	case "J":
		if c.line >= p.lastLine() {
			return nil
		}

		z := pos{c.line + 1, len(indentOf(p.plain[c.line+1]))}

		return p.edit(m, pos{c.line, p.lineLen(c.line)}, z, " ")

	case "p", "P":
		return p.vimPaste(m, key == "p", max(n, 1))
	case "u":
		return p.history(m, true)
	case "v", "V":
		a := p.at()
		p.anchor, p.vim.mode = &a, map[bool]int{true: vimChar, false: vimLine}[key == "v"]

		return nil

	case "/":
		return p.startFind(m)
	case "n", "N":
		if len(p.hits) > 0 {
			return p.findGo(m, map[bool]int{true: -1, false: 1}[key == "N"])
		}

		return nil

	case ":":
		return m.gotoLineQuery(":")
	}

	if t, ok := p.vimMove(m, key, n); ok {
		p.anchor = nil
		p.setCursor(m, p.vimClamp(t))
	}

	return nil
}

// vimInsert leaves normal mode; the editor's own keys take over from here.
func (p *preview) vimInsert(m *Model) {
	p.vim = vimState{mode: vimInsert}
	p.anchor = nil
	p.follow(m.pvW(), m.pvH())
}

// vimMove is where a motion lands the cursor, n times over; ok is false when
// the keys are not a motion. The shared ones go through moved(), the cursor
// walk every preview already uses.
func (p *preview) vimMove(m *Model, motion string, n int) (pos, bool) {
	switch motion {
	case "G":
		if n > 0 {
			return pos{min(n-1, p.lastLine()), 0}, true
		}

	case "w", "b", "e":
		c := p.at()
		for range max(n, 1) {
			c = p.vimWord(c, motion)
		}

		return c, true

	case "h", "j", "k", "l", "0", "$", "gg":
		motion = strings.TrimSuffix(motion, "g") // gg is moved()'s g: the file's start
	default:
		return pos{}, false
	}
	// moved() walks from the cursor, so step it and put it back.
	was := p.cur
	for range max(n, 1) {
		np, ok := p.moved(motion, m.pvH())
		if !ok {
			p.cur = was
			return pos{}, false
		}

		p.cur = np
	}

	t := p.at()
	p.cur = was

	return t, true
}

// vimClass splits runes the way vim's word motions do: whitespace, word
// characters, and punctuation as a class of its own.
func vimClass(r rune) int {
	switch {
	case r == '\n' || unicode.IsSpace(r):
		return 0
	case isWordRune(r):
		return 1
	}

	return 2
}

// runeAt is the rune under a position; past the line end it is the newline.
func (p *preview) runeAt(c pos) rune {
	if c.col >= p.lineLen(c.line) {
		return '\n'
	}

	return p.plain[c.line][c.col]
}

// fwd and back step one position through the text, across line ends; ok is
// false at the two ends of the file.
func (p *preview) fwd(c pos) (pos, bool) {
	switch {
	case c.col < p.lineLen(c.line):
		return pos{c.line, c.col + 1}, true
	case c.line < p.lastLine():
		return pos{c.line + 1, 0}, true
	}

	return c, false
}

func (p *preview) back(c pos) (pos, bool) {
	switch {
	case c.col > 0:
		return pos{c.line, c.col - 1}, true
	case c.line > 0:
		return pos{c.line - 1, p.lineLen(c.line - 1)}, true
	}

	return c, false
}

// vimWord is one w, b or e: to the next word start, the previous one, or the
// end of the word ahead, counting punctuation as a word of its own.
func (p *preview) vimWord(c pos, kind string) pos {
	step := p.fwd
	if kind == "b" {
		step = p.back
	}

	from := vimClass(p.runeAt(c))

	c, ok := step(c)
	if !ok {
		return c
	}

	if kind == "w" { // leave the run the cursor stood in
		for from != 0 && vimClass(p.runeAt(c)) == from {
			if c, ok = step(c); !ok {
				return c
			}
		}
	}

	for vimClass(p.runeAt(c)) == 0 { // then over the whitespace
		if c, ok = step(c); !ok {
			return c
		}
	}

	if kind == "w" {
		return c
	}

	for cl := vimClass(p.runeAt(c)); ; { // b and e land on the far end of the run
		next, ok := step(c)
		if !ok || vimClass(p.runeAt(next)) != cl {
			return c
		}

		c = next
	}
}

// textIn is the text between two display positions, out of the buffer so a
// tab comes back as a tab.
func (p *preview) textIn(a, z pos) string {
	return p.buf.textIn(p.rawPos(a), p.rawPos(z))
}

// vimLines is n lines from line as one linewise register entry, newline and all.
func (p *preview) vimLines(line, n int) string {
	var sb strings.Builder
	for i := line; i < min(line+n, len(p.plain)); i++ {
		sb.WriteString(string(p.buf.line(i)) + "\n")
	}

	return sb.String()
}

// vimOperate is d, c or y over a motion, or doubled over whole lines.
func (p *preview) vimOperate(m *Model, op, motion string, n int) tea.Cmd {
	c := p.at()
	if motion == "g" { // dgg and friends need their second g
		p.vim.pend = strconv.Itoa(n) + op + motion
		return nil
	}

	if motion == op { // dd, cc, yy
		return p.vimLineOp(m, op, c.line, n)
	}

	if op == "c" && motion == "w" { // cw is ce: vim leaves the space after the word
		motion = "e"
	}

	t, ok := p.vimMove(m, motion, n)
	if !ok {
		return nil
	}

	if motion == "gg" || motion == "G" { // the linewise motions take whole lines
		from, to := min(c.line, t.line), max(c.line, t.line)
		return p.vimLineOp(m, op, from, to-from+1)
	}

	if motion == "w" && t.line > c.line && c.col < p.lineLen(c.line) {
		t = pos{c.line, p.lineLen(c.line)} // dw on the last word stops at the line end
	}

	a, z := c, t
	if !posLE(a, z) {
		a, z = z, a
	}

	if motion == "e" { // e is inclusive, so the range takes the rune it landed on
		z, _ = p.fwd(z)
	}

	m.vimReg, m.vimRegLine = p.textIn(a, z), false
	if op == "y" {
		p.anchor = nil
		p.setCursor(m, a)

		return nil
	}

	cmd := p.edit(m, a, z, "")
	if op == "c" {
		p.vimInsert(m)
	}

	return cmd
}

// vimLineOp is dd, cc and yy over n lines: cc empties them and keeps one with
// its indent, dd takes the newline with it.
func (p *preview) vimLineOp(m *Model, op string, line, n int) tea.Cmd {
	last := min(line+n-1, p.lastLine())
	m.vimReg, m.vimRegLine = p.vimLines(line, last-line+1), true

	switch op {
	case "y":
		p.anchor = nil
		p.setCursor(m, pos{line, 0})

		return nil

	case "c":
		cmd := p.edit(m, pos{line, 0}, pos{last, p.lineLen(last)}, indentOf(p.plain[line]))
		p.vimInsert(m)

		return cmd
	}

	a, z := pos{line, 0}, pos{last + 1, 0}
	if last >= p.lastLine() { // the last line takes the newline before it instead
		a, z = pos{max(line-1, 0), 0}, pos{last, p.lineLen(last)}
		if line > 0 {
			a = pos{line - 1, p.lineLen(line - 1)}
		}
	}

	return p.edit(m, a, z, "")
}

// vimPaste puts the register after the cursor (p) or before it (P).
func (p *preview) vimPaste(m *Model, after bool, n int) tea.Cmd {
	if m.vimReg == "" {
		return nil
	}

	text, c := strings.Repeat(m.vimReg, n), p.at()
	if !m.vimRegLine {
		at := c
		if after && p.lineLen(c.line) > 0 {
			at.col = min(c.col+1, p.lineLen(c.line))
		}

		return p.edit(m, at, at, text)
	}

	line := c.line + b2i(after)
	if line > p.lastLine() { // pasting under the last line makes the newline itself
		end := pos{c.line, p.lineLen(c.line)}
		return p.edit(m, end, end, "\n"+strings.TrimSuffix(text, "\n"))
	}

	cmd := p.edit(m, pos{line, 0}, pos{line, 0}, text)
	p.setCursor(m, pos{line, 0})

	return cmd
}

// vimSel is what visual mode has selected: the cursor's rune counts, and
// V takes whole lines.
func (p *preview) vimSel() (a, z pos) {
	a, z, _ = p.selection()
	if p.vim.mode == vimLine {
		return pos{a.line, 0}, pos{z.line, p.lineLen(z.line)}
	}

	z, _ = p.fwd(z)

	return a, z
}

// vimVisual runs a key in visual mode: an operator over the selection, a
// motion that grows it, or a switch between the two visual modes.
func (p *preview) vimVisual(m *Model, n int, key string) tea.Cmd {
	if p.anchor == nil {
		p.vimEsc(m)
		return nil
	}

	switch key {
	case "g":
		p.vim.pend = key
		return nil

	case "v", "V":
		want := map[bool]int{true: vimChar, false: vimLine}[key == "v"]
		if p.vim.mode == want {
			p.vimEsc(m)
			return nil
		}

		p.vim.mode = want

		return nil

	case "o":
		a := p.at()
		p.cur, p.anchor = *p.anchor, &a

		return nil

	case "d", "x", "y", "c", "s":
		op := map[string]string{"x": "d", "s": "c"}[key] // x is d, s is c, over a selection
		if op == "" {
			op = key
		}

		a, z := p.vimSel()
		lineWise := p.vim.mode == vimLine

		p.vim.mode, p.anchor = vimNormal, nil
		if lineWise {
			return p.vimLineOp(m, op, a.line, z.line-a.line+1)
		}

		m.vimReg, m.vimRegLine = p.textIn(a, z), false
		if op == "y" {
			p.setCursor(m, a)
			return nil
		}

		cmd := p.edit(m, a, z, "")
		if op == "c" {
			p.vimInsert(m)
		}

		return cmd
	}

	if t, ok := p.vimMove(m, key, n); ok {
		p.cur = t // the anchor stays put: the selection grows
		p.follow(m.pvW(), m.pvH())
	}

	return nil
}
