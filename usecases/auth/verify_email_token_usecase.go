package auth_usecase

import (
	"time"

	"vozko/domain/auth"
)

type verifyEmailTokenUseCase struct {
	tokenRepo auth.EmailVerificationRepository
}

func NewVerifyEmailTokenUseCase(tokenRepo auth.EmailVerificationRepository) auth.VerifyEmailTokenUseCase {
	return &verifyEmailTokenUseCase{
		tokenRepo: tokenRepo,
	}
}

func (uc *verifyEmailTokenUseCase) Execute(token string) error {
	if token == "" {
		return auth.ErrMissingVerificationToken
	}

	verificationToken, err := uc.tokenRepo.FindByToken(token)
	if err != nil {
		return err
	}

	if verificationToken == nil {
		return auth.ErrInvalidVerificationToken
	}

	if verificationToken.Used {
		return auth.ErrVerificationTokenUsed
	}

	if time.Now().After(verificationToken.ExpiresAt) {
		return auth.ErrInvalidVerificationToken
	}

	return nil
}
