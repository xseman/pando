package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// maxSearchMatches caps one search, like VS Code's search.maxResults.
const maxSearchMatches = 2000

type searchOpts struct {
	query, include, exclude string
	caseSens, word, regex   bool
	hidden                  bool // search dotfiles too (Files' hidden setting)
}

// searchLine is one matching line; ranges are byte offsets of its matches.
type searchLine struct {
	line   int // 1-based
	text   string
	ranges [][2]int
}

type searchFile struct {
	path  string // relative to the workspace
	lines []searchLine
	count int // matches, not lines
}

type (
	searchTickMsg int // a pause in typing ended; the search generation it belongs to
	searchMsg     struct {
		gen       int
		files     []searchFile
		truncated bool
		err       error
	}
)

// srRow is a result row: a directory (file -1), a file header (line -1) or
// one of a file's matching lines.
type srRow struct {
	file, line   int
	dir, label   string
	depth, count int
}

// searchView is VS Code's Search: a query with match case, whole word and
// regex toggles, an optional replace box, files-to-include and files-to-exclude
// globs, and matches grouped by file. It searches while typing.
type searchView struct {
	query, replace, include, exclude textinput.Model
	showReplace, showDetails         bool
	preserveCase                     bool
	tree                             bool
	caseSens, word, regex            bool
	files                            []searchFile
	truncated                        bool
	err                              string
	busy                             bool
	gen                              int
	cancel                           context.CancelFunc
	closed                           map[string]bool
	l                                list
	ws                               string         // workspace the results belong to
	re                               *regexp.Regexp // the last query as a regexp, for replace
}

func (s *searchView) init() {
	s.query, s.replace, s.include = textinput.New(), textinput.New(), textinput.New()
	s.query.Prompt, s.replace.Prompt, s.include.Prompt = "", "", ""
	s.exclude = textinput.New()
	s.exclude.Prompt = ""
	s.query.Placeholder, s.replace.Placeholder = "Search", "Replace"
	s.include.Placeholder, s.exclude.Placeholder = "e.g. *.ts, src/**", "e.g. **/dist"
	s.closed, s.l = map[string]bool{}, list{sel: -1}
}

// boxes are the text inputs in the order tab visits them.
func (s *searchView) boxes() []*textinput.Model {
	out := []*textinput.Model{&s.query}
	if s.showReplace {
		out = append(out, &s.replace)
	}

	if s.showDetails {
		out = append(out, &s.include, &s.exclude)
	}

	return out
}

func (s *searchView) editing() bool {
	return s.query.Focused() || s.replace.Focused() || s.include.Focused() || s.exclude.Focused()
}

func (s *searchView) blur() {
	for _, in := range []*textinput.Model{&s.query, &s.replace, &s.include, &s.exclude} {
		in.Blur()
	}
}

// replacing is the replacement text while the replace box is open.
func (s *searchView) replacing() (string, bool) {
	return s.replace.Value(), s.showReplace && s.replace.Value() != ""
}

// focus puts the caret in the query box.
func (s *searchView) focus() tea.Cmd {
	s.blur()
	s.query.CursorEnd()

	return s.query.Focus()
}

// clear drops the results, for another workspace or the clear action.
func (s *searchView) clear() {
	if s.cancel != nil {
		s.cancel()
	}

	s.gen++
	s.files, s.truncated, s.err, s.busy, s.cancel = nil, false, "", false, nil
	s.l = list{sel: -1}
}

// headH is the rows above the results: a blank row, the query box, the
// replace box and the include/exclude details when open, and the summary.
func (s *searchView) headH() int { return 3 + b2i(s.showReplace) + 4*b2i(s.showDetails) }

