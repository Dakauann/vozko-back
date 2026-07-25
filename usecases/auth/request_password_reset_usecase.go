package auth_usecase

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/notification"
	"vozko/domain/user"
)

const resetTokenExpiry = 1 * time.Hour

type requestPasswordResetUseCase struct {
	userRepo     user.UserRepository
	tokenRepo    auth.PasswordResetTokenRepository
	emailService notification.EmailService
	frontendURL  string
}

func NewRequestPasswordResetUseCase(
	userRepo user.UserRepository,
	tokenRepo auth.PasswordResetTokenRepository,
	emailService notification.EmailService,
) auth.RequestPasswordResetUseCase {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = brand.Active().SiteURL
	}

	return &requestPasswordResetUseCase{
		userRepo:     userRepo,
		tokenRepo:    tokenRepo,
		emailService: emailService,
		frontendURL:  frontendURL,
	}
}

func (uc *requestPasswordResetUseCase) Execute(input auth.RequestPasswordResetInput) error {
	u, err := uc.userRepo.FindByEmail(input.Email)
	if err != nil {
		return nil
	}

	code, err := generateResetCode()
	if err != nil {
		return err
	}

	// One live code per account: drop any outstanding tokens so a fresh request
	// always resets the attempt counter and can't be raced against a stale code.
	if err := uc.tokenRepo.DeleteByUserID(u.ID); err != nil {
		return err
	}

	resetToken := &auth.PasswordResetToken{
		TokenHash: hashSecretCode(code),
		UserID:    u.ID,
		Email:     u.Email,
		ExpiresAt: time.Now().UTC().Add(resetTokenExpiry),
		Used:      false,
		CreatedAt: time.Now().UTC(),
	}

	if err := uc.tokenRepo.Create(resetToken); err != nil {
		return err
	}

	go uc.sendResetEmail(u.Email, code)

	return nil
}

func (uc *requestPasswordResetUseCase) sendResetEmail(email, token string) {
	subject := "Código de Recuperação de Senha - " + brand.Active().Name
	if err := uc.emailService.SendTemplate(email, subject, "password_reset.html", map[string]interface{}{
		"Email":     email,
		"Token":     token,
		"ExpiresIn": "1 hora",
	}); err != nil {
		log.Printf("PASSWORD RESET EMAIL FAILED for %s: %v", email, err)
	}
}

func generateResetCode() (string, error) {

	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}
