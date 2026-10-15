// Package db reads and edits databases through their command line clients,
// as internal/git drives git: sqlite3 for a SQLite file, psql for Postgres,
// mariadb (or mysql) for MariaDB and MySQL; no driver, no cgo. Each call is
// one client process fed a script on stdin; a row comes back as a JSON array
// on a line of its own, every value text, null or a blob's first bytes, so a
// NULL never reads as an empty string.
package db

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The kinds of database, by the client that reaches them.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MariaDB  = "mariadb" // MySQL too
)

// DB is a database and the client that reaches it.
type DB struct {
	Kind   string
	Target string // the SQLite file, or the server's URL
	// Password is one asked for where the URL, ~/.pgpass or ~/.my.cnf has
	// none. It goes to the client in its environment, never on its command
	// line, where ps would show it.
	Password string
	// Keyring is the name the system keyring may keep its password under,
	// looked up when Password is empty.
	Keyring string
}

// File is the SQLite database at path.
func File(path string) DB { return DB{Kind: SQLite, Target: path} }

// Parse is the database a URL names: postgres:// or postgresql://, mariadb://
// or mysql://, with the user, host, port and database in it, or a SQLite
// file as sqlite:///path.
func Parse(raw string) (DB, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return DB{}, errors.New("not a URL") // its text may hold a password
	}

	switch u.Scheme {
	case "sqlite":
		p := u.Path
		if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' { // a drive after the third slash
			p = p[1:]
		}

		return File(filepath.FromSlash(p)), nil

	case "postgres", "postgresql":
		return DB{Kind: Postgres, Target: raw}, nil
	case "mariadb", "mysql":
		return DB{Kind: MariaDB, Target: raw}, nil
	}

	return DB{}, fmt.Errorf("%s:// is not postgres://, mariadb://, mysql:// or sqlite://", u.Scheme)
}

// client is the program that reaches kind: mariadb, else mysql, for MariaDB.
func client(kind string) string {
	switch kind {
	case Postgres:
		return "psql"
	case MariaDB:
		if _, err := exec.LookPath("mariadb"); err != nil {
			return "mysql"
		}

		return "mariadb"
	}

	return "sqlite3"
}

// Has reports the client kind needs on the PATH.
func Has(kind string) bool { _, err := exec.LookPath(client(kind)); return err == nil }

// header starts every SQLite database file.
const header = "SQLite format 3\x00"

// IsSQLite reports a file that starts with SQLite's header: a .db can be
// anything, Windows' Thumbs.db for one.
func IsSQLite(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer func() { _ = f.Close() }() // read only: nothing to flush

	b := make([]byte, len(header))
	_, err = io.ReadFull(f, b)

	return err == nil && string(b) == header
}

const (
	timeout  = 60 * time.Second // one client call
	blobHead = 32               // the bytes of a blob a page shows
)

// ErrPassword is a server that wants a password the client was not given,
// or not this one.
var ErrPassword = errors.New("password needed")

// command is the client for d, reading its script on stdin, and what goes
// before the script. A read is read-only. psql and mariadb take the password
// in their environment; ~/.psqlrc and ~/.sqliterc, which could change what
// comes back, are skipped, while ~/.pgpass and ~/.my.cnf still give theirs.
func (d DB) command(ctx context.Context, write bool) (cmd *exec.Cmd, pre string, err error) {
	if d.Kind == SQLite {
		// -safe keeps a query inside the database (no .shell, no ATTACH),
		// and -bail stops at the first error, which rolls an open
		// transaction back as sqlite3 exits.
		args := []string{"-init", os.DevNull, "-safe", "-batch", "-bail", "-noheader", "-list"}
		if !write {
			args = append(args, "-readonly")
		}

		// sqlite3 starts with foreign keys off, where the apps that own the
		// file turn them on: a deletion cascades, an orphan fails, as there.
		return exec.CommandContext(ctx, "sqlite3", append(args, d.Target)...), ".timeout 3000\npragma foreign_keys = on;\n", nil
	}

	u, err := url.Parse(d.Target)
	if err != nil {
		return nil, "", errors.New("not a URL")
	}

	pw := d.password(u)
	if d.Kind == Postgres {
		cmd, pre = psql(ctx, u, pw, write)
	} else {
		cmd, pre = mariadb(ctx, u, pw, write)
	}

	return cmd, pre, nil
}

// password is d's: the one given, else the URL's (libpq also takes one as
// password= in the query), else the keyring's.
func (d DB) password(u *url.URL) string {
	if d.Password != "" {
		return d.Password
	}

	if pw, ok := u.User.Password(); ok {
		return pw
	}

	if pw, _ := QueryPassword(u.RawQuery); pw != "" {
		return pw
	}

	if d.Keyring != "" {
		return LoadPassword(d.Keyring)
	}

	return ""
}