// rows lists the result rows: files with their matching lines, grouped under
// directories in tree mode.
func (s *searchView) rows() []srRow {
	if !s.tree {
		var out []srRow
		for i := range s.files {
			out = append(out, s.fileRows(i, 0)...)
		}

		return out
	}

	byPath := make(map[string]int, len(s.files))

	paths := make([]string, len(s.files))
	for i, f := range s.files {
		byPath[f.path], paths[i] = i, f.path
	}

	var (
		out  []srRow
		walk func(t *ptree, dir string, depth int)
	)

	walk = func(t *ptree, dir string, depth int) {
		for _, name := range t.dirNames() {
			child, label, p := t.dirs[name], name, path.Join(dir, name)
			for len(child.files) == 0 && len(child.dirs) == 1 { // compact single-child chains
				for n, c := range child.dirs {
					label, p, child = label+"/"+n, path.Join(p, n), c
				}
			}

			out = append(out, srRow{file: -1, dir: p, label: label, depth: depth, count: s.dirCount(p)})
			if !s.closed["dir:"+p] {
				walk(child, p, depth+1)
			}
		}

		for _, f := range t.sortedFiles() {
			out = append(out, s.fileRows(byPath[f], depth)...)
		}
	}
	walk(buildTree(paths), "", 0)

	return out
}

// fileRows is one file's header and, unless it is folded, its matching lines.
func (s *searchView) fileRows(i, depth int) []srRow {
	out := []srRow{{file: i, line: -1, depth: depth}}
	if !s.closed[s.files[i].path] {
		for j := range s.files[i].lines {
			out = append(out, srRow{file: i, line: j, depth: depth})
		}
	}

	return out
}

// dirCount is how many matches sit under a directory.
func (s *searchView) dirCount(dir string) int {
	n := 0

	for _, f := range s.files {
		if strings.HasPrefix(f.path, dir+"/") {
			n += f.count
		}
	}

	return n
}

func (s *searchView) toggle(m *Model, key string) tea.Cmd {
	switch key {
	case "alt+c":
		s.caseSens = !s.caseSens
	case "alt+w":
		s.word = !s.word
	case "alt+r":
		s.regex = !s.regex
	case "alt+p":
		s.preserveCase = !s.preserveCase
		return nil
	}

	return s.restart(m)
}

