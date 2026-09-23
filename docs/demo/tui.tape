# The TUI: files, a diff, search, the terminal and an agent session.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and claude)
Output docs/demo/tui.gif

Require pando
Require ttyd
Require claude

Source docs/demo/settings.tape
Set TypingSpeed 50ms
Source docs/demo/setup.tape
Type `cfg 34 claude` Enter
# A worktree beside the checkout, named at random: Spaces marks the checkout
# ⌂ and the worktree ⑂.
Type "pando project add . >/dev/null && pando ws new >/dev/null" Enter
Type "clear" Enter
Show

Type "pando" Enter
Sleep 1500ms

# Explorer, then quick open by name.
Down 2
Sleep 600ms
Ctrl+p
Sleep 600ms
Type "store.go"
Sleep 600ms
Enter
Sleep 1500ms

# Source Control: the sections and one uncommitted change (diff.gif has the rest).
Type "2"
Sleep 1200ms
Down 3
Sleep 600ms
Type "o"
Sleep 2s

# Search across the workspace.
Type "4"
Sleep 600ms
Type "Total"
Sleep 1500ms
Enter
Sleep 400ms
Down
Enter
Sleep 1800ms

# The terminal panel under the editor (ctrl+` or 5), a plain shell.
Type "5"
Sleep 900ms
Type "go run ."
Enter
Sleep 1800ms
Ctrl+]
Sleep 300ms
Type "5"
Sleep 600ms

# Spaces: the project's checkout (⌂) and its worktree (⑂), and a claude
# session hanging off its branch; its symbol follows it - working while it
# answers, idle after. (Claude asks its onboarding questions -
# whether it trusts the folder, and the default permission mode - once; answer
# them by hand before recording, they are remembered.)
Type "3"
Sleep 600ms
Type "m"
Sleep 600ms
Down Down
Sleep 400ms
Enter
Sleep 500ms
Type "claude"
Sleep 300ms
Enter
Wait+Screen@30s /Try "/
Sleep 1500ms
Type "hi"
Enter
Wait+Screen@15s /running/
# The tree flickers back to idle between claude's own steps, so one idle ends
# the recording on a spinner. Wait for it twice with a pause between: the
# answer's own wording wraps in a 25-column panel and cannot be matched.
Wait+Screen@60s /idle/
Sleep 3s
Wait+Screen@60s /idle/
Sleep 2s
