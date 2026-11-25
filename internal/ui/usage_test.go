package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// workEnv picks the work account in usageModel.
var workEnv = []string{"CLAUDE_CONFIG_DIR=/home/u/.claude-work"}

// usageModel is a test model with usage on and three accounts measured: two
// of claude, the default active and work's limits stale, and codex's one.
func usageModel(t *testing.T) *Model {
	t.Helper()

	m := testModelSized(t, 120, 30)
	m.st.Settings.Usage, m.st.Settings.Switch, m.st.Settings.SwitchAt = true, "notify", 95
	m.st.Agents["claude"], m.st.Agents["codex"] = []string{"claude"}, []string{"codex"}
	m.st.Seen = map[string][][]string{"claude": {workEnv}}

	soon := time.Now().Add(50*time.Hour + 30*time.Minute)
	m.usage = proto.Usage{Accounts: []proto.Account{
		{Agent: "claude", Name: "default", Plan: "Max 5x", Limits: []proto.Limit{
			{Window: "5h", Percent: 40, Resets: soon},
			{Window: "7d", Percent: 96, Resets: soon},
			{Window: "7d", Model: "Fable", Percent: 18, Resets: soon},
		}},
		{Agent: "claude", Name: ".claude-work", Env: workEnv, Plan: "Team 5x", Note: "paused: its sign-in lapsed", Limits: []proto.Limit{{Window: "7d", Percent: 30}}},
		{Agent: "codex", Name: "default", Plan: "Free", Limits: []proto.Limit{{Window: "30d", Percent: 0, Resets: soon}}},
	}}
	m.resize()

	return m
}

// TestUsageView holds what the tab draws and does in a narrow column: an
// agent's accounts each over a row per window, a model's own allowance a ┃ on
// the window it shares, a stale one its reason in place of its limits, an only account on its agent's row; the
// cursor goes from account to account, and ⏎ makes one the account new
// sessions start with.
func TestUsageView(t *testing.T) {
	m := usageModel(t)
	m.showView(viewUsage)

	out := checkWidths(t, m)
	for _, want := range []string{
		"Sessions  Usage", "✳ Claude Code", "◎ Codex", "default · Max 5x", "active", ".claude-work · Team", "paused",
		" 40% ", " 96% ", "┃", "2d 2h", "  its sign-in lapsed", "Codex · Free", "30d ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q:\n%s", want, out)
		}
	}

	rows := m.us.rows(m)

	kinds := make([]int, len(rows))
	for i, r := range rows {
		kinds[i] = r.kind
	}

	if want := []int{usAgent, usAccount, usLimit, usLimit, usGap, usAccount, usNote, usGap, usAgent, usLimit}; !slices.Equal(kinds, want) {
		t.Fatalf("rows %v, want %v", kinds, want)
	}

	if len(rows[3].ticks) != 1 || rows[3].ticks[0].Percent != 18 || !rows[8].solo || rows[0].solo || strings.Contains(out, " 18% ") || strings.Contains(out, " 30% ") {
		t.Fatalf("7d row %+v, codex %+v, the first %+v, or a tick's or stale share drawn:\n%s", rows[3], rows[8], rows[0], out)
	}

	if week := strings.Split(out, "\n")[bodyY(t, m, " 96% ")]; !strings.Contains(week, "▰┃▰▰▰▰▰▰") {
		t.Fatalf("the weekly meter: %q", week)
	}

	// The cursor starts on the first account and passes the heading over it.
	press(m, "up")

	if m.us.l.sel != 1 {
		t.Fatalf("cursor on row %d, want the first account", m.us.l.sel)
	}

	// Down goes to the next account, past the rows of this one's limits.
	press(m, "down")

	if r := m.us.selected(m); r == nil || r.acct.Name != ".claude-work" {
		t.Fatalf("down selects %+v", r)
	}

	press(m, "enter")

	if !slices.Equal(m.st.Active["claude"], workEnv) {
		t.Fatalf("active %v", m.st.Active)
	}

	if out := checkWidths(t, m); strings.Count(out, "active") != 1 || !strings.Contains(strings.Split(out, "\n")[bodyY(t, m, ".claude-wor")], "active") {
		t.Fatalf("after ⏎:\n%s", out)
	}

	// An agent's only account has nothing to pick.
	press(m, "end", "enter")

	if _, set := m.st.Active["codex"]; set {
		t.Fatalf("codex made active: %v", m.st.Active)
	}

	press(m, "a")

	if m.st.Settings.Switch != "auto" || !strings.Contains(checkWidths(t, m), "auto") {
		t.Fatalf("profile_switch %q", m.st.Settings.Switch)
	}

	// A filter keeps the accounts it matches under their agent.
	press(m, "ctrl+f", "w", "o", "r", "k")

	if got := m.us.rows(m); len(got) != 3 || got[1].acct.Name != ".claude-work" {
		t.Fatalf("filtered rows %+v", got)
	}
}

