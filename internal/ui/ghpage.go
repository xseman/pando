package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// A pull request's or an issue's page in the editor has its state and its
// commands in the header row, right-aligned where VS Code's editor actions
// are: ● Open ✓ 2/2, then the leading command with its name and a ▾, and the
// rest as icons. The footer names the one under the pointer. Every button is
// an action of ghActs, which the page's keys and its context menu run as well.

// ghAct is a command of a pull request's or an issue's page.
type ghAct struct {
	label   string // the menu's entry; "" is a rule
	g       glyph  // the header's button; the zero glyph for a menu entry only
	btn     string // its name, shown by the leading one
	key     string // bound while the page is focused
	run     func(*Model) tea.Cmd
	primary bool // leads the header, with its name
	group   int  // the commands between two rules; the primary one's group is its ▾ menu
}

// ghActs are the commands the page's state allows, in menu order: a closed
// pull request has Reopen, no Merge, a draft has Ready in its place. One of
// them leads the header: Ready, Merge while no check is in its way, else the
// worktree, and Reopen for a closed one.
func (p *preview) ghActs() []ghAct {
	d := p.gh
	kind, ns, _ := strings.Cut(p.rev, "/")
	dir, pr, open := p.root, kind == "pr", d.State == "OPEN"
	mergeFirst := pr && open && !d.IsDraft && (d.Checks() == "pass" || d.Checks() == "")

	var (
		acts []ghAct
		none glyph
	)

	group := 0

	add := func(label, btn, key string, g glyph, run func(*Model) tea.Cmd) {
		acts = append(acts, ghAct{label: label, btn: btn, key: key, g: g, run: run, group: group})
	}
	lead := func(on bool) { acts[len(acts)-1].primary = on }
	rule := func() {
		acts = append(acts, ghAct{})
		group++
	}
	// Every command that changes the page asks first.
	do := func(question, label, text string, args ...string) func(*Model) tea.Cmd {
		return ghConfirm(question, label, func(*Model) tea.Cmd { return ghDo(dir, text, args...) })
	}

	switch {
	case pr && open:
		add("Checkout in New Worktree", "Worktree", "w", icWorktree, func(m *Model) tea.Cmd { return m.gh.checkout(m, d.Item) })
		lead(!mergeFirst && !d.IsDraft)

	case open:
		add("Start Working in New Worktree…", "Worktree", "w", icWorktree, func(m *Model) tea.Cmd { return m.gh.startIssue(m, d.Item) })
		lead(true)
	}

	if pr {
		add("Open Changes", "Changes", "d", icCompare, func(m *Model) tea.Cmd { return m.openGHDiff(d.Number) })
		add("Open Changed Files", "", "", none, func(m *Model) tea.Cmd { return m.comparePR(d.Number) })
	}

	switch {
	case pr && open && d.IsDraft:
		add("Mark Ready for Review", "Ready", "R", icCheckAll,
			do(fmt.Sprintf("Mark pull request #%s ready for review? Its reviewers are notified.", ns), "Mark Ready", "#"+ns+" is ready for review", "pr", "ready", ns))
		lead(true)

	case pr && open:
		add("Merge Pull Request…", "Merge", "M", icMerge, func(m *Model) tea.Cmd { return m.gh.merge(m, d.Item) })
		lead(mergeFirst)
		add("Convert to Draft", "", "", none,
			do(fmt.Sprintf("Convert pull request #%s to a draft?", ns), "Convert to Draft", "#"+ns+" is a draft", "pr", "ready", ns, "--undo"))
	}

	rule()
	add("Add Comment…", "Comment", "c", icComment, func(m *Model) tea.Cmd { return m.pv.ghCompose(m, "") })

	what := map[string]string{"pr": "Pull Request", "issue": "Issue"}[kind]

	switch {
	case open:
		add("Close "+what, "Close", "x", icPRClosed,
			do(fmt.Sprintf("Close %s #%s?", strings.ToLower(what), ns), "Close "+what, "closed #"+ns, kind, "close", ns))

	case d.State == "CLOSED":
		add("Reopen "+what, "Reopen", "x", icReopen,
			do(fmt.Sprintf("Reopen %s #%s?", strings.ToLower(what), ns), "Reopen "+what, "reopened #"+ns, kind, "reopen", ns))
		lead(true)
	}

	rule()
	add("Edit Title…", "", "", none, func(m *Model) tea.Cmd { return ghEditTitle(m, dir, kind, ns, d.Title) })

	if pr && open {
		var have []string
		for _, r := range d.reviewers() {
			have = append(have, r.login)
		}

		add("Edit Reviewers…", "", "", none, func(m *Model) tea.Cmd { return ghEditList(m, dir, kind, ns, "Reviewers", "reviewer", have) })
	}

	add("Edit Assignees…", "", "", none, func(m *Model) tea.Cmd { return ghEditList(m, dir, kind, ns, "Assignees", "assignee", d.people()) })
	add("Edit Labels…", "", "", none, func(m *Model) tea.Cmd { return ghEditList(m, dir, kind, ns, "Labels", "label", d.labels()) })
	rule()
	add("Open on GitHub", "", "o", none, func(*Model) tea.Cmd { return openURL(d.URL) })
	add("Copy Link", "", "", none, func(*Model) tea.Cmd { return setClipboard(d.URL, "copied "+d.URL) })
	add("Refresh", "", "ctrl+r", none, func(m *Model) tea.Cmd { return m.pv.load(m) })

	return acts
}

