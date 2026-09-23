package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// errChangedOnDisk stops a save from overwriting someone else's write.
var errChangedOnDisk = errors.New("the file changed on disk")

// buffer is the editable text behind a file preview: the file's raw lines
// (tabs kept, unlike the preview's expanded ones), an undo stack of splices,
// and what the file looked like on disk when it was read.
type buffer struct {
	lines  [][]rune
	endNL  bool // the file ended with a newline, so saving keeps one
	dirty  bool
	undos  []edit
	redos  []edit
	modAt  time.Time // mtime at load: a newer one means someone else wrote
	typing bool      // the last change was a plain insert, so the next one coalesces
}

// edit undoes one change: put text back over the range [a, z).
type edit struct {
	a, z pos
	text string
	cur  pos // where the cursor was before the change
}

// newBuffer holds the same lines the preview shows: render() drops a trailing
// newline, so the buffer does too and save puts it back.
func newBuffer(text string, mod time.Time) *buffer {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	endNL := strings.HasSuffix(text, "\n") || text == ""
	raw := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	b := &buffer{lines: make([][]rune, len(raw)), modAt: mod, endNL: endNL}
	for i, l := range raw {
		b.lines[i] = []rune(l)
	}

	return b
}

func (b *buffer) text() string {
	out := make([]string, len(b.lines))
	for i, l := range b.lines {
		out[i] = string(l)
	}

	return strings.Join(out, "\n")
}

func (b *buffer) line(i int) []rune {
	if i < 0 || i >= len(b.lines) {
		return nil
	}

	return b.lines[i]
}

// clamp keeps a position inside the text.
func (b *buffer) clamp(p pos) pos {
	p.line = max(min(p.line, len(b.lines)-1), 0)
	p.col = max(min(p.col, len(b.line(p.line))), 0)

	return p
}

// textIn is the text between two positions.
func (b *buffer) textIn(a, z pos) string {
	a, z = b.clamp(a), b.clamp(z)
	if !posLE(a, z) {
		a, z = z, a
	}

	if a.line == z.line {
		return string(b.line(a.line)[a.col:z.col])
	}

	var sb strings.Builder
	sb.WriteString(string(b.line(a.line)[a.col:]))

	for i := a.line + 1; i < z.line; i++ {
		sb.WriteString("\n" + string(b.line(i)))
	}

	sb.WriteString("\n" + string(b.line(z.line)[:z.col]))

	return sb.String()
}

// replace puts text over the range [a, z) and returns where it ends.
func (b *buffer) replace(a, z pos, text string) pos {
	a, z = b.clamp(a), b.clamp(z)
	if !posLE(a, z) {
		a, z = z, a
	}

	head, tail := b.line(a.line)[:a.col], b.line(z.line)[z.col:]
	ins := strings.Split(text, "\n")

	mid := make([][]rune, len(ins))
	for i, s := range ins {
		mid[i] = []rune(s)
	}

	end := pos{a.line + len(mid) - 1, len(mid[len(mid)-1])}
	if len(mid) == 1 {
		end.col += a.col
	}

	mid[0] = append(append([]rune{}, head...), mid[0]...)
	mid[len(mid)-1] = append(mid[len(mid)-1], tail...)
	rest := b.lines[z.line+1:] // still the old array: the splice below reallocates
	b.lines = append(b.lines[:a.line:a.line], mid...)
	b.lines = append(b.lines, rest...)

	return end
}

// apply replaces [a, z) with text, remembering how to undo it. It returns
// where the new text ends.
func (b *buffer) apply(a, z pos, text string, cur pos) pos {
	prev := b.textIn(a, z)
	end := b.replace(a, z, text)
	e := edit{a: b.clamp(a), z: end, text: prev, cur: cur}
	// Typing one character after the last one extends that undo step.
	insert := prev == "" && !strings.Contains(text, "\n")
	if last := len(b.undos) - 1; b.typing && insert && last >= 0 &&
		b.undos[last].z == b.clamp(a) && b.undos[last].text == "" {
		b.undos[last].z = end
	} else {
		b.undos = append(b.undos, e)
	}

	b.typing = insert
	b.redos, b.dirty = nil, true

	return end
}

// undo reverses the last change and returns where the cursor belongs; ok is
// false when there is nothing left.
func (b *buffer) undo() (pos, bool) { return b.step(&b.undos, &b.redos) }
func (b *buffer) redo() (pos, bool) { return b.step(&b.redos, &b.undos) }

func (b *buffer) step(from, to *[]edit) (pos, bool) {
	if len(*from) == 0 {
		return pos{}, false
	}

	e := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	prev := b.textIn(e.a, e.z)
	end := b.replace(e.a, e.z, e.text)
	*to = append(*to, edit{a: e.a, z: end, text: prev, cur: e.cur})
	b.dirty, b.typing = true, false

	if e.text == "" { // the change was an insert: come back to where it started
		return e.a, true
	}

	return end, true
}

