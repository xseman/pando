package ui

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

type exNode struct {
	path, name string
	depth      int
	dir        bool
}

type explorer struct {
	root     string
	expanded map[string]bool
	nodes    []exNode
	l        list
	deco     map[string]byte
}

// ponytail: filter matches stop here; a narrower query shows the rest.
const maxFilterMatches = 2000

func (e *explorer) setRoot(m *Model, root string) {
	*e = explorer{root: root, expanded: map[string]bool{}, l: list{sel: -1}}
	e.rebuild(m)
}

// rebuild re-reads every expanded directory, keeping the selection by path.
// With a Ctrl+F query it shows the matching files of the whole workspace
// under their directories instead.
func (e *explorer) rebuild(m *Model) {
	selPath := ""
	if e.l.sel >= 0 && e.l.sel < len(e.nodes) {
		selPath = e.nodes[e.l.sel].path
	}

	e.nodes = e.nodes[:0]
	if q := m.query(viewFiles); q != "" {
		e.filtered(m, q)
	} else {
		e.walk(m, e.root, 0)
	}

	if selPath != "" {
		e.l.sel = -1
		for i, n := range e.nodes {
			if n.path == selPath {
				e.l.sel = i
			}
		}
	}

	e.l.clamp(len(e.nodes), m.bodyH(viewFiles))
}

func (e *explorer) walk(m *Model, dir string, depth int) {
	ents, _ := os.ReadDir(dir)

	nodes := make([]exNode, 0, len(ents))
	for _, ent := range ents {
		name := ent.Name()
		if name == ".git" || (!m.st.Settings.Hidden && strings.HasPrefix(name, ".")) {
			continue
		}

		p := filepath.Join(dir, name)

		isDir := ent.IsDir()
		if ent.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(p); err == nil {
				isDir = st.IsDir()
			}
		}

		nodes = append(nodes, exNode{path: p, name: name, depth: depth, dir: isDir})
	}

	slices.SortFunc(nodes, func(a, b exNode) int {
		return cmp.Or(b2i(b.dir)-b2i(a.dir), strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)))
	})

	for _, n := range nodes {
		e.nodes = append(e.nodes, n)
		if n.dir && e.expanded[n.path] {
			e.walk(m, n.path, depth+1)
		}
	}
}

// filtered lists index files whose name matches q (the whole path when q
// contains a slash), with their directories.
func (e *explorer) filtered(m *Model, q string) {
	if m.index.ws != e.root {
		return // the index is still loading
	}

	byPath := strings.Contains(q, "/")

	var paths []string

	for _, f := range m.index.files {
		if !m.st.Settings.Hidden && (strings.HasPrefix(f, ".") || strings.Contains(f, "/.")) {
			continue
		}

		target := f
		if !byPath {
			target = filepath.Base(f)
		}

		if fuzzy(q, target) >= 0 {
			if paths = append(paths, f); len(paths) == maxFilterMatches {
				break
			}
		}
	}

	var walk func(t *ptree, dir string, depth int)

	walk = func(t *ptree, dir string, depth int) {
		for _, name := range t.dirNames() {
			p := filepath.Join(dir, name)
			e.nodes = append(e.nodes, exNode{path: p, name: name, depth: depth, dir: true})
			walk(t.dirs[name], p, depth+1)
		}

		for _, f := range t.sortedFiles() {
			e.nodes = append(e.nodes, exNode{path: filepath.Join(e.root, f), name: filepath.Base(f), depth: depth})
		}
	}
	walk(buildTree(paths), e.root, 0)
}

func (e *explorer) selected() *exNode {
	if e.l.sel >= 0 && e.l.sel < len(e.nodes) {
		return &e.nodes[e.l.sel]
	}

	return nil
}

// reveal expands the ancestors of path and selects it.
func (e *explorer) reveal(m *Model, path string) {
	if !strings.HasPrefix(path, e.root+"/") {
		return
	}

	for d := filepath.Dir(path); len(d) > len(e.root); d = filepath.Dir(d) {
		e.expanded[d] = true
	}

	e.rebuild(m)

	for i, n := range e.nodes {
		if n.path == path {
			e.l.sel = i
			e.l.snap(m.bodyH(viewFiles))
		}
	}
}