// TestUsageWide holds the tab in a column wide enough for its names' column:
// an account's name over its plan beside its limits, ● the active one's, a
// stale one's reason beside its name; the cursor's account tinted down all
// its rows.
func TestUsageWide(t *testing.T) {
	m := usageModel(t)
	m.st.Settings.Right, m.st.Settings.WidthR = proto.Columns{{Views: []string{"agents"}}}, 62
	m.resize()
	m.showView(viewUsage)

	out := checkWidths(t, m)
	lines := strings.Split(out, "\n")

	for want, after := range map[string]string{
		" ● default       5h":                          "40%",
		"Max 5x        7d":                             "┃",
		"   .claude-work  paused · its sign-in lapsed": "",
		"   Free          30d":                         "0%",
	} {
		if !strings.Contains(lines[bodyY(t, m, want)], after) {
			t.Fatalf("no %q with %q:\n%s", want, after, out)
		}
	}

	rows := m.us.rows(m)

	kinds := make([]int, len(rows))
	for i, r := range rows {
		kinds[i] = r.kind
	}

	if want := []int{usAgent, usLimit, usLimit, usGap, usNote, usName, usGap, usAgent, usLimit}; !slices.Equal(kinds, want) || !rows[1].head || rows[2].head || !rows[4].head || !rows[7].head {
		t.Fatalf("rows %v, want %v; heads %+v", kinds, want, rows)
	}

	press(m, "down")

	sel, _ := m.rowColors(viewUsage, true, false)
	view := strings.Split(m.View().Content, "\n")

	if plan, first := view[bodyY(t, m, "Team 5x")], view[bodyY(t, m, "Max 5x")]; !strings.Contains(plan, bgParams(sel)) || strings.Contains(first, bgParams(sel)) {
		t.Fatalf("the selected account's plan row %q, the first's %q", plan, first)
	}

	// alt+↑ moves the whole block, and the cursor stays on its head.
	press(m, "alt+up")

	if r, rows := m.us.selected(m), m.us.rows(m); r == nil || r.acct.Name != ".claude-work" || rows[1].acct.Name != ".claude-work" || !rows[1].head || rows[3].kind != usGap {
		t.Fatalf("after alt+up %+v, rows %+v", r, rows[:3])
	}
}

