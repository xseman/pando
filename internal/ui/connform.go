package ui

import (
	"cmp"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/db"
)

// The system keyring, which tests take away.
var (
	hasKeyring    = db.HasKeyring
	keyringLoad   = db.LoadPassword
	keyringSave   = db.SavePassword
	keyringForget = db.ForgetPassword
)

// connKinds are what a connection can be, in the order the form cycles
// them: the URL scheme each is kept under and the port it defaults to.
var connKinds = []struct{ label, scheme, port string }{
	{"PostgreSQL", "postgres", "5432"},
	{"MariaDB", "mariadb", "3306"},
	{"MySQL", "mysql", "3306"},
	{"SQLite", "sqlite", ""},
}

// The connection form's text boxes, by the part of the URL each holds.
const (
	cfName = iota
	cfHost
	cfPort
	cfUser
	cfPassword
	cfDatabase
	cfOptions // the URL's query: sslmode=require, ssl-ca=…
	cfFile
	cfBoxes
)

// The form's rows that are no text box.
const (
	cfKind = -1 // which kind
	cfSave = -2 // where the password is kept
)

var connLabels = map[int]string{
	cfKind: "Type", cfSave: "Save", cfName: "Name", cfHost: "Host", cfPort: "Port", cfUser: "User",
	cfPassword: "Password", cfDatabase: "Database", cfOptions: "Options", cfFile: "File",
}

// connForm is the New and Edit Connection box, DataGrip's Data Source: a box
// per part of the URL, the password with where it is kept, and what a test
// found. config.toml keeps the URL without a password, but one written into
// it by hand.
type connForm struct {
	old     string // the name it edits, "" for a new one
	written string // a password written into its URL by hand: it stays there while the box holds it
	kind    int    // in connKinds
	in      [cfBoxes]textinput.Model
	at      int  // the row with the keyboard, in rows()
	keep    bool // the password goes in the keyring, Save: Forever; else it is kept for the run
	test    string
	ok      bool
	gen     int // a later test drops an earlier one's answer
}

type connTestMsg struct {
	f    *connForm
	gen  int
	objs int
	err  error
}

// newConnForm is the form for the connection called name, a new one for "".
func newConnForm(m *Model, name string) *connForm {
	f := &connForm{old: name, keep: hasKeyring()}
	for i := range f.in {
		f.in[i] = modalInput()
		f.in[i].Prompt = ""
		f.in[i].Blur()
	}

	f.in[cfPassword].EchoMode = textinput.EchoPassword
	f.in[cfHost].SetValue("localhost")
	f.in[cfPort].SetValue(connKinds[0].port)

	if u, err := url.Parse(m.st.Databases[name]); name != "" && err == nil {
		file := ""
		if d, err := db.Parse(m.st.Databases[name]); err == nil && d.Kind == db.SQLite {
			file = d.Target
		}

		for k, c := range connKinds {
			if c.scheme == u.Scheme || c.scheme+"ql" == u.Scheme { // the postgresql scheme too
				f.kind = k
			}
		}

		pw, _ := u.User.Password()
		inQuery, query := db.QueryPassword(u.RawQuery) // libpq's password=: in the box, not in Options for all to see
		pw = cmp.Or(pw, inQuery)
		f.written = pw

		for i, v := range map[int]string{
			cfName: name, cfHost: u.Hostname(), cfPort: u.Port(), cfUser: u.User.Username(), cfPassword: pw,
			cfDatabase: strings.Trim(u.Path, "/"), cfOptions: query, cfFile: file,
		} {
			f.in[i].SetValue(v)
		}

		switch { // the box left empty keeps the password it has, wherever it is
		case pw != "": // in the URL, hand-written
		case m.db.passwords[name] != "":
			f.in[cfPassword].Placeholder, f.keep = "kept for this run", false
		default: // the keyring may have one: asking it here would hold up the UI
			f.in[cfPassword].Placeholder = "unchanged when empty"
		}
	}

	f.move(1) // to the name

	return f
}

// rows are the form's rows for its kind: a file for SQLite, a server's
// parts for the others.
func (f *connForm) rows() []int {
	if connKinds[f.kind].scheme == "sqlite" {
		return []int{cfKind, cfName, cfFile}
	}

	return []int{cfKind, cfName, cfHost, cfPort, cfUser, cfPassword, cfSave, cfDatabase, cfOptions}
}

