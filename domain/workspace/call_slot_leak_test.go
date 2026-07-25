package workspace

import "testing"

// TestLeak_UnreleasedSlotsAreNeverReclaimed reproduces the production leak: a call
// that acquires a slot but never calls Release — because its goroutine hung in the
// bridge or was killed by a restart, so the deferred releaseCallSlot never ran —
// permanently consumes a slot. There is NO reclaim path other than an explicit
// Release, so N dead calls pin the cap and block all further dialing even with zero
// calls actually active. This is exactly the observed "8 real, counter at 50".
func TestLeak_UnreleasedSlotsAreNeverReclaimed(t *testing.T) {
	mgr, shared, subs, plan := newTestCallSlotManager("replica-1")
	setupSubscription(subs, plan, "ws-1", 5) // plan limit = 5 concurrent calls

	// 5 calls dial and acquire their slots.
	for i := 0; i < 5; i++ {
		if _, ok := mgr.Acquire("ws-1", 1000); !ok {
			t.Fatalf("acquire %d should succeed under the cap of 5", i+1)
		}
	}
	// Correctly at capacity: the 6th is rejected.
	if _, ok := mgr.Acquire("ws-1", 1000); ok {
		t.Fatal("6th acquire must be rejected at cap 5")
	}

	// Now all 5 calls DIE without releasing (hang / restart → defer never runs).
	// Nothing decrements the counter — real active calls is now 0.

	key := workspaceCallKey("ws-1")
	if got := shared.get(key); got != 5 {
		t.Fatalf("counter=%d, want 5", got)
	}

	// THE LEAK: with 0 real calls active, capacity is still fully exhausted, and it
	// stays that way indefinitely — there is no self-heal, no reconciler, no TTL that
	// fires while the key is live. Every future dial is falsely rejected.
	for attempt := 1; attempt <= 3; attempt++ {
		if _, ok := mgr.Acquire("ws-1", 1000); ok {
			t.Fatalf("attempt %d unexpectedly succeeded — leak would be self-healing (it isn't)", attempt)
		}
	}
	if got := shared.get(key); got != 5 {
		t.Fatalf("LEAK CONFIRMED but counter drifted: got %d", got)
	}
	t.Logf("LEAK CONFIRMED: 0 real calls active, counter pinned at %d/5 — capacity permanently blocked, no self-heal", shared.get(key))

	// Recovery is release-ONLY — the exact path the leak skips. One explicit Release
	// frees exactly one slot; nothing else ever would.
	mgr.Release("ws-1")
	if _, ok := mgr.Acquire("ws-1", 1000); !ok {
		t.Fatal("after one Release a slot must free — confirms recovery depends solely on Release running")
	}
}
