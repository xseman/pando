# Projects: pando started with no directory reopens the last workspace, the
# project switcher lists every project with its sessions, Add project
# completes a path, and Spaces switches straight to another project's session.
# VHS cannot click, so the status bar's project switcher is bound to a key
# here; in pando you click the project name next to the branch.
#
# This tape types a path into the Add Project picker, which no variable can
# reach, so it pins the default paths and records on its own. claude runs in
# the main repository only: a second one would be a second folder for you to
# trust by hand before `make demo` works at all, and the switcher shows a
# session count either way.
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
Type `cfg 30 shell ctrl+t=agents.openProject ctrl+y=view.focusSidebar` Enter
# Two projects with a shell session each; claude is typed into the first one,
# so only its repository has to be trusted. The workspace shown last is that
# one, and the switcher counts a session either way.
Type "pando project add /tmp/pando-demo >/dev/null && pando project add /tmp/pando-demo-api >/dev/null" Enter
Type `id=$(pando session new --ws /tmp/pando-demo | jq -r .id); pando session send $id "$claude_cmd" --enter` Enter
Type "pando session new --ws /tmp/pando-demo-api >/dev/null" Enter
Type `pando call state.set '{"last_workspace": "/tmp/pando-demo"}' >/dev/null && cd /tmp && clear` Enter
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
Type "api"
Sleep 600ms
Enter
Sleep 2500ms

# Add Project in the same picker completes the path: the rest shows dimmed,
# tab takes it. No focus change first: a [keys] binding never fires inside a
# session, so one ctrl+] would hand ctrl+t and everything after it to the agent.
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
# Focus the sidebar first (ctrl+0, bound to ctrl+y here because VHS cannot
# send it): the prompt left the focus on main, where 1-4 belong to the editor
# and the empty one swallows them.
Ctrl+y
Sleep 300ms
Type "3"
Sleep 1500ms
# Down to the claude session of the *other* project. Switching a project
# already puts its own session on screen, so ⏎ on that one would only take it
# off again; this is the cross-project jump the tape is about.
Type "g"
Down Down
Sleep 800ms
Enter
Sleep 3s
