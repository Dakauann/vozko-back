package auth_usecase

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"vozko/domain/auth"
	realcache "vozko/infra/cache"
)

// countingSharedState is an accumulating in-memory SharedState. It reuses the
// package's testSharedState for the bulk of the interface but gives IncrWithTTL
// real counting semantics (the base fake always returns 1), so the *production*
// failure throttle actually accumulates across attempts — exactly as Redis does.
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

// realThrottle wires the ACTUAL production throttle (infra/cache) with the
// production thresholds (10 failures / 15 min) over an accumulating store.
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
		&testRecordMetric{},
	).WithFailureThrottle(thr)
}

// TestLoginThrottle_LocksSharedAccountAcrossAllDevices reproduces the reported
// production incident: one account ("anapaula@sudaseg") used from ~5 identical
// office computers. The per-account failure throttle is keyed ONLY by the email,
// so failed attempts from every computer land in the SAME counter. Ten bad
// attempts spread across five machines lock the account for ALL of them — and a
// machine typing the CORRECT password is then rejected with the user-facing
// "Too many login attempts" (AUTH_RATE_LIMIT_EXCEEDED) alert.
func TestLoginThrottle_LocksSharedAccountAcrossAllDevices(t *testing.T) {
	state := newCountingSharedState()
	uc := realThrottle(state)

	const account = "user@test.com" // the single shared login
	// 5 computers, each making 2 attempts with a stale/wrong saved password.
	// Distinct DeviceInfo + IPAddress prove device/IP is irrelevant to the lock.
	for computer := 1; computer <= 5; computer++ {
		for attempt := 1; attempt <= 2; attempt++ {
			_, err := uc.Execute(auth.CredentialsInput{
				Email:      account,
				Password:   "stale-saved-password",
				DeviceInfo: "Office-PC-" + strconv.Itoa(computer),
				IPAddress:  "179.191.107.18", // shared office NAT egress
			})
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("computer %d attempt %d: want ErrInvalidCredentials, got %v", computer, attempt, err)
			}
		}
	}

	// A 6th computer now tries the CORRECT password from a brand-new device/IP.
	// If the throttle were device- or IP-scoped this would succeed; because it is
	// account-scoped, the legitimate user is locked out platform-wide.
	_, err := uc.Execute(auth.CredentialsInput{
		Email:      account,
		Password:   "password123", // correct
		DeviceInfo: "Office-PC-6-fresh",
		IPAddress:  "203.0.113.99", // different IP entirely
	})
	var tooMany *auth.TooManyAttemptsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("shared account must be locked account-wide after 10 aggregate failures; got %v", err)
	}
	t.Logf("PROVEN: 10 failures across 5 devices locked the shared account; "+
		"a 6th device with the correct password got TooManyAttempts (retry-after %s). "+
		"This is the 'rate limit alert' the user receives on every computer.", tooMany.RetryAfter)
}

// TestLoginThrottle_DoesNotLockOtherAccounts is the control: failures on one
// account must not spill over to a different account (no false collateral lock).
func TestLoginThrottle_DoesNotLockOtherAccounts(t *testing.T) {
	state := newCountingSharedState()
	repo := newTestUserRepo()
	seedLoginUser(repo)
	repo.users["other@test.com"] = repo.users["user@test.com"] // same password fixture
	thr := realcache.NewFailureThrottle(state, "loginfail", 10, 15*time.Minute)
	uc := NewCredentialsLoginUseCase(
		repo, &testPasswordService{}, &testTokenIssuer{}, newTestSessionRepo(),
		&testEmailPublisher{}, &testRecordMetric{},
	).WithFailureThrottle(thr)

	for i := 0; i < 12; i++ {
		_, _ = uc.Execute(auth.CredentialsInput{Email: "user@test.com", Password: "wrong"})
	}
	// The untouched account must still be allowed through the throttle.
	_, err := uc.Execute(auth.CredentialsInput{Email: "other@test.com", Password: "password123"})
	if err != nil {
		t.Fatalf("a different account must not be locked by the first account's failures, got %v", err)
	}
}
