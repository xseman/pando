package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xseman/pando/internal/proto"
)

// BenchmarkScreenScrolled fetches a page scrolled back into a full
// scrollback, as the TUI does on every screen event while it is scrolled.
func BenchmarkScreenScrolled(b *testing.B) {
	s := testSession(200, 50)

	var out strings.Builder
	for i := range 12000 {
		fmt.Fprintf(&out, "\x1b[38;5;%dmline %d\x1b[0m \x1b[1m%s\x1b[m\r\n", i%255, i, strings.Repeat("word ", 30))
	}

	s.drain(func() { _, _ = s.emu.WriteString(out.String()) })

	b.ReportAllocs()

	for b.Loop() {
		_ = s.screen(proto.ScreenParams{Cols: 200, Rows: 50, Scroll: 30})
	}
}

// BenchmarkOutput feeds a session busy output past a full scrollback, as
// its reader does: the emulator, then the rows scrolled off moved to back.
func BenchmarkOutput(b *testing.B) {
	s := testSession(200, 50)

	var out strings.Builder
	for i := range 100 {
		fmt.Fprintf(&out, "\x1b[38;5;%dm● %d\x1b[0m Thinking about the problem, reading internal/ui/app.go:%d\r\n", i%200+16, i, i)
	}

	chunk := out.String()

	for range scrollMax / 100 {
		s.drain(func() { _, _ = s.emu.WriteString(chunk) })
	}

	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()

	for b.Loop() {
		s.drain(func() { _, _ = s.emu.WriteString(chunk) })
	}
}
