//go:build unix

package proto

import "syscall"

// daemonAttrs detach `pando serve` from the terminal that started it: a
// session of its own, which that terminal's hangup does not reach.
func daemonAttrs() []*syscall.SysProcAttr { return []*syscall.SysProcAttr{{Setsid: true}} }
