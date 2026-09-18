package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/xseman/pando/internal/lsp"
)

const storeGo = "package a\n\ntype Store struct {\n\titems []int\n}\n\nfunc (s *Store) Total() int {\n\treturn 0\n}\n\nfunc tax() int { return 1 }\nfunc total() int { return 2 }\n"

// storeSymbols is what gopls says about storeGo.
var storeSymbols = []lsp.Symbol{
	{Name: "Store", Kind: 23, Line: 2, Col: 5, EndCol: 10, From: 2, To: 4},
	{Name: "items", Container: "Store", Kind: 8, Line: 3, Col: 1, EndCol: 6, From: 3, To: 3},
	{Name: "Total", Container: "Store", Kind: 6, Line: 6, Col: 16, EndCol: 21, From: 6, To: 8},
	{Name: "tax", Kind: 12, Line: 10, Col: 5, EndCol: 8, From: 10, To: 10},
	{Name: "total", Kind: 12, Line: 11, Col: 5, EndCol: 10, From: 11, To: 11},
}

func TestGotoSymbol(t *testing.T) {
	m, path := editorModel(t, "store.go", storeGo)
	m.pv.setCursor(m, pos{7, 1})

	if m.key(keyMsg("ctrl+shift+o")) == nil {
		t.Fatal("ctrl+shift+o asks the server")
	}

	if !slices.ContainsFunc(m.commands(), func(it item) bool { return commandID(it.label) == "editor.goToSymbol" }) {
		t.Fatal("Editor: Go to Symbol… is missing from the commands")
	}

	open := func() {
		t.Helper()
		m.flash("asking gopls…", false)
		m.Update(symbolsMsg{path: path, query: "@", syms: storeSymbols})

		if m.modal == nil || m.modal.prefix != "@" || m.msg != "" {
			t.Fatalf("no symbol picker, or the asking flash stayed: %q", m.msg)
		}
	}
	open()
	// The file's symbols in document order, the one around the cursor selected.
	md := m.modal
	if got, want := labels(m), []string{"symbols (5)", "Store", "items", "Total", "tax", "total"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	if md.l.sel != 3 || m.pv.cur != (pos{7, 1}) {
		t.Fatalf("sel = %d, cur = %+v: opening moves nothing", md.l.sel, m.pv.cur)
	}
	// "@:" groups them by kind, groups by name.
	press(m, ":")

	want := []string{"fields (1)", "items", "functions (2)", "tax", "total", "methods (1)", "Total", "structs (1)", "Store"}
	if got := labels(m); !slices.Equal(got, want) {
		t.Fatalf("grouped = %q, want %q", got, want)
	}
	// The editor follows the selection: items is selected, and its name.
	if a, b, ok := m.pv.selection(); !ok || a != (pos{3, 4}) || b != (pos{3, 9}) {
		t.Fatalf("follows the selection: %+v..%+v (%v)", a, b, ok)
	}

	press(m, "t", "o")

	if got := labels(m); slices.Contains(got, "fields (1)") || !slices.Contains(got, "functions (1)") || !slices.Contains(got, "structs (1)") {
		t.Fatalf("filtered groups = %q", got)
	}

	checkWidths(t, m)
	// esc puts the editor back where it was.
	press(m, "esc")

	if m.modal != nil || m.pv.cur != (pos{7, 1}) || m.pv.anchor != nil {
		t.Fatalf("esc: modal=%v cur=%+v anchor=%v", m.modal != nil, m.pv.cur, m.pv.anchor)
	}
	// ↓ goes to the next symbol, ⏎ stays there.
	open()
	press(m, "down", "enter")

	if a, b, ok := m.pv.selection(); m.modal != nil || !ok || a != (pos{10, 5}) || b != (pos{10, 8}) {
		t.Fatalf("enter: modal=%v %+v..%+v", m.modal != nil, a, b)
	}
	// Deleting the @ goes back to Go to File, and the editor back where it was.
	m.pv.setCursor(m, pos{0, 0})
	open()
	press(m, "down")
	press(m, "backspace")

	if m.modal != nil || m.pv.cur != (pos{0, 0}) {
		t.Fatalf("backspace over @: modal=%v cur=%+v", m.modal != nil, m.pv.cur)
	}
	// An answer for a file that is no longer open is dropped.
	m.Update(symbolsMsg{path: path + ".old", query: "@", syms: storeSymbols})

	if m.modal != nil {
		t.Fatal("symbols of another file opened a picker")
	}
}

func TestMarkdownSymbols(t *testing.T) {
	text := "# Title\n\ntext\n\n## Install\n\n```sh\n# not a heading\n```\n\n### Linux ##\n\n# Other\n"
	syms := mdSymbols(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))

	var got []string
	for _, s := range syms {
		got = append(got, s.Name+"<"+s.Container)
	}

	if want := []string{"Title<", "Install<Title", "Linux<Install", "Other<"}; !slices.Equal(got, want) {
		t.Fatalf("headings = %q, want %q", got, want)
	}

	if s := syms[1]; s.Line != 4 || s.Col != 3 || s.EndCol != 10 || s.From != 4 || s.To != 11 || lsp.KindName(s.Kind) != "strings" {
		t.Fatalf("Install = %+v", s)
	}

	if syms[0].To != 11 || syms[3].To != 12 {
		t.Fatalf("sections: Title to %d, Other to %d", syms[0].To, syms[3].To)
	}

	// No server needed, and "@" in quick open lands in the same picker.
	m, _ := editorModel(t, "notes.md", text)
	m.pv.setCursor(m, pos{11, 0})
	m.quickOpen(indexMsg{m.ws, []string{"notes.md"}, true})
	press(m, "@")

	if m.modal == nil || m.modal.prefix != "@" || m.modal.input.Value() != "@" {
		t.Fatalf("@ in quick open: %+v", m.modal)
	}

	if rows := labels(m); len(rows) != 5 || m.modal.l.sel != 3 {
		t.Fatalf("rows %q, sel %d (Linux holds the cursor)", rows, m.modal.l.sel)
	}
}
