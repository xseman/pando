# Quick open: ctrl+p over everything. Empty it lists the changed files and
# the rest; a word finds files and the lines that hold it, Text the same
# lines under their files; Sessions is the Sessions tree, and > the commands.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/quickopen.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26` Enter
# Two shell sessions, for the Sessions tab to list.
Type "pando project add . >/dev/null && pando session new --ws . --name shop --wait -- bash --norc >/dev/null" Enter
Type "pando session new --ws . --name api --wait -- bash --norc >/dev/null; clear" Enter
Type "pando" Enter
Wait+Screen@30s /\^.p/
Sleep 300ms
Show
Sleep 800ms

# ctrl+p: empty, All lists what git status changed, then the files.
Ctrl+p
Wait+Screen@5s /Files  Sessions  Text/
Sleep 1800ms

# A word: the files it names, then the lines holding it.
Type "store"
Sleep 2500ms

# Text (tab, tab, tab): every line under its file, numbered; ⏎ opens one there.
Ctrl+u
Type "total"
Sleep 1200ms
Tab
Sleep 500ms
Tab
Sleep 500ms
Tab
Sleep 2s
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 600ms
Enter
Sleep 2s

# Sessions: the tree, every project open; a word narrows it, ⏎ goes there.
Ctrl+p
Wait+Screen@5s /Files  Sessions  Text/
Tab
Sleep 400ms
Tab
Sleep 1500ms
Type "api"
Sleep 1s
Enter
Sleep 2s

# > from All: the commands, a word narrowing them.
Ctrl+]
Sleep 300ms
Ctrl+p
Wait+Screen@5s /Files  Sessions  Text/
Type ">wrap"
Sleep 2s
Escape
Sleep 1s
