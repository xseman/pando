package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// defaultFormatters are the tools pando reaches for when [format] in
// config.toml says nothing about the language.
var defaultFormatters = map[string][]string{"go": {"gofmt"}}

// formatCommand is the formatter for a file: [format] keyed by the file's
// extension or by its language id, then pando's own defaults. Any tool for any
// language needs one line of config, nothing else.
func (m *Model) formatCommand(path string) []string {
	_, argv := commandFor(m.st.Settings.Format, defaultFormatters, path)
	return argv
}

// noFormatter says what to put in config.toml for a file pando cannot format.
func noFormatter(path string) error {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	return fmt.Errorf("no formatter for .%s — add one to [format] in config.toml, e.g. %s = [\"prettier\", \"--stdin-filepath\", \"$FILE\"]", ext, ext)
}

// runFormatter pipes src through the tool and returns what it writes. The
// tool reads stdin and writes stdout, the one interface every formatter has;
// $FILE in an argument becomes the file's path, for the tools that want to
// know what they are reading.
// ponytail: this runs on the update loop, so a tool that hangs freezes the TUI
// until the timeout; make it a tea.Cmd if a slow formatter ever bites.
func runFormatter(argv []string, path, src string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := slices.Clone(argv)
	for i, a := range args {
		args[i] = strings.ReplaceAll(a, "$FILE", path)
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = filepath.Dir(path)
	cmd.Stdin = strings.NewReader(src)

	var out, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); msg != "" {
			return "", errors.New(args[0] + ": " + msg)
		}

		return "", fmt.Errorf("%s: %w", args[0], err)
	}

	return out.String(), nil
}

// format runs the file's formatter over the text the editor holds, VS Code's
// Format Document. A tool that fails changes nothing: a file that does not
// parse keeps its text and the complaint goes to the status bar.
func (p *preview) format(m *Model) error {
	if !p.editable() {
		return errors.New("open a file first")
	}

	argv := m.formatCommand(p.path)
	if len(argv) == 0 {
		return noFormatter(p.path)
	}

	out, err := runFormatter(argv, p.path, p.text())
	if err != nil {
		return err
	}

	p.setText(m, out)

	return nil
}

// setText puts formatted text into the editor. Only the lines that actually
// differ go through the edit, so a gofmt that fixed one line re-lexes one line
// and not the file, and the view keeps its scroll and its cursor.
func (p *preview) setText(m *Model, text string) {
	endNL := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	old, want := strings.Split(p.buf.text(), "\n"), strings.Split(text, "\n")

	a := 0
	for a < len(old) && a < len(want) && old[a] == want[a] {
		a++
	}

	za, zb := len(old), len(want)
	for za > a && zb > a && old[za-1] == want[zb-1] {
		za--
		zb--
	}

	if za == a && zb == a { // already formatted
		p.buf.endNL = endNL
		return
	}

	last := len(old) - 1

	from, to, repl := pos{a, 0}, pos{za, 0}, strings.Join(want[a:zb], "\n")
	switch {
	case za < len(old): // the run ends before the last line, so it takes its newline
		if zb > a {
			repl += "\n"
		}

	case a > 0: // it runs to the end of the file: keep the newline before it
		from, to = pos{a - 1, len(p.buf.line(a - 1))}, pos{last, len(p.buf.line(last))}
		if repl = ""; zb > a {
			repl = "\n" + strings.Join(want[a:zb], "\n")
		}

	default: // every line changed
		to = pos{last, len(p.buf.line(last))}
	}

	at, top, left := p.cur, p.top, p.left
	p.editRaw(m, from, to, repl)
	p.buf.endNL = endNL
	p.top, p.left = top, left // the edit scrolled to its end; the reader did not move
	p.setCursor(m, at)
}

// formatDoc is Format Document: the key and the menu entry both land here.
func (p *preview) formatDoc(m *Model) tea.Cmd {
	if err := p.format(m); err != nil {
		return flash(err.Error(), true)
	}

	return flash("formatted "+filepath.Base(p.path), false)
}
