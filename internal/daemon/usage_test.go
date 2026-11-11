package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// claudeAnswer is what Anthropic's usage endpoint answered a Team seat, cut
// to what is read and the buckets left null beside it.
const claudeAnswer = `{"five_hour":{"utilization":40.0,"resets_at":"2026-11-10T22:19:59.585986+00:00","limit_dollars":null},
"seven_day":{"utilization":28.0,"resets_at":"2026-11-11T12:59:59.586012+00:00"},"seven_day_opus":null,"extra_usage":{"is_enabled":false},
"limits":[{"kind":"session","group":"session","percent":40,"resets_at":"2026-11-10T22:19:59.585986+00:00","scope":null},
{"kind":"weekly_all","group":"weekly","percent":28,"resets_at":"2026-11-11T12:59:59.586012+00:00","scope":null},
{"kind":"weekly_scoped","group":"weekly","percent":18,"resets_at":"2026-11-11T12:59:59.586232+00:00","scope":{"model":{"id":null,"display_name":"Fable"},"surface":null}}]}`

// codexAnswer is account/rateLimits/read's result for a free plan.
const codexAnswer = `{"rateLimits":{"limitId":"codex","primary":{"usedPercent":0,"windowDurationMins":43200,"resetsAt":1795769865},"secondary":null,"planType":"free"},
"rateLimitsByLimitId":{"codex":{"limitId":"codex","limitName":null,"primary":{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1795769865},
"secondary":{"usedPercent":51,"windowDurationMins":10080,"resetsAt":0},"planType":"plus"},
"spark":{"limitId":"spark","limitName":"GPT-Spark","primary":{"usedPercent":140,"windowDurationMins":10080,"resetsAt":1795769865},"secondary":null}}}`

