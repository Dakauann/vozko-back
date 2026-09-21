package auth_usecase

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"vozko/domain/auth"
	realcache "vozko/infra/cache"
)

type countingSharedState struct{ *testSharedState }

func newCountingSharedState() *countingSharedState {
	return &countingSharedState{newTestSharedState()}
}

func (s *countingSharedState) IncrWithTTL(k string, _ time.Duration) (int64, error) {
	n, _ := strconv.Atoi(s.data[k])
	n++
	s.data[k] = strconv.Itoa(n)
	return int64(n), nil
}

func realThrottle(state *countingSharedState) auth.CredentialsLoginUseCase {
	repo := newTestUserRepo()
	seedLoginUser(repo)
	thr := realcache.NewFailureThrottle(state, "loginfail", 10, 15*time.Minute)
	return NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
	).WithFailureThrottle(thr)
}

func TestLoginThrottle_LocksSharedAccountAcrossAllDevices(t *testing.T) {
	state := newCountingSharedState()
	uc := realThrottle(state)

	const account = "user@test.com"
	for computer := 1; computer <= 5; computer++ {
		for attempt := 1; attempt <= 2; attempt++ {
			_, err := uc.Execute(auth.CredentialsInput{
				Email:      account,
				Password:   "stale-saved-password",
				DeviceInfo: "Office-PC-" + strconv.Itoa(computer),
				IPAddress:  "179.191.107.18",
			})
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("computer %d attempt %d: want ErrInvalidCredentials, got %v", computer, attempt, err)
			}
		}
	}

	_, err := uc.Execute(auth.CredentialsInput{
		Email:      account,
		Password:   "password123",
		DeviceInfo: "Office-PC-6-fresh",
		IPAddress:  "203.0.113.99",
	})
	var tooMany *auth.TooManyAttemptsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("shared account must be locked account-wide after 10 aggregate failures; got %v", err)
	}
	t.Logf("PROVEN: 10 failures across 5 devices locked the shared account; "+
		"a 6th device with the correct password got TooManyAttempts (retry-after %s). "+
		"This is the 'rate limit alert' the user receives on every computer.", tooMany.RetryAfter)
}

func TestLoginThrottle_DoesNotLockOtherAccounts(t *testing.T) {
	state := newCountingSharedState()
	repo := newTestUserRepo()
	seedLoginUser(repo)
	repo.users["other@test.com"] = repo.users["user@test.com"]
	thr := realcache.NewFailureThrottle(state, "loginfail", 10, 15*time.Minute)
	uc := NewCredentialsLoginUseCase(
		repo, &testPasswordService{}, &testTokenIssuer{}, newTestSessionRepo(),
		&testEmailPublisher{},
	).WithFailureThrottle(thr)

	for i := 0; i < 12; i++ {
		_, _ = uc.Execute(auth.CredentialsInput{Email: "user@test.com", Password: "wrong"})
	}
	_, err := uc.Execute(auth.CredentialsInput{Email: "other@test.com", Password: "password123"})
	if err != nil {
		t.Fatalf("a different account must not be locked by the first account's failures, got %v", err)
	}
}
