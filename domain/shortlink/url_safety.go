package shortlink

import (
	"net/url"
	"strings"
)

func ValidateTargetURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ErrTargetURLRequired
	}
	if len(raw) > MaxTargetURLLength {
		return ErrTargetURLTooLong
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ErrTargetURLInvalid
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return ErrTargetURLScheme
	}

	if u.Host == "" || u.Hostname() == "" {
		return ErrTargetURLInvalid
	}
	if u.User != nil {
		return ErrTargetURLCredentials
	}
	return nil
}

func TargetHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}
