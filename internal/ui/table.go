package ui

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/db"
)

// pvTable is a table or a view of a database in a grid: path is its
// source's id, a SQLite file's path or a server's name; name the table.
const pvTable = "table"

const (
	tablePage = 200 // the rows a table reads at once
	cellMax   = 40  // the widest a column is drawn; the whole value is in its prompt
	longValue = 200 // longer, or with a line break, a value is edited in an editor tab
	tableHead = 5   // the boxes and the header over the rows
)

// tableKeys are a table's keys, for the help.
var tableKeys = [][2]string{
	{"↑↓←→", "move"},
	{"⇧↑↓", "select rows"},
	{"⏎", "edit"},
	{"o", "add row"},
	{"^d", "duplicate"},
	{"d", "delete"},
	{"u", "revert"},
	{"^s", "submit"},
	{"s", "sort"},
	{"[ ]", "page"},
	{"^f", "where"},
	{"r", "reload"},
	{"y", "copy"},
	{"m", "menu"},
}

// table is a table editor's state, which the copies of its tab share: the
// table, the page on screen, the boxes over it and the edits not submitted.
type table struct {
	id           string // the source the view lists it under: a SQLite file's path, a server's name
	db           db.DB
	schema, name string
	t            db.Table
	rows         []db.Row
	total        int
	exact, more  bool // total was counted, not the server's estimate; rows follow the page
	offset       int  // the first row's, of the rows the boxes keep
	where, order textinput.Model
	applied      [2]string // the boxes as the page was read: esc puts them back
	box          int       // the box with the keyboard: 0 none, 1 WHERE, 2 ORDER BY
	l            list      // the cursor's row and the rows in view
	col, left    int       // the cursor's column and the first column in view
	anchor       int       // the other end of the rows selected with shift, -1 for none
	reveal       bool      // the cursor's cell comes into view at the next draw: a row was added
	edits        []*rowEdit
	gen          int
	busy, ready  bool
	sending      bool               // a submit is on its way: another would write its edits twice
	stale        bool               // a submit wrote since the page was read: its rows may be gone, their keys taken
	stop         context.CancelFunc // stops the read running
	err          string
}

// rowEdit is what is pending for one row: columns set, the row deleted, or
// a row added (key "").
type rowEdit struct {
	key string
	del bool
	set map[int]*string // nil for NULL
	old map[int]db.Cell // the set columns as they were read
}

type tableMsg struct {
	tb     *table
	gen    int
	t      db.Table
	offset int
	page   db.Page
	count  bool // the rows were counted, not read
	err    error
}

type tableDoneMsg struct {
	tb    *table
	sent  []*rowEdit
	err   error
	close bool // the tab goes once they are written
}

func newTable(id string, d db.DB, schema, name string) *table {
	tb := &table{id: id, db: d, schema: schema, name: name, where: textinput.New(), order: textinput.New(), anchor: -1}
	tb.where.Prompt, tb.where.Placeholder = "WHERE ", "e.g. id > 10"
	tb.order.Prompt, tb.order.Placeholder = "ORDER BY ", "e.g. id desc"

	return tb
}

// load reads the page at offset of the rows the boxes keep, and what the
// table is the first time or with reopen. The page on screen stays until
// the answer replaces it.
func (tb *table) load(offset int, reopen bool) tea.Cmd {
	ctx, gen := tb.start()
	t, where, order := tb.t, tb.applied[0], tb.applied[1]
	d, schema, name := tb.db, tb.schema, tb.name

	return func() tea.Msg {
		msg := tableMsg{tb: tb, gen: gen, offset: offset}
		if t.Name == "" || reopen {
			if t, msg.err = d.Open(ctx, schema, name); msg.err != nil {
				return msg
			}
		}

		msg.t = t
		msg.page, msg.err = t.Page(ctx, where, order, offset, tablePage)

		return msg
	}
}

// countRows counts every row the boxes keep, where the total is the
// server's estimate.
func (tb *table) countRows() tea.Cmd {
	if tb.busy { // a read on its way would be stopped, its rows never shown
		return flash("a read is on its way", false)
	}

	ctx, gen := tb.start()
	t, where := tb.t, tb.applied[0]

	return func() tea.Msg {
		n, err := t.Count(ctx, where)
		return tableMsg{tb: tb, gen: gen, page: db.Page{Total: n}, count: true, err: err}
	}
}

// start begins a read that esc stops, in place of the one running.
func (tb *table) start() (context.Context, int) {
	tb.cancel()
	tb.gen++
	tb.busy = true

	ctx, stop := context.WithCancel(context.Background())
	tb.stop = stop

	return ctx, tb.gen
}

// cancel stops the read running; true when one was.
func (tb *table) cancel() bool {
	if tb.stop == nil {
		return false
	}

	tb.stop()
	tb.stop = nil

	return true
}

func (tb *table) onLoad(msg tableMsg) {
	if msg.gen != tb.gen {
		return
	}

	tb.busy = false
	tb.cancel() // done: its context goes

	switch {
	case errors.Is(msg.err, context.Canceled): // esc: the page on screen stays, with its columns
		return
	case msg.err != nil:
		tb.err = errLine(msg.err.Error())
		return

	case msg.count:
		tb.total, tb.exact, tb.err = msg.page.Total, true, ""
		return
	}

	if msg.t.Name != "" { // the columns the rows were read with
		if len(tb.edits) > 0 && (!slices.EqualFunc(tb.t.Cols, msg.t.Cols, func(a, b db.Column) bool { return a.Name == b.Name }) || !slices.Equal(tb.t.Keys, msg.t.Keys)) {
			tb.err = "the columns changed under the edits made meanwhile: submit or revert them, then r reads again"
			return // the edits name columns by their place among those read before, and rows by the key read before
		}

		tb.t = msg.t
	}

	p := msg.page
	tb.rows, tb.total, tb.exact, tb.more, tb.offset, tb.stale = p.Rows, p.Total, p.Exact, p.More, msg.offset, false
	tb.err, tb.ready, tb.anchor = "", true, -1 // other rows: the selection goes
	tb.l.sel = max(min(tb.l.sel, tb.count()-1), 0)
	tb.col = max(min(tb.col, len(tb.t.Cols)-1), 0)
}

