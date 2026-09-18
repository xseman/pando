package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}

type seg struct {
	s     string
	st    lipgloss.Style
	ownBg bool // keep the segment's background under a row highlight (badges)
}

func sg(s string, st lipgloss.Style) seg    { return seg{s: s, st: st} }
func sgOwn(s string, st lipgloss.Style) seg { return seg{s: s, st: st, ownBg: true} }

// row paints left segments and right-aligned segments into exactly w cells.
// A non-nil bg is applied to every segment so inner styles don't break it.
func row(w int, bg color.Color, left []seg, right ...seg) string {
	rw := 0
	for _, r := range right {
		rw += ansi.StringWidth(r.s)
	}

	if rw > w {
		right, rw = nil, 0
	}

	var b strings.Builder

	paint := func(x seg) {
		if bg != nil && !x.ownBg {
			x.st = x.st.Background(bg)
		}

		b.WriteString(x.st.Render(x.s))
	}

	used := 0
	for _, x := range left {
		rem := w - rw - used
		if rem <= 0 {
			break
		}

		if ansi.StringWidth(x.s) > rem {
			x.s = ansi.Truncate(x.s, rem, "…")
		}

		paint(x)
		used += ansi.StringWidth(x.s)
	}

	if gap := w - rw - used; gap > 0 {
		paint(sg(strings.Repeat(" ", gap), plain))
	}

	for _, x := range right {
		paint(x)
	}

	return b.String()
}

// fit truncates or pads a pre-styled line to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}

	sw := ansi.StringWidth(s)
	if sw > w {
		s = ansi.Truncate(s, w, "")
		sw = ansi.StringWidth(s)
	}

	return s + "\x1b[m" + strings.Repeat(" ", w-sw)
}

func blank(w int) string { return strings.Repeat(" ", max(w, 0)) }

// center places s in the middle of w cells.
func center(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return ansi.Truncate(s, max(w, 0), "")
	}

	l := (w - sw) / 2

	return blank(l) + s + blank(w-sw-l)
}

func chevron(open bool) string {
	if open {
		return "▾ "
	}

	return "▸ "
}

// list is a windowed selection: the wheel scrolls the view only, keys move
// the selection and snap the view to it. sel -1 = nothing selected yet.
type list struct{ sel, top int }

func (l *list) clamp(n, h int) {
	if l.sel >= n {
		l.sel = n - 1
	}

	l.top = max(min(l.top, n-h), 0)
}

func (l *list) move(d, n, h int) {
	if n == 0 {
		return
	}

	if l.sel < 0 {
		l.sel = 0
	} else {
		l.sel = max(0, min(n-1, l.sel+d))
	}

	l.snap(h)
}

func (l *list) snap(h int) {
	if l.sel < l.top {
		l.top = l.sel
	}

	if h > 0 && l.sel >= l.top+h {
		l.top = l.sel - h + 1
	}
}

func (l *list) wheel(d, n, h int) { l.top = max(0, min(l.top+d, n-h)) }

func (l *list) at(y, n int) int {
	if i := l.top + y; y >= 0 && i < n {
		return i
	}

	return -1
}

// render draws rows top..top+h with a right-edge scrollbar on overflow.
func (l *list) render(w, h, n int, rowFn func(i, w int) string) []string {
	return l.renderBar(w, h, n, 0, rowFn)
}

// renderBar is render with the scrollbar kept off the first skip lines, rows
// that stay put while the rest scrolls; those get the bar's column too.
func (l *list) renderBar(w, h, n, skip int, rowFn func(i, w int) string) []string {
	out := make([]string, max(h, 0))
	bar := n > h && w > 1
	rw := w
	thumb, thumbH := 0, 0

	if bar {
		rw = w - 1
		bh, bn := h-skip, n-skip
		thumbH = max(1, bh*bh/bn)
		thumb = skip + l.top*(bh-thumbH)/max(bn-bh, 1)
	}

	for y := range out {
		lw := rw
		if y < skip {
			lw = w
		}

		line := blank(lw)
		if i := l.top + y; i < n {
			line = rowFn(i, lw)
		}

		if bar && y >= skip {
			if y >= thumb && y < thumb+thumbH {
				line += dim.Render("┃")
			} else {
				line += " "
			}
		}

		out[y] = line
	}

	return out
}

// ptree groups slash-separated relative paths by directory.
type ptree struct {
	dirs  map[string]*ptree
	files []string // full relative paths of the files directly in this directory
}

