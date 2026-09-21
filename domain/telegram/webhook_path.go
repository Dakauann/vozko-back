package telegram

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

const (
	WebhookPathPrefix   = "/webhooks/telegram"
	WebhookPathTemplate = WebhookPathPrefix + "/{accountId}"
	BusinessWebhookPath = WebhookPathPrefix + "/business"
)

func WebhookURLFor(baseURL, accountID string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + WebhookPathPrefix + "/" + accountID
}

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

const SecretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

func GenerateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("telegram: generate webhook secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func GenerateDeepLinkToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("telegram: generate deep link token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
