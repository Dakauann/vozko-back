package auth

import "time"

type EmailVerificationToken struct {
	Token     string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Used      bool
}

type EmailVerificationRepository interface {
	Create(token *EmailVerificationToken) error
	FindByToken(token string) (*EmailVerificationToken, error)
	MarkAsUsed(token string) error
	DeleteExpired() error
	CountByEmailInWindow(email string, windowDuration time.Duration) (int, error)
}
