package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

func TestEditorStripAndHistory(t *testing.T) {
	m := testModelSized(t, 100, 24)
	m.focus = onMain
	many := filepath.Join(m.ws, "many.go")
	mustWrite(t, many, strings.Repeat("line\n", 10))
	fire(m, m.openFile(filepath.Join(m.ws, "README.md")))

	if m.stripH() != 1 {
		t.Fatal("a single editor still has its strip")
	}

	fire(m, m.openFile(many))

	if len(m.editors) != 2 || m.edIdx != 1 || m.stripH() != 1 {
		t.Fatalf("editors=%d active=%d strip=%d", len(m.editors), m.edIdx, m.stripH())
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if strip := ansi.Cut(out[0], m.mainX(), m.w); !strings.Contains(strip, "README.md") || !strings.Contains(strip, "many.go ✕") {
		t.Fatalf("strip %q", strip)
	}

	// Each editor keeps its own cursor.
	press(m, "down", "down", "down")
	send(m, keyMsg("ctrl+tab"))

	if !strings.HasSuffix(m.pv.path, "README.md") || m.pv.cur.line != 0 {
		t.Fatalf("ctrl+tab: %s at %+v", m.pv.path, m.pv.cur)
	}

	send(m, keyMsg("ctrl+tab"))

	if !strings.HasSuffix(m.pv.path, "many.go") || m.pv.cur.line != 3 {
		t.Fatalf("back to many.go at %+v", m.pv.cur)
	}

	// History walks the visited editors, clicking a tab switches.
	send(m, keyMsg("alt+,"))

	if !strings.HasSuffix(m.pv.path, "README.md") {
		t.Fatalf("alt+, went to %s", m.pv.path)
	}

	send(m, keyMsg("alt+."))

	if !strings.HasSuffix(m.pv.path, "many.go") {
		t.Fatalf("alt+. went to %s", m.pv.path)
	}

	click(m, m.mainX()+2, 0, tea.MouseLeft)

	if m.edIdx != 0 {
		t.Fatalf("clicking a tab activates it: %d", m.edIdx)
	}

	checkWidths(t, m)

	// ctrl+w closes the active editor, then the preview.
	m.focus = onMain
	send(m, keyMsg("ctrl+w"))

	if len(m.editors) != 1 || !strings.HasSuffix(m.pv.path, "many.go") {
		t.Fatalf("after close: %d editors, %s", len(m.editors), m.pv.path)
	}

	send(m, keyMsg("ctrl+w"))

	if m.preview || len(m.editors) != 0 {
		t.Fatalf("last close ends the preview: %v %d", m.preview, len(m.editors))
	}
}

func TestGoToLine(t *testing.T) {
	m, _ := editorModel(t, "a.go", "one\n\ttwo\nthree\nfour\nfive\n")
	m.pv.setCursor(m, pos{4, 2})
	press(m, "ctrl+g")

	if m.modal == nil || m.modal.title != "Go to Line" || m.modal.input.Value() != ":" {
		t.Fatalf("ctrl+g opens the : picker: %+v", m.modal)
	}

	if rows := labels(m); len(rows) != 1 || !strings.Contains(rows[0], "Current line 5, character 3") || !strings.Contains(rows[0], "1 to 5") {
		t.Fatalf("empty query rows %q", rows)
	}

	if v, _, _ := m.modal.view(m); !strings.Contains(ansi.Strip(v), "Current line 5") {
		t.Fatalf("the hint row is not drawn:\n%s", ansi.Strip(v))
	}
	// The editor follows the number as it is typed; esc puts it back.
	press(m, "3")

	if m.pv.cur.line != 2 || m.modal == nil || m.pv.rangeHi != 3 {
		t.Fatalf("typing 3 shows line 3: %+v, highlighted %d", m.pv.cur, m.pv.rangeHi)
	}

	if !strings.Contains(m.View().Content, bgParams(pal.rangeHiBg)) {
		t.Fatal("the line Go to Line shows is not highlighted")
	}

	press(m, "backspace")

	if m.pv.cur != (pos{4, 2}) {
		t.Fatalf("erasing the number goes back: %+v", m.pv.cur)
	}

	press(m, "2", "esc")

	if m.modal != nil || m.pv.cur != (pos{4, 2}) || m.pv.rangeHi != 0 {
		t.Fatalf("esc restores: %+v", m.pv.cur)
	}
	// A character counts a tab as one, "," works like ":", past the end clamps.
	m.focus = 0 // a sidebar column: ctrl+g works from there too
	press(m, "ctrl+g", "2", ",", "2")

	if rows := labels(m); len(rows) != 1 || rows[0] != "Go to line 2, character 2" {
		t.Fatalf("rows %q", rows)
	}

	press(m, "enter")

	if m.modal != nil || m.pv.cur != (pos{1, 4}) || m.pv.anchor != nil || m.focus != onMain || m.pv.rangeHi != 0 {
		t.Fatalf("go to 2,2: %+v focus %v", m.pv.cur, m.focus)
	}

	press(m, "ctrl+g", "9", "9", "enter")

	if m.pv.cur.line != 4 {
		t.Fatalf("past the end: %+v", m.pv.cur)
	}
	// ":" in quick open is the same picker; "@" goes on to symbols, anything
	// else back to files.
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"a.go"}})
	press(m, ":")

	if m.modal == nil || m.modal.title != "Go to Line" || m.modal.input.Value() != ":" {
		t.Fatalf(": in quick open: %+v", m.modal)
	}

	press(m, "4", "enter")

	if m.pv.cur.line != 3 {
		t.Fatalf(":4 from quick open: %+v", m.pv.cur)
	}
	// A [keys] binding for ctrl+g still wins with a file open.
	m.st.Settings.Keys = map[string]string{"ctrl+g": "view.showSearch"}
	press(m, "ctrl+g")

	if m.modal != nil {
		t.Fatalf("[keys] ctrl+g was ignored: %s", m.modal.title)
	}

	m = previewModel(t, "file", "one", "two", "three", "four", "five")
	// Quick open takes a position after the name.
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"src/deep/x.go", "README.md"}})
	press(m, "x", ".", "g", "o", ":", "1", ":", "3")

	if len(m.modal.disp) != 1 {
		t.Fatalf("a position does not narrow the match: %d rows", len(m.modal.disp))
	}

	send(m, keyMsg("enter"))

	if !strings.HasSuffix(m.pv.path, "x.go") || m.pv.cur != (pos{0, 2}) {
		t.Fatalf("opened %s at %+v", m.pv.path, m.pv.cur)
	}
}

