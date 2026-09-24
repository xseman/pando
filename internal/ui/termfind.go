package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// termFind is VS Code's terminal find widget over a session or the Terminal
// panel's shell: the query and its toggles, the matches in everything the
// emulator holds (scrollback, then the screen) and the one selected.
//
// Lines are counted from the oldest scrollback line, the way session.read
// returns them, so row y of a screen scrolled back by s is line
// Scrollback-s+y. Output appending to the bottom keeps that count.
// ponytail: the emulator dropping its oldest lines, or an app redrawing
// above its prompt, shifts it until the next search.
type termFind struct {
	filter
	caseSens, word, regex bool
	id                    string   // the session the matches were collected in
	lines                 []string // its plain text, as session.read gives it
	hits                  []termHit
	hit                   int
	seq                   int  // the newest search asked for; older answers are dropped
	busy                  bool // a search is on its way; a screen update waits for it
}

// termHit is one match: a line of the text and its cells [from, to).
type termHit struct{ line, from, to int }

// termTextMsg is session.read's answer to search seq of the terminal t.
type termTextMsg struct {
	t     *term
	id    string
	seq   int
	lines []string
	move  int // 0 keeps the selection in place; -1, +1 step from it; 2 is a new query
	err   error
}

// focusedTerm is the terminal with the keyboard and the session it shows:
// the Terminal panel's shell, or the session in the editor area or its
// column. nil when the keyboard is elsewhere.
func (m *Model) focusedTerm() (*term, string) {
	switch {
	case m.focus == onPanel || m.termFocused():
		return &m.tv.term, m.tv.id
	case m.sessFocused(), m.focus == onMain && m.showsSession():
		return &m.term, m.sess
	}

	return nil, ""
}

// openFind is ⌃f in a terminal: the widget with the keyboard, prefilled with
// a one-line selection, searching at once when there is a query.
func (t *term) openFind(m *Model, id string) tea.Cmd {
	if t.find.input.Placeholder == "" {
		t.find.filter = newFind()
	}

	if s := t.selText(); t.hasSel && s != "" && !strings.Contains(s, "\n") {
		t.find.input.SetValue(s)
	}

	t.find.on, t.find.editing = true, true
	t.find.input.CursorEnd()

	return tea.Batch(t.find.input.Focus(), t.search(m, id, 2))
}

// closeFind hides the widget and its highlights, as VS Code's terminal does,
// and gives the keyboard back to the shell.
func (t *term) closeFind() {
	t.find.on, t.find.editing = false, false
	t.find.input.Blur()
	t.find.hits = nil
}

// search collects the query's matches again. The alternate screen has no
// scrollback, so its screen is searched as it is; otherwise the text comes
// from session.read and the matches land in onTermText.
func (t *term) search(m *Model, id string, move int) tea.Cmd {
	t.find.seq++
	seq := t.find.seq

	if t.find.input.Value() == "" || id == "" {
		t.find.hits, t.find.lines, t.find.id = nil, nil, id
		return nil
	}

	if t.scr.AltScreen {
		lines := make([]string, len(t.scr.Lines))
		for i, l := range t.scr.Lines {
			lines[i] = ansi.Strip(l)
		}

		return m.onTermText(termTextMsg{t: t, id: id, seq: seq, lines: lines, move: move})
	}

	t.find.busy = true

	return func() tea.Msg {
		var text string

		err := proto.Call("session.read", proto.ReadParams{ID: id, Scrollback: true}, &text)

		return termTextMsg{t: t, id: id, seq: seq, lines: strings.Split(text, "\n"), move: move, err: err}
	}
}

