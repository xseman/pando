# Resume: the daemon stops, and the claude conversation comes back with it.
# pando respawns the session's shell and types the command that continues the
# agent it held, by conversation id.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and claude)
Output docs/demo/resume.gif

Require pando
Require ttyd
Require claude

Source docs/demo/settings.tape
Source docs/demo/setup.tape
# resume: claude comes back in the terminal, as it ran, rather than as a
# Claude Code background job that would outlive the recording.
Type `cfg 30 resume` Enter
# claude typed into a shell session, as `n` then `claude` would: the shell
# is what the daemon respawns, and what it types the resume command into.
# The shell is started with claude's isolated config directory in its
# environment (see demo_claude in cfg.sh), so what it runs keeps its
# conversation there.
Type "pando project add . >/dev/null && pando session new --ws . --name barista --wait -- env CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR bash >/dev/null" Enter
Type `pando session send barista "$claude_cmd" --wait >/dev/null` Enter
Type `pando session send barista 'The secret word is cortado. Answer in one short line.' --wait >/dev/null` Enter
# The sends above return only once claude has answered, so the shell is busy
# for a while: wait for pando and the conversation in it, or the GIF would
# open on the hidden setup.
Type "clear; pando" Enter
Wait+Screen@120s /PANDO-DEMO/
Type "3"
Wait+Screen@30s /cortado/
Sleep 500ms
Show

# A conversation in progress.
Sleep 2500ms

# Close pando (q asks first), then stop the daemon: every session's program
# goes with it.
Type "q"
Sleep 800ms
Enter
Sleep 600ms
Type "pando stop" Enter
Sleep 1200ms

# The next pando starts a daemon, which brings the session back: its shell,
# and claude resumed on the same conversation. 3 shows Spaces at once, beside
# the shell typing the resume command.
Type "pando" Enter
Wait+Screen@30s /PANDO-DEMO/
Type "3"
Wait+Screen@60s /cortado/
Sleep 2500ms

# Still the same conversation: ask it, ctrl+] focusing it from Spaces. (The
# wait for the answer is cut.)
Ctrl+]
Sleep 300ms
Type "Which word?"
Enter
Hide
Wait+Screen@90s /Which word\?[\s\S]*●/
Sleep 1s
Show
Sleep 3s
