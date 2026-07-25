package auth

import "errors"

var (
	ErrInvalidCredentials            = errors.New("invalid credentials")
	ErrEmailAlreadyExists            = errors.New("email already exists")
	ErrDocumentAlreadyExists         = errors.New("a user with this CPF/CNPJ already exists")
	ErrInvalidCustomerType           = errors.New("invalid customer type")
	ErrMissingCustomerDocument       = errors.New("missing customer document")
	ErrInvalidCustomerDocument       = errors.New("invalid customer document")
	ErrInvalidResetToken             = errors.New("invalid or expired reset token")
	ErrResetTokenUsed                = errors.New("reset token already used")
	ErrWeakPassword                  = errors.New("password does not meet security requirements")
	ErrInvalidVerificationToken      = errors.New("invalid or expired verification token")
	ErrVerificationTokenUsed         = errors.New("verification token already used")
	ErrMissingVerificationToken      = errors.New("missing verification token")
	ErrEmailVerificationRateExceeded = errors.New("email verification rate limit exceeded")

	ErrSessionRevoked         = errors.New("session has been revoked")
	ErrSessionExpired         = errors.New("session has expired")
	ErrSessionNotFound        = errors.New("session not found")
	ErrTokenVersionMismatch   = errors.New("token version outdated")
	ErrCurrentPasswordInvalid = errors.New("current password is incorrect")
	ErrRefreshTokenReuse      = errors.New("refresh token reuse detected")
)
