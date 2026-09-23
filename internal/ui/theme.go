package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// palette holds every color the UI draws with. Nothing else defines a color,
// so switching themes can never leave a dark-only color on a light terminal.
// Values follow VS Code's 2026 themes; herdr-sidebar's Material set colors the file icons.
type palette struct {
	light                                                                        bool
	modified, untracked, added, renamed, deleted, conflict, ignored              color.Color
	selBg, selUnfocusedBg, hoverBg                                               color.Color
	selFg                                                                        color.Color // nil keeps each segment's own color
	accent, headerAccent                                                         color.Color
	buttonBg, buttonFg, buttonHoverBg, buttonSep, mutedButtonBg, mutedButtonFg   color.Color
	inputBorder, inputBg, sectionBg, badgeBg, badgeFg                            color.Color
	keycapBg, keycapFg                                                           color.Color
	textSelBg, matchBg, wordHiBg                                                 color.Color
	rangeHiBg, findMatchBg, findHitBg                                            color.Color // Go to Line's line; find's other and current matches
	diffDelBg, diffDelWordBg, diffAddBg, diffAddWordBg, diffDelMark, diffAddMark color.Color
	// merge conflict blocks in an editor: VS Code's merge.*Background over the editor background
	mergeCurrentHeadBg, mergeCurrentBg, mergeIncomingHeadBg, mergeIncomingBg, mergeCommonHeadBg, mergeCommonBg color.Color
	ok, warn, errc, attention                                                                                  color.Color
	blockedBg, doneBg                                                                                          color.Color // session_highlight: error and attention over the sidebar
	blockedSoftBg, doneSoftBg                                                                                  color.Color // the other shade of their pulse
	sliderBg, sliderActiveBg, rulerBorder                                                                      color.Color // VS Code's scrollbarSlider over the editor, and editorOverviewRuler.border
	tabBg, tabBorder                                                                                           color.Color // tab.inactiveBackground and tab.border, a shade stronger for a terminal
}

func hex(s string) color.Color { return lipgloss.Color(s) }
func ansi16(n int) color.Color { return lipgloss.Color(strconv.Itoa(n)) }

