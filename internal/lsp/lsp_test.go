package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readMsg reads one framed JSON-RPC message.
func readMsg(t *testing.T, r *bufio.Reader) map[string]any {
	t.Helper()

	n := 0

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil
		}

		if line = strings.TrimSpace(line); line == "" {
			break
		}

		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil
	}

	var msg map[string]any
	if err := json.Unmarshal(buf, &msg); err != nil {
		t.Errorf("server got invalid json: %v", err)
	}

	return msg
}

func writeMsg(w io.Writer, msg any) {
	b, _ := json.Marshal(msg)
	// The fake server writes from its own goroutine: a closed pipe ends it.
	_, _ = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

// mustWrite writes a file and the directories above it, or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	_ = os.MkdirAll(filepath.Dir(path), 0o755) // a failing WriteFile names the path
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestClient(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	mustWrite(t, file, "package a\n")

	cr, sw := io.Pipe() // server → client
	sr, cw := io.Pipe() // client → server
	answered := make(chan bool, 1)
	initOpts := make(chan any, 1)
	opened := make(chan string, 8)

	go func() { // a minimal server
		r := bufio.NewReader(sr)
		for {
			msg := readMsg(t, r)
			if msg == nil {
				return
			}

			id, method := msg["id"], msg["method"]
			switch method {
			case "initialize":
				// t.Errorf, not Fatalf: this is the server goroutine.
				params, ok := msg["params"].(map[string]any)
				if !ok {
					t.Errorf("initialize params = %#v, want a JSON object", msg["params"])
				}

				initOpts <- params["initializationOptions"] // a nil map indexes to nil

				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"capabilities": map[string]any{}}})
				// A string id, as TypeScript 7's tsc sends; it waits for the answer.
				writeMsg(sw, map[string]any{
					"jsonrpc": "2.0", "id": "ts1", "method": "workspace/configuration",
					"params": map[string]any{"items": []any{map[string]any{"section": "typescript"}, map[string]any{"section": "editor"}}},
				})

			case "textDocument/definition": // a single Location
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
					"uri": uri(file), "range": map[string]any{
						"start": map[string]any{"line": 4, "character": 2},
						"end":   map[string]any{"line": 4, "character": 7},
					},
				}})

			case "textDocument/references": // LocationLinks
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{
					map[string]any{"targetUri": uri(file), "targetSelectionRange": map[string]any{
						"start": map[string]any{"line": 1, "character": 0},
						"end":   map[string]any{"line": 1, "character": 3},
					}},
					map[string]any{"uri": uri(file), "range": map[string]any{
						"start": map[string]any{"line": 9, "character": 1},
						"end":   map[string]any{"line": 9, "character": 4},
					}},
				}})

			case "textDocument/rename": // the new name goes into the file
				name, _ := msg["params"].(map[string]any)["newName"].(string)
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
					"changes": map[string]any{uri(file): []any{map[string]any{
						"range":   map[string]any{"start": map[string]any{"line": 0, "character": 8}, "end": map[string]any{"line": 0, "character": 9}},
						"newText": name,
					}}},
				}})

			case "textDocument/hover":
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
					"contents": map[string]any{"kind": "markdown", "value": "```go\nfunc A()\n```\n\nA does it."},
				}})

			case "textDocument/signatureHelp":
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
					"signatures": []any{map[string]any{
						"label":      "A(x int, y string)",
						"parameters": []any{map[string]any{"label": "x int"}, map[string]any{"label": []int{9, 17}}},
					}},
					"activeParameter": 1,
				}})

			case "textDocument/formatting":
				opts, _ := msg["params"].(map[string]any)["options"].(map[string]any)
				writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{map[string]any{
					"range":   map[string]any{"start": map[string]any{"line": 1, "character": 0}, "end": map[string]any{"line": 1, "character": 2}},
					"newText": fmt.Sprintf("%v%v", opts["tabSize"], opts["insertSpaces"]),
				}}})

			case "textDocument/didOpen":
				doc, _ := msg["params"].(map[string]any)["textDocument"].(map[string]any)

				text, _ := doc["text"].(string)
				select {
				case opened <- "open " + text:
				default:
				}

			case "textDocument/didChange":
				p, _ := msg["params"].(map[string]any)
				doc, _ := p["textDocument"].(map[string]any)
				changes, _ := p["contentChanges"].([]any)
				change, _ := changes[0].(map[string]any)

				opened <- fmt.Sprintf("change %v %v", doc["version"], change["text"])

			case "textDocument/didClose":
				opened <- "close"

			case nil: // our answer to the server's own request
				if msg["id"] == "ts1" {
					res, _ := msg["result"].([]any)
					answered <- len(res) == 2
				}
			}
		}
	}()

	c := newClient(cw, cr, nil)
	if err := c.initialize(dir, map[string]any{"tsserver": map[string]any{"path": "/ts/lib/tsserver.js"}}); err != nil {
		t.Fatal(err)
	}

	if got := fmt.Sprint(<-initOpts); got != "map[tsserver:map[path:/ts/lib/tsserver.js]]" {
		t.Fatalf("initializationOptions = %s", got)
	}

	select {
	case ok := <-answered:
		if !ok {
			t.Fatal("the server's request was answered with the wrong number of items")
		}

	case <-time.After(5 * time.Second):
		t.Fatal("the server's request went unanswered")
	}

	locs, err := c.Definition(file, 0, 0)
	if err != nil || len(locs) != 1 || locs[0] != (Location{file, 4, 2, 4, 7}) {
		t.Fatalf("definition = %+v (%v)", locs, err)
	}

	locs, err = c.References(file, 0, 0)
	if err != nil || len(locs) != 2 || locs[0].Line != 1 || locs[1].Line != 9 {
		t.Fatalf("references = %+v (%v)", locs, err)
	}
	// The first request opens the file from disk, once; Sync sends the
	// editor's text as the next version, and nothing when it is the same.
	if got := <-opened; got != "open package a\n" || len(opened) > 0 {
		t.Fatalf("the requests opened %q, then %d more", got, len(opened))
	}

	for _, text := range []string{"package overlay\n", "package overlay\n", "package a\n"} {
		if err := c.Sync(file, text); err != nil {
			t.Fatal(err)
		}
	}

	c.Forget(file)

	for _, want := range []string{"change 2 package overlay\n", "change 3 package a\n", "close"} {
		if got := <-opened; got != want {
			t.Fatalf("sent %q, want %q", got, want)
		}
	}

	if _, err := c.Definition(file, 0, 0); err != nil || <-opened != "open package a\n" {
		t.Fatalf("a forgotten file opens again from disk (%v)", err)
	}

	if err := c.Rename(file, 0, 8, "b"); err != nil {
		t.Fatal(err)
	}

	if b, _ := os.ReadFile(file); string(b) != "package b\n" {
		t.Fatalf("rename wrote %q", b)
	}

	if h, err := c.Hover(file, 0, 0); err != nil || h != "```go\nfunc A()\n```\n\nA does it." {
		t.Fatalf("hover = %q (%v)", h, err)
	}

	if s, err := c.SignatureHelp(file, 0, 0); err != nil || s == nil || s.Label[s.From:s.To] != "y string" {
		t.Fatalf("signature help = %+v (%v)", s, err)
	}

	got, err := c.Format(file, "package b\n  x\n", 2, true) // the server's edit, applied to the text sent
	if err != nil || got != "package b\n2truex\n" {
		t.Fatalf("format = %q (%v)", got, err)
	}
}

