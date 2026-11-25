package ui

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xseman/pando/internal/proto"
)

// usageMsg is the daemon's proto.Usage, read at startup and when the tab
// shows; the daemon pushes it as a `usage` event besides.
type usageMsg proto.Usage

func loadUsage() tea.Cmd {
	return func() tea.Msg {
		var u proto.Usage
		if proto.Call("usage.get", nil, &u) != nil {
			return nil // an older daemon does not know the method
		}

		return usageMsg(u)
	}
}

// refreshUsage has the daemon ask every account's service again; it answers
// with what it has when it asked a moment ago.
func refreshUsage() tea.Cmd {
	return func() tea.Msg {
		var u proto.Usage
		if err := proto.Call("usage.refresh", nil, &u); err != nil {
			return flashMsg{"usage: " + err.Error(), true}
		}

		return usageMsg(u)
	}
}

// hasUsage reports limits to show: usage is on and the daemon knows an
// account. Sessions' header has the Usage tab then, and the status bar its
// chip.
func (m *Model) hasUsage() bool { return m.st.Settings.Usage && len(m.usage.Accounts) > 0 }

// dropUsageTab forgets the Usage tab as Sessions' pick once it is gone:
// coming back later, it would take the column from under the keyboard.
func (m *Model) dropUsageTab() {
	if m.headTab == viewUsage && !m.hasUsage() {
		m.headTab = 0
	}
}

// accountAt is the index in Usage.Accounts of agent's account that env
// picks, the default for none; -1 for no such account.
func (m *Model) accountAt(agent string, env []string) int {
	return slices.IndexFunc(m.usage.Accounts, func(a proto.Account) bool { return a.Agent == agent && slices.Equal(a.Env, env) })
}

// account is agent's account that env picks, the default for none.
func (m *Model) account(agent string, env []string) (proto.Account, bool) {
	i := m.accountAt(agent, env)
	if i < 0 {
		return proto.Account{}, false
	}

	return m.usage.Accounts[i], true
}

// onUsage takes the limits and says a new offer out loud, once: the profile
// new sessions start with is near a limit and another has room.
func (m *Model) onUsage(u proto.Usage) tea.Cmd {
	had := m.usage.Offer
	m.usage, m.chips = u, nil
	m.dropUsageTab()
	m.us.l.clamp(len(m.us.rows(m)), m.bodyH(viewUsage))

	for _, agent := range slices.Sorted(maps.Keys(u.Offer)) {
		from, known := m.account(agent, m.activeEnv(agent))
		to, ok := m.account(agent, u.Offer[agent])

		if _, old := had[agent]; old || !known || !ok {
			continue
		}

		peak, _ := from.Peak()

		return flash(fmt.Sprintf("%s %s is at %.0f%% of a limit · %s has room", agent, from.Name, peak, to.Name), false)
	}

	return nil
}

// movedProfile says a profile the daemon made active since before, as
// profile_switch "auto" does; one picked here is in m.st already.
func (m *Model) movedProfile(before map[string][]string) tea.Cmd {
	for _, agent := range slices.Sorted(maps.Keys(m.st.Active)) {
		a, ok := m.account(agent, m.st.Active[agent])
		if ok && m.st.Settings.Switch == "auto" && !slices.Equal(before[agent], a.Env) {
			return flash("new "+agent+" sessions start as "+a.Name, false)
		}
	}

	return nil
}

// useProfile makes account a the one new sessions of its agent start with.
func (m *Model) useProfile(a proto.Account) tea.Cmd {
	if m.st.Active == nil {
		m.st.Active = map[string][]string{}
	}

	m.st.Active[a.Agent] = a.Env // optimistic: the daemon sends the state back

	return tea.Batch(
		do("state.set", map[string]any{"active_profile": map[string][]string{a.Agent: a.Env}}),
		flash("new "+a.Agent+" sessions start as "+a.Name, false))
}

