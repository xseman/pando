package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// waiting is a session of the model's worktree in the given status, last
// changed ago before now.
func waiting(m *Model, id, status string, ago time.Duration) proto.Session {
	return proto.Session{
		SessionSpec: proto.SessionSpec{ID: id, Workspace: m.ws, Agent: "claude"},
		Status:      status, Updated: time.Now().Add(-ago),
	}
}

// TestNextAlarm walks the sessions waiting on the user with alt+a: blocked,
// then failed, then done, the longest waiting first in each, round again.
func TestNextAlarm(t *testing.T) {
	m := testModel(t)
	done := waiting(m, "done", "idle", time.Hour)
	done.Attention = true
	m.sessions = append(m.sessions, done,
		waiting(m, "late", "blocked", time.Minute),
		waiting(m, "early", "blocked", time.Hour),
		waiting(m, "failed", "exited", 2*time.Hour),
		waiting(m, "working", "running", time.Hour),
		waiting(m, "asleep", "asleep", time.Hour))

	var got []string

	for range 5 {
		press(m, "alt+a")
		got = append(got, m.sess)
	}

	if want := []string{"early", "late", "failed", "done", "early"}; !slices.Equal(got, want) {
		t.Fatalf("alt+a went through %q, want %q", got, want)
	}
}

// TestNextAlarmNoneWaiting leaves the session on screen where it is.
func TestNextAlarmNoneWaiting(t *testing.T) {
	m := testModel(t)
	was := m.sess

	press(m, "alt+a")

	if m.sess != was || len(m.alarms()) != 0 {
		t.Fatalf("nothing waits, but session %q is on screen, was %q", m.sess, was)
	}
}
