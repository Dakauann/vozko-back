package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqWithHeaders(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestGetClientIP_PrefersCloudflareConnectingIP(t *testing.T) {
	r := reqWithHeaders("10.0.0.1:1234", map[string]string{
		"CF-Connecting-IP": "203.0.113.7",
		"X-Real-IP":        "198.51.100.9",
		"X-Forwarded-For":  "66.66.66.66, 5.6.7.8",
	})
	got := getClientIP(r)
	if got != "203.0.113.7" {
		t.Fatalf("expected CF-Connecting-IP to win, got %s", got)
	}
	if got == "66.66.66.66" {
		t.Fatal("spoofable X-Forwarded-For must never override Cloudflare's authoritative header")
	}
}

func TestGetClientIP_FallsBackToXRealIP(t *testing.T) {
	r := reqWithHeaders("10.0.0.1:1234", map[string]string{
		"X-Real-IP":       "198.51.100.9",
		"X-Forwarded-For": "1.2.3.4",
	})
	if got := getClientIP(r); got != "198.51.100.9" {
		t.Fatalf("expected X-Real-IP, got %s", got)
	}
}

func TestGetClientIP_FallsBackToXFFThenRemoteAddr(t *testing.T) {
	r := reqWithHeaders("10.0.0.1:1234", map[string]string{
		"X-Forwarded-For": "1.2.3.4, 5.6.7.8",
	})
	if got := getClientIP(r); got != "1.2.3.4" {
		t.Fatalf("expected first XFF entry as last-resort fallback, got %s", got)
	}

	r2 := reqWithHeaders("192.168.1.50:5555", nil)
	if got := getClientIP(r2); got != "192.168.1.50" {
		t.Fatalf("expected RemoteAddr host, got %s", got)
	}
}

func TestGetClientIP_StripsIPv6BracketsAndPort(t *testing.T) {
	r := reqWithHeaders("[2001:db8::1]:443", nil)
	if got := getClientIP(r); got != "2001:db8::1" {
		t.Fatalf("expected IPv6 without brackets/port, got %s", got)
	}
}
