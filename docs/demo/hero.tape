# The README's still: Explorer on the left, Spaces on the right with two
# projects and their sessions, and between them two editor groups side by
# side, the file and the claude session that just changed it. Only hero.png
# is kept; the GIF vhs insists on writing, a few frames, is ignored by git.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, jq, a Chrome, gopls and claude)
Output docs/demo/hero.gif

Require pando
Require ttyd
Require jq
Require gopls
Require claude

Source docs/demo/settings.tape
# 200×48 cells: room for the file and the session side by side.
Set FontSize 16
Set Width 2080
Set Height 1144
Source docs/demo/setup.tape
# VHS cannot send a palette command: Move Editor into Left Group is ctrl+e here.
Type `cfg 27 hero claude-edits claude-ask ctrl+e=view.moveEditorIntoLeftGroup` Enter
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
# The session over the editor area once pando is up; the focus stays in the
# sidebar. Claude lays its text out once, so it gets its prompt only when it
# stands beside the file, as wide as it is shown.
Type `clear; (sleep 3; pando session switch $a >/dev/null; sleep 9; pando session send $a 'add a Count method to Store' --enter >/dev/null) & pando .` Enter
Sleep 5s

# store.go in front of it, then into a group of its own on the left: the file
# and the session that changed it side by side.
Ctrl+p
Sleep 300ms
Wait+Screen@5s /Go to File/
Type "store.go"
Enter
Sleep 1s
Ctrl+e
Sleep 1500ms
Wait+Screen@240s /\ds . done/
Sleep 3s
# A tape hidden to its end has no frames, and vhs saves no screenshot then.
Show
Sleep 100ms
Screenshot docs/demo/hero.png
Sleep 500ms
