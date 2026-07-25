package auth_usecase

import (
	"time"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/notification"
	"vozko/domain/user"
)

type changePasswordUseCase struct {
	userRepo        user.UserRepository
	passwordService auth.PasswordService
	sessionRepo     auth.SessionRepository
	shared          cache.SharedState
	notifier        notification.Notifier
	dashboardURL    string
}

var _ auth.ChangePasswordUseCase = (*changePasswordUseCase)(nil)

func NewChangePasswordUseCase(
	userRepo user.UserRepository,
	passwordService auth.PasswordService,
	sessionRepo auth.SessionRepository,
	shared cache.SharedState,
) *changePasswordUseCase {
	return &changePasswordUseCase{
		userRepo:        userRepo,
		passwordService: passwordService,
		sessionRepo:     sessionRepo,
		shared:          shared,
	}
}

// WithNotifier enables the "password changed" confirmation email. Returns the use
// case for chaining at wiring time.
func (uc *changePasswordUseCase) WithNotifier(n notification.Notifier, dashboardURL string) *changePasswordUseCase {
	uc.notifier = n
	uc.dashboardURL = dashboardURL
	return uc
}

func (uc *changePasswordUseCase) notifyPasswordChanged(email string) {
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
		DedupKey: "password_changed:change:" + email,
		DedupTTL: 5 * time.Minute,
	})
}

func (uc *changePasswordUseCase) Execute(input auth.ChangePasswordInput) error {
	u, err := uc.userRepo.FindByID(input.UserID)
	if err != nil {
		return err
	}

	if err := uc.passwordService.Verify(u.Password, input.CurrentPassword); err != nil {
		return auth.ErrCurrentPasswordInvalid
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

	uc.notifyPasswordChanged(u.Email)
	return nil
}
