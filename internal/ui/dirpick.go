package ui

import (
	"bufio"
	"bytes"
	"cmp"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// dirPlace is a folder the picker offers on its left: its label and path.
type dirPlace struct{ label, path string }

// userDirs are the XDG user directories the picker offers, in its order.
var userDirs = []string{"DOCUMENTS", "DOWNLOAD", "DESKTOP", "PICTURES", "MUSIC", "VIDEOS"}

// parseUserDirs reads user-dirs.dirs, XDG_DOCUMENTS_DIR="$HOME/Documents"
// and the like, into the paths it names, in userDirs' order; one that is
// home itself, or not absolute, is left out.
func parseUserDirs(b []byte, home string) []string {
	byKey := map[string]string{}

	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || !strings.HasPrefix(k, "XDG_") || !strings.HasSuffix(k, "_DIR") {
			continue
		}

		v = strings.Trim(v, `"`)
		if rest, ok := strings.CutPrefix(v, "$HOME"); ok {
			v = home + rest
		}

		if v = filepath.Clean(v); filepath.IsAbs(v) && v != filepath.Clean(home) && !strings.Contains(v, "$") {
			byKey[strings.TrimSuffix(strings.TrimPrefix(k, "XDG_"), "_DIR")] = v
		}
	}

	var out []string

	for _, k := range userDirs {
		if v, ok := byKey[k]; ok {
			out = append(out, v)
		}
	}

	return out
}

// places are the folders the picker offers: home, the XDG user directories,
// GTK's bookmarks, and the folders holding pando's projects; each once, and
// only those there are.
func places(home string, projects []string) []dirPlace {
	var out []dirPlace

	add := func(label, p string) {
		if st, err := os.Stat(p); err != nil || !st.IsDir() || slices.ContainsFunc(out, func(q dirPlace) bool { return q.path == p }) {
			return
		}

		out = append(out, dirPlace{cmp.Or(label, filepath.Base(p)), p})
	}

	add("Home", home)

	config := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	if b, err := os.ReadFile(filepath.Join(config, "user-dirs.dirs")); err == nil {
		for _, p := range parseUserDirs(b, home) {
			add("", p)
		}
	}

	if b, err := os.ReadFile(filepath.Join(config, "gtk-3.0", "bookmarks")); err == nil {
		for line := range strings.Lines(string(b)) {
			u, label, _ := strings.Cut(strings.TrimSpace(line), " ")
			if p, err := url.Parse(u); err == nil && p.Scheme == "file" {
				add(label, p.Path)
			}
		}
	}

	for _, p := range projects {
		add("", filepath.Dir(p))
	}

	return out
}

// dirForm is the folder picker: a path to type, the places on the left and
// the typed path's folders on the right, then Cancel and the primary button.
// The path is the truth: what follows its last slash filters the folders.
// tab walks path, places, folders and buttons; ⏎ on a folder goes in, on the
// path or the primary button picks it.
type dirForm struct {
	title, ok string
	in        textinput.Model
	places    []dirPlace
	dirs      []string // the folders of the path's directory that match, ".." first
	at        int      // the part with the keyboard: dirPath, dirPlaces, dirList or dirButtons
	place     int
	ptop      int // the first place shown, the places scrolling as the folders do
	sel, top  int
	btn       int
	iw        int
	pick      func(m *Model, dir string) tea.Cmd
}

const (
	dirPath = iota
	dirPlaces
	dirList
	dirButtons
)

// Layout of the picker: the places' column, the listing's rows.
const (
	dirPlacesW = 18
	dirListH   = 12
)

// newDirPicker opens the picker at dir; pick gets the folder chosen.
func newDirPicker(m *Model, title, ok, dir string, pick func(m *Model, dir string) tea.Cmd) *modal {
	home, _ := os.UserHomeDir()

	f := &dirForm{title: title, ok: ok, in: modalInput(), places: places(home, m.st.Projects), pick: pick}
	f.in.Prompt = "❱ "
	f.in.SetValue(strings.TrimSuffix(dir, "/") + "/")
	f.in.CursorEnd()
	f.list()

	return &modal{form: f, x: -1}
}

