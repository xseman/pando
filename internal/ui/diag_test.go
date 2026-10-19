package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// diagModel is an editor on a.go, its line 2 tab-indented, with
// lsp_diagnostics on and an error and a warning reported for it.
func diagModel(t *testing.T) (*Model, string) {
	t.Helper()

	m, path := editorModel(t, "a.go", "package a\n\nfunc A() {\n\tx := undefined\n}\n")
	m.st.Settings.Diags = true
	m.Update(diagMsg{path: {
		{Line: 3, Col: 6, EndLine: 3, EndCol: 15, Severity: 1, Message: "undefined: undefined", Source: "compiler"},
		{Line: 3, Col: 1, EndLine: 3, EndCol: 2, Severity: 2, Message: "declared and not used: x"},
		{Line: 0, Col: 0, EndLine: 0, EndCol: 7, Severity: 3, Message: "an information"},
	}})

	return m, path
}

// TestDiagnosticsInEditor squiggles the errors and warnings under their
// text, a tab counting tab_size cells, says the one under the cursor in the
// footer and the one under the pointer in a tip, counts them in the status
// bar, and F8 / shift+F8 step through them.
func TestDiagnosticsInEditor(t *testing.T) {
	m, _ := diagModel(t)

	out := m.View().Content
	if strings.Count(out, "\x1b[4:3m") < 2 || !strings.Contains(ansi.Strip(out), "✖ 1 ⚠ 1") {
		t.Fatalf("squiggles and the status bar's counts:\n%q", out)
	}

	row := m.pv.probs[3]
	if len(row) != 2 || row[0].from != tabW+5 || row[0].to != tabW+14 || row[1].from != tabW || row[1].to != tabW+1 {
		t.Fatalf("the problems' cells on the row: %+v", row)
	}

	press(m, "f8")

	if c := m.pv.at(); c.line != 3 || c.col != tabW {
		t.Fatalf("F8 goes to the first problem's start: %+v", c)
	}

	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "⚠ declared and not used: x") {
		t.Fatalf("the footer says the problem under the cursor:\n%s", out)
	}

	press(m, "f8")

	if c := m.pv.at(); c.col != tabW+5 {
		t.Fatalf("F8 again: the next one, %+v", c)
	}

	press(m, "f8")

	if c := m.pv.at(); c.line != 3 || c.col != tabW {
		t.Fatalf("F8 past the last goes around to the first: %+v", c)
	}

	press(m, "shift+f8")

	if c := m.pv.at(); c.line != 3 || c.col != tabW+5 {
		t.Fatalf("shift+F8 before the first goes around to the last: %+v", c)
	}

	// The pointer resting on a squiggle shows its message.
	var x, y int

	for i, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if j := strings.Index(l, "undefined"); j >= 0 && strings.Contains(l, ":=") {
			x, y = ansi.StringWidth(l[:j])+2, i
		}
	}

	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	elapse(m, time.Second)

	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "✖ undefined: undefined  compiler") {
		t.Fatalf("the tip under the pointer:\n%s", out)
	}

	checkWidths(t, m)
}

// TestDiagnosticPastTheEnd: a truncated file's error past its final
// newline stands on the last line's last character, where F8 goes and the
// footer says it.
func TestDiagnosticPastTheEnd(t *testing.T) {
	m, path := editorModel(t, "w.go", "package w\n\nfunc T() {\n\t_ = 1\n")
	m.st.Settings.Diags = true
	m.Update(diagMsg{path: {{Line: 4, Severity: 1, Message: "expected ';', found 'EOF'"}}})
	m.View()

	if row := m.pv.probs[3]; len(row) != 1 || row[0].from != tabW+4 || row[0].to != tabW+5 {
		t.Fatalf("on the last line's last cell: %+v", m.pv.probs)
	}

	press(m, "f8")
	press(m, "f8")

	if c := m.pv.at(); c.line != 3 || c.col != tabW+5 {
		t.Fatalf("F8 goes to the end of the last line, and stays: %+v", c)
	}

	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "✖ expected ';', found 'EOF'") {
		t.Fatalf("the footer says it:\n%s", out)
	}
}

// TestDiagnosticsOff: with lsp_diagnostics off nothing is kept, and turning
// it off drops what there was and the servers, which turned on again report
// afresh.
func TestDiagnosticsOff(t *testing.T) {
	m, path := diagModel(t)

	m.lsps.failed = map[string]error{"go": errors.New("gone")}
	m.st.Settings.Diags = false
	m.noteDiags()

	if m.lsps.failed != nil {
		t.Fatal("turned off, the servers go, and turned on start afresh")
	}

	if len(m.diags) != 0 || strings.Contains(m.View().Content, "\x1b[4:3m") {
		t.Fatal("turned off, the problems go")
	}

	m.Update(diagMsg{path: {{Line: 0, Severity: 1, Message: "x"}}})

	if len(m.diags) != 0 {
		t.Fatal("off, new ones are not kept")
	}
}

