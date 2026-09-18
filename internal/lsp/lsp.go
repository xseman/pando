// Package lsp speaks just enough of the Language Server Protocol over stdio
// to jump to a definition, list references and run a code action: initialize,
// didOpen and a handful of requests. No dependencies, no diagnostics.
package lsp

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Location is a place in a file. Columns are UTF-16 code units, as LSP counts
// them; RuneCol converts them for a terminal.
type Location struct {
	Path                       string
	Line, Col, EndLine, EndCol int
}

// Client is a running language server: one process, one reader goroutine, and
// the requests waiting for it. Every method is safe for concurrent use.
type Client struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	wmu    sync.Mutex // one writer at a time
	mu     sync.Mutex
	nextID int
	pend   map[int]chan result
	opened map[string]bool
	over   map[string]string // unsaved editor text, by path
	dead   error
}

type result struct {
	body json.RawMessage
	err  error
}

// Start runs a language server in root and initializes it. opts are its
// initializationOptions, nil for none.
func Start(root string, argv []string, opts map[string]any) (*Client, error) {
	if len(argv) == 0 {
		return nil, errors.New("no language server configured")
	}

	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("%s is not installed", argv[0])
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := newClient(in, out)

	c.cmd = cmd
	if err := c.initialize(root, opts); err != nil {
		c.Close()
		return nil, fmt.Errorf("%s: %w", argv[0], err)
	}

	return c, nil
}

// newClient wires a client to a server's pipes; tests use it directly.
func newClient(in io.WriteCloser, out io.Reader) *Client {
	c := &Client{in: in, pend: map[int]chan result{}, opened: map[string]bool{}, over: map[string]string{}}
	go c.read(out)

	return c
}

func (c *Client) initialize(root string, opts map[string]any) error {
	_, err := c.call("initialize", map[string]any{
		"initializationOptions": opts,
		"processId":             os.Getpid(),
		"rootUri":               uri(root),
		"workspaceFolders":      []any{map[string]any{"uri": uri(root), "name": filepath.Base(root)}},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{},
				"definition":      map[string]any{"linkSupport": true},
				"references":      map[string]any{},
				"rename":          map[string]any{},
				"documentSymbol":  map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"completion": map[string]any{
					"completionItem": map[string]any{"snippetSupport": true, "insertReplaceSupport": true},
					"contextSupport": true,
				},
				"codeAction": map[string]any{
					"dataSupport":    true,
					"resolveSupport": map[string]any{"properties": []string{"edit"}},
					"codeActionLiteralSupport": map[string]any{
						"codeActionKind": map[string]any{"valueSet": []string{"quickfix", "refactor", "source"}},
					},
				},
			},
			"workspace": map[string]any{
				"workspaceFolders": true, "configuration": true,
				"applyEdit":     true,
				"workspaceEdit": map[string]any{"documentChanges": true},
			},
		},
	})
	if err != nil {
		return err
	}

	return c.notify("initialized", map[string]any{})
}

// Close shuts the server down; a server that ignores it is killed.
func (c *Client) Close() {
	// Best effort throughout: a server that has already gone needs no goodbye.
	_ = c.notify("shutdown", nil)
	_ = c.notify("exit", nil)

	_ = c.in.Close()
	if c.cmd != nil {
		done := make(chan struct{})

		go func() { _ = c.cmd.Wait(); close(done) }() // the exit status is of no use

		select {
		case <-done:
		case <-time.After(time.Second):
			_ = c.cmd.Process.Kill() // it ignored exit, and may be gone already
		}
	}
}

// Definition is where the symbol at line:col is defined.
func (c *Client) Definition(path string, line, col int) ([]Location, error) {
	return c.locations("textDocument/definition", path, line, col, nil)
}

// References are every use of the symbol at line:col, its declaration included.
func (c *Client) References(path string, line, col int) ([]Location, error) {
	return c.locations("textDocument/references", path, line, col, map[string]any{"includeDeclaration": true})
}

