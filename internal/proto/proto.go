// Package proto is the wire format and client side of pando's unix socket API:
// one JSON request line per connection, one JSON response line back.
// `subscribe` keeps its connection open and streams Event lines instead.
package proto

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Request is one call: a method name and the params that method expects,
// sent as a single JSON line.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the JSON line answering a Request: Result or Error, never both.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Event is pushed to subscribers. Kind is one of: state, workspaces,
// sessions, screen (ID = session), focus (Data = FocusParams),
// update (Data = Update).
type Event struct {
	Kind string          `json:"event"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Settings live in config.toml; the JSON names are the same as the TOML keys.
type Settings struct {
	Hidden    bool                `json:"hidden" toml:"hidden"`
	Icons     string              `json:"icons" toml:"icons"` // "nerd" | "emoji" | "ascii"
	Width     int                 `json:"width" toml:"width"` // left sidebar
	WidthR    int                 `json:"width_right" toml:"width_right"`
	GitDeco   bool                `json:"git_deco" toml:"git_deco"`
	Left      Columns             `json:"left" toml:"left"` // sidebar columns from the screen edge to main; unlisted views dock left
	Right     Columns             `json:"right" toml:"right"`
	GitTree   bool                `json:"git_tree" toml:"git_tree"`
	Drawers   []string            `json:"git_drawers" toml:"git_drawers"` // visible Source Control drawers
	GitPanes  map[string]Pane     `json:"git_panes" toml:"git_panes"`     // Git drawers by title
	QuickTree bool                `json:"quick_open_tree" toml:"quick_open_tree"`
	Theme     string              `json:"color_theme" toml:"color_theme"`             // "vscode" (dark or light by background) | "vscode-dark" | "vscode-light" | "terminal"
	DiffView  string              `json:"diff_view" toml:"diff_view"`                 // "inline" | "split"
	Borders   bool                `json:"panel_borders" toml:"panel_borders"`         // frame each panel with a titled border
	ActBar    string              `json:"activity_bar" toml:"activity_bar"`           // "top" (a row of chips) | "side" (icons down the outer edge)
	TermPos   string              `json:"terminal_position" toml:"terminal_position"` // "bottom" (a panel under the editor) | "left" | "right"
	TermH     int                 `json:"terminal_height" toml:"terminal_height"`     // rows of the bottom panel
	TermOpen  bool                `json:"terminal_open" toml:"terminal_open"`         // the panel's state for a workspace without one in State.Terminals: the last one set
	SessPos   string              `json:"session_position" toml:"session_position"`   // where a session opens: "right" | "left" (a column of its own) | "editor" (over the editor area)
	SessHi    string              `json:"session_highlight" toml:"session_highlight"` // a session waiting, done unseen or failed: "tint" its row and tab, pulsing until clicked | "steady" | "off"
	LSP       map[string][]string `json:"lsp" toml:"lsp"`                             // language to server command
	Format    map[string][]string `json:"format" toml:"format"`                       // language to formatter command, the file on stdin
	FmtSave   bool                `json:"format_on_save" toml:"format_on_save"`       // run that formatter when a file is saved
	Vim       bool                `json:"vim_mode" toml:"vim_mode"`                   // an editor opens in vim's normal mode
	Wrap      bool                `json:"word_wrap" toml:"word_wrap"`                 // editors wrap long lines instead of scrolling them sideways
	Keys      map[string]string   `json:"keys" toml:"keys"`                           // key to command id, "" unbinds; see pando doctor
	Colors    map[string]string   `json:"colors" toml:"colors"`                       // palette overrides for the active theme, keys in pando doctor
	Shell     string              `json:"shell" toml:"shell"`                         // the shell a terminal opens, "zsh -l"; "" for the [agents] shell preset, else the login shell
	Sounds    bool                `json:"sounds" toml:"sounds"`                       // play a sound when a background session finishes or blocks
	Updates   bool                `json:"update_check" toml:"update_check"`           // ask GitHub once a day whether a newer pando was released
	Anim      bool                `json:"animations" toml:"animations"`               // text effects: names morph, counts roll, busy labels shimmer
	SoundDone string              `json:"sound_done" toml:"sound_done"`               // the files played, "" for the terminal bell
	SoundReq  string              `json:"sound_request" toml:"sound_request"`
	SpSort    string              `json:"spaces_sort" toml:"spaces_sort"`   // Spaces sessions by "created" or "updated"
	SpGroup   string              `json:"spaces_group" toml:"spaces_group"` // Spaces rows: "workspace" (the project tree) or "time" (Today, Yesterday, …)
	SpHide    []string            `json:"spaces_hide" toml:"spaces_hide"`   // session states Spaces leaves out: blocked, running, done, idle, exited
}

// Column is one sidebar column: views shown as tabs, and its width in cells
// (0 = the side's default width).
type Column struct {
	Views []string `json:"views" toml:"views"`
	Width int      `json:"width,omitempty" toml:"width,omitempty"`
}

// Columns are one side's sidebar columns. A flat list of views, the format
// before columns, reads as a single column.
type Columns []Column

// UnmarshalJSON decodes either shape; a flat list of views becomes one column.
func (c *Columns) UnmarshalJSON(b []byte) error {
	var flat []string
	if json.Unmarshal(b, &flat) == nil {
		*c = nil
		if len(flat) > 0 {
			*c = Columns{{Views: flat}}
		}

		return nil
	}

	var cols []Column

	err := json.Unmarshal(b, &cols)
	*c = cols

	return err
}

// UnmarshalTOML accepts `["files", "git"]` and `[{ views = ["files"], width = 40 }]`,
// the same two shapes as JSON, so one decoder does both.
func (c *Columns) UnmarshalTOML(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("columns: %w", err)
	}

	return c.UnmarshalJSON(b)
}

// Pane is a Source Control drawer's remembered geometry: whether it is open,
// and how many rows tall.
type Pane struct {
	Open bool `json:"open" toml:"open"`
	H    int  `json:"h" toml:"h"`
}

// State is everything a client sees, the reply to state.get: the settings
// from config.toml plus what state.json remembers.
type State struct {
	Settings  Settings            `json:"settings"`
	Projects  []string            `json:"projects"`
	Agents    map[string][]string `json:"agents"`
	Resume    map[string][]string `json:"resume"`     // agent to the command that continues its last conversation
	ResumeID  map[string][]string `json:"resume_id"`  // agent to the command that continues conversation {id}
	ResumeJob map[string][]string `json:"resume_job"` // agent to the command that attaches to background job {id}
	Drafts    map[string]string   `json:"drafts"`
	Editors   map[string]Editors  `json:"editors"` // per workspace: what its tab strip had open
	// Terminals is per workspace: whether its Terminal panel was open, and
	// the shell it showed.
	Terminals map[string]Terminal `json:"terminals"`
	Sessions  []SessionSpec       `json:"sessions"`
	// Worktrees is each project's worktree paths in the order workspace.move
	// left them; git's order for any it does not name, after these.
	Worktrees map[string][]string `json:"worktrees,omitempty"`
	// LastWorkspace is the workspace the TUI showed last; pando started
	// outside a repository opens it again.
	LastWorkspace string `json:"last_workspace,omitempty"`
}

// Editors is a workspace's tab strip as state.json keeps it, VS Code's
// workbench.editors: the files open, the one showing, and where each was.
type Editors struct {
	Open   []Editor `json:"open"`
	Active int      `json:"active"` // index into Open
}

// Terminal is a workspace's Terminal panel as state.json keeps it, so every
// workspace opens and shuts its own.
type Terminal struct {
	Open bool   `json:"open"`
	Tab  string `json:"tab,omitempty"` // the shell it showed
}

// Editor is one open file with its cursor (0-based) and scroll. An untitled
// buffer has no path; Name is what the tab shows until it is saved.
type Editor struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"` // untitled buffer: "Untitled-1", path empty
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Top  int    `json:"top,omitempty"` // first visible screen row
	MD   int    `json:"md,omitempty"`  // Markdown: 0 source, 1 rendered, 2 both
}