// renameProfile asks for the name account a goes by, in Usage, the profile
// picker and on a session's chip, as session.profile takes it; empty gives
// it back the one its config or [profiles] gives it.
func (m *Model) renameProfile(a proto.Account) tea.Cmd {
	m.modal = newPrompt("Rename profile · empty names it by its config", a.Name, func(_ *Model, v string) tea.Cmd {
		names := map[string]map[string]string{a.Agent: {strings.Join(a.Env, " "): v}}

		return do("state.set", map[string]any{"profile_names": names})
	})

	return nil
}

// activeEnv is the profile new sessions of agent start with: State.Active's,
// the default's none once that profile is no account any more, as the daemon
// starts them.
func (m *Model) activeEnv(agent string) []string {
	env := m.st.Active[agent]
	if _, ok := m.account(agent, env); !ok {
		return nil
	}

	return env
}

// active reports whether a is the account new sessions of its agent start with.
func (m *Model) active(a proto.Account) bool { return slices.Equal(m.activeEnv(a.Agent), a.Env) }

// spends is the account a session of agent under env spends: its config's,
// or the default's for a profile with no config of its own. False while
// nothing is known of it.
func (m *Model) spends(agent string, env []string) (proto.Account, bool) {
	a, ok := m.account(agent, env)
	if own := slices.ContainsFunc(env, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(m.st.ResumeEnv[agent], k)
	}); !ok && !own { // no config of its own: it runs on the default's sign-in
		a, ok = m.account(agent, nil)
	}

	return a, ok && m.hasUsage()
}

// pickMeter is the meter's cells the profile picker asks room for.
const pickMeter = 30

// roomWindows are the limit windows accounts name, in the order they name
// them: claude's 5h and 7d.
func roomWindows(accounts []proto.Account) []string {
	var out []string

	for _, a := range accounts {
		for _, l := range a.Limits {
			if !slices.Contains(out, l.Window) {
				out = append(out, l.Window)
			}
		}
	}

	return out
}

// windowLimit is what a meter of limits on window shows: the limit every
// model draws from, else the fullest of the models' own, the others as its
// ticks. The zero limit for a window none of limits is on.
func windowLimit(limits []proto.Limit, window string) (proto.Limit, []proto.Limit) {
	on := slices.DeleteFunc(slices.Clone(limits), func(l proto.Limit) bool { return l.Window != window })
	if len(on) == 0 {
		return proto.Limit{}, nil
	}

	i := slices.IndexFunc(on, func(l proto.Limit) bool { return l.Model == "" })
	if i < 0 {
		i = slices.Index(on, slices.MaxFunc(on, func(x, y proto.Limit) int { return cmp.Compare(x.Percent, y.Percent) }))
	}

	shown := on[i]
	ticks := slices.Delete(on, i, i+1)

	return shown, ticks
}

// roomRows draw account a in a profile picker as Usage's wide layout does:
// its name, marked ● when active, over its plan in nameW cells, beside a row
// per window, its label in labelW cells, a meter of w cells, then the share
// and the reset; why its limits are not current instead of them. Each row is
// its left and its right.
func roomRows(a proto.Account, name string, active bool, nameW, labelW, w int) [][2][]seg {
	names := slices.DeleteFunc([]string{name, a.Plan}, func(s string) bool { return s == "" })
	now := time.Now()

	var rows [][2][]seg

	if a.Note != "" {
		rows = append(rows, [2][]seg{noteSegs(a.Note, labelW+1+w+13)})
	} else {
		for _, win := range roomWindows([]proto.Account{a}) {
			l, ticks := windowLimit(a.Limits, win)
			rows = append(rows, [2][]seg{
				append([]seg{sg(pad(limitLabel(l), labelW, false)+" ", dim)}, limitMeter(w, l.Percent, ticks, fg(pal.accent))...),
				{sg(fmt.Sprintf("%3.0f%%", l.Percent), levelStyle(l.Percent, plain)), sg(pad(until(l.Resets, now), 9, true), dim)},
			})
		}
	}

	for len(rows) < len(names) {
		rows = append(rows, [2][]seg{})
	}

	for i := range rows {
		mark, text, st := sg("  ", plain), "", dim

		switch {
		case i == 0 && active:
			mark, text, st = sg("● ", fg(pal.ok)), names[0], plain
		case i == 0:
			text, st = names[0], plain
		case i < len(names):
			text = names[i]
		}

		rows[i][0] = append([]seg{mark, sg(pad(text, nameW, false)+"  ", st)}, rows[i][0]...)
	}

	return rows
}