var (
	// vscodeDark and vscodeLight are VS Code's Dark 2026 and Light 2026: flat
	// neutral greys, a teal (dark) or blue (light) accent, translucent
	// list colors flattened over the editor and sidebar backgrounds.
	vscodeDark = palette{
		modified: hex("#e5ba7d"), untracked: hex("#73c991"), added: hex("#73c991"), renamed: hex("#73c991"),
		deleted: hex("#f48771"), conflict: hex("#f48771"), ignored: hex("#8c8c8c"),
		selBg: hex("#383839"), selUnfocusedBg: hex("#2c2d2e"), hoverBg: hex("#2b2c2d"),
		accent: hex("#3994bc"), headerAccent: hex("#48a0c7"),
		buttonBg: hex("#297aa0"), buttonFg: hex("#ffffff"), buttonHoverBg: hex("#3a8db5"), buttonSep: hex("#5aa9c9"),
		mutedButtonBg: hex("#1e3a47"), mutedButtonFg: hex("#8fb3c4"),
		inputBorder: hex("#333536"), inputBg: hex("#242526"), sectionBg: hex("#202122"), badgeBg: hex("#307e9f"), badgeFg: hex("#ffffff"),
		keycapBg: hex("#313233"), keycapFg: hex("#bfbfbf"),
		textSelBg: hex("#245c73"), matchBg: hex("#1e4352"), wordHiBg: hex("#192d37"),
		rangeHiBg: hex("#242526"), findMatchBg: hex("#1d3d4b"), findHitBg: hex("#255f77"),
		diffDelBg: hex("#401d1d"), diffDelWordBg: hex("#562f2d"), diffAddBg: hex("#1b2d1d"), diffAddWordBg: hex("#274129"),
		diffDelMark: hex("#f28772"), diffAddMark: hex("#72c892"),
		mergeCurrentHeadBg: hex("#2f7366"), mergeCurrentBg: hex("#25403b"), mergeIncomingHeadBg: hex("#2f628f"), mergeIncomingBg: hex("#25394b"),
		mergeCommonHeadBg: hex("#383838"), mergeCommonBg: hex("#282828"),
		ok: hex("#72c892"), warn: hex("#cca700"), errc: hex("#f48771"), attention: hex("#ad80d7"),
		blockedBg: hex("#4f342e"), doneBg: hex("#3d3248"),
		blockedSoftBg: hex("#35272a"), doneSoftBg: hex("#2b2733"),
		sliderBg: hex("#606162"), sliderActiveBg: hex("#6e6f70"), rulerBorder: hex("#2a2b2c"),
		tabBg: hex("#26272a"), tabBorder: hex("#3c3d40"),
	}
	vscodeLight = palette{
		light:    true,
		modified: hex("#667309"), untracked: hex("#587c0c"), added: hex("#587c0c"), renamed: hex("#587c0c"),
		deleted: hex("#ad0707"), conflict: hex("#ad0707"), ignored: hex("#8e8e90"),
		selBg: hex("#d6d6d8"), selUnfocusedBg: hex("#e7e7e8"), hoverBg: hex("#e6e6e9"), selFg: hex("#202020"),
		accent: hex("#0069cc"), headerAccent: hex("#0069cc"),
		// VS Code Light's solid button with RGB white text; ANSI white is grey on light profiles.
		buttonBg: hex("#0069cc"), buttonFg: hex("#ffffff"), buttonHoverBg: hex("#0056a6"), buttonSep: hex("#5c9fe0"),
		mutedButtonBg: hex("#e6f2fa"), mutedButtonFg: hex("#3b5b7a"),
		inputBorder: hex("#d8d8d8"), inputBg: hex("#f7f7fa"), sectionBg: hex("#f0f0f3"), badgeBg: hex("#0069cc"), badgeFg: hex("#ffffff"),
		keycapBg: hex("#eaeaea"), keycapFg: hex("#202020"),
		textSelBg: hex("#bfd9f2"), matchBg: hex("#cce2f5"), wordHiBg: hex("#d9e9f7"),
		rangeHiBg: hex("#eaeaea"), findMatchBg: hex("#e5f0fa"), findHitBg: hex("#a6cbed"),
		diffDelBg: hex("#f3dada"), diffDelWordBg: hex("#e2a8a8"), diffAddBg: hex("#e6ecdb"), diffAddWordBg: hex("#c5d1aa"),
		diffDelMark: hex("#ad0707"), diffAddMark: hex("#587c0c"),
		mergeCurrentHeadBg: hex("#a0e4d7"), mergeCurrentBg: hex("#d9f4ef"), mergeIncomingHeadBg: hex("#a0d3ff"), mergeIncomingBg: hex("#d9edff"),
		mergeCommonHeadBg: hex("#c0c0c0"), mergeCommonBg: hex("#e6e6e6"),
		ok: hex("#388a34"), warn: hex("#b69500"), errc: hex("#ad0707"), attention: hex("#652d90"),
		blockedBg: hex("#edd4d4"), doneBg: hex("#e2d9e8"),
		blockedSoftBg: hex("#f6e8e8"), doneSoftBg: hex("#efeaf3"),
		sliderBg: hex("#8a8a8a"), sliderActiveBg: hex("#777777"), rulerBorder: hex("#f0f1f2"),
		tabBg: hex("#e8e8ec"), tabBorder: hex("#d0d0d6"),
	}
	// terminalPal inherits the terminal profile's ANSI colors. Diff tints stay
	// RGB: an ANSI background would collide with remapped syntax colors.
	terminalPal = palette{
		modified: ansi16(3), untracked: ansi16(2), added: ansi16(10), renamed: ansi16(2),
		deleted: ansi16(1), conflict: ansi16(9), ignored: ansi16(8),
		selBg: ansi16(8), selUnfocusedBg: ansi16(0), hoverBg: ansi16(0), selFg: ansi16(15),
		accent: ansi16(4), headerAccent: ansi16(12),
		buttonBg: ansi16(4), buttonFg: ansi16(15), buttonHoverBg: ansi16(12), buttonSep: ansi16(12),
		mutedButtonBg: ansi16(0), mutedButtonFg: ansi16(7),
		inputBorder: ansi16(8), inputBg: ansi16(0), sectionBg: ansi16(0), badgeBg: ansi16(8), badgeFg: ansi16(15),
		keycapBg: ansi16(8), keycapFg: ansi16(15),
		textSelBg: ansi16(8), matchBg: ansi16(3), wordHiBg: ansi16(8),
		rangeHiBg: ansi16(0), findMatchBg: ansi16(8), findHitBg: ansi16(3),
		diffDelBg: vscodeDark.diffDelBg, diffDelWordBg: vscodeDark.diffDelWordBg,
		diffAddBg: vscodeDark.diffAddBg, diffAddWordBg: vscodeDark.diffAddWordBg,
		diffDelMark: vscodeDark.diffDelMark, diffAddMark: vscodeDark.diffAddMark,
		mergeCurrentHeadBg: vscodeDark.mergeCurrentHeadBg, mergeCurrentBg: vscodeDark.mergeCurrentBg,
		mergeIncomingHeadBg: vscodeDark.mergeIncomingHeadBg, mergeIncomingBg: vscodeDark.mergeIncomingBg,
		mergeCommonHeadBg: vscodeDark.mergeCommonHeadBg, mergeCommonBg: vscodeDark.mergeCommonBg,
		ok: ansi16(2), warn: ansi16(3), errc: ansi16(1), attention: ansi16(5),
		blockedBg: vscodeDark.blockedBg, doneBg: vscodeDark.doneBg,
		blockedSoftBg: vscodeDark.blockedSoftBg, doneSoftBg: vscodeDark.doneSoftBg,
		sliderBg: ansi16(8), sliderActiveBg: ansi16(7), rulerBorder: ansi16(8),
		tabBg: ansi16(0), tabBorder: ansi16(8),
	}

	// themeNames is the Settings cycle order; "vscode" follows the terminal background.
	// defaultDrawers are the Source Control drawers shown without a setting.
	defaultDrawers = []string{"Commits", "Graph", "Branches", "Stashes", "Remotes"}

	themeNames = []string{"vscode", "vscode-dark", "vscode-light", "terminal"}

	// iconSets is the Settings cycle order: Nerd Font glyphs, herdr-sidebar's
	// emoji, or plain text labels.
	iconSets = []string{"nerd", "emoji", "ascii"}

	pal        = vscodeDark
	iconsNerd  bool // glyphs from a Nerd Font
	iconsEmoji bool // emoji, which any font draws

	plain  = lipgloss.NewStyle()
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	accent = lipgloss.NewStyle().Foreground(pal.headerAccent).Bold(true)
)

