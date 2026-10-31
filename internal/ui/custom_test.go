package ui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// custModel is a test model with the Customizations view on, a claude and a
// codex installed, and a home and worktree of its own holding one of each.
func custModel(t *testing.T) (m *Model, home string) {
	t.Helper()

	m = testModelSized(t, 120, 40)
	m.hasCustom = true
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")

	bin := t.TempDir()
	t.Setenv("PATH", bin)

	for _, name := range []string{"claude", "codex"} {
		mustWrite(t, filepath.Join(bin, name), "")

		if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}

		m.st.Agents[name] = []string{name}
	}

	conf := filepath.Join(home, ".claude")
	mustWrite(t, filepath.Join(conf, "skills", "ponytail", "SKILL.md"), "---\nname: ponytail\ndescription: >\n  Lazy senior\n  dev mode.\n---\nbody\n")
	mustWrite(t, filepath.Join(conf, "skills", ".trash", "old", "SKILL.md"), "---\nname: old\n---\n")
	mustWrite(t, filepath.Join(conf, "skills", "synced", "notes.txt"), "no SKILL.md here\n")
	mustWrite(t, filepath.Join(conf, "CLAUDE.md"), "# rules\n")
	mustWrite(t, filepath.Join(conf, "agents", "deep-task.md"), "---\nname: deep-task\ndescription: Hard subtasks\n---\n")
	mustWrite(t, filepath.Join(conf, "settings.json"), `{
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "TOKEN=hunter2 bash '/opt/hooks/notify.sh' start"}]}],
    "Stop": [{"matcher": "*", "hooks": [{"type": "prompt", "prompt": "check"}]}]
  },
  "enabledPlugins": {"figma@official": false, "fizzy@37signals": true}
}
`)
	mustWrite(t, filepath.Join(conf, "plugins", "installed_plugins.json"), `{"version": 2, "plugins": {
  "figma@official": [{"scope": "user"}],
  "fizzy@37signals": [{"scope": "user"}],
  "other@x": [{"scope": "project", "projectPath": "/somewhere/else"}]
}}
`)
	mustWrite(t, filepath.Join(home, ".claude.json"), `{
  "oauthAccount": {"emailAddress": "someone@example.com"},
  "mcpServers": {
    "figma": {"command": "npx", "env": {"API_KEY": "s3cret-env"}},
    "stack": {"type": "http", "url": "https://x", "headers": {"Authorization": "Bearer s3cret-header"}}
  },
  "projects": {"`+filepath.ToSlash(m.ws)+`": {"mcpServers": {"local": {"type": "sse"}}}}
}
`)

	mustWrite(t, filepath.Join(m.ws, "CLAUDE.md"), "# project\n")
	mustWrite(t, filepath.Join(m.ws, "AGENTS.md"), "# project\n")
	mustWrite(t, filepath.Join(m.ws, ".mcp.json"), `{"mcpServers": {"shared": {"command": "x"}}}`)
	mustWrite(t, filepath.Join(m.ws, ".claude", "skills", "release", "SKILL.md"), "plain, no frontmatter\n")
	mustWrite(t, filepath.Join(home, ".agents", "skills", "git-commit", "SKILL.md"), "---\nname: git-commit\n---\n")
	mustWrite(t, filepath.Join(home, ".codex", "skills", "git-commit", "SKILL.md"), "---\nname: git-commit\ndescription: shadowed\n---\n")
	mustWrite(t, filepath.Join(home, ".codex", "skills", "local", "SKILL.md"), "---\nname: local\n---\n")

	return m, home
}

// custShow opens the view and scans.
func custShow(m *Model) { dbDo(m, m.showView(viewCustom)) }

// custNames are the names of the entries of a section's scope.
func custNames(s *custScan, sec, scope int) []string {
	var out []string
	for _, it := range s[sec][scope] {
		out = append(out, it.name)
	}

	return out
}

func TestSkillMeta(t *testing.T) {
	for _, c := range []struct{ in, name, desc string }{
		{"---\nname: a\ndescription: b\n---\n", "a", "b"},
		{"---\r\nname: a\r\ndescription: \"b: c\"\r\n---\r\n", "a", "b: c"},
		{"---\nname: a\ndescription: >\n  one\n  two\nmodel: opus\n---\n", "a", "one two"},
		{"---\nname: a\ndescription: |-\n  one\n---\n", "a", "one"},
		{"---\nname: a\ndescription: one\n  two\n---\n", "a", "one two"},
		{"\ufeff---\nname: a\n---\n", "a", ""},
		{"---\nname: a\n---\nname: not this\n", "a", ""},
		{"---\nmetadata:\n  name: nested\n---\n", "", ""},
		{"name: no fence\n", "", ""},
		{"", "", ""},
	} {
		if name, desc := skillMeta([]byte(c.in)); name != c.name || desc != c.desc {
			t.Errorf("%q: %q %q, want %q %q", c.in, name, desc, c.name, c.desc)
		}
	}
}

