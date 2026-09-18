package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/lsp"
)

func TestPeek(t *testing.T) {
	m := testModelSized(t, 120, 30)
	code := filepath.Join(m.ws, "src", "deep", "x.go")
	doc := filepath.Join(m.ws, "README.md")
	m.pv, m.preview, m.focus = preview{kind: pvFile, path: code}, true, onMain
	m.openPeek("References", []lsp.Location{
		{Path: code, Line: 0, Col: 8, EndLine: 0, EndCol: 9},
		{Path: doc, Line: 0, Col: 2, EndLine: 0, EndCol: 4},
	})

	if m.peekH() < 6 || len(m.pk.rows) != 4 || m.pk.l.sel != 1 {
		t.Fatalf("peek: h=%d rows=%d sel=%d", m.peekH(), len(m.pk.rows), m.pk.l.sel)
	}

	out := strings.Split(checkWidths(t, m), "\n")
	top := 1 + m.stripH() + m.pvH()

	head := ansi.Cut(out[top], m.mainX(), m.w)
	if !strings.Contains(head, "References (2)") || !strings.Contains(head, "README.md") {
		t.Fatalf("peek header = %q", head)
	}

	row := ansi.Cut(out[top+1], m.mainX(), m.w)

	src, list, _ := strings.Cut(row, "│")
	if !strings.Contains(src, "# hi") || !strings.Contains(list, "README.md") {
		t.Fatalf("peek row: source %q, list %q", src, list)
	}

	// Keys belong to the widget while it is open.
	press(m, "down")

	if m.pk.l.sel != 2 {
		t.Fatalf("down: sel=%d", m.pk.l.sel)
	}

	press(m, "left")

	if !m.pk.closed[code] || len(m.pk.rows) != 3 {
		t.Fatalf("left folds: rows=%d", len(m.pk.rows))
	}

	press(m, "right", "down", "enter")

	if m.pk != nil || !strings.HasSuffix(m.pv.path, "x.go") || m.pv.cur != (pos{0, 9}) {
		t.Fatalf("enter opens and closes: pk=%v path=%s cur=%+v", m.pk, m.pv.path, m.pv.cur)
	}
	// The definition of a single location opens straight away.
	fire(m, m.onLSP(lspMsg{locs: []lsp.Location{{Path: doc, Line: 0, Col: 2, EndLine: 0, EndCol: 4}}}))

	if !strings.HasSuffix(m.pv.path, "README.md") || m.pk != nil {
		t.Fatalf("one definition opens it: %s", m.pv.path)
	}

	if cmd := m.onLSP(lspMsg{refs: true}); cmd == nil {
		t.Fatal("no results flashes")
	}

	m.pk = &peek{}
	click(m, m.w-2, 1+m.stripH()+m.pvH(), tea.MouseLeft)

	if m.pk != nil {
		t.Fatal("✕ closes the peek")
	}
}

func TestLspCommand(t *testing.T) {
	m := testModel(t)

	m.st.Settings.LSP = map[string][]string{"zig": {"zls"}, "typescript": {"my-ts-server"}}
	for path, want := range map[string]string{
		"a/b.go":  "gopls",        // a default
		"a/b.zig": "zls",          // a language pando knows nothing about, by extension
		"a/b.ts":  "my-ts-server", // a default replaced by language id
		"a/b.zzz": "",             // nothing configured
	} {
		lang, argv := m.lspCommand(path)

		got := ""
		if len(argv) > 0 {
			got = argv[0]
		}

		if got != want {
			t.Errorf("%s → %q (%s), want %q", path, got, lang, want)
		}
	}

	if lang, _ := m.lspCommand("a/b.zig"); lang != "zig" {
		t.Errorf("unknown extensions are their own language id: %q", lang)
	}
}

