package daemon

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// start points pando at a scratch runtime/config/data dir and returns a boot
// func; calling it again after Close restarts the daemon on the same dirs.
func start(t *testing.T) func() *Daemon {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PANDO_RUNTIME_DIR", filepath.Join(tmp, "run"))

	if err := os.MkdirAll(proto.Dir(), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", proto.Dir(), err)
	}

	boot := func() *Daemon {
		d, err := New(filepath.Join(tmp, "cfg"), filepath.Join(tmp, "data"))
		if err != nil {
			t.Fatal(err)
		}

		_ = os.Remove(proto.SocketPath()) // a previous boot's socket, if any

		ln, err := net.Listen("unix", proto.SocketPath())
		if err != nil {
			t.Fatal(err)
		}

		go func() { _ = d.Serve(ln) }() // Close makes Accept return

		return d
	}

	return boot
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}

	t.Fatalf("timed out waiting for %s", what)
}

// call sends one request to the running daemon; an error fails the test here
// rather than as a puzzling assertion further down.
func call(t *testing.T, method string, params, out any) {
	t.Helper()

	if err := proto.Call(method, params, out); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

// mustWrite writes a file and the directories above it, or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	_ = os.MkdirAll(filepath.Dir(path), 0o755) // a failing WriteFile names the path
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustRead returns a file the test is about to assert on, or fails the test.
func mustRead(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(b)
}

// touch dates a file an hour into the future, so the daemon sees a foreign
// write. reloadConfig compares mtimes for inequality, and every touch lands on
// a later clock reading, so repeated touches of one file keep reloading it.
func touch(t *testing.T, path string) {
	t.Helper()

	at := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// mustGit runs a git command in root, or fails the test naming the command.
func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()

	if _, err := git.Run(root, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func TestDaemon(t *testing.T) {
	boot := start(t)
	d := boot()

	repo := t.TempDir()
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		mustGit(t, repo, a...)
	}

	events, err := proto.Subscribe()
	if err != nil {
		t.Fatal(err)
	}

	var root string
	call(t, "project.add", map[string]string{"path": filepath.Join(repo, ".")}, &root)

	if ev := <-events; ev.Kind != "workspaces" {
		t.Fatalf("event = %+v", ev)
	}

	var ws proto.Workspace
	call(t, "workspace.new", map[string]string{"project": root, "branch": "feat/x"}, &ws)

	var list []proto.Workspace
	call(t, "workspace.list", nil, &list)

	if len(list) != 2 || list[1].Branch != "feat/x" || !list[0].Main {
		t.Fatalf("workspaces = %+v", list)
	}

	// Without a branch the daemon names one, as herdr does.
	var named proto.Workspace
	call(t, "workspace.new", map[string]string{"project": root}, &named)

	if !strings.HasPrefix(named.Branch, "worktree/") || filepath.Base(named.Path) != strings.ReplaceAll(named.Branch, "/", "-") {
		t.Fatalf("unnamed worktree = %+v", named)
	}

	call(t, "workspace.remove", map[string]string{"path": named.Path}, nil)

	// project.move reorders the project list the Spaces tree walks. A second
	// project goes to the top, an index past the end clamps to the last place.
	second := t.TempDir()

	var root2 string
	call(t, "project.add", map[string]string{"path": second}, &root2)

	order := func() []string {
		var st proto.State
		call(t, "state.get", nil, &st)

		return st.Projects
	}
	if got := order(); len(got) != 2 || got[0] != root || got[1] != root2 {
		t.Fatalf("projects after add = %v", got)
	}

	call(t, "project.move", proto.MoveParams{Path: root2, To: 0}, nil)

	if got := order(); got[0] != root2 || got[1] != root {
		t.Fatalf("after moving to the top = %v", got)
	}

	call(t, "project.move", proto.MoveParams{Path: root2, To: 99}, nil)

	if got := order(); got[0] != root || got[1] != root2 {
		t.Fatalf("an index past the end clamps to the last place = %v", got)
	}

	if err := proto.Call("project.move", proto.MoveParams{Path: "/nowhere", To: 0}, nil); err == nil {
		t.Fatal("moving a path that is not a project must fail")
	}

	// Settings patches merge into config.toml's values; unknown keys are errors.
	call(t, "state.set", map[string]any{"settings": map[string]any{"hidden": false}, "drafts": map[string]string{root: "wip"}}, nil)

	var st proto.State
	call(t, "state.get", nil, &st)

	if st.Settings.Hidden || st.Settings.Width != 40 || !st.Settings.Borders || st.Drafts[root] != "wip" || st.Agents["shell"] == nil {
		t.Fatalf("state = %+v", st)
	}

	call(t, "state.set", map[string]any{"last_workspace": root}, nil)
	call(t, "state.get", nil, &st)

	if st.LastWorkspace != root || st.Drafts[root] != "wip" {
		t.Fatalf("last workspace = %q, drafts %v", st.LastWorkspace, st.Drafts)
	}
	// Open editors are kept per workspace; null or an empty list forgets it.
	eds := map[string]proto.Editors{root: {Open: []proto.Editor{{Path: root + "/a.go", Line: 3, Col: 1, Top: 2}}}}
	call(t, "state.set", map[string]any{"editors": eds}, nil)
	call(t, "state.get", nil, &st)

	if len(st.Editors[root].Open) != 1 || st.Editors[root].Open[0].Line != 3 || st.Editors[root].Open[0].Top != 2 {
		t.Fatalf("editors = %+v", st.Editors)
	}

	call(t, "state.set", map[string]any{"editors": map[string]any{root: nil}}, nil)

	var fresh proto.State // decoding into st would merge into its map
	call(t, "state.get", nil, &fresh)

	if len(fresh.Editors) != 0 {
		t.Fatalf("forgotten editors = %+v", fresh.Editors)
	}

	pane := func(title string, h int) map[string]any {
		return map[string]any{"settings": map[string]any{"git_panes": map[string]any{title: map[string]any{"open": true, "h": h}}}}
	}
	call(t, "state.set", pane("Graph", 5), nil)
	call(t, "state.set", pane("File History", 3), nil)
	call(t, "state.get", nil, &st)

	if st.Settings.GitPanes["Graph"].H != 5 || st.Settings.GitPanes["File History"].H != 3 || st.Settings.Hidden {
		t.Fatalf("merged settings = %+v", st.Settings)
	}

	if err := proto.Call("state.set", map[string]any{"settings": map[string]any{"widht": 3}}, nil); err == nil || !strings.Contains(err.Error(), "widht") {
		t.Fatalf("unknown setting: err = %v", err)
	}

	if got := proto.DaemonBuild(); got == "" || got != proto.BuildID() {
		t.Fatalf("daemon build %q, client build %q", got, proto.BuildID())
	}

	// A shell session: type into it, read the screen back.
	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws.Path, "cmd": []string{"sh"}, "cols": 40, "rows": 10}, &s)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "echo hel''lo; pwd\r"}, nil)

	var text string

	waitFor(t, "echo output", func() bool {
		call(t, "session.read", map[string]any{"id": s.ID}, &text)
		return strings.Contains(text, "hello\n") && strings.Contains(text, "feat-x")
	})
	// Keys are encoded by the emulator: ctrl+u clears the line, enter runs it.
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "garbage", Keys: []proto.Key{{Code: 'u', Mod: 4 /*ctrl*/}}}, nil)
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "printf '\\033]0;T1\\007'", Keys: []proto.Key{{Code: 13}}}, nil)
	waitFor(t, "title", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 1 && ss[0].Title == "T1"
	})
	// What runs in the foreground reaches clients, and so does a rename,
	// which a restart keeps.
	waitFor(t, "program", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 1 && ss[0].Program == "sh"
	})
	call(t, "session.rename", map[string]string{"id": s.ID, "name": " build "}, nil)

	// Shifted letters arrive as text; OSC 11 queries get the client's colors.
	// `read -d` and $'\a' are bash builtins, so this session names bash rather
	// than sh: /bin/sh is dash on the CI runner and would swallow both.
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("look up bash: %v", err)
	}

	var light proto.Session
	call(t, "session.new", map[string]any{"workspace": ws.Path, "cmd": []string{shell}, "bg": "#fafafa"}, &light)
	// read stops at the reply's BEL, so input typed later cannot race ahead of
	// the reply; ${x#?} drops its ESC, or the echo would be parsed as OSC again.
	call(t, "session.input", proto.InputParams{ID: light.ID, Text: "printf '\\033]11;?\\007' >/dev/tty; read -r -d $'\\a' x; echo \"<${x#?}>\"", Keys: []proto.Key{{Code: 13}}}, nil)
	waitFor(t, "bg reply", func() bool {
		call(t, "session.read", map[string]any{"id": light.ID}, &text)
		return strings.Contains(text, "rgb:fafa/fafa/fafa>")
	})
	call(t, "session.input", proto.InputParams{ID: light.ID, Text: "echo shift", Keys: []proto.Key{{Code: 'a', Mod: 1, Text: "A"}, {Code: 13}}}, nil)
	waitFor(t, "shifted key", func() bool {
		call(t, "session.read", map[string]any{"id": light.ID}, &text)
		return strings.Contains(text, "\nshiftA")
	})
	call(t, "session.kill", map[string]string{"id": light.ID}, nil)

	var scr proto.Screen
	call(t, "session.screen", proto.ScreenParams{ID: s.ID, Cols: 30, Rows: 5}, &scr)

	if len(scr.Lines) != 5 {
		t.Fatalf("resized screen has %d lines", len(scr.Lines))
	}

	if err := proto.Call("workspace.remove", map[string]string{"path": ws.Path}, nil); err == nil {
		t.Fatal("removing a workspace with live sessions must fail")
	}

	// Screen events arrive for output.
	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "echo x\r"}, nil)
	waitFor(t, "screen event", func() bool {
		select {
		case ev := <-events:
			return ev.Kind == "screen" && ev.ID == s.ID
		default:
			return false
		}
	})

	// Restart: the session is respawned from the saved spec.
	d.Close()
	d = boot()

	var ss []proto.Session
	call(t, "session.list", nil, &ss)

	if len(ss) != 1 || ss[0].ID != s.ID || ss[0].Workspace != ws.Path || ss[0].Name != "build" {
		t.Fatalf("after restart sessions = %+v", ss)
	}

	var reloaded proto.State
	call(t, "state.get", nil, &reloaded)

	if reloaded.Settings.GitPanes["File History"].H != 3 || reloaded.Settings.Hidden {
		t.Fatalf("settings after restart: %+v", reloaded.Settings)
	}

	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "exit 3\r"}, nil)
	waitFor(t, "exit", func() bool {
		call(t, "session.list", nil, &ss)
		return ss[0].Status == "exited" && ss[0].ExitCode == 3 && ss[0].Attention
	})
	call(t, "session.kill", map[string]string{"id": s.ID}, nil)
	call(t, "workspace.remove", map[string]string{"path": ws.Path}, nil)

	if err := proto.Call("nope", nil, nil); err == nil || !strings.Contains(err.Error(), "unknown method") {
		t.Fatalf("unknown method err = %v", err)
	}

	d.Close()
}

