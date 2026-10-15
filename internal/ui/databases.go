package ui

import (
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/xseman/pando/internal/db"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// hasDB reports a database client on the PATH, sqlite3, psql or mariadb,
// which the Databases view needs; tests turn it off.
var hasDB = func() bool { return db.Has(db.SQLite) || db.Has(db.Postgres) || db.Has(db.MariaDB) }

// dbPatterns are the names a SQLite file goes by; its header decides.
var dbPatterns = []string{"*.db", "*.sqlite", "*.sqlite3"}

// dbTTL is how long the list of files stands before the view, on screen,
// lists them again: an agent may have made one since.
const dbTTL = 10 * time.Second

// envDB is the source the worktree's .env DATABASE_URL is listed as.
const envDB = ".env"

// dbView is the Databases view: the worktree's SQLite files, found by name
// and told by their header, and those Explorer opened besides; the server
// its .env's DATABASE_URL names; the servers config.toml's [databases] names.
// Each unfolds to its tables and views, read when it opens.
type dbView struct {
	l         list
	ws        string             // whose files these are
	files     []string           // absolute paths, found and opened
	extra     []string           // opened from Explorer: kept through a listing that does not find them
	env       string             // the worktree's .env DATABASE_URL
	at        time.Time          // when the files were listed
	listing   bool               // a listing is on its way
	open      map[string]bool    // unfolded sources, by id
	objs      map[string]*dbObjs // per source, nil until read
	passwords map[string]string  // asked for, by source id: kept in memory only
	gen       int                // another workspace or a refresh drops answers on the way
}

// dbSource is a database the view lists: id is a SQLite file's path or a
// server's name, which rows and tabs know it by.
type dbSource struct {
	id, label string
	db        db.DB
	err       error // a server URL that does not parse
}

// dbObjs is a source's tables and views, or why they could not be read.
type dbObjs struct {
	list []db.Object
	err  error
}

// dbRow is a row of the view: a source, one of its tables or views, or a
// line in place of them.
type dbRow struct {
	src  *dbSource
	obj  *db.Object
	note string
}

func (r dbRow) key() string {
	if r.obj != nil {
		return r.src.id + "\x00" + objName(*r.obj)
	}

	return r.src.id
}

// objName is a table as the view and its tab name it: schema.table outside
// the connection's own schema.
func objName(o db.Object) string {
	if o.Schema != "" {
		return o.Schema + "." + o.Name
	}

	return o.Name
}

type dbFilesMsg struct {
	ws    string
	gen   int
	files []string
	env   string
}

type dbObjsMsg struct {
	id, target string // the source, and the server it named then
	gen        int
	objs       []db.Object
	err        error
}

// sync starts over for another workspace: its own files.
func (v *dbView) sync(m *Model) {
	if v.ws == m.ws && v.open != nil {
		return
	}

	pw := v.passwords
	if pw == nil {
		pw = map[string]string{}
	}

	delete(pw, envDB) // another worktree's .env names another server
	*v = dbView{l: list{sel: -1}, ws: m.ws, open: map[string]bool{}, objs: map[string]*dbObjs{}, passwords: pw, gen: v.gen + 1}
}

// envURL is DATABASE_URL in the .env at the root of ws, "" for none.
func envURL(ws string) string {
	b, err := os.ReadFile(filepath.Join(ws, ".env"))
	if err != nil {
		return ""
	}

	last := ""

	for l := range strings.Lines(string(b)) { // the last one, as every dotenv loader and a shell take it
		v, ok := strings.CutPrefix(strings.TrimPrefix(strings.TrimSpace(l), "export "), "DATABASE_URL=")
		if !ok {
			continue
		}

		if v = strings.TrimSpace(v); v != "" && (v[0] == '"' || v[0] == '\'') { // quoted: up to its other quote
			if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
				last = v[1 : end+1]
				continue
			}
		}

		v, _, _ = strings.Cut(v, " #")
		last = strings.TrimSpace(v)
	}

	return last
}

