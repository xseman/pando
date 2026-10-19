package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// lightPlus is VS Code's Light+ code colors as a chroma style: control flow
// magenta, other keywords blue, types teal, functions brown, names dark
// blue, strings dark red, comments green. No background and no text color:
// the terminal's show through.
var lightPlus = chroma.MustNewStyle("light-plus", chroma.StyleEntries{
	chroma.Comment:               "#008000",
	chroma.CommentPreproc:        "#0000ff",
	chroma.Keyword:               "#0000ff",
	chroma.KeywordPseudo:         "#af00db", // keyword.control, which refine sorts out
	chroma.KeywordNamespace:      "#af00db",
	chroma.KeywordType:           "#267f99",
	chroma.Name:                  "#001080",
	chroma.NameFunction:          "#795e26",
	chroma.NameDecorator:         "#795e26",
	chroma.NameClass:             "#267f99",
	chroma.NameNamespace:         "#267f99",
	chroma.NameBuiltin:           "#267f99",
	chroma.NameException:         "#267f99",
	chroma.NameBuiltinPseudo:     "#0000ff",
	chroma.NameConstant:          "#0070c1",
	chroma.NameTag:               "#800000",
	chroma.NameAttribute:         "#e50000",
	chroma.LiteralString:         "#a31515",
	chroma.LiteralStringRegex:    "#811f3f",
	chroma.LiteralStringEscape:   "#ee0000",
	chroma.LiteralStringInterpol: "#0000ff",
	chroma.LiteralNumber:         "#098658",
	chroma.OperatorWord:          "#0000ff",
	chroma.GenericHeading:        "bold #800000",
	chroma.GenericSubheading:     "bold #800000",
	chroma.GenericStrong:         "bold",
	chroma.GenericEmph:           "italic",
	chroma.GenericInserted:       "#098658",
	chroma.GenericDeleted:        "#a31515",
	chroma.Error:                 "#cd3131",
})

// syntaxStyle is the code colors for a dark or a light terminal.
func syntaxStyle(dark bool) *chroma.Style {
	if dark {
		return styles.Get("github-dark")
	}

	return lightPlus
}

// controlWords are the keywords VS Code colors as keyword.control: what
// changes where the code goes, and imports.
var controlWords = map[string]bool{
	"return": true, "if": true, "else": true, "elif": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "default": true, "break": true, "continue": true, "try": true,
	"catch": true, "except": true, "finally": true, "throw": true, "raise": true, "import": true,
	"export": true, "from": true, "yield": true, "await": true, "goto": true, "select": true,
	"defer": true, "go": true, "range": true, "fallthrough": true, "with": true, "match": true,
}

// jsFamily are the lexers that call every identifier NameOther.
var jsFamily = map[string]bool{"JavaScript": true, "TypeScript": true, "react": true}

// refine retypes tokens the way VS Code's grammars tell them apart, where a
// lexer lumps them together: control keywords become KeywordPseudo, and in
// JavaScript and TypeScript a name before ( a function, a capitalized one a
// class, an all-caps one a constant, and from before a string a keyword. It
// changes types only: the text and the number of tokens stay.
func refine(lexer chroma.Lexer, it chroma.Iterator) chroma.Iterator {
	var toks []chroma.Token
	for t := it(); t != chroma.EOF; t = it() {
		toks = append(toks, t)
	}

	js := jsFamily[lexer.Config().Name]

	for i, t := range toks {
		switch {
		case t.Type.InCategory(chroma.Keyword) && t.Type != chroma.KeywordType && t.Type != chroma.KeywordConstant &&
			t.Type != chroma.KeywordNamespace:
			switch {
			case controlWords[t.Value]:
				toks[i].Type = chroma.KeywordPseudo
			case t.Type == chroma.KeywordPseudo: // a lexer's own pseudo keyword is no control word
				toks[i].Type = chroma.Keyword
			}

		case !js || t.Type != chroma.NameOther:
		case t.Value == "from" && nextType(toks, i).InCategory(chroma.LiteralString):
			toks[i].Type = chroma.KeywordPseudo
		case i+1 < len(toks) && strings.HasPrefix(toks[i+1].Value, "("):
			toks[i].Type = chroma.NameFunction
		case len(t.Value) > 1 && strings.ToUpper(t.Value) == t.Value && strings.ToLower(t.Value) != t.Value:
			toks[i].Type = chroma.NameConstant
		case startsUpper(t.Value):
			toks[i].Type = chroma.NameClass
		}
	}

	return chroma.Literator(toks...)
}