// Console is the command that opens d's client for a person to type into,
// the password left out of it: env names the variable the client reads it
// from, pw is the one pando has, "" for none.
func (d DB) Console() (argv []string, env, pw string) {
	if d.Kind == SQLite {
		return []string{"sqlite3", d.Target}, "", ""
	}

	u, err := url.Parse(d.Target)
	if err != nil {
		return nil, "", ""
	}

	pw = d.password(u)
	if d.Kind == Postgres {
		return []string{"psql", bare(u)}, "PGPASSWORD", pw
	}

	return append([]string{client(MariaDB)}, mariadbArgs(u)...), "MYSQL_PWD", pw
}

// mariadbOptions are the URL query keys a MariaDB URL may set, each the
// client's --option of that name: TLS and the transport. A .env from a
// cloned repository is no place for --plugin-dir or --init-command.
var mariadbOptions = []string{
	"ssl", "skip-ssl", "ssl-ca", "ssl-capath", "ssl-cert", "ssl-key", "ssl-verify-server-cert", "ssl-mode", "protocol", "socket", "compress",
}

// mariadbArgs are the client's arguments for the server at u: host, port,
// user, database and the options mariadbOptions allows.
func mariadbArgs(u *url.URL) []string {
	var args []string
	if u.Hostname() != "" {
		args = append(args, "-h", u.Hostname())
	}

	if u.Port() != "" {
		args = append(args, "-P", u.Port())
	}

	if name := u.User.Username(); name != "" {
		args = append(args, "-u", name)
	}

	if base := strings.Trim(u.Path, "/"); base != "" {
		args = append(args, "-D", base)
	}

	args = append(args, "--default-character-set=utf8mb4") // not the locale's: latin1 under C would mangle what it writes

	q := u.Query()
	for _, k := range slices.Sorted(maps.Keys(q)) {
		for _, v := range q[k] {
			switch {
			case !slices.Contains(mariadbOptions, k):
			case v == "":
				args = append(args, "--"+k)
			default:
				args = append(args, "--"+k+"="+v)
			}
		}
	}

	return args
}

// QueryPassword splits libpq's password= out of a raw query: the password,
// percent-decoded as libpq does it (a + stays a +), and the rest as it was.
func QueryPassword(raw string) (pw, rest string) {
	var keep []string

	for _, kv := range strings.Split(raw, "&") {
		k, v, _ := strings.Cut(kv, "=")
		if k, _ = url.PathUnescape(k); k == "password" { // libpq decodes the name too: pass%77ord
			pw, _ = url.PathUnescape(v) // malformed: no password
			continue
		}

		keep = append(keep, kv)
	}

	return pw, strings.Join(keep, "&")
}

// bare is u as a command line may show it: without the user's password or
// the password= libpq takes in the query.
func bare(u *url.URL) string {
	v := *u
	if v.User != nil {
		v.User = url.User(v.User.Username())
	}

	_, v.RawQuery = QueryPassword(v.RawQuery)

	return v.String()
}

// psql reaches the server at u, the password in its environment. The
// script sets the timeout, and a read's session read-only, first, where
// the options= of a URL cannot undo them as it would PGOPTIONS.
func psql(ctx context.Context, u *url.URL, pw string, write bool) (cmd *exec.Cmd, pre string) {
	cmd = exec.CommandContext(ctx, "psql", "-X", "-q", "-A", "-t", "-w", "-v", "ON_ERROR_STOP=1", "-d", bare(u))

	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")

	if pw != "" {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+pw)
	}

	// quote doubles a ' and leaves a \ be: standard_conforming_strings off
	// would take it for an escape, and 'x\y' for 'xy', another row's key.
	// PGCLIENTENCODING or the URL may pick another encoding than the UTF-8
	// pando writes and reads, and extra_float_digits below 1 prints 0.3 for
	// 0.1 + 0.2 as well: two float keys as one.
	pre = "set standard_conforming_strings = on;\nset client_encoding = 'UTF8';\nset extra_float_digits = 3;\n" +
		"set statement_timeout = " + strconv.Itoa(int(timeout.Milliseconds())) + ";\n"
	if !write {
		pre += "set session characteristics as transaction read only;\n"
	}

	return cmd, pre
}

