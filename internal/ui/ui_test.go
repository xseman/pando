package ui

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

func TestFuzzy(t *testing.T) {
	if got := fuzzy("xyz", "main.go"); got != -1 {
		t.Errorf("non-match scored %d, want -1", got)
	}

	if got := fuzzy("mg", "main.go"); got < 0 {
		t.Errorf("subsequence scored %d, want a match", got)
	}

	seg, spread := fuzzy("app", "internal/ui/app.go"), fuzzy("app", "a/p/p.go")
	if seg <= spread {
		t.Errorf("consecutive segment-start match scored %d, spread one %d; want higher", seg, spread)
	}
}

func TestList(t *testing.T) {
	l := list{sel: -1}
	l.move(1, 10, 3)

	if l.sel != 0 {
		t.Fatalf("first move selects row 0, got %d", l.sel)
	}

	l.move(5, 10, 3)

	if l.sel != 5 || l.top != 3 {
		t.Fatalf("snap: %+v", l)
	}

	l.wheel(100, 10, 3)

	if l.sel != 5 || l.top != 7 {
		t.Fatalf("wheel scrolls the view only: %+v", l)
	}

	if l.at(0, 10) != 7 || l.at(5, 10) != -1 {
		t.Fatalf("at: %d %d", l.at(0, 10), l.at(5, 10))
	}
}

// mustMkdir creates a directory tree, or fails the test where it went wrong.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// mustWrite writes a file and the directories above it, or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustRemove deletes a file, or fails the test; a missing file is a mistake in
// the test, not a pass.
func mustRemove(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// touch dates a file an hour into the future, so pando sees a foreign write.
func touch(t *testing.T, path string) {
	t.Helper()

	at := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// mustGit runs a git command in root, or fails the test naming the command.
func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()

	if _, err := git.Run(root, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

// mustRead reads a file back, or fails the test; tests assert on the content.
func mustRead(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(b)
}

// drainInputs consumes what the model sends to the daemon. The tests run no
// daemon, so without a reader a long key sequence would fill m.inputs and
// block Update.
func drainInputs(m *Model) {
	go func() {
		for range m.inputs {
			continue // the keystrokes have nowhere to go in a test
		}
	}()
}

func testModelSized(t *testing.T, w, h int) *Model {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "src", "deep", "x.go"), "package x\n")
	mustWrite(t, filepath.Join(root, "README.md"), "# hi\n")
	mustWrite(t, filepath.Join(root, ".env"), "A=1\n")
	st := proto.State{
		Settings: proto.Settings{Width: 30, Icons: "ascii", SessPos: "editor"}, Projects: []string{root},
		Agents: map[string][]string{"shell": {"sh"}},
	}
	wss := []proto.Workspace{{Path: root, Project: root, Branch: "main", Main: true}}
	ss := []proto.Session{{SessionSpec: proto.SessionSpec{ID: "s1", Workspace: root, Agent: "shell"}, Status: "idle"}}
	m := New(st, wss, ss, root, nil)
	m.sess = "" // nothing on screen: a test that wants the session opens it
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})

	return m
}

func testModel(t *testing.T) *Model {
	t.Helper()
	return testModelSized(t, 100, 20)
}

// Every rendered line must be exactly the window width, or columns shear.
func checkWidths(t *testing.T, m *Model) string {
	t.Helper()

	content := m.View().Content

	lines := strings.Split(content, "\n")
	if len(lines) != m.termH {
		t.Fatalf("view has %d lines, want %d", len(lines), m.termH)
	}

	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Fatalf("line %d width %d, want %d: %q", i, w, m.w, ansi.Strip(l))
		}
	}

	return ansi.Strip(content)
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
	"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd, "tab": tea.KeyTab, "space": tea.KeySpace,
	"f2": tea.KeyF2, "f3": tea.KeyF3, "f12": tea.KeyF12,
}

// keyMsg builds a key press from its string form, e.g. "shift+down", "ctrl+]", "x".
func keyMsg(s string) tea.KeyPressMsg {
	var mod tea.KeyMod

	for {
		if rest, ok := strings.CutPrefix(s, "ctrl+"); ok && rest != "" {
			mod, s = mod|tea.ModCtrl, rest
		} else if rest, ok := strings.CutPrefix(s, "shift+"); ok && rest != "" {
			mod, s = mod|tea.ModShift, rest
		} else if rest, ok := strings.CutPrefix(s, "alt+"); ok && rest != "" {
			mod, s = mod|tea.ModAlt, rest
		} else {
			break
		}
	}

	if c, ok := namedKeys[s]; ok {
		return tea.KeyPressMsg{Code: c, Mod: mod}
	}

	r, _ := utf8.DecodeRuneInString(s)
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}

	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

// tabAt is a screen column inside view v's activity chip.
func tabAt(m *Model, v view) int {
	s := m.colOf(v)
	for _, t := range m.tabs(s) {
		if t.v == v {
			return m.colRect(s).x + t.x + 1
		}
	}

	return -1
}

func click(m *Model, x, y int, b tea.MouseButton) {
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: b})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: b})
}

func TestKeyMsgStrings(t *testing.T) {
	for _, s := range []string{"shift+down", "ctrl+]", "ctrl+f", "enter", "x"} {
		if got := keyMsg(s).String(); got != s {
			t.Errorf("keyMsg(%q).String() = %q", s, got)
		}
	}
}

func TestExplorerAndLayout(t *testing.T) {
	m := testModel(t)

	out := checkWidths(t, m)
	if !strings.Contains(out, "README.md") || strings.Contains(out, ".env") {
		t.Fatalf("initial view:\n%s", out)
	}

	if m.ex.nodes[0].name != "src" {
		t.Fatalf("dirs must sort first: %+v", m.ex.nodes)
	}

	press(m, "j", "enter", "j", "l")

	if len(m.ex.nodes) != 4 || m.ex.nodes[2].name != "x.go" {
		t.Fatalf("expanded tree: %+v", m.ex.nodes)
	}

	press(m, "j", "h") // on x.go: h jumps to the parent dir

	if m.ex.selected().name != "deep" {
		t.Fatalf("h on a file selects its parent, got %s", m.ex.selected().name)
	}

	m.st.Settings.Hidden = true
	m.ex.rebuild(m)

	if !strings.Contains(checkWidths(t, m), ".env") {
		t.Fatal("hidden files toggle")
	}

	m.ex.reveal(m, filepath.Join(m.ws, "src", "deep", "x.go"))

	if m.ex.selected().name != "x.go" {
		t.Fatal("reveal must select the file")
	}
}