func TestAttentionAfterUnseenBurst(t *testing.T) {
	now := time.Now()

	s := &session{status: "running", busySince: now.Add(-5 * time.Second), lastOutput: now.Add(-2 * time.Second)}
	if !s.tick(now) || s.status != "idle" || !s.attention {
		t.Fatalf("long unseen burst: %+v", s)
	}

	s = &session{status: "running", busySince: now.Add(-5 * time.Second), lastOutput: now.Add(-2 * time.Second), lastViewed: now}
	s.tick(now)

	if s.attention {
		t.Fatal("viewed session must not need attention")
	}
}

func TestSessionClosesOnCleanExit(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"sh", "-c", "sleep 0.3"}}, &s)
	waitFor(t, "a clean exit to close the session", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 0
	})

	var st proto.State
	call(t, "state.get", nil, &st)

	if len(st.Sessions) != 0 {
		t.Fatalf("closed session still saved: %+v", st.Sessions)
	}
}

// TestConcurrentState guards a fatal bug: state.get marshalled proto.State
// after dropping d.mu, so the encoder walked Drafts, Editors and the settings
// maps while another connection's state.set wrote them — "concurrent map
// iteration and map write", which no recover catches and which takes every
// session down with the daemon.
func TestConcurrentState(t *testing.T) {
	d := start(t)()
	defer d.Close()
	// proto.Call, not the call helper: t.Fatalf belongs to the test goroutine.
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for n := range 40 {
				ws := fmt.Sprintf("/ws/%d/%d", i, n)

				err := proto.Call("state.set", map[string]any{
					"drafts":   map[string]string{ws: "wip"},
					"editors":  map[string]any{ws: map[string]any{"open": []any{map[string]any{"path": ws + "/a.go"}}}},
					"settings": map[string]any{"git_panes": map[string]any{ws: map[string]any{"open": true, "h": n}}},
					"agents":   map[string][]string{ws: {"sh", "-c", ws}},
				}, nil)
				if err != nil {
					t.Errorf("state.set: %v", err)
					return
				}
			}
		})
		wg.Go(func() {
			for range 40 {
				var st proto.State
				if err := proto.Call("state.get", nil, &st); err != nil {
					t.Errorf("state.get: %v", err)
					return
				}
			}
		})
	}

	wg.Wait()

	var st proto.State
	call(t, "state.get", nil, &st)

	if len(st.Drafts) != 160 || len(st.Editors) != 160 || len(st.Settings.GitPanes) != 160 {
		t.Fatalf("drafts %d editors %d panes %d, want 160 each", len(st.Drafts), len(st.Editors), len(st.Settings.GitPanes))
	}
}