func TestLocalServer(t *testing.T) {
	m := testModel(t)
	root := t.TempDir()
	bin := func(dir string) string {
		t.Helper()

		p := filepath.Join(root, dir, "node_modules", ".bin", "typescript-language-server")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		return p
	}
	file := filepath.Join(root, "packages", "web", "src", "app.ts")
	// Nothing installed in the project: the name, found on PATH.
	if _, argv := m.lspCommand(file); argv[0] != "typescript-language-server" || argv[1] != "--stdio" {
		t.Fatalf("no node_modules: %q", argv)
	}

	top := bin("")
	if _, argv := m.lspCommand(file); argv[0] != top || argv[1] != "--stdio" {
		t.Fatalf("the repository's copy: %q", argv)
	}
	// The nearest node_modules wins, as in a monorepo package.
	near := bin(filepath.Join("packages", "web"))
	if _, argv := m.lspCommand(file); argv[0] != near {
		t.Fatalf("the package's copy: %q, want %s", argv, near)
	}
	// TypeScript 7 in the package: its own tsc is the server, even over an
	// installed typescript-language-server, which cannot drive it.
	web := filepath.Join(root, "packages", "web", "node_modules")
	for _, f := range []string{"typescript/package.json", ".bin/tsc"} {
		if err := os.MkdirAll(filepath.Join(web, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(web, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if _, argv := m.lspCommand(file); argv[0] != filepath.Join(web, ".bin", "tsc") || argv[1] != "--lsp" {
		t.Fatalf("typescript 7: %q", argv)
	}
	// An older TypeScript has tsserver.js: typescript-language-server again.
	mustWrite(t, filepath.Join(web, "typescript", "lib", "tsserver.js"), "")

	if _, argv := m.lspCommand(file); argv[0] != near {
		t.Fatalf("typescript 5: %q", argv)
	}
	// TypeScript 7 without its bin linked: no tsc from PATH, which may be old.
	mustRemove(t, filepath.Join(web, "typescript", "lib", "tsserver.js"))
	mustRemove(t, filepath.Join(web, ".bin", "tsc"))

	if _, argv := m.lspCommand(file); argv[0] != near {
		t.Fatalf("typescript 7 without tsc: %q", argv)
	}
	// typescript-language-server is told where the package's TypeScript is,
	// which it would only look for in the workspace root; none, nothing.
	if opts := serverOptions(file, []string{"typescript-language-server"}); opts != nil {
		t.Fatalf("no tsserver.js: %v", opts)
	}

	server := filepath.Join(web, "typescript", "lib", "tsserver.js")
	mustWrite(t, server, "")

	opts := serverOptions(file, []string{"/usr/bin/typescript-language-server", "--stdio"})
	if ts, _ := opts["tsserver"].(map[string]any); ts["path"] != server {
		t.Fatalf("options %v, want tsserver.path %s", opts, server)
	}

	if opts := serverOptions(file, []string{"gopls"}); opts != nil {
		t.Fatalf("gopls got %v", opts)
	}
	// A path in config is used as written.
	m.st.Settings.LSP = map[string][]string{"ts": {"/opt/tsls", "--stdio"}}
	if _, argv := m.lspCommand(file); argv[0] != "/opt/tsls" {
		t.Fatalf("configured path: %q", argv)
	}

	if defaultServers["typescript"][0] != "typescript-language-server" {
		t.Fatal("the defaults table must not be rewritten")
	}
}

func TestCodeActions(t *testing.T) {
	m := testModelSized(t, 120, 30)
	code := filepath.Join(m.ws, "src", "deep", "x.go")
	m.pv, m.preview, m.focus = preview{kind: pvFile, path: code, ready: true, plain: [][]rune{[]rune("package x")}}, true, onMain

	// The menu offers it wherever a server is configured, and [keys] can bind it.
	if !slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == "editor.quickFix" }) {
		t.Fatal("Editor: Quick Fix… is missing from the commands")
	}

	// No server for the language: the flash says what to put in config.toml.
	m.st.Settings.LSP = map[string][]string{"go": {}}
	defaultServers["go"] = nil

	t.Cleanup(func() { defaultServers["go"] = []string{"gopls"} })

	cmd := m.pv.key(m, keyMsg("."))

	msg, _ := cmd().(flashMsg)
	if !strings.Contains(msg.text, "no language server for .go") {
		t.Fatalf("flash = %q", msg.text)
	}

	// An empty answer says so rather than opening an empty menu.
	cmd = m.onActions(actionsMsg{})

	msg, _ = cmd().(flashMsg)
	if m.modal != nil || !strings.Contains(msg.text, "no code actions") {
		t.Fatalf("empty actions: modal=%v flash=%q", m.modal, msg.text)
	}
	// The menu opens under the cursor line, its actions under their group.
	m.onActions(actionsMsg{acts: []lsp.Action{
		{Title: "Browse documentation", Kind: "source.doc"},
		{Title: "Extract function", Kind: "refactor.extract.function"},
	}})

	if m.modal == nil {
		t.Fatal("no menu")
	}

	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, ansi.Strip(it.label))
	}

	want := []string{"Extract", "Extract function", "More Actions…", "Browse documentation"}
	if !slices.Equal(labels, want) {
		t.Fatalf("menu rows = %q, want %q", labels, want)
	}

	if m.modal.l.sel != 1 {
		t.Fatalf("the first action is selected, not the heading: sel=%d", m.modal.l.sel)
	}

	if x, y := m.lightbulb(0); m.modal.x != x || m.modal.y != y || y <= 1 {
		t.Fatalf("menu at %d,%d, want the row under the cursor %d,%d", m.modal.x, m.modal.y, x, y)
	}
}

