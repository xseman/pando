package ui

import (
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xseman/pando/internal/proto"
)

// Text effects, after the message box's scramble. A name that changes morphs
// into the new one through ASCII noise, a session or worktree that appears
// decodes out of it and a session killed from here dissolves into it; a
// session that starts waiting has a band of light cross its name, a count
// rolls its digits like an odometer, and a label that stands for work in
// progress shimmers. The band of light fades in and out over a gradient
// blended from the text's color to the accent, where both are RGB colors.
// noteFx starts them by comparing what the model shows
// with what it showed after every Update, so no call site has to remember
// to; one ticker drives them all and stops when nothing moves.
// animations = false turns every one off.

// fxTickMsg advances every running effect by a frame.
type fxTickMsg struct{}

const fxFrame = 60 * time.Millisecond

func fxTick() tea.Cmd { return tea.Tick(fxFrame, func(time.Time) tea.Msg { return fxTickMsg{} }) }

type fxKind int

const (
	fxMorph fxKind = iota // from settles into to through noise, character by character
	fxSweep               // a band of light crosses to once
	fxRoll                // to's digits step there from from's, the ones first
)

// fxBand is how many cells either side of its middle the band of light
// fades over.
const fxBand = 3

// Morph timing, in frames: a character is noise for fxNoise frames before it
// settles, the settling runs left to right over at most fxSpread frames, and
// each character is up to fxJitter frames off its neighbour.
const fxNoise, fxSpread, fxJitter = 3, 12, 4

// anim is one effect running on a key's text.
type anim struct {
	kind     fxKind
	from, to string
	start    int // the frame it started on
	dur      int // frames it runs
}

// effects are the running effects and what each key showed last time noteFx
// looked.
type effects struct {
	on      bool // animations is set
	frame   int
	ticking bool
	anims   map[string]anim
	seen    map[string]string
	primed  bool        // seen holds a first look: a key new after it is news
	fg, bg  color.Color // the terminal's own colors, for text styled without one
}

// cell is one character of an effect's frame.
type cell struct {
	s    string
	tone int // toneSet, toneNoise or toneLit
	glow int // how lit a toneLit cell is: 1 at the band's edge to fxBand+1 in its middle, 0 fully
}

const (
	toneSet   = iota // the text itself
	toneNoise        // a scrambled character, faint
	toneLit          // under the sweep's band or a digit still rolling
)

// fxHash is a stable pseudo-random number per character and frame, so a
// frame renders the same twice and tests can pin it.
func fxHash(i, f int) int {
	h := uint32(i)*2654435761 ^ uint32(f)*40503 //nolint:gosec // wraparound is the point
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15

	return int(h >> 1)
}

// noise is character i's scramble at frame t.
func noise(i, t int) string { return string(scrambleGlyphs[fxHash(i, t)%len(scrambleGlyphs)]) }

// settleAt is the frame character i of n settles on in a morph.
func settleAt(i, n int) int { return fxNoise + i*min(n, fxSpread)/max(n, 1) + fxHash(i, 0)%fxJitter }

func morphDur(from, to string) int {
	n := max(len([]rune(from)), len([]rune(to)))
	return fxNoise + min(n, fxSpread) + fxJitter
}

// morphCells is frame t of from turning into to: a character both share
// stays, the others show from, then noise, then to. Characters past the end
// of to settle as blanks, so the width holds until the morph ends.
func morphCells(from, to string, t int) []cell {
	f, g := []rune(from), []rune(to)
	n := max(len(f), len(g))
	out := make([]cell, 0, n)

	for i := range n {
		inF, inG := i < len(f), i < len(g)
		if inF && inG && f[i] == g[i] {
			out = append(out, cell{s: string(g[i])})
			continue
		}

		at := settleAt(i, n)

		switch {
		case t >= at && inG:
			out = append(out, cell{s: string(g[i])})
		case t >= at:
			out = append(out, cell{s: " "})
		case t < at-fxNoise && inF:
			out = append(out, cell{s: string(f[i])})
		default:
			out = append(out, cell{s: noise(i, t), tone: toneNoise})
		}
	}

	return out
}

