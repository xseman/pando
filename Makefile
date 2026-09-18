PREFIX ?= $(HOME)/.local
# Stamped into the binary: `pando version`, and what the update check compares
# against. A build without it reports "dev" and is never offered an update.
VERSION ?= dev
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.12.0
GOLANGCI ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0

# The tapes, split by whether a recording of one can be moved somewhere else.
# A parallel tape gets a repository and a daemon of its own, so several record
# at once. A serial tape cannot: it either runs claude, and a second directory
# would be a second one to trust by hand, or it shows the repository's path on
# screen, where /tmp/pando-demo is what belongs in the GIF.
PAR_TAPES ?= markdown edit lsp vim
SEQ_TAPES ?= cli diff tui panels projects sessions
JOBS ?= 4

.PHONY: build install test lint fmt clean demo $(addprefix demo-,$(PAR_TAPES) $(SEQ_TAPES))

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
# ttyd, ffmpeg, a Chrome, jq, gopls and claude. One tape: make demo-lsp.
demo: install
	$(MAKE) -j$(JOBS) $(addprefix demo-,$(PAR_TAPES))
	$(MAKE) $(addprefix demo-,$(SEQ_TAPES))

$(addprefix demo-,$(PAR_TAPES)): demo-%: install
	PANDO_DEMO_REPO=/tmp/pando-$*/pando-demo PANDO_DEMO_STATE=/tmp/pando-$*/state vhs docs/demo/$*.tape

$(addprefix demo-,$(SEQ_TAPES)): demo-%: install
	vhs docs/demo/$*.tape

clean:
	rm -f pando