// TestDiagnosticsInDiff squiggles a working-tree diff's new side by line
// number, its columns counted in the file on disk, inline and side by side;
// a staged diff's new side is not the file, and shows none.
func TestDiagnosticsInDiff(t *testing.T) {
	for _, split := range []bool{false, true} {
		m := testModelSized(t, 160, 30)
		m.st.Settings.Diags, m.st.Settings.DiffView = true, ifElse(split, "split", "inline")
		mustWrite(t, filepath.Join(m.ws, "f.go"), opsText(300, map[int]string{100: "m"}))

		m.pv = preview{kind: pvDiff, root: m.ws, path: "f.go", entry: git.Entry{Path: "f.go", Letter: 'M'}}
		m.preview, m.focus = true, onMain
		raw := opsDiff(300, map[int]string{100: "m"})
		lines, plain, meta, numW := render(pvDiff, "f.go", raw, true)
		m.pv.onLoad(m, previewMsg{key: m.pv.id(), raw: raw, lines: lines, plain: plain, meta: meta, numW: numW, disk: fileLines(filepath.Join(m.ws, "f.go"))})
		m.Update(diagMsg{filepath.Join(m.ws, "f.go"): {{Line: 100, Col: 0, EndLine: 100, EndCol: 7, Severity: 1, Message: "bad"}}})

		if out := m.View().Content; !strings.Contains(out, "\x1b[4:3m") {
			t.Fatalf("split %v: the changed line's problem is squiggled", split)
		}

		// From the first change, F8 steps line by line, a problem in a gap
		// unfolding it, around the end; shift+F8 back.
		m.Update(diagMsg{filepath.Join(m.ws, "f.go"): {
			{Line: 100, EndLine: 100, EndCol: 7, Severity: 1, Message: "bad"},
			{Line: 100, Col: 3, EndLine: 100, EndCol: 5, Severity: 2, Message: "also"},
			{Line: 200, Col: 2, EndLine: 200, EndCol: 4, Severity: 1, Message: "far"},
		}})

		line := func(key string) int {
			press(m, key)
			return m.pv.lineNo(m.pv.at().line) - 1
		}

		for k, want := range []int{200, 100, 200} {
			if got := line("f8"); got != want {
				t.Fatalf("split %v: F8 #%d on line %d, want %d", split, k, got, want)
			}
		}

		if got := line("shift+f8"); got != 100 {
			t.Fatalf("split %v: shift+F8 back around: line %d", split, got)
		}

		m.pv.entry.Staged = true

		if strings.Contains(m.View().Content, "\x1b[4:3m") {
			t.Fatalf("split %v: a staged diff shows none", split)
		}
	}
}

// opsText is the file opsDiff's new side is.
func opsText(n int, ops map[int]string) string {
	var b strings.Builder

	for i := range n {
		switch ops[i] {
		case "a":
			fmt.Fprintf(&b, "line %d\nadded %d\n", i, i)
		case "d":
		case "m":
			fmt.Fprintf(&b, "changed %d\n", i)
		default:
			fmt.Fprintf(&b, "line %d\n", i)
		}
	}

	return b.String()
}

// TestProblemsList lists every file's problems, each with its message and
// position, opened by ctrl+shift+m or a click on the status bar's counts; ⏎
// goes there. Explorer and Source Control color a file with problems and
// count them.
func TestProblemsList(t *testing.T) {
	m, path := diagModel(t)

	_, cmd := m.Update(keyMsg("ctrl+shift+m"))
	if cmd == nil {
		t.Fatal("ctrl+shift+m builds the list")
	}

	m.Update(cmd())

	if m.probs == nil || m.probs.title != "Problems" || m.probs.count() != 3 {
		t.Fatalf("the list: %+v", m.probs)
	}

	out := ansi.Strip(checkWidths(t, m))
	if !strings.Contains(out, "✖ undefined: undefined") || !strings.Contains(out, "4:7") {
		t.Fatalf("a problem's row: its message and position:\n%s", out)
	}

	if c, n := m.probDecoFor(path); c != pal.errc || n != "2" {
		t.Fatalf("a file's decoration: %v %q", c, n)
	}

	if c, _ := m.probDecoFor(filepath.Dir(path)); c != pal.errc {
		t.Fatal("its folder takes its worst")
	}

	// The open list follows its problems, the selected one kept, and closes
	// with the last.
	m.probs.l.sel = 2 // the warning, under its file and the error: the worst first

	_, cmd = m.Update(diagMsg{path: m.diags[path][1:]})

	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatal("a change rebuilds the open list")
	}

	m.Update(batch[1]())

	if at, ok := m.probs.selected(); m.probs.count() != 2 || !ok || at.msg != "declared and not used: x" {
		t.Fatalf("the list now, the warning still selected: %d, %+v", m.probs.count(), at)
	}

	m.Update(diagMsg{path: nil})

	if m.probs != nil {
		t.Fatal("no problems left, the list closes")
	}
}

