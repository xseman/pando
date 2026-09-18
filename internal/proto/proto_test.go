package proto

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// mustJSON marshals v the way the wire does and fails the test if it cannot.
func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}

	return string(b)
}

// decodeTOML runs doc through the real TOML decoder, the path config.toml
// takes, and returns the `left` key it produced.
func decodeTOML(t *testing.T, doc string) (Columns, error) {
	t.Helper()

	var v struct {
		Left Columns `toml:"left"`
	}

	err := toml.Unmarshal([]byte(doc), &v)

	return v.Left, err
}

// views builds a column with no explicit width.
func views(v ...string) Column { return Column{Views: v} }

// columnShapes are every input Columns accepts, in both spellings: js is the
// JSON text, tm the TOML value text for the same shape.
var columnShapes = []struct {
	name string
	js   string
	tm   string
	want Columns
}{
	{"empty list", `[]`, `[]`, nil},
	{"flat views", `["files","git"]`, `["files", "git"]`, Columns{views("files", "git")}},
	{"flat one view", `["files"]`, `["files"]`, Columns{views("files")}},
	{
		"column objects",
		`[{"views":["files","git"],"width":22},{"views":["agents"]}]`,
		`[{ views = ["files", "git"], width = 22 }, { views = ["agents"] }]`,
		Columns{Column{Views: []string{"files", "git"}, Width: 22}, views("agents")},
	},
	{"column no views", `[{"width":5}]`, `[{ width = 5 }]`, Columns{Column{Width: 5}}},
	{"column empty object", `[{}]`, `[{}]`, Columns{Column{}}},
	{"column empty views", `[{"views":[]}]`, `[{ views = [] }]`, Columns{Column{Views: []string{}}}},
	{"unknown key ignored", `[{"views":["files"],"bogus":1}]`, `[{ views = ["files"], bogus = 1 }]`, Columns{views("files")}},
}

func TestColumnsUnmarshalJSON(t *testing.T) {
	// null is a JSON-only shape: TOML has no null, so it is not in the table.
	var c Columns
	if err := json.Unmarshal([]byte(`null`), &c); err != nil || c != nil {
		t.Fatalf("null: got %#v, %v; want nil, nil", c, err)
	}

	for _, tc := range columnShapes {
		t.Run(tc.name, func(t *testing.T) {
			var got Columns
			if err := json.Unmarshal([]byte(tc.js), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.js, err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unmarshal %s: got %#v, want %#v", tc.js, got, tc.want)
			}
		})
	}
}

// TestColumnsUnmarshalJSONNullView pins a quirk of the flat-list branch: a
// null element decodes into a string, so it survives as an empty view name
// rather than erroring. Callers must tolerate "" in Views.
func TestColumnsUnmarshalJSONNullView(t *testing.T) {
	var c Columns
	if err := json.Unmarshal([]byte(`["files",null]`), &c); err != nil {
		t.Fatalf("unmarshal null view: %v", err)
	}

	want := Columns{views("files", "")}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("null view: got %#v, want %#v", c, want)
	}
}

// columnErrors are shapes neither branch accepts. Every one must come back as
// an error, never a panic and never a silent nil.
var columnErrors = []struct{ name, in string }{
	{"string", `"files"`},
	{"object", `{"views":["files"]}`},
	{"bool", `true`},
	{"number", `3`},
	{"numbers", `[1,2]`},
	{"bools", `[true]`},
	{"nested list", `[[]]`},
	{"views not a list", `[{"views":"files"}]`},
	{"views not strings", `[{"views":[1]}]`},
	{"width not a number", `[{"width":"wide"}]`},
	{"mixed views then column", `["files",{"views":["git"]}]`},
	{"mixed column then views", `[{"views":["git"]},"files"]`},
	{"truncated", `[{"views":`},
}

