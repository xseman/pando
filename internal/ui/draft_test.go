package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// untitledModel is an empty main area with a new untitled buffer typed into.
func untitledModel(t *testing.T, text string) *Model {
	t.Helper()
	m := testModelSized(t, 100, 24)
	m.sess, m.focus = "", onMain
	fire(m, m.newUntitled())
	press(m, strings.Split(text, "")...)

	return m
}

func TestUntitledTypesAndSaves(t *testing.T) {
	m := untitledModel(t, "hello")
	if !m.pv.untitled() || !m.pv.editable() {
		t.Fatalf("an untitled buffer is an editor: %+v", m.pv.kind)
	}

	if m.pv.name != "Untitled-1" || m.pv.path != "" {
		t.Fatalf("name %q path %q", m.pv.name, m.pv.path)
	}

	if got := m.pv.buf.text(); got != "hello" {
		t.Fatalf("typed %q", got)
	}

	if !m.pv.dirty() {
		t.Fatal("a typed-into buffer is dirty")
	}

	if out := checkWidths(t, m); !strings.Contains(out, "Untitled-1 ●") || !strings.Contains(out, "hello") {
		t.Fatalf("screen:\n%s", out)
	}

	// A second one gets the next number, and the two are separate tabs.
	fire(m, m.newUntitled())

	if m.pv.name != "Untitled-2" || len(m.editors) != 2 {
		t.Fatalf("second untitled %q, %d editors", m.pv.name, len(m.editors))
	}

	send(m, keyMsg("ctrl+tab"))

	if m.pv.name != "Untitled-1" || m.pv.buf.text() != "hello" {
		t.Fatalf("back to %q with %q", m.pv.name, m.pv.buf.text())
	}

	// ⌃s asks where it goes, prefilled with the workspace.
	press(m, "ctrl+s")

	if m.modal == nil || m.modal.title != "Save as" {
		t.Fatalf("modal %+v", m.modal)
	}

	if got := m.modal.input.Value(); got != m.ws+string(filepath.Separator) {
		t.Fatalf("prefilled %q, want the workspace", got)
	}

	m.modal.input.SetValue(filepath.Join(m.ws, "notes.txt"))
	send(m, keyMsg("enter"))

	b, err := os.ReadFile(filepath.Join(m.ws, "notes.txt"))
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("saved file = %q, %v", b, err)
	}

	if m.pv.untitled() || m.pv.name != "" || m.pv.dirty() {
		t.Fatalf("after save: path %q name %q dirty %v", m.pv.path, m.pv.name, m.pv.dirty())
	}

	if m.editors[m.edIdx].path != filepath.Join(m.ws, "notes.txt") {
		t.Fatalf("the tab follows the file: %+v", m.editors[m.edIdx])
	}

	if out := checkWidths(t, m); !strings.Contains(out, "notes.txt") {
		t.Fatalf("strip keeps the old name:\n%s", out)
	}
}

func TestUntitledSaveAsOverwrite(t *testing.T) {
	m := untitledModel(t, "new")
	press(m, "ctrl+s")
	m.modal.input.SetValue(filepath.Join(m.ws, "README.md"))
	send(m, keyMsg("enter"))
	// README.md exists in the test workspace, so it asks before clobbering it.
	if m.modal == nil || !strings.Contains(m.modal.title, "already exists") {
		t.Fatalf("modal %+v", m.modal)
	}

	if b, _ := os.ReadFile(filepath.Join(m.ws, "README.md")); string(b) != "# hi\n" {
		t.Fatalf("nothing is written until the question is answered: %q", b)
	}

	if labels(m)[0] != "Overwrite it" {
		t.Fatalf("items %v", labels(m))
	}

	send(m, keyMsg("enter"))

	if b, _ := os.ReadFile(filepath.Join(m.ws, "README.md")); string(b) != "new\n" {
		t.Fatalf("overwritten = %q", b)
	}
}

