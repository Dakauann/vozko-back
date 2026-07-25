package auth

import "time"

// PasswordResetToken is a single-use, per-user password reset grant. The 6-digit
// code is never stored: only its SHA-256 hash lives in TokenHash, and lookups are
// bound to UserID so a code can only be tried against the account it was issued
// for. Attempts caps online guessing of the low-entropy code.
type PasswordResetToken struct {
	ID        string
	TokenHash string
	UserID    string
	Email     string
	Attempts  int
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}

type PasswordResetTokenRepository interface {
	Create(token *PasswordResetToken) error
	// FindActiveByUserID returns the newest unused, unexpired reset token for a
	// user, or ErrInvalidResetToken when none exists.
	FindActiveByUserID(userID string) (*PasswordResetToken, error)
	// IncrementAttempts atomically bumps the wrong-guess counter and returns the
	// new value.
	IncrementAttempts(id string) (int, error)
	MarkUsed(id string) error
	// DeleteByUserID removes any outstanding tokens for a user so only one code is
	// ever live at a time.
	DeleteByUserID(userID string) error
	DeleteExpired() error
}

type RequestPasswordResetInput struct {
	Email string
}

type ResetPasswordInput struct {
	// Email binds the code to an account so wrong guesses can be counted and
	// capped; without it a code could be brute-forced across all users.
	Email string
	// Token is the plaintext 6-digit code the user received by email.
	Token       string
	NewPassword string
}

type RequestPasswordResetUseCase interface {
	Execute(input RequestPasswordResetInput) error
}

type ResetPasswordUseCase interface {
	Execute(input ResetPasswordInput) error
}
