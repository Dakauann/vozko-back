package auth

import "time"

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
	FindActiveByUserID(userID string) (*PasswordResetToken, error)
	IncrementAttempts(id string) (int, error)
	MarkUsed(id string) error
	DeleteByUserID(userID string) error
	DeleteExpired() error
}

type RequestPasswordResetInput struct {
	Email string
}

type ResetPasswordInput struct {
	Email       string
	Token       string
	NewPassword string
}

type RequestPasswordResetUseCase interface {
	Execute(input RequestPasswordResetInput) error
}

type ResetPasswordUseCase interface {
	Execute(input ResetPasswordInput) error
}