// Draft is an editor's unsaved text. Drafts live as files in the data
// directory, not in state.json, so a buffer's size never bloats the state.
// Path is empty for an untitled buffer, which Name identifies instead.
type Draft struct {
	WS   string    `json:"ws"`
	Path string    `json:"path,omitempty"`
	Name string    `json:"name,omitempty"` // untitled: "Untitled-1"
	Text string    `json:"text"`
	Mod  time.Time `json:"mod"` // the file's mtime when the editor read it
}

// Key names the draft within its workspace: the file's path, or the untitled
// buffer's name. Both sides of the socket derive the storage id from it.
func (d Draft) Key() string {
	if d.Path != "" {
		return d.Path
	}

	return "untitled:" + d.Name
}

// SessionSpec is a session as state.json stores it: what a restarted daemon
// needs to respawn it.
type SessionSpec struct {
	ID        string   `json:"id"`
	Workspace string   `json:"workspace"`
	Agent     string   `json:"agent"`
	Cmd       []string `json:"cmd"`
	Resume    []string `json:"resume,omitempty"` // what a restarted daemon types to bring the agent back
	// Conversation is the agent conversation the session had open, when the
	// agent says which: a restart continues it once, not the latest one.
	Conversation *Conversation `json:"conversation,omitempty"`
	Name         string        `json:"name,omitempty"`   // the tab's own name, from a rename
	Parent       string        `json:"parent,omitempty"` // the agent session whose Terminal panel this shell belongs to; killed with it
	FG           string        `json:"fg,omitempty"`     // host terminal colors, #rrggbb
	BG           string        `json:"bg,omitempty"`
	Created      time.Time     `json:"created,omitzero"` // when session.new made it; zero for sessions older than the field
}

