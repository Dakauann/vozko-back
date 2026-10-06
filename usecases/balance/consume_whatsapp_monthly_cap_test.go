package balance_usecase

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/balance"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type noMonthlySendCaps struct{}

func (noMonthlySendCaps) TakeMonthlySendSlot(string, string, time.Time) (bool, error) {
	return false, nil
}

func (noMonthlySendCaps) GiveBackMonthlySendSlot(string, string) error { return nil }

type stubMonthlySendSlots struct {
	took      bool
	takeErr   error
	giveErr   error
	takenFor  []string
	periods   []time.Time
	givenBack []string
}

func (s *stubMonthlySendSlots) TakeMonthlySendSlot(workspaceID, referenceID string, period time.Time) (bool, error) {
	s.takenFor = append(s.takenFor, workspaceID+"/"+referenceID)
	s.periods = append(s.periods, period)
	return s.took, s.takeErr
}

func (s *stubMonthlySendSlots) GiveBackMonthlySendSlot(workspaceID, referenceID string) error {
	s.givenBack = append(s.givenBack, workspaceID+"/"+referenceID)
	return s.giveErr
}

type failingDebitRepo struct {
	*waMockBalanceRepo
	err error
}

func (f failingDebitRepo) DebitBalance(balance.DebitBalanceInput) (*balance.Transaction, error) {
	return nil, f.err
}

func consumeWithSlots(repo balance.Repository, slots balance.MonthlySendSlots, now time.Time) balance.ConsumeWhatsappTemplateUseCase {
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{}, slots)
	uc.(*consumeWhatsappTemplateUseCase).now = func() time.Time { return now }
	return uc
}

func TestConsumeWhatsappTemplate_TakesTheSlotOfThisMonthBeforeCharging(t *testing.T) {
	repo := newWAMockBalanceRepo()
	slots := &stubMonthlySendSlots{took: true}
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)

	if _, err := consumeWithSlots(repo, slots, now).Execute("ws-1", "entry-1", "MARKETING"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(slots.takenFor) != 1 || slots.takenFor[0] != "ws-1/entry-1" || !slots.periods[0].Equal(now) {
		t.Fatalf("unexpected take %v %v", slots.takenFor, slots.periods)
	}
	if len(repo.debits) != 1 || len(slots.givenBack) != 0 {
		t.Fatalf("a successful charge keeps its slot: debits %d, given back %v", len(repo.debits), slots.givenBack)
	}
}

func TestConsumeWhatsappTemplate_CapReachedChargesNothing(t *testing.T) {
	repo := newWAMockBalanceRepo()
	slots := &stubMonthlySendSlots{takeErr: balance.ErrMonthlySendCapReached}

	_, err := consumeWithSlots(repo, slots, time.Now()).Execute("ws-1", "entry-1", "MARKETING")
	if !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("want ErrMonthlySendCapReached, got %v", err)
	}
	if len(repo.debits) != 0 || len(slots.givenBack) != 0 {
		t.Fatal("nothing is charged and nothing is given back")
	}
}

func TestConsumeWhatsappTemplate_SlotFailureChargesNothing(t *testing.T) {
	repo := newWAMockBalanceRepo()
	boom := errors.New("db down")

	_, err := consumeWithSlots(repo, &stubMonthlySendSlots{takeErr: boom}, time.Now()).Execute("ws-1", "entry-1", "MARKETING")
	if !errors.Is(err, boom) || errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("want the infrastructure error, not the cap, got %v", err)
	}
	if len(repo.debits) != 0 {
		t.Fatal("nothing may be charged when the slot is unknown")
	}
}

func TestConsumeWhatsappTemplate_MissingSlotsFailClosed(t *testing.T) {
	repo := newWAMockBalanceRepo()
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{}, nil)

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); err == nil {
		t.Fatal("a use case wired without slots must refuse to charge")
	}
	if len(repo.debits) != 0 {
		t.Fatal("nothing may be charged without slots")
	}
	if err := uc.Refund("ws-1", "entry-1", "MARKETING"); err != nil {
		t.Fatalf("a refund never depends on slots, got %v", err)
	}
}