// Action is one code action a server offers: a quick fix, a refactoring or a
// source action. The raw body goes back to the server to resolve or run it.
type Action struct {
	Title string
	Kind  string
	edit  json.RawMessage
	raw   json.RawMessage
	cmd   *command
}

type command struct {
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Actions are the code actions for a range, VS Code's ⌃. menu.
func (c *Client) Actions(path string, line, col, endLine, endCol int) ([]Action, error) {
	if err := c.open(path); err != nil {
		return nil, err
	}

	raw, err := c.call("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri(path)},
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": col},
			"end":   map[string]any{"line": endLine, "character": endCol},
		},
		"context": map[string]any{"diagnostics": []any{}},
	})
	if err != nil {
		return nil, err
	}

	return parseActions(raw), nil
}

// Rename gives the symbol at line:col a new name everywhere the server knows
// it, writing the edits straight to the files.
func (c *Client) Rename(path string, line, col int, name string) error {
	if err := c.open(path); err != nil {
		return err
	}

	raw, err := c.call("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri(path)},
		"position":     map[string]any{"line": line, "character": col},
		"newName":      name,
	})
	if err != nil {
		return err
	}

	if string(raw) == "null" || len(raw) == 0 {
		return errors.New("nothing to rename here")
	}

	return applyWorkspaceEdit(raw)
}

// Run applies an action: the edit it came with, the edit a resolve fills in,
// or the command that makes the server send one back as workspace/applyEdit.
func (c *Client) Run(a Action) error {
	if a.edit == nil && a.raw != nil {
		if raw, err := c.call("codeAction/resolve", a.raw); err == nil {
			if list := parseActions(append(append([]byte("["), raw...), ']')); len(list) == 1 {
				a.edit = list[0].edit
			}
		}
	}

	if a.edit != nil {
		return applyWorkspaceEdit(a.edit)
	}

	if a.cmd != nil {
		_, err := c.call("workspace/executeCommand", map[string]any{
			"command": a.cmd.Command, "arguments": a.cmd.Arguments,
		})

		return err
	}

	return errors.New("the server offered nothing to apply")
}

