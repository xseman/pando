package ui

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// The page of a pull request or an issue is laid out as GitHub's web page is,
// and the VS Code extension's: the conversation down a vertical rail, a box
// for each comment with its author and time across the top, commits as rows
// on the rail, and from ghWideMin cells a column beside it with the reviewers,
// assignees, labels and checks. The lines are the editor's own, so the cursor
// walks them and a selection copies them; ghHit says what a click does on a
// row of them.

const (
	ghWideMin = 110 // the width from which the sidebar stands beside the conversation
	ghSideW   = 32  // its column, with the divider and the air after it
)

// ghHit is what a click, or ⏎ with the cursor on the row, does over the
// columns x0..x1 of a row of the page.
type ghHit struct {
	x0, x1     int
	run        func(*Model) tea.Cmd
	key        string // the pointer on a hit tints the lit cells of every hit with its key, and is p.hot
	lit0, lit1 int    // none when equal
}

// ghFramed reports a hot key the layout draws, a frame or a ⋯ lit, which is
// laid out again when the pointer comes or goes; a commit's row is tinted as
// it is drawn.
func ghFramed(key string) bool {
	return key != "" && !strings.HasPrefix(key, "commit ")
}

// ghLine is one row of the page as drawn, the plain text of it, and its hits.
type ghLine struct {
	mdText
	hits []ghHit
}

func (r ghLine) width() int { return ansi.StringWidth(r.p) }

// padTo fills the row with blanks to w cells, or cuts it there.
func (r ghLine) padTo(w int) ghLine {
	if n := r.width(); n > w {
		r.s, r.p = ansi.Truncate(r.s, w, "…"), ansi.Truncate(r.p, w, "…")
	} else {
		r.mdText = r.add(mdText{blank(w - n), blank(w - n)})
	}

	return r
}

// ghLayout lays the loaded page out w cells wide, and keeps what a click on
// each row does in p.ghHits.
func (p *preview) ghLayout(m *Model, w int) (styled, plain []string) {
	d := p.gh
	kind, ns, _ := strings.Cut(p.rev, "/")
	faint := fg(pal.descFg) // Faint is not drawn by every terminal

	var rows []ghLine

	head := mdt(" "+d.Author.Login, bold)
	if kind == "pr" {
		head = head.add(mdt(" · "+d.HeadRefName+" → "+d.BaseRefName, faint))
	}

	wide := w >= ghWideMin
	lw := w

	if wide {
		lw = w - ghSideW
	}

	rows = append(rows, ghLine{mdText: head}, ghLine{})

	if !wide {
		rows = append(rows, p.ghMeta(faint)...)
		rows = append(rows, ghLine{})
	}

	rows = append(rows, p.ghConversation(m, ns, lw, faint)...)

	if wide {
		side := p.ghSide(ghSideW-3, faint) // the divider and the air before it take three
		rows = ghJoin(rows, side, lw)
	}

	styled, plain = make([]string, len(rows)), make([]string, len(rows))
	p.ghHits = make([][]ghHit, len(rows))

	for i, r := range rows {
		styled[i], plain[i], p.ghHits[i] = r.s, r.p, r.hits
	}

	return styled, plain
}

// ghJoin puts the sidebar's rows beside the first rows of the conversation,
// behind a divider; the conversation's rows are lw cells.
func ghJoin(left, side []ghLine, lw int) []ghLine {
	for i, s := range side {
		for i >= len(left) {
			left = append(left, ghLine{})
		}

		l := left[i].padTo(lw)
		div := mdt(" │ ", fg(pal.rulerBorder))
		hits := l.hits

		for _, h := range s.hits {
			hits = append(hits, ghHit{x0: h.x0 + lw + 3, x1: h.x1 + lw + 3, run: h.run})
		}

		left[i] = ghLine{mdText: l.add(div).add(s.mdText), hits: hits}
	}

	return left
}

// ghActed is the page's command labelled label, or nil where the state has
// none: Edit Reviewers is for an open pull request only.
func (p *preview) ghActed(label string) func(*Model) tea.Cmd {
	for _, a := range p.ghActs() {
		if a.label == label {
			return a.run
		}
	}

	return nil
}

