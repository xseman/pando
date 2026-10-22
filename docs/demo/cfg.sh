#!/usr/bin/env bash
# The config.toml every tape writes before it starts pando. Sourced by
# setup.tape, so a tape names only what makes it different:
#
#   cfg 26 ctrl+k=editor.saveFile        # width, then key bindings
#   cfg 34 claude-edits                  # claude with edits and Bash allowed, no prompts (tui)
#   cfg 34 claude-edits claude-ask       # and agent ask: one that asks before every command, whatever settings.json says
#   cfg 26 vim ctrl+k=editor.saveFile    # a setting of its own
#   cfg 30 resume                        # claude resumed in the foreground, in the demo's palette
#   cfg 22 hero                          # Spaces on the right, the rest on the left
#
# VHS cannot send ctrl+s, F12 or a drag, so every tape binds what it needs to a
# ctrl letter; keeping the presets here is what stops every tape from repeating
# the same escaped printf.

# The palette pando and the agent record in, "light" or "dark". Keep it in step
# with Set Theme in settings.tape, which paints the terminal around them.
mode=light
# mode=dark

# The command resume.tape types into its shell session.
claude_cmd="claude --permission-mode acceptEdits"

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
	# Every claude a tape starts is this one, the flags above given, and its
	# settings as a file (an argument holding JSON would not survive being typed
	# into a shell), so a command typed on screen is claude's own: a resume
	# command fits a line. cfg's claude.json has "tui": "default", claude's
	# classic renderer, the one the GIFs show, whatever the recording machine's
	# settings.json picked (fullscreen draws on the alternate screen).
	mkdir -p "$STATE/bin"
	printf '#!/bin/sh\nexec %s --no-chrome --strict-mcp-config --settings %s "$@"\n' "$(command -v claude)" "$PANDO_CONFIG_DIR/claude.json" > "$STATE/bin/claude"
	chmod +x "$STATE/bin/claude"
	export PATH=$STATE/bin:$PATH
	[ -f "$demo_claude_src/.credentials.json" ] || return 0
	mkdir -p "$dir"
	# Without the refresh token's expiry: claude warns in its last three days.
	(umask 077; jq 'del(.claudeAiOauth.refreshTokenExpiresAt)' "$demo_claude_src/.credentials.json" > "$dir/.credentials.json")
	printf '{"hasCompletedOnboarding": true, "lspRecommendationDisabled": true, "theme": "%s", "projects": {"%s": {"hasTrustDialogAccepted": true}, "%s": {"hasTrustDialogAccepted": true}}}\n' "$mode" "$REPO" "$STATE" > "$dir/.claude.json"
	export CLAUDE_CONFIG_DIR=$dir
}

cfg() {
	local width=$1 top="" agents="" keys="" a
	shift
	for a in "$@"; do
		case $a in
		vim) top+=$'vim_mode = true\n' ;;
		resume) # the resumed claude in the foreground, not a Claude Code background
			# job that outlives the recording
			top+=$'claude_background = false\n' ;;
		hero) # the README's still: every view on the left but Spaces
			top+=$'width_right = 30\nleft = [{ views = ["files", "git", "search", "github", "databases"] }]\nright = [{ views = ["agents"] }]\n' ;;
		claude-ask) agents+="ask = [\"claude\", \"--permission-mode\", \"default\"]"$'\n' ;;
		claude-edits) agents+="claude = [\"claude\", \"--permission-mode\", \"acceptEdits\", \"--allowedTools\", \"Bash\"]"$'\n' ;;
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
	# Every panel on the left. A terminal's bash skips the recorder's
	# .bashrc: its prompt is vhs's.
	{
		printf 'icons = "nerd"\npanel_borders = false\ncolor_theme = "vscode-%s"\nwidth = %s\nshell = "bash --norc"\n%s' "$mode" "$width" "$top"
		[ -n "$agents" ] && printf '[agents]\n%s' "$agents"
		[ -n "$keys" ] && printf '[keys]\n%s' "$keys"
	} > "$PANDO_CONFIG_DIR/config.toml"
}
