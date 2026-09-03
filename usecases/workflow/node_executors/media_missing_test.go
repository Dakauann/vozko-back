package node_executors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A media URL that 404s is the origin telling us the file is not there. The
// link fallback exists for media WhatsApp can fetch when we cannot — it cannot
// conjure a file that does not exist, so falling back on a 404 handed WhatsApp
// a URL that 404s for it too. The contact received nothing, the node reported
// sent=true, and the flow continued as though the photo had arrived: the one
// outcome an operator cannot detect and cannot branch on.

func TestMissingMediaIsReportedNotFallenBackOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		wantFlag bool // must be recognisable as "not there"
	}{
		{"not found", http.StatusNotFound, true},
		{"gone", http.StatusGone, true},
		// Everything else may still work through WhatsApp's own fetcher, so it
		// must NOT be treated as missing.
		{"forbidden to us", http.StatusForbidden, false},
		{"origin erroring", http.StatusInternalServerError, false},
		{"rate limited", http.StatusTooManyRequests, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			s := &whatsappSender{}
			_, _, err := s.downloadMedia(context.Background(), srv.URL+"/foto.jpeg")
			if err == nil {
				t.Fatal("a non-2xx download must be an error")
			}
			if got := errors.Is(err, errMediaNotFound); got != tc.wantFlag {
				t.Errorf("errors.Is(err, errMediaNotFound) = %v, want %v (err: %v)", got, tc.wantFlag, err)
			}
		})
	}
}

// The status has to survive being wrapped, because the send path checks the
// error several frames above where it is produced.
func TestMissingMediaSurvivesWrapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := &whatsappSender{}
	_, _, err := s.downloadMedia(context.Background(), srv.URL+"/foto.jpeg")

	if !errors.Is(err, errMediaNotFound) {
		t.Fatalf("direct: %v", err)
	}
	// downloadAndUpload and SendMedia both wrap; simulate two layers.
	twice := errors.Join(errors.New("send media"), err)
	if !errors.Is(twice, errMediaNotFound) {
		t.Error("the marker must survive wrapping, or the send path cannot act on it")
	}
}
