#!/usr/bin/env bash
# Adds a branch whose main.go conflicts with main's, for the merge at the end
# of diff.tape. Runs after setup.sh; the uncommitted work it left stays.
set -euo pipefail

cd "${1:-/tmp/pando-demo}"
git stash -q -u
git switch -qc feat
sed -i 's/s.Add("cortado", 300)/s.Add("cortado", 300)\n\ts.Add("flat white", 350)/' main.go
git commit -qam "a flat white on the menu"
git switch -q main
sed -i 's/s.Add("cortado", 300)/s.Add("cortado", 320)/' main.go
git commit -qam "the cortado costs more"
git stash pop -q
