package daemon

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// usageEvery is how often the daemon asks each agent's service for its
// limits. Anthropic's endpoint answers 429 readily, so no sooner.
const usageEvery = 15 * time.Minute

// usageReuse is how long an answer stands in for the next one asked for: a
// tab opened and shut again is not a request each time.
const usageReuse = 15 * time.Second

// usageTimeout bounds one account's question: a request, or a codex
// app-server started and answered.
const usageTimeout = 10 * time.Second

// nearLimit is how far under profile_switch_at an account stops being a good
// one to move to: it would be over soon itself.
const nearLimit = 15

// Why an account's limits are not current. A lapsed access token is routine:
// only the agent's own CLI renews it, the next time it runs. A busy service
// is routine too and says nothing worth showing: the next check asks again.
var (
	errPaused    = errors.New("paused: its sign-in lapsed, the agent renews it when it next runs")
	errSignedOut = errors.New("signed out")
	errBusy      = errors.New("busy: Anthropic limits how often usage is asked for")
)

// usageSources ask an agent's service for the limits of the account env
// picks, by [agents] preset. plan is known without the service, so it comes
// back with an error too.
var usageSources = map[string]func(ctx context.Context, env []string) (plan string, limits []proto.Limit, err error){
	"claude": claudeUsage,
	"codex":  codexUsage,
}

// claudeUsageURL is the endpoint Claude Code's own /usage reads; tests point
// it at a server of theirs.
var claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

// WatchUsage asks for the limits at startup and every usageEvery after, while
// usage is on, until the daemon closes. `pando serve` starts it; a daemon a
// test drives directly never reaches for the network on its own, nor looks
// through the home directory for configs.
func (d *Daemon) WatchUsage() {
	d.mu.Lock()
	d.finds = true
	d.mu.Unlock()

	for {
		d.refreshUsage()

		select {
		case <-d.ctx.Done():
			return
		case <-time.After(usageEvery):
		}
	}
}

// usageNow is what the daemon knows now, without asking anyone.
func (d *Daemon) usageNow() proto.Usage {
	d.mu.Lock()
	defer d.mu.Unlock()

	return proto.Usage{Accounts: slices.Clone(d.usage.Accounts), Offer: maps.Clone(d.usage.Offer)}
}

// refreshUsage asks every account's service again, moves the active profile
// where profile_switch says to, and tells the subscribers. An answer younger
// than usageReuse stands, however it was asked for; with usage off nobody is
// asked.
func (d *Daemon) refreshUsage() proto.Usage {
	d.usageMu.Lock() // one at a time: a codex account starts a process
	defer d.usageMu.Unlock()

	d.mu.Lock()
	prev, off := d.usage.Accounts, !d.state.Settings.Usage
	fresh := time.Since(d.usageAt) < usageReuse
	d.mu.Unlock()

	if fresh {
		return d.usageNow()
	}

	if d.findConfigs() { // with usage off too: the profile picker lists them
		d.broadcast(proto.Event{Kind: "state"})
	}

	if off {
		return d.usageNow()
	}

	d.mu.Lock()
	accounts := d.accounts()
	d.mu.Unlock()

	now := time.Now()
	for i, a := range accounts {
		accounts[i] = measure(d.ctx, a, prev, now)
	}

	d.mu.Lock()
	d.usage.Accounts, d.usageAt = accounts, now
	moved := d.advise(true)

	if moved {
		_ = d.save() // best effort: the next change writes it again
	}

	b, _ := json.Marshal(d.usage) // strings, numbers and times
	_ = writeFile(d.usagePath(), b)
	d.mu.Unlock()

	d.broadcast(proto.Event{Kind: "usage", Data: b})

	if moved {
		d.broadcast(proto.Event{Kind: "state"})
	}

	return d.usageNow()
}