// toggles are an input box's buttons, right-aligned before its edge: match
// case, whole word and regex on the query, preserve case and replace all on
// the replacement.
func (s *searchView) toggles(w int, box *textinput.Model) []rowAction {
	acts := []rowAction{
		{g: icCase, run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+c") }},
		{g: icWord, run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+w") }},
		{g: icRegex, run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+r") }},
	}
	if box == &s.replace {
		acts = []rowAction{
			{g: icPreserve, run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+p") }},
			{g: icReplaceAll, run: func(m *Model) tea.Cmd { return s.confirmReplaceAll(m) }},
		}
	}

	layoutRight(acts, w-3, 1) // " ▕ " follows the buttons

	return acts
}

// restart schedules a search for the current query after a pause in typing.
func (s *searchView) restart(_ *Model) tea.Cmd {
	s.clear()

	if s.query.Value() == "" {
		return nil
	}

	s.busy = true
	gen := s.gen

	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return searchTickMsg(gen) })
}

func (s *searchView) onTick(m *Model, gen int) tea.Cmd {
	if gen != s.gen {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.ws = cancel, m.ws
	o := searchOpts{
		query: s.query.Value(), include: s.include.Value(), exclude: s.exclude.Value(),
		caseSens: s.caseSens, word: s.word, regex: s.regex, hidden: m.st.Settings.Hidden,
	}
	s.re = matcher(o)
	root := m.ws

	return func() tea.Msg {
		//nolint:contextcheck // searchEngine only probes: exec.LookPath takes no context and git.Root has no context form.
		files, truncated, err := runSearch(ctx, root, o, searchEngine(root))
		return searchMsg{gen: gen, files: files, truncated: truncated, err: err}
	}
}

func (s *searchView) onResult(msg searchMsg) {
	if msg.gen != s.gen {
		return
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.busy, s.cancel = false, nil

	s.files, s.truncated, s.err = msg.files, msg.truncated, ""
	if msg.err != nil {
		s.err = msg.err.Error()
	}

	s.l = list{sel: -1}
}

// searchEngine picks ripgrep, git grep inside a repository, or grep.
func searchEngine(root string) string {
	if _, err := exec.LookPath("rg"); err == nil {
		return "rg"
	}

	if _, err := git.Root(root); err == nil {
		return "git"
	}

	return "grep"
}

// globs splits "src/**, *.go" into patterns.
func globs(include string) []string {
	var out []string

	for _, g := range strings.Split(include, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}

	return out
}

// grepFlags are the matching options git grep and grep share.
func grepFlags(o searchOpts) []string {
	flags := []string{"-F"}
	if o.regex {
		flags[0] = "-E"
	}

	if !o.caseSens {
		flags = append(flags, "-i")
	}

	if o.word {
		flags = append(flags, "-w")
	}

	return flags
}

// matcher finds match ranges for engines that report only lines.
func matcher(o searchOpts) *regexp.Regexp {
	pat := o.query
	if !o.regex {
		pat = regexp.QuoteMeta(pat)
	}

	if o.word {
		pat = `\b(?:` + pat + `)\b`
	}

	if !o.caseSens {
		pat = "(?i)" + pat
	}

	re, _ := regexp.Compile(pat)

	return re
}

// runSearch finds o.query under root and returns matches by file, sorted by
// path; truncated reports that maxSearchMatches stopped it.
func runSearch(ctx context.Context, root string, o searchOpts, engine string) ([]searchFile, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var args []string

	switch engine {
	case "rg":
		args = []string{"rg", "--json", "--no-config", "--no-messages", "--ignore-case"}
		if o.caseSens {
			args[4] = "--case-sensitive"
		}

		if o.word {
			args = append(args, "--word-regexp")
		}

		if !o.regex {
			args = append(args, "--fixed-strings")
		}

		if o.hidden {
			args = append(args, "--hidden", "--glob", "!.git")
		}

		for _, g := range globs(o.include) {
			args = append(args, "--glob", g)
		}

		for _, g := range globs(o.exclude) {
			args = append(args, "--glob", "!"+g)
		}

		args = append(args, "--regexp", o.query, ".")

	case "git":
		args = append([]string{"git", "grep", "-z", "-n", "-I", "--untracked", "--no-color"}, grepFlags(o)...)

		args = append(args, "-e", o.query, "--")
		for _, g := range globs(o.include) {
			if !strings.Contains(g, "/") {
				g = "**/" + g
			}

			args = append(args, ":(glob)"+g)
		}

		for _, g := range globs(o.exclude) {
			if !strings.Contains(g, "/") {
				g = "**/" + g
			}

			args = append(args, ":(glob,exclude)"+g)
		}

	default:
		args = append([]string{"grep", "-rnIZ", "--exclude-dir=.git"}, grepFlags(o)...)
		for _, g := range globs(o.include) {
			args = append(args, "--include="+path.Base(g))
		}

		for _, g := range globs(o.exclude) {
			args = append(args, "--exclude="+path.Base(g))
		}

		args = append(args, "-e", o.query, ".")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = root

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}

	if err := cmd.Start(); err != nil {
		return nil, false, err
	}

	re := matcher(o)
	byPath := map[string]*searchFile{}
	total, truncated := 0, false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for sc.Scan() {
		p, ln, ok := parseSearchLine(engine, sc.Bytes(), re)
		if !ok {
			continue
		}

		p = strings.TrimPrefix(p, "./")

		f := byPath[p]
		if f == nil {
			f = &searchFile{path: p}
			byPath[p] = f
		}

		n := max(len(ln.ranges), 1)

		f.lines, f.count, total = append(f.lines, ln), f.count+n, total+n
		if total >= maxSearchMatches {
			truncated = true

			cancel()

			break
		}
	}

	err = cmd.Wait()

	var exit *exec.ExitError
	switch {
	case truncated || ctx.Err() != nil:
		err = nil
	case errors.As(err, &exit) && exit.ExitCode() == 1: // no matches
		err = nil
	case err != nil:
		if msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); msg != "" {
			err = errors.New(msg)
		}
	}

	files := make([]searchFile, 0, len(byPath))
	for _, f := range byPath {
		files = append(files, *f)
	}

	slices.SortFunc(files, func(a, b searchFile) int { return strings.Compare(a.path, b.path) })

	return files, truncated, err
}

// parseSearchLine reads one output line: rg's JSON, or grep's
// "path\0line[:\0]text" from `git grep -z` and `grep -Z`.
func parseSearchLine(engine string, b []byte, re *regexp.Regexp) (string, searchLine, bool) {
	if engine == "rg" {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				Lines struct {
					Text string `json:"text"`
				} `json:"lines"`
				LineNumber int `json:"line_number"`
				Submatches []struct {
					Start int `json:"start"`
					End   int `json:"end"`
				} `json:"submatches"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &ev) != nil || ev.Type != "match" || ev.Data.Path.Text == "" {
			return "", searchLine{}, false
		}

		ln := searchLine{line: ev.Data.LineNumber, text: strings.TrimRight(ev.Data.Lines.Text, "\r\n")}
		for _, sm := range ev.Data.Submatches {
			ln.ranges = append(ln.ranges, [2]int{min(sm.Start, len(ln.text)), min(sm.End, len(ln.text))})
		}

		return ev.Data.Path.Text, ln, true
	}

	p, rest, ok := bytes.Cut(b, []byte{0})
	if !ok {
		return "", searchLine{}, false
	}

	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}

	if digits == 0 || digits == len(rest) {
		return "", searchLine{}, false
	}

	n, _ := strconv.Atoi(string(rest[:digits]))

	ln := searchLine{line: n, text: strings.TrimRight(string(rest[digits+1:]), "\r\n")}
	if re != nil {
		for _, r := range re.FindAllStringIndex(ln.text, -1) {
			ln.ranges = append(ln.ranges, [2]int{r[0], r[1]})
		}
	}

	return string(p), ln, true
}

func (s *searchView) lines(m *Model, w, h int) []string {
	out := []string{blank(w), s.inputRow(m, &s.query, w)}
	if s.showReplace {
		out = append(out, s.inputRow(m, &s.replace, w))
	}

	if s.showDetails {
		out = append(out, row(w, nil, []seg{sg("   files to include", dim)}), s.inputRow(m, &s.include, w),
			row(w, nil, []seg{sg("   files to exclude", dim)}), s.inputRow(m, &s.exclude, w))
	}

	out = append(out, s.summary(w))
	rows := s.rows()
	bh := max(h-len(out), 0)
	s.l.clamp(len(rows), bh)
	hover := m.hoverRow(viewSearch) - len(out)

	return append(out, s.l.render(w, bh, len(rows), func(i, rw int) string {
		return s.renderRow(m, rows[i], rw, i == s.l.sel, i-s.l.top == hover)
	})...)
}

// inputRow is a one-row input box: a chevron opening the replacement on the
// query row, the text with room to breathe, then the box's toggles.
func (s *searchView) inputRow(m *Model, in *textinput.Model, w int) string {
	box := lipgloss.NewStyle().Background(pal.inputBg)

	edge := fg(pal.inputBorder).Background(pal.inputBg)
	if in.Focused() {
		edge = fg(pal.accent).Background(pal.inputBg)
	}

	var acts []rowAction

	tw := 0

	if in == &s.query || in == &s.replace {
		acts = s.toggles(w, in)
		for _, a := range acts {
			tw += a.w
		}
	}

	field := w - 7 - tw
	if field < 1 {
		return blank(w)
	}

	in.SetStyles(inputStyles(m.dark))
	in.SetWidth(field)

	text := in.View()
	if pad := field - ansi.StringWidth(text); pad > 0 {
		text += box.Render(blank(pad))
	} else {
		text = ansi.Truncate(text, field, "")
	}

	lead := "  "
	if in == &s.query { // the chevron opens and closes the replacement
		lead = " " + chevron(s.showReplace)[:len(chevron(s.showReplace))-1]
	}

	var b strings.Builder
	b.WriteString(lead + edge.Render("▏") + box.Render(" ") + text)

	on := []bool{s.caseSens, s.word, s.regex}
	if in == &s.replace {
		on = []bool{s.preserveCase, false}
	}

	for i, a := range acts {
		st := box.Faint(true)
		if on[i] {
			st = lipgloss.NewStyle().Background(pal.accent).Foreground(pal.buttonFg)
		}

		b.WriteString(box.Render(" ") + st.Render(a.g.s()))
	}

	b.WriteString(box.Render(" ") + edge.Render("▕") + " ")

	return b.String()
}

// matches counts every match of the last search.
func (s *searchView) matches() int {
	n := 0
	for _, f := range s.files {
		n += f.count
	}

	return n
}

func (s *searchView) summary(w int) string {
	n := s.matches()
	text, st := "", dim

	switch {
	case s.err != "":
		text, st = s.err, fg(pal.errc)
	case s.busy:
		text = "Searching…"
	case s.query.Value() == "":
	case n == 0:
		text = "No results found."
	default:
		text = fmt.Sprintf("%s in %s", plural(n, "result"), plural(len(s.files), "file"))
		if s.truncated {
			text += " (limit reached)"
		}
	}

	mode := icTree
	if s.tree {
		mode = icList
	}

	return row(w, nil, []seg{sg("  "+text, st)}, sg(" "+mode.s()+" ", dim), sg(icEllipsis.s()+" ", dim))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return fmt.Sprintf("%d %ss", n, word)
}

func (s *searchView) renderRow(m *Model, r srRow, w int, selected, hovered bool) string {
	bg, base := m.rowColors(viewSearch, selected, hovered)

	indent := strings.Repeat("  ", r.depth)
	if r.file < 0 { // a directory in tree mode
		open := !s.closed["dir:"+r.dir]

		return row(w, bg, []seg{sg(" "+indent+chevron(open), dim), iconSeg(path.Base(r.dir), true, open), sg(r.label, base)},
			badge(strconv.Itoa(r.count)), sg(" ", plain))
	}

	f := s.files[r.file]
	if r.line < 0 {
		name, dir := path.Base(f.path), path.Dir(f.path)
		if dir == "." || s.tree {
			dir = ""
		}

		return row(w, bg, []seg{sg(" "+indent+chevron(!s.closed[f.path]), dim), iconSeg(name, false, false), sg(name, base), sg(" "+dir, dim)},
			badge(strconv.Itoa(f.count)), sg(" ", plain))
	}

	ln := f.lines[r.line]
	with, _ := s.replacing()
	pad := "     " + indent

	return row(w, bg, append([]seg{sg(pad, plain)}, matchSegs(ln, w-len(pad), base, s.replPreview(ln, with))...))
}

// replPreview is what each match of ln becomes, nil when replace is off.
func (s *searchView) replPreview(ln searchLine, with string) func(a, b int) string {
	if _, ok := s.replacing(); !ok || s.re == nil {
		return nil
	}

	ms := s.re.FindAllStringSubmatchIndex(ln.text, -1)

	return func(a, b int) string {
		for _, mi := range ms {
			if mi[0] == a && mi[1] == b {
				out := replText(ln.text, s.re, with, s.regex, mi)
				if s.preserveCase {
					out = keepCase(ln.text[a:b], out)
				}

				return out
			}
		}

		return with
	}
}

// replText is one match's replacement; $1 expands in regex mode.
func replText(line string, re *regexp.Regexp, with string, regex bool, mi []int) string {
	if !regex {
		return with
	}

	return string(re.ExpandString(nil, with, line, mi))
}

// keepCase copies the match's case onto the replacement, VS Code's AB
// toggle: ALL CAPS stays caps, a leading capital stays capitalised.
func keepCase(match, with string) string {
	switch {
	case match == "" || with == "":
		return with
	case match == strings.ToUpper(match) && match != strings.ToLower(match):
		return strings.ToUpper(with)
	case unicode.IsUpper(rune(match[0])):
		return strings.ToUpper(with[:1]) + with[1:]
	}

	return with
}

// replaceLine rewrites every match in line and reports how many.
func replaceLine(line string, re *regexp.Regexp, with string, regex, preserveCase bool) (string, int) {
	if re == nil {
		return line, 0
	}

	var b strings.Builder

	prev, n := 0, 0

	for _, mi := range re.FindAllStringSubmatchIndex(line, -1) {
		rep := replText(line, re, with, regex, mi)
		if preserveCase {
			rep = keepCase(line[mi[0]:mi[1]], rep)
		}

		b.WriteString(line[prev:mi[0]] + rep)
		prev, n = mi[1], n+1
	}

	return b.String() + line[prev:], n
}

// replaceFile rewrites the matching lines of one file; a file that changed
// since the search is refused instead of guessed at.
func replaceFile(file string, f searchFile, re *regexp.Regexp, with string, regex, preserveCase bool) (int, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}

	mode := os.FileMode(0o644)
	if st, err := os.Stat(file); err == nil {
		mode = st.Mode().Perm()
	}

	lines, n := strings.Split(string(raw), "\n"), 0
	for _, ln := range f.lines {
		i := ln.line - 1
		if i < 0 || i >= len(lines) || strings.TrimRight(lines[i], "\r") != ln.text {
			return 0, fmt.Errorf("%s changed since the search", filepath.Base(file))
		}

		out, k := replaceLine(lines[i], re, with, regex, preserveCase)
		lines[i], n = out, n+k
	}

	if n == 0 {
		return 0, nil
	}

	return n, os.WriteFile(file, []byte(strings.Join(lines, "\n")), mode)
}

// replaceRows rewrites one line's matches, a whole file's, or every match,
// then searches again. ponytail: no undo, git is the safety net.
func (s *searchView) replaceRows(m *Model, r srRow, all bool) tea.Cmd {
	with, ok := s.replacing()
	if !ok {
		return flash("open the replace box first (^h)", true)
	}

	var todo []searchFile

	switch {
	case all:
		todo = s.files
	case r.file < 0 || r.file >= len(s.files):
		return nil
	case r.line < 0:
		todo = []searchFile{s.files[r.file]}
	default:
		f := s.files[r.file]
		todo = []searchFile{{path: f.path, lines: []searchLine{f.lines[r.line]}}}
	}

	matches, files := 0, 0

	for _, f := range todo {
		n, err := replaceFile(filepath.Join(s.ws, f.path), f, s.re, with, s.regex, s.preserveCase)
		if err != nil {
			return flash(err.Error(), true)
		}

		matches, files = matches+n, files+b2i(n > 0)
	}

	return tea.Batch(flash(fmt.Sprintf("replaced %s in %s", plural(matches, "match"), plural(files, "file")), false), s.restart(m))
}

// matchSegs is a result line without its indentation, the matches
// highlighted; a match far to the right keeps a little context before it.
func matchSegs(ln searchLine, w int, base lipgloss.Style, repl func(a, b int) string) []seg {
	text := strings.ReplaceAll(ln.text, "\t", " ") // same byte offsets
	start := len(text) - len(strings.TrimLeft(text, " "))

	var segs []seg

	if len(ln.ranges) > 0 && ln.ranges[0][0] > start && ansi.StringWidth(text[start:ln.ranges[0][0]]) > w/3 {
		cut := ln.ranges[0][0]
		for n := 0; n < 12 && cut > start; n++ {
			_, size := utf8.DecodeLastRuneInString(text[:cut])
			cut -= size
		}

		start = cut

		segs = append(segs, sg("…", dim))
	}

	hl := base.Background(pal.matchBg)

	at := start
	for _, r := range ln.ranges {
		a, b := max(r[0], at), r[1]
		if b <= a || b > len(text) {
			continue
		}

		if a > at {
			segs = append(segs, sg(text[at:a], base))
		}

		segs = append(segs, sgOwn(text[a:b], hl.Strikethrough(repl != nil)))
		if repl != nil {
			segs = append(segs, sgOwn(repl(r[0], r[1]), base.Background(pal.diffAddBg)))
		}

		at = b
	}

	return append(segs, sg(text[at:], base))
}

// items are the Search commands for the command palette.
func (s *searchView) items(_ *Model) []item {
	return []item{
		{label: "Focus Query", hint: "4", run: func(m *Model) tea.Cmd { return tea.Batch(m.showView(viewSearch), s.focus()) }},
		{label: "Toggle Match Case", hint: "M-c", run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+c") }},
		{label: "Toggle Whole Word", hint: "M-w", run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+w") }},
		{label: "Toggle Regular Expression", hint: "M-r", run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+r") }},
		{label: "Rerun Search", hint: "^r", run: func(m *Model) tea.Cmd { return s.restart(m) }},
		{label: "Toggle Replace", hint: "^h", run: func(*Model) tea.Cmd { return s.toggleReplace() }},
		{label: "Replace in File", hint: "r", run: func(m *Model) tea.Cmd {
			if rows := s.rows(); s.l.sel >= 0 && s.l.sel < len(rows) {
				return s.replaceRows(m, rows[s.l.sel], false)
			}

			return nil
		}},
		{label: "Replace All…", hint: "R", run: func(m *Model) tea.Cmd { return s.confirmReplaceAll(m) }},
		{label: "Clear Results", hint: "x", run: func(*Model) tea.Cmd { s.query.Reset(); s.clear(); return nil }},
		{label: "Collapse All", hint: "C", run: func(*Model) tea.Cmd { s.collapseAll(); return nil }},
		{label: "Toggle Results as Tree", hint: "t", run: func(*Model) tea.Cmd { s.tree = !s.tree; s.l = list{sel: -1}; return nil }},
		{label: "Toggle Preserve Case", hint: "M-p", run: func(m *Model) tea.Cmd { return s.toggle(m, "alt+p") }},
		{label: "Toggle Files to Include", hint: "^i", run: func(*Model) tea.Cmd { s.showDetails = !s.showDetails; return nil }},
	}
}

func (s *searchView) collapseAll() {
	for _, r := range s.rows() {
		if r.file < 0 {
			s.closed["dir:"+r.dir] = true
		} else {
			s.closed[s.files[r.file].path] = true
		}
	}
}

// open reveals the selected match in the preview; on a file row it folds.
func (s *searchView) open(m *Model, rows []srRow) tea.Cmd {
	if s.l.sel < 0 || s.l.sel >= len(rows) {
		return nil
	}

	r := rows[s.l.sel]
	if r.file < 0 {
		s.closed["dir:"+r.dir] = !s.closed["dir:"+r.dir]
		return nil
	}

	f := s.files[r.file]
	if r.line < 0 {
		s.closed[f.path] = !s.closed[f.path]
		return nil
	}

	ln := f.lines[r.line]
	col, n := 0, 0

	if len(ln.ranges) > 0 {
		a, b := ln.ranges[0][0], ln.ranges[0][1]
		// The preview shows tabs as four spaces.
		col = utf8.RuneCountInString(expandTabs(ln.text[:a]))
		n = utf8.RuneCountInString(expandTabs(ln.text[a:b]))
	}

	return m.openFileAt(filepath.Join(s.ws, f.path), ln.line-1, col, n)
}

func (s *searchView) inputKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		s.blur()
		return nil

	case "enter", "down":
		s.blur()

		if rows := s.rows(); len(rows) > 1 && s.l.sel < 0 {
			s.l.sel = 1 // the first match
		}

		return nil

	case "tab":
		boxes := s.boxes()
		for i, in := range boxes {
			if in.Focused() {
				s.blur()
				return boxes[(i+1)%len(boxes)].Focus()
			}
		}

		return s.focus()

	case "alt+c", "alt+w", "alt+r":
		return s.toggle(m, k.String())
	case "ctrl+h":
		return s.toggleReplace()
	}

	in := &s.query
	switch {
	case s.replace.Focused():
		in = &s.replace
	case s.include.Focused():
		in = &s.include
	}

	before := in.Value()

	var cmd tea.Cmd

	*in, cmd = in.Update(k)
	if in.Value() != before && in != &s.replace {
		return tea.Batch(cmd, s.restart(m))
	}

	return cmd
}

// toggleReplace opens or closes the replace box, VS Code's ctrl+h.
func (s *searchView) toggleReplace() tea.Cmd {
	if s.showReplace = !s.showReplace; !s.showReplace {
		s.replace.Blur()
		return nil
	}

	s.query.Blur()
	s.include.Blur()

	return s.replace.Focus()
}

func (s *searchView) confirmReplaceAll(m *Model) tea.Cmd {
	with, ok := s.replacing()
	if !ok {
		return flash("open the replace box first (^h)", true)
	}

	n := s.matches()
	m.modal = newMenu(fmt.Sprintf("Replace %s with %q?", plural(n, "match"), with), -1, 0,
		item{label: "Replace All", run: func(m *Model) tea.Cmd { return s.replaceRows(m, srRow{}, true) }},
		cancelItem())

	return nil
}

func (s *searchView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := s.rows()
	h, n := max(m.bodyH(viewSearch)-s.headH(), 1), len(rows)

	var r *srRow
	if s.l.sel >= 0 && s.l.sel < n {
		r = &rows[s.l.sel]
	}

	switch k.String() {
	case "up", "k":
		if s.l.sel <= 0 {
			s.l.sel = -1
			return s.focus()
		}

		s.l.move(-1, n, h)

	case "down", "j":
		s.l.move(1, n, h)
	case "g", "home":
		s.l.move(-n, n, h)
	case "G", "end":
		s.l.move(n, n, h)
	case "enter", "space", "o":
		return s.open(m, rows)
	case "l", "right":
		if r != nil && r.line < 0 {
			s.closed[s.files[r.file].path] = false
		}

	case "h", "left":
		if r == nil {
			return nil
		}

		if r.line >= 0 { // to the file header
			s.l.sel = slices.IndexFunc(rows, func(x srRow) bool { return x.file == r.file && x.line < 0 })
			s.l.snap(h)

			return nil
		}

		s.closed[s.files[r.file].path] = true

	case "/", "i", "ctrl+f":
		return s.focus()
	case "ctrl+h":
		return s.toggleReplace()
	case "ctrl+i":
		s.showDetails = !s.showDetails
	case "t":
		s.tree = !s.tree
		s.l = list{sel: -1}

	case "alt+p":
		return s.toggle(m, "alt+p")
	case "r":
		if r != nil {
			return s.replaceRows(m, *r, false)
		}

	case "R":
		return s.confirmReplaceAll(m)
	case "alt+c", "alt+w", "alt+r":
		return s.toggle(m, k.String())
	case "ctrl+r":
		return s.restart(m)
	case "x":
		s.query.Reset()
		s.clear()

	case "C":
		s.collapseAll()
	}

	return nil
}

func (s *searchView) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()
	rows := s.rows()
	hh := s.headH()

	h := max(m.bodyH(viewSearch)-hh, 1)
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		s.l.wheel(wheelDelta(mo), len(rows), h)
		return nil
	}

	if _, click := msg.(tea.MouseClickMsg); !click || mo.Button != tea.MouseLeft {
		return nil
	}

	w := m.colRect(m.colOf(viewSearch)).w

	switch {
	case y == 0: // the blank row above the boxes
		return nil
	case y == 1:
		if x < 2 {
			return s.toggleReplace()
		}

		if a, ok := hit(s.toggles(w, &s.query), x); ok {
			return a.run(m)
		}

		s.blur()

		return s.query.Focus()

	case y == 2 && s.showReplace:
		if a, ok := hit(s.toggles(w, &s.replace), x); ok {
			return a.run(m)
		}

		s.blur()

		return s.replace.Focus()

	case s.showDetails && y == hh-4, s.showDetails && y == hh-3:
		s.blur()
		return s.include.Focus()

	case s.showDetails && y == hh-2:
		s.blur()
		return s.exclude.Focus()

	case y == hh-1: // the summary row: list/tree and the details toggle
		switch {
		case x >= w-3:
			s.showDetails = !s.showDetails
			if !s.showDetails {
				s.include.Blur()
				s.exclude.Blur()
			}

		case x >= w-6:
			s.tree = !s.tree
			s.l = list{sel: -1}
		}

		return nil
	}

	i := s.l.at(y-hh, len(rows))
	if i < 0 {
		return nil
	}

	s.l.sel = i

	return s.open(m, rows)
}
