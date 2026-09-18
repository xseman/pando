# Source Control: a working tree diff inline and side by side, reverting one
# line of the change, staging the rest, the commit, then a merge that
# conflicts: Merge Changes, the conflict resolved in the editor, Continue.
# VHS cannot send ctrl+s: the demo binds save and Accept Incoming Change to
# ctrl letters in its config.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/diff.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type "bash $DEMO/merge.sh" Enter
Type `cfg 30 ctrl+k=editor.saveFile ctrl+e=editor.acceptIncomingChange` Enter
Type "clear" Enter
Type "pando" Enter
Sleep 1500ms
Type "2"
Sleep 800ms
Down Down Down
Sleep 300ms
Show
Sleep 800ms

# The diff of an uncommitted change.
Type "o"
Sleep 1500ms

# Fold the sidebar to a rail, then the same diff side by side.
Type "b"
Sleep 400ms
Type "s"
Sleep 1800ms

# The cursor is a row of the diff: m on one line reverts just that line, after
# asking.
Down 4
Sleep 700ms
Type "m"
Sleep 800ms
Down
Sleep 400ms
Enter
Sleep 900ms
Enter
Sleep 1800ms

# ctrl+a selects what is left, m stages exactly what is selected.
Ctrl+a
Sleep 900ms
Type "m"
Sleep 1s
Enter
Sleep 1500ms

# And commit what is staged: ctrl+] brings the sidebar back, c focuses the
# message, enter commits.
Ctrl+]
Sleep 600ms
Type "c"
Sleep 400ms
Type "Add Discount"
Sleep 500ms
Enter
Sleep 2500ms

# A merge that conflicts, run in the terminal panel (5). Merge Changes shows
# up above the sections with ! after the branch, and Commit waits as Continue.
Type "5"
Sleep 800ms
Type "git merge feat"
Enter
Sleep 2000ms
Ctrl+]
Sleep 400ms
Type "5"
Sleep 1200ms

# ⏎ on a file that still holds conflict markers asks first.
Type "g"
Down
Sleep 600ms
Enter
Sleep 1500ms
Escape
Sleep 600ms

# O opens the file itself: the sides are tinted and the <<<<<<< line carries
# Accept Current | Incoming | Both. Accept Incoming Change (ctrl+e here) on the
# block, then save (ctrl+k).
Type "O"
Sleep 600ms
Ctrl+]
Sleep 1200ms
Down 11
Sleep 1200ms
Ctrl+e
Sleep 1500ms
Ctrl+k
Sleep 1200ms

# Back in Source Control ⏎ stages the resolved file, and Continue finishes
# the merge.
Ctrl+]
Sleep 600ms
Enter
Sleep 1500ms
Type "c"
Sleep 400ms
Type "Merge feat"
Sleep 500ms
Enter
Sleep 3s