// move gives the row d away the keyboard.
func (f *connForm) move(d int) tea.Cmd {
	rows := f.rows()
	f.at = (f.at + d + len(rows)) % len(rows)

	for i := range f.in {
		f.in[i].Blur()
	}

	if r := rows[f.at]; r >= 0 {
		f.in[r].CursorEnd()
		return f.in[r].Focus()
	}

	return nil
}

// cycle turns the kind d along, its default port with it.
func (f *connForm) cycle(d int) {
	was := connKinds[f.kind].port
	f.kind = (f.kind + d + len(connKinds)) % len(connKinds)

	if p := f.in[cfPort].Value(); p == "" || p == was {
		f.in[cfPort].SetValue(connKinds[f.kind].port)
	}
}

// file is the SQLite file box as an absolute path, ~ expanded.
func (f *connForm) file() string {
	p := strings.TrimSpace(f.in[cfFile].Value())
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, rest)
		}
	}

	if abs, err := filepath.Abs(p); err == nil && p != "" {
		p = abs
	}

	return p
}

// url is the connection as config.toml keeps it: no password, but one written
// into it by hand while the box still holds it.
func (f *connForm) url() string {
	k := connKinds[f.kind]
	if k.scheme == "sqlite" {
		p := filepath.ToSlash(f.file())
		if !strings.HasPrefix(p, "/") { // a Windows drive: sqlite:///C:/…
			p = "/" + p
		}

		return (&url.URL{Scheme: "sqlite", Path: p}).String()
	}

	host := strings.TrimSpace(f.in[cfHost].Value())
	if port := strings.TrimSpace(f.in[cfPort].Value()); port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") { // IPv6, bracketed in a URL
		host = "[" + host + "]"
	}

	u := url.URL{Scheme: k.scheme, Host: host}
	if user := strings.TrimSpace(f.in[cfUser].Value()); user != "" {
		u.User = url.User(user)
	}

	if f.written != "" && f.in[cfPassword].Value() == f.written { // the user's own, there before the form
		u.User = url.UserPassword(strings.TrimSpace(f.in[cfUser].Value()), f.written)
	}

	if name := strings.TrimSpace(f.in[cfDatabase].Value()); name != "" {
		u.Path = "/" + name
	}

	u.RawQuery = strings.TrimPrefix(strings.TrimSpace(f.in[cfOptions].Value()), "?")

	return u.String()
}

// name is the name box, or DataGrip's default: database@host, the file's.
func (f *connForm) name() string {
	if n := strings.TrimSpace(f.in[cfName].Value()); n != "" {
		return n
	}

	if connKinds[f.kind].scheme == "sqlite" {
		return filepath.Base(f.file())
	}

	who := cmp.Or(strings.TrimSpace(f.in[cfDatabase].Value()), strings.TrimSpace(f.in[cfUser].Value()), connKinds[f.kind].scheme)

	return who + "@" + strings.TrimSpace(f.in[cfHost].Value())
}

// target is the database as the form has it, with the password typed or,
// editing, the one it had.
func (f *connForm) target(m *Model) (db.DB, error) {
	d, err := db.Parse(f.url())
	if err != nil {
		return d, err
	}

	d.Password = f.in[cfPassword].Value()
	if d.Password == "" && f.old != "" {
		d.Password, d.Keyring = m.db.passwords[f.old], f.old
	}

	return d, nil
}

// testConn reads the connection's tables and says what it found.
func (f *connForm) testConn(m *Model) tea.Cmd {
	d, err := f.target(m)
	if err != nil {
		f.test, f.ok = err.Error(), false
		return nil
	}

	f.gen++
	f.test, f.ok = "testing…", true
	gen := f.gen

	return func() tea.Msg {
		objs, err := d.Objects()
		return connTestMsg{f: f, gen: gen, objs: len(objs), err: err}
	}
}

func (f *connForm) onTest(msg connTestMsg) {
	if msg.gen != f.gen {
		return
	}

	if f.ok = msg.err == nil; f.ok {
		f.test = "connected: " + plural(msg.objs, "table")
	} else {
		f.test = errLine(msg.err.Error())
	}
}