// ghItems are the page's commands for its context menu.
func (p *preview) ghItems() []item {
	var out []item

	for _, a := range p.ghActs() {
		if a.label == "" {
			out = append(out, separator())
			continue
		}

		out = append(out, item{label: a.label, hint: keyHint(a.key), run: a.run})
	}

	return append(out, separator())
}

// keyHint writes a key as the menus and the footer do: ctrl+r as ^r.
func keyHint(k string) string { return strings.Replace(k, "ctrl+", "^", 1) }

// ghCached is a page as it was fetched: its text and its document.
type ghCached struct {
	raw string
	doc *ghDoc
	at  time.Time // when it was fetched
}

// ghClick runs what the row under a click does: a commit shows, a comment
// folds, the ⋯ and the gears list or edit. Shift keeps selecting.
func (p *preview) ghClick(m *Model, at pos, mo tea.Mouse) (tea.Cmd, bool) {
	if p.kind != pvGH || p.gh == nil || mo.Mod&tea.ModShift != 0 || at.line >= len(p.ghHits) {
		return nil, false
	}

	hits := p.ghHits[at.line]
	for i := len(hits) - 1; i >= 0; i-- { // the later ones are the narrower: a ⋯ on a row that folds
		if h := hits[i]; at.col >= h.x0 && at.col < h.x1 {
			return h.run(m), true
		}
	}

	return nil, false
}

// ghHot is the key of the hit under the pointer on the page, w cells wide
// and h rows high, "" when it lights nothing.
func (p *preview) ghHot(m *Model, w, h int) string {
	x, y := m.mouseX-m.mainX(), m.mouseY-m.headH()-m.stripH()
	if p.kind != pvGH || p.gh == nil || m.modal != nil || x < 0 || x >= w || y < 0 || y >= h {
		return ""
	}

	at := p.posAt(w, x, y)
	if p.top+y >= len(p.rows(w)) || at.line >= len(p.ghHits) { // under the page's end
		return ""
	}

	hits := p.ghHits[at.line]
	for i := len(hits) - 1; i >= 0; i-- { // as ghClick: the narrower ones come later
		if h := hits[i]; at.col >= h.x0 && at.col < h.x1 {
			return h.key
		}
	}

	return ""
}

// ghKey runs the page's command bound to k; false when none is. ⏎ runs the
// first of the row the cursor is on.
func (p *preview) ghKey(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if p.kind != pvGH || p.gh == nil {
		return nil, false
	}

	s := k.String()
	if line := p.cur.line; s == "enter" && line < len(p.ghHits) && len(p.ghHits[line]) > 0 {
		return p.ghHits[line][0].run(m), true
	}

	for _, a := range p.ghActs() {
		if a.key == s {
			return a.run(m), true
		}
	}

	return nil, false
}

// checksSeg is how many of the checks passed, in the color of their sum;
// false for a pull request with none.
func (d ghDoc) checksSeg() (seg, bool) {
	c := d.Checks()
	if c == "" {
		return seg{}, false
	}

	pass := 0

	for _, k := range d.Rollup {
		if (git.Item{Rollup: []git.Check{k}}).Checks() == "pass" {
			pass++
		}
	}

	mark, letter := map[string]string{"pass": "✓", "fail": "✗", "pending": "○"}[c], map[string]byte{"pass": 'A', "fail": 'D', "pending": 'M'}[c]

	return sg(fmt.Sprintf("%s %d/%d ", mark, pass, len(d.Rollup)), fg(statusColor(letter))), true
}

