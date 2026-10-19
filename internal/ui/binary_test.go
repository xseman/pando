package ui

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
)

// A binary file opens as a card: its name, an image's format and size or
// the type it sniffs as, its size on disk; never as text, never edited.
func TestBinaryCard(t *testing.T) {
	m := testModelSized(t, 100, 20)

	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	pic, blob := filepath.Join(m.ws, "pic.png"), filepath.Join(m.ws, "blob.bin")
	mustWrite(t, pic, b.String())
	mustWrite(t, blob, "\x00\x01\x02"+strings.Repeat("x", 2000))

	open := func(path string) string {
		t.Helper()

		for _, msg := range runAll(m.openFileAt(path, 0, 0, 0, false)) {
			if pm, ok := msg.(previewMsg); ok {
				m.Update(pm)
			}
		}

		m.focus = onMain

		return checkWidths(t, m)
	}

	if out := open(pic); !strings.Contains(out, "pic.png") || !strings.Contains(out, "PNG image 3×2") ||
		!strings.Contains(out, fileSize(int64(b.Len()))+" on disk") || !strings.Contains(out, "not drawn") {
		t.Fatalf("image card:\n%s", out)
	}

	press(m, "x")

	if mustRead(t, pic) != b.String() || m.pv.editable() {
		t.Fatal("a binary file took an edit")
	}

	if out := open(blob); !strings.Contains(out, "Binary file") || !strings.Contains(out, "2.0 KB on disk") || !strings.Contains(out, "not text") {
		t.Fatalf("binary card:\n%s", out)
	}
}

// A byte order mark makes UTF-16 text, not a binary file.
func TestBinaryCardUTF16(t *testing.T) {
	for head, want := range map[string]string{"\xff\xfeh\x00i\x00": "UTF-16 LE text", "\xfe\xff\x00h\x00i": "UTF-16 BE text"} {
		if got := binaryCard("u.txt", []byte(head), 6)[1]; got != want {
			t.Errorf("%q: %q, want %q", head, got, want)
		}
	}
}

func TestFileSize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 53658: "52.4 KB",
		2202010: "2.1 MB", 5 << 30: "5.0 GB", 3 << 40: "3.0 TB", 2048 << 40: "2048.0 TB",
	} {
		if got := fileSize(n); got != want {
			t.Errorf("fileSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// FuzzBinaryCard checks a card is always four lines, the file's name first
// and its size third, whatever the bytes claim to be.
func FuzzBinaryCard(f *testing.F) {
	var b bytes.Buffer

	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))) // a seed; an error leaves it empty
	for _, s := range []string{b.String(), "GIF89a\x01\x00\x01\x00", "\xff\xd8\xff", "%PDF-1.4\x00", "\x00"} {
		f.Add([]byte(s), int64(len(s)))
	}

	f.Fuzz(func(t *testing.T, head []byte, size int64) {
		size = max(size, 0)

		card := binaryCard("/x/a b.bin", head, size)
		if len(card) != 4 || card[0] != "a b.bin" || card[2] != fileSize(size)+" on disk" {
			t.Fatalf("%q: %q", head, card)
		}

		for _, l := range card {
			if strings.ContainsAny(l, "\n\r") {
				t.Fatalf("%q: a line breaks: %q", head, card)
			}
		}
	})
}