func (e *explorer) lines(m *Model, w, h int) []string {
	filtering := m.query(viewFiles) != ""
	hover := m.hoverRow(viewFiles)

	return e.l.render(w, h, len(e.nodes), func(i, rw int) string {
		n := e.nodes[i]
		bg, name := m.rowColors(viewFiles, i == e.l.sel, i-e.l.top == hover)
		open := filtering || e.expanded[n.path]

		chev := "  "
		if n.dir {
			chev = chevron(open)
		}

		letter := e.deco[n.path]

		var right []seg

		switch {
		case letter == 'I':
			name = name.Foreground(pal.ignored)
		case letter == '*' || (n.dir && letter != 0):
			name = name.Foreground(pal.modified)
			right = []seg{sg("● ", fg(pal.modified))}

		case letter != 0:
			name = name.Foreground(statusColor(letter))
			right = []seg{sg(string(letter)+" ", fg(statusColor(letter)))}
		}

		return row(rw, bg, []seg{
			sg(" "+strings.Repeat("  ", n.depth)+chev, dim),
			iconSeg(n.name, n.dir, open),
			sg(n.name, name),
		}, right...)
	})
}

// collapseAll folds every directory and scrolls to the top.
func (e *explorer) collapseAll(m *Model) {
	e.expanded, e.l.top = map[string]bool{}, 0
	e.rebuild(m)
}

// toggle folds a directory; a filtered tree always shows every match.
func (e *explorer) toggle(m *Model, n *exNode) {
	if m.query(viewFiles) != "" {
		return
	}

	e.expanded[n.path] = !e.expanded[n.path]
	e.rebuild(m)
}

// target is the directory new entries are created in.
func (e *explorer) target() string {
	n := e.selected()
	switch {
	case n == nil:
		return e.root
	case n.dir:
		return n.path
	}

	return filepath.Dir(n.path)
}

func (e *explorer) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	h, cnt := m.bodyH(viewFiles), len(e.nodes)
	n := e.selected()

	switch k.String() {
	case "up", "k":
		e.l.move(-1, cnt, h)
	case "down", "j":
		e.l.move(1, cnt, h)
	case "pgup":
		e.l.move(-h, cnt, h)
	case "pgdown":
		e.l.move(h, cnt, h)
	case "g", "home":
		e.l.move(-cnt, cnt, h)
	case "G", "end":
		e.l.move(cnt, cnt, h)
	case "enter", "space":
		if n != nil && n.dir {
			e.toggle(m, n)
		} else if n != nil {
			return m.openFile(n.path)
		}

	case "l", "right":
		if n != nil && n.dir && !e.expanded[n.path] {
			e.toggle(m, n)
		} else if n != nil && !n.dir {
			return m.openFile(n.path)
		}

	case "h", "left":
		if n == nil {
			return nil
		}

		if n.dir && e.expanded[n.path] && m.query(viewFiles) == "" {
			e.toggle(m, n)
			return nil
		}

		for i := e.l.sel - 1; i >= 0; i-- {
			if e.nodes[i].depth < n.depth {
				e.l.sel = i
				e.l.snap(h)

				break
			}
		}

	case "r":
		e.rebuild(m)
		return m.refreshGit()

	case "C":
		e.collapseAll(m)
	case ".":
		return m.setSettings(map[string]any{"hidden": !m.st.Settings.Hidden})
	case "m":
		return e.menu(m, 0, h/2)
	default:
		if a := e.action(k.String()); a != nil {
			return a(m)
		}
	}

	return nil
}

