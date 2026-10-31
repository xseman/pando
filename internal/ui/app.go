// Package ui is pando's Bubble Tea client: views (Files, Git, Agents) docked
// in a left and a right sidebar around a main area showing the active agent
// session or a file preview, drawn like VS Code.
package ui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/font"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/lsp"
	"github.com/xseman/pando/internal/proto"
)

type view int

const (
	viewFiles view = iota
	viewGit
	viewAgents
	viewSearch
	viewTerm
	viewGitHub // pull requests, issues and notifications through gh, where it is installed
	viewDB     // the worktree's SQLite files and the servers it names, through their clients, where one is installed
)

var (
	viewTitles = []string{"Explorer", "Source Control", "Spaces", "Search", "Terminal", "GitHub", "Databases"}
	viewKeys   = []string{"files", "git", "agents", "search", "terminal", "github", "databases"} // values of settings.left and settings.right
	// showKeys show each view from anywhere where there is such a key, else
	// from a sidebar: the Terminal's toggles the panel.
	showKeys = []string{"^⇧e", "^⇧g", "3", "^⇧f", "^`", "6", "7"}
)

// hasGH reports gh on the PATH, which the GitHub view needs; tests turn it off.
var hasGH = git.HasGH

// onMain is the focus of the main area, onPanel the terminal panel under it;
// sidebar columns are focused by index.
const (
	onMain  = -1
	onPanel = -2
)

const (
	dragDivider = iota + 1
	dragPane
	dragSelect
	dragTab
	dragTerm
	dragRow
	dragScroll
	dragMsgSel
	dragStrip
	dragGrid
	dragOpen
)

// actH is the height of a sidebar's activity bar: the icons, then the row
// carrying the active border under them.
const actH = 2

// vertH is the height of a chip down a side activity bar: VS Code's 48 px
// button, a little over twice its list row; the icon on the second row, the
// first air over it.
const vertH = 2

// actW is the width of the vertical activity bar: one chip, the same the top
// bar draws, so both bars hold the same button.
func actW() int { return 2*len(barPad()) + ansi.StringWidth(icFiles.short()) }

// railW is the width a hidden side keeps: VS Code's activity bar, the
// side's view icons stacked down it as the side bar draws them, a click on
// one opening that view again.
func railW() int { return actW() }

// drag is a mouse gesture from a click until its release.
type drag struct {
	kind    int
	col     int    // dragDivider: the column being resized
	pane    string // dragPane: Git drawer title, or GitHub section title with v viewGitHub
	v       view   // dragTab: the tab being dragged; dragPane: viewGit or viewGitHub
	proj    string // dragRow: the project being moved in the Spaces list
	sess    string // dragRow: the session being moved under its worktree instead
	ws      string // dragRow: the worktree being moved under its project instead
	to      string // dragRow: the one of its kind whose place it would take on release, "" none
	strip   int    // dragStrip: the tab strip, stripEditor and on
	tab     string // dragStrip: the tab being moved, an editor's or a session's id; dragOpen: the file's held from Explorer, or the session's from Spaces
	org     int    // dragStrip: the screen column of the strip's first cell
	slot    int    // dragStrip: where the tab would land, an index in stripOrder; -1 nowhere
	x0      int    // dragTab, dragStrip: where it was picked up; dragRow: the row's column pressed; dragMsgSel: the message text's screen origin, with y0
	drop    *dropTarget
	split   *pane // dragGrid: the split whose divider moves
	dst     *pane // dragStrip, dragOpen, dragRow's session: the group the tab would go to, or beside on side
	side    int
	y0, h0  int
	moved   bool
	fine    bool        // dragTab: one row of motion is a drag (vertical bar chips sit a row apart)
	hide    bool        // dragTab: pressed on the open view's own chip, so let go where it was it hides its side
	bar     *scrollDrag // dragScroll: the scrollbar and where its slider was picked up
	chip    string      // dragStrip, dragOpen, dragRow's session: the tab the pointer carries (dragGhost), "" until it moves
	chipOff int         // the chip's column under the pointer
}

// dropTarget is where a dragged tab would land, drawn as a rectangle: over
// the whole sidebar it would open, or between the chips of the activity bar
// it would join.
type dropTarget struct {
	rc    rect
	y, h  int // the rows the rectangle covers; h 0 is the full panel height
	side  int
	col   int // the column joined, with at the tab's index in it; -1 otherwise
	at    int
	own   bool // a column of its own at that edge, instead of a tab of the one there
	under bool // the panel's place under the editor
	fill  bool // emptied and labelled though it covers h rows, as a group a tab splits off
	label string
}

type rect struct{ x, w int }

// frame is cols and layout as they stand for one View: drawing changes
// neither.
type frame struct {
	cols  []col
	rects []rect
	main  rect
}

// col is one sidebar column: tabs, the side of main it docks on, and its
// configured width (0 = the side's default).
type col struct {
	views []view
	right bool
	width int
}

// Model is the whole client: the daemon's state, the views docked in the
// sidebar columns, and the main area's editor or agent session.
type Model struct {
	w, h      int // h excludes the panel border rows
	termH     int
	st        proto.State
	wss       []proto.Workspace
	sessions  []proto.Session
	ws, sess  string
	focus     int                  // a column index or onMain
	recent    []int                // per view: when it was last shown; a column shows its most recent tab
	savedWs   string               // the workspace the daemon was last told about
	savedEds  proto.Editors        // the open editors the daemon was last told about
	waiting   map[string][]preview // table tabs with edits, and cell editors typed in, left in a workspace: no draft keeps them
	savedTerm proto.Terminal       // the workspace's Terminal panel, as this TUI last told the daemon

	savedDrafts map[string]string // per draft key: the unsaved text the daemon was last told about
	branchSeen  string            // the open worktree's branch the worktree list was last reloaded for
	ticks       int
	clock       int      // counter behind recent
	files       []string // files opened, most recent first, for quick open
	open        string   // the FILE[:LINE[:COL]] pando was started on, which Init opens
	hidden      [2]bool  // per side, left and right: folded into a rail
	unfold      [2]bool  // per side: opened by hand while squeezed, so it stays open until the next resize
	preview     bool     // main shows the preview instead of the session
	dark        bool
	fg, bg      string // host terminal colors as #rrggbb, passed to new sessions
	msg         string
	msgErr      bool
	msgAt       time.Time
	soundAt     time.Time         // when the last sound cue played
	seen        map[string]string // session to the state it was last clicked or shown in
	blinkOn     bool              // a pulsing tint is at its full shade
	blinking    bool              // its ticker runs
	spinWake    bool              // a spinMsg is on its way
	waking      map[string]bool   // sessions a session.wake is on its way for
	fx          effects           // text effects running, and what they compare against
	events      <-chan proto.Event
	inputs      chan proto.InputParams
	gitBusy     bool
	gitNext     time.Time // when the tick may refresh git again
	clicks      clicks
	drag        *drag
	// ambiguous marks a terminal that cannot tell ⌃` from ⌃space: both
	// arrive as NUL, so an editor's suggestions swallow the one that would
	// have opened the terminal panel. saidCtrlJ keeps the hint to once.
	ambiguous bool
	// spill is what a mouse report tmux typed out may go on with, see unspill.
	spill     string
	saidCtrlJ bool
	graphemes bool      // the terminal answered mode 2027: Bubble Tea measures the frame as it is laid out (wcSafe)
	tv        termPanel // the Terminal panel's shell
	termMax   bool      // the panel fills the editor area (⌃⇧↑)
	grid      *pane     // the editor groups, VS Code's grid: one group, or splits of them
	cur       *pane     // the group with the keyboard, its state in Model; a leaf of grid
	maxed     bool      // the active group fills the editor area, the others waiting

	filters   [viewDB + 1]filter // per view
	gh        ghView
	ghPages   map[string]ghCached        // the pages of pull requests and issues seen, shown again while they are fetched
	ghDrafts  map[string]*textarea.Model // each page's comment box, kept with what was typed in it when the page closes
	ghUser    string                     // who gh is signed in as: the comments that tag them are marked
	hasGH     bool                       // gh is installed: the GitHub view shows
	db        dbView
	hasDB     bool // a database client is installed: the Databases view shows
	mouseX    int  // last mouse position (content rows) for hover
	mouseY    int
	overBar   barPoint  // the scrollbar cell under the mouse: its slider hovers
	overTab   string    // the tab under the mouse, in any strip: its chip hovers
	overX     string    // the tab whose ✕ is under the mouse: the ✕ raises
	sashAt    sash      // the divider under the mouse, noSash for none
	sashSince time.Time // when the mouse came onto it
	mouseAt   time.Time
	tip       string              // the name and key of the button under the mouse in the frame being drawn
	sessTabs  map[string][]string // each workspace's sessions opened over the editor area, their tabs in its strip: roots, in the order opened
	sessAfter map[string][]string // a session tab moved among the files: the files' ids before it then (sessPos)
	tipDue    time.Time           // when the tipWakeMsg on its way comes, zero for none
	tipSeen   time.Time           // when a tip was last drawn: moving on, the next one shows at once
	tipOff    time.Time           // a key or a click puts the tip away until the pointer moves
	frame     *frame              // the columns while View draws, which asks for them row by row; nil otherwise
	drawn     *tea.View           // the last frame View drew, which a still message gets again
	drawnAt   time.Time           // when View drew it
	base      *base               // the frame under a drag's overlays (baseKey)
	safeIn    []string            // the last frame's lines (wcSafeLines)
	safeOut   []string            // and each made wcSafe
	still     bool                // the last message changed nothing on screen (stillEvent), or a motion waits for the next frame
	redrawDue bool                // a redrawMsg is on its way for the motion View skipped
	index     struct {            // workspace file list for quick open and the Files filter
		ws    string
		files []string
		at    time.Time
	}

	// fits and cuts are the rows fitted in this frame and the last, and cut
	// around an overlay (overlayBox); bars each column's activity bar as last
	// drawn.
	fits memo[fitKey, string]
	cuts memo[cutKey, [2]string]
	bars map[int]barDrawn

	ex  explorer
	scm scmView
	ag  agents
	sr  searchView

	lastCommand string // the command palette entry run last, listed first next time
	vimReg      string // vim_mode: the unnamed register, shared by every editor
	vimRegLine  bool   // what it holds was yanked line-wise, so p pastes it as lines
	pv          preview
	ptab        panelTab  // the bottom panel's tab: Terminal, Problems, References, Output
	probs, refs *peek     // the panel's Problems and References; nil for none
	out         outputLog // the panel's Output
	logIn       *logBox   // Output lines from the language servers' readers
	lsps        lspPool
	// lsp_diagnostics: the servers' readers leave diagnostics in diagIn;
	// diags are each file's, probDeco the colors they give Explorer and
	// Source Control. diagSeen and diagSeq time the shown editor's sync,
	// diagBusy is a sync of the changed files running, diagWarned a server
	// that would not start, said once.
	diagIn     *diagBox
	diags      map[string][]lsp.Diagnostic
	probDeco   map[string]probDeco
	diagSeen   editSeen
	diagSeq    int
	blame      blameState // the cursor's line's blame, for the status bar
	diagBusy   bool
	diagWarned map[string]bool
	diagOn     bool                   // lsp_diagnostics as noteDiags last saw it
	diagFailed map[string]diagFailMsg // a file whose server would not start, which the status bar says
	// Open editors: the tab strip in main, and where navigation has been.
	editors   []preview
	edIdx     int
	edUsed    int // the last preview.used handed out
	nav       []preview
	navAt     int
	restoring bool

	upd   proto.Update // what the daemon's last update check found
	nf    string       // the Nerd Font offer: "offer", "installing" or "" for none
	term  term
	modal *modal
}

type (
	eventMsg        proto.Event
	disconnectedMsg struct{}
	reconnectMsg    struct {
		ch  <-chan proto.Event
		err error
	}
	stateMsg      proto.State
	workspacesMsg []proto.Workspace
	sessionsMsg   []proto.Session
	tickMsg       struct{}
	blinkMsg      struct{}
	spinMsg       struct{} // turns every working session's ◐
	flashMsg      struct {
		text string
		err  bool
	}
	gitMsg struct {
		ws     string
		repos  []string
		status map[string]git.Status
		deco   map[string]byte
		took   time.Duration // how long git took: the tick waits ten times that before the next
	}
	indexMsg struct {
		ws    string
		files []string
		open  bool   // show quick open once loaded
		query string // a terminal link's path, which quick open looks for (openQuery)
		// line and col are the link's position, 1-based, 0 for none.
		line, col int
	}
)

// Run connects to (or starts) the daemon and runs the TUI. dir is the
// directory pando was started with; empty means none was given, and the
// workspace shown last opens instead.
func Run(dir, open string) error {
	if err := proto.EnsureDaemon(); err != nil {
		return err
	}
	// A daemon from an older build drops settings and calls it doesn't know.
	// Replace it silently when nothing runs in it, otherwise ask.
	stale := proto.DaemonBuild() != proto.BuildID()
	if stale {
		var running []proto.Session

		_ = proto.Call("session.list", nil, &running) // on error nothing is known to run
		if len(running) == 0 {
			if err := proto.RestartDaemon(); err != nil {
				return err
			}

			stale = false
		}
	}

	var (
		st  proto.State
		wss []proto.Workspace
		ss  []proto.Session
	)

	if err := proto.Call("state.get", nil, &st); err != nil {
		return err
	}

	_ = proto.Call("workspace.list", nil, &wss) // an empty list still opens dir

	ws, last := lastWorkspace(dir, st, wss)
	if !last {
		var err error
		if ws, wss, err = addWorkspace(dir); err != nil {
			return err
		}
	}

	_ = proto.Call("session.list", nil, &ss) // pando opens with no sessions listed

	events, err := proto.Subscribe()
	if err != nil {
		return err
	}

	m := New(st, wss, ss, ws, events)
	m.open = open

	if fam := terminalFont(); st.Settings.Icons == "nerd" && fam != "" && !nerdFamily(fam) && !font.Installed() { // an installed one is the fallback
		m.flash("terminal font lacks Nerd Font icons · pando doctor", true)
	}

	if stale {
		m.modal = staleModal(len(ss))
	}

	go func() { // one ordered writer, so keystrokes never reorder
		for p := range m.inputs {
			_ = proto.Call("session.input", p, nil) // a dead session drops its keystrokes
		}
	}()

	_, err = tea.NewProgram(m, tea.WithEnvironment(proto.WithoutNoColor(os.Environ())), tea.WithOutput(defaultCursor{os.Stdout})).Run()

	return err
}

// appCursor is a session's cursor at x, y in the style its app asked for
// with DECSCUSR n; 0 and 1 leave it to the terminal (defaultCursor).
func appCursor(x, y, n int) *tea.Cursor {
	c := tea.NewCursor(x, y)
	if n > 1 {
		c.Shape, c.Blink = tea.CursorShape((n-1)/2), n%2 == 1
	}

	return c
}

// defaultCursor is stdout with the terminal's own cursor. Bubble Tea has no
// way to ask for none: its plain cursor is a blinking block (DECSCUSR 1), so
// that becomes DECSCUSR 0, the shape and blink the terminal is set to. It also
// leaves the terminal at its default when pando exits.
type defaultCursor struct{ *os.File }

func (o defaultCursor) Write(b []byte) (int, error) { return o.WriteString(string(b)) }

// WriteString replaces the file's own, which io.WriteString would call.
func (o defaultCursor) WriteString(s string) (int, error) {
	return o.File.WriteString(strings.ReplaceAll(s, "\x1b[1 q", "\x1b[0 q"))
}

// addWorkspace makes dir a project of its own and returns the workspace to
// open in it, the innermost one holding dir, with the workspace list the
// project was added to. An empty dir is where pando was started.
func addWorkspace(dir string) (string, []proto.Workspace, error) {
	if dir == "" { // nothing to reopen: the first run starts where it was called
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", nil, err
		}
	}

	if r, err := filepath.EvalSymlinks(dir); err == nil { // as git prints it, as the workspaces are
		dir = r
	}

	var ws string
	if err := proto.Call("project.add", map[string]string{"path": dir}, &ws); err != nil {
		return "", nil, err
	}

	var wss []proto.Workspace

	_ = proto.Call("workspace.list", nil, &wss) // the project was just added
	for _, w := range wss {
		if (dir == w.Path || strings.HasPrefix(dir, w.Path+string(filepath.Separator))) && len(w.Path) >= len(ws) {
			ws = w.Path
		}
	}

	return ws, wss, nil
}

// lastWorkspace is the workspace pando opens when it is started with no
// directory, as VS Code reopens its last folder; ok is false when there is
// none left to reopen and a directory is added as a project instead.
func lastWorkspace(dir string, st proto.State, wss []proto.Workspace) (ws string, ok bool) {
	if dir != "" || st.LastWorkspace == "" {
		return "", false
	}

	return st.LastWorkspace, slices.ContainsFunc(wss, func(w proto.Workspace) bool { return w.Path == st.LastWorkspace })
}

// saveWorkspace tells the daemon which workspace is open, for lastWorkspace.
func (m *Model) saveWorkspace() tea.Cmd {
	if m.ws == "" || m.ws == m.savedWs {
		return nil
	}

	m.savedWs = m.ws

	return do("state.set", map[string]any{"last_workspace": m.ws})
}

// New builds the model for one workspace out of the state the daemon handed
// over, reading its events from events.
func New(st proto.State, wss []proto.Workspace, ss []proto.Session, ws string, events <-chan proto.Event) *Model {
	m := &Model{
		st: st, wss: wss, sessions: ss, recent: make([]int, len(viewKeys)), dark: true, events: events,
		inputs: make(chan proto.InputParams, 512), mouseY: -1, edIdx: -1, navAt: -1, sashAt: noSash,
		ambiguous:  true, // until the terminal answers that it disambiguates keys
		hasGH:      hasGH(),
		hasDB:      hasDB(),
		diagIn:     newDiagBox(),
		logIn:      newLogBox(),
		diags:      map[string][]lsp.Diagnostic{},
		diagFailed: map[string]diagFailMsg{}, diagWarned: map[string]bool{},
	}

	m.grid = newPane()
	m.cur = m.grid

	m.lsps.sink, m.lsps.log = m.diagIn.put, m.logIn.put
	for i := range m.filters {
		m.filters[i] = newFilter()
	}

	m.look()
	tabW = m.tabWidth()
	m.ag.l.sel = -1
	m.scm.init()
	m.sr.init()
	m.gh.init()
	m.switchWorkspace(ws) // attaches the Terminal panel too; Init starts a shell when it found none

	m.ag.collapsed, m.ag.folded = map[string]bool{}, nil // after the switch: its reveal must not unfold what was left folded
	for _, k := range slices.Backward(st.Folded) {
		m.ag.fold(k, true)
	}

	m.ag.l.sel = slices.IndexFunc(m.ag.rows(m), func(r agRow) bool { return r.shows(ws) })

	return m
}

func (m *Model) look() {
	applyLook(m.st.Settings.Theme, m.dark, m.st.Settings.Icons, m.st.Settings.Colors)
}

// Init asks for the terminal's colours and starts the clock, the event
// stream and the first loads.
func (m *Model) Init() tea.Cmd {
	// m.pv is the editor New restored for the workspace; its content loads here.
	cmds := []tea.Cmd{
		waitEvent(m.events), tick(), tea.RequestBackgroundColor, tea.RequestForegroundColor,
		m.refreshGit(), m.pv.load(m), m.loadDrafts(), loadUpdate(), checkFont(), m.ensureTerm(), m.gh.shown(m), m.loadGHUser(), m.diagIn.waitDiags(), m.logIn.waitLogs(),
	}
	if m.open != "" { // over the restored editor, whose answer is then for another
		cmds = append(cmds, m.openSpec(m.open))
	}

	return tea.Batch(cmds...)
}

// ghUserMsg is who gh is signed in as, which `gh api user` tells once.
type ghUserMsg string

func (m *Model) loadGHUser() tea.Cmd {
	if !m.hasGH {
		return nil
	}

	return func() tea.Msg { return ghUserMsg(git.GHLogin()) }
}

func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}

	r, g, b, _ := c.RGBA()

	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func waitEvent(ch <-chan proto.Event) tea.Cmd {
	if ch == nil {
		return nil
	}

	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return disconnectedMsg{}
		}

		return eventMsg(ev)
	}
}

func tick() tea.Cmd { return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// do runs a daemon call in the background, flashing failures.
func do(method string, params any) tea.Cmd {
	return func() tea.Msg {
		if err := proto.Call(method, params, nil); err != nil {
			return flashMsg{method + ": " + err.Error(), true}
		}

		return nil
	}
}

func load[T any](method string, wrap func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		var v T
		if err := proto.Call(method, nil, &v); err != nil {
			return flashMsg{method + ": " + err.Error(), true}
		}

		return wrap(v)
	}
}

func loadState() tea.Cmd {
	return load("state.get", func(v proto.State) tea.Msg { return stateMsg(v) })
}

func loadWorkspaces() tea.Cmd {
	return load("workspace.list", func(v []proto.Workspace) tea.Msg { return workspacesMsg(v) })
}

func loadSessions() tea.Cmd {
	return load("session.list", func(v []proto.Session) tea.Msg { return sessionsMsg(v) })
}

func flash(text string, err bool) tea.Cmd {
	return func() tea.Msg { return flashMsg{text, err} }
}

// setSettings applies a settings patch locally and persists it in the daemon.
func (m *Model) setSettings(patch map[string]any) tea.Cmd { return m.setSettingsWith(patch, nil) }

// setSettingsWith is setSettings with more of the state (its other state.set
// keys) in the same call, so no state event lands between the two halves.
func (m *Model) setSettingsWith(patch, more map[string]any) tea.Cmd {
	b, _ := json.Marshal(patch)
	_ = json.Unmarshal(b, &m.st.Settings) // optimistic: the daemon sends the settings back
	m.look()
	m.resize()
	m.syncWrap()
	m.fixFocus()
	m.ex.rebuild(m)
	m.scm.build(m)

	p := map[string]any{"settings": patch}
	maps.Copy(p, more)

	cmds := []tea.Cmd{do("state.set", p)}

	if _, ok := patch["color_theme"]; ok {
		m.pv.raw = "" // re-render the tints in the new palette
		cmds = append(cmds, m.pv.load(m))
	}

	cmds = append(cmds, m.syncTabs())

	return tea.Batch(cmds...)
}

// tickGit is the tick's refresh: git at most a tenth of the time, so a
// worktree whose status takes a second is read every ten, not every two.
// What changes it here (a save, a stage, a commit) refreshes at once.
func (m *Model) tickGit() tea.Cmd {
	if time.Now().Before(m.gitNext) {
		return nil
	}

	return m.refreshGit()
}

func (m *Model) refreshGit() tea.Cmd {
	if m.gitBusy {
		return nil
	}

	m.gitBusy = true
	ws, withDeco := m.ws, m.st.Settings.GitDeco

	return func() tea.Msg {
		start := time.Now()

		msg := gitMsg{ws: ws, repos: git.Discover(ws), status: map[string]git.Status{}, deco: map[string]byte{}}
		for _, r := range msg.repos {
			if st, err := git.Stat(r); err == nil {
				msg.status[r] = st
				if withDeco {
					maps.Copy(msg.deco, git.Decorations(r, st))
				}
			}
		}

		msg.took = time.Since(start)

		return msg
	}
}