// ghHeader draws the page's state and buttons as the right end of the header
// row, w cells wide. The leading button has a ▾ beside it that lists the
// commands of its group; the state gives way when the row is short.
func (p *preview) ghHeader(m *Model, w int) (segs []seg, btns []rowAction, width int) {
	d := p.gh
	all := p.ghActs()
	name, _ := p.label(m.ws)

	type piece struct {
		text string
		st   lipgloss.Style
		btn  *rowAction
	}

	var (
		state  []piece
		pieces []piece
	)

	word, c := "Open", pal.added

	switch {
	case d.State == "MERGED":
		word, c = "Merged", pal.attention
	case d.State == "CLOSED":
		word, c = "Closed", pal.deleted
	case d.IsDraft:
		word, c = "Draft", pal.descFg
	}

	state = append(state, piece{text: " ● " + word + " ", st: fg(c)})

	if g, ok := d.checksSeg(); ok {
		state = append(state, piece{text: " " + g.s, st: g.st})
	}

	add := func(text string, tip string, st lipgloss.Style, run func(*Model) tea.Cmd) {
		pieces = append(pieces, piece{text: text, st: st, btn: &rowAction{tip: tip, run: run}})
	}

	btnAll := slices.DeleteFunc(slices.Clone(all), func(a ghAct) bool { return a.btn == "" })
	slices.SortStableFunc(btnAll, func(a, b ghAct) int { return b2i(b.primary) - b2i(a.primary) })

	for i, a := range btnAll {
		tip := keyHint(a.key)
		label := " " + a.g.s() + " "

		if i == 0 && a.primary {
			if a.g.s() != a.btn { // in text mode the glyph is the name already
				label = " " + a.g.s() + " " + a.btn + " "
			}

			add(label, a.label+"  "+tip, accent, a.run)

			if a.group == 0 { // its group is the menu of the ▾
				var items []item

				for _, g := range all {
					if g.group == 0 && g.label != "" {
						items = append(items, item{label: g.label, hint: keyHint(g.key), run: g.run})
					}
				}

				add("▾ ", "More "+a.btn+" commands", accent, func(m *Model) tea.Cmd { return m.menuOf(items, m.mouseX, m.mouseY+1) })
			}

			continue
		}

		add(label, a.label+"  "+tip, dim, a.run)
	}

	if at := m.ghPages[p.id()].at; !at.IsZero() && time.Since(at) >= time.Minute { // shown from memory: how old
		pieces = append(pieces, piece{text: " " + ghAgo(at), st: dim})
	}

	add(" "+icRefresh.s()+" ", "Refresh  ^r", dim, func(m *Model) tea.Cmd { return m.pv.load(m) })
	add(" "+icEllipsis.s()+" ", "More commands  m", dim, func(m *Model) tea.Cmd { return m.pv.menu(m, m.mouseX, m.mouseY+1) })

	size := func(ps []piece) (n int) {
		for _, q := range ps {
			n += ansi.StringWidth(q.text)
		}

		return n
	}

	if ansi.StringWidth(name)+16+size(state)+size(pieces) <= w { // ✕ PR #n, and room for a title
		pieces = append(state, pieces...)
	}

	x, y := w-size(pieces)-1, m.stripH() // the header is the row under the strip

	for _, q := range pieces {
		st, tw := q.st, ansi.StringWidth(q.text)
		if q.btn != nil {
			q.btn.x, q.btn.w = x, tw
			btns = append(btns, *q.btn)

			if mx := m.mouseX - m.mainX(); m.mouseY == y && mx >= x && mx < x+tw {
				st, m.tip = keycapHot(), q.btn.tip
			}
		}

		segs = append(segs, sg(q.text, st))
		x += tw
	}

	return segs, btns, size(pieces)
}

// ghHit is the page's header button at column x, if any.
func (p *preview) ghHit(m *Model, x int) (rowAction, bool) {
	if p.kind != pvGH || p.gh == nil {
		return rowAction{}, false
	}

	_, btns, _ := p.ghHeader(m, m.mainW())

	return hit(btns, x)
}

// ghDo runs gh in dir and reports text, then the lists and the open page are
// fetched again (onDone).
func ghDo(dir, text string, args ...string) tea.Cmd {
	return func() tea.Msg {
		_, err := git.GH(dir, args...)
		return ghDoneMsg{text: text, err: err}
	}
}

// ghEditTitle asks for a new title for pull request or issue n.
func ghEditTitle(m *Model, dir, kind, n, title string) tea.Cmd {
	m.modal = newPrompt("Title of #"+n, title, func(_ *Model, v string) tea.Cmd {
		v = strings.TrimSpace(v)
		if v == "" || v == title {
			return nil
		}

		return ghDo(dir, "renamed #"+n, kind, "edit", n, "--title", v)
	})

	return nil
}