// added are the rows added here, in the order they were.
func (tb *table) added() []*rowEdit {
	var out []*rowEdit

	for _, e := range tb.edits {
		if e.key == "" {
			out = append(out, e)
		}
	}

	return out
}

// count is the rows on screen: the page, then the rows added.
func (tb *table) count() int { return len(tb.rows) + len(tb.added()) }

// editFor is what is pending for the row at key, a new edit when nothing is.
func (tb *table) editFor(key string) *rowEdit {
	if e := tb.editOf(key); e != nil {
		return e
	}

	e := &rowEdit{key: key, set: map[int]*string{}, old: map[int]db.Cell{}}
	tb.edits = append(tb.edits, e)

	return e
}

func (tb *table) editOf(key string) *rowEdit {
	if key == "" {
		return nil
	}

	if i := slices.IndexFunc(tb.edits, func(e *rowEdit) bool { return e.key == key }); i >= 0 {
		return tb.edits[i]
	}

	return nil
}

// editAt is what is pending for row i, nil for nothing.
func (tb *table) editAt(i int) *rowEdit {
	switch {
	case i < 0 || i >= tb.count():
		return nil
	case i < len(tb.rows):
		return tb.editOf(tb.rows[i].Key)
	}

	return tb.added()[i-len(tb.rows)]
}

// value is the cell at row i, column c as it will be once submitted, and
// whether an edit set it; an added row's unset cell is its DEFAULT.
func (tb *table) value(i, c int) (db.Cell, bool) {
	if c < 0 || c >= len(tb.t.Cols) { // a table of no columns
		return db.Cell{}, false
	}

	if e := tb.editAt(i); e != nil {
		if v, ok := e.set[c]; ok {
			if v == nil {
				return db.Cell{Null: true}, true
			}

			return db.Cell{Text: *v}, true
		}
	}

	if i >= 0 && i < len(tb.rows) {
		return tb.rows[i].Cells[c], false
	}

	return db.Cell{}, false
}

// cellRef is a cell an edit is on its way to, by its row's key or the added
// row itself and its column's name, with the value read for the conflict
// check: a prompt or an editor tab outlives the page it was opened on, and
// a reload may bring other columns.
type cellRef struct {
	tb          *table
	key         string
	add         *rowEdit
	name        string
	read, shown db.Cell // the row's value as read, and as the cell showed it, an edit pending
}

func (tb *table) ref(i, c int) cellRef {
	r := cellRef{tb: tb, name: tb.t.Cols[c].Name}
	r.shown, _ = tb.value(i, c)

	if i < len(tb.rows) {
		r.key, r.read = tb.rows[i].Key, tb.rows[i].Cells[c]
	} else {
		r.add = tb.added()[i-len(tb.rows)]
	}

	return r
}

// put makes the cell v, nil for NULL; why it could not, "" when it did.
func (r cellRef) put(v *string) string {
	tb := r.tb
	c := slices.IndexFunc(tb.t.Cols, func(col db.Column) bool { return col.Name == r.name })

	switch {
	case tb.frozen() != "":
		return tb.frozen() + ": the value was not kept"
	case c < 0:
		return r.name + " is gone: the value was not kept"
	case r.add != nil && !slices.Contains(tb.edits, r.add):
		return "the row is gone: the value was not kept"
	case r.add != nil:
		r.add.set[c] = v
		return ""
	}

	// The value the edit was made on: as read, or the page's now when a
	// submit since wrote what the cell showed. Any other value is another
	// hand's, or another row's that took the key: a conflict, at submit.
	read := r.read
	if i := slices.IndexFunc(tb.rows, func(row db.Row) bool { return row.Key == r.key }); i >= 0 && tb.rows[i].Cells[c] == r.shown {
		read = tb.rows[i].Cells[c]
	}

	e := tb.editFor(r.key)
	if _, ok := e.old[c]; !ok {
		e.old[c] = read
	}

	if same(e.old[c], v) { // back to what was read: no edit
		delete(e.set, c)
		delete(e.old, c)
	} else {
		e.set[c] = v
	}

	tb.prune(e)

	return ""
}

func same(c db.Cell, v *string) bool {
	return v == nil && c.Null || v != nil && !c.Null && !c.Blob && c.Text == *v
}

// prune drops an edit of a row read that no longer changes it.
func (tb *table) prune(e *rowEdit) {
	if e.key != "" && !e.del && len(e.set) == 0 {
		tb.drop(e)
	}
}

func (tb *table) drop(e *rowEdit) {
	tb.edits = slices.DeleteFunc(tb.edits, func(o *rowEdit) bool { return o == e })
}

// discard drops every edit, and the rows added with them.
func (tb *table) discard() {
	tb.edits = nil
	tb.clampSel()
}

// onItsWay is why the edits stay as they are while a submit runs: once
// through, it drops the ones it sent, and a later change to one with it.
const onItsWay = "a submit is on its way"

// frozen is why the edits stay as they are now, "" when they can change:
// while a submit runs, and until the page it wrote over is read again.
func (tb *table) frozen() string {
	switch {
	case tb.sending:
		return onItsWay
	case tb.stale:
		return "the rows are from before the submit: r reads them again"
	}

	return ""
}

// noEdits is why the table takes no edits.
func (tb *table) noEdits() string {
	if tb.t.View {
		return tb.name + " is read-only: a view"
	}

	return tb.name + " is read-only: no primary key finds its rows again"
}

// onTableLoad takes a page in; a server that wants a password asks for it,
// then reads again.
func (m *Model) onTableLoad(msg tableMsg) tea.Cmd {
	tb := msg.tb
	tb.onLoad(msg)

	if msg.gen == tb.gen && errors.Is(msg.err, db.ErrPassword) {
		return m.askPassword(tb.id, tb.db.Target, func(_ *Model, pw string) tea.Cmd {
			tb.db.Password = pw
			return tb.load(tb.offset, false) // opened already, or opened now: edits keep their columns
		})
	}

	return nil
}