// save writes the buffer, keeping the file's mode; it refuses when the file
// changed on disk since it was read unless force is set.
// ponytail: no atomic temp+rename, a torn write needs git to recover.
func (b *buffer) save(path string, force bool) error {
	st, err := os.Stat(path)
	switch {
	case err == nil && !force && !st.ModTime().Equal(b.modAt):
		return errChangedOnDisk
	case err != nil && !os.IsNotExist(err):
		return err
	}

	mode := os.FileMode(0o644)
	if err == nil {
		mode = st.Mode().Perm()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	text := b.text()
	if b.endNL { // the file had a final newline; keep it
		text += "\n"
	}

	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		return err
	}

	b.dirty, b.typing = false, false
	if st, err := os.Stat(path); err == nil {
		b.modAt = st.ModTime()
	}

	return nil
}

// The preview shows a tab as four spaces, the buffer keeps the tab: docCol
// and displayCol translate, as they do for a language server.

// rawPos is a cursor of the preview in buffer coordinates.
func (p *preview) rawPos(c pos) pos {
	if p.buf == nil {
		return c
	}

	return pos{c.line, docCol(string(p.buf.line(c.line)), c.col)}
}

// dispPos is rawPos backwards.
func (p *preview) dispPos(c pos) pos {
	if p.buf == nil {
		return c
	}

	return pos{c.line, displayCol(string(p.buf.line(c.line)), c.col)}
}

// unsaved is the editor's text when it differs from the file, "" otherwise.
func (p *preview) unsaved() string {
	if !p.dirty() {
		return ""
	}

	return p.unsavedOr(p.buf.text())
}

// refresh re-renders what an edit touched: buffer lines [from, newTo) take
// the place of the view's [from, oldTo).
// ponytail: only the changed lines go through chroma — colouring the whole
// file costs 260 ms at 5 000 lines, far too slow per keystroke — so an edit
// inside a block comment or a raw string can leave the rest of it coloured
// wrong until the file is saved and read again.
func (p *preview) refresh(m *Model, from, oldTo, newTo int) {
	// ponytail: chroma costs about 1.5 ms a line when it is set up per line and
	// about 50 ms for a thousand lines in one pass, so a change of more than a
	// screenful takes the whole-file pass instead. 100 is where the two meet.
	if newTo-from > 100 {
		p.refreshAll(m)
		return
	}

	styled := make([]string, 0, max(newTo-from, 0))

	plain := make([][]rune, 0, max(newTo-from, 0))
	for i := from; i < newTo; i++ {
		st, pl, _, _ := render(pvFile, p.path, string(p.buf.line(i)), m.dark)
		if len(st) == 0 || len(pl) == 0 {
			st, pl = []string{""}, []string{""}
		}

		styled = append(styled, st[0])
		plain = append(plain, []rune(pl[0]))
	}

	oldTo = max(min(oldTo, len(p.lines)), from)
	p.lines = append(p.lines[:from:from], append(styled, p.lines[oldTo:]...)...)
	p.plain = append(p.plain[:from:from], append(plain, p.plain[oldTo:]...)...)
	p.raw, p.numW, p.vis = "", len(strconv.Itoa(len(p.plain))), nil
	p.src, p.srcPlain, p.mdLines, p.mdShown = p.lines, p.plain, nil, 0
	p.rehit()
}

// refreshAll re-renders the whole file; undo and save take this path, where
// one pass over chroma is worth the correct colours.
func (p *preview) refreshAll(m *Model) {
	p.raw = p.unsavedOr(p.buf.text())
	styled, plain, _, numW := render(pvFile, p.path, p.raw, m.dark)
	p.lines, p.numW, p.vis = styled, numW, nil

	p.plain = make([][]rune, len(plain))
	for i, l := range plain {
		p.plain[i] = []rune(l)
	}

	p.src, p.srcPlain, p.mdLines, p.mdShown = p.lines, p.plain, nil, 0
	p.rehit()
}

// text is what the editor holds, the file's own content when it has no buffer.
func (p *preview) text() string {
	if p.buf != nil {
		return p.unsavedOr(p.buf.text())
	}

	return p.raw
}

func (p *preview) unsavedOr(text string) string {
	if p.buf != nil && p.buf.endNL {
		return text + "\n"
	}

	return text
}

// edit replaces the display range [a, z) with text and puts the cursor after
// it. It is the one path every change takes.
func (p *preview) edit(m *Model, a, z pos, text string) tea.Cmd {
	return p.editRaw(m, p.rawPos(a), p.rawPos(z), text)
}

