package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestFontChip(t *testing.T) {
	m := testModel(t)
	m.st.Settings.FontOffer = true

	if out := checkWidths(t, m); strings.Contains(out, "Nerd Font") {
		t.Fatalf("an offer before font.Offer answered:\n%s", out)
	}

	m.onFont(fontMsg(true))

	if out := checkWidths(t, m); !strings.Contains(out, "Nerd Font") {
		t.Fatalf("no Nerd Font offer in the status bar:\n%s", out)
	}

	// A click installs it; the chip says so and offers no ×, so a click
	// meant for it does not dismiss the offer mid-download.
	clickStatus(m, "Nerd Font")

	if out := checkWidths(t, m); m.nf != "installing" || strings.Contains(out, " × ") {
		t.Fatalf("nf = %q after the click:\n%s", m.nf, out)
	}

	// A failed install offers it again, one that worked switches the icons.
	if m.onFont(fontDoneMsg{errors.New("offline")}) == nil || m.nf != "offer" {
		t.Fatalf("a failed install: nf = %q, want the offer back and a flash", m.nf)
	}

	m.onFont(fontDoneMsg{})

	if m.nf != "" || m.st.Settings.Icons != "nerd" {
		t.Fatalf("after the install: nf = %q, icons = %q", m.nf, m.st.Settings.Icons)
	}

	// The × turns the offer off for good.
	m.st.Settings.Icons = "ascii"
	m.look()
	m.onFont(fontMsg(true))
	clickStatus(m, " × ")

	if out := checkWidths(t, m); m.st.Settings.FontOffer || strings.Contains(out, "Nerd Font") {
		t.Fatalf("the × left the offer on (%v):\n%s", m.st.Settings.FontOffer, out)
	}
}
