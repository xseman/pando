package ui

import (
	"cmp"
	"image/color"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

// completion is the suggest widget: VS Code's list under the cursor while you
// type. The words of the file fill it at once; a language server's answer
// replaces them when it comes, as VS Code's word-based suggestions give way to
// a provider's.
type completion struct {
	on      bool
	line    int        // buffer line of the word being typed
	from    int        // buffer column it starts at
	trigger string     // "." when a member list was asked for
	server  []compItem // the server's answer for this word; nil until it comes
	items   []compItem // what matches, in the order shown
	sel     int
	top     int
	seq     int // the newest request; answers to older ones are dropped
}

type compItem struct {
	label, detail, text, filter string
	col                         int // buffer column the text replaces from; -1 = the word typed so far
}

// compRows is how many suggestions show at once.
const compRows = 8

type compTickMsg struct{ seq int }

type compMsg struct {
	seq   int
	items []compItem
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// typedWord is the part of the identifier left of the cursor, in buffer
// coordinates.
func (p *preview) typedWord() (line, from int, word string) {
	r := p.rawPos(p.at())
	bl := p.buf.line(r.line)
	from, _ = wordRange(bl, r.col)

	return r.line, from, string(bl[from:min(r.col, len(bl))])
}

// autoSuggest reports whether typing opens the list by itself: in code, not in
// prose, where VS Code keeps it quiet too. ⌃space opens it anywhere.
func (m *Model) autoSuggest(path string) bool {
	_, argv := m.lspCommand(path)
	return len(argv) > 0 || lsp.LanguageOf(path) != ""
}

func (p *preview) closeComp() { p.comp = completion{seq: p.comp.seq} }

// suggest opens or refreshes the list for the word at the cursor. trigger is
// the character that asked for members; force opens it with nothing typed.
func (p *preview) suggest(m *Model, trigger string, force bool) tea.Cmd {
	if !p.editable() {
		return nil
	}

	line, from, word := p.typedWord()
	if word == "" && trigger == "" && !force {
		p.closeComp()
		return nil
	}

	if !p.comp.on || p.comp.line != line || p.comp.from != from {
		p.comp = completion{seq: p.comp.seq, trigger: trigger} // a new word: the old answer is not for it
	}

	p.comp.on, p.comp.line, p.comp.from = true, line, from
	if trigger != "" {
		p.comp.trigger = trigger
	}

	p.refilter(word)

	if _, argv := m.lspCommand(p.path); len(argv) == 0 {
		if len(p.comp.items) == 0 {
			p.closeComp()
		}

		return nil
	}
	// ponytail: every keystroke asks again after a short pause, with the whole
	// file sent along; keep the server's list and filter it locally while
	// isIncomplete is false if that ever shows up as lag on big files.
	p.comp.seq++
	seq := p.comp.seq

	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return compTickMsg{seq} })
}

// onCompTick sends the request once typing paused.
func (m *Model) onCompTick(msg compTickMsg) tea.Cmd {
	p := &m.pv
	if msg.seq != p.comp.seq || !p.comp.on || !p.editable() {
		return nil
	}

	lang, argv := m.lspCommand(p.path)
	r := p.rawPos(p.at())
	lineText := string(p.buf.line(r.line))
	path, root, pool, text, trigger := p.path, m.ws, &m.lsps, p.text(), p.comp.trigger
	col := lsp.UTF16Col(lineText, r.col)

	return func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return compMsg{seq: msg.seq}
		}

		c.Overlay(path, text)
		got, err := c.Completion(path, r.line, col, trigger)
		c.Overlay(path, "")

		if err != nil {
			return compMsg{seq: msg.seq}
		}

		items := make([]compItem, 0, len(got))
		for _, it := range got {
			ci := compItem{label: it.Label, detail: it.Detail, text: it.Text, filter: it.Filter, col: -1}
			if it.Col >= 0 {
				ci.col = lsp.RuneCol(lineText, it.Col)
			}

			items = append(items, ci)
		}

		return compMsg{seq: msg.seq, items: items}
	}
}

// onComp takes the server's answer, unless the user typed on since it was asked.
func (p *preview) onComp(msg compMsg) {
	if msg.seq != p.comp.seq || !p.comp.on {
		return
	}

	p.comp.server = msg.items
	if p.comp.server == nil {
		p.comp.server = []compItem{}
	}

	_, _, word := p.typedWord()
	p.refilter(word)
}

// refilter fills the list for word: the server's items when it has any, the
// words of the file otherwise.
func (p *preview) refilter(word string) {
	src := p.comp.server
	if len(src) == 0 && p.comp.trigger == "" {
		src = p.fileWords(word)
	}

	low := strings.ToLower(word)

	items := make([]compItem, 0, min(len(src), 100))
	for _, it := range src {
		if strings.HasPrefix(strings.ToLower(it.filter), low) {
			items = append(items, it)
		}

		if len(items) == 100 {
			break
		}
	}

	p.comp.items, p.comp.sel, p.comp.top = items, 0, 0
}

// fileWords are the identifiers of the file starting with word, nearest to the
// cursor first.
// ponytail: the whole buffer is scanned per keystroke, ~1 ms at 5 000 lines;
// cache the word set per buffer change if files get bigger than that.
func (p *preview) fileWords(word string) []compItem {
	low := strings.ToLower(word)
	dist := map[string]int{}

	cur := p.comp.line
	for i, bl := range p.buf.lines {
		for a := 0; a < len(bl); {
			if !isWordRune(bl[a]) {
				a++
				continue
			}

			b := a
			for b < len(bl) && isWordRune(bl[b]) {
				b++
			}

			w := string(bl[a:b])
			if i == cur && a == p.comp.from { // the word being typed is not a suggestion
				a = b
				continue
			}

			if b-a >= 2 && !unicode.IsDigit(bl[a]) && w != word && strings.HasPrefix(strings.ToLower(w), low) {
				d := max(i-cur, cur-i)
				if old, ok := dist[w]; !ok || d < old {
					dist[w] = d
				}
			}

			a = b
		}
	}

	out := make([]compItem, 0, len(dist))
	for w := range dist {
		out = append(out, compItem{label: w, text: w, filter: w, col: -1})
	}

	slices.SortFunc(out, func(a, b compItem) int {
		return cmp.Or(dist[a.label]-dist[b.label], strings.Compare(a.label, b.label))
	})

	return out
}

