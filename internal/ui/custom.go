package ui

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/xseman/pando/internal/proto"
)

// custTTL is how long a scan stands before the view, on screen, scans again:
// an agent may have installed something since.
const custTTL = 10 * time.Second

// mcpNote says what the MCP section is not: the agent's connectors and live
// state need the agent itself.
const mcpNote = "Config only, not live"

// The sections of the Customizations view. Every agent has Skills and
// Instructions; claude has the rest too.
const (
	custSkills = iota
	custRules
	custSubagents
	custHooks
	custMCP
	custPlugins
	custSections
)

var (
	custTitles = [custSections]string{"Skills", "Instructions", "Agents", "Hooks", "MCP Servers", "Plugins"}
	custScopes = [2]string{"User", "Workspace"}
)

// custAgent is where an agent keeps its skills and instructions, the user's
// then the workspace's. A user path is under home, or under the agent's
// config directory with "$/"; "/*.md" lists a directory.
type custAgent struct {
	env, dir string // the variable naming its config directory, its default under home
	skills   [2][]string
	rules    [2][]string
}

// custAgents are the agents the view knows, by [agents] preset. Where two
// directories hold the same skill name, the first one's is listed.
var custAgents = map[string]custAgent{
	"claude": {
		env: "CLAUDE_CONFIG_DIR", dir: ".claude",
		skills: [2][]string{{"$/skills"}, {".claude/skills"}},
		rules:  [2][]string{{"$/CLAUDE.md", "$/rules/*.md"}, {"CLAUDE.md", ".claude/CLAUDE.md", "CLAUDE.local.md", ".claude/rules/*.md"}},
	},
	"codex": {
		env: "CODEX_HOME", dir: ".codex",
		skills: [2][]string{{".agents/skills", "$/skills"}, {".agents/skills"}},
		rules:  [2][]string{{"$/AGENTS.override.md", "$/AGENTS.md"}, {"AGENTS.override.md", "AGENTS.md"}},
	},
	"grok": {
		dir:    ".grok",
		skills: [2][]string{{"$/skills", ".agents/skills", ".claude/skills"}, {".grok/skills", ".agents/skills", ".claude/skills"}},
		rules:  [2][]string{{"$/AGENTS.md"}, {"AGENTS.md", "CLAUDE.md"}},
	},
	"agy": {
		dir:    ".gemini",
		skills: [2][]string{{"$/config/skills"}, {".agents/skills", ".agent/skills"}},
		rules:  [2][]string{{"$/GEMINI.md"}, {"GEMINI.md", "AGENTS.md", ".agents/rules/*.md"}},
	},
}

// hasCustom reports an agent the Customizations view knows on the PATH; tests
// turn it off.
var hasCustom = func(st proto.State) bool {
	for name := range custAgents {
		if argv := st.Agents[name]; len(argv) > 0 {
			if _, err := exec.LookPath(argv[0]); err == nil {
				return true
			}
		}
	}

	return false
}

// custTarget is what a scan reads: one agent's files for one worktree.
type custTarget struct {
	agent, ws, home, conf string
}

// path is a custAgent path of scope.
func (t custTarget) path(scope int, p string) string {
	if scope == 1 {
		return filepath.Join(t.ws, p)
	}

	if rest, ok := strings.CutPrefix(p, "$/"); ok {
		return filepath.Join(t.conf, rest)
	}

	return filepath.Join(t.home, p)
}

// show is path as a row names it: from the worktree, or from home.
func (t custTarget) show(path string) string {
	if r, err := filepath.Rel(t.ws, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}

	if r, err := filepath.Rel(t.home, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.Join("~", r)
	}

	return path
}

// custItem is one row's worth of a customization: the file that defines it,
// with the line where that is one entry of a shared JSON file.
type custItem struct {
	name, detail string
	path         string
	line         int  // 1-based, 0 for the whole file
	warn         bool // named but unreadable: a dangling link, a broken file
}