// Conversation is an agent's conversation by the agent's own id.
type Conversation struct {
	Agent string `json:"agent"` // the program, a [resume_id] key
	ID    string `json:"id"`
	Job   string `json:"job,omitempty"` // the background job it runs in, which outlives pando: attach to it
	// Env is KEY=VALUE the agent ran with that its shell does not set, such
	// as CLAUDE_CONFIG_DIR: the command that brings it back sets them again.
	Env []string `json:"env,omitempty"`
}

// Workspace is one git worktree of a project, as workspace.list reports it.
type Workspace struct {
	Path    string `json:"path"`
	Project string `json:"project"`
	Branch  string `json:"branch"`
	Main    bool   `json:"main"`
}

// Session is a live session: its spec plus what the daemon sees of the
// process running in it.
type Session struct {
	SessionSpec
	Status    string `json:"status"` // blocked | running | idle | exited
	ExitCode  int    `json:"exit_code"`
	Attention bool   `json:"attention"`
	Title     string `json:"title"`
	Program   string `json:"program,omitempty"` // what runs in its foreground: the shell, or claude started in it
	// Updated is its last output, Created before it printed anything; it
	// starts over when a restarted daemon respawns the session.
	Updated time.Time `json:"updated,omitzero"`
}

// Update is what the daemon knows about a newer pando: the reply to
// update.status and update.check, and the data of every `update` event.
type Update struct {
	Current string `json:"current"` // the version this binary was built from
	Latest  string `json:"latest,omitempty"`
	Notes   string `json:"notes,omitempty"`
	// State is "" (nothing known yet), "checking", "available",
	// "downloading" (Done of Total bytes), "ready" (installed, restart to
	// run it) or "error".
	State string `json:"state"`
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
	Error string `json:"error,omitempty"`
}

