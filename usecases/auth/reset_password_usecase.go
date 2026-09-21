package auth_usecase

import (
	"crypto/subtle"
	"log"
	"time"
	"unicode"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/notification"
	"vozko/domain/user"
)

const maxResetAttempts = 5

type resetPasswordUseCase struct {
	userRepo        user.UserRepository
	tokenRepo       auth.PasswordResetTokenRepository
	passwordService auth.PasswordService
	sessionRepo     auth.SessionRepository
	shared          cache.SharedState
	notifier        notification.Notifier
	dashboardURL    string
}

var _ auth.ResetPasswordUseCase = (*resetPasswordUseCase)(nil)

func NewResetPasswordUseCase(
	userRepo user.UserRepository,
	tokenRepo auth.PasswordResetTokenRepository,
	passwordService auth.PasswordService,
	sessionRepo auth.SessionRepository,
	shared cache.SharedState,
) *resetPasswordUseCase {
	return &resetPasswordUseCase{
		userRepo:        userRepo,
		tokenRepo:       tokenRepo,
		passwordService: passwordService,
		sessionRepo:     sessionRepo,
		shared:          shared,
	}
}

func (uc *resetPasswordUseCase) WithNotifier(n notification.Notifier, dashboardURL string) *resetPasswordUseCase {
	uc.notifier = n
	uc.dashboardURL = dashboardURL
	return uc
}

func (uc *resetPasswordUseCase) notifyPasswordChanged(email string) {
	if uc.notifier == nil || email == "" {
		return
	}
	_ = uc.notifier.Notify(notification.Notification{
		Email:    email,
		Subject:  "Sua senha foi alterada - " + brand.Active().Name,
		Template: "password_changed.html",
		Placeholders: map[string]interface{}{
			"Email":        email,
			"DashboardURL": uc.dashboardURL,
		},
		DedupKey: "password_changed:reset:" + email,
		DedupTTL: 5 * time.Minute,
	})
}

func (uc *resetPasswordUseCase) Execute(input auth.ResetPasswordInput) error {
	if input.Email == "" || input.Token == "" {
		return auth.ErrInvalidResetToken
	}

	u, err := uc.userRepo.FindByEmail(input.Email)
	if err != nil {
		return auth.ErrInvalidResetToken
	}

	resetToken, err := uc.tokenRepo.FindActiveByUserID(u.ID)
	if err != nil {
		return auth.ErrInvalidResetToken
	}

	if time.Now().UTC().After(resetToken.ExpiresAt) {
		return auth.ErrInvalidResetToken
	}

	if resetToken.Attempts >= maxResetAttempts {
		_ = uc.tokenRepo.MarkUsed(resetToken.ID)
		return auth.ErrInvalidResetToken
	}

	presented := hashSecretCode(input.Token)
	if subtle.ConstantTimeCompare([]byte(presented), []byte(resetToken.TokenHash)) != 1 {
		attempts, _ := uc.tokenRepo.IncrementAttempts(resetToken.ID)
		if attempts >= maxResetAttempts {
			_ = uc.tokenRepo.MarkUsed(resetToken.ID)
			uc.notifyResetLocked(u.Email)
		}
		return auth.ErrInvalidResetToken
	}

	if !isStrongPassword(input.NewPassword) {
		return auth.ErrWeakPassword
	}

	hashedPassword, err := uc.passwordService.Hash(input.NewPassword)
	if err != nil {
		return err
	}

	u.Password = hashedPassword
	if err := uc.userRepo.Update(u.ID, u); err != nil {
		return err
	}

	sessions, _ := uc.sessionRepo.FindActiveByUserID(u.ID)
	for _, s := range sessions {
		if s.AccessJTI != "" {
			_ = uc.shared.SetString(revokedJTIPrefix+s.AccessJTI, "1", revokedJTITTL)
		}
	}

	_ = uc.sessionRepo.RevokeAllByUserID(u.ID)
	_, _ = uc.userRepo.IncrementTokenVersion(u.ID)

	_ = uc.shared.Del("cache:token_ver:" + u.ID)

	if err := uc.tokenRepo.MarkUsed(resetToken.ID); err != nil {
		return err
	}

	uc.notifyPasswordChanged(u.Email)
	return nil
}

func (uc *resetPasswordUseCase) notifyResetLocked(email string) {
	if uc.notifier == nil || email == "" {
		log.Printf("PASSWORD RESET LOCKED after %d attempts for %s", maxResetAttempts, email)
		return
	}
	_ = uc.notifier.Notify(notification.Notification{
		Email:    email,
		Subject:  "Tentativas de redefinição de senha bloqueadas - " + brand.Active().Name,
		Template: "password_reset_locked.html",
		Placeholders: map[string]interface{}{
			"Email":        email,
			"DashboardURL": uc.dashboardURL,
		},
		DedupKey: "password_reset_locked:" + email,
		DedupTTL: 5 * time.Minute,
	})
}

func isStrongPassword(password string) bool {
	if len(password) < 8 {
		return false
	}

	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}

	return hasUpper && hasLower && hasDigit
}