// noteSegs say why an account's limits are not current in w cells: its word
// ("paused") in the warning color, then what it explains, cut to fit.
func noteSegs(note string, w int) []seg {
	word, why, _ := strings.Cut(note, ": ")
	if why != "" {
		why = " · " + why
	}

	return []seg{sg(word, fg(pal.warn)), sg(pad(why, w-strWidth(word), false), dim)}
}

// until is how long from now until t, as a limit's reset reads: "3d 4h",
// "2h 10m", "45m"; "" for no time.
func until(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}

	left := max(t.Sub(now), 0)
	days, hours, mins := int(left.Hours())/24, int(left.Hours())%24, int(left.Minutes())%60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}

	return fmt.Sprintf("%dm", mins)
}

// usageChip is the status bar's account new sessions start with, of the agent
// in view or else the first known: its name and fullest limit, colored as the
// limit nears. With an offer it names the profile with room, and a click
// takes it; else a click shows the tab.
func (m *Model) usageChip() ([]seg, string, func(m *Model) tea.Cmd) {
	if !m.hasUsage() {
		return nil, "", nil
	}

	agent := sessionAgent(m.session(m.sess))
	if _, ok := m.account(agent, nil); !ok { // an agent nothing is known of
		agent = m.usage.Accounts[0].Agent
	}

	a, ok := m.account(agent, m.activeEnv(agent))
	peak, known := a.Peak()

	if !ok || !known {
		return nil, "", nil
	}

	st := levelStyle(peak, fg(pal.descFg))
	chip := []seg{sg(fmt.Sprintf(" %s %s %.0f%% ", agent, a.Name, peak), st)}

	to, offered := m.account(agent, m.usage.Offer[agent])
	if _, pending := m.usage.Offer[agent]; !pending || !offered {
		return chip, "Show Usage  9", func(m *Model) tea.Cmd { return m.showView(viewUsage) }
	}

	chip = append(chip, sg("use "+to.Name+" ", fg(pal.headerAccent)))

	return chip, "Start New " + agent + " Sessions as " + to.Name, func(m *Model) tea.Cmd { return m.useProfile(to) }
}

// levelStyle colors a share used: the error color from 90%, the warning from
// 75%, else calm.
func levelStyle(pct float64, calm lipgloss.Style) lipgloss.Style {
	switch {
	case pct >= 90:
		return fg(pal.errc)
	case pct >= 75:
		return fg(pal.warn)
	}

	return calm
}

// usageView is the Usage tab of Sessions: each agent's plan limits by
// sign-in, and which of them new sessions start with. It shows what the
// daemon measured (Model.usage) and asks for nothing itself. w is the width
// it was last drawn in, which picks its layout (rows).
type usageView struct {
	l list
	w int
}

// usWide is the narrowest Usage that draws an account's name and plan in a
// column beside its limits; a narrower one draws them over its limits.
const usWide = 50

const (
	usAgent   = iota // an agent's heading; its only account's row too
	usAccount        // a sign-in's name and plan over its limits, narrow
	usLimit          // one window of its allowance, or a model's own
	usNote           // why its limits are not current
	usName           // a row only the name column says something on, wide
	usGap            // the blank before an agent or account but the first
)

