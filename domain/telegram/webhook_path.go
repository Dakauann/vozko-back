package telegram

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// The webhook path is owned by the code, not by configuration.
//
// Telegram gives us no way to identify which bot an update belongs to: the
// Update object carries no bot id, unlike Meta's entry[].id. Tenancy therefore
// has to come from somewhere outside the body, and there are exactly two places
// it can come from, the URL we registered, and (in business mode) the
// business_connection_id inside the payload.
//
// So each bot gets its own URL, keyed by OUR account uuid. Never by the bot
// token: a token in a URL leaks through proxy logs, Referer headers and error
// reporters, and it is a permanent credential.
const (
	// WebhookPathPrefix is the mount point for per-account bot webhooks.
	WebhookPathPrefix = "/webhooks/telegram"
	// WebhookPathTemplate is the gorilla/mux pattern for the bot-mode endpoint.
	WebhookPathTemplate = WebhookPathPrefix + "/{accountId}"
	// BusinessWebhookPath is the single endpoint for the platform bot that
	// business accounts connect to. Every tenant shares it, and routing happens
	// on business_connection_id.
	BusinessWebhookPath = WebhookPathPrefix + "/business"
)

// WebhookURLFor builds the URL registered with setWebhook.
func WebhookURLFor(baseURL, accountID string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + WebhookPathPrefix + "/" + accountID
}

// ValidateWebhookBaseURL checks that a configured base URL is one Telegram will
// actually deliver to.
//
// Telegram's constraints are unusually strict and failing them produces silence
// rather than an error, so they are checked at boot:
//
//   - "Webhook requires SSL/TLS encryption, no matter which port is used. It's
//     not possible to use a plain-text HTTP webhook."
//   - "We currently support the following ports: 443, 80, 88 and 8443. Other
//     ports are not supported and will not work."
func ValidateWebhookBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("telegram: webhook base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("telegram: webhook base URL %q is not a valid URL: %w", raw, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("telegram: webhook base URL %q must be absolute (scheme + host)", raw)
	}

	isLocal := strings.HasPrefix(parsed.Hostname(), "localhost") || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !isLocal {
		return fmt.Errorf(
			"telegram: webhook base URL %q must use https, Telegram refuses plain-text webhooks entirely", raw)
	}

	if port := parsed.Port(); port != "" && !isLocal {
		switch port {
		case "443", "80", "88", "8443":
		default:
			return fmt.Errorf(
				"telegram: webhook base URL %q uses port %s; Telegram supports only 443, 80, 88 and 8443, "+
					"and silently never delivers to any other port", raw, port)
		}
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf(
			"telegram: webhook base URL %q must not carry a path, the path is owned by the code (%s)",
			raw, WebhookPathTemplate)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("telegram: webhook base URL %q must not carry a query string or fragment", raw)
	}
	return nil
}

// SecretTokenHeader is the header Telegram echoes the secret token in.
const SecretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

// GenerateWebhookSecret mints a per-account secret token.
//
// The Bot API allows "1-256 characters. Only characters A-Z, a-z, 0-9, _ and -",
// which base64url satisfies without padding. 32 random bytes is well beyond
// guessing range, and this value is the ONLY authenticity control the channel
// has, Telegram does not sign the body.
func GenerateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("telegram: generate webhook secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// GenerateDeepLinkToken mints a start-parameter token.
//
// 16 random bytes render as 22 base64url characters, comfortably inside
// Telegram's 64-character ceiling and inside its restricted alphabet.
func GenerateDeepLinkToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("telegram: generate deep link token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
