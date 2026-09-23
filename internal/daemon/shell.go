package daemon

import (
	"bufio"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// shellAgents are the sessions that are a plain shell: one opened with n, a
// session's tab, and a Terminal panel's shell. They open the shell setting.
var shellAgents = []string{"shell", "tab", "terminal"}

func isShellAgent(agent string) bool { return slices.Contains(shellAgents, agent) }

// quickExit is how soon a shell that fails counts as one that never started:
// a flag it does not know, a config it chokes on.
const quickExit = 2 * time.Second

// loginShell is the user's own shell: $SHELL, else their passwd entry, else "".
func loginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	uid := strconv.Itoa(os.Getuid())

	for sc := bufio.NewScanner(f); sc.Scan(); {
		if p := strings.Split(sc.Text(), ":"); len(p) == 7 && p[2] == uid {
			return p[6]
		}
	}

	return ""
}

// shellCandidates are the commands a shell session tries, in order: the shell
// setting, the agent's own [agents] preset, the shell preset, the login
// shell, then bash and sh, which are what a broken setting falls back on.
// d.mu is held.
func (d *Daemon) shellCandidates(agent string) [][]string {
	var out [][]string

	add := func(argv []string) {
		if len(argv) > 0 && !slices.ContainsFunc(out, func(c []string) bool { return slices.Equal(c, argv) }) {
			out = append(out, argv)
		}
	}

	add(strings.Fields(d.state.Settings.Shell))
	add(d.state.Agents[agent])
	add(d.state.Agents["shell"])
	add(strings.Fields(loginShell()))
	add([]string{"bash"})
	add([]string{"/bin/sh"})

	return out
}

// startShell starts shell session spec with the first of cands that starts.
// One that is not installed is skipped, and the session says so on its
// screen; one that fails at once hands over to the next through fallBack.
func (d *Daemon) startShell(spec proto.SessionSpec, cols, rows int, cands [][]string) (*session, error) {
	var (
		skipped []string
		err     error
	)

	for i, argv := range cands {
		if _, lerr := exec.LookPath(argv[0]); lerr != nil {
			skipped, err = append(skipped, argv[0]), lerr
			continue
		}

		spec.Cmd = argv

		var s *session
		if s, err = d.startWith(spec, cols, rows, cands[i+1:]); err != nil {
			skipped = append(skipped, argv[0])
			continue
		}

		if len(skipped) > 0 {
			s.notice(strings.Join(skipped, ", ") + " did not start; " + argv[0] + " instead")
		}

		return s, nil
	}

	return nil, err
}

// fallBack restarts shell session id, whose shell failed at once, with the
// next of the shells it had left, in its place in the list and with its id:
// the old one stays registered until the new one takes its place, so a
// client showing it never finds it gone. When none starts, the failed one
// stays listed with its exit code.
func (d *Daemon) fallBack(id string) {
	d.mu.Lock()
	s := d.sessions[id]
	closing := d.closing
	d.mu.Unlock()

	if s == nil || closing {
		return
	}

	spec := s.info().SessionSpec
	cols, rows := s.size()
	failed := spec.Cmd[0]

	ns, err := d.startShell(spec, cols, rows, s.fallback)
	if err != nil {
		d.broadcast(proto.Event{Kind: "sessions"})
		return
	}

	d.mu.Lock()
	if i := slices.Index(d.order, id); i >= 0 && i < len(d.order)-1 { // start appended it a second time
		d.order = d.order[:len(d.order)-1]
	}

	_ = d.save() // best effort: the tick writes it again
	d.mu.Unlock()

	s.kill() // it has exited: this only ends its input copier
	ns.notice(failed + " exited as soon as it started; " + ns.info().Cmd[0] + " instead")
	d.broadcast(proto.Event{Kind: "sessions"})
}
