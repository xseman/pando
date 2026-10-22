package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

// hover is VS Code's hover: what the language server says of the word under
// the pointer once it rests there (tipDelay), or of the one at the cursor
// on ⌃k (VS Code's ⌃k ⌃i, a chord pando has no room for).
type hover struct {
	seq      int    // the newest request; answers to older ones are dropped
	line     int    // the display line asked about
	col      int    // the display column asked about
	from, to int    // the word's display columns: the pointer keeps the hover while on it
	mouse    bool   // the pointer asked, and the box sits by it; else under the cursor
	text     string // the server's Markdown; "" while none
	lines    []string
	linesW   int // the width lines were rendered at
}

type hoverTickMsg struct{ seq int }

type hoverMsg struct {
	seq  int
	text string
	err  error
}

// hoverable reports an editor a language server can hover in: a file
// shown as text.
func (p *preview) hoverable(m *Model) bool {
	if p.kind != pvFile || !p.ready || p.buf == nil || p.untitled() || p.md == 1 || len(p.plain) == 0 {
		return false
	}

	_, argv := m.lspCommand(p.path)

	return len(argv) > 0
}

// hoverMove follows the pointer over the text: on to another word it puts
// the hover away and asks again once the pointer rests (tips on); over the
// same word it keeps it.
func (p *preview) hoverMove(m *Model, np pos, over bool) tea.Cmd {
	h := &p.hov
	if h.mouse && over && np.line == h.line && np.col >= h.from && np.col < h.to {
		return nil
	}

	if h.mouse || over {
		*h = hover{seq: h.seq + 1}
	}

	if !over || !m.st.Settings.Tips || !p.hoverable(m) || np.line >= len(p.plain) {
		return nil
	}

	a, b := wordRange(p.plain[np.line], np.col)
	if np.col >= b {
		return nil
	}

	h.line, h.col, h.from, h.to, h.mouse = np.line, np.col, a, b, true
	seq := h.seq

	return tea.Tick(tipDelay, func(time.Time) tea.Msg { return hoverTickMsg{seq} })
}

// showHover is ⌃k: the hover for the word at the cursor, under it.
func (m *Model) showHover() tea.Cmd {
	p := &m.pv
	if !p.hoverable(m) {
		if p.kind == pvFile && p.path != "" {
			return flash(noServer(p.path), true)
		}

		return flash("open a file first", true)
	}

	at := p.at()
	a, b := wordRange(p.plain[at.line], at.col)
	p.hov = hover{seq: p.hov.seq + 1, line: at.line, col: at.col, from: a, to: b}

	_, argv := m.lspCommand(p.path)
	m.flash("asking "+argv[0]+"…", false)

	return m.askHover(p.hov.seq)
}

// askHover sends the request for hover seq, if it is still the newest. The
// pointer's never starts a server again that died or would not start.
func (m *Model) askHover(seq int) tea.Cmd {
	p := &m.pv
	if seq != p.hov.seq || !p.hoverable(m) || p.hov.line >= len(p.plain) {
		return nil
	}

	lang, argv := m.lspCommand(p.path)
	r := p.rawPos(pos{p.hov.line, p.hov.col})
	col := lsp.UTF16Col(string(p.buf.line(r.line)), r.col)
	path, root, pool, text := p.path, m.ws, &m.lsps, p.text()

	get := pool.get
	if p.hov.mouse {
		get = pool.background
	}

	return func() tea.Msg {
		c, err := get(root, path, lang, argv)
		if err != nil {
			return hoverMsg{seq: seq, err: err}
		}

		_ = c.Sync(path, text) // a server gone answers the request with why

		s, err := c.Hover(path, r.line, col)

		return hoverMsg{seq: seq, text: s, err: err}
	}
}

func (m *Model) onHover(msg hoverMsg) tea.Cmd {
	h := &m.pv.hov
	if msg.seq != h.seq {
		return nil
	}

	if h.mouse { // the pointer's goes without a word when there is nothing to say
		h.text = ifElse(msg.err == nil, msg.text, "")
		return nil
	}

	m.answered()

	switch {
	case msg.err != nil:
		return flash(msg.err.Error(), true)
	case msg.text == "":
		return flash("no hover information", false)
	}

	h.text = msg.text

	return nil
}

// hoverShown reports a hover on screen: the pointer's while it rests, not
// since a key or a click, with tips on.
func (p *preview) hoverShown(m *Model) bool {
	h := &p.hov
	if h.text == "" || m.focus != onMain && !h.mouse || !m.showsPreview() {
		return false
	}

	return !h.mouse || m.st.Settings.Tips && m.mouseAt.After(m.tipOff)
}

