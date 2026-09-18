# Projects: pando started with no directory reopens the last workspace, the
# project switcher lists every project with its sessions, Add project
# completes a path, and Spaces switches straight to another project's session.
# VHS cannot click, so the status bar's project switcher is bound to a key
# here; in pando you click the project name next to the branch.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq, a Chrome and claude)
Output docs/demo/projects.gif

Require pando
Require ttyd
Require jq
Require claude

Source docs/demo/settings.tape
Set TypingSpeed 50ms
Source docs/demo/setup.tape
Type "bash $DEMO/setup.sh /tmp/pando-demo-api >/dev/null && bash $DEMO/setup.sh /tmp/pando-web >/dev/null" Enter
# The api project gets a face of its own: another file, another change.
Type `cd /tmp/pando-demo-api && git mv main.go server.go && printf 'package main\n\n// Version is what the API answers on /version.\nconst Version = 12\n' > version.go && git add -A && git commit -qm 'the api' && printf '\nfunc healthy() bool { return true }\n' >> server.go && cd /tmp/pando-demo` Enter
Type `cfg 30 shell ctrl+t=agents.openProject` Enter
# Two projects, each with claude started in a shell session and left waiting.
Type "pando project add /tmp/pando-demo >/dev/null && pando project add /tmp/pando-demo-api >/dev/null" Enter
Type `for ws in /tmp/pando-demo /tmp/pando-demo-api; do id=$(pando session new --ws $ws | jq -r .id); pando session send $id "$claude_cmd" --enter; done` Enter
Type `pando call state.set '{"last_workspace": "/tmp/pando-demo-api"}' >/dev/null && cd /tmp && clear` Enter
Sleep 6s
Show

# pando with no directory opens the workspace it showed last, session and all.
Type "pwd" Enter
Sleep 1200ms
Type "pando" Enter
Sleep 2500ms

# The project switcher: every project, its sessions, the open one marked.
Ctrl+t
Sleep 1500ms
Type "pando-demo"
Sleep 600ms
Enter
Sleep 2500ms

# Add Project in the same picker completes the path: the rest shows dimmed,
# tab takes it.
Ctrl+]
Sleep 300ms
Ctrl+t
Sleep 700ms
Type "add"
Sleep 500ms
Enter
Sleep 500ms
Ctrl+u
Type "/tmp/pando-w"
Sleep 1200ms
Tab
Sleep 1s
Enter
Sleep 1500ms

# Spaces: projects, worktrees, sessions; enter on a session switches to it.
Type "3"
Sleep 1500ms
Type "G"
Up Up
Sleep 800ms
Enter
Sleep 3s