// TestUsageTabs holds the header: Usage is its second tab while an account is
// known and usage is on, each tab cut to its share in a narrow column, and
// the body goes back to Sessions when the tab goes.
func TestUsageTabs(t *testing.T) {
	m := usageModel(t)
	m.hasCustom = true

	labels := func(w int) []string {
		var out []string

		end := 0

		for _, tb := range m.headTabs(viewAgents, w) {
			if tb.x < end || tb.x+tb.w > w {
				t.Fatalf("tabs overlap or leave %d cells: %+v", w, m.headTabs(viewAgents, w))
			}

			out, end = append(out, tb.label), tb.x+tb.w+1
		}

		return out
	}

	if got := labels(40); !slices.Equal(got, []string{"Sessions", "Usage", "Customizations"}) {
		t.Fatalf("tabs %q", got)
	}

	if got := labels(22); !slices.Equal(got, []string{"Sessi", "Usage", "Custo"}) {
		t.Fatalf("narrow tabs %q", got)
	}

	press(m, "9")

	if !m.shown(viewUsage) || m.shown(viewAgents) {
		t.Fatal("9 does not show it")
	}

	// Gone and back, the tab does not take the column again by itself.
	m.onUsage(proto.Usage{})

	if !m.shown(viewAgents) {
		t.Fatal("no accounts, and Sessions is not back")
	}

	m.onUsage(usageModel(t).usage)

	if m.shown(viewUsage) || len(m.headTabs(viewAgents, 60)) != 3 {
		t.Fatal("the tab came back in front on its own")
	}

	press(m, "9")

	m.st.Settings.Usage = false

	if !m.shown(viewAgents) || slices.ContainsFunc(m.headTabs(viewAgents, 40), func(tb tab) bool { return tb.v == viewUsage }) {
		t.Fatal("the tab stays with usage off")
	}

	if slices.ContainsFunc(m.viewItems(), func(it item) bool { return it.label == "Show Usage" }) {
		t.Fatal("in the palette with usage off")
	}

	if chip, _, _ := m.usageChip(); chip != nil {
		t.Fatalf("a chip with usage off: %+v", chip)
	}
}

// TestUsageChip holds the status bar: the account new sessions start with
// and its fullest limit, and with an offer the profile with room, which a
// click makes the active one.
func TestUsageChip(t *testing.T) {
	m := usageModel(t)

	status, _ := m.statusLine(m.w)
	if !strings.Contains(ansi.Strip(status), " claude default 96% ") || strings.Contains(ansi.Strip(status), "use ") {
		t.Fatalf("status %q", ansi.Strip(status))
	}

	clickStatus(m, "claude default")

	if !m.shown(viewUsage) {
		t.Fatal("a click does not show the tab")
	}

	m.onUsage(proto.Usage{Accounts: m.usage.Accounts, Offer: map[string][]string{"claude": workEnv}})

	if status, _ = m.statusLine(m.w); !strings.Contains(ansi.Strip(status), " claude default 96% use .claude-work ") {
		t.Fatalf("status with an offer %q", ansi.Strip(status))
	}

	clickStatus(m, "use .claude-work")

	if !slices.Equal(m.st.Active["claude"], workEnv) {
		t.Fatalf("active %v", m.st.Active)
	}

	// A profile dropped from the config since: sessions start on the default,
	// and the chip and the tab say so.
	m.st.Active["claude"] = []string{"CLAUDE_CONFIG_DIR=/gone"}

	if status, _ = m.statusLine(m.w); !strings.Contains(ansi.Strip(status), " claude default 96% ") || !m.active(m.usage.Accounts[0]) {
		t.Fatalf("status with a profile gone %q", ansi.Strip(status))
	}
}

