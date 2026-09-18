package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/lsp"
)

// defaultServers are the language servers pando starts without a [lsp] entry.
var defaultServers = map[string][]string{
	"go":              {"gopls"},
	"typescript":      {"typescript-language-server", "--stdio"},
	"typescriptreact": {"typescript-language-server", "--stdio"},
	"javascript":      {"typescript-language-server", "--stdio"},
	"javascriptreact": {"typescript-language-server", "--stdio"},
}

// lspPool keeps one language server per language and binary for the open
// workspace: packages with their own node_modules get their own server.
// Servers start on the first jump and die with the TUI.
type lspPool struct {
	mu      sync.Mutex
	root    string
	servers map[string]*lsp.Client
}

func (p *lspPool) get(root, path, lang string, argv []string) (*lsp.Client, error) {
	opts := serverOptions(path, argv)

	p.mu.Lock()
	if p.root != root {
		p.closeLocked()
		p.root = root
	}

	key := fmt.Sprint(lang, " ", argv[0], " ", opts) // a TypeScript of its own gets a server of its own
	c := p.servers[key]
	p.mu.Unlock()

	if c != nil {
		return c, nil
	}

	c, err := lsp.Start(root, argv, opts) // outside the lock: starting takes seconds
	if err != nil {
		if _, look := exec.LookPath(argv[0]); look != nil && argv[0] == "typescript-language-server" {
			err = fmt.Errorf("%w: npm i -g typescript-language-server, or TypeScript 7 in the project", err)
		}

		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if old := p.servers[key]; old != nil { // another jump won the race
		go c.Close()
		return old, nil
	}

	if p.servers == nil {
		p.servers = map[string]*lsp.Client{}
	}

	p.servers[key] = c

	return c, nil
}

func (p *lspPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closeLocked()
}

func (p *lspPool) closeLocked() {
	for key, c := range p.servers {
		go c.Close()

		delete(p.servers, key)
	}
}

// lspCommand is the server for a file: [lsp] in config.toml keyed by the
// file's extension or by its language id, then pando's own defaults. Any
// language needs one line of config, nothing else.
func (m *Model) lspCommand(path string) (lang string, argv []string) {
	lang, argv = commandFor(m.st.Settings.LSP, defaultServers, path)
	if len(argv) > 0 && filepath.Base(argv[0]) == "typescript-language-server" {
		if tsc := nativeTSC(path); tsc != "" {
			argv = []string{tsc, "--lsp", "--stdio"}
		}
	}

	return lang, argv
}

// nativeTSC is the project's tsc when its TypeScript is 7 or newer: the Go
// port ships no tsserver.js for typescript-language-server to drive, but its
// tsc is a language server itself, so such a project needs nothing else
// installed. "" when the nearest TypeScript is an older one.
func nativeTSC(path string) string {
	ts := nearestTS(path)
	if ts == "" {
		return ""
	}

	if _, err := os.Stat(filepath.Join(ts, "lib", "tsserver.js")); err == nil {
		return ""
	}

	if bin := localBin(path, []string{"tsc"})[0]; bin != "tsc" {
		return bin // not a tsc on PATH, which may be an older one
	}

	return ""
}

// serverOptions are the initializationOptions a server needs for a file.
// typescript-language-server looks for TypeScript only in the workspace root's
// node_modules and then beside itself, so a package deeper down, as in a
// monorepo, is told where its own TypeScript is.
func serverOptions(path string, argv []string) map[string]any {
	if filepath.Base(argv[0]) != "typescript-language-server" {
		return nil
	}

	ts := nearestTS(path)
	if ts == "" {
		return nil
	}

	server := filepath.Join(ts, "lib", "tsserver.js")
	if _, err := os.Stat(server); err != nil {
		return nil
	}

	return map[string]any{"tsserver": map[string]any{"path": server}}
}

// nearestTS is the node_modules/typescript nearest above path, "" when none.
func nearestTS(path string) string {
	dir, _ := filepath.Abs(filepath.Dir(path))
	for {
		ts := filepath.Join(dir, "node_modules", "typescript")
		if _, err := os.Stat(filepath.Join(ts, "package.json")); err == nil {
			return ts
		}

		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}

		dir = up
	}
}

// commandFor picks a file's command out of a table keyed by extension or by
// LSP language id, falling back to pando's own defaults. [lsp] and [format]
// are configured the same way, so they look one up the same way.
func commandFor(table, fallback map[string][]string, path string) (lang string, argv []string) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if lang = lsp.LanguageOf(path); lang == "" {
		lang = ext
	}

	for _, t := range []map[string][]string{table, fallback} {
		for _, key := range []string{ext, lang} {
			if argv, ok := t[key]; ok && len(argv) > 0 {
				return lang, localBin(path, argv)
			}
		}
	}

	return lang, nil
}

