package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/db"
)

// sqlite runs sql on the database at path and returns what it printed, or
// fails the test.
func sqlite(t *testing.T, path, sql string) string {
	t.Helper()

	out, err := exec.Command("sqlite3", path, sql).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3 %q: %v %s", sql, err, out)
	}

	return strings.TrimSuffix(string(out), "\n")
}

// dbModel is a test model whose worktree, a repository, holds app.db: 450
// orders, the third with a line break in its note, and a view. sqlite3 is
// back for the Databases view; the test skips where it is not installed.
func dbModel(t *testing.T) (*Model, string) {
	t.Helper()

	if !db.Has(db.SQLite) {
		t.Skip("sqlite3 is not installed")
	}

	m := testModelSized(t, 120, 30)
	m.hasDB = true
	mustGit(t, m.ws, "init", "-q")
	mustWrite(t, filepath.Join(m.ws, "Thumbs.db"), "not a database")

	path := filepath.Join(m.ws, "app.db")
	sqlite(t, path, `create table orders(id integer primary key, note text, total real);
		with recursive s(v) as (select 1 union all select v + 1 from s where v < 450) insert into orders(note, total) select 'note ' || v, v from s;
		update orders set note = 'one' || char(10) || 'two' where id = 3;
		create view big as select * from orders where total > 400;`)

	return m, path
}

// dbDo runs cmd, which reads or writes databases, and hands the answers back
// the way the program would, with what they start in turn. Only commands of
// the Databases view and the tables go through it: anything else could
// reach a daemon or sleep on a timer.
func dbDo(m *Model, cmd tea.Cmd) {
	for _, msg := range runAll(cmd) {
		if msg == nil {
			continue
		}

		_, next := m.Update(msg)

		if _, ok := msg.(tableDoneMsg); ok { // it reads the page again
			dbDo(m, next)
		}
	}
}

// dbKey presses k, whose command reads or writes a table, and runs it.
func dbKey(m *Model, k string) {
	_, cmd := m.Update(keyMsg(k))
	dbDo(m, cmd)
}

// typeText types s one key at a time.
func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// openOrders opens the orders table with the keyboard on it.
func openOrders(t *testing.T) (*Model, string, *table) {
	t.Helper()

	m, path := dbModel(t)
	dbDo(m, m.openTable(path, db.Object{Name: "orders"}, false))
	m.focus = onMain

	if m.pv.tbl == nil || !m.pv.tbl.ready {
		t.Fatalf("orders did not load: %+v", m.pv.tbl)
	}

	return m, path, m.pv.tbl
}

func TestDatabasesView(t *testing.T) {
	m, path := dbModel(t)
	dbDo(m, m.showView(viewDB))

	if rows := m.db.rows(m); len(rows) != 1 || rows[0].src.id != path { // Thumbs.db is no database
		t.Fatalf("files: %+v", rows)
	}

	press(m, "j")
	dbKey(m, "enter")

	var names []string

	for _, r := range m.db.rows(m) {
		if r.obj != nil {
			names = append(names, r.obj.Name+map[bool]string{true: " (view)"}[r.obj.View])
		}
	}

	if !slices.Equal(names, []string{"big (view)", "orders"}) {
		t.Fatalf("objects: %q", names)
	}
	// The tables hang off their database's chevron, its guide lit while it
	// is selected.
	if lead, lit := treeLead(t, m.db.lines(m, 40, 10), "orders"); !strings.HasPrefix(lead, " │ ") || strings.Count(lead, "│") != 1 || lit != 0 {
		t.Fatalf("orders: %q lit %d", lead, lit)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "DATABASES") || !strings.Contains(out, "orders") {
		t.Fatalf("view:\n%s", out)
	}

	press(m, "ctrl+f", "o", "r", "d")

	if rows := m.db.rows(m); len(rows) != 2 || rows[1].obj == nil || rows[1].obj.Name != "orders" {
		t.Fatalf("filtered: %+v", rows)
	}
}

func TestExplorerOpensDatabase(t *testing.T) {
	m, path := dbModel(t)

	dbDo(m, m.openFile(path, true))

	if !m.shown(viewDB) || !m.db.open[path] || m.db.selected(m).src.id != path || m.pv.kind != "" {
		t.Fatalf("app.db: shown %v, open %v, preview %q", m.shown(viewDB), m.db.open, m.pv.kind)
	}

	m.openFile(filepath.Join(m.ws, "Thumbs.db"), true)

	if m.pv.kind != pvFile {
		t.Fatalf("a .db that is no database opens as a file: %q", m.pv.kind)
	}
}

func TestTableGrid(t *testing.T) {
	m, _, tb := openOrders(t)

	out := checkWidths(t, m)
	if tb.total != 450 || len(tb.rows) != tablePage || !strings.Contains(out, "1–200 of 450") || !strings.Contains(out, "one↵two") ||
		!strings.Contains(out, "id INTEGER · primary key") {
		t.Fatalf("first page, %d of %d:\n%s", len(tb.rows), tb.total, out)
	}

	dbKey(m, "]")

	if tb.offset != tablePage || tb.rows[0].Cells[1].Text != "note 201" {
		t.Fatalf("next page: offset %d, %+v", tb.offset, tb.rows[0])
	}

	dbKey(m, "s")
	dbKey(m, "s")

	if out := checkWidths(t, m); tb.applied[1] != `"id" desc` || tb.offset != 0 || tb.rows[0].Cells[0].Text != "450" || !strings.Contains(out, "id ↓") {
		t.Fatalf("sorted down: %q, first %+v", tb.applied[1], tb.rows[0])
	}

	press(m, "ctrl+f")
	typeText(m, "total > 440")
	dbKey(m, "enter")

	if tb.total != 10 || tb.box != 0 {
		t.Fatalf("where: %d rows, box %d", tb.total, tb.box)
	}

	press(m, "ctrl+f", "ctrl+u")
	typeText(m, "bogus = 1")
	dbKey(m, "enter")

	if tb.err != "no such column: bogus" || len(tb.rows) != 10 {
		t.Fatalf("a bad where: %q, %d rows kept", tb.err, len(tb.rows))
	}

	press(m, "ctrl+f", "ctrl+u", "esc")

	if tb.where.Value() != "bogus = 1" || tb.box != 0 {
		t.Fatalf("esc puts the box back: %q", tb.where.Value())
	}
}

func TestTableEstimateAndStop(t *testing.T) {
	m, _, tb := openOrders(t)

	// A big table's total is the server's estimate until Count Rows counts it.
	m.Update(tableMsg{tb: tb, gen: tb.gen, page: db.Page{Rows: tb.rows, Total: 123456, More: true}})

	if out := checkWidths(t, m); !strings.Contains(out, "1–200 of ~123456") {
		t.Fatalf("an estimate:\n%s", out)
	}

	i := slices.IndexFunc(tb.items(), func(it item) bool { return it.label == "Count Rows" })
	if i < 0 {
		t.Fatal("no Count Rows in the menu")
	}

	dbDo(m, tb.items()[i].run(m))

	if !tb.exact || tb.total != 450 || slices.ContainsFunc(tb.items(), func(it item) bool { return it.label == "Count Rows" }) {
		t.Fatalf("counted: %d, exact %v", tb.total, tb.exact)
	}

	// esc stops a read before it closes the tab: the page on screen stays.
	cmd := tb.page(1)

	if out := checkWidths(t, m); !strings.Contains(out, "loading… esc stops") {
		t.Fatalf("a read running:\n%s", out)
	}

	press(m, "esc")
	dbDo(m, cmd)

	if m.pv.tbl != tb || tb.busy || tb.offset != 0 || tb.rows[0].Cells[0].Text != "1" || tb.err != "" {
		t.Fatalf("stopped: tab %v, busy %v, offset %d, err %q", m.pv.tbl == tb, tb.busy, tb.offset, tb.err)
	}

	dbKey(m, "]")
	dbKey(m, "]")
	dbKey(m, "]") // past the last page: nothing to read

	if tb.offset != 2*tablePage || tb.more || len(tb.rows) != 50 || tb.busy {
		t.Fatalf("the last page: offset %d, %d rows", tb.offset, len(tb.rows))
	}
}

