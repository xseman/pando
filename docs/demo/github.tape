# GitHub: the open pull requests with their checks, one opened as its
# rendered description, its changes as a diff, then checked out in a
# worktree of its own.
# The demo repository has no GitHub remote: a fake gh (gh/gh) answers from
# the canned files beside it, so nothing reaches the network.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/github.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 40` Enter
# The daemon keeps the PATH of the shell that starts it: the fake gh goes
# first on the PATH pando starts with.
Type "export PATH=$DEMO/gh:$PATH; clear" Enter
Type "pando" Enter
Sleep 1500ms
# Notifications folds to its header at the bottom (⏎ on it, 16th row once
# everything is listed), which leaves Pull Requests the height to list all
# three; 1 goes back to Explorer.
Type "6"
Wait+Screen@10s /review requested/
Type "g"
Down 15
Enter
Type "g"
Type "1"
Sleep 500ms
Show
Sleep 600ms

# 6 is the GitHub view: pull requests with their checks (passing, failing,
# still running) and issues.
Type "6"
Sleep 2000ms

# ⏎ on a pull request renders its description in the editor: who wants to
# merge what, the text, the checks and the comments.
Down@120ms 7
Sleep 500ms
Enter
Sleep 3500ms

# d opens its changes, gh pr diff.
Type "d"
Sleep 3s

# w checks it out in a worktree of its own and switches to it: ⑂ on its row,
# the branch in the status bar.
Type "w"
Sleep 3s

# Spaces (3): the worktree beside the checkout, ready for a session.
Type "3"
Sleep 2500ms
