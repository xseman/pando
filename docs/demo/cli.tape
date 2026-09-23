# The CLI: the same daemon the TUI talks to, driven from a script.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq and a Chrome)
Output docs/demo/cli.gif

Require pando
Require ttyd
Require jq

Source docs/demo/settings.tape
Set Height 480
Source docs/demo/setup.tape
Type "clear" Enter
Show

# A project, and a branch in its own git worktree; without a name it gets
# one, herdr's worktree/<adjective>-<noun>-<hex>. Every command prints JSON.
Type "pando project add ." Enter
Sleep 1s
Type "pando ws new | jq -c" Enter
Sleep 1800ms

# A shell session, driven from the script: type into it, read its screen back.
Type "id=$(pando session new --ws . | jq -r .id)" Enter
Sleep 1200ms
Type `pando session send $id 'go run .' --enter` Enter
Sleep 1500ms
Type "pando session read $id" Enter
Sleep 2500ms

# Settings are config.toml's keys; running pando windows pick them up.
Type "pando set diff_view split && grep diff_view $PANDO_CONFIG_DIR/config.toml" Enter
Sleep 2500ms
