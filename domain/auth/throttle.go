package auth

import (
	"errors"
	"time"
)

// ErrTooManyAttempts is returned when an account has exceeded the failed-login
// throttle. Match it with errors.Is; use errors.As on TooManyAttemptsError to read
// the retry-after hint.
var ErrTooManyAttempts = errors.New("too many login attempts")

type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string { return ErrTooManyAttempts.Error() }
func (e *TooManyAttemptsError) Unwrap() error { return ErrTooManyAttempts }