// TestProfileRoom holds the pickers that name a profile: each says how spent
// it is, a new session's opens on the active one, and a session's move lists
// the others.
func TestProfileRoom(t *testing.T) {
	m := usageModel(t)
	m.st.Active = map[string][]string{"claude": workEnv}

	if !m.profilePicker(m.ws, "claude", nil) {
		t.Fatal("no choice of profile")
	}

	items := m.modal.items

	// The footer says what runs where, the settings that tell profiles apart
	// in the accent color.
	if ansi.Strip(items[1].detail) != m.ws+" ❯ "+workEnv[0]+" claude" || !strings.Contains(items[1].detail, fgParams(pal.headerAccent)+"m"+workEnv[0]) {
		t.Fatalf("the work profile's detail %q", items[1].detail)
	}

	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "├") || !strings.Contains(v, ansi.Strip(items[1].detail)) {
		t.Fatal("the footer does not say what the selected profile runs")
	}

	// A profile is Usage's block: its name over its plan beside a row per
	// window, 5h over 7d, the meters and shares lined up, Fable's own limit
	// a ┃ on the 7d meter, a blank between two profiles. Work's stale limits
	// are not drawn, why instead, and ● marks the active one. The box ends
	// with the last profile's rows.
	lines := strings.Split(v, "\n")
	cell := func(s, sub string) int {
		before, _, _ := strings.Cut(s, sub)
		return ansi.StringWidth(before)
	}

	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "default  5h ") })
	if at < 0 || at+5 >= len(lines) {
		t.Fatalf("no default block:\n%s", v)
	}

	top := lines[slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╭") })]
	box := func(l string) string { return ansi.Cut(l, cell(top, "╭")+1, cell(top, "╮")) } // the line inside the frame

	h5, d7, gap, work, plan, rule := box(lines[at]), box(lines[at+1]), box(lines[at+2]), box(lines[at+3]), box(lines[at+4]), lines[at+5]
	if !strings.Contains(h5, " 40%    2d 2h") || !strings.Contains(d7, "Max 5x   7d ▰") || !strings.Contains(d7, " 96%    2d 2h") || strings.Count(d7, "┃") != 1 ||
		cell(h5, "▰") != cell(d7, "▰") || cell(h5, "%") != cell(d7, "%") ||
		strings.TrimSpace(gap) != "" ||
		!strings.Contains(work, "● work     paused · its sign-in lapsed") || strings.Contains(work+plan, "%") ||
		strings.TrimSpace(plan) != "Team 5x" || !strings.HasPrefix(strings.TrimSpace(rule), "├") {
		t.Fatalf("the profiles' blocks:\n%s", strings.Join([]string{h5, d7, gap, work, plan, rule}, "\n"))
	}

	// Both of work's rows are its own: the cursor on it, they are drawn
	// selected, and a click on its plan's row picks it.
	md := m.modal
	if sel := md.l.sel; md.disp[sel].label != "work" || !md.disp[sel+1].part || md.owner(sel+1) != sel {
		t.Fatalf("work's plan row is not part of it: %+v", md.disp)
	}

	checkWidths(t, m)

	if items[0].lead || !items[1].lead {
		t.Fatalf("the active profile is not the one selected: %+v", items)
	}

	// A profile with no config of its own spends the default's plan.
	m.st.ResumeEnv = map[string][]string{"claude": {"CLAUDE_CONFIG_DIR"}}

	if a, ok := m.spends("claude", []string{"ANTHROPIC_MODEL=x"}); !ok || a.Name != "default" {
		t.Fatalf("a profile on the default's sign-in spends %+v", a)
	}

	if a, ok := m.spends("claude", []string{"CLAUDE_CONFIG_DIR=/elsewhere"}); ok {
		t.Fatalf("a config nothing is known of spends %+v", a)
	}

	m.modal = nil
	m.st.ResumeID = map[string][]string{"claude": {"claude", "--resume", "{id}"}}
	m.sessions[0].Conversation = &proto.Conversation{Agent: "claude", ID: "c1", Env: workEnv}

	id := m.sessions[0].ID
	m.moveProfile(id)

	if m.modal == nil || len(m.modal.items) != 1 || m.modal.items[0].label != "default" {
		t.Fatalf("move picker %+v", m.modal)
	}

	// A profile is told by all it sets, not by its config alone: one that
	// sets no config is not the default, and a session in it can go there.
	m.st.Settings.Profiles = map[string]map[string][]string{"claude": {"fast": {"ANTHROPIC_MODEL=x"}}}
	m.st.ResumeEnv = map[string][]string{"claude": {"CLAUDE_CONFIG_DIR"}}
	m.sessions[0].Env = []string{"TERM=x", "ANTHROPIC_MODEL=x"}
	m.sessions[0].Conversation = &proto.Conversation{Agent: "claude", ID: "c1"}
	m.moveProfile(id)

	labels := make([]string, len(m.modal.items))
	for i, it := range m.modal.items {
		labels[i] = it.label
	}

	if want := []string{"default", "work"}; !slices.Equal(labels, want) {
		t.Fatalf("from fast the picker offers %q, want %q", labels, want)
	}

	m.modal = nil
	m.sessions[0].Conversation = nil

	m.moveProfile(id)

	if m.modal != nil || m.canMove(m.session(id)) {
		t.Fatal("a session with no conversation known can be moved")
	}
}