// action maps the shared key/menu actions on the selected node.
func (e *explorer) action(key string) func(m *Model) tea.Cmd {
	n := e.selected()

	switch key {
	case "n", "N":
		dir, folder := e.target(), key == "N"

		title := "New file in "
		if folder {
			title = "New folder in "
		}

		return func(m *Model) tea.Cmd {
			rel, _ := filepath.Rel(m.ws, dir)
			m.modal = newPrompt(title+rel, "", func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}

				p := filepath.Join(dir, v)

				var err error
				if folder {
					err = os.MkdirAll(p, 0o755)
				} else if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
					var f *os.File
					if f, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
						_ = f.Close() // nothing was written to it
					}
				}

				if err != nil {
					return flash(err.Error(), true)
				}

				e.expanded[dir] = true
				e.reveal(m, p)

				if !folder {
					return m.openFile(p)
				}

				return nil
			})

			return nil
		}
	}

	if n == nil {
		return nil
	}

	path := n.path

	switch key {
	case "R", "f2":
		return func(m *Model) tea.Cmd {
			m.modal = newPrompt("Rename", filepath.Base(path), func(m *Model, v string) tea.Cmd {
				if v == "" || v == filepath.Base(path) {
					return nil
				}

				to := filepath.Join(filepath.Dir(path), v)
				if _, err := os.Lstat(to); err == nil {
					return flash(v+" already exists", true)
				}

				if err := os.Rename(path, to); err != nil {
					return flash(err.Error(), true)
				}

				e.reveal(m, to)

				return m.refreshGit()
			})

			return nil
		}

	case "D", "delete":
		return func(m *Model) tea.Cmd {
			m.modal = newMenu("Delete "+filepath.Base(path)+"?", -1, 0,
				item{label: "Delete permanently", run: func(m *Model) tea.Cmd {
					if err := os.RemoveAll(path); err != nil {
						return flash(err.Error(), true)
					}

					e.rebuild(m)

					return tea.Batch(flash("deleted "+filepath.Base(path), false), m.refreshGit())
				}},
				cancelItem())

			return nil
		}

	case "y":
		return func(*Model) tea.Cmd { return tea.Batch(tea.SetClipboard(path), flash("copied "+path, false)) }
	case "Y":
		return func(m *Model) tea.Cmd {
			rel, _ := filepath.Rel(m.ws, path)
			return tea.Batch(tea.SetClipboard(rel), flash("copied "+rel, false))
		}

	case "o":
		return func(*Model) tea.Cmd {
			if err := openExternal(path); err != nil {
				return flash("open: "+err.Error(), true)
			}

			return nil
		}

	case "s":
		return func(*Model) tea.Cmd {
			return func() tea.Msg {
				root, err := git.Root(filepath.Dir(path))
				if err == nil {
					_, err = git.Run(root, "add", "-A", "--", path)
				}

				if err != nil {
					return flashMsg{"stage: " + err.Error(), true}
				}

				return scmMsg{root: root, text: "staged " + filepath.Base(path)}
			}
		}

	case "e":
		if n.dir {
			return nil
		}

		return func(m *Model) tea.Cmd { return m.edit(path) }
	}

	return nil
}

// items are the Explorer commands for the selection.
func (e *explorer) items(_ *Model) []item {
	n := e.selected()
	mk := func(label, hint, key string) item {
		a := e.action(key)

		return item{label: label, hint: hint, run: func(m *Model) tea.Cmd {
			if a == nil {
				return nil
			}

			return a(m)
		}}
	}
	items := []item{
		mk("New File…", "n", "n"), mk("New Folder…", "N", "N"),
		{label: "Collapse All", hint: "C", run: func(m *Model) tea.Cmd { m.ex.collapseAll(m); return nil }},
	}

	if n != nil {
		if !n.dir {
			items = append(items, mk("Edit in $EDITOR", "e", "e"))
		}

		items = append(items, mk("Rename…", "R", "R"), mk("Delete…", "D", "D"),
			mk("Copy Path", "y", "y"), mk("Copy Relative Path", "Y", "Y"),
			mk("Stage Changes", "s", "s"), mk("Open with Default App", "o", "o"))
	}

	return items
}

