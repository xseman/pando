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

// TestUsageView holds what the tab draws and does: an agent's accounts with a
// row per window, a model's own allowance a mark on the window it shares, a
// stale account dim under its reason, an only account on its agent's row;
// ⏎ makes an account the one new sessions start with.
func TestUsageView(t *testing.T) {
	m := usageModel(t)
	m.showView(viewUsage)

	out := checkWidths(t, m)
	for _, want := range []string{
		"Sessions  Usage", "Claude Code", "default · Max 5x", "active", ".claude-work · Team 5x", "use",
		" 40% ", " 96% ", "2d 2h", "paused: its sign-in lapsed", "Codex · Free", "30d ",
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

	if want := []int{usAgent, usAccount, usLimit, usLimit, usAccount, usLimit, usNote, usAgent, usLimit}; !slices.Equal(kinds, want) {
		t.Fatalf("rows %v, want %v", kinds, want)
	}

	if !slices.Equal(rows[3].ticks, []float64{18}) || !rows[7].solo || rows[0].solo {
		t.Fatalf("7d row %+v, codex %+v", rows[3], rows[7])
	}

	if week := strings.Split(out, "\n")[bodyY(t, m, " 96% ")]; !strings.Contains(week, "┃") || !strings.Contains(week, "━") {
		t.Fatalf("the weekly meter: %q", week)
	}

	// The account under the cursor, from any of its rows.
	press(m, "down", "down", "down", "down", "down", "enter")

	if !slices.Equal(m.st.Active["claude"], workEnv) {
		t.Fatalf("active %v", m.st.Active)
	}

	if out := checkWidths(t, m); strings.Count(out, "active") != 1 || !strings.Contains(strings.Split(out, "\n")[bodyY(t, m, ".claude-work")], "active") {
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

	if got := m.us.rows(m); len(got) != 4 || got[1].acct.Name != ".claude-work" {
		t.Fatalf("filtered rows %+v", got)
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
	if len(items) != 2 || !strings.HasPrefix(items[0].inline, "96% · 2d 2h") || items[1].inline != "30% · paused" {
		t.Fatalf("items %+v", items)
	}

	if items[0].lead || !items[1].lead {
		t.Fatalf("the active profile is not the one selected: %+v", items)
	}

	// A profile with no config of its own spends the default's plan.
	m.st.ResumeEnv = map[string][]string{"claude": {"CLAUDE_CONFIG_DIR"}}

	if got := m.room("claude", []string{"ANTHROPIC_MODEL=x"}); !strings.HasPrefix(got, "96%") {
		t.Fatalf("a profile on the default's sign-in says %q", got)
	}

	if got := m.room("claude", []string{"CLAUDE_CONFIG_DIR=/elsewhere"}); got != "" {
		t.Fatalf("a config nothing is known of says %q", got)
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

	if want := []string{"default", strings.Join(workEnv, " ")}; !slices.Equal(labels, want) {
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
	text := func(pct float64, ticks ...float64) string {
		var b strings.Builder
		for _, s := range limitMeter(8, pct, ticks, plain) {
			b.WriteString(s.s)
		}

		return b.String()
	}

	for _, c := range []struct {
		got, want string
	}{
		{text(0), "────────"},
		{text(50), "━━━━────"},
		{text(56), "━━━━╾───"},
		{text(100), "━━━━━━━━"},
		{text(25, 100, 0), "┃━─────┃"},
	} {
		if c.got != c.want {
			t.Errorf("meter %q, want %q", c.got, c.want)
		}
	}
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