// parseActions reads both shapes a server may send: a CodeAction with an edit
// or data to resolve, and a bare Command. Anything it cannot read is no
// actions rather than an error, so it returns no error at all.
func parseActions(raw json.RawMessage) []Action {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil // null, or something we do not understand
	}

	var out []Action

	for _, body := range list {
		var a struct {
			Title     string          `json:"title"`
			Kind      string          `json:"kind"`
			Edit      json.RawMessage `json:"edit"`
			Data      json.RawMessage `json:"data"`
			Command   json.RawMessage `json:"command"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(body, &a) != nil || a.Title == "" {
			continue
		}

		act := Action{Title: a.Title, Kind: a.Kind, edit: a.Edit}
		if a.Data != nil || a.Edit == nil {
			act.raw = body
		}

		switch {
		case len(a.Command) > 0 && a.Command[0] == '"': // a bare Command
			var name string

			_ = json.Unmarshal(a.Command, &name) // a JSON string: it cannot fail
			act.cmd, act.raw = &command{name, a.Arguments}, nil

		case len(a.Command) > 0:
			var cmd command
			if json.Unmarshal(a.Command, &cmd) == nil {
				act.cmd = &cmd
			}
		}

		out = append(out, act)
	}

	return out
}

type textEdit struct {
	Range struct {
		Start, End struct{ Line, Character int }
	} `json:"range"`
	NewText string `json:"newText"`
}

// applyWorkspaceEdit writes a server's edits straight to the files, in the
// order it sent them: create, rename and delete a file, or splice text into
// one. ponytail: no undo; git is the undo.
func applyWorkspaceEdit(raw json.RawMessage) error {
	var we struct {
		Changes         map[string][]textEdit `json:"changes"`
		DocumentChanges []struct {
			Kind    string `json:"kind"` // "" is a text edit
			URI     string `json:"uri"`
			OldURI  string `json:"oldUri"`
			NewURI  string `json:"newUri"`
			Options struct {
				Overwrite      bool `json:"overwrite"`
				IgnoreIfExists bool `json:"ignoreIfExists"`
			} `json:"options"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []textEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(raw, &we); err != nil {
		return err
	}

	n := 0

	for u, edits := range we.Changes {
		path, err := fromURI(u)
		if err != nil {
			continue
		}

		if err := applyEdits(path, edits); err != nil {
			return err
		}

		n++
	}

	for _, dc := range we.DocumentChanges {
		var err error

		switch dc.Kind {
		case "create":
			err = createFile(dc.URI, dc.Options.Overwrite, dc.Options.IgnoreIfExists)
		case "rename":
			err = renameFile(dc.OldURI, dc.NewURI)
		case "delete":
			err = deleteFile(dc.URI)
		default:
			var path string
			if path, err = fromURI(dc.TextDocument.URI); err == nil {
				err = applyEdits(path, dc.Edits)
			}
		}

		if err != nil {
			return err
		}

		n++
	}

	if n == 0 {
		return errors.New("the action changed nothing")
	}

	return nil
}

func createFile(uri string, overwrite, ignore bool) error {
	path, err := fromURI(uri)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil && !overwrite {
		if ignore {
			return nil
		}

		return fmt.Errorf("%s already exists", filepath.Base(path))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, nil, 0o644)
}

func renameFile(from, to string) error {
	src, err := fromURI(from)
	if err != nil {
		return err
	}

	dst, err := fromURI(to)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	return os.Rename(src, dst)
}

func deleteFile(uri string) error {
	path, err := fromURI(uri)
	if err != nil {
		return err
	}

	return os.Remove(path)
}

// applyEdits splices edits into a file, last one first so earlier offsets hold.
func applyEdits(path string, edits []textEdit) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	st, err := os.Stat(path)
	if err != nil {
		return err
	}

	text := string(b)
	slices.SortStableFunc(edits, func(a, b textEdit) int {
		return cmp.Or(b.Range.Start.Line-a.Range.Start.Line, b.Range.Start.Character-a.Range.Start.Character)
	})

	lines := strings.Split(text, "\n")
	for _, e := range edits {
		a := offsetOf(lines, e.Range.Start.Line, e.Range.Start.Character)

		z := offsetOf(lines, e.Range.End.Line, e.Range.End.Character)
		if a > z {
			a, z = z, a
		}

		text = text[:a] + e.NewText + text[z:]
	}

	return os.WriteFile(path, []byte(text), st.Mode().Perm())
}

// offsetOf is the byte offset of an LSP position in a file split on newlines.
func offsetOf(lines []string, line, u16 int) int {
	off, total := 0, 0

	for i, l := range lines {
		if i == line {
			off = total + len(string([]rune(l)[:RuneCol(l, u16)]))
		}

		total += len(l) + 1
	}

	if line >= len(lines) {
		return max(total-1, 0)
	}

	return off
}

func (c *Client) locations(method, path string, line, col int, ctx map[string]any) ([]Location, error) {
	if err := c.open(path); err != nil {
		return nil, err
	}

	params := map[string]any{
		"textDocument": map[string]any{"uri": uri(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if ctx != nil {
		params["context"] = ctx
	}

	raw, err := c.call(method, params)
	if err != nil {
		return nil, err
	}

	return parseLocations(raw), nil
}

// Item is one completion a server offers. Text replaces the line from Col
// (a UTF-16 column, as the server counts) to the cursor; Col is -1 when the
// server left the range to the client, which then replaces the word typed so far.
type Item struct {
	Label, Detail, Text string
	Filter              string // what the typed word is matched against
	Col                 int
	sort                string
}

// Completion asks what can be typed at line:col. trigger is the character that
// asked for it ("." after a value), "" when the user did.
func (c *Client) Completion(path string, line, col int, trigger string) ([]Item, error) {
	if err := c.open(path); err != nil {
		return nil, err
	}

	ctx := map[string]any{"triggerKind": 1}
	if trigger != "" {
		ctx = map[string]any{"triggerKind": 2, "triggerCharacter": trigger}
	}

	raw, err := c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri(path)},
		"position":     map[string]any{"line": line, "character": col},
		"context":      ctx,
	})
	if err != nil {
		return nil, err
	}

	return parseCompletion(raw), nil
}

