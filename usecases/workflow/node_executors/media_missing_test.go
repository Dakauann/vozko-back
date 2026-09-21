package node_executors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMissingMediaIsReportedNotFallenBackOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		wantFlag bool
	}{
		{"not found", http.StatusNotFound, true},
		{"gone", http.StatusGone, true},
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
	twice := errors.Join(errors.New("send media"), err)
	if !errors.Is(twice, errMediaNotFound) {
		t.Error("the marker must survive wrapping, or the send path cannot act on it")
	}
}