// findConfigs looks for the agents' config directories, under the home
// directory and in what runs, and keeps the new ones in d.state.Seen; it
// reports any. A look younger than usageReuse stands: with usage off nothing
// else spaces them.
func (d *Daemon) findConfigs() bool {
	d.mu.Lock()

	finds := d.finds && time.Since(d.foundAt) >= usageReuse
	if finds {
		d.foundAt = time.Now()
	}
	d.mu.Unlock()

	home, err := os.UserHomeDir()
	if !finds || err != nil {
		return false
	}

	if target, err := filepath.EvalSymlinks(home); err == nil { // a home that is a link: the walk would stop at it
		home = target
	}

	found := configsRunning() // first: of two directories with one account, the one in use is the config
	for agent, dirs := range configsUnder(home) {
		found[agent] = append(found[agent], dirs...)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	saw := d.discover(home, found)
	if saw {
		_ = d.save() // best effort: the next change writes it again
	}

	return saw
}

// usagePath keeps the last limits over a restart: a paused account has
// nothing else to show, and a restart is no reason to ask everyone again.
func (d *Daemon) usagePath() string { return filepath.Join(d.dataDir, "usage.json") }

// accounts are the sign-ins to ask about, limits not yet in: per agent with a
// source that is installed, its default, its [profiles] that pick a config
// of their own ([resume_env]), then each config pando saw it run in that none
// of them sets. A profile that sets anything else runs on the default's
// sign-in. d.mu is held.
func (d *Daemon) accounts() []proto.Account {
	var out []proto.Account

	for _, agent := range slices.Sorted(maps.Keys(usageSources)) {
		argv := d.state.Agents[agent]
		if len(argv) == 0 {
			continue
		}

		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}

		out = append(out, proto.Account{Agent: agent, Name: "default"})

		profiles, keys := d.state.Settings.Profiles[agent], d.state.ResumeEnv[agent]
		for _, name := range slices.Sorted(maps.Keys(profiles)) {
			own := slices.ContainsFunc(profiles[name], func(kv string) bool {
				k, _, _ := strings.Cut(kv, "=")
				return slices.Contains(keys, k)
			})
			if own {
				out = append(out, proto.Account{Agent: agent, Name: name, Env: homeEnv(profiles[name])})
			}
		}

		for _, env := range d.state.Seen[agent] {
			if _, ok := account(out, agent, env); !ok && len(env) > 0 {
				_, dir, _ := strings.Cut(env[0], "=")
				out = append(out, proto.Account{Agent: agent, Name: filepath.Base(dir), Env: env})
			}
		}
	}

	return out
}

// account is agent's account that env picks, the default for none.
func account(accounts []proto.Account, agent string, env []string) (proto.Account, bool) {
	i := slices.IndexFunc(accounts, func(a proto.Account) bool { return a.Agent == agent && slices.Equal(a.Env, env) })
	if i < 0 {
		return proto.Account{}, false
	}

	return accounts[i], true
}

// homeEnv is KEY=VALUE env with a leading ~ in each value expanded, as a
// shell does: a profile in config.toml is written with one.
func homeEnv(env []string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return env
	}

	out := make([]string, len(env))
	for i, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if rest, ok := strings.CutPrefix(v, "~"); ok {
			v = filepath.Join(home, rest)
		}

		out[i] = k + "=" + v
	}

	return out
}

// measure asks account a's service for its limits at now. A failed check
// keeps what prev had for it, a window that has reset since back at zero, and
// says why in Note, but for a busy service.
func measure(ctx context.Context, a proto.Account, prev []proto.Account, now time.Time) proto.Account {
	if old, ok := account(prev, a.Agent, a.Env); ok {
		a.Plan, a.Limits, a.Checked = old.Plan, slices.Clone(old.Limits), old.Checked
	}

	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()

	plan, limits, err := usageSources[a.Agent](ctx, a.Env)
	a.Plan = cmp.Or(plan, a.Plan)

	if err != nil {
		if !errors.Is(err, errBusy) {
			a.Note = err.Error()
		}

		for i, l := range a.Limits {
			if !l.Resets.IsZero() && l.Resets.Before(now) {
				a.Limits[i].Percent, a.Limits[i].Resets = 0, time.Time{}
			}
		}

		return a
	}

	a.Limits, a.Checked = limits, now

	return a
}

