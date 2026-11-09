package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// typeQuick types s into quick open and runs the text search the last key
// asked for, every batch through Update, as the program would.
func typeQuick(m *Model, s string) {
	var last tea.Cmd

	for _, r := range s {
		_, last = m.Update(keyMsg(string(r)))
	}

	runQuick(m, last)
}

// runQuick runs the text search cmd holds, every batch through Update.
func runQuick(m *Model, cmd tea.Cmd) {
	for _, msg := range runAll(cmd) {
		if tick, ok := msg.(quickTickMsg); ok {
			_, cmd := m.Update(tick)
			for cmd != nil {
				_, cmd = m.Update(cmd())
			}
		}
	}
}

// Tab and shift+tab go round the tabs, a click goes to one, and each shows
// its own kind; ctrl+p opens on All.
func TestQuickOpenTabs(t *testing.T) {
	m := testModelSized(t, 120, 30)
	send(m, keyMsg("ctrl+p"))

	if m.modal == nil || m.modal.tabs == nil || m.modal.tab != qoAll {
		t.Fatalf("ctrl+p: %+v", m.modal)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "╭  All  Files  Sessions  Text ──") {
		t.Fatalf("tabs in the title row:\n%s", out)
	}

	press(m, "tab")

	if out := checkWidths(t, m); m.modal.tab != qoFiles || !strings.Contains(out, "^t [tree]") {
		t.Fatalf("tab to Files, with its tree toggle: tab %d\n%s", m.modal.tab, out)
	}

	press(m, "tab")

	if got := labels(m); m.modal.tab != qoSessions || !slices.Equal(got, []string{"○ " + filepath.Base(m.ws) + "  ⌂ main", "└─ ○ shell"}) {
		t.Fatalf("Sessions lists the Sessions tree: %q", got)
	}

	press(m, "tab")

	if got := labels(m); m.modal.tab != qoText || !slices.Equal(got, []string{"type 2 or more characters"}) {
		t.Fatalf("Text with no query: %q", got)
	}

	press(m, "tab", "shift+tab")

	if m.modal.tab != qoText {
		t.Fatalf("tab round to All, shift+tab back to Text: %d", m.modal.tab)
	}

	x, y, _, _, _ := m.modal.rect(m)
	_, xs := choices(qoTabs, m.modal.tab, true)
	click(m, x+2+xs[qoFiles]+1, y, tea.MouseLeft)

	if m.modal == nil || m.modal.tab != qoFiles {
		t.Fatalf("a click on Files: %+v", m.modal)
	}

	m.Update(tea.WindowSizeMsg{Width: 36, Height: 20})

	if out := checkWidths(t, m); !strings.Contains(out, "Sessio… ^t [tree]") {
		t.Fatalf("tabs cut short on the right in a narrow box:\n%s", out)
	}
}

// All lists the recent files, the sessions waiting on you and the rest
// while browsing; a query lists files before sessions, each group capped,
// and the cap's row goes on to the group's tab with the query.
func TestQuickOpenAll(t *testing.T) {
	m := testModelSized(t, 120, 40)
	m.sessions[0].Name = "shell" // what the tree's filter finds it by
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "claude"}, Status: "blocked"})
	m.files = []string{filepath.Join(m.ws, "f03.go")}

	files := []string{"shell.txt"}
	for i := range 12 {
		files = append(files, fmt.Sprintf("f%02d.go", i))
	}

	m.quickOpen(indexMsg{ws: m.ws, files: files}, qoAll)

	if got := labels(m); !slices.Equal(got[:7], []string{"recently opened", "f03.go", "", "waiting on you", "× claude", "", "files"}) {
		t.Fatalf("browsing All: %q", got)
	}

	press(m, "s", "h", "e", "l", "l")

	if got := labels(m); !slices.Equal(got, []string{"files", "shell.txt", "", "sessions", "× " + filepath.Base(m.ws) + "  ⌂ main", "└─ ○ shell", "", "text"}) { // the text still searching
		t.Fatalf("files before sessions, those under their project: %q", got)
	}

	if m.modal.l.sel != 1 {
		t.Fatalf("⏎ opens the file: sel %d", m.modal.l.sel)
	}

	press(m, "ctrl+u", "f", "0")

	got := labels(m)
	if len(got) < 10 || got[9] != "+ 3 more" || m.modal.disp[9].hint != "Files ⇥" { // f10.go matches too
		t.Fatalf("eight files and the rest behind a row: %q", got)
	}

	m.modal.l.sel = 9
	press(m, "enter")

	if m.modal == nil || m.modal.tab != qoFiles || m.modal.input.Value() != "f0" || len(labels(m)) != 11 {
		t.Fatalf("+ 3 more goes to Files with the query: %+v", m.modal)
	}

	checkWidths(t, m)
}