func (it *custItem) matches(q string) bool {
	return strings.Contains(strings.ToLower(it.name), q) || strings.Contains(strings.ToLower(it.detail), q)
}

// custScan is what one scan found, by section and scope.
type custScan [custSections][2][]custItem

// scanCustom reads t's customizations. It never reads what a file's MCP
// entries keep in env or headers, so no row can show it.
func scanCustom(t custTarget) *custScan {
	var out custScan

	a := custAgents[t.agent]

	for scope := range 2 {
		seen := map[string]bool{}

		for _, d := range a.skills[scope] {
			for _, it := range skillsIn(t.path(scope, d)) {
				if !seen[it.name] {
					seen[it.name] = true
					out[custSkills][scope] = append(out[custSkills][scope], it)
				}
			}
		}

		slices.SortFunc(out[custSkills][scope], func(a, b custItem) int { return cmp.Compare(a.name, b.name) })

		for _, r := range a.rules[scope] {
			out[custRules][scope] = append(out[custRules][scope], t.rules(scope, r)...)
		}
	}

	if t.agent == "claude" {
		t.scanClaude(&out)
	}

	for sec := range out { // skills come from anyone: nothing of theirs reaches the terminal raw
		for scope := range out[sec] {
			for i := range out[sec][scope] {
				it := &out[sec][scope][i]
				it.name, it.detail = printable(it.name), printable(it.detail)
			}
		}
	}

	return &out
}

// skillsIn lists the skills under dir, a SKILL.md in each directory. Dot
// directories hold an agent's own: claude's .trash, codex's .system.
func skillsIn(dir string) []custItem {
	entries, _ := os.ReadDir(dir) // no directory is the usual case

	var out []custItem

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}

		sub := filepath.Join(dir, e.Name())
		path := filepath.Join(sub, "SKILL.md")

		if b, err := os.ReadFile(path); err == nil {
			name, desc := skillMeta(b)
			out = append(out, custItem{name: cmp.Or(name, e.Name()), detail: desc, path: path})

			continue
		}

		if _, err := os.Stat(sub); err != nil { // listed but not there: a link to nothing
			out = append(out, custItem{name: e.Name(), detail: "points to nothing", path: sub, warn: true})
		}
	}

	return out
}

// mdFiles are the Markdown files directly in dir, sorted.
func mdFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)

	var out []string

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") && !e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}

	return out
}

// rules lists the instruction files one custAgent path names.
func (t custTarget) rules(scope int, rel string) []custItem {
	var out []custItem

	if dir, ok := strings.CutSuffix(rel, "/*.md"); ok {
		for _, p := range mdFiles(t.path(scope, dir)) {
			out = append(out, custItem{name: t.show(p), path: p})
		}

		return out
	}

	p := t.path(scope, rel)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		out = append(out, custItem{name: t.show(p), path: p})
	}

	return out
}

// skillMeta is the name and description in the YAML block a skill or an
// agent opens with: top-level scalars, a value's indented lines joined, which
// is the shapes those fields take. Neither has a newline.
func skillMeta(b []byte) (name, desc string) {
	lines := strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n")
	if strings.TrimRight(lines[0], " \r") != "---" {
		return "", ""
	}

	var cur *string

	for _, l := range lines[1:] {
		l = strings.TrimRight(l, " \r")
		if l == "---" {
			break
		}

		if cur != nil && l != "" && (l[0] == ' ' || l[0] == '\t') {
			*cur = strings.TrimSpace(*cur + " " + strings.TrimSpace(l))
			continue
		}

		cur = nil
		key, val, _ := strings.Cut(l, ":")

		switch key {
		case "name":
			cur = &name
		case "description":
			cur = &desc
		default:
			continue
		}

		val = strings.TrimSpace(val)
		if strings.Trim(val, ">|+-") == "" { // a block scalar: its lines follow
			val = ""
		}

		*cur = strings.Trim(val, `"'`)
	}

	return name, desc
}

