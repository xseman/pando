package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/lsp"
)

// symbolsMsg is the outline of a file, from its language server.
type symbolsMsg struct {
	path, query string
	syms        []lsp.Symbol
	err         error
}

// gotoSymbol is VS Code's Go to Symbol in Editor: the file's symbols in a
// picker whose query starts with "@", and "@:" groups them by kind. Markdown
// needs no server: its headings are the symbols.
func (m *Model) gotoSymbol(query string) tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || !p.ready {
		return flash("open a file first", true)
	}

	lang, argv := m.lspCommand(p.path)
	if len(argv) == 0 {
		if isMarkdown(p.path) {
			return m.openSymbols(symbolsMsg{path: p.path, query: query, syms: mdSymbols(p.rawLines())})
		}

		return flash(noServer(p.path), true)
	}

	path, root, pool, text := p.path, m.ws, &m.lsps, p.unsaved()
	m.flash("asking "+argv[0]+"…", false)

	return func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return symbolsMsg{path: path, query: query, err: err}
		}

		c.Overlay(path, text)
		syms, err := c.Symbols(path)

		return symbolsMsg{path, query, syms, err}
	}
}

// openSymbols shows the picker. Moving through it moves the editor to the
// symbol, and esc puts the editor back where it was, as VS Code does.
func (m *Model) openSymbols(msg symbolsMsg) tea.Cmd {
	m.answered()

	p := &m.pv
	switch {
	case msg.err != nil:
		return flash(msg.err.Error(), true)
	case p.kind != pvFile || p.path != msg.path: // another file opened meanwhile
		return nil
	case len(msg.syms) == 0:
		return flash("no symbols in "+filepath.Base(msg.path), false)
	}

	query := msg.query
	if md := m.modal; md != nil && md.filter && md.prefix == "" && strings.HasPrefix(md.input.Value(), "@") {
		query = md.input.Value() // typed on in quick open while the server answered
	}

	cur, top, left, anchor := p.cur, p.top, p.left, p.anchor

	items := make([]item, len(msg.syms))
	for i, s := range msg.syms {
		items[i] = item{label: s.Name, hint: s.Container, group: lsp.KindName(s.Kind), run: func(m *Model) tea.Cmd {
			m.pv.showSymbol(m, s)
			return nil
		}}
	}

	md := newPicker("Go to Symbol", nil)
	md.items, md.prefix = items, "@"
	md.input.SetValue(query)
	md.input.CursorEnd()
	md.refilter()
	md.move = func(m *Model) { md.disp[md.l.sel].run(m) }
	md.cancel = func(m *Model) { m.pv.cur, m.pv.top, m.pv.left, m.pv.anchor = cur, top, left, anchor }
	md.change = func(m *Model, v string) tea.Cmd {
		if strings.HasPrefix(v, "@") {
			return nil
		}

		md.dismiss(m)

		if strings.HasPrefix(v, ":") {
			return m.gotoLineQuery(v)
		}
		// ponytail: back to Go to File without what was typed after the @.
		return m.loadIndex(true)
	}

	if query == "@" { // start at the symbol around the cursor
		k := -1

		for i, s := range msg.syms {
			if s.From <= cur.line && cur.line <= s.To {
				k = i
			}
		}

		if k >= 0 {
			_, _, _, _, rows := md.rect(m)
			md.l.sel = k + 1 // under the "symbols (n)" heading
			md.l.snap(rows)
		}
	}

	m.modal = md

	return nil
}