// sweepCells is frame t of a band of light crossing text, entering from the
// left: its middle is on cell t-fxBand.
func sweepCells(text string, t int) []cell {
	rs := []rune(text)
	out := make([]cell, len(rs))

	for i, r := range rs {
		out[i] = cell{s: string(r)}
		if d := abs(i - (t - fxBand)); d <= fxBand {
			out[i].tone, out[i].glow = toneLit, fxBand+1-d
		}
	}

	return out
}

// sweepDur is the frames a band takes to cross n cells, in and out.
func sweepDur(n int) int { return n + 2*fxBand + 1 }

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// rollSteps is how far each digit of to turns from from's, both right
// aligned, and the direction: up when the number grew.
func rollSteps(from, to string) (f, g []rune, steps []int, up bool) {
	f, g = []rune(from), []rune(to)
	n := max(len(f), len(g))
	f = append([]rune(strings.Repeat(" ", n-len(f))), f...)
	g = append([]rune(strings.Repeat(" ", n-len(g))), g...)
	a, _ := strconv.Atoi(from)
	b, _ := strconv.Atoi(to)
	up = b >= a

	steps = make([]int, n)
	for i := range n {
		if !isDigit(f[i]) || !isDigit(g[i]) {
			continue
		}

		steps[i] = int(g[i]-f[i]+10) % 10
		if !up {
			steps[i] = int(f[i]-g[i]+10) % 10
		}
	}

	return f, g, steps, up
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func rollDur(from, to string) int {
	_, _, steps, _ := rollSteps(from, to)

	d := 0

	for i, s := range steps {
		if s > 0 {
			d = max(d, len(steps)-1-i+s+1)
		}
	}

	return d
}

// rollCells is frame t of from's digits turning to to's, a step a frame, a
// place starting a frame after the one to its right.
func rollCells(from, to string, t int) []cell {
	f, g, steps, up := rollSteps(from, to)
	out := make([]cell, len(g))

	for i := range g {
		if steps[i] == 0 {
			out[i] = cell{s: string(g[i])}
			continue
		}

		k := min(max(t-(len(g)-1-i), 0), steps[i])

		d := int(f[i]-'0') + k
		if !up {
			d = int(f[i]-'0') - k + 10
		}

		out[i] = cell{s: string(rune('0' + d%10)), tone: toneLit}
		if k == steps[i] {
			out[i].tone = toneSet
		}
	}

	return out
}

// start runs an effect on key from this frame; with animations off it does
// nothing.
func (e *effects) start(key string, kind fxKind, from, to string) {
	if !e.on {
		return
	}

	a := anim{kind: kind, from: from, to: to, start: e.frame}

	switch kind {
	case fxMorph:
		a.dur = morphDur(from, to)
	case fxSweep:
		a.dur = sweepDur(len([]rune(to)))
	case fxRoll:
		a.dur = rollDur(from, to)
	}

	e.run(key, a)
}

// run puts a built effect on key; one that would show nothing is dropped.
func (e *effects) run(key string, a anim) {
	if !e.on || a.dur <= 0 {
		return
	}

	if e.anims == nil {
		e.anims = map[string]anim{}
	}

	a.start = e.frame
	e.anims[key] = a
}

// stop ends key's effect: it shows its text as it is.
func (e *effects) stop(key string) { delete(e.anims, key) }

// at is key's running effect and how many frames it is in.
func (e *effects) at(key string) (anim, int, bool) {
	a, ok := e.anims[key]
	if !ok || e.frame-a.start >= a.dur {
		return anim{}, 0, false
	}

	return a, e.frame - a.start, true
}

// cells is the frame key's effect shows now, nil when none runs.
func (e *effects) cells(key string) []cell {
	a, t, ok := e.at(key)
	if !ok {
		return nil
	}

	switch a.kind {
	case fxSweep:
		return sweepCells(a.to, t)
	case fxRoll:
		return rollCells(a.from, a.to, t)
	case fxMorph:
	}

	return morphCells(a.from, a.to, t)
}

// text is what key shows this frame, as plain text: final, unless an effect
// runs on it.
func (e *effects) text(key, final string) string {
	cs := e.cells(key)
	if cs == nil {
		return final
	}

	return cellText(cs)
}

// segs is key's frame for a row in st: noise faint, the sweep's band and
// rolling digits lit.
func (e *effects) segs(key, final string, st lipgloss.Style) []seg {
	cs := e.cells(key)
	if cs == nil {
		return []seg{sg(final, st)}
	}

	return e.cellSegs(cs, st, lit(st))
}

func cellText(cs []cell) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.s)
	}

	return b.String()
}