// usRow is a row of the view. acct is the account it belongs to, an agent's
// first for its heading; solo marks an agent with one account, which has
// nothing to pick. head is the row the cursor takes for its account; line is
// the row's place in its account's block when wide, which the name column
// reads (blockNames). ticks are the models' own allowances on the row's
// window.
type usRow struct {
	kind  int
	acct  proto.Account
	solo  bool
	head  bool
	line  int
	lim   proto.Limit
	ticks []proto.Limit
}

// agentNames are the agents as their makers call them.
var agentNames = map[string]string{"claude": "Claude Code", "codex": "Codex", "agy": "Antigravity", "copilot": "Copilot", "grok": "Grok"}

// agentMark is the mark before an agent's heading, Claude Code's in its own
// orange; none for an agent without one.
func agentMark(agent string) seg {
	switch agent {
	case "claude":
		return sg("✳ ", fg(hex("#d97757")))
	case "codex":
		return sg("◎ ", dim)
	}

	return seg{}
}

// inert reports a row the cursor passes over: all but an account's head.
func (r usRow) inert() bool { return !r.head }

// blockNames are what the wide layout's name column says down an account's
// rows: its name, then its plan; an agent's only account its plan alone, as
// the heading names the agent.
func blockNames(a proto.Account, solo bool) []string {
	names := []string{a.Name, a.Plan}
	if solo {
		names = names[1:]
	}

	return slices.DeleteFunc(names, func(s string) bool { return s == "" })
}

// rows are each agent, then its accounts, a blank between two, with a row
// per limit window, a model's own allowance a tick on the window it shares;
// an account whose limits are not current says why instead. Wide, an
// account's name and plan run down a column beside its limits, its first row
// its head; narrow, they head its limits on a row of their own. A filter
// keeps the accounts it matches.
func (v *usageView) rows(m *Model) []usRow {
	var out []usRow

	wide := v.w >= usWide
	q := strings.ToLower(m.query(viewUsage))

	for i, a := range m.usage.Accounts {
		solo := (i == 0 || m.usage.Accounts[i-1].Agent != a.Agent) && (i == len(m.usage.Accounts)-1 || m.usage.Accounts[i+1].Agent != a.Agent)
		if q != "" && !strings.Contains(strings.ToLower(a.Agent+" "+agentNames[a.Agent]+" "+a.Name+" "+a.Plan), q) {
			continue
		}

		if len(out) == 0 || out[len(out)-1].acct.Agent != a.Agent {
			if len(out) > 0 {
				out = append(out, usRow{kind: usGap})
			}

			out = append(out, usRow{kind: usAgent, acct: a, solo: solo, head: solo})
		}

		if !solo && out[len(out)-1].kind != usAgent {
			out = append(out, usRow{kind: usGap})
		}

		if !solo && !wide {
			out = append(out, usRow{kind: usAccount, acct: a, head: true})
		}

		block := len(out)

		switch {
		case a.Note == "":
			for _, l := range a.Limits {
				shared := slices.IndexFunc(out[block:], func(r usRow) bool { return r.lim.Window == l.Window && r.lim.Model == "" })
				if l.Model != "" && shared >= 0 {
					out[block+shared].ticks = append(out[block+shared].ticks, l)
					continue
				}

				out = append(out, usRow{kind: usLimit, acct: a, solo: solo, lim: l})
			}

		case wide || strings.Contains(a.Note, ": "): // narrow, the word alone ends the account's row
			out = append(out, usRow{kind: usNote, acct: a, solo: solo})
		}

		if !wide {
			continue
		}

		for len(out)-block < len(blockNames(a, solo)) {
			out = append(out, usRow{kind: usName, acct: a, solo: solo})
		}

		for j := block; j < len(out); j++ {
			out[j].line = j - block
		}

		if !solo {
			out[block].head = true
		}
	}

	return out
}

// headOf is the head of the account row i is part of, -1 for a gap or the
// heading of an agent with accounts under it.
func headOf(rows []usRow, i int) int {
	for j := i; j >= 0 && j < len(rows) && rows[j].kind != usGap; j-- {
		if rows[j].head {
			return j
		}

		if rows[j].kind == usAgent {
			break
		}
	}

	return -1
}