func TestTableEdits(t *testing.T) {
	m, path, tb := openOrders(t)

	press(m, "l", "enter") // note of row 1

	if m.modal == nil {
		t.Fatal("no prompt for the cell")
	}

	press(m, "ctrl+u")
	typeText(m, "it's  spaced ")
	press(m, "enter", "j", "d", "o", "enter")
	typeText(m, "new")
	press(m, "enter", "g", "j", "j", "j", "right") // row 4, total

	for _, it := range tb.items() {
		if it.label == "Set NULL" {
			it.run(m)
		}
	}

	lay := tb.layout(100)
	for i, want := range map[int]string{0: " M", 1: " D", 3: " M", len(tb.rows): " A"} {
		if got := ansi.Strip(tb.rowLine(m, lay, i)); !strings.HasPrefix(got, want) {
			t.Errorf("row %d: %q, want it marked%s", i, got, want)
		}
	}

	if out := checkWidths(t, m); len(tb.edits) != 4 || !strings.Contains(out, "4 pending") {
		t.Fatalf("%d edits:\n%s", len(tb.edits), out)
	}

	dbKey(m, "ctrl+s")

	if m.msg != "submitted 4 changes" || len(tb.edits) != 0 {
		t.Fatalf("submit: %q, %d left", m.msg, len(tb.edits))
	}

	got := sqlite(t, path, "select '[' || note || ']' from orders where id = 1; select count(*) from orders where id = 2; "+
		"select ifnull(total, 'null') from orders where id = 4; select note from orders where id = 451")
	if got != "[it's  spaced ]\n0\nnull\nnew" {
		t.Fatalf("in the database: %q", got)
	}
}

func TestTableConflict(t *testing.T) {
	m, path, tb := openOrders(t)

	press(m, "l", "enter", "ctrl+u")
	typeText(m, "mine")
	press(m, "enter")
	sqlite(t, path, "update orders set note = 'theirs' where id = 1")
	dbKey(m, "ctrl+s")

	if !strings.Contains(m.msg, "row 1: the row changed since it was read") || len(tb.edits) != 1 {
		t.Fatalf("conflict: %q, %d edits", m.msg, len(tb.edits))
	}

	if got := sqlite(t, path, "select note from orders where id = 1"); got != "theirs" {
		t.Fatalf("written over: %q", got)
	}
}

func TestTableCellEditor(t *testing.T) {
	m, _, tb := openOrders(t)

	press(m, "j", "j", "l", "enter") // row 3's note has a line break

	if m.pv.kind != pvFile || m.pv.cell == nil || m.pv.buf.text() != "one\ntwo" || m.pv.dirty() {
		t.Fatalf("cell editor: %q %+v", m.pv.kind, m.pv.cell)
	}

	for _, e := range m.groupSpec().Open {
		if strings.HasPrefix(e.Name, "orders.note") {
			t.Fatalf("a cell editor is kept with the editors: %+v", e)
		}
	}

	press(m, "ctrl+end", "!")
	m.confirmQuit() // no draft keeps the value: it is a table change, lost

	if strings.Contains(m.modal.title, "unsaved") || !strings.Contains(m.modal.title, "1 table change not submitted") {
		t.Fatalf("quitting with a value on its way: %q", m.modal.title)
	}

	press(m, "esc", "ctrl+s")

	if v, set := tb.value(2, 1); m.pv.kind != pvTable || !set || v.Text != "one\ntwo!" || len(m.editors) != 1 {
		t.Fatalf("back in the table: %q, %q set %v, %d tabs", m.pv.kind, v.Text, set, len(m.editors))
	}
}

// TestTableCellEditorOutlives keeps a long value's editor tab right when
// its table changed under it: a submit read the row again, a reload lost
// the column.
func TestTableCellEditorOutlives(t *testing.T) {
	m, path, tb := openOrders(t)
	cellTab := func() int { return slices.IndexFunc(m.editors, func(e preview) bool { return e.cell != nil }) }
	tableTab := func() int { return slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb }) }

	press(m, "j", "j", "l", "enter", "ctrl+end", "!", "ctrl+s") // row 3's note: one↵two!
	dbKey(m, "ctrl+s")                                          // submitted, read again

	press(m, "enter") // the editor again, on what the database has now
	m.showEditor(tableTab())
	press(m, "u") // nothing pending: a no-op
	m.showEditor(cellTab())
	press(m, "ctrl+end", "backspace", "ctrl+s") // back to one↵two: an edit, not "back to what was read"

	if v, set := tb.value(2, 1); !set || v.Text != "one\ntwo" || len(tb.edits) != 1 {
		t.Fatalf("after a submit: %q set %v, %d edits", v.Text, set, len(tb.edits))
	}

	tb.discard()
	press(m, "enter")
	m.showEditor(tableTab())
	sqlite(t, path, "alter table orders drop column note")
	dbKey(m, "r") // no edits: the columns are read again
	m.showEditor(cellTab())
	m.msg = ""
	press(m, "ctrl+end", "?")
	dbKey(m, "ctrl+s")

	if !strings.Contains(m.msg, "note is gone") || len(tb.edits) != 0 {
		t.Fatalf("a column gone: %q, %d edits", m.msg, len(tb.edits))
	}
}

// TestCloseDialogAfterSubmit answers a close dialog after a submit closed
// another tab: the dialog's tab is found again, not the one now at its place.
func TestCloseDialogAfterSubmit(t *testing.T) {
	m, _, tb := openOrders(t)

	press(m, "j", "j", "l", "enter", "ctrl+end", "!") // tab 1: row 3's note
	press(m, "ctrl+n", "x")                           // tab 2: an untitled file
	m.showEditor(0)
	press(m, "g", "d", "q")
	submit := m.modal.choose(m, slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Submit and close" })) // answered later

	cell := slices.IndexFunc(m.editors, func(e preview) bool { return e.cell != nil })
	m.closeEditor(cell)
	dbDo(m, submit) // the table's tab goes: the others move
	m.modal.choose(m, slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Close anyway" }))

	if slices.ContainsFunc(m.editors, func(e preview) bool { return e.cell != nil || e.tbl == tb }) || !slices.ContainsFunc(m.editors, func(e preview) bool { return e.untitled() && e.cell == nil && e.dirty() }) {
		t.Fatalf("closed the wrong tab: %d left", len(m.editors))
	}
}

// TestTableCellEditorKeyTaken saves a long value's editor whose row a submit
// deleted and whose key a row it added took: the value does not land there.
func TestTableCellEditorKeyTaken(t *testing.T) {
	m, path, tb := openOrders(t)
	sqlite(t, path, "update orders set note = 'old' || char(10) || 'row' where id = 450")

	table := func() { m.showEditor(slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb })) }

	press(m, "ctrl+f")
	typeText(m, "id >= 449")
	dbKey(m, "enter")
	press(m, "j", "l", "enter", "ctrl+end", "!") // row 450's note, typed in, not put back
	table()
	press(m, "d", "o")
	dbKey(m, "ctrl+s") // 450 goes, the row added takes its rowid

	m.showEditor(slices.IndexFunc(m.editors, func(e preview) bool { return e.cell != nil }))
	dbKey(m, "ctrl+s")
	table()

	m.msg = ""
	dbKey(m, "ctrl+s")

	if got := sqlite(t, path, "select quote(note) from orders where id = 450"); got != "NULL" || !strings.Contains(m.msg, "changed") {
		t.Fatalf("the row that took the key got %s (%q)", got, m.msg)
	}
}

// TestTableCellEditorKeepsBytes edits values the prompt's box would change:
// a tab, a \r\n, a last line break. Saved unchanged they are no edit, and
// changed they keep what the editor does not show.
func TestTableCellEditorKeepsBytes(t *testing.T) {
	m, path, tb := openOrders(t)
	sqlite(t, path, "update orders set note = 'a'||char(9)||'b' where id = 1; update orders set note = 'x'||char(13,10)||'y'||char(13,10) where id = 2; "+
		"update orders set note = 'm'||char(13,10)||'i'||char(10)||'x' where id = 3")
	dbKey(m, "r")

	table := func() { m.showEditor(slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb })) }

	press(m, "g", "l", "enter") // a⇥b

	if m.pv.cell == nil {
		t.Fatalf("a tab went to the prompt: %v", m.modal != nil)
	}

	press(m, "ctrl+s")
	table()
	press(m, "j", "enter", "ctrl+end", "!", "backspace", "ctrl+s") // x\r\ny\r\n, typed in and back
	table()
	press(m, "j", "enter", "ctrl+s") // m\r\ni\nx: line breaks of both kinds

	if len(tb.edits) != 0 {
		t.Fatalf("unchanged values made edits: %+v", tb.changes())
	}

	table()
	press(m, "k", "enter", "ctrl+home", "!", "ctrl+s")
	table()
	dbKey(m, "ctrl+s")

	if got := sqlite(t, path, "select hex(note) from orders where id = 2"); got != "2178"+"0D0A"+"79"+"0D0A" {
		t.Fatalf("written as %s", got)
	}
}