// editRaw is edit in buffer coordinates, where a tab is one rune.
func (p *preview) editRaw(m *Model, a, z pos, text string) tea.Cmd {
	if !p.editable() {
		return nil
	}

	p.typed = true // VS Code cancels its word highlighter on every change

	a, z = p.buf.clamp(a), p.buf.clamp(z)
	if !posLE(a, z) {
		a, z = z, a
	}

	end := p.buf.apply(a, z, text, p.rawPos(p.at()))
	p.refresh(m, a.line, z.line+1, end.line+1)
	p.anchor = nil
	p.setCursor(m, p.dispPos(end))

	return nil
}

// setCursor puts the cursor inside the new text and scrolls to it.
func (p *preview) setCursor(m *Model, c pos) {
	p.cur = pos{max(min(c.line, p.lastLine()), 0), max(c.col, 0)}
	p.cur.col = min(p.cur.col, p.lineLen(p.cur.line))
	p.follow(m.pvW(), m.pvH())
}

// cutRange is the selection, or the cursor's line when there is none.
func (p *preview) cutRange() (a, z pos) {
	if a, z, ok := p.selection(); ok {
		return a, z
	}

	c := p.at()
	switch {
	case c.line < p.lastLine():
		return pos{c.line, 0}, pos{c.line + 1, 0}
	case c.line > 0: // the last line takes the newline before it with it
		return pos{c.line - 1, p.lineLen(c.line - 1)}, pos{c.line, p.lineLen(c.line)}
	}

	return pos{c.line, 0}, pos{c.line, p.lineLen(c.line)}
}

// indentOf is the leading whitespace of a buffer line, for auto-indent.
func indentOf(line []rune) string {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return string(line[:i])
		}
	}

	return string(line)
}

// commentOf is the line comment of a file, by extension.
func commentOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".sh", ".bash", ".zsh", ".fish", ".rb", ".pl", ".yaml", ".yml", ".toml", ".conf", ".tf", ".mk":
		return "#"
	case ".sql", ".lua", ".hs":
		return "--"
	case ".lisp", ".el", ".clj":
		return ";"
	case ".vim":
		return `"`
	}

	return "//"
}

// editKey handles the keys that change the text; ok is false for the rest.
func (p *preview) editKey(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !p.editable() {
		return nil, false
	}

	s := k.String()
	a, z, hasSel := p.selection()
	c := p.at()

	switch s {
	case "enter":
		if !hasSel {
			a, z = c, c
		}

		return p.edit(m, a, z, "\n"+indentOf(p.buf.line(a.line))), true

	case "tab":
		if !hasSel {
			a, z = c, c
		}

		return p.edit(m, a, z, "\t"), true

	case "backspace":
		if hasSel {
			return p.edit(m, a, z, ""), true
		}

		r := p.rawPos(c)
		switch {
		case r.col > 0:
			return p.editRaw(m, pos{r.line, r.col - 1}, r, ""), true
		case r.line > 0:
			return p.editRaw(m, pos{r.line - 1, len(p.buf.line(r.line - 1))}, r, ""), true
		}

		return nil, true

	case "delete":
		if hasSel {
			return p.edit(m, a, z, ""), true
		}

		r := p.rawPos(c)
		switch {
		case r.col < len(p.buf.line(r.line)):
			return p.editRaw(m, r, pos{r.line, r.col + 1}, ""), true
		case r.line < len(p.buf.lines)-1:
			return p.editRaw(m, r, pos{r.line + 1, 0}, ""), true
		}

		return nil, true

	case "ctrl+z":
		return p.history(m, true), true
	case "ctrl+y", "ctrl+shift+z":
		return p.history(m, false), true
	case "ctrl+s":
		return p.save(m, false), true
	case "ctrl+d", "alt+shift+down": // VS Code's Copy Line Down; ⌃d is pando's old name for it
		return p.copyLines(m, 1), true
	case "alt+shift+up":
		return p.copyLines(m, -1), true
	case "ctrl+shift+k":
		a, z := p.cutRange()
		return p.edit(m, a, z, ""), true

	case "alt+up", "alt+down":
		return p.moveLine(m, map[bool]int{true: -1, false: 1}[s == "alt+up"]), true
	case "ctrl+/", "ctrl+_": // some terminals send ⌃_ for ⌃/
		return p.toggleComment(m), true
	case "ctrl+x":
		a, z := p.cutRange()
		text := p.buf.textIn(p.rawPos(a), p.rawPos(z))

		return tea.Batch(setClipboard(text, ""), p.edit(m, a, z, "")), true
	}
	// Anything printable types, as it does in an editor.
	if k.Mod&^tea.ModShift == 0 && k.Text != "" {
		if !hasSel {
			a, z = c, c
		}

		return p.edit(m, a, z, k.Text), true
	}

	return nil, false
}