// Percent is how much of the download is in, 0 while the size is unknown.
func (u Update) Percent() int {
	if u.Total <= 0 {
		return 0
	}

	return int(min(u.Done*100/u.Total, 100))
}

// Screen is a session's rendered terminal, the reply to session.screen.
type Screen struct {
	Lines         []string `json:"lines"`
	CursorX       int      `json:"cursor_x"`
	CursorY       int      `json:"cursor_y"`
	CursorVisible bool     `json:"cursor_visible"`
	Mouse         bool     `json:"mouse"` // app enabled mouse reporting
	// Scrollback is the lines Scroll can page back into; 0 while the app
	// draws on the alternate screen, which has none and scrolls itself.
	Scrollback int  `json:"scrollback"`
	AltScreen  bool `json:"alt_screen,omitempty"` // the app is on the alternate screen, as vim, less and a fullscreen claude are
}

// Key mirrors uv.Key so the daemon's emulator can encode it for the PTY
// according to the app's current terminal modes.
type Key struct {
	Code rune   `json:"code"`
	Mod  int    `json:"mod,omitempty"`
	Text string `json:"text,omitempty"`
}

// Mouse is a mouse event to replay into a session's terminal.
type Mouse struct {
	Kind   string `json:"kind"` // click | release | motion | wheel
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button int    `json:"button"`
	Mod    int    `json:"mod,omitempty"`
}

// InputParams is session.input. Every field that is set is delivered, in
// field order: Text goes straight to the PTY, the rest through the emulator
// so the app's terminal modes decide the encoding.
type InputParams struct {
	ID    string `json:"id"`
	Text  string `json:"text,omitempty"` // raw bytes to the PTY
	Keys  []Key  `json:"keys,omitempty"`
	Paste string `json:"paste,omitempty"`
	Mouse *Mouse `json:"mouse,omitempty"`
}

// ReadParams is session.read: the session's screen as plain text. Scrollback
// prepends everything the emulator still holds; Lines keeps only the last N
// of what that selects, which is how a caller asks for a tail without
// pulling ten thousand lines through the socket.
type ReadParams struct {
	ID         string `json:"id"`
	Scrollback bool   `json:"scrollback,omitempty"`
	Lines      int    `json:"lines,omitempty"`
}

// WaitParams is session.wait: block until the session's status is one of
// Until, or its screen matches Match, whichever happens first. It is the call
// that lets a script drive an agent without polling.
type WaitParams struct {
	ID string `json:"id"`
	// Until is any of idle, blocked, running, exited. Empty waits for the
	// settled states: idle, blocked, exited.
	Until []string `json:"until,omitempty"`
	// Match is a Go regular expression tested against the screen text. Set on
	// its own it is the only condition; with Until, either one returns.
	Match string `json:"match,omitempty"`
	// Timeout in milliseconds. 0 waits forever.
	Timeout int `json:"timeout_ms,omitempty"`
}

// Settled are the statuses session.wait stops at when Until says nothing:
// every state in which the session is no longer working on its own.
var Settled = []string{"idle", "blocked", "exited"}