// TestTableNullLookedAt opens the prompt over a NULL and confirms it with
// nothing typed: still NULL. A U+FFFD, which the prompt's box drops, goes to
// an editor.
func TestTableNullLookedAt(t *testing.T) {
	m, path, tb := openOrders(t)
	sqlite(t, path, "update orders set note = null where id = 1; update orders set note = 'caf'||char(65533) where id = 2")
	dbKey(m, "r")

	press(m, "g", "l", "enter", "enter")

	if len(tb.edits) != 0 {
		t.Fatalf("a NULL looked at became an edit: %+v", tb.changes())
	}

	press(m, "j", "enter")

	if m.pv.cell == nil {
		t.Fatal("a U+FFFD went to the prompt")
	}
}

// TestTableCellEditorSaveAndClose saves a long value's editor from the close
// dialog: it closes itself, the tab after it stays.
func TestTableCellEditorSaveAndClose(t *testing.T) {
	m, _, tb := openOrders(t)

	press(m, "j", "j", "l", "enter", "ctrl+end", "!")

	press(m, "ctrl+n", "x") // an untitled file with text after it

	i := slices.IndexFunc(m.editors, func(e preview) bool { return e.cell != nil })
	m.showEditor(i)
	m.closeEditor(i)
	m.modal.choose(m, slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Save and close" }))

	if j := slices.IndexFunc(m.editors, func(e preview) bool { return e.untitled() && e.cell == nil }); j < 0 || !m.editors[j].dirty() || len(tb.edits) != 1 {
		t.Fatalf("the untitled file: tab %d of %d, %d table edits", j, len(m.editors), len(tb.edits))
	}
}

// TestTableNoColumns keys through a table of no columns, as Postgres has.
func TestTableNoColumns(t *testing.T) {
	m, _, tb := openOrders(t)
	m.Update(tableMsg{
		tb: tb, gen: tb.gen, t: db.Table{DB: tb.db, Name: "orders", Keys: []string{"rowid"}},
		page: db.Page{Rows: []db.Row{{Key: `["1"]`}}, Total: 1, Exact: true},
	})

	press(m, "enter", "u", "d", "u", "o")
	tb.setNull(0)
	checkWidths(t, m)
}

// TestTableReopenFails reloads a table whose columns changed under a filter
// they no longer satisfy: the page on screen keeps the columns it was read
// with.
func TestTableReopenFails(t *testing.T) {
	m, path, tb := openOrders(t)

	press(m, "ctrl+f")
	typeText(m, "note like 'note%'")
	dbKey(m, "enter")
	sqlite(t, path, "alter table orders drop column note; alter table orders add column a text; alter table orders add column b text")
	dbKey(m, "r")
	checkWidths(t, m)

	if len(tb.t.Cols) != 3 || !strings.Contains(tb.err, "note") {
		t.Fatalf("columns %d, error %q", len(tb.t.Cols), tb.err)
	}
}

// TestTableCellEditorAgain presses ⏎ on a cell whose editor is open with
// text typed: the editor shows, the text kept. Saved as a file, it is a file.
func TestTableCellEditorAgain(t *testing.T) {
	m, _, tb := openOrders(t)

	press(m, "j", "j", "l", "enter", "ctrl+end", "!")
	m.showEditor(slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb }))
	press(m, "enter")

	if m.pv.cell == nil || m.pv.buf.text() != "one\ntwo!" || !m.pv.dirty() {
		t.Fatalf("the editor again: %q", m.pv.buf.text())
	}

	dbDo(m, m.saveAs(m.edIdx, filepath.Join(m.ws, "app.db", "x.txt"), nil)) // app.db is no folder

	if m.pv.cell == nil {
		t.Fatal("a failed Save As lost the cell")
	}

	path := filepath.Join(m.ws, "note.txt")
	dbDo(m, m.saveAs(m.edIdx, path, nil))

	if m.pv.cell != nil || mustRead(t, path) != "one\ntwo!" || len(tb.edits) != 0 {
		t.Fatalf("saved as a file: cell %v, %d edits", m.pv.cell != nil, len(tb.edits))
	}
}

// TestTableCellEditorsApart opens long values' editors for cells that show
// the same row number at different times, and for two added rows: each its
// own, never another's.
func TestTableCellEditorsApart(t *testing.T) {
	m, path, tb := openOrders(t)
	sqlite(t, path, "update orders set note = 'four' || char(10) || 'lines' where id = 4")

	cells := func() int {
		return len(slices.DeleteFunc(slices.Clone(m.editors), func(e preview) bool { return e.cell == nil }))
	}
	table := func() { m.showEditor(slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb })) }

	press(m, "j", "j", "l", "enter", "ctrl+end", "!") // row 3: id 3
	table()
	press(m, "ctrl+f")
	typeText(m, "id <> 1")
	dbKey(m, "enter")
	press(m, "j", "j", "enter") // row 3 again: id 4 now

	if m.pv.cell == nil || m.pv.cell.key != `["4"]` || m.pv.buf.text() != "four\nlines" || cells() != 2 {
		t.Fatalf("row 3 after a filter: key %v, %q, %d editors", m.pv.cell != nil && m.pv.cell.key == `["4"]`, m.pv.buf.text(), cells())
	}

	table()
	press(m, "k", "ctrl+d", "ctrl+d", "enter") // the second added row's note
	table()
	press(m, "k", "enter") // the first's

	if cells() != 4 {
		t.Fatalf("two added rows share an editor: %d editors", cells())
	}
}

// TestTableColumnsChangeUnderEdits makes edits while a reload reads columns
// that changed: they keep naming the columns they were made in.
func TestTableColumnsChangeUnderEdits(t *testing.T) {
	m, path, tb := openOrders(t)

	sqlite(t, path, "alter table orders drop column note; alter table orders add column note text")

	_, reload := m.Update(keyMsg("r")) // no edits: it reads the columns again
	press(m, "o", "l", "enter")        // note, as the columns were
	typeText(m, "hello")
	press(m, "enter")
	dbDo(m, reload)
	checkWidths(t, m)

	if !strings.Contains(tb.err, "columns changed") || tb.t.Cols[1].Name != "note" {
		t.Fatalf("adopted: %q, %+v", tb.err, tb.t.Cols)
	}

	dbKey(m, "ctrl+s")

	if got := sqlite(t, path, "select quote(note), quote(total) from orders where id = 451"); got != "'hello'|NULL" {
		t.Fatalf("written: %s", got)
	}

	// Nor a key that changed under them: the edits find rows by the one read before.
	// The view goes first: SQLite 3.45 refuses the rename while one reads a dropped table.
	sqlite(t, path, "drop view big; create table o2(id integer, note text primary key, total real) without rowid; insert into o2 select id, coalesce(note, id), total from orders; drop table orders; alter table o2 rename to orders")

	tb.err = ""
	_, reload = m.Update(keyMsg("r"))
	press(m, "g", "d")
	dbDo(m, reload)

	if !strings.Contains(tb.err, "columns changed") || !slices.Equal(tb.t.Keys, []string{"rowid"}) {
		t.Fatalf("keys adopted: %q %q", tb.err, tb.t.Keys)
	}
}

// TestTableDeletedStrikesValues strikes a row marked for deletion through its
// values only: struck padding reads as dashes around them.
func TestTableDeletedStrikesValues(t *testing.T) {
	m, _, tb := openOrders(t)
	press(m, "d")

	line := tb.rowLine(m, tb.layout(m.pvW()), 0)
	buf := uv.NewScreenBuffer(ansi.StringWidth(line), 1)
	uv.NewStyledString(line).Draw(buf, buf.Bounds())

	struck := 0

	for x := range ansi.StringWidth(line) {
		c := buf.CellAt(x, 0)
		if c == nil || c.Style.Attrs&uv.AttrStrikethrough == 0 {
			continue
		}

		if c.Content == " " {
			t.Fatalf("a space struck at %d: %q", x, ansi.Strip(line))
		}

		struck++
	}

	if struck == 0 {
		t.Fatalf("nothing struck: %q", ansi.Strip(line))
	}
}