// jsonKey is one key of a JSON object, its 1-based line and its value.
type jsonKey struct {
	name string
	line int
	raw  json.RawMessage
}

// jsonKeys lists, in file order, the keys of the object path leads to in b:
// none where it leads nowhere or to no object, an error where b does not
// parse up to there.
func jsonKeys(b []byte, path ...string) ([]jsonKey, error) {
	dec := json.NewDecoder(bytes.NewReader(b))

	for depth := 0; ; depth++ {
		if t, err := dec.Token(); err != nil {
			return nil, err
		} else if t != json.Delim('{') {
			return nil, nil
		}

		var (
			keys  []jsonKey
			found bool
		)

		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return nil, err
			}

			name, _ := t.(string)
			at := dec.InputOffset()

			if depth < len(path) && name == path[depth] {
				found = true
				break
			}

			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return nil, err
			}

			if depth == len(path) {
				keys = append(keys, jsonKey{name, 1 + bytes.Count(b[:at], []byte("\n")), raw})
			}
		}

		if !found {
			return keys, nil
		}
	}
}

// badFile is the row of a config file that does not parse: an empty section
// would say it holds nothing.
func badFile(path string) []custItem {
	return []custItem{{name: filepath.Base(path), detail: "invalid JSON", path: path, warn: true}}
}

// scanClaude reads the sections only claude has.
func (t custTarget) scanClaude(out *custScan) {
	user := filepath.Join(t.conf, "settings.json")
	shared, local := filepath.Join(t.ws, ".claude", "settings.json"), filepath.Join(t.ws, ".claude", "settings.local.json")

	for scope, dir := range [2]string{filepath.Join(t.conf, "agents"), filepath.Join(t.ws, ".claude", "agents")} {
		for _, p := range mdFiles(dir) {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}

			name, desc := skillMeta(b)
			out[custSubagents][scope] = append(out[custSubagents][scope], custItem{name: cmp.Or(name, strings.TrimSuffix(filepath.Base(p), ".md")), detail: desc, path: p})
		}
	}

	out[custHooks][0] = hooksIn(user)
	out[custHooks][1] = append(hooksIn(shared), hooksIn(local)...)

	// With CLAUDE_CONFIG_DIR set the file lives in it, otherwise beside it.
	state := filepath.Join(t.home, ".claude.json")
	if t.conf != filepath.Join(t.home, ".claude") {
		state = filepath.Join(t.conf, ".claude.json")
	}

	out[custMCP][0] = mcpIn(state, "mcpServers")
	out[custMCP][1] = append(mcpIn(state, "projects", filepath.ToSlash(t.ws), "mcpServers"), mcpIn(filepath.Join(t.ws, ".mcp.json"), "mcpServers")...)

	t.plugins(out, user, shared, local)
}

// hooksIn lists the hooks of a settings file, a row per command, in file order.
func hooksIn(path string) []custItem {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	keys, err := jsonKeys(b, "hooks")
	if err != nil {
		return badFile(path)
	}

	var out []custItem

	for _, k := range keys {
		var groups []struct {
			Hooks []struct{ Type, Command string }
		}

		if json.Unmarshal(k.raw, &groups) != nil {
			return badFile(path)
		}

		for _, g := range groups {
			for _, h := range g.Hooks {
				out = append(out, custItem{name: k.name, detail: cmp.Or(hookLabel(h.Command), h.Type), path: path, line: k.line})
			}
		}
	}

	return out
}

// hookLabel names a hook's command by the last file it mentions, or its first
// word. It never shows a whole command, nor a word with "=": commands carry
// tokens in the environment they set.
func hookLabel(command string) string {
	var first, file string

	for _, f := range strings.Fields(command) {
		f = strings.Trim(f, `"'`)

		switch {
		case strings.Contains(f, "="):
		case strings.IndexAny(f, "/~.$") == 0 && strings.Contains(f, "/"): // a path, not a header's value
			file = filepath.Base(f)
		case first == "":
			first = f
		}
	}

	return cmp.Or(file, first)
}

