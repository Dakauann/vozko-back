package whatsapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

func TestTemplateHeaderMediaIsDownloadedOnlyThroughTheGuard(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no upload may start for a refused download: %s %s", r.Method, r.URL)
	}))
	defer api.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	defer internal.Close()

	cases := map[string]struct {
		downloads media.RemoteFiles
		url       string
		want      error
	}{
		"loopback":        {remotefile.NewDownloader(netguard.NewHTTPClient(2 * time.Second)), internal.URL + "/promo.png", shared.ErrEgressBlocked},
		"cloud metadata":  {remotefile.NewDownloader(netguard.NewHTTPClient(2 * time.Second)), "http://169.254.169.254/latest/meta-data/", shared.ErrEgressBlocked},
		"no guarded path": {nil, internal.URL + "/promo.png", media.ErrRemoteFilesUnwired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewClient(Config{
				BaseURL:        api.URL,
				PhoneNumberID:  "PNID",
				AccessToken:    "token",
				AppID:          "APP",
				HTTPClient:     api.Client(),
				MediaDownloads: tc.downloads,
			})
			_, err := c.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{URL: tc.url, FileName: "promo.png"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
