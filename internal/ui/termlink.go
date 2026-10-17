package ui

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// termLink is a link in a line of terminal output, found the way VS Code's
// terminal finds them: an OSC 8 hyperlink, a URL, or a path with the line and
// column a compiler, a linter or a traceback prints after it.
type termLink struct {
	from, to  int    // the cells it covers, [from, to)
	url       string // a hyperlink's or a URL's target; empty for a path
	path      string // the path as printed
	line, col int    // 1-based; 0 when the text gives none
}

var (
	urlRe      = regexp.MustCompile("(?:https?|file)://[^\\s<>\"'`]+")
	fileLineRe = regexp.MustCompile(`^(file://.*?):([1-9]\d*)(?::(\d+))?$`)             // Node's file:///a.mjs:12:5
	pathRe     = regexp.MustCompile(`(?:[A-Za-z]:[\\/])?[\p{L}\p{M}\p{N}_.~+@%=/\\-]+`) // marks too: macOS names files decomposed
	quotedRe   = regexp.MustCompile(`"([^"\x00-\x1f]+)"`)                               // a path with spaces, as Python quotes it
	// lineRe is the position after a path, or after its closing quote: :12,
	// :12:5, (12), (12,5), (12, 5), ", line 12" or ", line 12, column 5"
	// (Python), #L12 or #L12C5 (GitHub's anchors).
	lineRe = regexp.MustCompile(`^"?(?::(\d+)(?::(\d+))?|\((\d+)(?:, ?(\d+))?\)|", line (\d+)(?:, col(?:umn)? (\d+))?|#L(\d+)(?:C(\d+))?)`)
)

// termLinks are the links on a terminal line as the daemon renders it,
// escape sequences in, ordered by cell: its OSC 8 hyperlinks, then the URLs
// and paths in its text that none of them covers.
func termLinks(line string) []termLink {
	plain := ansi.Strip(line)
	w := ansi.StringWidth(plain)

	out := slices.DeleteFunc(osc8Links(line), func(l termLink) bool { return l.to > w }) // past a broken escape the two count apart
	for _, l := range textLinks(plain) {
		if !slices.ContainsFunc(out, func(h termLink) bool { return l.from < h.to && h.from < l.to }) {
			out = append(out, l)
		}
	}

	slices.SortFunc(out, func(a, b termLink) int { return a.from - b.from })

	return out
}

// osc8Links are the runs of cells an OSC 8 sequence gives a target, one link
// per run.
func osc8Links(line string) []termLink {
	var (
		out    []termLink
		target string
		x      int
		state  byte
	)

	p := ansi.GetParser()
	defer ansi.PutParser(p)

	for line != "" {
		seq, width, n, next := ansi.DecodeSequence(line, state, p)
		if n == 0 {
			break
		}

		line, state = line[n:], next

		switch {
		case width == 0 && ansi.HasOscPrefix(seq) && p.Command() == 8:
			target = ""
			if parts := strings.SplitN(string(p.Data()), ";", 3); len(parts) == 3 {
				target = parts[2]
			}

		case width > 0 && target != "":
			if k := len(out) - 1; k >= 0 && out[k].url == target && out[k].to == x {
				out[k].to += width
			} else {
				out = append(out, termLink{from: x, to: x + width, url: target})
			}
		}

		x += width
	}

	return out
}