func TestConsumeWhatsappTemplate_FailedChargeGivesBackOnlyASlotItTook(t *testing.T) {
	insufficient := failingDebitRepo{waMockBalanceRepo: newWAMockBalanceRepo(), err: balance.ErrInsufficientBalance}
	cases := []struct {
		name     string
		took     bool
		wantGive int
	}{
		{"new slot is given back", true, 1},
		{"slot already held by an earlier delivery stays", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slots := &stubMonthlySendSlots{took: tc.took}
			_, err := consumeWithSlots(insufficient, slots, time.Now()).Execute("ws-1", "entry-1", "MARKETING")
			if !errors.Is(err, balance.ErrInsufficientBalance) {
				t.Fatalf("want the charge error, got %v", err)
			}
			if len(slots.givenBack) != tc.wantGive {
				t.Fatalf("given back %v, want %d", slots.givenBack, tc.wantGive)
			}
		})
	}
}

func TestConsumeWhatsappTemplate_GiveBackFailureStillReportsTheChargeError(t *testing.T) {
	insufficient := failingDebitRepo{waMockBalanceRepo: newWAMockBalanceRepo(), err: balance.ErrInsufficientBalance}
	slots := &stubMonthlySendSlots{took: true, giveErr: errors.New("db down")}

	if _, err := consumeWithSlots(insufficient, slots, time.Now()).Execute("ws-1", "entry-1", "MARKETING"); !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("want the charge error, got %v", err)
	}
}

func TestConsumeWhatsappTemplate_RefundGivesTheSlotBack(t *testing.T) {
	repo := newWAMockBalanceRepo()
	slots := &stubMonthlySendSlots{}

	if err := consumeWithSlots(repo, slots, time.Now()).Refund("ws-1", "entry-1", "MARKETING"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if len(repo.credits) != 1 || len(slots.givenBack) != 1 || slots.givenBack[0] != "ws-1/entry-1" {
		t.Fatalf("credits %d, given back %v", len(repo.credits), slots.givenBack)
	}
}

func TestConsumeWhatsappTemplate_FailedRefundKeepsTheSlot(t *testing.T) {
	slots := &stubMonthlySendSlots{}
	uc := consumeWithSlots(&waErrBalanceRepo{creditErr: errors.New("db write failed")}, slots, time.Now())

	if err := uc.Refund("ws-1", "entry-1", "MARKETING"); err == nil {
		t.Fatal("a failed refund must surface")
	}
	if len(slots.givenBack) != 0 {
		t.Fatal("no money went back, so the slot stays taken")
	}
}

func TestConsumeWhatsappTemplate_SubscriptionIsCheckedBeforeTheSlot(t *testing.T) {
	repo := newWAMockBalanceRepo()
	slots := &stubMonthlySendSlots{took: true}
	subscriptionErr := errors.New("no subscription")
	uc := NewConsumeWhatsappTemplateUseCase(repo, newWATestPricer(), &allowAllSubscriptionChecker{err: subscriptionErr}, slots)

	if _, err := uc.Execute("ws-1", "entry-1", "MARKETING"); !errors.Is(err, subscriptionErr) {
		t.Fatalf("want the subscription error, got %v", err)
	}
	if len(slots.takenFor) != 0 {
		t.Fatal("a workspace that cannot send never takes a slot")
	}
}

func TestConsumeWhatsappTemplate_UnpricedTemplateNeverTakesASlot(t *testing.T) {
	slots := &stubMonthlySendSlots{took: true}
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), workspace_pricing.NewPricer(&errPricingRepo{}), &allowAllSubscriptionChecker{}, slots)

	if _, err := uc.Execute("ws-1", "entry-1", "UTILITY"); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("want ErrPriceUnavailable, got %v", err)
	}
	if len(slots.takenFor) != 0 {
		t.Fatal("a send that cannot be priced never takes a slot")
	}
}

func TestConsumeWhatsappTemplate_UncappedWorkspaceChargesNormally(t *testing.T) {
	repo := newWAMockBalanceRepo()
	uc := consumeWithSlots(repo, noMonthlySendCaps{}, time.Now())

	for i := 0; i < 3; i++ {
		if _, err := uc.Execute("ws-1", fmt.Sprintf("entry-%d", i), "MARKETING"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if len(repo.debits) != 3 {
		t.Fatalf("every send is charged as before, got %d", len(repo.debits))
	}
	if err := uc.Refund("ws-1", "entry-0", "MARKETING"); err != nil || len(repo.credits) != 1 {
		t.Fatalf("refunds work as before: %v, credits %d", err, len(repo.credits))
	}
}
