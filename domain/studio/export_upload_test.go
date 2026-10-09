package studio

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func mp4(size int) []byte {
	video := make([]byte, size)
	copy(video, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	return video
}

func TestAnExportIsReadOnlyWhenItIsAnMP4WithinTheLimit(t *testing.T) {
	video := mp4(4096)
	got, err := io.ReadAll(NewExportReader(iotest.OneByteReader(bytes.NewReader(video))))
	if err != nil || !bytes.Equal(got, video) {
		t.Fatalf("read %d bytes, err %v", len(got), err)
	}
}

func TestAnExportThatIsNotAnMP4OrIsEmptyIsRefused(t *testing.T) {
	html := append([]byte("<html><script>alert(1)</script>"), make([]byte, 64)...)
	for name, body := range map[string][]byte{"html": html, "empty": {}, "too short": {0, 0, 0, 8}} {
		if _, err := io.ReadAll(NewExportReader(bytes.NewReader(body))); !errors.Is(err, ErrInvalidExport) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestAnExportOverTheLimitStopsWhileItIsRead(t *testing.T) {
	_, err := io.Copy(io.Discard, NewExportReader(bytes.NewReader(mp4(MaxExportBytes+1))))
	if !errors.Is(err, ErrExportTooLarge) {
		t.Fatalf("err %v", err)
	}
	if _, err := io.Copy(io.Discard, NewExportReader(bytes.NewReader(mp4(MaxExportBytes)))); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

func TestAnExportLivesInItsProjectFolder(t *testing.T) {
	if key := ExportKey("ws-1", "p-1", "e-1"); key != "studio-exports/ws-1/p-1/e-1.mp4" {
		t.Fatalf("key %q", key)
	}
}
