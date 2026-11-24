package daemon

import (
	"bufio"
	"bytes"
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
	errPaused    = errors.New("paused: sign-in lapsed until the agent runs")
	errSignedOut = errors.New("signed out")
	errBusy      = errors.New("busy: the service limits how often usage is asked for")
)

// usageSources ask an agent's service for the limits of the account env
// picks, by [agents] preset. plan is known without the service, so it comes
// back with an error too.
var usageSources = map[string]func(ctx context.Context, env []string) (plan string, limits []proto.Limit, err error){
	"claude":  claudeUsage,
	"codex":   codexUsage,
	"agy":     agyUsage,
	"copilot": copilotUsage,
	"grok":    grokUsage,
}

// The endpoints each agent's own CLI reads its limits from; tests point them
// at a server of theirs.
var (
	claudeUsageURL  = "https://api.anthropic.com/api/oauth/usage"
	agyUsageURL     = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	copilotUsageURL = "https://api.github.com/copilot_internal/user"
	grokUsageURL    = "https://cli-chat-proxy.grok.com/v1/billing"
)

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
				out = append(out, proto.Account{Agent: agent, Name: proto.ConfigName(agent, dir), Env: env})
			}
		}
	}

	ordered(out, d.state.Order)

	return out
}

// ordered sorts accounts, grouped by agent A–Z, in the order order gives each
// agent's; the ones it does not name keep their order after them.
func ordered(accounts []proto.Account, order map[string][][]string) {
	rank := func(a proto.Account) int {
		named := order[a.Agent]
		if i := slices.IndexFunc(named, func(env []string) bool { return slices.Equal(env, a.Env) }); i >= 0 {
			return i
		}

		return len(named)
	}

	slices.SortStableFunc(accounts, func(x, y proto.Account) int {
		return cmp.Or(cmp.Compare(x.Agent, y.Agent), rank(x)-rank(y))
	})
}

// setOrder takes each agent's account order in patch, an empty one forgetting
// it, and puts the accounts measured last in it. d.mu is held.
func (d *Daemon) setOrder(patch map[string][][]string) {
	if d.state.Order == nil {
		d.state.Order = map[string][][]string{}
	}

	for agent, order := range patch {
		if len(order) == 0 {
			delete(d.state.Order, agent)
		} else {
			d.state.Order[agent] = order
		}
	}

	d.usage.Accounts = slices.Clone(d.usage.Accounts) // a refresh reads the last one outside d.mu
	ordered(d.usage.Accounts, d.state.Order)
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

	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	var body json.RawMessage
	if err := askJSON(req, login.Token, &body); err != nil {
		return plan, nil, err
	}

	limits, err := parseClaudeUsage(body)

	return plan, limits, err
}

// askJSON sends req with token as its bearer and decodes the answer into v.
// A refused token reads as signed out, 429 as a busy service. The token goes
// to the one host it was issued for: a redirect is refused.
func askJSON(req *http.Request, token string, v any) error {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Do(req)
	if err != nil {
		return errors.New("unreachable: " + err.Error())
	}

	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errSignedOut
	case resp.StatusCode == http.StatusTooManyRequests:
		return errBusy
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("unavailable: %s answered %s", req.URL.Host, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return errors.New("unavailable: " + err.Error())
	}

	return nil
}

// envIn is key's value in env, else the daemon's own: where a profile points
// an agent.
func envIn(env []string, key string) string {
	value := os.Getenv(key)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			value = v
		}
	}

	return value
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

// agyToken is the sign-in agy keeps: its access token, the refresh token
// that renews it, and when it lapses.
type agyToken struct {
	Access  string    `json:"access_token"`
	Refresh string    `json:"refresh_token"`
	Expiry  time.Time `json:"expiry"`
}

