# Language server: references, a code action, a rename and completion, answered
# by gopls. VHS cannot send F12, shift+F12, ctrl+. or F2, so the demo binds
# those commands to ctrl letters in its config (the browser VHS records in eats
# ctrl+r, so rename is ctrl+y).
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and gopls)
Output docs/demo/lsp.gif

Require pando
Require ttyd
Require gopls

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 ctrl+e=editor.findReferences ctrl+h=view.goBack ctrl+y=editor.renameSymbol ctrl+n=editor.quickFix` Enter
Type "go build ./... >/dev/null 2>&1; clear" Enter
# gopls indexes on its first request; ask once here so the demo never waits for it.
Type "pando" Enter
Sleep 2s
Ctrl+p
Sleep 300ms
Type "store.go"
Enter
Sleep 1s
Ctrl+]
Ctrl+g
Type "18:17"
Enter
Sleep 300ms
Ctrl+e
Wait+Screen@60s /References/
Escape
Sleep 500ms
Show
Sleep 800ms

# Where is Total used? gopls answers into the peek under the editor.
Ctrl+g
Sleep 300ms
Type "18:17"
Sleep 600ms
Enter
Sleep 600ms
Ctrl+e
Sleep 1200ms
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 700ms

# enter takes you to the reference, in main.go; ctrl+<- steps back (ctrl+h here).
Enter
Sleep 1200ms
Ctrl+h
Sleep 1s

# The code actions for a line, VS Code's ctrl+. menu (ctrl+n here).
Ctrl+g
Sleep 300ms
Type "28:14"
Enter
Sleep 600ms
Ctrl+n
Sleep 2s
Enter
Sleep 2s

# F2 (ctrl+y here) renames the symbol everywhere the server knows it, in a box
# over the symbol: main.go changes too.
Ctrl+g
Sleep 300ms
Type "18:17"
Enter
Sleep 600ms
Ctrl+y
Sleep 900ms
Ctrl+u
Type "Sum"
Sleep 500ms
Enter
Sleep 2s

# Typing asks gopls: after "s." the list holds the store's methods, S picks Sum.
Ctrl+g
Sleep 300ms
Type "27:44"
Enter
Sleep 500ms
Enter
Tab
Type "total := s."
Sleep 2s
Type "S"
Sleep 1200ms
Enter
Sleep 1500ms

# The rename wrote two files, the code action a new test file.
Ctrl+]
Sleep 400ms
Type "2"
Sleep 3s
