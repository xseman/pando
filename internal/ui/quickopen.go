package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Quick open's tabs, as JetBrains' Search Everywhere has them: everything,
// then one kind each.
const (
	qoAll = iota
	qoFiles
	qoSessions
	qoText
)

var qoTabs = []string{"All", "Files", "Sessions", "Text"}

const (
	quickTextMin   = 2                      // the shortest query searched for in the files
	quickTextLimit = 200                    // the most matches a query finds; Search finds them all
	quickTextAll   = 5                      // the lines All shows
	quickTextWait  = 150 * time.Millisecond // the pause in typing a search waits for
)

// quickOpenState is one quick open's lists and its search through the
// files' text.
type quickOpenState struct {
	md           *modal
	ws           string
	files        []item // the recent ones first
	recent       int
	sessions     []item // alt+t's: the sessions, then the worktrees
	waiting      []item // the sessions waiting on you, All's while browsing
	base         []item // All's files and sessions, grouped for browsing or a query
	baseBrowsing bool
	asked        bool // "@" went on to the symbols

	query           string // the one the text belongs to
	text            []searchFile
	truncated, busy bool
	err             error
	gen             int
	cancel          context.CancelFunc
}

type (
	quickTickMsg struct { // a pause in typing ended
		q   *quickOpenState
		gen int
	}
	quickTextMsg struct { // a batch of the text search
		q *quickOpenState
		searchMsg
	}
)

// quickOpen is ctrl+p, and alt+t on its Sessions tab: files, sessions and
// the lines of files in one picker, a tab for each kind and All for the lot.
// A "path:12:5" query opens that line and column.
func (m *Model) quickOpen(msg indexMsg, tab int) {
	// The last few files opened come first, under their own heading while
	// browsing; a query ranks all files as one list, where the stable sort
	// lets them win a tie between equal matches.
	var recent []string

	for _, p := range m.files {
		if rel, err := filepath.Rel(msg.ws, p); err == nil && slices.Contains(msg.files, rel) {
			recent = append(recent, rel)
		}
	}

	recent = recent[:min(len(recent), 5)]

	files := slices.Concat(recent, slices.DeleteFunc(slices.Clone(msg.files), func(f string) bool { return slices.Contains(recent, f) }))

	md := &modal{filter: true, x: -1, input: modalInput(), tabs: qoTabs}
	q := &quickOpenState{md: md, ws: msg.ws, recent: len(recent), sessions: m.navigatorItems(), waiting: m.sessionItems(m.alarms())}

	q.files = make([]item, len(files))
	for i, f := range files {
		p := filepath.Join(msg.ws, f)

		q.files[i] = item{label: f, run: func(m *Model) tea.Cmd {
			m.ex.reveal(m, p)

			line, col := parseLineCol(lineSuffix(md.input.Value()))

			return m.openLine(p, line, col)
		}}
	}

	md.setTab, md.change, md.alt = q.setTab, q.change, q.toSearch
	md.cancel = func(*Model) { q.stop() } // esc: no rg left reading for nobody
	_ = q.setTab(m, tab)                  // an empty query searches nothing
	m.modal = md
}

// setTab shows tab i for the query as typed.
func (q *quickOpenState) setTab(m *Model, i int) tea.Cmd {
	md := q.md
	md.tab, md.lineQuery, md.treeable = i, i == qoAll || i == qoFiles, i == qoFiles
	md.tree = md.treeable && m.st.Settings.QuickTree

	md.caps = nil
	if i == qoAll {
		md.caps = map[string]int{"files": 8, "sessions": 3}
	}

	cmd := q.search()
	q.fill(m)
	md.refilter()

	return cmd
}

// change follows the query. In All and Files "@" goes to a symbol in the
// open file and ":" to a line, as in VS Code.
func (q *quickOpenState) change(m *Model, v string) tea.Cmd {
	if q.md.tab == qoAll || q.md.tab == qoFiles {
		if strings.HasPrefix(v, ":") && m.showsPreview() && m.pv.ready {
			m.modal = nil
			return m.gotoLineQuery(v)
		}

		if strings.HasPrefix(v, "@") {
			if q.asked {
				return nil
			}

			q.asked = true

			return m.gotoSymbol(v)
		}

		q.asked = false
	}

	cmd := q.search()
	q.fill(m)

	if cmd != nil { // the modal refilters only after a change that asks for nothing
		q.md.refilter()
	}

	return cmd
}