func TestUntil(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	for d, want := range map[time.Duration]string{
		76 * time.Hour:                 "3d 4h",
		130 * time.Minute:              "2h 10m",
		45*time.Minute + 59e9:          "45m",
		-time.Minute:                   "0m",
		24*time.Hour - time.Nanosecond: "23h 59m",
	} {
		if got := until(now.Add(d), now); got != want {
			t.Errorf("until %v = %q, want %q", d, got, want)
		}
	}

	if got := until(time.Time{}, now); got != "" {
		t.Errorf("no reset reads %q", got)
	}
}

func TestLimitMeter(t *testing.T) {
	for _, c := range []struct {
		got, want string
	}{
		{segsText(limitMeter(8, 0, nil, dim)), "▱▱▱▱▱▱▱▱"},
		{segsText(limitMeter(8, 2, nil, dim)), "▰▱▱▱▱▱▱▱"}, // any use shows
		{segsText(limitMeter(8, 50, nil, dim)), "▰▰▰▰▱▱▱▱"},
		{segsText(limitMeter(8, 56, nil, dim)), "▰▰▰▰▱▱▱▱"},
		{segsText(limitMeter(8, 100, nil, dim)), "▰▰▰▰▰▰▰▰"},
	} {
		if c.got != c.want {
			t.Errorf("meter %q, want %q", c.got, c.want)
		}
	}

	// A tick is a ┃ colored as its own share is, one at 0% none.
	ticked := limitMeter(8, 25, []proto.Limit{{Percent: 100}, {Percent: 0}, {Percent: 50}}, dim)
	if segsText(ticked) != "▰▰▱▱┃▱▱┃" || ticked[7].st.GetForeground() != pal.errc || ticked[4].st.GetForeground() != plain.GetForeground() {
		t.Errorf("ticks %q", segsText(ticked))
	}

	// The track is tinted where the fill would turn the warning and the error
	// color; the fill takes them once it is that far.
	low, high := limitMeter(8, 25, nil, dim), limitMeter(8, 96, nil, dim)
	if low[0].st.GetForeground() != dim.GetForeground() || low[5].st.GetForeground() != pal.indentGuide ||
		low[6].st.GetForeground() != meterTrack[1].GetForeground() || low[7].st.GetForeground() != meterTrack[2].GetForeground() ||
		high[0].st.GetForeground() != pal.errc {
		t.Errorf("meter colors %v %v %v %v, full %v", low[0].st.GetForeground(), low[5].st.GetForeground(), low[6].st.GetForeground(), low[7].st.GetForeground(), high[0].st.GetForeground())
	}
}

// TestRoomWindows holds the picker's columns, a window each, and what a
// window's meter shows: the limit every model draws from, else the fullest
// of the models' own, agy's groups, the others its ticks.
func TestRoomWindows(t *testing.T) {
	groups := []proto.Limit{{Window: "5h", Model: "Gemini", Percent: 12}, {Window: "5h", Model: "Claude + GPT", Percent: 81}, {Window: "7d", Model: "Gemini", Percent: 30}}

	if got := roomWindows([]proto.Account{{Limits: groups}, {Limits: []proto.Limit{{Window: "1mo"}}}}); !slices.Equal(got, []string{"5h", "7d", "1mo"}) {
		t.Fatalf("windows %q", got)
	}

	shown, ticks := windowLimit(groups, "5h")
	if shown.Model != "Claude + GPT" || len(ticks) != 1 || ticks[0].Model != "Gemini" {
		t.Fatalf("5h shows %+v, ticks %+v", shown, ticks)
	}

	if shown, _ := windowLimit(groups, "1mo"); shown != (proto.Limit{}) {
		t.Fatal("a window with no limit shows one")
	}
}

