package daemon

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// TestUpdatedSkipsShortFlip keeps Updated through a flip to running and back
// shorter than longBurst: a redraw, which can hit every session in one tick.
func TestUpdatedSkipsShortFlip(t *testing.T) {
	now := time.Now()
	before := now.Add(-3 * time.Hour)

	s := &session{status: "idle", active: before, busySince: now.Add(-time.Second), lastOutput: now}
	if !s.tick(now) || s.status != "running" || !s.info().Updated.Equal(before) {
		t.Fatalf("a burst just begun: %+v", s.info())
	}

	later := now.Add(2 * time.Second)
	if !s.tick(later) || s.status != "idle" || !s.info().Updated.Equal(before) {
		t.Fatalf("a burst of a second: %+v", s.info())
	}
}

// TestUpdatedCountsWork moves Updated once a burst has lasted, and again when
// it ends; a question counts whatever came before it.
func TestUpdatedCountsWork(t *testing.T) {
	now := time.Now()

	s := &session{status: "idle", active: now.Add(-time.Hour), busySince: now.Add(-longBurst), lastOutput: now}
	s.tick(now)

	if !s.info().Updated.Equal(now) {
		t.Fatalf("a burst that lasted: %+v", s.info())
	}

	end := now.Add(5 * time.Second)
	s.tick(end)

	if s.status != "idle" || !s.info().Updated.Equal(end) {
		t.Fatalf("its end: %+v", s.info())
	}

	s.status = "blocked"
	if !s.worked("idle") {
		t.Fatal("a question did not count")
	}
}

// TestUpdatedToldMidBurst tells the clients once a burst has lasted, though no
// status changes to carry it, and only once.
func TestUpdatedToldMidBurst(t *testing.T) {
	now := time.Now()

	s := &session{status: "running", active: now.Add(-time.Hour), busySince: now.Add(-longBurst), lastOutput: now}
	if !s.tick(now) || !s.info().Updated.Equal(now) {
		t.Fatalf("a burst that lasted: %+v", s.info())
	}

	if s.tick(now.Add(time.Millisecond)) {
		t.Fatal("the same burst was told twice")
	}
}

// TestReadIsNoLook leaves a session's attention alone: a script reads the
// screen with session.read, and only session.screen is the user looking at it.
func TestReadIsNoLook(t *testing.T) {
	s := &session{status: "idle", attention: true, emu: vt.NewEmulator(20, 4)}
	_ = s.text(false, 0)
	s.tick(time.Now())

	if !s.attention {
		t.Fatal("reading the screen cleared attention")
	}
}