// mcpIn lists the MCP servers of the object at path in a JSON file, by their
// transport. The values stay unread beyond that: env and headers hold secrets.
// ponytail: the editor stops at maxPreviewLines, so a project's servers past
// that line of a long .claude.json open on the last line it shows.
func mcpIn(file string, path ...string) []custItem {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}

	keys, err := jsonKeys(b, path...)
	if err != nil {
		return badFile(file)
	}

	var out []custItem

	for _, k := range keys {
		var s struct{ Type, Command string }

		_ = json.Unmarshal(k.raw, &s) // a server of a shape unknown is still listed
		out = append(out, custItem{name: k.name, detail: cmp.Or(s.Type, ifElse(s.Command != "", "stdio", "")), path: file, line: k.line})
	}

	return out
}

// plugins lists the installed plugins of the user and of this worktree, each
// with its marketplace; settings turn one off, the later file winning.
func (t custTarget) plugins(out *custScan, settings ...string) {
	path := filepath.Join(t.conf, "plugins", "installed_plugins.json")

	b, err := os.ReadFile(path)
	if err != nil {
		return
	}

	keys, err := jsonKeys(b, "plugins")
	if err != nil {
		out[custPlugins][0] = badFile(path)
		return
	}

	off := map[string]bool{}

	for _, f := range settings {
		sb, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		enabled, _ := jsonKeys(sb, "enabledPlugins") // hooksIn says when it does not parse
		for _, k := range enabled {
			off[k.name] = string(k.raw) == "false"
		}
	}

	for _, k := range keys {
		var installs []struct {
			Scope       string
			ProjectPath string
		}

		if json.Unmarshal(k.raw, &installs) != nil {
			out[custPlugins][0] = append(out[custPlugins][0], custItem{name: k.name, detail: "unknown entry", path: path, line: k.line, warn: true})
			continue
		}

		name, market, _ := strings.Cut(k.name, "@")
		if off[k.name] {
			market = strings.TrimSuffix("off · "+market, " · ")
		}

		for _, in := range installs {
			scope := 0

			if in.Scope != "user" {
				if in.ProjectPath != t.ws {
					continue
				}

				scope = 1
			}

			out[custPlugins][scope] = append(out[custPlugins][scope], custItem{name: name, detail: market, path: path, line: k.line})
		}
	}
}

// customView is the Customizations tab of Sessions, VS Code's editor of that
// name reduced to a list: what one agent loads in this worktree, each entry
// opening its file. It reads and never writes; the agent shown follows the
// session.
type customView struct {
	l        list
	agent    string // the agent listed: the session's, or the last one picked
	sess     string // the session that agent was taken from
	tgt      custTarget
	got      *custScan // nil until read
	at       time.Time // when it was read
	scanning bool
	open     [custSections]bool
}

// nameW is the name column: the longest name with a detail beside it, at most
// 40% of w. Every entry counts, folded or filtered out, so the column stays
// put.
func (s *custScan) nameW(w int) int {
	n := 0

	for _, sec := range s {
		for _, items := range sec {
			for _, it := range items {
				if it.detail != "" {
					n = max(n, strWidth(it.name))
				}
			}
		}
	}

	return min(n, w*2/5)
}

const (
	cuAgent = iota // the row that names the agent and picks another
	cuSection
	cuScope
	cuItem
	cuNote
)

// custRow is a row of the view; n is a section's or a scope's count. A
// section's scope is the only one it has entries in, -1 for none or both.
type custRow struct {
	kind, sec, scope, n int
	it                  *custItem
}

// custKey is what finds a row again after a scan.
type custKey struct {
	kind, sec, scope int
	it               custItem
}

func (r custRow) key() custKey {
	k := custKey{kind: r.kind, sec: r.sec, scope: r.scope}
	if r.it != nil {
		k.it = *r.it
	}

	return k
}

type custMsg struct {
	tgt  custTarget
	scan *custScan
}

