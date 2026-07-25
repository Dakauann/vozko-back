package branchinfra

import (
	"testing"

	dialer_domain "vozko/domain/dialer"
	dialerinfra "vozko/infra/dialer"
)

// TestPresenceRegistrar_MakesBranchATransferTarget is the end-to-end proof at the
// seam: after a phone registers, the branch is resolvable by the SAME registry the
// transfer engine uses (FindByUser), and offering it a transfer rings the phone.
func TestPresenceRegistrar_MakesBranchATransferTarget(t *testing.T) {
	registry := dialerinfra.NewInProcSessionRegistry()
	ring := &fakeRing{}
	p := NewPresenceRegistrar(registry, ring, nil, nil)

	// Before registration the user is not a dialer target at all.
	if _, ok := registry.FindByUser("ws-1", "user-1"); ok {
		t.Fatal("user must not be a target before the phone registers")
	}

	p.OnBranchReachable(regBranch())

	sess, ok := registry.FindByUser("ws-1", "user-1")
	if !ok {
		t.Fatal("a registered branch must be findable by the transfer engine's FindByUser")
	}
	if sess.ID() != "branch:b1" {
		t.Fatalf("resolved session id = %q, want branch:b1", sess.ID())
	}

	// The transfer engine offers the target by calling Notify; for a branch that
	// must ring the phone.
	if err := sess.Notify(offer("call-1", "tr-1")); err != nil {
		t.Fatal(err)
	}
	if ring.count() != 1 {
		t.Fatalf("offering a transfer to the branch must ring the phone, got %d", ring.count())
	}

	// A reserved branch is excluded from ListAvailable, exactly like a busy agent.
	sess.Reserve("tr-1")
	if got := registry.ListAvailable("ws-1"); len(got) != 0 {
		t.Fatalf("reserved branch must be excluded from ListAvailable, got %d", len(got))
	}
	sess.Release("tr-1")

	// Re-REGISTER is idempotent: still exactly one session.
	p.OnBranchReachable(regBranch())
	if all := registry.ListAll("ws-1"); len(all) != 1 {
		t.Fatalf("re-register must not duplicate the session, got %d", len(all))
	}

	// When the phone goes offline the branch stops being a target.
	p.OnBranchUnreachable("b1")
	if _, ok := registry.FindByUser("ws-1", "user-1"); ok {
		t.Fatal("an offline branch must be removed as a transfer target")
	}
}

var _ dialer_domain.DialerSessionRegistry = (*dialerinfra.InProcSessionRegistry)(nil)