// colorKeys maps config.toml [colors] keys to the palette fields they override.
func (p *palette) colorKeys() map[string]*color.Color {
	return map[string]*color.Color{
		"modified": &p.modified, "untracked": &p.untracked, "added": &p.added, "renamed": &p.renamed,
		"deleted": &p.deleted, "conflict": &p.conflict, "ignored": &p.ignored,
		"sel_bg": &p.selBg, "sel_unfocused_bg": &p.selUnfocusedBg, "hover_bg": &p.hoverBg, "sel_fg": &p.selFg,
		"accent": &p.accent, "header_accent": &p.headerAccent,
		"button_bg": &p.buttonBg, "button_fg": &p.buttonFg, "button_hover_bg": &p.buttonHoverBg, "button_sep": &p.buttonSep,
		"muted_button_bg": &p.mutedButtonBg, "muted_button_fg": &p.mutedButtonFg,
		"input_border": &p.inputBorder, "input_bg": &p.inputBg, "section_bg": &p.sectionBg, "badge_bg": &p.badgeBg, "badge_fg": &p.badgeFg,
		"keycap_bg": &p.keycapBg, "keycap_fg": &p.keycapFg, "text_sel_bg": &p.textSelBg, "match_bg": &p.matchBg, "word_hi_bg": &p.wordHiBg,
		"range_hi_bg": &p.rangeHiBg, "find_match_bg": &p.findMatchBg, "find_hit_bg": &p.findHitBg,
		"diff_del_bg": &p.diffDelBg, "diff_del_word_bg": &p.diffDelWordBg, "diff_add_bg": &p.diffAddBg,
		"diff_add_word_bg": &p.diffAddWordBg, "diff_del_mark": &p.diffDelMark, "diff_add_mark": &p.diffAddMark,
		"merge_current_head_bg": &p.mergeCurrentHeadBg, "merge_current_bg": &p.mergeCurrentBg,
		"merge_incoming_head_bg": &p.mergeIncomingHeadBg, "merge_incoming_bg": &p.mergeIncomingBg,
		"merge_common_head_bg": &p.mergeCommonHeadBg, "merge_common_bg": &p.mergeCommonBg,
		"ok": &p.ok, "warn": &p.warn, "error": &p.errc, "attention": &p.attention,
		"blocked_bg": &p.blockedBg, "done_bg": &p.doneBg, "blocked_soft_bg": &p.blockedSoftBg, "done_soft_bg": &p.doneSoftBg,
		"scrollbar_slider": &p.sliderBg, "scrollbar_slider_active": &p.sliderActiveBg, "overview_ruler_border": &p.rulerBorder,
		"tab_bg": &p.tabBg, "tab_border": &p.tabBorder,
	}
}

