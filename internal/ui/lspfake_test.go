package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// fakeLSPEnv makes the test binary a language server instead (TestMain):
// a test points [lsp] at os.Args[0] with it set. It answers hover with A's
// doc, signature help inside A( with A's parameters, and formatting by
// trimming each line's end and turning leading tabs into spaces when asked.
const fakeLSPEnv = "PANDO_FAKE_LSP"

func fakeLSP() {
	r, docs := bufio.NewReader(os.Stdin), map[string]string{}

	reply := func(id json.RawMessage, result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Printf("Content-Length: %d\r\n\r\n%s", len(b), b)
	}

	for {
		n := 0

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
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
			return
		}

		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
				Position struct{ Line, Character int } `json:"position"`
				Options  struct {
					TabSize      int  `json:"tabSize"`
					InsertSpaces bool `json:"insertSpaces"`
				} `json:"options"`
			} `json:"params"`
		}

		_ = json.Unmarshal(buf, &msg) // a test's own client sends nothing else

		text := docs[msg.Params.TextDocument.URI]
		lines := strings.Split(text, "\n")

		switch msg.Method {
		case "exit":
			return
		case "textDocument/didOpen":
			docs[msg.Params.TextDocument.URI] = msg.Params.TextDocument.Text
		case "textDocument/didChange":
			docs[msg.Params.TextDocument.URI] = msg.Params.ContentChanges[0].Text
		case "textDocument/hover":
			reply(msg.ID, map[string]any{"contents": map[string]any{"kind": "markdown", "value": "```go\nfunc A(x int, y int)\n```\n\nA adds them."}})

		case "textDocument/signatureHelp":
			p := msg.Params.Position
			before := ""

			if p.Line < len(lines) {
				before = string([]rune(lines[p.Line])[:min(p.Character, len([]rune(lines[p.Line])))])
			}

			i := strings.LastIndex(before, "A(")
			if i < 0 || strings.Contains(before[i:], ")") {
				reply(msg.ID, nil)
				continue
			}

			reply(msg.ID, map[string]any{
				"signatures":      []any{map[string]any{"label": "A(x int, y int)", "documentation": "A adds them.", "parameters": []any{map[string]any{"label": "x int"}, map[string]any{"label": "y int"}}}},
				"activeParameter": strings.Count(before[i:], ","),
			})

		case "textDocument/formatting":
			out := make([]string, len(lines))
			for i, l := range lines {
				l = strings.TrimRight(l, " \t")
				if msg.Params.Options.InsertSpaces {
					trimmed := strings.TrimLeft(l, "\t")
					l = strings.Repeat(" ", msg.Params.Options.TabSize*(len(l)-len(trimmed))) + trimmed
				}

				out[i] = l
			}

			reply(msg.ID, []any{map[string]any{
				"range":   map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": len(lines), "character": 0}},
				"newText": strings.Join(out, "\n"),
			}})

		default:
			if msg.Method == "initialize" { // what a server says on stderr, for the Output
				fmt.Fprintln(os.Stderr, "fake server ready")
			}

			if len(msg.ID) > 0 { // initialize, shutdown, anything asked: an answer of nothing
				reply(msg.ID, map[string]any{"capabilities": map[string]any{}})
			}
		}
	}
}