// ensure lists the files, and reads the .env, when they are older than dbTTL.
func (v *dbView) ensure(m *Model) tea.Cmd {
	if !m.hasDB {
		return nil
	}

	v.sync(m)

	if v.listing || time.Since(v.at) <= dbTTL {
		return nil
	}

	v.listing = true
	ws, gen := v.ws, v.gen

	return func() tea.Msg {
		names, _ := git.FilesNamed(ws, dbPatterns...) // outside a repository none: Explorer still opens them

		var files []string

		for _, n := range names {
			if p := filepath.Join(ws, n); db.IsSQLite(p) {
				files = append(files, p)
			}
		}

		return dbFilesMsg{ws: ws, gen: gen, files: files, env: envURL(ws)}
	}
}

// shown is ensure while the view is on screen; the tick asks it too.
func (v *dbView) shown(m *Model) tea.Cmd {
	if !m.shown(viewDB) {
		return nil
	}

	return v.ensure(m)
}

// sources are the databases the view lists: the SQLite files, the .env's
// server, then config.toml's by name.
func (v *dbView) sources(m *Model) []dbSource {
	var out []dbSource

	for _, f := range v.files {
		out = append(out, dbSource{id: f, label: v.rel(f), db: db.File(f)})
	}

	server := func(id, raw string) {
		s := dbSource{id: id, label: id}
		s.db, s.err = db.Parse(raw)
		s.db.Password = v.passwords[id]

		if id != envDB { // the keyring may keep its password, under its name
			s.db.Keyring = id
		}

		out = append(out, s)
	}

	if v.env != "" {
		server(envDB, v.env)
	}

	for _, name := range slices.Sorted(maps.Keys(m.st.Databases)) {
		if name != envDB { // the worktree's: one written so by hand would take its rows and its password
			server(name, m.st.Databases[name])
		}
	}

	return out
}

// source is the database id names, false when the view lists none.
func (v *dbView) source(m *Model, id string) (dbSource, bool) {
	srcs := v.sources(m)
	if i := slices.IndexFunc(srcs, func(s dbSource) bool { return s.id == id }); i >= 0 {
		return srcs[i], true
	}

	return dbSource{}, false
}

// read asks for a source's tables and views.
func (v *dbView) read(m *Model, id string) tea.Cmd {
	s, ok := v.source(m, id)
	if !ok || s.err != nil {
		return nil
	}

	v.objs[id] = &dbObjs{}
	gen := v.gen

	return func() tea.Msg {
		objs, err := s.db.Objects()
		return dbObjsMsg{id: id, target: s.db.Target, gen: gen, objs: objs, err: err}
	}
}

func (v *dbView) onFiles(m *Model, msg dbFilesMsg) {
	if msg.ws != v.ws || msg.gen != v.gen {
		return
	}

	if msg.env != v.env { // another server: the password typed for the last one is not its
		delete(v.passwords, envDB)
		delete(v.objs, envDB)
	}

	key := v.selKey(m)
	v.listing, v.at, v.env = false, time.Now(), msg.env
	v.files = slices.Compact(slices.Sorted(slices.Values(append(msg.files, v.extra...))))
	v.follow(m, key)
}

func (v *dbView) onObjs(m *Model, msg dbObjsMsg) tea.Cmd {
	o := v.objs[msg.id]
	if s, _ := v.source(m, msg.id); o == nil || msg.gen != v.gen || s.db.Target != msg.target { // the source names another server since
		return nil
	}

	key := v.selKey(m) // the rows under the source change: the cursor stays on its row
	o.list, o.err = msg.objs, msg.err

	v.follow(m, key)

	if errors.Is(msg.err, db.ErrPassword) {
		return m.askPassword(msg.id, msg.target, func(m *Model, _ string) tea.Cmd { return v.read(m, msg.id) })
	}

	return nil
}

// askPassword asks for the password of target, the server source id names,
// then runs then with it. The source keeps it in memory for the run, never
// in a file, if it still names that server when the answer comes: a .env
// rewritten meanwhile names another, and loses it again when it does
// later. It goes to the client in its environment.
func (m *Model) askPassword(id, target string, then func(m *Model, pw string) tea.Cmd) tea.Cmd {
	if m.modal != nil { // the keys typed next are another box's: say so, and ask when it reads again
		return flash(id+" wants a password: r reads it again to ask", true)
	}

	title := "Password for " + id
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		title += " · " + strings.TrimPrefix(u.User.Username()+"@"+u.Host, "@")
	}

	md := newPrompt(title, "", func(m *Model, pw string) tea.Cmd {
		if s, ok := m.db.source(m, id); ok && s.db.Target == target {
			m.db.sync(m) // a table opened before the view showed
			m.db.passwords[id] = pw
		}

		return then(m, pw)
	})
	md.raw = true
	md.input.EchoMode = textinput.EchoPassword
	m.modal = md

	return nil
}