// parseColor reads "#rrggbb" or an ANSI color number 0-255.
func parseColor(s string) (color.Color, bool) {
	if _, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32); err == nil && len(s) == 7 && s[0] == '#' {
		return hex(s), true
	}

	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return lipgloss.Color(s), true
	}

	return nil, false
}

// applyLook selects the palette, applies the config's [colors] overrides
// (invalid ones are skipped; pando doctor lists the keys) and the icon set.
func applyLook(theme string, dark bool, icons string, colors map[string]string) {
	switch {
	case theme == "terminal":
		pal = terminalPal
	case theme == "vscode-dark", theme != "vscode-light" && dark:
		pal = vscodeDark
	default:
		pal = vscodeLight
	}

	keys := pal.colorKeys()
	for k, v := range colors {
		if c, ok := parseColor(v); ok && keys[k] != nil {
			*keys[k] = c
		}
	}

	iconsNerd, iconsEmoji = icons == "nerd", icons == "emoji"
	accent = lipgloss.NewStyle().Foreground(pal.headerAccent).Bold(true)
}

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func statusColor(letter byte) color.Color {
	switch letter {
	case 'M', '*':
		return pal.modified
	case 'A':
		return pal.added
	case 'U':
		return pal.untracked
	case 'D':
		return pal.deleted
	case 'R', 'C':
		return pal.renamed
	case '!':
		return pal.conflict
	}

	return pal.ignored
}

// keycap draws a key as a small raised chip.
func keycap(k string) string { return keycapHot().Render(" " + k + " ") }

// keycapHot is the raised look of a key, or of a button under the mouse.
func keycapHot() lipgloss.Style {
	return lipgloss.NewStyle().Background(pal.keycapBg).Foreground(pal.keycapFg)
}

// selStyle is the selected tab's chip: bold on the selection background.
// tabGap is the column after every tab of a strip, where tab.border's
// hairline sets it apart from the next.
const tabGap = 1

