package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xseman/pando/internal/proto"
)

// TestMoveTab moves the focused strip's tab with ⌃⇧pgup ⌃⇧pgdn, wrapping
// at either end as herdr's move_tab_previous and move_tab_next do: the
// Terminal's shell with the keyboard in the panel, the session's tab with a
// session in view, which keeps its own tab first, else the active editor.
func TestMoveTab(t *testing.T) {
	m := testModelSized(t, 120, 24)
	drainInputs(m)

	m.focus = onMain
	for _, f := range []string{"README.md", ".env"} {
		fire(m, m.openFile(filepath.Join(m.ws, f)))
	}

	names := func() string {
		var out []string
		for _, e := range m.editors {
			out = append(out, filepath.Base(e.path))
		}

		return strings.Join(out, " ")
	}

	press(m, "ctrl+shift+pgdown")

	if got := names(); got != ".env README.md" || m.edIdx != 0 || !strings.HasSuffix(m.pv.path, ".env") {
		t.Fatalf("⌃⇧pgdn wraps the last editor to the front, still active: %s, edIdx %d", got, m.edIdx)
	}

	if e := m.st.Editors[m.ws]; e.Active != 0 || len(e.Open) != 2 {
		t.Fatalf("the new order is saved: %+v", e)
	}

	press(m, "ctrl+shift+pgup")

	if got := names(); got != "README.md .env" || m.edIdx != 1 {
		t.Fatalf("⌃⇧pgup wraps the first editor to the end: %s, edIdx %d", got, m.edIdx)
	}

	if !slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == "view.moveTabLeft" }) {
		t.Fatal("Move Tab Left is a command [keys] can bind")
	}

	tab := func(id string) proto.Session {
		return proto.Session{SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: tabAgent, Parent: "s1"}, Status: "idle"}
	}

	shell := func(id string) proto.Session {
		s := termSession(m.ws, id)
		s.Parent = "s1"

		return s
	}

	m.Update(sessionsMsg{m.sessions[0], tab("t1"), tab("t2"), shell("p1"), shell("p2")})
	m.Update(focusSessionMsg("s1"))

	ids := func(ss []proto.Session) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}

		return strings.Join(out, " ")
	}

	press(m, "ctrl+shift+pgdown")

	if got := ids(m.tabsOf("s1")); got != "s1 t1 t2" {
		t.Fatalf("the session's own tab does not move: %s", got)
	}

	m.Update(focusSessionMsg("t1"))
	m.focus = onMain
	press(m, "ctrl+shift+pgup")

	if got := ids(m.tabsOf("s1")); got != "s1 t2 t1" {
		t.Fatalf("⌃⇧pgup wraps the first tab past the last, behind the session's own: %s", got)
	}

	m.tv.id = "p1"
	m.st.Settings.TermOpen, m.st.Settings.TermPos, m.st.Settings.TermH = true, "bottom", 8
	m.resize()
	m.focus = onPanel
	press(m, "ctrl+shift+pgdown")

	if got := ids(m.termSessions()); got != "p2 p1" || m.tv.id != "p1" {
		t.Fatalf("⌃⇧pgdn in the Terminal moves its shell: %s, showing %s", got, m.tv.id)
	}
}
