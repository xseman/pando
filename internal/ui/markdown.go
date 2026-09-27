package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// mdText is a piece of rendered output, styled and as plain text.
type mdText struct{ s, p string }

func mdt(s string, st lipgloss.Style) mdText {
	if s == "" {
		return mdText{}
	}

	return mdText{st.Render(s), s}
}

func (a mdText) add(b mdText) mdText { return mdText{a.s + b.s, a.p + b.p} }

// mdTok is an inline run in one style; hard is a hard line break. padL and
// padR put a cell of the run's own background before its first word and
// after its last, so inline code and the title read as chips, as glow draws
// them; the pad wraps with its word.
type mdTok struct {
	text       string
	st         lipgloss.Style
	hard       bool
	padL, padR bool
}

type mdRenderer struct {
	src           []byte
	w             int
	dark          bool
	styled, plain []string
}

// renderMarkdown lays Markdown out w cells wide like an editor's rendered
// preview, in glow's layout: styled lines and the same lines as plain text,
// mdMargin cells of air on either side. ponytail: images show their alt text
// and HTML stays source.
func renderMarkdown(src string, w int, dark bool) (styled, plain []string) {
	margin := mdText{}
	if w >= 3*mdMargin+20 {
		margin, w = mdText{blank(mdMargin), blank(mdMargin)}, w-mdMargin // the right margin; the prefix takes the left
	}

	r := &mdRenderer{src: []byte(src), w: max(w, 10), dark: dark}
	r.children(mdParser.Parse(text.NewReader(r.src)), margin, margin, false)

	return r.styled, r.plain
}

// mdMargin is the air left and right of rendered Markdown, glow's margin.
const mdMargin = 2

func (r *mdRenderer) line(prefix, content mdText) {
	r.styled = append(r.styled, prefix.s+content.s)
	r.plain = append(r.plain, prefix.p+content.p)
}

// children renders n's blocks: first prefixes the first line, rest every
// other; loose content gets a blank line between blocks.
func (r *mdRenderer) children(n ast.Node, first, rest mdText, tight bool) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		prefix := rest
		if c == n.FirstChild() {
			prefix = first
		} else if !tight {
			r.line(rest, mdText{})
		}

		r.block(c, prefix, rest)
	}
}

func (r *mdRenderer) block(n ast.Node, first, rest mdText) {
	width := max(r.w-ansi.StringWidth(rest.p), 1)
	rule := func() mdText { return mdt(strings.Repeat("─", width), fg(pal.inputBorder)) }

	switch n := n.(type) {
	case *ast.Heading:
		// glow's: the title a chip in the accent, the others marked by their #s.
		if n.Level == 1 {
			toks := r.inline(n, lipgloss.NewStyle().Background(pal.accent).Foreground(pal.buttonFg).Bold(true))
			if len(toks) > 0 {
				toks[0].padL, toks[len(toks)-1].padR = true, true
			}

			r.wrap(toks, first, rest, width)

			break
		}

		marks := mdTok{text: strings.Repeat("#", n.Level) + " ", st: accent}
		r.wrap(append([]mdTok{marks}, r.inline(n, accent)...), first, rest, width)

	case *ast.Paragraph, *ast.TextBlock:
		r.wrap(r.inline(n, plain), first, rest, width)
	case *ast.ThematicBreak:
		r.line(first, rule())
	case *ast.FencedCodeBlock:
		r.code(n.Lines(), string(n.Language(r.src)), first, rest, width)
	case *ast.CodeBlock:
		r.code(n.Lines(), "", first, rest, width)
	case *ast.HTMLBlock:
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			r.line(first, mdt(strings.TrimRight(string(seg.Value(r.src)), "\r\n"), dim))
			first = rest
		}

	case *ast.Blockquote:
		bar := mdt("▎ ", fg(pal.inputBorder))
		r.children(n, first.add(bar), rest.add(bar), false)

	case *ast.List:
		depth := 0

		for a := n.Parent(); a != nil; a = a.Parent() {
			if _, ok := a.(*ast.List); ok {
				depth++
			}
		}

		num := n.Start
		for it := n.FirstChild(); it != nil; it = it.NextSibling() {
			marker := []string{"• ", "◦ ", "▪ "}[depth%3]
			if n.IsOrdered() {
				marker = fmt.Sprintf("%d%c ", num, n.Marker)
				num++
			}

			prefix := rest
			if it == n.FirstChild() {
				prefix = first
			} else if !n.IsTight {
				r.line(rest, mdText{})
			}

			pad := strings.Repeat(" ", ansi.StringWidth(marker))

			prefix = prefix.add(mdt(marker, fg(pal.headerAccent)))
			if it.FirstChild() == nil {
				r.line(prefix, mdText{})
				continue
			}

			r.children(it, prefix, rest.add(mdText{pad, pad}), n.IsTight)
		}

	case *east.Table:
		r.table(n, first, rest, width)
	default:
		r.children(n, first, rest, false)
	}
}

