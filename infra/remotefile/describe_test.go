package remotefile

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDescribeSniffsTheTypeAndReadsTheSize(t *testing.T) {
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), 4000)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "file", time.Time{}, bytes.NewReader(pdf))
	}))
	defer srv.Close()

	contentType, size, err := NewFetcher(srv.Client(), 0).Describe(context.Background(), srv.URL)
	if err != nil || contentType != "application/pdf" || size != int64(len(pdf)) {
		t.Fatalf("Describe = %q, %d, %v", contentType, size, err)
	}
}

func TestDescribeNeedsRanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("%PDF-1.7"))
	}))
	defer srv.Close()
	if _, _, err := NewFetcher(srv.Client(), 0).Describe(context.Background(), srv.URL); err == nil {
		t.Fatal("a server without ranges cannot be described without downloading the whole file")
	}
}
