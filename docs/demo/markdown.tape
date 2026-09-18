# Markdown: source, rendered in place, and both side by side.
# VHS cannot send ctrl+shift+v, so the demo binds the Markdown commands to ctrl
# letters in its config.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/markdown.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 30 ctrl+n=editor.toggleRenderedMarkdown ctrl+o=editor.markdownSideBySide` Enter
Type "cp $DEMO/README.demo.md README.md && clear" Enter
Type "pando" Enter
Sleep 1500ms
Ctrl+p
Sleep 300ms
Type "README.md"
Sleep 300ms
Enter
Sleep 800ms
Ctrl+]
Sleep 300ms
Show

# A README opens as source.
Sleep 1800ms

# ctrl+shift+v (ctrl+n here) renders it in place: code through the highlighter,
# tables, task lists.
Ctrl+n
Sleep 2500ms

# Side by side wants a wide main area: fold the sidebar to a rail, then both
# halves, scrolling together.
Ctrl+]
Sleep 300ms
Type "b"
Sleep 500ms
Ctrl+o
Sleep 2s
Down 14
Sleep 2s
