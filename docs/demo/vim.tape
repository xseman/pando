# Vim mode: one small edit made the vim way. The editor opens in NORMAL,
# V-LINE yanks a method, P puts a copy above it, and C / cc turn the copy
# into another method in INSERT. The mode sits in the editor's header, Ln and
# Col in the status bar.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/vim.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 vim` Enter
Type "clear" Enter
Type "pando" Enter
Sleep 1500ms
Ctrl+p
Sleep 300ms
Type "store.go"
Sleep 300ms
Enter
Sleep 800ms
Ctrl+]
Sleep 300ms
Show
Sleep 800ms

# 12G jumps to Add's comment; V and 4j take it, the method and the blank
# line under it: V-LINE.
Type@200ms "12G"
Sleep 500ms
Type "V"
Sleep 300ms
Type@150ms "4j"
Sleep 900ms

# y yanks the lines and P puts the copy above them, the cursor on its top.
Type "y"
Sleep 400ms
Type "P"
Sleep 900ms

# j6w goes down a line and walks the words to Add (the count shows beside NORMAL) and C changes the
# rest of the line: INSERT until esc.
Type@200ms "j6w"
Sleep 400ms
Type "C"
Sleep 300ms
Type "Remove(name string) {"
Sleep 400ms
Escape
Sleep 400ms

# cc retypes the body line, its indent kept.
Type@150ms "jcc"
Sleep 300ms
Type "delete(s.items, name)"
Sleep 400ms
Escape
Sleep 400ms

# The comment last: 2k0w puts the cursor on its Add. Its "." opens the
# suggest list, empty in a comment, so the first esc closes that and the
# second goes back to NORMAL.
Type@150ms "2k0wC"
Sleep 300ms
Type "Remove drops an item."
Sleep 400ms
Escape
Sleep 150ms
Escape
Sleep 2s
