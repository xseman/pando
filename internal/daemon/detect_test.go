package daemon

import (
	"testing"
	"time"
)

func TestScreenState(t *testing.T) {
	cases := []struct {
		program, title string
		lines          []string
		want           string
	}{
		{"claude", "Claude Code", []string{"⏺ Done.", "", "✻ Worked for 5s", "", "───", "❯ ", "───"}, "idle"},
		{"claude", "Claude Code", []string{"✻ Orbiting… (2s · ↓ 66 tokens)", "", "───", "❯ ", "───"}, "running"},
		{"claude", "Claude Code", []string{"✶ Reading 1 file…", "❯ "}, "running"},
		{"claude", "Claude Code", []string{"✻ Cooking… (12s · esc to interrupt)", "❯ "}, "running"},
		{"claude", "Claude Code", []string{"Bash command", "  rm -rf build", "Do you want to proceed?", "❯ 1. Yes", "  2. No", "esc to cancel"}, "blocked"},
		{"codex", "Action Required · codex", []string{"Allow command?"}, "blocked"},
		{"codex", "codex", []string{"• Working (3s • esc to interrupt)"}, "running"},
		{"gemini", "", []string{"│ Apply this change?", "│ ● Yes, allow once"}, "blocked"},
		{"opencode", "", []string{"△ Permission required"}, "blocked"},
		{"opencode", "", []string{"esc to interrupt"}, "running"},
		{"bash", "", []string{"$ "}, ""},
		{"claude", "Claude Code", []string{"plain output, no prompt"}, ""}, // unrecognised: output timing decides
	}
	for _, c := range cases {
		if got := screenState(c.program, c.title, c.lines); got != c.want {
			t.Errorf("%s %q %q = %q, want %q", c.program, c.title, c.lines, got, c.want)
		}
	}
}

func TestBlockedNeedsAttention(t *testing.T) {
	now := time.Now()

	s := &session{status: "running", seen: "blocked", lastOutput: now}
	if !s.tick(now) || s.status != "blocked" || !s.attention {
		t.Fatalf("blocked screen: %+v", s)
	}

	s = &session{status: "running", seen: "blocked", lastOutput: now, lastViewed: now}
	s.tick(now)

	if s.status != "blocked" || s.attention {
		t.Fatalf("viewed blocked session must not need attention: %+v", s)
	}
	// The prompt says idle before the output window closes.
	s = &session{status: "running", seen: "idle", busySince: now.Add(-5 * time.Second), lastOutput: now}
	s.tick(now)

	if s.status != "idle" || !s.attention {
		t.Fatalf("idle prompt after a burst: %+v", s)
	}
}