func TestActivityBarHeaderFooter(t *testing.T) {
	m := testModel(t)
	lines := strings.Split(checkWidths(t, m), "\n")
	raw := strings.Split(m.View().Content, "\n") // checkWidths strips the colors
	l := m.colRect(0)
	// Spaces and Search share an "S" chip: the ascii set trades the letter for a true label.
	if !strings.Contains(lines[0], "F   G   S   S") || !strings.Contains(raw[0], fgParams(pal.headerAccent)) || !strings.Contains(lines[1], "━━━") {
		t.Fatalf("activity bar is the chips with the active one underlined:\n%s\n%s", lines[0], lines[1])
	}

	if want := strings.ToUpper(filepath.Base(m.ws)); !strings.Contains(lines[actH], want) {
		t.Fatalf("header %q lacks %q", lines[actH], want)
	}

	if status := lines[len(lines)-1]; !strings.Contains(status, strings.ToUpper(filepath.Base(m.ws))[:3]) || !strings.Contains(status, "^⇧p") {
		t.Fatalf("status bar = %q", status)
	}
	// Mouse activity over the sidebar shows the header actions; +f creates a file.
	m.Update(tea.MouseMotionMsg{X: 5, Y: 8})

	header := strings.Split(checkWidths(t, m), "\n")[actH]
	if !strings.Contains(header, "+f") {
		t.Fatalf("hover header = %q", header)
	}

	acts := m.headerActions(0, viewFiles, l.w)
	click(m, acts[0].x, actH, tea.MouseLeft)

	if m.modal == nil || !strings.HasPrefix(m.modal.title, "New file") {
		t.Fatalf("new file action: %+v", m.modal)
	}

	m.modal = nil
	// Hovering a row paints the hover background.
	m.Update(tea.MouseMotionMsg{X: 5, Y: m.bodyTop(viewFiles)})

	if !strings.Contains(m.View().Content, bgParams(pal.hoverBg)) {
		t.Fatal("hovered row is not highlighted")
	}
	// The gear opens settings with hotkeys; « hides the sidebar.
	click(m, l.w-2, 0, tea.MouseLeft)

	if m.modal == nil || !strings.Contains(checkWidths(t, m), "Hotkeys") {
		t.Fatal("gear opens settings")
	}

	m.modal = nil
	hideAt := m.headerActions(0, viewFiles, l.w)
	click(m, hideAt[len(hideAt)-1].x, actH, tea.MouseLeft)

	if l = m.colRect(0); !m.railed(0) || l.w != railW {
		t.Fatalf("« folds the sidebar to a rail: w=%d", l.w)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.HasPrefix(out[0], " F") || !strings.HasPrefix(out[1], " G") || !strings.HasPrefix(out[len(out)-2], " »") {
		t.Fatalf("rail:\n%s", strings.Join(out, "\n"))
	}

	click(m, 0, 1, tea.MouseLeft) // the Git icon reopens the sidebar on Git

	if v, _ := m.viewOn(0); m.railed(0) || v != viewGit || m.focus != 0 {
		t.Fatalf("rail click: railed=%v view=%d focus=%d", m.railed(0), v, m.focus)
	}
}

func TestViewsModalsAndFocus(t *testing.T) {
	m := testModel(t)
	press(m, "3")

	out := checkWidths(t, m)
	if v, _ := m.viewOn(0); v != viewAgents || !strings.Contains(out, "shell") || !strings.Contains(out, "main") {
		t.Fatalf("agents view:\n%s", out)
	}

	click(m, tabAt(m, viewGit), 0, tea.MouseLeft)

	if v, _ := m.viewOn(0); v != viewGit {
		t.Fatalf("tab click: view = %d", v)
	}

	checkWidths(t, m)

	press(m, ",")

	for m.modal.disp[m.modal.l.sel].label != "Hidden files" {
		press(m, "down")
	}

	press(m, "enter") // toggles hidden and stays open

	if !m.st.Settings.Hidden || m.modal == nil || m.modal.items[3].hint != "on" {
		t.Fatalf("settings toggle: hidden=%v modal=%v", m.st.Settings.Hidden, m.modal)
	}

	press(m, "esc")

	m.quickOpen(indexMsg{ws: m.ws, files: []string{"README.md", "src/deep/x.go"}})
	press(m, "x", ".")

	if len(m.modal.disp) != 1 || m.modal.disp[0].label != "src/deep/x.go" {
		t.Fatalf("picker filter: %+v", m.modal.disp)
	}

	press(m, "enter")

	if m.modal != nil || !m.preview || m.pv.kind != pvFile {
		t.Fatalf("picker choose opens preview: modal=%v preview=%v", m.modal, m.pv)
	}

	lines, plainLines, meta, numW := render(pvFile, "x.go", "package x\n", true)
	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: "package x\n", lines: lines, plain: plainLines, meta: meta, numW: numW})

	if out := checkWidths(t, m); !strings.Contains(out, "package x") || !strings.Contains(out, "^w close") {
		t.Fatalf("preview content:\n%s", out)
	}

	press(m, "ctrl+]") // left → main

	if m.focus != onMain {
		t.Fatalf("ctrl+] focuses main, focus = %d", m.focus)
	}

	press(m, "ctrl+w") // a file types letters now, so ⌃w closes it

	if m.preview {
		t.Fatal("ctrl+w closes preview")
	}

	m.switchSession("s1") // over the editor: session_position is "editor" here
	m.focus = onMain
	m.term = term{id: "s1", scr: proto.Screen{Lines: []string{"$ \x1b[31mred\x1b[m", "second"}, CursorVisible: true, CursorX: 2}}

	out = checkWidths(t, m)
	if !strings.Contains(out, "red") || !strings.Contains(out, "shell") {
		t.Fatalf("session view:\n%s", out)
	}

	if v := m.View(); v.Cursor == nil || v.Cursor.X != m.mainX()+2 || v.Cursor.Y != 2 {
		t.Fatalf("cursor sits under the session strip and header: %+v", v.Cursor)
	}

	press(m, "l")

	if p := <-m.inputs; p.ID != "s1" || len(p.Keys) != 1 || p.Keys[0].Text != "l" {
		t.Fatalf("key forwarded as %+v", p)
	}

	press(m, "b") // session focus: b goes to the session too
	<-m.inputs
	press(m, "ctrl+]", "b") // main → (no right sidebar) → left, then hide it

	if l := m.colRect(0); l.w != railW || m.focus != onMain {
		t.Fatalf("b folds the sidebar to a rail and focuses main: left=%d focus=%d", l.w, m.focus)
	}

	checkWidths(t, m)
}

func TestTwoSidebarsAndDrag(t *testing.T) {
	m := testModel(t)
	click(m, tabAt(m, viewAgents), 0, tea.MouseRight)

	if m.modal == nil || m.modal.title != "Spaces" {
		t.Fatalf("right click on a view chip opens its menu: %+v", m.modal)
	}

	press(m, "enter") // Move to Right Sidebar

	if m.colOf(viewAgents) != 1 || !m.cols()[1].right || m.focus != 1 {
		t.Fatalf("right = %v, focus = %d", m.st.Settings.Right, m.focus)
	}

	cs, c := m.layout()

	l, r := cs[0], cs[1]
	if r.w == 0 || c.w < 20 || l.x+l.w+1 != c.x || c.x+c.w+1 != r.x {
		t.Fatalf("layout l=%+v c=%+v r=%+v", l, c, r)
	}

	checkWidths(t, m)

	if header := ansi.Strip(ansi.Cut(strings.Split(m.View().Content, "\n")[0], r.x, m.w)); !strings.Contains(header, "SPACES") {
		t.Fatalf("right sidebar header = %q", header)
	}

	for _, want := range []int{0, onMain, 1} {
		press(m, "ctrl+]")

		if m.focus != want {
			t.Fatalf("ctrl+] cycle: focus = %d, want %d", m.focus, want)
		}
	}

	m.Update(tea.MouseClickMsg{X: l.w, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 40, Y: 5, Button: tea.MouseLeft})

	if m.drag == nil || !strings.Contains(m.View().Content, "┃") {
		t.Fatal("dragging highlights the divider")
	}

	checkWidths(t, m)
	m.Update(tea.MouseReleaseMsg{X: 40, Y: 5, Button: tea.MouseLeft})

	if l = m.colRect(0); m.drag != nil || m.st.Settings.Left[0].Width != 40 || l.w != 40 {
		t.Fatalf("left width = %d (setting %v)", l.w, m.st.Settings.Left)
	}

	r = m.colRect(1)
	m.Update(tea.MouseClickMsg{X: r.x - 1, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 70, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 70, Y: 5, Button: tea.MouseLeft})

	if r = m.colRect(1); r.x != 71 || r.w != 29 {
		t.Fatalf("right sidebar after drag: %+v", r)
	}

	checkWidths(t, m)

	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})

	if _, c = m.layout(); c.w < 20 {
		t.Fatalf("main shrank to %d", c.w)
	}

	checkWidths(t, m)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	click(m, tabAt(m, viewAgents), 0, tea.MouseRight)
	press(m, "enter")

	if len(m.cols()) != 1 || m.colOf(viewAgents) != 0 {
		t.Fatalf("move back: cols=%+v", m.cols())
	}

	checkWidths(t, m)
}

func TestPanelBorders(t *testing.T) {
	m := testModel(t)
	m.switchSession("s1")
	m.setSettings(map[string]any{"panel_borders": true})

	if m.bord() != 1 || m.h != 18 {
		t.Fatalf("borders: bord=%d h=%d", m.bord(), m.h)
	}

	lines := strings.Split(checkWidths(t, m), "\n")
	if !strings.HasPrefix(lines[0], "┌─ Explorer") || !strings.Contains(lines[0], "┌─ shell") || !strings.HasPrefix(lines[len(lines)-2], "└") || !strings.Contains(lines[len(lines)-1], "^⇧p") {
		t.Fatalf("frame and status bar:\n%s\n%s\n%s", lines[0], lines[len(lines)-2], lines[len(lines)-1])
	}

	click(m, tabAt(m, viewGit), 1, tea.MouseLeft) // one row lower under the frame

	if v, _ := m.viewOn(0); v != viewGit {
		t.Fatal("clicks are offset by the top border")
	}

	cs, _ := m.layout()
	l := cs[0]
	m.Update(tea.MouseClickMsg{X: l.x + l.w, Y: 6, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 41, Y: 6, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 41, Y: 6, Button: tea.MouseLeft})

	if cs, c := m.layout(); cs[0].w != 40 || c.x != cs[0].x+cs[0].w+2 {
		t.Fatalf("framed drag: l=%+v c=%+v", cs[0], c)
	}

	checkWidths(t, m)
}

func gitModel(t *testing.T) *Model {
	t.Helper()
	m := testModelSized(t, 100, 40)
	root := m.ws
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "main", Changes: []git.Entry{
		{Path: "a/b.go", Letter: 'M'}, {Path: "a/c.go", Letter: 'U'}, {Path: "x/y/z.go", Letter: 'M'}, {Path: "d.go", Letter: 'D'},
	}}}})
	press(m, "2")

	return m
}

func rowTexts(s *scmView) []string {
	var out []string

	for _, r := range s.rows {
		switch r.kind {
		case rowFile:
			out = append(out, filepath.Base(r.entry.Path))
		case rowSection:
			out = append(out, r.title+":"+r.text)
		case rowDir:
			out = append(out, r.text)
		}
	}

	return out
}

func rowIndex(s *scmView, kind int, root string) int {
	for i, r := range s.rows {
		if r.kind == kind && (root == "" || r.root == root) {
			return i
		}
	}

	return -1
}