func TestColumnsUnmarshalJSONErrors(t *testing.T) {
	for _, tc := range columnErrors {
		t.Run(tc.name, func(t *testing.T) {
			var c Columns
			if err := json.Unmarshal([]byte(tc.in), &c); err == nil {
				t.Fatalf("unmarshal %s: got %#v, want an error", tc.in, c)
			}
		})
	}
}

// TestColumnsUnmarshalJSONPartial documents that a failed decode still writes
// through to the receiver: `[1]` leaves one empty column behind. Anything
// calling UnmarshalJSON must check the error before reading the value.
func TestColumnsUnmarshalJSONPartial(t *testing.T) {
	c := Columns{views("files")}
	if err := c.UnmarshalJSON([]byte(`[1]`)); err == nil {
		t.Fatal("unmarshal [1]: want an error")
	}

	if !reflect.DeepEqual(c, Columns{Column{}}) {
		t.Fatalf("partial decode: got %#v, want one empty column", c)
	}
}

func TestColumnsUnmarshalTOML(t *testing.T) {
	for _, tc := range columnShapes {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeTOML(t, "left = "+tc.tm)
			if err != nil {
				t.Fatalf("decode %s: %v", tc.tm, err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decode %s: got %#v, want %#v", tc.tm, got, tc.want)
			}
		})
	}
}

// TestColumnsUnmarshalTOMLTables covers the array-of-tables spelling, which
// TOML allows for the same value and JSON has no equivalent of.
func TestColumnsUnmarshalTOMLTables(t *testing.T) {
	got, err := decodeTOML(t, "[[left]]\nviews = [\"files\"]\nwidth = 22\n\n[[left]]\nviews = [\"agents\"]\n")
	if err != nil {
		t.Fatalf("decode array of tables: %v", err)
	}

	want := Columns{Column{Views: []string{"files"}, Width: 22}, views("agents")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decode array of tables: got %#v, want %#v", got, want)
	}
}

func TestColumnsUnmarshalTOMLErrors(t *testing.T) {
	// A TOML document can hold values JSON has no shape for; they must fail
	// the same way the malformed JSON does.
	docs := []struct{ name, doc string }{
		{"integer", `left = 5`},
		{"string", `left = "files"`},
		{"datetime", `left = 1979-05-27T07:32:00Z`},
		{"nan", `left = nan`}, // json.Marshal refuses NaN: the "columns: " wrap
		{"inf", `left = inf`},
		{"numbers", `left = [1, 2]`},
		{"mixed", `left = ["files", { views = ["git"] }]`},
		{"nested list", `left = [[]]`},
		{"width not a number", `left = [{ width = "wide" }]`},
	}
	for _, tc := range docs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeTOML(t, tc.doc); err == nil {
				t.Fatalf("decode %s: want an error", tc.doc)
			}
		})
	}
}

// TestColumnsUnmarshalTOMLUnencodable reaches the json.Marshal error branch
// directly, with a value no TOML document can produce.
func TestColumnsUnmarshalTOMLUnencodable(t *testing.T) {
	var c Columns

	err := c.UnmarshalTOML([]any{make(chan int)})
	if err == nil {
		t.Fatal("unmarshal a channel: want an error")
	}

	if !strings.HasPrefix(err.Error(), "columns: ") {
		t.Fatalf("unmarshal a channel: got %q, want a columns: prefix", err)
	}
}

func TestDir(t *testing.T) {
	t.Run("runtime dir wins", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
		t.Setenv("PANDO_RUNTIME_DIR", "/tmp/gvt/run")

		if got := Dir(); got != "/tmp/gvt/run" {
			t.Fatalf("Dir: got %q, want /tmp/gvt/run", got)
		}
	})
	t.Run("xdg gets a pando subdirectory", func(t *testing.T) {
		xdg := "/run/user/1000"

		t.Setenv("PANDO_RUNTIME_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", xdg)

		if got, want := Dir(), filepath.Join(xdg, "pando"); got != want {
			t.Fatalf("Dir: got %q, want %q", got, want)
		}
	})
	t.Run("temp dir fallback", func(t *testing.T) {
		t.Setenv("PANDO_RUNTIME_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "")

		want := filepath.Join(os.TempDir(), "pando-"+strconv.Itoa(os.Getuid()))
		if got := Dir(); got != want {
			t.Fatalf("Dir: got %q, want %q", got, want)
		}
	})
}

