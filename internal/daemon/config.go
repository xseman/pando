package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/xseman/pando/internal/proto"
)

// config.toml holds what a user configures: settings and agent presets.
// state.json keeps what pando remembers: projects, drafts and sessions.
type config struct {
	proto.Settings
	Agents map[string][]string `toml:"agents"`
	Resume map[string][]string `toml:"resume"`
	// ResumeID continues the very conversation a session had open, when its
	// agent says which; {id} in an argument becomes the conversation's id.
	ResumeID map[string][]string `toml:"resume_id"`
	// ResumeJob attaches to the background job a session was attached to,
	// while it still runs; {id} becomes the job's id.
	ResumeJob map[string][]string `toml:"resume_job"`
}

// nerdFont reports whether fontconfig knows a Nerd Font, once per process.
var nerdFont = sync.OnceValue(func() bool {
	out, err := exec.Command("fc-list", ":", "family").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "nerd")
})

func defaultConfig() config {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// The desktop's own event sounds, so nothing ships with pando.
	done, req := "/usr/share/sounds/freedesktop/stereo/complete.oga", "/usr/share/sounds/freedesktop/stereo/dialog-information.oga"
	if runtime.GOOS == "darwin" {
		done, req = "/System/Library/Sounds/Glass.aiff", "/System/Library/Sounds/Ping.aiff"
	}

	return config{
		Settings: proto.Settings{
			Hidden: true, Width: 40, WidthR: 32, GitDeco: true, GitTree: true,
			Theme: "vscode", DiffView: "inline", Borders: true, FmtSave: true,
			ActBar: "top", TermPos: "bottom", TermH: 12, SessPos: "right", SessHi: "tint",
			Sounds: true, SoundDone: done, SoundReq: req, Updates: true,
			Drawers: []string{"Commits", "Graph", "Branches", "Stashes", "Remotes"},
		},
		Agents: map[string][]string{
			"claude": {"claude"}, "codex": {"codex"}, "gemini": {"gemini"},
			"opencode": {"opencode"}, "shell": {shell}, "terminal": {shell},
		},
		Resume: map[string][]string{
			"claude": {"claude", "--continue"}, "codex": {"codex", "resume", "--last"},
			"opencode": {"opencode", "--continue"},
		},
		ResumeID:  map[string][]string{"claude": {"claude", "--resume", "{id}"}},
		ResumeJob: map[string][]string{"claude": {"claude", "attach", "{id}"}},
	}
}

// resolve fills values that depend on the machine.
func (c *config) resolve() {
	if c.Icons == "" {
		c.Icons = "emoji"
		if nerdFont() {
			c.Icons = "nerd"
		}
	}
}

// loadConfig reads path over the defaults. An [agents] table replaces the
// default presets, so a preset can be removed by leaving it out.
func loadConfig(path string) (config, error) {
	c := defaultConfig()
	agents, resume, resumeID, resumeJob := c.Agents, c.Resume, c.ResumeID, c.ResumeJob
	c.Agents, c.Resume, c.ResumeID, c.ResumeJob = nil, nil, nil, nil

	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		c = defaultConfig()
		c.resolve()

		return c, err
	}

	for _, k := range md.Undecoded() {
		fmt.Fprintf(os.Stderr, "%s: unknown key %s\n", path, k)
	}

	if c.Agents == nil {
		c.Agents = agents
	}

	if c.Resume == nil {
		c.Resume = resume
	}

	if c.ResumeID == nil {
		c.ResumeID = resumeID
	}

	if c.ResumeJob == nil {
		c.ResumeJob = resumeJob
	}

	c.resolve()

	return c, nil
}

func tomlValue(v any) string {
	b, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return `""`
	}

	return strings.TrimSpace(strings.TrimPrefix(string(b), "v = "))
}

func list(l []string) []string {
	if l == nil {
		return []string{}
	}

	return l
}

func tomlColumns(cs proto.Columns) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		views := c.Views
		if views == nil {
			views = []string{}
		}

		parts[i] = "{ views = " + tomlValue(views)
		if c.Width > 0 {
			parts[i] += fmt.Sprintf(", width = %d", c.Width)
		}

		parts[i] += " }"
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// table writes a settings map under the heading already in b; toml sorts the
// keys and quotes the ones that are not bare.
func table[V any](b *strings.Builder, m map[string]V) {
	out, err := toml.Marshal(m)
	if err != nil {
		return // the settings maps are string keys to strings or string slices
	}

	b.Write(out) // a Builder never fails
}

