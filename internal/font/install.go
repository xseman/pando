package font

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/xseman/pando/internal/update"
)

// zipURL is the nerd-fonts release's icons-only font and zipSHA what it must
// hash to; a new release moves both by hand. The tests point them elsewhere.
var (
	zipURL = "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.5.1/NerdFontsSymbolsOnly.zip"
	zipSHA = "fdca3682534f6f65e1ccb2345b0362ccf67d9b8eca7c8025330946e93e2473bc"
)

// Install puts Symbols Nerd Font Mono, the icons alone, and its license where
// the user keeps fonts, for the terminal to fall back on. On Linux the
// release's fontconfig rule comes along, so those icons come from it even
// when another font claims the same code points. Linux and macOS only.
func Install(ctx context.Context) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return errors.New("installing a font is for Linux and macOS")
	}

	dir := dirs()[0]
	want := map[string]string{
		"SymbolsNerdFontMono-Regular.ttf": filepath.Join(dir, "SymbolsNerdFontMono-Regular.ttf"),
		"LICENSE":                         filepath.Join(dir, "SymbolsNerdFont-LICENSE"),
	}

	if runtime.GOOS == "linux" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return err
		}

		want["10-nerd-font-symbols.conf"] = filepath.Join(cfg, "fontconfig", "conf.d", "10-nerd-font-symbols.conf")
	}

	tmp, err := update.Download(ctx, update.Release{URL: zipURL, SHA256: zipSHA}, "", nil)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(tmp) }()

	zr, err := zip.OpenReader(tmp)
	if err != nil {
		return err
	}

	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if dst, ok := want[f.Name]; ok {
			if err := extract(f, dst); err != nil {
				return err
			}
		}
	}

	if runtime.GOOS == "linux" {
		_ = exec.CommandContext(ctx, "fc-cache", "-f", dir).Run() // without fontconfig there is no cache to refresh
	}

	return nil
}

// extract writes zip entry f to dst, making its directory.
func extract(f *zip.File, dst string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}

	defer func() { _ = r.Close() }()

	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	return os.WriteFile(dst, b, 0o644)
}