// parseHover reads every shape a Hover's contents come in.
func TestParseHover(t *testing.T) {
	for raw, want := range map[string]string{
		`{"contents":{"kind":"markdown","value":"**a**"}}`:             "**a**",
		`{"contents":{"kind":"plaintext","value":"a  b"}}`:             "```\na  b\n```",
		`{"contents":"just text"}`:                                     "just text",
		`{"contents":{"language":"go","value":"var x int"}}`:           "```go\nvar x int\n```",
		`{"contents":[{"language":"ts","value":"x: number"},"the x"]}`: "```ts\nx: number\n```\n\nthe x",
		`{"contents":[]}`: "",
		`null`:            "",
		`{"contents":{"kind":"markdown","value":"  padded  "},"range":{}}`: "padded",
	} {
		if got := parseHover(json.RawMessage(raw)); got != want {
			t.Errorf("parseHover(%s) = %q, want %q", raw, got, want)
		}
	}
}

// parseSignature picks the active signature and finds its active parameter
// by name or by UTF-16 offsets.
func TestParseSignature(t *testing.T) {
	for _, c := range []struct {
		raw, label, active, doc string
	}{
		{`{"signatures":[{"label":"f(a, b)","parameters":[{"label":"a"},{"label":"b"}]}],"activeParameter":1}`, "f(a, b)", "b", ""},
		{`{"signatures":[{"label":"f(😀, b)","parameters":[{"label":[2,4]},{"label":[6,7]}],"activeParameter":0}],"activeParameter":1}`, "f(😀, b)", "😀", ""},
		{`{"signatures":[{"label":"g()","documentation":{"kind":"markdown","value":"G."}},{"label":"h(x)","documentation":"H.","parameters":[{"label":"x"}]}],"activeSignature":1}`, "h(x)", "x", "H."},
		{`{"signatures":[{"label":"f()","parameters":[]}],"activeParameter":3}`, "f()", "", ""},
		{`{"signatures":[{"label":"f(a)","parameters":[{"label":[3,1]}]}]}`, "f(a)", "", ""},
	} {
		s := parseSignature(json.RawMessage(c.raw))
		if s == nil {
			t.Errorf("%s: no signature", c.raw)
			continue
		}

		if rs := []rune(s.Label); s.Label != c.label || string(rs[s.From:s.To]) != c.active || s.Doc != c.doc {
			t.Errorf("%s: %+v, active %q", c.raw, s, string(rs[s.From:s.To]))
		}
	}

	for _, raw := range []string{`null`, `{"signatures":[]}`, `{`} {
		if s := parseSignature(json.RawMessage(raw)); s != nil {
			t.Errorf("%s: %+v, want none", raw, s)
		}
	}
}

