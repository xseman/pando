package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestFormatDocument formats through a tool that needs no installing: sed.
func TestFormatDocument(t *testing.T) {
	m, path := editorModel(t, "a.go", "package  a\n")
	m.st.Settings.Format = map[string][]string{"go": {"sed", "s/  */ /g"}}

	// Type, then format: the tool's text replaces the buffer, the cursor stays.
	press(m, "ctrl+e")
	m.pv.setCursor(m, pos{0, 8})

	if cmd := m.pv.formatDoc(m); cmd == nil {
		t.Fatal("formatDoc returned no flash")
	}

	if got := m.pv.buf.text(); got != "package a" {
		t.Fatalf("formatted %q", got)
	}

	if m.pv.cur.line != 0 || m.pv.cur.col != 8 {
		t.Fatalf("cursor moved to %+v", m.pv.cur)
	}

	if !m.pv.dirty() {
		t.Fatal("formatting left the buffer clean")
	}
	// One undo takes the file back to what was typed.
	m.pv.history(m, true)

	if got := m.pv.buf.text(); got != "package  a" {
		t.Fatalf("after undo %q", got)
	}

	// A tool that fails changes nothing and says why.
	m.st.Settings.Format = map[string][]string{"go": {"sh", "-c", "echo 'a.go:1: broken' >&2; exit 1"}}

	cmd := m.pv.formatDoc(m)
	if got := m.pv.buf.text(); got != "package  a" {
		t.Fatalf("a failed formatter rewrote the file: %q", got)
	}

	if msg, _ := cmd().(flashMsg); !strings.Contains(msg.text, "broken") || !msg.err {
		t.Fatalf("flash = %+v", msg)
	}

	// $FILE reaches the tool; the file has no formatter of its own.
	m.st.Settings.Format = map[string][]string{"go": {"sh", "-c", `cat >/dev/null; basename "$FILE"`}}
	m.pv.formatDoc(m)

	if got := m.pv.buf.text(); got != "a.go" {
		t.Fatalf("$FILE = %q", got)
	}

	// Save runs the formatter first when format_on_save is on.
	m.st.Settings.Format = map[string][]string{"go": {"sed", "s/  */ /g"}}
	m.st.Settings.FmtSave = true
	m.pv.setText(m, "package   a\n")
	m.pv.save(m, false)

	if got, _ := os.ReadFile(path); string(got) != "package a\n" {
		t.Fatalf("save wrote %q", got)
	}
}

func TestFormatCommand(t *testing.T) {
	m := testModel(t)
	if argv := m.formatCommand("/tmp/x.go"); len(argv) != 1 || argv[0] != "gofmt" {
		t.Fatalf("go default = %v", argv)
	}

	m.st.Settings.Format = map[string][]string{"go": {"gofumpt"}, "ts": {"prettier"}}
	if argv := m.formatCommand("/tmp/x.go"); argv[0] != "gofumpt" {
		t.Fatalf("config beats the default: %v", argv)
	}

	if argv := m.formatCommand("/tmp/x.ts"); argv[0] != "prettier" {
		t.Fatalf("by extension: %v", argv)
	}

	if argv := m.formatCommand("/tmp/x.zig"); argv != nil {
		t.Fatalf("unconfigured language = %v", argv)
	}

	if err := noFormatter("/tmp/x.zig"); !strings.Contains(err.Error(), "[format]") {
		t.Fatalf("the hint does not name the table: %v", err)
	}
}

// Formatting does not move the reader: the scroll and the cursor stay where
// they were, whatever the tool rewrote further down the file.
func TestFormatKeepsView(t *testing.T) {
	var b strings.Builder
	b.WriteString("package a\n")

	for i := range 200 {
		fmt.Fprintf(&b, "\nfunc F%d() int {\n\treturn %d\n}\n", i, i)
	}

	m, _ := editorModel(t, "big.go", b.String())
	m.pv.editRaw(m, pos{399, 0}, pos{399, 0}, "   ") // "   \treturn 99", gofmt will tidy it

	m.pv.top, m.pv.cur = 380, pos{399, 4}
	if err := m.pv.format(m); err != nil {
		t.Fatal(err)
	}

	if m.pv.top != 380 || m.pv.cur != (pos{399, 4}) {
		t.Fatalf("format scrolled to top=%d cur=%+v", m.pv.top, m.pv.cur)
	}

	if got := string(m.pv.buf.line(399)); got != "\treturn 99" {
		t.Fatalf("line 399 = %q", got)
	}

	if len(m.pv.plain) != len(m.pv.buf.lines) {
		t.Fatalf("view has %d lines, buffer %d", len(m.pv.plain), len(m.pv.buf.lines))
	}
}