// segsText is the text of segments, their styles dropped.
func segsText(ss []seg) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.s)
	}

	return b.String()
}

// TestUsageTabsBesideBar is the header beside a side activity bar: it is
// drawn narrower than its column, and a click lands on the tab drawn there.
func TestUsageTabsBesideBar(t *testing.T) {
	m := usageModel(t)
	m.hasCustom = true
	m.st.Settings.ActBar = "side"
	m.st.Settings.Right = proto.Columns{{Views: []string{"agents", "search"}, Width: 30}}
	m.resize()

	s := m.colOf(viewAgents)
	rc, bw := m.colRect(s), m.barW(s)

	if bw == 0 || m.side(s) != 1 {
		t.Fatalf("column %d: bar %d, side %d", s, bw, m.side(s))
	}

	top := bodyY(t, m, "  Usage")

	for _, tb := range m.headTabs(viewAgents, rc.w-bw) {
		click(m, rc.x+tb.x, top, tea.MouseLeft) // its first cell: the tabs of a wider header start further right

		if !m.shown(tb.v) {
			t.Fatalf("a click on %q at %d shows another tab", tb.label, tb.x)
		}
	}
}

// TestHeadTabHover is the mouse over the header's tabs: the rule under the
// one it is on is tinted, at that tab's cells, and the shown one stays as it
// is.
func TestHeadTabHover(t *testing.T) {
	m := usageModel(t)
	m.hasCustom = true
	m.st.Settings.ActBar = "side"
	m.st.Settings.Right = proto.Columns{{Views: []string{"agents", "search"}, Width: 30}}
	m.resize()

	s := m.colOf(viewAgents)
	rc := m.colRect(s)
	top := bodyY(t, m, "  Usage")

	for _, tb := range m.headTabs(viewAgents, rc.w-m.barW(s)) {
		m.Update(tea.MouseMotionMsg{X: rc.x + tb.x, Y: top})

		rule := strings.Split(m.View().Content, "\n")[top+1]
		before, _, hot := strings.Cut(rule, "\x1b["+fgParams(pal.sashHover))

		switch {
		case tb.v == viewAgents && hot:
			t.Fatal("the shown tab lights under the mouse")
		case tb.v != viewAgents && (!hot || ansi.StringWidth(before) != rc.x+tb.x):
			t.Fatalf("%q at %d: tinted %v, after %d cells", tb.label, rc.x+tb.x, hot, ansi.StringWidth(before))
		}
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + 1, Y: top + 3})

	if rule := strings.Split(m.View().Content, "\n")[top+1]; strings.Contains(rule, fgParams(pal.sashHover)) {
		t.Fatal("the tint stays once the mouse left the header")
	}
}

// TestUsageUseTip is the mouse on an account's "use": it says what a click
// does, and nothing beside it does; on a meter with a model's ┃, it names it.
func TestUsageUseTip(t *testing.T) {
	m := usageModel(t)
	press(m, "9")

	s := m.colOf(viewUsage)
	rc, y := m.colRect(s), bodyY(t, m, ".claude-work")
	right := rc.x + rc.w - 1 - ifElse(m.side(s) == 0, 0, m.barW(s))

	m.Update(tea.MouseMotionMsg{X: right - 1, Y: y})
	m.View()

	if want := "Start New claude Sessions as .claude-work  ⏎"; m.tip != want {
		t.Fatalf("tip %q, want %q", m.tip, want)
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + 6, Y: y})
	m.View()

	if m.tip != "" {
		t.Fatalf("the account's name says %q", m.tip)
	}

	m.Update(tea.MouseMotionMsg{X: right - 1, Y: bodyY(t, m, "default")})
	m.View()

	if m.tip != "" {
		t.Fatalf("the active account says %q", m.tip)
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + 6, Y: bodyY(t, m, " 96% ")})
	m.View()

	if want := "┃ Fable's Own 7d Limit  18%"; m.tip != want {
		t.Fatalf("the weekly meter says %q, want %q", m.tip, want)
	}
}

