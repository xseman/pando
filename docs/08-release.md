# Releasing

How a release is cut, what it ships, and how `install.sh` and the self-update
fetch and verify it.

## Pipeline

```text
feat:/fix: commits on master
        │ release-please (.github/workflows/release.yml)
        ▼
release PR ──merge──▶ tag vX.Y.Z, CHANGELOG.md, GitHub release
                              │ artifacts job, one ubuntu runner
                              ▼
          pando-{linux,darwin}-{amd64,arm64}, install.sh,
          CHECKSUMS.txt (sha256sum pando-*)
                              │
              ┌───────────────┴────────────────┐
              ▼                                ▼
   install.sh: download, verify,      daemon (internal/update): download,
   mv into ~/.local/bin               verify, os.Rename over the binary
```

| File                             | Role                                                             |
| -------------------------------- | ---------------------------------------------------------------- |
| `.github/.release-config.json`   | one package at the root, `release-type: go`, changelog sections  |
| `.github/.release-manifest.json` | the last released version; release-please writes it              |
| `.github/workflows/release.yml`  | release-please, then the cross-compiled artifacts                |
| `install.sh`                     | what `curl … \| sh` runs                                         |

- `initial-version: 0.1.0` pins the first release; release-please would
  otherwise start at `1.0.0`.
- The workflow needs a `RELEASE_PLEASE_TOKEN` secret (PAT with `contents` and
  `pull-requests` write): pull requests opened with `GITHUB_TOKEN` start no CI.
- `CGO_ENABLED=0`, so one runner cross-compiles every target.
- `-X …/internal/update.Version` links the version in. A `dev` build skips
  the daily check, but `pando update` installs the latest release over it:
  the way back after `make install`.
- `workflow_dispatch` with a tag rebuilds that tag's artifacts.

## Asset names are API

`pando-$GOOS-$GOARCH`, no archive, plus `CHECKSUMS.txt`. `install.sh` and
`update.Asset` derive the name from the running platform and refuse anything
`CHECKSUMS.txt` does not cover. Renaming an asset breaks every installed pando:
add platforms, never rename.

## install.sh

- `PANDO_VERSION` pins a release (default latest), `PANDO_INSTALL_DIR` the
  target (default `~/.local/bin`).
- The latest tag comes from the `/releases/latest` redirect: no API token, no
  rate limit.
- It verifies with `sha256sum`, `shasum` or `openssl`; with none it refuses.

## Self-update

```text
update.check   ─▶ GitHub API ─▶ tag, asset URL, sha256 ─▶ "available"
update.install ─▶ download ─▶ verify ─▶ os.Rename ─▶ "ready"
                     └─ update events (done/total) ─▶ status bar, pando update
```

- The daemon owns it: one download serves every TUI and `pando update`.
- `WatchUpdates` checks at startup and every 24 h while `update_check` is on.
- The download lands next to the binary and is renamed over it: the kernel
  refuses writes to a running executable (ETXTBSY), and a rename is atomic for
  anyone starting pando meanwhile.
- The running process keeps its old inode, so the last step is always "restart
  to update". The next TUI then finds a daemon from the older build: it
  restarts it silently when no session runs, else asks (`staleModal`).
- Tests swap `update.Path` and `update.API`; none reaches GitHub.