func TestGitLayoutTreeAndPanes(t *testing.T) {
	m := gitModel(t)
	out := checkWidths(t, m)
	// A 30-column sidebar leaves no room for the branch in the placeholder.
	for _, want := range []string{"SOURCE CONTROL", "Message (⏎ to commit)", "✓ Commit", "Changes", " 3 ", "Untracked Changes", " 1 "} {
		if !strings.Contains(out, want) {
			t.Fatalf("git view lacks %q:\n%s", want, out)
		}
	}

	if k := []int{m.scm.rows[0].kind, m.scm.rows[1].kind, m.scm.rows[2].kind, m.scm.rows[3].kind, m.scm.rows[4].kind}; !slices.Equal(k, []int{rowGap, rowMsg, rowGap, rowCommit, rowGap}) {
		t.Fatalf("blank rows around the message box and Commit: %v", k)
	}
	// Keyboard navigation skips the message box and buttons.
	press(m, "down")

	if r := m.scm.selected(); r == nil || r.kind != rowSection {
		t.Fatalf("first selectable row = %+v", r)
	}

	press(m, "t")

	if !m.st.Settings.GitTree {
		t.Fatal("t switches to tree mode")
	}

	want := []string{"Changes:3", "a", "b.go", "x/y", "z.go", "d.go", "Untracked Changes:1", "a", "c.go"}
	if got := rowTexts(&m.scm); !slices.Equal(got, want) {
		t.Fatalf("tree rows = %v, want %v", got, want)
	}

	checkWidths(t, m)
	m.scm.sel = slices.IndexFunc(m.scm.rows, func(r scmRow) bool { return r.kind == rowDir && r.text == "a" })
	press(m, "enter")

	if got := rowTexts(&m.scm); slices.Contains(got, "b.go") {
		t.Fatalf("collapsed a still shows its files: %v", got)
	}

	// Clicking Commit with an empty message focuses the box instead of committing.
	top := m.bodyTop(viewGit)
	click(m, 10, top+rowIndex(&m.scm, rowCommit, ""), tea.MouseLeft)

	if m.scm.busy != "" || !m.scm.input.Focused() {
		t.Fatalf("empty commit: busy=%q focused=%v", m.scm.busy, m.scm.input.Focused())
	}

	m.scm.input.Blur()
	// ∨ opens the commit menu.
	l := m.colRect(0)
	click(m, l.w-2, top+rowIndex(&m.scm, rowCommit, ""), tea.MouseLeft)

	if m.modal == nil || len(m.modal.disp) != 3 || m.modal.disp[2].label != "Commit (Amend)" {
		t.Fatalf("commit menu: %+v", m.modal)
	}

	m.modal = nil

	// Drawers are headers at the bottom of the pane area.
	H := m.scm.paneH(m)

	_, ds := m.scm.geometry(m, H)
	if ds[len(ds)-1].head != H-1 {
		t.Fatalf("last drawer header at %d, want %d", ds[len(ds)-1].head, H-1)
	}
	// Drawer headers sit under a rule, without the section shading.
	screen := strings.Split(m.View().Content, "\n")
	if head := ansi.Cut(screen[top+ds[0].head], 0, l.w); strings.Contains(head, bgParams(pal.sectionBg)) || !strings.Contains(ansi.Strip(screen[top+ds[0].head-1]), "───") {
		t.Fatalf("drawer header %q under %q", ansi.Strip(head), ansi.Strip(screen[top+ds[0].head-1]))
	}

	gy := top + ds[0].head
	click(m, 3, gy, tea.MouseLeft)

	if !m.pane("Graph").Open {
		t.Fatal("clicking a drawer header opens it")
	}

	_, ds = m.scm.geometry(m, H)
	h0, gy := ds[0].h, top+ds[0].head
	m.Update(tea.MouseClickMsg{X: 3, Y: gy, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 3, Y: gy - 3, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 3, Y: gy - 3, Button: tea.MouseLeft})

	if got := m.pane("Graph"); !got.Open || got.H != h0+3 {
		t.Fatalf("dragging the header up 3 rows: %+v (was %d)", got, h0)
	}

	checkWidths(t, m)

	press(m, "ctrl+f", "z")

	if got := rowTexts(&m.scm); !slices.Equal(got, []string{"Changes:1", "x/y", "z.go"}) {
		t.Fatalf("filtered rows = %v", got)
	}

	checkWidths(t, m)
	press(m, "enter", "esc")

	if m.filters[viewGit].on {
		t.Fatal("esc clears the filter")
	}
}

// TestCommitMessageLines is the message box growing like VS Code's: shift+enter
// (alt+enter where the terminal cannot send it) starts a line and adds a row,
// deleting it takes the row away, enter still commits.
func TestCommitMessageLines(t *testing.T) {
	m := gitModel(t)
	msgRows := func() int {
		return len(slices.DeleteFunc(slices.Clone(m.scm.rows), func(r scmRow) bool { return r.kind != rowMsg }))
	}

	press(m, "c", "f", "i", "x", "shift+enter", "b", "o", "d", "y", "alt+enter", "e", "n", "d")

	if v := m.scm.input.Value(); v != "fix\nbody\nend" || msgRows() != 3 {
		t.Fatalf("value %q in %d rows", v, msgRows())
	}

	out := ansi.Strip(checkWidths(t, m))
	if !strings.Contains(out, "▏ fix") || !strings.Contains(out, "▏ body") || !strings.Contains(out, "▏ end") {
		t.Fatalf("three lines drawn:\n%s", out)
	}
	// The ∨ stays in the top-right corner behind its ▏, against the edge;
	// the lines under it run on.
	top, w := m.bodyTop(viewGit)+rowIndex(&m.scm, rowMsg, m.ws), m.colRect(m.colOf(viewGit)).w
	for i, want := range []string{" ▏" + icChevron.s() + "▕ ", "   ▕ ", "   ▕ "} {
		if got := ansi.Strip(ansi.Cut(strings.Split(m.View().Content, "\n")[top+i], w-5, w)); got != want {
			t.Fatalf("line %d ends %q, want %q", i, got, want)
		}
	}

	press(m, "backspace", "backspace", "backspace", "backspace")

	if msgRows() != 2 {
		t.Fatalf("a deleted line keeps its row: %d rows", msgRows())
	}

	for range 20 {
		press(m, "shift+enter")
	}

	if msgRows() != maxMsgLines {
		t.Fatalf("the box stops growing at %d rows, has %d", maxMsgLines, msgRows())
	}

	checkWidths(t, m)
}

// TestCommitMessageSelect is the message box's selection, as in the editor:
// ctrl+a selects everything and typing replaces it, ctrl+x cuts it, ctrl+c
// copies only a selection, and a mouse drag selects across lines.
func TestCommitMessageSelect(t *testing.T) {
	m := gitModel(t)
	press(m, "c", "o", "l", "d", "shift+enter", "t", "w", "o", "ctrl+a")

	if got := m.scm.input.SelectedText(); got != "old\ntwo" {
		t.Fatalf("ctrl+a selected %q", got)
	}

	if !strings.Contains(m.View().Content, bgParams(pal.textSelBg)) {
		t.Fatal("the selection is not drawn")
	}

	press(m, "n", "e", "w")

	if v := m.scm.input.Value(); v != "new" || len(slices.DeleteFunc(slices.Clone(m.scm.rows), func(r scmRow) bool { return r.kind != rowMsg })) != 1 {
		t.Fatalf("typing over the selection: %q", v)
	}

	if _, cmd := m.Update(keyMsg("ctrl+c")); cmd != nil {
		t.Fatal("ctrl+c without a selection copies nothing")
	}

	press(m, "ctrl+a")

	if _, cmd := m.Update(keyMsg("ctrl+x")); cmd == nil || m.scm.input.Value() != "" {
		t.Fatalf("ctrl+x: value %q, copies %v", m.scm.input.Value(), cmd != nil)
	}
	// A drag from "b" on the first line to "e" on the second selects between.
	m.scm.input.SetValue("abc\ndef")
	m.scm.fit(m)

	top := m.bodyTop(viewGit) + rowIndex(&m.scm, rowMsg, m.ws)
	x := m.colRect(m.colOf(viewGit)).x + 3

	m.Update(tea.MouseClickMsg{X: x + 1, Y: top, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x + 1, Y: top + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x + 1, Y: top + 1, Button: tea.MouseLeft})

	if got := m.scm.input.SelectedText(); got != "bc\nd" || !m.scm.input.Focused() || m.drag != nil {
		t.Fatalf("drag selected %q, focused %v", got, m.scm.input.Focused())
	}

	checkWidths(t, m)
}

// TestCommitSplitHover darkens only the half of the split Commit button
// under the mouse: the label or the ∨ with the ▏ before it, also from the
// button's edges in the blank rows around it.
func TestCommitSplitHover(t *testing.T) {
	m := gitModel(t)
	top := m.bodyTop(viewGit) + rowIndex(&m.scm, rowCommit, m.ws)
	rc := m.colRect(m.colOf(viewGit))
	hot := bgParams(pal.buttonHoverBg)
	line := func() string { return strings.Split(m.View().Content, "\n")[top] }
	// The hover color is the one the label's or the ∨'s text is drawn on.
	label := func() bool { return strings.Contains(line(), hot+"m        "+icCheck.s()) }
	menu := func() bool { return strings.Contains(line(), hot+"m▏") }

	m.Update(tea.MouseMotionMsg{X: rc.x + 5, Y: top})

	if !label() || menu() {
		t.Fatalf("the label alone darkens under the mouse: %q", line())
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + rc.w - 3, Y: top})

	if label() || !menu() {
		t.Fatalf("the ∨ alone darkens under the mouse: %q", line())
	}
	// The blank rows around it are its taller edges: slivers in its colors
	// that hover with it.
	rows := strings.Split(m.View().Content, "\n")
	if !strings.Contains(rows[top-1], "▁") || !strings.Contains(rows[top+1], "▔") {
		t.Fatalf("no edges around the button:\n%s\n%s\n%s", rows[top-1], line(), rows[top+1])
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + 5, Y: top - 1})

	if !label() {
		t.Fatalf("the label does not darken with the mouse on its edge: %q", line())
	}

	checkWidths(t, m)
}