// agyTier is a plan as Google's Cloud Code API names it.
type agyTier struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// agyUsage asks Google's Cloud Code API for the quota of agy's sign-in: the
// plan, then a 5-hour and a weekly bucket per group of models. The token is
// only read: agy alone renews it.
func agyUsage(ctx context.Context, env []string) (string, []proto.Limit, error) {
	login := agyLogin(ctx, env)

	switch {
	case login.Access == "":
		return "", nil, errSignedOut
	case !login.Expiry.IsZero() && login.Expiry.Before(time.Now()) && login.Refresh != "":
		return "", nil, errPaused
	case !login.Expiry.IsZero() && login.Expiry.Before(time.Now()):
		return "", nil, errSignedOut
	}

	ask := func(method string, body, v any) error {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, agyUsageURL+":"+method, bytes.NewReader(b))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "antigravity") // as agy's own requests name themselves

		return askJSON(req, login.Access, v)
	}

	var assist struct {
		Paid    *agyTier        `json:"paidTier"`
		Current *agyTier        `json:"currentTier"`
		Project json.RawMessage `json:"cloudaicompanionProject"` // its id, or {"id": …}
	}
	if err := ask("loadCodeAssist", struct{}{}, &assist); err != nil {
		return "", nil, err
	}

	plan := agyPlan(cmp.Or(assist.Paid, assist.Current))

	var project struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(assist.Project, &project.ID) != nil {
		_ = json.Unmarshal(assist.Project, &project) // none: the summary is asked without one
	}

	body := map[string]string{}
	if project.ID != "" {
		body["project"] = project.ID
	}

	var quota json.RawMessage
	if err := ask("retrieveUserQuotaSummary", body, &quota); err != nil {
		return plan, nil, err
	}

	limits, err := parseAgyQuota(quota)

	return plan, limits, err
}

// agyLogin is the sign-in agy keeps in the Secret Service keyring, else in its
// antigravity-oauth-token file.
//
// shortcut: the keyring is read with secret-tool, so on macOS, where agy uses
// the Keychain, it reads signed out. Read it with `security` when someone on
// macOS asks.
func agyLogin(ctx context.Context, env []string) agyToken {
	b, err := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "gemini", "username", "antigravity").Output()
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		dir := envIn(env, "AGY_DIR")
		if home, err := os.UserHomeDir(); dir == "" && err == nil {
			dir = filepath.Join(home, ".gemini", "antigravity-cli")
		}

		b, _ = os.ReadFile(filepath.Join(dir, "antigravity-oauth-token")) // none reads as signed out
	}

	var v struct {
		Token agyToken `json:"token"`
	}

	_ = json.Unmarshal(b, &v)

	return v.Token
}

// agyPlan names a tier as agy does: Ultra, Pro or Free where its id or name
// says one, else its name.
func agyPlan(tier *agyTier) string {
	if tier == nil {
		return ""
	}

	named := strings.ToLower(tier.ID + " " + tier.Name)
	for _, word := range []string{"ultra", "pro", "free"} {
		if strings.Contains(named, word) {
			return title(word)
		}
	}

	return cmp.Or(strings.TrimPrefix(tier.Name, "Google AI "), tier.ID)
}

