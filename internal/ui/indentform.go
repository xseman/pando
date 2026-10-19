package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// indentForm is the indentation dialog: spaces or tabs, how wide, then
// Cancel, Auto (the file's own again: its .editorconfig or its lines) and
// Apply. ←→ change the row's value, ↑↓ and tab move between rows, a digit
// picks the width, ⏎ applies, esc cancels.
type indentForm struct {
	title  string
	spaces bool
	width  int // 1 to 8
	at     int // the row with the keyboard: indentStyle, indentSize or indentButtons
	btn    int // the button with the keyboard, on that row
	iw     int // the inner width lines last drew at, which a click measures against
	apply  func(m *Model, spaces bool, size int) tea.Cmd
	reset  func(m *Model) tea.Cmd // Auto; nil hides it
}

const (
	indentStyle = iota
	indentSize
	indentButtons
)

// indentSizes are the widths the dialog offers.
var indentSizes = []string{"1", "2", "3", "4", "5", "6", "7", "8"}

func (f *indentForm) buttons() []string {
	if f.reset == nil {
		return []string{"Cancel", "Apply"}
	}

	return []string{"Cancel", "Auto", "Apply"}
}

// size is the box's width and rows: a blank row around each of the three.
func (f *indentForm) size() (w, h, placed int) { return 40, 8, 8 }

// rowSegs is a row's segments, left-aligned, and where each choice starts.
func (f *indentForm) rowSegs(r int) ([]seg, []int) {
	switch r {
	case indentStyle:
		return choices([]string{"Spaces", "Tabs"}, ifElse(f.spaces, 0, 1), f.at == indentStyle)
	case indentSize:
		return choices(indentSizes, f.width-1, f.at == indentSize)
	}

	var (
		segs []seg
		xs   []int
	)

	labels, primary, at := f.buttons(), len(f.buttons())-1, 0
	if f.at == indentButtons {
		primary = f.btn
	}

	for i, l := range labels {
		st := lipgloss.NewStyle().Background(pal.mutedButtonBg).Foreground(pal.mutedButtonFg)
		if i == primary { // what ⏎ runs
			st = lipgloss.NewStyle().Background(pal.buttonBg).Foreground(pal.buttonFg)
		}

		if i > 0 {
			segs, at = append(segs, sg(" ", plain)), at+1
		}

		segs, xs = append(segs, sgOwn(" "+l+" ", st)), append(xs, at)
		at += ansi.StringWidth(l) + 2
	}

	return segs, xs
}

// rowX is where row r starts: the choices centered, the buttons right.
func (f *indentForm) rowX(r int) int {
	segs, _ := f.rowSegs(r)

	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.s)
	}

	if r == indentButtons {
		return max(f.iw-w-1, 0)
	}

	return max((f.iw-w)/2, 0)
}

func (f *indentForm) lines(w int) []string {
	f.iw = w - 2
	side := dim.Render("│")
	title := ansi.Truncate(" "+f.title+" ", f.iw, "… ")
	blank := side + strings.Repeat(" ", f.iw) + side
	out := []string{dim.Render("╭") + bold.Render(title) + dim.Render(strings.Repeat("─", max(f.iw-ansi.StringWidth(title), 0))+"╮")}

	for r := range 3 {
		segs, _ := f.rowSegs(r)
		out = append(out, blank, side+row(f.iw, nil, append([]seg{sg(strings.Repeat(" ", f.rowX(r)), plain)}, segs...))+side)
	}

	return append(out, blank, dim.Render("╰"+strings.Repeat("─", f.iw)+"╯"))
}

func (f *indentForm) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(indentSizes) {
		f.width, f.at = n, indentSize
		return nil
	}

	switch s {
	case "esc", "ctrl+c", "q":
		m.modal = nil
	case "enter", "space":
		return f.press(m, ifElse(f.at == indentButtons, f.btn, len(f.buttons())-1))
	case "a":
		if f.reset != nil {
			return f.press(m, 1)
		}

	case "up", "k", "shift+tab":
		f.at = (f.at + 2) % 3
	case "down", "j", "tab":
		f.at = (f.at + 1) % 3
	case "left", "h", "right", "l":
		f.step(ifElse(s == "left" || s == "h", -1, 1))
	}

	return nil
}

// step moves the row's value d along, the buttons wrapping.
func (f *indentForm) step(d int) {
	switch f.at {
	case indentStyle:
		f.spaces = d < 0
	case indentSize:
		f.width = max(1, min(f.width+d, len(indentSizes)))
	default:
		n := len(f.buttons())
		f.btn = (f.btn + d + n) % n
	}
}

// press runs button i: Cancel closes, Auto and Apply act and close.
func (f *indentForm) press(m *Model, i int) tea.Cmd {
	m.modal = nil

	switch f.buttons()[i] {
	case "Auto":
		return f.reset(m)
	case "Apply":
		return f.apply(m, f.spaces, f.width)
	}

	return nil
}

// click picks the choice or presses the button under cell cx of row r,
// counted inside the frame; a click elsewhere on a row gives it the keyboard.
func (f *indentForm) click(m *Model, r, cx int) tea.Cmd {
	at := r / 2 // a blank row before each of the three
	if r%2 == 0 || at > indentButtons {
		return nil
	}

	f.at = at
	segs, xs := f.rowSegs(at)

	for i, x := range xs {
		x += f.rowX(at)
		if j := i * ifElse(at == indentButtons, 2, 1); cx >= x && cx < x+ansi.StringWidth(segs[j].s) {
			switch at {
			case indentStyle:
				f.spaces = i == 0
			case indentSize:
				f.width = i + 1
			default:
				f.btn = i
				return f.press(m, i)
			}
		}
	}

	return nil
}

func (f *indentForm) paste(tea.PasteMsg) tea.Cmd { return nil }