func TestConfig(t *testing.T) {
	cfg := t.TempDir()
	path := filepath.Join(cfg, "config.toml")

	d, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.setState(json.RawMessage(`{"settings":{"diff_view":"split"},"drafts":{"/p":"x"},"editors":{"/p":{"open":[{"path":"/p/a.go","line":1,"col":0}],"active":0}}}`)); err != nil {
		t.Fatal(err)
	}

	b := mustRead(t, path)

	state := mustRead(t, filepath.Join(cfg, "state.json"))
	if !strings.Contains(b, `diff_view = "split"`) || strings.Contains(state, "settings") || !strings.Contains(state, `"/p": "x"`) || !strings.Contains(state, `"/p/a.go"`) {
		t.Fatalf("after state.set:\n%s\n%s", b, state)
	}

	// An edit outside pando reloads and notifies clients.
	events := make(chan proto.Event, 4)
	d.subs[events] = struct{}{}

	mustWrite(t, path, strings.Replace(b, "hidden = true", "hidden = false", 1))
	touch(t, path)
	d.reloadConfig()

	if d.state.Settings.Hidden || len(events) != 1 {
		t.Fatalf("reload: hidden=%v events=%d", d.state.Settings.Hidden, len(events))
	}
	// A broken file keeps the last good settings and is never overwritten.
	mustWrite(t, path, "hidden = ")
	touch(t, path)
	d.reloadConfig()

	if _, err := d.setState(json.RawMessage(`{"settings":{"width":30}}`)); err == nil || d.state.Settings.Hidden {
		t.Fatalf("broken config: err=%v settings=%+v", err, d.state.Settings)
	}

	if got := mustRead(t, path); got != "hidden = " {
		t.Fatalf("broken config was overwritten: %q", got)
	}

	// Every field survives a write and a read.
	c := config{
		Settings: proto.Settings{
			Hidden: false, Icons: "ascii", Width: 41, WidthR: 33, GitDeco: false,
			Left: proto.Columns{{Views: []string{"agents"}, Width: 24}, {Views: []string{"files"}}}, Right: proto.Columns{{Views: []string{"git"}}}, GitTree: false,
			GitPanes: map[string]proto.Pane{"File History": {Open: true, H: 4}}, QuickTree: true,
			Drawers: []string{"Graph", "Tags"},
			Theme:   "terminal", DiffView: "split", ActBar: "top", TermPos: "right", TermH: 14, TermOpen: true, Borders: false, Colors: map[string]string{"accent": "#ff8800", "ok": "2"}, Keys: map[string]string{"ctrl+g": "view.showSearch"},
			LSP: map[string][]string{"go": {"gopls"}}, Format: map[string][]string{"ts": {"prettier", "--stdin-filepath", "$FILE"}}, FmtSave: true,
			Sounds: true, SoundDone: "/a.oga", SoundReq: "",
		}, Agents: map[string][]string{"my agent": {"x", "y \"z\""}},
		Resume:   map[string][]string{"my agent": {"x", "--continue"}},
		ResumeID: map[string][]string{"my agent": {"x", "--resume", "{id}"}},
	}
	round := filepath.Join(t.TempDir(), "config.toml")
	mustWrite(t, round, string(c.encode()))

	if got, err := loadConfig(round); err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip: %v\n got %+v\nwant %+v\n%s", err, got, c, c.encode())
	}

	// A flat view list, the format before columns, is one column in TOML and JSON.
	mustWrite(t, round, `left = ["files", "git"]`)

	var fromJSON proto.Settings
	if err := json.Unmarshal([]byte(`{"left":["files","git"]}`), &fromJSON); err != nil {
		t.Fatal(err)
	}

	want := proto.Columns{{Views: []string{"files", "git"}}}
	if got, err := loadConfig(round); err != nil || !reflect.DeepEqual(got.Left, want) || !reflect.DeepEqual(fromJSON.Left, want) {
		t.Fatalf("flat columns: %v toml %+v json %+v", err, got.Left, fromJSON.Left)
	}

	// A fresh config directory gets the defaults.
	fresh, err := New(t.TempDir(), t.TempDir())
	if s := fresh.state.Settings; err != nil || !s.Hidden || !s.GitDeco || !s.GitTree || !s.Borders || s.Width != 40 || s.Theme != "vscode" || s.Icons == "" {
		t.Fatalf("defaults: %v %+v", err, s)
	}
}