// ghMeta is who is on the page, in a few rows above the conversation: the
// sidebar's content for a panel too narrow to have one.
func (p *preview) ghMeta(faint lipgloss.Style) []ghLine {
	d := p.gh

	var rows []ghLine

	row := func(label string, v mdText) {
		rows = append(rows, ghLine{mdText: mdt(fmt.Sprintf(" %-11s", label), faint).add(v)})
	}

	if rs := d.reviewers(); len(rs) > 0 {
		parts := make([]string, len(rs))
		for i, r := range rs {
			parts[i] = r.mark + " " + r.login
		}

		row("Reviewers", mdt(strings.Join(parts, "  "), plain))
	}

	if as := d.people(); len(as) > 0 {
		row("Assignees", mdt(strings.Join(as, ", "), plain))
	}

	if ls := d.labels(); len(ls) > 0 {
		row("Labels", mdt(strings.Join(ls, "  "), plain))
	}

	if g, ok := d.checksSeg(); ok {
		row("Checks", mdt(strings.TrimSpace(g.s), plain))
	}

	return rows
}

// ghSide is the sidebar: a section each for the reviewers, the assignees, the
// labels and the checks, with the gear that edits them where there is a
// command for it.
func (p *preview) ghSide(w int, faint lipgloss.Style) []ghLine {
	d := p.gh

	var rows []ghLine

	section := func(title string, edit func(*Model) tea.Cmd) {
		if len(rows) > 0 {
			rows = append(rows, ghLine{mdText: mdt(strings.Repeat("─", w), fg(pal.rulerBorder))})
		}

		r := ghLine{mdText: mdt(title, bold)}

		if edit != nil {
			gear := icOptions.s()
			r.mdText = r.add(mdt(blank(max(w-ansi.StringWidth(title)-ansi.StringWidth(gear), 1)), plain)).add(mdt(gear, faint))
			r.hits = []ghHit{{x0: ansi.StringWidth(title), x1: w, run: edit}}
		}

		rows = append(rows, r)
	}
	none := func() { rows = append(rows, ghLine{mdText: mdt("None yet", faint)}) }

	section("Reviewers", p.ghActed("Edit Reviewers…"))

	if rs := d.reviewers(); len(rs) > 0 {
		for _, r := range rs {
			rows = append(rows, ghLine{mdText: mdt(r.mark+" ", faint).add(mdt(r.login, plain))})
		}
	} else {
		none()
	}

	section("Assignees", p.ghActed("Edit Assignees…"))

	if as := d.people(); len(as) > 0 {
		for _, a := range as {
			rows = append(rows, ghLine{mdText: mdt(a, plain)})
		}
	} else {
		none()
	}

	section("Labels", p.ghActed("Edit Labels…"))

	if ls := d.labels(); len(ls) > 0 {
		rows = append(rows, ghLine{mdText: mdt(strings.Join(ls, "  "), plain)})
	} else {
		none()
	}

	if len(d.Rollup) > 0 {
		section("Checks", nil)

		if g, ok := d.checksSeg(); ok {
			rows = append(rows, ghLine{mdText: mdt(g.s, g.st)})
		}

		for _, c := range d.Rollup {
			mark := map[string]string{"pass": "✓", "fail": "✗", "pending": "○"}[git.Item{Rollup: []git.Check{c}}.Checks()]
			rows = append(rows, ghLine{mdText: mdt(mark+" ", faint).add(mdt(ansi.Truncate(cmp.Or(c.Name, c.Context), w-2, "…"), plain))})
		}
	}

	return rows
}

