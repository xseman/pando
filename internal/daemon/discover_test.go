package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestDiscover(t *testing.T) {
	home := t.TempDir()
	for _, m := range configMarks { // the developer's own configs are not the defaults here
		t.Setenv(m.key, "")
	}

	claude := func(id string) string { return `{"oauthAccount":{"accountUuid":"` + id + `"}}` }
	mustWrite(t, filepath.Join(home, ".claude.json"), claude("main"))
	mustWrite(t, filepath.Join(home, ".claude", ".claude.json"), claude("main")) // a leftover in the default
	mustWrite(t, filepath.Join(home, ".claude-nike", ".claude.json"), claude("nike"))
	mustWrite(t, filepath.Join(home, "accounts", "claude", "work", ".claude.json"), claude("work"))
	mustWrite(t, filepath.Join(home, "backup", ".claude.json"), claude("nike"))                // a copy: the same account
	mustWrite(t, filepath.Join(home, "node_modules", "pkg", ".claude.json"), claude("dep"))    // skipped
	mustWrite(t, filepath.Join(home, "a", "b", "c", "d", ".claude.json"), claude("deep"))      // past scanDepth
	mustWrite(t, filepath.Join(home, "signed-out", ".claude.json"), `{"numStartups":3}`)       // no sign-in
	mustWrite(t, filepath.Join(home, ".codex", "auth.json"), `{"tokens":{"account_id":"c1"}}`) // the default
	mustWrite(t, filepath.Join(home, ".codex-two", "auth.json"), `{"tokens":{"account_id":"c2"}}`)
	mustWrite(t, filepath.Join(home, ".opencode", "auth.json"), `{"openrouter":{"key":"k"}}`) // another program's

	mustWrite(t, filepath.Join(home, ".grok", "auth.json"), `{"https://auth.x.ai::a":{"principal_id":"g1"}}`)
	mustWrite(t, filepath.Join(home, "grok-team", "auth.json"), `{"https://auth.x.ai::a":{"principal_id":"g2"}}`)
	mustWrite(t, filepath.Join(home, ".copilot", "config.json"), "// managed\n"+`{"lastLoggedInUser":{"login":"me"}}`)
	mustWrite(t, filepath.Join(home, "copilot-work", "config.json"), "// managed\n"+`{"lastLoggedInUser":{"login":"work"}}`)
	mustWrite(t, filepath.Join(home, ".gemini", "google_accounts.json"), `{"active":"me@x"}`) // the default
	mustWrite(t, filepath.Join(home, "gemini-work", ".gemini", "google_accounts.json"), `{"active":"work@x"}`)
	mustWrite(t, filepath.Join(home, "client", ".claude.json"), `{"oauthAccount":{"accountUuid":"main","organizationUuid":"client"}}`) // the user's other organization
	mustWrite(t, filepath.Join(home, "app", "config.json"), `{"port":80}`)                                                             // any program's
	mustWrite(t, filepath.Join(home, "aa-copy", ".claude.json"), claude("used"))
	mustWrite(t, filepath.Join(home, "zz-in-use", ".claude.json"), claude("used"))

	old := time.Now().Add(-time.Hour) // a copy was written before its config went on being used
	for _, dir := range []string{"aa-copy", "backup"} {
		if err := os.Chtimes(filepath.Join(home, dir, ".claude.json"), old, old); err != nil {
			t.Fatal(err)
		}
	}

	d := &Daemon{}
	d.state.Agents = map[string][]string{"claude": {"go"}, "codex": {"go"}, "grok": {"go"}, "gemini": {"go"}, "copilot": {"not-installed"}}
	saw := d.discover(home, configsUnder(home))

	want := map[string][][]string{
		"claude": {
			{"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude-nike")},
			{"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "accounts", "claude", "work")},
			{"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "client")},
			{"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "zz-in-use")}, // not aa-copy, the same account
		},
		"codex":  {{"CODEX_HOME=" + filepath.Join(home, ".codex-two")}},
		"grok":   {{"GROK_HOME=" + filepath.Join(home, "grok-team")}},
		"gemini": {{"GEMINI_CLI_HOME=" + filepath.Join(home, "gemini-work")}},
	}

	for _, envs := range d.state.Seen { // found in the order their files were written
		slices.SortFunc(envs, slices.Compare)
	}

	if !saw || !reflect.DeepEqual(d.state.Seen, want) {
		t.Errorf("seen %v (new %v), want %v", d.state.Seen, saw, want)
	}

	if d.discover(home, configsUnder(home)) {
		t.Errorf("a second look found new configs: %v", d.state.Seen)
	}
}