func buildTree(paths []string) *ptree {
	root := &ptree{}
	for _, p := range paths {
		t := root

		parts := strings.Split(p, "/")
		for _, d := range parts[:len(parts)-1] {
			if t.dirs == nil {
				t.dirs = map[string]*ptree{}
			}

			if t.dirs[d] == nil {
				t.dirs[d] = &ptree{}
			}

			t = t.dirs[d]
		}

		t.files = append(t.files, p)
	}

	return root
}

// byLowerBase orders paths by base name, case-insensitively.
func byLowerBase(a, b string) int {
	return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
}

func (t *ptree) dirNames() []string {
	names := slices.Collect(maps.Keys(t.dirs))
	slices.SortFunc(names, byLowerBase)

	return names
}

// sortedFiles orders the directory's files by base name.
func (t *ptree) sortedFiles() []string {
	files := slices.Clone(t.files)
	slices.SortFunc(files, byLowerBase)

	return files
}

// filter is a view's Ctrl+F query line, shown under the view header.
type filter struct {
	input   textinput.Model
	on      bool
	editing bool
}

func newFilter() filter {
	f := filter{input: textinput.New()}
	f.input.Prompt = " / "
	f.input.Placeholder = "filter · esc clears"

	return f
}

// item is one modal entry; run is nil for informational rows.
type item struct {
	label  string
	hint   string
	run    func(m *Model) tea.Cmd
	styled bool   // label carries its own styles (keycaps, headings)
	inline string // dimmed text right after the label
	detail string // a dimmed second row under the label
	search string // what the filter matches instead of the label
	always bool   // stays on top whatever the filter
	group  string // the heading a prefixed picker files it under after ":"
	cont   bool   // the detail row of the item above, made by refilter
}

// cancelItem is the last entry of a menu or a confirmation: it closes the
// overlay and does nothing else.
func cancelItem() item {
	return item{label: "Cancel", run: func(*Model) tea.Cmd { return nil }}
}

// matchItem scores an item for a picker query: words starting with @ or !
// must appear verbatim in its search text, the rest match fuzzily.
func matchItem(q string, it item) int {
	if it.search == "" {
		return fuzzy(q, it.label)
	}

	lower, rest := strings.ToLower(it.search), []string{}

	for _, w := range strings.Fields(q) {
		if len(w) > 1 && (w[0] == '@' || w[0] == '!') {
			if !strings.Contains(lower, strings.ToLower(w)) {
				return -1
			}

			continue
		}

		rest = append(rest, w)
	}

	return fuzzy(strings.Join(rest, " "), it.search)
}

// modal is the single overlay: a menu (items), a filterable picker
// (items + filter), or a prompt (input + submit).
type modal struct {
	title     string
	items     []item
	build     func(m *Model) []item // rebuilds items after run when keep is set
	disp      []item                // rows as shown: filtered, ranked, grouped in tree mode
	input     textinput.Model
	filter    bool
	lineQuery bool // a trailing ":12:5" is a position, not part of the match
	submit    func(m *Model, value string) tea.Cmd
	keep      bool
	tree      bool // group picker results under their directory
	// prefix starts every query of a picker that lists one kind of thing, "@"
	// for symbols; "@:" after it groups the items under their group, as VS
	// Code's quick access does.
	prefix   string
	change   func(m *Model, v string) tea.Cmd // after the query changed, before refiltering
	move     func(m *Model)                   // after the selection moved
	cancel   func(m *Model)                   // closed without choosing
	complete func(v string) []string          // a prompt's suggestions for what is typed: the rest shows dimmed, tab or → takes it
	treeable bool                             // offer the list/tree toggle
	l        list
	x, y     int // anchor; x < 0 centers
}

// inputStyles paints a text input on the input box background.
func inputStyles(dark bool) textinput.Styles {
	st := textinput.DefaultStyles(dark)
	for _, ss := range []*textinput.StyleState{&st.Focused, &st.Blurred} {
		ss.Text, ss.Placeholder, ss.Prompt = ss.Text.Background(pal.inputBg), ss.Placeholder.Background(pal.inputBg), ss.Prompt.Background(pal.inputBg)
	}

	return st
}

func newMenu(title string, x, y int, items ...item) *modal {
	md := &modal{title: title, items: items, x: x, y: y}
	md.refilter()

	return md
}

func newPicker(title string, items []item) *modal {
	md := &modal{title: title, items: items, filter: true, x: -1}
	md.input = textinput.New()
	md.input.Prompt = "› "
	md.input.Focus()
	md.refilter()

	return md
}