func TestTableAsksBeforeLosingEdits(t *testing.T) {
	m, _, _ := openOrders(t)

	// ctrl+c in a sidebar quits at once: with table edits, which no draft
	// keeps, it asks first.
	press(m, "d")
	dbDo(m, m.showView(viewDB))
	m.focus = 0
	press(m, "ctrl+c")

	if m.modal == nil || !strings.Contains(m.modal.title, "table change not submitted") {
		t.Fatalf("ctrl+c with table edits: %+v", m.modal)
	}

	if _, cmd := m.Update(keyMsg("enter")); m.modal != nil || cmd != nil { // a hasty ⏎ is Cancel
		t.Fatalf("⏎ in the dialog: modal %v, a command %v", m.modal != nil, cmd != nil)
	}

	press(m, "ctrl+c", "esc")
	m.focus = onMain
	press(m, "q")

	if m.modal == nil || !strings.Contains(m.modal.title, "1 change not submitted") {
		t.Fatalf("closing: %+v", m.modal)
	}

	press(m, "esc")
	m.confirmQuit()

	if !strings.Contains(m.modal.title, "1 table change not submitted") {
		t.Fatalf("quitting: %q", m.modal.title)
	}

	// Discarded, the edits go with the tab: Go Back brings back the table,
	// never them.
	press(m, "esc", "q")

	tb := m.pv.tbl
	m.modal.choose(m, slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Discard and close" }))

	if len(tb.edits) != 0 || slices.ContainsFunc(m.editors, func(e preview) bool { return e.tbl == tb }) {
		t.Fatalf("discarded: %d edits, tab open %v", len(tb.edits), m.pv.tbl == tb)
	}
}

// TestTableSelectionOutlivesRows acts on a selection whose added rows went
// with Revert All: the range ends at the last row there is.
func TestTableSelectionOutlivesRows(t *testing.T) {
	m, _, tb := openOrders(t)

	press(m, "o", "o", "shift+up")
	tb.items()[slices.IndexFunc(tb.items(), func(it item) bool { return it.label == "Revert All" })].run(m)
	press(m, "d", "u")
	checkWidths(t, m)

	if a, b := tb.selected(); b >= tb.count() || a > b && tb.anchor >= 0 {
		t.Fatalf("selection %d–%d of %d rows", a, b, tb.count())
	}
}

// TestTableSubmitsOnce keeps a second ctrl+s from sending the same edits
// while the first submit is on its way: an added row went in twice.
func TestTableSubmitsOnce(t *testing.T) {
	m, path, tb := openOrders(t)

	press(m, "o", "l", "enter")
	typeText(m, "x")
	press(m, "enter") // the note: one value set, the rest DEFAULT

	_, first := m.Update(keyMsg("ctrl+s"))
	_, second := m.Update(keyMsg("ctrl+s"))
	dbDo(m, tea.Batch(first, second))

	if got := sqlite(t, path, "select count(*) from orders"); got != "451" || tb.sending || len(tb.edits) != 0 {
		t.Fatalf("submitted twice: %s rows, sending %v, %d edits", got, tb.sending, len(tb.edits))
	}

	// While one is on its way the edits stay as they are: it drops the ones
	// it sent once through, a later change to one of them with it.
	press(m, "g", "0", "l", "enter", "ctrl+u", "y", "enter") // row 1's note

	_, cmd := m.Update(keyMsg("ctrl+s"))

	for _, k := range []string{"d", "o", "u"} {
		m.msg = ""
		dbKey(m, k) // the flash

		if m.msg != onItsWay || len(tb.edits) != 1 {
			t.Fatalf("%s during a submit: %q, %d edits", k, m.msg, len(tb.edits))
		}
	}

	dbDo(m, cmd)

	if got := sqlite(t, path, "select note from orders where id = 1"); got != "y" || len(tb.edits) != 0 || tb.sending {
		t.Fatalf("after the submit: %q, %d edits", got, len(tb.edits))
	}
}

// TestTableStaleAfterSubmit keeps the page a submit wrote over from taking
// edits until it is read again: a row it deleted may have given its key to
// one it added.
func TestTableStaleAfterSubmit(t *testing.T) {
	m, _, tb := openOrders(t)

	_, submit := m.Update(keyMsg("d"))
	dbDo(m, submit)
	_, submit = m.Update(keyMsg("ctrl+s"))

	var reload tea.Cmd

	for _, msg := range runAll(submit) {
		if done, ok := msg.(tableDoneMsg); ok {
			_, reload = m.Update(done) // the read after it, held
		}
	}

	m.msg = ""
	dbKey(m, "d")

	if !strings.Contains(m.msg, "before the submit") || len(tb.edits) != 0 {
		t.Fatalf("an edit on the page before the submit: %q, %d edits", m.msg, len(tb.edits))
	}

	dbDo(m, reload)
	dbKey(m, "d")

	if len(tb.edits) != 1 {
		t.Fatalf("read again, the page takes edits: %d", len(tb.edits))
	}
}

// TestTableEditsWaitForWorkspace switches to another workspace with table
// edits pending: no draft keeps them, so their tab waits, counted on quit,
// and comes back with its workspace.
func TestTableEditsWaitForWorkspace(t *testing.T) {
	m, _, tb := openOrders(t)
	ws := m.ws

	press(m, "d")
	m.switchWorkspace(t.TempDir())

	m.confirmQuit()

	if !strings.Contains(m.modal.title, "1 table change not submitted") || !slices.ContainsFunc(m.modal.items, func(it item) bool {
		return strings.Contains(it.hint, "table changes lost") && strings.HasPrefix(it.label, "Save all")
	}) {
		t.Fatalf("quitting elsewhere: %q", m.modal.title)
	}

	m.modal = nil
	m.switchWorkspace(ws)

	if !slices.ContainsFunc(m.editors, func(e preview) bool { return e.tbl == tb }) || len(tb.edits) != 1 {
		t.Fatalf("back: %d tabs, %d edits", len(m.editors), len(tb.edits))
	}

	// ⏎⏎ on a DEFAULT of a row added keeps it DEFAULT, not ''.
	press(m, "o", "l", "enter", "enter")

	if v, set := tb.value(tb.count()-1, 1); set {
		t.Fatalf("a DEFAULT looked at became %+v", v)
	}
}

// TestTablePreviewTabKeepsEdits opens another table over the preview tab of
// one with edits: that one stays, pinned, as an edited file does.
func TestTablePreviewTabKeepsEdits(t *testing.T) {
	m, path := dbModel(t)
	dbDo(m, m.openTable(path, db.Object{Name: "orders"}, true))
	m.focus = onMain

	tb := m.pv.tbl
	press(m, "d")
	dbDo(m, m.openTable(path, db.Object{Name: "big", View: true}, true))

	if i := slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb }); i < 0 || m.editors[i].transient || len(tb.edits) != 1 || m.tableEdits() != 1 {
		t.Fatalf("the edited table's tab: %d of %d, %d edits", i, len(m.editors), len(tb.edits))
	}
}

func TestTableView(t *testing.T) {
	m, path := dbModel(t)
	dbDo(m, m.openTable(path, db.Object{Name: "big"}, false))
	m.focus = onMain

	for _, k := range []string{"enter", "o", "d"} {
		m.msg = ""
		dbKey(m, k) // the flash

		if !strings.Contains(m.msg, "read-only") || len(m.pv.tbl.edits) != 0 {
			t.Fatalf("%s in a view: %q", k, m.msg)
		}
	}
}

func TestShown(t *testing.T) {
	if got := shown("a\nb\tc\x1b[31m\r"); got != "a↵b c�[31m" {
		t.Fatalf("shown = %q", got)
	}

	if got := shown("{\n  \"a\": 1\r\n}"); got != `{↵"a": 1↵}` {
		t.Fatalf("indentation after a line break stays: %q", got)
	}

	if got := shown(strings.Repeat("x", 10*cellMax)); len(got) != 4*cellMax {
		t.Fatalf("a long value is cut first: %d", len(got))
	}
}

func TestTableScrollsSideways(t *testing.T) {
	m, path := dbModel(t)

	var cols []string
	for c := range 30 {
		cols = append(cols, fmt.Sprintf("column_%02d", c))
	}

	sqlite(t, path, "create table wide("+strings.Join(cols, ", ")+"); insert into wide(column_00) values (1);")
	dbDo(m, m.openTable(path, db.Object{Name: "wide"}, false))
	m.focus = onMain
	tb := m.pv.tbl

	out := checkWidths(t, m)
	if tb.left != 0 || !strings.Contains(out, "column_00") || strings.Contains(out, "column_29") {
		t.Fatalf("first columns:\n%s", out)
	}

	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "column_00") && strings.Contains(l, "…") {
			t.Fatalf("the edge cuts a column, it is not truncated: %q", l)
		}
	}

	press(m, "$")

	if out := checkWidths(t, m); tb.left == 0 || !strings.Contains(out, "column_29") || strings.Contains(out, "column_00") {
		t.Fatalf("last column in view, left %d:\n%s", tb.left, out)
	}

	if lay := tb.layout(m.pvW()); !lay.hbar().on() || lay.colAt(lay.gutter+lay.xs[29]-lay.left+1) != 29 {
		t.Fatalf("bar %+v, column under the last one's first cell %d", lay.hbar(), lay.colAt(lay.gutter+lay.xs[29]-lay.left+1))
	}
}

