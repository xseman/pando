# Configuration and state

Where pando keeps its files, every `config.toml` setting with its default, and
what `state.json` remembers. Keys: [09-keys.md](09-keys.md).

[Files](#files) · [Changes](#changes) · [Settings](#settings) ·
[Tables](#tables) · [Shell](#shell) · [Columns](#columns) · [Keys](#keys) ·
[Language servers](#language-servers-and-formatters) · [Colors](#colors) ·
[state.json](#statejson) · [Drafts](#drafts)

## Files

```text
config   $PANDO_CONFIG_DIR | os.UserConfigDir()/pando (~/.config/pando)
├ config.toml   what you configure
└ state.json    what pando remembers
data     $PANDO_DATA_DIR | $XDG_DATA_HOME/pando | ~/.local/share/pando
├ drafts/       unsaved editor text
└ worktrees/<project>/<branch>/   worktrees pando creates
runtime  $PANDO_RUNTIME_DIR | $XDG_RUNTIME_DIR/pando | /tmp/pando-<uid>
├ pando.sock    the socket every client dials (path ≤ ~104 bytes)
├ pando.lock    flock: one daemon per runtime dir
└ daemon.log    daemon stderr: config errors, unknown keys
```

## Changes

```text
TUI setting ─┐
pando set ───┴ state.set ─▶ daemon ─ saveConfig ─▶ config.toml
                              ▲  │
config.toml edited ───────────┘  └─ "state" event ─▶ every TUI
  (mtime polled every 0.5 s, reloadConfig)
```

- `pando set KEY VALUE` takes VALUE as JSON, else a string; unknown keys fail.
- `encode` rewrites the file with its own comments; yours are lost. Unknown
  keys in it are logged.
- A file that does not parse is kept; the last good values (at startup the
  defaults) stay and saving is refused until it is fixed.

## Settings

| Key                   | Default     | Meaning                                                                        |
| --------------------- | ----------- | ------------------------------------------------------------------------------ |
| `color_theme`         | `vscode`    | `vscode` (by background), `vscode-dark`, `vscode-light`, `terminal` (ANSI)     |
| `icons`               | `ascii`     | `ascii`, `nerd` (needs a Nerd Font), `emoji`                                   |
| `activity_bar`        | `top`       | `top` (above the view), `side` (down the sidebar edge)                         |
| `panel_borders`       | `true`      | titled border around each panel                                                |
| `animations`          | `true`      | names morph, counts roll, busy labels shimmer                                  |
| `width`               | `40`        | default column width                                                           |
| `width_right`         | `32`        | right                                                                          |
| `left`, `right`       | `[]`        | sidebar columns: [Columns](#columns)                                           |
| `hidden`              | `true`      | Explorer shows dotfiles                                                        |
| `explorer_sort`       | `name`      | Explorer files by `name` or `type` (extension, then name), folders first       |
| `git_deco`            | `true`      | Explorer colors entries by git status                                          |
| `git_tree`            | `true`      | Source Control changes as a tree                                               |
| `git_drawers`         | 5 drawers   | Graph, Commits, Branches, Remotes, Stashes; top to bottom, any drawer's title  |
| `git_panes`           | none        | per drawer: `open`, `h` rows; the TUI writes it                                |
| `diff_view`           | `inline`    | `inline`, `split` (from 90 columns)                                            |
| `diff_full`           | `false`     | a file's diff shows the whole file, not only the changes (`z`)                 |
| `quick_open_tree`     | `false`     | quick open groups by directory                                                 |
| `breadcrumbs`         | `true`      | a file's path under its tab; click a folder to browse it                       |
| `terminal_position`   | `bottom`    | `bottom`, `left`, `right`                                                      |
| `terminal_height`     | `12`        | bottom panel rows                                                              |
| `terminal_open`       | `false`     | panel state for a worktree without its own: the last set                       |
| `shell`               | `""`        | command every terminal opens: [Shell](#shell)                                  |
| `session_position`    | `right`     | `right`, `left` (a column), `editor` (over the editor area)                    |
| `session_highlight`   | `tint`      | waiting, done or failed session: `tint` (pulsing), `steady`, `off`             |
| `spaces_sort`         | `created`   | `created`, `updated` (last output)                                             |
| `spaces_group`        | `workspace` | `workspace`, `time` (Today, Yesterday, …)                                      |
| `spaces_project_sort` | `manual`    | `manual` (drag, `alt+↑↓`), `updated` (sessions' last output)                   |
| `spaces_hide`         | `[]`        | states left out: `blocked` `running` `done` `idle` `exited`                    |
| `sounds`              | `true`      | sound for a session out of view                                                |
| `sound_done`          | desktop     | file on finish; `""`: bell                                                     |
| `sound_request`       | desktop     | file on a question; `""`: bell                                                 |
| `update_check`        | `true`      | ask GitHub daily for a newer pando                                             |
| `claude_background`   | `true`      | resume claude in the background (`pando claude`)                               |
| `vim_mode`            | `false`     | editors open in vim normal mode                                                |
| `word_wrap`           | `false`     | wrap long lines; off, a horizontal scrollbar                                   |
| `tab_size`            | `4`         | cells a tab draws, spaces per indent                                           |
| `insert_spaces`       | `false`     | `tab` inserts `tab_size` spaces                                                |
| `render_whitespace`   | `selection` | `·` `→` for blanks: `none`, `boundary`, `selection`, `trailing`, `all`         |
| `markdown_width`      | `80`        | rendered Markdown width, margins included; `0` fills the panel                 |
| `editor_limit`        | `0`         | most editor tabs, least recent closing; `0` none                               |
| `format_on_save`      | `true`      | `ctrl+s` runs the `[format]` tool                                              |

Desktop: freedesktop `complete.oga`, `dialog-information.oga`.

A new setting: a tagged `proto.Settings` field, its default in
`defaultConfig` (`internal/daemon/config.go`), a `kv` line in `encode` (which
rewrites the file: no line, never saved), a `settingsItems` toggle if any.

## Tables

| Table          | Maps, and built-in defaults                                                         |
| -------------- | ----------------------------------------------------------------------------------- |
| `[keys]`       | key to command id, `""` unbinds                                                     |
| `[colors]`     | palette key to `#rrggbb` or ANSI 0–255                                              |
| `[lsp]`        | extension or language id to server; go, typescript, javascript built in             |
| `[format]`     | extension or language id to formatter; go: `gofmt` built in                         |
| `[profiles]`   | per preset, profile name to the `KEY=VALUE` a new session of it can run with        |
| `[agents]`     | preset to a new session's command: claude, codex, gemini, opencode, shell, terminal |
| `[resume]`     | program to its "continue last": claude, codex, opencode                             |
| `[resume_id]`  | program to its "continue `{id}`": claude                                            |
| `[resume_job]` | program to its "re-attach job `{id}`": claude                                       |
| `[resume_env]` | program to the variables naming its config: `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, …    |

The last five replace the built-ins (an `[agents]` without `codex` drops it);
the rest add. Resume: [01-architecture.md](01-architecture.md).

## Shell

Every terminal opens the first of `shellCandidates` that starts: `shell`; the
kind's `[agents]` preset, then `shell`'s; `$SHELL` (else `/etc/passwd`);
`bash`; `/bin/sh`. One that fails within two seconds hands over to the next in
the same session (`fallBack`), which says so on screen. On Windows the `shell`
preset is `powershell`, and `powershell`, `cmd` stand in for `bash`, `/bin/sh`.

## Columns

```toml
left  = [{ views = ["agents"], width = 22 }, { views = ["files", "git", "search"] }]
right = []
```

Columns run from the screen edge toward main. Views: `files`, `git`, `agents`,
`search`, `session`. A flat list is one column; an unlisted view joins the left
column next to main.

## Keys

```toml
[keys]
"ctrl+g" = "view.showSearch"
"ctrl+b" = ""                   # unbind
```

An id is the palette label in camelCase (`git.switchBranch`); `pando doctor`
lists them, bar the contextual `view.closeOtherEditors`, `view.killTerminal`,
`view.nextTerminal`, `view.previousTerminal`. A binding beats pando's own and
never fires in a text box or a terminal, but does in an editable file: a bound
or unbound printable key can no longer be typed there.

## Language servers and formatters

```toml
[lsp]
zig    = ["zls"]                              # by extension
python = ["pyright-langserver", "--stdio"]    # by language id
[format]
ts     = ["prettier", "--stdin-filepath", "$FILE"]
```

The extension beats the language id. A formatter pipes stdin to
stdout; `$FILE` is the path. Lookup and TypeScript: [04-preview.md](04-preview.md).

## Colors

```toml
[colors]
accent = "#ff8800"
sel_bg = "8"           # an ANSI color number
```

Overrides survive a theme switch; invalid ones are skipped. Keys (`colorKeys`):

- git: `modified` `untracked` `added` `renamed` `deleted` `conflict` `ignored`
- lists: `sel_bg` `sel_unfocused_bg` `sel_fg` `hover_bg` `section_bg` `description`
- accent: `accent` `header_accent` `sash_hover`
- buttons: `button_bg` `button_fg` `button_hover_bg` `button_sep` `muted_button_bg` `muted_button_fg`
- inputs: `input_border` `input_bg` `badge_bg` `badge_fg` `keycap_bg` `keycap_fg`
- editor: `text_sel_bg` `match_bg` `word_hi_bg` `range_hi_bg` `find_match_bg` `find_hit_bg` `whitespace` `line_number` `line_number_active`
- diff: `diff_del_bg` `diff_del_word_bg` `diff_add_bg` `diff_add_word_bg` `diff_del_mark` `diff_add_mark`
- merge: `merge_current_head_bg` `merge_current_bg` `merge_incoming_head_bg` `merge_incoming_bg` `merge_common_head_bg` `merge_common_bg`
- status: `ok` `warn` `error` `attention`
- sessions: `blocked_bg` `done_bg` `blocked_soft_bg` `done_soft_bg`
- chrome: `tab_border` `scrollbar_slider` `scrollbar_slider_hover` `scrollbar_slider_active` `overview_ruler_border`
- Markdown: `md_code` `md_code_bg`

## state.json

Written by the daemon (`save`). A TUI sends its per-workspace entries on its
tick, on leaving the workspace and on quitting.

| Key              | Holds; how an entry is forgotten                                |
| ---------------- | --------------------------------------------------------------- |
| `projects`       | project paths in Spaces order                                   |
| `worktrees`      | per project, worktree order from `workspace.move`               |
| `sessions`       | session specs a restarted daemon respawns                       |
| `drafts`         | per repository, the commit message; `""`                        |
| `editors`        | per workspace, the tab strip; `null` or empty `open`            |
| `terminals`      | per workspace, panel `open` and the shell `tab`; `null`         |
| `session_views`  | per workspace, session `position` and `width` (0: half); `null` |
| `last_workspace` | reopened when pando starts outside a repository                 |
| `spaces_folded`  | folded Spaces headings, paths and `time:…`; sent whole          |
| `seen_env`       | per program, the `[resume_env]` values seen; never, edit it out |

```json
"editors": { "/home/me/app": { "active": 0, "open": [
	{ "path": "/home/me/app/main.go", "line": 41, "col": 3, "top": 30 },
	{ "path": "", "name": "Untitled-2", "line": 0, "col": 5, "md": 0 } ] } }
```

`editors` keeps files, not diffs: 0-based cursor, top row, `md` (0 source, 1
rendered, 2 both); no `path` is untitled. `terminals` and `session_views` are
sent with `terminal_open`, `session_position` and `left`/`right`, the fallback
for a worktree without an entry.

## Drafts

Unsaved editor text, one file per draft in the data dir:

```text
drafts/<hash16(workspace)>/<hash16(path, or "untitled:"+name)>.json
{"ws":"/home/me/app","path":"/home/me/app/main.go","text":"…","mod":"2026-…"}
```

`mod` is the mtime the editor read, so a file changed meanwhile asks before it
is overwritten. The tick sends `draft.set` (no broadcast; empty `text`
forgets); a save or a discarding close drops it. `draft.list` lists them.