// TestSuggestHover raises the ∨ button under the mouse, and only there, up to
// the box's edge; a click opens its menu, whose item generates the message.
func TestSuggestHover(t *testing.T) {
	m := gitModel(t)
	top := m.bodyTop(viewGit) + rowIndex(&m.scm, rowMsg, m.ws)
	rc := m.colRect(m.colOf(viewGit))
	hot := bgParams(pal.keycapBg)
	line := func() string { return strings.Split(m.View().Content, "\n")[top] }

	m.Update(tea.MouseMotionMsg{X: rc.x + 5, Y: top})

	if strings.Contains(line(), hot) {
		t.Fatal("the button is raised with the mouse over the text")
	}

	m.Update(tea.MouseMotionMsg{X: rc.x + rc.w - 4, Y: top})

	if !strings.Contains(line(), hot) {
		t.Fatalf("the button is not raised under the mouse: %q", line())
	}

	if edge := ansi.Cut(line(), rc.w-2, rc.w-1); !strings.Contains(edge, hot) {
		t.Fatalf("the raised button stops short of the edge: %q", edge)
	}

	checkWidths(t, m)
	click(m, rc.x+rc.w-4, top, tea.MouseLeft)

	labels := func() []string {
		var out []string
		for _, it := range m.modal.items {
			out = append(out, it.label)
		}

		return out
	}

	if m.modal == nil || !slices.Equal(labels(), []string{"Generate Commit Message", "Generate with Description", "Match Repository Style"}) {
		t.Fatalf("the ∨ opens its menu: %v", labels())
	}
	// Rewrite shows with text in the box, Regenerate once a suggestion landed.
	m.modal = nil
	m.scm.input.SetValue("wip")
	m.scm.last = &git.SuggestOpts{Body: true}
	m.Update(scmMsg{root: m.ws, message: "feat: add x"})
	click(m, rc.x+rc.w-4, top, tea.MouseLeft)

	if got := labels(); len(got) != 5 || got[3] != "Rewrite Current Message" || got[4] != "Regenerate" {
		t.Fatalf("with a message and a suggestion: %v", got)
	}
}

// TestSuggestScramble is the message box while ✦ writes a message: ASCII
// noise that settles into "Generating…", typing held off, the suggestion
// decoding into the box, and the ticker stopping once it has.
func TestSuggestScramble(t *testing.T) {
	m := gitModel(t)
	root := m.ws
	m.st.Settings.Anim = true
	press(m, "c")

	m.scm.busy, m.scm.busyRoot, m.scm.frame = "suggesting", root, 0

	out := ansi.Strip(checkWidths(t, m))
	if strings.Contains(out, "Message (") || strings.Contains(out, "Generating") {
		t.Fatalf("frame 0 is noise, not the placeholder or the phrase:\n%s", out)
	}

	for m.scm.frame < 31 { // every phrase holds settled at frame 31
		m.Update(fxTickMsg{})
	}

	if out := ansi.Strip(checkWidths(t, m)); !strings.Contains(out, "Generating") {
		t.Fatalf("frame %d settles on the phrase:\n%s", m.scm.frame, out)
	}

	press(m, "z")

	if m.scm.input.Value() != "" {
		t.Fatalf("typing while suggesting: %q", m.scm.input.Value())
	}

	m.Update(scmMsg{root: root, message: "feat: add z"})

	if out := ansi.Strip(checkWidths(t, m)); strings.Contains(out, "feat: add z") || m.scm.input.Value() != "feat: add z" {
		t.Fatalf("the suggestion decodes in, value %q:\n%s", m.scm.input.Value(), out)
	}

	for i := 0; ; i++ {
		if _, cmd := m.Update(fxTickMsg{}); cmd == nil {
			break
		}

		if i == 100 {
			t.Fatal("the ticker runs on after the suggestion landed")
		}
	}

	if out := ansi.Strip(checkWidths(t, m)); !strings.Contains(out, "feat: add z") {
		t.Fatalf("the suggestion shows:\n%s", out)
	}
}

// TestGitActionButton is VS Code's SCM action button: Commit while there is
// something to commit, else Publish Branch without an upstream, else Sync
// Changes when ahead or behind, else a muted Commit. Publish and Sync have no
// ∨ menu; a click runs the shown action.
func TestGitActionButton(t *testing.T) {
	m := gitModel(t)
	root := m.ws
	show := func(st git.Status) string {
		t.Helper()

		m.scm.busy, m.scm.busyRoot = "", ""
		m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: st}})

		return checkWidths(t, m)
	}

	cases := []struct {
		st   git.Status
		want string
		menu bool
	}{
		{git.Status{Branch: "main", Changes: []git.Entry{{Path: "a.go", Letter: 'M'}}}, "✓ Commit", true},
		{git.Status{Branch: "main", Upstream: "origin/main", Ahead: 2, Staged: []git.Entry{{Path: "a.go", Letter: 'M', Staged: true}}}, "✓ Commit", true},
		{git.Status{Branch: "feat"}, "☁ Publish Branch", false},
		{git.Status{Branch: "main", Upstream: "origin/main", Ahead: 2, Behind: 1}, "⇅ Sync Changes 1↓ 2↑", false},
		{git.Status{Branch: "main", Upstream: "origin/main", Ahead: 1}, "⇅ Sync Changes 1↑", false},
		{git.Status{Branch: "main", Upstream: "origin/main"}, "✓ Commit", true},
		{git.Status{Upstream: "", Branch: ""}, "✓ Commit", true}, // detached HEAD: nothing to publish
	}
	for _, c := range cases {
		out := show(c.st)
		if !strings.Contains(out, c.want) || strings.Contains(out, "Sync Changes") != strings.HasPrefix(c.want, "⇅") {
			t.Fatalf("%+v: want %q:\n%s", c.st, c.want, out)
		}

		_, button, _ := strings.Cut(out, c.want)
		button, _, _ = strings.Cut(button, "\n")

		if strings.Contains(button, "▏∨") != c.menu {
			t.Fatalf("%+v: menu shown = %v", c.st, !c.menu)
		}

		if n := slices.IndexFunc(m.scm.rows, func(r scmRow) bool { return r.kind == rowCommit }); n < 0 || strings.Count(out, "✓ Commit") > 1 {
			t.Fatalf("%+v: one action row:\n%s", c.st, out)
		}
	}
	// Nothing to commit and nothing to push: the Commit button is muted.
	show(git.Status{Branch: "main", Upstream: "origin/main"})

	if !strings.Contains(m.View().Content, bgParams(pal.mutedButtonBg)) {
		t.Fatal("an idle Commit button is muted")
	}

	show(git.Status{Branch: "main", Changes: []git.Entry{{Path: "a.go", Letter: 'M'}}})

	if strings.Contains(m.View().Content, bgParams(pal.mutedButtonBg)) {
		t.Fatal("a Commit button with changes is not muted")
	}
	// Clicking the button runs what it shows; the in-flight label follows.
	show(git.Status{Branch: "main", Upstream: "origin/main", Behind: 1})

	top := m.bodyTop(viewGit)
	click(m, 10, top+rowIndex(&m.scm, rowCommit, root), tea.MouseLeft)

	if m.scm.busy != "syncing" || !strings.Contains(checkWidths(t, m), "Syncing…") {
		t.Fatalf("clicking Sync Changes: busy=%q", m.scm.busy)
	}
	// Publish first lists the remotes; S without an upstream does the same.
	show(git.Status{Branch: "feat"})

	for _, run := range []func() tea.Cmd{func() tea.Cmd { return m.scm.press(m) }, func() tea.Cmd { return m.scm.key(m, keyMsg("S")) }} {
		cmd := run()
		if cmd == nil {
			t.Fatal("publish returned no command")
		}

		if msg, ok := cmd().(remotesMsg); !ok || msg.root != root {
			t.Fatalf("publish command = %#v", msg)
		}
	}

	if !hasCommand(m, "git.publishBranch") {
		t.Fatal("no git.publishBranch command")
	}
}

