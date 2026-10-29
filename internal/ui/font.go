package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/font"
)

// fontMsg is font.Offer's answer, asked once at startup: the file system walk
// stays off the first frame.
type fontMsg bool

// fontDoneMsg is how installing the Nerd Font went.
type fontDoneMsg struct{ err error }

func checkFont() tea.Cmd { return func() tea.Msg { return fontMsg(font.Offer()) } }

// onFont takes the offer's answer and the install's outcome. An install that
// failed offers it again; one that worked switches the icons to it.
func (m *Model) onFont(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case fontMsg:
		m.nf = ifElse(bool(msg), "offer", "")
	case fontDoneMsg:
		if msg.err != nil {
			m.nf = "offer"
			return flash("Nerd Font: "+msg.err.Error(), true)
		}

		m.nf = ""

		return tea.Batch(m.setSettings(map[string]any{"icons": "nerd"}),
			flash("Nerd Font installed · icons set to nerd · restart the terminal if they show as boxes", false))
	}

	return nil
}

// fontChip is the status bar's offer of Symbols Nerd Font Mono while no Nerd
// Font is installed: a click installs it, a shimmer runs while it does.
func (m *Model) fontChip() ([]seg, func(m *Model) tea.Cmd) {
	if m.nf == "" || !m.st.Settings.FontOffer {
		return nil, nil
	}

	accent := fg(pal.headerAccent)
	if m.nf == "installing" {
		return append(append([]seg{sg(" ", accent)}, m.shimmer("installing Nerd Font…", accent.Faint(true), accent.Bold(true))...), sg(" ", accent)), nil
	}

	return []seg{sg(" "+icDown.s()+" Nerd Font ", accent)}, (*Model).installFont
}

func (m *Model) installFont() tea.Cmd {
	m.nf = "installing" // the chip changes before the download ends

	return func() tea.Msg { return fontDoneMsg{font.Install(context.Background())} }
}

// dismissFont is the offer's ×: it is not made again, nor on another start.
func (m *Model) dismissFont() tea.Cmd {
	m.nf = ""

	return m.setSettings(map[string]any{"nerd_font_offer": false})
}