// tabClose is the end of a tab's label: the ✕ on the active tab, as much
// blank on the others, so a tab keeps its width whichever one is active and
// the strip never shifts under the pointer, as VS Code's does.
func tabClose(active bool) string {
	if active {
		return icClose.s() + " "
	}

	return strings.Repeat(" ", ansi.StringWidth(icClose.s())) + " "
}

// tabChip draws one tab of a strip as VS Code does: the active one in the
// selection's colors, any other on its own background (bg, else
// tab.inactiveBackground), and the border's hairline after it.
func tabChip(label string, active bool, bg color.Color) []seg {
	if bg == nil {
		bg = pal.tabBg
	}

	st := dim.Background(bg)
	if active {
		st = selStyle()
	}

	return []seg{sgOwn(label, st), sg("▏", fg(pal.tabBorder))}
}

func selStyle() lipgloss.Style {
	st := lipgloss.NewStyle().Background(pal.selBg).Bold(true)
	if pal.selFg != nil {
		st = st.Foreground(pal.selFg)
	}

	return st
}

// badge is a count in a chip of its own color.
func badge(s string) seg {
	return sgOwn(" "+s+" ", lipgloss.NewStyle().Background(pal.badgeBg).Foreground(pal.badgeFg))
}

// glyph is a Nerd Font icon, named after its VS Code codicon (or its Font
// Awesome / devicon name where herdr-sidebar picks one of those), with the
// emoji herdr-sidebar draws in its place and the text shown with neither.
type glyph struct{ name, nerd, text, emoji string }

func (g glyph) s() string {
	switch {
	case iconsNerd:
		return g.nerd
	case iconsEmoji && g.emoji != "":
		return g.emoji
	}

	return g.text
}

// short is the glyph at its narrowest, for a chip one cell wide: the icon, or
// the first letter of the label without one.
func (g glyph) short() string {
	if iconsNerd || (iconsEmoji && g.emoji != "") {
		return g.s()
	}

	r, _ := utf8.DecodeRuneInString(g.text) // a one-rune label is a symbol, not a word

	return string(r)
}

