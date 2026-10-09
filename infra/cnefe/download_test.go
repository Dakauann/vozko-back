package cnefe

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func payload() []byte {
	return bytes.Repeat([]byte("0123456789abcdef"), 4096)
}

type rangeServer struct {
	body        []byte
	ignoreRange bool
	failFirst   int32
	requests    atomic.Int32
	ranges      []string
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.requests.Add(1)
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	if n <= s.failFirst {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	header := r.Header.Get("Range")
	if header == "" || s.ignoreRange {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.body)
		return
	}
	start, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if start >= len(s.body) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(s.body)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.body)-1, len(s.body)))
	w.Header().Set("Content-Length", strconv.Itoa(len(s.body)-start))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(s.body[start:])
}

func downloader() *Downloader {
	return &Downloader{Client: http.DefaultClient, Retries: 3, Wait: func(context.Context, time.Duration) error { return nil }}
}

func TestFetchResumesAPartialDownloadWithARangeRequest(t *testing.T) {
	server := &rangeServer{body: payload()}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	half := len(server.body) / 2
	if err := os.WriteFile(dest+partSuffix, server.body[:half], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloader().Fetch(context.Background(), srv.URL+"/14_RR.zip", dest); err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, server.body) {
		t.Fatalf("the resumed file differs from the source (%d of %d bytes)", len(got), len(server.body))
	}
	if server.ranges[0] != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("first request asked %q, want the bytes after the %d already on disk", server.ranges[0], half)
	}
	if _, err := os.Stat(dest + partSuffix); !os.IsNotExist(err) {
		t.Fatal("the partial file must be gone once the download is complete")
	}
}

func TestFetchStartsOverWhenTheServerIgnoresTheRange(t *testing.T) {
	server := &rangeServer{body: payload(), ignoreRange: true}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	if err := os.WriteFile(dest+partSuffix, []byte("garbage that must not stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloader().Fetch(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, server.body) {
		t.Fatal("a full answer must replace the partial file, not be appended to it")
	}
}

func TestFetchRetriesAfterAServerError(t *testing.T) {
	server := &rangeServer{body: payload(), failFirst: 2}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	if err := downloader().Fetch(context.Background(), srv.URL, dest); err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	if server.requests.Load() != 3 {
		t.Fatalf("requests = %d, want two failures and one success", server.requests.Load())
	}
}

func TestFetchGivesUpAfterItsRetries(t *testing.T) {
	server := &rangeServer{body: payload(), failFirst: 100}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	if err := downloader().Fetch(context.Background(), srv.URL, dest); err == nil {
		t.Fatal("Fetch() must fail once the retries are spent")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("a failed download must never leave a file that looks complete")
	}
}

func TestFetchKeepsACompleteFile(t *testing.T) {
	server := &rangeServer{body: payload()}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	if err := os.WriteFile(dest, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloader().Fetch(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	if server.requests.Load() != 0 {
		t.Fatalf("requests = %d, want none for a file already downloaded", server.requests.Load())
	}
}

func TestFetchFinishesAPartialFileTheServerSaysIsComplete(t *testing.T) {
	server := &rangeServer{body: payload()}
	srv := httptest.NewServer(server)
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "14_RR.zip")
	if err := os.WriteFile(dest+partSuffix, server.body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloader().Fetch(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, server.body) {
		t.Fatal("the complete partial file must become the download")
	}
}