// readOnly is why the cell at row i, column c cannot be edited, "" when it can.
func (tb *table) readOnly(i, c int) string {
	cell, _ := tb.value(i, c)

	switch e := tb.editAt(i); {
	case tb.frozen() != "":
		return tb.frozen()
	case tb.t.ReadOnly():
		return tb.noEdits()
	case i < 0 || i >= tb.count() || c >= len(tb.t.Cols):
		return "no cell here"
	case e != nil && e.del:
		return "the row is to be deleted: u keeps it"
	case tb.t.Cols[c].Generated:
		return tb.t.Cols[c].Name + " is generated from the other columns"
	case cell.Blob:
		return "a blob is shown, not edited"
	}

	return ""
}

// add puts a new row after the others, a copy of row from but for its key
// columns when from is one, and moves the cursor to it.
func (tb *table) add(from int) tea.Cmd {
	switch {
	case tb.frozen() != "":
		return flash(tb.frozen(), true)
	case tb.t.ReadOnly():
		return flash(tb.noEdits(), true)
	}

	e := &rowEdit{set: map[int]*string{}}

	for c, col := range tb.t.Cols {
		cell, set := tb.value(from, c)
		if from < 0 || from >= tb.count() || col.PK || col.Generated || cell.Blob || from >= len(tb.rows) && !set {
			continue
		}

		if cell.Null {
			e.set[c] = nil
		} else {
			e.set[c] = &cell.Text
		}
	}

	tb.edits = append(tb.edits, e)
	tb.l.sel, tb.reveal, tb.anchor = tb.count()-1, true, -1

	return nil
}

// selected is the rows an action takes: those selected with shift, else the
// cursor's.
func (tb *table) selected() (a, b int) {
	if tb.anchor < 0 {
		return tb.l.sel, tb.l.sel
	}

	return min(tb.anchor, tb.l.sel), min(max(tb.anchor, tb.l.sel), tb.count()-1) // added rows may have gone since
}

// picked reports row i among those selected with shift.
func (tb *table) picked(i int) bool {
	a, b := tb.selected()
	return tb.anchor >= 0 && i >= a && i <= b
}

// deleted reports every row read in [a, b] marked to be deleted, and some.
func (tb *table) deleted(a, b int) bool {
	if a >= len(tb.rows) {
		return false
	}

	for i := a; i <= b && i < len(tb.rows); i++ {
		if e := tb.editOf(tb.rows[i].Key); e == nil || !e.del {
			return false
		}
	}

	return true
}

// clampSel keeps the cursor and the selection on rows still there: an added
// row goes at once.
func (tb *table) clampSel() {
	tb.l.sel = max(min(tb.l.sel, tb.count()-1), 0)
	tb.anchor = min(tb.anchor, tb.count()-1)
}

// deleteRows marks the rows taken to be deleted, or keeps them again when
// they all are; added rows among them go at once.
func (tb *table) deleteRows() tea.Cmd {
	a, b := tb.selected()

	switch {
	case tb.frozen() != "":
		return flash(tb.frozen(), true)
	case tb.t.ReadOnly():
		return flash(tb.noEdits(), true)
	case a < 0 || a >= tb.count():
		return nil
	}

	del, added := !tb.deleted(a, b), tb.added()

	for i := a; i <= b; i++ {
		if i >= len(tb.rows) {
			tb.drop(added[i-len(tb.rows)])
			continue
		}

		e := tb.editFor(tb.rows[i].Key)
		e.del = del
		tb.prune(e)
	}

	tb.clampSel()

	return nil
}

// revert undoes what is pending at the cursor: the cell's edit, else the
// row's deletion, else an added row. Rows selected lose everything pending.
func (tb *table) revert(c int) tea.Cmd {
	if why := tb.frozen(); why != "" {
		return flash(why, true)
	}

	a, b := tb.selected()
	if tb.anchor >= 0 {
		added := tb.added()

		for i := a; i <= b; i++ {
			switch {
			case i >= len(tb.rows):
				tb.drop(added[i-len(tb.rows)])
			case tb.editOf(tb.rows[i].Key) != nil:
				tb.drop(tb.editOf(tb.rows[i].Key))
			}
		}

		tb.clampSel()

		return nil
	}

	e := tb.editAt(a)

	switch _, set := tb.value(a, c); {
	case e == nil:
	case set:
		delete(e.set, c)
		delete(e.old, c)
		tb.prune(e)

	case e.del:
		e.del = false
		tb.prune(e)

	case e.key == "":
		tb.drop(e)
		tb.clampSel()
	}

	return nil
}

// setNull sets column c to NULL in the rows taken, where it can be set.
func (tb *table) setNull(c int) tea.Cmd {
	a, b := tb.selected()
	why, n := "", 0

	for i := a; i <= b; i++ {
		if why = tb.readOnly(i, c); why == "" {
			tb.ref(i, c).put(nil)

			n++
		}
	}

	if n == 0 && why != "" {
		return flash(why, true)
	}

	return nil
}

// changes are the edits as Apply takes them, in the order made, copied: a
// submit runs while the grid takes more.
func (tb *table) changes() []db.Change {
	out := make([]db.Change, len(tb.edits))
	for k, e := range tb.edits {
		out[k] = db.Change{Key: e.key, Del: e.del}
		if !e.del {
			out[k].Set, out[k].Old = maps.Clone(e.set), maps.Clone(e.old)
		}
	}

	return out
}

// submit writes the edits in one transaction; andClose drops the tab once they
// are written.
func (tb *table) submit(andClose bool) tea.Cmd {
	switch {
	case tb.sending:
		return flash("a submit is on its way", false)
	case len(tb.edits) == 0:
		return flash("no changes to submit", false)
	}

	t, changes, sent := tb.t, tb.changes(), slices.Clone(tb.edits)
	tb.sending = true

	return func() tea.Msg { return tableDoneMsg{tb: tb, sent: sent, err: t.Apply(changes), close: andClose} }
}