// drawers are the Source Control history drawers the settings show, top to
// bottom: open ones above closed ones, each in git_drawers order.
func (m *Model) drawers() []git.Drawer {
	want := m.st.Settings.Drawers
	if want == nil {
		want = defaultDrawers
	}

	var out []git.Drawer

	for _, t := range want {
		if i := slices.IndexFunc(git.Drawers, func(d git.Drawer) bool { return d.Title == t }); i >= 0 {
			out = append(out, git.Drawers[i])
		}
	}

	slices.SortStableFunc(out, func(a, b git.Drawer) int { return b2i(m.pane(b.Title).Open) - b2i(m.pane(a.Title).Open) })

	return out
}

// toggleDrawer shows a history drawer at the bottom, or hides it.
func (m *Model) toggleDrawer(title string) tea.Cmd {
	var want []string

	for _, d := range m.drawers() {
		if d.Title != title {
			want = append(want, d.Title)
		}
	}

	if len(want) == len(m.drawers()) {
		want = append(want, title)
	}

	return tea.Batch(m.setSettings(map[string]any{"git_drawers": want}), m.scm.loadDrawers(m))
}

func (m *Model) loadIndex(open bool) tea.Cmd {
	ws := m.ws
	return func() tea.Msg { return indexMsg{ws: ws, files: git.ListFiles(ws, 20000), open: open} }
}

// cols lists the sidebar columns from the left edge of the screen: left
// columns, then right ones. Views missing from the settings (a fresh config,
// or a view added later) join the left column next to main.
func (m *Model) cols() []col {
	if m.frame != nil {
		return m.frame.cols
	}

	return m.allCols()
}

// allCols are the columns as the settings have them.
func (m *Model) allCols() []col {
	seen := make([]bool, len(viewKeys))

	var out []col

	for side, cs := range [2]proto.Columns{m.st.Settings.Left, m.st.Settings.Right} {
		for _, c := range cs {
			k := col{right: side == 1, width: c.Width}
			for _, n := range c.Views {
				if v := slices.Index(viewKeys, n); v >= 0 && !seen[v] {
					seen[v] = true
					k.views = append(k.views, view(v))
				}
			}

			if len(k.views) > 0 {
				out = append(out, k)
			}
		}
	}

	side, docked := 0, m.termPos() != "bottom"
	if m.termPos() == "right" {
		side = 1
	}

	if !docked || !m.termOpen() { // a shut terminal is no column of its own
		out = slices.DeleteFunc(out, func(c col) bool { return slices.Equal(c.views, []view{viewTerm}) })
	}

	if !docked { // nor is a bottom one a tab
		for i := range out {
			out[i].views = slices.DeleteFunc(out[i].views, func(v view) bool { return v == viewTerm })
		}
	}

	if !m.hasGH { // no gh, no GitHub view
		out = removeView(out, viewGitHub)
	}

	if !m.hasDB {
		out = removeView(out, viewDB)
	}

	for v, ok := range seen {
		if view(v) == viewGitHub && !m.hasGH || view(v) == viewDB && !m.hasDB {
			continue
		}

		if view(v) == viewTerm {
			if docked && m.termOpen() && !slices.ContainsFunc(out, func(c col) bool { return slices.Contains(c.views, viewTerm) }) {
				if side == 0 {
					out = slices.Insert(out, 0, col{views: []view{viewTerm}})
				} else {
					out = append(out, col{views: []view{viewTerm}, right: true})
				}
			}

			continue
		}

		if ok {
			continue
		}

		i := leftCount(out) - 1
		if i < 0 {
			out, i = slices.Insert(out, 0, col{}), 0
		}

		out[i].views = append(out[i].views, view(v))
	}

	return out
}

// wants are the widths columns cs ask for: their own or their side's
// default, at least 20; a folded side's first column its rail, the others on
// that side nothing.
func (m *Model) wants(cs []col, fold [2]bool) []int {
	ws := make([]int, len(cs))
	if m.w < 40 {
		return ws
	}

	var railed [2]bool

	for i, k := range cs {
		side := b2i(k.right)
		switch {
		case fold[side]:
			if !railed[side] {
				ws[i], railed[side] = railW(), true
			}

		default:
			w := k.width
			if w <= 0 {
				w = []int{m.st.Settings.Width, m.st.Settings.WidthR}[side]
			}

			if w <= 0 {
				w = 32
			}

			ws[i] = max(w, 20)
		}
	}

	return ws
}

// overBy is how many cells columns of widths ws and a mainMin-wide editor
// need beyond the screen.
func (m *Model) overBy(ws []int, mainMin int) int {
	b := m.bord()

	n := mainMin + 2*b - m.w
	for _, w := range ws {
		if w > 0 {
			n += w + 1 + b
		}
	}

	return n
}

// folds are the sides folded into a rail: hidden by hand, or squeezed. The
// side without Spaces is squeezed when, its columns and the others at 20
// cells, it would still leave the editor under minEditor: it folds before
// Spaces gives up a cell, and its rail opens it again (unfold), Spaces and
// the editor then making room.
func (m *Model) folds(cs []col) [2]bool {
	f := m.hidden

	sp := colWith(cs, viewAgents)
	if m.w < 40 || sp < 0 || f[b2i(cs[sp].right)] {
		return f
	}

	other := 1 - b2i(cs[sp].right)
	if f[other] || m.unfold[other] || !slices.ContainsFunc(cs, func(c col) bool { return b2i(c.right) == other }) {
		return f
	}

	ws := m.wants(cs, f)
	for i := range ws {
		if i != sp {
			ws[i] = min(ws[i], 20)
		}
	}

	f[other] = m.overBy(ws, minEditor) > 0

	return f
}

// leftCount is the number of left columns, which come first in cols.
func leftCount(cs []col) int {
	n := 0
	for _, c := range cs {
		n += b2i(!c.right)
	}

	return n
}

// colOf is the column holding tab v.
func (m *Model) colOf(v view) int {
	return slices.IndexFunc(m.cols(), func(c col) bool { return slices.Contains(c.views, v) })
}

// colViews are the tabs of column i.
func (m *Model) colViews(i int) []view {
	if cs := m.cols(); i >= 0 && i < len(cs) {
		return cs[i].views
	}

	return nil
}

// side is 1 for a right column, 0 otherwise.
func (m *Model) side(i int) int {
	cs := m.cols()
	return b2i(i >= 0 && i < len(cs) && cs[i].right)
}

// railViews are the tabs of every column on column i's side, listed on its rail.
func (m *Model) railViews(i int) []view {
	var out []view

	for _, c := range m.cols() {
		if b2i(c.right) == m.side(i) {
			out = append(out, c.views...)
		}
	}

	return out
}

// viewOn is the tab column i shows, its most recently shown one, the shut
// Terminal keeping its tab but showing nothing; false when there is no such
// column.
func (m *Model) viewOn(i int) (view, bool) {
	vs := m.colViews(i)
	if len(vs) == 0 {
		return 0, false
	}

	shut := func(v view) bool { return v == viewTerm && !m.termOpen() }

	best := vs[0]
	for _, v := range vs {
		if !shut(v) && (shut(best) || m.recent[v] > m.recent[best]) {
			best = v
		}
	}

	return best, true
}

// shown reports whether v is the visible tab of an open column.
func (m *Model) shown(v view) bool {
	i := m.colOf(v)
	a, ok := m.viewOn(i)

	return ok && a == v && m.colRect(i).w > 0 && !m.railed(i)
}

// railed reports a column on a hidden side; the side folds into one narrow
// rail listing its tabs.
func (m *Model) railed(i int) bool {
	cs := m.cols()
	return i >= 0 && i < len(cs) && m.folds(cs)[b2i(cs[i].right)] && m.w >= 40
}

func (m *Model) anyRailed() bool {
	cs := m.cols()
	f := m.folds(cs)

	return m.w >= 40 && slices.ContainsFunc(cs, func(c col) bool { return f[b2i(c.right)] })
}

func (m *Model) focused(v view) bool { return m.focus == m.colOf(v) && m.shown(v) }

// bord is 1 when panels are framed with borders, which cost a row above and below.
func (m *Model) bord() int { return b2i(m.st.Settings.Borders && m.termH >= 10 && m.w >= 40) }

func (m *Model) resize() { m.h = max(m.termH-2*m.bord(), 1) }

// layout places left columns | main | right columns, separated by a divider
// or by the facing borders of framed panels. A folded side collapses into
// one rail. Short of room, the other columns narrow to 20 cells, the
// outermost first, then Spaces does, then the editor gives up all but 20
// cells; only then does a column go, the outermost first and Spaces last.
func (m *Model) layout() (cs []rect, c rect) {
	if m.frame != nil {
		return m.frame.rects, m.frame.main
	}

	b := m.bord()
	gap := 1 + b
	cols := m.cols()
	ws := m.wants(cols, m.folds(cols))
	left := leftCount(cols)
	sp := colWith(cols, viewAgents)

	order := make([]int, 0, len(cols)) // right columns from the right edge, then left ones from the left edge
	for i := len(cols) - 1; i >= left; i-- {
		if i != sp {
			order = append(order, i)
		}
	}

	for i := range left {
		if i != sp {
			order = append(order, i)
		}
	}

	if sp >= 0 {
		order = append(order, sp)
	}

	for _, i := range order {
		if d := m.overBy(ws, minEditor); d > 0 && ws[i] > railW() {
			ws[i] = max(ws[i]-d, 20)
		}
	}

	for _, i := range order {
		if m.overBy(ws, 20) > 0 && ws[i] > railW() {
			ws[i] = 0
		}
	}

	cs = make([]rect, len(cols))
	for i, w := range ws {
		cs[i].w = w
	}

	x := b

	for i := range left {
		if cs[i].w > 0 {
			cs[i].x, x = x, x+cs[i].w+gap
		}
	}

	c.x = x

	xr := m.w - b
	for i := len(cols) - 1; i >= left; i-- {
		if cs[i].w > 0 {
			cs[i].x = xr - cs[i].w
			xr = cs[i].x - gap
		}
	}

	c.w = xr - c.x

	return cs, c
}

func (m *Model) colRect(i int) rect {
	if cs, _ := m.layout(); i >= 0 && i < len(cs) {
		return cs[i]
	}

	return rect{}
}

// panelH is a panel's height: everything above the status bar.
func (m *Model) panelH() int { return max(m.h-1-m.ruled(), 1) }

// ruled is 1 when a rule parts the panels from the status bar, VS Code's
// statusBar.border: an underlined row, the frame's
// bottom edge doing it when panel_borders draws one.
func (m *Model) ruled() int { return b2i(m.bord() == 0 && m.h > 2) }

// termPos is where the Terminal lives: a panel under the editor, or a sidebar.
func (m *Model) termPos() string {
	switch m.st.Settings.TermPos {
	case "left", "right":
		return m.st.Settings.TermPos
	}

	return "bottom"
}

// termOpen reports the Terminal showing in this workspace, wherever it sits:
// each workspace keeps its own, and one that has none yet takes the last set.
func (m *Model) termOpen() bool {
	if t, ok := m.st.Terminals[m.ws]; ok {
		return t.Open
	}

	return m.st.Settings.TermOpen
}

// setTermOpen opens or shuts the Terminal in this workspace alone, patch
// riding along in the same state.set; terminal_open follows, as the default
// for a workspace that has not had the panel yet.
func (m *Model) setTermOpen(open bool, patch map[string]any) tea.Cmd {
	if patch == nil {
		patch = map[string]any{}
	}

	patch["terminal_open"] = open
	t := proto.Terminal{Open: open, Tab: m.tv.id}
	m.keepTerm(t)

	return m.setSettingsWith(patch, map[string]any{"terminals": map[string]proto.Terminal{m.ws: t}})
}

// saveTerm tells the daemon the workspace's panel and the shell it shows when
// that changed since the last save, or when the workspace has none saved yet:
// left behind, it keeps the state it had rather than following the default.
func (m *Model) saveTerm() tea.Cmd {
	if m.ws == "" {
		return nil
	}

	t := proto.Terminal{Open: m.termOpen(), Tab: m.tv.id, Max: m.termMax}
	if _, ok := m.st.Terminals[m.ws]; ok && t == m.savedTerm {
		return nil
	}

	m.keepTerm(t)

	return do("state.set", map[string]any{"terminals": map[string]proto.Terminal{m.ws: t}})
}

// keepTerm records t as the workspace's panel ahead of the daemon's echo.
func (m *Model) keepTerm(t proto.Terminal) {
	if m.st.Terminals == nil {
		m.st.Terminals = map[string]proto.Terminal{}
	}

	m.st.Terminals[m.ws], m.savedTerm = t, t
}

// termRows is the height of the bottom panel, 0 when it is elsewhere or shut.
func (m *Model) termRows() int {
	if !m.termOpen() || m.termPos() != "bottom" {
		return 0
	}

	if m.termMax {
		return m.panelH()
	}

	h := m.st.Settings.TermH
	if h <= 0 {
		h = 12
	}

	return max(min(h, m.panelH()-3), m.panelTop()+2)
}

// bodyTop is the first list row of view v, below the activity bar, the view
// header (the session and the Terminal have none: their strips have the buttons) and the filter line.
func (m *Model) bodyTop(v view) int {
	return m.barH(m.colOf(v)) + headRows(v) + 3*b2i(m.filters[v].on)
}

// headRows is the rows of view header over view v's body: 1, but the session's
// and the Terminal's strips carry their buttons instead.
func headRows(v view) int { return b2i(v != viewTerm) }

// barH is column i's activity bar height; a column with a single tab has none.
func (m *Model) barH(i int) int {
	if m.sideBar() {
		return 0 // the icons run down the edge instead of across the top
	}

	return actH * b2i(len(m.colViews(i)) > 1)
}

// sideBar reports the vertical activity bar: view icons down each sidebar's
// outer edge, VS Code's own arrangement.
func (m *Model) sideBar() bool { return m.st.Settings.ActBar == "side" }

// barW is the width the vertical activity bar takes from column i.
func (m *Model) barW(i int) int {
	if !m.sideBar() || len(m.colViews(i)) < 2 || m.railed(i) || m.colRect(i).w < actW()+20 {
		return 0
	}

	return actW()
}

// bodyH is view v's list height.
func (m *Model) bodyH(v view) int { return max(m.panelH()-m.bodyTop(v), 1) }

// query is view v's active Ctrl+F filter text.
func (m *Model) query(v view) string {
	if !m.filters[v].on {
		return ""
	}

	return strings.TrimSpace(m.filters[v].input.Value())
}

// hoverRow is the list row of view v under the mouse, -1 when elsewhere. A
// vertical activity bar is not the body: the mouse over a chip lights that
// chip, never the row beside it.
func (m *Model) hoverRow(v view) int {
	if !m.shown(v) {
		return -1
	}

	i := m.colOf(v)
	rc, bw := m.colRect(i), m.barW(i)

	x0, x1 := rc.x, rc.x+rc.w
	if m.side(i) == 1 {
		x1 -= bw
	} else {
		x0 += bw
	}

	if m.mouseX < x0 || m.mouseX >= x1 {
		return -1
	}

	y := m.mouseY - m.bodyTop(v)
	if y < 0 || y >= m.bodyH(v) {
		return -1
	}

	if _, ok := m.listBar(v, y); ok && m.mouseX == x1-1 { // the scrollbar is not the row
		return -1
	}

	return y
}

// listBar is a sidebar list's scrollbar, draggable as the editor's is: the id
// its drag goes by, the bar row a view row is, its geometry as it is now, and
// how it scrolls. The rows it covers are drawn by list.renderBar.
type listBar struct {
	id  string
	row int
	geo func() vbar
	to  func(top int) tea.Cmd
}

// listBar is the scrollbar beside row y of view v's body, counted as
// hoverRow counts; false where the list has none, or not beside that row.
func (m *Model) listBar(v view, y int) (listBar, bool) {
	var b listBar

	switch v {
	case viewFiles:
		b = listBar{geo: func() vbar { return vbar{len(m.ex.nodes), m.bodyH(viewFiles), m.ex.l.top} }, to: func(top int) tea.Cmd { m.ex.l.top = top; return nil }}
	case viewDB:
		b = listBar{geo: func() vbar { return vbar{len(m.db.rows(m)), m.bodyH(viewDB), m.db.l.top} }, to: func(top int) tea.Cmd { m.db.l.top = top; return nil }}
	case viewAgents:
		return m.ag.bar(m, y)
	case viewGitHub:
		return m.gh.bar(m, y)
	case viewSearch:
		hh := m.sr.headH()
		b = listBar{row: -hh, geo: func() vbar { return vbar{len(m.sr.rows()), max(m.bodyH(viewSearch)-hh, 0), m.sr.l.top} }, to: func(top int) tea.Cmd { m.sr.l.top = top; return nil }}

	case viewGit:
		return m.scm.bar(m, y)
	default:
		return b, false
	}

	b.id, b.row = "list:"+viewKeys[v], b.row+y
	g := b.geo()

	return b, g.on() && b.row >= 0 && b.row < g.h
}

// rowColors are a list row's background and default text style.
func (m *Model) rowColors(v view, selected, hovered bool) (color.Color, lipgloss.Style) {
	base := plain
	if pal.selFg != nil {
		base = fg(pal.selFg)
	}

	switch {
	case selected && m.focused(v):
		return pal.selBg, base
	case selected:
		return pal.selUnfocusedBg, base
	case hovered:
		return pal.hoverBg, plain
	}

	return nil, plain
}

// fixFocus moves keyboard focus off a sidebar that is no longer visible.
func (m *Model) fixFocus() {
	if m.focus == onPanel && m.termRows() == 0 {
		m.focus = onMain
	}

	if m.focus >= 0 && (m.colRect(m.focus).w == 0 || m.railed(m.focus)) {
		m.focus = onMain
	}
}

// cycleFocus is ctrl+]: sidebar columns and main from left to right,
// unhiding a hidden side on the way.
func (m *Model) cycleFocus() {
	cs := m.cols()
	left := leftCount(cs)

	order := make([]int, 0, len(cs)+1)
	for i := range left {
		order = append(order, i)
	}

	order = append(order, onMain)
	if m.termRows() > 0 { // the panel under the editor is its own stop
		order = append(order, onPanel)
	}

	for i := left; i < len(cs); i++ {
		order = append(order, i)
	}

	if ps := m.grid.leaves(); m.focus == onMain && m.tiled() && m.cur != ps[len(ps)-1] { // the next group is the next stop
		_ = m.focusGroup(ps[slices.Index(ps, m.cur)+1]) // ⌃] fetches the screens
		return
	}

	i := slices.Index(order, m.focus)
	for range order {
		i = (i + 1) % len(order)

		f := order[i]
		if f == onMain && m.tiled() { // into the editor area at its first group
			_ = m.focusGroup(m.grid.leaves()[0])
			return
		}

		if f == onMain || f == onPanel {
			m.focus = f
			return
		}

		side := m.side(f)
		was, wasOpen := m.hidden[side], m.unfold[side]

		m.hidden[side], m.unfold[side] = false, true
		if m.colRect(f).w > 0 && !m.railed(f) {
			m.focus = f
			return
		}

		m.hidden[side], m.unfold[side] = was, wasOpen
	}
}

// showView selects tab v, unhides its side and focuses its column; the shut
// Terminal's tab opens it.
func (m *Model) showView(v view) tea.Cmd {
	if v == viewTerm && !m.termOpen() {
		return tea.Batch(m.setTermOpen(true, nil), m.showView(v), m.startTerm())
	}

	i := m.colOf(v)
	m.clock++
	m.recent[v] = m.clock

	if side := m.side(i); i >= 0 && m.folds(m.cols())[side] {
		m.unfold[side] = true // opened by hand: it stays open where it would be squeezed
	}

	m.hidden[m.side(i)], m.focus = false, i
	m.fixFocus()

	switch v {
	case viewGit:
		return tea.Batch(m.scm.loadDrawers(m), m.fetchScreen())
	case viewGitHub:
		return tea.Batch(m.gh.ensure(m), m.fetchScreen())
	case viewDB:
		return tea.Batch(m.db.ensure(m), m.fetchScreen())
	default:
		return m.fetchScreen()
	}
}

// colSettings is the settings form of a column layout, without empty columns.
func colSettings(cs []col) [2]proto.Columns {
	sides := [2]proto.Columns{{}, {}}

	for _, c := range cs {
		if len(c.views) == 0 {
			continue
		}

		names := make([]string, len(c.views))
		for k, v := range c.views {
			names[k] = viewKeys[v]
		}

		side := b2i(c.right)
		sides[side] = append(sides[side], proto.Column{Views: names, Width: c.width})
	}

	return sides
}

// setColWidth resizes column i locally; saveCols persists it.
func (m *Model) setColWidth(i, w int) {
	cs := m.cols()
	if i < 0 || i >= len(cs) {
		return
	}

	cs[i].width = w
	sides := colSettings(cs)
	m.st.Settings.Left, m.st.Settings.Right = sides[0], sides[1]
}

func (m *Model) saveCols() tea.Cmd {
	return m.setSettings(map[string]any{"left": m.st.Settings.Left, "right": m.st.Settings.Right})
}

// removeView takes tab v out of its column; a column left empty disappears.
func removeView(cs []col, v view) []col {
	out := cs[:0]
	for _, c := range cs {
		c.views = slices.DeleteFunc(slices.Clone(c.views), func(x view) bool { return x == v })
		if len(c.views) > 0 {
			out = append(out, c)
		}
	}

	return out
}

// dock saves a changed column layout and shows tab v in it.
func (m *Model) dock(cs []col, v view) tea.Cmd {
	sides := colSettings(cs)
	patch := map[string]any{"left": sides[0], "right": sides[1]}

	if v == viewTerm { // the panel opens where its tab went
		m.termMax = false
		patch["terminal_position"] = ifElse(cs[colWith(cs, v)].right, "right", "left")

		return tea.Batch(m.setTermOpen(true, patch), m.showView(v), m.startTerm())
	}

	return tea.Batch(m.setSettings(patch), m.showView(v), m.scm.loadDrawers(m))
}

// colWith is the index of the column holding v, -1 when no column does.
func colWith(cs []col, v view) int {
	return slices.IndexFunc(cs, func(c col) bool { return slices.Contains(c.views, v) })
}

// moveView docks tab v in the column next to main on side `to` (0 left, 1
// right).
func (m *Model) moveView(v view, to int) tea.Cmd {
	cs := removeView(m.cols(), v)
	left := leftCount(cs)

	switch {
	case to == 0 && left > 0:
		cs[left-1].views = append(cs[left-1].views, v)
	case to == 0:
		cs = slices.Insert(cs, 0, col{views: []view{v}})
	case left < len(cs):
		cs[left].views = append(cs[left].views, v)
	default:
		cs = slices.Insert(cs, left, col{views: []view{v}, right: true})
	}

	return m.dock(cs, v)
}

// splitView moves tab v into a column of its own at the outer edge of its side.
func (m *Model) splitView(v view) tea.Cmd {
	cs := m.cols()
	right := cs[m.colOf(v)].right

	cs = removeView(cs, v)
	if right {
		cs = append(cs, col{views: []view{v}, right: true})
	} else {
		cs = slices.Insert(cs, 0, col{views: []view{v}})
	}

	return m.dock(cs, v)
}

// mergeView moves tab v into a neighboring column on its side, toward main first.
func (m *Model) mergeView(v view) tea.Cmd {
	cs := m.cols()
	i := m.colOf(v)

	toward := i + 1
	if cs[i].right {
		toward = i - 1
	}

	for _, j := range []int{toward, 2*i - toward} {
		if j < 0 || j >= len(cs) || cs[j].right != cs[i].right {
			continue
		}

		anchor := cs[j].views[0]
		cs = removeView(cs, v)
		k := slices.IndexFunc(cs, func(c col) bool { return slices.Contains(c.views, anchor) })
		cs[k].views = append(cs[k].views, v)

		return m.dock(cs, v)
	}

	return nil
}

