# The TUI: three claude agents, each in a worktree of its own, come to life
# as their prompts arrive, and the tree shows each one's state as it changes:
# one finishes (✓), one waits for a yes (×, pulsing), one keeps working (◐).
# The finished one's change is reviewed and committed without leaving pando.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq, a Chrome and claude)
Output docs/demo/tui.gif

Require pando
Require ttyd
Require jq
Require claude

Source docs/demo/settings.tape
Set TypingSpeed 50ms
Source docs/demo/setup.tape
# claude-edits for two of them, agent ask (claude-ask) for the one that has to
# wait for a yes. VHS cannot send ctrl+0: Focus Sidebar is ctrl+y here.
Type `cfg 34 claude-edits claude-ask ctrl+y=view.focusSidebar` Enter
Type "pando project add . >/dev/null; for b in count discount readme; do pando ws new $b >/dev/null; done" Enter
Type "wt=/tmp/pando-state/data/worktrees/pando-demo" Enter
Type "a=$(pando session new --agent claude --ws $wt/count --wait | jq -r .id)" Enter
Type "b=$(pando session new --agent ask --ws $wt/discount --wait | jq -r .id)" Enter
Type "c=$(pando session new --agent claude --ws $wt/readme --wait | jq -r .id)" Enter
# The prompts go in from a background job once pando is up, one by one, so
# the tree comes alive on screen.
Type `(sleep 3; pando session send $a 'add a Count method to Store' --enter; sleep 1; pando session send $b 'run go vet ./... and go run .' --enter; sleep 1; pando session send $c 'document the store package in README.md, with an example' --enter) >/dev/null 2>&1 &` Enter
Type "clear; pando" Enter
Sleep 1500ms
Type "3"
Sleep 500ms
Show

# Three worktrees, a claude in each, idle (○) until its prompt arrives: then
# ◐, and each renames itself after its work.
Wait+Screen@30s /running[\s\S]*running[\s\S]*running/
Sleep 1500ms

# One waits for a yes (× pulses), then one finishes (✓) while the third
# works on. (The wait for it is cut.)
Wait+Screen@90s /blocked/
Sleep 1500ms
Hide
Wait+Screen@90s /✓/
Show
Sleep 2s

# ⏎ on the finished one opens it in the editor area: what it did.
Type "g"
Down 3
Sleep 400ms
Enter
Sleep 2500ms

# ctrl+] goes back to the tree; Source Control (2) follows the session to its
# worktree, and o opens the change it made in front of the session.
Ctrl+]
Sleep 300ms
Type "2"
Sleep 1000ms
Down 3
Sleep 400ms
Type "o"
Sleep 800ms

# b hides the sidebar it was opened from, so the diff gets the room to be read.
Type "b"
Sleep 3s

# ctrl+b brings the sidebar back and ctrl+0 (ctrl+y here) focuses it; a stages
# it all, c writes the message, ⏎ commits on the worktree's branch.
Ctrl+b
Sleep 300ms
Ctrl+y
Sleep 300ms
Type "a"
Sleep 600ms
Type "c"
Sleep 300ms
Type "Add Store.Count"
Sleep 400ms
Enter
Sleep 3s