var (
	icFiles      = glyph{"folder", "\uea83", "Files", "📁"}
	icGit        = glyph{"code-fork (fa)", "\uf126", "Git", "🔀"}
	icAgents     = glyph{"robot (mdi)", "󰚩", "Agents", "🤖"}
	icSpaces     = glyph{"multiple-windows", "\ueb23", "Spaces", "🪟"}
	icTerminal   = glyph{"terminal", "", "Term", "💻"}
	icDot        = glyph{"circle-filled", "\ueabc", "•", ""}
	icGear       = glyph{"gear", "", "⚙", "⚙"}
	icNewFile    = glyph{"new-file", "", "+f", "📄"}
	icNewDir     = glyph{"new-folder", "", "+d", "📁"}
	icRefresh    = glyph{"refresh", "", "⟳", "⟳"}
	icCollapse   = glyph{"collapse-all", "", "-", "⊟"}
	icTree       = glyph{"list-tree", "", "tree", ""}
	icList       = glyph{"list-flat", "", "list", ""}
	icAdd        = glyph{"add", "", "+", ""}
	icRemove     = glyph{"remove", "", "−", ""}
	icDiscard    = glyph{"discard", "", "↶", ""}
	icWorktree   = glyph{"repo-forked", "", "+⎇", ""}
	icSparkle    = glyph{"star-full", "\ueb59", "✦", ""} // a codicon: one cell, where mdi's creation often draws two
	icCheck      = glyph{"check", "", "✓", ""}
	icChevron    = glyph{"chevron-down", "", "∨", ""}
	icClose      = glyph{"close", "", "✕", ""}
	icBranch     = glyph{"git-branch (dev)", "\ue725", "⎇", "⎇"}
	icPublish    = glyph{"cloud-upload", "\ueac3", "☁", ""}
	icSync       = glyph{"sync", "\uea77", "⇅", ""}
	icSplit      = glyph{"split-horizontal", "", "split", ""}
	icInline     = glyph{"diff", "", "inline", ""}
	icOpenLeft   = glyph{"chevron-right", "", "»", ""}
	icOpenRight  = glyph{"chevron-left", "", "«", ""}
	icSearch     = glyph{"search", "\uea6d", "Search", "🔍"}
	icCase       = glyph{"case-sensitive", "\ueab1", "Aa", ""}
	icWord       = glyph{"whole-word", "\ueb7e", "ab", ""}
	icRegex      = glyph{"regex", "\ueb38", ".*", ""}
	icEllipsis   = glyph{"ellipsis", "\uea7c", "⋯", ""}
	icClearAll   = glyph{"clear-all", "\ueabf", "✕", ""}
	icOptions    = glyph{"settings", "\ueb52", "☰", ""} // VS Code's view options: sliders, not the gear
	icPreview    = glyph{"open-preview", "\ueb28", "md", ""}
	icSource     = glyph{"go-to-file", "\uea94", "src", ""}
	icPrevRev    = glyph{"arrow-left", "\uea9b", "←", ""}
	icUp         = glyph{"arrow-up", "\ueaa1", "↑", ""}
	icDown       = glyph{"arrow-down", "\uea9a", "↓", ""}
	icNextRev    = glyph{"arrow-right", "\uea9c", "→", ""}
	icHistory    = glyph{"history", "\uea82", "rev", ""}
	icReplace    = glyph{"replace", "\ueb3d", "↹", ""}
	icPreserve   = glyph{"preserve-case", "\ueb2e", "AB", ""}
	icReplaceAll = glyph{"replace-all", "\ueb3c", "⇄", ""}
	icTag        = glyph{"tag", "\uea66", "#", ""}
	icDetach     = glyph{"debug-disconnect", "\uead0", "◇", ""}
	icMainWt     = glyph{"home", "\ueb06", "⌂", ""}           // a project's own checkout
	icLinkedWt   = glyph{"source-control", "\uea68", "⑂", ""} // a linked worktree of it
	allGlyphs    = []glyph{
		icFiles, icGit, icSpaces, icAgents, icGear, icNewFile, icNewDir, icRefresh, icCollapse, icTree, icList,
		icAdd, icRemove, icDiscard, icWorktree, icSparkle, icCheck, icChevron, icClose, icBranch, icPublish, icSync, icSplit, icInline,
		icOpenLeft, icOpenRight, icSearch, icCase, icWord, icRegex, icEllipsis, icClearAll, icOptions, icPreview, icSource,
		icPrevRev, icNextRev, icUp, icDown, icHistory, icTag, icDetach, icReplace, icPreserve, icReplaceAll, icTerminal, icDot,
		icMainWt, icLinkedWt,
	}
)

// viewIcon is a view's tab and rail icon. Spaces gets the windows glyph, the
// docked session the robot: one is where the work lives, the other an agent.
func viewIcon(v view) glyph {
	return []glyph{icFiles, icGit, icSpaces, icSearch, icTerminal, icAgents}[v]
}

