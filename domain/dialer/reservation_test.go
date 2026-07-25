package dialer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTTL = DialerReservationTTL

func TestReservationState_ReserveIdleAndIdempotent(t *testing.T) {
	var r ReservationState
	now := time.Unix(1_700_000_000, 0)

	if !r.Reserve("offer-1", false, now, testTTL) {
		t.Fatal("Reserve on idle should succeed")
	}
	if !r.ReservedLive(now, testTTL) {
		t.Fatal("reserved session must report live")
	}
	if !r.Reserve("offer-1", false, now, testTTL) {
		t.Fatal("re-reserving same token must be idempotent")
	}
	if r.Reserve("offer-2", false, now, testTTL) {
		t.Fatal("a different token must not steal a live reservation")
	}
}

func TestReservationState_RejectedWhenActive(t *testing.T) {
	var r ReservationState
	now := time.Unix(1_700_000_000, 0)
	if r.Reserve("offer-1", true, now, testTTL) {
		t.Fatal("must not reserve a session that already has an attached call")
	}
	if r.ReservedLive(now, testTTL) {
		t.Fatal("a rejected reserve must not occupy the session")
	}
}

func TestReservationState_EmptyTokenRejected(t *testing.T) {
	var r ReservationState
	now := time.Unix(1_700_000_000, 0)
	if r.Reserve("", false, now, testTTL) {
		t.Fatal("empty token must be rejected")
	}
}

func TestReservationState_ReleaseTokenScoped(t *testing.T) {
	var r ReservationState
	now := time.Unix(1_700_000_000, 0)
	r.Reserve("offer-1", false, now, testTTL)

	r.Release("offer-2") // foreign
	if !r.ReservedLive(now, testTTL) {
		t.Fatal("foreign release must not clear")
	}
	r.Release("") // empty
	if !r.ReservedLive(now, testTTL) {
		t.Fatal("empty release must not clear")
	}
	r.Release("offer-1")
	if r.ReservedLive(now, testTTL) {
		t.Fatal("matching release must clear")
	}
	r.Release("offer-1") // double release safe
	if !r.Reserve("offer-2", false, now, testTTL) {
		t.Fatal("freed session must be reservable again")
	}
}

func TestReservationState_ClearConsumes(t *testing.T) {
	var r ReservationState
	now := time.Unix(1_700_000_000, 0)
	r.Reserve("offer-1", false, now, testTTL)
	r.Clear() // attach-consume
	if r.ReservedLive(now, testTTL) {
		t.Fatal("Clear must drop the reservation")
	}
	r.Release("offer-1") // stale release after consume must be safe
	if r.ReservedLive(now, testTTL) {
		t.Fatal("state must remain free")
	}
}

func TestReservationState_TTLBackstop(t *testing.T) {
	var r ReservationState
	base := time.Unix(1_700_000_000, 0)
	r.Reserve("offer-1", false, base, testTTL)

	if !r.ReservedLive(base.Add(testTTL-time.Millisecond), testTTL) {
		t.Fatal("within TTL must be live")
	}
	if r.ReservedLive(base.Add(testTTL), testTTL) {
		t.Fatal("at/after TTL must lazily expire")
	}
	if !r.Reserve("offer-2", false, base.Add(2*testTTL), testTTL) {
		t.Fatal("a fresh offer must reserve after TTL expiry")
	}
}

func TestReservationState_ConcurrentOnlyOneWinner(t *testing.T) {
	// The state is caller-locked, so a real session guards it with a mutex; this
	// test emulates that to prove exactly one token wins under contention.
	var r ReservationState
	var mu sync.Mutex
	now := time.Unix(1_700_000_000, 0)

	const n = 64
	var wins int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		token := "offer-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		go func() {
			defer wg.Done()
			<-start
			mu.Lock()
			ok := r.Reserve(token, false, now, testTTL)
			mu.Unlock()
			if ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one Reserve must win, got %d", wins)
	}
}