// dropAt is where tab v dropped at x, y would go: over a sidebar's activity
// bar, or the half of the column toward main, it joins that column at the gap
// between the chips under the mouse; past the half toward the screen edge it
// gets a column of its own beside it. The panel alone also goes under the
// editor (underDrop).
func (m *Model) dropAt(v view, x, y int) dropTarget {
	if t, ok := m.underDrop(x, y); ok && v == viewTerm {
		return t
	}

	cs, c := m.layout()
	left := leftCount(m.cols())

	side, i := 1, left
	if x < c.x+c.w/2 {
		side, i = 0, left-1
	}

	w := []int{m.st.Settings.Width, m.st.Settings.WidthR}[side]
	w = max(min(w, m.w/3), 20)

	edge := rect{x: m.bord(), w: w}
	if side == 1 {
		edge.x = m.w - m.bord() - w
	}

	own := dropTarget{rc: edge, side: side, col: -1, own: true, label: "New Column"}

	if j := slices.IndexFunc(cs, func(r rect) bool { return r.w > railW() && x >= r.x && x < r.x+r.w }); j >= 0 {
		i = j
	}

	if i < 0 || i >= len(cs) || cs[i].w <= railW() {
		return own
	}

	rc := cs[i]

	inner := x >= rc.x+rc.w/2
	if side == 1 {
		inner = x < rc.x+rc.w/2
	}

	if !inner && !m.onBar(i, x, y) {
		return own
	}

	return m.slotAt(v, i, x, y)
}

// underDrop is the drop under the editor at x, y: the panel's own rows while
// it is there, else the lowest quarter of main.
func (m *Model) underDrop(x, y int) (dropTarget, bool) {
	_, c := m.layout()
	h := max(m.termRows(), m.panelH()/4)

	return dropTarget{rc: c, y: m.panelH() - h, h: h, col: -1, under: true, fill: true, label: "Bottom Panel"},
		x >= c.x && x < c.x+c.w && y >= m.panelH()-h && y < m.panelH()
}

// onBar reports x, y over column i's activity bar: the top rows, or the strip
// down its outer edge.
func (m *Model) onBar(i, x, y int) bool {
	rc := m.colRect(i)
	if bw := m.barW(i); bw > 0 {
		if m.side(i) == 1 {
			return x >= rc.x+rc.w-bw
		}

		return x < rc.x+bw
	}

	return y < m.barH(i)
}

// slotAt is the place in column i's activity bar tab v takes when dropped at
// x, y: the gap between the chips on either side of the mouse across a top
// bar, the chip under the mouse down a side bar. The rectangle marks that spot.
func (m *Model) slotAt(v view, i, x, y int) dropTarget {
	rc, side := m.colRect(i), m.side(i)
	views := m.colViews(i)
	idx := slices.Index(views, v) // -1 from another column
	t := dropTarget{side: side, col: i}

	if m.sideBar() {
		k := max(min(y/vertH, len(views)-b2i(idx >= 0)), 0)
		t.at = k

		t.rc = rect{x: rc.x, w: actW()}
		if side == 1 {
			t.rc.x = rc.x + rc.w - actW()
		}

		t.y, t.h = max(min(k*vertH, m.panelH()-vertH), 0), vertH

		return t
	}

	tabs := m.tabs(i)

	g, gx, cw := len(tabs), 1, 3 // the gap before chip g, its column, and a chip's width
	if n := len(tabs); n > 0 {
		cw, gx = tabs[0].chipW, tabs[n-1].x+tabs[n-1].w
	}

	for k, c := range tabs {
		if x-rc.x < c.x+c.chipW/2 {
			g, gx = k, c.x-1
			break
		}
	}

	t.at = g - b2i(idx >= 0 && idx < g)
	t.rc = rect{x: max(min(rc.x+gx-cw/2, rc.x+rc.w-cw), rc.x), w: cw}
	t.h = actH

	return t
}

// dragTabTo follows a dragged tab and docks it where it is let go. A click
// that never moved is just a click.
func (m *Model) dragTabTo(d *drag, x, y int, release bool) tea.Cmd {
	dy := 2 - b2i(d.fine)
	if max(x-d.x0, d.x0-x) >= 2 || max(y-d.y0, d.y0-y) >= dy {
		d.moved = true
	}

	if d.moved {
		t := m.dropAt(d.v, x, y)
		d.drop = &t
	}

	if !release {
		return nil
	}

	m.drag = nil

	switch {
	case !d.moved && d.hide: // VS Code's activity bar: a second click on the open view hides its side
		return m.hide(m.colOf(d.v))
	case !d.moved, d.drop.under && m.termRows() > 0:
		return nil
	case d.drop.under:
		return m.moveTerminal("bottom")
	case d.drop.own:
		return m.splitTo(d.v, d.drop.side)
	case d.drop.col >= 0:
		return m.placeView(d.v, d.drop.col, d.drop.at)
	}

	return m.moveView(d.v, d.drop.side)
}

// placeView makes v the at-th tab of column i, reordering its own column or
// joining another; a layout that does not change is not saved.
func (m *Model) placeView(v view, i, at int) tea.Cmd {
	cs := m.cols()
	if i < 0 || i >= len(cs) {
		return nil
	}

	was := cs[i].views
	for j := range cs {
		cs[j].views = slices.DeleteFunc(slices.Clone(cs[j].views), func(x view) bool { return x == v })
	}

	cs[i].views = slices.Insert(cs[i].views, max(min(at, len(cs[i].views)), 0), v)
	if slices.Equal(cs[i].views, was) {
		return m.showView(v)
	}

	cs = slices.DeleteFunc(cs, func(c col) bool { return len(c.views) == 0 })

	return m.dock(cs, v)
}

// splitTo gives v a column of its own at the outer edge of side `to`.
func (m *Model) splitTo(v view, to int) tea.Cmd {
	cs := removeView(m.cols(), v)
	if to == 0 {
		cs = slices.Insert(cs, 0, col{views: []view{v}})
	} else {
		cs = append(cs, col{views: []view{v}, right: true})
	}

	return m.dock(cs, v)
}

// ifElse is a when c holds, else b.
func ifElse[T any](c bool, a, b T) T {
	if c {
		return a
	}

	return b
}

// hideSession takes the session off the screen; it runs on in the background
// and comes back from Agents or its tab.
func (m *Model) hideSession() tea.Cmd {
	if m.sessTabs != nil { // its tab goes with it
		m.sessTabs[m.ws] = slices.DeleteFunc(slices.Clone(m.sessTabs[m.ws]), func(id string) bool { return id == m.sess })
	}

	m.sess, m.term = "", term{}
	m.fixFocus()

	return m.fetchScreen()
}

// sessionTitleMenu is the session title's right click menu.
func (m *Model) sessionTitleMenu() tea.Cmd {
	id := m.sess
	if id == "" {
		return nil
	}

	end := []item{{label: "Close (keeps running)", run: func(m *Model) tea.Cmd { return m.hideSession() }}}
	if s := m.session(id); s != nil && canSleep(*s) {
		end = append(end, item{label: "Sleep Session", run: func(m *Model) tea.Cmd { return m.sleepSession(id) }})
	}

	return m.menuOf(menuGroups(
		[]item{
			{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
			{label: m.pinLabel(id), run: func(m *Model) tea.Cmd { return m.togglePin(id) }},
		},
		append(end,
			item{label: "Kill Session…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }},
		)), m.mouseX, m.mouseY)
}

// menuOf opens items as a context menu at x, y.
func (m *Model) menuOf(items []item, x, y int) tea.Cmd {
	m.modal = newMenu("", x, y, items...)
	return nil
}

func (m *Model) tabMenu(v view, x, y int) tea.Cmd {
	cs := m.cols()
	i := m.colOf(v)

	other, label := 1, "Move to Right Sidebar"
	if cs[i].right {
		other, label = 0, "Move to Left Sidebar"
	}

	items := []item{{label: label, run: func(m *Model) tea.Cmd { return m.moveView(v, other) }}}
	if v == viewTerm { // its own say where it goes
		items = m.terminalItems()
	}

	if len(cs[i].views) > 1 {
		items = append(items, item{label: "Move to Own Column", run: func(m *Model) tea.Cmd { return m.splitView(v) }})
	}

	if slices.ContainsFunc(cs, func(c col) bool { return c.right == cs[i].right && !slices.Contains(c.views, v) }) {
		items = append(items, item{label: "Merge into Next Column", run: func(m *Model) tea.Cmd { return m.mergeView(v) }})
	}

	m.modal = newMenu(viewTitles[v], x, y, menuGroups(items, []item{{label: "Hide Sidebar", hint: "b", run: func(m *Model) tea.Cmd { return m.hide(i) }}})...)

	return nil
}

// terminalItems are what the panel offers wherever it is: where it goes, and
// its shells.
func (m *Model) terminalItems() []item {
	var items []item

	for _, pos := range []string{"bottom", "left", "right"} {
		if pos == m.termPos() && m.termOpen() {
			continue
		}

		items = append(items, item{
			label: "Move Terminal to " + strings.ToUpper(pos[:1]) + pos[1:],
			run:   func(m *Model) tea.Cmd { return m.moveTerminal(pos) },
		})
	}

	if m.termOpen() && m.termPos() == "bottom" {
		items = append(items, item{
			label: "Toggle Terminal Maximized", hint: "^⇧↑",
			run: func(m *Model) tea.Cmd { return m.maximizeTerminal(!m.termMax) },
		})
	}

	items = append(items, item{label: "Focus Terminal", run: func(m *Model) tea.Cmd { return m.focusTerminal() }},
		item{label: "New Terminal", hint: "^⇧`", run: func(m *Model) tea.Cmd { return tea.Batch(m.openTerminalPanel(), m.newTerm()) }})
	if m.session(m.tv.id) != nil {
		items = append(items, item{label: "Kill Terminal…", run: func(m *Model) tea.Cmd { return m.confirmKill(m.tv.id) }})
	}

	if len(m.termSessions()) > 1 {
		items = append(items, item{label: "Next Terminal", run: func(m *Model) tea.Cmd { return m.cycleTerm(1) }},
			item{label: "Previous Terminal", run: func(m *Model) tea.Cmd { return m.cycleTerm(-1) }})
	}

	return append(items, item{label: "Close Terminal", hint: "^`", run: func(m *Model) tea.Cmd {
		if !m.termOpen() {
			return nil
		}

		return m.toggleTerminal()
	}})
}

// dirtyEditors counts the open editors with unsaved text, in every group.
func (m *Model) dirtyEditors() (n int) { // a table's edits and a cell's value are tableEdits
	m.eachGroup(func() {
		n += b2i(m.pv.dirty() && m.pv.tbl == nil && m.pv.cell == nil)
		for _, e := range m.editors {
			if e.dirty() && e.tbl == nil && e.cell == nil && e.id() != m.pv.id() {
				n++
			}
		}
	})

	return n
}

// saveAll writes every editor that has unsaved text, the open one included.
// Untitled buffers are skipped — a bulk save cannot stop to ask where each
// one goes — and stay safe as drafts.
func (m *Model) saveAll() {
	m.eachGroup(func() {
		if m.pv.dirty() && !m.pv.untitled() && m.pv.buf != nil {
			_ = m.pv.buf.save(m.pv.path, false) // best effort on the way out
		}

		for _, e := range m.editors {
			if e.dirty() && !e.untitled() && e.buf != nil { // a table's edits are submitted, not saved
				_ = e.buf.save(e.path, false) // best effort on the way out
			}
		}
	})
}

// toggleSidebars folds both sides away, or brings them back.
func (m *Model) toggleSidebars() tea.Cmd {
	both := !m.hidden[0] && !m.hidden[1]
	m.hidden, m.unfold = [2]bool{both, both}, [2]bool{!both, !both}
	m.fixFocus()

	return m.fetchScreen()
}

// focusSidebar puts the focus on the first sidebar that is open.
func (m *Model) focusSidebar() tea.Cmd {
	for i := range m.cols() {
		if m.colRect(i).w > 0 && !m.railed(i) {
			m.focus = i
			return m.fetchScreen()
		}
	}

	return nil
}

// openTerminalPanel shows the terminal wherever it is docked, without closing
// it when it is already there.
func (m *Model) openTerminalPanel() tea.Cmd {
	if m.termOpen() {
		return nil
	}

	return m.toggleTerminal()
}

// toggleRendered is ⌃⇧v: Markdown as source or rendered.
func (m *Model) toggleRendered() tea.Cmd {
	if !m.showsPreview() || m.pv.kind != pvFile || !isMarkdown(m.pv.path) {
		return flash("open a Markdown file first", true)
	}

	return m.pv.toggleMarkdown(m, 1)
}

// hide folds column i's side into a rail.
func (m *Model) hide(i int) tea.Cmd {
	m.hidden[m.side(i)], m.unfold[m.side(i)] = true, false
	m.fixFocus()

	return m.fetchScreen()
}

// tab is one view chip of a sidebar's activity bar.
type tab struct {
	v            view
	x, w, chipW  int
	label, badge string
}

// barPad is the air on each side of an activity bar icon, the gear's too, so
// the icon is centered in a chip an odd number of cells wide; a word label,
// without icons, is wide enough with less.
func barPad() string {
	if iconsNerd || iconsEmoji {
		return "  "
	}

	return " "
}

// gearLabel is the settings gear at the activity bar's right end.
func gearLabel() string { return barPad() + icGear.s() + barPad() }

func (m *Model) tabs(s int) []tab {
	build := func(short bool, pad string, gap int) []tab {
		var out []tab

		x := 1

		for _, v := range m.colViews(s) {
			label := viewIcon(v).s()
			if short {
				label = viewIcon(v).short()
			}

			t := tab{v: v, x: x, label: pad + label + pad}
			if c := m.attentionCount(); v == viewAgents && c > 0 {
				t.badge = fmt.Sprintf("•%d", c)
			}

			t.chipW = ansi.StringWidth(t.label)
			t.w = t.chipW + ansi.StringWidth(t.badge)
			x += t.w + gap
			out = append(out, t)
		}

		return out
	}
	// Chips tighten, one cell of air, then none between them, then none
	// around the icon, and text labels shrink to their initials, rather than
	// run into the gear: every view keeps its chip down to the narrowest
	// sidebar.
	var out []tab

	for i, l := range []struct {
		pad string
		gap int
	}{{barPad(), 1}, {" ", 1}, {" ", 0}, {"", 1}} {
		out = build(i > 0, l.pad, l.gap)
		if n := len(out); n == 0 || out[n-1].x+out[n-1].w <= m.colRect(s).w-ansi.StringWidth(gearLabel()) {
			break
		}
	}

	return out
}

// titleAction is a hover button at the right end of a view header; its tip
// names it and its key.
type titleAction struct {
	label, tip string
	x, w       int
	hot        bool // under the mouse
	run        func(m *Model) tea.Cmd
}

// ponytail: terminals send no "mouse left" event, so header actions show for
// a few seconds after mouse activity over the sidebar, like herdr-sidebar.
const actionsLinger = 3 * time.Second

// headerActions lays the buttons out over the view's body width — the column
// minus a vertical activity bar — and hit-tests the mouse in the same space.
func (m *Model) headerActions(s int, v view, w int) []titleAction {
	rc := m.colRect(s)
	if time.Since(m.mouseAt) > actionsLinger || m.mouseX < rc.x || m.mouseX >= rc.x+rc.w {
		return nil
	}

	bw := m.barW(s)
	w = min(w, rc.w-bw)

	mx := m.mouseX - rc.x
	if m.side(s) == 0 {
		mx -= bw // a left column's bar sits before the body
	}

	var acts []titleAction

	add := func(g glyph, tip string, run func(*Model) tea.Cmd) {
		acts = append(acts, titleAction{label: " " + g.s() + " ", tip: tip, run: run})
	}

	switch v {
	case viewFiles:
		add(icNewFile, "New File…  n", func(m *Model) tea.Cmd { return m.ex.action("n")(m) })
		add(icNewDir, "New Folder…  N", func(m *Model) tea.Cmd { return m.ex.action("N")(m) })
		add(icRefresh, "Refresh  r", func(m *Model) tea.Cmd { m.ex.rebuild(m); return m.refreshGit() })
		add(icCollapse, "Collapse All  C", func(m *Model) tea.Cmd { m.ex.collapseAll(m); return nil })

	case viewGit:
		tree := m.st.Settings.GitTree

		add(icBranch, "Switch Branch…  B", m.scm.branchPicker)
		add(icCheck, "Commit  C", m.scm.commit)
		add(ifElse(tree, icList, icTree), ifElse(tree, "View as List  t", "View as Tree  t"), m.scm.toggleTree)
		add(icRefresh, "Refresh  r", func(m *Model) tea.Cmd { return tea.Batch(m.refreshGit(), m.scm.loadDrawers(m)) })
		add(icCollapse, "Collapse All", m.scm.collapseAll)

	case viewSearch:
		add(icReplace, "Toggle Replace  ^h", func(*Model) tea.Cmd { return m.sr.toggleReplace() })
		add(icRefresh, "Rerun Search  ^r", m.sr.restart)
		add(icClearAll, "Clear Results  x", func(m *Model) tea.Cmd { m.sr.query.Reset(); m.sr.clear(); return nil })
		add(icCollapse, "Collapse All  C", func(m *Model) tea.Cmd { m.sr.collapseAll(); return nil })

	case viewTerm: // its buttons are in its tab strip

	case viewGitHub:
		add(icPRNew, "Create Pull Request", func(m *Model) tea.Cmd { return m.ghTerminal("gh pr create") })
		add(icRefresh, "Refresh  ^r", m.gh.refresh)
		add(icCollapse, "Collapse All  C", func(m *Model) tea.Cmd { m.gh.collapseAll(); return nil })

	case viewDB:
		add(icAdd, "New Connection…  n", m.db.newConn)
		add(icRefresh, "Refresh  r", m.db.refresh)
		add(icCollapse, "Collapse All  C", func(m *Model) tea.Cmd { m.db.collapseAll(); return nil })

	case viewAgents:
		add(icAdd, "New Session…  n", func(m *Model) tea.Cmd { return m.ag.newSession(m, m.ag.selected(m)) })
		add(icResume, "Resume Session…", func(m *Model) tea.Cmd { return m.ag.resume(m, m.ag.selected(m)) })
		add(icWorktree, "New Worktree…  w", func(m *Model) tea.Cmd { return m.ag.newWorktree(m, m.ag.selected(m)) })
		add(icFind, "Search  ^f", func(m *Model) tea.Cmd { return m.startFilter(viewAgents) })
		add(icRefresh, "Refresh  r", func(*Model) tea.Cmd { return tea.Batch(loadState(), loadWorkspaces(), loadSessions()) })
		add(icOptions, "View Options…  o", func(m *Model) tea.Cmd { return m.ag.viewMenu(m, m.mouseX, m.mouseY+1) })
	}

	if m.barH(s) == 0 && bw == 0 { // no activity bar to hold the gear
		add(icGear, settingsTip, func(m *Model) tea.Cmd { return m.manageMenu(m.mouseX, m.mouseY+1) })
	}

	x := w
	for i := len(acts) - 1; i >= 0; i-- {
		acts[i].w = ansi.StringWidth(acts[i].label)
		x -= acts[i].w
		acts[i].x = x
		acts[i].hot = m.mouseY == m.barH(s) && mx >= x && mx < x+acts[i].w
	}

	return acts
}

// settingsTip is the tip of the gear that opens the Manage menu.
const settingsTip = "Manage  F10"

// viewTip names view v and its key, for its activity bar chip.
func viewTip(v view) string { return strings.TrimSpace(viewTitles[v] + "  " + showKeys[v]) }

// tipWakeMsg wakes View once the pointer has rested tipDelay, for the tip of
// the button under it, and again at actionsLinger, when the tip goes with
// the header actions; due is when it was sent for.
type tipWakeMsg struct{ due time.Time }

// motionGap is the least time between the frames mouse motion draws. Bubble
// Tea draws after every message but puts at most 60 frames a second on the
// terminal, and a pointer dragged fast sends a motion per cell crossed, so
// drawing each one queues them up and the drag trails the pointer. Tests set
// it to 0: every motion draws.
var motionGap = time.Second / 60

// redrawMsg draws the frame motion skipped, the pointer where it stopped.
type redrawMsg struct{}

// tipShown reports whether the pointer has rested long enough for a tip, or
// just moved on from one, as VS Code's toolbars show the next at once; and
// not so long that it should be gone, nor since a key or a click: a tip
// never sits over what is typed.
func (m *Model) tipShown() bool {
	d := time.Since(m.mouseAt)

	return m.mouseAt.After(m.tipOff) && d < actionsLinger && (d >= tipDelay || time.Since(m.tipSeen) < tipDelay)
}

// tipLater wakes View at the next of those moments, unless a wake on its way
// comes first: one moving on to another button comes sooner than the one
// taking the last tip down.
func (m *Model) tipLater() tea.Cmd {
	var due time.Time

	switch d := time.Since(m.mouseAt); {
	case !m.st.Settings.Tips, !m.mouseAt.After(m.tipOff): // off, or put away until the pointer moves
		return nil
	case d < tipDelay:
		due = m.mouseAt.Add(tipDelay)
	case d < actionsLinger:
		due = m.mouseAt.Add(actionsLinger)
	default:
		return nil
	}

	if !m.tipDue.IsZero() && !due.Before(m.tipDue) {
		return nil
	}

	m.tipDue = due

	return tea.Tick(time.Until(due), func(time.Time) tea.Msg { return tipWakeMsg{due} })
}

// switchWorkspace opens path: the sidebar views, the session and the
// editors it had open follow. The command saves the editors of the workspace
// left behind and loads the one that was active in the new one.
func (m *Model) switchWorkspace(path string) tea.Cmd {
	unfolded := m.ag.reveal(m, path)

	if path == m.ws {
		return unfolded
	}

	saved := tea.Batch(unfolded, m.saveEditors(), m.saveTerm(), m.saveDrafts()) // the panel's too
	m.savedDrafts = nil
	open := m.termOpen()

	if m.waiting == nil {
		m.waiting = map[string][]preview{}
	}

	var left []preview // every group's, until it shows again

	m.eachGroup(func() {
		left = append(left, slices.DeleteFunc(slices.Clone(m.editors), func(e preview) bool { return !unsent(e) })...)
	})
	m.waiting[m.ws] = left
	m.scm.cmps[m.ws] = m.scm.cmp
	m.ws = path
	m.ex.setRoot(m, path)
	m.scm.reset()
	m.scm.cmp, m.scm.cmpSel = m.scm.cmps[path], "" // each project compares on its own
	m.sr.clear()
	m.probs, m.refs = nil, nil
	m.gh.sync(m) // another project's repository: its own lists
	hub := m.gh.shown(m)
	m.lsps.reset(path) // the servers belong to the workspace they indexed
	m.dropDiags()
	m.editors, m.edIdx, m.nav, m.navAt = nil, -1, nil, -1 // editors of another worktree
	m.pv.close(m)                                         // a preview from another worktree would be misleading

	if s := m.session(m.sess); s == nil || s.Workspace != path {
		m.sess = ""
		for _, s := range m.mainSessions() {
			if s.Workspace == path {
				m.sess = s.ID
				break
			}
		}

		m.term = term{}
	}

	restored := m.restoreEditors()
	if back := m.waiting[path]; len(back) > 0 { // the tables and values it left unsent, after its files
		delete(m.waiting, path)
		m.editors = append(m.editors, back...)

		if m.edIdx < 0 {
			restored = m.restore(back[0])
		}
	}
	// A session over the editor is what shows, the tabs waiting in the strip.
	m.preview = m.preview && m.sess == ""
	// The panel as this workspace left it: open or shut, maximized or not,
	// on the shell it showed.
	m.savedTerm, m.tv.id, m.tv.term = m.st.Terminals[path], "", term{}
	m.termMax = m.savedTerm.Max && m.termOpen()

	if open != m.termOpen() && m.w > 0 { // New switches before the window has a size
		m.resize()
		m.fixFocus()
	}

	return tea.Batch(saved, restored, m.loadDrafts(), m.ensureTerm(), hub, m.db.shown(m))
}

// nextTab is the session a closed tab hands its focus to: the tab left of it,
// else the one right of it, in the strip it was in; "" when it was the last
// one, which closes the strip with it. left is the session list that remains.
func nextTab(tabs []proto.Session, id string, left []proto.Session) string {
	alive := func(s proto.Session) bool {
		return slices.ContainsFunc(left, func(o proto.Session) bool { return o.ID == s.ID })
	}

	i := slices.IndexFunc(tabs, func(s proto.Session) bool { return s.ID == id })
	if i < 0 {
		return ""
	}

	for j := i - 1; j >= 0; j-- {
		if alive(tabs[j]) {
			return tabs[j].ID
		}
	}

	for j := i + 1; j < len(tabs); j++ {
		if alive(tabs[j]) {
			return tabs[j].ID
		}
	}

	return ""
}

func (m *Model) switchSession(id string) tea.Cmd {
	s := m.session(id)
	if s == nil {
		return nil
	}

	m.acknowledge(id) // clicked: its state is seen

	if m.showsSession() { // shown without a tab, as a worktree or a restart brings one back: it keeps one
		m.openSessTab(m.sess)
	}

	switched := m.switchWorkspace(s.Workspace)
	if p := m.holder(id); p != nil { // its tab is in another group: that one shows it
		switched = tea.Batch(switched, m.focusGroup(p))
	}

	for _, p := range m.grid.leaves() {
		if p != m.cur && p.g.sess == id { // behind another group's files, tabless: it comes here
			p.g.sess, p.g.term = "", term{}
		}
	}

	if m.sess != id { // the workspace's groups came back with another in front
		m.term = term{}
	}

	m.sess, m.preview = id, false
	m.openSessTab(id)

	return tea.Batch(switched, m.ensureTerm(), m.fetchScreen(), m.refreshGit(), m.wake(id))
}

func (m *Model) session(id string) *proto.Session {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			return &m.sessions[i]
		}
	}

	return nil
}