// ScreenParams is session.screen. Cols and Rows resize the session when both
// are set; Scroll pages back into scrollback.
type ScreenParams struct {
	ID     string `json:"id"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Scroll int    `json:"scroll,omitempty"` // lines back into scrollback
}

// MoveParams is project.move and workspace.move: Path takes the place To
// holds now, among the projects or among its project's worktrees, and the
// ones after it shift up. An index outside the list is clamped to it.
type MoveParams struct {
	Path string `json:"path"`
	To   int    `json:"to"`
}

// SessionMoveParams is session.move: session ID takes the place session To
// holds now, in the order session.list and the Spaces tree list sessions.
// Both are sessions of the same workspace, not tabs; ID's tabs move with it.
type SessionMoveParams struct {
	ID string `json:"id"`
	To string `json:"to"`
}

// FocusParams is focus: the daemon broadcasts it unchanged so every attached
// TUI shows the same workspace, session or file.
type FocusParams struct {
	Workspace string `json:"workspace,omitempty"`
	Session   string `json:"session,omitempty"`
	Open      string `json:"open,omitempty"`
}

// Dir is pando's runtime directory, holding the socket, the lock and the
// daemon log.
func Dir() string {
	if d := os.Getenv("PANDO_RUNTIME_DIR"); d != "" {
		return d
	}

	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "pando")
	}

	return filepath.Join(os.TempDir(), fmt.Sprintf("pando-%d", os.Getuid()))
}

// SocketPath is the unix socket every client dials.
func SocketPath() string { return filepath.Join(Dir(), "pando.sock") }

func dial() (net.Conn, error) { return net.DialTimeout("unix", SocketPath(), time.Second) }

// Call sends one request and decodes the reply into result, which may be nil;
// a *json.RawMessage keeps it raw. The one-second timeout covers the dial
// only: a daemon that accepts the connection and then wedges blocks the
// caller, so the TUI makes its calls from a tea.Cmd.
func Call(method string, params, result any) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	req := Request{Method: method}
	if params != nil {
		if req.Params, err = json.Marshal(params); err != nil {
			return err
		}
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}

	if resp.Error != "" {
		return errors.New(resp.Error)
	}

	if result != nil && resp.Result != nil {
		return json.Unmarshal(resp.Result, result)
	}

	return nil
}

// Subscribe streams events until the connection drops, then closes the
// channel. Sends block on a 64-deep buffer, so the caller must keep receiving
// until the close; abandoning the channel strands the goroutine and the
// connection for the life of the process.
func Subscribe() (<-chan Event, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}

	if err := json.NewEncoder(conn).Encode(Request{Method: "subscribe"}); err != nil {
		_ = conn.Close()
		return nil, err
	}

	ch := make(chan Event, 64)

	go func() {
		defer func() { _ = conn.Close() }()
		defer close(ch)

		dec := json.NewDecoder(conn)

		for {
			var ev Event
			if dec.Decode(&ev) != nil {
				return
			}

			ch <- ev
		}
	}()

	return ch, nil
}

// EnsureDaemon starts `pando serve` in the background unless one answers.
func EnsureDaemon() error {
	if Call("ping", nil, nil) == nil {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}

	logf, err := os.OpenFile(filepath.Join(Dir(), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	defer func() { _ = logf.Close() }() // the daemon keeps its own descriptor

	cmd := exec.Command(exe, "serve")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = WithoutNoColor(os.Environ())

	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it if it exits while we run

	for range 50 {
		time.Sleep(40 * time.Millisecond)

		if Call("ping", nil, nil) == nil {
			return nil
		}
	}

	return fmt.Errorf("daemon did not start, see %s", filepath.Join(Dir(), "daemon.log"))
}

// BuildID identifies this pando binary; a daemon reports its own from ping.
// ponytail: size and mtime of the executable, not a content hash.
var BuildID = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	st, err := os.Stat(exe)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano())
})

// DaemonBuild is the running daemon's BuildID, "" for daemons that predate it.
func DaemonBuild() string {
	var r struct {
		Build string `json:"build"`
	}
	if Call("ping", nil, &r) != nil {
		return ""
	}

	return r.Build
}

// RestartDaemon replaces the running daemon with this binary's. Sessions
// are respawned from their saved specs; their scrollback is lost.
func RestartDaemon() error {
	_ = Call("shutdown", nil, nil) // no daemon answering is the state we want
	for i := 0; i < 100 && Call("ping", nil, nil) == nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}

	var err error
	for range 10 { // the old process keeps the lock until its sessions are gone
		if err = EnsureDaemon(); err == nil {
			return nil
		}
	}

	return err
}

// WithoutNoColor drops NO_COLOR: agent shells set it, and a daemon or TUI
// inheriting it would render every session and the UI monochrome.
func WithoutNoColor(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "NO_COLOR=")
	})
}