// refresh lists the files and reads the unfolded sources again.
func (v *dbView) refresh(m *Model) tea.Cmd {
	v.gen++
	v.at, v.listing = time.Time{}, false
	clear(v.objs)

	cmds := []tea.Cmd{v.ensure(m)}

	for id, open := range v.open {
		if open {
			cmds = append(cmds, v.read(m, id))
		}
	}

	return tea.Batch(cmds...)
}

func (v *dbView) collapseAll() {
	clear(v.open)
	v.l.sel = min(v.l.sel, 0)
}

// rel is a file as the view names it: from the workspace when it is in it.
func (v *dbView) rel(path string) string {
	if r, err := filepath.Rel(v.ws, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}

	return path
}

// rows lists the sources, each followed by its tables and views when it is
// unfolded; a filter keeps the sources and tables whose names have it, the
// tables of sources read already.
func (v *dbView) rows(m *Model) []dbRow {
	q := strings.ToLower(m.query(viewDB))

	var out []dbRow

	srcs := v.sources(m)
	for i := range srcs {
		s := &srcs[i]
		o := v.objs[s.id]

		var kids []dbRow

		switch {
		case !v.open[s.id] && q == "":
		case s.err != nil:
			kids = []dbRow{{src: s, note: s.err.Error()}}
		case o == nil || o.list == nil && o.err == nil:
			if q == "" {
				kids = []dbRow{{src: s, note: "Loading…"}}
			}

		case o.err != nil:
			kids = []dbRow{{src: s, note: errLine(o.err.Error())}}
		case len(o.list) == 0:
			kids = []dbRow{{src: s, note: "No tables"}}
		default:
			for k := range o.list {
				if q == "" || strings.Contains(strings.ToLower(objName(o.list[k])), q) {
					kids = append(kids, dbRow{src: s, obj: &o.list[k]})
				}
			}
		}

		if q != "" && len(kids) == 0 && !strings.Contains(strings.ToLower(s.label), q) {
			continue
		}

		out = append(append(out, dbRow{src: s}), kids...)
	}

	return out
}

func (v *dbView) selected(m *Model) *dbRow {
	if rows := v.rows(m); v.l.sel >= 0 && v.l.sel < len(rows) {
		return &rows[v.l.sel]
	}

	return nil
}

func (v *dbView) selKey(m *Model) string {
	if r := v.selected(m); r != nil {
		return r.key()
	}

	return ""
}

// follow keeps the selection on the row it was on as the rows change.
func (v *dbView) follow(m *Model, key string) {
	rows, h := v.rows(m), m.bodyH(viewDB)
	if i := slices.IndexFunc(rows, func(r dbRow) bool { return r.note == "" && r.key() == key }); i >= 0 {
		v.l.sel = i
		v.l.snap(h)
	}

	v.l.clamp(len(rows), h)
}

func (v *dbView) lines(m *Model, w, h int) []string {
	rows := v.rows(m)
	hover := m.hoverRow(viewDB)
	v.l.bar = m.barState("list:databases")

	if len(rows) == 0 {
		note := "No databases in this worktree"
		if v.listing && v.at.IsZero() {
			note = "Looking for databases…"
		}

		return []string{row(w, nil, []seg{sg(" "+note, dim)})}
	}

	return v.l.render(w, h, len(rows), func(i, rw int) string {
		r := rows[i]
		bg, name := m.rowColors(viewDB, i == v.l.sel, i-v.l.top == hover)

		switch {
		case r.note != "":
			return row(rw, bg, []seg{sg("     "+r.note, dim)})
		case r.obj != nil && r.obj.View:
			return row(rw, bg, []seg{sg("     "+icView.s()+" ", dim), sg(objName(*r.obj), name)}, sg("view ", dim))
		case r.obj != nil:
			return row(rw, bg, []seg{sg("     "+icTable.s()+" ", dim), sg(objName(*r.obj), name)})
		case r.src.db.Kind == db.SQLite:
			dir, base := filepath.Split(r.src.label)
			return row(rw, bg, []seg{sg(" "+chevron(v.open[r.src.id]), dim), iconSeg(base, false, false), sg(base, name), sg(" "+strings.TrimSuffix(dir, string(filepath.Separator)), dim)})
		}

		// A server takes a database file's icon, none where icons are text.
		return row(rw, bg, []seg{sg(" "+chevron(v.open[r.src.id]), dim), iconSeg(".db", false, false), sg(r.src.label, name)}, sg(r.src.db.Kind+" ", dim))
	})
}

