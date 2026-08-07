package unofficial_whatsapp

import (
	"strings"
	"testing"
)

// The delivery token is the ONLY authenticity control this channel has: the
// provider signs nothing and accepts no header of ours. Every assertion here is
// about that credential not leaking and not being guessable.

func TestDeliveryTokensAreUniqueAndURLSafe(t *testing.T) {
	seen := make(map[string]struct{}, 128)
	for i := 0; i < 128; i++ {
		token, err := GenerateDeliveryToken()
		if err != nil {
			t.Fatalf("GenerateDeliveryToken: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("token repeated after %d draws", i)
		}
		seen[token] = struct{}{}

		// It travels in a URL path. Anything needing escaping would be
		// registered escaped and arrive unescaped, and the lookup would miss —
		// a channel that silently receives nothing.
		if strings.ContainsAny(token, "/?#%&= ") {
			t.Fatalf("token %q needs URL escaping", token)
		}
		// 32 random bytes as unpadded base64url.
		if len(token) < 40 {
			t.Fatalf("token %q is shorter than expected; entropy may have been lost", token)
		}
	}
}

// The lookup key must be the digest, never the token. A dumped instances table
// must not yield working webhook URLs.
func TestHashDeliveryTokenIsStableAndOneWay(t *testing.T) {
	token, err := GenerateDeliveryToken()
	if err != nil {
		t.Fatalf("GenerateDeliveryToken: %v", err)
	}

	hash := HashDeliveryToken(token)
	if hash == token {
		t.Fatal("the stored lookup key must not be the token itself")
	}
	if hash != HashDeliveryToken(token) {
		t.Fatal("hashing must be stable or the webhook stops resolving")
	}
	if len(hash) != 64 {
		t.Fatalf("hash length = %d, want 64 hex characters", len(hash))
	}
	if strings.Contains(hash, token) {
		t.Fatal("the digest leaks the token")
	}

	other, _ := GenerateDeliveryToken()
	if HashDeliveryToken(other) == hash {
		t.Fatal("distinct tokens collided")
	}
}

func TestDeliveryTokenMatches(t *testing.T) {
	if !DeliveryTokenMatches("abc", "abc") {
		t.Error("equal tokens must match")
	}
	if DeliveryTokenMatches("abc", "abd") || DeliveryTokenMatches("abc", "ab") {
		t.Error("unequal tokens must not match")
	}
}

// The access log must never carry the credential. Without this the rotation
// story is meaningless: the token would be written in plaintext to every log
// sink on every inbound message.
func TestRedactWebhookPathHidesTheToken(t *testing.T) {
	token, _ := GenerateDeliveryToken()
	path := WebhookPathPrefix + "/" + token

	redacted := RedactWebhookPath(path)
	if strings.Contains(redacted, token) {
		t.Fatalf("redaction left the token in %q", redacted)
	}
	if !strings.HasPrefix(redacted, WebhookPathPrefix) {
		t.Errorf("redaction lost the route: %q", redacted)
	}

	// Unrelated paths pass through untouched, so the middleware can call this
	// on everything.
	if got := RedactWebhookPath("/conversations/123"); got != "/conversations/123" {
		t.Errorf("unrelated path was rewritten to %q", got)
	}
}

func TestWebhookURLFor(t *testing.T) {
	got := WebhookURLFor("https://api.example.com/", "tok3n")
	want := "https://api.example.com" + WebhookPathPrefix + "/tok3n"
	if got != want {
		t.Errorf("WebhookURLFor = %q, want %q", got, want)
	}
}

// The base URL is validated at boot because every failure here is SILENT: a
// wrong scheme or a stray path produces no error at registration time, only
// events that never arrive.
func TestValidateWebhookBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https origin", "https://api.example.com", false},
		{"trailing slash tolerated", "https://api.example.com/", false},
		{"localhost for development", "http://localhost:8080", false},
		{"empty", "", true},
		// The token travels in the path; plaintext http would hand the
		// channel's only credential to anyone on the wire.
		{"plain http in production", "http://api.example.com", true},
		{"relative", "/webhooks", true},
		{"carries a path we own", "https://api.example.com/api/v1", true},
		{"carries a query", "https://api.example.com?x=1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWebhookBaseURL(tc.url)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateWebhookBaseURL(%q) error = %v, wantErr %v", tc.url, err, tc.wantErr)
			}
		})
	}
}
