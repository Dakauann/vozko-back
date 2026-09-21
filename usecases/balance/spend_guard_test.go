package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
)

type fakeFloorChecker struct {
	micros int64
	err    error
	reads  int
}

func (f *fakeFloorChecker) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (f *fakeFloorChecker) GetBalance(string) (int64, error) {
	f.reads++
	return f.micros, f.err
}
func (f *fakeFloorChecker) Invalidate(string)          {}
func (f *fakeFloorChecker) InvalidateDebounced(string) {}

func TestSpendGuardAllowsAtAndAboveTheFloor(t *testing.T) {
	for _, micros := range []int64{balance.MinAIFloorMicros, balance.MinAIFloorMicros + 1, 5_000_000} {
		checker := &fakeFloorChecker{micros: micros}
		if !NewSpendGuard(checker, "test").Allow("ws-1") {
			t.Errorf("balance %d micros was refused, want allowed", micros)
		}
	}
}

func TestSpendGuardRefusesBelowTheFloor(t *testing.T) {
	checker := &fakeFloorChecker{micros: balance.MinAIFloorMicros - 1}
	if NewSpendGuard(checker, "test").Allow("ws-1") {
		t.Fatal("a balance below the floor was allowed to spend")
	}
}

func TestSpendGuardFailsClosedWhenTheBalanceCannotBeRead(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000, err: errors.New("connection reset")}
	if NewSpendGuard(checker, "test").Allow("ws-1") {
		t.Fatal("an unreadable balance was allowed to spend")
	}
}

func TestSpendGuardAllowsWithoutAChecker(t *testing.T) {
	if !NewSpendGuard(nil, "test").Allow("ws-1") {
		t.Fatal("a nil checker refused; it must allow")
	}
	var zero SpendGuard
	if !zero.Allow("ws-1") {
		t.Fatal("the zero guard refused; it must allow")
	}
}

func TestSpendGuardRefusesAnEmptyWorkspace(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000}
	if NewSpendGuard(checker, "test").Allow("   ") {
		t.Fatal("an empty workspace id was allowed to spend")
	}
	if checker.reads != 0 {
		t.Fatalf("the balance was read %d times for an empty workspace, want 0", checker.reads)
	}
}

func TestCanAffordIsNotTheFloor(t *testing.T) {
	belowFloor := balance.MinAIFloorMicros - 1
	checker := &fakeFloorChecker{micros: belowFloor}
	guard := NewSpendGuard(checker, "test")

	if guard.Allow("ws-1") {
		t.Fatal("a balance below the floor passed the floor check")
	}
	if !guard.CanAfford("ws-1", 1_000) {
		t.Error("a workspace below the AI floor could not afford a cheap message; the two checks are not the same question")
	}
}

func TestCanAffordAllowsAFreeActionWithoutReadingTheBalance(t *testing.T) {
	checker := &fakeFloorChecker{micros: 0}
	guard := NewSpendGuard(checker, "test")

	for _, amount := range []int64{0, -1} {
		if !guard.CanAfford("ws-1", amount) {
			t.Errorf("amount %d was refused; a free action has nothing to afford", amount)
		}
	}
	if checker.reads != 0 {
		t.Errorf("the balance was read %d times for a free action, want 0", checker.reads)
	}
}

func TestCanAffordAtAndAboveThePrice(t *testing.T) {
	for _, bal := range []int64{5_000, 5_001, 1_000_000} {
		checker := &fakeFloorChecker{micros: bal}
		if !NewSpendGuard(checker, "test").CanAfford("ws-1", 5_000) {
			t.Errorf("balance %d could not afford 5000 micros", bal)
		}
	}
}

func TestCanAffordRefusesBelowThePrice(t *testing.T) {
	checker := &fakeFloorChecker{micros: 4_999}
	if NewSpendGuard(checker, "test").CanAfford("ws-1", 5_000) {
		t.Fatal("a balance below the price was allowed to spend")
	}
}

func TestCanAffordFailsClosedWhenTheBalanceCannotBeRead(t *testing.T) {
	checker := &fakeFloorChecker{micros: 1_000_000, err: errors.New("connection reset")}
	if NewSpendGuard(checker, "test").CanAfford("ws-1", 5_000) {
		t.Fatal("an unreadable balance was allowed to spend")
	}
}

func TestCanAffordAllowsWithoutAChecker(t *testing.T) {
	if !NewSpendGuard(nil, "test").CanAfford("ws-1", 5_000) {
		t.Fatal("a nil checker refused; it must allow")
	}
	var zero SpendGuard
	if !zero.CanAfford("ws-1", 5_000) {
		t.Fatal("the zero guard refused; it must allow")
	}
}

func TestCanAffordRefusesAnEmptyWorkspace(t *testing.T) {
	checker := &fakeFloorChecker{micros: 1_000_000}
	if NewSpendGuard(checker, "test").CanAfford("  ", 5_000) {
		t.Fatal("an empty workspace id was allowed to spend")
	}
	if checker.reads != 0 {
		t.Fatalf("the balance was read %d times for an empty workspace, want 0", checker.reads)
	}
}

func TestRequiredGuardRefusesWithoutAChecker(t *testing.T) {
	guard := NewRequiredSpendGuard(nil, "test")

	if guard.Allow("ws-1") {
		t.Error("the floor check passed with no checker wired; that is spending money we cannot verify")
	}
	if guard.CanAfford("ws-1", 5_000) {
		t.Error("the affordability check passed with no checker wired")
	}
}

func TestRequiredGuardStillAllowsAFreeAction(t *testing.T) {
	if !NewRequiredSpendGuard(nil, "test").CanAfford("ws-1", 0) {
		t.Error("a zero-cost action was refused for want of a balance checker")
	}
}

func TestOptionalGuardStillAllowsWithoutAChecker(t *testing.T) {
	guard := NewSpendGuard(nil, "test")
	if !guard.Allow("ws-1") || !guard.CanAfford("ws-1", 5_000) {
		t.Error("the optional guard refused with no checker; it must allow")
	}
}
