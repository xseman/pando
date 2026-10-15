# Databases: a SQLite file of the worktree as a grid. Sort and filter it,
# edit a cell, duplicate and delete rows, look at the SQL a submit runs and
# submit it in one transaction; then the client's own console for the rest,
# and the form that adds a server.
# VHS cannot send ctrl+s, so the demo binds Submit Changes to ctrl+k.
# Record with: make demo   (needs vhs v0.10 or v0.11, ttyd, ffmpeg, a Chrome and sqlite3)
Output docs/demo/databases.gif

Require pando
Require ttyd
Require sqlite3

Source docs/demo/settings.tape
Source docs/demo/setup.tape
Type `cfg 26 ctrl+k=editor.submitChanges` Enter
Type "sqlite3 shop.db < $DEMO/shop.sql && clear" Enter
Type "pando" Enter
Sleep 1500ms
# 7 shows the Databases view; shop.db unfolds to its tables, orders opens.
Type "7"
Sleep 500ms
Type "j"
Enter
Sleep 600ms
Type "jjj"
Enter
Sleep 900ms
Ctrl+]
Sleep 300ms
Show

# The table under its WHERE and ORDER BY boxes, 200 rows a page.
Sleep 1800ms

# s sorts by the cursor's column: up, then down.
Type@150ms "llll"
Sleep 300ms
Type "s"
Sleep 900ms
Type "s"
Sleep 1300ms

# ctrl+f types SQL into WHERE; ⏎ reads the rows it keeps.
Ctrl+F
Sleep 300ms
Type "status = 'new'"
Sleep 400ms
Enter
Sleep 1600ms

# ⏎ edits the cell in a prompt over it: M marks the row.
Type "l"
Sleep 300ms
Enter
Sleep 600ms
Ctrl+U
Type "paid"
Sleep 400ms
Enter
Sleep 1000ms

# ctrl+d duplicates a row, A, its key left to the database: one cell changed.
Type "j"
Sleep 200ms
Ctrl+D
Sleep 900ms
Type@150ms "0l"
Enter
Sleep 500ms
Ctrl+U
Type "Grace"
Sleep 300ms
Enter
Sleep 900ms

# d marks a row to be deleted, D.
Type@150ms "gjj"
Sleep 200ms
Type "d"
Sleep 1300ms

# m, Show Pending SQL: what a submit runs, nothing written yet.
Type "m"
Sleep 600ms
Down@80ms 8
Sleep 500ms
Enter
Sleep 2800ms
Escape
Sleep 400ms

# ctrl+s (ctrl+k here) writes every edit in one transaction, or none.
Ctrl+K
Sleep 2200ms

# Open Console: sqlite3 in the Terminal panel, for any other SQL.
Type "m"
Sleep 500ms
Down@60ms 13
Sleep 400ms
Enter
Sleep 1500ms
Type@60ms "select status, count(*), sum(total) from orders group by 1;"
Sleep 300ms
Enter
Sleep 2800ms

# n in the view adds a server, as DataGrip's Data Source: Postgres, MariaDB
# or MySQL, its password in the system keyring, never in config.toml.
Ctrl+]
Sleep 300ms
Type "n"
Sleep 1200ms
Up
Sleep 300ms
Right
Sleep 700ms
Down
Type@80ms "shop"
Sleep 300ms
Tab
Tab
Tab
Type@80ms "app"
Tab
Type@80ms "secret"
Sleep 2500ms
Escape
Sleep 1000ms