// parseAgyQuota reads the quota summary: per group of models (Gemini, Claude
// and GPT) a 5-hour and a weekly bucket, as the share left. Each bucket is its
// group's own, no group's use counts against another's, so every limit names
// its group. An answer of buckets without groups lists Gemini's.
func parseAgyQuota(b []byte) ([]proto.Limit, error) {
	type bucket struct {
		ID       string   `json:"bucketId"`
		Name     string   `json:"displayName"`
		Window   string   `json:"window"`
		Left     *float64 `json:"remainingFraction"`
		Amount   *float64 `json:"remainingAmount"`
		Resets   string   `json:"resetTime"`
		Disabled bool     `json:"disabled"`
	}

	type group struct {
		Name    string   `json:"displayName"`
		Buckets []bucket `json:"buckets"`
	}

	var v struct {
		Groups  []group  `json:"groups"`
		Buckets []bucket `json:"buckets"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, errors.New("unavailable: " + err.Error())
	}

	if len(v.Groups) == 0 && len(v.Buckets) > 0 {
		v.Groups = []group{{Name: "Gemini", Buckets: v.Buckets}}
	}

	var out []proto.Limit

	for _, g := range v.Groups {
		for _, bk := range g.Buckets {
			resets, _ := time.Parse(time.RFC3339, bk.Resets) // none: the reset is not known

			// protobuf JSON leaves a zero out: a bucket that resets and says
			// nothing of what is left has nothing left. An amount left is no
			// share of the whole.
			used := 100.0

			switch {
			case bk.Disabled:
				continue
			case bk.Left != nil:
				used = (1 - *bk.Left) * 100
			case bk.Amount != nil && *bk.Amount != 0, bk.Amount == nil && resets.IsZero():
				continue
			}

			p, ok := percent(used)
			if !ok {
				continue
			}

			window := "5h"
			if strings.Contains(strings.ToLower(bk.Window+" "+bk.ID+" "+bk.Name), "week") {
				window = "7d"
			}

			out = append(out, proto.Limit{Window: window, Model: strings.TrimSpace(g.Name), Percent: p, Resets: resets})
		}
	}

	return out, nil
}

// copilotPlans name Copilot's plans as GitHub's quota answer spells them.
var copilotPlans = map[string]string{"individual": "Pro", "individual_pro": "Pro+", "business": "Business", "enterprise": "Enterprise"}

// copilotUsage asks GitHub for the Copilot quota of the token Copilot CLI
// takes first: COPILOT_GITHUB_TOKEN, GH_TOKEN or GITHUB_TOKEN, else gh's
// sign-in to github.com.
//
// shortcut: the CLI keeps its own sign-in in the keyring, which pando does not
// read, so gh's stands in for it, in every config alike. Read the keyring once
// someone signs Copilot in as another account than gh.
func copilotUsage(ctx context.Context, env []string) (string, []proto.Limit, error) {
	token := cmp.Or(envIn(env, "COPILOT_GITHUB_TOKEN"), envIn(env, "GH_TOKEN"), envIn(env, "GITHUB_TOKEN"))
	if token == "" {
		out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
		if err != nil { // no gh, or nobody signed in to it
			return "", nil, errSignedOut
		}

		token = strings.TrimSpace(string(out))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotUsageURL, http.NoBody)
	if err != nil {
		return "", nil, err
	}

	var body json.RawMessage
	if err := askJSON(req, token, &body); err != nil {
		return "", nil, err
	}

	return parseCopilotQuota(body)
}

// parseCopilotQuota reads GitHub's answer: a month's allowance per kind of
// request, premium requests the plan's own, chat and completions the free
// plan's. One without a cap, or with nothing granted, is no limit.
func parseCopilotQuota(b []byte) (string, []proto.Limit, error) {
	type quota struct {
		Granted   float64  `json:"entitlement"`
		Left      *float64 `json:"percent_remaining"`
		Unlimited bool     `json:"unlimited"`
	}

	var v struct {
		Plan   string           `json:"copilot_plan"`
		SKU    string           `json:"access_type_sku"` // "free_limited_copilot" on the free plan
		Resets string           `json:"quota_reset_date_utc"`
		Quotas map[string]quota `json:"quota_snapshots"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", nil, errors.New("unavailable: " + err.Error())
	}

	plan := cmp.Or(copilotPlans[v.Plan], title(v.Plan))
	if strings.Contains(v.SKU, "free") {
		plan = "Free"
	}

	resets, _ := time.Parse(time.RFC3339, v.Resets) // none: the reset is not known

	ids := slices.SortedFunc(maps.Keys(v.Quotas), func(a, b string) int {
		return cmp.Or(cmp.Compare(b2i(a != "premium_interactions"), b2i(b != "premium_interactions")), cmp.Compare(a, b))
	})

	var out []proto.Limit

	for _, id := range ids {
		q := v.Quotas[id]
		if q.Unlimited || q.Granted <= 0 || q.Left == nil {
			continue
		}

		p, ok := percent(100 - *q.Left)
		if !ok {
			continue
		}

		scope := id
		if id == "premium_interactions" {
			scope = ""
		}

		out = append(out, proto.Limit{Window: "1mo", Model: scope, Percent: p, Resets: resets})
	}

	return plan, out, nil
}

