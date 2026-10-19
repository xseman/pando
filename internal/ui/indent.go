package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// indent is how a file indents, as far as it says: style "tab", "space" or
// "" for the settings' insert_spaces, size the columns of one level (a
// tab's width), 0 for the settings' tab_size. picked is the status bar's
// choice, which a reload keeps.
type indent struct {
	style  string
	size   int
	picked bool
}

// fileIndent is a file's own indentation: its .editorconfig, then what its
// lines show (VS Code's editor.detectIndentation) for what that leaves out.
func fileIndent(path, text string) indent {
	ind := detectIndent(text)

	ec := editorConfig(path)
	if ec.style != "" {
		ind.style = ec.style
	}

	if ec.size > 0 || ec.style == "tab" { // a tab's width is the settings' unless it says one
		ind.size = ec.size
	}

	return ind
}

// detectIndent guesses a text's indentation from its lines, VS Code's
// guessIndentation simplified: tabs or spaces by which more lines start
// with, the spaces of a level by the step most lines indent by. A text
// with no indented line says nothing.
func detectIndent(text string) indent {
	var tabs, spaces int

	var steps [9]int

	prev := 0

	for n, line := range strings.Split(text, "\n") {
		if n >= 10000 { // enough to tell
			break
		}

		ws := len(line) - len(strings.TrimLeft(line, " \t"))
		if ws == len(strings.TrimRight(line, "\r")) { // blank: no say, nor a step
			continue
		}

		switch {
		case line[0] == '\t':
			tabs++
		case strings.HasPrefix(line, "  "): // one space is a doc comment's " *", not an indent
			spaces++
		}

		if !strings.Contains(line[:ws], "\t") {
			if d := max(ws-prev, prev-ws); d > 0 && d < len(steps) {
				steps[d]++
			}

			prev = ws
		}
	}

	switch {
	case tabs > spaces:
		return indent{style: "tab"}
	case spaces == 0:
		return indent{}
	}

	size := 0
	for _, d := range []int{2, 4, 6, 8, 3, 5, 7} { // VS Code's order: the even ones win a tie
		if steps[d] > steps[size] {
			size = d
		}
	}

	return indent{style: "space", size: size}
}

// editorConfig is what the .editorconfig files above path say of its
// indentation: indent_style, indent_size and tab_width, the nearest file
// and its last matching section winning, up to one with root = true.
func editorConfig(path string) indent {
	type file struct{ dir, text string }

	var files []file

	for dir := filepath.Dir(path); ; {
		if b, err := os.ReadFile(filepath.Join(dir, ".editorconfig")); err == nil {
			files = append(files, file{dir, string(b)})
			if root, _ := parseEditorConfig(string(b)); root {
				break
			}
		}

		up := filepath.Dir(dir)
		if up == dir {
			break
		}

		dir = up
	}

	props := map[string]string{}

	for i := len(files) - 1; i >= 0; i-- { // the farthest first, so the nearest wins
		rel, err := filepath.Rel(files[i].dir, path)
		if err != nil {
			continue
		}

		_, secs := parseEditorConfig(files[i].text)
		for _, s := range secs {
			if re := ecGlob(s.glob); re != nil && re.MatchString(filepath.ToSlash(rel)) {
				for k, v := range s.props {
					props[k] = v
				}
			}
		}
	}

	var ind indent
	if s := props["indent_style"]; s == "tab" || s == "space" {
		ind.style = s
	}

	size, _ := strconv.Atoi(props["indent_size"])
	if tw, _ := strconv.Atoi(props["tab_width"]); tw > 0 && (size <= 0 || ind.style == "tab") { // a tab draws tab_width wide; indent_size = tab, or none, is a tab
		size = tw
	}

	if size > 0 {
		ind.size = size
	}

	return ind
}

// ecSection is one [glob] of an .editorconfig and its properties, keys and
// values lower-cased; "unset" is a value like any other, which no
// indentation reads.
type ecSection struct {
	glob  string
	props map[string]string
}

// parseEditorConfig reads an .editorconfig: root = true in its preamble, and
// its sections in order. A line it cannot read is skipped, as editors do.
func parseEditorConfig(text string) (root bool, secs []ecSection) {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' && strings.HasSuffix(line, "]") {
			secs = append(secs, ecSection{glob: line[1 : len(line)-1], props: map[string]string{}})
			continue
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))
		if len(secs) == 0 {
			root = root || k == "root" && v == "true"
			continue
		}

		secs[len(secs)-1].props[k] = v
	}

	return root, secs
}

// ecGlob turns an .editorconfig section name into a regexp over a path
// relative to the file's directory, slash-separated: * stays within a
// directory, ** crosses them, ? is one character, [a-z] and [!a-z] one of a
// set, {a,b} either; a name without a slash matches at any depth. nil when
// it makes no regexp. ponytail: no {1..3} number ranges.
func ecGlob(glob string) *regexp.Regexp {
	var b strings.Builder

	if !strings.Contains(glob, "/") {
		b.WriteString("(?:.*/)?")
	}

	glob = strings.TrimPrefix(glob, "/")

	depth := 0

	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			b.WriteString(".*")

			i++

		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[' && strings.IndexByte(glob[i:], ']') > 1:
			j := i + strings.IndexByte(glob[i:], ']')
			set := glob[i+1 : j]

			neg := ""
			if set[0] == '!' {
				neg, set = "^", set[1:]
			}

			b.WriteString("[" + neg + strings.NewReplacer(`\`, `\\`, `[`, `\[`).Replace(set) + "]")

			i = j

		case c == '{':
			depth++

			b.WriteString("(?:")

		case c == '}' && depth > 0:
			depth--

			b.WriteString(")")

		case c == ',' && depth > 0:
			b.WriteString("|")
		case c == '\\' && i+1 < len(glob):
			b.WriteString(regexp.QuoteMeta(glob[i+1 : i+2]))
			i++

		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}

	re, err := regexp.Compile("(?s)^" + b.String() + strings.Repeat(")", depth) + "$")
	if err != nil {
		return nil
	}

	return re
}