func TestResumeAgentAfterRestart(t *testing.T) {
	boot := start(t)
	d := boot()
	ws := t.TempDir()
	// A stand-in agent: a program that sits in the foreground until it is killed.
	agent := filepath.Join(ws, "myagent")
	mustWrite(t, agent, "#!/bin/sh\necho AGENT UP\nsleep 300\n")

	if err := os.Chmod(agent, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", agent, err)
	}

	d.mu.Lock()
	d.state.Resume = map[string][]string{"myagent": {"echo", "RESUMED"}}
	d.mu.Unlock()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)

	screen := func(id string) string {
		t.Helper()

		var scr proto.Screen
		call(t, "session.screen", proto.ScreenParams{ID: id, Cols: 40, Rows: 8}, &scr)

		return strings.Join(scr.Lines, "\n")
	}

	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "./myagent\r"}, nil)
	waitFor(t, "the agent to start", func() bool { return strings.Contains(screen(s.ID), "AGENT UP") })

	// What the ticker does: notice the program and remember how to bring it back.
	sess := d.sessions[s.ID]

	waitFor(t, "the running agent to be recognised", func() bool { return remembered(d, sess).Resume != nil })

	if got := remembered(d, sess); !reflect.DeepEqual(got.Resume, []string{"echo", "RESUMED"}) || got.Conversation != nil {
		t.Fatalf("resume = %v %v", got.Resume, got.Conversation)
	}

	d.mu.Lock()
	saveErr := d.save()
	d.mu.Unlock()

	if saveErr != nil {
		t.Fatal(saveErr)
	}

	// A restarted daemon respawns the shell and continues the agent in it.
	d.Close()

	d = boot()
	defer d.Close()

	var ss []proto.Session
	call(t, "session.list", nil, &ss)

	if len(ss) != 1 {
		t.Fatalf("sessions after restart: %+v", ss)
	}

	waitFor(t, "the agent to be resumed", func() bool { return strings.Contains(screen(ss[0].ID), "RESUMED") })
}

