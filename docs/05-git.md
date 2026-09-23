# Git integration

`internal/git` shells out to `git -C root …` with a 60 s timeout,
`GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0`. No libgit2, no cgo; every
feature is a porcelain or plumbing command whose output is parsed once.

## Status

`git status --porcelain -z --branch --renames --untracked-files=all` → branch,
upstream, ahead/behind and entries:

```
XY path        X = index, Y = worktree
M  a.go        staged
 M b.go        changed
?? c.go        untracked  → letter U
UU d.go        conflict   → letter !, Conflicts, XY kept ("UU")
```

Entries split into the four view sections (merge / staged / tracked /
untracked); a conflict (`UU AA DD AU UA DU UD`) goes to `Conflicts` only, with
its pair in `XY` for `ConflictText` ("both modified", "deleted by them", …).
`Decorations` colors the Explorer from the same status, propagating a `*` up
the parent directories.

`operation` names what is in progress by the files VS Code checks in
`rev-parse --git-dir`: `MERGE_HEAD` → merge, `rebase-merge` or `rebase-apply`
→ rebase, `CHERRY_PICK_HEAD` → cherry-pick. `HasConflictMarkers` is VS Code's
regexp over the working tree file. `Diff` of a conflict is `diff --ours`
(working tree against stage 2), a two-way diff where plain `diff` would print
a combined one. `Continue` finishes: `commit -m` for a merge or cherry-pick,
or `commit --no-edit --cleanup=strip` with no message, which takes the
`MERGE_MSG` git wrote without its `# Conflicts:` comments; `-c
core.editor=true rebase --continue` for a rebase. `Status.MergeMsg` is that
file cleaned up the same way, for the message box to show.

## Worktrees and projects

A project is a repository's main worktree; `git worktree list --porcelain`
gives its worktrees, `Discover` also finds nested repositories under it.
`pando ws new BRANCH` creates a worktree under
`~/.local/share/pando/worktrees/<project>/<branch>`.

## Branches

```
git for-each-ref --sort=-creatordate refs/heads refs/remotes refs/tags
  → Ref{Name, Kind: branch|remote|tag, When, Author, Hash, Subject, Head}
checkout: branch → git switch NAME
          remote → git switch --track NAME   (a local branch of that name wins)
          tag    → git switch --detach NAME
```

A branch already checked out in another worktree is not an error: pando parses
the path out of git's message and opens that worktree instead.

## Writing to the index

Beyond `add`/`reset`, pando writes blobs directly for partial staging:

```
content ─ git hash-object -w --no-filters --stdin ─▶ sha
          git update-index --add --cacheinfo MODE,sha,PATH
```

`MODE` comes from `git ls-files -s`, so an executable bit survives.

## Commit

`Commit`, `Amend`, `Sync` (`pull --rebase --autostash`, then `push`), `Publish`
(`push -u REMOTE HEAD`, after `remote add` when given a URL), `Remotes`
(`remote -v`, push URLs) and `Suggest`, which pipes the staged diff into
`claude -p --model haiku` for a commit message. `PublishGitHub` stands in for
VS Code's GitHub extension with `gh repo create NAME --private|--public
--source ROOT --remote origin --push`; `HasGH` and `GHLogin` tell whether gh
is there and who it is signed in as. Commit & Sync publishes instead of
syncing while the branch has no upstream: to the only remote at once,
otherwise it opens the Publish picker after the commit.

## Errors

Every failure from this package is a `*git.Error` carrying the arguments, what
git printed on stderr and its exit status; `Error()` is still just the stderr
line the UI shows. Read the status with `git.ExitCode(err)` — 0 for success,
-1 when the command never ran — and never by matching on the message:

```go
if git.ExitCode(err) == 1 { … }          // diff --quiet: the file differs
errors.Is(err, context.DeadlineExceeded) // the 60 s timeout fired
```

`Revisions` returns revisions or an error, never both. A repository with no
commits yet has no `HEAD` to log, so it answers with the single "Uncommitted
changes" row instead of git's `fatal:`.