// startsUpper reports s starting with a capital letter.
func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

// nextType is the type of the first token after i that is not blank.
func nextType(toks []chroma.Token, i int) chroma.TokenType {
	for _, t := range toks[i+1:] {
		if strings.TrimSpace(t.Value) != "" {
			return t.Type
		}
	}

	return chroma.Text
}

// mdLexer is chroma's Markdown lexer with one rule more, which takes a run of
// text no other rule can start a match in as one token; chroma's tries every
// rule at each character and spends ~80 ms on 1 000 lines. The tokens are
// the same, which FuzzMarkdownLexer holds.
var mdLexer chroma.Lexer = frontmatter{chroma.MustNewLexer(lexers.Markdown.Config(), markdownRules).SetRegistry(lexers.GlobalLexerRegistry)}

// markdownRules are chroma's, lexers/markdown.go, with the run before the
// catch-all. The run holds nothing a rule can start at: none of \ ` @ # ! [ <,
// no space before * _ ~, no line end, after which the line rules start.
func markdownRules() chroma.Rules {
	return chroma.Rules{
		"root": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `^(#[^#].+\n)`, Type: chroma.ByGroups(chroma.GenericHeading)},
			{Pattern: `^(#{2,6}.+\n)`, Type: chroma.ByGroups(chroma.GenericSubheading)},
			{Pattern: `^(\s*)([*-] )(\[[ xX]\])( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([*-])(\s)(.+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Text, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([0-9]+\.)( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*>\s)(.+\n)`, Type: chroma.ByGroups(chroma.Keyword, chroma.GenericEmph)},
			{Pattern: "^(```\\n)([\\w\\W]*?)(^```$)", Type: chroma.ByGroups(chroma.String, chroma.Text, chroma.String)},
			{Pattern: "^(```)(\\w+)(\\n)([\\w\\W]*?)(^```$)", Type: chroma.UsingByGroup(2, 4, chroma.String, chroma.String, chroma.String, chroma.Text, chroma.String)},
			chroma.Include("inline"),
		},
		"inline": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `\\.`, Type: chroma.Text},
			{Pattern: `(\s)(\*|_)((?:(?!\2).)*)(\2)((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericEmph, chroma.GenericEmph, chroma.GenericEmph, chroma.Text)},
			{Pattern: `(\s)((\*\*|__).*?)\3((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericStrong, chroma.GenericStrong, chroma.Text)},
			{Pattern: `(\s)(~~[^~]+~~)((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericDeleted, chroma.Text)},
			{Pattern: "`[^`]+`", Type: chroma.LiteralStringBacktick},
			{Pattern: `[@#][\w/:]+`, Type: chroma.NameEntity},
			{Pattern: `(!?\[)([^]]+)(\])(\()([^)]+)(\))`, Type: chroma.ByGroups(chroma.Text, chroma.NameTag, chroma.Text, chroma.Text, chroma.NameAttribute, chroma.Text)},
			{Pattern: "(?:[^\\s\\\\`@#!\\[<]|[ \\t](?![*_~]))+", Type: chroma.Text},
			{Pattern: `.|\n`, Type: chroma.Text},
		},
	}
}

// frontmatter hands a file's leading YAML frontmatter to chroma's Markdown
// lexer, which lexes it as YAML, and the rest to its own.
type frontmatter struct{ chroma.Lexer }

func (l frontmatter) Tokenise(opts *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	end := strings.Index(text, "\n---\n") + len("\n---\n")
	if !strings.HasPrefix(text, "---\n") || end < len("\n---\n") {
		return l.Lexer.Tokenise(opts, text)
	}

	head, err := lexers.Markdown.Tokenise(opts, text[:end])
	if err != nil {
		return nil, err
	}

	rest, err := l.Lexer.Tokenise(opts, text[end:])
	if err != nil {
		return nil, err
	}

	return chroma.Concaterator(head, rest), nil
}