func TestUntitledCloseAsks(t *testing.T) {
	m := untitledModel(t, "wip")
	// ⌃w on unsaved text asks; Cancel keeps the tab and the text.
	send(m, keyMsg("ctrl+w"))

	if m.modal == nil || !strings.Contains(m.modal.title, "Untitled-1") {
		t.Fatalf("modal %+v", m.modal)
	}

	if got := labels(m); len(got) != 3 || got[0] != "Save and close" || got[1] != "Close anyway" {
		t.Fatalf("items %v", got)
	}

	press(m, "down", "down")
	send(m, keyMsg("enter")) // Cancel

	if m.modal != nil || len(m.editors) != 1 || m.pv.buf.text() != "wip" {
		t.Fatalf("cancel kept nothing: %d editors", len(m.editors))
	}

	// Save and close asks where first, and only then drops the tab.
	send(m, keyMsg("ctrl+w"))
	send(m, keyMsg("enter")) // Save and close

	if m.modal == nil || m.modal.title != "Save as" {
		t.Fatalf("save and close asks where: %+v", m.modal)
	}

	if len(m.editors) != 1 {
		t.Fatal("the tab stays until the save goes through")
	}

	m.modal.input.SetValue(filepath.Join(m.ws, "wip.txt"))
	send(m, keyMsg("enter"))

	if b, _ := os.ReadFile(filepath.Join(m.ws, "wip.txt")); string(b) != "wip\n" {
		t.Fatalf("wrote %q", b)
	}

	if len(m.editors) != 0 || m.pv.kind != "" {
		t.Fatalf("the tab closed after the save: %d editors, kind %q", len(m.editors), m.pv.kind)
	}

	// Close anyway drops it.
	m2 := untitledModel(t, "gone")
	send(m2, keyMsg("ctrl+w"))
	press(m2, "down")
	send(m2, keyMsg("enter"))

	if m2.modal != nil || len(m2.editors) != 0 || m2.pv.kind != "" {
		t.Fatalf("close anyway: %d editors, modal %+v", len(m2.editors), m2.modal)
	}

	// esc asks too, rather than dropping the text silently.
	m3 := untitledModel(t, "keep")
	press(m3, "esc")

	if m3.modal == nil || !strings.Contains(m3.modal.title, "without saving") {
		t.Fatalf("esc on unsaved text asks: %+v", m3.modal)
	}
}

func TestDoubleClickEmptyPanelStartsAFile(t *testing.T) {
	m := testModelSized(t, 100, 24)
	m.sess, m.preview = "", false
	m.pv.close(m)

	if out := checkWidths(t, m); !strings.Contains(out, "new file") {
		t.Fatalf("the empty panel offers one:\n%s", out)
	}
	// One click is not enough: the panel is a click target for the focus too.
	click(m, m.mainX()+4, 6, tea.MouseLeft)

	if m.pv.untitled() {
		t.Fatal("a single click does not start a file")
	}

	click(m, m.mainX()+4, 6, tea.MouseLeft)

	if !m.pv.untitled() || m.focus != onMain {
		t.Fatalf("a double click starts one: kind %q focus %d", m.pv.kind, m.focus)
	}

	press(m, "h", "i")

	if m.pv.buf.text() != "hi" {
		t.Fatalf("typed %q", m.pv.buf.text())
	}

	checkWidths(t, m)
}

func TestDraftsSurviveRestart(t *testing.T) {
	m := untitledModel(t, "draft text")
	readme := filepath.Join(m.ws, "README.md")
	fire(m, m.openFile(readme))
	press(m, "X") // unsaved edit to a file that exists

	// What the tick sends the daemon.
	if cmd := m.saveDrafts(); cmd == nil {
		t.Fatal("unsaved text is sent")
	}

	if got := m.savedDrafts; len(got) != 2 || got["untitled:Untitled-1"] != "draft text\n" || got[readme] != "X# hi\n" {
		t.Fatalf("drafts %v", got)
	}

	if m.saveDrafts() != nil {
		t.Fatal("unchanged drafts are not sent again")
	}

	// A saved editor stops having a draft.
	press(m, "ctrl+s")

	if m.saveDrafts() == nil || len(m.savedDrafts) != 1 {
		t.Fatalf("after saving: %v", m.savedDrafts)
	}

	// A new TUI: the strip remembers the untitled tab, the drafts fill it in.
	m.saveEditors()
	st := m.st
	m2 := New(st, m.wss, nil, m.ws, nil)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	if len(m2.editors) != 2 {
		t.Fatalf("restored %d editors: %+v", len(m2.editors), m2.st.Editors[m.ws])
	}

	fire(m2, m2.Init())
	m2.Update(draftsMsg([]proto.Draft{{WS: m.ws, Name: "Untitled-1", Text: "draft text"}}))

	i := 0
	if m2.editors[1].name == "Untitled-1" {
		i = 1
	}

	if m2.editors[i].name != "Untitled-1" {
		t.Fatalf("no untitled tab: %+v", m2.editors)
	}

	fire(m2, m2.showEditor(i))

	if m2.pv.name != "Untitled-1" || m2.pv.buf.text() != "draft text" || !m2.pv.dirty() {
		t.Fatalf("restored %q = %q dirty %v", m2.pv.name, m2.pv.text(), m2.pv.dirty())
	}

	if out := checkWidths(t, m2); !strings.Contains(out, "Untitled-1 ●") || !strings.Contains(out, "draft text") {
		t.Fatalf("screen:\n%s", out)
	}
}

