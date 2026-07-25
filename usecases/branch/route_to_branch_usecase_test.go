package branch_usecase

import (
	"testing"
	"time"

	"vozko/domain/branch"
)

func liveBinding(sipUser string) branch.RegistrationBinding {
	return branch.RegistrationBinding{SIPUser: sipUser, CallID: "c1", ReceivedFrom: "203.0.113.7:41000", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestRouteToBranch_OKReturnsContacts(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	store := newFakeStore()
	store.live["1001"] = []branch.RegistrationBinding{liveBinding("1001")}
	uc := NewRouteToBranchUseCase(newFakeRepo(b), store)

	res, err := uc.Execute("ws-1", "1001")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != branch.RouteOK || len(res.Contacts) != 1 {
		t.Fatalf("reason/contacts = %q/%d, want ok/1", res.Reason, len(res.Contacts))
	}
}

func TestRouteToBranch_Offline(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc := NewRouteToBranchUseCase(newFakeRepo(b), newFakeStore()) // no live bindings

	res, _ := uc.Execute("ws-1", "1001")
	if res.Reason != branch.RouteOffline {
		t.Fatalf("reason = %q, want offline", res.Reason)
	}
}

func TestRouteToBranch_DNDAndDisabled(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	store := newFakeStore()
	store.live["1001"] = []branch.RegistrationBinding{liveBinding("1001")}

	b.DND = true
	if res, _ := NewRouteToBranchUseCase(newFakeRepo(b), store).Execute("ws-1", "1001"); res.Reason != branch.RouteDND {
		t.Fatalf("reason = %q, want dnd", res.Reason)
	}

	b.DND = false
	b.Enabled = false
	if res, _ := NewRouteToBranchUseCase(newFakeRepo(b), store).Execute("ws-1", "1001"); res.Reason != branch.RouteDisabled {
		t.Fatalf("reason = %q, want disabled", res.Reason)
	}
}

func TestRouteToBranch_CrossWorkspaceIsolation(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	store := newFakeStore()
	store.live["1001"] = []branch.RegistrationBinding{liveBinding("1001")}
	uc := NewRouteToBranchUseCase(newFakeRepo(b), store)

	// A transfer from a DIFFERENT workspace must not resolve this branch.
	res, _ := uc.Execute("ws-OTHER", "1001")
	if res.Reason != branch.RouteBranchNotFound {
		t.Fatalf("reason = %q, want not_found (cross-workspace isolation)", res.Reason)
	}
}

func TestRouteToBranch_NotFound(t *testing.T) {
	uc := NewRouteToBranchUseCase(newFakeRepo(), newFakeStore())
	if res, _ := uc.Execute("ws-1", "9999"); res.Reason != branch.RouteBranchNotFound {
		t.Fatalf("reason = %q, want not_found", res.Reason)
	}
}
