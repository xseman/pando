#!/usr/bin/env bash
# The config.toml every tape writes before it starts pando. Sourced by
# setup.tape, so a tape names only what makes it different:
#
#   cfg 26 ctrl+k=editor.saveFile        # width, then key bindings
#   cfg 34 claude-edits                  # claude with edits and Bash allowed, no prompts (cli, tui)
#   cfg 34 claude-edits claude-ask       # and agent ask: one that asks before every command, whatever settings.json says
#   cfg 26 vim ctrl+k=editor.saveFile    # a setting of its own
#   cfg 30 resume                        # claude resumed in the foreground, in the demo's palette
#
# VHS cannot send ctrl+s, F12 or a drag, so every tape binds what it needs to a
# ctrl letter; keeping the presets here is what stops every tape from repeating
# the same escaped printf.

# The palette pando and the agent record in, "light" or "dark". Keep it in step
# with Set Theme in settings.tape, which paints the terminal around them.
mode=light
# mode=dark

# The command resume.tape types into its shell session, after cfg has written
# claude.json. Its "tui": "default" keeps claude in its classic renderer, the
# one the GIFs show, whatever the recording machine's settings.json picked (a
# fullscreen claude draws on the alternate screen and looks nothing like them).
claude_cmd="claude --no-chrome --strict-mcp-config --permission-mode acceptEdits --settings $PANDO_CONFIG_DIR/claude.json"

# The claude a tape runs is a stranger's: a config directory of its own with the
# login and nothing else, so no CLAUDE.md, memory, hooks, plugins, status line
# or history of the person recording shows up in a GIF, and no update banner,
# account name or connected services either (--no-chrome: the browser
# extension's first-run dialog would take the keyboard; --strict-mcp-config: a
# resumed claude lists the account's connectors as needing authentication).
# Only the credentials file is carried over, and it is never on screen. Without
# one (macOS keeps it in the keychain) claude runs in the real config
# directory, and what the GIFs show is checked by eye. The plan's name in
# claude's banner is still there.
demo_claude() {
	: "${demo_claude_src:=${CLAUDE_CONFIG_DIR:-$HOME/.claude}}"
	local dir=$STATE/claude
	export DISABLE_AUTOUPDATER=1 CLAUDE_CODE_HIDE_ACCOUNT_INFO=1 ENABLE_CLAUDEAI_MCP_SERVERS=false
	[ -f "$demo_claude_src/.credentials.json" ] || return 0
	mkdir -p "$dir"
	install -m600 "$demo_claude_src/.credentials.json" "$dir/.credentials.json"
	printf '{"hasCompletedOnboarding": true, "theme": "%s", "projects": {"%s": {"hasTrustDialogAccepted": true}, "%s": {"hasTrustDialogAccepted": true}}}\n' "$mode" "$REPO" "$STATE" > "$dir/.claude.json"
	export CLAUDE_CONFIG_DIR=$dir
}

cfg() {
	local width=$1 top="" agents="" rid="" keys="" a s
	shift
	# claude's settings as a file: a resume command is typed into a shell,
	# where an argument holding JSON would not survive.
	s='"--settings", "'$PANDO_CONFIG_DIR/claude.json'"'
	for a in "$@"; do
		case $a in
		vim) top+=$'vim_mode = true\n' ;;
		resume) # the resumed claude in the foreground, not a Claude Code background
			# job that outlives the recording, and in the palette it started in
			top+=$'claude_background = false\n'
			rid+="claude = [\"claude\", \"--no-chrome\", \"--strict-mcp-config\", \"--permission-mode\", \"acceptEdits\", $s, \"--resume\", \"{id}\"]"$'\n' ;;
		claude-ask) agents+="ask = [\"claude\", \"--no-chrome\", \"--strict-mcp-config\", \"--permission-mode\", \"default\", $s]"$'\n' ;;
		claude-edits) agents+="claude = [\"claude\", \"--no-chrome\", \"--strict-mcp-config\", \"--permission-mode\", \"acceptEdits\", \"--allowedTools\", \"Bash\", $s]"$'\n' ;;
		*=*) keys+="\"${a%%=*}\" = \"${a#*=}\""$'\n' ;;
		*) echo "cfg: unknown argument $a" >&2 ;;
		esac
	done
	mkdir -p "$PANDO_CONFIG_DIR"
	demo_claude
	# The env is claude's own: every claude started with this file, a resumed one
	# after a daemon restart too, whatever its shell inherited.
	printf '{"theme": "%s", "tui": "default", "env": {"DISABLE_AUTOUPDATER": "1", "CLAUDE_CODE_HIDE_ACCOUNT_INFO": "1", "ENABLE_CLAUDEAI_MCP_SERVERS": "false"}}\n' "$mode" > "$PANDO_CONFIG_DIR/claude.json"
	# Top-level keys first: after a [table] header they would belong to it.
	# Every panel on the left, the session over the editor area: at the 98
	# columns a GIF records in, a session docked beside the editor leaves
	# both too narrow to read, and a file opened shows in front of it.
	{
		printf 'icons = "nerd"\npanel_borders = false\ncolor_theme = "vscode-%s"\nwidth = %s\nsession_position = "editor"\n%s' "$mode" "$width" "$top"
		[ -n "$agents" ] && printf '[agents]\n%s' "$agents"
		[ -n "$rid" ] && printf '[resume_id]\n%s' "$rid"
		[ -n "$keys" ] && printf '[keys]\n%s' "$keys"
	} > "$PANDO_CONFIG_DIR/config.toml"
}