// encode writes every key with its documentation. ponytail: comments the
// user adds are lost on the next write; keep a line-level patcher if that bites.
func (c *config) encode() []byte {
	s := c.Settings

	var b strings.Builder

	kv := func(comment, key string, v any) {
		if comment != "" {
			b.WriteString("\n# " + strings.ReplaceAll(comment, "\n", "\n# ") + "\n")
		}

		fmt.Fprintf(&b, "%s = %s\n", key, tomlValue(v))
	}

	b.WriteString("# pando settings. pando rewrites this file when a setting changes in the TUI\n" +
		"# or through `pando set KEY VALUE`; comments other than these are not kept.\n" +
		"# Edits made here apply to running pando windows within a few seconds.\n")
	kv("Color theme: \"vscode\" follows the terminal background, \"vscode-dark\",\n\"vscode-light\", or \"terminal\" for the terminal profile's ANSI colors.", "color_theme", s.Theme)
	kv("Icons: \"nerd\" needs a Nerd Font as the terminal font, \"emoji\" and \"ascii\" work everywhere.", "icons", s.Icons)
	kv("Diffs: \"inline\" or \"split\" (side by side when the main area has 90 columns).", "diff_view", s.DiffView)
	kv("Show dotfiles in Files.", "hidden", s.Hidden)
	kv("Color Files entries by their git status.", "git_deco", s.GitDeco)
	kv("Show Source Control changes as a tree instead of a flat list.", "git_tree", s.GitTree)
	kv("Group quick open (ctrl+p) results by directory.", "quick_open_tree", s.QuickTree)
	kv("Source Control history drawers to show: Graph, Commits, File History, Branches,\nWorktrees, Remotes, Stashes, Tags.", "git_drawers", list(s.Drawers))
	kv("Frame each panel with a titled border.", "panel_borders", s.Borders)
	kv("Default widths of left and right sidebar columns without their own width.", "width", s.Width)
	kv("", "width_right", s.WidthR)
	kv("Activity bar: \"top\" (icons above the view) or \"side\" (down the sidebar's outer edge).", "activity_bar", s.ActBar)
	kv("Format a file with its [format] tool when it is saved.", "format_on_save", s.FmtSave)
	kv("Editors open in vim's normal mode: i inserts, esc goes back.", "vim_mode", s.Vim)
	kv("Play a sound when a session out of view finishes a run or waits for an answer:\nthe files played by paplay, pw-play, afplay, ffplay or mpv; \"\" rings the terminal bell.", "sounds", s.Sounds)
	kv("", "sound_done", s.SoundDone)
	kv("", "sound_request", s.SoundReq)
	kv("Check GitHub once a day for a newer pando and offer it in the status bar.\nThe update replaces the pando binary; restart pando to run it.", "update_check", s.Updates)
	kv("Terminal panel: \"bottom\" (under the editor), \"left\" or \"right\" (a sidebar column\nof its own), its height in rows, and whether it is open.", "terminal_position", s.TermPos)
	kv("", "terminal_height", s.TermH)
	kv("", "terminal_open", s.TermOpen)
	kv("Where a session opens: \"right\" or \"left\" (a column of its own beside the editor)\nor \"editor\" (over the editor area).", "session_position", s.SessPos)
	kv("A session that waits for an answer or finished unseen: \"tint\" its row and tab in\nits symbol's color (colors blocked_bg, done_bg), \"blink\" that tint, or \"off\".", "session_highlight", s.SessHi)
	b.WriteString("\n# Sidebar columns per side, from the screen edge toward main, each with its tabs\n" +
		"# and optional width. Views: \"files\", \"git\", \"agents\", \"search\", \"session\"; unlisted\n" +
		"# views join the left column next to main.\n")
	fmt.Fprintf(&b, "left = %s\nright = %s\n", tomlColumns(s.Left), tomlColumns(s.Right))

	if len(s.GitPanes) > 0 {
		b.WriteString("\n# Source Control drawers: open state and height in rows.\n")
		table(&b, map[string]any{"git_panes": s.GitPanes})
	}

	b.WriteString("\n# Language servers, keyed by file extension or LSP language id; pando starts one\n" +
		"# on the first F12 in a matching file, e.g. zig = [\"zls\"] or python = [\"pyright-langserver\", \"--stdio\"].\n" +
		"# go and typescript/javascript work out of the box when their server is installed.\n[lsp]\n")
	table(&b, s.LSP)
	b.WriteString("\n# Formatters, keyed the same way; the file goes in on stdin and the formatted\n" +
		"# text comes back on stdout, $FILE in an argument becomes the file's path, e.g.\n" +
		"# ts = [\"prettier\", \"--stdin-filepath\", \"$FILE\"] or python = [\"black\", \"-q\", \"-\"].\n" +
		"# go uses gofmt out of the box. format_on_save runs them on ctrl+s.\n[format]\n")
	table(&b, s.Format)
	b.WriteString("\n# Key bindings: a key to a command id from `pando doctor`, \"\" unbinds it,\n" +
		"# e.g. \"ctrl+g\" = \"view.showSearch\".\n[keys]\n")
	table(&b, s.Keys)
	b.WriteString("\n# Color overrides for the active theme: palette keys from `pando doctor` as\n" +
		"# \"#rrggbb\" or an ANSI color number, e.g. accent = \"#ff8800\".\n[colors]\n")
	table(&b, s.Colors)
	b.WriteString("\n# Agent presets: the command a new session of that agent runs.\n[agents]\n")
	table(&b, c.Agents)
	b.WriteString("\n# Continuing an agent: when pando restarts it types this into the session's\n" +
		"# shell instead of leaving an empty prompt, keyed by the program it saw running.\n[resume]\n")
	table(&b, c.Resume)
	b.WriteString("\n# Continuing the very conversation a session had open, when the agent says which\n" +
		"# (claude does); {id} becomes its id. Two sessions never continue the same one.\n[resume_id]\n")
	table(&b, c.ResumeID)
	b.WriteString("\n# Attaching again to the background job a session was attached to (claude attach),\n" +
		"# which outlives pando; {id} becomes the job's id. A job that ended is resumed instead.\n[resume_job]\n")
	table(&b, c.ResumeJob)

	return []byte(b.String())
}
