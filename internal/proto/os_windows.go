package proto

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// daemonAttrs detach `pando serve` from the console that started it, tried in
// order: a hidden console of its own, which closing that one does not end,
// out of the job its starter runs in, else in it when the job does not let
// it go. Out of it, the daemon outlives a host that ends its job with it, an
// ssh session's. Hidden, not none: a process without a console opens a window
// for every console program it runs, git included.
func daemonAttrs() []*syscall.SysProcAttr {
	return []*syscall.SysProcAttr{
		{CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_BREAKAWAY_FROM_JOB},
		{CreationFlags: windows.CREATE_NO_WINDOW},
	}
}