func TestRenameSymbol(t *testing.T) {
	m := testModelSized(t, 120, 30)
	code := filepath.Join(m.ws, "src", "deep", "x.go")
	m.pv, m.preview, m.focus = preview{
		kind: pvFile, path: code, ready: true,
		plain: [][]rune{[]rune("package x"), []rune("func Total() int {")}, cur: pos{1, 7},
	}, true, onMain

	// F2 opens a prompt carrying the identifier under the cursor.
	press(m, "f2")

	if m.modal == nil || m.modal.input.Value() != "Total" {
		t.Fatalf("rename prompt = %v", m.modal != nil)
	}
	// It is inline: the box opens at the start of the symbol, not centred.
	if x, y := m.lightbulb(m.pv.cur.col - 5 + 3); m.modal.x != x || m.modal.y != y {
		t.Fatalf("rename box at %d,%d, want the symbol at %d,%d", m.modal.x, m.modal.y, x, y)
	}
	// The same name, or none, is a no-op.
	if cmd := m.modal.submit(m, "Total"); cmd != nil {
		t.Fatal("renaming to the same name does nothing")
	}

	if a, b := wordRange([]rune("a.b_c2 d"), 3); a != 2 || b != 6 {
		t.Fatalf("wordRange = %d,%d, want 2,6", a, b)
	}

	if a, b := wordRange([]rune("  "), 1); a != b {
		t.Fatalf("wordRange on blanks = %d,%d, want an empty range", a, b)
	}

	if !slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == "editor.renameSymbol" }) {
		t.Fatal("Editor: Rename Symbol… is missing from the commands")
	}
}

