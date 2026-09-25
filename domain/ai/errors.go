package ai

import "errors"

var (
	ErrNoMessages           = errors.New("ai: at least one message is required")
	ErrProviderUnavailable  = errors.New("ai: provider unavailable")
	ErrBillingNotConfigured = errors.New("ai: billing publisher is required")
	ErrStreamIncomplete     = errors.New("ai: the provider stream ended before the model finished")
)