// hoverBox is the hover's Markdown in a box: by the pointer, or under the
// cursor's line (above it when it would run off the editor). The problem
// under the pointer heads it, VS Code's hover over a squiggle.
func (p *preview) hoverBox(m *Model) (box []string, x, y int, ok bool) {
	if !p.hoverShown(m) {
		return nil, 0, 0, false
	}

	h := &p.hov

	natural := 0 // as wide as its longest line wants, a code block's padding and the air included
	for l := range strings.Lines(h.text) {
		natural = max(natural, ansi.StringWidth(strings.TrimRight(l, "\n"))+2*mdMargin+2)
	}

	if w := max(min(80, m.mainW()-4, natural), 20); h.lines == nil || h.linesW != w {
		styled, _ := renderMarkdown(h.text, w, m.dark)
		h.lines, h.linesW = trimBlank(styled), w

		if w >= 3*mdMargin+20 { // the air rendered Markdown keeps left and right: the box has its own
			for i, l := range h.lines {
				h.lines[i] = ansi.Cut(l, mdMargin, w)
			}
		}
	}

	lines := h.lines
	if t := p.probTip; h.mouse && t != nil && m.mouseX == t.x && m.mouseY == t.y {
		lines = append([]string{t.text, dim.Render(strings.Repeat("─", min(h.linesW, ansi.StringWidth(t.text))))}, lines...)
	}

	if most := max(m.pvH()/2, 3); len(lines) > most {
		lines = append(lines[:most-1:most-1], dim.Render("…"))
	}

	box, x, y = m.hoverBox(lines)
	if !h.mouse {
		x, y = m.lightbulb(p.at().col - h.from + 2) // the border and the space before the text
		if y+len(box) > m.mainY()+m.headH()+m.stripH()+m.pvH() {
			y = max(y-1-len(box), 0)
		}

		x = max(min(x, m.w-ansi.StringWidth(box[0])), 0)
	}

	return box, x, y, true
}

// trimBlank drops the blank lines rendering leaves around the text.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}

	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// sigHelp is VS Code's parameter hints: the signature of the call being
// typed, its active parameter in bold, above the cursor. "(" and "," ask for
// it, every edit while it shows asks again, and an answer of none puts it
// away; esc or a key that moves the cursor does too.
type sigHelp struct {
	seq int
	sig *lsp.Signature
}

type sigMsg struct {
	seq int
	sig *lsp.Signature
}

func (p *preview) closeSig() { p.sig = sigHelp{seq: p.sig.seq + 1} }

// sigKey takes esc from the hints, and puts them away for a key that is not
// an edit; it reports taking the key, for that esc alone.
func (p *preview) sigKey(k tea.KeyPressMsg) bool {
	if p.sig.sig == nil {
		return false
	}

	switch s := k.String(); {
	case s == "esc":
		p.closeSig()
		return true

	case k.Text != "" && k.Mod&^tea.ModShift == 0, s == "backspace", s == "delete", s == "enter", s == "tab":
	default:
		p.closeSig()
	}

	return false
}

// sigAfter asks for the signature after an edit that may have changed it.
func (p *preview) sigAfter(m *Model, k tea.KeyPressMsg) tea.Cmd {
	if k.Text != "(" && k.Text != "," && p.sig.sig == nil || !p.hoverable(m) {
		return nil
	}

	lang, argv := m.lspCommand(p.path)
	r := p.rawPos(p.at())
	col := lsp.UTF16Col(string(p.buf.line(r.line)), r.col)
	path, root, pool, text := p.path, m.ws, &m.lsps, p.text()
	p.sig.seq++
	seq := p.sig.seq

	return func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return sigMsg{seq: seq}
		}

		_ = c.Sync(path, text) // a server gone answers the request with why

		sig, _ := c.SignatureHelp(path, r.line, col) // an error is no hint

		return sigMsg{seq: seq, sig: sig}
	}
}

func (p *preview) onSig(msg sigMsg) {
	if msg.seq == p.sig.seq {
		p.sig.sig = msg.sig
	}
}

// sigBox draws the signature above the cursor's line, below it when there
// is no room above: the label, the active parameter bold, then the first
// line of its documentation.
func (p *preview) sigBox(m *Model) (box []string, x, y int, ok bool) {
	s := p.sig.sig
	if s == nil || m.focus != onMain || !m.showsPreview() || !p.editable() {
		return nil, 0, 0, false
	}

	if _, _, vis := p.cursor(m.pvW(), m.pvH()); !vis {
		return nil, 0, 0, false
	}

	w := max(min(100, m.mainW()-4), 10)
	rs := []rune(s.Label)
	hi := lipgloss.NewStyle().Foreground(pal.headerAccent).Bold(true)
	lines := []string{ansi.Truncate(plain.Render(string(rs[:s.From]))+hi.Render(string(rs[s.From:s.To]))+plain.Render(string(rs[s.To:])), w, "…")}

	if doc, _, _ := strings.Cut(strings.TrimSpace(s.Doc), "\n"); doc != "" {
		lines = append(lines, dim.Render(ansi.Truncate(doc, w, "…")))
	}

	box, _, _ = m.hoverBox(lines)
	x, y = m.lightbulb(0)
	x = max(min(x, m.w-ansi.StringWidth(box[0])), 0)

	top := m.mainY() + m.headH() + m.stripH()
	if y-1-len(box) >= top {
		y -= 1 + len(box)
	}

	return box, x, y, true
}
