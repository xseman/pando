# Agent sessions: two claude sessions at once. One waits 5 seconds and then
# edits a file, the other only says hi. The tree marks each one working, the
# one that finishes out of view done and pulsing until it is clicked, and
# switching to it shows what it did; then a shell tab of its own beside it.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and claude)
Output docs/demo/sessions.gif

Require pando
Require ttyd
Require claude

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 30 claude-edits ctrl+o=view.newTabInSession` Enter
Type "clear" Enter
Type "pando" Enter
Sleep 1500ms
Type "3"
Sleep 500ms
Show
Sleep 600ms

# New Agent Session starts claude, with something slow to do. (Claude asks
# once whether it trusts a new folder; answer that by hand before recording,
# it is remembered per path.)
Type "m"
Sleep 400ms
Down Down
Enter
Sleep 400ms
Type "claude"
Enter
Wait+Screen@30s /Try "/
Sleep 1s
Type@20ms "run sleep 5, then add a Count method to Store returning len(s.items)"
Enter
Wait+Screen@15s /running/
Sleep 1500ms

# A second session next to it, for something quick.
Ctrl+]
Sleep 300ms
Type "m"
Sleep 400ms
Down Down
Enter
Sleep 400ms
Type "claude"
Enter
Wait+Screen@30s /Try "/
Sleep 1s
Type "hi"
Enter

# The first one finishes while nobody looks at it: done, a sound, and its
# row pulses in the attention color until it is clicked.
Wait+Screen@120s /✓/
Sleep 3s

# Switching to it shows what it did, and stops the pulse.
Ctrl+]
Sleep 300ms
Type "g"
Down Down
Sleep 600ms
Enter
Sleep 3500ms

# A session has tabs of its own, as a herdr workspace does: a shell beside
# claude (the + on its strip; New Tab in Session, ctrl+o in this demo, since
# VHS cannot click). The Spaces tree still lists one session.
Ctrl+]
Sleep 300ms
Ctrl+o
Sleep 1500ms
Type "go vet ./... && go run ."
Enter
Sleep 2500ms

# [ goes back to claude's tab.
Ctrl+]
Sleep 300ms
Type "["
Sleep 3s
