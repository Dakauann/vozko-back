package tools_usecase

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// oggVorbisServer serves the exact shape that fooled the link path: a real OGG
// container whose codec is Vorbis, not Opus, under a .ogg name and an audio/ogg
// Content-Type. Everything about it looks right except the one thing WhatsApp
// checks.
func oggVorbisServer(t *testing.T) string {
	t.Helper()
	body := append([]byte("OggS\x00\x02"), []byte("\x00\x00\x00\x00\x00\x00\x00\x00vorbis")...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/jingle.ogg"
}
