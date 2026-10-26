package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// commentSyntax is how a language comments out a line: its line token, or for
// a language without one, the pair a block opens and closes with.
type commentSyntax struct{ line, open, close string }

// commentTokens is the comment syntax by chroma lexer name, so a file the
// preview colours toggles too, and by file name or extension where chroma has
// no lexer or the wrong one: go.mod is AMPL's *.mod to it, and an extension
// several lexers claim goes to the first, .fs to Forth, .m to Mathematica,
// Helm's .tpl to Smarty.
// The tokens, and the language an extension means, are VS Code's where it
// ships the language; .s takes the one comment every GNU as target reads.
var commentTokens = func() map[string]commentSyntax {
	groups := map[commentSyntax]string{
		{line: "//"}: `ActionScript, ActionScript 3, AL, Alloy, ANTLR, ArangoDB AQL, Arduino, Ballerina, Beef,
			Bicep, C, C#, C++, C3, Cassandra CQL, Ceylon, cfstatement, ChaiScript, Chapel, CUE, D, Dart, Dax,
			Devicetree, Dylan, FSharp, GAS, Gleam, GLSL, Go, Groovy, Hare, Haxe, HLSL, HolyC, Igor, Io, Java,
			JavaScript, JSON, Jsonnet, KDL, Kotlin, Lateralus, lox, Metal, microcad, MLIR, Modelica, MonkeyC,
			MoonBit, Objective-C, ObjectPascal, Odin, OnesEnterprise, OpenEdge ABL, OpenSCAD, PHP, PHTML, Pony,
			POVRay, PowerQuery, Promela, Protocol Buffer, PSL, QML, react, ReasonML, Ring, RPGLE, Rust, Sass,
			Scala, Scilab, SCSS, Solidity, SourcePawn, Spade, Stylus, Swift, systemverilog, TableGen, Templ,
			Thrift, TradingView, TypeScript, TypoScript, Typst, ucode, V, V shell, Vala, verilog,
			WebGPU Shading Language, Whiley, YANG, Zed, Zig,
			go.mod, go.work, Jenkinsfile, .adoc, .cu, .cuh, .fs, .fsx, .less, .m, .mm, .sbt, .v`,
		{line: "#"}: `AMPL, ApacheConf, Awk, Bash, BQN, Caddyfile, Caddyfile Directives, Cap'n Proto, CFEngine3,
			CMake, CoffeeScript, Crystal, Cython, Desktop file, Docker, Elixir, Fish, GDScript, GDScript3,
			Gettext, Gherkin, Gnuplot, GraphQL, HCL, HLB, ISCdhcpd, Janet, Julia, Jungle, Kakoune,
			Lighttpd configuration file, Makefile, MCFunction, Meson, Mojo, Myghty, Nginx configuration file,
			Nim, Nix, Nu, Org Mode, PacmanConf, Perl, PkgConfig, PowerShell, PromQL, properties,
			Protocol Buffer Text Format, PRQL, Puppet, Python, Python 2, R, Ragel, Raku, Rego, RPMSpec, Ruby,
			Sed, Sieve, Smali, SPARQL, SquidConf, SYSTEMD, Tcl, Tcsh, Termcap, Terminfo, Terraform, TOML,
			Turtle, VHS, WDTE, Xorg, YAML, YAML+Jinja,
			.gitignore, .dockerignore, .npmignore, .prettierignore, .gitattributes, .gitmodules, .npmrc,
			.bazelrc, .profile, CODEOWNERS, requirements.txt, Brewfile, Podfile, Procfile, crontab, hosts,
			fstab, sshd_config, ssh_config, COMMIT_EDITMSG, MERGE_MSG, git-rebase-todo, .conf, .dockerfile`,
		{line: "--"}: `Ada, Agda, AppleScript, ATL, Elm, Haskell, Idris, Lean4, Lua, Luau, Materialize SQL dialect,
			MoonScript, MySQL, Pig, PL/pgSQL, PostgreSQL SQL dialect, SQL, Transact-SQL, VHDL,
			.cabal, .psql, .purs`,
		{line: ";;"}: `Clojure, Common Lisp, EmacsLisp, Fennel, Hy, Racket, Scheme, WebAssembly Text Format,
			.cljc, .cljs`,
		{line: ";"}: `ABNF, ArmAsm, Arturo, AutoHotkey, AutoIt, BlitzBasic, dns, INI, LLVM, Markless,
			MorrowindScript, NASM, NSIS, reg, RGBDS Assembly, scdoc, stas, TASM, Z80 Assembly`,
		{line: "%"}:               `BibTeX, Erlang, LilyPond, Matlab, MiniZinc, Octave, PostScript, Prolog, TeX, Turing, .cls, .sty`,
		{line: "'"}:               `QBasic, VB.net, .vbs`,
		{line: "!"}:               `Factor, Fortran, FortranFixed`,
		{line: `"`}:               `ABAP, VimL`,
		{line: `\`}:               `Forth`,
		{line: "@REM"}:            `Batchfile`,
		{line: "⍝"}:               `APL`,
		{line: "NB."}:             `J`,
		{line: "*>"}:              `COBOL`,
		{line: ".."}:              `reStructuredText`,
		{line: `.\"`}:             `Groff`,
		{line: "##"}:              `Cheetah, Genshi Text, Mako`,
		{line: "//-"}:             `.jade, .pug`,
		{line: "-#"}:              `.haml`,
		{line: "/"}:               `.slim`,
		{open: "/*", close: "*/"}: `CSS, JSONata, Rexx, SAS, .s`,
		{open: "<!--", close: "-->"}: `Angular2, DTD, ERB, Genshi, Genshi HTML, HTML, markdown, Mason, Svelte,
			vue, XML, .astro, .plist, .xaml`,
		{open: "(*", close: "*)"}:      `Coq, EBNF, Mathematica, Modula-2, OCaml, Standard ML`,
		{open: "{#", close: "#}"}:      `Django/Jinja, Twig, .j2, .jinja, .jinja2, .njk`,
		{open: "{{!--", close: "--}}"}: `Handlebars`,
		{open: "{{!", close: "}}"}:     `.mustache`,
		{open: "{*", close: "*}"}:      `Smarty`,
		{open: "{{/*", close: "*/}}"}:  `Go HTML Template, Go Template, Go Text Template, .tmpl, .tpl`,
		{open: `"`, close: `"`}:        `Smalltalk`,
	}
	m := map[string]commentSyntax{}

	for c, names := range groups {
		for n := range strings.SplitSeq(names, ",") {
			m[strings.TrimSpace(n)] = c
		}
	}

	return m
}()

// commentOf is the comment syntax of a file: by its name or extension, else by
// the language the preview colours it as. ok is false for plain text.
// ponytail: a script known only by its #! line gets #, a node one too.
func commentOf(path, text string) (c commentSyntax, ok bool) {
	base := filepath.Base(path)
	if c, ok := commentTokens[base]; ok {
		return c, true
	}

	if c, ok := commentTokens[strings.ToLower(filepath.Ext(base))]; ok {
		return c, true
	}

	lx := matchLexer(base)
	if lx == nil && path != "" && text != "" { // the guess the preview colours by
		lx = lexers.Analyse(text)
	}

	if lx != nil {
		c, ok = commentTokens[lx.Config().Name]
		return c, ok
	}

	if strings.HasPrefix(text, "#!") {
		return commentSyntax{line: "#"}, true
	}

	return c, false
}

// commentIn is the comment syntax at a line of a markup file: inside a
// <script> JavaScript's, a <style> CSS's, a Markdown code fence its
// language's, as VS Code and Zed take the language under the cursor and not
// the file's.
func commentIn(c commentSyntax, text string, line int) commentSyntax {
	if c.open != "<!--" {
		return c
	}

	tag, fence, inFence := "", "", false

	for l := range strings.SplitSeq(text, "\n") {
		if line == 0 { // the lines before it only
			break
		}

		line--

		if t := strings.TrimLeft(l, " \t"); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence, fence = !inFence, ""
			if f := strings.Fields(strings.TrimLeft(t, "`~")); inFence && len(f) > 0 {
				fence = f[0]
			}

			continue
		}

		if inFence { // a fence's <script> is its text
			continue
		}

		low := strings.ToLower(l)
		script, style := strings.LastIndex(low, "<script"), strings.LastIndex(low, "<style")
		end := max(strings.LastIndex(low, "</script"), strings.LastIndex(low, "</style"))

		switch {
		case script > end && script > style:
			tag = "script"
		case style > end:
			tag = "style"
		case end >= 0:
			tag = ""
		}
	}

	switch {
	case inFence:
		if lx := langLexer(fence); lx != nil {
			if f, ok := commentTokens[lx.Config().Name]; ok {
				return f
			}
		}

	case tag == "script":
		return commentSyntax{line: "//"}
	case tag == "style":
		return commentSyntax{open: "/*", close: "*/"}
	}

	return c
}

