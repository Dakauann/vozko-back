package voipinfra

import (
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"vozko/domain/branch"
)

// qualifyFakeStore implements the BindingStore surface qualify touches (Remove +
// CountLive); the rest are no-ops.
type qualifyFakeStore struct {
	live    map[string]int // sip_user -> live contact count
	removed [][2]string
}

func (s *qualifyFakeStore) Upsert(branch.RegistrationBinding) error { return nil }
func (s *qualifyFakeStore) Remove(sipUser, callID string) error {
	s.removed = append(s.removed, [2]string{sipUser, callID})
	if s.live[sipUser] > 0 {
		s.live[sipUser]--
	}
	return nil
}
func (s *qualifyFakeStore) ListLive(string) ([]branch.RegistrationBinding, error) { return nil, nil }
func (s *qualifyFakeStore) CountLive(sipUser string) (int, error)                 { return s.live[sipUser], nil }
func (s *qualifyFakeStore) ReapExpired(time.Time) []branch.RegistrationBinding    { return nil }

type qualifyFakePresence struct{ unreachable []string }

func (p *qualifyFakePresence) OnBranchReachable(branch.RegisteredBranch) {}
func (p *qualifyFakePresence) OnBranchUnreachable(id string) {
	p.unreachable = append(p.unreachable, id)
}

func qualifyBinding() branch.RegistrationBinding {
	return branch.RegistrationBinding{
		SIPUser: "1001", CallID: "call-a", BranchID: "branch-1001",
		Contact: "sip:1001@203.0.113.7:5060", ReceivedFrom: "203.0.113.7:5060",
	}
}

func newQualifyRegistrar(store branch.BindingStore, presence branch.BranchPresenceListener) *BranchRegistrar {
	return NewBranchRegistrar(
		BranchRegistrarConfig{PublicHost: "127.0.0.1", ListenPort: 5070, Realm: "vozko"},
		nil, store, nil, presence, nil,
	)
}

// A contact is evicted only after qualifyMaxMisses consecutive no-response probes, and
// that takes the branch offline when it was the last contact.
func TestQualify_EvictsAfterConsecutiveMisses(t *testing.T) {
	store := &qualifyFakeStore{live: map[string]int{"1001": 1}}
	presence := &qualifyFakePresence{}
	reg := newQualifyRegistrar(store, presence)
	reg.probe = func(sip.Uri) bool { return false } // always miss

	b := qualifyBinding()
	for i := 1; i < qualifyMaxMisses; i++ {
		reg.qualifyContact(b)
		if len(store.removed) != 0 {
			t.Fatalf("evicted after %d miss(es); want eviction only at %d", i, qualifyMaxMisses)
		}
	}
	reg.qualifyContact(b) // the qualifyMaxMisses-th miss
	if len(store.removed) != 1 || store.removed[0] != [2]string{"1001", "call-a"} {
		t.Fatalf("removed = %v, want one (1001, call-a) after %d misses", store.removed, qualifyMaxMisses)
	}
	if len(presence.unreachable) != 1 || presence.unreachable[0] != "branch-1001" {
		t.Fatalf("unreachable = %v, want [branch-1001] once the last contact is gone", presence.unreachable)
	}
}

// A successful probe resets the miss counter, so an occasional dropped packet never
// evicts a live phone.
func TestQualify_SuccessResetsMisses(t *testing.T) {
	store := &qualifyFakeStore{live: map[string]int{"1001": 1}}
	reg := newQualifyRegistrar(store, &qualifyFakePresence{})

	reachable := false
	reg.probe = func(sip.Uri) bool { return reachable }

	b := qualifyBinding()
	reg.qualifyContact(b) // miss #1
	reachable = true
	reg.qualifyContact(b) // success -> counter reset
	reachable = false
	reg.qualifyContact(b) // miss #1 again (not the 2nd)

	if len(store.removed) != 0 {
		t.Fatalf("evicted despite an intervening success; misses should have reset: %v", store.removed)
	}
}