// TestProfileShown: a session off its agent's default says which profile it
// runs in, in its row and its frame's title; one on the default says nothing.
func TestProfileShown(t *testing.T) {
	m := usageModel(t)
	s := &m.sessions[0]
	s.Conversation = &proto.Conversation{Agent: "claude", ID: "c1"}
	m.sess = s.ID

	if got := m.profileOf(s); got != "" || strings.Contains(m.mainTitle(), "claude-work") {
		t.Fatalf("on the default it says %q, title %q", got, m.mainTitle())
	}

	if got := m.harnessOf(s); got != "claude · default" {
		t.Fatalf("on the default its tip says %q", got)
	}

	s.Env, m.profileOn = append(s.Env, workEnv...), nil // as a sessions message does
	m.st.Settings.Right, m.st.Settings.WidthR = proto.Columns{{Views: []string{"agents"}}}, 47
	m.resize()

	if got := m.profileOf(s); got != "work" {
		t.Fatalf("in a config no profile names it says %q", got)
	}

	if got := m.harnessOf(s); ansi.Strip(got) != "claude work" || got == ansi.Strip(got) {
		t.Fatalf("its tip says %q, not in the row's color", got)
	}

	if !strings.HasSuffix(m.mainTitle(), " · work") {
		t.Fatalf("title %q", m.mainTitle())
	}

	if !strings.Contains(ansi.Strip(m.View().Content), "work  "+sessionName(*s)+" ") { // the profile, a blank, the name on its plate
		t.Fatal("the session's row does not say its profile on a chip before its name")
	}

	// profile_initial, or a column a whole profile would leave a name too
	// little room in, puts its first letter on the chip.
	m.st.Settings.Initial = true

	if !strings.Contains(ansi.Strip(m.View().Content), " w  "+sessionName(*s)+" ") {
		t.Fatal("profile_initial does not cut the chip to a letter")
	}

	m.st.Settings.Initial, m.st.Settings.WidthR = false, 32
	m.resize()

	if !strings.Contains(ansi.Strip(m.View().Content), " w  "+sessionName(*s)+" ") {
		t.Fatal("a narrow column keeps the whole profile")
	}

	// A session on its agent's default names it in the column: the names line up.
	other := *s
	other.ID, other.Name, other.Env = "s2", "other", nil
	m.sessions, m.profileOn = append(m.sessions, other), nil
	m.st.Settings.WidthR = 47
	m.resize()

	at := map[string]int{}

	for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		for _, name := range []string{sessionName(*s), "other"} {
			if i := strings.Index(l, " "+name+" "); i >= 0 {
				at[name] = ansi.StringWidth(l[:i])
			}
		}
	}

	if at["other"]+1 != at[sessionName(*s)] { // the plate's blank where the other's name starts
		t.Fatalf("names at %v", at)
	}

	if !strings.Contains(ansi.Strip(m.View().Content), " default other ") {
		t.Fatal("the session on the default does not say default")
	}

	s = &m.sessions[0]

	m.st.Settings.Profiles, m.profileOn = map[string]map[string][]string{"claude": {"work": workEnv}}, nil

	if got := m.profileOf(s); got != "work" {
		t.Fatalf("in profile work it says %q", got)
	}

	checkWidths(t, m)
}