// inline flattens n's inline content into styled runs.
func (r *mdRenderer) inline(n ast.Node, st lipgloss.Style) []mdTok {
	var (
		out  []mdTok
		walk func(n ast.Node, st lipgloss.Style)
	)

	walk = func(n ast.Node, st lipgloss.Style) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				out = append(out, mdTok{text: string(c.Segment.Value(r.src)), st: st})
				switch {
				case c.HardLineBreak():
					out = append(out, mdTok{hard: true})
				case c.SoftLineBreak():
					out = append(out, mdTok{text: " ", st: st})
				}

			case *ast.String:
				out = append(out, mdTok{text: string(c.Value), st: st})
			case *ast.CodeSpan:
				var b strings.Builder

				for g := c.FirstChild(); g != nil; g = g.NextSibling() {
					if t, ok := g.(*ast.Text); ok {
						b.Write(t.Segment.Value(r.src))
					}
				}

				out = append(out, mdTok{text: b.String(), st: st.Background(pal.mdCodeBg).Foreground(pal.mdCode), padL: true, padR: true})

			case *ast.Emphasis:
				if c.Level >= 2 {
					walk(c, st.Bold(true))
				} else {
					walk(c, st.Italic(true))
				}

			case *east.Strikethrough:
				walk(c, st.Strikethrough(true))
			case *ast.Link:
				walk(c, st.Foreground(pal.headerAccent).Underline(true))
			case *ast.AutoLink:
				out = append(out, mdTok{text: string(c.Label(r.src)), st: st.Foreground(pal.headerAccent).Underline(true)})
			case *ast.Image:
				var alt strings.Builder
				for _, t := range r.inline(c, st) {
					alt.WriteString(t.text)
				}

				out = append(out, mdTok{text: "[image: " + alt.String() + "]", st: dim})

			case *east.TaskCheckBox:
				box := "☐ "
				if c.IsChecked {
					box = "☑ "
				}

				out = append(out, mdTok{text: box, st: st})

			case *ast.RawHTML:
				for i := range c.Segments.Len() {
					seg := c.Segments.At(i)
					out = append(out, mdTok{text: string(seg.Value(r.src)), st: dim})
				}

			default:
				walk(c, st)
			}
		}
	}
	walk(n, st)

	return out
}

// wrap flows inline runs into lines at most width cells wide, breaking
// between words; a word longer than a line is split.
func (r *mdRenderer) wrap(toks []mdTok, first, rest mdText, width int) {
	var cur mdText

	used, space := 0, false
	prefix := first
	flush := func() {
		r.line(prefix, cur)
		prefix, cur, used = rest, mdText{}, 0
	}

	for _, t := range toks {
		if t.hard {
			flush()

			space = false

			continue
		}

		s := expandTabs(t.text)
		if strings.HasPrefix(s, " ") {
			space = true
		}

		fields := strings.Fields(s)
		if len(fields) > 0 && t.padL {
			fields[0] = " " + fields[0]
		}

		if len(fields) > 0 && t.padR {
			fields[len(fields)-1] += " "
		}

		for i, word := range fields {
			ww := ansi.StringWidth(word)

			sp := b2i((space || i > 0) && used > 0)
			if used > 0 && used+sp+ww > width {
				flush()

				sp = 0
			}

			if sp == 1 {
				gap := t.st
				if i == 0 && t.padL { // the space before a chip is not the chip's
					gap = plain
				}

				cur, used = cur.add(mdt(" ", gap)), used+1
			}

			for used+ww > width {
				part := ansi.Truncate(word, width-used, "")
				if part == "" {
					break
				}

				cur, word = cur.add(mdt(part, t.st)), word[len(part):]
				ww = ansi.StringWidth(word)

				flush()
			}

			cur, used = cur.add(mdt(word, t.st)), used+ww
			space = false
		}

		if strings.HasSuffix(s, " ") {
			space = true
		}
	}

	if used > 0 {
		flush()
	}
}

