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

// account is agent's account that env picks, the default for none.
func (m *Model) account(agent string, env []string) (proto.Account, bool) {
	i := slices.IndexFunc(m.usage.Accounts, func(a proto.Account) bool { return a.Agent == agent && slices.Equal(a.Env, env) })
	if i < 0 {
		return proto.Account{}, false
	}

	return m.usage.Accounts[i], true
}

// onUsage takes the limits and says a new offer out loud, once: the profile
// new sessions start with is near a limit and another has room.
func (m *Model) onUsage(u proto.Usage) tea.Cmd {
	had := m.usage.Offer
	m.usage = u
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

// room is how spent agent's account under env is, for a row that names it:
// its fullest limit and when that resets, or why its limits are not current.
// "" while nothing is known of it.
func (m *Model) room(agent string, env []string) string {
	a, ok := m.account(agent, env)
	if own := slices.ContainsFunc(env, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(m.st.ResumeEnv[agent], k)
	}); !ok && !own { // no config of its own: it runs on the default's sign-in
		a, ok = m.account(agent, nil)
	}

	if !ok || !m.hasUsage() {
		return ""
	}

	if len(a.Limits) == 0 {
		return noteWord(a.Note)
	}

	fullest := slices.MaxFunc(a.Limits, func(x, y proto.Limit) int { return cmp.Compare(x.Percent, y.Percent) })
	out := fmt.Sprintf("%.0f%%", fullest.Percent)

	if left := until(fullest.Resets, time.Now()); left != "" {
		out += " · " + left
	}

	if a.Note != "" {
		out += " · " + noteWord(a.Note)
	}

	return out
}

