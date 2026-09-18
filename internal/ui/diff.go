package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// lineMeta describes one preview row of a diff.
type lineMeta struct {
	a, b int  // old and new line numbers, 0 = none
	kind byte // c context, a added, d deleted, h hunk gap, p plain text, f file name
}

type diffLine struct {
	lineMeta
	text string
}

var diffMeta = []string{
	"index ", "--- ", "+++ ", "old mode", "new mode", "similarity", "rename ", "copy ",
	"new file mode", "deleted file mode", "\\",
}

// parseDiff turns `git diff` or `git show` output into rows: headers are
// dropped, text outside hunks (a commit message, the stat) stays plain, and
// a file-name row starts each file when there are several.
func parseDiff(raw string) []diffLine {
	var out []diffLine

	oldNo, newNo, files := 0, 0, 0
	inHunk, seenHunk := false, false

	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			inHunk, seenHunk = false, false
			files++

			name := line
			if i := strings.LastIndex(line, " b/"); i >= 0 {
				name = line[i+3:]
			}

			out = append(out, diffLine{lineMeta{kind: 'f'}, name})

			continue
		}

		if !inHunk && slices.ContainsFunc(diffMeta, func(p string) bool { return strings.HasPrefix(line, p) }) {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			for _, f := range strings.Fields(line)[1:] {
				n, _ := strconv.Atoi(strings.SplitN(f[1:], ",", 2)[0])
				switch f[0] {
				case '-':
					oldNo = n
				case '+':
					newNo = n
				}
			}

			if seenHunk {
				out = append(out, diffLine{lineMeta{kind: 'h'}, ""})
			}

			seenHunk, inHunk = true, true

			continue
		}

		switch {
		case !inHunk:
			out = append(out, diffLine{lineMeta{kind: 'p'}, line})
		case strings.HasPrefix(line, "\\"):
		case strings.HasPrefix(line, "-"):
			out = append(out, diffLine{lineMeta{a: oldNo, kind: 'd'}, line[1:]})
			oldNo++

		case strings.HasPrefix(line, "+"):
			out = append(out, diffLine{lineMeta{b: newNo, kind: 'a'}, line[1:]})
			newNo++

		case strings.HasPrefix(line, " ") || line == "": // some tools trim blank context lines
			out = append(out, diffLine{lineMeta{a: oldNo, b: newNo, kind: 'c'}, strings.TrimPrefix(line, " ")})
			oldNo++
			newNo++

		default: // the next commit of `git log -p`
			inHunk = false

			out = append(out, diffLine{lineMeta{kind: 'p'}, line})
		}
	}

	if files <= 1 { // one file is already named in the header
		kept := out[:0]
		for _, l := range out {
			if l.kind != 'f' {
				kept = append(kept, l)
			}
		}

		out = kept
	}

	return out
}

// wordRanges pairs each run of deletions with the additions right after it
// and returns, per row, the rune range that differs (common prefix and suffix
// excluded). Lines that share nothing get no range.
func wordRanges(lines []diffLine) map[int][2]int {
	ranges := map[int][2]int{}

	for i := 0; i < len(lines); {
		del := i
		for i < len(lines) && lines[i].kind == 'd' {
			i++
		}

		add := i
		for i < len(lines) && lines[i].kind == 'a' {
			i++
		}

		if del == add || add == i {
			if i == del {
				i++
			}

			continue
		}

		for k := range min(add-del, i-add) {
			o, n := []rune(lines[del+k].text), []rune(lines[add+k].text)

			pre := 0
			for pre < len(o) && pre < len(n) && o[pre] == n[pre] {
				pre++
			}

			suf := 0
			for suf < len(o)-pre && suf < len(n)-pre && o[len(o)-1-suf] == n[len(n)-1-suf] {
				suf++
			}

			if pre+suf == 0 {
				continue
			}

			ranges[del+k] = [2]int{pre, len(o) - suf}
			ranges[add+k] = [2]int{pre, len(n) - suf}
		}
	}

	return ranges
}

type tok struct {
	text string
	e    chroma.StyleEntry
}

// tokenLines highlights lines as one text, so multi-line constructs keep
// their state, and splits the tokens back per line.
func tokenLines(lexer chroma.Lexer, lines []string, dark bool) [][]tok {
	out := make([][]tok, len(lines))
	for i, l := range lines {
		out[i] = []tok{{text: l}}
	}

	if lexer == nil || len(lines) == 0 {
		return out
	}

	style := styles.Get("github")
	if dark {
		style = styles.Get("github-dark")
	}

	it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		return out
	}

	hl := make([][]tok, len(lines))
	i := 0

	for t := it(); t != chroma.EOF; t = it() {
		e := style.Get(t.Type)
		for j, part := range strings.Split(t.Value, "\n") {
			if j > 0 {
				i++
			}

			if part != "" && i < len(hl) {
				hl[i] = append(hl[i], tok{part, e})
			}
		}
	}

	for i := range out {
		out[i] = hl[i]
	}

	return out
}

