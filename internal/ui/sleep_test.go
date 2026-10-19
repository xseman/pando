package ui

import (
	"slices"
	"strings"
	"testing"
)

// TestAsleepSession: Spaces marks an asleep session ◌ and the Filter can
// leave it out; z puts a session to sleep, asking first while it works;
// opening one, or a key typed into it, wakes it once, and the strip says so.
func TestAsleepSession(t *testing.T) {
	m := testModelSized(t, 120, 30)
	m.sessions[0].Name = "napper"
	m.sessions[0].Status = "asleep"
	m.st.Settings.Width = 40
	m.showView(viewAgents)

	out := checkWidths(t, m)
	if !strings.Contains(out, "◌ napper") || !strings.Contains(out, "asleep") {
		t.Fatalf("Spaces:\n%s", out)
	}

	m.st.Settings.SpHide = []string{"asleep"}
	if out := checkWidths(t, m); strings.Contains(out, "napper") {
		t.Fatalf("the Filter keeps an asleep session:\n%s", out)
	}

	m.st.Settings.SpHide = nil

	// Asleep: nothing to put to sleep, so no z and no menu item.
	m.ag.l.sel = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.kind == agSession })
	if cmd := m.ag.key(m, keyMsg("z")); cmd != nil || slices.Contains(itemLabels(m.ag.items(m)), "Sleep Session") {
		t.Fatal("an asleep session offers to sleep")
	}

	// Idle: z sends it at once; working, it asks.
	m.sessions[0].Status = "idle"
	if !slices.Contains(itemLabels(m.ag.items(m)), "Sleep Session") {
		t.Fatalf("menu %q", itemLabels(m.ag.items(m)))
	}

	cmd := m.ag.key(m, keyMsg("z"))
	if cmd == nil || m.modal != nil {
		t.Fatalf("z on an idle session: %v, modal %v", cmd, m.modal)
	}

	if msg, ok := cmd().(flashMsg); !ok || !strings.Contains(msg.text, "session.sleep") { // no daemon in a test: the call it made fails
		t.Fatalf("z sent %#v", msg)
	}

	m.sessions[0].Status = "running"
	if cmd := m.ag.key(m, keyMsg("z")); cmd != nil || m.modal == nil || !strings.Contains(m.modal.title, "working") {
		t.Fatalf("z on a working session asks first: %v", m.modal)
	}

	m.modal = nil

	// Opening it wakes it, once; the strip says so until the answer.
	m.sessions[0].Status = "asleep"
	m.switchSession("s1")

	if !m.waking["s1"] {
		t.Fatal("opening an asleep session does not wake it")
	}

	if cmd := m.wake("s1"); cmd != nil {
		t.Fatal("a second wake while one is on its way")
	}

	if hint, _ := m.sessionHint(); !strings.Contains(hint, "waking") {
		t.Fatalf("hint while waking %q", hint)
	}

	m.Update(wokeMsg{id: "s1"})

	if m.waking["s1"] {
		t.Fatal("the answer leaves it waking")
	}

	if hint, _ := m.sessionHint(); !strings.Contains(hint, "asleep") {
		t.Fatalf("hint asleep %q", hint)
	}

	// A key typed into it wakes it instead of reaching a process not there.
	if cmd := m.term.key(m, "s1", keyMsg("x")); cmd == nil || !m.waking["s1"] || len(m.inputs) != 0 {
		t.Fatalf("a key into an asleep session: %v, waking %v, %d inputs", cmd, m.waking, len(m.inputs))
	}

	// Its process is back but its agent not yet: keys typed meanwhile would
	// land ahead of the command that resumes it.
	m.sessions[0].Status = "running"
	m.term.key(m, "s1", keyMsg("y"))

	if len(m.inputs) != 0 {
		t.Fatalf("a key while waking reached the session: %d inputs", len(m.inputs))
	}
}

// itemLabels are the labels of menu items.
func itemLabels(items []item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.label)
	}

	return out
}

// TestShownSessionStaysViewed: with sleep_after on, the session on screen is
// asked for every 30 s though it prints nothing, so the daemon counts it
// viewed and does not put it to sleep under the person reading it.
func TestShownSessionStaysViewed(t *testing.T) {
	m := testModelSized(t, 120, 30)
	m.focus = onMain
	m.switchSession("s1")

	for _, sleep := range []int{0, 30} {
		m.st.Settings.Sleep = sleep
		m.term.fetching, m.tv.fetching = false, false

		for range 15 {
			m.Update(tickMsg{})
		}

		if m.term.fetching != (sleep > 0) {
			t.Fatalf("sleep_after %d: the shown session asked for: %v", sleep, m.term.fetching)
		}
	}
}