// sessionAgent is the agent s runs that the view knows, "" for none: a shell
// says nothing about which agent's files matter.
func sessionAgent(s *proto.Session) string {
	if s == nil {
		return ""
	}

	for _, name := range []string{s.Program, s.Agent} { // claude started in a shell tab is claude
		if _, ok := custAgents[name]; ok {
			return name
		}
	}

	return ""
}

// sessionEnv is what s sets key to, "" for nothing: a profile's other config.
func sessionEnv(s *proto.Session, key string) string {
	if s == nil {
		return ""
	}

	out := ""

	for _, kv := range s.Env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			out = v
		}
	}

	return out
}

// custInstalled are the agents the view knows that are on the PATH.
func (m *Model) custInstalled() []string {
	return slices.DeleteFunc(m.harnesses(), func(name string) bool {
		_, ok := custAgents[name]
		return !ok
	})
}

// target is what the view should list now. Another session brings its agent;
// a shell keeps the last one, and with none yet it is claude, or the first
// agent installed.
func (v *customView) target(m *Model) custTarget {
	s := m.session(m.sess)

	if v.agent == "" {
		v.open[custSkills] = true
		v.agent = "claude"

		if have := m.custInstalled(); len(have) > 0 && !slices.Contains(have, v.agent) {
			v.agent = have[0]
		}
	}

	if m.sess != v.sess {
		v.sess = m.sess
		v.agent = cmp.Or(sessionAgent(s), v.agent)
	}

	a := custAgents[v.agent]
	home, _ := os.UserHomeDir()
	conf := cmp.Or(sessionEnv(s, a.env), os.Getenv(a.env), filepath.Join(home, a.dir))

	return custTarget{agent: v.agent, ws: m.ws, home: home, conf: expandHome(conf)}
}

// ensure scans when the target changed or the last scan is older than custTTL.
func (v *customView) ensure(m *Model) tea.Cmd {
	if !m.hasCustom {
		return nil
	}

	t := v.target(m)
	if t != v.tgt {
		v.tgt, v.got, v.at, v.scanning = t, nil, time.Time{}, false
		v.l.sel, v.l.top = 0, 0
	}

	if v.scanning || time.Since(v.at) <= custTTL {
		return nil
	}

	v.scanning = true

	return func() tea.Msg { return custMsg{t, scanCustom(t)} }
}

// shown is ensure while the view is on screen; the tick asks it too.
func (v *customView) shown(m *Model) tea.Cmd {
	if !m.shown(viewCustom) {
		return nil
	}

	return v.ensure(m)
}

func (v *customView) onScan(m *Model, msg custMsg) {
	if msg.tgt != v.tgt { // another agent or worktree since
		return
	}

	var key custKey

	r := v.selected(m)
	if r != nil {
		key = r.key()
	}

	v.got, v.at, v.scanning = msg.scan, time.Now(), false

	rows, h := v.rows(m), m.bodyH(viewCustom)
	if i := slices.IndexFunc(rows, func(o custRow) bool { return o.key() == key }); r != nil && i >= 0 {
		v.l.sel = i
		v.l.snap(h)
	}

	v.l.clamp(len(rows), h)
}

func (v *customView) refresh(m *Model) tea.Cmd {
	v.at = time.Time{}
	return v.ensure(m)
}

func (v *customView) collapseAll() {
	clear(v.open[:])
	v.l.sel = min(v.l.sel, 0)
}