// history undoes or redoes one step and follows the cursor.
func (p *preview) history(m *Model, back bool) tea.Cmd {
	p.typed = true

	at, ok := map[bool]func() (pos, bool){true: p.buf.undo, false: p.buf.redo}[back]()
	if !ok {
		return flash(map[bool]string{true: "nothing to undo", false: "nothing to redo"}[back], false)
	}

	p.refreshAll(m)
	p.anchor = nil
	p.setCursor(m, p.dispPos(at))

	return nil
}

// moveLine swaps the cursor's line with the one above or below it.
func (p *preview) moveLine(m *Model, d int) tea.Cmd {
	c := p.at()

	to := c.line + d
	if to < 0 || to > p.lastLine() {
		return nil
	}

	here, there := string(p.buf.line(c.line)), string(p.buf.line(to))
	a, z := pos{min(c.line, to), 0}, pos{max(c.line, to), p.lineLen(max(c.line, to))}

	text := there + "\n" + here
	if d < 0 {
		text = here + "\n" + there
	}

	cmd := p.edit(m, a, z, text)
	p.setCursor(m, pos{to, c.col})

	return cmd
}

// copyLines duplicates the cursor's line, or every line the selection touches,
// as a block next to itself: VS Code's copyLinesUp/Down. The text is the same
// either way; down moves the selection onto the copy, up leaves it where it was.
func (p *preview) copyLines(m *Model, d int) tea.Cmd {
	a, z, ok := p.selection()
	if !ok {
		a, z = p.at(), p.at()
	} else if z.col == 0 && z.line > a.line { // as changedLines: a selection ending at column 0 leaves that line out
		z.line--
	}

	out := make([]string, 0, z.line-a.line+1)
	for i := a.line; i <= z.line; i++ {
		out = append(out, string(p.buf.line(i)))
	}

	anchor, cur := p.anchor, p.cur // edit clears the anchor
	end := pos{z.line, p.lineLen(z.line)}
	cmd := p.edit(m, end, end, "\n"+strings.Join(out, "\n"))

	if d > 0 {
		n := z.line - a.line + 1

		cur.line += n
		if anchor != nil {
			anchor = &pos{anchor.line + n, anchor.col}
		}
	}

	p.anchor = anchor
	p.setCursor(m, cur)

	return cmd
}

// toggleComment comments or uncomments the selected lines, VS Code's ⌃/.
func (p *preview) toggleComment(m *Model) tea.Cmd {
	a, z, ok := p.selection()
	if !ok {
		a, z = p.at(), p.at()
	}

	mark := commentOf(p.path)
	on := false

	for i := a.line; i <= z.line; i++ {
		l := strings.TrimSpace(string(p.buf.line(i)))
		if l != "" && !strings.HasPrefix(l, mark) {
			on = true
			break
		}
	}

	var out []string

	for i := a.line; i <= z.line; i++ {
		l := string(p.buf.line(i))
		switch {
		case on && strings.TrimSpace(l) == "":
			// an empty line keeps its emptiness
		case on:
			indent := indentOf(p.buf.line(i))
			l = indent + mark + " " + strings.TrimPrefix(l, indent)

		default:
			trimmed := strings.TrimSpace(l)
			l = strings.Replace(l, trimmed, strings.TrimPrefix(strings.TrimPrefix(trimmed, mark), " "), 1)
		}

		out = append(out, l)
	}

	last := pos{z.line, p.lineLen(z.line)}
	cmd := p.edit(m, pos{a.line, 0}, last, strings.Join(out, "\n"))
	p.setCursor(m, pos{z.line, p.lineLen(z.line)})

	return cmd
}

// save writes the file; a foreign write in the meantime asks first, and a
// buffer with no file yet asks where it goes.
func (p *preview) save(m *Model, force bool) tea.Cmd {
	if p.buf == nil {
		return nil
	}

	if p.untitled() {
		return m.saveAsPrompt(m.edIdx, nil)
	}

	if !p.buf.dirty && !force {
		return flash("no changes to save", false)
	}

	note := ""

	if m.st.Settings.FmtSave && len(m.formatCommand(p.path)) > 0 {
		if err := p.format(m); err != nil { // the file is saved unformatted, as VS Code saves it
			note = " — " + err.Error()
		}
	}

	switch err := p.buf.save(p.path, force); {
	case errors.Is(err, errChangedOnDisk):
		path := p.path
		m.modal = newMenu(filepath.Base(path)+" changed on disk", -1, 0,
			item{label: "Overwrite it", run: func(m *Model) tea.Cmd { return m.pv.save(m, true) }},
			item{label: "Reload and lose my edits", run: func(m *Model) tea.Cmd {
				m.pv.buf = nil
				return m.pv.load(m)
			}},
			cancelItem())

		return nil

	case err != nil:
		return flash(err.Error(), true)
	}

	return tea.Batch(m.refreshGit(), flash("saved "+filepath.Base(p.path)+note, note != ""))
}
