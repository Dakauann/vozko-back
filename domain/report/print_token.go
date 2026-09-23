package report

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const PrintTokenTTL = 5 * time.Minute

var (
	ErrPrintTokenInvalid = errors.New("report: the print token is not valid")
	ErrPrintTokenExpired = errors.New("report: the print token has expired")
	ErrPrintSecretUnset  = errors.New("report: the print token secret is not configured")
)

type PrintGrant struct {
	JobID       string
	WorkspaceID string
	ExpiresAt   time.Time
}

func NewPrintToken(secret string, grant PrintGrant) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrPrintSecretUnset
	}
	if grant.JobID == "" || grant.WorkspaceID == "" {
		return "", ErrPrintTokenInvalid
	}

	payload := grant.JobID + "." + grant.WorkspaceID + "." +
		strconv.FormatInt(grant.ExpiresAt.UTC().Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + signPrintPayload(secret, encoded), nil
}

func VerifyPrintToken(secret, token string, now time.Time) (PrintGrant, error) {
	if strings.TrimSpace(secret) == "" {
		return PrintGrant{}, ErrPrintSecretUnset
	}

	encoded, signature, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found || encoded == "" || signature == "" {
		return PrintGrant{}, ErrPrintTokenInvalid
	}
	if !hmac.Equal([]byte(signature), []byte(signPrintPayload(secret, encoded))) {
		return PrintGrant{}, ErrPrintTokenInvalid
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return PrintGrant{}, ErrPrintTokenInvalid
	}

	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return PrintGrant{}, ErrPrintTokenInvalid
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return PrintGrant{}, ErrPrintTokenInvalid
	}

	grant := PrintGrant{
		JobID:       parts[0],
		WorkspaceID: parts[1],
		ExpiresAt:   time.Unix(seconds, 0).UTC(),
	}
	if now.After(grant.ExpiresAt) {
		return PrintGrant{}, ErrPrintTokenExpired
	}
	return grant, nil
}

func signPrintPayload(secret, encoded string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