// TestDiagFiles: the files diagnostics are for, the editors' first, then the
// workspace's changed files without the deleted ones, at most maxDiagFiles.
func TestDiagFiles(t *testing.T) {
	m, path := diagModel(t)
	m.scm.status = map[string]git.Status{m.ws: {Changes: []git.Entry{
		{Path: "a.go", Letter: 'M'}, {Path: "b.go", Letter: 'M'}, {Path: "gone.go", Letter: 'D'}, {Path: "new.go", Letter: 'U'},
	}}, "/elsewhere": {Changes: []git.Entry{{Path: "c.go", Letter: 'M'}}}}

	var got []string
	for _, f := range m.diagFiles() {
		got = append(got, filepath.Base(f.path))
	}

	if strings.Join(got, " ") != "a.go b.go new.go" || m.diagFiles()[0].path != path {
		t.Fatalf("files %v", got)
	}

	for i := range 2 * maxDiagFiles {
		m.scm.status[m.ws] = git.Status{Changes: append(m.scm.status[m.ws].Changes, git.Entry{Path: fmt.Sprint(i, ".go"), Letter: 'M'})}
	}

	if n := len(m.diagFiles()); n != maxDiagFiles {
		t.Fatalf("capped at %d: %d", maxDiagFiles, n)
	}
}

// TestNoteEdits: a change to the shown editor's text, an edit or an undo,
// arms its sync; nothing new arms nothing.
func TestNoteEdits(t *testing.T) {
	m, _ := diagModel(t)
	m.noteEdits()

	if m.noteEdits() != nil {
		t.Fatal("the same text arms nothing")
	}

	press(m, "x")

	if m.noteEdits() != nil || m.diagSeen.edits == 0 {
		t.Fatal("an edit armed it in Update")
	}

	seq := m.diagSeq
	press(m, "ctrl+z")

	if m.diagSeq == seq {
		t.Fatal("an undo arms it too")
	}
}

// TestPoolBackground: a sync nobody asked for starts no server another
// workspace's or one that would not start; a request tries again.
func TestPoolBackground(t *testing.T) {
	var p lspPool

	_, err := p.get("/a", "/a/x.go", "go", []string{"no-such-language-server"})
	if err == nil {
		t.Fatal("a missing server fails")
	}

	if _, err2 := p.background("/a", "/a/x.go", "go", []string{"no-such-language-server"}); err2 == nil || err2.Error() != err.Error() {
		t.Fatalf("a background sync gets the failure, not another start: %v", err2)
	}

	if _, err := p.background("/b", "/b/x.go", "go", []string{"gopls"}); !errors.Is(err, errOtherRoot) || p.root != "/a" {
		t.Fatalf("a background sync of another root keeps this one's servers: %v", err)
	}
}

// TestGlobalTSC uses a TypeScript 7 tsc on PATH for a project with none of
// its own, where typescript-language-server has no tsserver.js to drive;
// an older TypeScript is the language server's.
func TestGlobalTSC(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "lib", "node_modules", "typescript")
	mustWrite(t, filepath.Join(pkg, "package.json"), `{"version":"7.0.2"}`)
	mustWrite(t, filepath.Join(pkg, "bin", "tsc"), "#!/bin/sh\n")

	if err := os.Chmod(filepath.Join(pkg, "bin", "tsc"), 0o755); err != nil {
		t.Fatal(err)
	}

	mustMkdir(t, filepath.Join(dir, "bin"))

	if err := os.Symlink(filepath.Join(pkg, "bin", "tsc"), filepath.Join(dir, "bin", "tsc")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", filepath.Join(dir, "bin"))

	if got := globalTSC(); got != filepath.Join(dir, "bin", "tsc") {
		t.Fatalf("TypeScript 7 on PATH: %q", got)
	}

	mustWrite(t, filepath.Join(pkg, "lib", "tsserver.js"), "")

	if got := globalTSC(); got != "" {
		t.Fatalf("an older TypeScript is typescript-language-server's: %q", got)
	}
}

// TestDiagFailureShows keeps a server that would not start in the status bar
// while its file shows, until it takes the file; and the problems list opens
// with no file open, on the first problem's.
func TestDiagFailureShows(t *testing.T) {
	m, path := diagModel(t)
	m.Update(diagFailMsg{"typescript-language-server", path, errors.New("Could not find a valid TypeScript")})

	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "⚠ no diagnostics") {
		t.Fatalf("the status bar says the file has no diagnostics:\n%s", out)
	}

	m.Update(diagOKMsg(path))

	if strings.Contains(ansi.Strip(m.View().Content), "no diagnostics") {
		t.Fatal("a server that took the file clears it")
	}

	n := testModel(t)
	n.st.Settings.Diags = true
	n.Update(diagMsg{path: {{Line: 1, Severity: 1, Message: "bad"}}})

	_, cmd := n.Update(keyMsg("ctrl+shift+m"))
	n.Update(cmd())

	if n.probs == nil || n.ptab != ptProblems || !n.termOpen() {
		t.Fatalf("the list opens in the panel's Problems: %v, tab %d", n.probs != nil, n.ptab)
	}
}