func TestFindInFile(t *testing.T) {
	m := testModelSized(t, 120, 30)
	code := filepath.Join(m.ws, "src", "deep", "x.go")
	lines := []string{"package x", "", "func Total() int { return 0 }", "// total again", "var x = Total"}
	styled := make([]string, len(lines))

	plain := make([][]rune, len(lines))
	for i, l := range lines {
		styled[i], plain[i] = l, []rune(l)
	}

	m.pv, m.preview, m.focus = preview{kind: pvFile, path: code, ready: true, lines: styled, plain: plain}, true, onMain

	// ⌃f opens the box; typing collects the matches, case-insensitively.
	press(m, "ctrl+f")

	if !m.pv.find.editing || !m.typing() {
		t.Fatal("ctrl+f focuses the find box")
	}

	press(m, "t", "o", "t", "a", "l")

	if len(m.pv.hits) != 3 || m.pv.hit != 0 {
		t.Fatalf("hits = %+v hit=%d", m.pv.hits, m.pv.hit)
	}
	// The match is selected, so the cursor and the status follow it.
	if a, b, ok := m.pv.selection(); !ok || a != (pos{2, 5}) || b != (pos{2, 10}) {
		t.Fatalf("selection = %+v..%+v (%v)", a, b, ok)
	}

	if got := m.pv.findStatus(); got != "1 of 3" {
		t.Fatalf("status = %q", got)
	}

	out := checkWidths(t, m)
	if !strings.Contains(out, "1 of 3") {
		t.Fatalf("the footer shows the count:\n%s", out)
	}
	// ⏎ walks forward and wraps, esc closes the box but keeps the matches.
	press(m, "enter")

	if a, _, _ := m.pv.selection(); a.line != 3 {
		t.Fatalf("enter goes to the next match: %+v", a)
	}

	press(m, "enter", "enter")

	if m.pv.hit != 0 || m.pv.findStatus() != "1 of 3" {
		t.Fatalf("enter wraps: hit=%d", m.pv.hit)
	}

	press(m, "esc")

	if m.pv.find.on || m.typing() {
		t.Fatal("esc closes the find box")
	}
	// F3 keeps walking the matches with the box closed, as VS Code does.
	press(m, "f3")

	if m.pv.hit != 1 {
		t.Fatalf("f3 goes to the next match, hit=%d", m.pv.hit)
	}

	press(m, "shift+f3")

	if m.pv.hit != 0 {
		t.Fatalf("shift+f3 goes back, hit=%d", m.pv.hit)
	}
	// A query with no matches says so rather than jumping anywhere.
	press(m, "ctrl+f")
	m.pv.find.input.SetValue("zzz")
	m.pv.refind(m)

	if len(m.pv.hits) != 0 || m.pv.findStatus() != "No results" {
		t.Fatalf("no results: %+v %q", m.pv.hits, m.pv.findStatus())
	}

	// The toggles: match case, whole word, regular expression (⌥c ⌥w ⌥r).
	m.pv.find.input.SetValue("total")
	press(m, "alt+c")

	if len(m.pv.hits) != 1 || m.pv.hits[0].at.line != 3 {
		t.Fatalf("match case: %+v", m.pv.hits)
	}

	press(m, "alt+c")
	m.pv.find.input.SetValue("tota")
	press(m, "alt+w")

	if len(m.pv.hits) != 0 {
		t.Fatalf("whole word: %+v", m.pv.hits)
	}

	press(m, "alt+w", "alt+r")
	m.pv.find.input.SetValue(`T.tal\b`)
	m.pv.refind(m)

	if len(m.pv.hits) != 3 || m.pv.hits[0].end-m.pv.hits[0].at.col != 5 {
		t.Fatalf("regex: %+v", m.pv.hits)
	}
	// The widget floats at the top right; matches carry find's colors, the
	// current one its own.
	view := m.View().Content
	x0, bw, ok := m.pv.findRect(m)

	top := strings.Split(ansi.Strip(view), "\n")[1+m.stripH()+1]
	if !ok || !strings.Contains(ansi.Cut(top, m.mainX()+x0, m.mainX()+x0+bw), "Aa") || !strings.Contains(top, " of 3") {
		t.Fatalf("widget row %q", top)
	}

	if !strings.Contains(view, bgParams(pal.findHitBg)) || !strings.Contains(view, bgParams(pal.findMatchBg)) {
		t.Fatal("matches are not painted")
	}

	checkWidths(t, m)
	// Its buttons click: regex off again, then ✕ closes.
	_, acts := m.pv.findActs(bw)
	click(m, m.mainX()+x0+acts[2].x+1, 1+m.stripH()+1, tea.MouseLeft)

	if m.pv.findRegex {
		t.Fatal("clicking .* did not turn the regex off")
	}

	click(m, m.mainX()+x0+acts[5].x+1, 1+m.stripH()+1, tea.MouseLeft)

	if m.pv.find.on {
		t.Fatal("clicking ✕ did not close the widget")
	}
}

// TestFindFollowsEdits keeps the matches in step with the text: an edit or a
// reload recollects them instead of dropping them.
func TestFindFollowsEdits(t *testing.T) {
	m, _ := editorModel(t, "a.go", "total\nx\ntotal\n")
	press(m, "ctrl+f", "t", "o", "t", "a", "l")

	if len(m.pv.hits) != 2 || m.pv.findStatus() != "1 of 2" {
		t.Fatalf("hits %+v %q", m.pv.hits, m.pv.findStatus())
	}

	m.pv.editRaw(m, pos{1, 0}, pos{1, 1}, "total")

	if len(m.pv.hits) != 3 || m.pv.findStatus() != "1 of 3" {
		t.Fatalf("after an edit: %+v %q", m.pv.hits, m.pv.findStatus())
	}

	m.pv.refreshAll(m)

	if len(m.pv.hits) != 3 {
		t.Fatalf("after a full refresh: %+v", m.pv.hits)
	}
}