func (m *Model) onTableDone(msg tableDoneMsg) tea.Cmd {
	tb := msg.tb
	tb.sending = false

	if msg.err != nil {
		var ae *db.ApplyError

		at, hint := "", ""
		if errors.As(msg.err, &ae) && ae.Index >= 0 && ae.Index < len(msg.sent) {
			at = tb.describe(msg.sent[ae.Index]) + ": "
		}

		if ae != nil && ae.Conflict {
			hint = ": u reverts the edit, r then shows the row as it is now"
		}

		return flash(at+errLine(msg.err.Error())+ifElse(tb.db.Kind == db.MariaDB, "", " · nothing was written")+hint, true) // MyISAM has no transactions
	}

	tb.edits = slices.DeleteFunc(tb.edits, func(e *rowEdit) bool { return slices.Contains(msg.sent, e) })
	tb.clampSel()   // the rows added are the database's now
	tb.stale = true // until the page is read again: a row deleted may have given its key to one added
	cmd := tea.Batch(tb.load(tb.offset, false), flash("submitted "+plural(len(msg.sent), "change"), false))

	if i := slices.IndexFunc(m.editors, func(e preview) bool { return e.tbl == tb }); msg.close && i >= 0 {
		return tea.Batch(cmd, m.dropEditor(i))
	}

	return cmd
}

// describe names the row an edit is for, as the gutter numbers it.
func (tb *table) describe(e *rowEdit) string {
	if e.key == "" {
		return "a new row"
	}

	if i := slices.IndexFunc(tb.rows, func(r db.Row) bool { return r.Key == e.key }); i >= 0 {
		return fmt.Sprintf("row %d", tb.offset+i+1)
	}

	return "a row"
}

// tableEdits counts the edits not submitted in every open table, a long
// value's editor not put back among them: no draft keeps either.
func (m *Model) tableEdits() int {
	n := b2i(m.pv.cell != nil && m.pv.dirty())

	for _, e := range slices.Concat(append(slices.Collect(maps.Values(m.waiting)), m.editors)...) { // other workspaces' too
		switch {
		case e.tbl != nil:
			n += len(e.tbl.edits)
		case e.cell != nil && e.dirty() && e.id() != m.pv.id():
			n++
		}
	}

	return n
}

// unsent reports a tab holding what no draft keeps: a table with edits, a
// table value's editor typed in.
func unsent(e preview) bool {
	return e.tbl != nil && len(e.tbl.edits) > 0 || e.cell != nil && e.dirty()
}

func (tb *table) input() *textinput.Model {
	if tb.box == 2 {
		return &tb.order
	}

	return &tb.where
}

// focusBox gives box b the keyboard, 0 the grid.
func (tb *table) focusBox(b int) tea.Cmd {
	tb.where.Blur()
	tb.order.Blur()

	if tb.box = b; b == 0 {
		return nil
	}

	in := tb.input()
	in.CursorEnd()

	return in.Focus()
}

func (tb *table) boxKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc": // back to the grid, the boxes as the page was read
		tb.where.SetValue(tb.applied[0])
		tb.order.SetValue(tb.applied[1])

		return tb.focusBox(0)

	case "enter":
		tb.applied = [2]string{tb.where.Value(), tb.order.Value()}
		tb.l.sel, tb.l.top = 0, 0

		return tea.Batch(tb.focusBox(0), tb.load(0, false))

	case "tab", "shift+tab":
		return tb.focusBox(3 - tb.box)
	}

	in := tb.input()

	var cmd tea.Cmd

	*in, cmd = in.Update(k)

	return cmd
}

// sort orders the rows by column c: ascending, then descending, then as
// the table has them. ORDER BY says it, so the box can take it further.
func (tb *table) sort(c int) tea.Cmd {
	if c >= len(tb.t.Cols) {
		return nil
	}

	name, next := tb.db.Ident(tb.t.Cols[c].Name), ""

	switch tb.applied[1] {
	case name:
		next = name + " desc"
	case name + " desc":
	default:
		next = name
	}

	tb.order.SetValue(next)
	tb.applied[1], tb.l.sel, tb.l.top = next, 0, 0

	return tb.load(0, false)
}

// sorted is the column ORDER BY sorts by as sort put it, and whether
// descending; -1 when it says something else.
func (tb *table) sorted() (int, bool) {
	for c, col := range tb.t.Cols {
		switch tb.applied[1] {
		case tb.db.Ident(col.Name):
			return c, false
		case tb.db.Ident(col.Name) + " desc":
			return c, true
		}
	}

	return -1, false
}

// page reads the page d pages on, when there is one.
func (tb *table) page(d int) tea.Cmd {
	off := tb.offset + d*tablePage
	if off < 0 || d > 0 && !tb.more {
		return nil
	}

	tb.l.sel, tb.l.top = 0, 0

	return tb.load(off, false)
}

// status is the header's word on the page: which rows of how many, ~ for
// the server's estimate, a read running and the edits pending.
func (tb *table) status() string {
	var s string

	switch {
	case !tb.ready:
	case len(tb.rows) == 0:
		s = "no rows"
	default:
		s = fmt.Sprintf("%d–%d of %s%d", tb.offset+1, tb.offset+len(tb.rows), ifElse(tb.exact, "", "~"), tb.total)
	}

	if tb.busy {
		s = strings.TrimPrefix(s+" · loading… esc stops", " · ")
	}

	if a, b := tb.selected(); tb.anchor >= 0 {
		s += " · " + strconv.Itoa(b-a+1) + " selected"
	}

	if n := len(tb.edits); n > 0 {
		s += " · " + strconv.Itoa(n) + " pending"
	}

	return s
}

// column is the cursor's column for the status bar: its name and type, and
// what keeps it from an edit of its own.
func (tb *table) column() string {
	if !tb.ready || tb.col >= len(tb.t.Cols) {
		return ""
	}

	c := tb.t.Cols[tb.col]
	s := c.Name + " " + cmp.Or(c.Type, "any")

	switch {
	case c.PK:
		s += " · primary key"
	case c.Generated:
		s += " · generated"
	}

	return s
}

// shown is a value on one line of a cell, as oneLine has it. Past what a
// cell shows it is cut first, so a long value costs no more than a short one.
func shown(s string) string {
	if len(s) > 4*cellMax {
		s = s[:4*cellMax]
	}

	return oneLine(s)
}