// TestPublishFlow is VS Code's Publish Branch: one remote is pushed to at
// once, several are picked from with an Add remote item that asks for the
// URL and the name, and none falls back to Publish to GitHub through gh or
// to a warning.
func TestPublishFlow(t *testing.T) {
	m := gitModel(t)
	root := m.ws
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "feat"}}})

	reset := func() { m.scm.busy, m.scm.busyRoot, m.modal = "", "", nil }

	m.scm.onRemotes(m, remotesMsg{root: root, remotes: []git.Remote{{Name: "origin", URL: "git@x:a.git"}}})

	if m.scm.busy != "publishing" || m.modal != nil || !strings.Contains(checkWidths(t, m), "Publishing…") {
		t.Fatalf("one remote: busy=%q modal=%v", m.scm.busy, m.modal != nil)
	}

	reset()
	m.scm.onRemotes(m, remotesMsg{root: root, remotes: []git.Remote{{Name: "origin", URL: "git@x:a.git"}, {Name: "fork", URL: "git@y:b.git"}}})

	if m.modal == nil || !strings.Contains(m.modal.title, `branch "feat"`) || len(m.modal.items) != 3 || m.scm.busy != "" {
		t.Fatalf("two remotes: modal=%+v", m.modal)
	}

	out := checkWidths(t, m)
	for _, want := range []string{"origin", "git@x:a.git", "fork", "Add a new remote"} {
		if !strings.Contains(out, want) {
			t.Fatalf("remote picker lacks %q:\n%s", want, out)
		}
	}

	press(m, "down", "down", "enter") // Add remote sits first, then origin, fork

	if m.scm.busy != "publishing" || m.modal != nil {
		t.Fatalf("picking fork: busy=%q", m.scm.busy)
	}

	reset()
	m.scm.onRemotes(m, remotesMsg{root: root, remotes: []git.Remote{{Name: "origin"}, {Name: "fork"}}})
	press(m, "enter")

	if m.modal == nil || m.modal.submit == nil || m.modal.title != "Remote URL" {
		t.Fatalf("Add remote asks for the URL first: %+v", m.modal)
	}

	press(m, "g", "i", "t", "@", "z", "enter")

	if m.modal == nil || m.modal.submit == nil || !strings.Contains(m.modal.title, "Remote name") {
		t.Fatalf("then for the name: %+v", m.modal)
	}

	press(m, "u", "p", "enter")

	if m.scm.busy != "publishing" || m.modal != nil {
		t.Fatalf("publishing to the new remote: busy=%q", m.scm.busy)
	}

	if rs, _ := git.Remotes(root); len(rs) != 0 { // nothing ran yet: the command does
		t.Fatalf("remotes added synchronously: %v", rs)
	}

	reset()

	cmd := m.scm.onRemotes(m, remotesMsg{root: root})
	if msg, _ := cmd().(flashMsg); m.modal != nil || m.scm.busy != "" || !strings.Contains(msg.text, "no remotes") || !msg.err {
		t.Fatalf("no remotes without gh: modal=%v flash=%+v", m.modal != nil, msg)
	}

	reset()
	m.scm.onRemotes(m, remotesMsg{root: root, gh: true, login: "me"})

	if m.modal == nil || m.modal.input.Value() != filepath.Base(root) || len(m.modal.items) != 2 {
		t.Fatalf("Publish to GitHub picker: %+v", m.modal)
	}

	out = checkWidths(t, m)
	for _, want := range []string{"private repository", "public repository", "github.com/me"} {
		if !strings.Contains(out, want) {
			t.Fatalf("GitHub picker lacks %q:\n%s", want, out)
		}
	}

	press(m, "ctrl+u") // an empty name keeps the picker open
	m.modal.input.SetValue("")
	press(m, "enter")

	if m.modal == nil || m.scm.busy != "" {
		t.Fatal("an empty repository name publishes")
	}

	m.modal.input.SetValue("My Repo!")
	press(m, "enter")

	if m.scm.busy != "publishing" || m.modal != nil {
		t.Fatalf("publishing to GitHub: busy=%q", m.scm.busy)
	}
}

func TestGitMultiRepo(t *testing.T) {
	m := gitModel(t)
	root, child := m.ws, filepath.Join(m.ws, "libs", "child")
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root, child}, status: map[string]git.Status{
		root:  {Branch: "main", Changes: []git.Entry{{Path: "a.go", Letter: 'M'}}},
		child: {Branch: "dev", Ahead: 1, Upstream: "origin/dev", Staged: []git.Entry{{Path: "c.go", Letter: 'A', Staged: true}}},
	}})

	out := checkWidths(t, m)
	if !strings.Contains(out, "child") || !strings.Contains(out, "dev* 1↑ 0↓") || !strings.Contains(out, "Staged Changes") {
		t.Fatalf("multi repo view:\n%s", out)
	}

	top := m.bodyTop(viewGit)
	click(m, 5, top+rowIndex(&m.scm, rowMsg, child), tea.MouseLeft)

	if m.scm.root() != child || !m.scm.input.Focused() {
		t.Fatalf("clicking a message box activates its repo: active=%s", m.scm.root())
	}

	if !strings.Contains(m.View().Content, bgParams(pal.mutedButtonBg)) {
		t.Fatal("the inactive repository's Commit button is muted")
	}
	// Selecting a file in the other repository makes that one active.
	m.scm.input.Blur()
	m.scm.sel = rowIndex(&m.scm, rowFile, root)
	m.scm.onSelect(m)

	if m.scm.root() != root || m.scm.history != "a.go" {
		t.Fatalf("select file: active=%s history=%q", m.scm.root(), m.scm.history)
	}
}

func TestFilesAndAgentsFilter(t *testing.T) {
	m := testModel(t)
	m.index.ws, m.index.files, m.index.at = m.ws, []string{"README.md", "src/deep/x.go", ".env"}, time.Now()
	press(m, "ctrl+f", "x")

	var names []string
	for _, n := range m.ex.nodes {
		names = append(names, n.name)
	}

	if !slices.Equal(names, []string{"src", "deep", "x.go"}) || m.ex.nodes[2].depth != 2 {
		t.Fatalf("filtered tree = %v", names)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "/ x") {
		t.Fatalf("filter line missing:\n%s", out)
	}

	press(m, "esc")

	if len(m.ex.nodes) != 2 || m.filters[viewFiles].on {
		t.Fatalf("esc restores the tree: %+v", m.ex.nodes)
	}

	press(m, "3", "ctrl+f", "s", "h")

	var kinds []int
	for _, r := range m.ag.rows(m) {
		kinds = append(kinds, r.kind)
	}

	if !slices.Equal(kinds, []int{agProject, agWorkspace, agSession}) {
		t.Fatalf("agents filter keeps ancestors: %v", kinds)
	}

	press(m, "q") // still typing: q is text, not quit

	if len(m.ag.rows(m)) != 0 {
		t.Fatal("no row matches 'shq'")
	}

	checkWidths(t, m)
}

func previewModel(t *testing.T, kind string, lines ...string) *Model {
	t.Helper()
	m := testModel(t)
	m.pv = preview{kind: kind, path: filepath.Join(m.ws, "x.txt"), root: m.ws}
	m.preview = true

	plainLines := make([]string, len(lines))
	for i, l := range lines {
		plainLines[i] = ansi.Strip(l)
	}

	m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: strings.Join(plainLines, "\n"), lines: lines, plain: plainLines, numW: 1})
	m.focus = onMain

	return m
}

func TestPreviewExpandSelection(t *testing.T) {
	m := previewModel(t, "show", "func f() {", "    x := g(ab, cd)", "}")
	m.pv.cur = pos{1, 11}

	want := []string{
		"ab", "ab, cd", "(ab, cd)", "x := g(ab, cd)", "    x := g(ab, cd)",
		"\n    x := g(ab, cd)\n", "{\n    x := g(ab, cd)\n}", "func f() {\n    x := g(ab, cd)\n}",
	}
	for i, w := range want {
		press(m, "alt+shift+right")

		if got := m.pv.selectedText(); got != w {
			t.Fatalf("expand step %d = %q, want %q", i, got, w)
		}
	}

	press(m, "alt+shift+right") // the whole file is the end of the chain

	if got := m.pv.selectedText(); got != want[len(want)-1] {
		t.Fatalf("expand past the file = %q", got)
	}

	press(m, "alt+shift+left", "alt+shift+left")

	if got := m.pv.selectedText(); got != "\n    x := g(ab, cd)\n" {
		t.Fatalf("shrink = %q", got)
	}

	press(m, "right", "alt+shift+right") // a move starts a new chain from the cursor

	if got := m.pv.selectedText(); got != "}" {
		t.Fatalf("expand after move = %q", got)
	}

	press(m, "alt+shift+left")

	if m.pv.anchor != nil {
		t.Fatalf("shrink to the cursor left a selection: %q", m.pv.selectedText())
	}
}

func TestPreviewCursorAndSelection(t *testing.T) {
	m := previewModel(t, "show", "\x1b[32mhello world\x1b[m", "second line", "third")
	press(m, "right", "shift+down", "shift+right")

	if got := m.pv.selectedText(); got != "ello world\nse" {
		t.Fatalf("keyboard selection = %q", got)
	}

	out := checkWidths(t, m)
	if !strings.Contains(out, "13 selected") || !strings.Contains(out, "Ln 2, Col 3") {
		t.Fatalf("header:\n%s", strings.Split(out, "\n")[0])
	}

	if v := m.View(); v.Cursor == nil || v.Cursor.X != m.mainX()+2 || v.Cursor.Y != 2 {
		t.Fatalf("cursor = %+v", v.Cursor)
	}

	press(m, "esc")

	if m.pv.anchor != nil || !m.preview {
		t.Fatal("esc clears the selection before closing")
	}

	c := m.mainX()
	m.Update(tea.MouseClickMsg{X: c, Y: 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: c + 5, Y: 1, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: c + 5, Y: 1, Button: tea.MouseLeft})

	if got := m.pv.selectedText(); got != "hello" {
		t.Fatalf("mouse selection = %q", got)
	}

	if !strings.Contains(m.View().Content, bgParams(pal.textSelBg)) {
		t.Fatal("selection is drawn with the selection background")
	}

	checkWidths(t, m)

	press(m, "ctrl+a")

	if got := m.pv.selectedText(); got != "hello world\nsecond line\nthird" {
		t.Fatalf("ctrl+a = %q", got)
	}

	click(m, c+3, 3, tea.MouseLeft)

	if m.pv.anchor != nil || m.pv.at() != (pos{2, 3}) {
		t.Fatalf("click: anchor=%v cur=%v", m.pv.anchor, m.pv.at())
	}

	click(m, c+1, 0, tea.MouseLeft) // ✕ in the header

	if m.preview {
		t.Fatal("clicking ✕ closes the preview")
	}
}