// onTermText collects the matches in a search's text and selects one: the
// same match again, the next or the previous one, or for a new query the
// last one at or above where the selection was, VS Code's incremental
// find-previous from the bottom.
func (m *Model) onTermText(msg termTextMsg) tea.Cmd {
	f := &msg.t.find
	if msg.seq != f.seq {
		return nil
	}

	f.busy = false
	if msg.err != nil || !f.on {
		return nil
	}

	prev, had := termHit{line: len(msg.lines)}, false
	if f.hit < len(f.hits) && f.id == msg.id {
		prev, had = f.hits[f.hit], true
	}

	f.id, f.lines, f.hits, f.hit = msg.id, msg.lines, nil, 0

	re := matcher(searchOpts{query: f.input.Value(), caseSens: f.caseSens, word: f.word, regex: f.regex})
	if re == nil {
		return nil
	}

	for i, line := range msg.lines {
		for _, r := range re.FindAllStringIndex(line, -1) {
			if r[1] > r[0] {
				a := ansi.StringWidth(line[:r[0]])
				f.hits = append(f.hits, termHit{i, a, a + ansi.StringWidth(line[r[0]:r[1]])})
			}
		}
	}

	if len(f.hits) == 0 {
		return nil
	}

	less := func(a, b termHit) bool { return a.line < b.line || a.line == b.line && a.from < b.from }
	last := func(ok func(termHit) bool) int { // the last hit ok takes, -1 for none
		i := -1

		for k, h := range f.hits {
			if ok(h) {
				i = k
			}
		}

		return i
	}

	f.hit = len(f.hits) - 1 // nothing selected before: the newest match

	if had {
		switch msg.move {
		case 1: // the first after prev, wrapping to the oldest
			if i := last(func(h termHit) bool { return !less(prev, h) }); i+1 < len(f.hits) {
				f.hit = i + 1
			} else {
				f.hit = 0
			}

		case -1: // the last before prev, wrapping to the newest
			if i := last(func(h termHit) bool { return less(h, prev) }); i >= 0 {
				f.hit = i
			}

		default: // prev itself, or the last before it
			if i := last(func(h termHit) bool { return !less(prev, h) }); i >= 0 {
				f.hit = i
			}
		}
	}

	if msg.move == 0 { // the output moved on: stay, without scrolling to it
		return nil
	}

	return msg.t.reveal(m)
}

// findGo steps to the next (d > 0) or the previous match, searching the text
// as it is now so a match printed since is not missed.
func (t *term) findGo(m *Model, id string, d int) tea.Cmd {
	if t.find.input.Value() == "" {
		return nil
	}

	return t.search(m, id, max(min(d, 1), -1))
}

// reveal scrolls the selected match into view, to the middle of the screen
// when it is outside it, as xterm's search addon does.
func (t *term) reveal(m *Model) tea.Cmd {
	f := &t.find
	if t.scr.AltScreen || f.hit >= len(f.hits) {
		return nil
	}

	rows, sb := len(t.scr.Lines), t.scr.Scrollback
	line, top := f.hits[f.hit].line, sb-t.scroll

	if line >= top && line < top+rows {
		return nil
	}

	t.scroll = max(min(sb-(line-rows/2), sb), 0)

	return m.fetchScreen()
}

// findSpans are the matches on screen row y, the selected one stronger.
func (t *term) findSpans(y int) []cellSpan {
	f := &t.find
	if !f.on || f.id != t.id || len(f.hits) == 0 {
		return nil
	}

	line := t.lineAt(y)

	var spans []cellSpan

	for i, h := range f.hits {
		if h.line == line {
			bg := pal.findMatchBg
			if i == f.hit {
				bg = pal.findHitBg
			}

			spans = append(spans, cellSpan{h.from, h.to, bg})
		}
	}

	return spans
}

// paintFind lays the matches on screen row y of line.
func (t *term) paintFind(y int, line string) string {
	spans := t.findSpans(y)
	if spans == nil {
		return line
	}

	return paintSpans(line, spans, 0, max(ansi.StringWidth(line), spans[len(spans)-1].to))
}

func (t *term) findStatus() string {
	switch {
	case t.find.input.Value() == "":
		return ""
	case len(t.find.hits) == 0:
		return "No results"
	}

	return fmt.Sprintf("%d of %d", t.find.hit+1, len(t.find.hits))
}

// findRect is the widget in a terminal body w cells wide (its scrollbar
// included) and h rows high: first column and width, floating at the top
// right as the editor's does.
func (t *term) findRect(w, h int) (x0, bw int, ok bool) {
	tw := w - 1 // the scrollbar
	bw = min(findW, tw-2)

	return tw - bw - 1, bw, t.find.on && bw >= 36 && h >= 3
}

// findActs are the widget's buttons, as the editor's: the three toggles,
// then ↑ previous, ↓ next and close.
func (t *term) findActs(id string, bw int) (field int, acts []rowAction) {
	toggle := func(key string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return t.toggleFind(m, id, key) }
	}

	acts = []rowAction{
		{g: icCase, run: toggle("alt+c")},
		{g: icWord, run: toggle("alt+w")},
		{g: icRegex, run: toggle("alt+r")},
		{g: icUp, run: func(m *Model) tea.Cmd { return t.findGo(m, id, -1) }},
		{g: icDown, run: func(m *Model) tea.Cmd { return t.findGo(m, id, 1) }},
		{g: icClose, run: func(*Model) tea.Cmd { t.closeFind(); return nil }},
	}

	return findLayout(acts, bw), acts
}

