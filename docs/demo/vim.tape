# Vim mode: the editor opens in normal mode, letters are commands again, and
# i / a / o put the text back under the keyboard. The mode sits in the header
# next to Ln, Col.
# VHS cannot send ctrl+s: the demo binds Save File to ctrl+k in its config.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/vim.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 vim ctrl+k=editor.saveFile` Enter
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
Sleep 1s

# NORMAL in the header: hjkl move, w and b walk words, gg and G the file.
Type@150ms "jjjj"
Sleep 600ms
Type@200ms "ww"
Sleep 600ms
Type "G"
Sleep 800ms
Type@200ms "gg"
Sleep 1s

# dd deletes a line and u puts it back; yy and p copy one.
Type@150ms "jjjj"
Sleep 400ms
Type@200ms "dd"
Sleep 1200ms
Type "u"
Sleep 1200ms
Type@200ms "yyp"
Sleep 1500ms
Type@200ms "dd"
Sleep 1s

# v selects by hand, V by the line; d, y and c run over the selection.
Type "v"
Sleep 300ms
Type@150ms "lllll"
Sleep 800ms
Type "d"
Sleep 1200ms
Type "u"
Sleep 1s

# o opens a line below and hands the keyboard back: INSERT, where the editor
# is the editor again (edit.gif has the rest of it).
Type@200ms "Go"
Sleep 600ms
Type "func (s *Store) Count() int  { return len(s.items) }"
Sleep 1s
Escape
Sleep 1s

# ctrl+s (ctrl+k here) saves through gofmt; the dot on the tab goes.
Ctrl+k
Sleep 2500ms

# cw changes a word. The suggest list opens while inserting, as it always
# does, so the first esc closes it and the second goes back to NORMAL.
Type@200ms "gg"
Sleep 300ms
Type@150ms "jjww"
Sleep 600ms
Type@200ms "cw"
Sleep 400ms
Type "basket"
Sleep 1s
Escape
Sleep 700ms
Escape
Sleep 1s

# / is the find widget and : the go-to-line picker, the editor's own, reached
# from normal mode.
Type "/"
Sleep 400ms
Type "items"
Sleep 1200ms
Escape
Sleep 600ms
Type ":"
Sleep 300ms
Type "20"
Sleep 900ms
Enter
Sleep 2s
