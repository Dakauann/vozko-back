package uazapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

func TestAVoiceNoteSourceIsDownloadedOnlyThroughTheGuard(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer internal.Close()

	guarded := NewVoiceTranscoder(remotefile.NewDownloader(netguard.NewHTTPClient(2 * time.Second)))
	for _, url := range []string{internal.URL + "/voice.ogg", "http://169.254.169.254/latest/meta-data/"} {
		if _, err := guarded.ToVoiceNote(context.Background(), url); !errors.Is(err, shared.ErrEgressBlocked) {
			t.Fatalf("%s: err = %v, want the egress refusal", url, err)
		}
	}
	if _, err := NewVoiceTranscoder(nil).ToVoiceNote(context.Background(), internal.URL+"/voice.ogg"); !errors.Is(err, media.ErrRemoteFilesUnwired) {
		t.Fatalf("err = %v, want the unwired refusal", err)
	}
}