// oneLine is s on one line: line breaks as ↵ without the indentation after
// them, other control characters, which would move the terminal's cursor,
// as �.
func oneLine(s string) string {
	brk := false // a line break came last: the next line's indentation goes

	return strings.Map(func(r rune) rune {
		was := brk
		brk = false

		switch {
		case r == '\n':
			brk = true
			return '↵'

		case was && (r == ' ' || r == '\t' || r == '\r'):
			brk = true
			return -1

		case r == '\t':
			return ' '
		case r == '\r':
			return -1
		case unicode.IsControl(r):
			return '�'
		}

		return r
	}, s)
}

// text is the cell at row i, column c as the grid writes it.
func (tb *table) text(i, c int) string {
	cell, set := tb.value(i, c)

	switch {
	case cell.Null:
		return "NULL"
	case i >= len(tb.rows) && !set:
		return "DEFAULT"
	}

	return shown(cell.Text)
}

// numeric reports a column of numbers by its type, in any of the three
// dialects: its values align right.
func numeric(typ string) bool {
	t := strings.ToLower(typ)

	return slices.ContainsFunc([]string{"int", "real", "float", "double", "num", "dec", "serial", "money"}, func(n string) bool {
		return strings.Contains(t, n)
	})
}

// tableLayout is where the columns stand: the gutter stays, the columns
// after it, " value │" each, scroll sideways as the editor's long lines do,
// cells [left, left+view) of them in view.
type tableLayout struct {
	gutter, digits int   // the gutter's cells, its │ included, and the row numbers'
	xs, ws         []int // each column's first cell past the gutter, and its value's width
	total, view    int   // the cells of every column, and of the part in view
	left           int   // the first of them in view
}

func (tb *table) layout(w int) tableLayout {
	lay := tableLayout{digits: len(strconv.Itoa(max(tb.offset+len(tb.rows), 1)))}
	lay.gutter = lay.digits + 5
	lay.view = max(w-lay.gutter, 1)

	for c, col := range tb.t.Cols {
		cw := ansi.StringWidth(col.Name) + 2 // room for the sort mark
		for i := range tb.count() {
			cw = max(cw, ansi.StringWidth(tb.text(i, c)))
		}

		lay.xs, lay.ws = append(lay.xs, lay.total), append(lay.ws, min(cw, cellMax))
		lay.total += lay.ws[c] + 3
	}

	// No further than the last column, or than the column the end's view
	// starts in: follow scrolls by whole columns.
	end := lay.total - lay.view
	if i := slices.IndexFunc(lay.xs, func(x int) bool { return x >= end }); i >= 0 {
		end = lay.xs[i]
	}

	tb.left = max(min(tb.left, end), 0)
	lay.left = tb.left

	return lay
}

// follow scrolls the cursor's column into view, whole where it fits, by
// whole columns: no column is left cut at the left edge.
func (tb *table) follow(lay tableLayout) {
	if tb.col >= len(lay.xs) {
		return
	}

	x, end := lay.xs[tb.col], lay.xs[tb.col]+lay.ws[tb.col]+3

	switch {
	case x < tb.left:
		tb.left = x
	case end > tb.left+lay.view:
		tb.left = x // wider than the view: its start
		if i := slices.IndexFunc(lay.xs[:tb.col], func(cx int) bool { return cx >= end-lay.view }); i >= 0 {
			tb.left = lay.xs[i]
		}
	}
}

// hbar is the bar under the rows, shown while the columns run past the edge.
func (lay tableLayout) hbar() vbar { return vbar{lay.total, lay.view, lay.left} }

// colAt is the column under cell x of the body, -1 for none.
func (lay tableLayout) colAt(x int) int {
	x += lay.left - lay.gutter

	for c := range lay.xs {
		if x >= lay.left && x >= lay.xs[c] && x < lay.xs[c]+lay.ws[c]+3 {
			return c
		}
	}

	return -1
}

// line is a row of the grid: its gutter, then its cells drawn whole and cut
// to the ones in view, as the editor cuts a long line.
func (lay tableLayout) line(bg color.Color, gutter, cells []seg) string {
	full := row(max(lay.total, lay.left+lay.view), bg, cells)
	return row(lay.gutter, bg, gutter) + ansi.Cut(full, lay.left, lay.left+lay.view)
}

// pad fits s into w cells, truncated with … or padded, on the left when right.
func pad(s string, w int, right bool) string {
	s = ansi.Truncate(s, w, "…")
	if right {
		return blank(w-ansi.StringWidth(s)) + s
	}

	return s + blank(w-ansi.StringWidth(s))
}

func isNumber(s string) bool { _, err := strconv.ParseFloat(s, 64); return err == nil }

func (tb *table) headLine(lay tableLayout) string {
	sc, desc := tb.sorted()
	edge := fg(pal.whitespace)
	gutter := []seg{sg("   "+pad("#", lay.digits, true)+" ", fg(pal.lineNumber)), sg("│", edge)}

	var cells []seg

	for c, col := range tb.t.Cols {
		name := col.Name
		if c == sc {
			name += ifElse(desc, " ↓", " ↑")
		}

		cells = append(cells, sg(" "+pad(name, lay.ws[c], false)+" ", bold), sg("│", edge))
	}

	return lay.line(nil, gutter, cells)
}

// rule is the line under the header; with no rows under it, its joints end
// there.
func (tb *table) rule(lay tableLayout) string {
	joint := ifElse(tb.count() == 0, "┴", "┼")

	var b strings.Builder

	for c := range lay.ws {
		j := joint
		if c == len(lay.ws)-1 { // the last column's border closes the rule
			j = ifElse(tb.count() == 0, "┘", "┤")
		}

		b.WriteString(strings.Repeat("─", lay.ws[c]+2) + j)
	}

	b.WriteString(blank(lay.left + lay.view - lay.total)) // scrolled past the last column

	return fg(pal.whitespace).Render(strings.Repeat("─", lay.gutter-1) + joint + ansi.Cut(b.String(), lay.left, lay.left+lay.view))
}

