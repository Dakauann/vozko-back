package balance_repository

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/balance"
)

// The charge aggregation is the query that took the platform down on
// 2026-09-08: seconds of work over two multi-gigabyte tables, with nothing
// stopping a second caller from starting its own copy while the first was
// still running. Nine accumulated, each pinning a core, and the database
// stopped answering anything else — /auth/login included.
//
// These tests pin the two properties that keep that from repeating: identical
// concurrent questions share ONE execution, and different questions do not.

type flightInner struct {
	balance.Repository // unimplemented methods panic; the tests never reach them

	calls   int32
	release chan struct{}
}

func (f *flightInner) AggregateWhatsAppTemplateCharges(
	balance.WhatsAppChargeFilter,
) (*balance.WhatsAppChargeStats, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.release != nil {
		<-f.release
	}
	return &balance.WhatsAppChargeStats{NetDispatches: 7}, nil
}

func TestConcurrentIdenticalChargeQueriesRunOnce(t *testing.T) {
	inner := &flightInner{release: make(chan struct{})}
	repo := &CachedBalanceRepository{inner: inner}

	filter := balance.WhatsAppChargeFilter{
		WorkspaceID:   "ws-1",
		DepartmentIDs: []string{"d1", "d2"},
	}

	const callers = 9 // the number that actually piled up in production
	var wg sync.WaitGroup
	results := make([]*balance.WhatsAppChargeStats, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := repo.AggregateWhatsAppTemplateCharges(filter)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
				return
			}
			results[i] = got
		}(i)
	}

	// Hold the first execution open until everyone has had time to arrive, so
	// the test measures coalescing rather than a lucky sequential ordering.
	waitFor(t, func() bool { return atomic.LoadInt32(&inner.calls) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&inner.calls); got != 1 {
		close(inner.release)
		wg.Wait()
		t.Fatalf("%d queries were running at once, want 1 — this is the pile-up that took the database down", got)
	}
	close(inner.release)
	wg.Wait()

	if got := atomic.LoadInt32(&inner.calls); got != 1 {
		t.Errorf("inner ran %d times, want 1 — %d concurrent callers must share one query", got, callers)
	}
	for i, r := range results {
		if r == nil || r.NetDispatches != 7 {
			t.Errorf("caller %d got %+v, every caller must receive the shared answer", i, r)
		}
	}
}

// Each caller must own its value. They joined one execution, so handing back
// the same pointer would let one caller's write be seen by the others.
func TestEachCallerGetsItsOwnCopy(t *testing.T) {
	inner := &flightInner{}
	repo := &CachedBalanceRepository{inner: inner}
	filter := balance.WhatsAppChargeFilter{WorkspaceID: "ws-1"}

	a, err := repo.AggregateWhatsAppTemplateCharges(filter)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.AggregateWhatsAppTemplateCharges(filter)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two callers received the same pointer")
	}
	a.NetDispatches = 999
	if b.NetDispatches != 7 {
		t.Errorf("mutating one caller's result changed another's: %d", b.NetDispatches)
	}
}

// Coalescing must never merge two DIFFERENT questions, which would answer one
// workspace's report with another's numbers.
func TestDifferentQuestionsDoNotShareAFlight(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b balance.WhatsAppChargeFilter
	}{
		{
			"different workspace",
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1"},
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-2"},
		},
		{
			"different department",
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", DepartmentIDs: []string{"d1"}},
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", DepartmentIDs: []string{"d2"}},
		},
		{
			"different campaign type",
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", CampaignType: "a"},
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", CampaignType: "b"},
		},
		{
			"different period",
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", From: timePtr(time.Unix(0, 0))},
			balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", From: timePtr(time.Unix(86400, 0))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if chargeFlightKey(tc.a) == chargeFlightKey(tc.b) {
				t.Errorf("both filters key to %q — they would share an answer", chargeFlightKey(tc.a))
			}
		})
	}
}

// The same departments in a different order are the same question. Without
// sorting they would key differently and each order would pay for its own
// query — which is the pile-up this exists to prevent.
func TestDepartmentOrderDoesNotSplitTheFlight(t *testing.T) {
	a := balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", DepartmentIDs: []string{"d1", "d2", "d3"}}
	b := balance.WhatsAppChargeFilter{WorkspaceID: "ws-1", DepartmentIDs: []string{"d3", "d1", "d2"}}

	if chargeFlightKey(a) != chargeFlightKey(b) {
		t.Errorf("same departments in another order keyed differently:\n  %q\n  %q",
			chargeFlightKey(a), chargeFlightKey(b))
	}
	// The caller's slice must survive: sorting it in place would reorder a
	// filter the caller still holds.
	if a.DepartmentIDs[0] != "d1" || b.DepartmentIDs[0] != "d3" {
		t.Error("chargeFlightKey reordered the caller's slice")
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}
