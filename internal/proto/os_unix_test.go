//go:build unix

package proto

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestDaemonArgvScope runs a stand-in daemon the way EnsureDaemon does and
// asks systemd what its scope does once the kernel kills a process in it.
func TestDaemonArgvScope(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "pando")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nsystemctl --user show -p OOMPolicy --value \"$(basename \"$(cut -d: -f3 /proc/self/cgroup)\")\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	argv := daemonArgv(exe)
	if argv[0] != "systemd-run" {
		t.Skip("no systemd user manager")
	}

	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("%s: %v", strings.Join(argv, " "), err)
	}

	if got := strings.TrimSpace(string(out)); got != "continue" {
		t.Errorf("OOMPolicy = %q, want continue", got)
	}
}

// TestDaemonArgvNoBus starts the daemon as before where systemd-run cannot
// reach a user manager.
func TestDaemonArgvNoBus(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	if got, want := daemonArgv("/x/pando"), []string{"/x/pando", "serve"}; !slices.Equal(got, want) {
		t.Errorf("daemonArgv = %q, want %q", got, want)
	}
}
