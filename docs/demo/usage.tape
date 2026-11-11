# Usage: the plan limits of every sign-in, claude's default and a work
# profile and codex's, each window with how much is used and when it resets.
# The default is near its weekly limit, so the status bar offers work; ⏎ on
# work makes it the profile new sessions start with, and the profile picker
# says how much room each one has left.
# The limits are made up (usage/usage.sh): no account of the recording
# machine's is asked, and nothing reaches the network.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome, python3 and openssl)
Output docs/demo/usage.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type "source $DEMO/usage/usage.sh" Enter
Type `cfg 44 usage` Enter
# A shell session, so Sessions holds one.
Type "pando project add . >/dev/null && pando session new --ws . --name shop --wait -- bash --norc >/dev/null" Enter
Type "pando session send shop 'go run .' --wait >/dev/null; clear" Enter
Type "pando" Enter
Wait+Screen@30s /\^.p/
Sleep 300ms
# The Usage tab asks the daemon for the limits it measured; back on Sessions
# once the status bar offers work.
Type "9"
Sleep 500ms
Type "r"
Wait+Screen@10s /use work/
Type "3"
Sleep 500ms
Show
Sleep 1800ms

# 9: the Usage tab, after Sessions. A meter per window, ┃ on it a model's
# own allowance; the default has used 96% of its week.
Type "9"
Sleep 3s

# ⏎ on work: new claude sessions start with it, and the status bar says so.
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 800ms
Enter
Sleep 2500ms

# A new claude session asks which profile, each with what it has left.
Type "3"
Sleep 600ms
Type "n"
Sleep 800ms
Type "cl"
Sleep 400ms
Enter
Sleep 3s
Escape
Sleep 1s