// The text of the files: under the files in All, all of it in Text, which
// counts it; ⏎ opens the line with the match selected.
func TestQuickOpenText(t *testing.T) {
	m := testModelSized(t, 120, 30)
	mustWrite(t, filepath.Join(m.ws, "notes", "a.txt"), "first line\n\t\tthe needle is here\n")
	mustWrite(t, filepath.Join(m.ws, "b.txt"), "needle needle\n")
	mustWrite(t, filepath.Join(m.ws, "needle.md"), "nothing\n")
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"b.txt", "needle.md", "notes/a.txt"}}, qoAll)

	for _, r := range "needle" {
		press(m, string(r))
	}

	if got := labels(m); !slices.Equal(got, []string{"files", "needle.md", "", "text"}) || m.modal.disp[3].hint != "searching…" {
		t.Fatalf("while the search runs: %q", got)
	}

	press(m, "ctrl+u")
	typeQuick(m, "needle")

	want := []string{"files", "needle.md", "", "text", "needle needle", "the needle is here"}
	if got := labels(m); !slices.Equal(got, want) || m.modal.disp[3].hint != "3 in 2 files" || m.modal.disp[5].hint != "notes/a.txt:2" {
		t.Fatalf("All with text: %q", got)
	}

	if m.modal.l.sel != 1 {
		t.Fatalf("the text came after the selection, which stays: %d", m.modal.l.sel)
	}

	out := checkWidths(t, m)

	press(m, "shift+tab")

	if got := labels(m); m.modal.tab != qoText || !slices.Equal(got, want[4:]) || m.modal.note != "3 in 2 files" {
		t.Fatalf("Text: %q, %q\n%s", got, m.modal.note, out)
	}

	m.modal.l.sel = 1
	send(m, keyMsg("enter"))

	if !strings.HasSuffix(m.pv.path, filepath.Join("notes", "a.txt")) || m.pv.cur != (pos{1, 18}) {
		t.Fatalf("opened %s at %+v", m.pv.path, m.pv.cur)
	}
}

// A pasted query is a typed one: All leaves its browsing lists and Text
// searches it.
func TestQuickOpenPaste(t *testing.T) {
	m := testModelSized(t, 120, 30)
	mustWrite(t, filepath.Join(m.ws, "b.txt"), "needle needle\n")
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"b.txt"}}, qoAll)

	m.Update(tea.PasteMsg{Content: "main"})

	if got := labels(m); !slices.ContainsFunc(got, func(l string) bool { return strings.HasSuffix(l, "⌂ main") }) {
		t.Fatalf("pasted into All: %q", got)
	}

	press(m, "ctrl+u", "shift+tab")

	_, cmd := m.Update(tea.PasteMsg{Content: "needle"})
	runQuick(m, cmd)

	if got := labels(m); !slices.Equal(got, []string{"needle needle"}) {
		t.Fatalf("pasted into Text: %q", got)
	}
}

// A position after the name opens there from All too.
func TestQuickOpenAllLineSuffix(t *testing.T) {
	m := previewModel(t, "file", "one")
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"src/deep/x.go", "README.md"}}, qoAll)
	press(m, "x", ".", "g", "o", ":", "1", ":", "3")
	send(m, keyMsg("enter"))

	if !strings.HasSuffix(m.pv.path, "x.go") || m.pv.cur != (pos{0, 2}) {
		t.Fatalf("opened %s at %+v", m.pv.path, m.pv.cur)
	}
}

// A narrower screen cuts the lines anew, the match kept, and a long path
// keeps its end.
func TestQuickOpenNarrow(t *testing.T) {
	m := testModelSized(t, 160, 30)
	deep := filepath.Join("src", "some", "really", "deeply", "nested", "directory", "structure", "file_with_long_name.txt")
	mustWrite(t, filepath.Join(m.ws, "long.txt"), strings.Repeat("x", 300)+" needle end\n")
	mustWrite(t, filepath.Join(m.ws, deep), "needle deep\n")
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"long.txt", deep}}, qoText)
	typeQuick(m, "needle")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	out := checkWidths(t, m)
	if !strings.Contains(out, "needle end long.txt:1") || !strings.Contains(out, "file_with_long_name.txt:1") {
		t.Fatalf("the match and the file's name on screen:\n%s", out)
	}

	if it := m.modal.disp[1]; !strings.HasPrefix(it.hint, "…") {
		t.Fatalf("a long path is cut on the left: %q", it.hint)
	}
}

