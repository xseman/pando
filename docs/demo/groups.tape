# Editor groups, VS Code's: ctrl+\ splits the file off into a group on the
# right, the maximize button gives that group the whole area and back, and a
# session moves into a group under it, the file and the session's output
# side by side. VHS cannot send ctrl+\ or a palette command: the demo binds
# Split Editor Right to ctrl+e, Toggle Maximized Editor Group to ctrl+y and
# Move Editor into Group Below to ctrl+k.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg and a Chrome)
Output docs/demo/groups.gif

Require pando
Require ttyd

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 22 ctrl+e=view.splitEditorRight ctrl+y=view.toggleMaximizedEditorGroup ctrl+k=view.moveEditorIntoGroupBelow` Enter
# A shell session that ran the shop, to put beside its code.
Type "pando project add . >/dev/null && pando session new --ws . --name shop --wait -- bash --norc >/dev/null" Enter
Type "pando session send shop 'go vet ./... && go run .' --wait >/dev/null; clear" Enter
Type "pando" Enter
Wait+Screen@30s /\^.p/
Sleep 300ms
Ctrl+p
Wait+Screen@5s /Go to File/
Type "main.go"
Enter
Sleep 300ms
Ctrl+p
Wait+Screen@5s /Go to File/
Type "store.go"
Enter
Sleep 500ms
Show
Sleep 1200ms

# ctrl+\ (ctrl+e here): store.go into a group of its own on the right.
Ctrl+e
Sleep 1800ms

# Its maximize button (ctrl+y here) gives it the whole area, pressed again the
# groups come back.
Ctrl+y
Sleep 1500ms
Ctrl+y
Sleep 1500ms

# alt+t picks the session, which opens in front of store.go; ctrl+] takes the
# keyboard out of it, and Move Editor into Group Below (ctrl+k here) puts it
# under the file.
Alt+t
Wait+Screen@5s /shop/
Sleep 600ms
Type "shop"
Sleep 400ms
Enter
Sleep 1500ms
Ctrl+]
Sleep 300ms
Ctrl+k
Sleep 3s
