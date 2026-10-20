package container

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/shared"
)

func TestMediaSendsDownloadOnlyThroughTheGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer server.Close()

	for _, url := range []string{server.URL + "/a.png", "http://169.254.169.254/latest/meta-data/", "http://192.168.0.10/a.png"} {
		if _, err := mediaDownloads().Download(context.Background(), url, 1024); !errors.Is(err, shared.ErrEgressBlocked) {
			t.Fatalf("%s: err = %v, a media send reached an internal address", url, err)
		}
	}
}

func TestMediaClientsOfFormsVoiceAndRendersAreGuarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer server.Close()

	for name, client := range map[string]*http.Client{
		"form images":   formImageClient(),
		"voice audio":   voiceAudioClient(),
		"media renders": renderMediaClient(),
	} {
		if _, err := client.Get(server.URL); !errors.Is(err, shared.ErrEgressBlocked) {
			t.Fatalf("%s: err = %v, the client reached an internal address", name, err)
		}
	}
}