func TestPreviewWrapAndGutter(t *testing.T) {
	long := strings.Repeat("abcdefghij", 12) // 120 cells, wider than the main area

	m := previewModel(t, "file", "short", long)
	if gw := m.pv.gutter(); gw != 3 { // a blank, the digit, a blank
		t.Fatalf("gutter = %d", gw)
	}

	press(m, "down", "end")

	if x, _, ok := m.pv.cursor(m.pvW(), m.pvH()); !ok || x != m.pvW()-1 {
		t.Fatalf("unwrapped end of line scrolls horizontally: x=%d ok=%v left=%d", x, ok, m.pv.left)
	}

	checkWidths(t, m)
	press(m, "alt+z") // ⌥z wraps; w types into the file

	rows := m.pv.rows(m.pvW())
	if len(rows) < 3 || rows[1].line != 1 || rows[2].line != 1 {
		t.Fatalf("wrapped rows = %+v", rows)
	}

	if _, y, ok := m.pv.cursor(m.pvW(), m.pvH()); !ok || y != len(rows)-1 {
		t.Fatalf("cursor on the last wrapped row: y=%d ok=%v", y, ok)
	}

	checkWidths(t, m)
}

func TestDiffPreview(t *testing.T) {
	m := diffModel(t, 100, 20)
	out := checkWidths(t, m)

	body := strings.Split(out, "\n")
	if !strings.Contains(body[0], "app.ts") || !strings.Contains(body[0], "src — working tree diff") {
		t.Fatalf("diff header = %q", body[0])
	}

	if !strings.Contains(body[2], " 2    - console.log") || !strings.Contains(body[3], "    2 + console.log") {
		t.Fatalf("diff gutters:\n%s\n%s", body[2], body[3])
	}
	// Tinted rows fill the main area to its right edge.
	row := strings.Split(m.View().Content, "\n")[3]
	if tail := ansi.Cut(row, m.w-3, m.w); !strings.Contains(tail, bgParams(pal.diffAddBg)) {
		t.Fatalf("row tint stops early: %q", tail)
	}
	// Copying a selection copies code, not gutters or marks.
	press(m, "down", "shift+end")

	if got := m.pv.selectedText(); got != `console.log("scm-playground up");` {
		t.Fatalf("diff selection = %q", got)
	}
}

func TestQuickOpenTree(t *testing.T) {
	m := testModel(t)
	m.st.Settings.QuickTree = true
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"b.go", "a/y.go", "a/x.go"}})

	if got := labels(m); !slices.Equal(got, []string{"a/", "  x.go", "  y.go", "./", "  b.go"}) {
		t.Fatalf("tree rows = %q", got)
	}

	if m.modal.l.sel != 1 {
		t.Fatalf("first actionable row selected, got %d", m.modal.l.sel)
	}

	press(m, "down", "down")

	if m.modal.l.sel != 4 {
		t.Fatalf("down skips directory rows, sel = %d", m.modal.l.sel)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "[list]") {
		t.Fatalf("toggle button missing:\n%s", out)
	}

	press(m, "ctrl+t")

	if m.modal.tree || m.st.Settings.QuickTree || len(m.modal.disp) != 3 {
		t.Fatalf("ctrl+t back to list: tree=%v rows=%d", m.modal.tree, len(m.modal.disp))
	}

	x, y, w, _, _ := m.modal.rect(m)
	m.Update(tea.MouseClickMsg{X: x + w - 4, Y: y, Button: tea.MouseLeft})

	if !m.modal.tree {
		t.Fatal("clicking the title toggle switches to tree")
	}
}

func TestThemes(t *testing.T) {
	m := gitModel(t)

	defer applyLook("vscode", true, "ascii", nil)

	m.dark = false
	m.look()

	if !pal.light || !strings.Contains(m.View().Content, bgParams(vscodeLight.buttonBg)) {
		t.Fatal("light terminals get VS Code Light's Commit button")
	}

	m.setSettings(map[string]any{"color_theme": "vscode-dark"})

	if pal.light {
		t.Fatal("vscode-dark ignores the terminal background")
	}

	m.setSettings(map[string]any{"color_theme": "terminal"})

	if pal.buttonBg != terminalPal.buttonBg {
		t.Fatal("terminal theme uses ANSI colors")
	}

	checkWidths(t, m)
	m.setSettings(map[string]any{"icons": "nerd"})

	if !iconsNerd || !strings.Contains(m.View().Content, icGit.nerd) {
		t.Fatal("nerd icons in the activity bar")
	}

	checkWidths(t, m)
}

func TestRowFitsExactly(t *testing.T) {
	for _, w := range []int{1, 5, 20} {
		got := row(w, pal.selBg, []seg{sg("a very long name that overflows", bold)}, sg(" M ", fg(pal.modified)))
		if ansi.StringWidth(got) != w {
			t.Errorf("row(%d) width = %d", w, ansi.StringWidth(got))
		}
	}

	if s := fit("\x1b[31mhello\x1b[m", 3); ansi.Strip(s) != "hel" {
		t.Errorf("fit truncate = %q", s)
	}
}

func TestGlyphs(t *testing.T) {
	names := map[string]bool{}

	for _, g := range allGlyphs {
		rs := []rune(g.nerd)

		pua := len(rs) == 1 && (rs[0] >= 0xe000 && rs[0] <= 0xf8ff || rs[0] >= 0xf0000)
		if !pua || ansi.StringWidth(g.nerd) != 1 || g.text == "" || names[g.name] {
			t.Errorf("glyph %s: nerd %q (%U) text %q", g.name, g.nerd, rs, g.text)
		}

		names[g.name] = true
	}
}

// scmRowAt is the index of the Source Control row of kind under the section
// title; file narrows it to one entry.
func scmRowAt(m *Model, kind int, title, file string) int {
	return slices.IndexFunc(m.scm.rows, func(r scmRow) bool {
		return r.kind == kind && r.title == title && (file == "" || r.entry.Path == file)
	})
}

// sidebarRow is row i of the Source Control body as drawn in the first column.
func sidebarRow(t *testing.T, m *Model, i int) string {
	t.Helper()
	return ansi.Cut(strings.Split(checkWidths(t, m), "\n")[m.bodyTop(viewGit)+i], 0, m.colRect(0).w)
}

func TestSectionActions(t *testing.T) {
	m := gitModel(t)
	top := m.bodyTop(viewGit)

	l := m.colRect(0)
	if !strings.Contains(m.View().Content, bgParams(pal.sectionBg)) || !strings.Contains(m.View().Content, bgParams(pal.badgeBg)) {
		t.Fatal("section headers are shaded with a grey count badge")
	}

	// Hovering a section shows its buttons before the count badge.
	ch := scmRowAt(m, rowSection, "Changes", "")
	m.Update(tea.MouseMotionMsg{X: 3, Y: top + ch})

	if got := sidebarRow(t, m, ch); !strings.Contains(got, "↶  +  3  ") {
		t.Fatalf("hovered Changes header = %q", got)
	}

	acts := m.scm.actions(m.scm.rows[ch], l.w)
	click(m, acts[1].x, top+ch, tea.MouseLeft)

	if m.scm.busy != "staging" {
		t.Fatalf("+ on Changes stages tracked files: busy=%q", m.scm.busy)
	}

	m.scm.busy = ""
	click(m, acts[0].x, top+ch, tea.MouseLeft)

	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Discard all changes") {
		t.Fatalf("↶ on Changes asks first: %+v", m.modal)
	}

	m.modal = nil

	// File rows: discard and stage; the untracked section stages with U too.
	f := scmRowAt(m, rowFile, "Changes", "a/b.go")
	m.Update(tea.MouseMotionMsg{X: 3, Y: top + f})

	if got := sidebarRow(t, m, f); !strings.Contains(got, "src  ↶  +  M ") {
		t.Fatalf("hovered file row = %q", got)
	}

	click(m, m.scm.actions(m.scm.rows[f], l.w)[1].x, top+f, tea.MouseLeft)

	if m.modal == nil || m.modal.title != "Discard changes in a/b.go?" {
		t.Fatalf("↶ on a file: %+v", m.modal)
	}

	m.modal = nil
	press(m, "U")

	if m.scm.busy != "staging" {
		t.Fatalf("U stages untracked files: busy=%q", m.scm.busy)
	}
}