// toggle folds or unfolds a source, reading it the first time.
func (v *dbView) toggle(m *Model, id string) tea.Cmd {
	v.open[id] = !v.open[id]
	if o := v.objs[id]; v.open[id] && (o == nil || errors.Is(o.err, db.ErrPassword)) { // a password asked for later asks now
		return v.read(m, id)
	}

	return nil
}

func (v *dbView) activate(m *Model, r *dbRow, transient bool) tea.Cmd {
	switch {
	case r == nil || r.note != "":
		return nil
	case r.obj != nil:
		return m.openTable(r.src.id, *r.obj, transient)
	}

	return v.toggle(m, r.src.id)
}

func (v *dbView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := v.rows(m)
	h, n := m.bodyH(viewDB), len(rows)
	r := v.selected(m)

	switch k.String() {
	case "up", "k":
		v.l.move(-1, n, h)
	case "down", "j":
		v.l.move(1, n, h)
	case "pgup":
		v.l.move(-h, n, h)
	case "pgdown":
		v.l.move(h, n, h)
	case "g", "home":
		v.l.move(-n, n, h)
	case "G", "end":
		v.l.move(n, n, h)
	case "enter", "space":
		return v.activate(m, r, true)
	case "l", "right":
		if r != nil && (r.obj != nil || !v.open[r.src.id]) {
			return v.activate(m, r, true)
		}

	case "h", "left":
		switch {
		case r == nil:
		case r.obj == nil && r.note == "" && v.open[r.src.id]:
			v.open[r.src.id] = false
		default: // to the source it is in
			v.l.sel = slices.IndexFunc(rows, func(o dbRow) bool { return o.src.id == r.src.id && o.obj == nil && o.note == "" })
			v.l.snap(h)
		}

	case "r", "ctrl+r":
		return v.refresh(m)
	case "C":
		v.collapseAll()
	case "c", "y":
		return v.copy(r, k.String() == "y")
	case "n":
		return v.newConn(m)
	case "t":
		if r != nil && r.note == "" {
			return console(*r.src)
		}

	case "e", "D", "delete":
		if r != nil && r.obj == nil && r.note == "" && m.st.Databases[r.src.id] != "" { // config.toml's, not a file's or .env's
			return ifElse(k.String() == "e", v.editConn, v.removeConn)(m, r.src.id)
		}

	case "m":
		return m.menuOf(v.items(m), 0, h/2)
	}

	return nil
}

// copy puts the selected table's name on the clipboard, or its source's:
// a file's path, a server's name.
func (v *dbView) copy(r *dbRow, path bool) tea.Cmd {
	switch {
	case r == nil || r.note != "":
		return nil
	case path || r.obj == nil:
		return setClipboard(r.src.id, "copied "+r.src.id)
	}

	name := objName(*r.obj)

	return setClipboard(name, "copied "+name)
}

func (v *dbView) items(m *Model) []item {
	r := v.selected(m)

	var items []item

	if r != nil && r.obj != nil {
		items = append(items, item{label: "Open Table", hint: "⏎", run: func(m *Model) tea.Cmd { return m.openTable(r.src.id, *r.obj, false) }})
	}

	if r != nil && r.note == "" {
		items = append(items,
			item{label: "Open Console", hint: "t", run: func(*Model) tea.Cmd { return console(*r.src) }},
			item{label: "Copy Name", hint: "c", run: func(*Model) tea.Cmd { return v.copy(r, false) }},
			item{label: "Copy Path", hint: "y", run: func(*Model) tea.Cmd { return v.copy(r, true) }},
			separator())
	}

	if r != nil && r.obj == nil && r.note == "" && m.st.Databases[r.src.id] != "" { // config.toml's
		id := r.src.id

		items = append(items,
			item{label: "Edit Connection…", hint: "e", run: func(m *Model) tea.Cmd { return v.editConn(m, id) }},
			item{label: "Remove Connection…", hint: "D", run: func(m *Model) tea.Cmd { return v.removeConn(m, id) }},
			separator())
	}

	return append(items,
		item{label: "New Connection…", hint: "n", run: v.newConn},
		item{label: "Refresh", hint: "r", run: v.refresh},
		item{label: "Collapse All", hint: "C", run: func(*Model) tea.Cmd { v.collapseAll(); return nil }})
}

