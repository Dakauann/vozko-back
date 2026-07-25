package ai

import "errors"

var (
	ErrNoMessages          = errors.New("ai: at least one message is required")
	ErrProviderUnavailable = errors.New("ai: provider unavailable")
)
