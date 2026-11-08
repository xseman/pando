package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/proto"
)

// promptsModel is a model with session s1 on screen and two saved prompts,
// plus a file that is not one.
func promptsModel(t *testing.T) (*Model, string) {
	t.Helper()

	t.Setenv("PANDO_CONFIG_DIR", t.TempDir())

	dir := promptsDir()
	mustWrite(t, filepath.Join(dir, "push.md"), "pushni po častiach\n\n")
	mustWrite(t, filepath.Join(dir, "zmeny.md"), "vylistuj zmeny\nza celú session\n")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "not a prompt")

	m := testModel(t)
	m.switchSession("s1")
	m.focus = onMain

	return m, dir
}

// openPrompts presses alt+i and delivers the prompts it reads.
func openPrompts(t *testing.T, m *Model) []string {
	t.Helper()

	_, cmd := m.Update(keyMsg("alt+i"))
	fire(m, cmd)

	if m.modal == nil || m.modal.title != "Prompts" {
		t.Fatalf("alt+i opens the prompts, modal %+v", m.modal)
	}

	var labels []string
	for _, it := range m.modal.disp {
		labels = append(labels, it.label)
	}

	return labels
}

// nothingSent fails when the model forwarded input to a session.
func nothingSent(t *testing.T, m *Model) {
	t.Helper()

	select {
	case in := <-m.inputs:
		t.Fatalf("sent %+v", in)
	case <-time.After(50 * time.Millisecond):
	}
}

// A prompt is found by a word of its text, goes in as one paste with its
// trailing blank lines cut, and is listed first the next time.
func TestPromptInsert(t *testing.T) {
	m, _ := promptsModel(t)

	labels := openPrompts(t, m)
	if want := []string{"push", "zmeny", "+ New Prompt…"}; !slices.Equal(labels, want) {
		t.Fatalf("rows %q, want %q", labels, want)
	}

	press(m, "c", "e", "l", "u", "enter")

	if in := sent(t, m); !reflect.DeepEqual(in, proto.InputParams{ID: "s1", Paste: "vylistuj zmeny\nza celú session", Wake: true}) {
		t.Fatalf("sent %+v", in)
	}

	if labels := openPrompts(t, m); labels[0] != "zmeny" {
		t.Fatalf("the prompt inserted last is not first: %q", labels)
	}
}

// alt+⏎ pastes the prompt, then submits it with an Enter of its own.
func TestPromptSend(t *testing.T) {
	m, _ := promptsModel(t)
	openPrompts(t, m)

	_, cmd := m.Update(keyMsg("alt+enter"))

	if in := sent(t, m); in.Paste != "pushni po častiach" {
		t.Fatalf("sent %+v, want the paste first", in)
	}

	fire(m, cmd)

	if in := sent(t, m); !reflect.DeepEqual(in, proto.InputParams{ID: "s1", Text: "\r"}) {
		t.Fatalf("sent %+v, want the Enter", in)
	}
}

// With Problems over the panel's shell the prompt goes to the session in
// view: it went to the hidden shell, and alt+⏎ ran it there.
func TestPromptPanelNotTerminal(t *testing.T) {
	m, _ := promptsModel(t)
	m.tv.id = "t1"
	m.showPanel(ptProblems)

	openPrompts(t, m)
	m.Update(keyMsg("enter"))

	if in := sent(t, m); in.ID != "s1" {
		t.Fatalf("sent to %q, want the session in view", in.ID)
	}
}

// A dangling link, as an editor's lock file, is skipped, not an error that
// hides every prompt.
func TestPromptDanglingLink(t *testing.T) {
	m, dir := promptsModel(t)

	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, ".#push.md")); err != nil {
		t.Fatal(err)
	}

	if labels := openPrompts(t, m); !slices.Equal(labels, []string{"push", "zmeny", "+ New Prompt…"}) {
		t.Fatalf("rows %q", labels)
	}
}

// A CRLF file reads as LF: its \r redrew the row from column 0 and went
// into the paste.
func TestPromptCRLF(t *testing.T) {
	m, dir := promptsModel(t)
	mustWrite(t, filepath.Join(dir, "crlf.md"), "line one\r\nline two\r\n")

	openPrompts(t, m)

	if hint := m.modal.disp[0].hint; hint != "line one" {
		t.Fatalf("row shows %q", hint)
	}

	m.Update(keyMsg("enter"))

	if in := sent(t, m); in.Paste != "line one\nline two" {
		t.Fatalf("pasted %q", in.Paste)
	}
}