// TestSocketPathLength pins the sun_path limit the runtime directory has to
// stay under. proto does not enforce it -- `pando serve` checks for > 104
// bytes -- so what proto owes the caller is an untruncated path; the kernel
// does the rejecting.
func TestSocketPathLength(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 200))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	t.Setenv("PANDO_RUNTIME_DIR", dir)

	long := SocketPath()
	if long != dir+"/pando.sock" || len(long) <= 104 {
		t.Fatalf("SocketPath: got %q (%d bytes), want the full path over the limit", long, len(long))
	}

	if ln, err := net.Listen("unix", long); err == nil {
		_ = ln.Close()

		t.Fatalf("listen on %d bytes: want an error", len(long))
	}
	// The same directory listens fine once the path is short enough, so the
	// failure above is the length and nothing else.
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())

	ln, err := net.Listen("unix", SocketPath())
	if err != nil {
		t.Fatalf("listen on %d bytes: %v", len(SocketPath()), err)
	}

	_ = ln.Close()
}

// fullSettings has every field set to something other than its zero value, so
// a field with a wrong tag cannot round-trip by accident.
func fullSettings() Settings {
	return Settings{
		Hidden:    true,
		Icons:     "nerd",
		Width:     30,
		WidthR:    40,
		GitDeco:   true,
		Left:      Columns{Column{Views: []string{"files", "git"}, Width: 22}, views("agents")},
		Right:     Columns{views("search")},
		GitTree:   true,
		Drawers:   []string{"Changes", "Graph"},
		GitPanes:  map[string]Pane{"Changes": {Open: true, H: 7}},
		QuickTree: true,
		Theme:     "vscode-dark",
		DiffView:  "split",
		Borders:   true,
		ActBar:    "side",
		TermPos:   "bottom",
		TermH:     12,
		TermOpen:  true,
		LSP:       map[string][]string{"go": {"gopls"}},
		Format:    map[string][]string{"go": {"gofmt"}},
		FmtSave:   true,
		Keys:      map[string]string{"ctrl+g": "view.showSearch"},
		Colors:    map[string]string{"selBg": "#264f78"},
		Sounds:    true,
		SoundDone: "/done.wav",
		SoundReq:  "/request.wav",
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	want := fullSettings()

	t.Run("json", func(t *testing.T) {
		var got Settings
		if err := json.Unmarshal([]byte(mustJSON(t, want)), &got); err != nil {
			t.Fatalf("unmarshal settings: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("json round trip: got %+v, want %+v", got, want)
		}
	})
	t.Run("toml", func(t *testing.T) {
		b, err := toml.Marshal(want)
		if err != nil {
			t.Fatalf("marshal settings: %v", err)
		}

		var got Settings
		if err := toml.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal settings: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("toml round trip: got %+v, want %+v", got, want)
		}
	})
}

// TestSettingsTags holds the promise the type's doc comment makes: every field
// is tagged for both encodings, under one name, and no two share it.
func TestSettingsTags(t *testing.T) {
	rt := reflect.TypeOf(Settings{})
	seen := map[string]string{}

	for i := range rt.NumField() {
		f := rt.Field(i)

		js, ok := f.Tag.Lookup("json")
		if !ok || js == "" || js == "-" {
			t.Errorf("%s: no json tag", f.Name)
			continue
		}

		tm, ok := f.Tag.Lookup("toml")
		if !ok || tm != js {
			t.Errorf("%s: toml tag %q, want the json name %q", f.Name, tm, js)
		}

		if prev, dup := seen[js]; dup {
			t.Errorf("%s: tag %q already used by %s", f.Name, js, prev)
		}

		seen[js] = f.Name
	}
}

// TestWireShapes pins the bytes on the socket: the field names and which of
// them disappear when empty.
func TestWireShapes(t *testing.T) {
	shapes := []struct {
		name string
		v    any
		want string
	}{
		{"request", Request{Method: "ping"}, `{"method":"ping"}`},
		{"request params", Request{Method: "state.set", Params: json.RawMessage(`{"hidden":true}`)}, `{"method":"state.set","params":{"hidden":true}}`},
		{"response result", Response{Result: json.RawMessage(`[]`)}, `{"result":[]}`},
		{"response error", Response{Error: "no such session"}, `{"error":"no such session"}`},
		{"response empty", Response{}, `{}`},
		{"event", Event{Kind: "state"}, `{"event":"state"}`},
		{"event screen", Event{Kind: "screen", ID: "s1"}, `{"event":"screen","id":"s1"}`},
		{"event focus", Event{Kind: "focus", Data: json.RawMessage(`{"session":"s1"}`)}, `{"event":"focus","data":{"session":"s1"}}`},
		{"focus params", FocusParams{Session: "s1"}, `{"session":"s1"}`},
		{"input", InputParams{ID: "s1", Text: "ls\n"}, `{"id":"s1","text":"ls\n"}`},
		{"input key", InputParams{ID: "s1", Keys: []Key{{Code: 'a', Mod: 2}}}, `{"id":"s1","keys":[{"code":97,"mod":2}]}`},
		{"screen params", ScreenParams{ID: "s1", Cols: 80, Rows: 24}, `{"id":"s1","cols":80,"rows":24}`},
		{"session spec", SessionSpec{ID: "s1", Workspace: "/w", Agent: "claude", Cmd: []string{"claude"}}, `{"id":"s1","workspace":"/w","agent":"claude","cmd":["claude"]}`},
		{"editor", Editor{Path: "/a.go", Line: 3}, `{"path":"/a.go","line":3,"col":0}`},
		{"pane", Pane{Open: true, H: 7}, `{"open":true,"h":7}`},
		{"column", Column{Views: []string{"files"}}, `{"views":["files"]}`},
	}
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustJSON(t, tc.v); got != tc.want {
				t.Fatalf("encode: got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestEventRoundTrip checks that a stream of events survives the encoder and
// decoder Subscribe puts them through.
func TestEventRoundTrip(t *testing.T) {
	want := Event{Kind: "focus", ID: "s1", Data: json.RawMessage(`{"workspace":"/w"}`)}

	var got Event
	if err := json.Unmarshal([]byte(mustJSON(t, want)), &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if got.Kind != want.Kind || got.ID != want.ID || string(got.Data) != string(want.Data) {
		t.Fatalf("event round trip: got %+v, want %+v", got, want)
	}

	var fp FocusParams
	if err := json.Unmarshal(got.Data, &fp); err != nil {
		t.Fatalf("unmarshal focus: %v", err)
	}

	if fp.Workspace != "/w" {
		t.Fatalf("focus workspace: got %q, want /w", fp.Workspace)
	}
}

func TestDraftKey(t *testing.T) {
	saved := Draft{WS: "/w", Path: "/w/a.go", Name: "Untitled-1", Text: "x", Mod: time.Unix(1, 0)}
	if got := saved.Key(); got != "/w/a.go" {
		t.Fatalf("Key: got %q, want the path", got)
	}

	if got := (Draft{WS: "/w", Name: "Untitled-1"}).Key(); got != "untitled:Untitled-1" {
		t.Fatalf("Key: got %q, want untitled:Untitled-1", got)
	}

	var back Draft
	if err := json.Unmarshal([]byte(mustJSON(t, saved)), &back); err != nil {
		t.Fatalf("unmarshal draft: %v", err)
	}

	if !back.Mod.Equal(saved.Mod) || back.Text != saved.Text || back.Key() != saved.Key() {
		t.Fatalf("draft round trip: got %+v, want %+v", back, saved)
	}
}

func TestBuildID(t *testing.T) {
	id := BuildID()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}

	st, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("stat %s: %v", exe, err)
	}

	want := strconv.FormatInt(st.Size(), 10) + "-" + strconv.FormatInt(st.ModTime().UnixNano(), 10)
	if id != want {
		t.Fatalf("BuildID: got %q, want %q", id, want)
	}

	if again := BuildID(); again != id {
		t.Fatalf("BuildID: got %q on the second call, want %q", again, id)
	}
}

func TestWithoutNoColor(t *testing.T) {
	env := []string{"PATH=/bin", "NO_COLOR=1", "TERM=xterm", "NO_COLORS=keep", "ANO_COLOR=keep"}
	got := WithoutNoColor(env)

	want := []string{"PATH=/bin", "TERM=xterm", "NO_COLORS=keep", "ANO_COLOR=keep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WithoutNoColor: got %q, want %q", got, want)
	}

	if len(env) != 5 || env[1] != "NO_COLOR=1" {
		t.Fatalf("WithoutNoColor: it modified its argument, %q", env)
	}

	if got := WithoutNoColor(nil); got != nil {
		t.Fatalf("WithoutNoColor(nil): got %q, want nil", got)
	}
}

// serveSocket answers the pando socket in-process: one handler call per
// connection, each value it returns encoded as one JSON line. It is enough to
// exercise the client side of the protocol without a daemon; internal/daemon
// covers the real server. The goroutines never touch t, which only the test
// goroutine may fail.
func serveSocket(t *testing.T, handle func(Request) []any) chan Request {
	t.Helper()

	ln, err := net.Listen("unix", SocketPath())
	if err != nil {
		t.Fatalf("listen %s: %v", SocketPath(), err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	seen := make(chan Request, 8)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = conn.Close() }()

				var req Request
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}

				seen <- req

				enc := json.NewEncoder(conn)
				for _, v := range handle(req) {
					if enc.Encode(v) != nil {
						return
					}
				}
			}()
		}
	}()

	return seen
}

