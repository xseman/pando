package ui

import (
	"image/color"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The panel's Output: what pando would otherwise drop or only flash, as
// VS Code's Output channel keeps it: each language server's stderr and log
// messages, and every error the status bar flashes, in full.

// outputLine is one line of the Output: when, from where, and whether it
// is an error.
type outputLine struct {
	at       time.Time
	src, msg string
	err      bool
}

// outputMax is the lines the Output keeps; past it the oldest outputTrim go.
const (
	outputMax  = 5000
	outputTrim = 500
)

// outputLog is the Output's lines and its list, which follows the newest
// line while its selection is on it.
type outputLog struct {
	lines []outputLine
	l     list
	h     int // the rows the list had when last drawn
	w     int // the cells it had
	wide  int // the widest line's cells, as drawn: it only grows until Clear
	left  int // the first cell shown, panned by the horizontal bar
}

// add appends lines, a line of each line a message has, the list following
// them while its selection is on the last line, or nowhere.
func (o *outputLog) add(lines ...outputLine) {
	follow := o.l.sel < 0 || o.l.sel >= len(o.lines)-1

	for _, l := range lines {
		for msg := range strings.SplitSeq(strings.TrimRight(l.msg, "\n"), "\n") {
			l.msg = printable(msg)
			o.lines = append(o.lines, l)
			o.wide = max(o.wide, ansi.StringWidth(l.row())+1)
		}
	}

	if n := len(o.lines); n > outputMax {
		cut := n - outputMax + outputTrim
		o.lines = append(o.lines[:0:0], o.lines[cut:]...)
		o.l.sel = max(o.l.sel-cut, -1)
		o.l.top = max(o.l.top-cut, 0)
	}

	if follow && len(o.lines) > 0 {
		o.l.sel = len(o.lines) - 1
		o.l.snap(max(o.h, 1))
	}
}

// printable is a line as one row shows it: no escapes, a tab as spaces, no
// other control characters.
func printable(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")

	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}

		return r
	}, s)
}

// text is every line, as Copy All puts it on the clipboard.
func (o *outputLog) text() string {
	var b strings.Builder
	for _, l := range o.lines {
		b.WriteString(l.String() + "\n")
	}

	return b.String()
}

func (l outputLine) String() string { return l.at.Format("15:04:05") + " " + l.src + ": " + l.msg }

// row is the line as its row shows it, unstyled.
func (l outputLine) row() string { return " " + l.at.Format("15:04:05") + " " + l.src + " " + l.msg }

// hbar is the horizontal bar under the lines, w cells: on while one is
// wider than that.
func (o *outputLog) hbar(w int) vbar { return vbar{o.wide, w, o.left} }

// pan moves the lines d cells sideways, within the widest.
func (o *outputLog) pan(d int) { o.left = max(min(o.left+d, o.wide-o.w), 0) }

func (o *outputLog) view(m *Model, w, h int) []string {
	o.h = h
	if len(o.lines) == 0 {
		out := []string{row(w, nil, []seg{sg("  Language servers' output and pando's errors show here.", dim)})}
		for len(out) < h {
			out = append(out, blank(w))
		}

		return out[:max(h, 0)]
	}

	o.w = w

	hb := o.hbar(w)
	if hb.on() { // its row under the lines
		h--
		o.h = h
	}

	o.pan(0)
	o.l.clamp(len(o.lines), h)
	o.l.bar = m.barState("list:output")

	hover := -1
	if _, y, ok := m.panelHover(); ok {
		hover = o.l.at(y, len(o.lines))
	}

	out := o.l.render(w, h, len(o.lines), func(i, rw int) string {
		l := o.lines[i]

		var bg color.Color

		switch {
		case i == o.l.sel && m.focus == onPanel:
			bg = pal.selBg
		case i == hover:
			bg = pal.hoverBg
		}

		full := row(max(o.wide, rw+o.left), bg, []seg{sg(" "+l.at.Format("15:04:05")+" ", dim), sg(l.src+" ", muted), sg(l.msg, ifElse(l.err, fg(pal.errc), plain))})

		return ansi.Cut(full, o.left, o.left+rw)
	})

	if hb.on() {
		out = append(out, hb.hcells(m.barState("output-h")))
	}

	return out
}

func (o *outputLog) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	h, n := max(o.h, 1), len(o.lines)

	switch k.String() {
	case "up", "k":
		o.l.move(-1, n, h)
	case "down", "j":
		o.l.move(1, n, h)
	case "pgup", "b":
		o.l.move(-h, n, h)
	case "pgdown", "f":
		o.l.move(h, n, h)
	case "g", "home":
		o.l.move(-n, n, h)
	case "G", "end":
		o.l.move(n, n, h)
	case "left", "h":
		o.pan(-8)
	case "right", "l":
		o.pan(8)
	case "y", "ctrl+c":
		if o.l.sel >= 0 && o.l.sel < n {
			return setClipboard(o.lines[o.l.sel].String(), "copied the line")
		}

	case "esc", "q", "tab":
		m.focus = onMain
	}

	return nil
}

// mouse is an event at body cell (x, y): the wheel scrolls, sideways with
// shift or a tilt; the bar under the lines pans them; a click selects.
func (o *outputLog) mouse(m *Model, msg tea.MouseMsg, x, y int) tea.Cmd {
	mo := msg.Mouse()
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		if d, ok := wheelX(mo); ok {
			o.pan(d)
			return nil
		}

		o.l.wheel(wheelDelta(mo), len(o.lines), max(o.h, 1))

		return nil
	}

	if hb := o.hbar(o.w); hb.on() && y == o.h { // the horizontal bar
		m.overBar = barPoint{"output-h", x}

		if c, click := msg.(tea.MouseClickMsg); click && c.Button == tea.MouseLeft {
			return m.hbarClick("output-h", x, mo.X-x, func() vbar { return o.hbar(o.w) }, func(left int) tea.Cmd {
				o.left = left
				return nil
			})
		}

		return nil
	}

	if c, click := msg.(tea.MouseClickMsg); click && c.Button == tea.MouseLeft {
		if i := o.l.at(y, len(o.lines)); i >= 0 {
			o.l.sel = i
		}
	}

	return nil
}

// logBox takes Output lines from goroutines, a language server's reader,
// and hands them to Update in a batch: a wake on its way stands for every
// line put since.
type logBox struct {
	mu    sync.Mutex
	lines []outputLine
	wake  chan struct{}
}

func newLogBox() *logBox { return &logBox{wake: make(chan struct{}, 1)} }

// put adds a line from src; it never blocks.
func (b *logBox) put(src, msg string) {
	b.mu.Lock()
	b.lines = append(b.lines, outputLine{at: time.Now(), src: src, msg: msg})
	b.mu.Unlock()

	select {
	case b.wake <- struct{}{}:
	default:
	}
}

type logsMsg []outputLine

// waitLogs is the lines put since the last batch, once there are some.
func (b *logBox) waitLogs() tea.Cmd {
	return func() tea.Msg {
		<-b.wake

		b.mu.Lock()
		defer b.mu.Unlock()

		lines := b.lines
		b.lines = nil

		return logsMsg(lines)
	}
}