// TestClaudeUsage holds what an account's sign-in gets from the endpoint: its
// windows and each model's own, the plan named by subscription and tier, and
// a token that lapsed or is missing never sent.
func TestClaudeUsage(t *testing.T) {
	asked, status := 0, http.StatusOK

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++

		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(claudeAnswer))
	}))
	defer srv.Close()

	claudeUsageURL = srv.URL

	t.Cleanup(func() { claudeUsageURL = "https://api.anthropic.com/api/oauth/usage" })

	dir := t.TempDir()
	env := []string{"CLAUDE_CONFIG_DIR=" + dir}
	login := func(token string, expires time.Time) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"`+token+`","expiresAt":`+
			strconv.FormatInt(expires.UnixMilli(), 10)+`,"subscriptionType":"team","rateLimitTier":"default_raven"}}`)
	}

	login("tok", time.Now().Add(time.Hour))
	mustWrite(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"userRateLimitTier":"default_claude_max_5x","organizationRateLimitTier":"default_raven"}}`)

	plan, limits, err := claudeUsage(context.Background(), env)
	if err != nil || plan != "Team 5x" {
		t.Fatalf("plan %q, err %v", plan, err)
	}

	got := make([]string, len(limits))
	for i, l := range limits {
		got[i] = l.Window + " " + l.Model + " " + strconv.FormatFloat(l.Percent, 'f', -1, 64)
	}

	if want := []string{"5h  40", "7d  28", "7d Fable 18"}; !slices.Equal(got, want) {
		t.Fatalf("limits %q, want %q", got, want)
	}

	if want := time.Date(2026, 11, 10, 22, 19, 59, 585986000, time.UTC); !limits[0].Resets.Equal(want) {
		t.Fatalf("resets %v, want %v", limits[0].Resets, want)
	}

	status = http.StatusUnauthorized

	if _, _, err := claudeUsage(context.Background(), env); !errors.Is(err, errSignedOut) {
		t.Fatalf("a refused sign-in: %v", err)
	}

	status = http.StatusTooManyRequests

	if _, _, err := claudeUsage(context.Background(), env); err == nil || !strings.HasPrefix(err.Error(), "busy") {
		t.Fatalf("a 429: %v", err)
	}

	asked = 0

	login("tok", time.Now().Add(-time.Hour))

	if plan, _, err := claudeUsage(context.Background(), env); !errors.Is(err, errPaused) || plan != "Team 5x" {
		t.Fatalf("a lapsed token: plan %q, err %v", plan, err)
	}

	login("", time.Time{})

	if _, _, err := claudeUsage(context.Background(), env); !errors.Is(err, errSignedOut) || asked != 0 {
		t.Fatalf("no token: err %v, asked %d times", err, asked)
	}

	// A refresh token that lapsed too: nothing renews it, the sign-in is gone.
	mustWrite(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1,"refreshTokenExpiresAt":2}}`)

	if _, _, err := claudeUsage(context.Background(), env); !errors.Is(err, errSignedOut) || asked != 0 {
		t.Fatalf("a lapsed refresh token: err %v, asked %d times", err, asked)
	}
}

// TestCodexUsageTimesOut is a codex that never answers, started by a launcher
// whose child holds the pipes, as the npm one does: out of time the question
// ends, and with it the lock every other check waits on.
func TestCodexUsageTimesOut(t *testing.T) {
	bin, home := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(bin, "codex"), "#!/bin/sh\n(while read -r line; do :; done; sleep 3) &\nwait\n")
	mustWrite(t, filepath.Join(home, "auth.json"), "{}")

	if err := os.Chmod(filepath.Join(bin, "codex"), 0o755); err != nil { // the test runs it
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		_, _, err := codexUsage(ctx, []string{"CODEX_HOME=" + home})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a codex that never answered gave limits")
		}

	case <-time.After(10 * time.Second):
		t.Fatal("the question outlived its time")
	}
}

func TestParseCodexLimits(t *testing.T) {
	plan, limits, err := parseCodexLimits([]byte(codexAnswer))
	if err != nil || plan != "Plus" {
		t.Fatalf("plan %q, err %v", plan, err)
	}

	want := []proto.Limit{
		{Window: "5h", Percent: 12.5, Resets: time.Unix(1795769865, 0)},
		{Window: "7d", Percent: 51},
		{Window: "7d", Model: "GPT-Spark", Percent: 100, Resets: time.Unix(1795769865, 0)},
	}
	if !reflect.DeepEqual(limits, want) {
		t.Fatalf("limits %+v\nwant   %+v", limits, want)
	}

	// An older server answers with the single bucket alone.
	if plan, limits, _ := parseCodexLimits([]byte(`{"rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":43200},"planType":"free"}}`)); plan != "Free" || len(limits) != 1 || limits[0].Window != "30d" {
		t.Fatalf("single bucket: plan %q, limits %+v", plan, limits)
	}
}

// TestRoomiest holds which account new sessions move to: none while the
// active one is under the threshold, else the one with room, one near its
// own limit and one whose numbers are stale after the others, never one
// nobody is signed in to.
func TestRoomiest(t *testing.T) {
	acct := func(name string, pct float64, note string) proto.Account {
		return proto.Account{Agent: "claude", Name: name, Env: []string{"D=" + name}, Limits: []proto.Limit{{Window: "7d", Percent: pct}}, Note: note}
	}

	for _, c := range []struct {
		what     string
		accounts []proto.Account
		want     string
	}{
		{"under the threshold", []proto.Account{acct("a", 94, ""), acct("b", 1, "")}, ""},
		{"the emptiest", []proto.Account{acct("a", 96, ""), acct("b", 40, ""), acct("c", 20, "")}, "c"},
		{"near its limit goes last", []proto.Account{acct("a", 96, ""), acct("b", 85, ""), acct("c", 60, "paused")}, "c"},
		{"stale after fresh", []proto.Account{acct("a", 96, ""), acct("b", 10, "paused"), acct("c", 50, "")}, "c"},
		{"signed out has no room", []proto.Account{acct("a", 96, ""), acct("b", 10, errSignedOut.Error())}, ""},
		{"all over", []proto.Account{acct("a", 96, ""), acct("b", 99, "")}, ""},
		{"another agent's", []proto.Account{acct("a", 96, ""), {Agent: "codex", Name: "b", Limits: []proto.Limit{{Percent: 1}}}}, ""},
		{"unknown active", []proto.Account{{Agent: "claude", Name: "a", Env: []string{"D=a"}}, acct("b", 1, "")}, ""},
	} {
		got, _ := roomiest(c.accounts, "claude", []string{"D=a"}, 95)
		if got.Name != c.want {
			t.Errorf("%s: moved to %q, want %q", c.what, got.Name, c.want)
		}
	}
}

// TestProfileSwitch holds profile_switch over a refresh: "notify" offers the
// account with room and leaves the active one, "auto" makes it the active
// one, and only a profile with a config of its own is an account.
func TestProfileSwitch(t *testing.T) {
	used := map[string]float64{"": 96, "D=b": 30}
	usageSources["sh"] = func(_ context.Context, env []string) (string, []proto.Limit, error) {
		if len(env) > 0 && env[0] == "D=out" {
			return "", nil, errSignedOut
		}

		return "Plan", []proto.Limit{{Window: "7d", Percent: used[strings.Join(env, " ")], Resets: time.Now().Add(time.Hour)}}, nil
	}

	t.Cleanup(func() { delete(usageSources, "sh") })

	d := start(t)()
	defer d.Close()

	d.mu.Lock()
	d.state.Agents = map[string][]string{"sh": {"sh"}}
	d.state.Settings.Profiles = map[string]map[string][]string{"sh": {"b": {"D=b"}, "out": {"D=out"}, "fast": {"MODEL=x"}}}
	d.state.ResumeEnv = map[string][]string{"sh": {"D"}}
	d.mu.Unlock()

	refresh := func() proto.Usage {
		t.Helper()

		d.mu.Lock()
		d.usageAt = time.Time{} // past the reuse window
		d.mu.Unlock()

		var u proto.Usage
		call(t, "usage.refresh", nil, &u)

		return u
	}
	active := func() []string {
		var st proto.State
		call(t, "state.get", nil, &st)

		return st.Active["sh"]
	}

	u := refresh()

	names := make([]string, len(u.Accounts))
	for i, a := range u.Accounts {
		names[i] = a.Name + ":" + a.Note
	}

	if want := []string{"default:", "b:", "out:signed out"}; !slices.Equal(names, want) {
		t.Fatalf("accounts %q, want %q", names, want)
	}

	if !slices.Equal(u.Offer["sh"], []string{"D=b"}) || active() != nil {
		t.Fatalf("notify: offer %v, active %v", u.Offer, active())
	}

	// Taking the offer ends it, and what was measured survives a restart's read.
	call(t, "state.set", map[string]any{"active_profile": map[string][]string{"sh": {"D=b"}}}, nil)

	u = proto.Usage{}
	call(t, "usage.get", nil, &u)

	if len(u.Offer) != 0 || !slices.Equal(active(), []string{"D=b"}) {
		t.Fatalf("after use: offer %v, active %v", u.Offer, active())
	}

	var kept proto.Usage
	if err := json.Unmarshal([]byte(mustRead(t, d.usagePath())), &kept); err != nil || len(kept.Accounts) != 3 {
		t.Fatalf("usage.json: %v, %+v", err, kept)
	}

	// A session whose caller names no env starts with the active profile; an
	// empty one is the default, said; a profile gone from the config is not
	// gone back to.
	ws := t.TempDir()
	started := func(params map[string]any) []string {
		t.Helper()

		params["workspace"], params["agent"] = ws, "sh"

		var s proto.Session
		call(t, "session.new", params, &s)
		call(t, "session.kill", map[string]string{"id": s.ID}, nil)

		return s.Env
	}

	if got := started(map[string]any{}); !slices.Equal(got, []string{"D=b"}) {
		t.Fatalf("no env named: started with %v", got)
	}

	if got := started(map[string]any{"env": []string{}}); len(got) != 0 {
		t.Fatalf("the default named: started with %v", got)
	}

	call(t, "state.set", map[string]any{"active_profile": map[string][]string{"sh": {"D=gone"}}}, nil)

	if got := started(map[string]any{}); len(got) != 0 {
		t.Fatalf("a profile no longer there: started with %v", got)
	}

	// "auto" turned on moves at once; a profile picked by hand stands until
	// the next refresh, which moves off it again.
	call(t, "state.set", map[string]any{"active_profile": map[string][]string{"sh": {}}}, nil)
	call(t, "state.set", map[string]any{"settings": map[string]any{"profile_switch": "auto"}}, nil)

	u = proto.Usage{}
	call(t, "usage.get", nil, &u)

	if len(u.Offer) != 0 || !slices.Equal(active(), []string{"D=b"}) {
		t.Fatalf("auto turned on: offer %v, active %v", u.Offer, active())
	}

	call(t, "state.set", map[string]any{"active_profile": map[string][]string{"sh": {}}}, nil)

	if active() != nil {
		t.Fatalf("a profile picked by hand was moved off at once: %v", active())
	}

	if u = refresh(); len(u.Offer) != 0 || !slices.Equal(active(), []string{"D=b"}) {
		t.Fatalf("auto: offer %v, active %v", u.Offer, active())
	}

	if err := proto.Call("session.profile", proto.SessionProfileParams{ID: "x", Env: []string{"=b"}}, nil); err == nil || !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Fatalf("an env entry without a key: %v", err)
	}

	// Off, nobody is asked, and what was measured once offers nothing.
	call(t, "state.set", map[string]any{"active_profile": map[string][]string{"sh": {}}, "settings": map[string]any{"profile_switch": "notify"}}, nil)

	u = proto.Usage{}
	call(t, "usage.get", nil, &u)

	if !slices.Equal(u.Offer["sh"], []string{"D=b"}) {
		t.Fatalf("notify again: offer %v", u.Offer)
	}

	call(t, "state.set", map[string]any{"settings": map[string]any{"usage": false}}, nil)

	used["D=b"] = 99

	if u = refresh(); u.Accounts[1].Limits[0].Percent != 30 || len(u.Offer) != 0 {
		t.Fatalf("with usage off: %+v, offer %v", u.Accounts[1], u.Offer)
	}
}

// TestMeasureKeepsLast is a failed check: the last limits stay, a window that
// has reset since back at zero, with the reason beside them.
func TestMeasureKeepsLast(t *testing.T) {
	usageSources["sh"] = func(context.Context, []string) (string, []proto.Limit, error) { return "", nil, errPaused }

	t.Cleanup(func() { delete(usageSources, "sh") })

	now := time.Now()
	prev := []proto.Account{{Agent: "sh", Plan: "Max", Checked: now.Add(-time.Hour), Limits: []proto.Limit{
		{Window: "5h", Percent: 80, Resets: now.Add(-time.Minute)},
		{Window: "7d", Percent: 60, Resets: now.Add(time.Hour)},
	}}}

	got := measure(context.Background(), proto.Account{Agent: "sh"}, prev, now)
	if got.Note != errPaused.Error() || got.Plan != "Max" || !got.Checked.Equal(prev[0].Checked) {
		t.Fatalf("kept %+v", got)
	}

	if got.Limits[0].Percent != 0 || !got.Limits[0].Resets.IsZero() || got.Limits[1].Percent != 60 {
		t.Fatalf("limits %+v", got.Limits)
	}

	if prev[0].Limits[0].Percent != 80 {
		t.Fatal("the last answer was changed in place")
	}
}

// TestProfileSession moves a session's agent to another config: the agent
// ends, its transcript is linked under the same project there, and the same
// conversation continues in it, the session's env following.
func TestProfileSession(t *testing.T) {
	needProc(t)

	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // the daemon's

	conversations["myagent"] = claudeSource{}

	t.Cleanup(func() { delete(conversations, "myagent") })

	work, other := filepath.Join(t.TempDir(), "work"), filepath.Join(t.TempDir(), "other")

	d := start(t)()
	defer d.Close()

	ws := t.TempDir()
	agent := filepath.Join(ws, "myagent")
	mustWrite(t, agent, "#!/bin/sh\necho ${1:-AGENT} $2 IN \"$CLAUDE_CONFIG_DIR\"\nsleep 300\n")

	if err := os.Chmod(agent, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", agent, err)
	}

	d.mu.Lock()
	d.state.ResumeID = map[string][]string{"myagent": {agent, "RESUMED", "{id}"}}
	d.state.ResumeEnv = map[string][]string{"myagent": {"CLAUDE_CONFIG_DIR"}}
	d.state.Settings.Profiles = map[string]map[string][]string{"myagent": {"work": {"CLAUDE_CONFIG_DIR=" + work, "MODEL=haiku"}}}
	d.mu.Unlock()

	screen := func(id string) string {
		t.Helper()

		var scr proto.Screen
		call(t, "session.screen", proto.ScreenParams{ID: id, Cols: 100, Rows: 8}, &scr)

		return strings.Join(scr.Lines, "\n")
	}

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{agent}, "env": []string{"X=1", "MODEL=haiku", "CLAUDE_CONFIG_DIR=" + work}}, &s)
	waitFor(t, "the agent to start in its config", func() bool { return strings.Contains(screen(s.ID), "AGENT IN "+work) })

	if err := proto.Call("session.profile", proto.SessionProfileParams{ID: s.ID, Env: []string{"CLAUDE_CONFIG_DIR=" + other}}, nil); err == nil {
		t.Fatal("a session with no conversation known was moved")
	}

	// publish says the session's agent has the conversation open, in the
	// config it runs in, as Claude Code does.
	publish := func(conf string) {
		t.Helper()

		pid := d.sessions[s.ID].cmd.Process.Pid

		b, err := json.Marshal(claudeProcess{PID: pid, SessionID: "conv-m", ProcStart: startTime(pid)})
		if err != nil {
			t.Fatal(err)
		}

		mustWrite(t, filepath.Join(conf, "sessions", strconv.Itoa(pid)+".json"), string(b))
	}

	transcript := filepath.Join(work, "projects", "-ws", "conv-m.jsonl")
	publish(work)
	mustWrite(t, transcript, "{}\n")
	mustWrite(t, filepath.Join(work, "projects", "-ws", "conv-m", "subagents", "a.json"), "{}\n")

	var moved proto.Session
	call(t, "session.profile", proto.SessionProfileParams{ID: s.ID, Env: []string{"CLAUDE_CONFIG_DIR=" + other}}, &moved)
	waitFor(t, "the conversation to continue in the other config", func() bool {
		return strings.Contains(screen(s.ID), "RESUMED conv-m IN "+other)
	})

	// What the profile it left set goes with that profile; the caller's own stays.
	if want := []string{"X=1", "CLAUDE_CONFIG_DIR=" + other}; !slices.Equal(moved.Env, want) || moved.Asleep {
		t.Fatalf("moved %+v, want env %v", moved.SessionSpec, want)
	}

	for _, name := range []string{"conv-m.jsonl", filepath.Join("conv-m", "subagents", "a.json")} {
		if got := mustRead(t, filepath.Join(other, "projects", "-ws", name)); got != "{}\n" {
			t.Fatalf("%s in the other config reads %q", name, got)
		}
	}

	if dst, err := os.Readlink(filepath.Join(other, "projects", "-ws", "conv-m.jsonl")); err != nil || dst != transcript {
		t.Fatalf("the transcript there is %q, %v: want a link to %s", dst, err, transcript)
	}

	// Back again: the transcript is where it was, nothing to link.
	publish(other)
	call(t, "session.profile", proto.SessionProfileParams{ID: s.ID, Env: []string{"CLAUDE_CONFIG_DIR=" + work}}, &moved)
	waitFor(t, "the conversation to continue where it began", func() bool {
		return strings.Contains(screen(s.ID), "RESUMED conv-m IN "+work)
	})

	if fi, err := os.Lstat(transcript); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("the transcript itself: %v, %v", fi, err)
	}

	// Asleep, it stays so: no sleep or wake tells an attached TUI it moved,
	// the move itself does.
	publish(work)
	call(t, "session.sleep", map[string]any{"id": s.ID}, nil)

	events := subscribed(t)
	call(t, "session.profile", proto.SessionProfileParams{ID: s.ID, Env: []string{"CLAUDE_CONFIG_DIR=" + other}}, &moved)

	if !moved.Asleep {
		t.Fatal("an asleep session woke to move")
	}

	for timeout := time.After(5 * time.Second); ; {
		select {
		case ev := <-events:
			if ev.Kind != "sessions" {
				continue
			}

		case <-timeout:
			t.Fatal("no sessions event for an asleep session's move")
		}

		break
	}
}

// TestProfileSessionInShell moves an agent started in a shell, twice: the
// profile rides on the line typed into the shell, never on the shell, so the
// conversation is still found where the agent runs for the second move.
func TestProfileSessionInShell(t *testing.T) {
	needProc(t)

	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base) // the daemon's and the shell's: the default

	conversations["myagent"] = claudeSource{}

	t.Cleanup(func() { delete(conversations, "myagent") })

	work, other := filepath.Join(t.TempDir(), "work"), filepath.Join(t.TempDir(), "other")

	d := start(t)()
	defer d.Close()

	ws := t.TempDir()
	agent := filepath.Join(ws, "myagent")
	mustWrite(t, agent, "#!/bin/sh\necho ${1:-AGENT} $2 IN \"$CLAUDE_CONFIG_DIR\"\nsleep 300\n")

	if err := os.Chmod(agent, 0o755); err != nil { // the test runs it
		t.Fatalf("chmod %s: %v", agent, err)
	}

	d.mu.Lock()
	d.state.ResumeID = map[string][]string{"myagent": {agent, "RESUMED", "{id}"}}
	d.state.ResumeEnv = map[string][]string{"myagent": {"CLAUDE_CONFIG_DIR"}}
	d.mu.Unlock()

	var s proto.Session
	call(t, "session.new", map[string]any{"workspace": ws, "cmd": []string{"sh"}}, &s)
	call(t, "session.wait", proto.WaitParams{ID: s.ID, Timeout: 30000}, nil) // its prompt, before a line is typed at it

	// running waits for the agent in front of the shell saying want, and
	// publishes that it has the conversation open in conf.
	running := func(want, conf string) {
		t.Helper()

		var pid int

		waitFor(t, "the agent to say "+want, func() bool {
			var scr proto.Screen
			call(t, "session.screen", proto.ScreenParams{ID: s.ID, Cols: 100, Rows: 8}, &scr)

			var prog string

			d.mu.Lock()
			pid, prog = d.sessions[s.ID].foreground()
			d.mu.Unlock()

			return prog == "myagent" && strings.Contains(strings.Join(scr.Lines, "\n"), want)
		})

		b, err := json.Marshal(claudeProcess{PID: pid, SessionID: "conv-s", ProcStart: startTime(pid)})
		if err != nil {
			t.Fatal(err)
		}

		mustWrite(t, filepath.Join(conf, "sessions", strconv.Itoa(pid)+".json"), string(b))
	}

	call(t, "session.input", proto.InputParams{ID: s.ID, Text: "CLAUDE_CONFIG_DIR='" + work + "' ./myagent\r"}, nil)
	running("AGENT IN "+work, work)
	mustWrite(t, filepath.Join(work, "projects", "-ws", "conv-s.jsonl"), "{}\n")

	var moved proto.Session
	call(t, "session.profile", proto.SessionProfileParams{ID: s.ID, Env: []string{"CLAUDE_CONFIG_DIR=" + other}}, &moved)
	running("RESUMED conv-s IN "+other, other)

	if len(moved.Env) != 0 {
		t.Fatalf("the shell got the profile: %v", moved.Env)
	}

	call(t, "session.profile", proto.SessionProfileParams{ID: s.ID, Env: nil}, &moved)
	running("RESUMED conv-s IN "+base, base)

	if got := mustRead(t, filepath.Join(base, "projects", "-ws", "conv-s.jsonl")); got != "{}\n" {
		t.Fatalf("the transcript in the default config reads %q", got)
	}
}

func FuzzParseClaudeUsage(f *testing.F) {
	f.Add([]byte(claudeAnswer))
	f.Add([]byte(`{"five_hour":{"utilization":-1},"seven_day":{"utilization":1e99},"limits":[{"kind":"x","percent":7,"scope":{"model":{"id":"m"}}}]}`))
	f.Add([]byte(`{"limits":[{"scope":{"model":{"display_name":" "}}}]}`))

	f.Fuzz(func(t *testing.T, b []byte) {
		limits, err := parseClaudeUsage(b)
		if err == nil && len(limits) == 0 {
			t.Fatal("no limits and no error")
		}

		checkLimits(t, limits)
	})
}

func FuzzParseCodexLimits(f *testing.F) {
	f.Add([]byte(codexAnswer))
	f.Add([]byte(`{"rateLimits":{"primary":{"usedPercent":-3,"windowDurationMins":-1}}}`))
	f.Add([]byte(`{"rateLimitsByLimitId":{"a":{"primary":{"usedPercent":1e400}}}}`))

	f.Fuzz(func(t *testing.T, b []byte) {
		_, limits, _ := parseCodexLimits(b)
		checkLimits(t, limits)
	})
}

// checkLimits is what a view relies on: a share it can draw, in a window it
// can name.
func checkLimits(t *testing.T, limits []proto.Limit) {
	t.Helper()

	for _, l := range limits {
		if !(l.Percent >= 0 && l.Percent <= 100) || l.Window == "" {
			t.Fatalf("limit %+v", l)
		}
	}
}