// localBin runs the project's own copy of a command when one is installed: the
// nearest node_modules/.bin/<name> above the file, as VS Code prefers a
// workspace's typescript-language-server or prettier, so the server speaks the
// project's TypeScript version. PATH otherwise; a path in argv is kept as is.
// ponytail: a few stats per call, per keystroke while suggesting; cache per
// directory if that ever shows up.
func localBin(path string, argv []string) []string {
	if strings.ContainsRune(argv[0], filepath.Separator) {
		return argv
	}

	dir, _ := filepath.Abs(filepath.Dir(path))
	for {
		bin := filepath.Join(dir, "node_modules", ".bin", argv[0])
		if fi, err := os.Stat(bin); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return append([]string{bin}, argv[1:]...)
		}

		up := filepath.Dir(dir)
		if up == dir {
			return argv
		}

		dir = up
	}
}

// noServer says what to put in config.toml for a file pando has no server for.
func noServer(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	return fmt.Sprintf("no language server for .%s — add one to [lsp] in config.toml, e.g. %s = [\"%s-language-server\"]", ext, ext, ext)
}

type lspMsg struct {
	refs bool
	locs []lsp.Location
	err  error
}

// lspGo asks the language server where the symbol under the cursor is
// defined, or where it is used.
func (m *Model) lspGo(refs bool) tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || !p.ready || len(p.plain) == 0 {
		return flash("open a file first", true)
	}

	lang, argv := m.lspCommand(p.path)
	if len(argv) == 0 {
		return flash(noServer(p.path), true)
	}

	at := p.at()
	raw := fileLine(p.path, at.line)
	col := lsp.UTF16Col(raw, docCol(raw, at.col))
	path, root, pool := p.path, m.ws, &m.lsps
	text := p.unsaved()

	m.flash("asking "+argv[0]+"…", false)

	return func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return lspMsg{err: err}
		}

		c.Overlay(path, text)

		var locs []lsp.Location
		if refs {
			locs, err = c.References(path, at.line, col)
		} else {
			locs, err = c.Definition(path, at.line, col)
		}

		return lspMsg{refs: refs, locs: locs, err: err}
	}
}

type actionsMsg struct {
	acts []lsp.Action
	err  error
}

type appliedMsg struct {
	title string
	err   error
}

// lspActions asks for the code actions on the selection, or on the cursor
// line when nothing is selected: VS Code's ⌃. .
func (m *Model) lspActions() tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || !p.ready || len(p.plain) == 0 {
		return flash("open a file first", true)
	}

	lang, argv := m.lspCommand(p.path)
	if len(argv) == 0 {
		return flash(noServer(p.path), true)
	}

	saved, ok := m.saveForServer(p, "a code action")
	if !ok {
		return saved
	}

	a, b, ok := p.selection()
	if !ok {
		a = p.at()
		b = pos{a.line, p.lineLen(a.line)}
	}

	ra, rb := fileLine(p.path, a.line), fileLine(p.path, b.line)
	c0, c1 := lsp.UTF16Col(ra, docCol(ra, a.col)), lsp.UTF16Col(rb, docCol(rb, b.col))
	path, root, pool := p.path, m.ws, &m.lsps
	m.flash("asking "+argv[0]+"…", false)

	return tea.Batch(saved, func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return actionsMsg{err: err}
		}

		acts, err := c.Actions(path, a.line, c0, b.line, c1)

		return actionsMsg{acts: acts, err: err}
	})
}

// lspRename is F2: a prompt with the identifier under the cursor, then the
// server renames it everywhere and the files reload.
func (m *Model) lspRename() tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || !p.ready || len(p.plain) == 0 {
		return flash("open a file first", true)
	}

	lang, argv := m.lspCommand(p.path)
	if len(argv) == 0 {
		return flash(noServer(p.path), true)
	}

	saved, ok := m.saveForServer(p, "rename")
	if !ok {
		return saved
	}

	at := p.at()
	raw := fileLine(p.path, at.line)
	col := lsp.UTF16Col(raw, docCol(raw, at.col))
	path, root, pool := p.path, m.ws, &m.lsps
	a, b := wordRange(p.plain[at.line], at.col)
	old := string(p.plain[at.line][a:b])
	m.modal = newPrompt("", old, func(m *Model, name string) tea.Cmd {
		if name == "" || name == old {
			return nil
		}

		m.flash("renaming with "+argv[0]+"…", false)

		return func() tea.Msg {
			c, err := pool.get(root, path, lang, argv)
			if err == nil {
				err = c.Rename(path, at.line, col, name)
			}

			return appliedMsg{title: "renamed " + old + " to " + name, err: err}
		}
	})
	// The box sits over the symbol, where VS Code puts it, not in the middle
	// of the screen: renaming is an edit of that word, and it reads like one.
	m.modal.x, m.modal.y = m.lightbulb(at.col - a + 3) // the border and "› " before the name

	return saved
}

// wordRange is the identifier around col, as columns into the line.
func wordRange(line []rune, col int) (a, b int) {
	a, b = min(col, len(line)), min(col, len(line))
	for a > 0 && isWordRune(line[a-1]) {
		a--
	}

	for b < len(line) && isWordRune(line[b]) {
		b++
	}

	return a, b
}

// answered clears the "asking gopls…" flash once the server's answer is in.
func (m *Model) answered() {
	if strings.HasPrefix(m.msg, "asking ") {
		m.msg = ""
	}
}