// commentEdit is a line after a toggle, and where it changed: by runes from a
// column, which a cursor past that column follows.
type commentEdit struct {
	text   string
	at, by int
}

// toggleComments comments the lines out, or back in when every line with text
// is commented already, as VS Code's ⌃/ does: the line token goes at the
// shallowest indent, past blank lines. A language without one gets a block
// from the first line's text to the last line's end.
func toggleComments(lines []string, c commentSyntax) []commentEdit {
	out := make([]commentEdit, len(lines))
	for i, l := range lines {
		out[i].text = l
	}

	if c.line == "" {
		return toggleBlock(out, c)
	}

	some := slices.ContainsFunc(lines, hasText) // else the blank lines take part too
	on, ind := false, -1

	for _, l := range lines {
		if some && !hasText(l) {
			continue
		}

		n := indentLen(l)
		if ind < 0 || n < ind {
			ind = n
		}

		on = on || !strings.HasPrefix(l[n:], c.line)
	}

	for i, l := range lines {
		n := indentLen(l)

		switch {
		case some && !hasText(l):
		case on:
			out[i] = commentEdit{l[:ind] + c.line + " " + l[ind:], ind, utf8.RuneCountInString(c.line) + 1}
		default:
			rest := strings.TrimPrefix(strings.TrimPrefix(l[n:], c.line), " ")
			out[i] = commentEdit{l[:n] + rest, n, utf8.RuneCountInString(rest) - utf8.RuneCountInString(l[n:])}
		}
	}

	return out
}

