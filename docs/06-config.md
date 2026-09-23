# Configuration and state

Two files in `os.UserConfigDir()/pando` (Linux `~/.config/pando`), overridable
with `PANDO_CONFIG_DIR`:

```
config.toml   what you configure : settings, colors, agent presets
state.json    what pando remembers: projects, commit drafts, open editors, session specs
```

Unsaved editor text lives beside them, in the data directory: see Drafts below.

The daemon writes `config.toml` whenever a setting changes in the TUI or through
`pando set KEY VALUE`, keeping its own comments; it watches the file's mtime and
reloads edits within a second, then pushes a `state` event to every TUI.

```
edit config.toml ─mtime─▶ daemon reload ─event─▶ TUI redraw
TUI toggle ──state.set──▶ daemon ─write─▶ config.toml
```

A file that does not parse is kept and never overwritten: the daemon logs the
error, uses the last good values and refuses to save until it is fixed.

## Settings

| Key                                                                  | Values                                                                                                      |
| -------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `color_theme`                                                        | `vscode` (2026 Dark or Light by the terminal background), `vscode-dark`, `vscode-light`, `terminal`         |
| `icons`                                                              | `nerd` (needs a Nerd Font as the terminal font), `emoji` (herdr-sidebar's set), `ascii`                     |
| `activity_bar`                                                       | `top` (icons in a row above the view) or `side` (down the sidebar's outer edge)                             |
| `terminal_position`                                                  | `bottom` (a panel under the editor), `left` or `right`                                                      |
| `terminal_height`                                                    | rows of the bottom panel                                                                                    |
| `terminal_open`                                                      | whether the terminal panel is showing                                                                       |
| `session_position`                                                   | `right`, `left` (the side a session column docks to) or `editor` (over the editor area, until a file opens) |
| `session_highlight`                                                  | `tint` a session waiting (×) or done unseen (✓) in its symbol's color, `blink` that tint, or `off`          |
| `diff_view`                                                          | `inline`, `split`                                                                                           |
| `hidden`, `git_deco`, `git_tree`, `quick_open_tree`, `panel_borders` | booleans                                                                                                    |
| `sounds`, `sound_done`, `sound_request`                              | play a sound for sessions out of view; the files, `""` for the terminal bell                                |
| `update_check`                                                       | ask GitHub once a day whether a newer pando was released                                                    |
| `width`, `width_right`                                               | default column widths                                                                                       |
| `left`, `right`                                                      | sidebar columns                                                                                             |
| `[git_panes.TITLE]`                                                  | drawer `open` and `h`                                                                                       |
| `[keys]`                                                             | key to command id, `""` unbinds                                                                             |
| `[lsp]`                                                              | language id to server command                                                                               |
| `[format]`                                                           | language id to formatter command, `$FILE` for the path                                                      |
| `format_on_save`                                                     | run that formatter when a file is saved                                                                     |
| `vim_mode`                                                           | editors open in vim's normal mode                                                                           |
| `git_drawers`                                                        | which Source Control drawers show                                                                           |
| `[colors]`                                                           | palette overrides                                                                                           |
| `[agents]`                                                           | `name = ["command", "args"]`                                                                                |
| `[resume]`                                                           | program to the command that continues its last conversation                                                 |
| `[resume_id]`                                                        | program to the command that continues conversation `{id}`, when the program says which                      |
| `[resume_job]`                                                       | program to the command that attaches to background job `{id}` again while it runs                           |

## Columns

```toml
left  = [{ views = ["agents"], width = 22 }, { views = ["files", "git", "search"] }]
right = []
```

A flat list (`left = ["files", "git"]`) still reads as one column. Views missing
from both sides join the left column next to main, so a new view appears
without touching the config.

## Keys

```toml
[keys]
"ctrl+g" = "view.showSearch"
"b" = ""                       # unbind
```

Ids are the command palette's entries (`Git: Switch Branch…` is
`git.switchBranch`); `pando doctor` prints them. A binding goes before
pando's own keys, so `ctrl+b` or `ctrl+p` can be rebound or unbound too. It
never fires while a text box has the caret, and never inside an agent session.

Actions that had only a key have a command too: `view.focusSidebar`,
`view.focusEditor`, `view.focusTerminal`, `view.previousEditor`,
`view.closeOtherEditors`, `view.newTerminal`, `view.killTerminal`,
`view.nextTerminal`, `view.previousTerminal`, `explorer.collapseAll`,
`git.collapseAll`, `git.commitSync`, `git.commitAmend`, and in an editor
`editor.find`, `editor.replace`, `editor.selectAll`, `editor.toggleWordWrap`,
`editor.cut`, `editor.duplicateLine`, `editor.deleteLine`, `editor.moveLineUp`,
`editor.moveLineDown`, `editor.copyLineUp`, `editor.copyLineDown`,
`editor.expandSelection`, `editor.shrinkSelection`, `editor.triggerSuggest`.

## Language servers

```toml
[lsp]
go = ["gopls"]                                 # a default
typescript = ["typescript-language-server", "--stdio"]
zig = ["zls"]                                  # by file extension
python = ["pyright-langserver", "--stdio"]     # by LSP language id
```

A key is a file extension or a language id; the extension wins when both
match, and an unknown extension becomes the language id sent in `didOpen`.
Go and TypeScript/JavaScript have defaults, everything else is one line. A
server starts on the first `F12`, `⇧F12` or `ctrl+.` in a matching file, one
per language and workspace.

The same server answers Go to Symbol (`ctrl+shift+o`); Markdown headings need
none.

A command without a `/` is looked up in the nearest `node_modules/.bin` above
the file first, then on `PATH`, for `[lsp]` and `[format]` alike. A package
with its own `node_modules` gets its own server. When the nearest
`node_modules/typescript` is TypeScript 7, which has no `tsserver.js` for
`typescript-language-server`, its own `tsc --lsp --stdio` is the server instead.
An older one is handed to `typescript-language-server` as `tsserver.path`: on its
own it looks only in the workspace root's `node_modules`, so a package deeper
down would get no TypeScript at all. Each TypeScript gets its own server.

## Colors

```toml
[colors]
accent = "#ff8800"
diff_add_bg = "#203928"
sel_bg = "8"           # an ANSI color number also works
```

Keys are the palette fields; `pando doctor` prints all of them with their
current values. Overrides apply on top of the selected theme, so switching
themes keeps them.

## Open editors

`state.json` keeps each workspace's tab strip, VS Code's `workbench.editors`:

```json
"editors": {
  "/home/me/app": { "open": [
    { "path": "/home/me/app/main.go", "line": 41, "col": 3, "top": 30 },
    { "path": "", "name": "Untitled-2", "line": 0, "col": 5 }
  ], "active": 0 }
}
```

Files only, with the 0-based cursor, the first visible row and the Markdown
mode; diffs and commits depend on git state that moves on. A tab with no path
is an untitled buffer, named by `name`. The TUI sends
`state.set {"editors": {WS: …}}` on its tick when the strip or a cursor changed,
when it leaves the workspace and when it quits; `null` or an empty `open`
forgets the workspace. Opening the workspace again reopens the tabs where they
were, skipping files that are gone. Two TUIs on one workspace each save their
own strip; the last one wins.

## Drafts

Unsaved editor text — an untitled buffer, or edits to a file that exists —
lives in the data directory rather than in `state.json`, one JSON file per
draft, so however much is typed the state file stays a list of paths:

```
~/.local/share/pando/drafts/<hash of the workspace>/<hash of the draft>.json
  {"ws":"/home/me/app","path":"/home/me/app/main.go","text":"…","mod":"2026-…"}
  {"ws":"/home/me/app","name":"Untitled-2","text":"…","mod":"0001-…"}
```

`mod` is the file's mtime when the editor read it, so a file someone else wrote
in the meantime still asks before it is overwritten, a restart later included.

The tick sends what changed (`draft.set`; an empty `text` forgets it) and drops
a draft as soon as its editor is saved or its tab is closed without saving.
`PANDO_DATA_DIR` overrides the directory. `pando call draft.list '{"ws":"…"}'`
lists them.

## Upgrades

An older daemon left running is detected by a build id in `ping`, and
restarted (or kept, with a warning) by the TUI.