func (m *Model) workspace(path string) *proto.Workspace {
	for i := range m.wss {
		if m.wss[i].Path == path {
			return &m.wss[i]
		}
	}

	return nil
}

// showsPreview reports whether the main area draws the preview.
func (m *Model) showsPreview() bool {
	return m.pv.kind != "" && (m.sess == "" || m.preview)
}

// showsSession reports a session filling the main area.
func (m *Model) showsSession() bool { return m.sess != "" && !m.preview }

// Update owns every message: keys and mouse, the daemon's events, and what
// the commands it started send back.
// Update handles msg, then starts a text effect on whatever it changed.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	cmd = tea.Batch(cmd, m.prune())

	if m.pv.transient && m.pv.tbl != nil && len(m.pv.tbl.edits) > 0 { // an edited table keeps its tab, as an edited file does
		m.pin()
	}

	m.noteFx()
	m.noteDiags()

	m.still = stillEvent(msg)

	_, motion := msg.(tea.MouseMotionMsg)
	_, redraw := msg.(redrawMsg)

	keep := motion || redraw // what lies under a drag changes with anything but its motion
	if m.base != nil && m.base.key.md != nil {
		_, keep = msg.(tea.KeyPressMsg) // under a modal, with anything but a key it takes
	}

	if !keep {
		m.base = nil
	}

	return m, tea.Batch(cmd, m.noteEdits(), m.noteGutter(), m.noteBlame(), m.animate(), m.spinLater(), m.motionFrame(msg))
}

// motionFrame keeps mouse motion to a frame per motionGap: a motion sooner
// than that after the last frame gets it again, and the frame after it
// comes once the gap is over.
func (m *Model) motionFrame(msg tea.Msg) tea.Cmd {
	wait := motionGap - time.Since(m.drawnAt)
	if _, ok := msg.(tea.MouseMotionMsg); !ok || m.drawn == nil || wait <= 0 {
		return nil
	}

	m.still = true
	if m.redrawDue {
		return nil
	}

	m.redrawDue = true

	return tea.Tick(wait, func(time.Time) tea.Msg { return redrawMsg{} })
}

