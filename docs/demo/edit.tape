# Editing a file in place: find, go to line, typing, save through gofmt,
# selecting lines and copying them, and a new file typed before it is named.
# VHS cannot send ctrl+s, shift+down or alt+shift+down: the demo binds the
# commands to ctrl letters in its config and types shift+down as its escape
# sequence.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/edit.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 ctrl+k=editor.saveFile ctrl+e=editor.copyLineDown` Enter
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

# Find in the file: the widget floats at the top right, n of m, every match
# tinted, the current one stronger; esc closes it.
Ctrl+f
Sleep 400ms
Type "items"
Sleep 1200ms
Enter
Sleep 800ms
Enter
Sleep 800ms
Escape
Sleep 400ms

# Go to line: the line lights up while the picker is open.
Ctrl+g
Sleep 300ms
Type "24:2"
Sleep 900ms
Enter
Sleep 500ms

# Letters type: an editor, not a viewer. Nothing lights up while a name is
# being typed — the occurrence highlight waits for the cursor to move onto a
# name the file already uses, as VS Code's does.
Enter
Enter
Type "// Count is how many items are in the basket."
Enter
Type "func (s *Store) Count() int {"
Enter
Type "return len(s.items)"
Enter
Type "}"
Sleep 1200ms

# ctrl+s (ctrl+k here) saves through gofmt: the body gets its indent and the
# dot on the tab goes away.
Ctrl+k
Sleep 2500ms

# shift+down selects the Count method line by line; alt+shift+down (ctrl+e
# here) copies the selected lines below them, and ctrl+z takes it back.
Ctrl+g
Sleep 300ms
Type "26:1"
Enter
Sleep 500ms
Type@1ms `[1;2B`
Sleep 250ms
Type@1ms `[1;2B`
Sleep 250ms
Type@1ms `[1;2B`
Sleep 250ms
Type@1ms `[1;2B`
Sleep 250ms
Sleep 700ms
Ctrl+e
Sleep 1500ms
Ctrl+z
Sleep 2s

# Move the cursor onto a name the file does use and every occurrence of it
# lights up at once — the type, its constructor and its methods.
Ctrl+g
Sleep 300ms
Type "4:8"
Enter
Sleep 2500ms

# ⌃n opens an untitled buffer — type first, name it at save time, VS Code's
# New Untitled File. A double click on an empty editor area does the same.
Ctrl+n
Sleep 1s
Type "package store"
Enter
Enter
Type "// Cents turns a price in whole euros into cents."
Enter
Type "func Cents(euros int) int  { return euros*100 }"
Sleep 1500ms

# ⌃s (ctrl+k here) asks where it goes, completing the path as fish does. The
# name decides the formatter, so gofmt still runs: the tab follows the file,
# the spacing is fixed and the dot on the tab goes.
Ctrl+k
Sleep 1s
Type "cents.go"
Sleep 1200ms
Enter
Sleep 3s
