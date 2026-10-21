//go:build unix

package proto

import (
	"os/exec"
	"syscall"
)

// daemonAttrs detach `pando serve` from the terminal that started it: a
// session of its own, which that terminal's hangup does not reach.
func daemonAttrs() []*syscall.SysProcAttr { return []*syscall.SysProcAttr{{Setsid: true}} }

// daemonArgv runs exe serve in a systemd scope of its own where a user
// manager answers. A daemon started from a terminal otherwise shares its
// scope, whose default OOMPolicy=stop ends every process in it once the
// kernel kills one: a session's runaway test took every session with it.
// OOMPolicy=continue loses only the process the kernel picked.
// ponytail: one scope for the daemon and every session; systemd-oomd, which
// ends a whole cgroup under memory pressure, still takes them all at once.
func daemonArgv(exe string) []string {
	scope := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "-p", "OOMPolicy=continue"}
	if exec.Command(scope[0], append(scope[1:], "true")...).Run() != nil { // no systemd, or no user bus
		return []string{exe, "serve"}
	}

	return append(scope, exe, "serve")
}
