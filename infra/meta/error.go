// Package meta is a shared client for Meta's Graph API family.
//
// It exists because the existing WhatsApp client (infra/whatsapp/business_phone)
// decodes Meta's structured error — code, error_subcode, is_transient — and then
// formats it into a string, so nothing downstream can decide whether a failure is
// retryable. It also hardcodes the API version in several places, takes no
// context.Context, and has no retry or rate limiting.
//
// This package keeps the error structured, honours context, retries transient
// failures with jittered backoff, and enforces per-account rate limits. Instagram
// uses it from the start; WhatsApp is expected to migrate onto it later.
package meta

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is Meta's error envelope, kept structured so callers can branch.
type Error struct {
	HTTPStatus int    `json:"-"`
	Code       int    `json:"code"`
	Subcode    int    `json:"error_subcode"`
	Type       string `json:"type"`
	Message    string `json:"message"`
	FBTraceID  string `json:"fbtrace_id"`
	// IsTransient is Meta's own signal that a retry may succeed.
	IsTransient bool   `json:"is_transient"`
	UserTitle   string `json:"error_user_title"`
	UserMsg     string `json:"error_user_msg"`
}

func (e *Error) Error() string {
	if e == nil {
		return "meta: <nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = e.UserMsg
	}
	return fmt.Sprintf("meta: http=%d code=%d subcode=%d type=%s trace=%s: %s",
		e.HTTPStatus, e.Code, e.Subcode, e.Type, e.FBTraceID, msg)
}

// Well-known Graph error codes. The full table is not published for Instagram
// messaging — four documentation URLs return 500 or a JS shell — so this list
// covers the codes we can act on and everything else falls through to the
// HTTP-status heuristics below. Extend it from real staging responses rather
// than from guesswork.
const (
	CodeUnknown           = 1
	CodeAPIService        = 2
	CodeRateLimit         = 4
	CodePermission        = 10
	CodeUserRateLimit     = 17
	CodeInvalidParam      = 100
	CodeSessionExpired    = 102
	CodeAppRateLimit      = 32
	CodeAccessTokenError  = 190
	CodePersonUnavailable = 551
	CodeMessagingRate     = 613
	// CodeWindowClosed is the Messenger code for a closed messaging window. The
	// Instagram equivalent is unverified; treat a match as authoritative but do
	// not rely on its absence.
	CodeWindowClosed = 1545041
)

// Retryable reports whether retrying the same request could plausibly succeed.
func (e *Error) Retryable() bool {
	if e == nil {
		return false
	}
	if e.IsTransient {
		return true
	}
	switch e.Code {
	case CodeUnknown, CodeAPIService, CodeRateLimit, CodeUserRateLimit,
		CodeAppRateLimit, CodeMessagingRate:
		return true
	}
	// 5xx and 429 are retryable regardless of body.
	if e.HTTPStatus >= 500 || e.HTTPStatus == http.StatusTooManyRequests {
		return true
	}
	return false
}

// NeedsReauth reports whether the account's token is dead and the tenant must
// reconnect. Retrying cannot fix these, so the caller should mark the account
// and stop consuming retries.
func (e *Error) NeedsReauth() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodeAccessTokenError, CodeSessionExpired:
		return true
	}
	return false
}

// IsRateLimit reports whether we exceeded a quota.
func (e *Error) IsRateLimit() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodeRateLimit, CodeUserRateLimit, CodeAppRateLimit, CodeMessagingRate:
		return true
	}
	return e.HTTPStatus == http.StatusTooManyRequests
}

// IsWindowClosed reports whether the send failed because the messaging window
// has closed. This is an expected, user-visible condition on Instagram, not a
// defect.
func (e *Error) IsWindowClosed() bool {
	return e != nil && e.Code == CodeWindowClosed
}

// IsRecipientUnreachable reports whether the recipient can never be messaged
// (blocked us, deactivated). The conversation should be marked dead rather than
// retried.
func (e *Error) IsRecipientUnreachable() bool {
	return e != nil && e.Code == CodePersonUnavailable
}

// IsPermission reports a missing or insufficient permission — a configuration or
// App Review problem, never transient.
func (e *Error) IsPermission() bool {
	return e != nil && (e.Code == CodePermission || e.Code == CodeInvalidParam && e.Subcode == 33)
}

// AsError extracts a *Error from an error chain.
func AsError(err error) (*Error, bool) {
	var me *Error
	if errors.As(err, &me) {
		return me, true
	}
	return nil, false
}

// IsRetryable is the chain-aware form of Error.Retryable.
func IsRetryable(err error) bool {
	if me, ok := AsError(err); ok {
		return me.Retryable()
	}
	// Transport-level failures (dial, timeout) are retryable; the client wraps
	// them in RequestError.
	var re *RequestError
	return errors.As(err, &re)
}

// IsReauthRequired is the chain-aware form of Error.NeedsReauth.
func IsReauthRequired(err error) bool {
	me, ok := AsError(err)
	return ok && me.NeedsReauth()
}

// RequestError wraps a transport failure (no HTTP response was obtained).
type RequestError struct {
	Op  string
	Err error
}

func (e *RequestError) Error() string { return fmt.Sprintf("meta: %s: %v", e.Op, e.Err) }
func (e *RequestError) Unwrap() error { return e.Err }

// errorBody is the shape Meta returns on failure.
type errorBody struct {
	Error Error `json:"error"`
}
