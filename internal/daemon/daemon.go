// Package daemon owns pando's shared state: projects, worktrees, agent
// sessions. TUI and CLI clients talk to it over proto's unix socket.
package daemon

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
	"github.com/xseman/pando/internal/update"
)

// Daemon holds the shared state and serves it over the unix socket.
type Daemon struct {
	statePath string
	cfgPath   string
	dataDir   string

	mu       sync.Mutex
	state    proto.State
	cfgMod   time.Time // config.toml mtime last read or written
	cfgErr   error     // config.toml does not parse: keep it, refuse to overwrite it
	sessions map[string]*session
	order    []string
	subs     map[chan proto.Event]struct{}
	pending  map[string]bool // screen events debounced per session
	ln       net.Listener
	closing  bool           // shutting down: exits are kills, keep the specs for respawn
	upd      proto.Update   // what the last update check found
	rel      update.Release // the release it would install

	// ctx ends when Close does: it stops the update watcher and cancels a
	// download in flight.
	ctx    context.Context
	cancel context.CancelFunc
}

// ConfigDir is where config.toml and state.json live.
func ConfigDir() string {
	if d := os.Getenv("PANDO_CONFIG_DIR"); d != "" {
		return d
	}

	d, _ := os.UserConfigDir()

	return filepath.Join(d, "pando")
}

// DataDir is where drafts and other per-workspace data live.
func DataDir() string {
	if d := os.Getenv("PANDO_DATA_DIR"); d != "" {
		return d
	}

	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "pando")
	}

	h, _ := os.UserHomeDir()

	return filepath.Join(h, ".local", "share", "pando")
}

// New loads config and state and respawns persisted sessions.
func New(configDir, dataDir string) (*Daemon, error) {
	proto.BuildID() // before a rebuild replaces the executable

	d := &Daemon{
		statePath: filepath.Join(configDir, "state.json"), cfgPath: filepath.Join(configDir, "config.toml"),
		dataDir: dataDir, sessions: map[string]*session{},
		subs: map[chan proto.Event]struct{}{}, pending: map[string]bool{},
		upd: proto.Update{Current: update.Version},
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	if b, err := os.ReadFile(d.statePath); err == nil {
		if err := json.Unmarshal(b, &d.state); err != nil {
			return nil, fmt.Errorf("%s: %w", d.statePath, err)
		}
	}

	if d.state.Drafts == nil {
		d.state.Drafts = map[string]string{}
	}

	if d.state.Editors == nil {
		d.state.Editors = map[string]proto.Editors{}
	}

	c, err := loadConfig(d.cfgPath)

	d.state.Settings, d.state.Agents, d.state.Resume, d.state.ResumeID, d.state.ResumeJob = c.Settings, c.Agents, c.Resume, c.ResumeID, c.ResumeJob
	if errors.Is(err, os.ErrNotExist) {
		if err := d.saveConfig(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	} else {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v; using defaults until it is fixed\n", err)
			d.cfgErr = err
		}

		d.cfgMod = modTime(d.cfgPath)
	}

	resumed := map[string]string{}

	for _, spec := range d.state.Sessions {
		if st, err := os.Stat(spec.Workspace); err != nil || !st.IsDir() || len(spec.Cmd) == 0 {
			continue
		}

		start := d.start
		if isShellAgent(spec.Agent) { // the shell it ran first, then what a new one would open
			shells := append([][]string{spec.Cmd}, d.shellCandidates(spec.Agent)...)
			start = func(spec proto.SessionSpec, cols, rows int) (*session, error) {
				return d.startShell(spec, cols, rows, shells)
			}
		}

		s, err := start(spec, 0, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "respawn", spec.ID, err)
			continue
		}

		d.resume(s, resumed)
	}

	return d, nil
}