// ghConversation is the description and the timeline under it, w cells wide.
func (p *preview) ghConversation(m *Model, n string, w int, faint lipgloss.Style) []ghLine {
	d := p.gh
	rail := fg(pal.rulerBorder)

	var rows []ghLine

	gap := func() { rows = append(rows, ghLine{mdText: mdt(" │", rail)}) }

	body := d.Body
	if strings.TrimSpace(body) == "" {
		body = "_No description provided._"
	}

	entries := append([]ghEntry{{kind: "comment", id: "body", author: d.Author.Login, when: d.CreatedAt, text: body, url: d.URL}}, d.Timeline...)

	prev := ""

	for _, e := range entries {
		if prev != "" && (e.kind != "commit" || prev != "commit") {
			gap()
		}

		prev = e.kind

		switch e.kind {
		case "commit":
			rows = append(rows, p.ghCommit(e, n, w, faint))
		case "event":
			r := mdt(" ", plain).add(mdt("·", faint)).add(mdt(" "+e.author, bold)).add(mdt(" "+e.text, plain)).add(mdt("  "+ghAgo(e.when), faint))
			rows = append(rows, ghLine{mdText: r}.padTo(w))

		case "review":
			rows = append(rows, p.ghReview(m, e, w, faint)...)
		default:
			rows = append(rows, p.ghBox(m, e, w, 0, faint)...)
		}
	}

	if d.TimelineErr != nil {
		gap()

		rows = append(rows, ghLine{mdText: mdt(" conversation: "+ansi.Strip(d.TimelineErr.Error()), faint)})
	}

	gap()

	return append(rows, p.ghComposeRows(m, w, faint)...)
}

// ghComposeRows is the box at the end of the page, as GitHub's: the text area
// in a frame headed Leave a comment, and under it the buttons, Close or Reopen
// (with the comment, when there is one) and Comment. A click anywhere in it
// puts the cursor there.
func (p *preview) ghComposeRows(m *Model, w int, faint lipgloss.Style) []ghLine {
	d := p.gh
	kind, n, _ := strings.Cut(p.rev, "/")
	bw, inner := w-3, w-7
	border := fg(pal.sashHover)

	if p.composing || p.hot == "compose" { // has the keyboard, or a click would give it
		border = fg(pal.headerAccent)
	}

	ta := p.composer(m)
	ta.SetWidth(max(inner, 1))

	side := mdt(" ", plain).add(mdt("│", fg(pal.rulerBorder))).add(mdt(" ", plain))

	focus := ghHit{x0: 0, x1: w, run: func(m *Model) tea.Cmd { return m.pv.ghCompose(m, "") }}
	if !p.composing {
		focus.key = "compose"
	}

	title := " Leave a comment "
	top := mdt(" ", plain).add(mdt("●", border)).add(mdt("─", border)).add(mdt("╭─", border)).add(mdt(title, faint)).add(mdt(strings.Repeat("─", max(bw-2-ansi.StringWidth(title)-1, 1))+"╮", border))
	rows := []ghLine{{mdText: top, hits: []ghHit{focus}}}

	for _, l := range strings.Split(strings.TrimRight(ta.View(), "\n"), "\n") {
		line := mdText{l, ansi.Strip(l)}
		if p.composing {
			line.s = realCaret(l)
		}

		pad := blank(max(inner-ansi.StringWidth(line.p), 0))
		rows = append(rows, ghLine{mdText: side.add(mdt("│ ", border)).add(line).add(mdText{pad, pad}).add(mdt(" │", border)), hits: []ghHit{focus}})
	}

	// Buttons, right-aligned: the command of the state, then the comment.
	text := strings.TrimSpace(ta.Value()) != ""

	type button struct {
		label string
		st    lipgloss.Style
		run   func(*Model) tea.Cmd
	}

	var buttons []button

	if verb := map[string]string{"OPEN": "close", "CLOSED": "reopen"}[d.State]; verb != "" {
		what := map[string]string{"pr": "pull request", "issue": "issue"}[kind]
		label := strings.ToUpper(verb[:1]) + verb[1:] + " " + what
		question := fmt.Sprintf("%s #%s?", label, n)

		if text {
			label += " with comment"
			question = fmt.Sprintf("%s #%s and send your comment?", strings.TrimSuffix(label, " with comment"), n)
		}

		buttons = append(buttons, button{label, keycapHot(), ghConfirm(question, label, func(m *Model) tea.Cmd { return m.pv.ghSend(m, verb) })})
	}

	buttons = append(buttons, button{"Comment", lipgloss.NewStyle().Background(pal.buttonBg).Foreground(pal.buttonFg), func(m *Model) tea.Cmd { return m.pv.ghSend(m, "") }})

	var (
		row  mdText
		offs []int
	)

	for i, b := range buttons {
		if i > 0 {
			row = row.add(mdt(" ", plain))
		}

		cell := " " + b.label + " "

		offs = append(offs, ansi.StringWidth(row.p))
		row = row.add(mdText{b.st.Render(cell), cell})
	}

	total := ansi.StringWidth(row.p)
	hits := []ghHit{focus}

	for i, b := range buttons {
		x := 5 + max(inner-total, 0) + offs[i]
		hits = append(hits, ghHit{x0: x, x1: x + ansi.StringWidth(b.label) + 2, run: b.run})
	}

	gap := blank(max(inner-total, 0))

	return append(rows,
		ghLine{mdText: side.add(mdt("│ ", border)).add(mdText{gap, gap}).add(row).add(mdt(" │", border)), hits: hits},
		ghLine{mdText: side.add(mdt("╰"+strings.Repeat("─", bw-2)+"╯", border))})
}