// Doctor lists every UI glyph with its codepoint and the installed font that
// draws it, so one screenshot shows what the terminal renders.
func Doctor(w io.Writer) {
	for _, g := range allGlyphs {
		r, _ := utf8.DecodeRuneInString(g.nerd)
		out, _ := exec.Command("fc-list", fmt.Sprintf(":charset=%x", r), "family").Output()
		font := "no installed font"

		for i, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" && (i == 0 || strings.Contains(strings.ToLower(l), "nerd")) {
				font = l
			}
		}

		_, _ = fmt.Fprintf(w, "%-16s U+%04X  %s  %-2s  %-6s  %s\n", g.name, r, g.nerd, g.emoji, g.text, font)
	}

	switch fam := terminalFont(); {
	case fam == "":
		_, _ = fmt.Fprintln(w, "\nThis terminal does not report its font (Alacritty does). The icons above need a\nNerd Font set as the terminal font; otherwise run `pando set icons emoji`.")
	case !nerdFamily(fam):
		_, _ = fmt.Fprintf(w, "\nTerminal font %q is not a Nerd Font, so icons show as boxes or random glyphs.\n"+
			"Set a Nerd Font in the terminal config and restart the terminal (a running terminal\n"+
			"may have missed the config change), or run `pando set icons emoji`.\n", fam)

	default:
		_, _ = fmt.Fprintf(w, "\nTerminal font: %s\n", fam)
	}

	p := vscodeDark
	keys := p.colorKeys()
	_, _ = fmt.Fprintln(w, "\n[colors] keys for config.toml, with the vscode-dark values:")

	for _, k := range slices.Sorted(maps.Keys(keys)) {
		v := "(terminal default)"
		if *keys[k] != nil {
			v = hexColor(*keys[k])
		}

		_, _ = fmt.Fprintf(w, "  %-18s %s\n", k, v)
	}
}

// terminalFont is the font the terminal draws with, "" when it cannot tell.
// Only Alacritty answers, with the live config of the calling window.
func terminalFont() string {
	if os.Getenv("ALACRITTY_SOCKET") == "" {
		return ""
	}

	out, err := exec.Command("alacritty", "msg", "get-config").Output()

	var c struct {
		Font struct {
			Normal struct {
				Family string `json:"family"`
			} `json:"normal"`
		} `json:"font"`
	}
	if err != nil || json.Unmarshal(out, &c) != nil {
		return ""
	}

	return c.Font.Normal.Family
}

// nerdFamily reports whether a font family name is a Nerd Font build.
func nerdFamily(family string) bool {
	for _, f := range strings.Fields(strings.ToLower(family)) {
		if f == "nerd" || f == "nf" || f == "nfm" || f == "nfp" {
			return true
		}
	}

	return false
}

type fileKind struct{ glyph, emoji, rgb string }

var fileKinds = map[string]fileKind{
	"dir": {"", "📁", "#90a4ae"}, "dirOpen": {"", "📂", "#90a4ae"}, "rust": {"", "🦀", "#dea584"},
	"python": {"", "🐍", "#3572a5"}, "js": {"", "🟨", "#f1e05a"}, "ts": {"", "🔷", "#3178c6"},
	"react": {"", "🟦", "#61dafb"}, "json": {"", "🧾", "#cbcb41"}, "markdown": {"", "📝", "#519aba"},
	"html": {"", "🌐", "#e34c26"}, "css": {"", "🎨", "#42a5f5"}, "config": {"", "🔧", "#6d8086"},
	"xml": {"", "📰", "#e37933"}, "shell": {"", "🐚", "#4eaa25"}, "powershell": {"\U000f0a0a", "💻", "#5391fe"},
	"c": {"", "🔩", "#f34b7d"}, "csharp": {"\U000f031b", "🟣", "#178600"}, "go": {"", "🐹", "#00add8"},
	"ruby": {"", "💎", "#701516"}, "php": {"", "🐘", "#4f5d95"}, "java": {"", "☕", "#b07219"},
	"kotlin": {"", "🟪", "#a97bff"}, "swift": {"", "🐦", "#f05138"}, "lua": {"", "🌙", "#51a0cf"},
	"sql": {"", "💾", "#f29111"}, "data": {"", "📊", "#33a852"}, "text": {"", "📄", "#9e9e9e"},
	"log": {"", "📋", "#757575"}, "pdf": {"", "📕", "#e53935"}, "image": {"", "📷", "#26a69a"},
	"audio": {"", "🎵", "#ec407a"}, "video": {"", "🎬", "#ff7043"}, "archive": {"", "🧳", "#afb42b"},
	"lock": {"", "🔒", "#ffd54f"}, "binary": {"", "⚡", "#ef5350"}, "font": {"", "🔤", "#b0bec5"},
	"notebook": {"", "📓", "#f57c00"}, "git": {"", "🙈", "#f14e32"}, "docker": {"", "🐳", "#0db7ed"},
	"package": {"", "📦", "#8d6e63"}, "build": {"", "🔨", "#6d8086"}, "readme": {"", "📖", "#42a5f5"},
	"license": {"", "📜", "#ffd54f"}, "env": {"", "🔑", "#ffd54f"}, "file": {"", "📄", "#90a4ae"},
}