// remember records what would bring session s's agent back after a restart:
// the conversation process pid of program prog has open, when the agent says
// which, else its latest. A process that only attaches to a background job
// records the job, to attach to again. It reports a change worth saving;
// d.mu is held.
func (d *Daemon) remember(s *session, pid int, prog string) bool {
	if src, t := conversations[prog], d.state.ResumeID[prog]; src != nil && len(t) > 0 && pid > 0 {
		if id, job := src.open(pid); id != "" {
			c := &proto.Conversation{Agent: prog, ID: id, Job: job, Env: s.ownEnv(src.env(pid))}

			argv := withID(t, id)
			if jt := d.state.ResumeJob[prog]; job != "" && len(jt) > 0 {
				argv = withID(jt, job)
			}

			return s.setResume(withEnv(c.Env, argv), c)
		}
	}

	return s.setResume(d.state.Resume[prog], nil)
}

// resume brings a respawned session's agent back, each conversation once:
// one that an earlier session in by already continues, or that a process
// outside pando has open, stays where it is and s says so instead. A
// background job still running is attached to again; what is left of the
// session before the restart is stopped first; a conversation with nothing
// in it yet starts the agent afresh, as its [agents] preset.
func (d *Daemon) resume(s *session, by map[string]string) {
	spec := s.spec

	c := spec.Conversation
	if c == nil {
		s.resumeWith(spec.Resume, 0)
		return
	}

	key := c.Agent + "/" + c.ID
	if other, ok := by[key]; ok {
		s.notice(fmt.Sprintf("not resumed, session %s continues this conversation", other))
		return
	}

	src := conversations[c.Agent]
	if src == nil {
		s.resumeWith(spec.Resume, 0)
		return
	}

	var leftover int

	argv := spec.Resume

	switch pid, job := src.holder(*c); {
	case job != "": // a background job: its daemon kept it through the restart
		if jt := d.state.ResumeJob[c.Agent]; len(jt) > 0 {
			argv = withEnv(c.Env, withID(jt, job))
		}

		by[key] = cmp.Or(spec.Name, spec.ID)

		s.resumeWith(argv, 0)

		return

	case pid > 0 && !d.ours(pid):
		s.notice(fmt.Sprintf("not resumed, process %d has this conversation open", pid),
			"once it is closed: "+strings.Join(spec.Resume, " "))

		return

	case pid > 0:
		leftover = pid
	}

	if t := d.state.ResumeID[c.Agent]; c.Job != "" && len(t) > 0 { // the job it was attached to ended
		argv = withEnv(c.Env, withID(t, c.ID))
	}

	if !src.saved(*c) {
		argv = withEnv(c.Env, d.state.Agents[c.Agent]) // nothing said yet: start it afresh, in its config
	} else {
		by[key] = cmp.Or(spec.Name, spec.ID)
	}

	s.resumeWith(argv, leftover)
}

// ours reports whether process pid is a leftover of this daemon's sessions:
// it ran in one under this runtime directory, and either that session is one
// the daemon respawns, or the process lost its terminal - a pty only a daemon
// that died held, whatever became of the session since.
func (d *Daemon) ours(pid int) bool {
	id := envOf(pid, "PANDO_SESSION")
	if envOf(pid, "PANDO_RUNTIME_DIR") != proto.Dir() || id == "" {
		return false
	}

	return !hasTerminal(pid) || slices.ContainsFunc(d.state.Sessions, func(s proto.SessionSpec) bool { return s.ID == id })
}

// Serve accepts clients until Close; it also drives the status ticker.
func (d *Daemon) Serve(ln net.Listener) error {
	d.ln = ln

	t := time.NewTicker(500 * time.Millisecond)

	defer t.Stop()
	go func() {
		for now := range t.C {
			d.mu.Lock()
			changed, dirty := false, false

			for _, s := range d.sessions {
				if d.closing { // a killed agent is not a left one: keep its resume
					break
				}

				pid, prog := s.foreground()
				changed = s.tick(now) || changed
				changed = s.setProgram(prog) || changed
				dirty = d.remember(s, pid, prog) || dirty
			}

			if dirty {
				_ = d.save() // best effort, it is written again next tick
			}
			d.mu.Unlock()

			if changed {
				d.broadcast(proto.Event{Kind: "sessions"})
			}

			d.reloadConfig()
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}

			return err
		}

		go d.handle(conn)
	}
}