// stillEvent reports a daemon event whose handling only asks for what
// changed: the screen stays as it was until the answer arrives, so View
// gives the last frame again. Bubble Tea draws after every message, and a
// busy session sends one of these before each of its screens.
func stillEvent(msg tea.Msg) bool {
	e, ok := msg.(eventMsg)

	return ok && (e.Kind == "screen" || e.Kind == "state" || e.Kind == "sessions" || e.Kind == "workspaces")
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.termH = msg.Width, msg.Height
		m.unfold = [2]bool{} // a new screen folds what does not fit again
		m.resize()
		m.fixFocus()

		return m, m.fetchScreen()

	case tea.KeyboardEnhancementsMsg:
		m.ambiguous = !msg.SupportsKeyDisambiguation()
		return m, nil

	case tea.ModeReportMsg: // what Bubble Tea switches its width method on
		if msg.Mode == ansi.ModeUnicodeCore && (msg.Value == ansi.ModeSet || msg.Value == ansi.ModeReset || msg.Value == ansi.ModePermanentlySet) {
			m.graphemes = true
		}

		return m, nil

	case tea.BackgroundColorMsg:
		m.dark, m.bg = msg.IsDark(), hexColor(msg.Color)
		m.look()
		m.scm.styleInput(m.dark)
		m.pv.raw = ""

		return m, m.pv.load(m)

	case tea.ForegroundColorMsg:
		m.fg = hexColor(msg.Color)
		return m, nil

	case blinkMsg:
		if !m.wantsBlink() {
			m.blinking, m.blinkOn = false, false
			return m, nil
		}

		m.blinkOn = !m.blinkOn

		return m, blinkTick()

	case fxTickMsg:
		return m, m.onFxTick()

	case spinMsg: // View turns the spinners; Update's spinLater sends the next
		m.spinWake = false
		return m, nil

	case redrawMsg:
		m.redrawDue = false
		return m, nil

	case tickMsg:
		m.ex.rebuild(m)
		m.scm.fit(m) // the box learns its width when drawn: a resize rewraps it
		cmds := []tea.Cmd{tick(), m.blink(), m.tickGit(), m.scm.saveDraft(m), m.scm.loadDrawers(m), m.gh.shown(m), m.db.shown(m), m.saveWorkspace(), m.saveEditors(), m.saveTerm(), m.saveDrafts()}
		m.eachGroup(func() { cmds = append(cmds, m.pv.reloadIfLive(m)) }) // what every group shows
		// ponytail: other projects' branches come back every 30 s, one git call
		// per project; watch their HEADs if that lags.
		m.ticks++
		if m.ticks%15 == 0 {
			cmds = append(cmds, loadWorkspaces())
		}
		// A session on screen is in use though it prints nothing: asked for
		// now and then, the daemon sees it viewed and leaves it awake.
		if m.ticks%15 == 0 && m.st.Settings.Sleep > 0 {
			cmds = append(cmds, m.fetchScreen())
		}

		if m.filters[viewFiles].on && time.Since(m.index.at) > 10*time.Second {
			cmds = append(cmds, m.loadIndex(false))
		}

		return m, tea.Batch(cmds...)

	case eventMsg:
		return m, tea.Batch(waitEvent(m.events), m.onEvent(proto.Event(msg)))
	case disconnectedMsg:
		m.events = nil
		m.flash("daemon disconnected, reconnecting…", true)

		return m, reconnect()

	case reconnectMsg:
		if msg.err != nil {
			return m, reconnect()
		}

		m.events = msg.ch
		m.flash("reconnected", false)

		return m, tea.Batch(waitEvent(m.events), loadState(), loadWorkspaces(), loadSessions(), m.fetchScreen())

	case draftsMsg:
		return m, m.onDrafts(msg)
	case stateMsg:
		full, sel := m.st.Settings.DiffFull, m.ag.selected(m)
		m.st = proto.State(msg)
		m.ag.follow(m, sel) // another TUI's pin adds rows above it
		m.look()
		m.resize()
		m.syncWrap()
		m.limitEditors() // a lowered editor_limit applies at once, as in VS Code
		m.ex.rebuild(m)
		m.scm.build(m)
		m.fixFocus()

		var reload tea.Cmd

		if m.st.Settings.DiffFull != full && m.pv.fileDiff() { // diff_full set elsewhere
			m.pv.keepPlace(m)
			reload = m.pv.refull(m)
		}

		return m, tea.Batch(m.syncTabs(), reload)

	case updateMsg:
		return m, m.onUpdate(proto.Update(msg))
	case fontMsg, fontDoneMsg:
		return m, m.onFont(msg)
	case workspacesMsg:
		m.wss = msg

		return m, nil

	case sessionsMsg:
		var next string

		alive := func(id string) bool {
			return slices.ContainsFunc(msg, func(s proto.Session) bool { return s.ID == id })
		}

		for _, p := range m.grid.leaves() {
			if p != m.cur {
				p.g.forget(alive)
			}
		}

		if prev := m.session(m.sess); prev != nil && !alive(prev.ID) {
			m.flash(sessionName(*prev)+" session closed", false)
			// Before the list is replaced: a closed session hands over to one
			// of the space.
			next = nextTab(m.spaceSessions(), m.sess, msg)

			m.sess, m.term = "", term{}
		}

		termNext := ""
		if m.tv.id != "" {
			termNext = nextTab(m.termSessions(), m.tv.id, msg)
		}

		cmd := m.sound(m.soundFor(msg))
		sel := m.ag.selected(m)

		m.sessions = msg

		m.ag.follow(m, sel)

		if m.showsSession() { // what the focused group shows is seen as it changes; another group's pulses on its frame
			m.acknowledge(m.sess)
		}

		cmd = tea.Batch(cmd, m.blink())
		if next != "" && m.holder(next) == nil { // a tab closed with others left: the neighbour takes over
			cmd = tea.Batch(cmd, m.switchSession(next))
		}

		if m.tv.id != "" && m.session(m.tv.id) == nil { // the panel's shell is gone
			m.tv.id, m.tv.term = "", term{}
			if termNext != "" {
				m.tv.id = termNext
			} else if m.termOpen() && m.ptab == ptTerm { // the last shell closed: so does the panel showing it
				return m, tea.Batch(cmd, m.toggleTerminal())
			}
		}

		return m, cmd

	case screenMsg:
		return m, m.onScreen(msg)
	case wokeMsg:
		delete(m.waking, msg.id)

		if msg.err != nil {
			return m, flash("session.wake: "+msg.err.Error(), true)
		}

		return m, tea.Batch(loadSessions(), m.fetchScreen())

	case termTextMsg:
		return m, m.onTermText(msg)
	case gitMsg:
		m.gitBusy, m.gitNext = false, time.Now().Add(10*msg.took)
		if msg.ws != m.ws {
			return m, m.refreshGit()
		}

		m.ex.deco = msg.deco
		m.scm.onGit(m, msg)

		var cmd tea.Cmd

		if w := m.workspace(m.ws); w != nil {
			// A checkout in a shell leaves the worktree list behind: the tree,
			// the tabs and the session header would name the old branch.
			if st, ok := msg.status[m.ws]; ok && st.Branch != w.Branch && st.Branch != m.branchSeen {
				m.branchSeen = st.Branch
				cmd = tea.Batch(cmd, loadWorkspaces())
			}
		}

		return m, tea.Batch(cmd, m.syncDiags())

	case diagMsg:
		return m, m.onDiags(msg)
	case diagTickMsg:
		return m, m.onDiagTick(int(msg))
	case diagFailMsg:
		m.warnDiag(msg)
		return m, nil

	case diagOKMsg:
		delete(m.diagFailed, string(msg))
		return m, nil

	case diagSyncedMsg:
		return m, m.onDiagSynced(msg)

	case changeStagedMsg:
		return m, m.onChangeStaged(msg)

	case gutterMsg:
		if p := &m.pv; p.path == msg.path && p.chgKey == msg.key {
			p.changes, p.chg = msg.changes, msg.chg

			m.followQuickDiff()
		}

		return m, nil

	case problemsMsg:
		if msg.live {
			if m.problemsOpen() {
				msg.pk.follow(m.probs)
				m.probs = msg.pk
			}

			return m, nil
		}

		return m, m.showPeek(msg.pk)

	case searchTickMsg:
		return m, m.sr.onTick(m, int(msg))
	case searchMsg:
		return m, m.sr.onResult(msg)

	case ghMsg:
		m.gh.onMsg(m, msg)
		return m, nil

	case dbFilesMsg:
		m.db.onFiles(m, msg)
		return m, nil

	case dbObjsMsg:
		return m, m.db.onObjs(m, msg)

	case tableMsg:
		return m, m.onTableLoad(msg)
	case connTestMsg:
		msg.f.onTest(msg)
		return m, nil

	case tableDoneMsg:
		return m, m.onTableDone(msg)

	case dbPasswordMsg:
		m.db.sync(m)

		if msg.pw != "" {
			m.db.passwords[msg.id] = msg.pw
		}

		cmd := m.db.read(m, msg.id)
		if msg.err != nil {
			cmd = tea.Batch(cmd, flash("keyring: "+errLine(msg.err.Error()), true))
		}

		return m, cmd

	case consoleMsg:
		return m, tea.Batch(m.showTerminal(), m.spawn(m.ws, termAgent, msg.argv, "", nil))

	case ghDoneMsg:
		return m, m.gh.onDone(m, msg)
	case ghUserMsg:
		m.ghUser = string(msg)
		m.pv.mdLines, m.pv.mdShown = nil, 0 // a page open is laid out again, with the comments that tag them marked

		return m, nil

	case ghSentMsg:
		return m, m.onGHSent(msg)
	case ghShownMsg:
		if msg.err != nil {
			return m, flash("commit: "+ghErrLine(msg.err), true)
		}

		return m, m.openShow(msg.root, msg.oid)

	case ghCheckoutMsg:
		return m, m.onGHCheckout(msg)

	case refsMsg:
		return m, m.scm.onRefs(m, msg)
	case remotesMsg:
		return m, m.scm.onRemotes(m, msg)
	case lspMsg:
		return m, m.onLSP(msg)
	case symbolsMsg:
		return m, m.openSymbols(msg)
	case compTickMsg:
		return m, m.onCompTick(msg)
	case compMsg:
		m.pv.onComp(msg)
		return m, nil

	case blameTickMsg:
		return m, m.onBlameTick(msg.seq)
	case blameMsg:
		m.onBlame(msg)
		return m, nil

	case hoverTickMsg:
		return m, m.askHover(msg.seq)
	case hoverMsg:
		return m, m.onHover(msg)
	case sigMsg:
		m.pv.onSig(msg)
		return m, nil

	case actionsMsg:
		return m, m.onActions(msg)
	case appliedMsg:
		return m, m.onApplied(msg)
	case scmMsg, drawerMsg, modalMsg, stageMsg, pastMsg, tipMsg, cmpMsg:
		return m, m.scm.onMsg(m, msg)
	case previewMsg:
		m.eachGroup(func() { // the group showing it, which need not have the keyboard
			if m.pv.id() == msg.key {
				m.pv.onLoad(m, msg)
			}
		})

		return m, m.syncTabs() // a file opened for the first time tells its indentation now

	case indexMsg:
		if msg.ws == m.ws {
			m.index.ws, m.index.files, m.index.at = msg.ws, msg.files, time.Now()
			m.ex.rebuild(m)
		}

		switch {
		case msg.query != "":
			return m, m.openQuery(msg)
		case msg.open:
			m.quickOpen(msg)
		}

		return m, nil

	case newSessionMsg:
		return m, m.onNewSession(proto.Session(msg))
	case newWorkspaceMsg:
		return m, m.onNewWorkspace(proto.Workspace(msg))
	case sessionWorktreeMsg: // switched there first, then the harness of its session
		return m, tea.Batch(m.onNewWorkspace(proto.Workspace(msg)), m.harnessPicker(msg.Path))
	case focusSessionMsg:
		m.focus = onMain
		return m, m.switchSession(string(msg))

	case flashMsg:
		m.flash(msg.text, msg.err)
		return m, nil

	case logsMsg:
		m.out.add(msg...)
		return m, m.logIn.waitLogs()

	case fileOpMsg:
		if msg.moved != "" && m.ex.clip == msg.moved { // a moved entry is pasted once, as in VS Code
			m.ex.clip, m.ex.cut = "", false
		}

		m.ex.reveal(m, msg.path)

		return m, tea.Batch(flash(msg.text, false), m.refreshGit())

	case tea.KeyPressMsg:
		m.tipOff = time.Now()

		k, ok := m.unspill(msg)
		if !ok {
			return m, nil
		}

		return m, m.key(k)

	case tea.PasteMsg:
		return m, m.paste(msg)
	case tea.MouseMsg:
		cmd := m.mouse(msg)
		if _, moved := msg.(tea.MouseMotionMsg); !moved { // a click's release, or the wheel, too
			m.tipOff = time.Now()
		}

		return m, tea.Batch(cmd, m.trackSash(), m.scm.hoverTip(m, msg), m.tipLater())

	case sashMsg: // the pointer has rested: View lights the sash
		return m, nil
	case tipWakeMsg: // the pointer has rested: View shows the tip under it, or takes it down
		if msg.due.Equal(m.tipDue) {
			m.tipDue = time.Time{}
		}

		return m, m.tipLater()
	}
	// Other input messages.
	var cmds []tea.Cmd

	if m.modal != nil && m.modal.hasInput() {
		var cmd tea.Cmd

		m.modal.input, cmd = m.modal.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.scm.input.Focused() {
		var cmd tea.Cmd

		m.scm.input, cmd = m.scm.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	for _, in := range []*textinput.Model{&m.sr.query, &m.sr.include} {
		if in.Focused() {
			var cmd tea.Cmd

			*in, cmd = in.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	for i := range m.filters {
		if m.filters[i].editing {
			var cmd tea.Cmd

			m.filters[i].input, cmd = m.filters[i].input.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) paste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd

	t, id := m.focusedTerm()

	switch {
	case m.modal != nil && m.modal.form != nil:
		cmd = m.modal.form.paste(msg)
	case m.modal != nil && m.modal.hasInput():
		m.modal.input, cmd = m.modal.input.Update(msg)
		if m.modal.filter {
			m.modal.refilter()
		}

		if m.modal.complete != nil {
			m.modal.input.SetSuggestions(m.modal.complete(m.modal.input.Value()))
		}

	case t != nil && t.find.editing: // the terminal's find, not its shell, nor a file's find left open
		t.find.input, cmd = t.find.input.Update(msg)
		cmd = tea.Batch(cmd, t.search(m, id, 2))

	case m.pv.composing && m.focus == onMain:
		*m.pv.compose, cmd = m.pv.compose.Update(msg)
		m.pv.relayout(m)

	case m.pv.tableBox() && m.focus == onMain:
		in := m.pv.tbl.input()
		*in, cmd = in.Update(msg)

	case m.pv.find.editing && m.pv.repl.Focused():
		m.pv.repl, cmd = m.pv.repl.Update(msg)
	case m.pv.find.editing:
		m.pv.find.input, cmd = m.pv.find.input.Update(msg)
		m.pv.refind(m)

	case m.focus == onMain && m.showsPreview() && m.pv.editable():
		a, z, ok := m.pv.selection()
		if !ok {
			a, z = m.pv.at(), m.pv.at()
		}

		cmd = m.pv.edit(m, a, z, msg.Content)

	case m.scm.input.Focused():
		m.scm.input, cmd = m.scm.input.Update(msg)
		m.scm.fit(m)

	case m.sr.editing():
		in := &m.sr.query
		if m.sr.include.Focused() {
			in = &m.sr.include
		}

		*in, cmd = in.Update(msg)
		cmd = tea.Batch(cmd, m.sr.restart(m))

	case m.focus == onMain && m.showsSession():
		m.term.scroll = 0
		m.inputs <- proto.InputParams{ID: m.sess, Paste: msg.Content}

	case m.termFocused() && m.tv.id != "":
		m.tv.scroll = 0
		m.inputs <- proto.InputParams{ID: m.tv.id, Paste: msg.Content}

	case m.focus != onMain:
		v, ok := m.viewOn(m.focus)

		switch {
		case ok && m.filters[v].editing:
			m.filters[v].input, cmd = m.filters[v].input.Update(msg)
			m.refilter(v)

		case ok && v == viewFiles: // a terminal that turns ^v into a paste
			if a := m.ex.action("ctrl+v"); a != nil {
				cmd = a(m)
			}
		}
	}

	return cmd
}

func reconnect() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		if err := proto.EnsureDaemon(); err != nil {
			return reconnectMsg{err: err}
		}

		ch, err := proto.Subscribe()

		return reconnectMsg{ch, err}
	})
}

// flash shows text in the status bar for a while: the line of it worth
// reading (errLine), as a git error's stderr would break the bar into rows.
func (m *Model) flash(text string, isErr bool) {
	m.msg, m.msgErr, m.msgAt = errLine(text), isErr, time.Now()
	if isErr { // the whole of it, which the status bar cuts to a line
		m.out.add(outputLine{at: m.msgAt, src: "pando", msg: text, err: true})
	}
}

func (m *Model) onEvent(ev proto.Event) tea.Cmd {
	switch ev.Kind {
	case "state":
		return loadState()
	case "workspaces": // a project added or removed changes the state's project list too
		return tea.Batch(loadState(), loadWorkspaces())
	case "sessions":
		return loadSessions()
	case "screen":
		if ev.ID == m.sess || ev.ID == m.tv.id {
			return m.fetchScreen()
		}

	case "update":
		if u, ok := updateEvent(ev.Data); ok {
			return m.onUpdate(u)
		}

	case "focus":
		var f proto.FocusParams
		if json.Unmarshal(ev.Data, &f) != nil {
			return nil
		}

		var cmds []tea.Cmd
		if f.Workspace != "" && m.workspace(f.Workspace) != nil {
			cmds = append(cmds, m.switchWorkspace(f.Workspace), m.refreshGit())
		}

		if f.Session != "" {
			cmds = append(cmds, loadSessions(), func() tea.Msg { return focusSessionMsg(f.Session) })
		}

		if f.Open != "" {
			cmds = append(cmds, m.openSpec(f.Open))
		}

		return tea.Batch(cmds...)
	}

	return nil
}

type focusSessionMsg string

// unspill undoes what tmux with extended keys makes of an esc a mouse report
// lands behind within escape-time: ⌥esc, then the report as typed text, each
// character running its key. It gives back the esc and says false for the text.
func (m *Model) unspill(k tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	s := k.String()
	if s == "alt+esc" {
		m.spill = "["
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	}

	if len(s) != 1 || !strings.Contains(m.spill, s) {
		m.spill = ""
		return k, true
	}

	switch s {
	case "[":
		m.spill = "<"
	case "<":
		m.spill = "0123456789;Mm"
	case "M", "m":
		m.spill = ""
	}

	return k, false
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	if m.modal != nil {
		// The palette and Quick Open replace any modal; a list keeps ⌃p as ↑.
		switch s := k.String(); {
		case s == "ctrl+shift+p" && m.modal.title != "Command Palette":
			m.modal.dismiss(m)
			return m.commandPalette()

		case s == "ctrl+p" && (m.modal.form != nil || m.modal.dialog || m.modal.items == nil):
			m.modal.dismiss(m)
			return m.loadIndex(true)
		}

		return m.modal.key(m, k)
	}

	m.fixFocus()

	s, c := k.String(), m.keyContext()
	// [keys] goes first, so every chord below can be rebound or unbound too;
	// a terminal and a text box keep their keys, as VS Code's do.
	if c&ctxPanels != 0 {
		if cmd, ok := m.runKey(s); ok {
			return cmd
		}
	}

	if b, ok := m.bindingFor(c, s); ok {
		return b.run(m, s)
	}

	if t, id := m.focusedTerm(); t != nil && t.find.editing {
		return t.findKey(m, id, k)
	}

	if m.focus == onPanel { // a terminal takes every other key, a list its own
		return m.panelKey(k)
	}

	if m.focus == onMain {
		switch {
		case m.showsSession():
			return m.term.key(m, m.sess, k)
		case m.peekH() > 0:
			return m.refs.key(m, k)
		case s == "esc" && m.pv.kind == "":
			return m.confirmQuit()
		}

		return m.pv.key(m, k)
	}

	v, _ := m.viewOn(m.focus)
	if v == viewTerm { // a terminal takes every other key, a list its own
		return m.panelKey(k)
	}

	if m.filters[v].editing {
		return m.filterKey(v, k)
	}

	if v == viewGit && m.scm.input.Focused() {
		return m.scm.inputKey(m, k)
	}

	if v == viewSearch && m.sr.editing() {
		return m.sr.inputKey(m, k)
	}

	switch s {
	case "q":
		return m.confirmQuit()
	case "ctrl+c":
		switch {
		case v == viewFiles: // Explorer's Copy, as VS Code's; q and esc still quit
			break
		case m.tableEdits() > 0: // no draft keeps them: ask, as q does
			return m.confirmQuit()
		default:
			return tea.Sequence(m.saveEditors(), m.saveTerm(), m.saveDrafts(), tea.Quit)
		}

	case "1", "2", "3", "4":
		cmd := m.showView(view(s[0] - '1'))
		if s == "4" {
			return tea.Batch(cmd, m.sr.focus())
		}

		return cmd

	case "6": // 5 is the terminal's; the view numbers skip it
		if !m.hasGH {
			return nil
		}

		return m.showView(viewGitHub)

	case "7":
		if !m.hasDB {
			return nil
		}

		return m.showView(viewDB)

	case "ctrl+f":
		if v == viewSearch {
			return m.sr.focus()
		}

		return m.startFilter(v)

	case ",":
		return m.openSettings()

	case "?":
		m.modal = helpModal(m.hasGH, m.hasDB)
		return nil

	case "b":
		return m.hide(m.focus)
	case "<", ">":
		d := 4
		if s == "<" {
			d = -4
		}

		m.setColWidth(m.focus, max(20, min(m.colRect(m.focus).w+d, 100)))

		return tea.Batch(m.saveCols(), m.fetchScreen())

	case "tab":
		if m.sess != "" || m.pv.kind != "" {
			m.focus = onMain
		}

		return nil

	case "esc":
		if m.filters[v].on {
			m.clearFilter(v)
			return nil
		}

		return m.confirmQuit()
	}

	return m.viewKey(v, k)
}

func (m *Model) viewKey(v view, k tea.KeyPressMsg) tea.Cmd {
	switch v {
	case viewFiles:
		return m.ex.key(m, k)
	case viewGit:
		return m.scm.key(m, k)
	case viewSearch:
		return m.sr.key(m, k)
	case viewGitHub:
		return m.gh.key(m, k)
	case viewDB:
		return m.db.key(m, k)
	default: // Agents; the Terminal and the session are handled before here.
		return m.ag.key(m, k)
	}
}

func (m *Model) startFilter(v view) tea.Cmd {
	f := &m.filters[v]
	f.on, f.editing = true, true
	cmd := f.input.Focus()

	m.refilter(v)

	if v == viewFiles && (m.index.ws != m.ws || time.Since(m.index.at) > 10*time.Second) {
		return tea.Batch(cmd, m.loadIndex(false))
	}

	return cmd
}

func (m *Model) clearFilter(v view) {
	f := &m.filters[v]
	f.input.Reset()
	f.input.Blur()
	f.on, f.editing = false, false

	m.refilter(v)
}

// refilter re-applies view v's query after it changed.
func (m *Model) refilter(v view) {
	switch v {
	case viewFiles:
		m.ex.l.top = 0
		m.ex.rebuild(m)

	case viewGit:
		m.scm.tops = map[string]int{}
		m.scm.build(m)

	case viewGitHub:
		m.gh.l.top = 0
	case viewDB:
		m.db.l.top = 0
	default:
		m.ag.l.top = 0
	}
}

func (m *Model) filterKey(v view, k tea.KeyPressMsg) tea.Cmd {
	f := &m.filters[v]

	switch k.String() {
	case "esc":
		m.clearFilter(v)
		return nil

	case "enter":
		f.editing = false
		f.input.Blur()

		if strings.TrimSpace(f.input.Value()) == "" {
			m.clearFilter(v)
		}

		return nil

	case "up", "down":
		return m.viewKey(v, k)
	}

	before := f.input.Value()

	var cmd tea.Cmd

	f.input, cmd = f.input.Update(k)
	if f.input.Value() != before {
		m.refilter(v)
	}

	return cmd
}

// stopEditing ends typing in every filter line; the filters stay applied.
func (m *Model) stopEditing() {
	for i := range m.filters {
		m.filters[i].editing = false
		m.filters[i].input.Blur()
	}

	m.sr.blur()
}

// closeEmptyFilters closes every filter line left empty and not typed in: a
// click away dismisses it.
func (m *Model) closeEmptyFilters() {
	for i := range m.filters {
		if f := &m.filters[i]; f.on && !f.editing && f.input.Value() == "" {
			m.clearFilter(view(i))
		}
	}
}

// shiftMouse moves a mouse event's row by dy (the border row above the content).
func shiftMouse(msg tea.MouseMsg, dy int) tea.MouseMsg {
	if dy == 0 {
		return msg
	}

	switch e := msg.(type) {
	case tea.MouseClickMsg:
		e.Y += dy
		return e

	case tea.MouseReleaseMsg:
		e.Y += dy
		return e

	case tea.MouseMotionMsg:
		e.Y += dy
		return e

	case tea.MouseWheelMsg:
		e.Y += dy
		return e
	}

	return msg
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	msg = shiftMouse(msg, -m.bord())
	mo := msg.Mouse()

	// A modal owns the pointer: what lies under it keeps the hover it had,
	// the row a right click opened a menu on, as VS Code's context menu does.
	if m.modal != nil {
		return m.modal.mouse(m, msg)
	}

	m.mouseX, m.mouseY, m.mouseAt = mo.X, mo.Y, time.Now()
	m.overBar = barPoint{} // the bar under it, if any, notes it again below
	m.overTab = ""         // so does the tab
	m.overX = ""

	_, click := msg.(tea.MouseClickMsg)
	if m.drag != nil {
		if !click {
			return m.dragMouse(msg)
		}

		m.drag = nil // its release never arrived
	}

	if mo.Y == m.h-1+m.bord() { // the status bar, under the frame
		if click && mo.Button == tea.MouseLeft {
			return m.statusMouse(mo.X)
		}

		return nil
	}

	if m.ruled() == 1 && mo.Y == m.panelH() { // the rule over it
		return nil
	}

	if mo.Y == -1 && click && m.bord() > 0 { // a frame's top edge carries its panel's title
		return m.titleMouse(mo)
	}

	if mo.Y < 0 || mo.Y >= m.h {
		return nil // border rows
	}

	if click {
		defer m.closeEmptyFilters() // after the click lands in the layout it was aimed at

		m.stopEditing()
	}

	cs, c := m.layout()
	gap := 1 + m.bord()

	for i, r := range cs {
		switch {
		case r.w == 0:
		case mo.X >= r.x && mo.X < r.x+r.w:
			return m.sideMouse(i, r, msg)
		case m.side(i) == 0 && mo.X >= r.x+r.w && mo.X < r.x+r.w+gap, m.side(i) == 1 && mo.X >= r.x-gap && mo.X < r.x:
			// The divider on a column's main side resizes that column.
			if click && mo.Button == tea.MouseLeft && !m.railed(i) {
				m.drag = &drag{kind: dragDivider, col: i}
			}

			return nil
		}
	}

	if mo.X < c.x || mo.X >= c.x+c.w {
		return nil
	}

	if n := m.termRows(); n > 0 && mo.Y >= m.areaH() {
		if click {
			m.focus = onPanel
			m.scm.input.Blur()
		}
		// y == 0 is its strip, with its tabs and buttons, y == 1 the rule
		// under it, the rest its body; above it the border, the sash.
		switch y := mo.Y - m.panelStripY(); {
		case y < 0:
			if click && mo.Button == tea.MouseLeft {
				m.drag = &drag{kind: dragTerm, y0: mo.Y, h0: m.termRows()}
			}

			return nil

		case y > 1:
			return m.panelMouse(msg, mo.X-c.x, y-termStripH)
		case y == 1, !click, mo.Button != tea.MouseLeft:
			return nil
		}

		return m.panelStripClick(mo.X-c.x, c.w, mo)
	}

	return m.groupMouse(msg, click)
}

// groupMouse is the mouse over the editor groups: a divider between two
// drags, a click gives the group under it the keyboard, the wheel scrolls
// the group under it where it is, and the rest is the active group's.
func (m *Model) groupMouse(msg tea.MouseMsg, click bool) tea.Cmd {
	mo := msg.Mouse()

	var focus tea.Cmd

	if m.tiled() {
		if p := m.splitAt(mo.X, mo.Y); p != nil {
			switch {
			case !click || mo.Button != tea.MouseLeft:
			case m.clicks.double(mo.X, mo.Y):
				return m.evenPair(p)
			default:
				m.drag = &drag{kind: dragGrid, split: p}
			}

			return nil
		}

		switch p := m.paneAt(mo.X, mo.Y); {
		case p == nil || p == m.cur:
		case click:
			focus = m.focusGroup(p)
		case mo.Button == tea.MouseWheelUp || mo.Button == tea.MouseWheelDown:
			var cmd tea.Cmd

			m.within(p, func() { cmd = m.groupMouse(msg, false) })

			return cmd

		default: // hovering another group lights its tab, not the active one's
			m.within(p, func() {
				if m.stripH() > 0 && mo.Y == m.mainY() {
					m.setOver(stripEditor, mo.X-m.mainX())
				}
			})

			return nil
		}
	}

	if click {
		m.focus = onMain
		m.scm.input.Blur()
	}

	x, y0, strip := mo.X-m.mainX(), m.mainY(), m.stripH()
	if strip > 0 && mo.Y == y0 {
		m.setOver(stripEditor, x)
	}

	if strip > 0 && click && mo.Y == y0 && (mo.Button == tea.MouseLeft || mo.Button == tea.MouseMiddle || mo.Button == tea.MouseRight) {
		return tea.Batch(focus, m.stripMouse(x, mo))
	}

	if m.showsSession() {
		return tea.Batch(focus, m.sessionMouse(msg, x, mo.Y-y0-m.headH()-strip, m.mainW()))
	}

	y := mo.Y - y0 - m.headH() - strip
	if ph := m.peekH(); ph > 0 { // the References inline: the text under them is past them
		m.refs.hov = -1

		switch r := m.peekRow(); {
		case y > r && y <= r+ph:
			return tea.Batch(focus, m.refs.mouse(m, msg, x, y-r-1))
		case y > r+ph && y < m.pvH():
			y -= ph
		}
	}

	return tea.Batch(focus, m.pv.mouse(m, msg, x, y))
}

// sessionMouse is the session's screen at body cell (x, y), w cells wide in
// the editor area: the find widget, the scrollbar in the last column, then
// the app.
func (m *Model) sessionMouse(msg tea.MouseMsg, x, y, w int) tea.Cmd {
	if cmd, ok := m.term.findMouse(m, m.sess, msg, x, y, w); ok {
		return cmd
	}

	if x == w-1 && y >= 0 { // the scrollbar
		m.overBar = barPoint{"session", y}

		if _, click := msg.(tea.MouseClickMsg); click && msg.Mouse().Button == tea.MouseLeft {
			return m.barMouse(&m.term, "session", y, msg.Mouse().Y)
		}

		return nil
	}

	return m.term.mouse(m, m.sess, msg, x, y)
}

// titleMouse is a click on a framed panel's title: the editor area's session
// gives its menu, a sidebar view is dragged or gives its menu as by its
// header.
func (m *Model) titleMouse(mo tea.Mouse) tea.Cmd {
	cs, c := m.layout()
	if mo.X >= c.x && mo.X < c.x+c.w {
		if m.showsSession() && mo.Button == tea.MouseRight {
			return m.sessionTitleMenu()
		}

		return nil
	}

	for i, r := range cs {
		if r.w == 0 || mo.X < r.x || mo.X >= r.x+r.w || m.railed(i) {
			continue
		}

		v, _ := m.viewOn(i)

		switch mo.Button {
		case tea.MouseLeft:
			m.drag = &drag{kind: dragTab, v: v, x0: mo.X, y0: mo.Y}
		case tea.MouseRight:
			return m.tabMenu(v, mo.X, mo.Y)
		}
	}

	return nil
}

// vertBarMouse maps a click in the vertical activity bar of column s to one of
// its tab icons, or to the gear at the bottom. y counts rows from the top.
func (m *Model) vertBarMouse(s int, mo tea.Mouse, y int, click bool) tea.Cmd {
	if !click {
		return nil
	}

	for _, t := range m.vertTabs(s) {
		if y < t.x || y >= t.x+vertH { // the icon's row and the air around it
			continue
		}

		if mo.Button == tea.MouseRight {
			return m.tabMenu(t.v, mo.X, mo.Y)
		}

		if mo.Button == tea.MouseLeft {
			m.drag = &drag{kind: dragTab, v: t.v, x0: mo.X, y0: mo.Y, fine: true}
			if v, _ := m.viewOn(s); v == t.v && !m.railed(s) {
				m.drag.hide = true
				return nil
			}
		}

		return m.showView(t.v)
	}

	if y >= m.panelH()-vertH && mo.Button == tea.MouseLeft {
		return m.manageMenu(mo.X, mo.Y)
	}

	return nil
}

func (m *Model) sideMouse(s int, rc rect, msg tea.MouseMsg) tea.Cmd {
	mo := msg.Mouse()
	_, click := msg.(tea.MouseClickMsg)

	v, ok := m.viewOn(s)
	if !ok {
		return nil
	}

	if m.railed(s) {
		return m.vertBarMouse(s, mo, mo.Y, click)
	}

	if click {
		m.focus = s
	}

	x, y, bar := mo.X-rc.x, mo.Y, m.barH(s)
	if bw := m.barW(s); bw > 0 {
		inBar := x < bw
		if m.side(s) == 1 {
			inBar = x >= rc.w-bw
		} else {
			x -= bw
		}

		if inBar {
			return m.vertBarMouse(s, mo, y, click)
		}
	}

	switch {
	case y < bar:
		if !click {
			return nil
		}

		if x >= rc.w-ansi.StringWidth(gearLabel()) {
			return m.manageMenu(mo.X, mo.Y+1)
		}

		for _, t := range m.tabs(s) {
			if x >= t.x && x < t.x+t.w {
				if mo.Button == tea.MouseRight {
					return m.tabMenu(t.v, mo.X, mo.Y)
				}

				if mo.Button == tea.MouseLeft { // drag it to the other side
					m.drag = &drag{kind: dragTab, v: t.v, x0: mo.X, y0: mo.Y, hide: t.v == v}
					if t.v == v {
						return nil
					}
				}

				return m.showView(t.v)
			}
		}

		if mo.Button == tea.MouseRight {
			return m.tabMenu(v, mo.X, mo.Y)
		}

		return nil

	case y == bar && headRows(v) > 0:
		if !click {
			return nil
		}

		for _, a := range m.headerActions(s, v, rc.w) {
			if x >= a.x && x < a.x+a.w {
				return a.run(m)
			}
		}

		if mo.Button == tea.MouseRight {
			return m.tabMenu(v, mo.X, mo.Y)
		}

		if mo.Button == tea.MouseLeft { // the title drags the view as its tab does
			m.drag = &drag{kind: dragTab, v: v, x0: mo.X, y0: mo.Y}
		}

		return nil
	}

	y -= bar + headRows(v)
	if m.filters[v].on {
		if y < 3 { // the box and its frame
			if click {
				m.filters[v].editing = true
				return m.filters[v].input.Focus()
			}

			return nil
		}

		y -= 3
	}

	if v != viewGit && click {
		m.scm.input.Blur()
	}

	if b, ok := m.listBar(v, y); ok && x == rc.w-m.barW(s)-1 { // the wheel over it scrolls the list, below
		m.overBar = barPoint{b.id, b.row}

		switch {
		case !click:
		case mo.Button != tea.MouseLeft:
			return nil
		default:
			return m.barClick(b.id, b.row, mo.Y-b.row, b.geo, b.to)
		}
	}

	return m.viewMouse(v, msg, x, y)
}

// viewMouse routes a mouse event to view v's body, y counted from its first
// list row.
func (m *Model) viewMouse(v view, msg tea.MouseMsg, x, y int) tea.Cmd {
	_, click := msg.(tea.MouseClickMsg)
	mo := msg.Mouse()

	switch v {
	case viewFiles:
		return m.ex.mouse(m, msg, y)
	case viewGit:
		return m.scm.mouse(m, msg, x, y)
	case viewSearch:
		return m.sr.mouse(m, msg, x, y)
	case viewTerm:
		if y == 0 { // the strip: its tabs and buttons
			if a, ok := hit(m.termButtons(), x); ok && click && mo.Button == tea.MouseLeft {
				return a.run(m)
			}

			if tab, ok := m.panelTabAt(x, panelTabX, m.termW()); ok && click && mo.Button == tea.MouseLeft {
				return m.pressPanelTab(tab, mo)
			}

			return nil
		}

		if y == 1 { // the rule under it
			return nil
		}

		return m.panelMouse(msg, x, y-termStripH)

	case viewGitHub:
		return m.gh.mouse(m, msg, y)
	case viewDB:
		return m.db.mouse(m, msg, y)
	default: // Agents.
		return m.ag.mouse(m, msg, x, y)
	}
}

func (m *Model) dragMouse(msg tea.MouseMsg) tea.Cmd {
	d, mo := m.drag, msg.Mouse()

	_, release := msg.(tea.MouseReleaseMsg)
	if _, motion := msg.(tea.MouseMotionMsg); !motion && !release {
		return nil
	}

	switch d.kind {
	case dragPane:
		if d.v == viewGitHub {
			return m.gh.dragPane(m, d, mo.Y, release)
		}

		return m.scm.dragPane(m, d, mo.Y, release)

	case dragSelect:
		return m.pv.dragTo(m, mo.X, mo.Y, release)
	case dragTab:
		return m.dragTabTo(d, mo.X, mo.Y, release)
	case dragTerm:
		return m.dragTerm(d, mo.Y, release)
	case dragRow:
		return m.ag.dragRowTo(m, d, mo.X, mo.Y, release)
	case dragScroll:
		return m.barDragTo(d.bar, mo.X, mo.Y, release)
	case dragStrip:
		return m.dragStripTo(d, mo.X, mo.Y, release)
	case dragGrid:
		return m.dragSplit(d, mo.X, mo.Y, release)
	case dragOpen:
		return m.dragOpenTo(d, mo.X, mo.Y, release)
	case dragMsgSel:
		m.scm.input.ExtendSelection(mo.X-d.x0, mo.Y-d.y0)

		if release {
			m.scm.input.EndSelection()
			m.drag = nil
		}

		return nil
	}

	if !release {
		r := m.colRect(d.col)

		w := mo.X - r.x
		if m.side(d.col) == 1 {
			w = r.x + r.w - 1 - mo.X
		}

		m.setColWidth(d.col, max(20, min(w, m.w-21)))

		if r := m.colRect(d.col); r.w >= 20 && r.w < w { // as wide as it shows: the editor keeps minEditor
			m.setColWidth(d.col, r.w)
		}

		d.moved = true

		return nil
	}

	m.drag = nil

	if !d.moved {
		return nil
	}

	return tea.Batch(m.saveCols(), m.fetchScreen())
}

// dragTerm resizes the bottom panel by the border over it, as VS Code's sash does;
// the height is saved when the row is let go.
func (m *Model) dragTerm(d *drag, y int, release bool) tea.Cmd {
	if release {
		m.drag = nil

		if !d.moved {
			return nil
		}

		return tea.Batch(m.setSettings(map[string]any{"terminal_height": m.st.Settings.TermH}), m.fetchScreen())
	}

	m.termMax, d.moved = false, true
	m.st.Settings.TermH = max(m.panelTop()+2, min(d.h0+d.y0-y, m.panelH()-3))

	return m.fetchScreen()
}

// minEditor is the least width the editor area keeps beside the columns.
const minEditor = 40

// View draws the columns side by side into the panel rows, then the status
// bar, the overlays and the cursor.
func (m *Model) View() tea.View {
	if m.still && m.drawn != nil {
		return *m.drawn
	}

	v := tea.NewView("")
	v.AltScreen = true

	v.MouseMode = tea.MouseModeAllMotion // hover rows and header actions
	if m.w == 0 || m.h == 0 {
		return v
	}

	m.tip = "" // the frame notes the button under the mouse again
	m.fits.frame()
	m.cuts.frame()

	cs, c := m.layout()
	m.frame = &frame{m.cols(), cs, c}

	defer func() { m.frame = nil }()

	b := m.bord()

	if k := m.baseKey(); k == (baseKey{}) || m.base == nil || m.base.key != k {
		ls, top, bot := m.baseFrame(cs, c, b)
		m.base = &base{key: k, lines: ls, top: top, bot: bot}
	}

	lines, top, bot := m.base.lines, m.base.top, m.base.bot
	if d := m.drag; d != nil && d.drop != nil {
		if m.base.drop == nil || *m.base.drop != *d.drop { // the pointer went on to another target
			t := *d.drop
			m.base.drop, m.base.boxed = &t, slices.Clone(lines)
			m.dropBox(m.base.boxed[:m.panelH()], t)
		}

		lines = m.base.boxed
	}

	lines = slices.Clone(lines)

	if box, x, y, ok := m.dragGhost(); ok {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.pv.findBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.pv.sigBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.pv.compBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.pv.hoverBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.scm.tipBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if boxes, xs, ys := m.termFindBoxes(); m.modal == nil {
		for i, box := range boxes {
			m.overlayBox(lines, box, xs[i], ys[i])
		}
	}

	if m.tip != "" && m.st.Settings.Tips && m.tipShown() && m.modal == nil && m.drag == nil {
		box, x, y := m.hoverBox([]string{m.tip})
		m.overlayBox(lines, box, x, y)
		m.tipSeen = time.Now()
	}

	cx, cy, caret := takeCaret(lines)

	switch {
	case m.modal != nil:
		box, x, y := m.modal.view(m)
		m.overlayBox(lines, strings.Split(box, "\n"), x, y)

		if x, y, ok := takeCaret(lines); ok {
			v.Cursor = tea.NewCursor(x, b+y)
		}

	case caret: // a text box has the keyboard
		v.Cursor = tea.NewCursor(cx, b+cy)

	case m.termFocused() && m.termShowing():
		x, y, _, h, _ := m.panelOrigin()
		if h > 0 && m.tv.scr.CursorVisible && !m.tv.find.editing && m.tv.term.id == m.tv.id && m.tv.scr.CursorX >= m.tv.left {
			v.Cursor = appCursor(x+m.tv.scr.CursorX-m.tv.left, b+y+m.tv.scr.CursorY, m.tv.scr.CursorStyle)
		}

	case m.focus == onMain && m.showsSession():
		if m.term.scr.CursorVisible && m.term.id == m.sess && !m.term.find.editing {
			v.Cursor = appCursor(m.mainX()+m.term.scr.CursorX, b+m.mainY()+m.headH()+m.stripH()+m.term.scr.CursorY, m.term.scr.CursorStyle)
		}

	case m.focus == onMain && m.showsPreview() && !m.pv.scrollOnly(m) && m.pv.kind != pvGH && m.peekH() == 0: // a page's cursor is its tinted row; the References inline have the keys
		if x, y, ok := m.pv.cursor(m.pvW(), m.pvH()); ok {
			v.Cursor = tea.NewCursor(m.mainX()+x, b+m.mainY()+m.headH()+m.stripH()+y)
			if m.pv.vimOn(m) && m.pv.vim.mode == vimInsert {
				v.Cursor.Shape = tea.CursorBar // block in normal mode, a bar while inserting
			}
		}
	}

	if b == 1 { // the status bar sits under the frame, as VS Code's sits under the workbench
		status, panels := lines[len(lines)-1], lines[:len(lines)-1]
		lines = append(append(append([]string{top}, panels...), bot), status)
	}

	if !m.graphemes {
		lines = m.wcSafeLines(lines)
	}

	v.SetContent(strings.Join(lines, "\n"))
	m.drawn, m.drawnAt = &v, time.Now()

	return v
}

func (m *Model) divider(i int) string {
	if st, ok := m.sashStyle(colSash(i)); ok {
		return renderText(st, "┃")
	}

	return renderText(fg(pal.panelBorder), "│")
}

// sash is a divider the mouse drags to resize: a column's edge toward the
// editor (col, its index), the bottom Terminal panel's title row, the header
// of an open Source Control drawer or GitHub section (v and pane, as its
// dragPane names them), or the divider of a split between editor groups.
type sash struct {
	col  int
	v    view
	pane string
	grid *pane
}

var (
	noSash   = sash{col: -1}
	termSash = sash{col: -2}
)

// colSash is the edge of column i toward the editor.
func colSash(i int) sash { return sash{col: i} }

// paneSash is the header of pane in view v, viewGit or viewGitHub.
func paneSash(v view, pane string) sash { return sash{col: -3, v: v, pane: pane} }

// gridSash is the divider of split p between editor groups.
func gridSash(p *pane) sash { return sash{col: -4, grid: p} }

// sashDelay is how long the pointer rests on a sash before it lights, VS
// Code's workbench.sash.hoverDelay default.
const sashDelay = 300 * time.Millisecond

// sashMsg wakes View once a sash may have waited out sashDelay.
type sashMsg struct{}

// sashUnder is the sash at content cell (x, y): the gaps the mouse code
// starts a dragDivider in, the border over the bottom panel and the pane
// headers whose drag resizes.
func (m *Model) sashUnder(x, y int) sash {
	if y < 0 || y >= m.panelH() {
		return noSash
	}

	cs, c := m.layout()
	gap := 1 + m.bord()

	for i, r := range cs {
		switch {
		case r.w == 0 || m.railed(i):
		case m.side(i) == 0 && x >= r.x+r.w && x < r.x+r.w+gap, m.side(i) == 1 && x >= r.x-gap && x < r.x:
			return colSash(i)
		}
	}

	if s := m.paneSashUnder(); s != noSash {
		return s
	}

	if p := m.splitAt(x, y); p != nil && m.tiled() {
		return gridSash(p)
	}

	if m.termRows() == 0 || y < m.areaH() || y >= m.panelStripY() || x < c.x || x >= c.x+c.w {
		return noSash
	}

	return termSash
}

// paneSashUnder is the pane header under the mouse that a drag resizes: an
// open Source Control drawer's, or a GitHub section's below another open one.
func (m *Model) paneSashUnder() sash {
	if y := m.hoverRow(viewGit); y >= 0 && len(m.scm.repos) > 0 {
		_, ds := m.scm.geometry(m, m.scm.paneH(m))
		for j, d := range ds {
			if t := m.drawers()[j].Title; j < len(m.scm.heads) && y == d.head && m.pane(t).Open {
				return paneSash(viewGit, t)
			}
		}
	}

	if y := m.hoverRow(viewGitHub); y >= 0 {
		rows := m.gh.rows(m)
		ps := m.gh.panes(rows, m.bodyH(viewGitHub))

		if i, p := m.gh.at(rows, ps, y); p != nil && i == p.start-1 && m.gh.resizes(ps, p.sec) {
			return paneSash(viewGitHub, ghSections[p.sec])
		}
	}

	return noSash
}

// trackSash notes the sash the mouse is on and wakes View after sashDelay
// to light it, as long as the mouse stays.
func (m *Model) trackSash() tea.Cmd {
	s := m.sashUnder(m.mouseX, m.mouseY)
	if s == m.sashAt {
		return nil
	}

	m.sashAt, m.sashSince = s, time.Now()
	if s == noSash {
		return nil
	}

	return tea.Tick(sashDelay, func(time.Time) tea.Msg { return sashMsg{} })
}

// sashStyle is how sash s draws when it is more than a plain divider: the
// accent while it is dragged, sash_hover once the pointer has rested on it,
// VS Code's sash.activeBorder and sash.hoverBorder. ok is false otherwise.
func (m *Model) sashStyle(s sash) (lipgloss.Style, bool) {
	if d := m.drag; d != nil {
		return fg(pal.accent), d.kind == dragDivider && colSash(d.col) == s || d.kind == dragTerm && s == termSash ||
			d.kind == dragPane && d.moved && paneSash(d.v, d.pane) == s || d.kind == dragGrid && gridSash(d.split) == s
	}

	return fg(pal.sashHover), m.modal == nil && s != noSash && s == m.sashAt && time.Since(m.sashSince) >= sashDelay
}

// sashRule is the rule a title row that is sash s draws across the cells
// its segments leave free, while s lights; none otherwise.
func (m *Model) sashRule(s sash, w int, segs ...[]seg) []seg {
	st, ok := m.sashStyle(s)
	if !ok {
		return nil
	}

	for _, ss := range segs {
		for _, x := range ss {
			w -= ansi.StringWidth(x.s)
		}
	}

	if w <= 2 {
		return nil
	}

	return []seg{sg(" "+strings.Repeat("━", w-2)+" ", st)}
}

func (m *Model) sideTitle(s int) string {
	if m.railed(s) { // a hidden side shows no view to name
		return ""
	}

	v, _ := m.viewOn(s)
	if v == viewTerm { // its tabs say it
		return ""
	}

	return viewTitles[v]
}

// mainTitle is the editor area's frame title: where it is, the project and
// worktree, since the tab says what it shows.
func (m *Model) mainTitle() string {
	switch {
	case m.showsSession():
		if s := m.session(m.sess); s != nil {
			return m.wsTitle(s.Workspace)
		}

	case m.pv.kind != "":
		return m.wsTitle(m.ws)
	}

	return "pando"
}

// wsTitle is "project · branch" for the workspace at path.
func (m *Model) wsTitle(path string) string {
	w := m.workspace(path)
	if w == nil {
		return filepath.Base(path)
	}

	if proj := filepath.Base(cmp.Or(w.Project, w.Path)); proj != wsName(*w) {
		return proj + " · " + wsName(*w)
	}

	return wsName(*w)
}

func (m *Model) sidebar(s, w int) []string {
	if m.railed(s) {
		return m.vertBar(s, w)
	}

	if bw := m.barW(s); bw > 0 {
		icons, body := m.vertBar(s, bw), m.sidebarBody(s, w-bw)
		for i := range body {
			if m.side(s) == 0 {
				body[i] = icons[i] + body[i]
			} else {
				body[i] += icons[i]
			}
		}

		return body
	}

	return m.sidebarBody(s, w)
}

func (m *Model) sidebarBody(s, w int) []string {
	active, _ := m.viewOn(s)

	out := make([]string, 0, m.h)
	if m.barH(s) > 0 {
		out = append(out, m.activityBar(s, w)...)
	}

	if headRows(active) > 0 {
		out = append(out, m.viewHeader(s, active, w))
	}

	if f := &m.filters[active]; f.on { // a framed box, as Search's
		out = append(out, framedBox(&f.input, w, f.editing, m.dark)...)
	}

	h := m.bodyH(active)
	switch active {
	case viewFiles:
		out = append(out, m.ex.lines(m, w, h)...)
	case viewGit:
		out = append(out, m.scm.lines(m, w, h)...)
	case viewSearch:
		out = append(out, m.sr.lines(m, w, h)...)
	case viewTerm:
		out = append(out, m.termLines(w, h)...)
	case viewGitHub:
		out = append(out, m.gh.lines(m, w, h)...)
	case viewDB:
		out = append(out, m.db.lines(m, w, h)...)
	default:
		out = append(out, m.ag.lines(m, w, h)...)
	}

	for len(out) < m.panelH() {
		out = append(out, blank(w))
	}

	return out[:m.panelH()]
}

// activityBar is the view switcher: an icon chip per view, marked the way VS
// Code marks them, with no fill behind them. The active one takes the accent
// color and activityBarTop.activeBorder under it, a bar on the row below,
// the one under the mouse a tint of both, and the settings gear sits on the
// right. Under the rest runs VS Code's sideBarActivityBarTop.border, a
// faint line through the middle of the row, air above it under the icons and
// below it before the view; a mark is that line in its own color.
func (m *Model) activityBar(s, w int) []string {
	active, _ := m.viewOn(s)
	rc := m.colRect(s)
	// Under the mouse a chip takes a tint of the accent, icon and mark both.
	hot := func(x, w int) bool {
		return m.mouseY < actH && time.Since(m.mouseAt) < actionsLinger && m.mouseX >= rc.x+x && m.mouseX < rc.x+x+w
	}

	tabs, gw := m.tabs(s), ansi.StringWidth(gearLabel())
	k := barKey{w: w, active: active, hot: -1, gear: hot(w-gw, gw), gen: lookGen}

	for i, t := range tabs {
		if hot(t.x, t.chipW) {
			k.hot = i
		}
	}

	if d, ok := m.bars[s]; !ok || d.key != k || !slices.Equal(d.tabs, tabs) {
		if m.bars == nil {
			m.bars = map[int]barDrawn{}
		}

		m.bars[s] = barDrawn{k, tabs, drawBar(k, tabs, gw)}
	}

	switch {
	case k.gear:
		m.tip = settingsTip
	case k.hot >= 0:
		m.tip = viewTip(tabs[k.hot].v)
	}

	return m.bars[s].lines
}

// barKey is what a column's activity bar is drawn from besides its chips:
// a bar drawn from the same is the one drawn last (Model.bars).
type barKey struct {
	w      int
	active view
	hot    int  // the chip under the mouse, -1 for none
	gear   bool // the gear under the mouse
	gen    int  // lookGen: the palette and icons it is drawn in
}

// barDrawn is an activity bar as drawn from key and tabs.
type barDrawn struct {
	key   barKey
	tabs  []tab
	lines []string
}

// drawBar draws an activity bar, its chips and the gear gw cells wide on a
// row, the active chip's and the hot one's marks on the row under it.
func drawBar(k barKey, tabs []tab, gw int) []string {
	idle := func(hot bool) lipgloss.Style {
		if hot {
			return fg(pal.sashHover)
		}

		return dim
	}

	var segs, marks []seg

	rule := func(n int) seg { return sg(strings.Repeat("─", max(n, 0)), fg(pal.rulerBorder)) }

	x, cx := 0, 0
	for i, t := range tabs {
		segs = append(segs, sg(blank(t.x-x), plain))

		st := idle(i == k.hot)
		if t.v == k.active { // the open view, in the accent color
			st = fg(pal.headerAccent).Bold(true)
		}

		if c := markColor(t.v == k.active, i == k.hot); c != nil {
			marks = append(marks, rule(t.x-cx), sg(strings.Repeat("─", t.chipW), fg(c)))
			cx = t.x + t.chipW
		}

		segs = append(segs, sgOwn(t.label, st))
		if t.badge != "" {
			segs = append(segs, sg(t.badge, fg(pal.attention)))
		}

		x = t.x + t.w
	}

	gearMark := rule(gw)
	if k.gear { // the gear is a chip too
		gearMark = sg(strings.Repeat("─", gw), fg(pal.sashHover))
	}

	return []string{
		row(k.w, nil, segs, sgOwn(gearLabel(), idle(k.gear))),
		row(k.w, nil, append(marks, rule(k.w-gw-cx), gearMark)),
	}
}

// markColor is the active border's color for a chip: the accent for the open
// view, a tint of it under the mouse (sash_hover), nothing otherwise.
func markColor(active, hovered bool) color.Color {
	switch {
	case active:
		return pal.headerAccent
	case hovered:
		return pal.sashHover
	}

	return nil
}

// vertBar is the activity bar down a sidebar's outer edge: the top bar's
// chips stacked, one `vertH`-row block each, on VS Code's
// activityBar.background (`bar_bg`). The active one takes the accent color
// and VS Code's activityBar.activeBorder on the strip's outer edge, the
// button high and centered on its icon, where the top bar underlines
// instead; the one
// under the mouse takes a tint of the accent, icon and mark. A view waiting on
// its agent has no room for a count here, so its icon takes the attention
// color instead.
// VS Code's activityBar.border runs down the inner edge, an eighth of a cell
// on the side facing the view as the CSS border is, so the icon sits midway
// between it and the mark; a rail has none, the column divider is its edge.
func (m *Model) vertBar(s, w int) []string {
	active, open := m.viewOn(s)
	open = open && !m.railed(s) // a hidden side shows no view: none of its chips is marked
	rc := m.colRect(s)

	x0 := rc.x
	if m.side(s) == 1 {
		x0 = rc.x + rc.w - w
	}
	// Under the mouse a chip lights up, as the top bar's does; the whole
	// block counts, not just the icon's row.
	hot := func(y int) bool {
		return time.Since(m.mouseAt) < actionsLinger && m.mouseX >= x0 && m.mouseX < x0+w &&
			m.mouseY >= y && m.mouseY < y+vertH
	}
	// The active border runs down the strip's outer edge, VS Code's the
	// button high: a heavy line from the middle of the row over the icon to
	// the middle of the one under it, the block elements' halves too thick
	// for it.
	marks, at, inner, border := [3]string{"╻", "┃", "╹"}, 0, w-1, "▕"
	if m.side(s) == 1 {
		at, inner, border = w-1, 0, "▏"
	}

	if m.railed(s) {
		inner = -1
	}

	// line is one row of the strip: the mark on the outer edge, the border on
	// the inner one, the icon centered.
	line := func(icon string, st lipgloss.Style, edge string, mark color.Color) string {
		iw := ansi.StringWidth(icon)
		l := (w - iw) / 2

		var segs []seg

		for i := 0; i < w; {
			switch {
			case mark != nil && i == at:
				segs, i = append(segs, sg(edge, fg(mark))), i+1
			case i == inner:
				segs, i = append(segs, sg(border, fg(pal.rulerBorder))), i+1
			case icon != "" && i == l:
				segs, i = append(segs, sg(icon, st)), i+iw
			default:
				segs, i = append(segs, sg(" ", plain)), i+1
			}
		}

		return row(w, pal.barBg, segs)
	}

	// cells are the strip's rows: an icon, a part of a mark. Where the
	// active chip's mark and the one under the mouse meet in a row, the
	// active one's part shows.
	type cell struct {
		icon, edge string
		st         lipgloss.Style
		mark       color.Color
	}

	rows := make([]cell, m.panelH())
	// chipAt draws one chip, the block from row y: its icon on row vertH/2,
	// the mark around it.
	chipAt := func(y int, g glyph, st lipgloss.Style, mark color.Color, on bool) {
		if y += vertH / 2; y >= len(rows) {
			return
		}

		rows[y].icon, rows[y].st = g.short(), st

		if mark == nil {
			return
		}

		for k, edge := range marks {
			r := y - 1 + k
			if r < 0 || r >= len(rows) || rows[r].mark != nil && !on {
				continue
			}

			rows[r].edge, rows[r].mark = edge, mark
		}
	}

	gearRow := len(rows) - vertH
	for _, t := range m.vertTabs(s) {
		if t.x+vertH > gearRow { // a short panel: the rest would run into the gear
			break
		}

		st, on := dim, open && t.v == active

		switch {
		case on:
			st = fg(pal.headerAccent).Bold(true)
		case hot(t.x):
			st = fg(pal.sashHover)
		}

		if t.badge != "" && !on {
			st = st.Foreground(pal.attention).Bold(true)
		}

		if hot(t.x) {
			m.tip = viewTip(t.v)
		}

		chipAt(t.x, viewIcon(t.v), st, markColor(on, hot(t.x)), on)
	}

	st := dim
	if hot(gearRow) {
		st, m.tip = fg(pal.sashHover), settingsTip
	}

	chipAt(gearRow, icGear, st, markColor(false, hot(gearRow)), false)

	out := make([]string, len(rows))
	base := line("", plain, "", nil)

	for y, c := range rows {
		if c.icon == "" && c.mark == nil {
			out[y] = base
			continue
		}

		out[y] = line(c.icon, c.st, c.edge, c.mark)
	}

	return out
}

// vertTabs are the same chips as tabs, in a block each down the edge: x is
// the block's first row, the icon on the middle one.
func (m *Model) vertTabs(s int) []tab {
	views := m.colViews(s)
	if m.railed(s) {
		views = m.railViews(s)
	}

	var out []tab

	for k, v := range views {
		t := tab{v: v, x: k * vertH, w: actW(), chipW: actW(), label: center(viewIcon(v).short(), actW())}
		if c := m.attentionCount(); v == viewAgents && c > 0 {
			t.badge = "•"
		}

		out = append(out, t)
	}

	return out
}

func (m *Model) viewHeader(s int, v view, w int) string {
	title := []string{strings.ToUpper(filepath.Base(m.ws)), "SOURCE CONTROL", "SPACES", "SEARCH", "TERMINAL", "GITHUB", "DATABASES"}[v]

	var right []seg

	if acts := m.headerActions(s, v, w); len(acts) > 0 {
		for _, a := range acts {
			st := dim
			if a.hot {
				st, m.tip = keycapHot(), a.tip
			}

			right = append(right, sg(a.label, st))
		}
	} else if sum := m.scm.summary(m, max(w-ansi.StringWidth(title)-3, 0)); v == viewGit && sum != "" {
		right = []seg{sg(sum+" ", dim)}
	}

	return row(w, nil, []seg{sg(" "+title, accent)}, right...)
}

// dropBox paints where a dragged tab would land: a frame over the panels a
// new column would cover, labelled and emptied, or a small one over the
// activity bar at the chip's slot, leaving the icons inside it visible.
func (m *Model) dropBox(lines []string, t dropTarget) {
	w, y0, h := t.rc.w, t.y, t.h
	if h == 0 {
		y0, h = 0, len(lines)
	}

	if w < 3 || h < 2 || y0+h > len(lines) {
		return
	}

	st := fg(pal.accent)
	label := ansi.Truncate(t.label, max(w-4, 0), "…")
	badge := renderText(lipgloss.NewStyle().Background(pal.buttonBg).Foreground(pal.buttonFg), " "+label+" ")
	pad := (w - 2 - ansi.StringWidth(badge)) / 2

	bar := renderText(st, "│")
	fill := bar + blank(w-2) + bar

	for y := y0; y < y0+h; y++ {
		var box string

		switch {
		case y == y0:
			box = renderText(st, "┌"+strings.Repeat("─", w-2)+"┐")
		case y == y0+h-1:
			box = renderText(st, "└"+strings.Repeat("─", w-2)+"┘")
		case y == y0+h/2 && (t.h == 0 || t.fill) && label != "":
			box = bar + blank(pad) + badge + blank(w-2-pad-ansi.StringWidth(badge)) + bar
		case t.h == 0 || t.fill:
			box = fill
		default:
			box = bar + cutTail(cutHead(lines[y], t.rc.x+w-1), t.rc.x+1) + "\x1b[m" + ansi.ResetHyperlink() + bar
		}

		// Each cut reads the row once, as far as it needs; a session's link the
		// box cuts through stops at it and goes on past it.
		lines[y] = cutHead(lines[y], t.rc.x) + "\x1b[m" + ansi.ResetHyperlink() + box + "\x1b[m" + cutTail(lines[y], t.rc.x+w)
	}
}

// wcSafeLines is wcSafe line by line, a line the last frame had in its row
// taking the answer it got there: most of a frame's lines are the last one's.
func (m *Model) wcSafeLines(lines []string) []string {
	out := make([]string, len(lines))

	for i, l := range lines {
		if i < len(m.safeIn) && m.safeIn[i] == l {
			out[i] = m.safeOut[i]
			continue
		}

		out[i] = wcSafe(l)
	}

	m.safeIn, m.safeOut = lines, out

	return out
}

// base is the frame under the overlays, drawn once for a drag or a modal
// (baseKey).
type base struct {
	key      baseKey
	lines    []string // the panel rows, the rule, the status bar
	top, bot string   // the frame's rules with borders on
	drop     *dropTarget
	boxed    []string // lines with drop's box over them, drawn once for it
}

// baseKey is what the frame under a drag's overlays hangs on while only its
// motions arrive: the drag, the slot it marks on its strip, the Spaces row
// it would take. Under a modal it is the modal while it takes keys, unless
// a key may change what is under it: a move or change hook (a theme's
// preview), an item run with the modal kept open. Zero for anything else,
// whose every frame draws it anew. Update drops the base on any other
// message.
type baseKey struct {
	d    *drag
	slot int
	to   string
	md   *modal
}

func (m *Model) baseKey() baseKey {
	if md := m.modal; m.drag == nil && md != nil && md.move == nil && md.change == nil && !md.keep {
		return baseKey{md: md}
	}

	d := m.drag
	if d == nil || d.kind != dragStrip && d.kind != dragOpen && d.kind != dragRow {
		return baseKey{}
	}

	return baseKey{d: d, slot: d.slot, to: d.to}
}

// baseFrame draws the columns side by side into the panel rows, then the
// rule and the status bar, and with borders on (b) the frame's top and
// bottom rules.
func (m *Model) baseFrame(cs []rect, c rect, b int) (lines []string, top, bot string) {
	type panel struct {
		rc    rect
		lines []string
		title string
		focus int
		lit   int // the focus that lights its edges, the panel's on a maximized one
	}

	var ps []panel

	left := leftCount(m.cols())
	for i := 0; i <= len(cs); i++ {
		if i == left {
			p := panel{c, m.mainLines(c.w), m.mainTitle(), onMain, onMain}
			if m.termMax && m.termRows() > 0 {
				p.title, p.lit = "", onPanel
			}

			ps = append(ps, p)
		}

		if i < len(cs) && cs[i].w > 0 {
			ps = append(ps, panel{cs[i], m.sidebar(i, cs[i].w), m.sideTitle(i), i, i})
		}
	}

	edge := func(p panel) lipgloss.Style { return m.frameStyle(p.lit) }
	status, _ := m.statusLine(m.w)

	// owner is the column the divider before panel j resizes: the sidebar
	// column beside it.
	owner := func(j int) int {
		if ps[j-1].focus != onMain && ps[j-1].focus < left {
			return ps[j-1].focus
		}

		return ps[j].focus
	}

	// The edges either side of each panel, the same on every row.
	ls, rs := make([]string, len(ps)), make([]string, len(ps))
	for j, p := range ps {
		switch {
		case b == 1: // the divider is the two frame edges either side of it
			ls[j], rs[j] = renderText(edge(p), "│"), renderText(edge(p), "│")

			if j > 0 {
				if st, ok := m.sashStyle(colSash(owner(j))); ok {
					ls[j] = renderText(st, "┃")
				}
			}

			if j+1 < len(ps) {
				if st, ok := m.sashStyle(colSash(owner(j + 1))); ok {
					rs[j] = renderText(st, "┃")
				}
			}

		case j > 0:
			ls[j] = m.divider(owner(j))
		}
	}

	// The edges of the editor area beside a group's frame, unless they light
	// as a sash: main's frame edges, or the dividers before and after it.
	mj := slices.IndexFunc(ps, func(p panel) bool { return p.focus == onMain })
	lit := func(j int) bool { _, ok := m.sashStyle(colSash(owner(j))); return ok }
	hasL := (b == 1 || mj > 0) && (mj == 0 || !lit(mj))
	hasR := (b == 1 || mj+1 < len(ps)) && (mj+1 == len(ps) || !lit(mj+1))
	fs := m.frames()

	// frameEdge is main's edge at column x on row i in the frame of the group
	// beside it, "" for none.
	frameEdge := func(x, i int, has bool) string {
		if !has {
			return ""
		}

		if at := frameAt(fs, x, i); at >= 0 {
			return renderText(fs[at].st, fs[at].glyph(x, i))
		}

		return ""
	}

	// boxEdges are main's edges on row i of a framed bottom panel, none above
	// it: the editor's corners, the panel's, then its sides.
	boxEdges := func(i int) (string, string) {
		if b == 0 || m.termMax || m.termRows() == 0 || i < m.areaH() {
			return "", ""
		}

		l, r, st := "│", "│", m.frameStyle(onPanel)

		switch i - m.areaH() {
		case 0:
			l, r, st = "└", "┘", m.frameStyle(onMain)
		case 1:
			l, r = "┌", "┐"
		}

		return renderText(st, l), renderText(st, r)
	}

	lines = make([]string, m.panelH()+m.ruled()+1) // the panel rows, the rule, then the status bar
	for i := range m.panelH() {
		var sb strings.Builder

		n := 0
		for j, p := range ps {
			n += len(ls[j]) + len(p.lines[i]) + len(rs[j])
		}

		sb.Grow(n)

		el, er := frameEdge(c.x-1, i, hasL), frameEdge(c.x+c.w, i, hasR)
		bl, br := boxEdges(i)

		if el == "" && hasL {
			el = bl
		}

		if er == "" && hasR {
			er = br
		}

		for j, p := range ps {
			l, r := ls[j], rs[j]

			switch {
			case j == mj && el != "":
				l = el
			case j == mj+1 && b == 0 && er != "":
				l = er
			}

			if j == mj && b == 1 && er != "" {
				r = er
			}

			sb.WriteString(l)
			sb.WriteString(p.lines[i])
			sb.WriteString(r)
		}

		lines[i] = sb.String()
	}

	if m.ruled() == 1 {
		lines[len(lines)-2] = m.statusRule(cs, fs)
	}

	lines[len(lines)-1] = status

	if b == 0 {
		return lines, "", ""
	}

	var tb, bb strings.Builder

	for _, p := range ps {
		t := ""
		if p.title != "" {
			t = ansi.Truncate(" "+p.title+" ", max(p.rc.w-2, 0), "…")
		}

		// The title keeps the text's shade, the lines go soft.
		tb.WriteString(renderText(edge(p), "┌─") + renderText(ifElse(m.focus == p.lit, edge(p).Bold(true), dim), t) +
			renderText(edge(p), strings.Repeat("─", max(p.rc.w-1-ansi.StringWidth(t), 0))+"┐"))

		st := edge(p)
		if p.focus == onMain && m.termRows() > 0 { // the panel's frame ends the row
			st = m.frameStyle(onPanel)
		}

		bb.WriteString(frameRun(fs, p.rc.x-1, m.panelH(), 1, "└", st) +
			frameRun(fs, p.rc.x, m.panelH(), p.rc.w, "─", st) +
			frameRun(fs, p.rc.x+p.rc.w, m.panelH(), 1, "┘", st))
	}

	return lines, tb.String(), bb.String()
}

// frameStyle is the edge of the frame focus lights.
func (m *Model) frameStyle(focus int) lipgloss.Style {
	if m.focus == focus {
		return fg(pal.accent)
	}

	return fg(pal.panelBorder)
}

// statusLine is the bottom row, VS Code's status bar: on its
// statusBar.background (`bar_bg`), in its grey, the branch, sync, the
// problems and flash messages on the left; search, the editor's items, the
// project, the palette and the bell for the agents waiting on the right.
func (m *Model) statusLine(w int) (string, []rowAction) {
	var (
		left, right []seg
		zones       []rowAction
	)

	ink := fg(pal.descFg) // VS Code's statusBar.foreground
	// The branch and the project are buttons: lit under the mouse, as VS Code's
	// status bar items are, and named by a tip.
	over := func(x, w int) bool { return m.mouseY == m.h-1+m.bord() && m.mouseX >= x && m.mouseX < x+w }
	button := func(text, tip string, st lipgloss.Style, run func(*Model) tea.Cmd) {
		x, w := 0, ansi.StringWidth(text)
		for _, sg := range left {
			x += ansi.StringWidth(sg.s)
		}

		seg := sg(text, st)
		if over(x, w) {
			seg, m.tip = sgOwn(text, st.Background(pal.keycapBg)), tip // over the bar's own background
		}

		left = append(left, seg)
		zones = append(zones, rowAction{x: x, w: w, run: run})
	}

	// A named instance leads, as VS Code's remote indicator does; its tip says
	// how to open it again.
	if n := m.st.Instance; n != "" {
		text := " " + n + " "
		if over(0, ansi.StringWidth(text)) {
			m.tip = "Instance  pando --instance " + n
		}

		left = append(left, sgOwn(text, lipgloss.NewStyle().Foreground(pal.badgeFg).Background(pal.badgeBg)))
	}

	name := filepath.Base(m.ws)
	if root := m.scm.root(); root != "" {
		st := m.scm.status[root]

		button(" "+icBranch.s()+" "+m.headLabel(root, st)+" ", "Switch Branch…", ink, func(m *Model) tea.Cmd { return m.scm.branchPicker(m) })

		// VS Code's sync item: the counts behind and ahead once there are
		// any, Publish Branch's cloud without an upstream.
		switch {
		case st.Upstream != "" && st.Ahead+st.Behind > 0:
			button(" "+icSync.s()+" "+m.aheadBehind(root, st, "%[2]s↓ %[1]s↑")+" ", "Synchronize Changes", ink, func(m *Model) tea.Cmd { return m.scm.sync(m) })
		case st.Upstream != "":
			button(" "+icSync.s()+" ", "Synchronize Changes", ink, func(m *Model) tea.Cmd { return m.scm.sync(m) })
		case st.Branch != "":
			button(" "+icPublish.s()+" ", "Publish Branch", ink, func(m *Model) tea.Cmd { return m.scm.publish(m) })
		}

		name = filepath.Base(root)
	}

	if m.st.Settings.Diags { // VS Code's problems item, there with none too: a click lists them
		errs, warns := m.diagCounts()
		button(fmt.Sprintf(" %s %d %s %d ", icError.s(), errs, icWarning.s(), warns), "Problems  ^⇧m", ink, (*Model).showProblems)
	}

	if m.msg != "" && time.Since(m.msgAt) < 5*time.Second {
		st := fg(pal.ok)
		if m.msgErr {
			st = fg(pal.errc)
		}

		left = append(left, sg(" ", st))
		if strings.HasPrefix(m.msg, "asking ") { // the answer clears it sooner than a decode settles: noise
			left = append(left, sg(m.msg, st))
		} else {
			left = append(left, m.fx.segs("flash", m.msg, st)...)
		}
	}
	// Each right-hand item carries the action its click runs and its tip; they
	// are laid out from the edge inwards once the list is complete.
	var (
		runs []func(m *Model) tea.Cmd
		tips []string
	)

	// An item is one or more segments: a shimmering label is several.
	var items [][]seg

	blameAt := -1 // the blame's item, the first to go when the bar is full

	addSegs := func(tip string, run func(m *Model) tea.Cmd, ss ...seg) {
		items, runs, tips = append(items, ss), append(runs, run), append(tips, tip)
	}
	add := func(text string, st lipgloss.Style, tip string, run func(m *Model) tea.Cmd) {
		addSegs(tip, run, sg(text, st))
	}

	searchRun := func(m *Model) tea.Cmd { return tea.Batch(m.showView(viewSearch), m.sr.focus()) }

	switch n := m.sr.matches(); {
	case m.sr.busy:
		addSegs("Show Search  ^⇧f", searchRun, m.shimmer(" searching… ", dim, bold)...)
	case n > 0:
		add(" "+m.fx.text("results", strconv.Itoa(n))+strings.TrimPrefix(plural(n, "result"), strconv.Itoa(n))+" ", ink, "Show Search  ^⇧f", searchRun)
	}

	if f, ok := m.diagFailed[m.pv.path]; ok && m.showsPreview() && m.pv.kind == pvFile { // no problems shown is not none
		add(" ⚠ no diagnostics ", fg(pal.warn), f.server+": "+firstLine(f.err.Error()), nil)
	}

	if m.showsPreview() && m.pv.tbl != nil && m.pv.tbl.column() != "" { // the cursor's column, where a file's cursor is
		add(" "+m.pv.tbl.column()+" ", ink, "", nil)
	}

	if text, tip, run, ok := m.blameItem(); ok {
		blameAt = len(items)

		add(text, ink, tip, run)
	}

	if m.showsPreview() && m.pv.ready && m.pv.md != 1 && !m.pv.side(m) && m.pv.kind != pvTable && m.pv.kind != pvSettings { // a side-by-side diff keeps its cursor line
		// VS Code's editor items: the cursor, going to a line; a file's
		// indentation, changing it. A rendering has no cursor to show, nor a
		// tab still loading.
		if pos, ln := m.cursorPos(); ln > 0 { // a hunk gap or a commit message has no line of the file
			add(pos, ink, "Go to Line…  ^g", func(m *Model) tea.Cmd { return m.gotoLineQuery(":") })
		}

		if m.pv.kind == pvFile || m.pv.kind == pvRev {
			add(" "+m.indentLabel()+" ", ink, "Change Indentation…", func(m *Model) tea.Cmd { return m.indentPicker() })
		}
	}

	add(" "+name+" ", ink, "Open Project…", func(m *Model) tea.Cmd { return m.projectPicker() })
	add(" ^⇧p ", ink, "Show All Commands", func(m *Model) tea.Cmd { return m.commandPalette() })

	if chip, run := m.updateChip(); chip != nil {
		addSegs(map[string]string{"available": "Install pando v" + m.upd.Latest, "ready": "Restart to Update"}[m.upd.State], run, chip...)
	}

	if chip, run := m.fontChip(); chip != nil {
		addSegs("Install Symbols Nerd Font Mono", run, chip...)

		if run != nil { // not while it installs
			add(" × ", ink, "Don't Offer the Nerd Font Again", (*Model).dismissFont)
		}
	}

	// VS Code's bell at the right end: the agents waiting on you, its dot and
	// count in the attention color while any is.
	navigate := func(m *Model) tea.Cmd { return m.agentNavigator() }
	if n := m.attentionCount(); n > 0 {
		add(" "+icBellDot.s()+" "+m.fx.text("attention", strconv.Itoa(n))+" ", fg(pal.attention), "Go to Agent or Worktree…  alt+t", navigate)
	} else {
		add(" "+icBell.s()+" ", ink, "Go to Agent or Worktree…  alt+t", navigate)
	}

	if used := segsWidth(left) + segsWidth(slices.Concat(items...)); blameAt >= 0 && used > w {
		items, runs, tips = slices.Delete(items, blameAt, blameAt+1), slices.Delete(runs, blameAt, blameAt+1), slices.Delete(tips, blameAt, blameAt+1)
	}

	x, hot := w, -1

	for i := len(items) - 1; i >= 0; i-- {
		rw := 0
		for _, s := range items[i] {
			rw += ansi.StringWidth(s.s)
		}

		x -= rw
		if runs[i] != nil {
			zones = append(zones, rowAction{x: x, w: rw, run: runs[i]})

			if over(x, rw) {
				hot, m.tip = i, tips[i]
			}
		}
	}

	for i, it := range items {
		for _, s := range it {
			if i == hot { // lit as the branch and the project are
				s = sgOwn(s.s, s.st.Background(pal.keycapBg))
			}

			right = append(right, s)
		}
	}

	return row(w, statusBg(), left, right...), zones
}

// statusRule is the row over the status bar, VS Code's statusBar.border: a
// thin underline in panel_border, right on top of the bar, on the activity
// bar's tint under a side strip, its inner border too, so the strip runs
// down to it.
func (m *Model) statusRule(cs []rect, fs []groupFrame) string {
	tint := make([]bool, m.w)
	edge := map[int]string{}

	for i, rc := range cs {
		bw := ifElse(m.railed(i), rc.w, m.barW(i))
		if rc.w == 0 || bw == 0 {
			continue
		}

		x0 := ifElse(m.side(i) == 1, rc.x+rc.w-bw, rc.x)
		for x := max(x0, 0); x < min(x0+bw, m.w); x++ {
			tint[x] = true
		}

		if !m.railed(i) {
			edge[ifElse(m.side(i) == 1, x0, x0+bw-1)] = ifElse(m.side(i) == 1, "▏", "▕")
		}
	}

	var b strings.Builder

	y := m.panelH()
	for x := 0; x < m.w; {
		if frameAt(fs, x, y) >= 0 { // the bottom of a group's frame
			j := x + 1
			for j < m.w && frameAt(fs, j, y) >= 0 {
				j++
			}

			b.WriteString(frameRun(fs, x, y, j-x, "━", fg(pal.panelBorder)))
			x = j

			continue
		}

		j, text := x+1, edge[x]
		for text == "" && j < m.w && tint[j] == tint[x] && edge[j] == "" && frameAt(fs, j, y) < 0 {
			j++
		}

		st := lipgloss.NewStyle().Underline(true).UnderlineColor(pal.panelBorder) // thinner than a ▁
		if text != "" {
			st = st.Foreground(pal.rulerBorder)
		} else {
			text = strings.Repeat(" ", j-x)
		}

		if tint[x] && pal.barBg != nil {
			st = st.Background(pal.barBg)
		}

		// Render underlines a cell at a time: style the run once.
		pre, suf, _ := strings.Cut(st.Render("\x00"), "\x00")
		b.WriteString(pre + text + suf)

		x = j
	}

	return b.String()
}

// statusBg is the status bar's background: bar_bg, section_bg for a theme
// without one.
func statusBg() color.Color { return cmp.Or[color.Color](pal.barBg, pal.sectionBg) }

// projectPicker is the status bar's project switcher: every project, the one
// on screen marked, choosing one opens its main worktree; the last row adds
// another, and stays out of the way of a typed name.
func (m *Model) projectPicker() tea.Cmd {
	var items []item

	current := m.ws
	if w := m.workspace(m.ws); w != nil {
		current = w.Project
	}

	for _, p := range m.st.Projects {
		path, n := p, 0
		for _, w := range m.wss {
			if w.Project == p && w.Main {
				path = w.Path
			}
		}

		for _, s := range m.mainSessions() {
			if w := m.workspace(s.Workspace); w != nil && w.Project == p {
				n++
			}
		}

		hint := plural(n, "session")
		if p == current {
			hint = "open · " + hint
		}

		items = append(items, item{label: filepath.Base(p), hint: hint, search: p, run: func(m *Model) tea.Cmd {
			switched := m.switchWorkspace(path)
			if m.sess != "" { // a session of that project is on screen now: talk to it
				m.focus = onMain
			}

			return tea.Batch(switched, m.refreshGit(), m.fetchScreen())
		}})
	}

	items = append(items, item{label: "+ Add Project…", search: "add new project", run: func(m *Model) tea.Cmd { return m.addProjectPrompt() }})
	m.modal = newPicker("Open Project", items)

	return nil
}

// statusMouse runs the status bar action under x.
func (m *Model) statusMouse(x int) tea.Cmd {
	_, zones := m.statusLine(m.w)
	if z, ok := hit(zones, x); ok {
		return z.run(m)
	}

	return nil
}

func (m *Model) attentionCount() int {
	n := 0

	for _, s := range m.sessions {
		if s.Attention && s.ID != m.sess {
			n++
		}
	}

	return n
}

// cursorPos is the status bar's cursor item, Ln and Col, the selection's
// size too, and the file's line it names: a diff's, not the row's; a split
// diff's cursor is a whole line.
func (m *Model) cursorPos() (pos string, ln int) {
	c := m.pv.at()
	ln = m.pv.lineNo(c.line)

	pos = fmt.Sprintf(" Ln %d, Col %d ", ln, c.col+1)
	if m.pv.split(m) {
		pos = fmt.Sprintf(" Ln %d ", ln)
	}

	n := m.pv.selCount()
	if m.pv.split(m) {
		n = utf8.RuneCountInString(m.pv.halfText())
	}

	if n > 0 {
		pos = fmt.Sprintf(" Ln %d, Col %d (%d selected) ", ln, c.col+1, n)
	}

	return pos, ln
}

// pvW is the editor's text width: the main area less the scrollbar's column.
func (m *Model) pvW() int { return max(m.mainW()-1, 1) }

// pvH is the preview body height: the main area less the editor strip, the
// header, the footer and the horizontal scrollbar's row.
func (m *Model) pvH() int {
	return max(m.mainH()-1-m.headH()-m.stripH()-m.hbarH(), 1)
}

// hbarH is the row the editor's horizontal scrollbar takes under the text:
// 1 while a line runs past the right edge.
func (m *Model) hbarH() int { return b2i(m.pv.hbar(m, m.pvW()).on()) }

// stripH is the rows of tab strip over the main area: a session always has
// one (it carries the + that opens another), and so do any open editor and
// the empty main area, with its rule under it.
func (m *Model) stripH() int {
	if m.showsSession() {
		return sessStripH
	}

	return 2 * b2i(m.pv.kind == "" || len(m.editors) > 0)
}

// headH is the rows of header under the tab strip: none for a session, whose
// name is its tab's, nor for a file with breadcrumbs off, whose buttons are in
// the strip, nor for the Settings, named by their tab; else 1.
func (m *Model) headH() int {
	if m.showsSession() || m.stripH() > 0 && (m.pv.pathHeader() && !m.st.Settings.Crumbs || m.pv.kind == pvSettings) { // the tab names Settings
		return 0
	}

	return 1
}

// buttonsUp reports the preview's buttons in the tab strip's row, as VS Code
// puts an editor's actions; without a strip they stay in the header.
func (m *Model) buttonsUp() bool { return !m.showsSession() && m.stripH() > 0 && m.pv.kind != "" }

// stripBtnW is the width the buttons take at the right end of the strip.
func (m *Model) stripBtnW() int {
	if !m.buttonsUp() {
		return 0
	}

	n := 1

	for _, a := range m.pv.buttons(m, m.mainW()) {
		n += a.w
	}

	return n
}

// sessH is the terminal body: the main area minus its strip and rule.
func (m *Model) sessH() int { return max(m.mainH()-sessStripH, 1) }

func (m *Model) mainLines(w int) []string {
	if n := m.termRows(); n > 0 { // the editor area, then the panel under it
		out := m.areaLines(w)[:m.areaH()]
		return append(out, m.termPanelLines(w, n)...)
	}

	return m.areaLines(w)
}

func (m *Model) editorLines(w int) []string {
	h := m.mainH() - 1

	var (
		header, footer string
		body           []string
	)

	switch {
	case m.showsSession():
		h = m.sessH()
		body = m.term.view(m, w, h)

	case m.pv.kind != "":
		h = m.pvH()

		header, body, footer = m.pv.view(m, w, h)
		if m.peekH() > 0 {
			body = append(m.withPeek(body, w, h), body[min(h, len(body)):]...)
		}

		h += m.hbarH() // the horizontal bar's row, which view puts under the text

	default:
		ws := m.ws // cut from its start, as the breadcrumbs are: the end names the worktree
		if over := ansi.StringWidth(ws) - (w - 8); over > 0 {
			ws = "…" + ansi.TruncateLeft(ws, over+1, "")
		}

		header = row(w, nil, []seg{sg(" pando", accent), sg("  "+ws, dim)})
		h -= m.stripH()
		body = welcome(w, h, m.sess != "", len(m.editors) > 0, m.focus == onMain)
	}

	out := make([]string, 0, m.h)
	if m.stripH() > 0 { // the files' and the sessions' tabs, one strip
		out = append(out, m.editorStrip(w), m.editorRule(w))
	}

	if m.headH() > 0 {
		out = append(out, header)
	}

	for i := range max(h, 0) {
		if i < len(body) {
			out = append(out, m.fit(body[i], w, ""))
		} else {
			out = append(out, blank(w))
		}
	}

	if footer != "" {
		out = append(out, footer)
	}

	for len(out) < m.mainH() {
		out = append(out, blank(w))
	}

	return out[:m.mainH()]
}

// welcome fills the empty main area. With a session behind its tab the first
// line names what is missing here — an editor — rather than claiming the
// workspace has no session; with files behind their tabs it points at those.
// The keys are split by where they work: Explorer and the other lists take n,
// w and a for themselves, so focused says whether the second group works yet.
func welcome(w, h int, sess, files, focused bool) []string {
	first := "No session in this workspace."
	if sess {
		first = "No editor open."
	}

	if files {
		first = "Pick a tab above, or:"
	}

	text := []string{
		first,
		"",
		"Anywhere:",
		"alt+s  new session",
		"alt+n  new tab, as the + above",
		"^p     quick open",
		"^]     next panel",
		"",
		ifElse(focused, "In this area:", "In this area, click it or ^] first:"),
		"n      new session",
		"w      new worktree",
		"a      add project",
		"3      Spaces panel",
		"^n     new file, or double click",
	}
	out := make([]string, max(h, 0))

	wide := 0
	for _, t := range text {
		wide = max(wide, lipgloss.Width(t))
	}

	top, left := max((h-len(text))/2, 0), blank((w-wide)/2)
	for i, t := range text {
		if top+i < h {
			out[top+i] = left + renderText(dim, t)
		}
	}

	return out
}

var hotkeys = map[view][][2]string{
	viewFiles:  {{"↑↓", "move"}, {"←→", "fold"}, {"⏎", "open"}, {"n N", "new"}, {"R", "rename"}, {"D", "delete"}, {"s", "stage"}, {"e", "edit"}, {"^x ^c ^v", "cut/copy/paste"}, {".", "dotfiles"}, {"^f", "filter"}},
	viewGit:    {{"↑↓", "move"}, {"⏎", "stage"}, {"o", "diff"}, {"t", "tree"}, {"c", "message"}, {"C", "commit"}, {"A", "suggest"}, {"S", "sync/publish"}, {"a u", "stage/unstage all"}, {"U", "stage untracked"}, {"O", "open file"}, {"B", "branch"}, {"d", "discard"}, {"^f", "filter"}},
	viewSearch: {{"^f /", "query"}, {"^h", "replace"}, {"r R", "replace file/all"}, {"⏎", "open"}, {"↑↓", "move"}, {"←→", "fold"}, {"alt+c", "case"}, {"alt+w", "word"}, {"alt+r", "regex"}, {"^r", "rerun"}, {"x", "clear"}, {"C", "collapse"}},
	viewAgents: {{"↑↓", "move"}, {"alt+↑↓", "reorder"}, {"⏎", "switch"}, {"n", "session"}, {"w", "worktree"}, {"a", "project"}, {"x", "kill"}, {"z", "sleep"}, {"o", "view options"}, {"^f", "filter"}},
	viewGitHub: {{"↑↓", "move"}, {"←→", "fold"}, {"⏎", "open"}, {"d", "changes"}, {"o", "on github.com"}, {"y", "copy link"}, {"w", "worktree"}, {"x", "done"}, {"^r", "refresh"}, {"C", "collapse"}, {"^f", "filter"}},
	viewDB:     {{"↑↓", "move"}, {"←→", "fold"}, {"⏎", "open"}, {"n", "new connection"}, {"e D", "edit/remove"}, {"c y", "copy name/path"}, {"r", "refresh"}, {"C", "collapse"}, {"^f", "filter"}},
}

var globalKeys = [][2]string{{"^]", "focus"}, {"^0 ^1", "side/editor"}, {"1-4 6 7", "views"}, {"^p", "open file"}, {"^⇧p", "commands"}, {"alt+t", "agents"}, {"alt+a", "next alarm"}, {"alt+n", "new tab"}, {"alt+s", "new session"}, {"^tab", "editors"}, {"^⇧pgup ^⇧pgdn", "move tab"}, {"^f", "find"}, {"^⇧m", "problems"}, {"^⇧u", "output"}, {"alt+, alt+.", "back/forward"}, {"^`", "terminal"}, {"^b", "hide"}, {"< >", "width"}, {"^,", "settings"}, {"F10", "manage"}, {"esc q", "quit"}}

var previewKeys = [][2]string{{"↑↓", "move"}, {"⇧↑↓", "select"}, {"^a", "all"}, {"y", "copy"}, {"s", "split diff"}, {"m", "stage/revert lines"}, {"] [", "next, prev change"}, {"} {", "next, prev file"}, {"F8 ⇧F8", "next, prev problem"}, {"^⇧[ ^⇧]", "fold, unfold"}, {"w", "wrap"}, {"^f", "find"}, {"^h", "replace"}, {"^g", "go to line"}, {"f12", "definition"}, {"⇧f12", "references"}, {"^k", "hover"}, {"^.", "code action"}, {"e", "edit"}, {"q", "close"}}

// keyRows lays key/description pairs out in two keycap columns.
func keyRows(pairs [][2]string) []item {
	kw, dw := 5, 10 // the widest key and the widest left-column description
	for i, p := range pairs {
		kw = max(kw, ansi.StringWidth(p[0]))
		if i%2 == 0 {
			dw = max(dw, ansi.StringWidth(p[1]))
		}
	}

	cell := func(p [2]string, dw int) string {
		return keycap(p[0]+blank(kw-ansi.StringWidth(p[0]))) + " " + renderText(dim, p[1]+blank(dw-ansi.StringWidth(p[1])))
	}

	var out []item

	for i := 0; i < len(pairs); i += 2 {
		label := cell(pairs[i], dw)
		if i+1 < len(pairs) {
			label += " " + cell(pairs[i+1], 0)
		}

		out = append(out, item{label: label, styled: true})
	}

	return out
}

func heading(s string) item { return item{label: renderText(bold, s), styled: true} }

// viewItems are the commands that belong to no view.
func (m *Model) viewItems() []item {
	show := func(v view) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			if v == viewSearch {
				return tea.Batch(m.showView(v), m.sr.focus())
			}

			return m.showView(v)
		}
	}

	items := []item{
		{label: "New Untitled File", hint: "^n", run: func(m *Model) tea.Cmd { return m.newUntitled() }},
		{label: "Quick Open…", hint: "^p", run: func(m *Model) tea.Cmd { return m.loadIndex(true) }},
		{label: "Open in a New Tab…", hint: "alt+n", run: (*Model).newTabMenu},
		{label: "Show Problems", hint: "^⇧m", run: (*Model).showProblems},
		{label: "Show Output", hint: "^⇧u", run: func(m *Model) tea.Cmd { return m.showPanel(ptOutput) }},
		{label: "Show Explorer", hint: showKeys[viewFiles], run: show(viewFiles)},
		{label: "Show Source Control", hint: showKeys[viewGit], run: show(viewGit)},
		{label: "Show Spaces", hint: showKeys[viewAgents], run: show(viewAgents)},
		{label: "Show Search", hint: showKeys[viewSearch], run: show(viewSearch)},
		{label: "Toggle Terminal", hint: "^`", run: func(m *Model) tea.Cmd { return m.toggleTerminal() }},
	}
	if m.hasGH {
		items = append(items, item{label: "Show GitHub", hint: "6", run: show(viewGitHub)})
	}

	if m.hasDB {
		items = append(items, item{label: "Show Databases", hint: "7", run: show(viewDB)})
	}

	for _, it := range m.terminalItems() {
		if it.label != "Close Terminal" {
			items = append(items, it)
		}
	}

	if m.sess != "" {
		items = append(items, item{label: "Close Session View", run: func(m *Model) tea.Cmd { return m.hideSession() }})
	}
	// VS Code's editor groups: a tab moves, a group splits off.
	items = append(items,
		item{label: "Split Editor Right", hint: `^\`, run: func(m *Model) tea.Cmd { return m.splitEditor(sideRight) }},
		item{label: "Split Editor Down", run: func(m *Model) tea.Cmd { return m.splitEditor(sideDown) }},
		item{label: "Move Editor into Left Group", run: func(m *Model) tea.Cmd { return m.moveEditor(sideLeft) }},
		item{label: "Move Editor into Right Group", run: func(m *Model) tea.Cmd { return m.moveEditor(sideRight) }},
		item{label: "Move Editor into Group Above", run: func(m *Model) tea.Cmd { return m.moveEditor(sideUp) }},
		item{label: "Move Editor into Group Below", run: func(m *Model) tea.Cmd { return m.moveEditor(sideDown) }})

	if m.grid.g == nil {
		items = append(items, item{label: "Toggle Maximized Editor Group", run: func(m *Model) tea.Cmd { return m.toggleMaxGroup() }})
	}

	items = append(items, []item{
		{label: "Focus Next Panel", hint: "^]", run: func(m *Model) tea.Cmd { m.cycleFocus(); return m.fetchScreen() }},
		{label: "Focus Sidebar", hint: "^0", run: func(m *Model) tea.Cmd { return m.focusSidebar() }},
		{label: "Toggle Sidebars", hint: "^b", run: func(m *Model) tea.Cmd { return m.toggleSidebars() }},
		{label: "Open Settings", hint: "^,", run: func(m *Model) tea.Cmd { return m.openSettings() }},
		{label: "Keyboard Shortcuts", hint: "?", run: func(m *Model) tea.Cmd { m.modal = helpModal(m.hasGH, m.hasDB); return nil }},
		{label: "Quit", hint: "q", run: func(m *Model) tea.Cmd { return m.confirmQuit() }},
	}...)
	if m.pv.kind != "" || m.sess != "" {
		items = append(items, item{label: "Focus Editor", hint: "^1", run: func(m *Model) tea.Cmd { m.focus = onMain; return m.fetchScreen() }})
	}

	if m.pv.kind != "" {
		items = append(items,
			item{label: "Go Back", hint: "alt+,", run: func(m *Model) tea.Cmd { return m.navGo(-1) }},
			item{label: "Go Forward", hint: "alt+.", run: func(m *Model) tea.Cmd { return m.navGo(1) }},
			item{label: "Next Editor", hint: "^tab", run: func(m *Model) tea.Cmd { return m.cycleEditor(1) }},
			item{label: "Previous Editor", hint: "^⇧tab", run: func(m *Model) tea.Cmd { return m.cycleEditor(-1) }},
			item{label: "Close Editor", hint: "^w", run: func(m *Model) tea.Cmd { return m.closeEditor(m.edIdx) }},
			item{label: "Close All Editors", run: func(m *Model) tea.Cmd { return m.closeEditors() }})
	}

	if len(m.editors) > 1 && m.pv.kind != "" {
		items = append(items, item{label: "Close Other Editors", run: func(m *Model) tea.Cmd { return m.closeOtherEditors() }})
	}

	if s := m.focusedStrip(); len(m.stripOrder(s)) > 1 && m.stripActive(s) != "" {
		items = append(items, item{label: "Move Tab Left", hint: "^⇧pgup", run: func(m *Model) tea.Cmd { return m.moveTab(-1) }},
			item{label: "Move Tab Right", hint: "^⇧pgdn", run: func(m *Model) tea.Cmd { return m.moveTab(1) }})
	}

	return items
}

// commands are every command available now, named "Category: Command"; the
// palette, the context menus and the [keys] config all read this one list.
func (m *Model) commands() []item {
	var items []item

	for _, c := range m.categories() {
		for _, it := range c.items {
			if it.run == nil || it.styled {
				continue
			}

			label, run := c.name+": "+it.label, it.run
			it.label, it.detail = label, ""
			it.run = func(m *Model) tea.Cmd { m.lastCommand = label; return run(m) }
			items = append(items, it)
		}
	}

	return items
}

// category is one of the palette's groups of commands.
type category struct {
	name  string
	items []item
}

// categories are the palette's commands by group, in its order.
func (m *Model) categories() []category {
	cs := []category{
		{"View", m.viewItems()},
		{"Explorer", m.ex.items(m)},
		{"Git", m.scm.items(m)},
		{"Agents", m.ag.items(m)},
		{"Search", m.sr.items(m)},
	}

	if m.hasGH {
		cs = append(cs, category{"GitHub", m.gh.items(m)})
	}

	if m.hasDB {
		cs = append(cs, category{"Databases", m.db.items(m)})
	}

	if m.showsPreview() {
		cs = append(cs, category{"Editor", append(m.pv.items(m), m.pv.keyItems(m)...)})
	}

	return append(cs, category{"Preferences", settingsItems(m)}, category{"Pando", m.updateItems()})
}

// manageMenu is the gear's menu, F10's too, at x, y: VS Code's Manage, then
// each of the palette's categories, a menu of its own in the same place.
func (m *Model) manageMenu(x, y int) tea.Cmd {
	items := []item{
		{label: "Command Palette", hint: "^⇧p", run: func(m *Model) tea.Cmd { return m.commandPalette() }},
		{label: "Settings", hint: "^,", run: func(m *Model) tea.Cmd { return m.openSettings() }},
		{label: "Keyboard Shortcuts", hint: "?", run: func(m *Model) tea.Cmd { m.modal = helpModal(m.hasGH, m.hasDB); return nil }},
		{sep: true},
	}

	for _, c := range m.categories() {
		if !slices.ContainsFunc(c.items, func(it item) bool { return it.run != nil }) {
			continue
		}

		name := c.name
		items = append(items, item{label: name, hint: "›", run: func(m *Model) tea.Cmd { return m.manageSub(name, x, y) }})
	}

	return m.menuOf(items, x, y)
}

// manageSub is category name's menu in the Manage menu's place: its first
// row, ← too, goes back there. A setting that is on has its check, and the
// Preferences change in place, the menu staying open on them.
func (m *Model) manageSub(name string, x, y int) tea.Cmd {
	back := func(m *Model) tea.Cmd {
		cmd := m.manageMenu(x, y)
		m.modal.focus(slices.IndexFunc(m.modal.disp, func(it item) bool { return it.label == name }))

		return cmd
	}

	build := func(m *Model) []item {
		var sub []item

		for _, c := range m.categories() {
			if c.name == name {
				sub = c.items
			}
		}

		for i, it := range sub {
			switch it.hint {
			case "on":
				sub[i].hint = "✓"
			case "off":
				sub[i].hint = ""
			}
		}

		return append([]item{{label: "‹ " + name, hint: "←", run: back}, {sep: true}}, sub...)
	}

	m.menuOf(build(m), x, y)
	m.modal.back = back
	m.modal.keep, m.modal.build = name == "Preferences", build
	m.modal.focus(min(2, len(m.modal.disp)-1)) // the first of its own, past the way back

	return nil
}

// overlayBox draws a box over the screen's lines at x, y.
func (m *Model) overlayBox(lines, box []string, x, y int) {
	for i, bl := range box {
		if y+i >= len(lines) {
			continue
		}

		bl = ansi.Truncate(bl, max(m.w-x, 0), "") // a box wider than the room left is cut at the edge
		cut := m.cutAround(lines[y+i], x, strWidth(bl))
		lines[y+i] = cut[0] + "\x1b[m" + ansi.ResetHyperlink() + bl + "\x1b[m" + cut[1] // a link the box covers stops at it
	}
}

// cutKey is what cutAround cuts: row ln around a box bw cells wide at x, on
// a screen w cells wide.
type cutKey struct {
	ln       string
	x, bw, w int
}

// cutAround is what of row ln shows either side of a box bw cells wide at
// x, remembered for the frame and the next: an overlay is mostly drawn over
// the rows it covered last.
func (m *Model) cutAround(ln string, x, bw int) [2]string {
	return m.cuts.get(cutKey{ln, x, bw, m.w}, func() [2]string {
		// A wide rune the box's edge cuts in half: the head drops it on the
		// left and the tail keeps it whole on the right, so its cell goes
		// blank. Each reads the row once, as far as it needs.
		left := cutHead(ln, x)
		left += strings.Repeat(" ", max(x-strWidth(left), 0))

		right := cutTail(ln, x+bw)
		if strWidth(right) > m.w-x-bw {
			right = " " + cutTail(ln, x+bw+1)
		}

		return [2]string{left, right}
	})
}

// commandID is a command's name in config.toml: "Git: Switch Branch…" is git.switchBranch.
func commandID(label string) string {
	cat, name, ok := strings.Cut(label, ": ")
	if !ok {
		return ""
	}

	var id strings.Builder
	id.WriteString(strings.ToLower(cat))

	for i, w := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		w = strings.ToLower(w)
		if i > 0 {
			w = strings.ToUpper(w[:1]) + w[1:]
		} else {
			id.WriteString(".")
		}

		id.WriteString(w)
	}

	return id.String()
}

// runKey runs what the config binds to a key; ok is false when it binds none.
func (m *Model) runKey(key string) (tea.Cmd, bool) {
	id, ok := m.st.Settings.Keys[key]
	if !ok {
		return nil, false
	}

	if id == "" {
		return nil, true // unbound on purpose
	}

	for _, it := range m.commands() {
		if commandID(it.label) == id {
			return it.run(m), true
		}
	}

	return flash("no command "+id, true), true
}

// typing reports a focused text input, where keys are text, not commands.
func (m *Model) typing() bool {
	return m.scm.input.Focused() || m.sr.editing() || m.pv.find.editing || m.pv.composing && m.focus == onMain || m.pv.tableBox() && m.focus == onMain ||
		slices.ContainsFunc(m.filters[:], func(f filter) bool { return f.editing })
}

// CommandIDs lists every command with the name config.toml binds keys to.
func CommandIDs(w io.Writer, cwd string) {
	m := New(proto.State{}, nil, nil, cwd, nil)
	m.w, m.termH = 100, 30
	m.resize()
	// A file stands open so the editor and language server commands are listed.
	m.pv = preview{
		kind: pvFile, path: filepath.Join(cwd, "file.go"), ready: true,
		plain: [][]rune{[]rune("x")}, buf: newBuffer("x\n", time.Time{}),
	}
	m.preview = true
	_, _ = fmt.Fprintln(w, "\ncommands for [keys] in config.toml:")
	seen := map[string]bool{}

	for _, path := range []string{"file.go", "file.md"} { // a language server and Markdown each add their own
		m.pv.path = filepath.Join(cwd, path)
		for _, it := range m.commands() {
			if id := commandID(it.label); !seen[id] {
				seen[id] = true
				_, _ = fmt.Fprintf(w, "  %-26s %-34s %s\n", id, it.label, it.hint)
			}
		}
	}
}

// confirmQuit is the popup esc and q go through, so neither closes pando by
// accident. ⌃c quits at once, unless table edits, which no draft keeps,
// would go with it.
func (m *Model) confirmQuit() tea.Cmd {
	title, kept := "Close pando?", "unsaved text is kept"
	if n := m.dirtyEditors(); n > 0 { // unsaved text would go with it
		title = fmt.Sprintf("Close pando? %s unsaved", plural(n, "file"))
	}

	if n := m.tableEdits(); n > 0 { // and table edits are lost: no draft keeps them
		title, kept = fmt.Sprintf("%s · %s not submitted", title, plural(n, "table change")), ifElse(m.dirtyEditors() > 0, kept+", table changes are lost", "table changes are lost")
	}

	m.modal = newDialog(title,
		item{label: "Close", hint: kept, run: func(m *Model) tea.Cmd {
			return tea.Sequence(m.saveEditors(), m.saveTerm(), m.saveDrafts(), tea.Quit)
		}},
		item{label: "Save all and close", hint: ifElse(m.tableEdits() > 0, "files saved, table changes lost: submit them first", ""), run: func(m *Model) tea.Cmd { // a submit is no save: it can fail
			m.saveAll()
			return tea.Sequence(m.saveEditors(), m.saveTerm(), m.saveDrafts(), tea.Quit)
		}},
		cancelItem())

	if m.tableEdits() > 0 { // a hasty ⏎ keeps them: Cancel has the keyboard
		m.modal.l.sel = len(m.modal.items) - 1
	}

	return nil
}

func (m *Model) commandPalette() tea.Cmd {
	items := m.commands()
	if i := slices.IndexFunc(items, func(it item) bool { return it.label == m.lastCommand }); i > 0 {
		items = append([]item{items[i]}, slices.Delete(slices.Clone(items), i, i+1)...)
	}

	m.modal = newPicker("Command Palette", items)

	return nil
}

// toggleDiffView switches diffs between inline and side by side.
func (m *Model) toggleDiffView() tea.Cmd {
	next := "split"
	if m.st.Settings.DiffView == "split" {
		next = "inline"
	}

	off := m.pv.row(m) - m.pv.top // the cursor's line keeps its place on screen
	cmd := m.setSettings(map[string]any{"diff_view": next})
	m.pv.scrollTo(m, off)

	return cmd
}

func staleModal(sessions int) *modal {
	return newDialog("The pando daemon runs an older build",
		item{label: "Restart it now", hint: fmt.Sprintf("%d sessions restart: their programs are stopped and resumed, scrollback and work in flight are lost", sessions), run: func(*Model) tea.Cmd {
			return func() tea.Msg {
				if err := proto.RestartDaemon(); err != nil {
					return flashMsg{"restart: " + err.Error(), true}
				}

				return flashMsg{"daemon restarted", false}
			}
		}},
		item{label: "Keep it", hint: "layout and tree settings will not be saved", run: func(*Model) tea.Cmd {
			return flash("old daemon kept · pando stop restarts it", true)
		}})
}

// helpModal is the keys of every view; the GitHub view's where gh is
// installed, the Databases view's where a database client is.
func helpModal(gh, dbs bool) *modal {
	items := []item{heading("Everywhere")}

	views := []view{viewFiles, viewGit, viewSearch, viewAgents}
	if gh {
		views = append(views, viewGitHub)
	}

	if dbs {
		views = append(views, viewDB)
	}

	items = append(items, keyRows(globalKeys)...)
	for _, v := range views {
		items = append(items, item{}, heading(viewTitles[v]))
		items = append(items, keyRows(hotkeys[v])...)
	}

	items = append(items, item{}, heading("Preview"))
	items = append(items, keyRows(previewKeys)...)

	if dbs {
		items = append(items, item{}, heading("Table"))
		items = append(items, keyRows(tableKeys)...)
	}

	items = append(items, item{}, item{label: dim.Render("drag a tab or a title to either side to dock it · right click it for more · drag │ to resize"), styled: true})

	return newMenu("Keys", -1, 0, items...)
}

// indentation is how the active editor indents.
func (m *Model) indentation() (spaces bool, size int) { return m.indentOf(&m.pv) }

// indentOf is how editor p indents: a file's own, what its .editorconfig or
// its lines say, over insert_spaces and tab_size.
func (m *Model) indentOf(p *preview) (spaces bool, size int) {
	spaces, size = m.st.Settings.Spaces, tabSize(m.st.Settings.TabSize)
	if p.kind != pvFile {
		return spaces, size
	}

	if p.ind.style != "" {
		spaces = p.ind.style == "space"
	}

	if p.ind.size > 0 {
		size = tabSize(p.ind.size)
	}

	return spaces, size
}

// tabWidth is the cells a tab takes in the active editor.
func (m *Model) tabWidth() int {
	_, size := m.indentation()
	return size
}

// indentLabel is the status bar's indentation item, VS Code's: the width a
// tab takes, or the spaces an indent is.
func (m *Model) indentLabel() string {
	spaces, size := m.indentation()
	if spaces {
		return fmt.Sprintf("Spaces: %d", size)
	}

	return fmt.Sprintf("Tab Size: %d", size)
}

// indentPicker is the indentation dialog: spaces or tabs, how wide. A file
// takes it as its own, as VS Code's model does, and Auto gives it its own
// back (its .editorconfig, else its lines); a revision has none, so it sets
// the settings.
func (m *Model) indentPicker() tea.Cmd {
	spaces, size := m.indentation()
	f := &indentForm{title: "Default Indentation", spaces: spaces, width: min(size, len(indentSizes)), at: indentSize}

	f.apply = func(m *Model, spaces bool, n int) tea.Cmd {
		return m.setSettings(map[string]any{"tab_size": n, "insert_spaces": spaces})
	}

	if m.pv.kind == pvFile {
		f.title = "Editor Indentation"
		f.apply = func(m *Model, spaces bool, n int) tea.Cmd {
			m.pv.ind = indent{style: ifElse(spaces, "space", "tab"), size: n, picked: true}
			m.saveSpot() // the tab keeps it

			return m.syncTabs()
		}
		f.reset = func(m *Model) tea.Cmd {
			m.pv.ind = fileIndent(m.pv.path, m.pv.text())
			m.saveSpot()

			return m.syncTabs()
		}
	}

	m.modal = &modal{form: f, x: -1}

	return nil
}

// syncTabs makes the active editor's tab width (tabWidth) the width of a
// tab, redrawing the editor with the cursor and the selection on the
// characters they were on.
func (m *Model) syncTabs() tea.Cmd {
	n := m.tabWidth()
	if n == tabW {
		return nil
	}

	p := &m.pv
	if p.buf == nil { // a diff or a file too big to edit: drawn again from its source
		tabW = n
		p.raw = ""

		return p.load(m)
	}

	cur, anchor := p.rawPos(p.cur), p.anchor
	if anchor != nil {
		a := p.rawPos(*anchor)
		anchor = &a
	}

	tabW = n

	p.refreshAll(m)

	p.cur = p.dispPos(cur)
	if anchor != nil {
		a := p.dispPos(*anchor)
		p.anchor = &a
	}

	return nil
}

// mdWidths are the values Settings cycles markdown_width through; 0 fills
// the panel.
var mdWidths = []int{80, 100, 120, 0}

func mdWidthHint(n int) string {
	if n <= 0 {
		return "panel"
	}

	return strconv.Itoa(n)
}