var snippetTab = regexp.MustCompile(`\$\{\d+:([^}]*)\}|\$\{\d+\}|\$\d+`)

// parseCompletion reads a CompletionList or a bare item array. Snippets lose
// their tab stops, keeping the placeholder text: pando has no snippet mode.
// Anything it cannot read is no completions rather than an error.
func parseCompletion(raw json.RawMessage) []Item {
	type rng struct{ Start struct{ Character int } }

	type item struct {
		Label            string `json:"label"`
		Detail           string `json:"detail"`
		SortText         string `json:"sortText"`
		FilterText       string `json:"filterText"`
		InsertText       string `json:"insertText"`
		InsertTextFormat int    `json:"insertTextFormat"`
		TextEdit         *struct {
			Range   *rng   `json:"range"`
			Insert  *rng   `json:"insert"`
			NewText string `json:"newText"`
		} `json:"textEdit"`
	}

	var list struct {
		Items []item `json:"items"`
	}
	if json.Unmarshal(raw, &list.Items) != nil && json.Unmarshal(raw, &list) != nil {
		return nil // null, or something we do not understand
	}

	out := make([]Item, 0, len(list.Items))
	for _, it := range list.Items {
		o := Item{Label: it.Label, Detail: it.Detail, Text: it.InsertText, Col: -1, Filter: it.FilterText, sort: it.SortText}
		if e := it.TextEdit; e != nil {
			o.Text = e.NewText
			if r := cmp.Or(e.Range, e.Insert); r != nil {
				o.Col = r.Start.Character
			}
		}

		if o.Text == "" {
			o.Text = it.Label
		}

		if it.InsertTextFormat == 2 {
			o.Text = snippetTab.ReplaceAllString(o.Text, "$1")
		}

		o.Filter = cmp.Or(o.Filter, it.Label)
		o.sort = cmp.Or(o.sort, it.Label)
		out = append(out, o)
	}

	slices.SortStableFunc(out, func(a, b Item) int { return strings.Compare(a.sort, b.sort) })

	return out
}

// Overlay hands the server the editor's unsaved text for a file, so answers
// match what is on screen; "" goes back to what is on disk.
func (c *Client) Overlay(path, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if text == "" {
		delete(c.over, path)
		return
	}

	c.over[path] = text
}

// open tells the server about a file, re-sending it when it is already open.
func (c *Client) open(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if text, ok := c.over[path]; ok {
		b = []byte(text)
	}
	c.mu.Unlock()

	doc := map[string]any{"uri": uri(path)}

	c.mu.Lock()
	was := c.opened[path]
	c.opened[path] = true
	c.mu.Unlock()

	if was {
		// Best effort: if the pipe is gone, the didOpen below says so.
		_ = c.notify("textDocument/didClose", map[string]any{"textDocument": doc})
	}

	lang := LanguageOf(path)
	if lang == "" { // an unknown extension stands in for its language id
		lang = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}

	return c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri(path), "languageId": lang, "version": 1, "text": string(b),
	}})
}

