// Package font finds a Nerd Font among the installed fonts.
package font

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Installed reports whether a Nerd Font is installed where the system and
// the user keep fonts.
func Installed() bool { return nerdFontIn(dirs()...) }

// dirs are where the system and the user install fonts; the user's own comes
// first.
func dirs() []string {
	h, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(h, "Library", "Fonts"), "/Library/Fonts"}
	case "windows":
		return []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts"), filepath.Join(os.Getenv("WINDIR"), "Fonts")}
	}

	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(h, ".local", "share")
	}

	return []string{filepath.Join(data, "fonts"), filepath.Join(h, ".fonts"), "/usr/local/share/fonts", "/usr/share/fonts"}
}

// nerdFontIn reports whether a font file under dirs is a Nerd Font, by its
// name: JetBrainsMonoNerdFont-Regular.ttf, "Hack Nerd Font Complete.ttf".
func nerdFontIn(dirs ...string) bool {
	found := false

	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
			}

			if strings.Contains(strings.ToLower(strings.ReplaceAll(e.Name(), " ", "")), "nerdfont") {
				found = true

				return fs.SkipAll
			}

			return nil
		})

		if found {
			return true
		}
	}

	return false
}
