#!/usr/bin/env bash
# Builds the throwaway workspace the demo tapes record: a small Go repository
# with a commit, a few uncommitted changes and a worktree.
set -euo pipefail

demo=${1:-/tmp/pando-demo}
rm -rf "$demo"                    # pando's own state is the tape's to clear
mkdir -p "$(dirname "$demo")"
mkdir -p "$demo/internal/store"
cd "$demo"

cat > go.mod <<'EOF'
module example.com/shop

go 1.26
EOF

cat > main.go <<'EOF'
package main

import (
	"fmt"

	"example.com/shop/internal/store"
)

func main() {
	s := store.New()
	s.Add("espresso", 250)
	s.Add("cortado", 300)
	fmt.Println(s.Total(), "cents")
}
EOF

cat > internal/store/store.go <<'EOF'
package store

// Store is a tiny in-memory basket.
type Store struct {
	items map[string]int
}

func New() *Store {
	return &Store{items: map[string]int{}}
}

// Add puts an item in the basket.
func (s *Store) Add(name string, cents int) {
	s.items[name] = cents
}

// Total is what the basket costs.
func (s *Store) Total() int {
	sum := 0
	for _, cents := range s.items {
		sum += cents
	}
	return sum
}
EOF

cat > README.md <<'EOF'
# shop

A basket you can add coffee to.

- `store.New` makes one
- `store.Add` puts something in it
- `store.Total` counts the cents
EOF

git init -q -b main
git config user.email demo@example.com
git config user.name "Demo"
git add -A
git commit -qm "the basket and its store"

# Uncommitted work, so Source Control has something to show.
cat >> internal/store/store.go <<'EOF'

// Discount takes a percentage off the basket.
func (s *Store) Discount(percent int) int {
	return s.Total() * (100 - percent) / 100
}
EOF
printf '\n## Prices\n\nEverything is in cents.\n' >> README.md
printf 'package store\n\nfunc Empty() *Store { return New() }\n' > internal/store/empty.go
