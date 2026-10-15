package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// mustDB creates a SQLite database from script in a temp dir, or skips the
// test where sqlite3 is not installed.
func mustDB(t *testing.T, script string) DB {
	t.Helper()

	if !Has(SQLite) {
		t.Skip("sqlite3 is not installed")
	}

	d := File(filepath.Join(t.TempDir(), "app.db"))
	if _, err := d.run(t.Context(), true, script); err != nil {
		t.Fatalf("create the database: %v", err)
	}

	return d
}

// mustOpen describes a table, or fails the test.
func mustOpen(t *testing.T, d DB, schema, name string) Table {
	t.Helper()

	tb, err := d.Open(t.Context(), schema, name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}

	return tb
}

// mustPage reads a table's first rows, or fails the test.
func mustPage(t *testing.T, tb Table, where, order string) ([]Row, int) {
	t.Helper()

	p, err := tb.Page(t.Context(), where, order, 0, 100)
	if err != nil || !p.Exact || p.More {
		t.Fatalf("page of %s: %v %+v", tb.Name, err, p)
	}

	return p.Rows, p.Total
}

// texts is a page's cells as text, NULL as <null>, the first n of each row.
func texts(rows []Row, n int) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		for _, c := range r.Cells[:n] {
			out[i] = append(out[i], map[bool]string{true: "<null>", false: c.Text}[c.Null])
		}
	}

	return out
}

func ptr(s string) *string { return &s }

const schema = `create table users(id integer primary key, name text not null, email text, age int default 18, avatar blob,
	slug text generated always as (lower(name)) virtual);
insert into users(name, email, age, avatar) values ('Ada', 'ada@x.io', 36, x'00ff10'), ('Linus', null, 54, null), ('Grace', '', 85, zeroblob(40));
create table kv(k text primary key, v) without rowid;
insert into kv values ('a', '1'), ('b', '2');
create table odd(rowid text, x);
create view adults as select name from users where age >= 50;
`