// remembered is what the ticker does for session s: notice the program and
// remember how to bring it back. It returns the spec as it is then.
func remembered(d *Daemon, s *session) proto.SessionSpec {
	d.mu.Lock()
	defer d.mu.Unlock()

	pid, prog := s.foreground()
	d.remember(s, pid, prog)

	return s.info().SessionSpec
}

// fakeClaude publishes that process pid has conversation id open, the way
// Claude Code does.
func fakeClaude(t *testing.T, pid int, id string) {
	t.Helper()

	b, err := json.Marshal(claudeProcess{PID: pid, SessionID: id, ProcStart: startTime(pid)})
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(claudeDir(), "sessions", strconv.Itoa(pid)+".json"), string(b))
}

func TestResumeConversation(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	conversations["myagent"] = claudeSource{}

	t.Cleanup(func() { delete(conversations, "myagent") })

	boot := start(t)
	d := boot()
	ws := t.TempDir()
	agent := filepath.Join(ws, "myagent")
	mustWrite(t, agent, "#!/bin/sh\necho AGENT UP\nsleep 300\n")

	if err := os.Chmod(agent, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", agent, err)
	}

	d.mu.Lock()
	d.state.Resume = map[string][]string{"myagent": {"echo", "LATEST"}}
	d.state.ResumeID = map[string][]string{"myagent": {"echo", "RESUMED", "{id}"}}
	d.state.Agents["myagent"] = []string{"echo", "FRESH"}
	cfgErr := d.saveConfig()
	d.mu.Unlock()

	if cfgErr != nil {
		t.Fatal(cfgErr)
	}

	screen := func(id string) string {
		t.Helper()

		var scr proto.Screen
		call(t, "session.screen", proto.ScreenParams{ID: id, Cols: 60, Rows: 8}, &scr)

		return strings.Join(scr.Lines, "\n")
	}

	// Two sessions came to have conversation one open, the third has
	// conversation two; the fourth's has nothing in it yet.
	for _, id := range []string{"one", "two"} {
		mustWrite(t, filepath.Join(claudeDir(), "projects", "-ws", id+".jsonl"), "{}\n")
	}

	convs := []string{"one", "one", "two", "empty"}
	ids := make([]string, len(convs))

	for i, conv := range convs {
		var s proto.Session
		call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)
		ids[i] = s.ID
		call(t, "session.input", proto.InputParams{ID: s.ID, Text: "./myagent\r"}, nil)
		waitFor(t, "the agent to start", func() bool { return strings.Contains(screen(s.ID), "AGENT UP") })

		sess := d.sessions[s.ID]

		var pid int

		waitFor(t, "the agent to hold the terminal", func() bool {
			var prog string

			pid, prog = sess.foreground()

			return prog == "myagent"
		})

		fakeClaude(t, pid, conv)

		want := proto.Conversation{Agent: "myagent", ID: conv}
		if got := remembered(d, sess); got.Conversation == nil || *got.Conversation != want || !reflect.DeepEqual(got.Resume, []string{"echo", "RESUMED", conv}) {
			t.Fatalf("remembered %v %v, want %v", got.Resume, got.Conversation, want)
		}
	}

	d.mu.Lock()
	saveErr := d.save()
	d.mu.Unlock()

	if saveErr != nil {
		t.Fatal(saveErr)
	}

	// Stopping takes the agents down with their shells.
	d.Close()

	// Meanwhile conversation two is opened outside pando, and the first
	// session's agent outlived its terminal, as after a crash.
	other := exec.Command("sleep", "60")
	leftover := exec.Command("sleep", "60")

	leftover.Env = append(os.Environ(), "PANDO_SESSION="+ids[0])

	for _, c := range []*exec.Cmd{other, leftover} {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	t.Cleanup(func() { _ = leftover.Process.Kill() }) // reaped below

	gone := make(chan struct{})

	go func() { _ = leftover.Wait(); close(gone) }()

	fakeClaude(t, other.Process.Pid, "two")
	fakeClaude(t, leftover.Process.Pid, "one")

	d = boot()
	defer d.Close()

	var ss []proto.Session
	call(t, "session.list", nil, &ss)

	if len(ss) != len(convs) {
		t.Fatalf("sessions after restart: %+v", ss)
	}

	waitFor(t, "conversation one to be resumed", func() bool { return strings.Contains(screen(ss[0].ID), "RESUMED one") })
	waitFor(t, "the empty one to start afresh", func() bool { return strings.Contains(screen(ss[3].ID), "$ echo FRESH") })

	select {
	case <-gone:
	case <-time.After(time.Second):
		t.Error("the leftover agent still runs next to its resumed conversation")
	}

	for i, want := range map[int]string{1: "session " + ss[0].ID + " continues", 2: "process " + strconv.Itoa(other.Process.Pid) + " has"} {
		if got := screen(ss[i].ID); !strings.Contains(got, want) || strings.Contains(got, "$ echo") {
			t.Errorf("session %d after restart:\n%s\nwant %q", i, got, want)
		}
	}
}