func TestTablePromptOverCell(t *testing.T) {
	m, _, _ := openOrders(t)
	press(m, "l") // row 1's note

	at := func(lines []string, s string) (int, int) {
		for y, l := range lines {
			if i := strings.Index(l, s); i >= 0 {
				return y, ansi.StringWidth(l[:i])
			}
		}

		return -1, -1
	}

	y0, x0 := at(strings.Split(checkWidths(t, m), "\n"), "│ note 1 ")
	press(m, "enter")
	y1, x1 := at(strings.Split(checkWidths(t, m), "\n"), "› note 1")

	if y0 < 0 || y1 != y0 || x1 != x0 { // "› " stands where "│ " did, the value where it was
		t.Fatalf("the value at %d,%d, the cell at %d,%d", y1, x1, y0, x0)
	}

	if x, _, w, _, _ := m.modal.rect(m); x < m.mainX() || x+w > m.mainX()+m.mainW() {
		t.Fatalf("the box at %d, %d wide, out of the editor area at %d, %d wide", x, w, m.mainX(), m.mainW())
	}
}

func TestTableSelection(t *testing.T) {
	board := fakeClipboard(t)
	m, path, tb := openOrders(t)

	press(m, "shift+down", "shift+down")

	if a, b := tb.selected(); a != 0 || b != 2 || !strings.Contains(checkWidths(t, m), "3 selected") {
		t.Fatalf("selected %d–%d", a, b)
	}

	press(m, "d")

	if len(tb.edits) != 3 || !tb.deleted(0, 2) {
		t.Fatalf("delete takes the selection: %d edits", len(tb.edits))
	}

	press(m, "u")

	if len(tb.edits) != 0 {
		t.Fatalf("revert takes the selection: %d edits", len(tb.edits))
	}

	press(m, "right", "right") // to total: moving across keeps the rows

	for _, it := range tb.items() {
		if strings.HasPrefix(it.label, "Set NULL") {
			it.run(m)
		}
	}

	dbKey(m, "y")

	if got := mustRead(t, board); got != "id\tnote\ttotal\n1\tnote 1\t\n2\tnote 2\t\n3\t\"one\ntwo\"\t\n" {
		t.Fatalf("rows as TSV: %q", got)
	}

	press(m, "esc")

	if tb.anchor >= 0 || m.pv.kind != pvTable {
		t.Fatalf("esc drops the selection, then closes: anchor %d, %q", tb.anchor, m.pv.kind)
	}

	dbKey(m, "ctrl+s")

	if got := sqlite(t, path, "select count(*) from orders where total is null"); got != "3" {
		t.Fatalf("NULL in the selected rows: %s", got)
	}

	press(m, "shift+down", "down")

	if tb.anchor >= 0 {
		t.Fatal("a move without shift keeps the selection")
	}
}

func TestEnvURL(t *testing.T) {
	ws := t.TempDir()
	if envURL(ws) != "" {
		t.Fatal("no .env, no URL")
	}

	for body, want := range map[string]string{
		"A=1\nDATABASE_URL=postgres://u@h/db\n":                      "postgres://u@h/db",
		"export DATABASE_URL=\"mariadb://u:p w@h/db\" # the app's\n": "mariadb://u:p w@h/db",
		"DATABASE_URL='postgres://h/db'\n":                           "postgres://h/db",
		"DATABASE_URL=postgres://h/db # local\n":                     "postgres://h/db",
		"# DATABASE_URL=postgres://h/db\nOTHER_DATABASE_URL=x\n":     "",
	} {
		mustWrite(t, filepath.Join(ws, ".env"), body)

		if got := envURL(ws); got != want {
			t.Errorf("%q: %q, want %q", body, got, want)
		}
	}
}

func TestDatabasesConnections(t *testing.T) {
	m, path := dbModel(t)
	mustWrite(t, filepath.Join(m.ws, ".env"), "DATABASE_URL=mariadb://app@127.0.0.1:1/shop\n")
	m.st.Databases = map[string]string{"local": "postgres://app@127.0.0.1:1/app", "cache": "redis://h"}
	dbDo(m, m.showView(viewDB))

	var got []string

	for _, s := range m.db.sources(m) {
		got = append(got, s.label+" "+s.db.Kind)
	}

	if want := []string{"app.db sqlite", ".env mariadb", "cache ", "local postgres"}; !slices.Equal(got, want) {
		t.Fatalf("sources %q, want %q", got, want)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "local") || !strings.Contains(out, "postgres") {
		t.Fatalf("view:\n%s", out)
	}

	m.db.open["cache"] = true // a URL that does not parse says so under it
	if rows := m.db.rows(m); !slices.ContainsFunc(rows, func(r dbRow) bool { return strings.Contains(r.note, "redis://") }) {
		t.Fatalf("rows: %+v", rows)
	}

	m.db.objs["local"] = &dbObjs{}
	m.Update(dbObjsMsg{id: "local", target: m.st.Databases["local"], gen: m.db.gen, err: fmt.Errorf("password authentication failed: %w", db.ErrPassword)})

	if m.modal == nil || !strings.Contains(m.modal.title, "local") || m.modal.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("no password prompt: %+v", m.modal)
	}

	typeText(m, "secret")
	_, cmd := m.Update(keyMsg("enter"))

	if s, _ := m.db.source(m, "local"); m.db.passwords["local"] != "secret" || s.db.Password != "secret" || cmd == nil {
		t.Fatalf("the password is not kept, or not read with: %+v", s)
	}

	if s, _ := m.db.source(m, path); s.db.Password != "" {
		t.Fatal("a SQLite file took a server's password")
	}
}

func TestTableAsksForPassword(t *testing.T) {
	m, _, tb := openOrders(t)
	tb.id = "local"
	m.Update(tableMsg{tb: tb, gen: tb.gen, err: fmt.Errorf("x: %w", db.ErrPassword)})

	if m.modal == nil || m.modal.input.EchoMode != textinput.EchoPassword {
		t.Fatal("no password prompt for a table")
	}

	typeText(m, "pw")
	m.Update(keyMsg("enter"))

	if tb.db.Password != "pw" || !tb.busy {
		t.Fatalf("the table does not read again with it: %q busy %v", tb.db.Password, tb.busy)
	}
}

func TestTableReopenKeepsEdits(t *testing.T) {
	m, path, tb := openOrders(t)
	press(m, "d")
	dbDo(m, m.openTable(path, db.Object{Name: "orders"}, false))

	if m.pv.tbl != tb || len(m.pv.tbl.edits) != 1 || len(m.editors) != 1 {
		t.Fatalf("opened again: same table %v, %d edits, %d tabs", m.pv.tbl == tb, len(m.pv.tbl.edits), len(m.editors))
	}
}

// TestTablePostgres edits a Postgres table through the grid, where
// PANDO_TEST_PG names a server it may write to.
func TestTablePostgres(t *testing.T) {
	raw := os.Getenv("PANDO_TEST_PG")
	if raw == "" || !db.Has(db.Postgres) {
		t.Skip("PANDO_TEST_PG is not set")
	}

	m := testModelSized(t, 120, 30)
	m.hasDB = true
	m.st.Databases = map[string]string{"pg": raw}

	d, _ := db.Parse(raw)
	if out, err := exec.Command("psql", "-X", "-q", "-d", raw, "-c",
		"drop table if exists pando_ui; create table pando_ui(id serial primary key, note text); insert into pando_ui(note) values ('one'), ('two');").CombinedOutput(); err != nil {
		t.Fatalf("set up: %v %s", err, out)
	}

	dbDo(m, m.showView(viewDB))
	dbDo(m, m.openTable("pg", db.Object{Name: "pando_ui"}, false))
	m.focus = onMain

	tb := m.pv.tbl
	if tb.db.Kind != db.Postgres || tb.total != 2 || !strings.Contains(checkWidths(t, m), "two") {
		t.Fatalf("a Postgres page: %+v %d", tb.db, tb.total)
	}

	press(m, "l", "enter", "ctrl+u")
	typeText(m, "uno")
	press(m, "enter", "j", "d")
	dbKey(m, "ctrl+s")

	if out, err := exec.Command("psql", "-X", "-At", "-d", d.Target, "-c", "select string_agg(note, ',') from pando_ui").Output(); err != nil || strings.TrimSpace(string(out)) != "uno" {
		t.Fatalf("in the database: %v %q (%s)", err, out, m.msg)
	}
}