// noteWord is an account's Note without its explanation: "paused".
func noteWord(note string) string {
	word, _, _ := strings.Cut(note, ":")
	return word
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
// daemon measured (Model.usage) and asks for nothing itself.
type usageView struct{ l list }

const (
	usAgent   = iota // an agent's heading; its only account's row too
	usAccount        // a sign-in: its name and plan
	usLimit          // one window of its allowance
	usNote           // why its limits are not current
)

// usRow is a row of the view. acct is the account it belongs to, an agent's
// first for its heading; solo marks an agent with one account, which has
// nothing to pick. ticks are the models' own allowances on the row's window.
type usRow struct {
	kind  int
	acct  proto.Account
	solo  bool
	lim   proto.Limit
	ticks []proto.Limit
}

// agentNames are the agents as their makers call them.
var agentNames = map[string]string{"claude": "Claude Code", "codex": "Codex"}

// rows are each agent, then its accounts with a row per limit window; a
// model's own allowance marks the window it shares instead of taking a row.
// A filter keeps the accounts it matches.
func (v *usageView) rows(m *Model) []usRow {
	var out []usRow

	q := strings.ToLower(m.query(viewUsage))

	for i, a := range m.usage.Accounts {
		solo := (i == 0 || m.usage.Accounts[i-1].Agent != a.Agent) && (i == len(m.usage.Accounts)-1 || m.usage.Accounts[i+1].Agent != a.Agent)
		if q != "" && !strings.Contains(strings.ToLower(a.Agent+" "+agentNames[a.Agent]+" "+a.Name+" "+a.Plan), q) {
			continue
		}

		if len(out) == 0 || out[len(out)-1].acct.Agent != a.Agent {
			out = append(out, usRow{kind: usAgent, acct: a, solo: solo})
		}

		if !solo {
			out = append(out, usRow{kind: usAccount, acct: a})
		}

		first := len(out)

		for _, l := range a.Limits {
			shared := slices.IndexFunc(out[first:], func(r usRow) bool { return r.lim.Window == l.Window && r.lim.Model == "" })
			if l.Model != "" && shared >= 0 {
				out[first+shared].ticks = append(out[first+shared].ticks, l)
				continue
			}

			out = append(out, usRow{kind: usLimit, acct: a, solo: solo, lim: l})
		}

		if a.Note != "" {
			out = append(out, usRow{kind: usNote, acct: a, solo: solo})
		}
	}

	return out
}

func (v *usageView) selected(m *Model) *usRow {
	if rows := v.rows(m); v.l.sel >= 0 && v.l.sel < len(rows) {
		return &rows[v.l.sel]
	}

	return nil
}

func (v *usageView) lines(m *Model, w, h int) []string {
	rows, now := v.rows(m), time.Now()
	hover, mx := m.hoverRow(viewUsage), m.mouseCol(viewUsage)
	v.l.bar = m.barState("list:usage")

	return v.l.render(w, h, len(rows), func(i, rw int) string {
		r := rows[i]
		bg, name := m.rowColors(viewUsage, i == v.l.sel, i-v.l.top == hover)
		plan := ifElse(r.acct.Plan == "", "", " · "+r.acct.Plan)

		switch r.kind {
		case usAgent:
			left := []seg{sg(" "+cmp.Or(agentNames[r.acct.Agent], r.acct.Agent), name.Bold(true))}
			if r.solo {
				return row(rw, bg, append(left, sg(plan, dim)))
			}

			return row(rw, bg, left, sg(ifElse(m.st.Settings.Switch == "auto", "auto ", ""), dim))

		case usAccount:
			left := []seg{sg("   "+r.acct.Name, name), sg(plan, dim)}
			if m.active(r.acct) {
				return row(rw, bg, left, sg("active ", fg(pal.ok)))
			}

			use := sg("use", fg(pal.headerAccent))
			if i-v.l.top == hover && overUse(mx, w) {
				use, m.tip = sgOwn("use", keycapHot()), "Start New "+r.acct.Agent+" Sessions as "+r.acct.Name+"  ⏎"
			}

			return row(rw, bg, left, use, sg(" ", plain))

		case usNote:
			return row(rw, bg, []seg{sg("   "+r.acct.Note, dim)})
		}

		label := pad(r.lim.Window, 3, false)
		if r.lim.Model != "" { // a model's allowance with no window of every model's to mark
			label = r.lim.Model + " " + r.lim.Window
		}

		lead := "   " + label + " "
		stale := r.acct.Note != ""
		tone := ifElse(stale, dim, levelStyle(r.lim.Percent, fg(pal.accent)))
		right := []seg{
			sg(fmt.Sprintf("%3.0f%%", r.lim.Percent), ifElse(stale, dim, levelStyle(r.lim.Percent, name))),
			sg(pad(until(r.lim.Resets, now), 8, true)+" ", dim),
		}

		cells := rw - strWidth(lead) - 14
		if cells < 3 { // too narrow for a meter that says anything
			return row(rw, bg, []seg{sg(lead, dim)}, right...)
		}

		ticks := ifElse(stale, nil, r.ticks)
		if len(ticks) > 0 && i-v.l.top == hover {
			tips := make([]string, len(ticks))
			for j, t := range ticks {
				tips[j] = fmt.Sprintf("┃ %s's Own %s Limit  %.0f%%", t.Model, r.lim.Window, t.Percent)
			}

			m.tip = strings.Join(tips, "   ")
		}

		return row(rw, bg, append([]seg{sg(lead, dim)}, limitMeter(cells, r.lim.Percent, ticks, tone)...), right...)
	})
}

// limitMeter draws pct of w cells in half-cell steps, tone over a faint
// track, with a mark at each of ticks.
func limitMeter(w int, pct float64, ticks []proto.Limit, tone lipgloss.Style) []seg {
	halves := int(math.Round(pct / 100 * float64(w) * 2))
	full := min(halves/2, w)
	track := fg(pal.indentGuide)

	cells := make([]seg, w)
	for i := range cells {
		switch {
		case i < full:
			cells[i] = sg("━", tone)
		case i == full && halves%2 == 1:
			cells[i] = sg("╾", tone)
		default:
			cells[i] = sg("─", track)
		}
	}

	for _, t := range ticks {
		cells[min(int(t.Percent/100*float64(w)), w-1)] = sg("┃", plain)
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

func (v *usageView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	h, n := m.bodyH(viewUsage), len(v.rows(m))

	switch k.String() {
	case "up", "k":
		v.l.move(-1, n, h)
	case "down", "j":
		v.l.move(1, n, h)
	case "pgup":
		v.l.move(-h, n, h)
	case "pgdown":
		v.l.move(h, n, h)
	case "g", "home":
		v.l.move(-n, n, h)
	case "G", "end":
		v.l.move(n, n, h)
	case "enter", "space":
		return v.use(m, v.selected(m))
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

	if r := v.selected(m); r != nil && !r.solo && !m.active(r.acct) {
		items = append(items,
			item{label: "Use " + r.acct.Name + " for New Sessions", hint: "⏎", run: func(m *Model) tea.Cmd { return m.useProfile(r.acct) }},
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
		if i >= 0 {
			v.l.sel = i
		}

		if mo.Button == tea.MouseRight {
			return m.menuOf(v.items(m), mo.X, mo.Y)
		}

		col := m.colOf(viewUsage)
		if w := m.colRect(col).w - m.barW(col); i >= 0 && rows[i].kind == usAccount && overUse(x, w) {
			return v.use(m, &rows[i])
		}
	}

	return nil
}
