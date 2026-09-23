// Package ui is pando's Bubble Tea client: views (Files, Git, Agents) docked
// in a left and a right sidebar around a main area showing the active agent
// session or a file preview, drawn like VS Code.
package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

type view int

const (
	viewFiles view = iota
	viewGit
	viewAgents
	viewSearch
	viewTerm
	viewSession // the agent session, docked beside the editor instead of over it
)

var (
	viewTitles = []string{"Explorer", "Source Control", "Spaces", "Search", "Terminal", "Session"}
	viewKeys   = []string{"files", "git", "agents", "search", "terminal", "session"} // values of settings.left and settings.right
)

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
)

// actH is the height of a sidebar's activity bar: the icons, then the row
// carrying the active border under them.
const actH = 2

// actW is the width of the vertical activity bar: one chip, the same the top
// bar draws, so both bars hold the same button.
func actW() int { return 2*len(barPad()) + ansi.StringWidth(icFiles.short()) }

// railW is the width a hidden sidebar keeps: a column of view icons to reopen it.
const railW = 2

// drag is a mouse gesture from a click until its release.
type drag struct {
	kind   int
	col    int    // dragDivider: the column being resized
	pane   string // dragPane: Git drawer title
	v      view   // dragTab: the tab being dragged
	proj   string // dragRow: the project being moved in the Spaces list
	from   int    // dragRow: the index it was picked up from
	x0     int    // dragTab: where it was picked up
	drop   *dropTarget
	y0, h0 int
	moved  bool
	fine   bool        // dragTab: one row of motion is a drag (vertical bar chips sit a row apart)
	bar    *scrollDrag // dragScroll: the scrollbar and where its slider was picked up
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
	main  bool // the editor area: a docked session goes back over it
	label string
}

type rect struct{ x, w int }

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
	w, h        int // h excludes the panel border rows
	termH       int
	st          proto.State
	wss         []proto.Workspace
	sessions    []proto.Session
	ws, sess    string
	focus       int               // a column index or onMain
	recent      []int             // per view: when it was last shown; a column shows its most recent tab
	savedWs     string            // the workspace the daemon was last told about
	savedEds    proto.Editors     // the open editors the daemon was last told about
	savedDrafts map[string]string // per draft key: the unsaved text the daemon was last told about
	branchSeen  string            // the open worktree's branch the worktree list was last reloaded for
	ticks       int
	clock       int     // counter behind recent
	hidden      [2]bool // per side, left and right: folded into a rail
	preview     bool    // main shows the preview instead of the session
	dark        bool
	fg, bg      string // host terminal colors as #rrggbb, passed to new sessions
	msg         string
	msgErr      bool
	msgAt       time.Time
	soundAt     time.Time         // when the last sound cue played
	lastTab     map[string]string // session to its tab shown last
	seen        map[string]string // session to the state it was last clicked or shown in
	blinkOn     bool              // a pulsing tint is at its full shade
	blinking    bool              // its ticker runs
	events      <-chan proto.Event
	inputs      chan proto.InputParams
	gitBusy     bool
	clicks      clicks
	drag        *drag
	// ambiguous marks a terminal that cannot tell ⌃` from ⌃space: both
	// arrive as NUL, so an editor's suggestions swallow the one that would
	// have opened the terminal panel. saidCtrlJ keeps the hint to once.
	ambiguous bool
	saidCtrlJ bool
	tv        termPanel // the Terminal panel's shell
	termMax   bool      // the panel fills the editor area (⌃⇧↑)
	filters   [6]filter // per view
	mouseX    int       // last mouse position (content rows) for hover
	mouseY    int
	mouseAt   time.Time
	index     struct { // workspace file list for quick open and the Files filter
		ws    string
		files []string
		at    time.Time
	}

	ex  explorer
	scm scmView
	ag  agents
	sr  searchView

	lastCommand string // the command palette entry run last, listed first next time
	vimReg      string // vim_mode: the unnamed register, shared by every editor
	vimRegLine  bool   // what it holds was yanked line-wise, so p pastes it as lines
	pv          preview
	pk          *peek // the references widget under the editor
	lsps        lspPool
	// Open editors: the tab strip in main, and where navigation has been.
	editors   []preview
	edIdx     int
	nav       []preview
	navAt     int
	restoring bool

	upd   proto.Update // what the daemon's last update check found
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
	flashMsg      struct {
		text string
		err  bool
	}
	gitMsg struct {
		ws     string
		repos  []string
		status map[string]git.Status
		deco   map[string]byte
	}
	indexMsg struct {
		ws    string
		files []string
		open  bool // show quick open once loaded
	}
)

