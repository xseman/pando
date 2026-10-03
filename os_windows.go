package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"golang.org/x/sys/windows"
)

// lockFile takes f's exclusive lock, failing at once when another process
// holds it.
func lockFile(f *os.File) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)

	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
}

// execv runs bin with argv as near to in pando's place as Windows has: in
// this console, pando waiting for it and exiting with its code. Ctrl+C is
// bin's to handle, not pando's.
func execv(bin string, argv []string) error {
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	signal.Ignore(os.Interrupt)

	err := cmd.Run()

	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}

	return err
}