// textLinks are the URLs and paths in plain text s: a run of path
// characters with a letter in it and a dot or a slash, or a line after it
// (Makefile:12). Whether it names a file is for resolveLink to find out, on
// the disk.
func textLinks(s string) []termLink {
	var out []termLink

	// ponytail: a cell is counted by the width of the text before it, so a
	// mark or a format character joining a grapheme across a link's edge
	// (U+06DD before a path) can put that link a cell off.
	// The offsets come in order within each pass (URLs, quoted paths, paths),
	// so the width runs on from the last one, starting over for the next pass:
	// a row as wide as the screen is measured once a pass, not once a link.
	at, width := 0, 0
	cells := func(i int) int {
		if i < at {
			at, width = 0, 0
		}

		width += ansi.StringWidth(s[at:i])
		at = i

		return width
	}

	urls := urlRe.FindAllStringIndex(s, -1)
	for _, u := range urls {
		target := trimURL(s[u[0]:u[1]])
		l := termLink{from: cells(u[0]), to: cells(u[0] + len(target)), url: target}

		if g := fileLineRe.FindStringSubmatch(target); g != nil { // Node's file:///a.mjs:12:5
			l.url, l.line, l.col = g[1], firstInt(g[2]), firstInt(g[3])
		}

		out = append(out, l)
	}

	// A quoted path with a position after it is one link, spaces and all
	// (VS Code's: File "/a b/x.py", line 3); without one a quoted string is
	// more likely prose, whose paths the runs below find.
	for _, q := range quotedRe.FindAllStringSubmatchIndex(s, -1) {
		line, col, n := position(s[q[3]:])
		if line == 0 || !strings.ContainsAny(s[q[2]:q[3]], `./\`) ||
			slices.ContainsFunc(urls, func(u []int) bool { return q[0] < u[1] && u[0] < q[1] }) {
			continue
		}

		urls = append(urls, []int{q[0], q[3] + n}) // no path run inside it
		out = append(out, termLink{from: cells(q[2]), to: cells(q[3] + n), path: s[q[2]:q[3]], line: line, col: col})
	}

	taken := 0 // the end of the last path's position, which the next run may start inside: a.go:1x/y

	for _, r := range pathRe.FindAllStringIndex(s, -1) {
		if r[0] < taken || slices.ContainsFunc(urls, func(u []int) bool { return r[0] < u[1] && u[0] < r[1] }) {
			continue
		}

		path := strings.TrimRight(s[r[0]:r[1]], ".-") // the end of a sentence
		if !strings.ContainsFunc(path, unicode.IsLetter) {
			continue
		}

		l, end := termLink{from: cells(r[0]), path: path}, r[0]+len(path)
		if line, col, n := position(s[end:]); line > 0 && end == r[1] {
			l.line, l.col = line, col
			end += n
		}

		if l.line == 0 && !strings.ContainsAny(path, `./\`) {
			continue // a word
		}

		l.to, taken = cells(end), end
		out = append(out, l)
	}

	slices.SortFunc(out, func(a, b termLink) int { return a.from - b.from })

	return out
}

// position is the line, column and length of the position lineRe reads at
// the start of s; line 0 when there is none, as lines count from 1.
func position(s string) (line, col, n int) {
	g := lineRe.FindStringSubmatch(s)
	if len(g) == 0 {
		return 0, 0, 0
	}

	if line = firstInt(g[1], g[3], g[5], g[7]); line == 0 {
		return 0, 0, 0
	}

	return line, firstInt(g[2], g[4], g[6], g[8]), len(g[0])
}

// trimURL drops what ends the sentence around a URL: punctuation, and a
// closing parenthesis it did not open.
func trimURL(u string) string {
	for {
		t := strings.TrimRight(u, ".,;:!?'\"")
		if strings.HasSuffix(t, ")") && strings.Count(t, "(") < strings.Count(t, ")") {
			t = t[:len(t)-1]
		}

		if t == u {
			return u
		}

		u = t
	}
}

func firstInt(ss ...string) int {
	for _, s := range ss {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}

	return 0
}

// linkHover is the terminal link under the pointer, in the logical line
// whose rows start at screen row ry, line n (lineAt), wrapped at cols: what
// a ctrl+click opens, underlined while the pointer stays on screen cell
// (sx, sy) and the line on those rows.
type linkHover struct {
	termLink          // from and to count the cells of the rows run together
	n, ry, rows, cols int
	sx, sy            int
	target            string // the file or the URL it opens
	search            bool   // no such file here, but printed with a line: quick open looks for it
}

// tip is VS Code's hover over a terminal link.
func (h *linkHover) tip() string {
	switch {
	case h.search:
		return "Search Workspace  ^click"
	case h.url != "" && !strings.HasPrefix(h.url, "file:"):
		return "Follow Link  ^click"
	}

	return "Open File in Editor  ^click"
}

// paintLink underlines the link under the pointer on screen row y of line,
// and puts its tip up.
// ponytail: the link loses its colors under the pointer, the trade the
// selection and the find hits make (paintSpans).
func (t *term) paintLink(m *Model, y int, line string) string {
	h := t.link
	if h == nil || y < h.ry || y >= h.ry+h.rows || t.lineAt(h.ry) != h.n || m.mouseX != h.sx || m.mouseY != h.sy {
		return line
	}

	off := (y - h.ry) * h.cols

	a, b := max(h.from-off, 0), h.to-off
	if b <= a {
		return line
	}

	m.tip = h.tip()

	return ansi.Cut(line, 0, a) + "\x1b[0;4m" + ansi.Strip(ansi.Cut(line, a, b)) + "\x1b[0m" + ansi.Cut(line, b, wideCols)
}

// linkAt is the link at cell x of screen row y, resolved against the
// worktree of session id; nil when there is none there or it leads nowhere.
func (t *term) linkAt(m *Model, id string, x, y int) *linkHover {
	if y < 0 || y >= len(t.scr.Lines) {
		return nil
	}

	ry, rows, line := t.runOn(y)
	x += (y - ry) * t.scr.Cols

	bases := []string{t.scr.Cwd} // the directory of the program in front first, as VS Code's terminal resolves; then the worktree
	if s := m.session(id); s != nil {
		bases = append(bases, s.Workspace)
	}

	for _, l := range termLinks(line) {
		if x < l.from || x >= l.to {
			continue
		}

		h := &linkHover{termLink: l, n: t.lineAt(ry), ry: ry, rows: rows, cols: t.scr.Cols}
		if h.target = resolveLink(bases, l); h.target == "" {
			// Not here: worth a search when printed with a line, or as a path
			// with a directory and an extension (rg -l, git status from a subdirectory).
			h.search = l.url == "" && (l.line > 0 && strings.ContainsAny(l.path, `./\`) ||
				strings.ContainsAny(l.path, `/\`) && filepath.Ext(l.path) != "")
			if !h.search {
				return nil
			}
		}

		return h
	}

	return nil
}

// runOn is the logical line screen row y is in: rows as wide as the
// terminal run on into the next, as wrapped text does (VS Code's terminal
// joins them the same way before it looks for links). Its first row, its
// rows, and the rows joined.
func (t *term) runOn(y int) (ry, rows int, line string) {
	// The emulator pads a row it cleared with spaces: one that ends in
	// them did not run on.
	full := func(i int) bool {
		return t.scr.Cols > 0 && ansi.StringWidth(strings.TrimRight(ansi.Strip(t.scr.Lines[i]), " ")) >= t.scr.Cols
	}

	ry, last := y, y
	for ry > 0 && full(ry-1) {
		ry--
	}

	for last+1 < len(t.scr.Lines) && full(last) {
		last++
	}

	return ry, last - ry + 1, strings.Join(t.scr.Lines[ry:last+1], "")
}

// resolveLink is the file or URL link l opens: a web URL as it is, a file
// URL's or a path's file when one is there. A relative path is the first of
// bases' that has it, or a diff's a/ or b/ path in one.
// ponytail: the directory of the program in front now, not of the one that
// printed the line (VS Code's shell integration knows that), and none where
// /proc does not say.
func resolveLink(bases []string, l termLink) string {
	path := l.path

	if l.url != "" {
		u, err := url.Parse(l.url)
		if err != nil || u.Scheme != "file" {
			return l.url
		}

		path = u.Path
	}

	try := []string{expandHome(path)}
	if !filepath.IsAbs(try[0]) {
		try = nil

		for _, base := range bases {
			if base == "" {
				continue
			}

			try = append(try, filepath.Join(base, path))

			if rest, ok := strings.CutPrefix(filepath.ToSlash(path), "a/"); ok {
				try = append(try, filepath.Join(base, rest))
			} else if rest, ok := strings.CutPrefix(filepath.ToSlash(path), "b/"); ok {
				try = append(try, filepath.Join(base, rest))
			}
		}
	}

	for _, p := range try {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}

	return ""
}

// followLink is a ctrl+click on h: a URL in the browser, a file in the
// editor at its line and column, a path not found in quick open.
func (m *Model) followLink(h linkHover) tea.Cmd {
	switch {
	case h.search:
		return m.searchLink(h.termLink)
	case h.url != "" && !strings.HasPrefix(h.url, "file:"):
		return openURL(h.url)
	}

	return m.openLine(h.target, h.line, h.col)
}

// openLine opens path at a tool's line and column, 1-based, 0 for none: a
// compiler's column counts a tab as one, the editor's cells as tab_size.
func (m *Model) openLine(path string, line, col int) tea.Cmd {
	if line <= 0 {
		return m.openFile(path, false)
	}

	return m.openFileAt(path, line-1, displayCol(fileLine(path, line-1), max(col-1, 0)), 0, false)
}

// searchLink looks for a path printed relative to somewhere else: the one
// file of the worktree whose path ends in it opens at its line, as VS Code's
// terminal does; otherwise quick open lists the candidates.
func (m *Model) searchLink(l termLink) tea.Cmd {
	load := m.loadIndex(true)

	q := filepath.ToSlash(l.path) // what the paths end in: not the ./ and ../ it was printed from
	for strings.HasPrefix(q, "./") || strings.HasPrefix(q, "../") {
		q = q[strings.Index(q, "/")+1:]
	}

	return func() tea.Msg {
		msg, _ := load().(indexMsg)
		msg.query, msg.line, msg.col = q, l.line, l.col

		return msg
	}
}

// openQuery is quick open for a query a link gave: the one file whose path
// ends in it opens at once, else the picker shows the query.
func (m *Model) openQuery(msg indexMsg) tea.Cmd {
	var hits []string

	for _, f := range msg.files {
		if f == msg.query || strings.HasSuffix(f, "/"+msg.query) {
			hits = append(hits, f)
		}
	}

	if len(hits) == 1 {
		return m.openLine(filepath.Join(msg.ws, hits[0]), msg.line, msg.col)
	}

	q := msg.query // quick open's own form, which its pick reads back
	if msg.line > 0 {
		q += ":" + strconv.Itoa(msg.line)
	}

	if msg.col > 0 {
		q += ":" + strconv.Itoa(msg.col)
	}

	m.quickOpen(msg)
	m.modal.input.SetValue(q)
	m.modal.refilter()

	return nil
}