// rows are the agent, then each section with its entries when unfolded, under
// a scope's row where both scopes have some; a filter unfolds every section
// and keeps the entries it matches.
func (v *customView) rows(m *Model) []custRow {
	out := []custRow{{kind: cuAgent}}
	if v.got == nil {
		return append(out, custRow{kind: cuNote})
	}

	q := strings.ToLower(m.query(viewCustom))
	sections := ifElse(v.tgt.agent == "claude", custSections, custRules+1)

	for sec := range sections {
		var kids []custRow

		count, scopes, only := 0, 0, -1

		for scope := range v.got[sec] {
			var hits []custRow

			for i := range v.got[sec][scope] {
				if it := &v.got[sec][scope][i]; q == "" || it.matches(q) {
					hits = append(hits, custRow{kind: cuItem, sec: sec, scope: scope, it: it})
				}
			}

			count += len(hits)

			if len(hits) > 0 {
				scopes, only = scopes+1, scope
				kids = append(append(kids, custRow{kind: cuScope, sec: sec, scope: scope, n: len(hits)}), hits...)
			}
		}

		if q != "" && count == 0 {
			continue
		}

		switch scopes {
		case 1: // the section's row says which
			kids = kids[1:]
		case 2:
			only = -1
		}

		out = append(out, custRow{kind: cuSection, sec: sec, scope: only, n: count})

		if q != "" || v.open[sec] {
			if sec == custMCP {
				out = append(out, custRow{kind: cuNote, sec: sec})
			}

			out = append(out, kids...)
		}
	}

	return out
}

func (v *customView) selected(m *Model) *custRow {
	if rows := v.rows(m); v.l.sel >= 0 && v.l.sel < len(rows) {
		return &rows[v.l.sel]
	}

	return nil
}

func (v *customView) lines(m *Model, w, h int) []string {
	rows := v.rows(m)
	hover := m.hoverRow(viewCustom)
	v.l.bar = m.barState("list:customizations")

	level := func(i int) int { return ifElse(rows[i].kind == cuAgent || rows[i].kind == cuSection, 0, 1) }
	ag := activeGuide(len(rows), v.l.sel, level)

	nameW := 0
	if v.got != nil {
		nameW = v.got.nameW(w)
	}

	return v.l.render(w, h, len(rows), func(i, rw int) string {
		r := rows[i]
		bg, name := m.rowColors(viewCustom, i == v.l.sel, i-v.l.top == hover)
		lead := append([]seg{sg(" ", plain)}, ag.at(i, level(i))...)
		count := sg(strconv.Itoa(r.n)+" ", dim)

		switch r.kind {
		case cuAgent:
			return row(rw, bg, []seg{sg(" "+v.agent+" ▾", name)}, sg(filepath.Base(m.ws)+" ", dim))
		case cuSection:
			right := []seg{count}
			if r.scope >= 0 {
				right = []seg{sg(strings.ToLower(custScopes[r.scope])+"  ", dim), count}
			}

			return row(rw, bg, []seg{sg(" "+chevron(v.open[r.sec]), dim), sg(custTitles[r.sec], name)}, right...)

		case cuScope:
			return row(rw, bg, append(lead, sg("  "+custScopes[r.scope], dim)), count)
		case cuNote:
			return row(rw, bg, append(lead, sg("  "+ifElse(r.sec == custMCP, mcpNote, "Reading…"), dim)))
		}

		lead, col := append(lead, sg("  ", plain)), nameW

		if r.it.warn {
			mark := icWarning.s() + " "
			lead, col = append(lead, sg(mark, fg(pal.warn))), col-strWidth(mark)
		}

		if r.it.detail == "" {
			return row(rw, bg, append(lead, sg(r.it.name, name)))
		}

		return row(rw, bg, append(lead, sg(pad(r.it.name, max(col, 1), false)+" ", name), sg(r.it.detail, dim)))
	})
}

// openItem shows an entry's file, at its line where it has one.
func (m *Model) openItem(it *custItem, transient bool) tea.Cmd {
	if it.line > 0 {
		return m.openFileAt(it.path, it.line-1, 0, 0, transient)
	}

	return m.openFile(it.path, transient)
}

// pick asks which agent to list.
func (v *customView) pick(m *Model) tea.Cmd {
	var items []item

	for _, name := range m.custInstalled() {
		items = append(items, item{label: name, inline: ifElse(name == v.agent, "shown", ""), run: func(m *Model) tea.Cmd {
			v.agent = name
			return v.ensure(m)
		}})
	}

	if len(items) == 0 {
		return flash("no agent installed", true)
	}

	m.modal = newPicker("Show Agent", items)

	return nil
}