// FuzzSkillMeta holds that a value stays on one row, and that a file without
// the opening fence names nothing.
func FuzzSkillMeta(f *testing.F) {
	f.Add("---\nname: a\ndescription: >\n  b\n---\n")
	f.Add("name: a\n")
	f.Add("---\nname: \"\ndescription: |\n\t\n")
	f.Fuzz(func(t *testing.T, in string) {
		name, desc := skillMeta([]byte(in))
		if strings.Contains(name, "\n") || strings.Contains(desc, "\n") {
			t.Fatalf("a newline in %q %q", name, desc)
		}

		if !strings.HasPrefix(strings.TrimPrefix(in, "\ufeff"), "---") && name+desc != "" {
			t.Fatalf("no fence, yet %q %q", name, desc)
		}
	})
}

func TestJSONKeys(t *testing.T) {
	b := []byte("{\n  \"skip\": {\"a\": [1, {\"b\": 2}]},\n  \"a\": {\n    \"x\": 1,\n    \"y\": {\"z\": 2}\n  },\n  \"s\": \"text\"\n}\n")

	keys, err := jsonKeys(b, "a")
	if err != nil || len(keys) != 2 || keys[0].name != "x" || keys[0].line != 4 || keys[1].name != "y" || keys[1].line != 5 || string(keys[1].raw) != `{"z": 2}` {
		t.Fatalf("a: %+v %v", keys, err)
	}

	if keys, err := jsonKeys(b, "a", "y"); err != nil || len(keys) != 1 || keys[0].name != "z" || keys[0].line != 5 {
		t.Fatalf("a.y: %+v %v", keys, err)
	}

	if keys, err := jsonKeys(b); err != nil || len(keys) != 3 {
		t.Fatalf("top: %+v %v", keys, err)
	}

	for _, path := range [][]string{{"none"}, {"s"}, {"a", "x"}, {"a", "none", "deeper"}} {
		if keys, err := jsonKeys(b, path...); err != nil || keys != nil {
			t.Fatalf("%v: %+v %v", path, keys, err)
		}
	}

	if _, err := jsonKeys([]byte(`{"a": {"x": `), "a"); err == nil {
		t.Fatal("a cut file is an error")
	}
}