// advise sets usage.Offer: per agent whose active account is at or over
// profile_switch_at, the account with the most room. With profile_switch
// "auto" and move it makes that one active instead, and reports whether any
// did. Only the active account crossing the threshold starts anything, so
// auto never flaps back to an account that just reset. d.mu is held.
func (d *Daemon) advise(move bool) bool {
	s := d.state.Settings
	at := float64(min(max(s.SwitchAt, 50), 100))
	d.usage.Offer = nil

	if !s.Usage || s.Switch != "notify" && s.Switch != "auto" { // off: what was measured once says nothing now
		return false
	}

	moved := false

	for agent := range usageSources {
		active := d.state.Active[agent]
		if _, ok := account(d.usage.Accounts, agent, active); !ok { // a profile gone since: sessions start on the default (activeEnv)
			active = nil
		}

		to, ok := roomiest(d.usage.Accounts, agent, active, at)

		switch {
		case !ok:
		case s.Switch == "notify":
			if d.usage.Offer == nil {
				d.usage.Offer = map[string][]string{}
			}

			d.usage.Offer[agent] = list(to.Env)

		case move:
			if d.state.Active == nil {
				d.state.Active = map[string][]string{}
			}

			d.state.Active[agent], moved = to.Env, true
		}
	}

	return moved
}

// roomiest is agent's account worth moving new sessions to: the active one
// is at or over at percent of a limit and this one is under it. One nearLimit
// points under at or closer comes after the others, a stale answer after one
// measured now, then the emptiest, then the one that resets first. An account
// nobody is signed in to has no room a session could use.
func roomiest(accounts []proto.Account, agent string, active []string, at float64) (proto.Account, bool) {
	current, ok := account(accounts, agent, active)
	if peak, known := current.Peak(); !ok || !known || peak < at {
		return proto.Account{}, false
	}

	var room []proto.Account

	for _, a := range accounts {
		peak, known := a.Peak()
		if a.Agent == agent && !slices.Equal(a.Env, active) && known && peak < at && a.Note != errSignedOut.Error() {
			room = append(room, a)
		}
	}

	if len(room) == 0 {
		return proto.Account{}, false
	}

	rank := func(a proto.Account) (near, stale int, peak float64, reset time.Time) {
		peak, _ = a.Peak()

		for _, l := range a.Limits {
			if !l.Resets.IsZero() && (reset.IsZero() || l.Resets.Before(reset)) {
				reset = l.Resets
			}
		}

		return b2i(peak >= at-nearLimit), b2i(a.Note != ""), peak, reset
	}

	best := slices.MinFunc(room, func(a, b proto.Account) int {
		an, as, ap, ar := rank(a)
		bn, bs, bp, br := rank(b)

		return cmp.Or(cmp.Compare(an, bn), cmp.Compare(as, bs), cmp.Compare(ap, bp), ar.Compare(br))
	})

	return best, true
}

func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}

// activeEnv is the profile a session of agent starts with when its caller
// names none: State.Active's, while it is still one of the agent's accounts.
// A profile since dropped from config.toml is not gone back to.
func (d *Daemon) activeEnv(agent string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	env := d.state.Active[agent]
	if _, ok := account(d.accounts(), agent, env); !ok {
		return nil
	}

	return env
}

// setActive makes the profiles in patch the ones new sessions start with, an
// empty one an agent's default again, and offers anew. d.mu is held.
func (d *Daemon) setActive(patch map[string][]string) {
	if d.state.Active == nil {
		d.state.Active = map[string][]string{}
	}

	for agent, env := range patch {
		if len(env) == 0 {
			delete(d.state.Active, agent)
		} else {
			d.state.Active[agent] = env
		}
	}

	d.advise(false)
}

// window names a limit's span as the views show it: 300 minutes is "5h",
// 10080 "7d".
func window(minutes int) string {
	switch {
	case minutes <= 0:
		return "limit"
	case minutes%(24*60) == 0:
		return strconv.Itoa(minutes/(24*60)) + "d"
	case minutes%60 == 0:
		return strconv.Itoa(minutes/60) + "h"
	}

	return strconv.Itoa(minutes) + "m"
}

// percent is v as a share used, 0 to 100; false for what is no number.
func percent(v float64) (float64, bool) { return min(v, 100), v >= 0 }