func TestSplitDiff(t *testing.T) {
	_, _, meta, _ := render(pvDiff, "src/app.ts", sampleDiff, true)
	if got := splitRows(meta); !slices.Equal(got, []splitRow{{0, 0, false}, {1, 2, false}, {-1, 3, false}, {4, 4, false}}) {
		t.Fatalf("split rows = %+v", got)
	}

	m := diffModel(t, 130, 20)
	if m.pv.split(m) {
		t.Fatal("diffs start inline")
	}

	press(m, "s")

	if m.st.Settings.DiffView != "split" || !m.pv.split(m) {
		t.Fatal("s shows the diff side by side")
	}

	out := strings.Split(checkWidths(t, m), "\n")

	c, lw := m.mainX(), (m.pvW()-1)/2
	if l, r := ansi.Cut(out[2], c, c+lw), ansi.Cut(out[2], c+lw+1, m.w); !strings.HasPrefix(l, " 2 - console.log(\"scm-playground") || !strings.HasPrefix(r, " 2 + console.log(\"scm oh yeah") {
		t.Fatalf("paired row:\n%q\n%q", l, r)
	}

	if l, r := ansi.Cut(out[3], c, c+lw), ansi.Cut(out[3], c+lw+1, m.w); strings.TrimSpace(l) != "" || !strings.HasPrefix(r, " 3 + // TODO") {
		t.Fatalf("addition without a deletion:\n%q\n%q", l, r)
	}

	if m.View().Cursor != nil {
		t.Fatal("the side-by-side view has no text cursor")
	}

	if !strings.Contains(out[0], "inline") || !strings.Contains(out[len(out)-2], "s inline") {
		t.Fatalf("header/footer:\n%s\n%s", out[0], out[len(out)-2])
	}

	// A narrow main area falls back to inline and keeps the setting.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	if m.pv.split(m) || !strings.Contains(checkWidths(t, m), " 2    - console.log") {
		t.Fatal("narrow main area shows the diff inline")
	}

	m.Update(tea.WindowSizeMsg{Width: 130, Height: 20})
	click(m, m.w-2, 0, tea.MouseLeft) // the header toggle

	if m.st.Settings.DiffView != "inline" {
		t.Fatalf("header toggle: diff_view = %q", m.st.Settings.DiffView)
	}
}

func TestMenuHover(t *testing.T) {
	m := testModel(t)
	press(m, ",")
	x, y, _, _, _ := m.modal.rect(m)
	icons := slices.IndexFunc(m.modal.disp, func(it item) bool { return it.label == "Icons" })
	m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 1 + icons})

	if m.modal.l.sel != icons {
		t.Fatalf("hover selects the row under the mouse: sel=%d want %d", m.modal.l.sel, icons)
	}

	heading := slices.IndexFunc(m.modal.disp, func(it item) bool { return strings.Contains(it.label, "Hotkeys") })
	m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 1 + heading})

	if m.modal.l.sel != icons {
		t.Fatal("hovering a heading keeps the selection")
	}
}

func TestDiffLineMenu(t *testing.T) {
	m := diffModel(t, 100, 20)
	press(m, "down", "shift+down") // the deletion, up to column 0 of the next line

	if dels, adds := m.pv.changedLines(); !maps.Equal(dels, map[int]bool{2: true}) || len(adds) != 0 {
		t.Fatalf("one line: dels=%v adds=%v", dels, adds)
	}

	press(m, "shift+down", "shift+end")

	if dels, adds := m.pv.changedLines(); !maps.Equal(dels, map[int]bool{2: true}) || !maps.Equal(adds, map[int]bool{2: true, 3: true}) {
		t.Fatalf("three lines: dels=%v adds=%v", dels, adds)
	}
	// A right click inside the selection keeps it and opens the menu.
	click(m, m.mainX()+m.pv.gutter()+2, 3, tea.MouseRight)

	if m.modal == nil || m.pv.anchor == nil || !slices.Equal(labels(m), []string{"Stage Selected Ranges", "Revert Selected Ranges…", "Open File at This Line", "Copy", "Go to Line…", "Edit in $EDITOR"}) {
		t.Fatalf("menu = %v, selection kept = %v", m.modal, m.pv.anchor != nil)
	}

	press(m, "down", "enter")

	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Revert the selected changes") {
		t.Fatalf("revert asks first: %+v", m.modal)
	}

	m.modal = nil
	m.pv.entry.Staged = true
	m.pv.menu(m, 0, 0)

	if labels(m)[0] != "Unstage Selected Ranges" {
		t.Fatalf("staged diff menu = %v", labels(m))
	}

	m.pv.entry = git.Entry{Path: "new.ts", Letter: 'U'}
	m.pv.menu(m, 0, 0)

	if labels(m)[0] != "Open File at This Line" || labels(m)[1] != "Copy" {
		t.Fatalf("untracked files have no stage/revert actions: %v", labels(m))
	}
}

func TestNerdFamily(t *testing.T) {
	for fam, want := range map[string]bool{"JetBrains Mono": false, "JetBrainsMono Nerd Font": true, "JetBrainsMono NFM": true, "Hack": false} {
		if nerdFamily(fam) != want {
			t.Errorf("nerdFamily(%q) = %v", fam, !want)
		}
	}
}

// choose picks a menu row by its label.
func choose(t *testing.T, m *Model, label string) {
	t.Helper()

	i := slices.IndexFunc(m.modal.disp, func(it item) bool { return it.label == label })
	if i < 0 {
		t.Fatalf("menu lacks %q: %+v", label, m.modal.disp)
	}

	m.modal.choose(m, i)
}

func TestColumns(t *testing.T) {
	m := testModelSized(t, 140, 20)
	m.setSettings(map[string]any{"left": proto.Columns{{Views: []string{"agents"}, Width: 24}, {Views: []string{"files", "git"}}}})

	cs, c := m.layout()
	if len(cs) != 2 || cs[0] != (rect{0, 24}) || cs[1] != (rect{25, 30}) || c.x != 56 {
		t.Fatalf("columns %+v main %+v", cs, c)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	if !strings.HasPrefix(out[0], " SPACES") || !strings.Contains(ansi.Cut(out[actH], 25, 55), strings.ToUpper(filepath.Base(m.ws))) {
		t.Fatalf("a single-tab column starts with its header:\n%s\n%s", out[0], out[actH])
	}

	m.Update(tea.MouseMotionMsg{X: 2, Y: 0})
	acts := m.headerActions(0, viewAgents, 24) // …, ⚙, « at the end
	click(m, acts[len(acts)-2].x, 0, tea.MouseLeft)

	if m.modal == nil || m.modal.title != "Settings" {
		t.Fatalf("the header gear opens settings: %+v", m.modal)
	}

	m.modal = nil

	m.focus = 0
	for _, want := range []int{1, onMain, 0} {
		press(m, "ctrl+]")

		if m.focus != want {
			t.Fatalf("ctrl+] focus = %d, want %d", m.focus, want)
		}
	}

	layout := func() string { return fmt.Sprint(m.st.Settings.Left, m.st.Settings.Right) }
	// Git moves into its own column at the outer edge, then merges back toward main.
	click(m, tabAt(m, viewGit), 0, tea.MouseRight)
	choose(t, m, "Move to Own Column")

	if got := layout(); got != "[{[git] 0} {[agents] 24} {[files search] 0}] []" || m.focus != 0 {
		t.Fatalf("own column: %s focus=%d", got, m.focus)
	}

	checkWidths(t, m)
	click(m, tabAt(m, viewGit), 0, tea.MouseRight)
	choose(t, m, "Merge into Next Column")

	if got := layout(); got != "[{[agents git] 24} {[files search] 0}] []" {
		t.Fatalf("merge: %s", got)
	}
	// Hiding the side folds all its columns into one rail.
	press(m, "b")

	if cs, _ := m.layout(); cs[0].w != railW || cs[1].w != 0 {
		t.Fatalf("rail: %+v", cs)
	}

	checkWidths(t, m)
}

// TestGitSticky keeps the message box, Commit and the section header of the
// top rows in place while the changes scroll under them.
func TestGitSticky(t *testing.T) {
	m := testModelSized(t, 100, 40)
	root := m.ws

	var staged, changes []git.Entry
	for i := range 15 {
		staged = append(staged, git.Entry{Path: fmt.Sprintf("s%02d.go", i), Letter: 'M'})
	}

	for i := range 40 {
		changes = append(changes, git.Entry{Path: fmt.Sprintf("c%02d.go", i), Letter: 'M'})
	}

	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "main", Staged: staged, Changes: changes}}})
	press(m, "2")
	// Rows: 0-4 gap, message, gap, Commit, gap; 5 Staged Changes, 6-20 staged
	// files; 21 Changes, 22-61 changes.
	if m.scm.rows[5].kind != rowSection || m.scm.rows[21].title != "Changes" {
		t.Fatalf("rows: %v", rowTexts(&m.scm))
	}

	top := m.bodyTop(viewGit)

	ch, _ := m.scm.geometry(m, m.scm.paneH(m))
	if ch < 16 {
		t.Fatalf("changes pane of %d rows is too short for the test", ch)
	}

	line := func(y int) string {
		t.Helper()
		return ansi.Cut(strings.Split(checkWidths(t, m), "\n")[top+y], 0, m.colRect(0).w)
	}
	if p := m.scm.pinned(0, ch); p != nil || !strings.Contains(line(6), "s00.go") {
		t.Fatalf("unscrolled: pinned %v, row 6 %q", p, line(6))
	}
	// The scrollbar runs beside the files, not beside the message box and Commit.
	bar := func() {
		t.Helper()

		for y := range 6 {
			if strings.Contains(line(y), "┃") {
				t.Fatalf("top %d: scrollbar beside row %d %q", m.scm.tops[""], y, line(y))
			}
		}

		if !slices.ContainsFunc(strings.Split(checkWidths(t, m), "\n")[top+6:top+ch], func(l string) bool { return strings.Contains(l, "┃") }) {
			t.Fatalf("top %d: no scrollbar beside the files", m.scm.tops[""])
		}
	}
	bar()
	// A wheel step hides three files under the sticky rows, nothing else moves.
	m.Update(tea.MouseWheelMsg{X: 5, Y: top + 8, Button: tea.MouseWheelDown})

	if m.scm.tops[""] != 3 || !strings.Contains(line(1), "Message") || !strings.Contains(line(3), "Commit") ||
		!strings.Contains(line(5), "Staged Changes") || !strings.Contains(line(6), "s03.go") {
		t.Fatalf("scrolled 3: top %d\n%q\n%q\n%q\n%q", m.scm.tops[""], line(1), line(3), line(5), line(6))
	}
	// The next section header scrolls up to the sticky line, then takes it.
	m.scm.tops[""] = 15

	if !strings.Contains(line(5), "Staged Changes") || !strings.Contains(line(6), "Changes") {
		t.Fatalf("top 15: %q / %q", line(5), line(6))
	}

	m.scm.tops[""] = 16

	if l := line(5); !strings.Contains(l, "Changes") || strings.Contains(l, "Staged") || !strings.Contains(line(6), "c00.go") {
		t.Fatalf("top 16: %q / %q", l, line(6))
	}

	bar()
	// With no scrollbar beside them, the message box and Commit keep the same
	// one-cell margin on both sides, and the ∨ has room inside the button.
	w := m.colRect(0).w
	if l := line(1); ansi.Cut(l, 1, 2) != "▏" || ansi.Cut(l, w-2, w-1) != "▕" {
		t.Fatalf("message box edges in %q", l)
	}

	raw := strings.Split(m.View().Content, "\n")[top+3]
	if l := line(3); ansi.Cut(l, w-3, w-2) != "∨" || !strings.Contains(raw, icChevron.s()+" \x1b[m ") {
		t.Fatalf("Commit button edge in %q (raw %q)", l, raw)
	}
	// The sticky rows answer clicks as themselves: Commit with no message
	// focuses the box, the section header folds its section.
	click(m, 10, top+3, tea.MouseLeft)

	if !m.scm.input.Focused() || m.scm.tops[""] != 16 {
		t.Fatalf("sticky Commit: focused=%v top=%d", m.scm.input.Focused(), m.scm.tops[""])
	}

	m.scm.input.Blur()
	m.focus = m.colOf(viewGit)
	// ↑ onto a row under the sticky ones scrolls it out from under them.
	m.scm.sel = 22
	press(m, "up")

	if m.scm.sel != 21 || m.scm.tops[""] != 16 {
		t.Fatalf("up onto the sticky header: sel %d top %d", m.scm.sel, m.scm.tops[""])
	}

	press(m, "up")

	if m.scm.sel != 20 || m.scm.under(20, m.scm.tops[""], ch) || !strings.Contains(line(20-m.scm.tops[""]), "s14.go") {
		t.Fatalf("up under the sticky rows: sel %d top %d", m.scm.sel, m.scm.tops[""])
	}

	m.scm.sel = -1
	m.scm.tops[""] = 16
	click(m, 3, top+5, tea.MouseLeft)

	if !m.scm.closed[sectionKey(root, "Changes")] {
		t.Fatal("a click on the sticky header folds Changes")
	}
	// A pane too short for them just scrolls.
	if p := m.scm.pinned(16, 10); p != nil {
		t.Fatalf("short pane pins %v", p)
	}
}