// sameAccount reports a and b the one sign-in.
func sameAccount(a, b proto.Account) bool { return a.Agent == b.Agent && slices.Equal(a.Env, b.Env) }

func (v *usageView) selected(m *Model) *usRow {
	if rows := v.rows(m); v.l.sel >= 0 && v.l.sel < len(rows) && !rows[v.l.sel].inert() {
		return &rows[v.l.sel]
	}

	return nil
}

func (v *usageView) lines(m *Model, w, h int) []string {
	v.w = w
	rows, now := v.rows(m), time.Now()
	hover, mx := m.hoverRow(viewUsage), m.mouseCol(viewUsage)

	// The limits' labels in one column, so their meters line up, and wide the
	// names' column before them.
	labelW, nameW := 3, 0

	for _, r := range rows {
		if r.kind == usLimit {
			labelW = max(labelW, strWidth(limitLabel(r.lim)))
		}

		for _, n := range blockNames(r.acct, r.solo) {
			nameW = max(nameW, strWidth(n))
		}
	}

	v.l.bar = m.barState("list:usage")

	if v.l.sel >= 0 && v.l.sel < len(rows) && rows[v.l.sel].inert() { // first shown, or filtered: on an account
		v.move(m, 1, len(rows), m.bodyH(viewUsage))
	}

	// The cursor and the pointer take an account's whole block.
	selHead, hoverHead := v.l.sel, -1
	if hover >= 0 {
		hoverHead = headOf(rows, v.l.top+hover)
	}

	slot := v.dropSlot(m, rows) // the drop band takes a row of its own
	n := len(rows) + ifElse(slot >= 0, 1, 0)

	return v.l.render(w, h, n, func(i, rw int) string {
		y := i - v.l.top

		switch {
		case slot < 0 || i < slot:
		case i == slot: // a band in the selection's tint, as Sessions' drop mark
			return row(rw, pal.selBg, nil)
		default:
			i--
		}

		r := rows[i]
		if r.kind == usGap {
			return blank(rw)
		}

		head := headOf(rows, i)
		bg, name := m.rowColors(viewUsage, head >= 0 && head == selHead, head >= 0 && head == hoverHead)
		plan := ifElse(r.acct.Plan == "", "", " · "+r.acct.Plan)
		wide := w >= usWide

		var lead []seg
		if wide {
			lead = nameColumn(m, r, nameW, name)
		}

		switch r.kind {
		case usAgent:
			left := []seg{sg(" ", plain), agentMark(r.acct.Agent), sg(cmp.Or(agentNames[r.acct.Agent], r.acct.Agent), name.Bold(true))}

			switch {
			case !r.solo:
				return row(rw, bg, left, sg(ifElse(m.st.Settings.Switch == "auto", "auto ", ""), dim))
			case wide:
				return row(rw, bg, left)
			}

			return row(rw, bg, append(left, sg(plan, dim)), v.status(m, r, false, false)...)

		case usAccount:
			left := []seg{sg("   "+r.acct.Name, name), sg(plan, dim)}
			over := y == hover && overUse(mx, w)

			return row(rw, bg, left, v.status(m, r, head == selHead || head == hoverHead, over)...)

		case usNote:
			if !wide {
				_, why, _ := strings.Cut(r.acct.Note, ": ")
				return row(rw, bg, []seg{sg("   "+why, dim)})
			}

			return row(rw, bg, append(lead, noteSegs(r.acct.Note, rw-segsWidth(lead)-1)...))

		case usName:
			return row(rw, bg, lead)
		}

		if !wide {
			lead = []seg{sg("   ", plain)}
		}

		lead = append(lead, sg(pad(limitLabel(r.lim), labelW, false)+" ", dim))
		right := []seg{
			sg(fmt.Sprintf(" %3.0f%%", r.lim.Percent), levelStyle(r.lim.Percent, name)),
			sg(pad(until(r.lim.Resets, now), 9, true)+" ", dim),
		}

		cells := rw - segsWidth(lead) - segsWidth(right)
		if cells < 3 { // too narrow for a meter that says anything
			return row(rw, bg, lead, right...)
		}

		if len(r.ticks) > 0 && y == hover {
			tips := make([]string, len(r.ticks))
			for j, t := range r.ticks {
				tips[j] = fmt.Sprintf("┃ %s's Own %s Limit  %.0f%%", t.Model, r.lim.Window, t.Percent)
			}

			m.tip = strings.Join(tips, "   ")
		}

		return row(rw, bg, append(lead, limitMeter(cells, r.lim.Percent, r.ticks, fg(pal.accent))...), right...)
	})
}