// claudeUsage asks Anthropic's OAuth usage endpoint with the access token
// Claude Code keeps in the config directory env picks. The token is only
// read: Claude Code alone renews it.
//
// shortcut: reads .credentials.json only; macOS keeps the token in the
// Keychain, so an account there reads signed out. Read it with `security`
// when someone on macOS asks.
func claudeUsage(ctx context.Context, env []string) (string, []proto.Limit, error) {
	dir := claudeDirIn(env)

	var creds struct {
		Login struct {
			Token   string `json:"accessToken"`
			Expires int64  `json:"expiresAt"` // epoch milliseconds
			Renews  int64  `json:"refreshTokenExpiresAt"`
			Plan    string `json:"subscriptionType"`
			Tier    string `json:"rateLimitTier"`
		} `json:"claudeAiOauth"`
	}

	b, _ := os.ReadFile(filepath.Join(dir, ".credentials.json")) // none reads as signed out
	_ = json.Unmarshal(b, &creds)
	login := creds.Login
	plan := claudePlan(login.Plan, cmp.Or(claudeTier(dir, env), login.Tier))

	now := time.Now().UnixMilli()

	switch {
	case login.Token == "", login.Renews > 0 && login.Renews <= now: // nothing left to renew it with
		return plan, nil, errSignedOut
	case login.Expires > 0 && login.Expires <= now:
		return plan, nil, errPaused
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, http.NoBody)
	if err != nil {
		return plan, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+login.Token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")

	// The token goes to the one host it was issued for: a redirect is refused.
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Do(req)
	if err != nil {
		return plan, nil, errors.New("unreachable: " + err.Error())
	}

	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return plan, nil, errSignedOut
	case resp.StatusCode == http.StatusTooManyRequests:
		return plan, nil, errBusy
	case resp.StatusCode != http.StatusOK:
		return plan, nil, fmt.Errorf("unavailable: Anthropic answered %s", resp.Status)
	}

	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return plan, nil, errors.New("unavailable: " + err.Error())
	}

	limits, err := parseClaudeUsage(body)

	return plan, limits, err
}