// mariadb reaches the server at u by its parts; a read sets its session
// read-only first.
func mariadb(ctx context.Context, u *url.URL, pw string, write bool) (cmd *exec.Cmd, pre string) {
	// --binary-mode keeps the \r\n and the NUL a value holds, and runs no
	// client command (\!).
	args := append([]string{"--batch", "--raw", "--skip-column-names", "--binary-mode", "--connect-timeout=10"}, mariadbArgs(u)...)
	cmd = exec.CommandContext(ctx, client(MariaDB), args...)
	cmd.Env = os.Environ()

	if pw != "" {
		cmd.Env = append(cmd.Env, "MYSQL_PWD="+pw)
	}

	// A TIMESTAMP reads in the session's zone, where the hour a clock goes
	// back repeats: two rows would read as one key. UTC repeats none. quote
	// escapes with backslashes, which NO_BACKSLASH_ESCAPES would take
	// literally.
	pre = "set time_zone = '+00:00';\nset session sql_mode = replace(@@sql_mode, 'NO_BACKSLASH_ESCAPES', '');\n"
	if !write {
		pre += "set session transaction read only;\n"
	}

	return cmd, pre
}

// run feeds script to d's client and returns what it printed; cancelling
// ctx stops the client.
func (d DB) run(ctx context.Context, write bool, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, pre, err := d.command(ctx, write)
	if err != nil {
		return "", err
	}

	// Interrupted, psql and mariadb stop the query on the server as well,
	// which a killed client leaves running; Windows has no SIGINT to send.
	cmd.Cancel = func() error {
		if cmd.Process.Signal(os.Interrupt) != nil {
			return cmd.Process.Kill()
		}

		return nil
	}
	cmd.WaitDelay = 3 * time.Second // then killed
	cmd.Stdin = strings.NewReader(pre + script)

	var out, errb bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errb

	err = cmd.Run()

	switch msg := message(errb.String()); {
	case err == nil:
		return out.String(), nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return out.String(), fmt.Errorf("%s took longer than %s", client(d.Kind), timeout)
	case ctx.Err() != nil:
		return out.String(), ctx.Err()
	case errors.Is(err, exec.ErrNotFound):
		return out.String(), fmt.Errorf("%s is not installed", client(d.Kind))
	case authFailed.MatchString(msg):
		return out.String(), fmt.Errorf("%s: %w", msg, ErrPassword)
	case msg != "":
		return out.String(), errors.New(msg)
	}

	return out.String(), err
}

var (
	authFailed = regexp.MustCompile(`password authentication failed|no password supplied|Access denied for user`)
	// errLeads are the clients' ways to start an error line: sqlite3's,
	// psql's on a statement and on connecting, mariadb's; the message is
	// what follows them.
	errLeads = []*regexp.Regexp{
		regexp.MustCompile(`^(?:Error|Parse error|Runtime error)[^:]*: (?:in prepare, |stepping, )?(.*?)(?: \(\d+\))?$`),
		regexp.MustCompile(`^(?:psql:[^:]*:\d+: )?(?:ERROR|FATAL): +(.*)$`), // a script on stdin goes without the psql: part
		regexp.MustCompile(`^psql: error: .*?(?:FATAL|ERROR): +(.*)$`),
		regexp.MustCompile(`^ERROR \d+ \(\w+\)(?: at line \d+)?: (.*)$`),
	}
)

// message is the line of a client's stderr worth showing, without what its
// client puts before it: the first error, else the first line not a warning,
// which mariadb prints ahead of its errors.
func message(stderr string) string {
	ls := strings.Split(strings.TrimSpace(stderr), "\n")
	for _, l := range ls {
		for _, re := range errLeads {
			if m := re.FindStringSubmatch(l); m != nil {
				return m[1]
			}
		}
	}

	for _, l := range ls {
		if !strings.HasPrefix(strings.ToUpper(l), "WARNING") {
			return l
		}
	}

	return ls[0]
}

