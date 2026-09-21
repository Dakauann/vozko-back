package auth

import (
	"errors"
	"time"
)

var ErrTooManyAttempts = errors.New("too many login attempts")

type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string { return ErrTooManyAttempts.Error() }
func (e *TooManyAttemptsError) Unwrap() error { return ErrTooManyAttempts }