func TestProgramOf(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"/usr/bin/claude"}, "claude"},
		{[]string{"node", "/home/u/.local/bin/claude.js"}, "claude"},
		{[]string{"bash", "-l"}, "bash"},
		{[]string{}, ""},
	} {
		if got := programOf(c.argv); got != c.want {
			t.Errorf("programOf(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestOSCFilter(t *testing.T) {
	var f oscFilter

	in := "\x1b]0;✳ Claude Code\x07 ▐▛\x1b]2;Über\x1b\\ x ✳"
	got := string(f.ascii([]byte(in)))

	want := "\x1b]0; Claude Code\x07 ▐▛\x1b]2;ber\x1b\\ x ✳"
	if got != want {
		t.Fatalf("ascii = %q, want %q", got, want)
	}
	// The state survives a chunk boundary inside a title.
	f = oscFilter{}
	a := f.ascii([]byte("\x1b]0;a✳"))

	b := f.ascii([]byte("b\x07é"))
	if string(a)+string(b) != "\x1b]0;ab\x07é" {
		t.Fatalf("split = %q + %q", a, b)
	}
}

func TestSessionWait(t *testing.T) {
	d := start(t)()
	defer d.Close()

	dir := t.TempDir()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": dir, "cmd": []string{"sh", "-c", "echo READY; sleep 0.4"}}, &s)

	// The screen decides, not the status: the command is still running.
	var got proto.Session
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Match: "READY", Timeout: 5000}, &got)

	if got.ID != s.ID {
		t.Fatalf("match wait returned %q, want %q", got.ID, s.ID)
	}

	call(t, "session.wait", proto.WaitParams{ID: s.ID, Until: []string{"exited"}, Timeout: 5000}, &got)

	if got.Status != "exited" {
		t.Fatalf("waited for exited, got %q", got.Status)
	}
}