func TestDraftRestoresAFileEdit(t *testing.T) {
	m, path := editorModel(t, "a.go", "package a\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0
	st, _ := os.Stat(path)
	m.Update(draftsMsg([]proto.Draft{{WS: m.ws, Path: path, Text: "package a // wip\n", Mod: st.ModTime()}}))
	fire(m, m.pv.load(m))

	if !m.pv.dirty() || m.pv.buf.text() != "package a // wip" {
		t.Fatalf("restored %q dirty %v", m.pv.buf.text(), m.pv.dirty())
	}
	// The mtime came back with it, so saving does not need to be forced.
	if !m.pv.buf.modAt.Equal(st.ModTime()) {
		t.Fatalf("modAt %v, want %v", m.pv.buf.modAt, st.ModTime())
	}

	press(m, "ctrl+s")

	if b, _ := os.ReadFile(path); string(b) != "package a // wip\n" {
		t.Fatalf("saved %q", b)
	}

	// A draft for a file that is gone is dropped rather than reopened.
	m2 := testModelSized(t, 100, 24)
	m2.Update(draftsMsg([]proto.Draft{{WS: m2.ws, Path: filepath.Join(m2.ws, "gone.go"), Text: "x"}}))

	if len(m2.editors) != 0 {
		t.Fatalf("reopened a file that is gone: %+v", m2.editors)
	}
}

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "beta"))
	mustWrite(t, filepath.Join(dir, "b.txt"), "")
	mustWrite(t, filepath.Join(dir, "c.txt"), "")
	got := completePath(filepath.Join(dir, "b"))

	want := []string{filepath.Join(dir, "b.txt"), filepath.Join(dir, "beta") + "/"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("completePath = %v, want %v", got, want)
	}
}

// A tab restored with a draft but never opened must still count as unsaved:
// the next tick would otherwise decide it is clean and forget its text.
func TestUnopenedDraftTabKeepsItsText(t *testing.T) {
	m := testModelSized(t, 100, 24)
	readme := filepath.Join(m.ws, "README.md")
	st := m.st
	st.Editors = map[string]proto.Editors{m.ws: {Open: []proto.Editor{
		{Name: "Untitled-1"}, {Path: readme},
	}, Active: 1}}
	m2 := New(st, m.wss, nil, m.ws, nil)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m2.Update(draftsMsg([]proto.Draft{{WS: m.ws, Name: "Untitled-1", Text: "kept\n"}}))

	if m2.pv.name == "Untitled-1" {
		t.Fatal("README.md is the active tab in this setup")
	}

	if !m2.editors[0].dirty() {
		t.Fatal("the unopened draft tab is unsaved")
	}

	if n := m2.dirtyEditors(); n != 1 {
		t.Fatalf("dirtyEditors = %d, want 1", n)
	}

	if out := checkWidths(t, m2); !strings.Contains(out, "Untitled-1 ●") {
		t.Fatalf("the strip marks it:\n%s", out)
	}
	// The tick sends it back unchanged rather than deleting it.
	if cmd := m2.saveDrafts(); cmd != nil {
		t.Fatal("an unchanged draft is not resent")
	}

	if m2.savedDrafts["untitled:Untitled-1"] != "kept\n" {
		t.Fatalf("drafts %v", m2.savedDrafts)
	}
	// Opening it shows the text.
	fire(m2, m2.showEditor(0))

	if m2.pv.buf.text() != "kept" || !m2.pv.dirty() {
		t.Fatalf("opened %q dirty %v", m2.pv.buf.text(), m2.pv.dirty())
	}
}

