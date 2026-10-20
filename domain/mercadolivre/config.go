package mercadolivre

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
)

const (
	MinNotificationTokenLength = 43
	MaxNotificationTokenLength = 128
)

var (
	ErrNotificationTokenWeak = errors.New("mercado livre notification token must be 43 to 128 base64url characters (at least 32 random bytes)")
	ErrRedirectURIInvalid    = errors.New("mercado livre redirect uri must be an https url with a host and no query or fragment")
	ErrAppIDInvalid          = errors.New("mercado livre app id must be numeric")
)

var notificationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidateNotificationToken(token string) error {
	if len(token) < MinNotificationTokenLength || len(token) > MaxNotificationTokenLength || !notificationTokenPattern.MatchString(token) {
		return ErrNotificationTokenWeak
	}
	return nil
}

func ValidateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return fmt.Errorf("%w: %q", ErrRedirectURIInvalid, raw)
	}
	return nil
}

func ValidateAppID(appID string) error {
	if !ValidNumericID(appID) {
		return ErrAppIDInvalid
	}
	return nil
}
