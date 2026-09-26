package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
	"github.com/xseman/pando/internal/proto"
)

// TestMorphCells is a morph's contract: every frame is as wide as the wider
// text, a character both texts share never scrambles, and the last frame is
// the new text, blanks where the old one was longer.
func TestMorphCells(t *testing.T) {
	for _, c := range [][2]string{
		{"", "fix login bug"},
		{"claude", "claude · fix"},
		{"a much longer old name", "short"},
		{"main", "feature/x"},
		{"same", "same"},
		{"gone", ""},
	} {
		from, to := c[0], c[1]
		n := max(utf8.RuneCountInString(from), utf8.RuneCountInString(to))
		d := morphDur(from, to)

		for f := range d {
			cs := morphCells(from, to, f)
			if got := cellText(cs); ansi.StringWidth(got) != n {
				t.Fatalf("%q→%q frame %d: %q is %d wide, want %d", from, to, f, got, ansi.StringWidth(got), n)
			}

			rf, rt := []rune(from), []rune(to)
			for i, c := range cs {
				if i < len(rf) && i < len(rt) && rf[i] == rt[i] && (c.s != string(rt[i]) || c.tone != toneSet) {
					t.Fatalf("%q→%q frame %d: shared %q at %d scrambled to %q", from, to, f, rt[i], i, c.s)
				}
			}
		}

		if got := cellText(morphCells(from, to, d-1)); got != to+strings.Repeat(" ", n-utf8.RuneCountInString(to)) {
			t.Fatalf("%q→%q ends on %q", from, to, got)
		}
	}

	if cs := morphCells("", "abc", 0); cs[0].tone != toneNoise || cs[1].tone != toneNoise {
		t.Fatalf("a decode starts in noise: %+v", cs)
	}
}

// TestRollCells turns every digit a step a frame, the ones first, to the new
// number, up when it grew and down when it shrank.
func TestRollCells(t *testing.T) {
	for _, c := range [][2]string{{"3", "7"}, {"9", "12"}, {"12", "9"}, {"40", "40"}, {"199", "200"}} {
		from, to := c[0], c[1]
		d := rollDur(from, to)

		if got := strings.TrimLeft(cellText(rollCells(from, to, d)), " "); got != to {
			t.Fatalf("%s→%s ends on %q", from, to, got)
		}
	}

	if got := []string{cellText(rollCells("3", "7", 0)), cellText(rollCells("3", "7", 2)), cellText(rollCells("7", "3", 1))}; got[0] != "3" || got[1] != "5" || got[2] != "6" {
		t.Fatalf("3→7 at 0 and 2, 7→3 at 1: %v", got)
	}

	if rollDur("40", "40") != 0 {
		t.Fatal("an unchanged count does not roll")
	}
}

// fxModel is a test model with animations on and Spaces on screen.
func fxModel(t *testing.T) *Model {
	t.Helper()

	m := testModel(t)
	m.st.Settings.Anim = true
	m.Update(nil) // the first look primes what the effects compare against
	press(m, "3")

	return m
}

// settle ticks until every effect has run, failing if the ticker never stops.
func settle(t *testing.T, m *Model) {
	t.Helper()

	for i := 0; ; i++ {
		if _, cmd := m.Update(fxTickMsg{}); cmd == nil {
			return
		}

		if i == 200 {
			t.Fatal("the effects ticker never stops")
		}
	}
}

func withName(m *Model, id, name string) sessionsMsg {
	ss := append([]proto.Session(nil), m.sessions...)
	for i := range ss {
		if ss[i].ID == id {
			ss[i].Name = name
		}
	}

	return sessionsMsg(ss)
}

// TestSessionRenameMorphs is a renamed session: its name scrambles out of
// the old one into the new, in Spaces as on its tab, then holds.
func TestSessionRenameMorphs(t *testing.T) {
	m := fxModel(t)

	_, cmd := m.Update(withName(m, "s1", "refactor-auth"))
	if cmd == nil || !m.fx.ticking {
		t.Fatal("a rename starts the effects ticker")
	}

	if out := checkWidths(t, m); strings.Contains(out, "refactor-auth") {
		t.Fatalf("the new name shows before it morphs in:\n%s", out)
	}

	for range 5 {
		m.Update(fxTickMsg{})
		checkWidths(t, m)
	}

	settle(t, m)

	if out := checkWidths(t, m); !strings.Contains(out, "refactor-auth") {
		t.Fatalf("the name settles:\n%s", out)
	}

	if m.fx.ticking || len(m.fx.anims) != 0 {
		t.Fatalf("nothing runs once settled: ticking %v, %v", m.fx.ticking, m.fx.anims)
	}
}