// TestMergeChanges is VS Code's Merge Changes group: conflicted files above
// Staged Changes, stage-only buttons, a question before staging a file that
// still holds markers, and a Commit button that waits as Continue.
func TestMergeChanges(t *testing.T) {
	m := testModelSized(t, 100, 40)
	root := m.ws
	mustWrite(t, filepath.Join(root, "a.go"), "x\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feat\n")
	mustWrite(t, filepath.Join(root, "ok.go"), "resolved\n")

	st := git.Status{
		Branch: "main", Op: "merge",
		Conflicts: []git.Entry{{Path: "a.go", Letter: '!', XY: "UU"}, {Path: "ok.go", Letter: '!', XY: "UU"}, {Path: "lib/gone.go", Letter: '!', XY: "UD"}},
		Staged:    []git.Entry{{Path: "s.go", Letter: 'M', Staged: true}},
		Changes:   []git.Entry{{Path: "c.go", Letter: 'M'}},
	}
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: st}})
	press(m, "2")

	if got := rowTexts(&m.scm); !slices.Equal(got, []string{"Merge Changes:3", "a.go", "ok.go", "gone.go", "Staged Changes:1", "s.go", "Changes:1", "c.go"}) {
		t.Fatalf("rows = %q", got)
	}

	out := checkWidths(t, m)
	for _, want := range []string{"✓ Continue", "main*!", "a.go · both modified", "gone.go lib · deleted b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}

	if !hasCommand(m, "git.stageAllMergeChanges") {
		t.Fatal("Stage All Merge Changes is a command")
	}

	top := m.bodyTop(viewGit)
	sec := scmRowAt(m, rowSection, "Merge Changes", "")
	m.Update(tea.MouseMotionMsg{X: 3, Y: top + sec})

	if got := sidebarRow(t, m, sec); !strings.Contains(got, "+  3  ") || strings.Contains(got, "↶") {
		t.Fatalf("hovered Merge Changes header = %q", got)
	}

	f := scmRowAt(m, rowFile, "Merge Changes", "a.go")
	m.Update(tea.MouseMotionMsg{X: 3, Y: top + f})

	if got := sidebarRow(t, m, f); !strings.Contains(got, "src  +  ! ") || strings.Contains(got, "↶") {
		t.Fatalf("hovered conflict row = %q", got)
	}

	// No discard on the merge group; Commit refuses while it has entries.
	m.scm.sel = f
	press(m, "d")

	if m.modal != nil {
		t.Fatalf("d on a conflict: %+v", m.modal)
	}

	run := func(cmd tea.Cmd) {
		if cmd == nil {
			t.Fatal("no command")
		}

		m.Update(cmd())
	}

	m.scm.input.SetValue("merge feat")
	_, cmd := m.Update(keyMsg("C"))
	run(cmd)

	if m.scm.busy != "" || m.msg != "resolve the merge conflicts first" {
		t.Fatalf("commit with conflicts: busy=%q msg=%q", m.scm.busy, m.msg)
	}

	// ⏎ on a file with markers asks; without them it stages right away.
	_, cmd = m.Update(keyMsg("enter"))
	run(cmd)

	if m.modal == nil || m.modal.title != "Stage a.go with merge conflicts?" {
		t.Fatalf("⏎ on an unresolved file: %+v", m.modal)
	}

	m.modal = nil
	m.scm.sel = scmRowAt(m, rowFile, "Merge Changes", "ok.go")
	_, cmd = m.Update(keyMsg("enter"))
	run(cmd)

	if m.modal != nil || m.scm.busy != "staging" {
		t.Fatalf("⏎ on a resolved file stages: modal=%+v busy=%q", m.modal, m.scm.busy)
	}

	m.scm.busy = ""

	// A deletion conflict asks which side to keep.
	m.scm.sel = scmRowAt(m, rowFile, "Merge Changes", "lib/gone.go")
	press(m, "enter")

	if m.modal == nil || !strings.Contains(m.modal.title, "deleted by them and modified by us") || m.modal.items[0].label != "Keep Our Version" {
		t.Fatalf("⏎ on a deletion conflict: %+v", m.modal)
	}

	m.modal = nil

	// Stage All Merge Changes counts the files that still hold markers.
	run(m.scm.stageConflicts(m, root, ""))

	if m.modal == nil || m.modal.title != "Stage a.go with merge conflicts?" {
		t.Fatalf("stage all merge changes: %+v", m.modal)
	}

	m.modal = nil

	// Once the group is empty the button continues the merge.
	st.Conflicts = nil
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: st}})

	if !strings.Contains(checkWidths(t, m), "✓ Continue") {
		t.Fatal("Continue stays while the merge is open")
	}

	// An empty message box has nothing to commit without git's message …
	m.scm.input.SetValue("")
	_, cmd = m.Update(keyMsg("C"))
	run(cmd)

	if m.scm.busy != "" || m.msg != "commit message is empty" {
		t.Fatalf("continue without a message: busy=%q msg=%q", m.scm.busy, m.msg)
	}

	m.scm.input.Blur()

	// … and with it shows that message and commits it, as VS Code does.
	st.MergeMsg = "Merge branch 'develop' of example.com:web/app into main\n\nbody"
	m.scm.onGit(m, gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: st}})

	if out := checkWidths(t, m); !strings.Contains(out, "Merge branch 'dev") || strings.Contains(out, "body") {
		t.Fatalf("the message box shows git's first line:\n%s", out)
	}

	press(m, "C")

	if m.scm.busy != "committing" {
		t.Fatalf("C continues the merge: busy=%q msg=%q", m.scm.busy, m.msg)
	}
}
