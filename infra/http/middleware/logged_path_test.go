package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const webhookToken = "Zk3pQm8Lx2AbCdEfGh1234567890abcdefghijklmno"

func TestALoggedPathNeverCarriesAWebhookToken(t *testing.T) {
	if got := LoggedPath("/webhooks/unofficial-whatsapp/" + webhookToken); strings.Contains(got, webhookToken) {
		t.Fatalf("logged path = %q, want the delivery token hidden", got)
	}
	if got := LoggedPath("/auth/login"); got != "/auth/login" {
		t.Fatalf("logged path = %q, want an ordinary path unchanged", got)
	}
}

func TestARejectedWebhookIsLoggedWithoutItsToken(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	handler := NewRateLimiterMiddleware(&mockRateLimiter{allowed: false}).Validate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/unofficial-whatsapp/"+webhookToken, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(out.String(), webhookToken) || !strings.Contains(out.String(), "REJECT") {
		t.Fatalf("rate limit log = %q, want the rejection logged without the delivery token", out.String())
	}
}