// nameColumn is the wide layout's name column on row r, nameW cells and the
// gap after: the account's name in st on its first row, ● before it for the
// one new sessions start with, then its plan dim (blockNames).
func nameColumn(m *Model, r usRow, nameW int, st lipgloss.Style) []seg {
	names := blockNames(r.acct, r.solo)
	text := ""

	if r.line < len(names) {
		text = names[r.line]
	}

	mark := sg("   ", plain)
	if r.line == 0 && !r.solo && m.active(r.acct) {
		mark = sg(" ● ", fg(pal.ok))
	}

	if r.line > 0 || r.solo {
		st = dim
	}

	return []seg{mark, sg(pad(text, nameW, false)+"  ", st)}
}

// status ends an account's heading row in the narrow layout: why its limits
// are not current, then active for the one new sessions start with, or "use"
// while the cursor or the pointer is on it (shown) and a keycap with the
// pointer on it (over).
func (v *usageView) status(m *Model, r usRow, shown, over bool) []seg {
	var words []seg

	if word, _, _ := strings.Cut(r.acct.Note, ":"); word != "" {
		words = append(words, sg(word, fg(pal.warn)))
	}

	switch {
	case r.solo:
	case m.active(r.acct):
		words = append(words, sg("active", fg(pal.ok).Bold(true)))
	case over:
		words, m.tip = append(words, sgOwn("use", keycapHot())), "Start New "+r.acct.Agent+" Sessions as "+r.acct.Name+"  ⏎"
	case shown:
		words = append(words, sg("use", fg(pal.headerAccent)))
	}

	var out []seg

	for i, w := range words {
		if i > 0 {
			out = append(out, sg("  ", plain))
		}

		out = append(out, w)
	}

	if len(out) > 0 {
		out = append(out, sg(" ", plain))
	}

	return out
}

// limitLabel names a limit's row: its window, after the models it is theirs
// when it has no window of every model's to mark ("Gemini 5h").
func limitLabel(l proto.Limit) string {
	return strings.TrimPrefix(l.Model+" "+l.Window, " ")
}

// limitMeter draws pct of w cells, ▰ over a ▱ track, any use one cell at
// least: the fill in tone, or as levelStyle colors pct near its end, and the
// track tinted toward those colors past the shares they start at. Each of
// ticks, a model's own share, is a ┃ colored as that share is.
func limitMeter(w int, pct float64, ticks []proto.Limit, tone lipgloss.Style) []seg {
	full := min(int(math.Round(pct/100*float64(w))), w)
	if pct > 0 {
		full = max(full, 1)
	}

	fill := levelStyle(pct, tone)
	cells := make([]seg, w)

	for i := range cells {
		at := float64(i+1) / float64(w) * 100

		switch {
		case i < full:
			cells[i] = sg("▰", fill)
		case at > 90:
			cells[i] = sg("▱", meterTrack[2])
		case at > 75:
			cells[i] = sg("▱", meterTrack[1])
		default:
			cells[i] = sg("▱", meterTrack[0])
		}
	}

	for _, t := range ticks {
		if t.Percent > 0 {
			cells[min(int(t.Percent/100*float64(w)), w-1)] = sg("┃", levelStyle(t.Percent, plain))
		}
	}

	return cells
}

