package ai

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNoMessages           = errors.New("ai: at least one message is required")
	ErrProviderUnavailable  = errors.New("ai: provider unavailable")
	ErrBillingNotConfigured = errors.New("ai: billing publisher is required")
	ErrStreamIncomplete     = errors.New("ai: the provider stream ended before the model finished")
	ErrProviderTransient    = errors.New("ai: the provider call failed on a passing fault and may succeed if tried again")
	ErrContextOverflow      = errors.New("ai: the request is longer than the model's context window")
	ErrProviderBusy         = errors.New("ai: the provider is momentarily at its spending limit and refused the call before starting it")
)

type ProviderBusyError struct {
	Reason     string
	Limit      string
	RetryAfter time.Duration
	Err        error
}

func (e *ProviderBusyError) Error() string {
	wait := "unknown"
	if e.RetryAfter > 0 {
		wait = e.RetryAfter.String()
	}
	return fmt.Sprintf("%s (reason=%s, limit=%s, retry after %s); the platform credit balance is low enough to hit this limit, add provider credits", ErrProviderBusy, e.Reason, e.Limit, wait)
}

func (e *ProviderBusyError) Is(target error) bool { return target == ErrProviderBusy }

func (e *ProviderBusyError) Unwrap() error { return e.Err }

func BusyRetryAfter(err error) (time.Duration, bool) {
	var busy *ProviderBusyError
	if !errors.As(err, &busy) {
		return 0, false
	}
	return busy.RetryAfter, true
}
