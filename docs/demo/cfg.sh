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
mode=dark
# mode=light

# The command resume.tape types into its shell session, after cfg has written
# claude.json. Its "tui": "default" keeps claude in its classic renderer, the
# one the GIFs show, whatever the recording machine's settings.json picked (a
# fullscreen claude draws on the alternate screen and looks nothing like them).
claude_cmd="claude --settings $PANDO_CONFIG_DIR/claude.json"

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
			rid+="claude = [\"claude\", $s, \"--resume\", \"{id}\"]"$'\n' ;;
		claude-ask) agents+="ask = [\"claude\", \"--permission-mode\", \"default\", $s]"$'\n' ;;
		claude-edits) agents+="claude = [\"claude\", \"--permission-mode\", \"acceptEdits\", \"--allowedTools\", \"Bash\", $s]"$'\n' ;;
		*=*) keys+="\"${a%%=*}\" = \"${a#*=}\""$'\n' ;;
		*) echo "cfg: unknown argument $a" >&2 ;;
		esac
	done
	mkdir -p "$PANDO_CONFIG_DIR"
	printf '{"theme": "%s", "tui": "default"}\n' "$mode" > "$PANDO_CONFIG_DIR/claude.json"
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
