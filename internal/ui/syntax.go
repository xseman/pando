package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// simpleLightCode is Simple Light's code colors as a chroma style, its Zed
// theme's: variables blue, strings green, numbers brown, comments grey,
// keywords bold, the rest the text's own. No background and no text color:
// the terminal's show through. noinherit keeps a name that is no variable
// (a function, a type, a key) off Name's blue.
var simpleLightCode = chroma.MustNewStyle("simple-light", chroma.StyleEntries{
	chroma.Comment:               "#969696",
	chroma.CommentPreproc:        "noinherit",
	chroma.Keyword:               "bold",
	chroma.KeywordType:           "noinherit",
	chroma.KeywordConstant:       "noinherit",
	chroma.Name:                  "#0052a3",
	chroma.NameFunction:          "noinherit",
	chroma.NameClass:             "noinherit",
	chroma.NameNamespace:         "noinherit",
	chroma.NameBuiltin:           "noinherit",
	chroma.NameBuiltinPseudo:     "noinherit bold",
	chroma.NameException:         "noinherit",
	chroma.NameDecorator:         "noinherit",
	chroma.NameConstant:          "noinherit",
	chroma.NameTag:               "noinherit",
	chroma.NameAttribute:         "noinherit",
	chroma.NameLabel:             "noinherit",
	chroma.NameProperty:          "noinherit",
	chroma.NameEntity:            "noinherit",
	chroma.LiteralString:         "#007744",
	chroma.LiteralStringInterpol: "noinherit",
	chroma.LiteralNumber:         "#9c6b00",
	chroma.GenericHeading:        "bold #aa3731",
	chroma.GenericSubheading:     "bold #aa3731",
	chroma.GenericStrong:         "bold #448c27",
	chroma.GenericEmph:           "italic #448c27",
	chroma.GenericInserted:       "#007744",
	chroma.GenericDeleted:        "#c5000b",
	chroma.Error:                 "#c5000b",
})

// syntaxStyle is the code colors for a dark or a light terminal.
func syntaxStyle(dark bool) *chroma.Style {
	if dark {
		return styles.Get("github-dark")
	}

	return simpleLightCode
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
