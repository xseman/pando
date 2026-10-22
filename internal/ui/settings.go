package ui

import (
	"cmp"
	"image/color"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pvSettings is the Settings editor: a tab of its own, VS Code's settings
// editor, its sections as chips across the top and each setting a
// checkbox or a dropdown. A change applies at once, through the daemon.
const pvSettings = "settings"

// setting is one row of Settings: its section and group, its label (the
// palette's "Preferences: label" too), the values a dropdown offers (nil
// for a checkbox), its value as shown ("on" or "off" for a checkbox), and
// how a value is set.
type setting struct {
	section, group, label string
	opts                  []string
	get                   func(m *Model) string
	set                   func(m *Model, v string) tea.Cmd
}

// checkbox is a setting that is on or off, written as key.
func checkbox(section, group, label, key string, get func(s *Model) bool, then ...func(m *Model) tea.Cmd) setting {
	return setting{
		section: section, group: group, label: label,
		get: func(m *Model) string { return onOff(get(m)) },
		set: func(m *Model, v string) tea.Cmd {
			cmds := []tea.Cmd{m.setSettings(map[string]any{key: v == "on"})}
			for _, f := range then {
				cmds = append(cmds, f(m))
			}

			return tea.Batch(cmds...)
		},
	}
}

func onOff(b bool) string { return ifElse(b, "on", "off") }

// settingsTable is every setting Settings shows, in order.
func settingsTable() []setting {
	ints := func(ns []int, hint func(int) string) []string {
		out := make([]string, len(ns))
		for i, n := range ns {
			out[i] = hint(n)
		}

		return out
	}
	pick := func(ns []int, hint func(int) string, v string) int {
		return ns[max(slices.Index(ints(ns, hint), v), 0)]
	}
	screen := func(m *Model) tea.Cmd { return m.fetchScreen() }

	return []setting{
		{
			section: "Appearance", group: "Theme", label: "Color theme", opts: themeNames,
			get: func(m *Model) string { return cmp.Or(m.st.Settings.Theme, "vscode") },
			set: func(m *Model, v string) tea.Cmd { return m.setSettings(map[string]any{"color_theme": v}) },
		},
		{
			section: "Appearance", group: "Theme", label: "Icons", opts: iconSets,
			get: func(m *Model) string { return m.st.Settings.Icons },
			set: func(m *Model, v string) tea.Cmd { return m.setSettings(map[string]any{"icons": v}) },
		},
		{
			section: "Appearance", group: "Window", label: "Activity bar", opts: []string{"side", "top"},
			get: func(m *Model) string { return ifElse(m.sideBar(), "side", "top") },
			set: func(m *Model, v string) tea.Cmd {
				return tea.Batch(m.setSettings(map[string]any{"activity_bar": v}), m.fetchScreen())
			},
		},
		checkbox("Appearance", "Window", "Panel borders", "panel_borders", func(m *Model) bool { return m.st.Settings.Borders }, screen),
		checkbox("Appearance", "Window", "Tooltips", "tooltips", func(m *Model) bool { return m.st.Settings.Tips }),

		checkbox("Explorer", "Files", "Hidden files", "hidden", func(m *Model) bool { return m.st.Settings.Hidden }),
		checkbox("Explorer", "Files", "Git decorations", "git_deco", func(m *Model) bool { return m.st.Settings.GitDeco }, (*Model).refreshGit),
		{
			section: "Explorer", group: "Files", label: "Sort files by", opts: []string{"name", "type"},
			get: func(m *Model) string { return cmp.Or(m.st.Settings.ExSort, "name") },
			set: func(m *Model, v string) tea.Cmd { return m.setSettings(map[string]any{"explorer_sort": v}) },
		},
		checkbox("Explorer", "Files", "Auto reveal", "auto_reveal", func(m *Model) bool { return m.st.Settings.Reveal }),

		checkbox("Source Control", "Changes", "Git changes as tree", "git_tree", func(m *Model) bool { return m.st.Settings.GitTree }),
		{
			section: "Source Control", group: "Diffs", label: "Diff view", opts: []string{"inline", "split"},
			get: func(m *Model) string { return ifElse(m.st.Settings.DiffView == "split", "split", "inline") },
			set: func(m *Model, v string) tea.Cmd {
				if v == ifElse(m.st.Settings.DiffView == "split", "split", "inline") {
					return nil
				}

				return m.toggleDiffView()
			},
		},
		{
			section: "Source Control", group: "Diffs", label: "Diff whole file", get: func(m *Model) string { return onOff(m.st.Settings.DiffFull) },
			set: func(m *Model, v string) tea.Cmd {
				if (v == "on") == m.st.Settings.DiffFull {
					return nil
				}

				return m.toggleDiffFull()
			},
		},

		checkbox("Editor", "Code", "Format on save", "format_on_save", func(m *Model) bool { return m.st.Settings.FmtSave }),
		checkbox("Editor", "Code", "Diagnostics", "lsp_diagnostics", func(m *Model) bool { return m.st.Settings.Diags }),
		checkbox("Editor", "Code", "Vim mode", "vim_mode", func(m *Model) bool { return m.st.Settings.Vim }),
		checkbox("Editor", "View", "Breadcrumbs", "breadcrumbs", func(m *Model) bool { return m.st.Settings.Crumbs }),
		{
			section: "Editor", group: "View", label: "Word wrap", get: func(m *Model) string { return onOff(m.st.Settings.Wrap) },
			set: func(m *Model, v string) tea.Cmd {
				if (v == "on") == m.st.Settings.Wrap {
					return nil
				}

				return m.toggleWrap()
			},
		},
		{
			section: "Editor", group: "View", label: "Render whitespace", opts: blankModes,
			get: func(m *Model) string { return cmp.Or(m.st.Settings.Blanks, "none") },
			set: func(m *Model, v string) tea.Cmd { return m.setSettings(map[string]any{"render_whitespace": v}) },
		},
		{
			section: "Editor", group: "View", label: "Markdown width", opts: ints(mdWidths, mdWidthHint),
			get: func(m *Model) string { return mdWidthHint(m.st.Settings.MDWidth) },
			set: func(m *Model, v string) tea.Cmd {
				return m.setSettings(map[string]any{"markdown_width": pick(mdWidths, mdWidthHint, v)})
			},
		},
		{
			section: "Editor", group: "Tabs", label: "Editor limit", opts: ints(edLimits, edLimitHint),
			get: func(m *Model) string { return edLimitHint(m.st.Settings.EdLimit) },
			set: func(m *Model, v string) tea.Cmd {
				return m.setSettings(map[string]any{"editor_limit": pick(edLimits, edLimitHint, v)})
			},
		},

		checkbox("Sessions", "Claude", "Claude in background", "claude_background", func(m *Model) bool { return m.st.Settings.ClaudeBg }),
		{
			section: "Sessions", group: "Sleep", label: "Sleep after", opts: ints(sleepAfters, sleepHint),
			get: func(m *Model) string { return sleepHint(m.st.Settings.Sleep) },
			set: func(m *Model, v string) tea.Cmd {
				return m.setSettings(map[string]any{"sleep_after": pick(sleepAfters, sleepHint, v)})
			},
		},
	}
}

// sleepAfters are the minutes Settings cycles sleep_after through; 0 never.
var sleepAfters = []int{0, 30, 60, 120, 240, 480}

func sleepHint(n int) string {
	if n <= 0 {
		return "off"
	}

	return strconv.Itoa(n) + " min"
}

// next is the value after the setting's current one: on and off flip, a
// dropdown's go round.
func (s setting) next(m *Model) string {
	if s.opts == nil {
		return ifElse(s.get(m) == "on", "off", "on")
	}

	return s.opts[(slices.Index(s.opts, s.get(m))+1)%len(s.opts)]
}

// settingsItems are the settings for the palette and [keys], grouped under
// their sections' headings: running one moves it to its next value.
func settingsItems(m *Model) []item {
	var (
		items   []item
		section string
	)

	for _, s := range settingsTable() {
		if s.section != section {
			if section != "" {
				items = append(items, item{})
			}

			section = s.section
			items = append(items, heading(section))
		}

		items = append(items, item{label: s.label, hint: s.get(m), run: func(m *Model) tea.Cmd { return s.set(m, s.next(m)) }})
	}

	return items
}

// settingsPage is the Settings tab's place: its section and the row with
// the keyboard there.
type settingsPage struct {
	sec, at, top int
}

// settingsSections are the sections in the order the table has them.
func settingsSections() []string {
	var out []string

	for _, s := range settingsTable() {
		if !slices.Contains(out, s.section) {
			out = append(out, s.section)
		}
	}

	return out
}

// openSettings shows the Settings tab, the one open if there is one.
func (m *Model) openSettings() tea.Cmd {
	m.modal = nil
	if i := slices.IndexFunc(m.editors, func(e preview) bool { return e.kind == pvSettings }); i >= 0 {
		if i == m.edIdx && m.pv.kind == pvSettings {
			m.focus = onMain
			return nil
		}

		cmd := m.showEditor(i)
		m.focus = onMain

		return cmd
	}

	cmd := m.setPreview(preview{kind: pvSettings, name: "Settings", set: &settingsPage{}})
	m.focus = onMain

	return cmd
}

// settingsRow is one body row of the Settings tab: a group's heading or a
// setting (i into the table), -1 for neither.
type settingsRow struct {
	group string
	i     int
}

// rows are the section's body rows: each group's heading, then its settings,
// a blank between groups.
func (sp *settingsPage) rows() []settingsRow {
	var out []settingsRow

	section, group := settingsSections()[sp.sec], ""
	for i, s := range settingsTable() {
		if s.section != section {
			continue
		}

		if s.group != group {
			if group != "" {
				out = append(out, settingsRow{i: -1})
			}

			group = s.group
			out = append(out, settingsRow{group: group, i: -1})
		}

		out = append(out, settingsRow{i: i})
	}

	return out
}

// settings are the rows that are settings, as indexes into rows.
func (sp *settingsPage) settings() []int {
	var out []int

	for k, r := range sp.rows() {
		if r.i >= 0 {
			out = append(out, k)
		}
	}

	return out
}

// Layout of a Settings row: the label from settingsLabelX, the control from
// settingsCtrlX, a dropdown's ▼ settingsDropW past it.
const (
	settingsLabelX = 4
	settingsCtrlX  = 32
	settingsDropW  = 22
	settingsTopH   = 3 // the chips, a rule, a blank
)

// chips are the sections across the top and where each starts; hot is the
// one under the mouse, -1 none.
func (sp *settingsPage) chips(hot int) ([]seg, []int) {
	var (
		segs []seg
		xs   []int
	)

	at := 1

	for i, name := range settingsSections() {
		st := dim

		switch i {
		case sp.sec:
			st = bold
		case hot:
			st = plain
		}

		segs, xs = append(segs, sg(name, st), sg("  ", plain)), append(xs, at)
		at += ansi.StringWidth(name) + 2
	}

	return segs, xs
}

// hover is the tab's cell under the mouse; ok is false while it is
// elsewhere, or a modal is over it.
func (sp *settingsPage) hover(m *Model) (x, y int, ok bool) {
	x, y = m.mouseX-m.mainX(), m.mouseY-m.mainY()-m.headH()-m.stripH()

	return x, y, m.modal == nil && m.pv.kind == pvSettings && x >= 0 && x < m.mainW() && y >= 0 && y < m.pvH()
}

// chipAt is the section whose chip is at cell x of the top row, -1 none.
func (sp *settingsPage) chipAt(x int) int {
	chips, xs := sp.chips(-1)
	for i, cx := range xs {
		if x >= cx && x < cx+ansi.StringWidth(chips[2*i].s) {
			return i
		}
	}

	return -1
}

// body draws the tab w cells wide, h rows: the row and the section chip
// under the mouse lit, the chip marked as the panel's tabs are.
func (sp *settingsPage) body(m *Model, w, h int) []string {
	hx, hy, in := sp.hover(m)
	hot := ifElse(in && hy == 0, sp.chipAt(hx), -1)
	chips, xs := sp.chips(hot)

	var marks []ruleMark

	for i, x := range xs {
		if i == sp.sec || i == hot {
			marks = append(marks, ruleMark{x, ansi.StringWidth(chips[2*i].s), i == sp.sec, i == hot})
		}
	}

	out := []string{row(w, nil, append([]seg{sg(" ", plain)}, chips...)), ruleRow(w, marks), blank(w)}

	rows, sel := sp.rows(), sp.settings()

	at := -1
	if sp.at < len(sel) {
		at = sel[sp.at]
	}

	bh := max(h-settingsTopH, 1)
	if at >= 0 { // the row with the keyboard stays in view
		sp.top = max(min(sp.top, at), at-bh+1)
	}

	sp.top = max(min(sp.top, len(rows)-bh), 0)
	table := settingsTable()
	ruleW := min(w-2, settingsCtrlX+settingsDropW+4)

	for k := sp.top; k < len(rows) && len(out) < h; k++ {
		r := rows[k]

		switch {
		case r.i < 0 && r.group == "":
			out = append(out, blank(w))
		case r.i < 0:
			out = append(out, row(w, nil, []seg{sg("  "+r.group+" ", dim), sg(strings.Repeat("─", max(ruleW-ansi.StringWidth(r.group)-3, 0)), fg(pal.inputBorder))}))
		default:
			var bg color.Color

			switch {
			case k == at && m.focus == onMain: // a shade deeper than the hover, as VS Code's focused row
				bg = pal.selBg
			case in && hy-settingsTopH+sp.top == k:
				bg = pal.hoverBg
			}

			s := table[r.i]
			label := ansi.Truncate(s.label, settingsCtrlX-settingsLabelX-1, "…")
			segs := []seg{sg(blank(settingsLabelX)+label, plain), sg(blank(max(settingsCtrlX-settingsLabelX-ansi.StringWidth(label), 1)), plain)}

			if s.opts == nil {
				segs = append(segs, sg("[", dim), sg(ifElse(s.get(m) == "on", "x", " "), plain), sg("]", dim))
			} else {
				v := ansi.Truncate(s.get(m), settingsDropW-4, "…")
				segs = append(segs, sg("❱ ", dim), sg(v, plain), sg(blank(max(settingsDropW-2-ansi.StringWidth(v), 1)), plain), sg("▼", dim))
			}

			out = append(out, row(w, bg, segs))
		}
	}

	for len(out) < h {
		out = append(out, blank(w))
	}

	return out
}

// selected is the setting with the keyboard, false for none.
func (sp *settingsPage) selected() (setting, bool) {
	sel := sp.settings()
	if sp.at < 0 || sp.at >= len(sel) {
		return setting{}, false
	}

	return settingsTable()[sp.rows()[sel[sp.at]].i], true
}

// act is ⏎ or a click on a setting's control: a checkbox flips, a dropdown
// opens its values under the control.
func (sp *settingsPage) act(m *Model, s setting) tea.Cmd {
	if s.opts == nil {
		return s.set(m, s.next(m))
	}

	items := make([]item, len(s.opts))
	for i, v := range s.opts {
		items[i] = item{label: v, hint: ifElse(v == s.get(m), "✓", ""), run: func(m *Model) tea.Cmd { return s.set(m, v) }}
	}

	k := sp.settings()[sp.at] - sp.top
	x, y := m.mainX()+settingsCtrlX, m.mainY()+m.headH()+m.stripH()+settingsTopH+k+1 // right under the control
	m.menuOf(items, x, y)
	m.modal.l.sel = max(slices.Index(s.opts, s.get(m)), 0) // on the value it has, as VS Code's select

	return nil
}

func (sp *settingsPage) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	n := len(sp.settings())

	switch k.String() {
	case "up", "k":
		sp.at = max(sp.at-1, 0)
	case "down", "j":
		sp.at = min(sp.at+1, n-1)
	case "home", "g":
		sp.at = 0
	case "end", "G":
		sp.at = n - 1
	case "tab", "]":
		sp.sec, sp.at, sp.top = (sp.sec+1)%len(settingsSections()), 0, 0
	case "shift+tab", "[":
		sp.sec, sp.at, sp.top = (sp.sec+len(settingsSections())-1)%len(settingsSections()), 0, 0
	case "enter", "space":
		if s, ok := sp.selected(); ok {
			return sp.act(m, s)
		}

	case "left", "right", "h", "l":
		s, ok := sp.selected()
		if !ok {
			return nil
		}

		if s.opts == nil {
			return s.set(m, s.next(m))
		}

		i := slices.Index(s.opts, s.get(m)) + ifElse(k.String() == "left" || k.String() == "h", -1, 1)

		return s.set(m, s.opts[(i+len(s.opts))%len(s.opts)])

	case "q", "esc", "ctrl+w":
		return m.closeEditor(m.edIdx)
	}

	return nil
}

// mouse takes a click on a section's chip, a setting (its control acts),
// and the wheel; x and y count from the tab's body.
func (sp *settingsPage) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		sp.top = max(sp.top+wheelDelta(mo), 0)
		return nil
	}

	if c, click := msg.(tea.MouseClickMsg); !click || c.Button != tea.MouseLeft {
		return nil
	}

	if y == 0 {
		if i := sp.chipAt(x); i >= 0 {
			sp.sec, sp.at, sp.top = i, 0, 0
		}

		return nil
	}

	k := sp.top + y - settingsTopH

	at := slices.Index(sp.settings(), k)
	if y < settingsTopH || at < 0 {
		return nil
	}

	sp.at, m.focus = at, onMain
	if s, _ := sp.selected(); x >= settingsCtrlX && x < settingsCtrlX+ifElse(s.opts == nil, 3, settingsDropW) {
		return sp.act(m, s)
	}

	return nil
}

// settingsHints is the Settings tab's footer.
func settingsHints() string {
	return " ↑↓ move  ⏎ space set  ←→ change  tab ] [ section  q close"
}