func TestEditorMiddleClickAndSuperArrows(t *testing.T) {
	m := testModelSized(t, 100, 24)

	m.focus = onMain
	for _, f := range []string{"README.md", ".env"} {
		fire(m, m.openFile(filepath.Join(m.ws, f)))
	}

	send(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})

	if !strings.HasSuffix(m.pv.path, "README.md") {
		t.Fatalf("super+left goes back: %s", m.pv.path)
	}

	send(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})

	if !strings.HasSuffix(m.pv.path, ".env") {
		t.Fatalf("super+right goes forward: %s", m.pv.path)
	}

	tabs := m.editorTabs(m.mainW())
	click(m, m.mainX()+tabs[0].x+1, 0, tea.MouseMiddle)

	if len(m.editors) != 1 || !strings.HasSuffix(m.editors[0].path, ".env") {
		t.Fatalf("middle click closes a tab: %d left", len(m.editors))
	}
}

// TestTabChips frames every tab of a strip as VS Code does: the active one
// in the selection's colors, the others on tab.inactiveBackground, each with
// tab.border's hairline after it. The hairline is a gap no click lands on.
func TestTabChips(t *testing.T) {
	m := testModelSized(t, 100, 24)
	m.focus = onMain

	for _, f := range []string{"README.md", ".env"} {
		fire(m, m.openFile(filepath.Join(m.ws, f)))
	}

	strip := m.editorStrip(m.mainW())
	if !strings.Contains(strip, bgParams(pal.tabBg)) || !strings.Contains(strip, bgParams(pal.selBg)) || strings.Count(ansi.Strip(strip), "▏") != 2 {
		t.Fatalf("strip = %q", strip)
	}

	tabs := m.editorTabs(m.mainW())
	if len(tabs) != 2 || tabs[1].x != tabs[0].x+tabs[0].w+tabGap {
		t.Fatalf("tabs sit a hairline apart: %+v", tabs)
	}

	gap := m.mainX() + tabs[0].x + tabs[0].w
	if got := ansi.Strip(strip); []rune(got)[tabs[0].x+tabs[0].w] != '▏' {
		t.Fatalf("the hairline is not in the gap: %q", got)
	}

	click(m, gap, 0, tea.MouseLeft)

	if m.edIdx != 1 {
		t.Fatalf("a click on the hairline switched to %d", m.edIdx)
	}

	click(m, m.mainX()+tabs[0].x+1, 0, tea.MouseLeft)

	if m.edIdx != 0 {
		t.Fatalf("a click on the first tab shows it: %d", m.edIdx)
	}

	// Session tabs, and the Terminal's, are framed the same way.
	ss := m.tabsFor(100, []proto.Session{
		{SessionSpec: proto.SessionSpec{ID: "a", Agent: "shell"}},
		{SessionSpec: proto.SessionSpec{ID: "b", Agent: "shell"}},
	}, "a")
	if ss[1].x != ss[0].w+tabGap || !strings.Contains(row(100, nil, tabSegs(ss)), bgParams(pal.tabBg)) {
		t.Fatalf("session tabs: %+v", ss)
	}

	// A strip too narrow for them all keeps the active tab: the ones before
	// it give way, and a name too long on its own is cut.
	long := []proto.Session{
		{SessionSpec: proto.SessionSpec{ID: "a", Agent: "shell", Name: "Store Count method"}},
		{SessionSpec: proto.SessionSpec{ID: "b", Agent: "shell", Name: "bash"}},
		{SessionSpec: proto.SessionSpec{ID: "c", Agent: "shell", Name: "an agent with a very long task name"}},
	}

	for _, active := range []string{"a", "b", "c"} {
		tabs := m.tabsFor(25, long, active)

		shown := slices.ContainsFunc(tabs, func(t sessTab) bool { return t.active && t.id == active })
		last := tabs[len(tabs)-1]

		if !shown || !last.plus || last.x+last.w > 25 {
			t.Fatalf("active %s in 25 columns: %+v", active, tabs)
		}
	}

	if got := m.tabsFor(25, long, "c"); !strings.Contains(got[0].label, "…") {
		t.Fatalf("the long name is cut: %q", got[0].label)
	}
}