// list reads the folders the path names: the directory before its last
// slash, filtered by what follows it. ponytail: one read per keystroke on
// the update loop, as completeEntries does; a slow mount would show.
func (f *dirForm) list() {
	dir, base := filepath.Split(f.in.Value())

	f.dirs, f.sel, f.top = []string{".."}, 0, 0

	entries, err := os.ReadDir(expandHome(cmp.Or(dir, ".")))
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}

		if st, err := os.Stat(filepath.Join(expandHome(dir), name)); err == nil && st.IsDir() { // links to folders too
			f.dirs = append(f.dirs, name)
		}
	}

	slices.SortStableFunc(f.dirs[1:], func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
}

// dir is the folder the path names: what is typed, up to its last slash.
func (f *dirForm) dir() string {
	dir, _ := filepath.Split(f.in.Value())
	return expandHome(cmp.Or(dir, "."))
}

// goTo puts path in the box as a folder to list.
func (f *dirForm) goTo(path string) {
	f.in.SetValue(strings.TrimSuffix(path, "/") + "/")
	f.in.CursorEnd()
	f.list()
}

// enter goes into folder i of the listing, ".." up.
func (f *dirForm) enter(i int) {
	if i < 0 || i >= len(f.dirs) {
		return
	}

	if f.dirs[i] == ".." {
		f.goTo(filepath.Dir(filepath.Clean(f.dir())))
		return
	}

	f.goTo(filepath.Join(f.dir(), f.dirs[i]))
}

// chosen is the folder the primary button picks: the selected one in the
// listing while it has the keyboard, else what is typed.
func (f *dirForm) chosen() string {
	if f.at == dirList && f.sel > 0 && f.sel < len(f.dirs) {
		return filepath.Join(f.dir(), f.dirs[f.sel])
	}

	return filepath.Clean(expandHome(f.in.Value()))
}

func (f *dirForm) size() (w, h, placed int) { return 72, dirListH + 6, dirListH + 6 }

func (f *dirForm) lines(w int) []string {
	f.iw = w - 2
	side := dim.Render("│")
	title := ansi.Truncate(" "+f.title+" ", f.iw, "… ")
	empty := side + strings.Repeat(" ", f.iw) + side
	out := []string{dim.Render("╭") + bold.Render(title) + dim.Render(strings.Repeat("─", max(f.iw-ansi.StringWidth(title), 0))+"╮"), empty}

	f.in.SetWidth(f.iw - 4)
	out = append(out, side+fit(" "+realCaret(f.in.View()), f.iw)+side)

	lw := f.iw - dirPlacesW - 2
	f.top = max(min(f.top, f.sel), f.sel-dirListH+1)
	f.ptop = max(min(f.ptop, f.place), f.place-dirListH+1, 0)
	bar := vbar{len(f.dirs), dirListH, f.top}.cells(barState{})

	rowBg := func(focused, sel bool) color.Color {
		switch {
		case sel && focused:
			return pal.selBg
		case sel:
			return pal.hoverBg
		}

		return nil
	}

	for r := range dirListH {
		left := blank(dirPlacesW)

		if i := f.ptop + r; i < len(f.places) {
			p := f.places[i]
			left = row(dirPlacesW, rowBg(f.at == dirPlaces, i == f.place && f.at == dirPlaces), []seg{sg(" "+ansi.Truncate(p.label, dirPlacesW-2, "…"), plain)})
		}

		right := blank(lw)
		if i := f.top + r; i < len(f.dirs) {
			right = row(lw, rowBg(f.at == dirList, i == f.sel), []seg{sg(" "+f.dirs[i], plain)})
		}

		out = append(out, side+left+" "+right+bar[r]+side)
	}

	segs, _ := f.buttonSegs()
	out = append(out, empty, side+row(f.iw, nil, append([]seg{sg(strings.Repeat(" ", f.buttonX()), plain)}, segs...))+side)

	return append(out, dim.Render("╰"+strings.Repeat("─", f.iw)+"╯"))
}

