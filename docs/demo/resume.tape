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
Type "pando project add . >/dev/null && pando session new --ws . --name barista --wait >/dev/null" Enter
Type `pando session send barista "$claude_cmd" --wait >/dev/null` Enter
Type `pando session send barista 'Remember the word cortado. Answer in one short line.' --wait >/dev/null` Enter
Type "clear; pando" Enter
Sleep 2s
Type "3"
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
# and claude resumed on the same conversation.
Type "pando" Enter
Wait+Screen@60s /cortado/
Type "3"
Sleep 2500ms

# Still the same conversation: ask it. (The wait for the answer is cut.)
Ctrl+]
Sleep 200ms
Ctrl+]
Sleep 300ms
Type "Which word?"
Enter
Hide
Wait+Screen@90s /Which word\?[\s\S]*●/
Sleep 1s
Show
Sleep 3s
