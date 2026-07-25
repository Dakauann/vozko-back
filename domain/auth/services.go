package auth

import "vozko/domain/user"

type PasswordService interface {
	Hash(plain string) (string, error)
	Verify(hash string, plain string) error
}

type TokenIssuer interface {
	Issue(u *user.User) (*TokenPair, error)
	GenerateRefreshToken() (raw string, hash string, err error)
	HashRefreshToken(raw string) string
}
