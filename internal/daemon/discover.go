package daemon

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// configMark is how an agent's config directory is told from any other: the
// variable that picks it, what it is under the home directory when unset, the
// file the agent keeps its sign-in's account in, from the directory the
// variable names, and where in that JSON the account's id is: keys joined by
// dots, * for any key, and a space between the parts of an id made of more
// than one, the first of which a sign-in always has.
type configMark struct{ key, dir, file, id string }

// configMarks are the agents whose config directories pando finds on its
// own, by [agents] preset; another agent is a line here. Claude Code keeps
// the default config's account file beside the directory, ~/.claude.json,
// any other inside it, and its account is the user's, the same in each
// organization they are in, which have limits of their own. GEMINI_CLI_HOME stands in for the home directory, the
// config is the .gemini in it.
//
// ponytail: no opencode, whose docs name no variable that moves its
// auth.json, and no agy, whose sign-in is in the keyring.
var configMarks = map[string]configMark{
	"claude":  {"CLAUDE_CONFIG_DIR", ".claude", ".claude.json", "oauthAccount.accountUuid oauthAccount.organizationUuid"},
	"codex":   {"CODEX_HOME", ".codex", "auth.json", "tokens.account_id"},
	"grok":    {"GROK_HOME", ".grok", "auth.json", "*.principal_id"},
	"copilot": {"COPILOT_HOME", ".copilot", "config.json", "lastLoggedInUser.login"},
	"gemini":  {"GEMINI_CLI_HOME", "", ".gemini/google_accounts.json", "active"},
}

// scanDepth is how many directories under the home directory a config
// directory is looked for: ~/a/b/c at 3.
const scanDepth = 3

// scanSkip are the directories no agent's config is looked for under:
// dependencies, caches, toolchains and other programs' own data.
var scanSkip = []string{
	"node_modules", "vendor", "target", "dist", "build", "__pycache__", ".venv", "venv",
	".git", ".cache", ".npm", ".pnpm-store", ".yarn", ".bun", ".cargo", ".rustup", ".gradle", ".m2",
	".nvm", ".pyenv", ".rbenv", ".asdf", "go", "mise",
	"Trash", "Library", ".var", "snap", ".steam", ".mozilla", ".thunderbird", ".docker", "containers",
	// The user's own files, which macOS asks before letting a program read, and mounts.
	"Documents", "Desktop", "Downloads", "Pictures", "Music", "Movies", "Videos", "Public", "Templates", "mnt",
}

// accountID is the account the sign-in under dir is of, as m's file names
// it. "" for no sign-in, or another program's file of the same name.
func accountID(dir string, m configMark) string {
	b, _ := os.ReadFile(filepath.Join(dir, m.file)) // none reads as no sign-in

	var lines []string

	for l := range strings.Lines(string(b)) { // Copilot heads its config.json with // comments
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			lines = append(lines, l)
		}
	}

	var v any

	_ = json.Unmarshal([]byte(strings.Join(lines, "")), &v) // not JSON reads as no sign-in

	var id []string
	for _, path := range strings.Fields(m.id) {
		id = append(id, jsonString(v, strings.Split(path, ".")))
	}

	if id[0] == "" {
		return ""
	}

	return strings.Join(id, " ")
}

// jsonString is the string at path in decoded JSON v, "" for none; a * in
// path is any key, the first in order that leads to one.
func jsonString(v any, path []string) string {
	if len(path) == 0 {
		s, _ := v.(string)

		return s
	}

	obj, _ := v.(map[string]any)
	if path[0] != "*" {
		return jsonString(obj[path[0]], path[1:])
	}

	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if s := jsonString(obj[k], path[1:]); s != "" {
			return s
		}
	}

	return ""
}