// overUse reports cell x of an account's row, w cells wide, on its "use".
func overUse(x, w int) bool { return x >= w-5 }

// use makes row r's account the one new sessions start with; an agent's only
// account is that already.
func (v *usageView) use(m *Model, r *usRow) tea.Cmd {
	if r == nil || r.solo || m.active(r.acct) {
		return nil
	}

	return m.useProfile(r.acct)
}

// toggleAuto flips profile_switch between "auto" and "notify".
func (m *Model) toggleAuto() tea.Cmd {
	return m.setSettings(map[string]any{"profile_switch": ifElse(m.st.Settings.Switch == "auto", "notify", "auto")})
}

// move moves the selection by d rows and walks on over inert rows the same
// way, turning back at an end: the first row is a heading.
func (v *usageView) move(m *Model, d, n, h int) {
	v.l.move(d, n, h)

	rows, s := v.rows(m), min(max(d, -1), 1)
	for range n {
		if v.l.sel < 0 || v.l.sel >= len(rows) || !rows[v.l.sel].inert() {
			return
		}

		if v.l.sel+s < 0 || v.l.sel+s >= n {
			s = -s
		}

		v.l.move(s, n, h)
	}
}

func (v *usageView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	h, n := m.bodyH(viewUsage), len(v.rows(m))

	switch k.String() {
	case "up", "k":
		v.move(m, -1, n, h)
	case "down", "j":
		v.move(m, 1, n, h)
	case "pgup":
		v.move(m, -h, n, h)
	case "pgdown":
		v.move(m, h, n, h)
	case "g", "home":
		v.move(m, -n, n, h)
	case "G", "end":
		v.move(m, n, n, h)
	case "enter", "space":
		return v.use(m, v.selected(m))
	case "alt+up", "alt+down":
		return v.shift(m, map[bool]int{true: -1, false: 1}[k.String() == "alt+up"])
	case "R", "f2":
		if r := v.selected(m); r != nil {
			return m.renameProfile(r.acct)
		}

	case "a":
		return m.toggleAuto()
	case "r", "ctrl+r":
		return refreshUsage()
	case "m":
		return m.menuOf(v.items(m), 0, h/2)
	}

	return nil
}

func (v *usageView) items(m *Model) []item {
	var items []item

	if r := v.selected(m); r != nil {
		if !r.solo && !m.active(r.acct) {
			items = append(items, item{label: "Use " + r.acct.Name + " for New Sessions", hint: "⏎", run: func(m *Model) tea.Cmd { return m.useProfile(r.acct) }})
		}

		items = append(items,
			item{label: "Rename Profile…", hint: "R", run: func(m *Model) tea.Cmd { return m.renameProfile(r.acct) }},
			separator())
	}

	return append(items,
		item{label: checked(m.st.Settings.Switch == "auto", "Switch Profiles Automatically"), hint: "a", run: (*Model).toggleAuto},
		item{label: "Refresh", hint: "r", run: func(*Model) tea.Cmd { return refreshUsage() }})
}

func (v *usageView) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo, rows := msg.Mouse(), v.rows(m)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		v.l.wheel(wheelDelta(mo), len(rows), m.bodyH(viewUsage))
	case tea.MouseClickMsg:
		i := v.l.at(y, len(rows))

		head := headOf(rows, i)
		if head >= 0 {
			v.l.sel = head
		}

		if mo.Button == tea.MouseRight {
			return m.menuOf(v.items(m), mo.X, mo.Y)
		}

		col := m.colOf(viewUsage)
		if w := m.colRect(col).w - m.barW(col); i >= 0 && rows[i].kind == usAccount && overUse(x, w) {
			return v.use(m, &rows[i])
		}
		// An account of an agent with more than one drags among them by any
		// of its rows.
		if head >= 0 && mo.Button == tea.MouseLeft && !rows[head].solo {
			m.drag = &drag{kind: dragProfile, col: m.accountAt(rows[head].acct.Agent, rows[head].acct.Env), slot: -1, y0: mo.Y}
		}
	}

	return nil
}

