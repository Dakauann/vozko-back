package branchinfra

import (
	"testing"
	"time"

	"vozko/domain/branch"
)

func bind(sipUser, callID string, expiresAt time.Time) branch.RegistrationBinding {
	return branch.RegistrationBinding{SIPUser: sipUser, CallID: callID, ReceivedFrom: "5.6.7.8:5060", ExpiresAt: expiresAt}
}

func TestBindingStore_UpsertListRemove(t *testing.T) {
	s := NewInMemoryBindingStore()
	future := time.Now().Add(time.Hour)

	// Two devices (contacts) for one branch.
	_ = s.Upsert(bind("1001", "callA", future))
	_ = s.Upsert(bind("1001", "callB", future))
	if n, _ := s.CountLive("1001"); n != 2 {
		t.Fatalf("CountLive = %d, want 2", n)
	}

	// Re-REGISTER of the same call_id is an update, not a duplicate.
	_ = s.Upsert(bind("1001", "callA", future.Add(time.Minute)))
	if n, _ := s.CountLive("1001"); n != 2 {
		t.Fatalf("after re-register CountLive = %d, want 2", n)
	}

	_ = s.Remove("1001", "callA")
	live, _ := s.ListLive("1001")
	if len(live) != 1 || live[0].CallID != "callB" {
		t.Fatalf("after remove got %+v, want only callB", live)
	}

	// A different branch is isolated.
	if n, _ := s.CountLive("2002"); n != 0 {
		t.Fatalf("unrelated sip_user CountLive = %d, want 0", n)
	}
}

func TestBindingStore_ExpiryFiltering(t *testing.T) {
	s := NewInMemoryBindingStore()
	_ = s.Upsert(bind("1001", "live", time.Now().Add(time.Hour)))
	_ = s.Upsert(bind("1001", "dead", time.Now().Add(-time.Second)))

	live, _ := s.ListLive("1001")
	if len(live) != 1 || live[0].CallID != "live" {
		t.Fatalf("ListLive returned %+v, want only the live contact", live)
	}

	evicted := s.ReapExpired(time.Now())
	if len(evicted) != 1 || evicted[0].CallID != "dead" {
		t.Fatalf("ReapExpired evicted %+v, want the one dead contact", evicted)
	}
	if n, _ := s.CountLive("1001"); n != 1 {
		t.Fatalf("after reap CountLive = %d, want 1", n)
	}
}
