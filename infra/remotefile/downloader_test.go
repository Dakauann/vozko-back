package remotefile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
)

func TestADownloadKeepsTheBytesAndTheContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write([]byte("voice"))
	}))
	defer server.Close()
	file, err := NewDownloader(server.Client()).Download(context.Background(), server.URL, 64)
	if err != nil || string(file.Data) != "voice" || file.ContentType != "audio/ogg" {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}

func TestOnlyNotFoundAndGoneMeanTheFileIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		missing bool
	}{
		{"not found", http.StatusNotFound, true},
		{"gone", http.StatusGone, true},
		{"forbidden to us", http.StatusForbidden, false},
		{"origin erroring", http.StatusInternalServerError, false},
		{"rate limited", http.StatusTooManyRequests, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			_, err := NewDownloader(server.Client()).Download(context.Background(), server.URL+"/foto.jpeg", 64)
			if err == nil {
				t.Fatal("a non-2xx answer must be an error")
			}
			if got := errors.Is(err, media.ErrRemoteFileMissing); got != tc.missing {
				t.Fatalf("missing = %v, want %v (err: %v)", got, tc.missing, err)
			}
		})
	}
}

func TestADownloadOverTheLimitIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	if _, err := NewDownloader(server.Client()).Download(context.Background(), server.URL, 64); err == nil {
		t.Fatal("a file over the limit must be refused")
	}
}

func TestTheGuardedDownloaderRefusesInternalAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer server.Close()
	downloader := NewDownloader(netguard.NewHTTPClient(2 * time.Second))
	for _, url := range []string{server.URL, "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/", "http://[::1]/"} {
		_, err := downloader.Download(context.Background(), url, 64)
		if !errors.Is(err, shared.ErrEgressBlocked) {
			t.Fatalf("%s: err = %v, want the egress refusal", url, err)
		}
	}
}
