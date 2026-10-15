package db

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// keyringService is what pando keeps a database's password under.
const keyringService = "pando"

// keyring caches what the keyring keeps, by database name, "" for nothing:
// one keyring call per database a run.
var keyring sync.Map

// keyringTool is the program that reaches the system keyring: secret-tool
// (libsecret: GNOME Keyring, KWallet) on Linux, security (Keychain) on
// macOS; none on Windows.
func keyringTool() string {
	switch runtime.GOOS {
	case "darwin":
		return "security"
	case "windows":
		return ""
	}

	return "secret-tool"
}

// HasKeyring reports a system keyring to keep passwords in.
func HasKeyring() bool {
	if keyringTool() == "" {
		return false
	}

	_, err := exec.LookPath(keyringTool())

	return err == nil
}

// SavePassword keeps pw for the database called name in the system keyring.
func SavePassword(name, pw string) error {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// ponytail: security takes the password on its command line, where
		// ps shows it while it runs; the Keychain API needs cgo.
		cmd = exec.Command("security", "add-generic-password", "-U", "-s", keyringService, "-a", name, "-w", pw)
	} else {
		cmd = exec.Command("secret-tool", "store", "--label", "pando: "+name, "service", keyringService, "database", name)
		cmd.Stdin = strings.NewReader(pw)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New("keyring: " + msg)
		}

		return err
	}

	keyring.Store(name, pw)

	return nil
}

// LoadPassword is the password the system keyring keeps for the database
// called name, "" for none or no keyring.
func LoadPassword(name string) string {
	if pw, ok := keyring.Load(name); ok {
		s, _ := pw.(string) // only strings go in
		return s
	}

	var out []byte

	switch keyringTool() {
	case "security":
		out, _ = exec.Command("security", "find-generic-password", "-s", keyringService, "-a", name, "-w").Output() // none: nothing printed
	case "secret-tool":
		out, _ = exec.Command("secret-tool", "lookup", "service", keyringService, "database", name).Output() // the same
	}

	pw := strings.TrimSuffix(string(out), "\n")
	keyring.Store(name, pw)

	return pw
}

// ForgetPassword drops what the system keyring keeps for name, if anything:
// both tools fail when there is nothing to drop, and are not asked again.
func ForgetPassword(name string) {
	keyring.Delete(name)

	switch keyringTool() {
	case "security":
		_ = exec.Command("security", "delete-generic-password", "-s", keyringService, "-a", name).Run()
	case "secret-tool":
		_ = exec.Command("secret-tool", "clear", "service", keyringService, "database", name).Run()
	}
}
