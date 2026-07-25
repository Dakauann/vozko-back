package security

import (
	"errors"

	"golang.org/x/crypto/bcrypt"

	"vozko/domain/auth"
)

const MinPasswordHashCost = 12

type BcryptPasswordService struct {
	cost int
}

func NewBcryptPasswordService(cost int) auth.PasswordService {
	if cost <= 0 {
		cost = MinPasswordHashCost
	}
	return &BcryptPasswordService{
		cost: cost,
	}
}

func (s *BcryptPasswordService) Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password required")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), s.cost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (s *BcryptPasswordService) Verify(hash string, plain string) error {
	if hash == "" || plain == "" {
		return errors.New("invalid password payload")
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