func (c *Client) call(method string, params any) (json.RawMessage, error) {
	// ponytail: one ceiling for every request; executeCommand used to get 60 s.
	const timeout = 30 * time.Second

	c.mu.Lock()
	if c.dead != nil {
		defer c.mu.Unlock()
		return nil, c.dead
	}

	c.nextID++
	id := c.nextID
	ch := make(chan result, 1)
	c.pend[id] = ch
	c.mu.Unlock()

	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.mu.Lock()
		delete(c.pend, id) // nothing was sent: no reply will ever arrive
		c.mu.Unlock()

		return nil, err
	}

	select {
	case r := <-ch:
		return r.body, r.err
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pend, id)
		c.mu.Unlock()

		return nil, fmt.Errorf("%s timed out", method)
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *Client) send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)

	return err
}

// read dispatches replies to their callers and answers the server's own
// requests, which some servers wait for before they work.
func (c *Client) read(out io.Reader) {
	r := bufio.NewReader(out)

	for {
		n := 0

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				c.fail(err)
				return
			}

			if line = strings.TrimSpace(line); line == "" {
				break
			}

			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				n, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}

		if n <= 0 {
			continue
		}

		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			c.fail(err)
			return
		}

		var msg struct {
			ID     json.RawMessage `json:"id"` // a number from pando, a string from some servers
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(buf, &msg) != nil || len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue // a notification: diagnostics, progress, logs
		}

		if msg.Method != "" { // a request from the server
			var res any

			switch msg.Method {
			case "workspace/configuration": // no settings, one answer per item asked
				var p struct {
					Items []any `json:"items"`
				}

				_ = json.Unmarshal(msg.Params, &p) // no items: an empty list

				items := make([]any, len(p.Items))
				for i := range items {
					items[i] = map[string]any{}
				}

				res = items

			case "workspace/applyEdit": // what a code action's command sends back
				var p struct {
					Edit json.RawMessage `json:"edit"`
				}

				err := json.Unmarshal(msg.Params, &p)
				if err == nil {
					err = applyWorkspaceEdit(p.Edit)
				}

				res = map[string]any{"applied": err == nil}

			case "window/showDocument": // pando has no browser to send it to
				res = map[string]any{"success": false}
			}
			// Best effort: a write that fails means the next read fails too.
			_ = c.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": res})

			continue
		}

		res := result{body: msg.Result}
		if msg.Error != nil {
			res.err = errors.New(msg.Error.Message)
		}

		id, _ := strconv.Atoi(string(msg.ID))

		c.mu.Lock()
		ch := c.pend[id]
		delete(c.pend, id)
		c.mu.Unlock()

		if ch != nil {
			ch <- res
		}
	}
}

// fail wakes every caller when the server goes away.
func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dead == nil {
		c.dead = fmt.Errorf("language server stopped: %w", err)
	}

	for id, ch := range c.pend {
		ch <- result{err: c.dead}

		delete(c.pend, id)
	}
}

// parseLocations accepts everything servers return: one Location, a list of
// them, or LocationLinks. Anything it cannot read is no locations rather than
// an error.
func parseLocations(raw json.RawMessage) []Location {
	type rng struct {
		Start, End struct{ Line, Character int }
	}

	type loc struct {
		URI                  string `json:"uri"`
		Range                rng    `json:"range"`
		TargetURI            string `json:"targetUri"`
		TargetSelectionRange rng    `json:"targetSelectionRange"`
	}

	var list []loc
	if json.Unmarshal(raw, &list) != nil {
		var one loc
		if json.Unmarshal(raw, &one) != nil {
			return nil // null, or something we do not understand
		}

		list = []loc{one}
	}

	var out []Location

	for _, l := range list {
		u, r := l.URI, l.Range
		if u == "" {
			u, r = l.TargetURI, l.TargetSelectionRange
		}

		path, err := fromURI(u)
		if err != nil {
			continue
		}

		out = append(out, Location{path, r.Start.Line, r.Start.Character, r.End.Line, r.End.Character})
	}

	return out
}