// Ident quotes a name for d's SQL: in backticks for MariaDB, double quotes
// for the others.
func (d DB) Ident(s string) string {
	if d.Kind == MariaDB {
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	}

	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quote is s as a string literal in d's SQL; MariaDB reads a backslash in
// one as an escape.
func (d DB) quote(s string) string {
	if d.Kind == MariaDB {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}

	if d.Kind == SQLite && strings.ContainsAny(s, "\r\x00") {
		// sqlite3 reads its script by lines: it drops the \r of a \r\n, in a
		// string too, and ends a line at a NUL. In hex the text has neither.
		return "cast(x'" + hex.EncodeToString([]byte(s)) + "' as text)"
	}

	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// text is a value as text in d's SQL.
func (d DB) text(expr string) string {
	switch d.Kind {
	case MariaDB:
		return "cast(" + expr + " as char)"
	case SQLite: // a REAL to the digits it reads back with: as text it keeps 15, and 0.1 + 0.2 reads as 0.3
		return fmt.Sprintf("case when typeof(%[1]s) = 'real' and cast(cast(%[1]s as text) as real) <> %[1]s then printf('%%!.17g', %[1]s) "+
			"else cast(%[1]s as text) end", expr)
	}

	return "cast(" + expr + " as text)"
}

// array is the function that makes a JSON array in d's SQL.
func (d DB) array() string {
	if d.Kind == Postgres {
		return "json_build_array"
	}

	return "json_array"
}

// lines splits a client's output into its rows.
func lines(out string) []string {
	out = strings.TrimSuffix(out, "\n")
	if out == "" {
		return nil
	}

	return strings.Split(out, "\n")
}

// Object is a table or a view of a database.
type Object struct {
	Schema string // Postgres: a schema not the connection's own; "" otherwise
	Name   string
	View   bool // a view, a virtual or foreign table: its rows are read, never written
}

// Objects lists d's tables and views: SQLite's own, a virtual table's
// shadow tables and the system schemas left out.
func (d DB) Objects() ([]Object, error) {
	var query string

	switch d.Kind {
	case Postgres:
		// From pg_class: information_schema.tables leaves materialized views out.
		query = `select json_build_object('schema', case when n.nspname = current_schema() then '' else n.nspname end, ` +
			`'name', c.relname, 'type', case when c.relkind in ('r', 'p') then 'BASE TABLE' else 'VIEW' end) ` +
			`from pg_class c join pg_namespace n on n.oid = c.relnamespace ` +
			`where c.relkind in ('r', 'p', 'v', 'm', 'f') and n.nspname not in ('pg_catalog', 'information_schema') ` +
			`and n.nspname not like 'pg\_%' and has_table_privilege(c.oid, 'select') ` +
			`order by n.nspname <> current_schema(), n.nspname, c.relname;`

	case MariaDB:
		query = `select json_object('schema', '', 'name', table_name, 'type', table_type) from information_schema.tables ` +
			`where table_schema = database() order by table_name;`

	default:
		query = `select json_object('schema', '', 'name', name, 'type', case type when 'table' then 'BASE TABLE' else type end) ` +
			`from pragma_table_list where schema = 'main' and type != 'shadow' and name not like 'sqlite\_%' escape '\' order by name;`
	}

	out, err := d.run(context.Background(), false, query+"\n")
	if err != nil {
		return nil, err
	}

	var objs []Object

	for _, l := range lines(out) {
		var o struct{ Schema, Name, Type string }
		if err := json.Unmarshal([]byte(l), &o); err != nil {
			return nil, fmt.Errorf("%s printed %q: %w", client(d.Kind), l, err)
		}

		objs = append(objs, Object{Schema: o.Schema, Name: o.Name, View: o.Type != "BASE TABLE"})
	}

	return objs, nil
}

// Column is one column of a table.
type Column struct {
	Name, Type string
	PK         bool // part of the primary key
	Generated  bool // computed from the other columns, or an identity always generated: never written
	Blob       bool // binary, shown by its first bytes, never edited; SQLite tells by the value instead
}

// Table is a table or a view, with what reading and writing its rows takes.
type Table struct {
	DB           DB
	Schema, Name string
	Cols         []Column
	View         bool // a view, a virtual or foreign table
	// Keys are the columns a row is found by again: SQLite's rowid, but
	// for a WITHOUT ROWID table its primary key; the primary key elsewhere,
	// else Postgres's tableoid, ctid and xmin; none for a view, or a MariaDB
	// table without a primary key, whose rows are only read.
	Keys []string
}

// ReadOnly reports a table whose rows cannot be found again to be changed.
func (t Table) ReadOnly() bool { return len(t.Keys) == 0 }

// ref is the table as d's SQL names it.
func (t Table) ref() string {
	if t.Schema != "" {
		return t.DB.Ident(t.Schema) + "." + t.DB.Ident(t.Name)
	}

	return t.DB.Ident(t.Name)
}

// Open describes table name, in schema for Postgres.
func (d DB) Open(ctx context.Context, schema, name string) (Table, error) {
	t := Table{DB: d, Schema: schema, Name: name}

	var script string

	switch d.Kind {
	case Postgres:
		rel := d.quote(t.ref())
		script = fmt.Sprintf("select json_build_object('type', relkind) from pg_class where oid = %[1]s::regclass;\n"+
			"select json_build_object('name', a.attname, 'type', format_type(a.atttypid, a.atttypmod), "+
			"'pk', coalesce((select k.n from unnest(i.indkey) with ordinality as k(num, n) where k.num = a.attnum), 0), "+
			"'hidden', case when a.attgenerated <> '' or a.attidentity = 'a' then 2 else 0 end) "+
			"from pg_attribute a left join pg_index i on i.indrelid = a.attrelid and i.indisprimary "+
			"where a.attrelid = %[1]s::regclass and a.attnum > 0 and not a.attisdropped order by a.attnum;\n", rel)

	case MariaDB:
		script = fmt.Sprintf("select json_object('type', case table_type when 'BASE TABLE' then 'r' else 'v' end) "+
			"from information_schema.tables where table_schema = database() and table_name = %[1]s;\n"+
			"select json_object('name', c.column_name, 'type', c.column_type, 'pk', coalesce(s.seq_in_index, 0), "+
			"'hidden', case when c.extra like '%%GENERATED%%' then 2 else 0 end) from information_schema.columns c "+
			"left join information_schema.statistics s on s.table_schema = c.table_schema and s.table_name = c.table_name "+
			"and s.column_name = c.column_name and s.index_name = 'PRIMARY' "+
			"where c.table_schema = database() and c.table_name = %[1]s order by c.ordinal_position;\n", d.quote(name))

	default:
		script = fmt.Sprintf("select json_object('type', case when type = 'table' and not wr then 'r' when type = 'table' then 'w' else 'v' end) "+
			"from pragma_table_list where schema = 'main' and name = %[1]s;\n"+
			"select json_object('name', name, 'type', type, 'pk', pk, 'hidden', hidden) from pragma_table_xinfo(%[1]s);\n", d.quote(name))
	}

	out, err := d.run(ctx, false, script)
	if err != nil {
		return t, err
	}

	ls := lines(out)

	var kind struct{ Type string }

	if len(ls) == 0 || json.Unmarshal([]byte(ls[0]), &kind) != nil {
		return t, fmt.Errorf("no table %s", name)
	}

	keys := map[int]string{} // the primary key's columns by their place in it

	for _, l := range ls[1:] {
		var c struct {
			Name, Type string
			PK, Hidden int
		}

		if err := json.Unmarshal([]byte(l), &c); err != nil {
			return t, fmt.Errorf("%s printed %q: %w", client(d.Kind), l, err)
		}

		if c.Hidden == 1 { // a SQLite virtual table's hidden column
			continue
		}

		lt := strings.ToLower(c.Type)
		t.Cols = append(t.Cols, Column{
			Name: c.Name, Type: c.Type, PK: c.PK > 0, Generated: c.Hidden > 1,
			Blob: d.Kind == Postgres && lt == "bytea" || d.Kind == MariaDB && (strings.Contains(lt, "blob") || strings.Contains(lt, "binary") || strings.HasPrefix(lt, "bit") ||
				slices.Contains([]string{"geometry", "point", "linestring", "polygon", "multipoint", "multilinestring", "multipolygon", "geometrycollection"}, lt)), // as char a geometry fails the whole page
		})

		if c.PK > 0 {
			keys[c.PK] = c.Name
		}
	}

	t.View = kind.Type == "v" || kind.Type == "m" || kind.Type == "f"

	switch {
	case t.View: // read only
	case len(keys) > 0 && (d.Kind != SQLite || kind.Type == "w"): // SQLite keeps the rowid unless WITHOUT ROWID
		for _, k := range slices.Sorted(maps.Keys(keys)) {
			t.Keys = append(t.Keys, keys[k])
		}

	case d.Kind == Postgres && (kind.Type == "r" || kind.Type == "p"):
		// A partition's or a child's ctid repeats another's, and a vacuumed
		// slot takes a new row: xmin tells that one, and a row changed since,
		// from the row read.
		t.Keys = []string{"tableoid", "ctid", "xmin"}
	case d.Kind == SQLite && kind.Type == "r": // the rowid, by a name no column took
		for _, alias := range []string{"rowid", "_rowid_", "oid"} {
			if !slices.ContainsFunc(t.Cols, func(c Column) bool { return strings.EqualFold(c.Name, alias) }) {
				t.Keys = []string{alias}
				break
			}
		}
	}

	return t, nil
}

// Cell is one value: text, a NULL, or a blob shown by its first bytes.
type Cell struct {
	Text string // the value as text; a blob's first bytes in hex, as x'…'
	Null bool
	Blob bool // a blob: shown, never edited
}

// Row is one row of a page: Key finds it again for an edit, the key
// columns' values as a JSON array of text; "" in a read-only table.
type Row struct {
	Key   string
	Cells []Cell
}

// cell is column c as a page reads it: its text, or for a blob an object
// with its first bytes in hex and its length, which the row's JSON array
// holds as an object.
func (t Table) cell(c Column) string {
	n := t.DB.Ident(c.Name)

	switch {
	case t.DB.Kind == SQLite:
		return fmt.Sprintf("case typeof(%[1]s) when 'blob' then json_object('blob', hex(substr(%[1]s, 1, %[2]d)), 'len', length(%[1]s)) "+
			"else %[3]s end", n, blobHead, t.DB.text(n))

	case c.Blob && t.DB.Kind == Postgres:
		return fmt.Sprintf("case when %[1]s is not null then json_build_object('blob', upper(encode(substring(%[1]s from 1 for %[2]d), 'hex')), "+
			"'len', octet_length(%[1]s)) end", n, blobHead)

	case c.Blob:
		return fmt.Sprintf("case when %[1]s is not null then json_object('blob', hex(left(%[1]s, %[2]d)), 'len', length(%[1]s)) end", n, blobHead)
	}

	return t.DB.text(n)
}

// Page is a page of a table's rows, and how many rows there are.
type Page struct {
	Rows  []Row
	Total int  // the rows the filter keeps; for a big table and no filter, the server's estimate
	Exact bool // Total was counted
	More  bool // rows follow the page
}

// bigTable is the estimate past which a page does not count the rows of a
// table it shows unfiltered: count(*) reads every one of them.
const bigTable = 100_000

// estimate is the query for the server's guess at t's rows, as n.
func (t Table) estimate() string {
	if t.DB.Kind == Postgres {
		return "select reltuples::bigint as n from pg_class where oid = " + t.DB.quote(t.ref()) + "::regclass"
	}

	return "select coalesce(table_rows, 0) as n from information_schema.tables where table_schema = database() and table_name = " + t.DB.quote(t.Name)
}

// from is the FROM clause where keeps the rows of; the line break after it
// ends a -- comment the box may hold.
func (t Table) from(where string) string {
	if strings.TrimSpace(where) == "" {
		return " from " + t.ref()
	}

	return " from " + t.ref() + " where (" + where + "\n)"
}

// row is exprs as one JSON array in t's SQL. Postgres's json_build_array
// takes 100 arguments: a wider row joins jsonb arrays of 100.
func (t Table) row(exprs []string) string {
	if t.DB.Kind != Postgres || len(exprs) <= 100 {
		return t.DB.array() + "(" + strings.Join(exprs, ", ") + ")"
	}

	var parts []string
	for chunk := range slices.Chunk(exprs, 100) {
		parts = append(parts, "jsonb_build_array("+strings.Join(chunk, ", ")+")")
	}

	return strings.Join(parts, " || ")
}

// Page reads rows [offset, offset+limit) of those where keeps, sorted by
// order; an empty where keeps every row, an empty order sorts by the keys.
// where and order are SQL, as typed. A big server table shown unfiltered
// is counted by the server's estimate, which Count makes exact.
func (t Table) Page(ctx context.Context, where, order string, offset, limit int) (Page, error) {
	from := t.from(where)

	count := "select count(*)" + from
	if strings.TrimSpace(where) == "" && t.DB.Kind != SQLite {
		count = fmt.Sprintf("select case when e.n < %d then (select count(*)%s) else -e.n end from (%s) e", bigTable, from, t.estimate())
	}

	key, sort := "null", ""

	if len(t.Keys) > 0 {
		keys := make([]string, len(t.Keys))
		for k, c := range t.Keys {
			switch {
			case t.DB.Kind == SQLite: // as a literal, its type kept: a key column of no affinity holds 1 and '1' apart
				// quote() cuts a text at a NUL and keeps its \r: as hex, it does neither.
				keys[k] = fmt.Sprintf("case typeof(%[1]s) when 'text' then 'cast(x''' || hex(%[1]s) || ''' as text)' else quote(%[1]s) end", t.DB.Ident(c))
			case t.keyHex(c):
				keys[k] = "hex(" + t.DB.Ident(c) + ")"
			case t.keyFloat(c): // as char a FLOAT has 6 digits: 1 and 1.0000001 would read as one key
				keys[k] = "cast(cast(" + t.DB.Ident(c) + " as double) as char)"
			default:
				keys[k] = t.DB.text(t.DB.Ident(c))
			}
		}

		key = t.DB.array() + "(" + strings.Join(keys, ", ") + ")"
	}

	switch {
	case strings.TrimSpace(order) != "":
		sort = " order by " + order + "\n"
	case len(t.Keys) > 0:
		var ids []string

		for _, c := range t.Keys {
			if c != "xmin" { // an xid has no order; tableoid and ctid are one row's alone
				ids = append(ids, t.DB.Ident(c))
			}
		}

		sort = " order by " + strings.Join(ids, ", ")
	}

	exprs := []string{key}
	for _, c := range t.Cols {
		exprs = append(exprs, t.cell(c))
	}

	out, err := t.DB.run(ctx, false, count+";\n"+
		"select "+t.row(exprs)+from+sort+fmt.Sprintf(" limit %d offset %d;\n", limit+1, offset)) // one more: whether any follow
	if err != nil {
		return Page{}, err
	}

	ls := lines(out)
	if len(ls) == 0 {
		return Page{}, fmt.Errorf("%s printed nothing", client(t.DB.Kind))
	}

	n, err := strconv.Atoi(ls[0])
	if err != nil {
		return Page{}, fmt.Errorf("%s printed %q for the count", client(t.DB.Kind), ls[0])
	}

	p := Page{Total: max(n, -n), Exact: n >= 0}
	if p.Rows, err = parseRows(ls[1:], len(t.Cols)); err != nil {
		return Page{}, err
	}

	if p.More = len(p.Rows) > limit; p.More {
		p.Rows = p.Rows[:limit]
	}

	return p, nil
}

// Count counts the rows where keeps, every one.
func (t Table) Count(ctx context.Context, where string) (int, error) {
	out, err := t.DB.run(ctx, false, "select count(*)"+t.from(where)+";\n")
	if err != nil {
		return 0, err
	}

	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("%s printed %q for the count", client(t.DB.Kind), out)
	}

	return n, nil
}

// parseRows reads the JSON arrays a page prints, a key then n values each.
func parseRows(ls []string, n int) ([]Row, error) {
	rows := make([]Row, 0, len(ls))

	for _, l := range ls {
		var v []json.RawMessage
		if err := json.Unmarshal([]byte(l), &v); err != nil || len(v) != n+1 {
			return nil, fmt.Errorf("the client printed %q", l)
		}

		r := Row{Cells: make([]Cell, n)}
		if string(v[0]) != "null" {
			r.Key = string(v[0])
		}

		for i, raw := range v[1:] {
			c, err := parseCell(raw)
			if err != nil {
				return nil, fmt.Errorf("the client printed %q: %w", l, err)
			}

			r.Cells[i] = c
		}

		rows = append(rows, r)
	}

	return rows, nil
}

func parseCell(raw json.RawMessage) (Cell, error) {
	switch {
	case string(raw) == "null":
		return Cell{Null: true}, nil
	case len(raw) > 0 && raw[0] == '{':
		var b struct {
			Blob string
			Len  int
		}

		if err := json.Unmarshal(raw, &b); err != nil {
			return Cell{}, err
		}

		text := "x'" + b.Blob + "'"
		if b.Len > blobHead {
			text = fmt.Sprintf("x'%s…' %d bytes", b.Blob, b.Len)
		}

		return Cell{Text: text, Blob: true}, nil
	}

	var s string

	err := json.Unmarshal(raw, &s)

	return Cell{Text: s}, err
}

// Change is an edit to one row: Set columns of the row at Key, add a row
// (Key ""), or delete one (Del).
type Change struct {
	Key string
	Del bool
	Set map[int]*string // column index to its new value, nil for NULL
	Old map[int]Cell    // the set columns as they were read: an update finds them unchanged, or fails
}

// ApplyError is the change Apply stopped at; nothing was written, where
// the table has transactions.
type ApplyError struct {
	Index    int    // the change, in the order given; -1 before the first
	Msg      string // the client's message
	Conflict bool   // its row changed or went since it was read
}

func (e *ApplyError) Error() string {
	if e.Conflict {
		return "the row changed since it was read"
	}

	return e.Msg
}

// Apply writes changes to t in one transaction: every one, or none when one
// fails. Each statement must touch exactly one row, which a temporary
// table's CHECK holds it to: an update whose row changed in the columns it
// sets, or went, since it was read fails as a conflict, and so does a
// deletion whose row went.
func (t Table) Apply(changes []Change) error {
	if t.ReadOnly() {
		return fmt.Errorf("%s is read-only", t.Name)
	}

	var b strings.Builder

	b.WriteString(ifElse(t.DB.Kind == SQLite, "begin immediate;\n", "begin;\n"))
	b.WriteString("create temporary table pando_one(n integer, constraint pando_conflict check (n = 1));\n")

	for i, c := range changes {
		s, where := t.statement(c)

		fmt.Fprintf(&b, "select '#%d';\n", i)

		switch {
		case t.DB.Kind == Postgres:
			fmt.Fprintf(&b, "with u as (%s returning 1) insert into pando_one select count(*) from u;\n", t.Statement(c))
		case t.DB.Kind == MariaDB && c.Key != "" && !c.Del: // row_count() counts the rows an update changed: one set to what it held is found, not changed
			fmt.Fprintf(&b, "select count(*) into @n from %s where %s for update;\n%s where %s;\ninsert into pando_one values (@n);\n", t.ref(), where, s, where)
		case t.DB.Kind == MariaDB:
			fmt.Fprintf(&b, "%s;\ninsert into pando_one values (row_count());\n", t.Statement(c))
		default:
			fmt.Fprintf(&b, "%s;\ninsert into pando_one values (changes());\n", t.Statement(c))
		}
	}

	b.WriteString("commit;\n")

	out, err := t.DB.run(context.Background(), true, b.String())
	if err == nil {
		return nil
	}

	e := &ApplyError{Index: -1, Msg: err.Error(), Conflict: strings.Contains(err.Error(), "pando_conflict")}

	for _, l := range lines(out) {
		if n, ok := strings.CutPrefix(l, "#"); ok {
			e.Index, _ = strconv.Atoi(n) // our own marker
		}
	}

	return e
}

// keyHex reports key column name read and matched as hex: a MariaDB binary
// one, whose bytes that are not UTF-8 a cast to text turns into ?.
func (t Table) keyHex(name string) bool {
	i := slices.IndexFunc(t.Cols, func(c Column) bool { return c.Name == name })
	return t.DB.Kind == MariaDB && i >= 0 && t.Cols[i].Blob
}

// keyFloat reports key column name a MariaDB FLOAT.
func (t Table) keyFloat(name string) bool {
	i := slices.IndexFunc(t.Cols, func(c Column) bool { return c.Name == name })
	return t.DB.Kind == MariaDB && i >= 0 && strings.HasPrefix(strings.ToLower(t.Cols[i].Type), "float")
}

// Statement is the SQL that makes change c to t.
func (t Table) Statement(c Change) string {
	s, where := t.statement(c)
	if where == "" {
		return s
	}

	return s + " where " + where
}

// statement is change c as SQL, and for an update or a deletion apart from
// it the condition that finds its row.
func (t Table) statement(c Change) (s, where string) {
	d := t.DB
	cols := slices.Sorted(maps.Keys(c.Set))

	if c.Key == "" && !c.Del {
		switch {
		case len(cols) == 0 && d.Kind == MariaDB:
			return "insert into " + t.ref() + " () values ()", ""
		case len(cols) == 0:
			return "insert into " + t.ref() + " default values", ""
		}

		names, vals := make([]string, len(cols)), make([]string, len(cols))
		for k, i := range cols {
			names[k], vals[k] = d.Ident(t.Cols[i].Name), t.literal(c.Set[i])
		}

		return fmt.Sprintf("insert into %s (%s) values (%s)", t.ref(), strings.Join(names, ", "), strings.Join(vals, ", ")), ""
	}

	var key []string

	_ = json.Unmarshal([]byte(c.Key), &key) // our own: the array a page read

	var conds []string

	for k, col := range t.Keys {
		switch {
		case k >= len(key):
		case d.Kind == SQLite: // read as a literal already
			conds = append(conds, d.Ident(col)+" = "+key[k])
		default:
			conds = append(conds, d.Ident(col)+" = "+ifElse(t.keyHex(col), "x", "")+d.quote(key[k]))
		}
	}

	if c.Del {
		return "delete from " + t.ref(), strings.Join(conds, " and ")
	}

	sets := make([]string, len(cols))
	for k, i := range cols {
		n := d.Ident(t.Cols[i].Name)
		sets[k] = n + " = " + t.literal(c.Set[i])

		switch old, ok := c.Old[i]; {
		case !ok:
		case old.Null:
			conds = append(conds, n+" is null")
		case d.Kind == MariaDB: // byte by byte: its collations take 'a' for 'A'
			conds = append(conds, "binary "+d.text(n)+" = binary "+d.quote(old.Text))
		default:
			conds = append(conds, d.text(n)+ifElse(d.Kind == Postgres, ` collate "C"`, " collate binary")+" = "+d.quote(old.Text)) // byte by byte, whatever the column's collation
		}
	}

	return "update " + t.ref() + " set " + strings.Join(sets, ", "), strings.Join(conds, " and ")
}

func (t Table) literal(v *string) string {
	if v == nil {
		return "null"
	}

	return t.DB.quote(*v)
}

func ifElse[T any](c bool, a, b T) T {
	if c {
		return a
	}

	return b
}