// rowLine draws row i: its letter, M edited, D to be deleted, A added, its
// number, and its cells, the edited ones in the modified color. NULL and
// DEFAULT are in italics, where a terminal draws no faint text.
func (tb *table) rowLine(m *Model, lay tableLayout, i int) string {
	e := tb.editAt(i)
	base, letter := plain, sg(" ", plain)

	switch {
	case i >= len(tb.rows):
		base, letter = fg(pal.added), sg("A", fg(pal.added))
	case e != nil && e.del:
		// The values struck, not the padding around them: that reads as dashes.
		base, letter = fg(pal.deleted).Strikethrough(true).StrikethroughSpaces(false), sg("D", fg(pal.deleted))
	case e != nil:
		letter = sg("M", fg(pal.modified))
	}

	num, numSt := "", fg(pal.lineNumber)
	if i < len(tb.rows) {
		num = strconv.Itoa(tb.offset + i + 1)
	}

	var bg color.Color

	mark := sg(" ", plain)

	switch {
	case tb.picked(i) && m.focus == onMain:
		bg, mark = pal.selBg, sg("▌", fg(pal.accent))
	case tb.picked(i):
		bg, mark = pal.selUnfocusedBg, sg("▌", fg(pal.accent))
	case i == tb.l.sel:
		bg = pal.hoverBg
	}

	if i == tb.l.sel {
		numSt = fg(pal.lineNumberActive)
	}

	cellBg := pal.selBg
	if m.focus == onMain && tb.box == 0 {
		cellBg = pal.textSelBg
	}

	edge := fg(pal.whitespace)
	gutter := []seg{mark, letter, sg(" "+pad(num, lay.digits, true)+" ", numSt), sg("│", edge)}

	var cells []seg

	for c, col := range tb.t.Cols {
		cell, set := tb.value(i, c)
		st := base

		switch {
		case cell.Null || cell.Blob || i >= len(tb.rows) && !set:
			st = fg(pal.descFg).Italic(true)
		case set && i < len(tb.rows) && (e == nil || !e.del):
			st = fg(pal.modified)
		}

		right := !cell.Null && (numeric(col.Type) || col.Type == "" && isNumber(cell.Text)) // untyped: by what it holds
		text := " " + pad(tb.text(i, c), lay.ws[c], right) + " "

		if i == tb.l.sel && c == tb.col {
			cells = append(cells, sgOwn(text, st.Background(cellBg)))
		} else {
			cells = append(cells, sg(text, st))
		}

		cells = append(cells, sg("│", edge))
	}

	return lay.line(bg, gutter, cells)
}

// gridH is the rows of the grid in a body h rows high, less the bar under
// them while it shows.
func (tb *table) gridH(h int, lay tableLayout) int {
	return max(h-tableHead-b2i(tb.err != "")-b2i(lay.hbar().on()), 1)
}

// body draws the boxes, the error a read gave, the header and the rows.
func (tb *table) body(m *Model, w, h int) []string {
	focused := m.focus == onMain
	bw := w / 2
	where, order := framedBox(&tb.where, bw, focused && tb.box == 1, m.dark), framedBox(&tb.order, w-bw, focused && tb.box == 2, m.dark)
	out := []string{where[0] + order[0], where[1] + order[1], where[2] + order[2]}

	if tb.err != "" {
		out = append(out, row(w, nil, []seg{sg(" "+tb.err, fg(pal.errc))}))
	}

	if !tb.ready || len(tb.t.Cols) == 0 {
		return out
	}

	lay := tb.layout(w - 1)
	if tb.reveal { // the added row's DEFAULTs may widen the columns: the cursor's stays in view
		tb.follow(lay)
		lay.left = tb.left
	}

	out = append(out, tb.headLine(lay), tb.rule(lay))

	n, gh := tb.count(), tb.gridH(h, lay)
	if n == 0 {
		return append(out, row(w, nil, []seg{sg(" No rows", fg(pal.descFg))}))
	}

	tb.l.clamp(n, gh)

	if tb.reveal {
		tb.l.snap(gh)
		tb.reveal = false
	}

	tb.l.bar = m.barState("table")
	out = append(out, tb.l.render(w, gh, n, func(i, _ int) string { return tb.rowLine(m, lay, i) })...)

	if hb := lay.hbar(); hb.on() {
		out = append(out, blank(lay.gutter)+hb.hcells(m.barState("table-h")))
	}

	return out
}

func (tb *table) hints() string {
	switch {
	case tb.box > 0:
		return " ⏎ apply  tab other box  esc back to the rows"
	case tb.t.ReadOnly() && tb.ready:
		return " ↑↓←→ move  s sort  [ ] page  ^f where  y copy  r reload  q close"
	}

	return " ⏎ edit  ⇧↑↓ select  o add  ^d duplicate  d delete  u revert  ^s submit  s sort  [ ] page  ^f where  m menu"
}

func (tb *table) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	if tb.box > 0 {
		return tb.boxKey(k)
	}

	lay := tb.layout(m.pvW())
	n, h, i := tb.count(), tb.gridH(m.pvH(), lay), tb.l.sel
	last := max(len(tb.t.Cols)-1, 0)

	// Up and down move the cursor; with shift they select the rows on the
	// way, as shift selects text in a file.
	moves := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -h, "b": -h, "pgdown": h, "f": h, "space": h, "g": -n, "ctrl+home": -n, "G": n, "ctrl+end": n}
	if d, ok := moves[strings.TrimPrefix(k.String(), "shift+")]; ok {
		switch {
		case !strings.HasPrefix(k.String(), "shift+"):
			tb.anchor = -1
		case tb.anchor < 0:
			tb.anchor = tb.l.sel
		}

		tb.l.move(d, n, h)
		tb.follow(lay)

		return nil
	}

	switch k.String() {
	case "left", "h":
		tb.col = max(tb.col-1, 0)
	case "right", "l":
		tb.col = min(tb.col+1, last)
	case "home", "0":
		tb.col = 0
	case "end", "$":
		tb.col = last
	case "[", "]":
		return tb.page(ifElse(k.String() == "[", -1, 1))
	case "s":
		return tb.sort(tb.col)
	case "ctrl+f":
		return tb.focusBox(1)
	case "enter":
		return m.editCell(tb, i, tb.col)
	case "o":
		return tb.add(-1)
	case "ctrl+d":
		return tb.add(i)
	case "d", "delete":
		return tb.deleteRows()
	case "u", "ctrl+z":
		return tb.revert(tb.col)
	case "r", "ctrl+r":
		return tb.load(tb.offset, len(tb.edits) == 0)
	case "y", "ctrl+c":
		return tb.copy(tb.col)
	case "m":
		return tb.menu(m, -1, 0)
	case "esc": // a read running first, then the selection, then the tab
		switch {
		case tb.cancel():
			return flash("stopped reading: r reads again", false)
		case tb.anchor >= 0:
			tb.anchor = -1
			return nil
		}

		return m.pv.closeAndRefocus(m)

	case "q":
		return m.pv.closeAndRefocus(m)
	}

	tb.follow(lay)

	return nil
}

