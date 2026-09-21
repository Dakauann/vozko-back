package ai

import "errors"

var (
	ErrNoMessages          = errors.New("ai: at least one message is required")
	ErrProviderUnavailable = errors.New("ai: provider unavailable")
	// ErrBillingNotConfigured is an AI adapter refusing to exist without the
	// publisher that turns its token usage into a charge.
	//
	// Every completion has already cost money at the provider by the time it
	// returns, so an adapter that cannot bill is one that spends without
	// collecting. Refusing at construction makes that a boot failure, which is
	// the last moment somebody is watching, instead of a silent early return on
	// every call afterwards.
	ErrBillingNotConfigured = errors.New("ai: billing publisher is required")
)