// Run connects to (or starts) the daemon and runs the TUI. dir is the
// directory pando was started with; empty means none was given, and the
// workspace shown last opens instead.
func Run(dir string) error {
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
	if fam := terminalFont(); st.Settings.Icons == "nerd" && fam != "" && !nerdFamily(fam) {
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

	_, err = tea.NewProgram(m, tea.WithEnvironment(proto.WithoutNoColor(os.Environ()))).Run()

	return err
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

	var ws string
	if err := proto.Call("project.add", map[string]string{"path": dir}, &ws); err != nil {
		return "", nil, err
	}

	var wss []proto.Workspace

	_ = proto.Call("workspace.list", nil, &wss) // the project was just added
	for _, w := range wss {
		if (dir == w.Path || strings.HasPrefix(dir, w.Path+"/")) && len(w.Path) >= len(ws) {
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
		inputs: make(chan proto.InputParams, 512), mouseY: -1, edIdx: -1, navAt: -1,
		ambiguous: true, // until the terminal answers that it disambiguates keys
	}
	for i := range m.filters {
		m.filters[i] = newFilter()
	}

	m.look()
	m.ag.l.sel = -1
	m.scm.init()
	m.sr.init()
	m.switchWorkspace(ws) // attaches the Terminal panel too; Init starts a shell when it found none

	return m
}

func (m *Model) look() {
	applyLook(m.st.Settings.Theme, m.dark, m.st.Settings.Icons, m.st.Settings.Colors)
}

// Init asks for the terminal's colours and starts the clock, the event
// stream and the first loads.
func (m *Model) Init() tea.Cmd {
	// m.pv is the editor New restored for the workspace; its content loads here.
	return tea.Batch(waitEvent(m.events), tick(), tea.RequestBackgroundColor, tea.RequestForegroundColor,
		m.refreshGit(), m.pv.load(m), m.loadDrafts(), loadUpdate(), m.ensureTerm())
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
func (m *Model) setSettings(patch map[string]any) tea.Cmd {
	b, _ := json.Marshal(patch)
	_ = json.Unmarshal(b, &m.st.Settings) // optimistic: the daemon sends the settings back
	m.look()
	m.resize()
	m.fixFocus()
	m.ex.rebuild(m)
	m.scm.build(m)

	cmds := []tea.Cmd{do("state.set", map[string]any{"settings": patch})}
	if _, ok := patch["color_theme"]; ok {
		m.pv.raw = "" // re-render the tints in the new palette
		cmds = append(cmds, m.pv.load(m))
	}

	return tea.Batch(cmds...)
}

func (m *Model) refreshGit() tea.Cmd {
	if m.gitBusy {
		return nil
	}

	m.gitBusy = true
	ws, withDeco := m.ws, m.st.Settings.GitDeco

	return func() tea.Msg {
		msg := gitMsg{ws: ws, repos: git.Discover(ws), status: map[string]git.Status{}, deco: map[string]byte{}}
		for _, r := range msg.repos {
			if st, err := git.Stat(r); err == nil {
				msg.status[r] = st
				if withDeco {
					maps.Copy(msg.deco, git.Decorations(r, st))
				}
			}
		}

		return msg
	}
}

// drawers are the Source Control history drawers the settings show, in
// git.Drawers order.
func (m *Model) drawers() []git.Drawer {
	want := m.st.Settings.Drawers
	if want == nil {
		want = defaultDrawers
	}

	var out []git.Drawer

	for _, d := range git.Drawers {
		if slices.Contains(want, d.Title) {
			out = append(out, d)
		}
	}

	return out
}

// toggleDrawer shows or hides one history drawer.
func (m *Model) toggleDrawer(title string) tea.Cmd {
	var want []string

	for _, d := range git.Drawers {
		shown := slices.ContainsFunc(m.drawers(), func(x git.Drawer) bool { return x.Title == d.Title })
		if shown != (d.Title == title) {
			want = append(want, d.Title)
		}
	}

	return tea.Batch(m.setSettings(map[string]any{"git_drawers": want}), m.scm.loadDrawers(m))
}

func (m *Model) loadIndex(open bool) tea.Cmd {
	ws := m.ws
	return func() tea.Msg { return indexMsg{ws, git.ListFiles(ws, 20000), open} }
}

// cols lists the sidebar columns from the left edge of the screen: left
// columns, then right ones. Views missing from the settings (a fresh config,
// or a view added later) join the left column next to main.
func (m *Model) cols() []col {
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

	side, docked := 0, m.termOpen() && m.termPos() != "bottom"
	if m.termPos() == "right" {
		side = 1
	}

	if !docked { // a shut or bottom terminal is no column at all
		out = slices.DeleteFunc(out, func(c col) bool { return slices.Equal(c.views, []view{viewTerm}) })
		for i := range out {
			out[i].views = slices.DeleteFunc(out[i].views, func(v view) bool { return v == viewTerm })
		}
	}

	if m.sess == "" || m.sessPos() == "editor" { // nothing on screen, or over the editor: its column goes, its place stays in the settings
		out = slices.DeleteFunc(out, func(c col) bool { return slices.Equal(c.views, []view{viewSession}) })
		for i := range out {
			out[i].views = slices.DeleteFunc(out[i].views, func(v view) bool { return v == viewSession })
		}
	}

	for v, ok := range seen {
		if view(v) == viewSession {
			// Unlisted: a session opens in a column of its own beside the
			// editor, unless session_position puts it over the editor area.
			if !ok && m.sess != "" && m.sessPos() != "editor" {
				out = slices.Insert(out, leftCount(out), col{views: []view{viewSession}, right: m.sessPos() == "right"})
			}

			continue
		}

		if view(v) == viewTerm {
			if docked && !slices.ContainsFunc(out, func(c col) bool { return slices.Contains(c.views, viewTerm) }) {
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

// viewOn is the tab column i shows, its most recently shown one; false when
// there is no such column.
func (m *Model) viewOn(i int) (view, bool) {
	vs := m.colViews(i)
	if len(vs) == 0 {
		return 0, false
	}

	best := vs[0]
	for _, v := range vs {
		if m.recent[v] > m.recent[best] {
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
	return len(m.colViews(i)) > 0 && m.hidden[m.side(i)] && m.w >= 40
}

func (m *Model) anyRailed() bool {
	return m.w >= 40 && slices.ContainsFunc(m.cols(), func(c col) bool { return m.hidden[b2i(c.right)] })
}

func (m *Model) focused(v view) bool { return m.focus == m.colOf(v) && m.shown(v) }

// bord is 1 when panels are framed with borders, which cost a row above and below.
func (m *Model) bord() int { return b2i(m.st.Settings.Borders && m.termH >= 10 && m.w >= 40) }

func (m *Model) resize() { m.h = max(m.termH-2*m.bord(), 1) }

// layout places left columns | main | right columns, separated by a divider
// or by the facing borders of framed panels. A hidden side collapses into
// one rail. Columns that do not fit, the outermost first, take no space;
// main keeps at least 20 cells.
func (m *Model) layout() (cs []rect, c rect) {
	b := m.bord()
	gap := 1 + b
	cols := m.cols()
	cs = make([]rect, len(cols))

	var railed [2]bool

	for i, k := range cols {
		side := b2i(k.right)
		switch {
		case m.w < 40:
		case m.hidden[side]:
			if !railed[side] {
				cs[i].w, railed[side] = railW, true
			}

		default:
			w := k.width
			if w <= 0 {
				w = []int{m.st.Settings.Width, m.st.Settings.WidthR}[side]
			}

			if w <= 0 {
				w = 32
			}

			cs[i].w = max(w, 20)
		}
	}

	for i, k := range cols { // a session column nobody sized takes half the editor area, as docking it by hand does
		if k.width > 0 || cs[i].w == 0 || m.hidden[b2i(k.right)] || !slices.Contains(k.views, viewSession) {
			continue
		}

		rest := m.w - 2*b - gap
		for j, r := range cs {
			if j != i && r.w > 0 {
				rest -= r.w + gap
			}
		}

		cs[i].w = max(rest/2, 20)
	}

	over := func() int {
		n := 20 + 2*b - m.w
		for _, r := range cs {
			if r.w > 0 {
				n += r.w + gap
			}
		}

		return n
	}
	left := leftCount(cols)

	shrink := make([]int, 0, len(cols)) // right columns from the right edge, then left ones from the left edge
	for i := len(cols) - 1; i >= left; i-- {
		shrink = append(shrink, i)
	}

	for i := range left {
		shrink = append(shrink, i)
	}

	for _, i := range shrink {
		if d := over(); d > 0 && cs[i].w > railW {
			if cs[i].w -= d; cs[i].w < 20 {
				cs[i].w = 0
			}
		}
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
func (m *Model) panelH() int { return max(m.h-1, 1) }

// sessPos is where a session opens: a column of its own on that side, or
// "editor", over the editor area.
func (m *Model) sessPos() string {
	if m.st.Settings.SessPos == "editor" || m.st.Settings.SessPos == "left" {
		return m.st.Settings.SessPos
	}

	return "right"
}

// termPos is where the Terminal lives: a panel under the editor, or a sidebar.
func (m *Model) termPos() string {
	switch m.st.Settings.TermPos {
	case "left", "right":
		return m.st.Settings.TermPos
	}

	return "bottom"
}

// termOpen reports the Terminal showing, wherever it sits.
func (m *Model) termOpen() bool { return m.st.Settings.TermOpen }

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

	return max(min(h, m.panelH()-3), 3)
}

// mainH is the main area above the terminal panel.
func (m *Model) mainH() int { return max(m.panelH()-m.termRows(), 0) }

func (m *Model) mainX() int { _, c := m.layout(); return c.x }
func (m *Model) mainW() int { _, c := m.layout(); return c.w }

// bodyTop is the first list row of view v, below the activity bar, the view
// header and the filter line.
func (m *Model) bodyTop(v view) int { return m.barH(m.colOf(v)) + 1 + b2i(m.filters[v].on) }

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

	if y := m.mouseY - m.bodyTop(v); y >= 0 && y < m.bodyH(v) {
		return y
	}

	return -1
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

	i := slices.Index(order, m.focus)
	for range order {
		i = (i + 1) % len(order)

		f := order[i]
		if f == onMain || f == onPanel {
			m.focus = f
			return
		}

		side := m.side(f)
		was := m.hidden[side]

		m.hidden[side] = false
		if m.colRect(f).w > 0 {
			m.focus = f
			return
		}

		m.hidden[side] = was
	}
}

// showView selects tab v, unhides its side and focuses its column.
func (m *Model) showView(v view) tea.Cmd {
	i := m.colOf(v)
	m.clock++
	m.recent[v] = m.clock
	m.hidden[m.side(i)], m.focus = false, i
	m.fixFocus()

	if v == viewGit {
		return tea.Batch(m.scm.loadDrawers(m), m.fetchScreen())
	}

	return m.fetchScreen()
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
	if v == viewAgents {
		cs = m.sessionFollows(cs)
	}

	sides := colSettings(cs)

	patch := map[string]any{"left": sides[0], "right": sides[1]}
	if i := colWith(cs, viewSession); i >= 0 { // the side a session opens on from now on
		patch["session_position"] = []string{"left", "right"}[b2i(cs[i].right)]
	}

	cmd := m.setSettings(patch)

	return tea.Batch(cmd, m.showView(v), m.scm.loadDrawers(m))
}

// colWith is the index of the column holding v, -1 when no column does.
func colWith(cs []col, v view) int {
	return slices.IndexFunc(cs, func(c col) bool { return slices.Contains(c.views, v) })
}

// sessionFollows moves the session's column to the side Spaces just moved to,
// keeping its width: sessions are started from Spaces, so the two stay
// together. A session over the editor area has no column to move.
func (m *Model) sessionFollows(cs []col) []col {
	i, j := colWith(cs, viewSession), colWith(cs, viewAgents)
	if i < 0 || j < 0 || cs[i].right == cs[j].right {
		return cs
	}

	side, w := cs[j].right, cs[i].width
	if w <= 0 {
		w = m.colRect(m.colOf(viewSession)).w
	}

	cs = removeView(cs, viewSession)

	return slices.Insert(cs, leftCount(cs), col{views: []view{viewSession}, right: side, width: max(w, 20)})
}

// moveView docks tab v in the column next to main on side `to` (0 left, 1
// right). The Terminal never shares a column, in either direction.
func (m *Model) moveView(v view, to int) tea.Cmd {
	switch v {
	case viewTerm:
		return m.moveTerminal([]string{"left", "right"}[to])
	case viewSession:
		return m.splitTo(v, to)
	default: // every sidebar view docks as an ordinary tab, below.
	}

	cs := removeView(m.cols(), v)
	left := leftCount(cs)

	alone := func(i int) bool { return i >= 0 && i < len(cs) && soloCol(cs[i]) }
	switch {
	case to == 0 && left > 0 && !alone(left-1):
		cs[left-1].views = append(cs[left-1].views, v)
	case to == 0:
		cs = slices.Insert(cs, max(left, 0), col{views: []view{v}})
	case left < len(cs) && !alone(left):
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
		if j < 0 || j >= len(cs) || cs[j].right != cs[i].right || soloCol(cs[j]) {
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
// gets a column of its own beside it.
func (m *Model) dropAt(v view, x, y int) dropTarget {
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

	if j := slices.IndexFunc(cs, func(r rect) bool { return r.w > railW && x >= r.x && x < r.x+r.w }); j >= 0 {
		i = j
	}

	if i < 0 || i >= len(cs) || cs[i].w <= railW {
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

	if v == viewTerm || soloCol(m.cols()[i]) { // never share a column: moveView opens one beside main
		return dropTarget{rc: rc, side: side, col: -1, label: []string{"Left Sidebar", "Right Sidebar"}[side]}
	}

	return m.slotAt(v, i, x, y)
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
		k := max(min(y/actH, len(views)-b2i(idx >= 0)), 0)
		t.at = k

		t.rc = rect{x: rc.x, w: actW()}
		if side == 1 {
			t.rc.x = rc.x + rc.w - actW()
		}

		t.y, t.h = max(min(k*actH, m.panelH()-actH), 0), actH

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
		if d.v == viewSession {
			t = m.sessionDrop(x)
		}

		d.drop = &t
	}

	if !release {
		return nil
	}

	m.drag = nil

	switch {
	case !d.moved:
		return nil
	case d.drop.main:
		return m.undockSession()
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

// soloCol reports a column that shares with no other view: the Terminal's, the session's.
func soloCol(c col) bool {
	return slices.Contains(c.views, viewTerm) || slices.Contains(c.views, viewSession)
}

// splitTo gives v a column of its own at the outer edge of side `to`; the
// session's goes right beside the editor, half its width.
func (m *Model) splitTo(v view, to int) tea.Cmd {
	if v == viewSession {
		_, c := m.layout()

		w := c.w / 2
		if i := m.colOf(v); i >= 0 {
			w = m.colRect(i).w // moving sides: the session and the editor keep their widths
		}

		cs := removeView(m.cols(), v)
		cs = slices.Insert(cs, leftCount(cs), col{views: []view{v}, right: to == 1, width: max(w, 20)})

		return m.dock(cs, v)
	}

	if v == viewTerm && m.termPos() != []string{"left", "right"}[to] {
		return m.moveTerminal([]string{"left", "right"}[to])
	}

	cs := removeView(m.cols(), v)
	if to == 0 {
		cs = slices.Insert(cs, 0, col{views: []view{v}})
	} else {
		cs = append(cs, col{views: []view{v}, right: true})
	}

	return m.dock(cs, v)
}

// sessionDrop is where a dragged session lands: the middle of the editor area
// puts it back over the editor, either side of it docks it there.
func (m *Model) sessionDrop(x int) dropTarget {
	_, c := m.layout()

	q := max(c.w/4, 1)
	switch {
	case x < c.x+q:
		return dropTarget{rc: rect{x: c.x, w: 2 * q}, side: 0, col: -1, own: true, label: "Dock Left"}
	case x >= c.x+c.w-q:
		return dropTarget{rc: rect{x: c.x + c.w - 2*q, w: 2 * q}, side: 1, col: -1, own: true, label: "Dock Right"}
	}

	return dropTarget{rc: c, col: -1, main: true, label: "Editor Area"}
}

// sessDocked reports the session in a column beside the editor.
func (m *Model) sessDocked() bool { return m.colOf(viewSession) >= 0 }

// undockSession puts the docked session back over the whole editor area. Its
// column stays in the settings: it is where the session docks again.
func (m *Model) undockSession() tea.Cmd {
	cmd := m.setSettings(map[string]any{"session_position": "editor"})
	m.preview, m.focus = false, onMain

	return tea.Batch(cmd, m.fetchScreen())
}

// sessSide is the side a session docks to: the one session_position names, or
// where its remembered column sits when it has the editor area.
func (m *Model) sessSide() int {
	switch m.sessPos() {
	case "left":
		return 0
	case "right":
		return 1
	}

	listed := func(cs proto.Columns) bool {
		return slices.ContainsFunc(cs, func(c proto.Column) bool { return slices.Contains(c.Views, viewKeys[viewSession]) })
	}
	if listed(m.st.Settings.Left) && !listed(m.st.Settings.Right) {
		return 0
	}

	return 1
}

// hideSession takes the session off the screen; it runs on in the background
// and comes back from Agents or its tab.
func (m *Model) hideSession() tea.Cmd {
	if m.sessFocused() { // its column goes: the editor takes the keyboard
		m.focus = onMain
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

	items := []item{{label: "Close (keeps running)", run: func(m *Model) tea.Cmd { return m.hideSession() }}}
	if m.sessDocked() {
		items = append(items, item{label: "Move to Editor Area", run: func(m *Model) tea.Cmd { return m.undockSession() }})
	}

	if i := m.colOf(viewSession); i < 0 || m.side(i) == 1 {
		items = append(items, item{label: "Dock Left", run: func(m *Model) tea.Cmd { return m.splitTo(viewSession, 0) }})
	}

	if i := m.colOf(viewSession); i < 0 || m.side(i) == 0 {
		items = append(items, item{label: "Dock Right", run: func(m *Model) tea.Cmd { return m.splitTo(viewSession, 1) }})
	}

	items = append(items, item{label: "Rename…", run: func(m *Model) tea.Cmd { return m.renameSession(id) }},
		item{label: "Kill Session…", run: func(m *Model) tea.Cmd { return m.confirmKill(id) }})

	return m.menuOf(items, m.mouseX, m.mouseY)
}

// menuOf opens items as a context menu at x, y.
func (m *Model) menuOf(items []item, x, y int) tea.Cmd {
	m.modal = newMenu("", x, y, items...)
	return nil
}

func (m *Model) tabMenu(v view, x, y int) tea.Cmd {
	if v == viewSession {
		return m.sessionTitleMenu()
	}

	if v == viewTerm {
		m.modal = newMenu(viewTitles[v], x, y, m.terminalItems()...)
		return nil
	}

	cs := m.cols()
	i := m.colOf(v)

	other, label := 1, "Move to Right Sidebar"
	if cs[i].right {
		other, label = 0, "Move to Left Sidebar"
	}

	items := []item{{label: label, run: func(m *Model) tea.Cmd { return m.moveView(v, other) }}}
	if len(cs[i].views) > 1 {
		items = append(items, item{label: "Move to Own Column", run: func(m *Model) tea.Cmd { return m.splitView(v) }})
	}

	if slices.ContainsFunc(cs, func(c col) bool { return c.right == cs[i].right && !slices.Contains(c.views, v) }) {
		items = append(items, item{label: "Merge into Next Column", run: func(m *Model) tea.Cmd { return m.mergeView(v) }})
	}

	items = append(items, item{label: "Hide Sidebar", hint: "b", run: func(m *Model) tea.Cmd { return m.hide(i) }})
	m.modal = newMenu(viewTitles[v], x, y, items...)

	return nil
}

// terminalItems are the Terminal's own moves: it is a panel, not a tab.
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

	if m.session(m.sess) != nil {
		items = append(items, item{label: "New Tab in Session", run: func(m *Model) tea.Cmd { return m.newTab() }})
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

// dirtyEditors counts the open editors with unsaved text.
func (m *Model) dirtyEditors() int {
	n := b2i(m.pv.dirty())
	for _, e := range m.editors {
		if e.dirty() && e.id() != m.pv.id() {
			n++
		}
	}

	return n
}

// saveAll writes every editor that has unsaved text, the open one included.
// Untitled buffers are skipped — a bulk save cannot stop to ask where each
// one goes — and stay safe as drafts.
func (m *Model) saveAll() {
	if m.pv.dirty() && !m.pv.untitled() {
		_ = m.pv.buf.save(m.pv.path, false) // best effort on the way out
	}

	for _, e := range m.editors {
		if e.dirty() && !e.untitled() {
			_ = e.buf.save(e.path, false) // best effort on the way out
		}
	}
}

// toggleSidebars folds both sides away, or brings them back.
func (m *Model) toggleSidebars() tea.Cmd {
	both := !m.hidden[0] && !m.hidden[1]
	m.hidden = [2]bool{both, both}
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
	m.hidden[m.side(i)] = true
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
	build := func(short bool) []tab {
		var out []tab

		x := 1

		pad := barPad()
		if short {
			pad = " "
		}

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
			x += t.w + 1
			out = append(out, t)
		}

		return out
	}
	// Chips tighten to one cell of air, and text labels shrink to their
	// initials, when they would run into the gear.
	out := build(false)
	if n := len(out); n > 0 && out[n-1].x+out[n-1].w > m.colRect(s).w-ansi.StringWidth(gearLabel()) {
		out = build(true)
	}

	return out
}

// titleAction is a hover button at the right end of a view header.
type titleAction struct {
	label string
	x, w  int
	hot   bool // under the mouse
	run   func(m *Model) tea.Cmd
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

	add := func(g glyph, run func(*Model) tea.Cmd) {
		acts = append(acts, titleAction{label: " " + g.s() + " ", run: run})
	}

	switch v {
	case viewFiles:
		add(icNewFile, func(m *Model) tea.Cmd { return m.ex.action("n")(m) })
		add(icNewDir, func(m *Model) tea.Cmd { return m.ex.action("N")(m) })
		add(icRefresh, func(m *Model) tea.Cmd { m.ex.rebuild(m); return m.refreshGit() })
		add(icCollapse, func(m *Model) tea.Cmd { m.ex.collapseAll(m); return nil })

	case viewGit:
		mode := icTree
		if m.st.Settings.GitTree {
			mode = icList
		}

		add(icBranch, m.scm.branchPicker)
		add(icCheck, m.scm.commit)
		add(mode, m.scm.toggleTree)
		add(icRefresh, func(m *Model) tea.Cmd { return tea.Batch(m.refreshGit(), m.scm.loadDrawers(m)) })
		add(icCollapse, func(m *Model) tea.Cmd { m.scm.collapseAll(m); return nil })

	case viewSearch:
		add(icReplace, func(*Model) tea.Cmd { return m.sr.toggleReplace() })
		add(icRefresh, m.sr.restart)
		add(icClearAll, func(m *Model) tea.Cmd { m.sr.query.Reset(); m.sr.clear(); return nil })
		add(icCollapse, func(m *Model) tea.Cmd { m.sr.collapseAll(); return nil })

	case viewTerm:
		add(icAdd, func(m *Model) tea.Cmd { return m.newTerm() })
		add(icClose, func(m *Model) tea.Cmd { return m.confirmKill(m.tv.id) })

	case viewSession:
		add(icClose, func(m *Model) tea.Cmd { return m.hideSession() })
	case viewAgents:
		add(icAdd, func(m *Model) tea.Cmd { return m.ag.newSession(m, m.ag.selected(m)) })
		add(icWorktree, func(m *Model) tea.Cmd { return m.ag.newWorktree(m, m.ag.selected(m)) })
		add(icRefresh, func(*Model) tea.Cmd { return tea.Batch(loadState(), loadWorkspaces(), loadSessions()) })
	}

	if m.barH(s) == 0 && bw == 0 { // no activity bar to hold the gear
		add(icGear, func(m *Model) tea.Cmd { m.modal = settingsModal(m); return nil })
	}

	hide := icOpenRight // the arrow points the way the sidebar folds
	if m.side(s) == 1 {
		hide = icOpenLeft
	}

	add(hide, func(m *Model) tea.Cmd { return m.hide(s) })

	x := w
	for i := len(acts) - 1; i >= 0; i-- {
		acts[i].w = ansi.StringWidth(acts[i].label)
		x -= acts[i].w
		acts[i].x = x
		acts[i].hot = m.mouseY == m.barH(s) && mx >= x && mx < x+acts[i].w
	}

	return acts
}

// switchWorkspace opens path: the sidebar views, the session and the
// editors it had open follow. The command saves the editors of the workspace
// left behind and loads the one that was active in the new one.
func (m *Model) switchWorkspace(path string) tea.Cmd {
	m.ag.reveal(m, path)

	if path == m.ws {
		return nil
	}

	saved := tea.Batch(m.saveEditors(), m.saveDrafts())
	m.savedDrafts = nil
	m.ws = path
	m.ex.setRoot(m, path)
	m.scm.reset()
	m.sr.clear()
	m.pk = nil
	m.lsps.close()                                        // the servers belong to the workspace they indexed
	m.editors, m.edIdx, m.nav, m.navAt = nil, -1, nil, -1 // editors of another worktree
	m.pv.close(m)                                         // a preview from another worktree would be misleading

	if s := m.session(m.sess); s == nil || s.Workspace != path {
		m.sess = ""
		for _, s := range m.agentSessions() {
			if s.Workspace == path {
				m.sess = s.ID
				break
			}
		}

		m.term = term{}
	}

	restored := m.restoreEditors()
	m.preview = m.preview && (m.sess == "" || m.sessDocked()) // a session over the editor is what shows; the tabs wait in the strip

	return tea.Batch(saved, restored, m.loadDrafts(), m.ensureTerm())
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

	if m.sess != id {
		m.term = term{}
	}
	// Before the workspace switch, which keeps a session of its workspace
	// rather than picking one — and the Terminal panel follows the session.
	m.sess, m.preview = id, false

	if m.lastTab == nil {
		m.lastTab = map[string]string{}
	}

	m.lastTab[m.rootOf(id)] = id

	for _, t := range m.tabsOf(m.rootOf(id)) { // clicked: its state and its tabs' are seen
		m.acknowledge(t.ID)
	}

	switched := m.switchWorkspace(s.Workspace)

	return tea.Batch(switched, m.ensureTerm(), m.fetchScreen(), m.refreshGit())
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
	return m.pv.kind != "" && (m.sess == "" || m.preview || m.sessDocked())
}

// showsSession reports a session filling the main area.
func (m *Model) showsSession() bool { return m.sess != "" && !m.preview && !m.sessDocked() }

// Update owns every message: keys and mouse, the daemon's events, and what
// the commands it started send back.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.termH = msg.Width, msg.Height
		m.resize()
		m.fixFocus()

		return m, m.fetchScreen()

	case tea.KeyboardEnhancementsMsg:
		m.ambiguous = !msg.SupportsKeyDisambiguation()
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

	case tickMsg:
		m.ex.rebuild(m)
		cmds := []tea.Cmd{tick(), m.blink(), m.refreshGit(), m.pv.reloadIfLive(m), m.scm.saveDraft(m), m.scm.loadDrawers(m), m.saveWorkspace(), m.saveEditors(), m.saveDrafts()}
		// ponytail: other projects' branches come back every 30 s, one git call
		// per project; watch their HEADs if that lags.
		m.ticks++
		if m.ticks%15 == 0 {
			cmds = append(cmds, loadWorkspaces())
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
		m.st = proto.State(msg)
		m.look()
		m.resize()
		m.ex.rebuild(m)
		m.scm.build(m)
		m.fixFocus()

		return m, nil

	case updateMsg:
		return m, m.onUpdate(proto.Update(msg))
	case workspacesMsg:
		m.wss = msg
		return m, nil

	case sessionsMsg:
		var next string

		if prev := m.session(m.sess); prev != nil && !slices.ContainsFunc(msg, func(s proto.Session) bool { return s.ID == prev.ID }) {
			m.flash(sessionName(*prev)+" session closed", false)
			// Before the list is replaced: a closed tab hands over to its
			// neighbour in the session, a closed session to one of the space.
			if next = nextTab(m.tabsOf(m.rootOf(m.sess)), m.sess, msg); next == "" {
				next = nextTab(m.spaceSessions(), m.rootOf(m.sess), msg)
			}

			m.sess, m.term = "", term{}
		}

		termNext := ""
		if m.tv.id != "" {
			termNext = nextTab(m.termSessions(), m.tv.id, msg)
		}

		cmd := m.sound(m.soundFor(msg))

		m.sessions = msg

		for _, s := range m.mainSessions() { // what is on screen is seen as it changes
			if m.inView(s.ID) {
				m.acknowledge(s.ID)
			}
		}

		cmd = tea.Batch(cmd, m.blink())
		if next != "" { // a tab closed with others left: the neighbour takes over
			cmd = tea.Batch(cmd, m.switchSession(next))
		}

		if m.tv.id != "" && m.session(m.tv.id) == nil { // the panel's shell is gone
			m.tv.id, m.tv.term = "", term{}
			if termNext != "" {
				m.tv.id = termNext
			} else if m.termOpen() { // the last tab closed: so does the panel
				return m, tea.Batch(cmd, m.toggleTerminal())
			}
		}

		return m, cmd

	case screenMsg:
		return m, m.onScreen(msg)
	case gitMsg:
		m.gitBusy = false
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

		return m, cmd

	case searchTickMsg:
		return m, m.sr.onTick(m, int(msg))
	case searchMsg:
		m.sr.onResult(msg)
		return m, nil

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

	case actionsMsg:
		return m, m.onActions(msg)
	case appliedMsg:
		return m, m.onApplied(msg)
	case scmMsg, drawerMsg, modalMsg, stageMsg:
		return m, m.scm.onMsg(m, msg)
	case previewMsg:
		m.pv.onLoad(m, msg)
		return m, nil

	case indexMsg:
		if msg.ws == m.ws {
			m.index.ws, m.index.files, m.index.at = msg.ws, msg.files, time.Now()
			m.ex.rebuild(m)
		}

		if msg.open {
			m.quickOpen(msg)
		}

		return m, nil

	case newSessionMsg:
		return m, m.onNewSession(proto.Session(msg))
	case newWorkspaceMsg:
		return m, m.onNewWorkspace(proto.Workspace(msg))
	case focusSessionMsg:
		m.focus = onMain
		return m, m.switchSession(string(msg))

	case flashMsg:
		m.flash(msg.text, msg.err)
		return m, nil

	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		return m, m.paste(msg)
	case tea.MouseMsg:
		return m, m.mouse(msg)
	}
	// Cursor blink and other input messages.
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

	switch {
	case m.modal != nil && m.modal.hasInput():
		m.modal.input, cmd = m.modal.input.Update(msg)
		if m.modal.filter {
			m.modal.refilter()
		}

		if m.modal.complete != nil {
			m.modal.input.SetSuggestions(m.modal.complete(m.modal.input.Value()))
		}

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
	case m.sr.editing():
		in := &m.sr.query
		if m.sr.include.Focused() {
			in = &m.sr.include
		}

		*in, cmd = in.Update(msg)
		cmd = tea.Batch(cmd, m.sr.restart(m))

	case m.focus == onMain && m.showsSession(), m.sessFocused(): // over the editor, or docked in its column
		m.term.scroll = 0
		m.inputs <- proto.InputParams{ID: m.sess, Paste: msg.Content}

	case m.termFocused() && m.tv.id != "":
		m.tv.scroll = 0
		m.inputs <- proto.InputParams{ID: m.tv.id, Paste: msg.Content}

	case m.focus != onMain:
		if v, ok := m.viewOn(m.focus); ok && m.filters[v].editing {
			m.filters[v].input, cmd = m.filters[v].input.Update(msg)
			m.refilter(v)
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

func (m *Model) flash(text string, isErr bool) {
	m.msg, m.msgErr, m.msgAt = text, isErr, time.Now()
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
			cmds = append(cmds, m.openFile(f.Open))
		}

		return tea.Batch(cmds...)
	}

	return nil
}

type focusSessionMsg string

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	if m.modal != nil {
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

	if m.focus == onPanel { // the panel is a terminal: every other key is the shell's
		return m.termPanelKey(k)
	}

	if m.focus == onMain {
		switch {
		case m.showsSession():
			return m.term.key(m, m.sess, k)
		case m.pk != nil:
			return m.pk.key(m, k)
		case s == "esc" && m.pv.kind == "":
			return m.confirmQuit()
		}

		return m.pv.key(m, k)
	}

	v, _ := m.viewOn(m.focus)
	if v == viewTerm { // the panel is a terminal: every other key is the shell's
		return m.termPanelKey(k)
	}

	if v == viewSession {
		return m.term.key(m, m.sess, k)
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
		return tea.Sequence(m.saveEditors(), m.saveDrafts(), tea.Quit)
	case "1", "2", "3", "4":
		cmd := m.showView(view(s[0] - '1'))
		if s == "4" {
			return tea.Batch(cmd, m.sr.focus())
		}

		return cmd

	case "ctrl+f":
		if v == viewSearch {
			return m.sr.focus()
		}

		return m.startFilter(v)

	case "ctrl+p":
		return m.loadIndex(true)
	case ",":
		m.modal = settingsModal(m)
		return nil

	case "?":
		m.modal = helpModal()
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

	case "[", "]":
		return m.cycleSession(map[string]int{"[": -1, "]": 1}[s])
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

func (m *Model) cycleSession(d int) tea.Cmd {
	ss := m.tabsOf(m.rootOf(m.sess))
	if len(ss) == 0 {
		ss = m.spaceSessions() // none in view: the space's first
	}

	if len(ss) == 0 {
		return nil
	}

	i := 0

	for j, s := range ss {
		if s.ID == m.sess {
			i = (j + d + len(ss)) % len(ss)
		}
	}

	return m.switchSession(ss[i].ID)
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

	if mo.Y == -1 && click && m.bord() > 0 { // a frame's top edge carries its panel's title
		return m.titleMouse(mo)
	}

	if mo.Y < 0 || mo.Y >= m.h {
		return nil // border rows
	}

	if click {
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

	if n := m.termRows(); n > 0 && mo.Y >= m.mainH() {
		if click {
			m.focus = onPanel
			m.scm.input.Blur()
		}
		// y == 0 is the panel's title row and its tabs, the rest its body.
		switch y := mo.Y - m.mainH(); {
		case y > 0:
			return m.termPanelMouse(msg, mo.X-c.x, y-1)
		case !click:
			return nil
		case mo.X >= c.x+c.w-3:
			return m.toggleTerminal()
		}

		x := mo.X - c.x - 1

		onTab := slices.ContainsFunc(m.termTabs(m.termStripW()), func(t sessTab) bool { return x >= t.x && x < t.x+t.w })
		if mo.Button == tea.MouseLeft && !onTab { // the rest of the row is the panel's sash
			m.drag = &drag{kind: dragTerm, y0: mo.Y, h0: m.termRows()}
			return nil
		}

		return m.termStripMouse(x, mo.Button)
	}

	if click {
		m.focus = onMain
		m.scm.input.Blur()
	}

	strip := m.stripH()
	if strip > 0 && click && mo.Y == 0 && (mo.Button == tea.MouseLeft || mo.Button == tea.MouseMiddle || mo.Button == tea.MouseRight && m.showsSession()) {
		if m.showsSession() {
			return m.sessionStripMouse(mo.X-c.x, mo.Button)
		}

		return m.stripMouse(mo.X-c.x, mo.Button)
	}

	if m.showsSession() {
		if click && mo.Y == strip { // the session's title: drag it beside the editor
			return m.sessionTitleClick(mo)
		}

		if y := mo.Y - 1 - strip; mo.X-c.x == c.w-1 && y >= 0 { // the scrollbar
			if click && mo.Button == tea.MouseLeft {
				return m.barMouse(&m.term, "session", y, mo.Y)
			}

			return nil
		}

		return m.term.mouse(m, m.sess, msg, mo.X-c.x, mo.Y-1-strip)
	}

	if top := 1 + strip + m.pvH(); m.pk != nil && mo.Y >= top {
		return m.pk.mouse(m, msg, mo.X-c.x, mo.Y-top)
	}

	return m.pv.mouse(m, msg, mo.X-c.x, mo.Y-1-strip)
}

// sessionTitleClick picks the session up by its title, or opens its menu.
func (m *Model) sessionTitleClick(mo tea.Mouse) tea.Cmd {
	switch mo.Button {
	case tea.MouseLeft:
		m.drag = &drag{kind: dragTab, v: viewSession, x0: mo.X, y0: mo.Y}
	case tea.MouseRight:
		return m.sessionTitleMenu()
	}

	return nil
}

// titleMouse is a click on a framed panel's title: the editor area's session,
// or a sidebar view, dragged or given its menu as by its header.
func (m *Model) titleMouse(mo tea.Mouse) tea.Cmd {
	cs, c := m.layout()
	if mo.X >= c.x && mo.X < c.x+c.w {
		if m.showsSession() {
			return m.sessionTitleClick(mo)
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
		if y < t.x || y >= t.x+actH { // the icon's row and the air under it
			continue
		}

		if mo.Button == tea.MouseRight {
			return m.tabMenu(t.v, mo.X, mo.Y)
		}

		if mo.Button == tea.MouseLeft {
			m.drag = &drag{kind: dragTab, v: t.v, x0: mo.X, y0: mo.Y, fine: true}
		}

		return m.showView(t.v)
	}

	if y >= m.panelH()-actH && mo.Button == tea.MouseLeft {
		m.modal = settingsModal(m)
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
		views := m.railViews(s)
		switch {
		case !click:
			return nil
		case mo.Y < len(views):
			if mo.Button == tea.MouseLeft {
				m.drag = &drag{kind: dragTab, v: views[mo.Y], x0: mo.X, y0: mo.Y}
			}

			return m.showView(views[mo.Y])

		case mo.Y == m.panelH()-1:
			return m.showView(v)
		}

		return nil
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
			m.modal = settingsModal(m)
			return nil
		}

		for _, t := range m.tabs(s) {
			if x >= t.x && x < t.x+t.w {
				if mo.Button == tea.MouseRight {
					return m.tabMenu(t.v, mo.X, mo.Y)
				}

				if mo.Button == tea.MouseLeft { // drag it to the other side
					m.drag = &drag{kind: dragTab, v: t.v, x0: mo.X, y0: mo.Y}
				}

				return m.showView(t.v)
			}
		}

		if mo.Button == tea.MouseRight {
			return m.tabMenu(v, mo.X, mo.Y)
		}

		return nil

	case y == bar:
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

	y -= bar + 1
	if m.filters[v].on {
		if y == 0 {
			if click {
				m.filters[v].editing = true
				return m.filters[v].input.Focus()
			}

			return nil
		}

		y--
	}

	if v != viewGit && click {
		m.scm.input.Blur()
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
		if y == 0 { // the tab strip
			if click && (mo.Button == tea.MouseLeft || mo.Button == tea.MouseMiddle || mo.Button == tea.MouseRight) {
				return m.termStripMouse(x, mo.Button)
			}

			return nil
		}

		return m.termPanelMouse(msg, x, y-1)

	case viewSession:
		if y == 0 { // its tabs
			if click {
				return m.sessionStripMouse(x, mo.Button)
			}

			return nil
		}

		if x == m.sessW()-1 && y > 0 { // the scrollbar
			if click && mo.Button == tea.MouseLeft {
				return m.barMouse(&m.term, "session", y-1, mo.Y)
			}

			return nil
		}

		return m.term.mouse(m, m.sess, msg, x, y-1)

	default: // Agents.
		return m.ag.mouse(m, msg, y)
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
		return m.scm.dragPane(m, d, mo.Y, release)
	case dragSelect:
		return m.pv.dragTo(m, mo.X, mo.Y, release)
	case dragTab:
		return m.dragTabTo(d, mo.X, mo.Y, release)
	case dragTerm:
		return m.dragTerm(d, mo.Y, release)
	case dragRow:
		return m.ag.dragRowTo(m, d, mo.Y, release)
	case dragScroll:
		return m.barDragTo(d.bar, mo.Y, release)
	}

	if !release {
		r := m.colRect(d.col)

		w := mo.X - r.x
		if m.side(d.col) == 1 {
			w = r.x + r.w - 1 - mo.X
		}

		if _, c := m.layout(); slices.Contains(m.colViews(d.col), viewSession) && c.w-(w-r.w) < snapMain {
			m.drag = nil // stretched nearly over the editor: it takes the editor area
			return m.undockSession()
		}

		m.setColWidth(d.col, max(20, min(w, m.w-21)))
		d.moved = true

		return nil
	}

	m.drag = nil

	if !d.moved {
		return nil
	}

	return tea.Batch(m.saveCols(), m.fetchScreen())
}

// dragTerm resizes the bottom panel by its title row, as VS Code's sash does;
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
	m.st.Settings.TermH = max(3, min(d.h0+d.y0-y, m.panelH()-3))

	return m.fetchScreen()
}

// snapMain is the editor width below which a widening session column takes
// the whole editor area instead.
const snapMain = 24

// View draws the columns side by side into the panel rows, then the status
// bar, the overlays and the cursor.
func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true

	v.MouseMode = tea.MouseModeAllMotion // hover rows and header actions
	if m.w == 0 || m.h == 0 {
		return v
	}

	cs, c := m.layout()
	b := m.bord()

	type panel struct {
		rc    rect
		lines []string
		title string
		focus int
	}

	var ps []panel

	left := leftCount(m.cols())
	for i := 0; i <= len(cs); i++ {
		if i == left {
			ps = append(ps, panel{c, m.mainLines(c.w), m.mainTitle(), onMain})
		}

		if i < len(cs) && cs[i].w > 0 {
			ps = append(ps, panel{cs[i], m.sidebar(i, cs[i].w), m.sideTitle(i), i})
		}
	}

	edge := func(p panel) lipgloss.Style {
		if m.focus == p.focus {
			return fg(pal.accent)
		}

		return dim
	}
	status, _ := m.statusLine(m.w)

	lines := make([]string, m.panelH()+1) // the panel rows, then the status bar
	for i := range m.panelH() {
		var sb strings.Builder

		for j, p := range ps {
			switch {
			case b == 1:
				sb.WriteString(edge(p).Render("│") + p.lines[i] + edge(p).Render("│"))
			case j > 0:
				owner := p.focus // a divider belongs to the sidebar column beside it
				if ps[j-1].focus != onMain && ps[j-1].focus < left {
					owner = ps[j-1].focus
				}

				sb.WriteString(m.divider(owner) + p.lines[i])

			default:
				sb.WriteString(p.lines[i])
			}
		}

		lines[i] = sb.String()
	}

	lines[len(lines)-1] = status
	if m.drag != nil && m.drag.kind == dragTab && m.drag.drop != nil {
		m.dropBox(lines[:m.panelH()], *m.drag.drop)
	}

	if box, x, y, ok := m.pv.findBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	if box, x, y, ok := m.pv.compBox(m); ok && m.modal == nil {
		m.overlayBox(lines, box, x, y)
	}

	switch {
	case m.modal != nil:
		box, x, y := m.modal.view(m)
		m.overlayBox(lines, strings.Split(box, "\n"), x, y)

	case m.focus == onPanel && m.termRows() > 1:
		if m.tv.scr.CursorVisible && m.tv.term.id == m.tv.id && m.tv.scr.CursorX >= m.tv.left {
			v.Cursor = tea.NewCursor(c.x+m.tv.scr.CursorX-m.tv.left, b+m.mainH()+1+m.tv.scr.CursorY)
		}

	case m.focus == onMain && m.showsSession():
		if m.term.scr.CursorVisible && m.term.id == m.sess {
			v.Cursor = tea.NewCursor(c.x+m.term.scr.CursorX, b+1+m.stripH()+m.term.scr.CursorY)
		}

	case m.sessFocused():
		if m.term.scr.CursorVisible && m.term.id == m.sess {
			v.Cursor = tea.NewCursor(m.colRect(m.focus).x+m.term.scr.CursorX, b+m.bodyTop(viewSession)+1+m.term.scr.CursorY)
		}

	case m.focus == onMain && m.showsPreview() && !m.pv.scrollOnly(m):
		if x, y, ok := m.pv.cursor(c.w, m.pvH()); ok {
			v.Cursor = tea.NewCursor(c.x+x, b+1+m.stripH()+y)
			if m.pv.vimOn(m) && m.pv.vim.mode == vimInsert {
				v.Cursor.Shape = tea.CursorBar // block in normal mode, a bar while inserting
			}
		}
	}

	if b == 1 {
		var top, bot strings.Builder

		for _, p := range ps {
			t := ansi.Truncate(" "+p.title+" ", max(p.rc.w-2, 0), "…")
			top.WriteString(edge(p).Render("┌─") + edge(p).Bold(m.focus == p.focus).Render(t) +
				edge(p).Render(strings.Repeat("─", max(p.rc.w-1-ansi.StringWidth(t), 0))+"┐"))
			bot.WriteString(edge(p).Render("└" + strings.Repeat("─", p.rc.w) + "┘"))
		}
		// The status bar sits under the frame, as VS Code's sits under the workbench.
		status, panels := lines[len(lines)-1], lines[:len(lines)-1]
		lines = append(append(append([]string{top.String()}, panels...), bot.String()), status)
	}

	v.SetContent(strings.Join(lines, "\n"))

	return v
}

func (m *Model) divider(i int) string {
	if m.drag != nil && m.drag.kind == dragDivider && m.drag.col == i {
		return fg(pal.accent).Render("┃")
	}

	return dim.Render("│")
}

func (m *Model) sideTitle(s int) string {
	v, _ := m.viewOn(s)
	if s := m.session(m.sess); v == viewSession && s != nil {
		return sessionName(*s)
	}

	return viewTitles[v]
}

func (m *Model) mainTitle() string {
	switch {
	case m.showsSession():
		if s := m.session(m.sess); s != nil {
			return sessionName(*s)
		}

	case m.pv.kind != "":
		name, _ := m.pv.label(m.ws)
		return name
	}

	return "pando"
}

func (m *Model) sidebar(s, w int) []string {
	if m.railed(s) {
		return m.rail(s, w)
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

	out = append(out, m.viewHeader(s, active, w))
	if f := &m.filters[active]; f.on {
		f.input.SetWidth(max(w-4, 1))
		out = append(out, fit(f.input.View(), w))
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
	case viewSession:
		out = append(out, m.sessionLines(w, h)...)
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
// color and activityBarTop.activeBorder under it, a heavy rule the row
// below, the one under the mouse a neutral line, and the settings gear sits
// on the right.
func (m *Model) activityBar(s, w int) []string {
	active, _ := m.viewOn(s)
	rc := m.colRect(s)
	// Under the mouse a chip lights up, as VS Code's activity bar does.
	hot := func(x, w int) bool {
		return m.mouseY < actH && time.Since(m.mouseAt) < actionsLinger && m.mouseX >= rc.x+x && m.mouseX < rc.x+x+w
	}
	idle := func(x, w int) lipgloss.Style {
		if hot(x, w) {
			return plain
		}

		return dim
	}

	var segs, marks []seg

	x, cx := 0, 0
	for _, t := range m.tabs(s) {
		segs = append(segs, sg(blank(t.x-x), plain))

		st := idle(t.x, t.chipW)
		if t.v == active { // the open view, in the accent color
			st = fg(pal.headerAccent).Bold(true)
		}

		if c := markColor(t.v == active, hot(t.x, t.chipW)); c != nil {
			marks = append(marks, sg(blank(t.x-cx), plain), sg(strings.Repeat("━", t.chipW), fg(c)))
			cx = t.x + t.chipW
		}

		segs = append(segs, sgOwn(t.label, st))
		if t.badge != "" {
			segs = append(segs, sg(t.badge, fg(pal.attention)))
		}

		x = t.x + t.w
	}

	gw := ansi.StringWidth(gearLabel())

	var gearMark []seg
	if hot(w-gw, gw) { // the gear is a chip too
		gearMark = []seg{sg(strings.Repeat("━", gw), fg(pal.inputBorder))}
	}

	return []string{
		row(w, nil, segs, sgOwn(gearLabel(), idle(w-gw, gw))),
		row(w, nil, marks, gearMark...),
	}
}

// markColor is the active border's color for a chip: the accent for the open
// view, a neutral line under the mouse, nothing otherwise.
func markColor(active, hovered bool) color.Color {
	switch {
	case active:
		return pal.headerAccent
	case hovered:
		return pal.inputBorder
	}

	return nil
}

// vertBar is the activity bar down a sidebar's outer edge: the top bar's
// chips stacked, one `actH`-row block each, with no fill behind them. The
// active one takes the accent color and VS Code's activityBar.activeBorder
// down the strip's outer edge, where the top bar underlines instead; the one
// under the mouse takes a neutral line. A view waiting on its agent has no
// room for a count here, so its icon takes the attention color instead.
func (m *Model) vertBar(s, w int) []string {
	active, _ := m.viewOn(s)
	rc := m.colRect(s)

	x0 := rc.x
	if m.side(s) == 1 {
		x0 = rc.x + rc.w - w
	}
	// Under the mouse a chip lights up, as the top bar's does; the whole
	// block counts, not just the icon's row.
	hot := func(y int) bool {
		return time.Since(m.mouseAt) < actionsLinger && m.mouseX >= x0 && m.mouseX < x0+w &&
			m.mouseY >= y && m.mouseY < y+actH
	}
	// The active border runs down the strip's outer edge, a quarter of a cell
	// wide; the block elements have no right quarter, so a right-docked strip
	// takes the half block, the nearest there is.
	edge, at := "▎", 0
	if m.side(s) == 1 {
		edge, at = "▐", w-1
	}

	out := make([]string, m.panelH())
	for y := range out {
		out[y] = blank(w)
	}
	// chipAt draws one chip: the icon centered on row y, the border beside it.
	chipAt := func(y int, g glyph, st lipgloss.Style, mark color.Color) {
		if y >= len(out) {
			return
		}

		icon := g.short()
		iw := ansi.StringWidth(icon)
		l := (w - iw) / 2

		var segs []seg

		for i := 0; i < w; {
			switch {
			case mark != nil && i == at:
				segs, i = append(segs, sg(edge, fg(mark))), i+1
			case i == l:
				segs, i = append(segs, sg(icon, st)), i+iw
			default:
				segs, i = append(segs, sg(" ", plain)), i+1
			}
		}

		out[y] = row(w, nil, segs)
	}

	gearRow := len(out) - actH
	for _, t := range m.vertTabs(s) {
		if t.x+actH > gearRow { // a short panel: the rest would run into the gear
			break
		}

		st := dim

		switch {
		case t.v == active:
			st = fg(pal.headerAccent).Bold(true)
		case hot(t.x):
			st = plain
		}

		if t.badge != "" && t.v != active {
			st = st.Foreground(pal.attention).Bold(true)
		}

		chipAt(t.x, viewIcon(t.v), st, markColor(t.v == active, hot(t.x)))
	}

	st := dim
	if hot(gearRow) {
		st = plain
	}

	chipAt(gearRow, icGear, st, markColor(false, hot(gearRow)))

	return out
}

// vertTabs are the same chips as tabs, in a block each down the edge: x is
// the icon's row, the rest of the block is the air under it.
func (m *Model) vertTabs(s int) []tab {
	var out []tab

	for k, v := range m.colViews(s) {
		t := tab{v: v, x: k * actH, w: actW(), chipW: actW(), label: center(viewIcon(v).short(), actW())}
		if c := m.attentionCount(); v == viewAgents && c > 0 {
			t.badge = "•"
		}

		out = append(out, t)
	}

	return out
}

// chipCell is a narrow chip's label in w cells: a space, then the icon; a
// two-cell emoji drops the space instead of overflowing.
func chipCell(label string, w int) string {
	if ansi.StringWidth(label) < w {
		label = " " + label
	}

	return fit(label, w)
}

// rail is a hidden sidebar: its view icons stacked, and a chevron at the
// bottom reopening the last view. A click on an icon opens that view.
func (m *Model) rail(s, w int) []string {
	views := m.railViews(s)

	out := make([]string, m.panelH())
	for y := range out {
		switch {
		case y < len(views):
			out[y] = row(w, nil, []seg{sg(chipCell(viewIcon(views[y]).short(), w), dim)})
		case y == len(out)-1:
			g := icOpenLeft
			if m.side(s) == 1 {
				g = icOpenRight
			}

			out[y] = row(w, nil, []seg{sg(" "+g.s(), accent)})

		default:
			out[y] = blank(w)
		}
	}

	return out
}

func (m *Model) viewHeader(s int, v view, w int) string {
	session := "SESSION"
	if s := m.session(m.sess); s != nil {
		session = sessionName(*s)
	}

	title := []string{strings.ToUpper(filepath.Base(m.ws)), "SOURCE CONTROL", "SPACES", "SEARCH", "TERMINAL", session}[v]

	var right []seg

	if acts := m.headerActions(s, v, w); len(acts) > 0 {
		for _, a := range acts {
			st := dim
			if a.hot {
				st = keycapHot()
			}

			right = append(right, sg(a.label, st))
		}
	} else if sum := m.scm.summary(); v == viewGit && sum != "" {
		right = []seg{sg(ansi.Truncate(sum, max(w-ansi.StringWidth(title)-3, 0), "…")+" ", dim)}
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
	badge := lipgloss.NewStyle().Background(pal.buttonBg).Foreground(pal.buttonFg).Render(" " + label + " ")
	pad := (w - 2 - ansi.StringWidth(badge)) / 2

	for y := y0; y < y0+h; y++ {
		var box string

		switch {
		case y == y0:
			box = st.Render("┌" + strings.Repeat("─", w-2) + "┐")
		case y == y0+h-1:
			box = st.Render("└" + strings.Repeat("─", w-2) + "┘")
		case y == y0+h/2 && t.h == 0 && label != "":
			box = st.Render("│") + blank(pad) + badge + blank(w-2-pad-ansi.StringWidth(badge)) + st.Render("│")
		case t.h == 0:
			box = st.Render("│") + blank(w-2) + st.Render("│")
		default:
			box = st.Render("│") + ansi.Cut(lines[y], t.rc.x+1, t.rc.x+w-1) + "\x1b[m" + st.Render("│")
		}

		lines[y] = ansi.Cut(lines[y], 0, t.rc.x) + "\x1b[m" + box + "\x1b[m" + ansi.Cut(lines[y], t.rc.x+w, m.w)
	}
}

// statusLine is the bottom row: the repository on the left, flash messages
// after it, agents and search on the right. VS Code's status bar.
func (m *Model) statusLine(w int) (string, []rowAction) {
	var (
		left, right []seg
		zones       []rowAction
	)
	// The branch and the project are buttons: lit under the mouse, as VS Code's
	// status bar items are.
	button := func(text string, st lipgloss.Style, run func(*Model) tea.Cmd) {
		x, w := 0, ansi.StringWidth(text)
		for _, sg := range left {
			x += ansi.StringWidth(sg.s)
		}

		seg := sg(text, st)
		if m.mouseY == m.h-1+m.bord() && m.mouseX >= x && m.mouseX < x+w {
			seg = sgOwn(text, st.Background(pal.keycapBg)) // over the bar's own background
		}

		left = append(left, seg)
		zones = append(zones, rowAction{x: x, w: w, run: run})
	}

	name := filepath.Base(m.ws)
	if root := m.scm.root(); root != "" {
		st := m.scm.status[root]

		branch := " " + icBranch.s() + " " + headLabel(st)
		if st.Ahead+st.Behind > 0 {
			branch += fmt.Sprintf(" ↑%d ↓%d", st.Ahead, st.Behind)
		}

		button(branch+" ", fg(pal.headerAccent), func(m *Model) tea.Cmd { return m.scm.branchPicker(m) })

		name = filepath.Base(root)
	}

	button(" "+name+" ", dim, func(m *Model) tea.Cmd { return m.projectPicker() })

	if m.msg != "" && time.Since(m.msgAt) < 5*time.Second {
		st := fg(pal.ok)
		if m.msgErr {
			st = fg(pal.errc)
		}

		left = append(left, sg(" "+m.msg, st))
	}
	// Each right-hand item carries the action its click runs; they are laid
	// out from the edge inwards once the list is complete.
	var runs []func(m *Model) tea.Cmd

	add := func(text string, st lipgloss.Style, run func(m *Model) tea.Cmd) {
		right = append(right, sg(text, st))
		runs = append(runs, run)
	}

	searchRun := func(m *Model) tea.Cmd { return tea.Batch(m.showView(viewSearch), m.sr.focus()) }
	if n := m.attentionCount(); n > 0 {
		add(fmt.Sprintf(" ! %d ", n), fg(pal.attention), func(m *Model) tea.Cmd { return m.agentNavigator() })
	}

	switch n := m.sr.matches(); {
	case m.sr.busy:
		add(" searching… ", dim, searchRun)
	case n > 0:
		add(" "+plural(n, "result")+" ", dim, searchRun)
	}

	add(" ^⇧p ", dim, func(m *Model) tea.Cmd { return m.commandPalette() })

	if text, st, run := m.updateChip(); text != "" {
		add(text, st, run)
	}

	x := w

	for i := len(right) - 1; i >= 0; i-- {
		rw := ansi.StringWidth(right[i].s)

		x -= rw
		if runs[i] != nil {
			zones = append(zones, rowAction{x: x, w: rw, run: runs[i]})
		}
	}

	return row(w, pal.sectionBg, left, right...), zones
}

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

		for _, s := range m.agentSessions() {
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

// pvH is the preview body height: the main area minus the editor strip, the
// header, the footer and the references widget.
// pvW is the editor's text width: the main area less the scrollbar's column.
func (m *Model) pvW() int { return max(m.mainW()-1, 1) }

func (m *Model) pvH() int { return max(m.mainH()-2-m.stripH()-m.peekH(), 1) }

// stripH is 1 when a tab strip is drawn over the main area: a session always
// has one (it carries the + that opens another), and so does any open editor.
func (m *Model) stripH() int {
	if m.showsSession() {
		return 1
	}

	return b2i(m.showsPreview() && len(m.editors) > 0)
}

// sessH is the terminal body: the main area minus its strip and header.
func (m *Model) sessH() int { return max(m.mainH()-2, 1) }

func (m *Model) mainLines(w int) []string {
	if n := m.termRows(); n > 0 { // the editor area, then the panel under it
		out := m.editorLines(w)[:m.mainH()]
		return append(out, m.termPanelLines(w, n)...)
	}

	return m.editorLines(w)
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
		header, body = m.term.view(m, w, h)

	case m.pv.kind != "":
		h = m.pvH()

		header, body, footer = m.pv.view(m, w, h)
		if m.pk != nil {
			for len(body) < h {
				body = append(body, "")
			}

			body = append(body, m.pk.view(m, w, m.peekH())...)
			h += m.peekH()
		}

	default:
		header = row(w, nil, []seg{sg(" pando", accent), sg("  "+m.ws, dim)})
		body = welcome(w, h, m.sess != "")
	}

	out := make([]string, 0, m.h)
	switch {
	case m.showsSession():
		out = append(out, m.sessionStrip(w))
	case m.stripH() > 0:
		out = append(out, m.editorStrip(w))
	}

	out = append(out, header)

	for i := range max(h, 0) {
		if i < len(body) {
			out = append(out, fit(body[i], w))
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

// welcome fills the empty main area. A session docked to a side column is on
// screen without filling main, so the first line names what is missing here —
// an editor — rather than claiming the workspace has no session.
func welcome(w, h int, sess bool) []string {
	first := "No session in this workspace."
	if sess {
		first = "No editor open."
	}

	text := []string{
		first,
		"",
		"double click or ^n   new file",
		"3  Spaces panel      n  new agent session",
		"w  new worktree      a  add project",
		"^p quick open        ^] cycle sidebar / main focus",
	}
	out := make([]string, max(h, 0))

	wide := 0
	for _, t := range text {
		wide = max(wide, lipgloss.Width(t))
	}

	top, left := max((h-len(text))/2, 0), blank((w-wide)/2)
	for i, t := range text {
		if top+i < h {
			out[top+i] = left + dim.Render(t)
		}
	}

	return out
}

var hotkeys = map[view][][2]string{
	viewFiles:  {{"↑↓", "move"}, {"←→", "fold"}, {"⏎", "open"}, {"n N", "new"}, {"R", "rename"}, {"D", "delete"}, {"s", "stage"}, {"e", "edit"}, {".", "dotfiles"}, {"^f", "filter"}},
	viewGit:    {{"↑↓", "move"}, {"⏎", "stage"}, {"o", "diff"}, {"t", "tree"}, {"c", "message"}, {"C", "commit"}, {"A", "suggest"}, {"S", "sync/publish"}, {"a u", "stage/unstage all"}, {"U", "stage untracked"}, {"O", "open file"}, {"B", "branch"}, {"d", "discard"}, {"^f", "filter"}},
	viewSearch: {{"^f /", "query"}, {"^h", "replace"}, {"r R", "replace file/all"}, {"⏎", "open"}, {"↑↓", "move"}, {"←→", "fold"}, {"M-c", "case"}, {"M-w", "word"}, {"M-r", "regex"}, {"^r", "rerun"}, {"x", "clear"}, {"C", "collapse"}},
	viewAgents: {{"↑↓", "move"}, {"M-↑↓", "reorder"}, {"⏎", "switch"}, {"n", "session"}, {"w", "worktree"}, {"a", "project"}, {"x", "kill"}, {"^f", "filter"}},
}

var globalKeys = [][2]string{{"^]", "focus"}, {"^0 ^1", "side/editor"}, {"1-4", "views"}, {"^p", "open file"}, {"^⇧p", "commands"}, {"M-t", "agents"}, {"^tab", "editors"}, {"^f", "find"}, {"^← ^→", "back/forward"}, {"[ ]", "sessions"}, {"^`", "terminal"}, {"^b", "hide"}, {"< >", "width"}, {"^,", "settings"}, {"esc q", "quit"}}

var previewKeys = [][2]string{{"↑↓", "move"}, {"⇧↑↓", "select"}, {"^a", "all"}, {"y", "copy"}, {"s", "split diff"}, {"m", "stage/revert lines"}, {"w", "wrap"}, {"^f", "find"}, {"^h", "replace"}, {"^g", "go to line"}, {"f12", "definition"}, {"⇧f12", "references"}, {"^.", "code action"}, {"e", "edit"}, {"q", "close"}}

// keyRows lays key/description pairs out in two keycap columns.
func keyRows(pairs [][2]string) []item {
	cell := func(p [2]string) string {
		return keycap(p[0]+blank(5-ansi.StringWidth(p[0]))) + " " + dim.Render(fmt.Sprintf("%-10s", p[1]))
	}

	var out []item

	for i := 0; i < len(pairs); i += 2 {
		label := cell(pairs[i])
		if i+1 < len(pairs) {
			label += " " + cell(pairs[i+1])
		}

		out = append(out, item{label: label, styled: true})
	}

	return out
}

func heading(s string) item { return item{label: bold.Render(s), styled: true} }

// settingsItems are the Settings modal's toggles.
func settingsItems(m *Model) []item {
	onOff := func(b bool) string {
		if b {
			return "on"
		}

		return "off"
	}
	s := m.st.Settings

	theme := s.Theme
	if theme == "" {
		theme = "vscode"
	}

	return []item{
		{label: "Color theme", hint: theme, run: func(m *Model) tea.Cmd {
			next := themeNames[(slices.Index(themeNames, theme)+1)%len(themeNames)]
			return m.setSettings(map[string]any{"color_theme": next})
		}},
		{label: "Diff view", hint: map[bool]string{true: "split", false: "inline"}[s.DiffView == "split"], run: func(m *Model) tea.Cmd { return m.toggleDiffView() }},
		{label: "Icons", hint: s.Icons, run: func(m *Model) tea.Cmd {
			next := iconSets[(slices.Index(iconSets, m.st.Settings.Icons)+1)%len(iconSets)]
			return m.setSettings(map[string]any{"icons": next})
		}},
		{label: "Hidden files", hint: onOff(s.Hidden), run: func(m *Model) tea.Cmd { return m.setSettings(map[string]any{"hidden": !m.st.Settings.Hidden}) }},
		{label: "Git decorations", hint: onOff(s.GitDeco), run: func(m *Model) tea.Cmd {
			return tea.Batch(m.setSettings(map[string]any{"git_deco": !m.st.Settings.GitDeco}), m.refreshGit())
		}},
		{label: "Git changes as tree", hint: onOff(s.GitTree), run: func(m *Model) tea.Cmd {
			return m.setSettings(map[string]any{"git_tree": !m.st.Settings.GitTree})
		}},
		{label: "Panel borders", hint: onOff(s.Borders), run: func(m *Model) tea.Cmd {
			return tea.Batch(m.setSettings(map[string]any{"panel_borders": !m.st.Settings.Borders}), m.fetchScreen())
		}},
		{label: "Format on save", hint: onOff(s.FmtSave), run: func(m *Model) tea.Cmd {
			return m.setSettings(map[string]any{"format_on_save": !m.st.Settings.FmtSave})
		}},
		{label: "Vim mode", hint: onOff(s.Vim), run: func(m *Model) tea.Cmd {
			return m.setSettings(map[string]any{"vim_mode": !m.st.Settings.Vim})
		}},
		{label: "Activity bar", hint: map[bool]string{true: "side", false: "top"}[m.sideBar()], run: func(m *Model) tea.Cmd {
			next := "side"
			if m.sideBar() {
				next = "top"
			}

			return tea.Batch(m.setSettings(map[string]any{"activity_bar": next}), m.fetchScreen())
		}},
	}
}

func settingsModal(m *Model) *modal {
	build := func(m *Model) []item {
		items := settingsItems(m)

		v := viewFiles
		if m.focus != onMain {
			v, _ = m.viewOn(m.focus)
		}

		items = append(items, item{}, heading("Hotkeys"))
		items = append(items, keyRows(hotkeys[v])...)

		return append(items, item{label: dim.Render("click/⏎ toggle · esc close"), styled: true})
	}
	md := newMenu("Settings", -1, 0, build(m)...)
	md.build, md.keep = build, true

	return md
}

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
		{label: "Show Explorer", hint: "1", run: show(viewFiles)},
		{label: "Show Source Control", hint: "2", run: show(viewGit)},
		{label: "Show Spaces", hint: "3", run: show(viewAgents)},
		{label: "Show Search", hint: "4", run: show(viewSearch)},
		{label: "Toggle Terminal", hint: "^`", run: func(m *Model) tea.Cmd { return m.toggleTerminal() }},
	}
	for _, it := range m.terminalItems() {
		if it.label != "Close Terminal" {
			items = append(items, it)
		}
	}

	if m.sess != "" {
		items = append(items, item{label: "Close Session View", run: func(m *Model) tea.Cmd { return m.hideSession() }})
		if m.sessDocked() {
			items = append(items, item{label: "Move Session to Editor Area", run: func(m *Model) tea.Cmd { return m.undockSession() }})
		}

		items = append(items, item{label: "Dock Session Left", run: func(m *Model) tea.Cmd { return m.splitTo(viewSession, 0) }},
			item{label: "Dock Session Right", run: func(m *Model) tea.Cmd { return m.splitTo(viewSession, 1) }})
	}

	items = append(items, []item{
		{label: "Focus Next Panel", hint: "^]", run: func(m *Model) tea.Cmd { m.cycleFocus(); return m.fetchScreen() }},
		{label: "Focus Sidebar", hint: "^0", run: func(m *Model) tea.Cmd { return m.focusSidebar() }},
		{label: "Toggle Sidebars", hint: "^b", run: func(m *Model) tea.Cmd { return m.toggleSidebars() }},
		{label: "Open Settings", hint: "^,", run: func(m *Model) tea.Cmd { m.modal = settingsModal(m); return nil }},
		{label: "Keyboard Shortcuts", hint: "?", run: func(m *Model) tea.Cmd { m.modal = helpModal(); return nil }},
		{label: "Quit", hint: "q", run: func(m *Model) tea.Cmd { return m.confirmQuit() }},
	}...)
	if m.pv.kind != "" || m.sess != "" {
		items = append(items, item{label: "Focus Editor", hint: "^1", run: func(m *Model) tea.Cmd { m.focus = onMain; return m.fetchScreen() }})
	}

	if m.pv.kind != "" {
		items = append(items,
			item{label: "Go Back", hint: "M-,", run: func(m *Model) tea.Cmd { return m.navGo(-1) }},
			item{label: "Go Forward", hint: "M-.", run: func(m *Model) tea.Cmd { return m.navGo(1) }},
			item{label: "Next Editor", hint: "^tab", run: func(m *Model) tea.Cmd { return m.cycleEditor(1) }},
			item{label: "Previous Editor", hint: "^⇧tab", run: func(m *Model) tea.Cmd { return m.cycleEditor(-1) }},
			item{label: "Close Editor", hint: "^w", run: func(m *Model) tea.Cmd { return m.closeEditor(m.edIdx) }},
			item{label: "Close All Editors", run: func(m *Model) tea.Cmd { return m.closeEditors() }})
	}

	if len(m.editors) > 1 {
		items = append(items, item{label: "Close Other Editors", run: func(m *Model) tea.Cmd { return m.closeOtherEditors() }})
	}

	return items
}

// commands are every command available now, named "Category: Command"; the
// palette, the context menus and the [keys] config all read this one list.
func (m *Model) commands() []item {
	var items []item

	add := func(category string, its []item) {
		for _, it := range its {
			if it.run == nil || it.styled {
				continue
			}

			label, run := category+": "+it.label, it.run
			it.label, it.detail = label, ""
			it.run = func(m *Model) tea.Cmd { m.lastCommand = label; return run(m) }
			items = append(items, it)
		}
	}
	add("View", m.viewItems())
	add("Explorer", m.ex.items(m))
	add("Git", m.scm.items(m))
	add("Agents", m.ag.items(m))
	add("Search", m.sr.items(m))

	if m.showsPreview() {
		add("Editor", append(m.pv.items(m), m.pv.keyItems(m)...))
	}

	add("Preferences", settingsItems(m))
	add("Pando", m.updateItems())

	return items
}

// overlayBox draws a box over the screen's lines at x, y.
func (m *Model) overlayBox(lines, box []string, x, y int) {
	for i, bl := range box {
		if y+i < len(lines) {
			ln := lines[y+i]
			bw := ansi.StringWidth(bl)
			lines[y+i] = ansi.Cut(ln, 0, x) + "\x1b[m" + bl + "\x1b[m" + ansi.Cut(ln, x+bw, m.w)
		}
	}
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
	return m.scm.input.Focused() || m.sr.editing() || m.pv.find.editing ||
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
// accident. ⌃c still quits at once.
func (m *Model) confirmQuit() tea.Cmd {
	title := "Close pando?"
	if n := m.dirtyEditors(); n > 0 { // unsaved text would go with it
		title = fmt.Sprintf("Close pando? %s unsaved", plural(n, "file"))
	}

	m.modal = newMenu(title, -1, 0,
		item{label: "Close", hint: "unsaved text is kept", run: func(m *Model) tea.Cmd {
			return tea.Sequence(m.saveEditors(), m.saveDrafts(), tea.Quit)
		}},
		item{label: "Save all and close", hint: "", run: func(m *Model) tea.Cmd {
			m.saveAll()
			return tea.Sequence(m.saveEditors(), m.saveDrafts(), tea.Quit)
		}},
		cancelItem())

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

	m.pv.top = 0

	return m.setSettings(map[string]any{"diff_view": next})
}

func staleModal(sessions int) *modal {
	return newMenu("The pando daemon runs an older build", -1, 0,
		item{label: fmt.Sprintf("Restart it now (%d sessions restart, their scrollback is lost)", sessions), run: func(*Model) tea.Cmd {
			return func() tea.Msg {
				if err := proto.RestartDaemon(); err != nil {
					return flashMsg{"restart: " + err.Error(), true}
				}

				return flashMsg{"daemon restarted", false}
			}
		}},
		item{label: "Keep it (layout and tree settings will not be saved)", run: func(*Model) tea.Cmd {
			return flash("old daemon kept · pando stop restarts it", true)
		}})
}

func helpModal() *modal {
	items := []item{heading("Everywhere")}

	items = append(items, keyRows(globalKeys)...)
	for _, v := range []view{viewFiles, viewGit, viewSearch, viewAgents} {
		items = append(items, item{}, heading(viewTitles[v]))
		items = append(items, keyRows(hotkeys[v])...)
	}

	items = append(items, item{}, heading("Preview"))
	items = append(items, keyRows(previewKeys)...)
	items = append(items, item{}, item{label: dim.Render("drag a tab or a title to either side to dock it · right click it for more · drag │ to resize"), styled: true})

	return newMenu("Keys", -1, 0, items...)
}