// search starts over when the tab shows text and the text found is another
// query's: after a pause in typing, from quickTextMin characters on.
func (q *quickOpenState) search() tea.Cmd {
	v := q.md.input.Value()
	if q.md.tab != qoAll && q.md.tab != qoText || v == q.query {
		return nil
	}

	q.stop()
	q.gen++
	q.query, q.text, q.truncated, q.err = v, nil, false, nil

	if q.busy = q.long(); !q.busy {
		return nil
	}

	gen := q.gen

	return tea.Tick(quickTextWait, func(time.Time) tea.Msg { return quickTickMsg{q, gen} })
}

// long reports whether the query is long enough to search the text for.
func (q *quickOpenState) long() bool {
	return utf8.RuneCountInString(strings.TrimSpace(q.query)) >= quickTextMin
}

// onTick runs the search the pause in typing waited for, as the Search view
// does but stopped at quickTextLimit.
func (q *quickOpenState) onTick(m *Model, gen int) tea.Cmd {
	if gen != q.gen || m.modal != q.md {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel

	return q.wait(searchAsync(ctx, q.ws, searchOpts{query: q.query, hidden: m.st.Settings.Hidden, limit: quickTextLimit}, gen))
}

// wait is the search's next batch.
func (q *quickOpenState) wait(ch chan searchMsg) tea.Cmd {
	return func() tea.Msg { return quickTextMsg{q, <-ch} }
}

// onText shows a batch of the search, the selection kept on its row: the
// text comes last, after what is selected.
func (q *quickOpenState) onText(m *Model, msg searchMsg) tea.Cmd {
	switch {
	case msg.gen != q.gen:
		return nil
	case m.modal != q.md: // closed meanwhile
		q.stop()
		return nil
	}

	q.text, q.truncated, q.err = msg.files, msg.truncated, msg.err
	if !msg.partial {
		q.stop()
	}

	if md := q.md; md.tab == qoAll || md.tab == qoText {
		sel, top := md.l.sel, md.l.top

		q.fill(m)
		md.refilter()

		if sel >= 0 && sel < len(md.disp) && md.disp[sel].run != nil {
			md.l.sel, md.l.top = sel, top
		}
	}

	if msg.partial {
		return q.wait(msg.ch)
	}

	return nil
}

// stop ends a search that runs.
func (q *quickOpenState) stop() {
	if q.cancel != nil {
		q.cancel()
	}

	q.busy, q.cancel = false, nil
}

// fill lists the tab's items for the query.
func (q *quickOpenState) fill(m *Model) {
	md, browsing := q.md, q.md.input.Value() == ""
	md.note = ""

	switch md.tab {
	case qoAll:
		if q.base == nil || q.baseBrowsing != browsing {
			q.base, q.baseBrowsing = q.allBase(browsing), browsing
		}

		md.items = q.base // the text goes into the room allBase left: the files are not copied for each key
		md.items = append(md.items, q.textItems(m, quickTextAll)...)

	case qoFiles:
		for i := range q.files {
			q.files[i].group = ""
			if browsing && q.recent > 0 {
				q.files[i].group = ifElse(i < q.recent, recentGroup, "files")
			}
		}

		md.items = q.files

	case qoSessions:
		md.items = q.sessions

	case qoText:
		md.items = q.textItems(m, 0)
		if q.long() && (q.busy || len(q.text) > 0) {
			md.note = q.status()
		}
	}

	if md.items == nil { // a picker with no items is no picker: ctrl+p over it opens quick open
		md.items = []item{}
	}
}

// allBase is All's files and sessions under their headings: while browsing
// the recent files, the sessions waiting on you and the other files; under a
// query all files, then the sessions and worktrees. It leaves room for the
// text.
func (q *quickOpenState) allBase(browsing bool) []item {
	type part struct {
		items []item
		group string
	}

	parts := []part{{q.files, "files"}, {q.sessions, "sessions"}}
	if browsing {
		parts = []part{{q.files[:q.recent], recentGroup}, {q.waiting, "waiting on you"}, {q.files[q.recent:], "files"}}
	}

	one := browsing && q.recent == 0 && len(q.waiting) == 0 // nothing to set apart: no headings
	n := 0

	for _, p := range parts {
		n += len(p.items)
	}

	out := make([]item, 0, n+quickTextAll+2) // a heading, the lines, a "+ N more"

	for _, p := range parts {
		for _, it := range p.items {
			it.group = ifElse(one, "", p.group)
			out = append(out, it)
		}
	}

	return out
}

// textItems are the lines found: in All (n > 0) the first n under a heading
// with a "+ N more" row, nothing when there is nothing to show; in Text all
// of them, or a row that says why there are none.
func (q *quickOpenState) textItems(m *Model, n int) []item {
	note := func(label, hint string) []item { return []item{{label: label, hint: hint, sep: true, always: true}} }

	if n > 0 {
		if !q.long() || !q.busy && len(q.text) == 0 && q.err == nil {
			return nil
		}

		rows := q.lines(m)
		out := append(note("text", q.status()), rows[:min(n, len(rows))]...)

		if len(rows) > n {
			more := q.md.more("text", len(rows)-n)
			if q.truncated { // more than the search counted
				more.label = fmt.Sprintf("+ %d+ more", len(rows)-n)
			}

			more.always = true
			out = append(out, more)
		}

		return out
	}

	switch {
	case !q.long():
		return note(fmt.Sprintf("type %d or more characters", quickTextMin), "")
	case q.err != nil:
		return note(q.err.Error(), "")
	case len(q.text) == 0 && q.busy:
		return note("searching…", "")
	case len(q.text) == 0:
		return note("no results for “"+q.query+"”", "alt+⏎ Search")
	}

	rows := q.lines(m)
	if q.truncated {
		rows = append(rows, item{label: "all in Search", hint: "alt+⏎", always: true, run: q.toSearch})
	}

	return rows
}

// status counts the matches as the Search view does, "200+" where the
// search stopped.
func (q *quickOpenState) status() string {
	switch {
	case q.busy:
		return "searching…"
	case q.err != nil:
		return q.err.Error()
	case q.truncated:
		return fmt.Sprintf("%d+ in %s", quickTextLimit, plural(len(q.text), "file"))
	}

	n := 0
	for _, f := range q.text {
		n += f.count
	}

	return fmt.Sprintf("%d in %s", n, plural(len(q.text), "file"))
}

// lines are a row for each line found, its path and number on the right;
// ⏎ opens it with the match selected.
func (q *quickOpenState) lines(m *Model) []item {
	_, _, w, _, _ := q.md.rect(m)

	var out []item

	for _, f := range q.text {
		p := filepath.Join(q.ws, f.path)

		for _, ln := range f.lines {
			hint := fmt.Sprintf("%s:%d", f.path, ln.line)
			if over := ansi.StringWidth(hint) - (w-2)/2; over > 0 { // a long path keeps its end: the name and the line
				hint = ansi.TruncateLeft(hint, over+1, "…")
			}

			out = append(out, item{label: excerpt(ln, w-ansi.StringWidth(hint)-5), hint: hint, always: true, run: func(m *Model) tea.Cmd {
				m.ex.reveal(m, p)

				col, n := ln.span()

				return m.openFileAt(p, ln.line-1, col, n, false)
			}})
		}
	}

	return out
}

// excerpt is ln's text for room cells: without its indentation, and from a
// little before the match when the match would end past room, as VS Code's
// Search shows it, though no later than fills the room.
func excerpt(ln searchLine, room int) string {
	t := strings.ReplaceAll(ln.text, "\t", " ") // byte for byte: the ranges hold
	from := len(t) - len(strings.TrimLeft(t, " "))

	if len(ln.ranges) > 0 {
		a, b := ln.ranges[0][0], ln.ranges[0][1]
		if from = min(from, a); ansi.StringWidth(t[from:b]) > room-1 {
			// A little before the match, earlier where the line ends before
			// the room does, later where the room is too short for the match.
			from = max(min(a-14, len(t)-room+1), min(b-room+2, a), from)
			for from > 0 && !utf8.RuneStart(t[from]) {
				from--
			}

			return "…" + t[from:]
		}
	}

	return t[from:]
}

// toSearch hands the query to the Search view, which has the regex, the
// replace, the files to include and every match: alt+⏎ in All and Text.
func (q *quickOpenState) toSearch(m *Model) tea.Cmd {
	if q.md.tab != qoAll && q.md.tab != qoText {
		return nil
	}

	q.stop()

	m.modal = nil
	m.sr.query.SetValue(q.md.input.Value())

	return tea.Batch(m.showView(viewSearch), m.sr.focus(), m.sr.restart(m))
}