func (m *Model) onActions(msg actionsMsg) tea.Cmd {
	m.answered()

	switch {
	case msg.err != nil:
		return flash(msg.err.Error(), true)
	case len(msg.acts) == 0:
		return flash("no code actions here", false)
	}
	// What fixes the code first, what only browses it last, as VS Code sorts them.
	slices.SortStableFunc(msg.acts, func(a, b lsp.Action) int { return groupOf(a.Kind) - groupOf(b.Kind) })

	lang, argv := m.lspCommand(m.pv.path)
	path, root, pool := m.pv.path, m.ws, &m.lsps

	items, group := make([]item, 0, len(msg.acts)+3), -1
	for _, a := range msg.acts {
		if g := groupOf(a.Kind); g != group {
			group = g
			items = append(items, heading(actionGroups[g]))
		}

		items = append(items, item{label: a.Title, run: func(m *Model) tea.Cmd {
			m.flash("applying "+a.Title+"…", false)

			return func() tea.Msg {
				c, err := pool.get(root, path, lang, argv)
				if err == nil {
					err = c.Run(a)
				}

				return appliedMsg{title: a.Title, err: err}
			}
		}})
	}

	x, y := m.lightbulb(0)

	return m.menuOf(items, x, y)
}

// actionGroups head the code action menu, as VS Code groups its own.
var actionGroups = []string{"Quick Fix", "Extract", "Inline", "Rewrite", "Refactor", "More Actions…"}

func groupOf(kind string) int {
	for i, prefix := range []string{"quickfix", "refactor.extract", "refactor.inline", "refactor.rewrite", "refactor"} {
		if strings.HasPrefix(kind, prefix) {
			return i
		}
	}

	return len(actionGroups) - 1
}

// lightbulb is where the code action menu opens: under the cursor's line, as
// VS Code opens its own.
// The row is counted in panel rows, the space a modal is placed in; a border
// shifts both by the same row.
// lightbulb is where a menu or a rename box opens: under the cursor's line,
// back is how many columns left of the cursor it starts.
func (m *Model) lightbulb(back int) (x, y int) {
	x, y = m.mainX(), 1+m.stripH()
	if cx, cy, ok := m.pv.cursor(m.mainW(), m.pvH()); ok {
		return max(x+cx-back, x), y + cy + 1
	}

	return x, y
}

// saveForServer writes the editor out before a server rewrites the file: the
// language server reads the file from disk, so unsaved text would be lost.
// ok is false when the save did not happen and the caller must stop.
func (m *Model) saveForServer(p *preview, what string) (tea.Cmd, bool) {
	if !p.dirty() {
		return nil, true
	}

	cmd := p.save(m, false)
	if p.dirty() { // changed on disk (the modal asks) or the write failed
		return tea.Batch(cmd, flash("save the file first (^s): "+what+" writes to disk", true)), false
	}

	return cmd, true
}

// onApplied reloads what the action rewrote: the file and the git status.
func (m *Model) onApplied(msg appliedMsg) tea.Cmd {
	if msg.err != nil {
		return flash(msg.err.Error(), true)
	}

	return tea.Batch(m.pv.load(m), m.refreshGit(), flash(msg.title, false))
}

func (m *Model) onLSP(msg lspMsg) tea.Cmd {
	m.answered()

	switch {
	case msg.err != nil:
		return flash(msg.err.Error(), true)
	case len(msg.locs) == 0:
		return flash("nothing found", false)
	case !msg.refs && len(msg.locs) == 1:
		return m.openLocation(msg.locs[0])
	}

	title := "References"
	if !msg.refs {
		title = "Definitions"
	}

	m.openPeek(title, msg.locs)

	return nil
}

// openLocation opens a server's location, translating its UTF-16 columns
// into the columns the preview shows.
func (m *Model) openLocation(l lsp.Location) tea.Cmd {
	raw := fileLine(l.Path, l.Line)

	col, end := displayCol(raw, lsp.RuneCol(raw, l.Col)), 0
	if l.EndLine == l.Line {
		end = displayCol(raw, lsp.RuneCol(raw, l.EndCol))
	}

	return m.openFileAt(l.Path, l.Line, col, max(end-col, 0))
}

// A file's columns and the preview's differ: the preview shows a tab as four
// spaces, a language server counts it as one character.

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// docCol is the file's rune column for a column shown in the preview.
func docCol(raw string, display int) int {
	at := 0
	for i, r := range []rune(raw) {
		if at >= display {
			return i
		}

		at += 1 + 3*b2i(r == '\t')
	}

	return len([]rune(raw))
}

// displayCol is docCol backwards.
func displayCol(raw string, doc int) int {
	at := 0

	for i, r := range []rune(raw) {
		if i >= doc {
			break
		}

		at += 1 + 3*b2i(r == '\t')
	}

	return at
}

// fileLines reads a file as its raw lines; one pando cannot read is empty.
func fileLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// fileLine is raw line n (0-based), without reading more than needed.
func fileLine(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for i := 0; sc.Scan(); i++ {
		if i == n {
			return sc.Text()
		}
	}

	return ""
}