// dragTo marks where the dragged account would land, a tinted row before the
// one whose place it takes or past it (dropSlot), and the release moves it
// there. Let go anywhere else it stays.
func (v *usageView) dragTo(m *Model, d *drag, y int, release bool) tea.Cmd {
	if y != d.y0 {
		d.moved = true
	}

	if d.moved {
		d.slot = v.dropOn(m, d, v.rows(m))
	}

	if !release {
		return nil
	}

	m.drag = nil

	if d.slot < 0 {
		return nil
	}

	return m.reorderProfile(d.col, d.slot)
}

// dropOn is the account whose place drag d takes with the pointer over one of
// its rows: one of the dragged one's agent, not itself. Over a gap or the
// band it keeps the last; -1 for none.
func (v *usageView) dropOn(m *Model, d *drag, rows []usRow) int {
	y := m.hoverRow(viewUsage)
	if y < 0 || d.col >= len(m.usage.Accounts) {
		return -1
	}

	i := v.l.top + y
	if at := v.dropSlot(m, rows); at >= 0 && i >= at { // the rows below the band sit a row lower
		if i == at {
			return d.slot
		}

		i--
	}

	if i >= len(rows) {
		return -1
	}

	if rows[i].kind == usGap {
		return d.slot
	}

	to := m.accountAt(rows[i].acct.Agent, rows[i].acct.Env)
	if to < 0 || to == d.col || m.usage.Accounts[to].Agent != m.usage.Accounts[d.col].Agent {
		return -1
	}

	return to
}

// dropSlot is the row the drop band goes before while an account drags: the
// head of the one it takes the place of moving up, past that one's rows
// moving down; -1 with no drop.
func (v *usageView) dropSlot(m *Model, rows []usRow) int {
	d := m.drag
	if d == nil || d.kind != dragProfile || d.slot < 0 || d.slot >= len(m.usage.Accounts) {
		return -1
	}

	to := m.usage.Accounts[d.slot]

	at := slices.IndexFunc(rows, func(r usRow) bool { return r.head && sameAccount(r.acct, to) })
	if at < 0 || d.slot < d.col {
		return at
	}

	at++
	for at < len(rows) && rows[at].kind != usGap && !rows[at].head {
		at++
	}

	return at
}

// shift moves the selected account d places among its agent's: alt+↑↓'s
// half of the drag.
func (v *usageView) shift(m *Model, d int) tea.Cmd {
	r := v.selected(m)
	if r == nil || r.solo {
		return nil
	}

	from := m.accountAt(r.acct.Agent, r.acct.Env)
	to := from + d

	if from < 0 || to < 0 || to >= len(m.usage.Accounts) || m.usage.Accounts[to].Agent != r.acct.Agent {
		return nil
	}

	return m.reorderProfile(from, to)
}

// reorderProfile puts account from where account to is in Usage.Accounts, both
// of one agent, here at once and in state.json's profile_order; the moved
// account stays selected.
func (m *Model) reorderProfile(from, to int) tea.Cmd {
	a := m.usage.Accounts[from]
	rest := slices.Delete(slices.Clone(m.usage.Accounts), from, from+1)
	m.usage.Accounts, m.chips = slices.Insert(rest, to, a), nil

	var order [][]string

	for _, b := range m.usage.Accounts {
		if b.Agent == a.Agent {
			order = append(order, b.Env)
		}
	}

	rows := m.us.rows(m)
	if i := slices.IndexFunc(rows, func(r usRow) bool {
		return r.head && sameAccount(r.acct, a)
	}); i >= 0 {
		m.us.l.sel = i
		m.us.l.snap(m.bodyH(viewUsage))
	}

	return do("state.set", map[string]any{"profile_order": map[string][][]string{a.Agent: order}})
}
