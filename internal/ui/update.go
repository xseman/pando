package ui

import (
	"encoding/json"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xseman/pando/internal/proto"
)

// updateMsg is the daemon's update status, read once at startup; after that
// the daemon pushes it as an `update` event.
type updateMsg proto.Update

// barCells is the width of the download meter in the status bar.
const barCells = 10

func loadUpdate() tea.Cmd {
	return func() tea.Msg {
		var u proto.Update
		if proto.Call("update.status", nil, &u) != nil {
			return nil // an older daemon does not know the method
		}

		return updateMsg(u)
	}
}

// onUpdate takes the new status and says what changed out loud: a failed
// download and a finished one, never a failed check — being offline is not
// news.
func (m *Model) onUpdate(u proto.Update) tea.Cmd {
	prev := m.upd

	m.upd = u
	switch {
	case u.State == "error" && prev.State == "downloading":
		return flash("update: "+u.Error, true)
	case u.State == "ready" && prev.State != "ready":
		return flash("pando v"+u.Latest+" installed · restart to update", false)
	}

	return nil
}

// updateChip is the status bar's rightmost item: a release to install, the
// download running, or the restart that puts it to use. It is empty the rest
// of the time, which is nearly always.
func (m *Model) updateChip() ([]seg, func(m *Model) tea.Cmd) {
	// chip is icon and body in st, padded a cell either side.
	chip := func(icon glyph, st lipgloss.Style, body ...seg) []seg {
		return append(append([]seg{sg(" "+icon.s()+" ", st)}, body...), sg(" ", st))
	}

	accent := fg(pal.headerAccent)

	switch m.upd.State {
	case "available":
		return chip(icUp, accent, m.fx.segs("update", "v"+m.upd.Latest, accent)...), (*Model).startUpdate
	case "downloading":
		if m.upd.Total == 0 {
			return chip(icDown, accent, m.shimmer("downloading…", accent.Faint(true), accent.Bold(true))...), nil
		}

		return chip(icDown, accent, sg(meter(m.upd.Percent(), barCells)+" "+strconv.Itoa(m.upd.Percent())+"%", accent)), nil

	case "ready":
		return chip(icRefresh, fg(pal.ok), m.fx.segs("update", "restart to update", fg(pal.ok))...), (*Model).finishUpdate
	}

	return nil, nil
}

// meter draws a filled bar w cells wide.
func meter(pct, w int) string {
	n := max(min(pct*w/100, w), 0)
	return strings.Repeat("█", n) + strings.Repeat("░", w-n)
}

// startUpdate is the click on the version: the daemon downloads the release
// and reports its progress as events.
func (m *Model) startUpdate() tea.Cmd {
	m.upd.State, m.upd.Done, m.upd.Total = "downloading", 0, 0 // the chip changes before the first event
	return do("update.install", nil)
}

// finishUpdate is the click after the binary is in place: only this process
// still runs the old one, so the update takes effect when pando is started
// again.
func (m *Model) finishUpdate() tea.Cmd {
	m.modal = newMenu("pando v"+m.upd.Latest+" is installed", -1, 0,
		item{label: "Quit pando", hint: "start it again to run v" + m.upd.Latest, run: func(m *Model) tea.Cmd {
			return tea.Sequence(m.saveEditors(), m.saveTerm(), m.saveDrafts(), tea.Quit)
		}},
		cancelItem())

	return nil
}

// checkUpdate is the command palette's manual check.
func (m *Model) checkUpdate() tea.Cmd {
	m.flash("checking for a newer pando…", false)

	return func() tea.Msg {
		var u proto.Update
		if err := proto.Call("update.check", nil, &u); err != nil {
			return flashMsg{"update: " + err.Error(), true}
		}

		if u.State == "current" {
			return flashMsg{"pando " + u.Current + " is the latest release", false}
		}

		return updateMsg(u)
	}
}

// updateItems are the self-update commands, the palette's "Pando:" entries.
func (m *Model) updateItems() []item {
	items := []item{{label: "Check for Updates", hint: "v" + m.upd.Current, run: (*Model).checkUpdate}}
	switch m.upd.State {
	case "available":
		items = append(items, item{label: "Install pando v" + m.upd.Latest, run: (*Model).startUpdate})
	case "ready":
		items = append(items, item{label: "Restart to Update", run: (*Model).finishUpdate})
	}

	return items
}

// updateEvent decodes an `update` event; the status travels with it, so no
// client has to ask for it while a download runs.
func updateEvent(data json.RawMessage) (proto.Update, bool) {
	var u proto.Update
	return u, json.Unmarshal(data, &u) == nil
}
