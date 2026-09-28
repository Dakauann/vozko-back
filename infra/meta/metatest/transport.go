package metatest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type rewriteTransport struct {
	host  string
	inner http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = r.host
	return r.inner.RoundTrip(req)
}

func Server(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse stub url: %v", err)
	}
	return &http.Client{Transport: rewriteTransport{host: base.Host, inner: srv.Client().Transport}}
}