// Close stops accepting and kills all session processes (specs stay saved).
func (d *Daemon) Close() {
	if d.ln != nil {
		_ = d.ln.Close() // unblocks Accept; a second Close is harmless
	}

	d.cancel()
	d.mu.Lock()
	d.closing = true

	ss := make([]*session, 0, len(d.sessions))
	for _, s := range d.sessions {
		ss = append(ss, s)
	}
	d.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range ss {
		wg.Go(s.kill)
	}

	wg.Wait()
}

func (d *Daemon) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var req proto.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	enc := json.NewEncoder(conn)

	if req.Method == "subscribe" {
		ch := make(chan proto.Event, 256)

		d.mu.Lock()
		d.subs[ch] = struct{}{}

		d.mu.Unlock()
		defer func() {
			d.mu.Lock()
			if _, ok := d.subs[ch]; ok {
				delete(d.subs, ch)
				close(ch)
			}
			d.mu.Unlock()
		}()

		go func() { // detect client hang-up
			buf := make([]byte, 1)
			_, _ = conn.Read(buf) // any result means the client is gone
			_ = conn.Close()
		}()

		for ev := range ch {
			if enc.Encode(ev) != nil {
				return
			}
		}

		return
	}

	result, err := d.dispatch(req.Method, req.Params)

	var resp proto.Response
	if err != nil {
		resp.Error = err.Error()
	} else if resp.Result, err = json.Marshal(result); err != nil {
		resp.Error = err.Error()
	}

	_ = enc.Encode(resp) // the client may already have hung up

	if req.Method == "shutdown" {
		go d.Close()
	}
}

func (d *Daemon) broadcast(ev proto.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range d.subs {
		select {
		case ch <- ev:
		default: // slow client: drop it, it resyncs on reconnect
			delete(d.subs, ch)
			close(ch)
		}
	}
}

func parse[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}

	return v, json.Unmarshal(raw, &v)
}

type pathParams struct {
	Path string `json:"path"`
}