// problem is why the form cannot be saved as it is, "" when it can.
func (f *connForm) problem(m *Model) string {
	name := f.name()
	_, taken := m.st.Databases[name]

	switch {
	case name == envDB:
		return envDB + " is the worktree's .env: another name"
	case taken && name != f.old:
		return name + " is taken"
	case connKinds[f.kind].scheme == "sqlite" && !db.IsSQLite(f.file()): // no host is a server's local socket
		return "no SQLite database at " + f.file()
	}

	if _, err := db.Parse(f.url()); err != nil {
		return err.Error()
	}

	return ""
}

// save writes the connection to config.toml through the daemon, and its
// password to the keyring (Save: Forever) or to memory, for the run.
func (f *connForm) save(m *Model) tea.Cmd {
	if why := f.problem(m); why != "" {
		f.test, f.ok = why, false
		return nil
	}

	inURL := f.written != "" && f.in[cfPassword].Value() == f.written // written into the URL by hand: it stays there
	name, old, keep := f.name(), f.old, f.keep && hasKeyring() && !inURL
	patch := map[string]string{name: f.url()}

	if old != "" && old != name {
		patch[old] = ""
	}

	if m.st.Databases == nil { // as setSettings does: here at once, the daemon's state event after
		m.st.Databases = map[string]string{}
	}

	for k, v := range patch {
		if v == "" {
			delete(m.st.Databases, k)
		} else {
			m.st.Databases[k] = v
		}
	}

	v := &m.db
	v.sync(m)

	pw := cmp.Or(f.in[cfPassword].Value(), v.passwords[old]) // left empty, the box keeps the one it had
	if pw != "" {
		v.passwords[name] = pw
	}

	if old != name {
		delete(v.passwords, old)
		delete(v.open, old)
		delete(v.objs, old)
	}

	delete(v.objs, name)
	v.open[name] = true
	m.modal = nil

	// The keyring: Forever keeps the password there, Until restart in memory
	// only; memory has it either way before the tables are read.
	kept := func() tea.Msg {
		if pw == "" && old != "" {
			pw = keyringLoad(old)
		}

		var err error

		switch {
		case keep && pw != "":
			err = keyringSave(name, pw)
		case !keep:
			keyringForget(name)
		}

		if old != "" && old != name && err == nil {
			keyringForget(old) // renamed: nothing left under the old name, once the new one has it
		}

		return dbPasswordMsg{id: name, pw: pw, err: err}
	}

	return tea.Batch(do("state.set", map[string]any{"databases": patch}), kept, flash("saved "+name, false))
}

func (f *connForm) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	row := f.rows()[f.at]

	switch s := k.String(); s {
	case "esc":
		m.modal = nil
		return nil

	case "tab", "down":
		return f.move(1)
	case "shift+tab", "up":
		return f.move(-1)
	case "enter":
		return f.save(m)
	case "ctrl+t":
		return f.testConn(m)
	case "left", "right", "space":
		switch row {
		case cfKind:
			f.cycle(ifElse(s == "left", -1, 1))
			return nil

		case cfSave:
			f.keep = !f.keep && hasKeyring()
			return nil
		}
	}

	if row < 0 {
		return nil
	}

	var cmd tea.Cmd

	f.in[row], cmd = f.in[row].Update(k)

	return cmd
}

// click is a click at cell cx past the frame of row k: the row takes the
// keyboard, and on Type a click on ‹ turns it back, elsewhere on the value
// on; on Save one flips it, as ← and → do.
func (f *connForm) click(_ *Model, k, cx int) tea.Cmd {
	const value = 11 // " ", the label padded to 9, " "

	if k >= len(f.rows()) { // the URL, the rule or the test's line
		return nil
	}

	cmd := f.move(k - f.at)

	switch row := f.rows()[k]; {
	case cx < value:
	case row == cfKind:
		f.cycle(ifElse(cx < value+2, -1, 1)) // "‹ " is the value's first two cells
	case row == cfSave:
		f.keep = !f.keep && hasKeyring()
	}

	return cmd
}

// paste types a pasted text into the box with the keyboard.
func (f *connForm) paste(msg tea.PasteMsg) tea.Cmd {
	if row := f.rows()[f.at]; row >= 0 {
		var cmd tea.Cmd

		f.in[row], cmd = f.in[row].Update(msg)

		return cmd
	}

	return nil
}

