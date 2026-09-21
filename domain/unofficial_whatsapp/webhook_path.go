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

const (
	WebhookPathPrefix   = "/webhooks/unofficial-whatsapp"
	WebhookPathTemplate = WebhookPathPrefix + "/{deliveryToken}"
)

const deliveryTokenBytes = 32

func GenerateDeliveryToken() (string, error) {
	buf := make([]byte, deliveryTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("unofficial whatsapp: generate delivery token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashDeliveryToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func DeliveryTokenMatches(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func WebhookURLFor(baseURL, deliveryToken string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + WebhookPathPrefix + "/" + deliveryToken
}

func RedactWebhookPath(path string) string {
	if !strings.HasPrefix(path, WebhookPathPrefix+"/") {
		return path
	}
	return WebhookPathPrefix + "/[redacted]"
}

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