// ghCommit is a commit as a row on the rail: its headline, its checks, whether
// it is verified and its hash; a click shows the commit.
func (p *preview) ghCommit(e ghEntry, n string, w int, faint lipgloss.Style) ghLine {
	var right mdText

	if mark := map[string]string{"pass": "✓", "fail": "✗", "pending": "○"}[e.state]; mark != "" {
		c := map[string]byte{"pass": 'A', "fail": 'D', "pending": 'M'}[e.state]
		right = right.add(mdt(mark+" ", fg(statusColor(c))))
	}

	if e.verified {
		right = right.add(mdt("Verified ", fg(pal.added)))
	}

	right = right.add(mdt(e.oid[:min(len(e.oid), 7)], faint))

	room := max(w-3-ansi.StringWidth(right.p)-2, 4)
	title := ansi.Truncate(e.text, room, "…")
	left := mdt(" ", plain).add(mdt("◦", faint)).add(mdt(" "+title, plain))
	r := ghLine{mdText: left.add(mdt(blank(w-ansi.StringWidth(left.p)-ansi.StringWidth(right.p)), plain)).add(right)}

	dir, oid := p.root, e.oid
	r.hits = []ghHit{{x0: 0, x1: w, run: func(*Model) tea.Cmd { return ghShow(dir, n, oid) }, key: "commit " + oid, lit1: w}}

	return r.padTo(w)
}

// ghReview is a review as its row on the rail, with the box of what it said
// and a box for each thread it started on the code.
func (p *preview) ghReview(m *Model, e ghEntry, w int, faint lipgloss.Style) []ghLine {
	node, sentence, st := "●", "reviewed", plain

	switch e.state {
	case "APPROVED":
		node, sentence, st = "✓", "approved these changes", fg(pal.added)
	case "CHANGES_REQUESTED":
		node, sentence, st = "✗", "requested changes", fg(pal.deleted)
	case "DISMISSED":
		sentence = "had their review dismissed"
	}

	r := mdt(" ", plain).add(mdt(node, st)).add(mdt(" "+e.author, bold)).add(mdt(" "+sentence, plain)).add(mdt("  "+ghAgo(e.when), faint))
	rows := []ghLine{(ghLine{mdText: r}).padTo(w)}

	if strings.TrimSpace(e.text) != "" {
		rows = append(rows, p.ghBox(m, e, w, 1, faint)...)
	}

	for _, t := range e.threads {
		rows = append(rows, p.ghThread(m, t, w, faint)...)
	}

	return rows
}

