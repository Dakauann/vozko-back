package balance_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
)

type noMonthlySendCaps struct{}

func (noMonthlySendCaps) GetMonthlySendCap(string) (*balance.MonthlySendCap, error) { return nil, nil }

type stubMonthlySendCaps struct {
	cap   *balance.MonthlySendCap
	err   error
	calls int
}

func (s *stubMonthlySendCaps) GetMonthlySendCap(string) (*balance.MonthlySendCap, error) {
	s.calls++
	return s.cap, s.err
}

func cappedConsumeUseCase(repo balance.Repository, caps balance.MonthlySendCapReader, now time.Time) balance.ConsumeWhatsappTemplateUseCase {
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{}, caps)
	uc.(*consumeWhatsappTemplateUseCase).now = func() time.Time { return now }
	return uc
}

func TestConsumeWhatsappTemplate_UncappedWorkspaceDebitsWithoutGuard(t *testing.T) {
	repo := newWAMockBalanceRepo()
	uc := cappedConsumeUseCase(repo, noMonthlySendCaps{}, time.Now())

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(repo.debits) != 1 || repo.debits[0].monthlyCap != nil {
		t.Fatalf("an uncapped workspace debits with no guard, got %+v", repo.debits)
	}
}

func TestConsumeWhatsappTemplate_CappedWorkspaceDebitsUnderItsMonthlyGuard(t *testing.T) {
	repo := newWAMockBalanceRepo()
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	caps := &stubMonthlySendCaps{cap: &balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 1000}}
	uc := cappedConsumeUseCase(repo, caps, now)

	if _, err := uc.Execute("ws-1", "entry-1", "UTILITY"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(repo.debits) != 1 {
		t.Fatalf("want one debit, got %d", len(repo.debits))
	}
	guard := repo.debits[0].monthlyCap
	if guard == nil || guard.Limit != 1000 || !guard.Since.Equal(balance.SendCapMonthStart(now)) {
		t.Fatalf("unexpected guard %+v", guard)
	}
}

func TestConsumeWhatsappTemplate_CapReadFailureBlocksTheDebit(t *testing.T) {
	repo := newWAMockBalanceRepo()
	boom := errors.New("db down")
	uc := cappedConsumeUseCase(repo, &stubMonthlySendCaps{err: boom}, time.Now())

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); !errors.Is(err, boom) {
		t.Fatalf("want the read error, got %v", err)
	}
	if len(repo.debits) != 0 {
		t.Fatal("nothing may be debited when the cap is unknown")
	}
}

func TestConsumeWhatsappTemplate_MissingCapReaderFailsClosed(t *testing.T) {
	repo := newWAMockBalanceRepo()
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{}, nil)

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); err == nil {
		t.Fatal("a use case wired without a cap reader must refuse to debit")
	}
	if len(repo.debits) != 0 {
		t.Fatal("nothing may be debited without a cap reader")
	}
}

func TestConsumeWhatsappTemplate_SubscriptionIsCheckedBeforeTheCap(t *testing.T) {
	repo := newWAMockBalanceRepo()
	caps := &stubMonthlySendCaps{}
	subscriptionErr := errors.New("no subscription")
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{err: subscriptionErr}, caps)

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); !errors.Is(err, subscriptionErr) {
		t.Fatalf("want the subscription error, got %v", err)
	}
	if caps.calls != 0 {
		t.Fatal("the cap is not read for a workspace that cannot send anyway")
	}
}

func TestConsumeWhatsappTemplate_RefundIgnoresTheCap(t *testing.T) {
	repo := newWAMockBalanceRepo()
	caps := &stubMonthlySendCaps{err: errors.New("db down")}
	uc := cappedConsumeUseCase(repo, caps, time.Now())

	if err := uc.Refund("ws-1", "entry-1", "MARKETING"); err != nil {
		t.Fatalf("a refund must never depend on the cap, got %v", err)
	}
	if caps.calls != 0 {
		t.Fatal("refunds do not read the cap")
	}
}