// code renders a code block on the input background, highlighted when its
// language is known.
func (r *mdRenderer) code(lines *text.Segments, lang string, first, rest mdText, width int) {
	src := make([]string, lines.Len())
	for i := range src {
		seg := lines.At(i)
		src[i] = strings.TrimRight(expandTabs(string(seg.Value(r.src))), "\r\n")
	}

	lexer := lexers.Get(lang)
	if lang == "" {
		lexer = nil
	}

	toks := tokenLines(lexer, src, r.dark)

	bg := lipgloss.NewStyle().Background(pal.mdCodeBg)
	for i, l := range src {
		body, used := paint(toks[i], pal.mdCodeBg, nil, [2]int{}, false), ansi.StringWidth(l)+1
		if used > width {
			body, l, used = ansi.Truncate(body, width-1, ""), ansi.Truncate(l, width-1, ""), width
		}

		r.line(first, mdText{bg.Render(" ") + body + bg.Render(blank(width-used)), " " + l})
		first = rest
	}
}

// table aligns cells in columns, shrinking the widest ones to fit; a cell
// wider than its column wraps onto more lines of its row, as glow's do.
func (r *mdRenderer) table(t *east.Table, first, rest mdText, width int) {
	var rows [][][]mdTok

	cols := len(t.Alignments)
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		st := plain
		if _, ok := row.(*east.TableHeader); ok {
			st = bold
		}

		var cells [][]mdTok
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, r.inline(c, st))
		}

		rows, cols = append(rows, cells), max(cols, len(cells))
	}

	widths := make([]int, cols)

	for _, row := range rows {
		for i, c := range row {
			for _, l := range r.cell(c, 1<<20).plain { // on one line, as wide as it wants
				widths[i] = max(widths[i], ansi.StringWidth(l))
			}
		}
	}

	for {
		total, widest := 3*(cols-1), 0
		for i, w := range widths {
			total += w
			if w > widths[widest] {
				widest = i
			}
		}

		if total <= width || widths[widest] <= 1 {
			break
		}

		widths[widest]--
	}

	sep := mdt(" │ ", fg(pal.inputBorder))

	for ri, row := range rows {
		cells, h := make([]*mdRenderer, len(widths)), 1
		for i, w := range widths {
			var toks []mdTok
			if i < len(row) {
				toks = row[i]
			}

			cells[i] = r.cell(toks, max(w, 1))
			h = max(h, len(cells[i].plain))
		}

		for y := range h {
			var line mdText

			for i, w := range widths {
				if i > 0 {
					line = line.add(sep)
				}

				var cell mdText
				if y < len(cells[i].plain) {
					cell = mdText{cells[i].styled[y], cells[i].plain[y]}
				}

				pad, left := max(w-ansi.StringWidth(cell.p), 0), 0

				if i < len(t.Alignments) {
					switch t.Alignments[i] {
					case east.AlignRight:
						left = pad
					case east.AlignCenter:
						left = pad / 2
					}
				}

				line = line.add(mdText{blank(left), blank(left)}).add(cell).add(mdText{blank(pad - left), blank(pad - left)})
			}

			r.line(first, line)
			first = rest
		}

		if ri == 0 {
			parts := make([]string, len(widths))
			for i, w := range widths {
				parts[i] = strings.Repeat("─", w)
			}

			r.line(rest, mdt(strings.Join(parts, "─┼─"), fg(pal.inputBorder)))
		}
	}
}

// cell lays a table cell's runs out w cells wide.
func (r *mdRenderer) cell(toks []mdTok, w int) *mdRenderer {
	c := &mdRenderer{src: r.src, w: w, dark: r.dark}
	c.wrap(toks, mdText{}, mdText{}, w)

	return c
}
