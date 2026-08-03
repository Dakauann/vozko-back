package instagram

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidState  = errors.New("instagram oauth: state is invalid or tampered")
	ErrExpiredState  = errors.New("instagram oauth: state has expired")
	ErrReplayedState = errors.New("instagram oauth: state has already been used")
)

// stateTTL bounds how long an authorize redirect stays valid.
const stateTTL = 15 * time.Minute

// OAuthState is the CSRF/replay protection for the connect flow.
//
// The existing WhatsApp embedded-signup flow has no state parameter at all, it
// relies on the popup being opened from an authenticated session, and it accepts
// a caller-supplied access_token, which lets a caller skip the code exchange
// entirely. A plain redirect flow cannot lean on that, and does not need to: a
// signed state carries the tenant identity, and a single-use nonce blocks replay.
type OAuthState struct {
	WorkspaceID string
	UserID      string
	Nonce       string
	ExpiresAt   time.Time
	// ReturnPath is where the callback sends the browser afterwards. It is
	// validated as a relative path so the state cannot be turned into an open
	// redirect.
	ReturnPath string
	// Popup records that the flow was launched in a popup, so the callback answers
	// with a page that posts the result to window.opener instead of redirecting
	// the (popup) tab to the dashboard.
	Popup bool
}

// EncodeState signs the state with the app secret.
//
// Format: base64url(payload) + "." + hex(HMAC-SHA256(payload)). The payload is
// pipe-delimited rather than JSON to keep the URL short and the parse trivial.
func EncodeState(s OAuthState, secret string) (string, error) {
	if strings.ContainsAny(s.WorkspaceID+s.UserID+s.Nonce+s.ReturnPath, "|") {
		return "", fmt.Errorf("instagram oauth: state fields must not contain '|'")
	}
	popup := "0"
	if s.Popup {
		popup = "1"
	}
	payload := strings.Join([]string{
		s.WorkspaceID,
		s.UserID,
		s.Nonce,
		strconv.FormatInt(s.ExpiresAt.UTC().Unix(), 10),
		s.ReturnPath,
		popup,
	}, "|")

	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + signPayload(encoded, secret), nil
}

// DecodeState verifies the signature and expiry, returning the state.
//
// The signature is compared in constant time, and the comparison happens BEFORE
// the payload is parsed so a tampered state never reaches the rest of the flow.
func DecodeState(raw, secret string) (*OAuthState, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidState
	}
	encoded, signature := parts[0], parts[1]

	expected := signPayload(encoded, secret)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, ErrInvalidState
	}

	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidState
	}

	fields := strings.Split(string(decoded), "|")
	if len(fields) != 6 {
		return nil, ErrInvalidState
	}
	expUnix, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return nil, ErrInvalidState
	}
	expiresAt := time.Unix(expUnix, 0).UTC()
	if time.Now().UTC().After(expiresAt) {
		return nil, ErrExpiredState
	}

	state := &OAuthState{
		WorkspaceID: fields[0],
		UserID:      fields[1],
		Nonce:       fields[2],
		ExpiresAt:   expiresAt,
		ReturnPath:  fields[4],
		Popup:       fields[5] == "1",
	}
	if state.WorkspaceID == "" || state.Nonce == "" {
		return nil, ErrInvalidState
	}
	return state, nil
}

func signPayload(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// NewNonce mints a cryptographically random nonce.
func NewNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// SafeReturnPath restricts the post-callback redirect to a relative path so a
// crafted state cannot bounce the user to another origin.
func SafeReturnPath(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return fallback
	}
	// Reject anything that could resolve to another origin: absolute URLs,
	// scheme-relative URLs, and backslash variants browsers normalize.
	if !strings.HasPrefix(candidate, "/") ||
		strings.HasPrefix(candidate, "//") ||
		strings.HasPrefix(candidate, "/\\") ||
		strings.Contains(candidate, "://") {
		return fallback
	}
	return candidate
}
