package shortlink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoopScanner(t *testing.T) {
	s := NewThreatScanner("", "", nil)
	verdict, err := s.Scan(context.Background(), "https://x.com")
	if err != nil || !verdict.Safe {
		t.Fatalf("noop scanner = %v %+v", err, verdict)
	}
}

func TestScannerConstructorDefaults(t *testing.T) {
	s := NewThreatScanner("key", "", nil)
	if s == nil {
		t.Fatal("expected scanner")
	}
}

func TestScannerSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := NewThreatScanner("key", srv.URL, srv.Client())
	verdict, err := s.Scan(context.Background(), "https://x.com")
	if err != nil || !verdict.Safe {
		t.Fatalf("safe = %v %+v", err, verdict)
	}
}

func TestScannerFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"matches":[{"threatType":"MALWARE"},{"threatType":"SOCIAL_ENGINEERING"}]}`))
	}))
	defer srv.Close()

	s := NewThreatScanner("key", srv.URL, srv.Client())
	verdict, err := s.Scan(context.Background(), "https://bad.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Safe || len(verdict.Threats) != 2 {
		t.Fatalf("flagged verdict = %+v", verdict)
	}
}

func TestScannerNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	s := NewThreatScanner("key", srv.URL, srv.Client())
	if _, err := s.Scan(context.Background(), "https://x.com"); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestScannerBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	s := NewThreatScanner("key", srv.URL, srv.Client())
	if _, err := s.Scan(context.Background(), "https://x.com"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestScannerRequestBuildError(t *testing.T) {
	s := NewThreatScanner("key", "http://%zz", &http.Client{})
	if _, err := s.Scan(context.Background(), "https://x.com"); err == nil {
		t.Fatal("expected request build error")
	}
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport down")
}

func TestScannerDoError(t *testing.T) {
	s := NewThreatScanner("key", "http://example.com/v4", &http.Client{Transport: errRoundTripper{}})
	if _, err := s.Scan(context.Background(), "https://x.com"); err == nil {
		t.Fatal("expected do error")
	}
}