var extKinds = func() map[string]string {
	groups := map[string]string{
		"rust": "rs", "python": "py pyi", "js": "js mjs cjs", "ts": "ts", "react": "jsx tsx", "json": "json jsonc",
		"markdown": "md markdown", "html": "html htm", "css": "css scss sass less",
		"config": "toml yaml yml ini cfg conf", "xml": "xml", "shell": "sh bash zsh fish",
		"powershell": "ps1 psm1 psd1 bat cmd", "c": "c h cpp cc cxx hpp hh", "csharp": "cs", "go": "go",
		"ruby": "rb", "php": "php", "java": "java jar", "kotlin": "kt kts", "swift": "swift", "lua": "lua",
		"sql": "sql db sqlite sqlite3", "data": "csv tsv", "text": "txt", "log": "log", "pdf": "pdf",
		"image": "png jpg jpeg gif webp bmp ico svg tiff", "audio": "mp3 wav flac ogg",
		"video": "mp4 mkv avi mov webm", "archive": "zip tar gz tgz bz2 xz 7z rar", "lock": "lock",
		"binary": "exe dll so dylib a o bin wasm", "font": "ttf otf woff woff2", "notebook": "ipynb",
	}
	m := map[string]string{}

	for kind, exts := range groups {
		for _, e := range strings.Fields(exts) {
			m[e] = kind
		}
	}

	return m
}()

func kindOf(name string, dir, open bool) string {
	if dir {
		if open {
			return "dirOpen"
		}

		return "dir"
	}

	l := strings.ToLower(name)
	switch l {
	case "cargo.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum":
		return "lock"
	case "cargo.toml", "package.json", "pyproject.toml", "go.mod", "gemfile":
		return "package"
	case "makefile", "justfile", "cmakelists.txt":
		return "build"
	case ".gitignore", ".gitattributes", ".gitmodules":
		return "git"
	case ".env":
		return "env"
	case "copying":
		return "license"
	}

	switch {
	case strings.HasPrefix(l, "dockerfile") || strings.HasPrefix(l, "docker-compose"):
		return "docker"
	case strings.HasPrefix(l, "readme"):
		return "readme"
	case strings.HasPrefix(l, "license"):
		return "license"
	case strings.HasPrefix(l, ".env."):
		return "env"
	}

	if i := strings.LastIndexByte(l, '.'); i >= 0 {
		if k, ok := extKinds[l[i+1:]]; ok {
			return k
		}
	}

	return "file"
}

// iconSeg is a file's tinted Nerd Font icon or its emoji, and a space; empty
// with text icons. Emoji carry their own colors and are two cells wide.
func iconSeg(name string, dir, open bool) seg {
	k := fileKinds[kindOf(name, dir, open)]

	switch {
	case iconsNerd:
		return sg(k.glyph+" ", fg(tint(k.rgb)))
	case iconsEmoji:
		return sg(k.emoji+" ", plain)
	}

	return sg("", plain)
}

// tint darkens bright icon colors on a light background so they stay legible.
func tint(rgb string) color.Color {
	if !pal.light {
		return hex(rgb)
	}

	v, err := strconv.ParseUint(strings.TrimPrefix(rgb, "#"), 16, 32)
	if err != nil {
		return hex(rgb)
	}

	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)

	const maxLuma = 0.42
	if luma := (0.2126*r + 0.7152*g + 0.0722*b) / 255; luma > maxLuma {
		k := maxLuma / luma
		r, g, b = r*k, g*k, b*k
	}

	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}