func uri(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

func fromURI(u string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "file" {
		return "", fmt.Errorf("not a file uri: %q", u)
	}

	return parsed.Path, nil
}

// Symbol is one entry of a file's outline. Line, Col and EndCol are its name
// (UTF-16 columns, as LSP counts); From and To are the lines it spans.
type Symbol struct {
	Name, Container   string
	Kind              int // LSP SymbolKind
	Line, Col, EndCol int
	From, To          int
}

// Symbols lists the symbols of a file in document order, nested ones after
// their parent.
func (c *Client) Symbols(path string) ([]Symbol, error) {
	if err := c.open(path); err != nil {
		return nil, err
	}

	raw, err := c.call("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri(path)},
	})
	if err != nil {
		return nil, err
	}

	return parseSymbols(raw), nil
}

// parseSymbols reads either answer a server may give: a DocumentSymbol tree
// or a flat SymbolInformation list.
func parseSymbols(raw json.RawMessage) []Symbol {
	type rng struct {
		Start, End struct{ Line, Character int }
	}

	type sym struct {
		Name           string `json:"name"`
		Kind           int    `json:"kind"`
		Range          *rng   `json:"range"`
		SelectionRange *rng   `json:"selectionRange"`
		Children       []sym  `json:"children"`
		ContainerName  string `json:"containerName"`
		Location       struct {
			Range rng `json:"range"`
		} `json:"location"`
	}

	var list []sym
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}

	var (
		out  []Symbol
		walk func(list []sym, parent string)
	)

	walk = func(list []sym, parent string) {
		for _, s := range list {
			if s.SelectionRange == nil { // SymbolInformation
				r := s.Location.Range
				out = append(out, Symbol{
					Name: s.Name, Container: s.ContainerName, Kind: s.Kind,
					Line: r.Start.Line, Col: r.Start.Character, EndCol: r.Start.Character, From: r.Start.Line, To: r.End.Line,
				})

				continue
			}

			sel := s.SelectionRange

			o := Symbol{
				Name: s.Name, Container: parent, Kind: s.Kind,
				Line: sel.Start.Line, Col: sel.Start.Character, EndCol: sel.Start.Character,
			}
			if sel.End.Line == sel.Start.Line {
				o.EndCol = sel.End.Character
			}

			o.From, o.To = o.Line, o.Line
			if s.Range != nil {
				o.From, o.To = s.Range.Start.Line, s.Range.End.Line
			}

			out = append(out, o)

			walk(s.Children, s.Name)
		}
	}
	walk(list, "")
	// SymbolInformation carries no order of its own.
	slices.SortStableFunc(out, func(a, b Symbol) int { return cmp.Or(a.Line-b.Line, a.Col-b.Col) })

	return out
}

// kindNames are VS Code's group names for the LSP symbol kinds, by number.
var kindNames = []string{
	"", "files", "modules", "namespaces", "packages", "classes",
	"methods", "properties", "fields", "constructors", "enumerations", "interfaces",
	"functions", "variables", "constants", "strings", "numbers", "booleans", "arrays",
	"objects", "keys", "null", "enumeration members", "structs", "events", "operators",
	"type parameters",
}

// KindName names a symbol kind as a group, "properties" for one it does not know.
func KindName(kind int) string {
	if kind > 0 && kind < len(kindNames) {
		return kindNames[kind]
	}

	return "properties"
}

// LanguageOf is the LSP language id of a file, "" when pando knows none.
func LanguageOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp":
		return "cpp"
	}

	return ""
}

// UTF16Col converts a rune column into the UTF-16 column LSP counts in.
func UTF16Col(line string, runeCol int) int {
	rs := []rune(line)
	return len(utf16.Encode(rs[:min(max(runeCol, 0), len(rs))]))
}

// RuneCol is UTF16Col backwards.
func RuneCol(line string, u16 int) int {
	rs := []rune(line)
	for i := range rs {
		if len(utf16.Encode(rs[:i])) >= u16 {
			return i
		}
	}

	return len(rs)
}
