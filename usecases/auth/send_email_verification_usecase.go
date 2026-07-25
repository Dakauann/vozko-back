package auth_usecase

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"time"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/notification"
)

type sendEmailVerificationUseCase struct {
	tokenRepo    auth.EmailVerificationRepository
	emailService notification.EmailService

	asyncDispatch func(func())
}

func NewSendEmailVerificationUseCase(tokenRepo auth.EmailVerificationRepository, emailService notification.EmailService) auth.SendEmailVerificationUseCase {
	return &sendEmailVerificationUseCase{
		tokenRepo:    tokenRepo,
		emailService: emailService,
		asyncDispatch: func(fn func()) {
			go fn()
		},
	}
}

func NewSendEmailVerificationUseCaseSync(tokenRepo auth.EmailVerificationRepository, emailService notification.EmailService) auth.SendEmailVerificationUseCase {
	return &sendEmailVerificationUseCase{
		tokenRepo:     tokenRepo,
		emailService:  emailService,
		asyncDispatch: func(fn func()) { fn() },
	}
}

func (uc *sendEmailVerificationUseCase) Execute(email string) error {
	const maxTokensPerHour = 3
	const rateWindow = time.Hour

	count, err := uc.tokenRepo.CountByEmailInWindow(email, rateWindow)
	if err != nil {
		return err
	}

	if count >= maxTokensPerHour {
		return auth.ErrEmailVerificationRateExceeded
	}

	token, err := generateVerificationCode()
	if err != nil {
		return err
	}

	verificationToken := &auth.EmailVerificationToken{
		Token:     token,
		Email:     email,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Used:      false,
	}

	if err := uc.tokenRepo.Create(verificationToken); err != nil {
		return err
	}

	uc.asyncDispatch(func() {
		uc.deliverVerificationEmail(email, token)
	})

	return nil
}

func (uc *sendEmailVerificationUseCase) deliverVerificationEmail(email, token string) {
	subject := "Código de Verificação - " + brand.Active().Name
	body := fmt.Sprintf("Seu código de verificação é:\n\n%s\n\nEste código expira em 15 minutos.", token)

	if err := uc.emailService.SendTemplate(email, subject, "email_verification.html", map[string]interface{}{
		"Email": email,
		"Token": token,
	}); err != nil {
		if fallbackErr := uc.emailService.SendEmail(email, subject, body); fallbackErr != nil {
			log.Printf("[auth] verification email delivery failed for %s: template=%v plain=%v", email, err, fallbackErr)
		}
	}
}

func generateVerificationCode() (string, error) {

	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}