// gotoLineQuery is VS Code's Go to Line: a picker whose query starts with ":".
// Its one row says where ⏎ goes, the editor shows the line as you type, and
// esc puts the cursor back. "@" in place of the ":" goes on to the symbols.
// ponytail: no negative lines counted from the end, as VS Code has.
func (m *Model) gotoLineQuery(query string) tea.Cmd {
	p := &m.pv
	if !m.showsPreview() || !p.ready || len(p.plain) == 0 {
		return flash("open a file first", true)
	}

	cur, top, left, anchor := p.cur, p.top, p.left, p.anchor
	back := func(m *Model) { m.pv.cur, m.pv.top, m.pv.left, m.pv.anchor, m.pv.rangeHi = cur, top, left, anchor, 0 }
	last := p.lastLine() + 1
	md := newPicker("Go to Line", nil)
	row := func(v string) item {
		line, col := parseLineCol(strings.ReplaceAll(strings.TrimPrefix(v, ":"), ",", ":"))
		if line <= 0 {
			return item{always: true, label: fmt.Sprintf("Current line %d, character %d · type a line from 1 to %d",
				cur.line+1, m.pv.charCol(cur)+1, last)}
		}

		line = min(line, last)

		label := fmt.Sprintf("Go to line %d", line)
		if col > 0 {
			label += fmt.Sprintf(", character %d", col)
		}

		return item{always: true, label: label, run: func(m *Model) tea.Cmd {
			m.pv.gotoLine(m, line-1, m.pv.screenCol(line-1, max(col-1, 0)))

			m.pv.rangeHi = line // highlighted while it is only shown, as VS Code does
			if m.modal == nil { // chosen
				m.focus, m.pv.rangeHi = onMain, 0
			}

			return nil
		}}
	}
	md.items = []item{row(query)}
	md.input.SetValue(query)
	md.input.CursorEnd()
	md.refilter()
	md.move = func(m *Model) { md.disp[md.l.sel].run(m) }
	md.cancel = back
	md.change = func(m *Model, v string) tea.Cmd {
		if strings.HasPrefix(v, ":") {
			if md.items = []item{row(v)}; md.items[0].run == nil {
				back(m) // the number was erased
			}

			return nil
		}

		md.dismiss(m)

		if strings.HasPrefix(v, "@") {
			return m.gotoSymbol(v)
		}

		return m.loadIndex(true)
	}
	m.modal = md
	md.moved(m)

	return nil
}

// charCol is the character a display position is on, a tab counting as one.
func (p *preview) charCol(at pos) int {
	if p.kind != pvFile {
		return at.col
	}

	return docCol(p.rawLine(at.line), at.col)
}

// screenCol is where character col of line shows.
func (p *preview) screenCol(line, col int) int {
	if p.kind != pvFile {
		return col
	}

	raw := p.rawLine(line)

	return displayCol(raw, min(col, utf8.RuneCountInString(raw)))
}

// showSymbol puts the cursor on a symbol's name, selected, in the middle of
// the view.
func (p *preview) showSymbol(m *Model, s lsp.Symbol) {
	raw := p.rawLine(s.Line)
	a := displayCol(raw, lsp.RuneCol(raw, s.Col))
	b := displayCol(raw, lsp.RuneCol(raw, s.EndCol))
	p.gotoLine(m, s.Line, b)

	if b > a {
		p.anchor = &pos{p.cur.line, a}
	}
}

// rawLine is a line as the file has it (a tab is one character), the unsaved
// text when there is some.
func (p *preview) rawLine(n int) string {
	if p.buf != nil {
		return string(p.buf.line(n))
	}

	return fileLine(p.path, n)
}

func (p *preview) rawLines() []string {
	if p.buf == nil {
		return fileLines(p.path)
	}

	out := make([]string, len(p.buf.lines))
	for i, l := range p.buf.lines {
		out[i] = string(l)
	}

	return out
}

// mdHeading is an ATX heading; a closing run of #s after a space is not part
// of the text.
var mdHeading = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)

// mdSymbols are a Markdown file's headings, each inside the heading above it
// with a lower level, as VS Code's Markdown outline has them.
// ponytail: ATX headings only, fenced code skipped; no setext (=== / ---).
func mdSymbols(lines []string) []lsp.Symbol {
	var out []lsp.Symbol

	var open []int // out indexes of the headings whose sections are still open

	fence := false

	for i, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			continue
		}

		g := mdHeading.FindStringSubmatchIndex(l)
		if fence || g == nil {
			continue
		}

		level := g[3] - g[2]
		for len(open) > 0 && levelOf(lines[out[open[len(open)-1]].Line]) >= level {
			out[open[len(open)-1]].To = i - 1
			open = open[:len(open)-1]
		}

		s := lsp.Symbol{
			Name: l[g[4]:g[5]], Kind: 15, Line: i, From: i,
			Col:    lsp.UTF16Col(l, utf8.RuneCountInString(l[:g[4]])),
			EndCol: lsp.UTF16Col(l, utf8.RuneCountInString(l[:g[5]])),
		}
		if len(open) > 0 {
			s.Container = out[open[len(open)-1]].Name
		}

		out = append(out, s)
		open = append(open, len(out)-1)
	}

	for _, k := range open {
		out[k].To = len(lines) - 1
	}

	return out
}

func levelOf(heading string) int {
	g := mdHeading.FindStringSubmatchIndex(heading)
	return g[3] - g[2]
}
