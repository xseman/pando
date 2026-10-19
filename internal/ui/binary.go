package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // image.DecodeConfig reads the formats registered
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"
)

// binaryCard describes a file pando shows no text of: its name, an image's
// format and size or the type its bytes sniff as, its size on disk, and why
// there is no text. head is its first bytes, size its length.
func binaryCard(path string, head []byte, size int64) []string {
	kind, why := "Binary file", "Binary files are shown as info, not text."

	t, params, _ := strings.Cut(http.DetectContentType(head), ";")

	switch c, format, err := image.DecodeConfig(bytes.NewReader(head)); {
	case err == nil:
		kind, why = fmt.Sprintf("%s image %d×%d", strings.ToUpper(format), c.Width, c.Height), "Images are shown as info, not drawn."
	case strings.Contains(params, "utf-16"): // its byte order mark: text, NULs and all
		kind, why = "UTF-16 "+strings.ToUpper(params[len(params)-2:])+" text", "Only UTF-8 text is shown and edited."
	case t != "application/octet-stream" && !strings.HasPrefix(t, "text/"):
		kind += " · " + t
	}

	return []string{filepath.Base(path), kind, fileSize(size) + " on disk", why}
}

// fileSize is n bytes as a person reads it, in steps of 1024.
func fileSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}

	f, unit := float64(n)/1024, "KB"
	for _, u := range []string{"MB", "GB", "TB"} {
		if f < 1024 {
			break
		}

		f, unit = f/1024, u
	}

	return fmt.Sprintf("%.1f %s", f, unit)
}
