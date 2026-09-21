package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
)

// fakeFloorChecker is the balance read, and only the balance read. It stands in
// for the cached checker so the guard's two decisions — below the floor, and
// unreadable — are testable without Redis or a database.
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
	// Exactly the floor passes: the check is "below", not "at or below", and
	// which one it is decides whether a workspace topped up to the minimum can
	// spend at all.
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

// A balance that cannot be READ is treated as too low. The alternative is
// spending money we cannot prove the customer has, every time Redis blinks.
func TestSpendGuardFailsClosedWhenTheBalanceCannotBeRead(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000, err: errors.New("connection reset")}
	if NewSpendGuard(checker, "test").Allow("ws-1") {
		t.Fatal("an unreadable balance was allowed to spend")
	}
}

// A deployment without balance tracking is not a deployment where every AI
// feature is dead. Same stance the audience engine's guard already takes.
func TestSpendGuardAllowsWithoutAChecker(t *testing.T) {
	if !NewSpendGuard(nil, "test").Allow("ws-1") {
		t.Fatal("a nil checker refused; it must allow")
	}
	var zero SpendGuard
	if !zero.Allow("ws-1") {
		t.Fatal("the zero guard refused; it must allow")
	}
}

// An empty workspace id is a call nobody pays for. Refused before the read,
// so a bug upstream cannot spend anonymously.
func TestSpendGuardRefusesAnEmptyWorkspace(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000}
	if NewSpendGuard(checker, "test").Allow("   ") {
		t.Fatal("an empty workspace id was allowed to spend")
	}
	if checker.reads != 0 {
		t.Fatalf("the balance was read %d times for an empty workspace, want 0", checker.reads)
	}
}

// CanAfford is the other half: an exact price, not a floor.
//
// The two are deliberately different questions. A workspace can sit below the
// AI floor and still afford a message costing a fraction of a cent, and
// refusing that would take the inbox away from somebody who can pay for what
// they are actually doing.
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

// Zero price means this plan does not charge for the action, so there is
// nothing to afford. This is what lets "the plan prices service messages at
// zero" express itself as no gate at all rather than as a special case written
// out at every call site.
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
	// Exactly the price passes: the check is "below", not "at or below", and a
	// workspace topped up to the exact cost must be able to spend it.
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

// Same stance as the floor: an unreadable balance is a refusal, because the
// alternative is spending money we cannot prove the customer has.
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

// A priced action with no workspace is money nobody pays for, refused before
// the read, exactly as the floor does.
func TestCanAffordRefusesAnEmptyWorkspace(t *testing.T) {
	checker := &fakeFloorChecker{micros: 1_000_000}
	if NewSpendGuard(checker, "test").CanAfford("  ", 5_000) {
		t.Fatal("an empty workspace id was allowed to spend")
	}
	if checker.reads != 0 {
		t.Fatalf("the balance was read %d times for an empty workspace, want 0", checker.reads)
	}
}

// The regression this variant exists to prevent, pinned both ways.
//
// The WhatsApp AI path used to fail CLOSED on a missing checker, with a log
// line that called it CRITICAL. Moving it onto the shared guard silently turned
// that into the guard's permissive default: nothing failed, no test broke, and
// the product would have answered customers for free until the bill arrived.
func TestRequiredGuardRefusesWithoutAChecker(t *testing.T) {
	guard := NewRequiredSpendGuard(nil, "test")

	if guard.Allow("ws-1") {
		t.Error("the floor check passed with no checker wired; that is spending money we cannot verify")
	}
	if guard.CanAfford("ws-1", 5_000) {
		t.Error("the affordability check passed with no checker wired")
	}
}

// A free action stays free even under the required guard: there is no money to
// verify, so there is nothing a missing checker could have told us.
func TestRequiredGuardStillAllowsAFreeAction(t *testing.T) {
	if !NewRequiredSpendGuard(nil, "test").CanAfford("ws-1", 0) {
		t.Error("a zero-cost action was refused for want of a balance checker")
	}
}

// The permissive variant keeps its stance, because a deployment without balance
// tracking is not a deployment where every paid feature is dead.
func TestOptionalGuardStillAllowsWithoutAChecker(t *testing.T) {
	guard := NewSpendGuard(nil, "test")
	if !guard.Allow("ws-1") || !guard.CanAfford("ws-1", 5_000) {
		t.Error("the optional guard refused with no checker; it must allow")
	}
}