// ghBox is a comment: a frame with its author and time across the top and ⋯
// at the end, the Markdown inside. A click on the top folds it to the top;
// on the ⋯ lists what can be done with it. hang is 1 for a box that hangs off
// a row above it, without a node of its own.
func (p *preview) ghBox(m *Model, e ghEntry, w, hang int, faint lipgloss.Style) []ghLine {
	bw := w - 3
	border := fg(pal.sashHover)
	box, more := "box "+e.id, "menu "+e.id

	mention := ghMentions(e.text, m.ghUser) // every box has the one frame, but the ones that tag the reader
	if mention {
		border = fg(pal.warn)
	}

	if p.hot == box || p.hot == more { // the pointer on its top, where a click folds it
		border = fg(pal.headerAccent)
	}

	node := mdt(" ", plain).add(mdt("●", border)).add(mdt("─", border))
	if hang > 0 {
		node = mdt(" ", plain).add(mdt("│", fg(pal.rulerBorder))).add(mdt(" ", plain))
	}

	side := mdt(" ", plain).add(mdt("│", fg(pal.rulerBorder))).add(mdt(" ", plain))

	said := strings.TrimRight(" commented "+ghAgo(e.when), " ")
	if hang > 0 {
		said = " left a comment"
	}

	if e.edited {
		said += " · edited"
	}

	if mention {
		said += " · mentions you"
	}

	folded := p.folded[e.id]

	title := mdt(e.author, bold).add(mdt(said, faint))
	if folded {
		title = mdt("▸ ", faint).add(title)
	}

	fixed := 3 + 1 + 1 + 1 + 3 // ╭─ , a blank, a blank, ⋯, and a blank, ─╮
	title.s, title.p = ansi.Truncate(title.s, max(bw-fixed-1, 1), "…"), ansi.Truncate(title.p, max(bw-fixed-1, 1), "…")
	fill := max(bw-fixed-ansi.StringWidth(title.p), 1)

	dots := mdt(" ⋯ ", faint)
	if p.hot == more {
		dots = mdt(" ⋯ ", keycapHot())
	}

	top := node.add(mdt("╭─ ", border)).add(title).add(mdt(" "+strings.Repeat("─", fill), border)).add(dots).add(mdt("─╮", border))
	menuX := 3 + 3 + ansi.StringWidth(title.p) + 1 + fill + 1

	id, quote, url := e.id, ghQuote(e.text), e.url
	toggle := func(m *Model) tea.Cmd { //nolint:unparam // a command's run returns what is to be done next; folding has none
		pv := &m.pv
		if pv.folded == nil {
			pv.folded = map[string]bool{}
		}

		pv.folded[id] = !pv.folded[id]
		pv.mdLines, pv.mdShown = nil, 0
		pv.syncMarkdown(m)

		return nil
	}
	menu := func(m *Model) tea.Cmd {
		items := []item{
			{label: "Quote Reply", run: func(m *Model) tea.Cmd { return m.pv.ghCompose(m, quote) }},
			{label: map[bool]string{true: "Unfold", false: "Fold"}[folded], run: toggle},
		}
		if url != "" {
			items = append(items,
				item{label: "Open on GitHub", run: func(*Model) tea.Cmd { return openURL(url) }},
				item{label: "Copy Link", run: func(*Model) tea.Cmd { return setClipboard(url, "copied "+url) }})
		}

		return m.menuOf(items, m.mouseX, m.mouseY+1)
	}

	rows := []ghLine{{mdText: top, hits: []ghHit{{x0: 0, x1: w, run: toggle, key: box}, {x0: menuX - 1, x1: menuX + 2, run: menu, key: more}}}}

	if !folded {
		styled, plainLines := p.ghRender(e.text, bw-4, m.dark)
		for len(plainLines) > 0 && strings.TrimSpace(plainLines[0]) == "" {
			styled, plainLines = styled[1:], plainLines[1:]
		}

		for len(plainLines) > 0 && strings.TrimSpace(plainLines[len(plainLines)-1]) == "" {
			styled, plainLines = styled[:len(styled)-1], plainLines[:len(plainLines)-1]
		}

		for i := range plainLines {
			line := mdText{styled[i], plainLines[i]}
			line = line.add(mdText{blank(bw - 4 - ansi.StringWidth(plainLines[i])), blank(bw - 4 - ansi.StringWidth(plainLines[i]))})
			rows = append(rows, ghLine{mdText: side.add(mdt("│ ", border)).add(line).add(mdt(" │", border))})
		}
	}

	bottom := side.add(mdt("╰"+strings.Repeat("─", bw-2)+"╯", border))

	return append(rows, ghLine{mdText: bottom})
}

