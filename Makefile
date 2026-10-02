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
PAR_TAPES ?= markdown lsp vim
SEQ_TAPES ?= cli diff edit github tui resume
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
demo: install docs/demo/bg.png
	$(MAKE) -j$(JOBS) $(addprefix demo-,$(PAR_TAPES))
	$(MAKE) $(addprefix demo-,$(SEQ_TAPES))

# A tape leaves its daemon running; each recipe stops it, pass or fail.
# The blue gradient around every GIF (settings.tape's MarginFill), built, not
# kept: four corner colors interpolated over a small image vhs scales up.
docs/demo/bg.png:
	ffmpeg -v error -y -f lavfi -i color=s=66x42 -frames:v 1 \
		-vf "format=rgb24,geq=r='(90*(W-1-X)*(H-1-Y)+38*X*(H-1-Y)+74*(W-1-X)*Y+51*X*Y)/((W-1)*(H-1))':g='(154*(W-1-X)*(H-1-Y)+126*X*(H-1-Y)+143*(W-1-X)*Y+134*X*Y)/((W-1)*(H-1))':b='(254*(W-1-X)*(H-1-Y)+252*X*(H-1-Y)+254*(W-1-X)*Y+253*X*Y)/((W-1)*(H-1))'" $@

$(addprefix demo-,$(PAR_TAPES)): demo-%: install docs/demo/bg.png
	PANDO_DEMO_REPO=/tmp/pando-$*/pando-demo PANDO_DEMO_STATE=/tmp/pando-$*/state vhs docs/demo/$*.tape; \
	s=$$?; PANDO_RUNTIME_DIR=/tmp/pando-$*/state/run pando stop >/dev/null 2>&1; exit $$s

$(addprefix demo-,$(SEQ_TAPES)): demo-%: install docs/demo/bg.png
	vhs docs/demo/$*.tape; \
	s=$$?; PANDO_RUNTIME_DIR=/tmp/pando-state/run pando stop >/dev/null 2>&1; exit $$s

clean:
	rm -f pando
