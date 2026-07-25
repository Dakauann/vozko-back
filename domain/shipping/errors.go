package shipping

import "errors"

var (
	ErrProviderNotConfigured = errors.New("shipping provider not configured")
	ErrAccountNotFound       = errors.New("shipping provider account not found")
	ErrAccountOwnership      = errors.New("shipping provider account ownership mismatch")
	ErrAuthorizationFailed   = errors.New("shipping provider authorization failed")
	ErrTokenRefreshFailed    = errors.New("shipping provider token refresh failed")
)