// configsUnder are the directories under home that hold an agent's account
// file, per agent: the file's own directory scanDepth deep, so a
// GEMINI_CLI_HOME one less, scanSkip left out. The one whose file was written
// last comes first: of a config and its copies, that is the one in use.
//
// ponytail: a symlinked directory is not followed, so a config kept behind
// one is found only once it runs (configsRunning) or a profile names it.
//
// ponytail: other filesystems are walked too, and a network mount that
// stopped answering holds the walk and with it refreshUsage; skip mounts by
// device when someone keeps one in the home directory outside scanSkip.
func configsUnder(home string) map[string][]string {
	out := map[string][]string{}

	_ = filepath.WalkDir(home, func(path string, e fs.DirEntry, err error) error {
		if err != nil { // a directory that cannot be read: the walk goes on beside it
			return nil //nolint:nilerr // skipping it is the handling
		}

		if e.IsDir() {
			depth := strings.Count(path[len(home):], string(filepath.Separator))
			if depth > scanDepth || slices.Contains(scanSkip, e.Name()) {
				return fs.SkipDir
			}

			return nil
		}

		for agent, m := range configMarks {
			dir, ok := strings.CutSuffix(path, string(filepath.Separator)+filepath.FromSlash(m.file))
			if ok && dir != home {
				out[agent] = append(out[agent], dir)
			}
		}

		return nil
	})

	for agent, dirs := range out {
		slices.SortStableFunc(dirs, func(a, b string) int {
			return accountTime(b, configMarks[agent]).Compare(accountTime(a, configMarks[agent]))
		})
	}

	return out
}

// accountTime is when the account file under dir was last written, zero for
// none.
func accountTime(dir string, m configMark) time.Time {
	info, err := os.Stat(filepath.Join(dir, m.file))
	if err != nil {
		return time.Time{}
	}

	return info.ModTime()
}

// configsRunning are the config directories the user's processes run with,
// per agent, wherever on the system they are but in the temporary
// directory, where a test's or a tool's config is gone tomorrow and
// d.state.Seen would keep it. Nothing where there is no /proc.
func configsRunning() map[string][]string {
	out := map[string][]string{}

	tmp := filepath.Clean(os.TempDir()) + string(filepath.Separator)

	paths, _ := filepath.Glob("/proc/[0-9]*/environ") // the pattern is well-formed
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil { // another user's, or gone meanwhile
			continue
		}

		for kv := range strings.SplitSeq(string(b), "\x00") {
			for agent, m := range configMarks {
				dir, ok := strings.CutPrefix(kv, m.key+"=")
				if ok && filepath.IsAbs(dir) && !strings.HasPrefix(filepath.Clean(dir), tmp) {
					out[agent] = append(out[agent], filepath.Clean(dir))
				}
			}
		}
	}

	return out
}

// discover records in d.state.Seen each of found's directories that holds a
// sign-in of an account no config pando knows has: the default, a profile's
// or one it saw. A copy of a config, the same account again, is not a
// profile: the first of found with an account stands for it, for good. It
// reports a new one; d.mu is held.
func (d *Daemon) discover(home string, found map[string][]string) bool {
	saw := false

	for agent, dirs := range found {
		argv := d.state.Agents[agent]
		if len(argv) == 0 { // no preset to start it with
			continue
		}

		if _, err := exec.LookPath(argv[0]); err != nil { // not installed: a file of its name is another program's
			continue
		}

		m := configMarks[agent]
		known := []string{accountID(home, m), accountID(cmp.Or(os.Getenv(m.key), filepath.Join(home, m.dir)), m)}

		for _, env := range slices.Concat(d.state.Seen[agent], slices.Collect(maps.Values(d.state.Settings.Profiles[agent]))) {
			for _, kv := range homeEnv(env) {
				if dir, ok := strings.CutPrefix(kv, m.key+"="); ok {
					known = append(known, accountID(dir, m))
				}
			}
		}

		for _, dir := range dirs {
			id := accountID(dir, m)
			if id == "" || slices.Contains(known, id) {
				continue
			}

			known = append(known, id)
			saw = d.see(agent, []string{m.key + "=" + dir}) || saw
		}
	}

	return saw
}