// TestNewSessionDecodes is a session that appears after the first look: its
// name decodes out of noise. The sessions there from the start do not.
func TestNewSessionDecodes(t *testing.T) {
	m := fxModel(t)
	if len(m.fx.anims) != 0 {
		t.Fatalf("the first look animates nothing: %v", m.fx.anims)
	}

	ss := append(append([]proto.Session(nil), m.sessions...),
		proto.Session{SessionSpec: proto.SessionSpec{ID: "s2", Workspace: m.ws, Agent: "shell", Name: "tests"}, Status: "running"})
	m.Update(sessionsMsg(ss))

	if a, _, ok := m.fx.at("sess:s2"); !ok || a.from != "" || a.to != "tests" {
		t.Fatalf("the new session decodes in: %+v", m.fx.anims)
	}

	if _, _, ok := m.fx.at("sess:s1"); ok {
		t.Fatal("the session already there is left alone")
	}
}

// TestBlockedSessionSweeps is a session that starts waiting: a band of light
// crosses its name once.
func TestBlockedSessionSweeps(t *testing.T) {
	m := fxModel(t)

	ss := append([]proto.Session(nil), m.sessions...)
	ss[0].Status = "blocked"
	m.Update(sessionsMsg(ss))

	a, _, ok := m.fx.at("sess:s1")
	if !ok || a.kind != fxSweep {
		t.Fatalf("blocked sweeps the name: %+v", m.fx.anims)
	}

	m.Update(fxTickMsg{})
	m.Update(fxTickMsg{})

	if cs := m.fx.cells("sess:s1"); cs[0].tone != toneLit || cs[len(cs)-1].tone == toneLit {
		t.Fatalf("the band starts at the left: %+v", cs)
	}

	settle(t, m)
	m.Update(sessionsMsg(ss))

	if len(m.fx.anims) != 0 {
		t.Fatal("still blocked is not news")
	}
}

// TestKillDissolves is Kill Session: the name dissolves into noise before
// the kill is sent, and stays blank until the session's row goes.
func TestKillDissolves(t *testing.T) {
	m := fxModel(t)
	s := *m.session("s1")
	m.ag.remove(m, &agRow{kind: agSession, s: s})

	if m.modal == nil {
		t.Fatal("Kill asks first")
	}

	run := m.modal.items[0].run
	m.modal = nil

	if cmd := run(m); cmd == nil {
		t.Fatal("the kill is sent")
	}

	a, _, ok := m.fx.at("sess:s1")
	if !ok || a.to != "" || a.from != sessionName(s) {
		t.Fatalf("the name dissolves: %+v", m.fx.anims)
	}

	m.fx.frame += morphDur(a.from, "")
	if got := m.fx.text("sess:s1", sessionName(s)); strings.TrimSpace(got) != "" {
		t.Fatalf("dissolved, the name holds blank: %q", got)
	}
}

// TestCommitDissolves is a commit landing: the message leaves the box
// through noise instead of vanishing.
func TestCommitDissolves(t *testing.T) {
	m := gitModel(t)
	m.st.Settings.Anim = true
	m.Update(nil)

	m.scm.input.SetValue("fix: the thing")
	checkWidths(t, m) // the box learns its width
	m.Update(scmMsg{root: m.ws, text: "committed", clear: true})

	a, _, ok := m.fx.at("box:" + m.ws)
	if !ok || a.from != "fix: the thing" {
		t.Fatalf("the message dissolves: %+v", m.fx.anims)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "the thing") || strings.Contains(out, "Message (") {
		t.Fatalf("frame 0 is still mostly the message:\n%s", out)
	}

	settle(t, m)

	if out := checkWidths(t, m); strings.Contains(out, "fix: the thing") || !strings.Contains(out, "Message (") {
		t.Fatalf("the placeholder is back:\n%s", out)
	}
}

// TestCountsRoll is a count that changes: the Changes badge and the commits
// ahead roll to the new number instead of jumping.
func TestCountsRoll(t *testing.T) {
	m := gitModel(t)
	m.st.Settings.Anim = true
	m.Update(nil)

	root := m.ws
	m.Update(gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "main", Ahead: 3, Upstream: "origin/main", Changes: []git.Entry{
		{Path: "a/b.go", Letter: 'M'},
	}}}})

	if a, _, ok := m.fx.at("ahead:" + root); !ok || a.kind != fxRoll || a.from != "0" || a.to != "3" {
		t.Fatalf("ahead rolls 0→3: %+v", m.fx.anims)
	}

	if a, _, ok := m.fx.at(sectionFx(root, "Changes")); !ok || a.from != "3" || a.to != "1" {
		t.Fatalf("Changes rolls 3→1: %+v", m.fx.anims)
	}

	checkWidths(t, m)
	settle(t, m)

	if out := checkWidths(t, m); !strings.Contains(out, "↑3") {
		t.Fatalf("ahead settles on 3:\n%s", out)
	}
}

