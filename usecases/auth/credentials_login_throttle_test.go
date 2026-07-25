package auth_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/auth"
	"vozko/domain/user"
)

type fakeThrottle struct {
	allowed    bool
	retryAfter time.Duration
	failures   int
	resets     int
}

func (f *fakeThrottle) Allowed(string) (bool, time.Duration, error) {
	return f.allowed, f.retryAfter, nil
}
func (f *fakeThrottle) RegisterFailure(string) error { f.failures++; return nil }
func (f *fakeThrottle) Reset(string) error           { f.resets++; return nil }

func newThrottledLoginUC(repo *testUserRepo, thr *fakeThrottle) auth.CredentialsLoginUseCase {
	return NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	).WithFailureThrottle(thr)
}

func seedLoginUser(repo *testUserRepo) {
	repo.users["user@test.com"] = &user.User{
		ID:       "u1",
		Email:    "user@test.com",
		Password: "bcrypt:password123",
		Role:     user.RoleUser,
	}
	repo.byID["u1"] = repo.users["user@test.com"]
}

func TestLoginThrottle_BlocksAndDoesNotAttemptAuthWhenThrottled(t *testing.T) {
	repo := newTestUserRepo()
	seedLoginUser(repo)
	thr := &fakeThrottle{allowed: false, retryAfter: 15 * time.Minute}
	uc := newThrottledLoginUC(repo, thr)

	_, err := uc.Execute(auth.CredentialsInput{Email: "user@test.com", Password: "password123"})

	var tooMany *auth.TooManyAttemptsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("expected *TooManyAttemptsError, got %v", err)
	}
	if !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatal("errors.Is(err, ErrTooManyAttempts) must hold")
	}
	if tooMany.RetryAfter != 15*time.Minute {
		t.Fatalf("retry-after must propagate, got %v", tooMany.RetryAfter)
	}
	if thr.failures != 0 {
		t.Fatalf("an already-blocked attempt must not register a new failure, got %d", thr.failures)
	}
}

func TestLoginThrottle_RegistersFailureOnWrongPassword(t *testing.T) {
	repo := newTestUserRepo()
	seedLoginUser(repo)
	thr := &fakeThrottle{allowed: true}
	uc := newThrottledLoginUC(repo, thr)

	_, err := uc.Execute(auth.CredentialsInput{Email: "user@test.com", Password: "wrong"})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if thr.failures != 1 {
		t.Fatalf("expected exactly one failure registered, got %d", thr.failures)
	}
	if thr.resets != 0 {
		t.Fatalf("must not reset on a failed login, got %d", thr.resets)
	}
}

func TestLoginThrottle_RegistersFailureOnUnknownAccount(t *testing.T) {
	repo := newTestUserRepo()
	thr := &fakeThrottle{allowed: true}
	uc := newThrottledLoginUC(repo, thr)

	_, err := uc.Execute(auth.CredentialsInput{Email: "ghost@test.com", Password: "x"})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if thr.failures != 1 {
		t.Fatalf("unknown account must also register a failure (anti-enumeration), got %d", thr.failures)
	}
}

func TestLoginThrottle_ResetsOnSuccessfulLogin(t *testing.T) {
	repo := newTestUserRepo()
	seedLoginUser(repo)
	thr := &fakeThrottle{allowed: true}
	uc := newThrottledLoginUC(repo, thr)

	pair, err := uc.Execute(auth.CredentialsInput{Email: "user@test.com", Password: "password123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected a token pair on success")
	}
	if thr.resets != 1 {
		t.Fatalf("expected the failure counter reset on success, got %d resets", thr.resets)
	}
	if thr.failures != 0 {
		t.Fatalf("expected no failures registered on success, got %d", thr.failures)
	}
}