// cellSegs groups cells into segments of one style each: noise faint, lit
// cells along the gradient from st to hi, or, where the colors cannot blend,
// hi on the band's middle three.
func (e *effects) cellSegs(cs []cell, st, hi lipgloss.Style) []seg {
	glows := e.glows(st, hi)
	style := func(c cell) (lipgloss.Style, int) {
		switch {
		case c.tone == toneNoise:
			return st.Faint(true), -1
		case c.tone != toneLit:
			return st, 0
		}

		g := c.glow
		if g == 0 { // a rolling digit
			g = fxBand + 1
		}

		switch {
		case glows != nil:
			return glows[g], g
		case g >= fxBand:
			return hi, fxBand
		}

		return st, 0
	}

	var (
		out  []seg
		last = -2
	)

	for _, c := range cs {
		st, k := style(c)
		if k == last {
			out[len(out)-1].s += c.s
			continue
		}

		out, last = append(out, sg(c.s, st)), k
	}

	return out
}

// glows are st's styles under the band, by glow: its color blended toward
// hi's in CIELAB, bold in the middle three. nil when either color is not RGB,
// as the terminal theme's ANSI colors are not.
func (e *effects) glows(st, hi lipgloss.Style) []lipgloss.Style {
	from, to := e.rgb(st), e.rgb(hi)
	if from == nil || to == nil {
		return nil
	}

	cs := lipgloss.Blend1D(fxBand+2, from, to)
	out := make([]lipgloss.Style, len(cs))

	for k, c := range cs {
		out[k] = st.Faint(false).Bold(k >= fxBand).Foreground(c)
	}

	return out
}

// rgb is the color st draws text in, nil unless it is an RGB color: its
// foreground, else the terminal's, halfway to the background when faint.
func (e *effects) rgb(st lipgloss.Style) color.Color {
	c := st.GetForeground()
	if !isRGB(c) {
		c = e.fg
	}

	if !isRGB(c) {
		return nil
	}

	if st.GetFaint() {
		bg := st.GetBackground()
		if !isRGB(bg) {
			bg = e.bg
		}

		if !isRGB(bg) {
			return nil
		}

		c = lipgloss.Blend1D(3, c, bg)[1]
	}

	return c
}

func isRGB(c color.Color) bool {
	_, ok := c.(color.RGBA)
	return ok
}

// lit is st under a band of light.
func lit(st lipgloss.Style) lipgloss.Style {
	return st.Faint(false).Foreground(pal.headerAccent).Bold(true)
}

// paintSegs renders segments as they are, for a caller that draws a string.
func paintSegs(ss []seg) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.st.Render(s.s))
	}

	return b.String()
}

// shimmer is text in st with a band in hi running across it over and over,
// for a label that stands for work in progress; still with animations off.
func (m *Model) shimmer(text string, st, hi lipgloss.Style) []seg {
	if !m.fx.on {
		return []seg{sg(text, st)}
	}

	n := len([]rune(text))

	return m.fx.cellSegs(sweepCells(text, m.fx.frame%(sweepDur(n)+5)), st, hi)
}

// fxWants is whether anything moves: an effect running, or a label that
// shimmers while its work runs.
func (m *Model) fxWants() bool {
	if !m.fx.on {
		return false
	}

	return len(m.fx.anims) > 0 || m.scm.busy != "" || m.sr.busy || m.upd.State == "downloading" && m.upd.Total == 0
}