// findBox draws the widget for a body w wide and h high; x0 is its column
// in the body.
func (t *term) findBox(m *Model, id string, w, h int) (box []string, x0 int, ok bool) {
	x0, bw, ok := t.findRect(w, h)
	if !ok {
		return nil, 0, false
	}

	field, acts := t.findActs(id, bw)
	edge := findEdge(t.find.editing)
	rule := strings.Repeat("─", bw-2)

	return []string{edge.Render("╭" + rule + "╮"), findRow(m, field, acts, findLook{
		input: &t.find.input, editing: t.find.editing, lead: "  ",
		on: [3]bool{t.find.caseSens, t.find.word, t.find.regex}, status: t.findStatus(),
		miss: len(t.find.hits) == 0 && t.find.input.Value() != "",
	}), edge.Render("╰" + rule + "╯")}, x0, true
}

// termFindBoxes are the widgets of the terminals on screen, placed on the
// screen: the session's, in the editor area or its column, and the Terminal
// panel's, under the editor or in its column.
func (m *Model) termFindBoxes() (boxes [][]string, xs, ys []int) {
	add := func(t *term, id string, x, y, w, h int) {
		if box, x0, ok := t.findBox(m, id, w, h); ok {
			boxes, xs, ys = append(boxes, box), append(xs, x+x0), append(ys, y)
		}
	}

	switch {
	case m.showsSession():
		add(&m.term, m.sess, m.mainX(), 1+m.stripH(), m.mainW(), m.sessH())
	case m.sessDocked() && m.shown(viewSession):
		i := m.colOf(viewSession)
		add(&m.term, m.sess, m.colRect(i).x, m.bodyTop(viewSession)+1, m.sessW(), max(m.bodyH(viewSession)-1, 1))
	}

	if !m.termShowing() {
		return boxes, xs, ys
	}

	w, h := m.termBody()
	if m.termRows() > 0 {
		add(&m.tv.term, m.tv.id, m.mainX(), m.mainH()+1, w, h)
	} else {
		add(&m.tv.term, m.tv.id, m.colRect(m.colOf(viewTerm)).x, m.bodyTop(viewTerm)+1, w, h)
	}

	return boxes, xs, ys
}

// findMouse handles a click on the widget at body cell (x, y) of a terminal
// w cells wide; ok is false outside it. A click elsewhere on the terminal
// leaves the widget open and gives the keyboard back to the shell.
func (t *term) findMouse(m *Model, id string, msg tea.MouseMsg, x, y, w int) (tea.Cmd, bool) {
	x0, bw, ok := t.findRect(w, len(t.scr.Lines))
	if !ok {
		return nil, false
	}

	_, click := msg.(tea.MouseClickMsg)
	if y < 0 || y >= 3 || x < x0 || x >= x0+bw {
		if click {
			t.find.editing = false
			t.find.input.Blur()
		}

		return nil, false
	}

	if !click || msg.Mouse().Button != tea.MouseLeft {
		return nil, true
	}

	if _, acts := t.findActs(id, bw); y == 1 {
		if a, ok := hit(acts, x-x0); ok {
			return a.run(m), true
		}
	}

	t.find.editing = true

	return t.find.input.Focus(), true
}

// toggleFind flips match case, whole word or regular expression and searches again.
func (t *term) toggleFind(m *Model, id, key string) tea.Cmd {
	switch key {
	case "alt+c":
		t.find.caseSens = !t.find.caseSens
	case "alt+w":
		t.find.word = !t.find.word
	case "alt+r":
		t.find.regex = !t.find.regex
	}

	return t.search(m, id, 2)
}

// findKey is a key while the widget's query has the keyboard. ⏎ goes to the
// previous match, up the scrollback toward older output, and ⇧⏎ to the
// next, as VS Code's terminal find does; the arrows follow the buttons.
func (t *term) findKey(m *Model, id string, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "esc", "ctrl+f":
		t.closeFind()
		return nil

	case "enter", "shift+f3", "up", "ctrl+p":
		return t.findGo(m, id, -1)
	case "shift+enter", "f3", "down", "ctrl+n":
		return t.findGo(m, id, 1)
	case "alt+c", "alt+w", "alt+r":
		return t.toggleFind(m, id, s)
	}

	before := t.find.input.Value()

	var cmd tea.Cmd

	t.find.input, cmd = t.find.input.Update(k)
	if t.find.input.Value() != before {
		return tea.Batch(cmd, t.search(m, id, 2))
	}

	return cmd
}

// refind searches again after the screen changed under an open widget,
// staying on the selected match; one search at a time, so a streaming agent
// does not queue a read per frame.
func (t *term) refind(m *Model) tea.Cmd {
	if !t.find.on || t.find.busy || t.find.input.Value() == "" {
		return nil
	}

	move := 0
	if t.find.id != t.id { // another session in the same view
		move = 2
		t.find.hits = nil
	}

	return t.search(m, t.id, move)
}
