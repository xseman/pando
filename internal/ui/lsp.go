package ui

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
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
// Servers start on the first jump, or with lsp_diagnostics when a file of
// theirs opens or changes, and die with the TUI. sink gets every server's
// diagnostics; failed is why a server would not start, which only a request
// tries again.
type lspPool struct {
	mu      sync.Mutex
	root    string
	servers map[string]*lsp.Client
	failed  map[string]error
	sink    lsp.Sink
	log     func(src, line string) // every server's stderr and log messages, by source; nil drops them
}

var errOtherRoot = errors.New("the workspace changed")

// get is the server for a request: a dead one starts again.
func (p *lspPool) get(root, path, lang string, argv []string) (*lsp.Client, error) {
	return p.server(root, path, lang, argv, true)
}

// background is the server for a sync nobody asked for, as diagnostics
// keep files in step: it never starts again one that died or would not
// start, which would respawn a crashing server every 2 s, and a workspace
// switched meanwhile keeps its servers.
func (p *lspPool) background(root, path, lang string, argv []string) (*lsp.Client, error) {
	return p.server(root, path, lang, argv, false)
}

func (p *lspPool) server(root, path, lang string, argv []string, asked bool) (*lsp.Client, error) {
	opts := serverOptions(path, argv)

	p.mu.Lock()
	if p.root != root {
		if !asked {
			p.mu.Unlock()
			return nil, errOtherRoot
		}

		p.closeLocked()
		p.root, p.failed = root, nil
	}

	key := fmt.Sprint(lang, " ", argv[0], " ", opts) // a TypeScript of its own gets a server of its own
	c, failed := p.servers[key], p.failed[key]

	switch {
	case c != nil && c.Err() != nil && asked: // it died: a request starts it again
		delete(p.servers, key)

		c = nil

	case c != nil && c.Err() != nil:
		p.mu.Unlock()
		return nil, c.Err()

	case c == nil && failed != nil && !asked:
		p.mu.Unlock()
		return nil, failed
	}
	p.mu.Unlock()

	if c != nil {
		return c, nil
	}

	var logf lsp.Logger

	if p.log != nil {
		src := "lsp:" + filepath.Base(argv[0])
		logf = func(line string) { p.log(src, line) }
	}

	c, err := lsp.Start(root, argv, opts, p.sink, logf) // outside the lock: starting takes seconds
	if err != nil {
		if _, look := exec.LookPath(argv[0]); look != nil && argv[0] == "typescript-language-server" {
			err = fmt.Errorf("%w: npm i -g typescript-language-server, or TypeScript 7 in the project", err)
		}

		if p.log != nil {
			p.log("lsp:"+filepath.Base(argv[0]), err.Error())
		}

		p.fail(root, key, err)

		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.failed, key)

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

// fail notes why the server for key would not start, while root is still
// the workspace.
func (p *lspPool) fail(root, key string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.root != root {
		return
	}

	if p.failed == nil {
		p.failed = map[string]error{}
	}

	p.failed[key] = err
}

// clients are the servers running now.
func (p *lspPool) clients() []*lsp.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Collect(maps.Values(p.servers))
}

// reset closes every server for workspace root, whose servers start next.
func (p *lspPool) reset(root string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closeLocked()
	p.root, p.failed = root, nil
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
	return serverFor(m.st.Settings.LSP, path)
}

// serverFor is lspCommand with [lsp] given, for a tea.Cmd: it stats its way
// to a project's TypeScript.
func serverFor(cfg map[string][]string, path string) (lang string, argv []string) {
	lang, argv = commandFor(cfg, defaultServers, path)
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
		return globalTSC()
	}

	if _, err := os.Stat(filepath.Join(ts, "lib", "tsserver.js")); err == nil {
		return ""
	}

	if bin := localBin(path, []string{"tsc"})[0]; bin != "tsc" {
		return bin // not a tsc on PATH, which may be an older one
	}

	return ""
}

// globalTSC is a TypeScript 7 tsc on PATH, for a project with none of its
// own, where typescript-language-server would find no tsserver.js to drive:
// VS Code falls back to the TypeScript it ships. "" for an older one, which
// typescript-language-server drives, or none.
func globalTSC() string {
	bin, err := exec.LookPath("tsc")
	if err != nil {
		return ""
	}

	target, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ""
	}

	pkg := filepath.Dir(filepath.Dir(target)) // node_modules/typescript/bin/tsc
	if _, err := os.Stat(filepath.Join(pkg, "package.json")); err != nil {
		return ""
	}

	if _, err := os.Stat(filepath.Join(pkg, "lib", "tsserver.js")); err == nil {
		return ""
	}

	return bin
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
	path, root, pool, text := p.path, m.ws, &m.lsps, p.text()

	m.flash("asking "+argv[0]+"…", false)

	return func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return lspMsg{err: err}
		}

		_ = c.Sync(path, text) // a server gone answers the request with why

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
	path, root, pool, text := p.path, m.ws, &m.lsps, p.text()
	m.flash("asking "+argv[0]+"…", false)

	return tea.Batch(saved, func() tea.Msg {
		c, err := pool.get(root, path, lang, argv)
		if err != nil {
			return actionsMsg{err: err}
		}

		_ = c.Sync(path, text) // a server gone answers the request with why

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
	path, root, pool, text := p.path, m.ws, &m.lsps, p.text()
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
				_ = c.Sync(path, text) // a server gone answers the rename with why
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
	x, y = m.mainX(), m.headH()+m.stripH()
	if cx, cy, ok := m.pv.cursor(m.pvW(), m.pvH()); ok {
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

	m.ex.rebuild(m) // a file the edit created shows at once, not at the next tick

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

	return m.openPeek(title, msg.locs)
}

// openLocation opens a server's location, translating its UTF-16 columns
// into the columns the preview shows.
func (m *Model) openLocation(l lsp.Location) tea.Cmd {
	raw := fileLine(l.Path, l.Line)

	col, end := displayCol(raw, lsp.RuneCol(raw, l.Col)), 0
	if l.EndLine == l.Line {
		end = displayCol(raw, lsp.RuneCol(raw, l.EndCol))
	}

	return m.openFileAt(l.Path, l.Line, col, max(end-col, 0), false)
}

// A file's columns and the preview's differ: the preview shows a tab as
// tabW spaces, a language server counts it as one character.

// tabW is the cells a tab takes in the editor, tab_size; syncTabs sets it.
var tabW = 4

// tabSize is setting n as a tab width: 4 when unset, at most 16.
func tabSize(n int) int {
	if n < 1 {
		return 4
	}

	return min(n, 16)
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabW)) }

// docCol is the file's rune column for a column shown in the preview.
func docCol(raw string, display int) int {
	at := 0
	for i, r := range []rune(raw) {
		if at >= display {
			return i
		}

		at += 1 + (tabW-1)*b2i(r == '\t')
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

		at += 1 + (tabW-1)*b2i(r == '\t')
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
