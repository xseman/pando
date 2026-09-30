# The CLI: an agent driven from a script, the way an orchestrator runs one
# (skills/pando/SKILL.md). A worktree, claude started in it, a prompt that
# blocks until the turn is over, and the diff it left.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq, a Chrome and claude)
Output docs/demo/cli.gif

Require pando
Require ttyd
Require jq
Require claude

Source docs/demo/settings.tape
Set Height 560
Set TypingSpeed 30ms
Source docs/demo/setup.tape
Type `cfg 34 claude-edits` Enter
Type "pando project add . >/dev/null; clear" Enter
Show

# A branch in a worktree of its own, named at random.
Type "wt=$(pando ws new | jq -r .path)" Enter
Sleep 1s

# claude in it, detached; --wait returns once it is ready for input.
Type "pando session new --agent claude --ws $wt --name counter --wait | jq -r .status" Enter
Wait+Screen@60s /idle/
Sleep 1s

# send --wait submits the prompt and blocks until the turn settles: no
# polling. (The turn is cut.)
Type `pando session send counter 'add a Count method in store.go' --wait | jq -c '{status, title}'` Enter
Sleep 2s
Hide
Wait+Screen@300s /"title"/
Show
Sleep 2s

# What it changed against main, committed or not, under the prompt that
# asked for it.
Type "git -C $wt diff -U0 main" Enter
Sleep 3500ms