// The tab strip caps at 20 and drops the oldest, which must never be one
// holding unsaved text: the tab going takes its draft with it.
func TestStripLRUSkipsUnsavedEditors(t *testing.T) {
	m := testModelSized(t, 100, 24)
	m.sess, m.focus = "", onMain
	fire(m, m.newUntitled())
	press(m, "k", "e", "e", "p")

	for i := range 25 {
		p := filepath.Join(m.ws, fmt.Sprintf("f%d.go", i))
		mustWrite(t, p, "package f\n")
		fire(m, m.openFile(p))
	}

	if len(m.editors) != 20 {
		t.Fatalf("%d editors, want the cap", len(m.editors))
	}

	i := slices.IndexFunc(m.editors, func(e preview) bool { return e.name == "Untitled-1" })
	if i < 0 {
		t.Fatal("the unsaved buffer was evicted and its draft would go with it")
	}

	if !m.editors[i].dirty() || m.editors[i].buf.text() != "keep" {
		t.Fatalf("kept but empty: %+v", m.editors[i])
	}
	// edIdx still points at the editor on screen after an eviction.
	if m.editors[m.edIdx].id() != m.pv.id() {
		t.Fatalf("edIdx %d is not the shown editor %q", m.edIdx, m.pv.path)
	}
}

// Close All Editors keeps what is unsaved, as Close Other Editors does.
func TestCloseAllKeepsUnsavedEditors(t *testing.T) {
	m := untitledModel(t, "wip")
	readme := filepath.Join(m.ws, "README.md")
	fire(m, m.openFile(readme))
	fire(m, m.closeEditors())

	if len(m.editors) != 1 || m.editors[0].name != "Untitled-1" {
		t.Fatalf("kept %+v", m.editors)
	}

	if m.pv.name != "Untitled-1" || m.pv.buf.text() != "wip" {
		t.Fatalf("the kept editor shows: %q %q", m.pv.name, m.pv.text())
	}
	// Saved, it closes with the rest.
	m.pv.buf.dirty = false
	m.editors[0] = m.pv.snapshot()
	fire(m, m.closeEditors())

	if len(m.editors) != 0 || m.pv.kind != "" {
		t.Fatalf("clean editors close: %d left, kind %q", len(m.editors), m.pv.kind)
	}
}

// Drafts arrive asynchronously: what was typed in the meantime is newer and
// must not be replaced by what the daemon had.
func TestTypingBeatsALateDraft(t *testing.T) {
	m, path := editorModel(t, "a.go", "package a\n")
	m.editors, m.edIdx = []preview{m.pv.snapshot()}, 0
	press(m, "Z") // typed before the drafts came back
	m.Update(draftsMsg([]proto.Draft{{WS: m.ws, Path: path, Text: "stale\n"}}))
	fire(m, m.pv.load(m))

	if got := m.pv.buf.text(); got != "Zpackage a" {
		t.Fatalf("the late draft won: %q", got)
	}
}

// Save As picks the formatter from the name just given, so a buffer typed
// without one still lands on disk formatted, as an ordinary ⌃s would.
func TestSaveAsFormatsWithTheNewName(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is what the default [format] uses")
	}

	m := untitledModel(t, "package a")
	m.st.Settings.FmtSave = true
	press(m, "enter")
	press(m, strings.Split("func A()  int{return 1}", "")...)
	press(m, "ctrl+s")
	to := filepath.Join(m.ws, "fmt.go")
	m.modal.input.SetValue(to)
	send(m, keyMsg("enter"))

	b, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}

	if string(b) != "package a\n\nfunc A() int { return 1 }\n" {
		t.Fatalf("gofmt did not run on the way out: %q", b)
	}

	if m.pv.dirty() || m.pv.path != to {
		t.Fatalf("after save: %q dirty %v", m.pv.path, m.pv.dirty())
	}
}