func TestSessionWaitErrors(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"sh", "-c", "sleep 5"}}, &s)

	// A timeout must name the status it gave up in, so a caller can tell it
	// apart from a session that never existed.
	err := proto.Call("session.wait", proto.WaitParams{ID: s.ID, Until: []string{"exited"}, Timeout: 300}, nil)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("waiting past the timeout: %v, want a timeout", err)
	}

	if err := proto.Call("session.wait", proto.WaitParams{ID: s.ID, Until: []string{"done"}}, nil); err == nil {
		t.Fatal("wait accepted a status it cannot reach")
	}

	if err := proto.Call("session.wait", proto.WaitParams{ID: s.ID, Match: "("}, nil); err == nil {
		t.Fatal("wait accepted a match that does not compile")
	}
}

func TestSessionByNameAndPrefix(t *testing.T) {
	d := start(t)()
	defer d.Close()

	dir := t.TempDir()
	cmd := []string{"sh", "-c", "sleep 5"}

	var a, b proto.Session
	call(t, "session.new", map[string]any{"workspace": dir, "name": "reviewer", "cmd": cmd}, &a)
	call(t, "session.new", map[string]any{"workspace": dir, "name": "builder", "cmd": cmd}, &b)

	var got proto.Session
	call(t, "session.get", map[string]string{"id": "reviewer"}, &got)

	if got.ID != a.ID {
		t.Fatalf("by name: %q, want %q", got.ID, a.ID)
	}

	call(t, "session.get", map[string]string{"id": "build"}, &got) // a prefix of the name

	if got.ID != b.ID {
		t.Fatalf("by prefix: %q, want %q", got.ID, b.ID)
	}

	call(t, "session.get", map[string]string{"id": a.ID[:3]}, &got) // a prefix of the id

	if got.ID != a.ID {
		t.Fatalf("by id prefix: %q, want %q", got.ID, a.ID)
	}

	// A name is a handle: a second session must not be able to take it, and
	// an empty target must not resolve to whichever session comes first.
	if err := proto.Call("session.new", map[string]any{"workspace": dir, "name": "reviewer", "cmd": cmd}, nil); err == nil {
		t.Fatal("two live sessions named reviewer")
	}

	if err := proto.Call("session.rename", map[string]string{"id": b.ID, "name": "reviewer"}, nil); err == nil {
		t.Fatal("rename took a name another session answers to")
	}

	if err := proto.Call("session.get", map[string]string{"id": ""}, nil); err == nil {
		t.Fatal("an empty id resolved to a session")
	}
}

