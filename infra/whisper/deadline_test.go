package whisper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func slowWhisperCpp(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"text":" Olá.","language":"pt","duration":1,"segments":[{"id":0,"start":0,"end":1,"text":" Olá."}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestALongTranscriptionRunsUntilTheCallersDeadline(t *testing.T) {
	srv := slowWhisperCpp(t, 200*time.Millisecond)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt", Timeout: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.TranscribeSegments(ctx, []byte("RIFF0000WAVEdata"), "pt"); err != nil {
		t.Fatalf("a caller that allows more time must get it, got %v", err)
	}
}

func TestATranscriptionWithoutADeadlineStopsAtTheDefault(t *testing.T) {
	srv := slowWhisperCpp(t, 500*time.Millisecond)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt", Timeout: 50 * time.Millisecond})
	started := time.Now()
	if _, err := client.TranscribeSegments(context.Background(), []byte("RIFF0000WAVEdata"), "pt"); err == nil {
		t.Fatal("without a deadline of its own, the call must stop at the default timeout")
	}
	if elapsed := time.Since(started); elapsed > 400*time.Millisecond {
		t.Fatalf("the default timeout was not applied, the call took %v", elapsed)
	}
}
