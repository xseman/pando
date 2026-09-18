# Panels: the terminal under the editor, then a claude session docked beside
# the editor on the right, and on the left next to Files.
# VHS cannot send ctrl+` or drag, so the demo binds the dock commands to keys
# in its config; in pando you drag a session by its title.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and claude)
Output docs/demo/panels.gif

Require pando
Require ttyd
Require claude

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 claude ctrl+k=view.dockSessionRight ctrl+e=view.dockSessionLeft` Enter
Type "clear" Enter
Type "pando" Enter
Sleep 1500ms
Ctrl+p
Sleep 300ms
Type "store.go"
Sleep 300ms
Enter
Sleep 800ms
Show
Sleep 1s

# ctrl+` (5 in a sidebar) opens a terminal under the editor, VS Code's spot.
Type "5"
Sleep 800ms
Type "go run ."
Enter
Sleep 1500ms

# A claude session (New Agent Session in Spaces) takes the editor area.
Ctrl+]
Sleep 300ms
Type "3"
Sleep 400ms
Type "m"
Sleep 400ms
Down Down
Enter
Sleep 400ms
Type "claude"
Enter
Wait+Screen@30s /Try "/
Sleep 1s

# Docked right, the file comes back beside it.
Ctrl+]
Sleep 300ms
Ctrl+k
Sleep 2500ms

# Docked left, it is a column next to the sidebar.
Ctrl+]
Sleep 300ms
Ctrl+e
Sleep 3s