// TestReplaceInFile is the find widget's replace box: ⌃h opens it, ⏎ in it
// replaces the current match and moves on, ⌃⌥⏎ replaces every match as one
// undo step, AB keeps the case and $1 expands in regex mode.
func TestReplaceInFile(t *testing.T) {
	m, _ := editorModel(t, "a.go", "Total\nx\ntotal TOTAL\n")
	press(m, "ctrl+h")

	if !m.pv.find.editing || !m.pv.replOn || m.pv.repl.Focused() {
		t.Fatalf("ctrl+h opens the replace box with the caret in the empty query: editing=%v on=%v repl=%v",
			m.pv.find.editing, m.pv.replOn, m.pv.repl.Focused())
	}

	press(m, "t", "o", "t", "a", "l", "tab")

	if !m.pv.repl.Focused() || len(m.pv.hits) != 3 {
		t.Fatalf("tab moves to the replacement: focused=%v hits=%+v", m.pv.repl.Focused(), m.pv.hits)
	}

	press(m, "s", "u", "m")

	if len(m.pv.hits) != 3 || m.pv.repl.Value() != "sum" {
		t.Fatalf("typing a replacement leaves the matches alone: %+v %q", m.pv.hits, m.pv.repl.Value())
	}
	// The widget shows both rows; the second holds the replacement and AB.
	view := ansi.Strip(m.View().Content)
	x0, bw, ok := m.pv.findRect(m)
	rows := strings.Split(view, "\n")

	repl := ansi.Cut(rows[1+m.stripH()+2], m.mainX()+x0, m.mainX()+x0+bw)
	if !ok || m.pv.findH() != 4 || !strings.Contains(repl, "sum") || !strings.Contains(repl, "AB") {
		t.Fatalf("replace row %q", repl)
	}

	checkWidths(t, m)

	// ⏎ replaces the current match and selects the next one.
	press(m, "enter")

	if got := m.pv.text(); got != "sum\nx\ntotal TOTAL\n" {
		t.Fatalf("replace one: %q", got)
	}

	if len(m.pv.hits) != 2 || m.pv.hit != 0 || m.pv.hits[0].at.line != 2 {
		t.Fatalf("after the replacement the next match is current: hit=%d %+v", m.pv.hit, m.pv.hits)
	}
	// Preserve case copies the match's case; replace all is one undo step.
	press(m, "alt+p", "ctrl+alt+enter")

	if got := m.pv.text(); got != "sum\nx\nsum SUM\n" {
		t.Fatalf("replace all with preserve case: %q", got)
	}

	if len(m.pv.hits) != 0 || m.pv.findStatus() != "No results" {
		t.Fatalf("nothing left to find: %+v %q", m.pv.hits, m.pv.findStatus())
	}

	press(m, "esc", "ctrl+z")

	if got := m.pv.text(); got != "sum\nx\ntotal TOTAL\n" {
		t.Fatalf("undo takes replace all back in one step: %q", got)
	}
	// Regex mode expands groups; a tab in the line stays a tab.
	m.pv.editRaw(m, pos{1, 0}, pos{1, 1}, "\tfoo(1)")
	press(m, "ctrl+h")

	if !m.pv.repl.Focused() {
		t.Fatal("ctrl+h with a query goes to the replacement")
	}

	m.pv.find.input.SetValue(`foo\((\d)\)`)
	m.pv.findRegex, m.pv.findPreserve = true, false
	m.pv.refind(m)
	m.pv.repl.SetValue("bar[$1]")
	press(m, "enter")

	if got := m.pv.text(); got != "sum\n\tbar[1]\ntotal TOTAL\n" {
		t.Fatalf("regex replace: %q", got)
	}
	// The chevron closes the box; a click in the replace row reopens focus there.
	click(m, m.mainX()+x0+1, 1+m.stripH()+1, tea.MouseLeft)

	if m.pv.replOn || m.pv.repl.Focused() || !m.pv.find.editing {
		t.Fatalf("the chevron closes the replace box: on=%v", m.pv.replOn)
	}

	click(m, m.mainX()+x0+1, 1+m.stripH()+1, tea.MouseLeft)

	if !m.pv.replOn || !m.pv.repl.Focused() {
		t.Fatal("the chevron opens it again")
	}

	if !hasCommand(m, "editor.replace") {
		t.Fatal("Editor: Replace is missing from the commands")
	}
}

// TestReplaceReadOnly keeps replace away from what cannot be edited.
func TestReplaceReadOnly(t *testing.T) {
	m := testModelSized(t, 120, 30)
	lines := []string{"total", "total"}

	styled, plain := make([]string, 2), make([][]rune, 2)
	for i, l := range lines {
		styled[i], plain[i] = l, []rune(l)
	}

	m.pv, m.preview, m.focus = preview{kind: pvFile, path: filepath.Join(m.ws, "x.go"), ready: true, lines: styled, plain: plain}, true, onMain
	press(m, "ctrl+h")

	if m.pv.find.on || m.pv.replOn {
		t.Fatal("ctrl+h does nothing without a buffer")
	}

	press(m, "ctrl+f", "ctrl+h")

	if m.pv.replOn || m.pv.findH() != 3 {
		t.Fatal("the widget of a read-only file has no replace box")
	}

	if hasCommand(m, "editor.replace") {
		t.Fatal("Editor: Replace offered on a read-only file")
	}
}