func TestCall(t *testing.T) {
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())
	seen := serveSocket(t, func(Request) []any {
		return []any{Response{Result: json.RawMessage(`{"build":"abc"}`)}}
	})

	var got struct {
		Build string `json:"build"`
	}
	if err := Call("ping", FocusParams{Session: "s1"}, &got); err != nil {
		t.Fatalf("call ping: %v", err)
	}

	if got.Build != "abc" {
		t.Fatalf("call ping: got %q, want abc", got.Build)
	}

	req := <-seen
	if req.Method != "ping" || string(req.Params) != `{"session":"s1"}` {
		t.Fatalf("the server saw %+v, want ping with the focus params", req)
	}
	// A nil result discards the reply, and nil params send no params key.
	if err := Call("shutdown", nil, nil); err != nil {
		t.Fatalf("call shutdown: %v", err)
	}

	if req = <-seen; req.Method != "shutdown" || req.Params != nil {
		t.Fatalf("the server saw %+v, want shutdown with no params", req)
	}

	if got := DaemonBuild(); got != "abc" {
		t.Fatalf("DaemonBuild: got %q, want abc", got)
	}
}

func TestCallError(t *testing.T) {
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())
	serveSocket(t, func(req Request) []any {
		if req.Method == "hangup" {
			return nil // close without answering
		}

		return []any{Response{Error: "no such session"}}
	})

	var out []Workspace

	err := Call("session.input", nil, &out)
	if err == nil || err.Error() != "no such session" {
		t.Fatalf("call: got %v, want the server's error", err)
	}

	if out != nil {
		t.Fatalf("call: got %#v, want the result left alone on an error", out)
	}

	if err := Call("hangup", nil, nil); err == nil {
		t.Fatal("call: want an error when the server answers nothing")
	}
}