// grokLogin is a sign-in Grok keeps in auth.json.
type grokLogin struct {
	Token   string `json:"key"`
	Expires string `json:"expires_at"`
	Renews  string `json:"refresh_token"`
}

// grokRenews is how long Grok's refresh token renews its access token, as
// Grok's docs give a sign-in with no expiry of its own.
const grokRenews = 30 * 24 * time.Hour

// grokUsage asks xAI for the credits of the sign-in Grok keeps in the home env
// picks, as Grok's own /usage does: the share of the week's or the month's
// credits used, and a unified-billing account's monthly quota.
func grokUsage(ctx context.Context, env []string) (string, []proto.Limit, error) {
	home := envIn(env, "GROK_HOME")
	if dir, err := os.UserHomeDir(); home == "" && err == nil {
		home = filepath.Join(dir, ".grok")
	}

	plan := grokPlan(home)

	var logins map[string]grokLogin // one per issuer

	b, _ := os.ReadFile(filepath.Join(home, "auth.json")) // none reads as signed out
	_ = json.Unmarshal(b, &logins)

	var login grokLogin

	for _, issuer := range slices.Sorted(maps.Keys(logins)) {
		if logins[issuer].Token != "" {
			login = logins[issuer]
			break
		}
	}

	// Grok renews its access token from the refresh token when it runs.
	expires, _ := time.Parse(time.RFC3339, login.Expires)
	lapsed := !expires.IsZero() && expires.Before(time.Now())

	switch {
	case login.Token == "":
		return plan, nil, errSignedOut
	case lapsed && login.Renews != "" && time.Since(expires) < grokRenews:
		return plan, nil, errPaused
	case lapsed:
		return plan, nil, errSignedOut
	}

	ask := func(url string, v any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return err
		}

		return askJSON(req, login.Token, v)
	}

	var credits struct {
		Config struct {
			Period struct {
				Type string    `json:"type"`
				End  time.Time `json:"end"`
			} `json:"currentPeriod"`
			Used    *grokAmount `json:"creditUsagePercent"`
			Unified bool        `json:"isUnifiedBillingUser"`
		} `json:"config"`
	}
	if err := ask(grokUsageURL+"?format=credits", &credits); err != nil {
		return plan, nil, err
	}

	c := credits.Config

	var out []proto.Limit

	// protobuf JSON leaves a zero out: no share is none used yet, but for a
	// unified-billing account, whose use the monthly quota counts, and a
	// period already over.
	if c.Used != nil || !c.Unified && c.Period.End.After(time.Now()) {
		used := 0.0
		if c.Used != nil {
			used = float64(*c.Used)
		}

		window := "1mo"
		if strings.Contains(c.Period.Type, "WEEK") {
			window = "7d"
		}

		if p, ok := percent(used); ok {
			out = append(out, proto.Limit{Window: window, Percent: p, Resets: c.Period.End})
		}
	}

	if !c.Unified {
		return plan, out, nil
	}

	var monthly struct {
		Config struct {
			Quota  grokAmount `json:"monthlyLimit"`
			Used   grokAmount `json:"used"`
			Resets time.Time  `json:"billingPeriodEnd"`
		} `json:"config"`
	}

	err := ask(grokUsageURL, &monthly)
	m := monthly.Config

	if p, ok := percent(float64(m.Used / m.Quota * 100)); err == nil && m.Quota > 0 && ok {
		out = append(out, proto.Limit{Window: "1mo", Percent: p, Resets: m.Resets})
	} else if len(out) == 0 { // the credits' window stands without it
		return plan, nil, cmp.Or(err, errors.New("unavailable: xAI named no limits"))
	}

	return plan, out, nil
}

// grokAmount is a number as xAI's protobuf JSON sends one: plain or quoted,
// or a money field's {"val": n}.
type grokAmount float64