func (v *customView) activate(m *Model, r *custRow, transient bool) tea.Cmd {
	switch {
	case r == nil:
	case r.kind == cuAgent:
		return v.pick(m)
	case r.kind == cuSection:
		v.open[r.sec] = !v.open[r.sec]
	case r.kind == cuItem:
		return m.openItem(r.it, transient)
	}

	return nil
}

func (v *customView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := v.rows(m)
	h, n := m.bodyH(viewCustom), len(rows)
	r := v.selected(m)

	switch k.String() {
	case "up", "k":
		v.l.move(-1, n, h)
	case "down", "j":
		v.l.move(1, n, h)
	case "pgup":
		v.l.move(-h, n, h)
	case "pgdown":
		v.l.move(h, n, h)
	case "g", "home":
		v.l.move(-n, n, h)
	case "G", "end":
		v.l.move(n, n, h)
	case "enter", "space":
		return v.activate(m, r, true)
	case "l", "right":
		if r != nil && (r.kind == cuItem || r.kind == cuSection && !v.open[r.sec]) {
			return v.activate(m, r, true)
		}

	case "h", "left":
		switch {
		case r == nil || r.kind == cuAgent:
		case r.kind == cuSection:
			v.open[r.sec] = false
		default: // to the section it is in
			v.l.sel = slices.IndexFunc(rows, func(o custRow) bool { return o.kind == cuSection && o.sec == r.sec })
			v.l.snap(h)
		}

	case "a":
		return v.pick(m)
	case "r", "ctrl+r":
		return v.refresh(m)
	case "C":
		v.collapseAll()
	case "c", "y":
		return v.copy(r, k.String() == "y")
	case "m":
		return m.menuOf(v.items(m), 0, h/2)
	}

	return nil
}

// copy puts the selected entry's name or path on the clipboard.
func (v *customView) copy(r *custRow, path bool) tea.Cmd {
	if r == nil || r.it == nil {
		return nil
	}

	s := ifElse(path, r.it.path, r.it.name)

	return setClipboard(s, "copied "+s)
}

func (v *customView) items(m *Model) []item {
	r := v.selected(m)

	var items []item

	if r != nil && r.it != nil {
		it := r.it

		items = append(items,
			item{label: "Open File", hint: "⏎", run: func(m *Model) tea.Cmd { return m.openItem(it, false) }},
			item{label: "Copy Name", hint: "c", run: func(*Model) tea.Cmd { return v.copy(r, false) }},
			item{label: "Copy Path", hint: "y", run: func(*Model) tea.Cmd { return v.copy(r, true) }},
			separator())
	}

	return append(items,
		item{label: "Show Agent…", hint: "a", run: v.pick},
		item{label: "Refresh", hint: "r", run: v.refresh},
		item{label: "Collapse All", hint: "C", run: func(*Model) tea.Cmd { v.collapseAll(); return nil }})
}

func (v *customView) mouse(m *Model, msg tea.MouseMsg, y int) tea.Cmd {
	mo, rows := msg.Mouse(), v.rows(m)

	switch msg.(type) {
	case tea.MouseWheelMsg:
		v.l.wheel(wheelDelta(mo), len(rows), m.bodyH(viewCustom))
	case tea.MouseClickMsg:
		i := v.l.at(y, len(rows))
		if i < 0 {
			if mo.Button == tea.MouseRight {
				return m.menuOf(v.items(m), mo.X, mo.Y)
			}

			return nil
		}

		v.l.sel = i
		if mo.Button == tea.MouseRight {
			return m.menuOf(v.items(m), mo.X, mo.Y)
		}

		cmd := v.activate(m, &rows[i], true)
		if rows[i].kind == cuItem && m.clicks.double(mo.X, mo.Y) {
			m.pin()
		}

		return cmd
	}

	return nil
}