func TestEditorsPersist(t *testing.T) {
	m := testModelSized(t, 100, 24)
	m.focus = onMain
	readme, many := filepath.Join(m.ws, "README.md"), filepath.Join(m.ws, "many.go")
	mustWrite(t, many, strings.Repeat("line\n", 10))
	fire(m, m.openFile(readme))
	fire(m, m.openFile(many))
	press(m, "down", "down", "down")

	if m.saveEditors() == nil {
		t.Fatal("changed editors save")
	}

	e := m.st.Editors[m.ws]
	if len(e.Open) != 2 || e.Active != 1 || e.Open[0].Path != readme || e.Open[1].Path != many || e.Open[1].Line != 3 {
		t.Fatalf("saved editors %+v", e)
	}

	if m.saveEditors() != nil {
		t.Fatal("unchanged editors do not save again")
	}

	send(m, keyMsg("ctrl+w"))

	if cmd := m.saveEditors(); cmd == nil || len(m.st.Editors[m.ws].Open) != 1 {
		t.Fatalf("closing a tab saves: %+v", m.st.Editors[m.ws])
	}

	// A new TUI on the workspace reopens them where they were, skipping a
	// file that is gone; with no session the active one shows at once.
	st := m.st
	st.Editors[m.ws] = proto.Editors{Open: []proto.Editor{
		{Path: readme}, {Path: filepath.Join(m.ws, "gone.go"), Line: 9}, {Path: many, Line: 3, Col: 1, Top: 1},
	}, Active: 2}
	m2 := New(st, m.wss, nil, m.ws, nil)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	if len(m2.editors) != 2 || m2.edIdx != 1 || !m2.preview || m2.pv.path != many || m2.pv.cur.line != 3 {
		t.Fatalf("restored %d editors, active %d, preview %v, %s at %+v", len(m2.editors), m2.edIdx, m2.preview, m2.pv.path, m2.pv.cur)
	}

	fire(m2, m2.pv.load(m2))

	if !m2.pv.ready || m2.pv.cur != (pos{3, 1}) || m2.pv.top != 1 {
		t.Fatalf("loaded: ready=%v cur=%+v top=%d", m2.pv.ready, m2.pv.cur, m2.pv.top)
	}

	out := strings.Split(checkWidths(t, m2), "\n")
	if strip := ansi.Cut(out[0], m2.mainX(), m2.w); !strings.Contains(strip, "README.md") || !strings.Contains(strip, "many.go ✕") {
		t.Fatalf("strip %q", strip)
	}

	if m2.saveEditors() == nil || len(m2.st.Editors[m.ws].Open) != 2 {
		t.Fatal("the pruned strip is saved back")
	}

	// With a session in the worktree the session shows; the tabs wait in the strip.
	ss := []proto.Session{{SessionSpec: proto.SessionSpec{ID: "s1", Workspace: m.ws, Agent: "shell"}, Status: "idle"}}
	m3 := New(st, m.wss, ss, m.ws, nil)
	m3.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	if len(m3.editors) != 2 || m3.preview || !m3.showsSession() {
		t.Fatalf("with a session: %d editors, preview %v", len(m3.editors), m3.preview)
	}

	send(m3, keyMsg("ctrl+tab"))

	if !m3.preview || m3.pv.path != readme {
		t.Fatalf("ctrl+tab brings a restored tab up: %v %s", m3.preview, m3.pv.path)
	}
}