func newPrompt(title, value string, submit func(m *Model, v string) tea.Cmd) *modal {
	md := &modal{title: title, submit: submit, x: -1}
	md.input = textinput.New()
	md.input.Prompt = "› "
	md.input.SetValue(value)
	md.input.CursorEnd()
	md.input.Focus()

	return md
}

func (md *modal) refilter() {
	q := ""
	if md.filter {
		q = md.input.Value()
	}

	if md.lineQuery {
		q, _, _ = strings.Cut(q, ":")
	}

	grouped := false

	if md.prefix != "" {
		q = strings.TrimPrefix(q, md.prefix)
		q, grouped = strings.CutPrefix(q, ":")
		q = strings.TrimSpace(q)
	}

	type hit struct{ i, score int }

	var hits []hit

	for i, it := range md.items {
		if s := matchItem(q, it); s >= 0 && !it.always {
			hits = append(hits, hit{i, s})
		}
	}

	switch {
	case q != "":
		slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.score, a.score) })
	case md.tree: // browsing: alphabetical, like a file tree
		slices.SortStableFunc(hits, func(a, b hit) int {
			return strings.Compare(strings.ToLower(md.items[a.i].label), strings.ToLower(md.items[b.i].label))
		})
	}

	md.disp = md.disp[:0]
	for _, it := range md.items {
		if it.always {
			md.disp = append(md.disp, it)
		}
	}

	switch {
	case md.prefix != "":
		if grouped {
			slices.SortStableFunc(hits, func(a, b hit) int { return strings.Compare(md.items[a.i].group, md.items[b.i].group) })
		}

		count := map[string]int{}
		for _, h := range hits {
			count[md.items[h.i].group]++
		}

		for i, h := range hits {
			it := md.items[h.i]
			switch {
			case grouped && (i == 0 || md.items[hits[i-1].i].group != it.group):
				md.disp = append(md.disp, heading(fmt.Sprintf("%s (%d)", it.group, count[it.group])))
			case !grouped && i == 0:
				md.disp = append(md.disp, heading(fmt.Sprintf("symbols (%d)", len(hits))))
			}

			md.disp = append(md.disp, it)
		}

	case md.tree:
		groups := map[string][]item{}

		var order []string // directories by their best match

		for _, h := range hits {
			it := md.items[h.i]

			dir := filepath.Dir(it.label)
			if _, ok := groups[dir]; !ok {
				order = append(order, dir)
			}

			groups[dir] = append(groups[dir], item{label: "  " + filepath.Base(it.label), hint: it.hint, run: it.run})
		}

		for _, dir := range order {
			md.disp = append(md.disp, item{label: dir + "/"})
			md.disp = append(md.disp, groups[dir]...)
		}

	default:
		group := ""

		for _, h := range hits {
			it := md.items[h.i]
			// Browsing shows the group headings; a query ranks across them.
			if q == "" && it.group != group {
				if group = it.group; group != "" {
					if len(md.disp) > 0 {
						md.disp = append(md.disp, item{}) // breathing room between groups
					}

					md.disp = append(md.disp, heading(group))
				}
			}

			md.disp = append(md.disp, it)
			if it.detail != "" {
				md.disp = append(md.disp, item{label: it.detail, cont: true})
			}
		}
	}

	md.l = list{sel: -1}
	md.step(1, 0)

	if md.l.sel < 0 && len(md.disp) > 0 {
		md.l.sel, md.l.top = 0, 0 // informational menus still scroll from the top; step scrolled past them
	}
}

// step moves to the next actionable row in direction d, scrolling when none is left.
func (md *modal) step(d, rows int) {
	for i := md.l.sel + d; i >= 0 && i < len(md.disp); i += d {
		if md.disp[i].run != nil {
			md.l.sel = i
			md.l.snap(rows)

			if i+1 < len(md.disp) && md.disp[i+1].cont && i+1 >= md.l.top+rows {
				md.l.top++ // keep the item's detail row in view
			}

			return
		}
	}

	md.l.wheel(d, len(md.disp), rows)
}

func (md *modal) hasInput() bool { return md.filter || md.submit != nil }

