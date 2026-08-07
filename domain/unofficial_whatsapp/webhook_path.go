package unofficial_whatsapp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// The webhook URL is the only authenticity control this channel has.
//
// The provider does not sign webhook bodies — there is no HMAC, no shared
// secret, no signing header — and its webhook configuration accepts nothing but
// a URL, so we cannot even ask it to echo a header of ours. Meta signs with
// X-Hub-Signature-256 and Telegram echoes a secret token; this provider does
// neither.
//
// So the path segment IS the credential, and it is treated as one:
//
//   - 32 random bytes, never our instance uuid, which is guessable from any
//     other API response;
//   - stored as a SHA-256 digest for lookup, so a dumped database row does not
//     yield a working URL;
//   - rotatable in one call, because leakage through a proxy log or an error
//     reporter is a when, not an if;
//   - redacted from request logs (see the delivery layer).
//
// It is still a bearer token in a URL. The residual risk — someone holding the
// URL can inject fabricated inbound messages — is a property of a provider that
// does not sign, not of this implementation. The instance-id cross-check in the
// handler narrows it; nothing here closes it.
const (
	// WebhookPathPrefix is the mount point for per-instance webhooks.
	WebhookPathPrefix = "/webhooks/unofficial-whatsapp"
	// WebhookPathTemplate is the gorilla/mux pattern for the endpoint.
	WebhookPathTemplate = WebhookPathPrefix + "/{deliveryToken}"
)

// deliveryTokenBytes is the entropy behind one webhook URL. 32 bytes is well
// beyond guessing range and renders as 43 base64url characters.
const deliveryTokenBytes = 32

// GenerateDeliveryToken mints a webhook path segment.
//
// base64url without padding keeps it safe in a URL path without escaping, which
// matters because an escaped token in a registered URL is a token the provider
// will send us and we will fail to match.
func GenerateDeliveryToken() (string, error) {
	buf := make([]byte, deliveryTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("unofficial whatsapp: generate delivery token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashDeliveryToken is the indexed lookup key for a token.
//
// SHA-256 rather than a password hash on purpose: this is a high-entropy random
// value, not a human secret, so there is nothing to slow down a guesser about —
// and the lookup is on the inbound hot path, where a deliberately slow hash
// would be a denial-of-service surface of its own.
func HashDeliveryToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// DeliveryTokenMatches compares two tokens in constant time.
func DeliveryTokenMatches(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WebhookURLFor builds the URL registered with the provider.
func WebhookURLFor(baseURL, deliveryToken string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + WebhookPathPrefix + "/" + deliveryToken
}

// RedactWebhookPath removes the token from a path before it reaches a log.
//
// Called from the access-log middleware. Without it the credential is written
// in plaintext to every log sink on every inbound message, which would make the
// rotation story above meaningless.
func RedactWebhookPath(path string) string {
	if !strings.HasPrefix(path, WebhookPathPrefix+"/") {
		return path
	}
	return WebhookPathPrefix + "/[redacted]"
}

// ValidateWebhookBaseURL checks a configured base URL at boot.
//
// The failure this catches is silent: a wrong scheme or a stray path produces no
// error at registration time, only messages that never arrive. Both existing
// channels validate their base URL at boot for the same reason.
func ValidateWebhookBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("unofficial whatsapp: webhook base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("unofficial whatsapp: webhook base URL %q is not a valid URL: %w", raw, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("unofficial whatsapp: webhook base URL %q must be absolute (scheme + host)", raw)
	}

	isLocal := strings.HasPrefix(parsed.Hostname(), "localhost") || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !isLocal {
		return fmt.Errorf(
			"unofficial whatsapp: webhook base URL %q must use https; the delivery token travels in the path "+
				"and is the channel's only authenticity control", raw)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf(
			"unofficial whatsapp: webhook base URL %q must not carry a path, the path is owned by the code (%s)",
			raw, WebhookPathTemplate)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("unofficial whatsapp: webhook base URL %q must not carry a query string or fragment", raw)
	}
	return nil
}