func TestSubscribe(t *testing.T) {
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())

	want := []Event{{Kind: "state"}, {Kind: "sessions"}, {Kind: "screen", ID: "s1"}}
	seen := serveSocket(t, func(Request) []any {
		out := make([]any, len(want))
		for i, ev := range want {
			out[i] = ev
		}

		return out
	})

	ch, err := Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	var got []Event
	for ev := range ch { // the server closes after the last event
		got = append(got, ev)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subscribe: got %+v, want %+v", got, want)
	}

	if req := <-seen; req.Method != "subscribe" {
		t.Fatalf("the server saw %q, want subscribe", req.Method)
	}
}

// TestNoDaemon checks the client calls fail rather than hang when nothing is
// listening. The daemon package covers the connected side end to end.
func TestNoDaemon(t *testing.T) {
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())

	if err := Call("ping", nil, nil); err == nil {
		t.Fatal("Call: want an error with no socket")
	}

	if _, err := Subscribe(); err == nil {
		t.Fatal("Subscribe: want an error with no socket")
	}

	if got := DaemonBuild(); got != "" {
		t.Fatalf("DaemonBuild: got %q, want empty with no socket", got)
	}
}

// TestCallUnmarshalableParams covers the branch of Call that runs before the
// reply can matter: params that do not encode.
func TestCallUnmarshalableParams(t *testing.T) {
	t.Setenv("PANDO_RUNTIME_DIR", t.TempDir())
	serveSocket(t, func(Request) []any { return []any{Response{}} })

	if err := Call("ping", make(chan int), nil); err == nil {
		t.Fatal("Call: want an error for params that do not encode")
	}
}