// rect returns the box position and size, and how many item rows fit.
func (md *modal) rect(m *Model) (x, y, w, h, rows int) {
	w = ansi.StringWidth(md.title) + 6
	for _, it := range md.items {
		w = max(w, ansi.StringWidth(it.label)+ansi.StringWidth(it.inline)+ansi.StringWidth(it.hint)+6)
	}

	if md.hasInput() {
		w = max(w, 60)
		if md.x >= 0 { // a box opened at a place in the text stays out of the way
			w = max(min(w, 30), ansi.StringWidth(md.input.Value())+8)
		}
	}

	w = min(w, m.w-2)
	maxRows := max(m.h-6, 1)

	rows = min(len(md.disp), maxRows)
	if md.filter {
		rows = max(rows, min(8, maxRows)) // a picker keeps a minimum height
	}

	if md.submit != nil {
		rows = 0
	}

	h = rows + 2
	if md.hasInput() {
		h++
	}

	if md.x < 0 {
		// A picker's top edge stays put while results change: it is placed as if full.
		top := h
		if md.filter {
			top = maxRows + 3
		}

		x, y = (m.w-w)/2, max((m.h-top)/3, 0)
	} else {
		x, y = min(md.x, m.w-w), min(md.y, m.h-h)
	}

	return max(x, 0), max(y, 0), w, h, rows
}

// toggle is the list/tree switch drawn at the right end of the title row.
func (md *modal) toggle() (label string, width int) {
	if !md.treeable {
		return "", 0
	}

	label = "[tree]"
	if md.tree {
		label = "[list]"
	}

	return label, ansi.StringWidth(" ^t "+label) + 1
}

func (md *modal) view(m *Model) (string, int, int) {
	x, y, w, _, rows := md.rect(m)
	iw := w - 2

	title := ""
	if md.title != "" {
		title = " " + md.title + " "
	}

	label, tw := md.toggle()
	title = ansi.Truncate(title, max(iw-tw, 0), "…")

	top := dim.Render("╭") + bold.Render(title) + dim.Render(strings.Repeat("─", max(iw-tw-ansi.StringWidth(title), 0)))
	if tw > 0 {
		top += dim.Render(" ^t ") + fg(pal.headerAccent).Render(label) + " "
	}

	lines := []string{top + dim.Render("╮")}
	side := dim.Render("│")

	if md.hasInput() {
		md.input.SetWidth(iw - 3)
		lines = append(lines, side+fit(md.input.View(), iw)+side)
	}

	for _, r := range md.l.render(iw, rows, len(md.disp), func(i, rw int) string {
		it := md.disp[i]
		if it.cont {
			var bg color.Color
			if i-1 == md.l.sel {
				bg = pal.selBg
			}

			return row(rw, bg, []seg{sg("   "+it.label, dim)})
		}

		if it.styled {
			return fit(" "+it.label, rw)
		}

		st := plain
		if it.run == nil {
			st = dim
		}

		var bg color.Color
		if i == md.l.sel && it.run != nil {
			bg = pal.selBg
			if pal.selFg != nil {
				st = st.Foreground(pal.selFg)
			}
		}

		left := []seg{sg(" "+it.label, st)}
		if it.inline != "" {
			left = append(left, sg(" "+it.inline, dim))
		}

		return row(rw, bg, left, sg(it.hint+" ", dim))
	}) {
		lines = append(lines, side+r+side)
	}

	lines = append(lines, dim.Render("╰"+strings.Repeat("─", iw)+"╯"))

	return strings.Join(lines, "\n"), x, y
}

func (md *modal) toggleTree(m *Model) tea.Cmd {
	md.tree = !md.tree
	md.refilter()

	return m.setSettings(map[string]any{"quick_open_tree": md.tree})
}

func (md *modal) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	_, _, _, _, rows := md.rect(m)
	if md.complete != nil {
		switch s := k.String(); {
		case s == "tab" || s == "right" && md.input.Position() == len([]rune(md.input.Value())):
			if sug := md.input.CurrentSuggestion(); sug != "" && sug != md.input.Value() {
				md.input.SetValue(sug)
				md.input.CursorEnd()
				md.input.SetSuggestions(md.complete(sug))

				return nil
			}

			if s == "tab" {
				return nil
			}

		case s == "up" || s == "down": // through the other suggestions
			var cmd tea.Cmd

			md.input, cmd = md.input.Update(k)

			return cmd
		}
	}

	switch k.String() {
	case "esc", "ctrl+c":
		md.dismiss(m)
		return nil

	case "up", "ctrl+p", "shift+tab":
		md.step(-1, rows)
		md.moved(m)

		return nil

	case "down", "ctrl+n", "tab":
		md.step(1, rows)
		md.moved(m)

		return nil

	case "ctrl+t":
		if md.treeable {
			return md.toggleTree(m)
		}

	case "enter":
		if md.submit != nil {
			m.modal = nil
			return md.submit(m, strings.TrimSpace(md.input.Value()))
		}

		return md.choose(m, md.l.sel)
	}

	if !md.hasInput() {
		switch k.String() {
		case "k":
			md.step(-1, rows)
		case "j":
			md.step(1, rows)
		case "q":
			m.modal = nil
		}

		return nil
	}

	var cmd tea.Cmd

	before := md.input.Value()

	md.input, cmd = md.input.Update(k)
	if v := md.input.Value(); md.complete != nil && v != before {
		md.input.SetSuggestions(md.complete(v))
	}

	if v := md.input.Value(); md.filter && v != before {
		if md.change != nil {
			if c := md.change(m, v); c != nil || m.modal != md {
				return tea.Batch(cmd, c)
			}
		}

		md.refilter()
		md.moved(m)
	}

	return cmd
}

