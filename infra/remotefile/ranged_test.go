package remotefile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func rangedServer(t *testing.T, body []byte, served *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counting := &countingWriter{ResponseWriter: w, served: served}
		http.ServeContent(counting, r, "file.mp4", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(server.Close)
	return server
}

type countingWriter struct {
	http.ResponseWriter
	served *atomic.Int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.served.Add(int64(len(p)))
	return w.ResponseWriter.Write(p)
}

func TestARemoteFileKnowsItsSizeAndReadsOnlyTheRangesAsked(t *testing.T) {
	body := make([]byte, 4<<20)
	copy(body[1000:], "middle")
	copy(body[len(body)-3:], "end")
	var served atomic.Int64
	server := rangedServer(t, body, &served)
	file, err := NewFetcher(server.Client(), 64).Open(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if file.Size() != int64(len(body)) {
		t.Fatalf("size %d", file.Size())
	}
	middle := make([]byte, 6)
	if n, err := file.ReadAt(middle, 1000); err != nil || n != 6 || string(middle) != "middle" {
		t.Fatalf("middle %q n %d err %v", middle, n, err)
	}
	tail := make([]byte, 8)
	n, err := file.ReadAt(tail, int64(len(body)-3))
	if n != 3 || string(tail[:n]) != "end" || !errors.Is(err, io.EOF) {
		t.Fatalf("tail %q n %d err %v", tail[:n], n, err)
	}
	if served.Load() > 64 {
		t.Fatalf("the server sent %d bytes for 9 asked", served.Load())
	}
}

func TestAServerThatIgnoresRangesIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer server.Close()
	if _, err := NewFetcher(server.Client(), 64).Open(context.Background(), server.URL); !errors.Is(err, ErrNoRanges) {
		t.Fatalf("err %v", err)
	}
}

func TestAFailedRemoteFileIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	if _, err := NewFetcher(server.Client(), 64).Open(context.Background(), server.URL); err == nil {
		t.Fatal("an error status must be refused")
	}
}
