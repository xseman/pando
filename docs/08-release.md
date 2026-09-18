# Releasing

Conventional commits on `master` → release-please opens a release PR → merging
it tags `vX.Y.Z`, writes `CHANGELOG.md` and publishes a GitHub release →
`.github/workflows/release.yml` builds the binaries and uploads them.

```
commit (feat: …)  ─▶  release PR  ─merge─▶  tag vX.Y.Z + release
                                                   │
                                                   ├─ pando-linux-amd64
                                                   ├─ pando-linux-arm64
                                                   ├─ pando-darwin-amd64
                                                   ├─ pando-darwin-arm64
                                                   ├─ CHECKSUMS.txt
                                                   └─ install.sh
```

| File                             | What it does                                                         |
| -------------------------------- | -------------------------------------------------------------------- |
| `.github/.release-config.json`   | one package at the repository root, `release-type: go`, tag `vX.Y.Z` |
| `.github/.release-manifest.json` | the last released version; release-please writes it                  |
| `.github/workflows/release.yml`  | release-please, then the cross-compiled artifacts                    |
| `install.sh`                     | what `curl … \| sh` runs                                              |
| `internal/update`                | the same download from inside pando                                  |

- The release job needs a `RELEASE_PLEASE_TOKEN` secret (a PAT with `contents`
  and `pull-requests` write). `GITHUB_TOKEN` would work too, but pull requests
  it opens do not start CI.
- Pure Go with `CGO_ENABLED=0`, so one Linux runner cross-compiles every
  target. `-X …/internal/update.Version` links the version in: it is what
  `pando version` prints and what the update check compares against. A build
  without it says `dev` and is never offered an update.
- `workflow_dispatch` with a tag rebuilds and re-uploads that tag's artifacts,
  for a release whose build failed.

## Asset names are API

`pando-$GOOS-$GOARCH`, no archive, plus `CHECKSUMS.txt` from `sha256sum`.
`install.sh` and `internal/update` both derive the file name from the platform
they run on and refuse anything `CHECKSUMS.txt` does not cover, so renaming an
asset breaks every installed pando. Add platforms, never rename.

## Self-update

```
update.check   ─ GitHub API ─▶ tag, asset URL, sha256   → state "available"
update.install ─ download ────▶ verify ──▶ rename over the binary
                     │
                     └─ update events: done/total ──▶ status bar meter
```

The daemon owns it (`internal/daemon/update.go`) so one download serves every
attached TUI, and the status bar chip and `pando update` follow the same
events. The binary is replaced by `os.Rename` next to itself: the kernel
refuses to write into a running executable, and a rename is atomic for anyone
starting pando meanwhile. The running process keeps its own inode, which is why
the last word is always "restart to update" — after which the TUI finds a
daemon from the older build and offers to restart that too.

`update.Path` and `update.API` are variables so a test can aim an install at a
scratch file and a local server; nothing in the test suite reaches GitHub.