func (a *grokAmount) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(b, []byte("{")) {
		var money struct {
			Val json.Number `json:"val"`
		}
		if err := json.Unmarshal(b, &money); err != nil {
			return err
		}

		b = []byte(cmp.Or(money.Val.String(), "0")) // a zero is left out
	}

	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}

	f, err := n.Float64()
	*a = grokAmount(f)

	return err
}

// grokPlan is the plan the settings Grok caches from xAI name.
func grokPlan(home string) string {
	var cache struct {
		Payload string `json:"payload"` // JSON again
	}

	b, _ := os.ReadFile(filepath.Join(home, "settings_cache.json")) // none: no plan
	_ = json.Unmarshal(b, &cache)

	var v struct {
		Settings struct {
			Tier string `json:"subscription_tier_display"`
		} `json:"settings"`
	}

	_ = json.Unmarshal([]byte(cache.Payload), &v)

	return v.Settings.Tier
}

// profileSession moves session p.ID's agent to the profile p.Env picks, none
// for its default: the agent ends and continues the same conversation there,
// as a sleep and a wake do. Only an agent whose open conversation pando can
// name ([resume_id]) can follow it into another config. A session asleep
// stays asleep, and wakes there. p.Name picks the profile in place of p.Env,
// and p.Send is typed once the agent is back at its prompt.
func (d *Daemon) profileSession(p proto.SessionProfileParams) (proto.Session, error) {
	env := p.Env

	s, err := d.session(p.ID)
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

	if known && p.Name != "" {
		env, err = d.profileEnv(c.Agent, p.Name)
	}
	d.mu.Unlock()

	if !known {
		return info, fmt.Errorf("session %s runs no agent whose conversation pando can continue elsewhere", info.ID)
	}

	if err != nil {
		return info, err
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

	if asleep && p.Send == "" { // no sleep or wake to say it moved
		d.broadcast(proto.Event{Kind: "sessions"})
		return s.info(), err
	}

	woken, werr := d.wakeSession(info.ID)
	if err := errors.Join(err, werr); err != nil || p.Send == "" {
		return woken, err
	}

	// A shell the agent has not started in yet would run the text, and a
	// question would take it as its answer.
	if woken.Status != "idle" || woken.Program != c.Agent {
		return woken, fmt.Errorf("session %s moved, the text was not sent: %s is not at its prompt (%s)", woken.ID, c.Agent, woken.Status)
	}

	ns, err := d.session(woken.ID) // a woken session is a new one under its id
	if err != nil {
		return woken, err
	}

	return woken, submit(ns, p.Send)
}

// profileEnv is what picks agent's profile name, as usage.get lists it. One
// nobody is signed in to is refused: the agent would wake at a sign-in. With
// usage off what was measured once says nothing now, as for advise. d.mu is
// held.
func (d *Daemon) profileEnv(agent, name string) ([]string, error) {
	var names []string

	for _, a := range d.accounts() {
		if a.Agent != agent {
			continue
		}

		names = append(names, a.Name)
		if a.Name != name {
			continue
		}

		if measured, _ := account(d.usage.Accounts, agent, a.Env); d.state.Settings.Usage && measured.Note == errSignedOut.Error() {
			return nil, fmt.Errorf("%s profile %q is signed out", agent, name)
		}

		return a.Env, nil
	}

	return nil, fmt.Errorf("%s has no profile %q, it has: %s", agent, name, strings.Join(names, " "))
}

// submit types text into s and presses Enter, as `pando session send
// --enter` does: more than one line as a paste, and Enter a moment later,
// which a line editor would otherwise take as a pasted newline.
func submit(s *session, text string) error {
	in := proto.InputParams{Text: text}
	if strings.Contains(text, "\n") {
		in = proto.InputParams{Paste: text}
	}

	if err := s.input(in); err != nil {
		return err
	}

	time.Sleep(50 * time.Millisecond)

	return s.input(proto.InputParams{Text: "\r"})
}