// A prompt found by its name comes before one that only mentions the word:
// typing "oprav" and ⏎ inserted the longer prompt listed first.
func TestPromptNameFirst(t *testing.T) {
	m, dir := promptsModel(t)
	mustWrite(t, filepath.Join(dir, "dlhy.md"), "navrhni opravu kódu")
	mustWrite(t, filepath.Join(dir, "oprav.md"), "Oprav chybu v teste")

	openPrompts(t, m)
	press(m, "o", "p", "r", "a", "v", "enter")

	if in := sent(t, m); in.Paste != "Oprav chybu v teste" {
		t.Fatalf("pasted %q, want the prompt named oprav", in.Paste)
	}
}

// A long first line shows cut to fit: a hint wider than the box vanished.
func TestPromptLongHint(t *testing.T) {
	m, dir := promptsModel(t)
	mustWrite(t, filepath.Join(dir, "long.md"), strings.Repeat("slovo ", 50))

	openPrompts(t, m)

	if screen := checkWidths(t, m); !strings.Contains(screen, "long   slovo slovo") || !strings.Contains(screen, "…") {
		t.Fatalf("the long prompt's text is not on screen:\n%s", screen)
	}
}

// A tab in the first line keeps the row as wide as the box: drawn as four
// cells but measured as none, it pushed the border out of line.
func TestPromptTabHint(t *testing.T) {
	m, dir := promptsModel(t)
	mustWrite(t, filepath.Join(dir, "aaa.md"), "\tindented\tline\nsecond")

	openPrompts(t, m)

	ends := map[int]bool{}
	screen := checkWidths(t, m)

	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, "│ aaa") || strings.Contains(l, "│ push") {
			ends[ansi.StringWidth(l[:strings.LastIndex(l, "│")])] = true
		}
	}

	if len(ends) != 1 {
		t.Fatalf("rows end at different columns: %v", ends)
	}
}

// The rest of the list is A–Z ignoring case and accents, not file bytes.
func TestPromptOrder(t *testing.T) {
	m, dir := promptsModel(t)
	mustWrite(t, filepath.Join(dir, "Banana.md"), "b")
	mustWrite(t, filepath.Join(dir, "Ábc.md"), "a")

	want := []string{"Ábc", "Banana", "push", "zmeny", "+ New Prompt…"}
	if labels := openPrompts(t, m); !slices.Equal(labels, want) {
		t.Fatalf("rows %q, want %q", labels, want)
	}
}

// With no session open the prompt goes nowhere and says so.
func TestPromptNoSession(t *testing.T) {
	m, _ := promptsModel(t)
	m.sess = ""
	m.focus = 0 // the sidebar

	openPrompts(t, m)
	_, cmd := m.Update(keyMsg("enter"))
	fire(m, cmd)

	nothingSent(t, m)

	if !m.msgErr {
		t.Fatalf("no error shown, status %q", m.msg)
	}
}

// New Prompt creates the file and opens it; an existing name opens that
// file untouched, a name with a slash is refused.
func TestPromptNew(t *testing.T) {
	m, dir := promptsModel(t)
	m.focus = 0 // the sidebar: the new file still takes the typing

	openPrompts(t, m)
	press(m, "down", "down", "enter") // + New Prompt…
	press(m, "r", "e", "v", "enter")

	path := filepath.Join(dir, "rev.md")
	if got := mustRead(t, path); got != "" {
		t.Fatalf("new prompt holds %q", got)
	}

	if m.pv.path != path || m.focus != onMain {
		t.Fatalf("editor shows %q, focus %d, want %q in focus", m.pv.path, m.focus, path)
	}

	openPrompts(t, m)
	press(m, "down", "down", "enter", "p", "u", "s", "h", "enter")

	if got := mustRead(t, filepath.Join(dir, "push.md")); got != "pushni po častiach\n\n" {
		t.Fatalf("an existing prompt changed: %q", got)
	}

	openPrompts(t, m)
	press(m, "down", "down", "enter", "a", "/", "b", "enter")

	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Fatalf("a name with a slash made %v", err)
	}
}

// ctrl+e opens the selected prompt's file, shift+del deletes it after asking.
func TestPromptEditDelete(t *testing.T) {
	m, dir := promptsModel(t)
	path := filepath.Join(dir, "push.md")

	openPrompts(t, m)
	_, cmd := m.Update(keyMsg("ctrl+e"))
	fire(m, cmd)

	if m.pv.path != path {
		t.Fatalf("ctrl+e shows %q, want %q", m.pv.path, path)
	}

	openPrompts(t, m)
	press(m, "shift+delete")

	if m.modal == nil || !m.modal.dialog {
		t.Fatalf("shift+del must ask, modal %+v", m.modal)
	}

	_, cmd = m.Update(keyMsg("enter"))
	fire(m, cmd)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("push.md is still there: %v", err)
	}
}