func TestSessionReadLines(t *testing.T) {
	d := start(t)()
	defer d.Close()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "cmd": []string{"sh", "-c", "seq 1 40; sleep 5"}}, &s)

	var text string

	waitFor(t, "seq to print", func() bool {
		call(t, "session.read", proto.ReadParams{ID: s.ID, Lines: 3}, &text)
		return strings.Contains(text, "40")
	})

	if n := len(strings.Split(text, "\n")); n != 3 {
		t.Fatalf("read --lines 3 gave %d lines: %q", n, text)
	}
}

func TestKillSessionKillsItsTerminals(t *testing.T) {
	d := start(t)()
	defer d.Close()

	ws := t.TempDir()
	stay := []string{"sleep", "30"}

	var parent, shell, other proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "name": "agent", "cmd": stay}, &parent)
	call(t, "session.new", map[string]any{"workspace": ws, "agent": "terminal", "parent": "agent", "cmd": stay}, &shell)
	call(t, "session.new", map[string]any{"workspace": ws, "agent": "terminal", "cmd": stay}, &other)

	if shell.Parent != parent.ID {
		t.Fatalf("parent by name resolves to its id: %q, want %q", shell.Parent, parent.ID)
	}

	if err := proto.Call("session.new", map[string]any{"workspace": ws, "agent": "terminal", "parent": "nobody", "cmd": stay}, nil); err == nil {
		t.Fatal("an unknown parent is refused")
	}

	call(t, "session.kill", map[string]string{"id": parent.ID}, nil)
	waitFor(t, "the session's shell to go with it", func() bool {
		var ss []proto.Session
		call(t, "session.list", nil, &ss)

		return len(ss) == 1 && ss[0].ID == other.ID
	})
}

func TestFocusResolvesSessionNames(t *testing.T) {
	d := start(t)()
	defer d.Close()

	call(t, "session.new", map[string]any{"workspace": t.TempDir(), "name": "agent", "cmd": []string{"sleep", "30"}}, nil)
	call(t, "focus", proto.FocusParams{Session: "agent"}, nil)

	if err := proto.Call("focus", proto.FocusParams{Session: "nobody"}, nil); err == nil {
		t.Fatal("focus on an unknown session is refused, not broadcast for no TUI to match")
	}
}

// TestXtermKey checks every special key with every modifier set a terminal
// can send: what xtermKey writes decodes back to the same key, the way the
// app in the session reads it, and what it leaves is what vt encodes itself.
func TestXtermKey(t *testing.T) {
	var d uv.EventDecoder

	keys := slices.Concat(slices.Collect(maps.Keys(xtermFinal)), slices.Collect(maps.Keys(xtermTilde)))
	mods := []uv.KeyMod{uv.ModShift, uv.ModCtrl, uv.ModMeta, uv.ModShift | uv.ModCtrl, uv.ModAlt | uv.ModCtrl, uv.ModAlt | uv.ModShift | uv.ModCtrl | uv.ModMeta}

	for _, code := range keys {
		for _, mod := range mods {
			seq, ok := xtermKey(code, mod)
			if !ok {
				t.Fatalf("key %d mod %d: not encoded", code, mod)
			}

			n, ev := d.Decode([]byte(seq))
			if multi, ok := ev.(uv.MultiEvent); ok { // F3 with modifiers is also a cursor report, as in xterm
				ev = multi[0]
			}

			if k, isKey := ev.(uv.KeyPressEvent); n != len(seq) || !isKey || k.Code != code || k.Mod != mod {
				t.Fatalf("key %d mod %d: %q decodes to %#v (%d of %d bytes)", code, mod, seq, ev, n, len(seq))
			}
		}

		for _, mod := range []uv.KeyMod{0, uv.ModAlt} {
			if _, ok := xtermKey(code, mod); ok {
				t.Fatalf("key %d mod %d is vt's to encode", code, mod)
			}
		}
	}

	if _, ok := xtermKey('a', uv.ModCtrl); ok {
		t.Fatal("ctrl+a is a control byte, vt's to send")
	}

	if _, ok := xtermKey(uv.KeyTab, uv.ModShift); ok {
		t.Fatal("shift+tab is vt's CSI Z")
	}
}