// accept puts the selected suggestion in place of the word typed so far.
func (p *preview) accept(m *Model) tea.Cmd {
	it := p.comp.items[p.comp.sel]
	r := p.rawPos(p.at())

	from := p.comp.from
	if it.col >= 0 && it.col <= r.col {
		from = it.col
	}

	p.closeComp()

	return p.editRaw(m, pos{r.line, from}, r, it.text)
}

// compKey takes the keys the open list owns; ok is false for the rest, which
// go on to the editor.
func (p *preview) compKey(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !p.comp.on {
		return nil, false
	}

	n := len(p.comp.items)

	switch s := k.String(); s {
	case "up", "down", "pgup", "pgdown":
		if n == 0 {
			break
		}

		step := map[string]int{"up": -1, "down": 1, "pgup": -compRows, "pgdown": compRows}[s]
		switch {
		case s == "up" && p.comp.sel == 0:
			p.comp.sel = n - 1 // the arrows wrap, as VS Code's do
		case s == "down" && p.comp.sel == n-1:
			p.comp.sel = 0
		default:
			p.comp.sel = max(0, min(p.comp.sel+step, n-1))
		}

		p.comp.top = max(min(p.comp.top, p.comp.sel), p.comp.sel-compRows+1)

		return nil, true

	case "enter", "tab":
		if n == 0 {
			break
		}

		return p.accept(m), true

	case "esc":
		p.closeComp()
		return nil, true

	case "backspace", "delete":
		return nil, false // the edit refilters
	}

	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		return nil, false // typing refilters after the edit
	}

	p.closeComp()

	return nil, false
}

// afterEdit keeps the list in step with what an edit key did.
func (p *preview) afterEdit(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rs := []rune(k.Text)
	switch {
	case k.Mod&^tea.ModShift != 0:
		p.closeComp()

	// ponytail: "." is the one trigger character; ask the server for its own
	// (triggerCharacters) when a language needs "::" or "->".
	case k.Text == ".":
		p.closeComp()

		if _, argv := m.lspCommand(p.path); len(argv) > 0 {
			return p.suggest(m, ".", true)
		}

	case len(rs) == 1 && isWordRune(rs[0]):
		if p.comp.on || m.autoSuggest(p.path) {
			return p.suggest(m, p.comp.trigger, false)
		}

	case (k.String() == "backspace" || k.String() == "delete") && p.comp.on:
		if _, _, word := p.typedWord(); word == "" && p.comp.trigger != "" {
			return p.suggest(m, p.comp.trigger, true) // back to the member list
		}

		return p.suggest(m, "", false)

	default:
		p.closeComp()
	}

	return nil
}

// compBox draws the list under the cursor's line, or above it when it would
// run off the editor.
func (p *preview) compBox(m *Model) (box []string, x, y int, ok bool) {
	if !p.comp.on || len(p.comp.items) == 0 || m.focus != onMain || !m.showsPreview() {
		return nil, 0, 0, false
	}

	if _, _, vis := p.cursor(m.mainW(), m.pvH()); !vis {
		return nil, 0, 0, false
	}

	_, _, word := p.typedWord()
	items := p.comp.items

	lw, dw := 0, 0
	for _, it := range items {
		lw, dw = max(lw, ansi.StringWidth(it.label)), max(dw, ansi.StringWidth(it.detail))
	}

	iw := lw + 2
	if dw > 0 {
		iw += min(dw, 30) + 2
	}

	iw = max(min(iw, 60, m.mainW()-2), 20)
	rows := min(len(items), compRows)
	hi := lipgloss.NewStyle().Foreground(pal.headerAccent).Bold(true)
	side := dim.Render("│")
	box = append(box, dim.Render("╭"+strings.Repeat("─", iw)+"╮"))

	for i := p.comp.top; i < p.comp.top+rows && i < len(items); i++ {
		it := items[i]
		st, h := plain, hi

		var bg color.Color

		ds := dim

		if i == p.comp.sel {
			bg = pal.selBg

			st, h, ds = st.Background(bg), h.Background(bg), ds.Background(bg)
			if pal.selFg != nil {
				st = st.Foreground(pal.selFg)
			}
		}

		n := min(len([]rune(word)), len([]rune(it.label)))
		lr := []rune(it.label)
		left := []seg{sg(" ", st), sg(string(lr[:n]), h), sg(string(lr[n:]), st)}
		detail := ansi.Truncate(it.detail, max(iw-lw-4, 0), "…")
		box = append(box, side+row(iw, bg, left, sg(detail+" ", ds))+side)
	}

	box = append(box, dim.Render("╰"+strings.Repeat("─", iw)+"╯"))
	back := cellsOf([]rune(word)) + 2 // the border and the space before the label
	x, y = m.lightbulb(back)
	// The body's rows run from 1+stripH() for pvH() rows; lightbulb's y is the
	// row under the cursor.
	if y+len(box) > 1+m.stripH()+m.pvH() {
		y = max(y-1-len(box), 0) // above the line instead
	}

	return box, x, y, true
}
