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

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func mp4Stream(size int64) io.Reader {
	head := mp4(12)
	return io.MultiReader(bytes.NewReader(head), io.LimitReader(zeros{}, size-int64(len(head))))
}

func gif(size int) []byte {
	animation := make([]byte, size)
	copy(animation, "GIF89a")
	return animation
}

func animatedWebP(size int) []byte {
	animation := make([]byte, size)
	copy(animation, "RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02")
	return animation
}

func pdf(size int) []byte {
	document := make([]byte, size)
	copy(document, "%PDF-1.4\n")
	return document
}

func TestAnExportIsKnownByItsFirstBytes(t *testing.T) {
	still := animatedWebP(64)
	still[20] = 0x10
	cases := map[string]struct {
		head []byte
		want ExportFormat
		ok   bool
	}{
		"mp4":            {mp4(64), ExportMP4, true},
		"gif 89a":        {gif(64), ExportGIF, true},
		"gif 87a":        {append([]byte("GIF87a"), make([]byte, 32)...), ExportGIF, true},
		"webp":           {animatedWebP(64), ExportWebP, true},
		"pdf":            {pdf(64), ExportPDF, true},
		"svg":            {[]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`), ExportSVG, true},
		"still webp":     {still, "", false},
		"html":           {[]byte("<html><script>alert(1)</script>"), "", false},
		"xml prolog":     {[]byte(`<?xml version="1.0"?><svg></svg>`), "", false},
		"empty":          {nil, "", false},
		"short mp4":      {[]byte{0, 0, 0, 8}, "", false},
		"png":            {append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...), ExportPNG, true},
		"jpeg":           {append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 32)...), ExportJPEG, true},
		"short png":      {[]byte("\x89PNG\r\n"), "", false},
		"jpeg lookalike": {[]byte{0xff, 0xd8, 0x00}, "", false},
		"bare riff":      {[]byte("RIFF\x00\x00\x00\x00WAVEfmt "), "", false},
		"short webp":     {[]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), "", false},
		"gif lookali":    {[]byte("GIF90a"), "", false},
	}
	for name, c := range cases {
		got, ok := DetectExport(c.head)
		if got != c.want || ok != c.ok {
			t.Fatalf("%s: got %q %v", name, got, ok)
		}
	}
}

func TestEachProjectKindExportsOnlyItsOwnFormats(t *testing.T) {
	for _, format := range []ExportFormat{ExportMP4, ExportGIF, ExportWebP, ExportPNG, ExportJPEG} {
		if !ExportAllowed(KindVideo, format) || ExportAllowed(KindImage, format) {
			t.Fatalf("%s belongs to video projects only", format)
		}
	}
	for _, format := range []ExportFormat{ExportPDF, ExportSVG} {
		if !ExportAllowed(KindImage, format) || ExportAllowed(KindVideo, format) {
			t.Fatalf("%s belongs to image projects only", format)
		}
	}
	if ExportAllowed(KindVideo, "tiff") || ExportAllowed(Kind("audio"), ExportMP4) {
		t.Fatal("unknown formats and kinds export nothing")
	}
}

func TestEachExportFormatHasItsContentTypeAndExtension(t *testing.T) {
	want := map[ExportFormat][2]string{
		ExportMP4:  {"video/mp4", ".mp4"},
		ExportGIF:  {"image/gif", ".gif"},
		ExportWebP: {"image/webp", ".webp"},
		ExportPDF:  {"application/pdf", ".pdf"},
		ExportSVG:  {"image/svg+xml", ".svg"},
		ExportPNG:  {"image/png", ".png"},
		ExportJPEG: {"image/jpeg", ".jpg"},
	}
	for format, pair := range want {
		if format.ContentType() != pair[0] || ExportKey("ws-1", "p-1", "e-1", format) != "studio-exports/ws-1/p-1/e-1"+pair[1] {
			t.Fatalf("%s: %q %q", format, format.ContentType(), ExportKey("ws-1", "p-1", "e-1", format))
		}
	}
}

func TestAnExportIsReadWhileItStaysWithinTheLimit(t *testing.T) {
	video := mp4(4096)
	got, err := io.ReadAll(NewExportReader(iotest.OneByteReader(bytes.NewReader(video))))
	if err != nil || !bytes.Equal(got, video) {
		t.Fatalf("read %d bytes, err %v", len(got), err)
	}
}

func TestAnExportOverTheLimitStopsWhileItIsRead(t *testing.T) {
	_, err := io.Copy(io.Discard, NewExportReader(mp4Stream(MaxExportBytes+1)))
	if !errors.Is(err, ErrExportTooLarge) {
		t.Fatalf("err %v", err)
	}
	if _, err := io.Copy(io.Discard, NewExportReader(mp4Stream(MaxExportBytes))); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

func TestAnExportDescriptionFallsBackToTheProjectNameAndHasALimit(t *testing.T) {
	if got, err := ExportDescription("  ", "Reels"); err != nil || got != "Reels" {
		t.Fatalf("blank: %q %v", got, err)
	}
	if got, err := ExportDescription("  Promo   Feed ", "Reels"); err != nil || got != "Promo Feed" {
		t.Fatalf("named: %q %v", got, err)
	}
	if _, err := ExportDescription(string(bytes.Repeat([]byte("á"), MaxExportDescriptionRunes+1)), "Reels"); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("too long: %v", err)
	}
}