func TestNumeric(t *testing.T) {
	for typ, want := range map[string]bool{
		"INTEGER": true, "bigint": true, "double precision": true, "numeric(10,2)": true, "decimal(10,2)": true, "REAL": true, "money": true,
		"text": false, "varchar(100)": false, "uuid": false, "timestamp with time zone": false, "BLOB": false, "": false,
	} {
		if numeric(typ) != want {
			t.Errorf("numeric(%q) = %v", typ, !want)
		}
	}
}

// connModel is a test model with the Databases view on and its sources
// listed.
func connModel(t *testing.T) (*Model, string) {
	t.Helper()

	m, path := dbModel(t)
	dbDo(m, m.showView(viewDB))

	return m, path
}

func TestConnFormNew(t *testing.T) {
	m, _ := connModel(t)
	press(m, "n")

	f := formOf(m)
	if f == nil || f.old != "" {
		t.Fatalf("no form: %+v", m.modal)
	}

	press(m, "up", "right", "down") // Type: MariaDB, back to the name
	press(m, "tab", "ctrl+u")
	typeText(m, "127.0.0.1")
	press(m, "tab", "tab")
	typeText(m, "root")
	press(m, "tab")
	typeText(m, "pw")
	press(m, "tab", "tab")
	typeText(m, "shop")

	if got := f.url(); got != "mariadb://root@127.0.0.1:3306/shop" || f.name() != "shop@127.0.0.1" {
		t.Fatalf("url %q, name %q", got, f.name())
	}

	if out := checkWidths(t, m); !strings.Contains(out, "New Connection") || !strings.Contains(out, "‹ MariaDB ›") || strings.Contains(out, "pw") {
		t.Fatalf("form:\n%s", out)
	}

	press(m, "enter")

	if m.modal != nil || m.st.Databases["shop@127.0.0.1"] != "mariadb://root@127.0.0.1:3306/shop" || m.db.passwords["shop@127.0.0.1"] != "pw" {
		t.Fatalf("saved: %v, passwords %v, modal %+v", m.st.Databases, m.db.passwords, m.modal)
	}
}

func TestConnFormProblems(t *testing.T) {
	m, path := connModel(t)
	m.st.Databases = map[string]string{"taken": "postgres://h/db"}

	for name, want := range map[string]string{".env": "the worktree's .env", "taken": "taken"} {
		f := newConnForm(m, "")
		f.in[cfName].SetValue(name)

		if got := f.problem(m); !strings.Contains(got, want) {
			t.Errorf("%s: %q", name, got)
		}
	}

	f := newConnForm(m, "")
	f.in[cfHost].SetValue("")

	if why := f.problem(m); why != "" { // the server's local socket
		t.Errorf("no host: %q", why)
	}

	f.cycle(-1) // SQLite
	f.in[cfFile].SetValue(filepath.Join(m.ws, "Thumbs.db"))

	if !strings.Contains(f.problem(m), "no SQLite database") {
		t.Errorf("not a database: %q", f.problem(m))
	}

	f.in[cfFile].SetValue(path)

	if f.problem(m) != "" || f.url() != "sqlite://"+filepath.ToSlash(path) {
		t.Errorf("a database file: %q, %s", f.problem(m), f.url())
	}
}

func TestConnFormTest(t *testing.T) {
	m, path := connModel(t)
	f := newConnForm(m, "")
	f.cycle(-1)
	f.in[cfFile].SetValue(path)

	for _, msg := range runAll(f.testConn(m)) {
		m.Update(msg)
	}

	if !f.ok || f.test != "connected: 2 tables" {
		t.Fatalf("test: %v %q", f.ok, f.test)
	}
}

func TestConnFormEditAndRemove(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"local": "postgresql://app@db.lan:6543/app?sslmode=require"}
	m.db.passwords["local"] = "pw"

	saved := map[string]string{}
	hasKeyring = func() bool { return true }
	keyringSave = func(name, pw string) error { saved[name] = pw; return nil }

	t.Cleanup(func() {
		hasKeyring = func() bool { return false }
		keyringSave = func(string, string) error { return nil }
	})

	m.db.editConn(m, "local")
	f := formOf(m)

	if f.url() != "postgres://app@db.lan:6543/app?sslmode=require" || f.in[cfOptions].Value() != "sslmode=require" || f.in[cfName].Value() != "local" {
		t.Fatalf("prefilled: %s %q", f.url(), f.in[cfName].Value())
	}

	if f.keep || f.in[cfPassword].Placeholder != "kept for this run" {
		t.Fatalf("a password kept for the run says so: keep %v, %q", f.keep, f.in[cfPassword].Placeholder)
	}

	f.in[cfName].SetValue("prod")
	f.keep = true // Save: Forever

	_, cmd := m.Update(keyMsg("enter"))
	for _, msg := range runAll(cmd) {
		if _, ok := msg.(flashMsg); ok { // the keyring's answer, or the flash
			m.Update(msg)
		}
	}

	if _, old := m.st.Databases["local"]; old || m.st.Databases["prod"] != "postgres://app@db.lan:6543/app?sslmode=require" || m.db.passwords["prod"] != "pw" || m.db.passwords["local"] != "" || saved["prod"] != "pw" {
		t.Fatalf("renamed: %v, passwords %v, keyring %v", m.st.Databases, m.db.passwords, saved)
	}

	m.db.removeConn(m, "prod")
	m.modal.choose(m, slices.IndexFunc(m.modal.items, func(it item) bool { return it.label == "Remove" }))

	if len(m.st.Databases) != 0 || m.db.passwords["prod"] != "" {
		t.Fatalf("removed: %v %v", m.st.Databases, m.db.passwords)
	}
}

// TestConnFormRenameKeyring renames a connection whose password only the
// keyring has: memory has it before the tables are read, and a keyring that
// fails to store it under the new name keeps it under the old.
func TestConnFormRenameKeyring(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"a": "postgres://app@db.lan/app"}

	var forgot []string

	hasKeyring = func() bool { return true }
	keyringLoad = func(name string) string { return map[string]string{"a": "pw"}[name] }
	keyringSave = func(string, string) error { return errors.New("locked") }
	keyringForget = func(name string) { forgot = append(forgot, name) }

	t.Cleanup(func() {
		hasKeyring = func() bool { return false }
		keyringLoad = func(string) string { return "" }
		keyringSave = func(string, string) error { return nil }
		keyringForget = func(string) {}
	})

	m.db.editConn(m, "a")
	formOf(m).in[cfName].SetValue("b")

	_, cmd := m.Update(keyMsg("enter"))
	for _, msg := range runAll(cmd) {
		if _, ok := msg.(dbPasswordMsg); ok {
			m.Update(msg)
		}
	}

	if m.db.passwords["b"] != "pw" || slices.Contains(forgot, "a") {
		t.Fatalf("memory %q, forgot %q", m.db.passwords["b"], forgot)
	}
}

// TestConnFormWrittenPassword saves, unchanged, a connection whose URL has
// a password written by hand: it stays in the URL, off the screen.
func TestConnFormWrittenPassword(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"w": "postgres://app:secret@h:5432/db"}

	m.db.editConn(m, "w")

	if out := checkWidths(t, m); strings.Contains(out, "secret") {
		t.Fatalf("the password shows:\n%s", out)
	}

	press(m, "enter")

	if got := m.st.Databases["w"]; got != "postgres://app:secret@h:5432/db" {
		t.Fatalf("saved as %s", got)
	}

	// libpq's password= in the query: in the box too, off the screen, and
	// kept as the user's own.
	m.st.Databases["q"] = "postgres://app@h:5432/db?password=s3cretQ&sslmode=disable"
	m.db.editConn(m, "q")

	if out := checkWidths(t, m); strings.Contains(out, "s3cretQ") || formOf(m).in[cfOptions].Value() != "sslmode=disable" {
		t.Fatalf("the password shows: %q\n%s", formOf(m).in[cfOptions].Value(), out)
	}

	press(m, "enter")

	if d, _ := db.Parse(m.st.Databases["q"]); !strings.Contains(m.st.Databases["q"], "s3cretQ") || d.Kind != db.Postgres {
		t.Fatalf("saved as %s", m.st.Databases["q"])
	}
}

// TestConnFormKinds edits a mysql:// connection: it stays MySQL, saved as it was.
func TestConnFormKinds(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"my": "mysql://root@127.0.0.1:3306/test"}

	m.db.editConn(m, "my")

	if f := formOf(m); connKinds[f.kind].label != "MySQL" || f.url() != "mysql://root@127.0.0.1:3306/test" {
		t.Fatalf("edited: %s %s", connKinds[f.kind].label, f.url())
	}
}

