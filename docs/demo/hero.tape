# The README's still: Explorer on the left, Spaces on the right with two
# projects and their sessions, and between them a file with gopls's references
# in a peek beside the claude session that just changed it. Only hero.png is
# kept; the GIF vhs insists on writing, a few frames, is ignored by git.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq, a Chrome, gopls and claude)
Output docs/demo/hero.gif

Require pando
Require ttyd
Require jq
Require gopls
Require claude

Source docs/demo/settings.tape
# 200×48 cells: room for the peek's code beside the session column.
Set FontSize 16
Set Width 2080
Set Height 1144
Source docs/demo/setup.tape
# VHS cannot send shift+F12: Find All References is ctrl+e here.
Type `cfg 22 hero claude-edits claude-ask ctrl+e=editor.findReferences` Enter
Type "go build ./... >/dev/null 2>&1; pando project add . >/dev/null; for b in discount readme; do pando ws new $b >/dev/null; done" Enter
# A second project, so Spaces holds two.
Type "web=$STATE/web; git init -q -b main $web && echo '<h1>shop</h1>' > $web/index.html && git -C $web add -A && git -C $web -c user.name=Demo -c user.email=demo@example.com commit -qm 'a page'" Enter
Type "pando project add $web >/dev/null; pando ws new landing --project $web >/dev/null; wt=$PANDO_DATA_DIR/worktrees" Enter
# claude trusts $REPO and $STATE but asks about a worktree of another repository.
Type `j=$CLAUDE_CONFIG_DIR/.claude.json; jq --arg p $wt/web/landing '.projects[$p].hasTrustDialogAccepted = true' $j > $j.new && mv $j.new $j` Enter
Type "a=$(pando session new --agent claude --ws $REPO --wait | jq -r .id)" Enter
Type "b=$(pando session new --agent ask --ws $wt/pando-demo/discount --wait | jq -r .id)" Enter
Type "c=$(pando session new --agent claude --ws $wt/pando-demo/readme --wait | jq -r .id)" Enter
Type "d=$(pando session new --agent claude --ws $wt/web/landing --wait | jq -r .id)" Enter
Type "pando session new --ws $web --name 'dev server' -- python3 -m http.server 8731 >/dev/null" Enter
# Two keep working, one waits for a yes, and the one shown beside the file is done.
Type "pando session send $c 'document the store package in README.md, with an example' --enter; pando session send $d 'turn index.html into a landing page for the coffee shop' --enter" Enter
Type "pando session send $b 'run go vet ./... and go run .' --wait --timeout 120000 >/dev/null" Enter
Type "pando session send $a 'add a Count method to Store' --wait --timeout 180000 >/dev/null; echo RE''ADY" Enter
Wait+Screen@240s /READY\n/
# The session column next to the file, 44 cells wide, and the finished session
# docked in it once pando is up; the focus stays in the sidebar.
Type `pando call state.set "{\"session_views\": {\"$REPO\": {\"position\": \"right\", \"width\": 44}}}" >/dev/null; clear; (sleep 3; pando session switch $a >/dev/null) & pando .` Enter
Sleep 5s

# store.go beside it, and the references to Total in a peek, main.go's picked.
Ctrl+p
Sleep 300ms
Wait+Screen@5s /Go to File/
Type "store.go"
Enter
Sleep 1s
Ctrl+g
Type "18:17"
Enter
Sleep 300ms
Ctrl+e
Wait+Screen@60s /References/
Down 3
Sleep 1s
# A tape hidden to its end has no frames, and vhs saves no screenshot then.
Show
Sleep 100ms
Screenshot docs/demo/hero.png
Sleep 500ms
