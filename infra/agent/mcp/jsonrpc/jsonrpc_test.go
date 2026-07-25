package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "echo" {
			http.Error(w, "bad method", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no bearer", 401)
			return
		}
		if r.Header.Get("X-Test-Header") != "yes" {
			http.Error(w, "no extra", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"ok":true}`)})
	}))
	defer srv.Close()
	var out struct {
		OK bool `json:"ok"`
	}
	_, err := Call(context.Background(), Options{
		URL: srv.URL, HTTP: http.DefaultClient, ID: 9, BearerToken: "tok",
		ExtraHeader: map[string]string{"X-Test-Header": "yes"},
	}, "echo", map[string]any{"a": 1}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("not ok")
	}
}

func TestCallNilHTTP(t *testing.T) {
	if _, err := Call(context.Background(), Options{}, "x", nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCallHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := Call(context.Background(), Options{URL: srv.URL, HTTP: http.DefaultClient}, "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: 1, Error: &ErrorObject{Code: -32001, Message: "nope"}})
	}))
	defer srv.Close()
	_, err := Call(context.Background(), Options{URL: srv.URL, HTTP: http.DefaultClient, ID: 1}, "x", nil, nil)
	var rpc *ErrorObject
	if !errors.As(err, &rpc) || rpc.Code != -32001 {
		t.Fatalf("err=%v", err)
	}
	if rpc.Error() == "" {
		t.Fatal("error string empty")
	}
}

func TestCallBadResultDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`"a-string"`)})
	}))
	defer srv.Close()
	var out struct {
		X int `json:"x"`
	}
	_, err := Call(context.Background(), Options{URL: srv.URL, HTTP: http.DefaultClient, ID: 1}, "x", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "decode result") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallBadResponseDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-json")
	}))
	defer srv.Close()
	_, err := Call(context.Background(), Options{URL: srv.URL, HTTP: http.DefaultClient}, "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallMarshalParamsFail(t *testing.T) {
	_, err := Call(context.Background(), Options{URL: "http://x", HTTP: http.DefaultClient}, "x", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshal params") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallBadURL(t *testing.T) {
	_, err := Call(context.Background(), Options{URL: "://bad", HTTP: http.DefaultClient}, "x", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallTransportFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	_, err := Call(context.Background(), Options{URL: srv.URL, HTTP: http.DefaultClient}, "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("err=%v", err)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" {
		t.Fail()
	}
	if !strings.HasSuffix(truncate(strings.Repeat("a", 10), 3), "…") {
		t.Fail()
	}
}