func FuzzColumnsJSON(f *testing.F) {
	for _, tc := range columnShapes {
		f.Add(tc.js)
	}

	for _, tc := range columnErrors {
		f.Add(tc.in)
	}

	for _, s := range []string{`null`, `["files",null]`, `[{"views":["a"],"width":-1}]`, `[{"views":["a"],"width":1e309}]`, ``, `[`} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		var c Columns
		if err := c.UnmarshalJSON([]byte(in)); err != nil {
			return
		}

		b, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("re-encode %q: %v", in, err)
		}

		var again Columns
		if err := again.UnmarshalJSON(b); err != nil {
			t.Fatalf("re-decode %s: %v", b, err)
		}

		if !reflect.DeepEqual(c, again) {
			t.Fatalf("%q decoded to %#v, re-encoded to %s, decoded to %#v", in, c, b, again)
		}
	})
}

func FuzzColumnsTOML(f *testing.F) {
	for _, tc := range columnShapes {
		f.Add("left = " + tc.tm)
	}

	for _, tc := range columnErrors {
		f.Add("left = " + tc.in)
	}

	for _, s := range []string{
		`left = nan`, `left = inf`, `left = 5`, `left = 1979-05-27T07:32:00Z`,
		"[[left]]\nviews = [\"files\"]\nwidth = 22\n", "[[left]]\n", `right = ["files"]`, ``,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, doc string) {
		var v struct {
			Left Columns `toml:"left"`
		}
		if err := toml.Unmarshal([]byte(doc), &v); err != nil {
			return
		}

		b, err := json.Marshal(v.Left)
		if err != nil {
			t.Fatalf("re-encode %q: %v", doc, err)
		}

		var again Columns
		if err := again.UnmarshalJSON(b); err != nil {
			t.Fatalf("re-decode %s: %v", b, err)
		}

		if !reflect.DeepEqual(v.Left, again) {
			t.Fatalf("%q decoded to %#v, re-encoded to %s, decoded to %#v", doc, v.Left, b, again)
		}
	})
}