// boxStyles keep a box's text in its color without the keyboard, as VS
// Code's inputs do: bubbles' turns ANSI 7, unreadable on a light theme. The
// placeholder takes input.placeholderForeground's, as inputStyles gives it.
func boxStyles(st textinput.Styles) textinput.Styles {
	st.Blurred.Text = st.Focused.Text
	st.Focused.Placeholder, st.Blurred.Placeholder = fg(pal.descFg), fg(pal.descFg)
	st.Cursor.Color = nil // the text's: ANSI 7 is near white on a light theme
	st.Focused.Prompt, st.Blurred.Prompt = plain, plain

	return st
}

// height is the box's rows, its frame included: the rows, the URL, a rule
// and the test's line.
func (f *connForm) height() int { return len(f.rows()) + 5 }

// size is placed as if PostgreSQL's, the tallest kind, so the top stays put
// as the kind turns.
func (f *connForm) size() (w, h, placed int) { return 72, f.height(), (&connForm{}).height() }

// lines draws the form w cells wide.
func (f *connForm) lines(w int) []string {
	iw := w - 2
	side := dim.Render("│")
	title := ansi.Truncate(" "+ifElse(f.old == "", "New Connection", "Edit "+f.old)+" ", iw, "… ")
	out := []string{dim.Render("╭") + bold.Render(title) + dim.Render(strings.Repeat("─", max(iw-ansi.StringWidth(title), 0))+"╮")}

	f.in[cfName].Placeholder = f.name()
	f.in[cfOptions].Placeholder = ifElse(connKinds[f.kind].scheme == "postgres", "sslmode=require", "ssl-ca=/path/to/ca.pem")

	for k, r := range f.rows() {
		focused := k == f.at
		label := sg(" "+pad(connLabels[r], 9, false)+" ", ifElse(focused, accent, dim))

		var val string

		switch r {
		case cfKind:
			val = "‹ " + connKinds[f.kind].label + " ›"
		case cfSave:
			val = ifElse(!hasKeyring(), "Until restart (no keyring here)", ifElse(f.keep, "‹ Forever › in the keyring", "‹ Until restart ›"))
		default:
			f.in[r].SetWidth(max(iw-14, 1))
			f.in[r].SetStyles(boxStyles(f.in[r].Styles()))
			val = realCaret(f.in[r].View())
		}

		out = append(out, side+row(iw, nil, []seg{label, sg(val, ifElse(focused && r < 0, accent, plain))})+side)
	}

	out = append(out,
		side+row(iw, nil, []seg{sg(" "+pad("URL", 9, false)+" ", dim), sg(redacted(f.url()), dim)})+side,
		dim.Render("├"+strings.Repeat("─", iw)+"┤"))

	status := sg(" "+f.test, ifElse(f.ok, fg(pal.ok), fg(pal.errc)))
	out = append(out, side+row(iw, nil, []seg{status}, sg("^t test  ⏎ save  esc ", dim))+side,
		dim.Render("╰"+strings.Repeat("─", iw)+"╯"))

	return out
}

// redacted is raw without its password, for the screen.
func redacted(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Redacted()
	}

	return raw
}

// newConn opens the form for a new connection.
func (v *dbView) newConn(m *Model) tea.Cmd { return v.editConn(m, "") }

// editConn opens the form on connection name from config.toml, a new one
// for "".
func (v *dbView) editConn(m *Model, name string) tea.Cmd {
	if _, err := url.Parse(m.st.Databases[name]); err != nil { // the form would save its empty boxes over it
		return flash(name+"'s URL does not parse: fix it in config.toml", true)
	}

	v.sync(m)
	m.modal = &modal{form: newConnForm(m, name), x: -1, area: m.editorArea()}

	return nil
}

// removeConn drops connection name from config.toml, and its password
// from the keyring, once asked.
func (v *dbView) removeConn(m *Model, name string) tea.Cmd {
	m.modal = newDialog("Remove the connection "+name+"?",
		item{label: "Remove", run: func(m *Model) tea.Cmd {
			delete(m.st.Databases, name)
			delete(v.passwords, name)
			delete(v.open, name)
			delete(v.objs, name)

			return tea.Batch(do("state.set", map[string]any{"databases": map[string]string{name: ""}}),
				func() tea.Msg { keyringForget(name); return nil },
				flash("removed "+name, false))
		}},
		cancelItem())
	m.modal.area = m.editorArea()

	return nil
}