// animate starts the ticker when something moves and none runs; the ticker
// stops itself when nothing is left to move.
func (m *Model) animate() tea.Cmd {
	if m.fx.ticking || !m.fxWants() {
		return nil
	}

	m.fx.ticking = true

	return fxTick()
}

func (m *Model) onFxTick() tea.Cmd {
	m.fx.frame++
	if m.scm.busy != "" {
		m.scm.frame++
	}

	for k := range m.fx.anims {
		if _, _, ok := m.fx.at(k); !ok {
			delete(m.fx.anims, k)
		}
	}

	if !m.fxWants() {
		m.fx.ticking = false
		return nil
	}

	return fxTick()
}

// noteFx compares what the model shows with what it showed and starts an
// effect on what changed: a session or worktree name morphs, or decodes in
// when it is new; a branch morphs; counts roll; a session that starts waiting
// or finishes is swept; a flash decodes in; the update chip morphs from the
// running version to the new one.
func (m *Model) noteFx() {
	e := &m.fx

	e.on, e.fg, e.bg = m.st.Settings.Anim, nil, nil
	if m.fg != "" {
		e.fg = lipgloss.Color(m.fg)
	}

	if m.bg != "" {
		e.bg = lipgloss.Color(m.bg)
	}

	if !e.on {
		e.anims, e.seen, e.primed = nil, nil, false
		return
	}

	next := make(map[string]string, len(e.seen))
	track := func(key, text string, kind fxKind, fresh bool) {
		next[key] = text

		old, ok := e.seen[key]

		switch {
		case !e.primed:
		case !ok && fresh:
			e.start(key, fxMorph, "", text)
		case ok && old != text:
			e.start(key, kind, old, text)
		}
	}

	perWs := map[string]int{}
	for _, s := range m.agentSessions() {
		perWs[s.Workspace]++
	}

	for _, s := range m.sessions {
		key := "sess:" + s.ID
		track(key, sessionName(s), fxMorph, true)

		mood := ""
		if s.Status == "blocked" || s.Attention {
			mood = s.Status + strconv.FormatBool(s.Attention)
		}

		next["mood:"+s.ID] = mood
		if old, ok := e.seen["mood:"+s.ID]; e.primed && ok && old != mood && mood != "" {
			if _, _, busy := e.at(key); !busy {
				e.start(key, fxSweep, "", sessionName(s))
			}
		}
	}

	for _, w := range m.wss {
		track("ws:"+w.Path, wsName(w), fxMorph, true)
		track("wsn:"+w.Path, strconv.Itoa(perWs[w.Path]), fxRoll, false)
	}

	for root, st := range m.scm.status {
		track("branch:"+root, st.Branch, fxMorph, false)
		track("ahead:"+root, strconv.Itoa(st.Ahead), fxRoll, false)
		track("behind:"+root, strconv.Itoa(st.Behind), fxRoll, false)
	}

	for _, r := range m.scm.rows {
		if r.kind == rowSection {
			track(sectionFx(r.root, r.title), r.text, fxRoll, false)
		}
	}

	track("results", strconv.Itoa(m.sr.matches()), fxRoll, false)
	track("files", strconv.Itoa(len(m.sr.files)), fxRoll, false)
	track("attention", strconv.Itoa(m.attentionCount()), fxRoll, false)

	next["flash"] = m.msgAt.String()
	if e.primed && e.seen["flash"] != next["flash"] && m.msg != "" {
		e.start("flash", fxMorph, "", m.msg)
	}

	next["update"] = m.upd.State
	if e.primed && e.seen["update"] != m.upd.State {
		switch m.upd.State {
		case "available":
			e.start("update", fxMorph, "v"+m.upd.Current, "v"+m.upd.Latest)
		case "ready":
			e.start("update", fxMorph, "", "restart to update")
		}
	}

	e.seen, e.primed = next, true
}

// sectionFx is the key of a Source Control section's count.
func sectionFx(root, title string) string { return "section:" + root + "\x00" + title }

// wsName is what a worktree row says: its branch, else its directory.
func wsName(w proto.Workspace) string {
	if w.Branch != "" {
		return w.Branch
	}

	return filepath.Base(w.Path)
}
