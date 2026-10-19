package ui

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/xseman/pando/internal/git"
)

// blameKey is what a line's blame was asked about: the file, the line, and
// the text and history it was blamed against.
type blameKey struct {
	path  string
	line  int // the buffer's line
	buf   *buffer
	edits int
	gen   int // the preview's baseGen: a commit moves the history
}

// blameState is the status bar's blame of the cursor's line, VS Code's
// git.blame.statusBarItem: asked once the cursor rests (blameDelay), shown
// while the cursor stays on the line it was asked for.
type blameState struct {
	want blameKey
	seq  int
	got  blameMsg
}

// blameDelay is how long the cursor rests on a line before its blame is asked.
const blameDelay = 250 * time.Millisecond

type blameTickMsg struct{ seq int }

type blameMsg struct {
	seq  int
	key  blameKey
	b    git.Blame
	root string
	ok   bool
}

// noteBlame asks again after blameDelay when the cursor's line, its text or
// its history moved: a tracked file shown as text only.
func (m *Model) noteBlame() tea.Cmd {
	p := &m.pv
	if p.kind != pvFile || p.buf == nil || p.base == nil || p.path == "" || p.trunc || !m.showsPreview() {
		return nil
	}

	key := blameKey{p.path, p.rawPos(p.at()).line, p.buf, p.buf.edits, p.baseGen}
	if key == m.blame.want {
		return nil
	}

	m.blame.want, m.blame.seq = key, m.blame.seq+1
	seq := m.blame.seq

	return tea.Tick(blameDelay, func(time.Time) tea.Msg { return blameTickMsg{seq} })
}

func (m *Model) onBlameTick(seq int) tea.Cmd {
	key := m.blame.want
	if seq != m.blame.seq || m.pv.path != key.path || m.pv.buf != key.buf {
		return nil
	}

	text := m.pv.text()

	return func() tea.Msg {
		b, err := git.BlameLine(key.path, key.line, text)

		root := ""
		if err == nil && b.Hash != "" {
			root, err = git.Root(filepath.Dir(key.path))
		}

		return blameMsg{seq: seq, key: key, b: b, root: root, ok: err == nil}
	}
}

func (m *Model) onBlame(msg blameMsg) {
	if msg.seq > m.blame.got.seq {
		m.blame.got = msg
	}
}

// blameItem is the status bar's blame of the cursor's line: who and when,
// the subject as its tip, a click opening the commit. ok is false when
// there is none to show: a line the answer is not about, or no answer.
func (m *Model) blameItem() (text, tip string, run func(*Model) tea.Cmd, ok bool) {
	g, p := m.blame.got, &m.pv
	if !g.ok || p.kind != pvFile || p.buf == nil || p.md == 1 || g.key.path != p.path || g.key.line != p.rawPos(p.at()).line || !m.showsPreview() {
		return "", "", nil, false
	}

	if g.b.Hash == "" {
		return " Not Committed Yet ", "the line has changed since the last commit", nil, true
	}

	run = func(m *Model) tea.Cmd { return m.openShow(g.root, g.b.Hash) }

	return " " + ansi.Truncate(g.b.Author, 20, "…") + ", " + ghAgo(g.b.When) + " ", g.b.Summary, run, true
}

// segsWidth is the cells segments take.
func segsWidth(ss []seg) int {
	n := 0
	for _, s := range ss {
		n += ansi.StringWidth(s.s)
	}

	return n
}