func (e *explorer) menu(m *Model, x, y int) tea.Cmd { return m.menuOf(e.items(m), x, y) }

func (e *explorer) mouse(m *Model, msg tea.MouseMsg, y int) tea.Cmd {
	mo := msg.Mouse()
	h := m.bodyH(viewFiles)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		e.l.wheel(wheelDelta(mo), len(e.nodes), h)
	case tea.MouseClickMsg:
		i := e.l.at(y, len(e.nodes))
		if i < 0 {
			return nil
		}

		e.l.sel = i

		n := &e.nodes[i]
		if mo.Button == tea.MouseRight {
			return e.menu(m, mo.X, mo.Y)
		}

		if n.dir {
			e.toggle(m, n)
			return nil
		}

		return m.openFile(n.path)
	}

	return nil
}

// edit opens path in $EDITOR as a session in the active workspace.
func (m *Model) edit(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	return m.newSession(m.ws, "editor", append(strings.Fields(editor), path))
}

// sessionColors are the colours a new session's emulator answers OSC 10/11
// with. A terminal that never answered pando's own query (tmux and ttyd
// swallow it) would otherwise leave the emulator on its white default, and
// agents like claude would pick a light theme inside a dark pando.
func (m *Model) sessionColors() (fg, bg string) {
	fg, bg = m.fg, m.bg
	if fg == "" {
		fg = map[bool]string{true: "#d4d4d4", false: "#1f2328"}[m.dark]
	}

	if bg == "" {
		bg = map[bool]string{true: "#1e1e1e", false: "#ffffff"}[m.dark]
	}

	return fg, bg
}

// newSession starts a session sized to the main area with the host's colors.
func (m *Model) newSession(ws, agent string, cmd []string) tea.Cmd {
	fg, bg := m.sessionColors()
	p := map[string]any{"workspace": ws, "agent": agent, "cmd": cmd, "cols": m.mainW(), "rows": m.sessH(), "fg": fg, "bg": bg}

	return func() tea.Msg {
		var s proto.Session
		if err := proto.Call("session.new", p, &s); err != nil {
			return flashMsg{agent + ": " + err.Error(), true}
		}

		return newSessionMsg(s)
	}
}

// quickOpen is ctrl+p; a "path:12:5" query opens that line and column.
func (m *Model) quickOpen(msg indexMsg) {
	md := &modal{title: "Go to File", filter: true, lineQuery: true, treeable: true, tree: m.st.Settings.QuickTree, x: -1}

	md.items = make([]item, len(msg.files))
	for i, f := range msg.files {
		p := filepath.Join(msg.ws, f)
		md.items[i] = item{label: f, run: func(m *Model) tea.Cmd {
			m.ex.reveal(m, p)

			if line, col := parseLineCol(lineSuffix(md.input.Value())); line > 0 {
				return m.openFileAt(p, line-1, max(col-1, 0), 0)
			}

			return m.openFile(p)
		}}
	}

	md.input = newPicker("", nil).input
	asked := false
	md.change = func(m *Model, v string) tea.Cmd { // "@" goes to a symbol in the open file, ":" to a line, as in VS Code
		if strings.HasPrefix(v, ":") && m.showsPreview() && m.pv.ready {
			m.modal = nil
			return m.gotoLineQuery(v)
		}

		if !strings.HasPrefix(v, "@") {
			asked = false
			return nil
		}

		if asked {
			return nil
		}

		asked = true

		return m.gotoSymbol(v)
	}
	md.refilter()
	m.modal = md
}

// lineSuffix is the ":12:5" part of a quick open query.
func lineSuffix(q string) string {
	_, after, ok := strings.Cut(q, ":")
	if !ok {
		return ""
	}

	return after
}

// parseLineCol reads "12" or "12:5"; a line of 0 means there is none.
func parseLineCol(v string) (line, col int) {
	l, c, _ := strings.Cut(strings.TrimSpace(v), ":")
	line, _ = strconv.Atoi(l)
	col, _ = strconv.Atoi(c)

	return line, col
}
