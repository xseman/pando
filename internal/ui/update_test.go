package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// statusY is the row the status bar draws on, under the panel frame.
func statusY(m *Model) int { return m.h - 1 + m.bord() }

func TestUpdateChip(t *testing.T) {
	m := testModel(t)
	if out := checkWidths(t, m); strings.Contains(out, "v0.2.0") {
		t.Fatalf("nothing is known about updates yet, the bar must stay quiet:\n%s", out)
	}

	// A release to install: the version sits in the right corner.
	m.onUpdate(proto.Update{Current: "0.1.0", Latest: "0.2.0", State: "available"})

	out := checkWidths(t, m)
	if !strings.Contains(out, "v0.2.0") {
		t.Fatalf("no version in the status bar:\n%s", out)
	}

	// Clicking it hands the download to the daemon; the chip turns into the
	// meter at once, without waiting for the first event.
	click(m, m.w-2, statusY(m), tea.MouseLeft)

	if m.upd.State != "downloading" {
		t.Fatalf("clicking the version did not start the download: %+v", m.upd)
	}

	m.onUpdate(proto.Update{Current: "0.1.0", Latest: "0.2.0", State: "downloading", Done: 7, Total: 10})

	out = checkWidths(t, m)
	if !strings.Contains(out, "70%") {
		t.Fatalf("no progress in the status bar:\n%s", out)
	}

	// Installed: the bar asks for the restart that puts it to use, and the
	// click confirms it rather than quitting under the user.
	m.onUpdate(proto.Update{Current: "0.1.0", Latest: "0.2.0", State: "ready"})

	out = checkWidths(t, m)
	if !strings.Contains(out, "restart to update") {
		t.Fatalf("no restart hint in the status bar:\n%s", out)
	}

	click(m, m.w-2, statusY(m), tea.MouseLeft)

	if m.modal == nil || !strings.Contains(m.modal.title, "0.2.0") {
		t.Fatalf("modal = %+v", m.modal)
	}
}

// A failed check is silent — being offline is not news — while a failed
// download says so.
func TestUpdateErrorsAreFlashedOnlyAfterADownload(t *testing.T) {
	m := testModel(t)
	m.onUpdate(proto.Update{Current: "0.1.0", State: "error", Error: "no route to host"})

	if out := checkWidths(t, m); strings.Contains(out, "no route to host") {
		t.Fatalf("a failed check must not flash:\n%s", out)
	}

	m.upd.State = "downloading"
	if cmd := m.onUpdate(proto.Update{Current: "0.1.0", State: "error", Error: "checksum"}); cmd == nil {
		t.Fatal("a failed download must flash")
	}
}

func TestMeter(t *testing.T) {
	for _, tc := range []struct{ pct, want int }{{0, 0}, {50, 5}, {100, 10}, {-5, 0}, {150, 10}} {
		if got := strings.Count(meter(tc.pct, barCells), "█"); got != tc.want {
			t.Errorf("meter(%d) filled %d cells, want %d", tc.pct, got, tc.want)
		}

		if got := len([]rune(meter(tc.pct, barCells))); got != barCells {
			t.Errorf("meter(%d) is %d cells wide, want %d", tc.pct, got, barCells)
		}
	}
}

// The update commands are in the palette, so the chip is not the only way to
// reach them.
func TestUpdateCommands(t *testing.T) {
	m := testModel(t)
	m.upd = proto.Update{Current: "0.1.0", Latest: "0.2.0", State: "available"}

	var labels []string
	for _, it := range m.commands() {
		labels = append(labels, it.label)
	}

	for _, want := range []string{"Pando: Check for Updates", "Pando: Install pando v0.2.0"} {
		if !slices.Contains(labels, want) {
			t.Errorf("no %q in the palette", want)
		}
	}
}
