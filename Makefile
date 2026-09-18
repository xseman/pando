PREFIX ?= $(HOME)/.local
# Stamped into the binary: `pando version`, and what the update check compares
# against. A build without it reports "dev" and is never offered an update.
VERSION ?= dev
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.12.0
GOLANGCI ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0

.PHONY: build install test lint fmt clean demo

build:
	go build -ldflags "-X github.com/xseman/pando/internal/update.Version=$(VERSION)" -o pando .

install: build
	install -Dm755 pando $(PREFIX)/bin/pando

test:
	go vet ./...
	go test -race ./...

# The style gate: gofumpt formatting plus the linters in .golangci.yml.
lint:
	$(GOLANGCI) run

fmt:
	$(GOFUMPT) -w .

# Needs vhs v0.10 or v0.11 (v0.12 records but writes no GIF: vhs#787),
# ttyd, ffmpeg, a Chrome, jq, gopls and claude.
demo: install
	vhs docs/demo/cli.tape
	vhs docs/demo/tui.tape
	vhs docs/demo/diff.tape
	vhs docs/demo/panels.tape
	vhs docs/demo/projects.tape
	vhs docs/demo/sessions.tape
	vhs docs/demo/markdown.tape
	vhs docs/demo/edit.tape
	vhs docs/demo/lsp.tape
	vhs docs/demo/vim.tape

clean:
	rm -f pando