// FuzzParseSignature checks the active parameter always lies in the label,
// whatever offsets or names a server sends.
func FuzzParseSignature(f *testing.F) {
	f.Add(`{"signatures":[{"label":"f(a, b)","parameters":[{"label":"a"},{"label":[3,4]}]}],"activeParameter":1}`)
	f.Add(`{"signatures":[{"label":"é","parameters":[{"label":"\u00a9"}]}],"activeSignature":-4}`)

	f.Fuzz(func(t *testing.T, raw string) {
		s := parseSignature(json.RawMessage(raw))
		if s == nil {
			return
		}

		if n := len([]rune(s.Label)); s.From < 0 || s.From > s.To || s.To > n {
			t.Fatalf("%s: active parameter %d..%d of %d runes", raw, s.From, s.To, n)
		}
	})
}

func TestColumns(t *testing.T) {
	line := "héllo 😀x"
	if got := UTF16Col(line, 7); got != 8 {
		t.Errorf("UTF16Col = %d, want 8", got)
	}

	if got := RuneCol(line, 8); got != 7 {
		t.Errorf("RuneCol = %d, want 7", got)
	}

	if got := LanguageOf("a/b.tsx"); got != "typescriptreact" {
		t.Errorf("LanguageOf = %q", got)
	}
}

func TestApplyWorkspaceEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")

	const src = "package a\n\nfunc λx() int {\n\treturn 1\n}\n"
	mustWrite(t, path, src)
	// Two edits at once, and a non-ASCII rune before the second so its column
	// counts in UTF-16 units, not in bytes.
	edit := fmt.Sprintf(`{"changes":{%q:[
		{"range":{"start":{"line":3,"character":8},"end":{"line":3,"character":9}},"newText":"42"},
		{"range":{"start":{"line":2,"character":5},"end":{"line":2,"character":7}},"newText":"Sum"}]}}`,
		"file://"+path)
	if err := applyWorkspaceEdit(json.RawMessage(edit)); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	want := "package a\n\nfunc Sum() int {\n\treturn 42\n}\n"
	if string(got) != want {
		t.Errorf("applyWorkspaceEdit\ngot  %q\nwant %q", got, want)
	}
}

func TestParseActions(t *testing.T) {
	acts := parseActions(json.RawMessage(`[
		{"title":"Organize Imports","kind":"source.organizeImports","edit":{"changes":{}}},
		{"title":"Extract function","command":{"command":"gopls.extract","arguments":[1]}},
		{"title":"Old style","command":"gopls.test","arguments":[2]},
		{"title":"Needs resolve","data":{"x":1}},
		{"noTitle":true}]`))
	if len(acts) != 4 {
		t.Fatalf("got %d actions, want 4", len(acts))
	}

	if acts[0].edit == nil || acts[0].Kind != "source.organizeImports" {
		t.Errorf("literal action: %+v", acts[0])
	}

	if acts[1].cmd == nil || acts[1].cmd.Command != "gopls.extract" {
		t.Errorf("command action: %+v", acts[1])
	}

	if acts[2].cmd == nil || acts[2].cmd.Command != "gopls.test" || acts[2].raw != nil {
		t.Errorf("bare command: %+v", acts[2])
	}

	if acts[3].raw == nil {
		t.Error("an action with data must keep its body for codeAction/resolve")
	}

	if parseActions(json.RawMessage(`null`)) != nil {
		t.Error("null is no actions")
	}
}

func TestApplyWorkspaceEditFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	mustWrite(t, src, "package a\n\nfunc A() {}\n")
	// What "extract declarations to new file" sends: create a file, fill it,
	// and cut the declaration out of the old one.
	edit := fmt.Sprintf(`{"documentChanges":[
		{"kind":"create","uri":%q},
		{"textDocument":{"uri":%q},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"package a\n\nfunc A() {}\n"}]},
		{"textDocument":{"uri":%q},"edits":[{"range":{"start":{"line":2,"character":0},"end":{"line":3,"character":0}},"newText":""}]}]}`,
		"file://"+filepath.Join(dir, "b.go"), "file://"+filepath.Join(dir, "b.go"), "file://"+src)
	if err := applyWorkspaceEdit(json.RawMessage(edit)); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(filepath.Join(dir, "b.go")); err != nil || string(b) != "package a\n\nfunc A() {}\n" {
		t.Fatalf("new file = %q, %v", b, err)
	}

	if b, _ := os.ReadFile(src); string(b) != "package a\n\n" {
		t.Fatalf("old file = %q", b)
	}

	// Rename and delete, the other two operations servers send.
	ren := fmt.Sprintf(`{"documentChanges":[{"kind":"rename","oldUri":%q,"newUri":%q}]}`,
		"file://"+filepath.Join(dir, "b.go"), "file://"+filepath.Join(dir, "c.go"))
	if err := applyWorkspaceEdit(json.RawMessage(ren)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "c.go")); err != nil {
		t.Fatalf("rename: %v", err)
	}

	del := fmt.Sprintf(`{"documentChanges":[{"kind":"delete","uri":%q}]}`, "file://"+filepath.Join(dir, "c.go"))
	if err := applyWorkspaceEdit(json.RawMessage(del)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "c.go")); err == nil {
		t.Fatal("delete left the file behind")
	}
}

func TestParseCompletion(t *testing.T) {
	raw := `{"isIncomplete":false,"items":[
		{"label":"Tax","sortText":"b"},
		{"label":"Total","detail":"func() int","sortText":"a","insertTextFormat":2,
		 "textEdit":{"range":{"start":{"line":0,"character":2},"end":{"line":0,"character":4}},"newText":"Total()$0"}},
		{"label":"fmt.Println","insertText":"Println(${1:a}, $2)","insertTextFormat":2,"filterText":"Println"},
		{"label":"rune","textEdit":{"insert":{"start":{"line":0,"character":5},"end":{"line":0,"character":6}},
		 "replace":{"start":{"line":0,"character":5},"end":{"line":0,"character":9}},"newText":"rune"}}]}`
	items := parseCompletion(json.RawMessage(raw))

	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	// sortText orders them, the label stands in where there is none.
	if want := []string{"Total", "Tax", "fmt.Println", "rune"}; !slices.Equal(labels, want) {
		t.Fatalf("order = %v, want %v", labels, want)
	}

	if it := items[0]; it.Text != "Total()" || it.Col != 2 || it.Detail != "func() int" || it.Filter != "Total" {
		t.Fatalf("a snippet edit = %+v", it)
	}

	if it := items[1]; it.Text != "Tax" || it.Col != -1 {
		t.Fatalf("a bare label = %+v", it)
	}

	if it := items[2]; it.Text != "Println(a, )" || it.Filter != "Println" {
		t.Fatalf("snippet tab stops = %+v", it)
	}

	if it := items[3]; it.Col != 5 {
		t.Fatalf("an insert/replace edit inserts: %+v", it)
	}

	if items := parseCompletion(json.RawMessage(`[{"label":"x"}]`)); len(items) != 1 || items[0].Text != "x" {
		t.Fatalf("a bare array = %+v", items)
	}

	if items := parseCompletion(json.RawMessage(`null`)); len(items) != 0 {
		t.Fatalf("null = %+v", items)
	}
}