// Text says why it lists nothing, and stops at quickTextLimit matches.
func TestQuickOpenTextStates(t *testing.T) {
	m := testModelSized(t, 120, 30)
	mustWrite(t, filepath.Join(m.ws, "many.txt"), strings.Repeat("needle\n", 250))
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"many.txt"}}, qoText)

	typeQuick(m, "z")

	if got := labels(m); !slices.Equal(got, []string{"type 2 or more characters"}) {
		t.Fatalf("one character: %q", got)
	}

	typeQuick(m, "zz")

	if got := labels(m); !slices.Equal(got, []string{"no results for “zzz”"}) || m.modal.disp[0].hint != "alt+⏎ Search" {
		t.Fatalf("nothing found: %q", got)
	}

	press(m, "ctrl+u")
	typeQuick(m, "needle")

	got := labels(m)
	if len(got) != quickTextLimit+1 || got[quickTextLimit] != "all in Search" || m.modal.note != "200+ in 1 file" {
		t.Fatalf("capped: %d rows, last %q, note %q", len(got), got[len(got)-1], m.modal.note)
	}

	press(m, "tab")

	if got := labels(m); got[len(got)-1] != fmt.Sprintf("+ %d+ more", quickTextLimit-quickTextAll) {
		t.Fatalf("All shows %d lines: %q", quickTextAll, got)
	}

	checkWidths(t, m)
}

// alt+⏎ hands the query to the Search view from All and Text, not from
// Files.
func TestQuickOpenToSearch(t *testing.T) {
	m := testModelSized(t, 120, 30)
	m.quickOpen(indexMsg{ws: m.ws, files: []string{"README.md"}}, qoFiles)
	press(m, "h", "i", "alt+enter")

	if m.modal == nil {
		t.Fatal("alt+⏎ in Files does nothing")
	}

	press(m, "shift+tab", "alt+enter")

	if m.modal != nil || m.sr.query.Value() != "hi" {
		t.Fatalf("alt+⏎ in All: modal %v, Search %q", m.modal, m.sr.query.Value())
	}
}

// excerpt drops the indentation and starts a little before a match that
// would end past the room, on a rune.
func TestExcerpt(t *testing.T) {
	long := strings.Repeat("x", 60) + "needle" + "yyy"
	wide := strings.Repeat("š", 30) + "needle"

	for _, c := range []struct {
		ln   searchLine
		room int
		want string
	}{
		{searchLine{text: "\t\tfoo bar", ranges: [][2]int{{6, 9}}}, 40, "foo bar"},
		{searchLine{text: long, ranges: [][2]int{{60, 66}}}, 80, long},
		{searchLine{text: long, ranges: [][2]int{{60, 66}}}, 30, "…" + long[40:]}, // the line's end fills the room
		{searchLine{text: long + strings.Repeat("z", 40), ranges: [][2]int{{60, 66}}}, 30, "…" + long[46:] + strings.Repeat("z", 40)},
		{searchLine{text: "no match"}, 3, "no match"},
		{searchLine{text: long, ranges: [][2]int{{60, 66}}}, 12, "…" + long[56:]}, // a narrow box still shows the match
	} {
		if got := excerpt(c.ln, c.room); got != c.want {
			t.Errorf("excerpt(%q, %d) = %q, want %q", c.ln.text, c.room, got, c.want)
		}
	}

	if got := excerpt(searchLine{text: wide, ranges: [][2]int{{60, 66}}}, 20); !utf8.ValidString(got) || !strings.HasSuffix(got, "needle") {
		t.Errorf("a cut inside a rune: %q", got)
	}
}

