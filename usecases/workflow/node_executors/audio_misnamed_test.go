package node_executors

import (
	"net/http"
	"testing"
)

var mp3WithCoverArt = append([]byte("ID3\x04\x00\x00\x00\x00\x01Lw"), make([]byte, 512)...)

func TestSniffingSeesAudioWhereTheNameSaysOtherwise(t *testing.T) {
	got := http.DetectContentType(mp3WithCoverArt)
	if got != "audio/mpeg" {
		t.Fatalf("DetectContentType = %q, want audio/mpeg — the re-route hangs on this", got)
	}
	if detectMediaType("https://cdn.example.com/x.mpeg") != "document" {
		t.Error("detectMediaType no longer answers document for .mpeg; this test describes why that mattered")
	}
}

func TestHonestExtensionsStillRouteByName(t *testing.T) {
	for url, want := range map[string]string{
		"https://cdn.example.com/a.mp3":     "audio",
		"https://cdn.example.com/a.ogg":     "audio",
		"https://cdn.example.com/a.opus":    "audio",
		"https://cdn.example.com/a.wav":     "audio",
		"https://cdn.example.com/a.jpg":     "image",
		"https://cdn.example.com/a.png?x=1": "image",
		"https://cdn.example.com/a.mp4":     "video",
		"https://cdn.example.com/a.pdf":     "document",
		"https://cdn.example.com/a.docx":    "document",
	} {
		if got := detectMediaType(url); got != want {
			t.Errorf("detectMediaType(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestNonAudioBytesAreNotReRouted(t *testing.T) {
	pdf := append([]byte("%PDF-1.4"), make([]byte, 512)...)
	if got := http.DetectContentType(pdf); got == "audio/mpeg" || len(got) > 5 && got[:6] == "audio/" {
		t.Fatalf("a PDF sniffed as %q would be re-routed as audio", got)
	}
}