// buttonSegs are Cancel and the primary button, the one ⏎ runs lit, and
// where each starts.
func (f *dirForm) buttonSegs() ([]seg, []int) {
	var (
		segs []seg
		xs   []int
	)

	at, lit := 0, ifElse(f.at == dirButtons, f.btn, 1)
	for i, l := range []string{"Cancel", f.ok} {
		st := lipgloss.NewStyle().Background(pal.mutedButtonBg).Foreground(pal.mutedButtonFg)
		if i == lit {
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

// buttonX is where the buttons start: right-aligned, a blank after them.
func (f *dirForm) buttonX() int {
	segs, _ := f.buttonSegs()

	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.s)
	}

	return max(f.iw-w-1, 0)
}

func (f *dirForm) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); {
	case s == "esc" || s == "ctrl+c":
		m.modal = nil
		return nil

	case s == "tab" || s == "shift+tab":
		f.at = (f.at + ifElse(s == "tab", 1, 3)) % 4
		if f.at == dirPath {
			return f.in.Focus()
		}

		f.in.Blur()

		return nil

	case s == "enter":
		return f.enterKey(m)
	case f.at == dirPath:
		if s == "down" {
			f.at = dirList
			f.in.Blur()

			return nil
		}

		before := f.in.Value()

		var cmd tea.Cmd

		f.in, cmd = f.in.Update(k)
		if f.in.Value() != before {
			f.list()
		}

		return cmd

	case f.at == dirButtons && (s == "left" || s == "right" || s == "h" || s == "l"):
		f.btn = 1 - f.btn
	case s == "up" || s == "k" || s == "down" || s == "j":
		d := ifElse(s == "up" || s == "k", -1, 1)

		switch f.at {
		case dirPlaces:
			f.place = max(0, min(f.place+d, len(f.places)-1))
		case dirList:
			f.sel = max(0, min(f.sel+d, len(f.dirs)-1))
		}

	case s == "backspace" && f.at == dirList:
		f.enter(0)
	}

	return nil
}

// enterKey is ⏎: a place or a folder goes in, the path or a button acts.
func (f *dirForm) enterKey(m *Model) tea.Cmd {
	switch f.at {
	case dirPlaces:
		if f.place < len(f.places) {
			f.goTo(f.places[f.place].path)
		}

		return nil

	case dirList:
		f.enter(f.sel)
		return nil

	case dirButtons:
		if f.btn == 0 {
			m.modal = nil
			return nil
		}
	}

	m.modal = nil

	return f.pick(m, f.chosen())
}

// click acts on row r inside the frame, cell cx: the path takes the
// keyboard, a place goes in, a folder is selected and a second click goes
// in, a button runs.
func (f *dirForm) click(m *Model, r, cx int) tea.Cmd {
	switch k := r - 2; {
	case r == 1:
		f.at = dirPath
		return f.in.Focus()

	case k >= 0 && k < dirListH && cx < dirPlacesW:
		f.at, f.place = dirPlaces, min(f.ptop+k, max(len(f.places)-1, 0))
		f.in.Blur()

		if i := f.ptop + k; i < len(f.places) {
			f.goTo(f.places[i].path)
		}

	case k >= 0 && k < dirListH && f.top+k < len(f.dirs):
		f.in.Blur()

		if i := f.top + k; f.at == dirList && i == f.sel {
			f.enter(i)
		} else {
			f.at, f.sel = dirList, i
		}

	case r == dirListH+3:
		segs, xs := f.buttonSegs()
		for i, x := range xs {
			if x += f.buttonX(); cx >= x && cx < x+ansi.StringWidth(segs[2*i].s) {
				f.at, f.btn = dirButtons, i
				return f.enterKey(m)
			}
		}
	}

	return nil
}

// wheel scrolls the column under cell cx by d rows: the places or the
// folders, the selection kept in view.
func (f *dirForm) wheel(cx, d int) {
	if cx < dirPlacesW {
		f.ptop = max(min(f.ptop+d, len(f.places)-dirListH), 0)
		f.place = min(max(f.place, f.ptop), f.ptop+dirListH-1)

		return
	}

	f.top = max(min(f.top+d, len(f.dirs)-dirListH), 0)
	f.sel = min(max(f.sel, f.top), f.top+dirListH-1)
}

// paste types a pasted path into the box.
func (f *dirForm) paste(msg tea.PasteMsg) tea.Cmd {
	if f.at != dirPath {
		return nil
	}

	var cmd tea.Cmd

	f.in, cmd = f.in.Update(msg)
	f.list()

	return cmd
}
