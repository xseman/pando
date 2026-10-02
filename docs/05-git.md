# Git

How `internal/git` runs git and gh: projects and worktrees, status, staging
selected lines, branches, commits. No libgit2: every feature is a git command
whose output is parsed once.

## Running git

`Run` is `git -C root …` with a 60 s timeout, `GIT_TERMINAL_PROMPT=0` (a
credential prompt fails instead of hanging) and `GIT_OPTIONAL_LOCKS=0`.
Every failure, git's or gh's, is a `*git.Error` carrying the arguments, stderr
(its `Error()` text) and the exit status. Test the status, never the message:

```go
if git.ExitCode(err) == 1 { … }          // diff --quiet: the file differs
errors.Is(err, context.DeadlineExceeded) // the 60 s timeout fired
```

## Projects and worktrees

A project is a repository's main worktree, kept in `state.json`; each of its
worktrees is a workspace, where sessions run.

```text
 state.json  projects: [/src/repo, /src/notes]
                             │
 /src/repo                   project = main worktree (git.MainRoot)
 ├─ .git/                    common dir, shared by every worktree
 │  └─ worktrees/feat-x/     the linked worktree's own git dir
 └─ libs/sub/  (.git)        nested repo: found by Discover, own status
                             │ git worktree add
 <data dir>/worktrees/repo/feat-x      ⑂ linked worktree, branch feat-x
 /src/notes                  not a repository: one workspace, Main
```

- `project.add` resolves a linked worktree to its project (`--git-common-dir`).
- `workspace.list` is `git worktree list --porcelain` per project, in the
  order `workspace.move` saved, git's order for the rest.
- `workspace.new` takes a branch, or `RandomBranch`
  (`worktree/<adjective>-<noun>-<4 hex>`), creating it if missing.
- `workspace.remove` refuses the main worktree and one with sessions.
- `Discover` finds nested repositories up to two levels down.

Claude Code locks a worktree it enters (`claude session NAME (pid N start T)`)
and `git worktree remove` refuses a locked one. `unlockStale` unlocks it when
that pid is gone or reused; a claude still running refuses the removal; any
other lock is left to git.

## Status

The TUI reads status on its 2 s tick, off the UI goroutine:

```text
 refreshGit (skipped while the last one runs)
   Discover(workspace) ─▶ repository roots
   per root, Stat:
     git status --porcelain -z --branch --renames --untracked-files=all
       └─ parseStatus ─▶ Branch, Upstream, Ahead, Behind, entries
     git rev-parse --git-dir ─▶ operationIn ─▶ Op, MergeMsg
   Decorations (git_deco on): entries + status --ignored ─▶ Explorer letters
   gitMsg ─▶ Model.Update
```

- Entries fall into `Staged` (X), `Changes` (Y; `??` as `U`, which the view
  splits off as Untracked) and `Conflicts` (`UU AA DD AU UA DU UD`, letter `!`,
  pair kept in `XY`).
- `operationIn` checks the worktree's own git dir as VS Code does:
  `MERGE_HEAD`, `rebase-merge`/`rebase-apply`, `CHERRY_PICK_HEAD`.
- `Diff` of a conflict is `diff --ours`, a two-way diff; an untracked file
  diffs against `/dev/null`.
- `Continue` runs `rebase --continue`, or commits a merge or cherry-pick,
  with git's `MERGE_MSG` (`--cleanup=strip`) when the box is empty.

## Staging selected lines

`ApplyLines` rebuilds the whole target file from a full-context diff, as
VS Code does, so no hunk offsets go wrong:

```text
 diff --unified=1000000000 [--cached] ─▶ pickLines ─▶ content
 content ─ git hash-object -w --no-filters --stdin ─▶ sha
           git update-index --add --cacheinfo MODE,sha,PATH
```

`MODE` comes from `git ls-files -s`, so an executable bit survives. Reverting
lines writes the working tree file instead.

## Revisions

`Revisions` lists a file's history newest first, following renames: its
uncommitted changes as a first row, then every commit. It returns revisions
or an error, never both; with no commits yet it is the one "Uncommitted
changes" row. `RevisionDiff` is one revision's patch.

## Branches

```text
git for-each-ref --sort=-creatordate refs/heads refs/remotes refs/tags
checkout: branch → git switch NAME
          remote → git switch --track NAME   (a local branch of that name wins)
          tag    → git switch --detach NAME
```

A branch checked out in another worktree opens that worktree instead.

## Commit and sync

- `Commit`, `Amend` (`--no-edit` with no message).
- `Sync`: `pull --rebase --autostash`, then `push`.
- `Publish`: `remote add` when given a URL, then `push -u REMOTE HEAD`.
  Commit & Sync publishes instead while the branch has no upstream.
- `Suggest`: `claude -p --model haiku` over the staged diff (else the
  unstaged, cut at 16 KiB); `SuggestOpts` adds a body, the recent subjects as
  a style, a message to rewrite or one to avoid. `parseSuggestion` is fuzzed.

## GitHub

`github.go` holds every gh call. `GH` runs gh in the workspace (gh finds the
repository by its remotes) with the 60 s timeout and `GH_PROMPT_DISABLED=1`;
JSON decodes straight into structs.

- `PullRequests`, `Issues`: `pr list` / `issue list --json …`.
- `Notifications`: `api repos/{owner}/{repo}/notifications`.
- `CheckoutPR`: `pr checkout N --branch B`, never `--force` (it would reset an
  existing branch).
- `PublishGitHub`: `repo create NAME --source ROOT --remote origin --push`.
- `Item.Checks` sums the check rollup as VS Code does: fail, else pending,
  else pass.

Tests run a fake gh from `PATH` that logs its arguments (`fakeGH`).
