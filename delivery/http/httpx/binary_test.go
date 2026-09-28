package httpx

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestWriteBinarySetsTypeLengthAndPrivateCaching(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteBinary(rec, []byte("abc"), "", "image/jpeg", 5*time.Minute)

	if rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Content-Length") != "3" || rec.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if rec.Body.String() != "abc" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestWriteBinaryKeepsTheReportedType(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteBinary(rec, []byte("x"), "image/png", "image/jpeg", time.Hour)
	if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatalf("headers = %v", rec.Header())
	}
}
