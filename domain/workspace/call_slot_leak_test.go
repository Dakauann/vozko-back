package workspace

import "testing"

func TestLeak_UnreleasedSlotsAreNeverReclaimed(t *testing.T) {
	mgr, shared, subs, plan := newTestCallSlotManager("replica-1")
	setupSubscription(subs, plan, "ws-1", 5)

	for i := 0; i < 5; i++ {
		if _, ok := mgr.Acquire("ws-1", 1000); !ok {
			t.Fatalf("acquire %d should succeed under the cap of 5", i+1)
		}
	}
	if _, ok := mgr.Acquire("ws-1", 1000); ok {
		t.Fatal("6th acquire must be rejected at cap 5")
	}

	key := workspaceCallKey("ws-1")
	if got := shared.get(key); got != 5 {
		t.Fatalf("counter=%d, want 5", got)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		if _, ok := mgr.Acquire("ws-1", 1000); ok {
			t.Fatalf("attempt %d unexpectedly succeeded, leak would be self-healing (it isn't)", attempt)
		}
	}
	if got := shared.get(key); got != 5 {
		t.Fatalf("LEAK CONFIRMED but counter drifted: got %d", got)
	}
	t.Logf("LEAK CONFIRMED: 0 real calls active, counter pinned at %d/5, capacity permanently blocked, no self-heal", shared.get(key))

	mgr.Release("ws-1")
	if _, ok := mgr.Acquire("ws-1", 1000); !ok {
		t.Fatal("after one Release a slot must free, confirms recovery depends solely on Release running")
	}
}
