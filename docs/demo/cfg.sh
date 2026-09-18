#!/usr/bin/env bash
# The config.toml every tape writes before it starts pando. Sourced by
# setup.tape, so a tape names only what makes it different:
#
#   cfg 26 ctrl+k=editor.saveFile        # width, then key bindings
#   cfg 34 claude                        # the agent preset the session tapes run
#   cfg 26 vim ctrl+k=editor.saveFile    # a setting of its own
#
# VHS cannot send ctrl+s, F12 or a drag, so every tape binds what it needs to a
# ctrl letter; keeping the presets here is what stops nine tapes from repeating
# the same escaped printf.

# The palette pando and the agent record in, "light" or "dark". Keep it in step
# with Set Theme in settings.tape, which paints the terminal around them.
mode=dark
# mode=light

# The command the session tapes start an agent with; projects.tape types it too.
claude_cmd="claude --settings '{\"theme\": \"$mode\"}'"

cfg() {
	local width=$1 keys="" a
	shift
	mkdir -p "$PANDO_CONFIG_DIR"
	{
		printf 'icons = "nerd"\npanel_borders = false\ncolor_theme = "vscode-%s"\nwidth = %s\n' "$mode" "$width"
		for a in "$@"; do
			case $a in
			vim) echo 'vim_mode = true' ;;
			claude) printf '[agents]\nclaude = ["claude", "--settings", "{\\"theme\\": \\"%s\\"}"]\n' "$mode" ;;
			claude-edits) printf '[agents]\nclaude = ["claude", "--permission-mode", "acceptEdits", "--allowedTools", "Bash", "--settings", "{\\"theme\\": \\"%s\\"}"]\n' "$mode" ;;
			shell) printf '[agents]\nshell = ["bash"]\n' ;;
			*=*) keys+=$(printf '"%s" = "%s"\n' "${a%%=*}" "${a#*=}")$'\n' ;;
			*) echo "cfg: unknown argument $a" >&2 ;;
			esac
		done
		[ -n "$keys" ] && printf '[keys]\n%s' "$keys"
	} > "$PANDO_CONFIG_DIR/config.toml"
}