// toggleBlock is toggleComments with a block: it opens at the first line's
// indent and closes at the last line's end. On one line it takes out the
// line's first comment instead, wherever it sits, as VS Code does: CSS and
// HTML comments do not nest, so a line ending in one would break.
func toggleBlock(out []commentEdit, c commentSyntax) []commentEdit {
	first, last := &out[0], &out[len(out)-1]
	n := indentLen(first.text)
	end := strings.TrimRight(last.text, " \t")

	i, j := -1, -1 // where the block to take out opens on the first line and closes on the last
	if first == last {
		if i = strings.Index(first.text, c.open); i >= 0 {
			if k := strings.Index(first.text[i+len(c.open):], c.close); k >= 0 {
				j = i + len(c.open) + k
			}
		}
	} else if strings.HasPrefix(first.text[n:], c.open) && strings.HasSuffix(end, c.close) {
		i, j = n, len(end)-len(c.close)
	}

	if i < 0 || j < 0 {
		*first = commentEdit{first.text[:n] + c.open + " " + first.text[n:], n, utf8.RuneCountInString(c.open) + 1}
		last.text += " " + c.close

		return out
	}

	// The close goes first: on one line it sits after the open, whose
	// offsets stay true.
	l, cs := last.text, j
	if cs > 0 && l[cs-1] == ' ' && (first != last || cs > i+len(c.open)) {
		cs--
	}

	last.text = l[:cs] + l[j+len(c.close):]

	l, oe := first.text, i+len(c.open)
	if oe < len(l) && l[oe] == ' ' && (first != last || oe < cs) {
		oe++
	}

	*first = commentEdit{l[:i] + l[oe:], utf8.RuneCountInString(l[:i]), -utf8.RuneCountInString(l[i:oe])}

	return out
}

func hasText(l string) bool { return strings.TrimSpace(l) != "" }

func indentLen(l string) int { return len(l) - len(strings.TrimLeft(l, " \t")) }

// toggleComment comments out every line the selection touches, or back in,
// VS Code's ⌃/. The selection stays on the lines, so a second ⌃/ takes them
// back.
func (p *preview) toggleComment(m *Model) tea.Cmd {
	text := p.text()

	c, ok := commentOf(p.path, text)
	if !ok {
		return flash("no comment syntax for "+filepath.Base(p.path), false)
	}

	a, z, sel := p.selection()
	if !sel {
		a, z = p.at(), p.at()
	} else if z.col == 0 && z.line > a.line { // as changedLines: a selection ending at column 0 leaves that line out
		z.line--
	}

	lines := make([]string, 0, z.line-a.line+1)
	for i := a.line; i <= z.line; i++ {
		lines = append(lines, string(p.buf.line(i)))
	}

	out := toggleComments(lines, commentIn(c, text, a.line))
	lines = lines[:0]

	for _, e := range out {
		lines = append(lines, e.text)
	}

	// follow moves a position with its text, in buffer coordinates: past its
	// line's change, and the cursor alone from the change on.
	follow := func(q pos, lone bool) pos {
		q = p.rawPos(q)
		if q.line < a.line || q.line > z.line {
			return q
		}

		e := out[q.line-a.line]
		if q.col > e.at || lone && q.col == e.at {
			q.col = max(e.at, q.col+e.by)
		}

		q.col = min(q.col, utf8.RuneCountInString(e.text))

		return q
	}

	cur, anchor := follow(p.at(), !sel), p.anchor
	if sel {
		k := follow(*anchor, false)
		anchor = &k
	}

	cmd := p.edit(m, pos{a.line, 0}, pos{z.line, p.lineLen(z.line)}, strings.Join(lines, "\n"))
	if sel { // edit clears the anchor
		k := p.dispPos(*anchor)
		p.anchor = &k
	}

	p.setCursor(m, p.dispPos(cur))

	return cmd
}