// dispatch answers one request. A nil result is not "nothing found": it is a
// method that has nothing to say, and handle marshals it to the JSON null the
// notification-style methods all reply with. Failure is the error alone.
func (d *Daemon) dispatch(method string, raw json.RawMessage) (any, error) {
	switch method {
	case "ping":
		return map[string]string{"build": proto.BuildID()}, nil
	case "shutdown":
		return "bye", nil
	case "update.status":
		return d.updateStatus(), nil
	case "update.check":
		if err := d.checkUpdate(); err != nil {
			return nil, err
		}

		return d.updateStatus(), nil

	case "update.install":
		if err := d.installUpdate(); err != nil {
			return nil, err
		}

		return d.updateStatus(), nil

	case "state.get":
		// Encoded here, under the lock: State carries live maps another
		// connection writes, and handle marshals after dispatch has returned.
		d.mu.Lock()
		defer d.mu.Unlock()

		d.state.Sessions = d.specs()
		b, err := json.Marshal(d.state)

		return json.RawMessage(b), err

	case "state.set":
		return d.setState(raw)
	case "draft.list":
		p, err := parse[struct {
			WS string `json:"ws"`
		}](raw)
		if err != nil {
			return nil, err
		}

		return d.listDrafts(p.WS)

	case "draft.set":
		// No broadcast: drafts are not part of State, and a state event per
		// keystroke would have every attached TUI re-read it.
		p, err := parse[proto.Draft](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.setDraft(p)

	case "project.add":
		p, err := parse[pathParams](raw)
		if err != nil {
			return nil, err
		}

		return d.addProject(p.Path)

	case "project.remove":
		p, err := parse[pathParams](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.removeProject(p.Path)

	case "project.move":
		p, err := parse[proto.MoveParams](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.moveProject(p.Path, p.To)

	case "workspace.list":
		d.mu.Lock()
		projects := slices.Clone(d.state.Projects)
		d.mu.Unlock()

		return workspaces(projects), nil

	case "workspace.new":
		p, err := parse[struct{ Project, Branch string }](raw)
		if err != nil {
			return nil, err
		}

		return d.newWorkspace(p.Project, p.Branch)

	case "workspace.remove":
		p, err := parse[pathParams](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.removeWorkspace(p.Path)

	case "focus":
		p, err := parse[proto.FocusParams](raw)
		if err != nil {
			return nil, err
		}

		if p.Session != "" { // a name or a prefix: the TUIs match on the id
			s, err := d.session(p.Session)
			if err != nil {
				return nil, err
			}

			p.Session = s.info().ID
		}

		data, _ := json.Marshal(p) // FocusParams is plain strings and ints
		d.broadcast(proto.Event{Kind: "focus", Data: data})

		return nil, nil //nolint:nilnil // focus only broadcasts: no result, and nothing that can fail.

	case "session.new":
		p, err := parse[struct {
			Workspace, Agent, Name, Parent, FG, BG string
			Cmd                                    []string
			Cols, Rows                             int
		}](raw)
		if err != nil {
			return nil, err
		}

		spec := proto.SessionSpec{Workspace: p.Workspace, Agent: p.Agent, Name: strings.TrimSpace(p.Name), Parent: p.Parent, Cmd: p.Cmd, FG: p.FG, BG: p.BG}

		return d.newSession(spec, p.Cols, p.Rows)

	case "session.list":
		d.mu.Lock()
		defer d.mu.Unlock()

		out := make([]proto.Session, 0, len(d.order))
		for _, id := range d.order {
			out = append(out, d.sessions[id].info())
		}

		return out, nil

	case "session.get":
		p, err := parse[struct{ ID string }](raw)
		if err != nil {
			return nil, err
		}

		s, err := d.session(p.ID)
		if err != nil {
			return nil, err
		}

		return s.info(), nil

	case "session.kill":
		p, err := parse[struct{ ID string }](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.killSession(p.ID)

	case "session.rename":
		p, err := parse[struct{ ID, Name string }](raw)
		if err != nil {
			return nil, err
		}

		return nil, d.renameSession(p.ID, strings.TrimSpace(p.Name))

	case "session.input":
		p, err := parse[proto.InputParams](raw)
		if err != nil {
			return nil, err
		}

		s, err := d.session(p.ID)
		if err != nil {
			return nil, err
		}

		return nil, s.input(p)

	case "session.screen":
		p, err := parse[proto.ScreenParams](raw)
		if err != nil {
			return nil, err
		}

		s, err := d.session(p.ID)
		if err != nil {
			return nil, err
		}

		return s.screen(p), nil

	case "session.read":
		p, err := parse[proto.ReadParams](raw)
		if err != nil {
			return nil, err
		}

		s, err := d.session(p.ID)
		if err != nil {
			return nil, err
		}

		return s.text(p.Scrollback, p.Lines), nil

	case "session.wait":
		p, err := parse[proto.WaitParams](raw)
		if err != nil {
			return nil, err
		}

		return d.waitSession(p)
	}

	return nil, fmt.Errorf("unknown method %q", method)
}

// specs must be called with d.mu held.
func (d *Daemon) specs() []proto.SessionSpec {
	out := make([]proto.SessionSpec, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.sessions[id].spec)
	}

	return out
}

// save writes state.json; it must be called with d.mu held.
func (d *Daemon) save() error {
	d.state.Sessions = d.specs()

	b, err := json.MarshalIndent(struct {
		Projects []string                 `json:"projects"`
		Drafts   map[string]string        `json:"drafts"`
		Editors  map[string]proto.Editors `json:"editors"`
		Sessions []proto.SessionSpec      `json:"sessions"`
		Last     string                   `json:"last_workspace,omitempty"`
	}{d.state.Projects, d.state.Drafts, d.state.Editors, d.state.Sessions, d.state.LastWorkspace}, "", "  ")
	if err != nil {
		return err
	}

	return writeFile(d.statePath, b)
}

// saveConfig writes config.toml; it must be called with d.mu held.
func (d *Daemon) saveConfig() error {
	if d.cfgErr != nil {
		return fmt.Errorf("not saved, fix it first: %w", d.cfgErr)
	}

	c := config{d.state.Settings, d.state.Agents, d.state.Resume, d.state.ResumeID, d.state.ResumeJob}
	if err := writeFile(d.cfgPath, c.encode()); err != nil {
		return err
	}

	d.cfgMod = modTime(d.cfgPath)

	return nil
}

// reloadConfig picks up edits to config.toml made outside pando.
func (d *Daemon) reloadConfig() {
	mod := modTime(d.cfgPath)
	d.mu.Lock()
	if mod.IsZero() || mod.Equal(d.cfgMod) {
		d.mu.Unlock()
		return
	}

	d.cfgMod = mod

	c, err := loadConfig(d.cfgPath)
	if d.cfgErr = err; err != nil {
		d.mu.Unlock()
		fmt.Fprintln(os.Stderr, err)

		return
	}

	d.state.Settings, d.state.Agents, d.state.Resume, d.state.ResumeID, d.state.ResumeJob = c.Settings, c.Agents, c.Resume, c.ResumeID, c.ResumeJob
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "state"})
}

func modTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}

	return time.Time{}
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func (d *Daemon) setState(raw json.RawMessage) (any, error) {
	var p struct {
		Settings json.RawMessage           `json:"settings"`
		Agents   map[string][]string       `json:"agents"`
		Drafts   map[string]string         `json:"drafts"`
		Editors  map[string]*proto.Editors `json:"editors"` // null or no open files forgets the workspace
		Last     *string                   `json:"last_workspace"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}

	d.mu.Lock()

	var err error

	if p.Settings != nil {
		// Onto the current values, so maps like git_panes keep the keys the
		// patch omits. Every map is cloned first: the decoder merges into the
		// map it is handed, and a reader may still hold that one.
		dec := json.NewDecoder(bytes.NewReader(p.Settings))
		dec.DisallowUnknownFields()

		next := d.state.Settings
		next.GitPanes = maps.Clone(next.GitPanes)
		next.LSP = maps.Clone(next.LSP)
		next.Format = maps.Clone(next.Format)
		next.Keys = maps.Clone(next.Keys)

		next.Colors = maps.Clone(next.Colors)
		if err = dec.Decode(&next); err != nil {
			d.mu.Unlock()
			return nil, fmt.Errorf("settings: %w", err)
		}

		d.state.Settings = next
	}

	if d.state.Agents == nil {
		d.state.Agents = map[string][]string{}
	}

	for k, v := range p.Agents {
		if len(v) == 0 {
			delete(d.state.Agents, k)
		} else {
			d.state.Agents[k] = v
		}
	}

	for k, v := range p.Drafts {
		if v == "" {
			delete(d.state.Drafts, k)
		} else {
			d.state.Drafts[k] = v
		}
	}

	for k, v := range p.Editors {
		if v == nil || len(v.Open) == 0 {
			delete(d.state.Editors, k)
		} else {
			d.state.Editors[k] = *v
		}
	}

	if p.Settings != nil || p.Agents != nil {
		err = d.saveConfig()
	}

	if p.Last != nil {
		d.state.LastWorkspace = *p.Last
	}

	if p.Drafts != nil || p.Editors != nil || p.Last != nil {
		err = errors.Join(err, d.save())
	}
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "state"})

	return nil, err
}

func (d *Daemon) addProject(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}

	if root, err := git.MainRoot(abs); err == nil {
		abs = root
	}

	d.mu.Lock()
	if !slices.Contains(d.state.Projects, abs) {
		d.state.Projects = append(d.state.Projects, abs)
		err = d.save()
	}
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "workspaces"})

	return abs, err
}

func (d *Daemon) removeProject(path string) error {
	for _, w := range workspaces([]string{path}) {
		if d.hasSessions(w.Path) {
			return fmt.Errorf("kill the sessions in %s first", w.Path)
		}
	}

	d.mu.Lock()
	d.state.Projects = slices.DeleteFunc(d.state.Projects, func(p string) bool { return p == path })
	err := d.save()
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "workspaces"})

	return err
}

// moveProject puts path at index to, the order the Spaces tree and the
// project switcher list projects in. An index outside the list is clamped,
// and an order that does not change is not saved.
func (d *Daemon) moveProject(path string, to int) error {
	d.mu.Lock()

	i := slices.Index(d.state.Projects, path)
	if i < 0 {
		d.mu.Unlock()
		return fmt.Errorf("%s is not a project", path)
	}

	to = max(min(to, len(d.state.Projects)-1), 0)
	if to == i {
		d.mu.Unlock()
		return nil
	}

	rest := slices.Delete(slices.Clone(d.state.Projects), i, i+1)
	d.state.Projects = slices.Insert(rest, to, path)
	err := d.save()
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "workspaces"})

	return err
}

// workspaces lists every project's worktrees; a non-git project is one workspace.
func workspaces(projects []string) []proto.Workspace {
	out := []proto.Workspace{}

	for _, p := range projects {
		wts, err := git.Worktrees(p)
		if err != nil || len(wts) == 0 {
			out = append(out, proto.Workspace{Path: p, Project: p, Main: true})
			continue
		}

		for i, w := range wts {
			out = append(out, proto.Workspace{Path: w.Path, Project: p, Branch: w.Branch, Main: i == 0})
		}
	}

	return out
}

var unsafeBranch = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (d *Daemon) newWorkspace(project, branch string) (proto.Workspace, error) {
	if branch == "" {
		branch = git.RandomBranch()
	}

	path := filepath.Join(d.dataDir, "worktrees", filepath.Base(project), unsafeBranch.ReplaceAllString(branch, "-"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return proto.Workspace{}, err
	}

	if err := git.AddWorktree(project, path, branch); err != nil {
		return proto.Workspace{}, err
	}

	d.broadcast(proto.Event{Kind: "workspaces"})

	return proto.Workspace{Path: path, Project: project, Branch: branch}, nil
}

func (d *Daemon) removeWorkspace(path string) error {
	d.mu.Lock()
	projects := slices.Clone(d.state.Projects)
	d.mu.Unlock()

	for _, w := range workspaces(projects) {
		if w.Path != path {
			continue
		}

		if w.Main {
			return errors.New("cannot remove a project's main worktree")
		}

		if d.hasSessions(path) {
			return errors.New("kill the sessions in this workspace first")
		}

		if err := git.RemoveWorktree(w.Project, path); err != nil {
			return err
		}

		d.broadcast(proto.Event{Kind: "workspaces"})

		return nil
	}

	return fmt.Errorf("unknown workspace %s", path)
}

func (d *Daemon) hasSessions(ws string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, s := range d.sessions {
		if s.spec.Workspace == ws {
			return true
		}
	}

	return false
}

// renameSession gives a session's tab a name of its own, kept across restarts;
// an empty name drops it.
// nameTaken reports whether a live session other than except already answers
// to name. Names are handles session() resolves by, so they stay unique.
func (d *Daemon) nameTaken(name string, except *session) bool {
	if name == "" {
		return false
	}

	for _, s := range d.sessions {
		if s != except && s.name() == name {
			return true
		}
	}

	return false
}

func (d *Daemon) renameSession(id, name string) error {
	s, err := d.session(id)
	if err != nil {
		return err
	}

	d.mu.Lock()
	if d.nameTaken(name, s) {
		d.mu.Unlock()
		return fmt.Errorf("another session is named %q", name)
	}

	s.mu.Lock()
	s.spec.Name = name
	s.mu.Unlock()

	err = d.save()
	d.mu.Unlock()
	d.broadcast(proto.Event{Kind: "sessions"})

	return err
}

// session resolves what a caller named: an id, the name a rename gave the
// session, or an unambiguous prefix of either. Two matches are an error, not
// a guess, so a script never drives the wrong agent.
func (d *Daemon) session(id string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if s, ok := d.sessions[id]; ok {
		return s, nil
	}

	if id == "" {
		return nil, errors.New("no session given")
	}

	var (
		hit     *session
		matches []string
	)

	for _, sid := range d.order {
		s := d.sessions[sid]
		if s == nil {
			continue
		}

		name := s.name()
		if name == id {
			return s, nil
		}

		if strings.HasPrefix(sid, id) || (name != "" && strings.HasPrefix(name, id)) {
			hit, matches = s, append(matches, sid)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("unknown session %q", id)
	case 1:
		return hit, nil
	}

	return nil, fmt.Errorf("session %q is ambiguous: %s", id, strings.Join(matches, " "))
}

// statuses are the values session.wait accepts and session.info reports.
var statuses = []string{"idle", "blocked", "running", "exited"}

// waitSession blocks until the session reaches one of the statuses in Until,
// its screen matches Match, or the timeout passes. It polls at half the rate
// the daemon recomputes status, so a caller pays one goroutine, not a call
// per frame.
func (d *Daemon) waitSession(p proto.WaitParams) (proto.Session, error) {
	s, err := d.session(p.ID)
	if err != nil {
		return proto.Session{}, err
	}

	until := p.Until
	if len(until) == 0 && p.Match == "" {
		until = proto.Settled
	}

	for _, u := range until {
		if !slices.Contains(statuses, u) {
			return proto.Session{}, fmt.Errorf("unknown status %q, want one of: %s", u, strings.Join(statuses, " "))
		}
	}

	var re *regexp.Regexp
	if p.Match != "" {
		if re, err = regexp.Compile(p.Match); err != nil {
			return proto.Session{}, fmt.Errorf("match: %w", err)
		}
	}

	var timeout <-chan time.Time

	if p.Timeout > 0 {
		t := time.NewTimer(time.Duration(p.Timeout) * time.Millisecond)
		defer t.Stop()

		timeout = t.C
	}

	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()

	for {
		info := s.info()
		if slices.Contains(until, info.Status) || (re != nil && re.MatchString(s.text(false, 0))) {
			return info, nil
		}

		select {
		case <-timeout:
			return info, fmt.Errorf("timeout waiting for session %q, status %s", info.ID, info.Status)
		case <-d.ctx.Done():
			return info, errors.New("the daemon is shutting down")
		case <-poll.C:
		}
	}
}

func (d *Daemon) newSession(spec proto.SessionSpec, cols, rows int) (proto.Session, error) {
	abs, err := filepath.Abs(spec.Workspace)
	if err != nil {
		return proto.Session{}, err
	}

	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return proto.Session{}, fmt.Errorf("workspace %s is not a directory", abs)
	}

	spec.Workspace = abs

	var shells [][]string // a shell with nothing given opens the shell setting, falling back on bash

	d.mu.Lock()
	if len(spec.Cmd) == 0 && isShellAgent(spec.Agent) {
		shells = d.shellCandidates(spec.Agent)
		spec.Cmd = shells[0]
	}

	if len(spec.Cmd) == 0 {
		spec.Cmd = d.state.Agents[spec.Agent]
	}
	d.mu.Unlock()

	if len(spec.Cmd) == 0 {
		return proto.Session{}, fmt.Errorf("unknown agent %q and no cmd given", spec.Agent)
	}

	if spec.Agent == "" {
		spec.Agent = filepath.Base(spec.Cmd[0])
	}

	d.mu.Lock()
	taken := d.nameTaken(spec.Name, nil)
	d.mu.Unlock()

	if taken {
		return proto.Session{}, fmt.Errorf("another session is named %q", spec.Name)
	}

	if spec.Parent != "" { // a name or a prefix resolves to the id it is kept by
		parent, err := d.session(spec.Parent)
		if err != nil {
			return proto.Session{}, fmt.Errorf("parent: %w", err)
		}

		spec.Parent = parent.info().ID
	}

	b := make([]byte, 3)
	rand.Read(b)
	spec.ID = hex.EncodeToString(b)
	spec.Created = time.Now()

	start := d.start
	if shells != nil {
		start = func(spec proto.SessionSpec, cols, rows int) (*session, error) {
			return d.startShell(spec, cols, rows, shells)
		}
	}

	s, err := start(spec, cols, rows)
	if err != nil {
		return proto.Session{}, err
	}

	d.mu.Lock()
	err = d.save()
	d.mu.Unlock()
	// s, not d.sessions[spec.ID]: a process that has already exited is gone
	// from the map again, and the nil session would take the daemon down.
	info := s.info()

	d.broadcast(proto.Event{Kind: "sessions"})

	return info, err
}

// start spawns a session and registers it. It returns the session it made, not
// the map entry: a short-lived process may already have removed itself.
func (d *Daemon) start(spec proto.SessionSpec, cols, rows int) (*session, error) {
	return d.startWith(spec, cols, rows, nil)
}

// startWith is start for a shell with fallback left to try: failing as soon
// as it starts, it is replaced by the first of them (fallBack).
func (d *Daemon) startWith(spec proto.SessionSpec, cols, rows int, fallback [][]string) (*session, error) {
	if cols <= 0 || rows <= 0 {
		cols, rows = 120, 40
	}

	id := spec.ID

	s, err := spawn(spec, cols, rows, func() {
		d.mu.Lock()
		if d.pending[id] {
			d.mu.Unlock()
			return
		}

		d.pending[id] = true
		d.mu.Unlock()
		time.AfterFunc(16*time.Millisecond, func() {
			d.mu.Lock()
			d.pending[id] = false
			d.mu.Unlock()
			d.broadcast(proto.Event{Kind: "screen", ID: id})
		})
	}, func() {
		// A clean exit closes the session; a failure stays listed with its code.
		d.mu.Lock()
		s, closing := d.sessions[id], d.closing
		d.mu.Unlock()
		// ponytail: a process exiting before start() registers it stays listed as exited.
		if !closing && s != nil && s.failedAtOnce() {
			go d.fallBack(id)
			return
		}

		if !closing && s != nil && s.info().ExitCode == 0 {
			_ = d.killSession(id) // only fails if it is already gone
			return
		}

		d.broadcast(proto.Event{Kind: "sessions"})
	})
	if err != nil {
		return nil, err
	}

	s.fallback, s.started = fallback, time.Now() // before it is registered: the exit callback reads them

	d.mu.Lock()
	d.sessions[id] = s
	d.order = append(d.order, id)
	d.mu.Unlock()

	return s, nil
}

func (d *Daemon) killSession(id string) error {
	s, err := d.session(id)
	if err != nil {
		return err
	}

	s.kill()
	d.mu.Lock()
	_, present := d.sessions[id]
	delete(d.sessions, id)

	d.order = slices.DeleteFunc(d.order, func(x string) bool { return x == id })
	if present {
		err = d.save()
	}
	// Its Terminal panel's shells go with it: a shell with no session to show
	// it in has no way to be reached, and a restart would respawn it anyway.
	var children []string

	for _, cid := range d.order {
		if d.sessions[cid].info().Parent == id {
			children = append(children, cid)
		}
	}
	d.mu.Unlock()

	if present {
		d.broadcast(proto.Event{Kind: "sessions"})
	}

	for _, cid := range children {
		_ = d.killSession(cid) // only fails if it is already gone
	}

	return err
}