// ghEditList asks for the logins or labels n should have, comma separated,
// and adds and removes the difference. flag is gh edit's: reviewer, assignee
// or label.
func ghEditList(m *Model, dir, kind, n, title, flag string, have []string) tea.Cmd {
	m.modal = newPrompt(title+", comma separated", strings.Join(have, ", "), func(_ *Model, v string) tea.Cmd {
		var want []string

		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimPrefix(strings.TrimSpace(s), "@"); s != "" {
				want = append(want, s)
			}
		}

		without := func(a, b []string) string {
			return strings.Join(slices.DeleteFunc(slices.Clone(a), func(s string) bool { return slices.Contains(b, s) }), ",")
		}

		args := []string{kind, "edit", n}
		if add := without(want, have); add != "" {
			args = append(args, "--add-"+flag, add)
		}

		if del := without(have, want); del != "" {
			args = append(args, "--remove-"+flag, del)
		}

		if len(args) == 3 {
			return nil
		}

		return ghDo(dir, "updated #"+n, args...)
	})

	return nil
}

// ghConfirm asks question before run: a dialog whose first button is the
// command, labelled label.
func ghConfirm(question, label string, run func(*Model) tea.Cmd) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		m.modal = newDialog(question, item{label: label, run: run}, cancelItem())
		return nil
	}
}

// newCommentArea is the box at the end of the page: Enter is a new line, as
// in a comment on the web; ^s sends.
func newCommentArea() textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.Placeholder = "Use Markdown to format your comment"
	ta.DynamicHeight, ta.MinHeight, ta.MaxHeight = true, 3, 12
	ta.MaxContentHeight = 1 << 16
	ta.KeyMap.SelectAll = key.NewBinding(key.WithKeys("ctrl+a"))
	ta.KeyMap.LineStart = key.NewBinding(key.WithKeys("home"))

	return ta
}

// composer is the page's comment box, made when it is first wanted and kept
// for the life of the TUI, as GitHub keeps a draft: closing the page and
// opening it again brings back what was typed.
func (p *preview) composer(m *Model) *textarea.Model {
	if p.compose == nil {
		if m.ghDrafts == nil {
			m.ghDrafts = map[string]*textarea.Model{}
		}

		if p.compose = m.ghDrafts[p.id()]; p.compose == nil {
			ta := newCommentArea()
			p.compose = &ta
			m.ghDrafts[p.id()] = p.compose
		}
	}

	p.compose.SetStyles(areaStyles(m.dark))

	return p.compose
}

// ghCompose puts the cursor in the comment box at the end of the page, with
// quote in it when there is one (after what is there already).
func (p *preview) ghCompose(m *Model, quote string) tea.Cmd {
	ta := p.composer(m)
	if quote != "" {
		ta.SetValue(ta.Value() + quote)
	}

	m.focus = onMain
	p.composing = true

	p.relayout(m)

	return ta.Focus()
}

// relayout lays the page out again, as the box has changed, and shows the
// end of it, where the box is.
func (p *preview) relayout(m *Model) {
	p.mdLines, p.mdShown = nil, 0
	p.syncMarkdown(m)
	p.top = max(len(p.plain)-m.pvH(), 0)
	p.cur.line = p.lastLine()
}

// composeKey is a key while the comment box has the keyboard: ^s sends, esc
// leaves it with its text, the rest is typing.
func (p *preview) composeKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		p.composing = false
		p.compose.Blur()
		p.relayout(m)

		return nil

	case "ctrl+s", "ctrl+enter":
		return p.ghSend(m, "")
	}

	var cmd tea.Cmd

	*p.compose, cmd = p.compose.Update(k)

	p.relayout(m)

	return cmd
}

// ghSentMsg is a comment, or a close or reopen with one, that gh took or
// refused: the box empties on success and keeps its text otherwise.
type ghSentMsg struct {
	area *textarea.Model
	text string
	err  error
}

// ghSend sends the box's text as a comment, or with verb "close" or "reopen"
// as the comment of that command. The box keeps it until gh has taken it.
func (p *preview) ghSend(m *Model, verb string) tea.Cmd {
	text := strings.TrimSpace(p.composer(m).Value())
	if text == "" && verb == "" {
		return flash("nothing to send", true)
	}

	kind, n, _ := strings.Cut(p.rev, "/")
	dir, area := p.root, p.compose
	args := []string{kind, "comment", n, "--body", text}
	done := "commented"

	if verb != "" {
		args, done = []string{kind, verb, n}, verb+"d #"+n
		if text != "" {
			args, done = append(args, "--comment", text), verb+"d #"+n+" with your comment"
		}
	}

	return func() tea.Msg {
		_, err := git.GH(dir, args...)
		return ghSentMsg{area: area, text: done, err: err}
	}
}

// onGHSent empties the box a comment was sent from, and fetches the page and
// the lists, where it now stands.
func (m *Model) onGHSent(msg ghSentMsg) tea.Cmd {
	if msg.err != nil {
		return flash("gh: "+ghErrLine(msg.err), true)
	}

	msg.area.Reset()

	if m.pv.compose == msg.area {
		m.pv.composing = false

		msg.area.Blur()
		m.pv.relayout(m)
	}

	return m.gh.onDone(m, ghDoneMsg{text: msg.text})
}
