//go:build unix

package main

import (
	"os"
	"syscall"
)

// lockFile takes f's exclusive lock, failing at once when another process
// holds it.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

// execv runs bin with argv in pando's place.
func execv(bin string, argv []string) error { return syscall.Exec(bin, argv, os.Environ()) }