// TestBranchMorphs is a checkout: the branch in the status bar morphs.
func TestBranchMorphs(t *testing.T) {
	m := gitModel(t)
	m.st.Settings.Anim = true
	m.Update(nil)

	root := m.ws
	m.Update(gitMsg{ws: root, repos: []string{root}, status: map[string]git.Status{root: {Branch: "feature/login"}}})

	if a, _, ok := m.fx.at("branch:" + root); !ok || a.from != "main" || a.to != "feature/login" {
		t.Fatalf("the branch morphs: %+v", m.fx.anims)
	}

	settle(t, m)

	if out := checkWidths(t, m); !strings.Contains(out, "feature/login") {
		t.Fatalf("the branch settles:\n%s", out)
	}
}

// TestFlashDecodes is a status bar message: it decodes in.
func TestFlashDecodes(t *testing.T) {
	m := fxModel(t)
	m.Update(flashMsg{text: "synced"})

	if a, _, ok := m.fx.at("flash"); !ok || a.to != "synced" {
		t.Fatalf("the flash decodes in: %+v", m.fx.anims)
	}
}

// TestShimmerWhileBusy is a label for work in progress: the ticker runs as
// long as the work does, and a band of light moves across the label.
func TestShimmerWhileBusy(t *testing.T) {
	m := fxModel(t)
	m.sr.busy = true

	if _, cmd := m.Update(nil); cmd == nil {
		t.Fatal("a search running ticks")
	}

	for range fxBand + 1 { // the band's middle comes onto the label
		m.Update(fxTickMsg{})
	}

	a := paintSegs(m.shimmer("searching…", dim, bold))
	m.Update(fxTickMsg{})

	if paintSegs(m.shimmer("searching…", dim, bold)) == a {
		t.Fatal("the band moves")
	}

	checkWidths(t, m)

	m.sr.busy = false
	if _, cmd := m.Update(fxTickMsg{}); cmd != nil {
		t.Fatal("the ticker stops with the work")
	}
}

// TestAnimationsOff is animations = false: nothing starts, nothing ticks.
func TestAnimationsOff(t *testing.T) {
	m := testModel(t)
	press(m, "3")
	m.Update(nil)

	if _, cmd := m.Update(withName(m, "s1", "renamed")); cmd != nil && m.fx.ticking {
		t.Fatal("no ticker with animations off")
	}

	if len(m.fx.anims) != 0 {
		t.Fatalf("no effect with animations off: %v", m.fx.anims)
	}

	if out := checkWidths(t, m); !strings.Contains(out, "renamed") {
		t.Fatalf("the name shows at once:\n%s", out)
	}
}

// TestBandGradient is the band of light over RGB colors: a ramp from the
// text's color up to the accent in the middle and back, bold in the middle
// three. Over ANSI colors, which cannot blend, only the middle three light.
func TestBandGradient(t *testing.T) {
	applyLook("vscode-dark", true, "ascii", nil)

	st := fg(hex("#cccccc"))
	e := effects{on: true}
	cs := sweepCells("abcdefghij", 6) // the middle on d

	ss := e.cellSegs(cs, st, lit(st))
	if len(ss) != 8 || ss[3].s != "d" || ss[7].s != "hij" {
		t.Fatalf("a segment a glow, then the rest: %+v", ss)
	}

	if hexColor(ss[3].st.GetForeground()) != hexColor(pal.headerAccent) || !ss[3].st.GetBold() {
		t.Fatalf("the middle is the accent, bold: %v", ss[3].st.GetForeground())
	}

	if c := hexColor(ss[0].st.GetForeground()); c == "#cccccc" || c == hexColor(pal.headerAccent) || ss[0].st.GetBold() {
		t.Fatalf("the edge is between the two, not bold: %v", c)
	}

	if hexColor(ss[1].st.GetForeground()) != hexColor(ss[5].st.GetForeground()) {
		t.Fatal("the ramp is symmetric")
	}

	ansiSt := fg(ansi16(7))
	flat := e.cellSegs(cs, ansiSt, lit(ansiSt))

	if len(flat) != 3 || flat[1].s != "cde" {
		t.Fatalf("ANSI colors light the middle three: %+v", flat)
	}

	e.fg, e.bg = hex("#cccccc"), hex("#1e1e1e")
	if e.glows(dim, bold) == nil {
		t.Fatal("unstyled text blends from the terminal's own colors")
	}
}
