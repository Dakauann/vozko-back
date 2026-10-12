package openroutergen

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/mediagen"
)

type unreachableFirst struct {
	mu       sync.Mutex
	failures int
	next     http.RoundTripper
}

func (u *unreachableFirst) RoundTrip(req *http.Request) (*http.Response, error) {
	u.mu.Lock()
	fail := u.failures > 0
	if fail {
		u.failures--
	}
	u.mu.Unlock()
	if fail {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return u.next.RoundTrip(req)
}

func countingImageAPI(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 20, 20))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"id":"gen-1","data":[{"b64_json":"` + encoded + `"}],"usage":{"cost":0.04}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestARequestThatNeverReachedTheProviderIsSentAgain(t *testing.T) {
	var hits atomic.Int32
	api := countingImageAPI(t, &hits)
	client := &http.Client{Transport: &unreachableFirst{failures: 2, next: http.DefaultTransport}}
	g, err := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	g.unsent = []time.Duration{0, 0}

	out, err := g.Generate(context.Background(), request(mediagen.AspectSquare), nil)
	if err != nil {
		t.Fatalf("a request that never left must be sent again, got %v", err)
	}
	if hits.Load() != 1 || out.ProviderCostMicros != 40000 {
		t.Fatalf("the provider must see exactly one request, saw %d (out %+v)", hits.Load(), out)
	}
}

func TestTheUnsentRetriesAreBounded(t *testing.T) {
	var hits atomic.Int32
	api := countingImageAPI(t, &hits)
	client := &http.Client{Transport: &unreachableFirst{failures: 10, next: http.DefaultTransport}}
	g, _ := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: client})
	g.unsent = []time.Duration{0, 0}

	if _, err := g.Generate(context.Background(), request(mediagen.AspectSquare), nil); err == nil {
		t.Fatal("an unreachable provider must fail the job once the retries run out")
	}
	if hits.Load() != 0 {
		t.Fatalf("nothing reached the provider, saw %d", hits.Load())
	}
}

func TestARequestThatMayHaveReachedTheProviderIsNeverSentAgain(t *testing.T) {
	var hits atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(api.Close)
	g, _ := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: &http.Client{Transport: http.DefaultTransport}})
	g.unsent = []time.Duration{0, 0}

	if _, err := g.Generate(context.Background(), request(mediagen.AspectSquare), nil); err == nil {
		t.Fatal("a dropped answer must fail the job")
	}
	if hits.Load() != 1 {
		t.Fatalf("a request the provider may have billed must never be sent again, saw %d", hits.Load())
	}
}

func TestTheUnsentRetryStopsWithTheJob(t *testing.T) {
	var hits atomic.Int32
	api := countingImageAPI(t, &hits)
	client := &http.Client{Transport: &unreachableFirst{failures: 10, next: http.DefaultTransport}}
	g, _ := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: client})
	g.unsent = []time.Duration{time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := g.Generate(ctx, request(mediagen.AspectSquare), nil); err == nil {
		t.Fatal("a cancelled job must fail")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("the wait outlived the job")
	}
}

func TestADroppedImageDownloadIsFetchedAgainBecauseItCostsNothing(t *testing.T) {
	image := pngBytes(t, 40, 40)
	var downloads atomic.Int32
	imageSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if downloads.Add(1) == 1 {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		_, _ = w.Write(image)
	}))
	t.Cleanup(imageSrv.Close)
	var generations atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		generations.Add(1)
		_, _ = w.Write([]byte(`{"id":"gen-2","data":[{"url":"` + imageSrv.URL + `/img.png"}],"usage":{"cost":0.02}}`))
	}))
	t.Cleanup(api.Close)
	g, _ := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: imageSrv.Client()})
	g.unsent = []time.Duration{0, 0}

	out, err := g.Generate(context.Background(), request(mediagen.AspectSquare), nil)
	if err != nil {
		t.Fatalf("a paid image must not be lost to one dropped download, got %v", err)
	}
	if generations.Load() != 1 || downloads.Load() != 2 || len(out.Bytes) == 0 {
		t.Fatalf("generations=%d downloads=%d", generations.Load(), downloads.Load())
	}
}