// dismiss closes the modal without choosing anything.
func (md *modal) dismiss(m *Model) {
	m.modal = nil
	if md.cancel != nil {
		md.cancel(m)
	}
}

func (md *modal) moved(m *Model) {
	if md.move != nil && md.l.sel >= 0 && md.l.sel < len(md.disp) && md.disp[md.l.sel].run != nil {
		md.move(m)
	}
}

// rowAt is the item at list row y; a detail row belongs to the item above.
func (md *modal) rowAt(y, rows int) int {
	i := md.l.at(y, len(md.disp))
	if i < 0 || y >= rows {
		return -1
	}

	if md.disp[i].cont && i > 0 {
		i--
	}

	return i
}

func (md *modal) choose(m *Model, i int) tea.Cmd {
	if i < 0 || i >= len(md.disp) {
		return nil
	}

	it := md.disp[i]
	if it.run == nil {
		return nil
	}

	if !md.keep {
		m.modal = nil
	}

	cmd := it.run(m)
	if md.keep && md.build != nil && m.modal == md {
		sel := md.l.sel
		md.items = md.build(m)
		md.refilter()
		md.l.sel = sel
	}

	return cmd
}

func (md *modal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	mo := msg.Mouse()
	x, y, w, h, rows := md.rect(m)
	inside := mo.X >= x && mo.X < x+w && mo.Y >= y && mo.Y < y+h

	top := y + 1
	if md.hasInput() {
		top++
	}

	switch msg.(type) {
	case tea.MouseMotionMsg: // the row under the mouse is the selection, like a GUI menu
		if i := md.rowAt(mo.Y-top, rows); inside && i >= 0 && md.disp[i].run != nil && i != md.l.sel {
			md.l.sel = i
			md.moved(m)
		}

	case tea.MouseWheelMsg:
		md.l.wheel(wheelDelta(mo), len(md.disp), rows)
	case tea.MouseClickMsg:
		if !inside {
			md.dismiss(m)
			return nil
		}

		if _, tw := md.toggle(); tw > 0 && mo.Y == y && mo.X >= x+w-1-tw && mo.X < x+w-1 {
			return md.toggleTree(m)
		}

		if i := md.rowAt(mo.Y-top, rows); i >= 0 {
			md.l.sel = i
			return md.choose(m, i)
		}
	}

	return nil
}

// fuzzy scores a case-insensitive subsequence match; -1 = no match.
func fuzzy(pattern, s string) int {
	if pattern == "" {
		return 0
	}

	p, ls := strings.ToLower(pattern), strings.ToLower(s)
	best := -1
	// ponytail: greedy match from every occurrence of the first rune, O(n·m); fine for paths.
	for start := strings.IndexByte(ls, p[0]); start >= 0; {
		score, pi, prev := 0, 0, -2
		for i := start; i < len(ls) && pi < len(p); i++ {
			if ls[i] != p[pi] {
				continue
			}

			score += 10
			if i == prev+1 {
				score += 15
			}

			if i == 0 || strings.IndexByte("/_-. ", ls[i-1]) >= 0 {
				score += 10
			}

			prev = i
			pi++
		}

		if pi < len(p) {
			break // later starts cannot match either
		}

		best = max(best, score*100-len(s))

		next := strings.IndexByte(ls[start+1:], p[0])
		if next < 0 {
			break
		}

		start += next + 1
	}

	return best
}

func openExternal(path string) error {
	for _, opener := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(opener); err == nil {
			return exec.Command(p, path).Start()
		}
	}

	return exec.ErrNotFound
}

// clicks detects double clicks on the same cell.
type clicks struct {
	x, y int
	at   time.Time
}

func (c *clicks) double(x, y int) bool {
	d := c.x == x && c.y == y && time.Since(c.at) < 400*time.Millisecond

	c.x, c.y, c.at = x, y, time.Now()
	if d {
		c.at = time.Time{}
	}

	return d
}