// TestEnvChangesServer drops the password typed for the .env's server once
// the .env names another: it is not that one's.
func TestEnvChangesServer(t *testing.T) {
	m, _ := dbModel(t)
	v := &m.db
	v.sync(m)
	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@db.prod/app"})
	v.passwords[envDB] = "prod"

	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@db.prod/app"})

	if v.passwords[envDB] != "prod" {
		t.Fatal("the same server lost its password")
	}

	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@elsewhere/app"})

	if s, _ := v.source(m, envDB); v.passwords[envDB] != "" || s.db.Password != "" {
		t.Fatalf("another server got the password: %q", s.db.Password)
	}
}

// TestConnectionRetargeted opens a table again after its connection was
// pointed at another database: a tab of its own, on the new one; the old
// one's late answer does not list under it.
func TestConnectionRetargeted(t *testing.T) {
	m, path := dbModel(t)
	other := filepath.Join(t.TempDir(), "other.db")
	sqlite(t, other, "create table orders(id integer primary key, note text); insert into orders(note) values ('elsewhere');")

	m.st.Databases = map[string]string{"c": "sqlite://" + filepath.ToSlash(path)}
	m.db.sync(m)
	dbDo(m, m.openTable("c", db.Object{Name: "orders"}, false))
	old := m.pv.tbl

	m.st.Databases["c"] = "sqlite://" + filepath.ToSlash(other)
	dbDo(m, m.openTable("c", db.Object{Name: "orders"}, false))

	if m.pv.tbl == old || m.pv.tbl.db.Target != other || m.pv.tbl.rows[0].Cells[1].Text != "elsewhere" {
		t.Fatalf("reopened on %s", m.pv.tbl.db.Target)
	}

	m.db.objs["c"] = &dbObjs{}
	m.Update(dbObjsMsg{id: "c", target: path, gen: m.db.gen, objs: []db.Object{{Name: "stale"}}})

	if len(m.db.objs["c"].list) != 0 {
		t.Fatalf("the old database's tables: %+v", m.db.objs["c"].list)
	}
}

// TestEnvPasswordPrompt answers a password prompt raised for the .env's
// server after the .env named another: that one does not get it.
func TestEnvPasswordPrompt(t *testing.T) {
	m, _ := dbModel(t)
	v := &m.db
	v.sync(m)
	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@db.prod/app"})
	v.objs[envDB] = &dbObjs{}
	m.Update(dbObjsMsg{id: envDB, target: "postgres://app@db.prod/app", gen: v.gen, err: db.ErrPassword})

	if m.modal == nil || !strings.Contains(m.modal.title, "app@db.prod") {
		t.Fatalf("the prompt names the server: %+v", m.modal)
	}

	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@elsewhere/app"})
	typeText(m, "prod")
	press(m, "enter")

	if s, _ := v.source(m, envDB); v.passwords[envDB] != "" || s.db.Password != "" {
		t.Fatalf("the .env's new server got the password: %q", s.db.Password)
	}
}

// TestEnvURLLast reads the .env as dotenv loaders and a shell do: the last
// DATABASE_URL wins, an empty one too.
func TestEnvURLLast(t *testing.T) {
	ws := t.TempDir()

	for env, want := range map[string]string{
		"DATABASE_URL=postgres://h/dev\nDATABASE_URL=postgres://h/app\n": "postgres://h/app",
		"DATABASE_URL=\nexport DATABASE_URL='postgres://h/app' # prod\n": "postgres://h/app",
		"DATABASE_URL=postgres://h/app\nDATABASE_URL=\n":                 "",
	} {
		mustWrite(t, filepath.Join(ws, ".env"), env)

		if got := envURL(ws); got != want {
			t.Errorf("%q: %q, want %q", env, got, want)
		}
	}
}

// TestConnFormLongName draws the form of a connection whose name is wider
// than the box.
func TestConnFormLongName(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"analytics@prod-db-01.eu-west-1.rds.amazonaws.com": "postgres://app@db/app"}

	m.db.editConn(m, "analytics@prod-db-01.eu-west-1.rds.amazonaws.com")
	checkWidths(t, m)
}

// TestPasswordPromptWaits keeps a password prompt from taking over a box
// the user is typing into: the keys typed next are that box's.
func TestPasswordPromptWaits(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"prod": "postgres://app@prod/app"}
	m.db.objs["prod"] = &dbObjs{}

	press(m, "n")
	form := formOf(m)
	_, cmd := m.Update(dbObjsMsg{id: "prod", target: m.st.Databases["prod"], gen: m.db.gen, err: db.ErrPassword})

	if m.modal == nil || formOf(m) != form || cmd == nil {
		t.Fatalf("the prompt took over the form: %+v", m.modal)
	}

	// Opened again, it reads again, and asks.
	m.modal = nil
	m.db.open["prod"] = false

	if m.db.toggle(m, "prod") == nil {
		t.Fatal("opened again, the source does not ask")
	}
}