// ghThread is a conversation on a line of a file, a box of its own under its
// review: the file and line across the top, each comment inside.
func (p *preview) ghThread(m *Model, t ghThread, w int, faint lipgloss.Style) []ghLine {
	bw := w - 3
	border := fg(pal.sashHover)

	for _, c := range t.comments {
		if ghMentions(c.text, m.ghUser) {
			border = fg(pal.warn)
		}
	}

	side := mdt(" ", plain).add(mdt("│", fg(pal.rulerBorder))).add(mdt(" ", plain))

	title := ansi.Truncate(fmt.Sprintf("%s:%d", t.path, t.line), max(bw-6, 1), "…")
	rows := []ghLine{{mdText: side.add(mdt("╭─ ", border)).add(mdt(title, faint)).add(mdt(" "+strings.Repeat("─", max(bw-5-ansi.StringWidth(title), 1))+"╮", border))}}

	body := func(line mdText) {
		pad := mdText{blank(bw - 4 - ansi.StringWidth(line.p)), blank(bw - 4 - ansi.StringWidth(line.p))}
		rows = append(rows, ghLine{mdText: side.add(mdt("│ ", border)).add(line).add(pad).add(mdt(" │", border))})
	}

	for i, c := range t.comments {
		if i > 0 {
			body(mdt(strings.Repeat("─", bw-4), border))
		}

		body(mdt(c.author, bold).add(mdt(" "+ghAgo(c.when), faint)))

		styled, plainLines := p.ghRender(c.text, bw-4, m.dark)
		for j := range plainLines {
			if j == 0 && strings.TrimSpace(plainLines[j]) == "" {
				continue
			}

			body(mdText{styled[j], plainLines[j]})
		}
	}

	return append(rows, ghLine{mdText: side.add(mdt("╰"+strings.Repeat("─", bw-2)+"╯", border))})
}

// ghMentions reports whether text tags login: @login, whole, as GitHub reads
// it. A reader who is not known (gh not signed in) is tagged by nothing.
func ghMentions(text, login string) bool {
	if login == "" {
		return false
	}

	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])@` + regexp.QuoteMeta(login) + `($|[^A-Za-z0-9-])`)

	return re.MatchString(text)
}

// ghBodyKey is a Markdown body laid out at a width, in a theme.
type ghBodyKey struct {
	text string
	w    int
	dark bool
}

// ghRender is renderMarkdown for the page's bodies, kept until the page is
// fetched: a keystroke in the comment box lays the page out again, and the
// bodies have not changed.
func (p *preview) ghRender(text string, w int, dark bool) (styled, plain []string) {
	k := ghBodyKey{text, w, dark}
	if b, ok := p.bodies[k]; ok {
		return b[0], b[1]
	}

	styled, plain = renderMarkdown(text, w, dark)

	if p.bodies == nil {
		p.bodies = map[ghBodyKey][2][]string{}
	}

	p.bodies[k] = [2][]string{styled, plain}

	return styled, plain
}

// ghShownMsg is a commit of the page, fetched if it was not here: show it.
type ghShownMsg struct {
	root, oid string
	err       error
}

// ghShow shows commit oid of pull request n, fetching the pull request's
// head first when the commit is not in the repository yet.
func ghShow(dir, n, oid string) tea.Cmd {
	return func() tea.Msg {
		if _, err := git.Run(dir, "cat-file", "-e", oid+"^{commit}"); err != nil {
			num := 0
			if _, scanErr := fmt.Sscanf(n, "%d", &num); scanErr != nil || num == 0 {
				return ghShownMsg{err: err}
			}

			if _, err := git.FetchPR(dir, num); err != nil {
				return ghShownMsg{err: err}
			}
		}

		return ghShownMsg{root: dir, oid: oid}
	}
}