// paint renders tokens over a row tint, with the word range on a darker tint.
func paint(toks []tok, bg, wordBg color.Color, word [2]int, hasWord bool) string {
	var b strings.Builder

	pos := 0

	for _, t := range toks {
		rs := []rune(t.text)
		cuts := []int{0, len(rs)}

		if hasWord {
			for _, c := range []int{word[0] - pos, word[1] - pos} {
				if c > 0 && c < len(rs) {
					cuts = append(cuts, c)
				}
			}

			slices.Sort(cuts)
		}

		for k := 0; k+1 < len(cuts); k++ {
			a, z := cuts[k], cuts[k+1]
			if a == z {
				continue
			}

			st := lipgloss.NewStyle()
			if t.e.Colour.IsSet() {
				st = st.Foreground(lipgloss.Color(t.e.Colour.String()))
			}

			if t.e.Bold == chroma.Yes {
				st = st.Bold(true)
			}

			if t.e.Italic == chroma.Yes {
				st = st.Italic(true)
			}

			switch {
			case hasWord && pos+a >= word[0] && pos+a < word[1]:
				st = st.Background(wordBg)
			case bg != nil:
				st = st.Background(bg)
			}

			b.WriteString(st.Render(string(rs[a:z])))
		}

		pos += len(rs)
	}

	return b.String()
}

// renderDiff styles parsed rows VS Code style: syntax colors per file over
// red/green row tints, with a darker tint on the changed words.
func renderDiff(lines []diffLine, path string, dark bool) (styled, plainLines []string, meta []lineMeta, numW int) {
	styled, plainLines, meta = make([]string, len(lines)), make([]string, len(lines)), make([]lineMeta, len(lines))
	maxNo := 0

	for i, l := range lines {
		plainLines[i], meta[i] = l.text, l.lineMeta
		maxNo = max(maxNo, l.a, l.b)
	}

	numW = max(len(strconv.Itoa(maxNo)), 2)
	ranges := wordRanges(lines)
	section := func(start, end int, file string) {
		lexer := lexers.Match(filepath.Base(file))
		// Two token streams approximate the old and new file, so a string or
		// comment opened on a context line colors both sides correctly.
		var (
			oldText, newText []string
			oldAt, newAt     []int
		)

		for i := start; i < end; i++ {
			switch lines[i].kind {
			case 'c':
				oldText, newText = append(oldText, lines[i].text), append(newText, lines[i].text)
				oldAt, newAt = append(oldAt, i), append(newAt, i)

			case 'd':
				oldText, oldAt = append(oldText, lines[i].text), append(oldAt, i)
			case 'a':
				newText, newAt = append(newText, lines[i].text), append(newAt, i)
			}
		}

		oldToks, newToks := tokenLines(lexer, oldText, dark), tokenLines(lexer, newText, dark)

		for k, i := range oldAt {
			if lines[i].kind == 'd' {
				r, ok := ranges[i]
				styled[i] = paint(oldToks[k], pal.diffDelBg, pal.diffDelWordBg, r, ok)
			}
		}

		for k, i := range newAt {
			switch lines[i].kind {
			case 'c':
				styled[i] = paint(newToks[k], nil, nil, [2]int{}, false)
			case 'a':
				r, ok := ranges[i]
				styled[i] = paint(newToks[k], pal.diffAddBg, pal.diffAddWordBg, r, ok)
			}
		}

		for i := start; i < end; i++ {
			switch l := lines[i]; l.kind {
			case 'p':
				if strings.HasPrefix(l.text, "commit ") {
					styled[i] = fg(pal.warn).Render(l.text)
				} else {
					styled[i] = dim.Render(l.text)
				}

			case 'f':
				styled[i] = bold.Render(l.text)
			}
		}
	}
	start, file := 0, path

	for i, l := range lines {
		if l.kind == 'f' {
			section(start, i, file)
			start, file = i, l.text
		}
	}

	section(start, len(lines), file)

	return styled, plainLines, meta, numW
}

// diffGutter is `old new ±` for row i, painted with the row's tint.
func diffGutter(m lineMeta, numW int, bg color.Color) string {
	st := dim
	if bg != nil {
		st = st.Background(bg)
	}

	if m.kind == 'h' {
		return st.Render(blank(2*numW+2) + "⋯ ")
	}

	num := func(n int) string {
		if n == 0 {
			return blank(numW)
		}

		return fmt.Sprintf("%*d", numW, n)
	}
	mark, mc := " ", pal.diffAddMark

	switch m.kind {
	case 'a':
		mark = "+"
	case 'd':
		mark, mc = "-", pal.diffDelMark
	case 'p', 'f':
		return st.Render(blank(2*numW + 4))
	}

	ms := fg(mc)
	if bg != nil {
		ms = ms.Background(bg)
	}

	return st.Render(num(m.a)+" "+num(m.b)+" ") + ms.Render(mark+" ")
}

func diffBg(kind byte) color.Color {
	switch kind {
	case 'a':
		return pal.diffAddBg
	case 'd':
		return pal.diffDelBg
	}

	return nil
}