// TestUsageDragReorder drags an account among its agent's: the rows stay put
// while a band marks the drop, the release moves it and saves the order,
// another agent's rows take no drop, and alt+↑↓ moves it within its agent.
func TestUsageDragReorder(t *testing.T) {
	m := usageModel(t)
	m.showView(viewUsage)

	cs, _ := m.layout()
	x := cs[m.colOf(viewUsage)].x + 1
	from, to := bodyY(t, m, ".claude-work · Team"), bodyY(t, m, "default · Max 5x")
	names := func() []string {
		out := make([]string, len(m.usage.Accounts))
		for i, a := range m.usage.Accounts {
			out[i] = a.Agent + ":" + a.Name
		}

		return out
	}
	original := names()

	m.Update(tea.MouseClickMsg{X: x, Y: from, Button: tea.MouseLeft})

	if m.drag == nil || m.drag.kind != dragProfile || m.drag.col != 1 {
		t.Fatalf("a press on an account starts a drag: %+v", m.drag)
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: bodyY(t, m, "Codex · Free"), Button: tea.MouseLeft})

	if m.drag.slot != -1 {
		t.Fatalf("another agent's row takes a drop: %d", m.drag.slot)
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: to, Button: tea.MouseLeft})

	if m.drag.slot != 0 || !slices.Equal(names(), original) || m.us.dropSlot(m, m.us.rows(m)) != 1 {
		t.Fatalf("the rows stay put while the drop is marked: slot %d, %v", m.drag.slot, names())
	}

	checkWidths(t, m)

	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: to, Button: tea.MouseLeft})
	if want := []string{"claude:.claude-work", "claude:default", "codex:default"}; cmd == nil || m.drag != nil || !slices.Equal(names(), want) {
		t.Fatalf("the release saves the order: cmd %v, drag %+v, %v", cmd != nil, m.drag, names())
	}

	if r := m.us.selected(m); r == nil || !r.head || r.acct.Name != ".claude-work" {
		t.Fatalf("the moved account stays selected: %+v", r)
	}

	press(m, "alt+up")

	if names()[0] != "claude:.claude-work" {
		t.Fatalf("alt+up past the first: %v", names())
	}

	press(m, "alt+down", "alt+down")

	if !slices.Equal(names(), original) {
		t.Fatalf("alt+down stays among its agent's: %v", names())
	}
}

// TestProfileChip holds a profile's chip on a session's row: its color by
// its place among its agent's accounts in Usage, so two profiles differ and
// a reorder moves the color.
func TestProfileChip(t *testing.T) {
	m := usageModel(t)
	rocket := []string{"CLAUDE_CONFIG_DIR=/home/u/.claude-rocket"}
	m.usage.Accounts = append(m.usage.Accounts, proto.Account{Agent: "claude", Name: "rocket", Env: rocket})

	work := m.profileColor("claude", ".claude-work")
	if work == m.profileColor("claude", "rocket") {
		t.Fatal("two profiles share a color")
	}

	m.reorderProfile(3, 1)

	if m.profileColor("claude", "rocket") != work {
		t.Fatal("the color does not follow the order in Usage")
	}
}

// TestProfileRenamed holds a session's profile by the name Usage gave it,
// the default's too.
func TestProfileRenamed(t *testing.T) {
	m := usageModel(t)
	s := &m.sessions[0]
	s.Conversation, s.Env = &proto.Conversation{Agent: "claude", ID: "c1"}, workEnv
	m.st.Names = map[string]map[string]string{"claude": {workEnv[0]: "stengl", "": "personal"}}

	if got := m.profileOf(s); got != "stengl" {
		t.Fatalf("renamed, it says %q", got)
	}

	s.Env, m.profileOn = nil, nil

	if got := m.harnessOf(s); got != "claude · personal" {
		t.Fatalf("on the renamed default its tip says %q", got)
	}
}

// TestProfileWidths holds the column of profiles before sessions' names: as
// wide as the longest profile among the sessions listed together, "default"
// included, none for a run of sessions on their agent's default.
func TestProfileWidths(t *testing.T) {
	m := usageModel(t)
	work := m.sessions[0]
	work.Conversation, work.Env = &proto.Conversation{Agent: "claude", ID: "c1"}, workEnv
	other := work
	other.ID, other.Env = "s2", nil

	rows := []agRow{
		{kind: agProject},
		{kind: agSession, s: work},
		{kind: agSub},
		{kind: agSession, s: other},
		{kind: agProject},
		{kind: agSession, s: other},
	}

	if got := m.ag.profileWidths(m, rows, 60); !slices.Equal(got, []int{0, 7, 7, 7, 0, 0}) {
		t.Fatalf("widths %v", got)
	}
}
