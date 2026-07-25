package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestMediaDownloadURL_Dialog360Rewrite verifies the pure host-swap: for
// 360dialog the returned Meta lookaside URL is rewritten to the 360dialog host
// with path + query preserved; for Meta it is returned unchanged.
func TestMediaDownloadURL_Dialog360Rewrite(t *testing.T) {
	const lookaside = "https://lookaside.fbsbx.com/whatsapp_business/attachments/?mid=130345565692730173924&source=getMedia&ext=1664537344507&hash=ATtBt0Cdio"

	d360 := NewClient(Config{
		BaseURL: "https://waba-v2.360dialog.io", AccessToken: "k",
		AuthHeaderName: "D360-API-KEY", OmitPhoneNumberInPath: true,
	}).(*Client)

	got := d360.mediaDownloadURL(lookaside)
	want := "https://waba-v2.360dialog.io/whatsapp_business/attachments/?mid=130345565692730173924&source=getMedia&ext=1664537344507&hash=ATtBt0Cdio"
	if got != want {
		t.Fatalf("360dialog rewrite:\n got=%s\nwant=%s", got, want)
	}
	// Path + query must survive exactly.
	u, _ := url.Parse(got)
	if u.Host != "waba-v2.360dialog.io" || u.Path != "/whatsapp_business/attachments/" {
		t.Fatalf("host/path not preserved: host=%s path=%s", u.Host, u.Path)
	}
	if u.Query().Get("mid") == "" || u.Query().Get("hash") == "" || u.Query().Get("ext") == "" {
		t.Fatalf("query params dropped: %s", u.RawQuery)
	}

	// Meta client leaves the lookaside URL untouched (downloaded with the bearer).
	meta := NewClient(Config{
		BaseURL: "https://graph.facebook.com/v22.0", PhoneNumberID: "123", AccessToken: "t",
	}).(*Client)
	if got := meta.mediaDownloadURL(lookaside); got != lookaside {
		t.Fatalf("meta must not rewrite; got=%s", got)
	}
}

// TestDownloadMedia_Dialog360_HitsChannelHost is the end-to-end regression for
// the inbound-media 401: the byte download must go to the 360dialog host (not
// Meta's lookaside CDN) with the D360-API-KEY, preserving the media path/query.
func TestDownloadMedia_Dialog360_HitsChannelHost(t *testing.T) {
	var infoAuth, dlAuth, dlPath, dlQuery string
	dlHit := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Step 2: the byte download, addressed to THIS (360dialog) host after rewrite.
		if r.URL.Path == "/whatsapp_business/attachments/" {
			dlHit, dlAuth, dlPath, dlQuery = true, r.Header.Get("D360-API-KEY"), r.URL.Path, r.URL.RawQuery
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write([]byte("OGGDATA"))
			return
		}
		// Step 1: GET /{media-id} -> returns a Meta lookaside URL (as 360dialog does).
		infoAuth = r.Header.Get("D360-API-KEY")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":       "https://lookaside.fbsbx.com/whatsapp_business/attachments/?mid=abc&source=getMedia&ext=123&hash=XYZ",
			"mime_type": "audio/ogg",
			"id":        "MEDIA1",
		})
	}))
	defer srv.Close()

	// BaseURL = the test server: it stands in for waba-v2.360dialog.io, so the
	// rewrite target host is srv's host.
	c := NewClient(Config{
		BaseURL: srv.URL, PhoneNumberID: "123", AccessToken: "chankey",
		AuthHeaderName: "D360-API-KEY", OmitPhoneNumberInPath: true,
	}).(*Client)

	data, mime, err := c.DownloadMedia(context.Background(), "MEDIA1")
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if !dlHit {
		t.Fatal("byte download never hit the 360dialog host (still going to lookaside → the 401 bug)")
	}
	if string(data) != "OGGDATA" || mime != "audio/ogg" {
		t.Fatalf("unexpected media: data=%q mime=%q", string(data), mime)
	}
	if infoAuth != "chankey" || dlAuth != "chankey" {
		t.Fatalf("both requests must carry D360-API-KEY; info=%q dl=%q", infoAuth, dlAuth)
	}
	if dlPath != "/whatsapp_business/attachments/" {
		t.Fatalf("download path not preserved: %s", dlPath)
	}
	if u, _ := url.Parse("?" + dlQuery); u.Query().Get("mid") != "abc" || u.Query().Get("hash") != "XYZ" {
		t.Fatalf("download query not preserved: %s", dlQuery)
	}
}