// parseClaudeUsage reads the endpoint's answer: the five-hour and seven-day
// buckets every model draws from, then each model's own allowance, which only
// the limits array names.
func parseClaudeUsage(b []byte) ([]proto.Limit, error) {
	type bucket struct {
		Used   *float64  `json:"utilization"` // a percentage: 0.6 is 0.6%
		Resets time.Time `json:"resets_at"`
	}

	var v struct {
		Session *bucket `json:"five_hour"`
		Week    *bucket `json:"seven_day"`
		Limits  []struct {
			Kind   string    `json:"kind"` // "weekly_scoped", "session"
			Used   *float64  `json:"percent"`
			Resets time.Time `json:"resets_at"`
			Scope  *struct {
				Model *struct {
					Name string `json:"display_name"`
					ID   string `json:"id"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, errors.New("unavailable: " + err.Error())
	}

	var out []proto.Limit

	add := func(window, model string, used *float64, resets time.Time) {
		if used == nil {
			return
		}

		if p, ok := percent(*used); ok {
			out = append(out, proto.Limit{Window: window, Model: model, Percent: p, Resets: resets})
		}
	}

	if v.Session != nil {
		add("5h", "", v.Session.Used, v.Session.Resets)
	}

	if v.Week != nil {
		add("7d", "", v.Week.Used, v.Week.Resets)
	}

	for _, l := range v.Limits {
		if l.Scope == nil || l.Scope.Model == nil {
			continue
		}

		model := strings.TrimSpace(cmp.Or(l.Scope.Model.Name, l.Scope.Model.ID))
		if model == "" {
			continue
		}

		window := "5h"
		if strings.Contains(l.Kind, "week") {
			window = "7d"
		}

		add(window, model, l.Used, l.Resets)
	}

	if len(out) == 0 {
		return nil, errors.New("unavailable: Anthropic named no limits")
	}

	return out, nil
}

// claudeTier is the rate-limit tier Claude Code's own profile states for the
// config directory dir. The credentials keep the tier their token was minted
// with, which an upgrade does not restamp; the profile follows the account.
// A seat with no tier of its own is limited by its organization's.
func claudeTier(dir string, env []string) string {
	path := filepath.Join(dir, ".claude.json")
	named := os.Getenv("CLAUDE_CONFIG_DIR") != "" || slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") })

	if home, err := os.UserHomeDir(); err == nil && !named {
		path = filepath.Join(home, ".claude.json") // the directory nothing names keeps its profile beside it
	}

	var v struct {
		Account struct {
			User string `json:"userRateLimitTier"`
			Org  string `json:"organizationRateLimitTier"`
		} `json:"oauthAccount"`
	}

	b, _ := os.ReadFile(path) // none: the credentials' tier stands
	_ = json.Unmarshal(b, &v)

	tier := cmp.Or(v.Account.User, v.Account.Org)
	if !maxTier.MatchString(tier) { // only one naming a multiplier says more than the credentials do
		return ""
	}

	return tier
}

var maxTier = regexp.MustCompile(`max_(\d+x)`)

// claudePlan names the plan: the subscription first, the tier's multiplier
// after it, so a Team seat on a Max ceiling reads "Team 5x", not Max.
func claudePlan(subscription, tier string) string {
	plan := title(subscription)

	m := maxTier.FindStringSubmatch(tier)
	if m == nil {
		return plan
	}

	return cmp.Or(plan, "Max") + " " + m[1]
}

// title is s with its first letter in upper case and _ as spaces: a plan as
// a service's API spells it.
func title(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return ""
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// codexUsage asks the Codex CLI's own app-server, started in the home env
// picks so it answers for that sign-in and renews its token there.
func codexUsage(ctx context.Context, env []string) (string, []proto.Limit, error) {
	home := ""

	for _, kv := range append([]string{"CODEX_HOME=" + os.Getenv("CODEX_HOME")}, env...) {
		if v, ok := strings.CutPrefix(kv, "CODEX_HOME="); ok && v != "" {
			home = v
		}
	}

	if dir, err := os.UserHomeDir(); home == "" && err == nil {
		home = filepath.Join(dir, ".codex")
	}

	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil { // no sign-in: the server would only say so
		return "", nil, errSignedOut
	}

	cmd := exec.CommandContext(ctx, "codex", "-s", "read-only", "-a", "on-request", "app-server")
	cmd.WaitDelay = time.Second

	cmd.Env = append(os.Environ(), env...)

	in, err := cmd.StdinPipe()
	if err != nil {
		return "", nil, err
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}

	// codex is often a launcher whose child, the server itself, holds these
	// pipes too: killing the launcher alone leaves the read below waiting on
	// it for good. Out of time, both ends close, which ends the read here
	// and, its input gone, the server there.
	cmd.Cancel = func() error {
		_ = in.Close()
		_ = out.Close()

		return cmd.Process.Kill()
	}

	if err := cmd.Start(); err != nil {
		return "", nil, errors.New("unavailable: " + err.Error())
	}

	// The server leaves when its input ends, answered or not: it stays open
	// until the answer is read, then the server is stopped.
	defer func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	_, err = in.Write([]byte(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"pando","version":"1"}}}` + "\n" +
		`{"method":"initialized","params":{}}` + "\n" +
		`{"id":2,"method":"account/rateLimits/read"}` + "\n"))
	if err != nil {
		return "", nil, errors.New("unavailable: " + err.Error())
	}

	lines := bufio.NewScanner(out)
	lines.Buffer(nil, 1<<20)

	for lines.Scan() {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(lines.Bytes(), &msg) != nil || msg.ID != 2 {
			continue // its notifications, and the answer to initialize
		}

		switch {
		case msg.Error != nil && strings.Contains(strings.ToLower(msg.Error.Message), "auth"):
			return "", nil, errSignedOut
		case msg.Error != nil:
			return "", nil, errors.New("unavailable: " + msg.Error.Message)
		}

		return parseCodexLimits(msg.Result)
	}

	return "", nil, errors.New("unavailable: codex app-server did not answer")
}

// parseCodexLimits reads account/rateLimits/read's result: every metered
// bucket, the subscription's own first, any other a model's allowance.
// rateLimits is the single-bucket view older servers answer with.
func parseCodexLimits(b []byte) (string, []proto.Limit, error) {
	type span struct {
		Used    *float64 `json:"usedPercent"`
		Minutes int      `json:"windowDurationMins"`
		Resets  int64    `json:"resetsAt"` // epoch seconds
	}

	type bucket struct {
		Name      string `json:"limitName"`
		Plan      string `json:"planType"`
		Primary   *span  `json:"primary"`
		Secondary *span  `json:"secondary"`
	}

	var v struct {
		One  *bucket           `json:"rateLimits"`
		ByID map[string]bucket `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", nil, errors.New("unavailable: " + err.Error())
	}

	if len(v.ByID) == 0 && v.One != nil {
		v.ByID = map[string]bucket{"codex": *v.One}
	}

	ids := slices.SortedFunc(maps.Keys(v.ByID), func(a, b string) int {
		return cmp.Or(cmp.Compare(b2i(a != "codex"), b2i(b != "codex")), cmp.Compare(a, b))
	})

	var (
		plan string
		out  []proto.Limit
	)

	for _, id := range ids {
		bk := v.ByID[id]
		plan = cmp.Or(plan, title(bk.Plan))

		model := cmp.Or(bk.Name, id)
		if id == "codex" {
			model = ""
		}

		for _, s := range []*span{bk.Primary, bk.Secondary} {
			if s == nil || s.Used == nil {
				continue
			}

			p, ok := percent(*s.Used)
			if !ok {
				continue
			}

			l := proto.Limit{Window: window(s.Minutes), Model: model, Percent: p}
			if s.Resets > 0 {
				l.Resets = time.Unix(s.Resets, 0)
			}

			out = append(out, l)
		}
	}

	return plan, out, nil
}

// profileSession moves session id's agent to the profile env picks, none for
// its default: the agent ends and continues the same conversation there, as
// a sleep and a wake do. Only an agent whose open conversation pando can
// name ([resume_id]) can follow it into another config. A session asleep
// stays asleep, and wakes there.
func (d *Daemon) profileSession(id string, env []string) (proto.Session, error) {
	s, err := d.session(id)
	if err != nil {
		return proto.Session{}, err
	}

	asleep := s.isAsleep()

	d.mu.Lock()
	if d.moving[s.spec.ID] {
		d.mu.Unlock()
		return s.info(), fmt.Errorf("session %s is going to sleep or waking", s.spec.ID)
	}

	if !asleep { // an agent started since the last tick counts, as for a sleep
		pid, prog := s.foreground()
		d.remember(s, pid, prog)
	}

	info := s.info()
	c := info.Conversation
	known := c != nil && conversations[c.Agent] != nil && len(d.state.ResumeID[c.Agent]) > 0
	d.mu.Unlock()

	if !known {
		return info, fmt.Errorf("session %s runs no agent whose conversation pando can continue elsewhere", info.ID)
	}

	// Before the agent ends: a transcript that cannot be linked leaves it running.
	if err := conversations[c.Agent].share(*c, env); err != nil {
		return info, fmt.Errorf("the conversation stays where it is: %w", err)
	}

	if !asleep {
		if err := d.sleepSession(info.ID, false); err != nil {
			return s.info(), err
		}
	}

	d.mu.Lock()
	// Woken meanwhile, by a key in the TUI: another process has the
	// conversation again, and this session is no longer the one listed.
	if d.sessions[info.ID] != s || !s.isAsleep() || d.moving[info.ID] {
		d.mu.Unlock()
		return s.info(), fmt.Errorf("session %s woke before it could move: it stays where it is", info.ID)
	}

	// What picks a config, and whatever else a profile of the agent sets:
	// the one it leaves must not follow it.
	keys := slices.Clone(d.state.ResumeEnv[c.Agent])

	for _, profile := range d.state.Settings.Profiles[c.Agent] {
		for _, kv := range profile {
			k, _, _ := strings.Cut(kv, "=")
			keys = append(keys, k)
		}
	}

	resume := withEnv(env, d.byID(c.Agent, c.ID))

	s.mu.Lock()
	s.spec.Env = slices.DeleteFunc(slices.Clone(s.spec.Env), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(keys, k)
	})

	// An agent that is the session's own process takes the profile from the
	// session. One started in a shell takes it from the line typed there:
	// set on the shell too, the conversation would read as the shell's
	// config, not the agent's (ownEnv), and be looked for in the wrong one.
	if s.spec.ResumeExec {
		s.spec.Env = append(s.spec.Env, env...)
	}

	s.spec.Conversation = &proto.Conversation{Agent: c.Agent, ID: c.ID, Env: env}
	s.spec.Resume = resume
	s.mu.Unlock()

	err = d.save()
	d.mu.Unlock()

	if asleep { // no sleep or wake to say it moved
		d.broadcast(proto.Event{Kind: "sessions"})
		return s.info(), err
	}

	woken, werr := d.wakeSession(info.ID)

	return woken, errors.Join(err, werr)
}
