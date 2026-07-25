package auth_usecase

import (
	"errors"
	"log"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/business_metrics"
	"vozko/domain/cache"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/infra/geolocation"
)

type credentialsLoginUseCase struct {
	userRepo        user.UserRepository
	passwordService auth.PasswordService
	tokenIssuer     auth.TokenIssuer
	sessionRepo     auth.SessionRepository
	emailPublisher  notification.PublishEmailUseCase
	recordMetric    business_metrics.RecordMetricUseCase
	throttle        cache.FailureThrottle
	notifier        notification.Notifier
	dashboardURL    string
}

var _ auth.CredentialsLoginUseCase = (*credentialsLoginUseCase)(nil)

func NewCredentialsLoginUseCase(
	userRepo user.UserRepository,
	passwordService auth.PasswordService,
	tokenIssuer auth.TokenIssuer,
	sessionRepo auth.SessionRepository,
	emailPublisher notification.PublishEmailUseCase,
	recordMetric business_metrics.RecordMetricUseCase,
) *credentialsLoginUseCase {
	return &credentialsLoginUseCase{
		userRepo:        userRepo,
		passwordService: passwordService,
		tokenIssuer:     tokenIssuer,
		sessionRepo:     sessionRepo,
		emailPublisher:  emailPublisher,
		recordMetric:    recordMetric,
	}
}

// WithFailureThrottle enables the per-account failed-login throttle (the brute
// force defence that, unlike the per-IP limit, does not collide for users on a
// shared network). Returns the use case for chaining at wiring time.
func (uc *credentialsLoginUseCase) WithFailureThrottle(t cache.FailureThrottle) *credentialsLoginUseCase {
	uc.throttle = t
	return uc
}

// WithNotifier enables the security alert email when an account is locked by the
// throttle. dashboardURL is the customer app base. Returns the use case for chaining.
func (uc *credentialsLoginUseCase) WithNotifier(n notification.Notifier, dashboardURL string) *credentialsLoginUseCase {
	uc.notifier = n
	uc.dashboardURL = dashboardURL
	return uc
}

func (uc *credentialsLoginUseCase) notifyAccountLocked(account string) {
	if uc.notifier == nil {
		return
	}
	// Deduped per account/window so an attacker hammering a victim cannot spam them.
	_ = uc.notifier.Notify(notification.Notification{
		Email:    account,
		Subject:  "Acesso bloqueado temporariamente - " + brand.Active().Name,
		Template: "login_locked.html",
		Placeholders: map[string]interface{}{
			"Email":    account,
			"ResetURL": uc.dashboardURL + "/reset-password",
		},
		DedupKey: "login_locked:" + account,
		DedupTTL: 15 * time.Minute,
	})
}

func (uc *credentialsLoginUseCase) Execute(input auth.CredentialsInput) (*auth.TokenPair, error) {
	account := strings.ToLower(strings.TrimSpace(input.Email))

	if uc.throttle != nil {
		allowed, retryAfter, terr := uc.throttle.Allowed(account)
		if terr != nil {
			log.Printf("[login-throttle] check failed (allowing; per-IP limit still applies): %v", terr)
		} else if !allowed {
			go uc.notifyAccountLocked(account)
			return nil, &auth.TooManyAttemptsError{RetryAfter: retryAfter}
		}
	}

	u, err := uc.userRepo.FindByEmail(input.Email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			// Record the failure for a non-existent account too: uniform behaviour
			// avoids leaking which emails exist, and throttles email enumeration.
			uc.registerLoginFailure(account)
			return nil, auth.ErrInvalidCredentials
		}
		return nil, err
	}

	if err := uc.passwordService.Verify(u.Password, input.Password); err != nil {
		uc.registerLoginFailure(account)
		return nil, auth.ErrInvalidCredentials
	}

	if uc.throttle != nil {
		_ = uc.throttle.Reset(account)
	}

	tokens, err := uc.tokenIssuer.Issue(u)
	if err != nil {
		return nil, err
	}

	rawRefresh, hashRefresh, err := uc.tokenIssuer.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	session := &auth.Session{
		UserID:           u.ID,
		RefreshTokenHash: hashRefresh,
		AccessJTI:        tokens.AccessJTI,
		DeviceInfo:       input.DeviceInfo,
		IPAddress:        input.IPAddress,
		ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
	}

	if input.IPAddress != "" {
		session.Location = geolocation.LookupIP(input.IPAddress)
	}

	if err := uc.sessionRepo.Create(session); err != nil {
		return nil, err
	}

	tokens.RefreshToken = rawRefresh

	uc.recordLogin(u.ID, u.Email)

	go uc.sendLoginEmail(u.Email, time.Now().Format("2006-01-02 15:04:05"))

	return tokens, nil
}

func (uc *credentialsLoginUseCase) registerLoginFailure(account string) {
	if uc.throttle == nil {
		return
	}
	if err := uc.throttle.RegisterFailure(account); err != nil {
		log.Printf("[login-throttle] failed to record failure: %v", err)
	}
}

func (uc *credentialsLoginUseCase) sendLoginEmail(email, loginTime string) {
	log.Printf("LOGIN EMAIL: Starting for %s at %s", email, loginTime)
	subject := "Login realizado - " + brand.Active().Name

	if err := uc.emailPublisher.Publish(
		email,
		subject,
		"login_successful.html",
		map[string]interface{}{
			"Email":     email,
			"LoginTime": loginTime,
		},
	); err != nil {
		log.Printf("LOGIN EMAIL FAILED: %v", err)
	} else {
		log.Printf("LOGIN EMAIL SENT SUCCESSFULLY to %s", email)
	}
}

func (uc *credentialsLoginUseCase) recordLogin(userID, email string) {
	if uc.recordMetric == nil {
		return
	}

	err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventUserLogin,
		EntityID:   userID,
		EntityType: business_metrics.EntityTypeUser,
		UserID:     &userID,
		Metadata: map[string]string{
			"email": email,
		},
	})

	if err != nil {
		log.Printf("failed to record user login metric: %v", err)
	}
}
