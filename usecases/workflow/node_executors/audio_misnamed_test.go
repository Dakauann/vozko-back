package node_executors

import (
	"net/http"
	"testing"
)

// An extension is a claim, not a fact.
//
// Live case: an operator uploaded an MP3 named ".mpeg". detectMediaType reads
// the URL, so it routed as a document; our own CDN stores it as video/mpeg;
// Meta refused the upload ("Received file of type 'video/mpeg'") and refused
// the link for the same reason. Every recipient got an undelivered bubble while
// the file played fine everywhere else. The bytes were audio the whole time,
// and audio only needs transcoding.

// mp3WithCoverArt is the head of the real file that failed: an ID3v2 tag, which
// is what makes http.DetectContentType answer audio/mpeg regardless of the name.
var mp3WithCoverArt = append([]byte("ID3\x04\x00\x00\x00\x00\x01Lw"), make([]byte, 512)...)

func TestSniffingSeesAudioWhereTheNameSaysOtherwise(t *testing.T) {
	got := http.DetectContentType(mp3WithCoverArt)
	if got != "audio/mpeg" {
		t.Fatalf("DetectContentType = %q, want audio/mpeg — the re-route hangs on this", got)
	}
	// And the name, which is what the old routing trusted.
	if detectMediaType("https://cdn.example.com/x.mpeg") != "document" {
		t.Error("detectMediaType no longer answers document for .mpeg; this test describes why that mattered")
	}
}

// The extensions that DO name themselves honestly must keep routing as they did:
// re-routing is a repair for a lie, not a new default.
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

// A document that really is a document must not be dragged into the audio path
// by the re-route: the guard is the audio/ prefix on the sniffed type.
func TestNonAudioBytesAreNotReRouted(t *testing.T) {
	pdf := append([]byte("%PDF-1.4"), make([]byte, 512)...)
	if got := http.DetectContentType(pdf); got == "audio/mpeg" || len(got) > 5 && got[:6] == "audio/" {
		t.Fatalf("a PDF sniffed as %q would be re-routed as audio", got)
	}
}