// The Sessions tab is the Sessions tree, every project open: those with a
// session awake on top, then the rest A–Z, one with no session too; the
// session on screen is selected, an asleep one says since when. A query
// finds whole words in any case and without accents, shows them, and
// selects the first session found; a project found brings all it holds, and
// ⏎ on one with no session switches into it, on top of the tree. "@asleep"
// narrows to the sessions asleep.
func TestQuickOpenSessionTree(t *testing.T) {
	m := testModelSized(t, 120, 40)
	bare, c := addProject(t, m, "dev"), addProject(t, m, "main")
	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "c1", Workspace: c, Agent: "claude", Name: "Zmazané pravidlá"}, Status: "asleep", Updated: time.Now().Add(-3 * time.Hour)})
	m.sess = "s1"
	m.quickOpen(indexMsg{ws: m.ws}, qoSessions)

	headA, headC := "○ "+filepath.Base(m.ws)+"  ⌂ main", "◌ "+filepath.Base(c)+"  ⌂ main"
	want := []string{headA, "└─ ○ shell", "· " + filepath.Base(bare) + "  ⌂ dev", headC, "└─ ◌ Zmazané pravidlá"}

	if got := labels(m); !slices.Equal(got, want) || m.modal.disp[m.modal.l.sel].label != "└─ ○ shell" {
		t.Fatalf("the tree, the session on screen selected: %q, sel %d", got, m.modal.l.sel)
	}

	if h := m.modal.disp[4].hint; h != "3h" {
		t.Fatalf("an asleep session says since when: %q", h)
	}

	checkWidths(t, m)
	typeText(m, "zmazane PRAV")

	if got := labels(m); !slices.Equal(got, want[3:]) || m.modal.l.sel != 1 {
		t.Fatalf("found without accents, its session selected: %q, sel %d", got, m.modal.l.sel)
	}

	var lit []string

	for _, s := range m.modal.disp[1].segs {
		if s.st.GetBold() {
			lit = append(lit, s.s)
		}
	}

	if !slices.Equal(lit, []string{"Zmazané", "prav"}) {
		t.Fatalf("the words found are lit whole: %q", lit)
	}

	press(m, "ctrl+u")

	m.st.Settings.SpHide = []string{"asleep"} // the panel's Filter: a search still finds them
	typeText(m, "@asleep")

	if got := labels(m); !slices.Equal(got, want[3:]) {
		t.Fatalf("@asleep, hidden in the panel: %q", got)
	}

	press(m, "ctrl+u")
	typeText(m, filepath.Base(bare))

	if got := labels(m); !slices.Equal(got, want[2:3]) {
		t.Fatalf("a project with no session found by its name: %q", got)
	}

	press(m, "enter")

	if m.ws != bare || !m.ag.top(m, bare) {
		t.Fatalf("⏎ switches into it, on top of the tree: ws %q", m.ws)
	}
}

// All shows the first sessions found under their projects, and a row that
// goes on to the Sessions tab with the query.
func TestQuickOpenAllSessions(t *testing.T) {
	m := testModelSized(t, 120, 40)
	for i := range 5 {
		m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: fmt.Sprint("f", i), Workspace: m.ws, Agent: "claude", Name: fmt.Sprint("fix ", i)}, Status: "idle"})
	}

	m.quickOpen(indexMsg{ws: m.ws}, qoAll)
	typeText(m, "fix")

	got := labels(m)
	if want := []string{"sessions", "○ " + filepath.Base(m.ws) + "  ⌂ main", "├─ ○ fix 0", "├─ ○ fix 1", "├─ ○ fix 2", "+ 2 more"}; len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("three sessions under their project: %q", got)
	}

	if m.modal.l.sel != 2 {
		t.Fatalf("no file found: the first session is selected, sel %d", m.modal.l.sel)
	}

	if m.modal.disp[5].hint != "Sessions ⇥" {
		t.Fatalf("the rest behind a row: %q", m.modal.disp[5].hint)
	}

	m.modal.l.sel = 5
	press(m, "enter")

	if m.modal == nil || m.modal.tab != qoSessions || m.modal.input.Value() != "fix" || len(labels(m)) != 6 {
		t.Fatalf("+ 2 more goes to Sessions with the query: %q", labels(m))
	}

	bare := addProject(t, m, "dev")
	m.quickOpen(indexMsg{ws: m.ws}, qoAll)
	typeText(m, filepath.Base(bare))

	if got, want := labels(m), []string{"sessions", "· " + filepath.Base(bare) + "  ⌂ dev"}; len(got) < len(want) || !slices.Equal(got[:len(want)], want) || m.modal.l.sel != 1 {
		t.Fatalf("a project with no session found by its name, selected: %q, sel %d", got, m.modal.l.sel)
	}
}

// FuzzWordRuns checks that what a row lights up is whole characters of the
// label, in order, apart, and each one a word of the query as hits finds it.
func FuzzWordRuns(f *testing.F) {
	for _, s := range [][2]string{{"zmazane prav", "Zmazané pravidlá"}, {"a", "aaa"}, {"ss", "Less CSS"}, {"i", "İstanbul"}, {"x", "\xff\xfe"}, {"", "abc"}} {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, q, s string) {
		edge := map[int]bool{len(s): true} // where a character starts, a broken byte being one
		for i := 0; i < len(s); {
			_, n := utf8.DecodeRuneInString(s[i:])
			edge[i], i = true, i+n
		}

		last := 0

		for _, r := range wordRuns(q, s) {
			if r[0] < last || r[1] <= r[0] || r[1] > len(s) {
				t.Fatalf("%q in %q: run %v after %d", q, s, r, last)
			}

			if !edge[r[0]] || !edge[r[1]] {
				t.Fatalf("%q in %q: run %v cuts a character", q, s, r)
			}

			if !slices.ContainsFunc(strings.Fields(unaccented(q)), func(w string) bool { return strings.Contains(unaccented(s[r[0]:r[1]]), w) }) {
				t.Fatalf("%q in %q: run %q is no word of the query", q, s, s[r[0]:r[1]])
			}

			last = r[1]
		}
	})
}