func (v *dbView) mouse(m *Model, msg tea.MouseMsg, y int) tea.Cmd {
	mo, rows := msg.Mouse(), v.rows(m)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		v.l.wheel(wheelDelta(mo), len(rows), m.bodyH(viewDB))
	case tea.MouseClickMsg:
		i := v.l.at(y, len(rows))
		if i < 0 {
			if mo.Button == tea.MouseRight {
				v.l.sel = -1
				return m.menuOf(v.items(m), mo.X, mo.Y)
			}

			return nil
		}

		v.l.sel = i
		if mo.Button == tea.MouseRight {
			return m.menuOf(v.items(m), mo.X, mo.Y)
		}

		cmd := v.activate(m, &rows[i], true)
		if rows[i].obj != nil && m.clicks.double(mo.X, mo.Y) {
			m.pin()
		}

		return cmd
	}

	return nil
}

// revealDB shows a SQLite file Explorer opened in the Databases view,
// unfolded to its tables.
func (m *Model) revealDB(path string) tea.Cmd {
	v := &m.db
	v.sync(m)

	if !slices.Contains(v.files, path) {
		v.extra = append(v.extra, path)
		v.files = slices.Sorted(slices.Values(append(v.files, path)))
	}

	var read tea.Cmd
	if !v.open[path] {
		read = v.toggle(m, path)
	}

	show := m.showView(viewDB)
	v.follow(m, path)

	return tea.Batch(show, read)
}

// openTable opens table o of source id in an editor tab; a tab it has open
// already shows, with its edits.
func (m *Model) openTable(id string, o db.Object, transient bool) tea.Cmd {
	s, ok := m.db.source(m, id)
	if !ok {
		s = dbSource{id: id, db: db.File(id)} // a file opened before the view listed it
	}

	return m.setPreview(preview{kind: pvTable, path: id, name: objName(o), transient: transient, tbl: newTable(id, s.db, o.Schema, o.Name)})
}

// dbPasswordMsg is the password a saved connection settled on, kept in
// memory before its tables are read; err is the keyring's, when it failed.
type dbPasswordMsg struct {
	id, pw string
	err    error
}

// consoleMsg is a database's client, ready to start in the Terminal panel.
type consoleMsg struct{ argv []string }

// console opens src's client in a new shell of the Terminal panel, for the
// SQL the grid does not run. The password, looked up off the UI's
// goroutine, goes through a file in the runtime directory that the shell
// reads and removes first: ps shows a command line, and state.json keeps a
// session's command and environment.
func console(src dbSource) tea.Cmd {
	return func() tea.Msg {
		argv, env, pw := src.db.Console()

		switch {
		case src.err != nil || len(argv) == 0:
			return flashMsg{src.id + ": not a URL", true}
		case !db.Has(src.db.Kind):
			return flashMsg{argv[0] + " is not installed", true}
		case pw == "":
			return consoleMsg{argv}
		case runtime.GOOS == "windows": // ponytail: no sh to hand the password over; the client asks for it
			return consoleMsg{append(argv, ifElse(src.db.Kind == db.MariaDB, []string{"-p"}, nil)...)}
		}

		f, err := os.CreateTemp(proto.Dir(), "pw-")
		if err != nil {
			return flashMsg{err.Error(), true}
		}

		_, err = f.WriteString(pw)
		if err = errors.Join(err, f.Close()); err != nil {
			_ = os.Remove(f.Name())
			return flashMsg{err.Error(), true}
		}

		time.AfterFunc(time.Minute, func() { _ = os.Remove(f.Name()) }) // a shell that never started

		// Started again by a restarted daemon the file is gone: psql asks for
		// the password by itself, mariadb when told to.
		ask := ifElse(src.db.Kind == db.MariaDB, " -p", "")
		script := `if v=$(cat "$1" 2>/dev/null); then rm -f "$1"; export ` + env + `="$v"; shift; exec "$@"; fi; shift; exec "$@"` + ask

		return consoleMsg{append([]string{"sh", "-c", script, "sh", f.Name()}, argv...)}
	}
}
