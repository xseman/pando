package ui

import (
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// players play one audio file and exit; the first installed one is used.
var players = [][]string{{"paplay"}, {"pw-play"}, {"afplay"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"}, {"mpv", "--no-video", "--really-quiet"}}

var player = sync.OnceValue(func() []string {
	for _, p := range players {
		if _, err := exec.LookPath(p[0]); err == nil {
			return p
		}
	}

	return nil
})

// playSound plays path in the background; without a player or a file it
// rings the terminal's bell instead, which the terminal turns into whatever
// the user configured there. Tests replace it.
var playSound = func(path string) {
	if p := player(); p != nil && path != "" {
		if _, err := os.Stat(path); err == nil {
			_ = exec.Command(p[0], append(p[1:], path)...).Start() // best effort: a cue is not worth an error
			return
		}
	}

	_, _ = os.Stdout.WriteString("\a")
}

const soundGap = time.Second // one cue per second, a batch of finishes is one sound

// soundFor is the cue a session list update deserves: "request" when a
// session out of view started waiting for an answer, "done" when one
// finished unseen, "" otherwise. herdr plays these for background workspaces.
func (m *Model) soundFor(next []proto.Session) string {
	cue := ""

	for _, s := range next {
		if s.ID == m.sess {
			continue
		}

		i := slices.IndexFunc(m.sessions, func(o proto.Session) bool { return o.ID == s.ID })
		if i < 0 {
			continue
		}

		old := m.sessions[i]
		switch {
		case s.Status == "blocked" && old.Status != "blocked":
			return "request"
		case s.Attention && !old.Attention && s.Status != "blocked":
			cue = "done"
		}
	}

	return cue
}

// sound plays the cue when sounds are on and the last one was a moment ago.
func (m *Model) sound(cue string) tea.Cmd {
	if cue == "" || !m.st.Settings.Sounds || time.Since(m.soundAt) < soundGap {
		return nil
	}

	m.soundAt = time.Now()

	path := m.st.Settings.SoundDone
	if cue == "request" {
		path = m.st.Settings.SoundReq
	}

	return func() tea.Msg { playSound(path); return nil }
}