func TestIsSQLite(t *testing.T) {
	d := mustDB(t, "create table t(x);")
	other := filepath.Join(t.TempDir(), "Thumbs.db")

	if err := os.WriteFile(other, []byte("not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsSQLite(d.Target) || IsSQLite(other) || IsSQLite(filepath.Join(t.TempDir(), "gone.db")) {
		t.Fatal("IsSQLite tells a database from other files")
	}
}

func TestParse(t *testing.T) {
	for raw, kind := range map[string]string{
		"postgres://app@localhost/app": Postgres, "postgresql://h/db": Postgres, "mariadb://root@127.0.0.1:3306/shop": MariaDB, "mysql://h/db": MariaDB,
	} {
		if d, err := Parse(raw); err != nil || d.Kind != kind || d.Target != raw {
			t.Errorf("Parse(%q) = %+v, %v", raw, d, err)
		}
	}

	if d, err := Parse("sqlite:///tmp/x.db"); err != nil || d != File("/tmp/x.db") {
		t.Errorf("sqlite:// = %+v, %v", d, err)
	}

	if _, err := Parse("redis://h"); err == nil {
		t.Error("redis:// parses")
	}

	if _, err := Parse("postgres://u:secret@h:x/db"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("a bad URL's error shows its password: %v", err)
	}
}

func TestConsole(t *testing.T) {
	argv, env, pw := DB{Kind: Postgres, Target: "postgres://me:secret@db.io:5433/app?sslmode=require"}.Console()
	if !slices.Equal(argv, []string{"psql", "postgres://me@db.io:5433/app?sslmode=require"}) || env != "PGPASSWORD" || pw != "secret" {
		t.Fatalf("psql: %q %s %q", argv, env, pw)
	}

	// libpq takes a password in the query too, + and all: it stays off the
	// command line as well.
	argv, _, pw = DB{Kind: Postgres, Target: "postgres://me@db.io/app?password=s3+cr%26t&options=-c%20search_path%3Dx"}.Console()
	if !slices.Equal(argv, []string{"psql", "postgres://me@db.io/app?options=-c%20search_path%3Dx"}) || pw != "s3+cr&t" {
		t.Fatalf("a password in the query: %q %q", argv, pw)
	}

	// A URL's query passes the options allowed, the rest stay out: a .env
	// is not to be trusted with --init-command.
	argv, env, pw = DB{Kind: MariaDB, Target: "mariadb://me@db.io/app?ssl-ca=/ca.pem&skip-ssl&init-command=drop+database+app", Password: "typed"}.Console()
	if !slices.Equal(argv[1:], []string{"-h", "db.io", "-u", "me", "-D", "app", "--default-character-set=utf8mb4", "--skip-ssl", "--ssl-ca=/ca.pem"}) || env != "MYSQL_PWD" || pw != "typed" {
		t.Fatalf("mariadb: %q %s %q", argv, env, pw)
	}

	if argv, _, pw := File("/tmp/app.db").Console(); !slices.Equal(argv, []string{"sqlite3", "/tmp/app.db"}) || pw != "" {
		t.Fatalf("sqlite3: %q", argv)
	}
}

func TestMessage(t *testing.T) {
	for in, want := range map[string]string{
		"Error: stepping, CHECK constraint failed: pando_conflict (19)":                                                             "CHECK constraint failed: pando_conflict",
		"Error: in prepare, no such column: bogus\n  select bogus\n  ^--- here":                                                     "no such column: bogus",
		"Parse error near line 2: no such column: bogus":                                                                            "no such column: bogus",
		"Runtime error near line 7: NOT NULL constraint failed: users.name (19)":                                                    "NOT NULL constraint failed: users.name",
		"psql:<stdin>:3: ERROR:  column \"bogus\" does not exist\nLINE 1: select bogus":                                             `column "bogus" does not exist`,
		"NOTICE:  table \"zz\" does not exist, skipping\nERROR:  duplicate key value violates unique constraint \"zz_pkey\"":        `duplicate key value violates unique constraint "zz_pkey"`,
		`psql: error: connection to server at "127.0.0.1", port 5432 failed: FATAL:  password authentication failed for user "app"`: `password authentication failed for user "app"`,
		"ERROR 1054 (42S22) at line 2: Unknown column 'bogus' in 'WHERE'":                                                           "Unknown column 'bogus' in 'WHERE'",
		"ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: NO)":                                        "Access denied for user 'root'@'localhost' (using password: NO)",
		"line 1: cannot run .shell in safe mode":                                                                                    "line 1: cannot run .shell in safe mode",
		"WARNING: option --ssl-verify-server-cert is disabled\nERROR 1146 (42S02) at line 1: Table 't.x' doesn't exist":             "Table 't.x' doesn't exist",
		"": "",
	} {
		if got := message(in); got != want {
			t.Errorf("message(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestObjects(t *testing.T) {
	objs, err := mustDB(t, schema).Objects()
	if err != nil {
		t.Fatal(err)
	}

	want := []Object{{Name: "adults", View: true}, {Name: "kv"}, {Name: "odd"}, {Name: "users"}}
	if !slices.Equal(objs, want) {
		t.Fatalf("objects %+v, want %+v", objs, want)
	}
}

func TestOpen(t *testing.T) {
	d := mustDB(t, schema)

	users := mustOpen(t, d, "", "users")
	if !slices.Equal(users.Keys, []string{"rowid"}) || len(users.Cols) != 6 || !users.Cols[0].PK || !users.Cols[5].Generated || users.Cols[3].Type != "INT" {
		t.Fatalf("users: %+v", users)
	}

	for name, keys := range map[string][]string{"kv": {"k"}, "odd": {"_rowid_"}, "adults": nil} {
		if tb := mustOpen(t, d, "", name); !slices.Equal(tb.Keys, keys) {
			t.Errorf("%s: keys %q, want %q", name, tb.Keys, keys)
		}
	}

	if _, err := d.Open(t.Context(), "", "missing"); err == nil {
		t.Fatal("a missing table opens")
	}
}

func TestPage(t *testing.T) {
	d := mustDB(t, schema+`insert into users(name, email) values ('Ken', 'line1
line2	"q" ž');`)
	users := mustOpen(t, d, "", "users")

	rows, total := mustPage(t, users, "", "")
	want := [][]string{
		{"1", "Ada", "ada@x.io", "36", "x'00FF10'", "ada"},
		{"2", "Linus", "<null>", "54", "<null>", "linus"},
		{"3", "Grace", "", "85", "x'" + strings.Repeat("00", blobHead) + "…' 40 bytes", "grace"},
		{"4", "Ken", "line1\nline2\t\"q\" ž", "18", "<null>", "ken"},
	}

	if got := texts(rows, 6); total != 4 || !slices.EqualFunc(got, want, slices.Equal) || rows[1].Key != `["2"]` || !rows[0].Cells[4].Blob {
		t.Fatalf("page: %d %q, want %q", total, got, want)
	}

	p, err := users.Page(t.Context(), "age > 40 -- the old ones", "name desc", 0, 1)
	if err != nil || p.Total != 2 || !p.More || len(p.Rows) != 1 || p.Rows[0].Cells[1].Text != "Linus" {
		t.Fatalf("filtered page: %v %+v", err, p)
	}

	if p, err = users.Page(t.Context(), "age > 40", "name desc", 1, 1); err != nil || p.More || p.Rows[0].Cells[1].Text != "Grace" {
		t.Fatalf("the last page: %v %+v", err, p)
	}

	if n, err := users.Count(t.Context(), "age > 40"); err != nil || n != 2 {
		t.Fatalf("count: %v %d", err, n)
	}

	if _, err := users.Page(t.Context(), "bogus = 1", "", 0, 10); err == nil || err.Error() != "no such column: bogus" {
		t.Fatalf("a bad filter: %v", err)
	}

	if _, err := users.Page(t.Context(), "1; delete from users", "", 0, 10); err == nil {
		t.Fatal("a page writes")
	}

	if rows, total := mustPage(t, mustOpen(t, d, "", "adults"), "", "name"); total != 2 || rows[0].Key != "" || rows[0].Cells[0].Text != "Grace" {
		t.Fatalf("view page: %d %+v", total, rows)
	}
}

func TestApply(t *testing.T) {
	d := mustDB(t, schema)
	users := mustOpen(t, d, "", "users")

	rows, _ := mustPage(t, users, "", "")
	tricky := "it's \\ a\n.shell touch pwned\n;--"

	err := users.Apply([]Change{
		{Key: rows[0].Key, Set: map[int]*string{2: ptr(tricky)}, Old: map[int]Cell{2: rows[0].Cells[2]}},
		{Key: rows[1].Key, Set: map[int]*string{2: ptr("linus@x.io"), 3: nil}, Old: map[int]Cell{2: rows[1].Cells[2], 3: rows[1].Cells[3]}},
		{Key: rows[2].Key, Del: true},
		{Set: map[int]*string{1: ptr("Barbara")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	after, total := mustPage(t, users, "", "")
	want := [][]string{{"1", "Ada", tricky, "36"}, {"2", "Linus", "linus@x.io", "<null>"}, {"3", "Barbara", "<null>", "18"}} // the rowid of the row deleted before it: no AUTOINCREMENT

	if got := texts(after, 4); total != 3 || !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("after apply: %q, want %q", got, want)
	}

	if _, err := os.Stat(filepath.Join(filepath.Dir(d.Target), "pwned")); err == nil {
		t.Fatal("a value ran a dot command")
	}
}

func TestApplyConflict(t *testing.T) {
	d := mustDB(t, schema)
	users := mustOpen(t, d, "", "users")
	rows, _ := mustPage(t, users, "", "")

	if _, err := d.run(t.Context(), true, "update users set email = 'changed' where id = 2;"); err != nil { // someone else, after the page was read
		t.Fatal(err)
	}

	err := users.Apply([]Change{
		{Key: rows[0].Key, Set: map[int]*string{1: ptr("Augusta")}, Old: map[int]Cell{1: rows[0].Cells[1]}},
		{Key: rows[1].Key, Set: map[int]*string{2: ptr("mine")}, Old: map[int]Cell{2: rows[1].Cells[2]}},
	})

	var ae *ApplyError
	if !errors.As(err, &ae) || ae.Index != 1 || !ae.Conflict {
		t.Fatalf("conflict: %#v", err)
	}

	if after, _ := mustPage(t, users, "", ""); after[0].Cells[1].Text != "Ada" {
		t.Fatal("the change before the conflict was written")
	}

	err = users.Apply([]Change{{Key: rows[0].Key, Del: true}, {Set: map[int]*string{2: ptr("no name")}}})
	if !errors.As(err, &ae) || ae.Index != 1 || ae.Conflict || !strings.Contains(ae.Msg, "NOT NULL") {
		t.Fatalf("constraint: %#v", err)
	}

	if _, total := mustPage(t, users, "", ""); total != 3 {
		t.Fatal("the deletion before the failed insert was written")
	}

	if err := mustOpen(t, d, "", "adults").Apply(nil); err == nil {
		t.Fatal("a view takes changes")
	}
}

func TestApplyWithoutRowid(t *testing.T) {
	d := mustDB(t, schema)
	kv := mustOpen(t, d, "", "kv")
	rows, _ := mustPage(t, kv, "", "")

	if err := kv.Apply([]Change{{Key: rows[1].Key, Set: map[int]*string{1: ptr("two")}, Old: map[int]Cell{1: rows[1].Cells[1]}}}); err != nil {
		t.Fatal(err)
	}

	if after, _ := mustPage(t, kv, "k = 'b'", ""); len(after) != 1 || after[0].Cells[1].Text != "two" {
		t.Fatalf("without rowid: %+v", after)
	}
}

// TestApplyForeignKeys keeps a SQLite file's foreign keys as the app that
// owns it does: sqlite3 starts with them off.
func TestApplyForeignKeys(t *testing.T) {
	d := mustDB(t, `create table authors(id integer primary key, name text);
create table posts(id integer primary key, author integer not null references authors(id) on delete cascade);
insert into authors values (1, 'ada'); insert into posts values (1, 1), (2, 1);`)
	authors, posts := mustOpen(t, d, "", "authors"), mustOpen(t, d, "", "posts")
	ar, _ := mustPage(t, authors, "", "")

	if err := authors.Apply([]Change{{Key: ar[0].Key, Del: true}}); err != nil {
		t.Fatal(err)
	}

	if pr, _ := mustPage(t, posts, "", ""); len(pr) != 0 {
		t.Fatalf("the deletion did not cascade: %q", texts(pr, 2))
	}

	if err := posts.Apply([]Change{{Set: map[int]*string{1: ptr("999")}}}); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("an orphan: %v", err)
	}
}

// TestApplyKeyTypes finds a row by a key of no type affinity, which holds
// 1, '1' and a blob apart, and checks an old value byte by byte under a
// NOCASE column.
func TestApplyKeyTypes(t *testing.T) {
	d := mustDB(t, `create table t(k primary key, v text) without rowid;
insert into t values (1, 'int one'), ('1', 'text one'), (x'00ff', 'blob'), ('a'||char(13,10)||'b', 'crlf'), ('a'||char(10)||'b', 'lf'),
	('n'||char(0)||'ul', 'nul'), ('n', 'n');
create table c(id integer primary key, name text collate nocase);
insert into c values (1, 'ABC');`)
	tb := mustOpen(t, d, "", "t")
	rows, _ := mustPage(t, tb, "", "v") // blob, crlf, int one, lf, n, nul, text one
	long := strings.Repeat("x\r\n", 600) + "\x00y"

	err := tb.Apply([]Change{
		{Key: rows[2].Key, Del: true},
		{Key: rows[0].Key, Set: map[int]*string{1: ptr("edited")}, Old: map[int]Cell{1: rows[0].Cells[1]}},
		{Key: rows[1].Key, Del: true}, // a\r\nb: sqlite3 would read the \r\n of its script as \n, and take a\nb
		{Key: rows[3].Key, Set: map[int]*string{1: &long}, Old: map[int]Cell{1: rows[3].Cells[1]}},
		{Key: rows[5].Key, Del: true}, // n\0ul: quote() would cut it to n, another row's key
	})
	if after, _ := mustPage(t, tb, "", "v"); err != nil || !slices.EqualFunc(texts(after, 2), [][]string{{"x'00FF'", "edited"}, {"n", "n"}, {"1", "text one"}, {"a\nb", long}}, slices.Equal) {
		t.Fatalf("keys of no affinity: %v %q", err, texts(after, 2))
	}

	// A REAL reads to the digits that read back as itself.
	if out, err := d.run(t.Context(), true, "create table r(id integer primary key, x real); insert into r values (1, 0.1 + 0.2), (2, 0.1);"); err != nil {
		t.Fatal(out, err)
	}

	if rr, _ := mustPage(t, mustOpen(t, d, "", "r"), "", ""); rr[0].Cells[1].Text != "0.30000000000000004" || rr[1].Cells[1].Text != "0.1" {
		t.Fatalf("reals: %q", texts(rr, 2))
	}

	c := mustOpen(t, d, "", "c")
	r, _ := mustPage(t, c, "", "")

	if _, err := d.run(t.Context(), true, "update c set name = 'abc';"); err != nil {
		t.Fatal(err)
	}

	var ae *ApplyError
	if err := c.Apply([]Change{{Key: r[0].Key, Set: map[int]*string{1: ptr("x")}, Old: map[int]Cell{1: r[0].Cells[1]}}}); !errors.As(err, &ae) || !ae.Conflict {
		t.Fatalf("a change of case under NOCASE: %v", err)
	}
}

// servers are the Postgres and MariaDB databases PANDO_TEST_PG and
// PANDO_TEST_MARIADB point at, each made ready for the test, whose tables in
// it are dropped first. Without them, or their clients, there are none.
func servers(t *testing.T) []DB {
	t.Helper()

	setup := map[string]string{
		Postgres: `drop schema if exists audit cascade; drop view if exists adults; drop materialized view if exists mv; drop table if exists users, nopk;
create table users(id serial primary key, name text not null default 'anon', email text, age int default 18, avatar bytea,
	slug text generated always as (lower(name)) stored);
create table nopk(a int, b text);
create view adults as select name from users where age >= 50;
create schema audit; create table audit.events(id int primary key, what text);
insert into users(name, email, age, avatar) values ('Ada', 'ada@x.io', 36, '\x00ff10'), ('Linus', null, 54, null), ('Grace', '', 85, null);
insert into nopk values (1, 'x');`,
		MariaDB: "drop view if exists adults; drop table if exists users, nopk;\n" +
			"create table users(id int auto_increment primary key, name varchar(100) not null default 'anon', email varchar(100), age int default 18, avatar blob,\n" +
			"\tslug varchar(100) as (lower(name)) virtual);\n" +
			"create table nopk(a int, b text);\n" +
			"create view adults as select name from users where age >= 50;\n" +
			"insert into users(name, email, age, avatar) values ('Ada', 'ada@x.io', 36, x'00ff10'), ('Linus', null, 54, null), ('Grace', '', 85, null);\n" +
			"insert into nopk values (1, 'x');",
	}

	var out []DB

	for kind, env := range map[string]string{Postgres: "PANDO_TEST_PG", MariaDB: "PANDO_TEST_MARIADB"} {
		raw := os.Getenv(env)
		if raw == "" || !Has(kind) {
			continue
		}

		d, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}

		if _, err := d.run(t.Context(), true, setup[kind]+"\n"); err != nil {
			t.Fatalf("%s: set up: %v", env, err)
		}

		out = append(out, d)
	}

	if len(out) == 0 {
		t.Skip("PANDO_TEST_PG and PANDO_TEST_MARIADB are not set")
	}

	return out
}

func TestServers(t *testing.T) {
	for _, d := range servers(t) {
		t.Run(d.Kind, func(t *testing.T) { testServer(t, d) })
	}
}

func testServer(t *testing.T, d DB) {
	t.Helper()

	objs, err := d.Objects()

	var names []string

	want := []string{"adults (view)", "nopk", "users"}
	if d.Kind == Postgres {
		want = append(want, "audit.events")
	}

	for _, o := range objs { // the ones this test made: other tests share the server
		if n := map[bool]string{true: o.Schema + ".", false: ""}[o.Schema != ""] + o.Name + map[bool]string{true: " (view)"}[o.View]; slices.Contains(want, n) {
			names = append(names, n)
		}
	}

	if err != nil || !slices.Equal(names, want) {
		t.Fatalf("objects: %v %q, want %q", err, names, want)
	}

	users := mustOpen(t, d, "", "users")
	if !slices.Equal(users.Keys, []string{"id"}) || len(users.Cols) != 6 || !users.Cols[4].Blob || !users.Cols[5].Generated {
		t.Fatalf("users: %+v", users)
	}

	keys := map[string][]string{Postgres: {"tableoid", "ctid", "xmin"}, MariaDB: nil}[d.Kind]
	if nopk := mustOpen(t, d, "", "nopk"); !slices.Equal(nopk.Keys, keys) {
		t.Fatalf("a table without a primary key: %q, want %q", nopk.Keys, keys)
	}

	if adults := mustOpen(t, d, "", "adults"); !adults.ReadOnly() {
		t.Fatal("a view takes edits")
	}

	rows, total := mustPage(t, users, "", "")
	if got := texts(rows, 5); total != 3 || !slices.Equal(got[1], []string{"2", "Linus", "<null>", "54", "<null>"}) ||
		got[2][2] != "" || rows[0].Cells[4].Text != "x'00FF10'" || !rows[0].Cells[4].Blob || rows[1].Key != `["2"]` {
		t.Fatalf("page: %d %q, key %s", total, got, rows[1].Key)
	}

	// A read is read-only whatever this filter says, and whatever options= a
	// Postgres URL brings: it would override PGOPTIONS.
	ro := d
	if d.Kind == Postgres {
		ro.Target += ifElse(strings.Contains(d.Target, "?"), "&", "?") + "options=-c%20default_transaction_read_only%3Doff"
	}

	_, err = mustOpen(t, ro, "", "users").Page(t.Context(), "true) and (true); delete from users; select 1 where (true", "", 0, 10)
	if _, total := mustPage(t, users, "", ""); err == nil || !strings.Contains(strings.ToLower(err.Error()), "read") || total != 3 {
		t.Fatalf("a page writes: %v, %d rows left", err, total)
	}

	tricky := "it's \\ a\n;-- \"q\""

	err = users.Apply([]Change{
		{Key: rows[0].Key, Set: map[int]*string{2: ptr(tricky)}, Old: map[int]Cell{2: rows[0].Cells[2]}},
		{Key: rows[1].Key, Set: map[int]*string{3: nil}, Old: map[int]Cell{3: rows[1].Cells[3]}},
		{Key: rows[2].Key, Del: true},
		{Set: map[int]*string{1: ptr("Barbara")}},
		{},
	})
	if err != nil {
		t.Fatal(err)
	}

	after, total := mustPage(t, users, "", "")
	if got := texts(after, 4); total != 4 || got[0][2] != tricky || got[1][3] != "<null>" || got[2][1] != "Barbara" || got[2][3] != "18" || got[3][1] != "anon" {
		t.Fatalf("after apply: %d %q", total, got)
	}

	// A value set to what it holds, written another way, is no conflict:
	// MariaDB's row_count() counts no row changed.
	if err := users.Apply([]Change{{Key: after[0].Key, Set: map[int]*string{3: ptr("0" + after[0].Cells[3].Text)}, Old: map[int]Cell{3: after[0].Cells[3]}}}); err != nil {
		t.Fatalf("the same value: %v", err)
	}

	if _, err := d.run(t.Context(), true, "update users set email = 'theirs' where id = 2;\n"); err != nil {
		t.Fatal(err)
	}

	err = users.Apply([]Change{{Key: after[1].Key, Set: map[int]*string{2: ptr("mine")}, Old: map[int]Cell{2: after[1].Cells[2]}}})

	var ae *ApplyError
	if !errors.As(err, &ae) || ae.Index != 0 || !ae.Conflict {
		t.Fatalf("conflict: %#v", err)
	}

	if d.Kind == Postgres {
		testPostgres(t, d)
	}

	// A big table shown unfiltered is counted by the server's estimate,
	// until Count counts it.
	fill := map[string]string{
		Postgres: "insert into big select generate_series(1, 100001); analyze big;",
		MariaDB:  "insert into big select seq from seq_1_to_100001; analyze table big;",
	}[d.Kind]
	if _, err := d.run(t.Context(), true, "drop table if exists big; create table big(n int primary key); "+fill+"\n"); err != nil {
		t.Fatal(err)
	}

	big := mustOpen(t, d, "", "big")

	p, err := big.Page(t.Context(), "", "", 0, 10)
	if err != nil || len(p.Rows) != 10 || !p.More || p.Total < 50_000 || d.Kind == Postgres && p.Exact { // InnoDB's guess may fall short of bigTable
		t.Fatalf("a big table: %v %d rows, %+v", err, len(p.Rows), p.Total)
	}

	if n, err := big.Count(t.Context(), ""); err != nil || n != 100001 {
		t.Fatalf("count: %v %d", err, n)
	}

	// Postgres builds a JSON array of 100 values at most: a wider row joins
	// arrays of 100.
	cols := make([]string, 120)
	for k := range cols {
		cols[k] = fmt.Sprintf("c%d int default %d", k, k)
	}

	if _, err := d.run(t.Context(), true, "drop table if exists wide; create table wide("+strings.Join(cols, ", ")+"); insert into wide(c0) values (0);\n"); err != nil {
		t.Fatal(err)
	}

	if r, _ := mustPage(t, mustOpen(t, d, "", "wide"), "", ""); len(r) != 1 || len(r[0].Cells) != 120 || r[0].Cells[119].Text != "119" {
		t.Fatalf("a wide table: %+v", r)
	}

	// A binary key is matched byte by byte: as text, MariaDB's turns the
	// bytes that are not UTF-8 into ?, another row's key. NUL is one too.
	bin := map[string]string{
		Postgres: `drop table if exists k; create table k(id bytea primary key, v text);
insert into k values ('\x41ff42', 'target'), ('\x413f42', 'bystander'), ('\x0011', 'nul');`,
		MariaDB: "drop table if exists k; create table k(id varbinary(8) primary key, v varchar(20));\n" +
			"insert into k values (unhex('41FF42'), 'target'), (unhex('413F42'), 'bystander'), (unhex('0011'), 'nul');",
	}[d.Kind]
	if _, err := d.run(t.Context(), true, bin+"\n"); err != nil {
		t.Fatal(err)
	}

	k := mustOpen(t, d, "", "k")
	kr, _ := mustPage(t, k, "", "")
	byV := map[string]Row{}

	for _, r := range kr {
		byV[r.Cells[1].Text] = r
	}

	err = k.Apply([]Change{
		{Key: byV["target"].Key, Del: true},
		{Key: byV["nul"].Key, Set: map[int]*string{1: ptr("edited")}, Old: map[int]Cell{1: byV["nul"].Cells[1]}},
	})
	if after, _ := mustPage(t, k, "", "v"); err != nil || len(after) != 2 || after[0].Cells[1].Text != "bystander" || after[1].Cells[1].Text != "edited" {
		t.Fatalf("binary keys: %v %q", err, texts(after, 2))
	}

	if d.Kind == Postgres { // a partition's ctid repeats another's: tableoid tells them apart
		if _, err := d.run(t.Context(), true, "drop table if exists parts; create table parts(a int, v text) partition by list (a);\n"+
			"create table parts_1 partition of parts for values in (1); create table parts_2 partition of parts for values in (2);\n"+
			"insert into parts values (1, 'one'), (2, 'two');\n"); err != nil {
			t.Fatal(err)
		}

		parts := mustOpen(t, d, "", "parts")
		pr, _ := mustPage(t, parts, "", "v")

		if err := parts.Apply([]Change{{Key: pr[1].Key, Del: true}}); err != nil || pr[0].Key == pr[1].Key {
			t.Fatalf("a partition's row: %v, keys %s %s", err, pr[0].Key, pr[1].Key)
		}
	}

	if d.Kind == MariaDB {
		testMariaDB(t, d)
	}

	// A materialized view is listed, read only: information_schema.tables leaves it out.
	if d.Kind == Postgres {
		if _, err := d.run(t.Context(), true, "drop materialized view if exists mv; create materialized view mv as select id, name from users;\n"); err != nil {
			t.Fatal(err)
		}

		objs, err := d.Objects()
		if i := slices.IndexFunc(objs, func(o Object) bool { return o.Name == "mv" }); err != nil || i < 0 || !objs[i].View {
			t.Fatalf("a materialized view: %v %+v", err, objs)
		}
	}

	// A line break of \r\n stays as written.
	if err := users.Apply([]Change{{Key: `["1"]`, Set: map[int]*string{2: ptr("a\r\nb")}}}); err != nil {
		t.Fatal(err)
	}

	if r, _ := mustPage(t, users, "id = 1", ""); r[0].Cells[2].Text != "a\r\nb" {
		t.Fatalf("\\r\\n: %q", r[0].Cells[2].Text)
	}

	if d.Kind == MariaDB { // under the C locale the client would pick latin1, and write é as Ã©
		t.Setenv("LC_ALL", "C")

		if err := users.Apply([]Change{{Key: `["1"]`, Set: map[int]*string{1: ptr("é")}}}); err != nil {
			t.Fatal(err)
		}

		if out, err := d.run(t.Context(), false, "select hex(name) from users where id = 1;\n"); err != nil || strings.TrimSpace(out) != "C3A9" {
			t.Fatalf("é went in as %s: %v", out, err)
		}
	}

	// Stopped, the client stops its query on the server too.
	ctx, stop := context.WithCancel(t.Context())
	time.AfterFunc(300*time.Millisecond, stop)

	if _, err := d.run(ctx, false, map[string]string{Postgres: "select pg_sleep(30);", MariaDB: "select sleep(30);"}[d.Kind]+"\n"); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped: %v", err)
	}

	running := map[string]string{
		Postgres: "select count(*) from pg_stat_activity where query like '%pg_sleep(30)%' and state = 'active' and pid <> pg_backend_pid();",
		MariaDB:  "select count(*) from information_schema.processlist where info like '%sleep(30)%' and id <> connection_id();",
	}[d.Kind]
	for n := 0; ; n++ {
		out, err := d.run(t.Context(), false, running+"\n")
		if err == nil && strings.TrimSpace(out) == "0" {
			break
		}

		if n == 20 {
			t.Fatalf("the query runs on: %v %s", err, out)
		}

		time.Sleep(100 * time.Millisecond)
	}

	wrong := d
	wrong.Password = "wrong"

	if _, err := wrong.Objects(); !errors.Is(err, ErrPassword) {
		t.Fatalf("a wrong password: %v", err)
	}
}

// testPostgres finds a row of a table without a primary key by its ctid,
// and writes a table of a schema not the connection's.
func testPostgres(t *testing.T, d DB) {
	t.Helper()

	nopk := mustOpen(t, d, "", "nopk")
	r, _ := mustPage(t, nopk, "", "")

	if err := nopk.Apply([]Change{{Key: r[0].Key, Set: map[int]*string{1: ptr("y")}, Old: map[int]Cell{1: r[0].Cells[1]}}}); err != nil {
		t.Fatal(err)
	}

	// Changed since in another column, the row is not the one read: its xmin moved.
	r, _ = mustPage(t, nopk, "", "")
	if _, err := d.run(t.Context(), true, "update nopk set a = 2;\n"); err != nil {
		t.Fatal(err)
	}

	var ae *ApplyError
	if err := nopk.Apply([]Change{{Key: r[0].Key, Del: true}}); !errors.As(err, &ae) || !ae.Conflict {
		t.Fatalf("a row changed since: %v", err)
	}

	// quote leaves a \ be, as standard_conforming_strings reads it: off,
	// 'x\y' would be 'xy', another row's key.
	if _, err := d.run(t.Context(), true, `drop table if exists bs; create table bs(id text primary key, v text); insert into bs values ('x\y', 'target'), ('xy', 'bystander');`+"\n"); err != nil {
		t.Fatal(err)
	}

	off := d
	off.Target += ifElse(strings.Contains(d.Target, "?"), "&", "?") + "options=-c%20standard_conforming_strings%3Doff"
	bs := mustOpen(t, off, "", "bs")
	br, _ := mustPage(t, bs, "", "v")

	if err := bs.Apply([]Change{{Key: br[1].Key, Del: true}}); err != nil {
		t.Fatal(err)
	}

	left, _ := mustPage(t, bs, "", "")
	if len(left) != 1 || left[0].Cells[1].Text != "bystander" {
		t.Fatalf("deleted another row: %q", texts(left, 2))
	}

	// extra_float_digits 0 prints 0.1 + 0.2 as 0.3: two float keys as one.
	if _, err := d.run(t.Context(), true, "drop table if exists fk; create table fk(k float8 primary key, v text); insert into fk values (0.3, 'a'), (0.1::float8 + 0.2::float8, 'b');\n"); err != nil {
		t.Fatal(err)
	}

	few := d
	few.Target += ifElse(strings.Contains(d.Target, "?"), "&", "?") + "options=-c%20extra_float_digits%3D0"
	fk := mustOpen(t, few, "", "fk")
	fr, _ := mustPage(t, fk, "", "v")

	if err := fk.Apply([]Change{{Key: fr[1].Key, Del: true}}); err != nil {
		t.Fatal(err)
	}

	if kept, _ := mustPage(t, fk, "", ""); len(kept) != 1 || kept[0].Cells[1].Text != "a" {
		t.Fatalf("deleted another row: %q", texts(kept, 2))
	}

	// PGCLIENTENCODING picks another encoding than the UTF-8 pando talks.
	t.Setenv("PGCLIENTENCODING", "LATIN2")

	if err := bs.Apply([]Change{{Key: left[0].Key, Set: map[int]*string{1: ptr("žluťoučký kůň")}, Old: map[int]Cell{1: left[0].Cells[1]}}}); err != nil {
		t.Fatal(err)
	}

	if out, err := d.run(t.Context(), false, "select length(v) from bs;\n"); err != nil || strings.TrimSpace(out) != "13" {
		t.Fatalf("written as %s characters: %v", out, err)
	}

	events := mustOpen(t, d, "audit", "events")
	if err := events.Apply([]Change{{Set: map[int]*string{0: ptr("1"), 1: ptr("logged")}}}); err != nil {
		t.Fatal(err)
	}

	if r, _ := mustPage(t, events, "", ""); len(r) != 1 || r[0].Cells[1].Text != "logged" {
		t.Fatalf("a schema's table: %+v", r)
	}
}

// testMariaDB keeps apart FLOAT keys that read alike at 6 digits, shows a
// BIT as bytes, and writes NUL.
func testMariaDB(t *testing.T, d DB) {
	t.Helper()

	if _, err := d.run(t.Context(), true, "drop table if exists f, u;\n"+
		"create table f(k float primary key, v text); insert into f values (1, 'one'), (1.0000001, 'other');\n"+
		"create table u(id int primary key, active bit(1), v text); insert into u values (1, 1, 'x');\n"); err != nil {
		t.Fatal(err)
	}

	f := mustOpen(t, d, "", "f")
	fr, _ := mustPage(t, f, "", "v")

	if err := f.Apply([]Change{{Key: fr[1].Key, Del: true}}); err != nil || fr[0].Key == fr[1].Key {
		t.Fatalf("float keys: %v, %s %s", err, fr[0].Key, fr[1].Key)
	}

	if left, _ := mustPage(t, f, "", ""); len(left) != 1 || left[0].Cells[1].Text != "one" {
		t.Fatalf("deleted another row: %q", texts(left, 2))
	}

	// A geometry reads as its bytes: as char it fails the whole page.
	if _, err := d.run(t.Context(), true, "drop table if exists g; create table g(id int primary key, loc point, n varchar(10)); insert into g values (1, point(1, 2), 'a');\n"); err != nil {
		t.Fatal(err)
	}

	if gr, _ := mustPage(t, mustOpen(t, d, "", "g"), "", ""); !gr[0].Cells[1].Blob || gr[0].Cells[2].Text != "a" {
		t.Fatalf("a geometry: %+v", gr)
	}

	u := mustOpen(t, d, "", "u")
	ur, _ := mustPage(t, u, "", "")

	if !u.Cols[1].Blob || ur[0].Cells[1].Text != "x'01'" {
		t.Fatalf("a bit: %+v %+v", u.Cols[1], ur[0].Cells[1])
	}

	if err := u.Apply([]Change{{Key: ur[0].Key, Set: map[int]*string{2: ptr("a\x00b")}, Old: map[int]Cell{2: ur[0].Cells[2]}}}); err != nil {
		t.Fatal(err)
	}

	if ur, _ = mustPage(t, u, "", ""); ur[0].Cells[2].Text != "a\x00b" {
		t.Fatalf("NUL: %q", ur[0].Cells[2].Text)
	}

	if err := u.Apply([]Change{{Key: ur[0].Key, Set: map[int]*string{2: ptr("y")}, Old: map[int]Cell{2: ur[0].Cells[2]}}}); err != nil {
		t.Fatalf("an old value with NUL: %v", err)
	}
}

func FuzzParseRows(f *testing.F) {
	f.Add("Ada", "", true)
	f.Add("line\n\"q\"\\", "NULL", false)
	f.Add("\x00\x1f ž", "{\"blob\":1}", true)
	// Rows a client prints, a key and text or NULL values, read back as written.
	f.Fuzz(func(t *testing.T, a, b string, null bool) {
		if !utf8.ValidString(a) || !utf8.ValidString(b) { // the clients' JSON is UTF-8
			t.Skip()
		}

		var second any = b
		if null {
			second = nil
		}

		line, err := json.Marshal([]any{[]string{"7"}, a, second})
		if err != nil {
			t.Skip()
		}

		rows, err := parseRows([]string{string(line)}, 2)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}

		want := Row{Key: `["7"]`, Cells: []Cell{{Text: a}, {Text: b, Null: null}}}
		if null {
			want.Cells[1].Text = ""
		}

		if got := rows[0]; got.Key != want.Key || !slices.Equal(got.Cells, want.Cells) {
			t.Fatalf("%s: %+v, want %+v", line, got, want)
		}
	})
}

// TestKeyring keeps, reads back and drops a password in the system keyring,
// where PANDO_TEST_KEYRING says the tests may write to it.
func TestKeyring(t *testing.T) {
	if os.Getenv("PANDO_TEST_KEYRING") == "" || !HasKeyring() {
		t.Skip("PANDO_TEST_KEYRING is not set")
	}

	name := "pando-test-" + t.Name()
	t.Cleanup(func() { ForgetPassword(name) })

	if err := SavePassword(name, "s3cret 'pw'"); err != nil {
		t.Fatal(err)
	}

	keyring.Delete(name) // past the cache, from the keyring itself

	if got := LoadPassword(name); got != "s3cret 'pw'" {
		t.Fatalf("read back %q", got)
	}

	ForgetPassword(name)
	keyring.Delete(name)

	if got := LoadPassword(name); got != "" {
		t.Fatalf("still kept: %q", got)
	}
}
