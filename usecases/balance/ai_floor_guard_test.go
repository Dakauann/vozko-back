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

func TestAIFloorGuardAllowsAtAndAboveTheFloor(t *testing.T) {
	// Exactly the floor passes: the check is "below", not "at or below", and
	// which one it is decides whether a workspace topped up to the minimum can
	// spend at all.
	for _, micros := range []int64{balance.MinAIFloorMicros, balance.MinAIFloorMicros + 1, 5_000_000} {
		checker := &fakeFloorChecker{micros: micros}
		if !NewAIFloorGuard(checker, "test").Allow("ws-1") {
			t.Errorf("balance %d micros was refused, want allowed", micros)
		}
	}
}

func TestAIFloorGuardRefusesBelowTheFloor(t *testing.T) {
	checker := &fakeFloorChecker{micros: balance.MinAIFloorMicros - 1}
	if NewAIFloorGuard(checker, "test").Allow("ws-1") {
		t.Fatal("a balance below the floor was allowed to spend")
	}
}

// A balance that cannot be READ is treated as too low. The alternative is
// spending money we cannot prove the customer has, every time Redis blinks.
func TestAIFloorGuardFailsClosedWhenTheBalanceCannotBeRead(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000, err: errors.New("connection reset")}
	if NewAIFloorGuard(checker, "test").Allow("ws-1") {
		t.Fatal("an unreadable balance was allowed to spend")
	}
}

// A deployment without balance tracking is not a deployment where every AI
// feature is dead. Same stance the audience engine's guard already takes.
func TestAIFloorGuardAllowsWithoutAChecker(t *testing.T) {
	if !NewAIFloorGuard(nil, "test").Allow("ws-1") {
		t.Fatal("a nil checker refused; it must allow")
	}
	var zero AIFloorGuard
	if !zero.Allow("ws-1") {
		t.Fatal("the zero guard refused; it must allow")
	}
}

// An empty workspace id is a call nobody pays for. Refused before the read,
// so a bug upstream cannot spend anonymously.
func TestAIFloorGuardRefusesAnEmptyWorkspace(t *testing.T) {
	checker := &fakeFloorChecker{micros: 5_000_000}
	if NewAIFloorGuard(checker, "test").Allow("   ") {
		t.Fatal("an empty workspace id was allowed to spend")
	}
	if checker.reads != 0 {
		t.Fatalf("the balance was read %d times for an empty workspace, want 0", checker.reads)
	}
}