func TestParseSymbols(t *testing.T) {
	// A DocumentSymbol tree: children follow their parent, named after it.
	tree := `[{"name":"Store","kind":23,"range":{"start":{"line":2,"character":0},"end":{"line":5,"character":1}},
		"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}},
		"children":[{"name":"items","kind":8,"range":{"start":{"line":3,"character":1},"end":{"line":3,"character":12}},
			"selectionRange":{"start":{"line":3,"character":1},"end":{"line":3,"character":6}}}]},
		{"name":"main","kind":12,"range":{"start":{"line":7,"character":0},"end":{"line":9,"character":1}},
		"selectionRange":{"start":{"line":7,"character":5},"end":{"line":7,"character":9}}}]`
	got := parseSymbols(json.RawMessage(tree))

	want := []Symbol{
		{Name: "Store", Kind: 23, Line: 2, Col: 5, EndCol: 10, From: 2, To: 5},
		{Name: "items", Container: "Store", Kind: 8, Line: 3, Col: 1, EndCol: 6, From: 3, To: 3},
		{Name: "main", Kind: 12, Line: 7, Col: 5, EndCol: 9, From: 7, To: 9},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("tree = %+v", got)
	}
	// SymbolInformation, out of order: sorted by position.
	flat := `[{"name":"b","kind":6,"containerName":"T","location":{"uri":"file:///a.go","range":{"start":{"line":9,"character":0},"end":{"line":11,"character":1}}}},
		{"name":"a","kind":12,"location":{"uri":"file:///a.go","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":4}}}}]`

	got = parseSymbols(json.RawMessage(flat))
	if len(got) != 2 || got[0].Name != "a" || got[1] != (Symbol{Name: "b", Container: "T", Kind: 6, Line: 9, From: 9, To: 11}) {
		t.Fatalf("flat = %+v", got)
	}

	if parseSymbols(json.RawMessage("null")) != nil {
		t.Fatal("null is no symbols")
	}

	if KindName(12) != "functions" || KindName(23) != "structs" || KindName(99) != "properties" {
		t.Fatalf("kind names: %s %s %s", KindName(12), KindName(23), KindName(99))
	}
}

// TestDiagnostics hands the sink what a server publishes on its own, and
// what a server that pulls answers after each sync, its severity 0 an error.
func TestDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.ts")
	mustWrite(t, file, "let x\n")

	for _, pull := range []bool{false, true} {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()

		sent := make(chan []any, 1)
		diag := []any{
			map[string]any{
				"range":    map[string]any{"start": map[string]any{"line": 0, "character": 4}, "end": map[string]any{"line": 0, "character": 5}},
				"severity": 2, "message": "unused x", "source": "ts",
			},
			map[string]any{
				"range":   map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 0}},
				"message": "no severity",
			},
		}

		go func() {
			r := bufio.NewReader(sr)
			for {
				msg := readMsg(t, r)
				if msg == nil {
					return
				}

				switch msg["method"] {
				case "initialize":
					caps := map[string]any{}
					if pull {
						caps["diagnosticProvider"] = map[string]any{"interFileDependencies": false}
					}

					writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": msg["id"], "result": map[string]any{"capabilities": caps}})

				case "textDocument/didOpen":
					if !pull {
						writeMsg(sw, map[string]any{
							"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
							"params": map[string]any{"uri": uri(file), "diagnostics": diag},
						})
					}

				case "textDocument/diagnostic":
					writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": msg["id"], "result": map[string]any{"kind": "full", "items": diag}})

				case "textDocument/codeAction":
					ctx, _ := msg["params"].(map[string]any)["context"].(map[string]any)
					ds, _ := ctx["diagnostics"].([]any)

					sent <- ds

					writeMsg(sw, map[string]any{"jsonrpc": "2.0", "id": msg["id"], "result": []any{}})
				}
			}
		}()

		got := make(chan []Diagnostic, 1)
		c := newClient(cw, cr, func(path string, ds []Diagnostic) {
			if path == file {
				got <- ds
			}
		})

		if err := c.initialize(dir, nil); err != nil {
			t.Fatal(err)
		}

		if err := c.Sync(file, "let x\n"); err != nil {
			t.Fatal(err)
		}

		select {
		case ds := <-got:
			want := []Diagnostic{{0, 4, 0, 5, 2, "unused x", "ts"}, {0, 0, 0, 0, 1, "no severity", ""}}
			if !slices.Equal(ds, want) {
				t.Fatalf("pull %v: diagnostics %+v, want %+v", pull, ds, want)
			}

		case <-time.After(5 * time.Second):
			t.Fatalf("pull %v: no diagnostics", pull)
		}

		// A code action request carries the diagnostics under its range, as
		// the server wrote them, for their quick fixes.
		if _, err := c.Actions(file, 0, 4, 0, 4); err != nil {
			t.Fatal(err)
		}

		if ds := <-sent; len(ds) != 1 || fmt.Sprint(ds[0]) != fmt.Sprint(diag[0]) {
			t.Fatalf("pull %v: the code action's diagnostics %v", pull, ds)
		}

		_ = cw.Close()
		_ = sw.Close()
	}
}