// copy puts the value at the cursor on the clipboard; rows selected go as
// TSV under the column names, NULL as nothing, for a spreadsheet.
func (tb *table) copy(c int) tea.Cmd {
	a, b := tb.selected()

	switch {
	case a < 0 || b >= tb.count() || c >= len(tb.t.Cols):
		return nil
	case tb.anchor < 0:
		cell, _ := tb.value(a, c)
		if cell.Null {
			return setClipboard("", "NULL: copied an empty string")
		}

		return setClipboard(cell.Text, "copied "+tb.t.Cols[c].Name)
	}

	var out strings.Builder

	w := csv.NewWriter(&out)
	w.Comma = '\t'

	rec := make([]string, len(tb.t.Cols))
	for c, col := range tb.t.Cols {
		rec[c] = col.Name
	}

	_ = w.Write(rec) // a strings.Builder takes every write

	for i := a; i <= b; i++ {
		for c := range rec {
			cell, _ := tb.value(i, c)
			rec[c] = ifElse(cell.Null, "", cell.Text)
		}

		_ = w.Write(rec)
	}

	w.Flush()

	return setClipboard(out.String(), "copied "+plural(b-a+1, "row"))
}

// editCell edits the cell at row i, column c: in the prompt, or in an editor
// tab when the value is long or has a line break, where ^s puts it back.
func (m *Model) editCell(tb *table, i, c int) tea.Cmd {
	if why := tb.readOnly(i, c); why != "" {
		return flash(why, true)
	}

	cell, set := tb.value(i, c)
	ref, col, def := tb.ref(i, c), tb.t.Cols[c], i >= len(tb.rows) && !set

	// The prompt's box turns a tab into a space and drops the other control
	// characters: a value holding one goes to an editor, which keeps them.
	if strings.ContainsFunc(cell.Text, func(r rune) bool { return unicode.IsControl(r) || r == utf8.RuneError }) || len(cell.Text) > longValue {
		row := "new"
		if i < len(tb.rows) {
			row = strconv.Itoa(tb.offset + i + 1)
		}

		p := preview{kind: pvFile, name: fmt.Sprintf("%s.%s:%s", tb.name, col.Name, row), draft: cell.Text, cell: &ref}

		if j := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == p.id() }); j >= 0 {
			return m.showEditor(j) // the cell's editor is open: its text, typed or not
		}

		return m.setPreview(p)
	}

	md := newPrompt(tb.name+"."+col.Name+" · "+cmp.Or(col.Type, "any"), cell.Text, func(_ *Model, v string) tea.Cmd {
		if v == "" && (cell.Null || def) { // a NULL or a DEFAULT looked at, nothing typed: as it was, not ''
			return nil
		}

		if why := ref.put(&v); why != "" {
			return flash(why, true)
		}

		return nil
	})
	md.raw = true

	// Over the cell, as the rename box sits over a symbol: the value where
	// the cell's is, its border and "› " before it, in the editor area.
	lay := tb.layout(m.pvW())
	tb.follow(lay)
	tb.l.snap(tb.gridH(m.pvH(), lay))

	md.x = m.mainX() + lay.gutter + lay.xs[c] - tb.left + 1 - 3
	md.y = m.headH() + m.stripH() + tableHead + b2i(tb.err != "") + i - tb.l.top - 1
	md.area = m.editorArea()
	m.modal = md

	return nil
}

// editorArea is the screen columns of the editor area, which a table's
// boxes keep to.
func (m *Model) editorArea() [2]int { return [2]int{m.mainX(), m.mainX() + m.mainW()} }

// menu opens the table's menu at screen cell (x, y), x < 0 centered, in the
// editor area.
func (tb *table) menu(m *Model, x, y int) tea.Cmd {
	cmd := m.menuOf(tb.items(), x, y)
	m.modal.area = m.editorArea()

	return cmd
}

// putCell is ^s in a cell's editor tab: the text goes back to its table as
// an edit, the tab closes, the table shows again.
func (m *Model) putCell(p *preview) tea.Cmd {
	tb := p.cell.tb

	j := slices.IndexFunc(m.editors, func(e preview) bool { // the table, or the same one opened again since
		return e.tbl == tb || e.tbl != nil && e.tbl.id == tb.id && e.tbl.schema == tb.schema && e.tbl.name == tb.name && e.tbl.db.Target == tb.db.Target
	})
	if j < 0 {
		return flash(tb.name+" is closed: open it again to keep the value", true)
	}

	p.cell.tb = m.editors[j].tbl

	// The editor shows no last line break and reads \r\n as \n: an
	// unchanged value goes back as it was, a changed one keeps its last line
	// break, and its \r\n when every line break was one.
	text, was := p.buf.text(), p.cell.shown.Text
	if strings.HasSuffix(was, "\n") {
		text += "\n"
	}

	switch n := strings.Count(was, "\n"); {
	case text == strings.ReplaceAll(was, "\r\n", "\n"):
		text = was
	case n > 0 && strings.Count(was, "\r\n") == n:
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}

	if why := p.cell.put(&text); why != "" {
		return flash(why, true)
	}

	p.buf.dirty = false

	i := slices.IndexFunc(m.editors, func(e preview) bool { return e.id() == p.id() })
	if i < 0 {
		return m.showEditor(j)
	}

	if j > i {
		j--
	}

	return tea.Batch(m.dropEditor(i), m.showEditor(j))
}