// FuzzJSONKeys holds that a key is on the line reported, whatever the file,
// and that the top level's keys are the ones encoding/json reads.
func FuzzJSONKeys(f *testing.F) {
	f.Add([]byte("{\n\"a\": {\n\"b\": 1}}\n"), "a")
	f.Add([]byte(`{"a": [`), "a")
	f.Add([]byte(`[1, 2]`), "")
	f.Fuzz(func(t *testing.T, b []byte, key string) {
		keys, err := jsonKeys(b, key)
		if err != nil {
			return
		}

		lines := bytes.Split(b, []byte("\n"))

		for _, k := range keys {
			if k.line < 1 || k.line > len(lines) {
				t.Fatalf("line %d of %d", k.line, len(lines))
			}

			if q, err := json.Marshal(k.name); err == nil && !bytes.ContainsAny(q, `\`) && !bytes.ContainsRune(q, 0xFFFD) && !bytes.Contains(lines[k.line-1], q) {
				t.Fatalf("key %s is not on line %d: %q", q, k.line, lines[k.line-1])
			}
		}

		var whole map[string]json.RawMessage
		if json.Unmarshal(b, &whole) == nil && whole != nil {
			top, err := jsonKeys(b)
			if err != nil {
				t.Fatalf("encoding/json reads what this does not: %v", err)
			}

			names := map[string]bool{}
			for _, k := range top {
				names[k.name] = true
			}

			if len(names) != len(whole) {
				t.Fatalf("%d keys, encoding/json reads %d", len(names), len(whole))
			}
		}
	})
}

func TestHookLabel(t *testing.T) {
	for in, want := range map[string]string{
		"bash '/home/me/.claude/hooks/state.sh' session":              "state.sh",
		`[ -x "$HOME/h/notify.sh" ] && X=1 "$HOME/h/notify.sh"`:       "notify.sh",
		"TOKEN=hunter2 notify-send done":                              "notify-send",
		`curl -H "x: abc/def" https://example.com/hook?token=hunter2`: "curl",
		"curl https://example.com/hook?token=hunter2 -d x":            "curl",
		"": "",
	} {
		if got := hookLabel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestScanCustomClaude(t *testing.T) {
	m, home := custModel(t)
	s := scanCustom(custTarget{agent: "claude", ws: m.ws, home: home, conf: filepath.Join(home, ".claude")})

	for _, c := range []struct {
		sec, scope int
		want       []string
	}{
		{custSkills, 0, []string{"ponytail"}}, // not .trash, not a directory without SKILL.md
		{custSkills, 1, []string{"release"}},  // named by its directory
		{custRules, 0, []string{filepath.Join("~", ".claude", "CLAUDE.md")}},
		{custRules, 1, []string{"CLAUDE.md"}},
		{custSubagents, 0, []string{"deep-task"}},
		{custHooks, 0, []string{"SessionStart", "Stop"}},
		{custMCP, 0, []string{"figma", "stack"}},
		{custMCP, 1, []string{"local", "shared"}},
		{custPlugins, 0, []string{"figma", "fizzy"}}, // not another project's
	} {
		if got := custNames(s, c.sec, c.scope); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: %v, want %v", custTitles[c.sec], custScopes[c.scope], got, c.want)
		}
	}

	if it := s[custSkills][0][0]; it.detail != "Lazy senior dev mode." {
		t.Errorf("skill: %+v", it)
	}

	if it := s[custHooks][0]; it[0].detail != "notify.sh" || it[0].line != 3 || it[1].detail != "prompt" || it[1].line != 4 {
		t.Errorf("hooks: %+v", it)
	}

	if it := s[custMCP][0]; it[0].detail != "stdio" || it[0].line != 4 || it[1].detail != "http" || it[1].line != 5 {
		t.Errorf("mcp: %+v", it)
	}

	if it := s[custPlugins][0]; it[0].detail != "off · official" || it[1].detail != "37signals" {
		t.Errorf("plugins: %+v", it)
	}
}

// TestScanCustomNoSecrets holds what the list promises: env, headers and a
// command's inline variables reach no row, whatever is on screen.
func TestScanCustomNoSecrets(t *testing.T) {
	m, home := custModel(t)
	s := scanCustom(custTarget{agent: "claude", ws: m.ws, home: home, conf: filepath.Join(home, ".claude")})

	for sec := range s {
		for scope := range s[sec] {
			for _, it := range s[sec][scope] {
				for _, secret := range []string{"s3cret", "hunter2", "someone@example.com"} {
					if strings.Contains(it.name+it.detail, secret) {
						t.Errorf("%s in %+v", secret, it)
					}
				}
			}
		}
	}
}

func TestScanCustomBroken(t *testing.T) {
	m, home := custModel(t)
	conf := filepath.Join(home, ".claude")

	mustWrite(t, filepath.Join(conf, "settings.json"), `{"hooks": `)
	mustWrite(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {"x"`)
	mustWrite(t, filepath.Join(conf, "plugins", "installed_plugins.json"), `nope`)

	if err := os.Symlink(filepath.Join(home, "gone"), filepath.Join(conf, "skills", "dangling")); err != nil {
		t.Skip(err)
	}

	// A description cannot move the terminal's cursor.
	mustWrite(t, filepath.Join(conf, "skills", "evil", "SKILL.md"), "---\nname: evil\ndescription: a\x1b[2Jb\x07\n---\n")

	s := scanCustom(custTarget{agent: "claude", ws: m.ws, home: home, conf: conf})

	for _, sec := range []int{custHooks, custMCP, custPlugins} {
		if it := s[sec][0]; len(it) != 1 || !it[0].warn || it[0].detail != "invalid JSON" {
			t.Errorf("%s: %+v", custTitles[sec], it)
		}
	}

	i := slices.IndexFunc(s[custSkills][0], func(it custItem) bool { return it.name == "dangling" })
	if i < 0 || !s[custSkills][0][i].warn {
		t.Errorf("a dangling skill: %+v", s[custSkills][0])
	}

	i = slices.IndexFunc(s[custSkills][0], func(it custItem) bool { return it.name == "evil" })
	if i < 0 || s[custSkills][0][i].detail != "ab" {
		t.Errorf("an escape in a description: %+v", s[custSkills][0])
	}
}

// TestScanCustomOtherAgents holds that each agent reads its own paths and
// has only Skills and Instructions.
func TestScanCustomOtherAgents(t *testing.T) {
	m, home := custModel(t)
	s := scanCustom(custTarget{agent: "codex", ws: m.ws, home: home, conf: filepath.Join(home, ".codex")})

	if got := custNames(s, custSkills, 0); !slices.Equal(got, []string{"git-commit", "local"}) {
		t.Errorf("skills: %v", got)
	}

	if s[custSkills][0][0].detail != "" { // ~/.agents/skills comes first
		t.Errorf("the shadowed one is listed: %+v", s[custSkills][0][0])
	}

	if got := custNames(s, custRules, 1); !slices.Equal(got, []string{"AGENTS.md"}) {
		t.Errorf("rules: %v", got)
	}

	if len(s[custHooks][0])+len(s[custMCP][0])+len(s[custPlugins][0]) != 0 {
		t.Errorf("claude's sections: %+v", s)
	}
}

// custRowAt selects the entry named name.
func custRowAt(t *testing.T, m *Model, name string) {
	t.Helper()

	i := slices.IndexFunc(m.cu.rows(m), func(r custRow) bool { return r.it != nil && r.it.name == name })
	if i < 0 {
		t.Fatalf("no %s in %s", name, checkWidths(t, m))
	}

	m.cu.l.sel = i
}

func custUnfold(m *Model, sec int) {
	m.cu.open[sec] = true
}

func TestCustomizationsView(t *testing.T) {
	m, home := custModel(t)

	dbKey(m, "8")

	out := checkWidths(t, m)
	for _, want := range []string{"Sessions  Customizations", "claude ▾", "Skills", "Instructions", "MCP Servers", "Plugins"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing:\n%s", want, out)
		}
	}

	// A hook opens its file on its line.
	custUnfold(m, custHooks)
	custRowAt(t, m, "Stop")
	press(m, "enter")

	if m.pv.path != filepath.Join(home, ".claude", "settings.json") || m.pv.cur.line != 3 {
		t.Fatalf("opened %q at %+v", m.pv.path, m.pv.cur)
	}

	// The MCP section says what it is, and shows no secret even unfolded.
	custUnfold(m, custMCP)

	out = checkWidths(t, m)
	if !strings.Contains(out, mcpNote) || strings.Contains(out, "s3cret") {
		t.Fatalf("mcp:\n%s", out)
	}

	// A section folds with ←, from a row in it too.
	custRowAt(t, m, "figma")
	press(m, "left")

	if r := m.cu.selected(m); r.kind != cuSection || r.sec != custMCP {
		t.Fatalf("left from an entry: %+v", r)
	}

	press(m, "left")

	if m.cu.open[custMCP] {
		t.Fatal("left on a section folds it")
	}

	press(m, "C")

	if slices.Contains(m.cu.open[:], true) {
		t.Fatalf("collapse: %v", m.cu.open)
	}

	// A filter unfolds what it matches and drops the rest.
	dbDo(m, m.startFilter(viewCustom))
	typeText(m, "deep")

	rows := m.cu.rows(m)
	if i := slices.IndexFunc(rows, func(r custRow) bool { return r.it != nil && r.it.name == "deep-task" }); i < 0 || len(rows) != 3 { // agent, Agents, the entry
		t.Fatalf("filtered: %+v", rows)
	}
}

// TestCustomizationsFollowSession holds that the list is the session's agent:
// a shell keeps the last one, and a pick stands until the session changes.
func TestCustomizationsFollowSession(t *testing.T) {
	m, _ := custModel(t)
	custShow(m)

	if m.cu.agent != "claude" {
		t.Fatalf("first agent: %q", m.cu.agent)
	}

	m.sessions = append(m.sessions, proto.Session{SessionSpec: proto.SessionSpec{ID: "c1", Workspace: m.ws, Agent: "codex"}})
	m.sess = "c1"
	dbDo(m, m.cu.shown(m))

	if m.cu.tgt.agent != "codex" || m.cu.got == nil || m.cu.got[custSkills][0][0].name != "git-commit" {
		t.Fatalf("codex: %+v", m.cu.tgt)
	}

	if rows := m.cu.rows(m); slices.ContainsFunc(rows, func(r custRow) bool { return r.kind == cuSection && r.sec > custRules }) {
		t.Fatalf("codex shows claude's sections: %+v", rows)
	}

	m.sess = "s1" // a shell
	dbDo(m, m.cu.shown(m))

	if m.cu.tgt.agent != "codex" {
		t.Fatalf("a shell changed it: %+v", m.cu.tgt)
	}

	// claude started in that shell, under another config.
	alt := t.TempDir()
	mustWrite(t, filepath.Join(alt, "skills", "work", "SKILL.md"), "---\nname: work\n---\n")

	s := m.session("s1")
	s.Program, s.Env = "claude", []string{"CLAUDE_CONFIG_DIR=" + alt}
	m.sess = "c1"
	dbDo(m, m.cu.shown(m))
	m.sess = "s1"
	dbDo(m, m.cu.shown(m))

	if m.cu.tgt.agent != "claude" || m.cu.tgt.conf != alt || !slices.Equal(custNames(m.cu.got, custSkills, 0), []string{"work"}) {
		t.Fatalf("a profile's claude: %+v", m.cu.tgt)
	}

	// The picker lists what is installed, and a pick stands.
	m.focus = m.colOf(viewCustom)
	m.modal = nil
	dbDo(m, m.cu.pick(m))

	if m.modal == nil {
		t.Fatal("no picker")
	}

	var picked bool

	for _, it := range m.modal.items {
		if it.label == "codex" {
			m.modal = nil
			dbDo(m, it.run(m))

			picked = true
		}
	}

	if !picked || m.cu.tgt.agent != "codex" {
		t.Fatalf("picked %v: %+v", picked, m.cu.tgt)
	}

	dbDo(m, m.cu.shown(m))

	if m.cu.tgt.agent != "codex" {
		t.Fatalf("the pick did not stand: %+v", m.cu.tgt)
	}
}

// TestCustomizationsTab holds where the view lives: a tab in Sessions' header,
// which costs Sessions' column no activity bar and no width, whatever the
// config lists, and goes where Sessions goes.
func TestCustomizationsTab(t *testing.T) {
	m, _ := custModel(t)
	m.st.Settings.Right = proto.Columns{{Views: []string{"agents", "customizations"}}}
	m.resize()

	s := m.colOf(viewAgents)
	if s < 0 || s != m.colOf(viewCustom) || m.barW(s) != 0 || m.barH(s) != 0 || len(m.colViews(s)) != 1 {
		t.Fatalf("column %d, bar %d×%d, views %v", s, m.barW(s), m.barH(s), m.colViews(s))
	}

	for _, c := range m.cols() {
		if slices.Contains(c.views, viewCustom) {
			t.Fatalf("a column holds it: %+v", c)
		}
	}

	if !strings.Contains(checkWidths(t, m), "Sessions  Customizations") || m.shown(viewCustom) {
		t.Fatalf("the header has no tabs:\n%s", checkWidths(t, m))
	}

	// The second tab by mouse: the header is the sidebar's first row under the frame.
	rc := m.colRect(s)
	top := bodyY(t, m, "Sessions  Customizations")

	m.Update(tea.MouseMotionMsg{X: rc.x + 5, Y: top})
	dbDoClick(m, rc.x+m.headTabs(viewAgents, rc.w)[1].x, top)

	if !m.shown(viewCustom) || m.shown(viewAgents) || m.focus != s {
		t.Fatalf("the tab did not switch: focus %d, custom %v", m.focus, m.shown(viewCustom))
	}

	if out := checkWidths(t, m); !strings.Contains(out, "claude ▾") {
		t.Fatalf("no list:\n%s", out)
	}

	// A rule sits under the tabs: a click there is nothing, the list starts below it.
	click(m, rc.x+3, top+1, tea.MouseLeft)

	if m.modal != nil || !m.shown(viewCustom) {
		t.Fatalf("the rule took a click: %+v", m.modal)
	}

	click(m, rc.x+3, top+2, tea.MouseLeft)

	if m.modal == nil {
		t.Fatal("the agent row is not the list's first")
	}

	press(m, "esc")

	// Another column's view leaves the tab as it is; 3 and the first tab go back.
	press(m, "1")

	if !m.shown(viewCustom) {
		t.Fatal("showing Explorer closed the tab")
	}

	press(m, "3")

	if m.shown(viewCustom) || !m.shown(viewAgents) {
		t.Fatal("3 does not bring Sessions back")
	}

	dbDo(m, m.showView(viewCustom))
	click(m, rc.x+3, top, tea.MouseLeft)

	if !m.shown(viewAgents) {
		t.Fatal("the first tab does not bring Sessions back")
	}

	// Switching moves nothing above the list: not the frame's title, the tabs,
	// or, with someone waiting, a count on them.
	m.sessions[0].Attention, m.sessions[0].Status = true, "blocked"
	m.mouseAt = time.Time{} // the mouse gone: each view has actions of its own

	header := func(v view) string {
		dbDo(m, m.showView(v))

		return m.sideTitle(s) + "\n" + strings.Split(checkWidths(t, m), "\n")[top]
	}

	if a, b := header(viewAgents), header(viewCustom); m.attentionCount() == 0 || ansi.Strip(a) != ansi.Strip(b) {
		t.Fatalf("the header moved:\n%s\n%s", a, b)
	}

	// Nor does the mouse: the actions the tabs leave room for show, in order.
	press(m, "3")

	iconsNerd = true
	defer func() { iconsNerd = false }()

	m.mouseAt, m.mouseX, m.mouseY = time.Now(), rc.x+5, top
	acts, tabs := m.headerActions(s, viewAgents, 32), m.headTabs(viewAgents, 32)

	if end := tabs[1].x + tabs[1].w; len(acts) == 0 || end >= acts[0].x || tabs[1].w != len("Customizations") {
		t.Fatalf("tabs %+v, actions %+v", tabs, acts)
	}

	if !strings.HasPrefix(acts[0].tip, "New Session") {
		t.Fatalf("first action %q", acts[0].tip)
	}

	iconsNerd = false

	// Its menu moves Sessions, the tab with it.
	m.tabMenu(viewCustom, 0, 0)

	if m.modal == nil || m.modal.title != "Sessions" {
		t.Fatalf("menu %+v", m.modal)
	}
}

// TestCustomizationsRows holds what the list spends a row on: a section names
// its only scope itself, scope rows stay where both have entries, and details
// line up in a column that folding does not move.
func TestCustomizationsRows(t *testing.T) {
	m, home := custModel(t)
	mustWrite(t, filepath.Join(home, ".claude", "skills", "a-longer-skill", "SKILL.md"), "---\ndescription: Long one\n---\n")

	dbKey(m, "8")
	custUnfold(m, custHooks)
	custUnfold(m, custRules)

	column := func(want string) (int, string) {
		out := ansi.Strip(checkWidths(t, m))

		for l := range strings.Lines(out) {
			if i := strings.Index(l, want); i >= 0 {
				return strWidth(l[:i]), l
			}
		}

		t.Fatalf("no %q:\n%s", want, out)

		return 0, ""
	}

	if _, l := column("Hooks"); !strings.Contains(l, "user") {
		t.Fatalf("Hooks does not name its scope: %q", l)
	}

	if _, l := column("Skills"); strings.Contains(l, "user") {
		t.Fatalf("Skills names one scope of two: %q", l)
	}

	column("Workspace") // Skills' and Instructions' only: Hooks has none

	if slices.ContainsFunc(m.cu.rows(m), func(r custRow) bool { return r.kind == cuScope && r.sec == custHooks }) {
		t.Fatal("Hooks has a scope row")
	}

	long, _ := column("Long one")
	short, _ := column("Lazy senior")
	hook, _ := column("notify.sh")

	if long != short || long != hook {
		t.Fatalf("details at %d, %d, %d", long, short, hook)
	}

	m.cu.open[custSkills] = false

	if at, _ := column("notify.sh"); at != hook {
		t.Fatalf("folding moved the column from %d to %d", hook, at)
	}
}

// bodyY is the screen row holding s.
func bodyY(t *testing.T, m *Model, s string) int {
	t.Helper()

	i := slices.IndexFunc(strings.Split(checkWidths(t, m), "\n"), func(l string) bool { return strings.Contains(l, s) })
	if i < 0 {
		t.Fatalf("no %q", s)
	}

	return i
}

// dbDoClick clicks and runs what the click starts.
func dbDoClick(m *Model, x, y int) {
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	dbDo(m, cmd)
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// TestCustomizationsAbsent holds that the tab stays out where no agent is
// installed: 8 does nothing, the header and the palette do not list it.
func TestCustomizationsAbsent(t *testing.T) {
	m := testModel(t)
	press(m, "8")

	if m.shown(viewCustom) || strings.Contains(checkWidths(t, m), "Sessions  Customizations") {
		t.Fatal("shown without an agent")
	}

	if slices.ContainsFunc(m.viewItems(), func(it item) bool { return strings.Contains(it.label, "Customizations") }) {
		t.Fatal("in the palette without an agent")
	}
}