// TestExplorerOpensFIFO opens a named pipe: nothing reads its header on the
// UI's goroutine, where it would wait for a writer forever.
func TestExplorerOpensFIFO(t *testing.T) {
	m, _ := dbModel(t)
	fifo := filepath.Join(m.ws, "pipe")

	if err := exec.Command("mkfifo", fifo).Run(); err != nil {
		t.Skip("no mkfifo")
	}

	done := make(chan struct{})

	go func() {
		m.openFile(fifo, true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("opening a FIFO blocks")
	}
}

// TestEnvNamedConnection skips a [databases] entry named as the worktree's
// .env: it would take its rows and its password.
func TestEnvNamedConnection(t *testing.T) {
	m, _ := dbModel(t)
	v := &m.db
	v.sync(m)
	v.onFiles(m, dbFilesMsg{ws: m.ws, gen: v.gen, env: "postgres://app@a/app"})
	m.st.Databases = map[string]string{envDB: "postgres://app@b/app", "bad": "postgres://app@db:54x/app"}

	if srcs := slices.DeleteFunc(v.sources(m), func(s dbSource) bool { return s.id != envDB }); len(srcs) != 1 || srcs[0].db.Target != "postgres://app@a/app" {
		t.Fatalf("sources named .env: %+v", srcs)
	}

	// A URL the form cannot show is not saved over with its empty boxes.
	dbDo(m, v.editConn(m, "bad"))

	if m.modal != nil || !strings.Contains(m.msg, "does not parse") {
		t.Fatalf("edited a URL that does not parse: %+v %q", m.modal, m.msg)
	}
}

// TestConnFormIPv6 brackets an IPv6 host in the URL, with a port or none.
func TestConnFormIPv6(t *testing.T) {
	m, _ := connModel(t)
	press(m, "n")

	f := formOf(m)
	f.in[cfHost].SetValue("::1")

	if got := f.url(); got != "postgres://[::1]:5432" {
		t.Fatalf("with a port: %s", got)
	}

	f.in[cfPort].SetValue("")

	if got := f.url(); got != "postgres://[::1]" {
		t.Fatalf("without one: %s", got)
	}
}

// TestConnFormUntilRestart turns Save to Until restart on a connection whose
// password the keyring keeps: it moves to memory, the keyring forgets it.
func TestConnFormUntilRestart(t *testing.T) {
	m, _ := connModel(t)
	m.st.Databases = map[string]string{"k": "postgres://app@db.lan/app"}

	var forgot []string

	hasKeyring = func() bool { return true }
	keyringLoad = func(name string) string { return map[string]string{"k": "from-keyring"}[name] }
	keyringForget = func(name string) { forgot = append(forgot, name) }

	t.Cleanup(func() {
		hasKeyring = func() bool { return false }
		keyringLoad = func(string) string { return "" }
		keyringForget = func(string) {}
	})

	m.db.editConn(m, "k")

	if f := formOf(m); f.in[cfPassword].Placeholder != "unchanged when empty" || !f.keep {
		t.Fatalf("form: %q keep %v", f.in[cfPassword].Placeholder, f.keep)
	}

	formOf(m).keep = false // Save: Until restart

	_, cmd := m.Update(keyMsg("enter"))
	for _, msg := range runAll(cmd) {
		if _, ok := msg.(dbPasswordMsg); ok {
			m.Update(msg)
		}
	}

	if m.db.passwords["k"] != "from-keyring" || !slices.Equal(forgot, []string{"k"}) {
		t.Fatalf("memory %q, forgot %q", m.db.passwords["k"], forgot)
	}
}

// TestConnFormBlurredText draws a box without the keyboard in the text's
// own color: bubbles' ANSI 7 vanishes on a light theme.
func TestConnFormBlurredText(t *testing.T) {
	m, _ := connModel(t)
	press(m, "n")

	for _, l := range formOf(m).lines(72) {
		if strings.Contains(l, "\x1b[37m") || strings.Contains(l, ";37m") {
			t.Fatalf("a box draws in ANSI 7: %q", ansi.Strip(l))
		}
	}
}

// TestConnFormMouse turns Type and Save with clicks, as ← and → do.
func TestConnFormMouse(t *testing.T) {
	m, _ := connModel(t)
	press(m, "n")

	f := formOf(m)
	x, y, _, _, _ := m.modal.rect(m)
	value := x + 1 + 11 // the frame, the label

	click(m, value, y+1, tea.MouseLeft) // ‹ on Type: back from PostgreSQL

	if f.kind != len(connKinds)-1 || f.at != 0 {
		t.Fatalf("‹ turns the type back: %s, row %d", connKinds[f.kind].label, f.at)
	}

	click(m, value+5, y+1, tea.MouseLeft) // the value: on

	if f.kind != 0 {
		t.Fatalf("a click on the value turns it on: %s", connKinds[f.kind].label)
	}

	hasKeyring = func() bool { return true }

	t.Cleanup(func() { hasKeyring = func() bool { return false } })

	f.keep = true
	click(m, value, y+1+slices.Index(f.rows(), cfSave), tea.MouseLeft)

	if f.keep || f.rows()[f.at] != cfSave || m.modal == nil {
		t.Fatalf("a click on Save flips it: keep %v, row %d", f.keep, f.at)
	}
}

func TestConsole(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the password goes through sh")
	}

	// A psql that prints the password it got and its arguments.
	bin, run := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(bin, "psql"), "#!/bin/sh\nprintf '%s|%s' \"$PGPASSWORD\" \"$*\"\n")

	if err := os.Chmod(filepath.Join(bin, "psql"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PANDO_RUNTIME_DIR", run)

	msg, ok := console(dbSource{id: "prod", db: db.DB{Kind: db.Postgres, Target: "postgres://me:it%27s%20secret@db.io/app"}})().(consoleMsg)
	if !ok || slices.ContainsFunc(msg.argv, func(a string) bool { return strings.Contains(a, "secret") }) {
		t.Fatalf("console: %q", msg.argv)
	}

	out, err := exec.Command(msg.argv[0], msg.argv[1:]...).Output()
	if err != nil || string(out) != "it's secret|postgres://me@db.io/app" {
		t.Fatalf("the client got %q: %v", out, err)
	}

	if left, _ := os.ReadDir(run); len(left) != 0 {
		t.Fatalf("the password's file stayed: %v", left)
	}

	// Started again, as a restarted daemon does, the client asks for it.
	if out, _ := exec.Command(msg.argv[0], msg.argv[1:]...).Output(); string(out) != "|postgres://me@db.io/app" {
		t.Fatalf("again: %q", out)
	}

	if msg, ok := console(dbSource{id: "app.db", db: db.File("app.db")})().(consoleMsg); db.Has(db.SQLite) && (!ok || !slices.Equal(msg.argv, []string{"sqlite3", "app.db"})) {
		t.Fatalf("sqlite3: %+v", msg)
	}

	// mariadb asks only when told to: -p, when it starts again.
	mustWrite(t, filepath.Join(bin, "mariadb"), "#!/bin/sh\nprintf '%s|%s' \"$MYSQL_PWD\" \"$*\"\n")

	if err := os.Chmod(filepath.Join(bin, "mariadb"), 0o755); err != nil {
		t.Fatal(err)
	}

	msg, _ = console(dbSource{id: "shop", db: db.DB{Kind: db.MariaDB, Target: "mariadb://me:pw@db.io/shop"}})().(consoleMsg)
	first, _ := exec.Command(msg.argv[0], msg.argv[1:]...).Output()
	again, _ := exec.Command(msg.argv[0], msg.argv[1:]...).Output()

	if args := "-h db.io -u me -D shop --default-character-set=utf8mb4"; string(first) != "pw|"+args || string(again) != "|"+args+" -p" {
		t.Fatalf("mariadb: %q, then %q", first, again)
	}
}

// formOf is the connection form the modal holds, nil for none.
func formOf(m *Model) *connForm {
	if m.modal == nil {
		return nil
	}

	f, _ := m.modal.form.(*connForm)

	return f
}

func TestTableAddScrollsWholeColumns(t *testing.T) {
	m, _, tb := openOrders(t)

	// Every column fits, until the duplicate's DEFAULT widens id: the
	// cursor's last column scrolls into view by whole columns, none cut.
	lay := tb.layout(1000)
	w := lay.gutter + lay.total + 1
	tb.col = len(tb.t.Cols) - 1
	tb.add(tb.l.sel)
	tb.body(m, w, 30)

	if lay := tb.layout(w - 1); tb.left == 0 || !slices.Contains(lay.xs, tb.left) {
		t.Fatalf("left %d, columns at %v", tb.left, lay.xs)
	}
}

func TestTablePendingSQLWraps(t *testing.T) {
	m, _, tb := openOrders(t)

	long := strings.Repeat("word ", 30) + "zzend"
	tb.ref(0, 1).put(&long)
	tb.add(0)
	dbDo(m, tb.showSQL(m))

	var labels, want []string
	for _, it := range m.modal.items[2:] {
		labels = append(labels, it.label)
	}

	for _, c := range tb.changes() {
		want = append(want, tb.t.Statement(c)+";")
	}

	if out := checkWidths(t, m); !slices.Equal(strings.Fields(strings.Join(labels, " ")), strings.Fields(strings.Join(want, " "))) ||
		!strings.Contains(out, "collate binary = 'note 1';") || !strings.Contains(out, "zzend', '1.0');") {
		t.Fatalf("statements in full: %q, want %q:\n%s", labels, want, out)
	}
}

// TestTablePendingSQLAtTop opens Pending SQL at the top of the editor area, as
// the connection form: lower, its title cut the WHERE and ORDER BY boxes.
func TestTablePendingSQLAtTop(t *testing.T) {
	m, _, tb := openOrders(t)
	tb.add(0)
	dbDo(m, tb.showSQL(m))

	if _, y, _, _, _ := m.modal.rect(m); y != m.headH()+m.stripH() {
		t.Fatalf("Pending SQL at row %d, the area's body starts at %d", y, m.headH()+m.stripH())
	}
}

// TestTableEditPins keeps a table's preview tab once it is edited, as an
// edited file's, and offers only esc under a prompt open over the grid.
func TestTableEditPins(t *testing.T) {
	m, _, _ := openOrders(t)
	m.pv.transient = true

	press(m, "enter") // the cell's prompt

	if h := m.pv.hints(m); h != " esc close" {
		t.Fatalf("hints under the prompt: %q", h)
	}

	press(m, "ctrl+u")
	typeText(m, "x")
	press(m, "enter")

	if m.pv.transient {
		t.Fatal("an edited table stays in the preview tab")
	}
}

// TestTableRuleCloses ends the rule under the header in the last column's own
// border, not a joint with nothing on its right.
func TestTableRuleCloses(t *testing.T) {
	m, _, tb := openOrders(t)
	_ = m

	if rule := strings.TrimRight(ansi.Strip(tb.rule(tb.layout(200))), " "); !strings.HasSuffix(rule, "┤") {
		t.Fatalf("rule ends %q", rule[max(len(rule)-12, 0):])
	}
}

func TestTableBoxesPlaceholders(t *testing.T) {
	m, _, tb := openOrders(t)

	out := ansi.Strip(strings.Join(tb.body(m, 68, 30), "\n")) // the demo's editor area
	if !strings.Contains(out, tb.where.Placeholder) || !strings.Contains(out, tb.order.Placeholder) {
		t.Fatalf("placeholders cut:\n%s", out)
	}
}

// TestConnFormAtAreaTop puts the connection form at the top of the editor
// area's body: a third of the way down it cut the grid's boxes in half.
func TestConnFormAtAreaTop(t *testing.T) {
	m, _ := connModel(t)
	press(m, "n")

	if m.modal == nil || m.modal.form == nil {
		t.Fatalf("no form: %+v", m.modal)
	}

	if _, y, _, _, _ := m.modal.rect(m); y != m.headH()+m.stripH() {
		t.Fatalf("form at row %d, the body starts at %d", y, m.headH()+m.stripH())
	}
}