// items are the table's menu, and its part of the command palette.
func (tb *table) items() []item {
	i, c := tb.l.sel, tb.col
	a, b := tb.selected()
	rows := ifElse(tb.anchor >= 0, " in the Selected Rows", "")

	read := []item{
		{label: ifElse(tb.anchor >= 0, "Copy Rows as TSV", "Copy Value"), hint: "y", run: func(*Model) tea.Cmd { return tb.copy(c) }},
		{label: "Sort by Column", hint: "s", run: func(*Model) tea.Cmd { return tb.sort(c) }},
		{label: "Filter…", hint: "^f", run: func(*Model) tea.Cmd { return tb.focusBox(1) }},
		{label: "Reload", hint: "r", run: func(*Model) tea.Cmd { return tb.load(tb.offset, len(tb.edits) == 0) }},
		{label: "Open Console", run: func(*Model) tea.Cmd { return console(dbSource{id: tb.id, db: tb.db}) }},
	}

	if tb.ready && !tb.exact { // the total is the server's estimate
		read = append(read, item{label: "Count Rows", run: func(*Model) tea.Cmd { return tb.countRows() }})
	}

	if tb.t.ReadOnly() { // nothing to edit
		return read
	}

	return append([]item{
		{label: "Edit Value", hint: "⏎", run: func(m *Model) tea.Cmd { return m.editCell(tb, i, c) }},
		{label: "Set NULL" + rows, run: func(*Model) tea.Cmd { return tb.setNull(c) }},
		{label: "Add Row", hint: "o", run: func(*Model) tea.Cmd { return tb.add(-1) }},
		{label: "Duplicate Row", hint: "^d", run: func(*Model) tea.Cmd { return tb.add(i) }},
		{label: ifElse(tb.deleted(a, b), "Keep", "Delete") + ifElse(a < b, " Rows", " Row"), hint: "d", run: func(*Model) tea.Cmd { return tb.deleteRows() }},
		{label: "Revert" + rows, hint: "u", run: func(*Model) tea.Cmd { return tb.revert(c) }},
		{label: "Revert All", run: func(*Model) tea.Cmd {
			if why := tb.frozen(); why != "" {
				return flash(why, true)
			}

			tb.discard()

			return nil
		}},
		separator(),
		{label: "Submit Changes", hint: "^s", run: func(*Model) tea.Cmd { return tb.submit(false) }},
		{label: "Show Pending SQL", run: tb.showSQL},
		separator(),
	}, read...)
}

// showSQL lists the statements a submit would run, each wrapped to the
// editor area in full; picking one copies it.
func (tb *table) showSQL(m *Model) tea.Cmd {
	if len(tb.edits) == 0 {
		return flash("no changes pending", false)
	}

	var all []string
	for _, c := range tb.changes() {
		all = append(all, tb.t.Statement(c)+";")
	}

	items := []item{{label: "Copy All", run: func(*Model) tea.Cmd { return setClipboard(strings.Join(all, "\n"), "copied the pending SQL") }}, separator()}

	for _, s := range all {
		// Within the menu's borders and margins, less the indentation of the
		// lines after the first: those go plain, the keyboard past them.
		lines := strings.Split(ansi.Wrap(oneLine(s), max(m.mainW()-8, 10), ""), "\n")
		items = append(items, item{label: lines[0], run: func(*Model) tea.Cmd { return setClipboard(s, "copied the statement") }})

		for _, l := range lines[1:] {
			items = append(items, item{label: "  " + l, styled: true})
		}
	}

	m.modal = newMenu("Pending SQL", -1, 0, items...)
	m.modal.area = m.editorArea()

	return nil
}

func (tb *table) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo, w, h := msg.Mouse(), m.pvW()+1, m.pvH()
	top := tableHead + b2i(tb.err != "") // the first row's line
	lay := tb.layout(w - 1)
	n, gh := tb.count(), tb.gridH(h, lay)

	_, click := msg.(tea.MouseClickMsg)

	switch {
	case y == top+gh && lay.hbar().on() && x >= lay.gutter: // the bar under the rows
		bx := x - lay.gutter
		m.overBar = barPoint{"table-h", bx}

		if click && mo.Button == tea.MouseLeft {
			return m.hbarClick("table-h", bx, mo.X-bx, func() vbar { return tb.layout(m.pvW()).hbar() }, func(left int) tea.Cmd {
				tb.left = left
				return nil
			})
		}

		return nil

	case x >= w-1 && y >= top && y < top+gh && n > gh: // the scrollbar
		m.overBar = barPoint{"table", y - top}
		if click && mo.Button == tea.MouseLeft {
			return m.barClick("table", y-top, mo.Y-(y-top), func() vbar { return vbar{n, gh, tb.l.top} }, func(t int) tea.Cmd {
				tb.l.top = t
				return nil
			})
		}

		return nil

	case !click:
		if _, wheel := msg.(tea.MouseWheelMsg); !wheel {
			return nil
		}

		if dx, ok := wheelX(mo); ok { // sideways, as the editor pans
			tb.left = max(tb.left+dx, 0)
			return nil
		}

		tb.l.wheel(wheelDelta(mo), n, gh)

		return nil

	case y >= 0 && y < 3: // the boxes
		return tb.focusBox(ifElse(x < w/2, 1, 2))
	case y == top-2 && mo.Button == tea.MouseLeft: // the header: sort
		if c := lay.colAt(x); c >= 0 {
			return tb.sort(c)
		}

		return nil
	}

	i := tb.l.at(y-top, n)
	if y < top || i < 0 {
		return nil
	}

	switch {
	case mo.Mod&tea.ModShift != 0: // the rows from the cursor to it, where the terminal passes shift on
		if tb.anchor < 0 {
			tb.anchor = tb.l.sel
		}

	case mo.Button == tea.MouseRight && tb.picked(i): // the menu acts on the selection
	default:
		tb.anchor = -1
	}

	tb.l.sel = i
	tb.focusBox(0)

	if c := lay.colAt(x); c >= 0 {
		tb.col = c
	}

	switch {
	case mo.Button == tea.MouseRight:
		return tb.menu(m, mo.X, mo.Y)
	case m.clicks.double(mo.X, mo.Y):
		return m.editCell(tb, i, tb.col)
	}

	return nil
}
