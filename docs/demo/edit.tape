# A new file typed before it is named: ctrl+n opens an untitled buffer,
# ctrl+s asks where it goes with fish's path completion, and gofmt tidies it
# on the way to disk while the tab takes the name and the tree shows it.
# VHS cannot send ctrl+s: the demo binds saving to ctrl+k in its config.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/edit.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 ctrl+k=editor.saveFile` Enter
Type "clear" Enter
Type "pando" Enter
Sleep 1500ms
Show
Sleep 1200ms

# The empty main area offers ctrl+n: Untitled-1, VS Code's New Untitled File.
# Typed sloppily on purpose, no indent and no spaces, for gofmt to fix.
Ctrl+n
Sleep 600ms
Type "package store"
Enter
Enter
Type "// Cents turns whole euros into cents."
Enter
Type "func Cents(euros int) int{"
Enter
Type "return euros*100"
Enter
Type "}"
Sleep 1200ms

# ctrl+s (ctrl+k here) asks where it goes; tab completes the path as fish does.
Ctrl+k
Sleep 700ms
Type "i"
Sleep 300ms
Tab
Sleep 400ms
Type "s"
Sleep 300ms
Tab
Sleep 400ms
Type "cents.go"
Sleep 900ms

# The name picks the formatter: gofmt indents the body and spaces the brace
# and the product, the dot on the tab goes, and the tree reveals cents.go.
Enter
Sleep 3s
