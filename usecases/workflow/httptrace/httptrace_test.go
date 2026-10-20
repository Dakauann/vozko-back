package httptrace

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeadersJoinRepeatedValuesSoNoValueIsLost(t *testing.T) {
	got := Headers(http.Header{"Set-Cookie": {"a=1", "b=2"}, "X-Id": {"9"}})
	if got["Set-Cookie"] != "a=1, b=2" || got["X-Id"] != "9" {
		t.Fatalf("got %v", got)
	}
	if Headers(nil) != nil {
		t.Fatal("no headers stay nil")
	}
}

func TestRedirectsCountsEveryHopTheClientFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			http.Redirect(w, r, "/c", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/a")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := Redirects(resp); got != 2 {
		t.Fatalf("redirects = %d, want 2", got)
	}
	if Redirects(nil) != 0 {
		t.Fatal("no response has no redirects")
	}
}
