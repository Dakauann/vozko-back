package branch_usecase

import (
	"testing"
	"time"

	"vozko/domain/branch"
)

func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

// testNonceSecret is the fixed HMAC secret shared between the use case under test and
// the helper that forges matching client nonces, so issued nonces are deterministic.
var testNonceSecret = []byte("vozko-branch-test-secret-key")

func testNonceService(now func() time.Time) *branch.NonceService {
	return branch.NewNonceService(testNonceSecret, now)
}

func newRegisterUC(store branch.BindingStore, branches ...*branch.Branch) (branch.HandleRegisterUseCase, *fakePresence) {
	p := &fakePresence{}
	uc := NewHandleRegisterUseCase(newFakeRepo(branches...), store, p, "vozko",
		func() time.Time { return fixedNow() },
		testNonceService(func() time.Time { return fixedNow() }))
	return uc, p
}

// authedRequest builds a REGISTER whose digest response is correct for the branch,
// using the qop=auth path and a signed nonce issued (at fixedNow) for the request's
// source IP and the branch realm, exactly what the registrar would have challenged.
func authedRequest(b *branch.Branch, callID, contact, receivedFrom string, expires int) branch.RegisterRequest {
	return authedRequestAt(b, callID, contact, receivedFrom, expires, fixedNow())
}

// authedRequestAt is authedRequest with an explicit nonce-issue time, so a test can
// forge a stale (expired) nonce by issuing it in the past.
func authedRequestAt(b *branch.Branch, callID, contact, receivedFrom string, expires int, issuedAt time.Time) branch.RegisterRequest {
	const (
		uri    = "sip:vozko"
		nc     = "00000001"
		cnonce = "cnonce-1"
		qop    = "auth"
	)
	nonce := testNonceService(func() time.Time { return issuedAt }).Issue(hostOnly(receivedFrom), b.Realm)
	resp := branch.ComputeDigestResponse(b.SecretHA1, "REGISTER", uri, nonce, qop, nc, cnonce)
	return branch.RegisterRequest{
		SIPUser: b.SIPUser, HasAuth: true, Realm: b.Realm, Nonce: nonce, URI: uri,
		Response: resp, QOP: qop, NC: nc, CNonce: cnonce, Method: "REGISTER",
		Contact: contact, ReceivedFrom: receivedFrom, Transport: "udp",
		CallID: callID, CSeq: 1, UserAgent: "MicroSIP", RequestedExpires: expires,
	}
}

func TestHandleRegister_ChallengeWhenNoAuth(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, presence := newRegisterUC(newFakeStore(), b)

	res, err := uc.Execute(branch.RegisterRequest{SIPUser: "1001", HasAuth: false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != branch.RegisterChallenge {
		t.Fatalf("action = %q, want challenge", res.Action)
	}
	if res.Realm != "vozko" || res.Nonce == "" {
		t.Fatalf("challenge realm/nonce = %q/%q, want a signed nonce", res.Realm, res.Nonce)
	}
	// A bare challenge must NOT put the branch online (not authenticated yet).
	if len(presence.reachable) != 0 {
		t.Fatalf("challenge marked branch reachable: %+v", presence.reachable)
	}
}

func TestHandleRegister_SuccessStoresBinding(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	store := newFakeStore()
	uc, presence := newRegisterUC(store, b)

	res, err := uc.Execute(authedRequest(b, "call-1", "sip:1001@192.168.0.9:5060", "203.0.113.7:41000", 60))
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != branch.RegisterOK || res.GrantedExpires != 60 {
		t.Fatalf("action/expires = %q/%d, want ok/60", res.Action, res.GrantedExpires)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(store.upserts))
	}
	got := store.upserts[0]
	// rewrite_contact: the binding must route to the real packet source, not the
	// phone's private Contact host.
	if got.ReceivedFrom != "203.0.113.7:41000" {
		t.Errorf("ReceivedFrom = %q, want the real source", got.ReceivedFrom)
	}
	if got.BranchID != b.ID || got.WorkspaceID != "ws-1" {
		t.Errorf("binding not bound to the branch: %+v", got)
	}
	if !got.ExpiresAt.Equal(fixedNow().Add(60 * time.Second)) {
		t.Errorf("ExpiresAt = %v, want now+60s", got.ExpiresAt)
	}
	// A successful REGISTER must put the branch online so transfers can target it.
	if len(presence.reachable) != 1 || presence.reachable[0].BranchID != b.ID {
		t.Fatalf("presence.reachable = %+v, want one entry for %s", presence.reachable, b.ID)
	}
}

func TestHandleRegister_BadCredentials(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, _ := newRegisterUC(newFakeStore(), b)

	req := authedRequest(b, "call-1", "sip:1001@x", "1.2.3.4:5060", 60)
	req.Response = "totally-wrong"
	res, _ := uc.Execute(req)
	if res.Action != branch.RegisterUnauthorized {
		t.Fatalf("action = %q, want unauthorized", res.Action)
	}
}

func TestHandleRegister_Deregister(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	store := newFakeStore()
	uc, presence := newRegisterUC(store, b)

	res, _ := uc.Execute(authedRequest(b, "call-1", "sip:1001@x", "1.2.3.4:5060", 0))
	if res.Action != branch.RegisterDeregistered {
		t.Fatalf("action = %q, want deregistered", res.Action)
	}
	if len(store.removes) != 1 || store.removes[0] != [2]string{"1001", "call-1"} {
		t.Fatalf("removes = %v, want one (1001, call-1)", store.removes)
	}
	// De-registering the last contact must take the branch offline as a transfer target.
	if len(presence.unreachable) != 1 || presence.unreachable[0] != b.ID {
		t.Fatalf("presence.unreachable = %v, want [%s]", presence.unreachable, b.ID)
	}
}

func TestHandleRegister_IntervalTooBrief(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, _ := newRegisterUC(newFakeStore(), b)

	res, _ := uc.Execute(authedRequest(b, "call-1", "sip:1001@x", "1.2.3.4:5060", 5))
	if res.Action != branch.RegisterIntervalTooBrief || res.MinExpires != branch.MinExpiresSeconds {
		t.Fatalf("action/min = %q/%d, want interval_too_brief/%d", res.Action, res.MinExpires, branch.MinExpiresSeconds)
	}
}

// TestHandleRegister_EnumerationParity is the anti-enumeration guarantee: a valid, an
// unknown, and a disabled extension must be INDISTINGUISHABLE to an unauthenticated
// prober, all get the same 401 challenge (never 404/403), so an attacker cannot map
// which extensions exist. This is the successor to Asterisk's alwaysauthreject=yes.
func TestHandleRegister_EnumerationParity(t *testing.T) {
	valid := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	disabled := enabledBranch("1002", "ws-1", "vozko", "s3cret")
	disabled.Enabled = false
	uc, _ := newRegisterUC(newFakeStore(), valid, disabled)

	for _, su := range []string{"1001" /*valid*/, "9999" /*unknown*/, "1002" /*disabled*/} {
		res, err := uc.Execute(branch.RegisterRequest{SIPUser: su, HasAuth: false, ReceivedFrom: "1.2.3.4:5060"})
		if err != nil {
			t.Fatalf("%s: %v", su, err)
		}
		if res.Action != branch.RegisterChallenge || res.Realm != "vozko" || res.Nonce == "" {
			t.Fatalf("sip_user %s: action=%q realm=%q nonce=%q, want an identical challenge", su, res.Action, res.Realm, res.Nonce)
		}
	}

	// And with auth present, an unknown extension returns the SAME 401 as a bad
	// password (never 404), no existence leak on the second leg either.
	unknown := authedRequest(valid, "call-x", "sip:9999@x", "1.2.3.4:5060", 60)
	unknown.SIPUser = "9999"
	if res, _ := uc.Execute(unknown); res.Action != branch.RegisterUnauthorized {
		t.Fatalf("authed unknown user action = %q, want unauthorized (indistinguishable from bad password)", res.Action)
	}
}

// TestHandleRegister_StaleNonce: a nonce past its lifetime is our own but expired, so
// the phone is re-challenged with stale=true (retries silently, no password prompt).
func TestHandleRegister_StaleNonce(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, _ := newRegisterUC(newFakeStore(), b)

	req := authedRequestAt(b, "call-1", "sip:1001@x", "203.0.113.7:5060", 60, fixedNow().Add(-60*time.Second))
	res, _ := uc.Execute(req)
	if res.Action != branch.RegisterChallenge || !res.Stale {
		t.Fatalf("expired nonce action/stale = %q/%v, want challenge/true", res.Action, res.Stale)
	}
}

// TestHandleRegister_NonceBoundToSource: a valid nonce replayed from a DIFFERENT
// source IP fails verification (fresh challenge, not stale). This is what bounds a
// captured REGISTER to the original sender and blocks off-path replay/relay.
func TestHandleRegister_NonceBoundToSource(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, _ := newRegisterUC(newFakeStore(), b)

	req := authedRequest(b, "call-1", "sip:1001@x", "203.0.113.7:5060", 60)
	req.ReceivedFrom = "198.51.100.9:5060" // same Authorization, replayed from elsewhere
	res, _ := uc.Execute(req)
	if res.Action != branch.RegisterChallenge || res.Stale {
		t.Fatalf("cross-source replay action/stale = %q/%v, want challenge/false", res.Action, res.Stale)
	}
}

// TestHandleRegister_ForgedNonce: a well-shaped but unsigned nonce is rejected.
func TestHandleRegister_ForgedNonce(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret")
	uc, _ := newRegisterUC(newFakeStore(), b)

	req := authedRequest(b, "call-1", "sip:1001@x", "203.0.113.7:5060", 60)
	req.Nonce = "1700000000.deadbeefdeadbeef" // right shape, wrong signature
	res, _ := uc.Execute(req)
	if res.Action != branch.RegisterChallenge || res.Stale {
		t.Fatalf("forged nonce action/stale = %q/%v, want challenge/false", res.Action, res.Stale)
	}
}

// TestHandleRegister_MaxContacts: an AOR already at MaxContacts (default 2) rejects a
// new distinct contact (403) but still allows refreshing an existing one.
func TestHandleRegister_MaxContacts(t *testing.T) {
	b := enabledBranch("1001", "ws-1", "vozko", "s3cret") // DefaultMaxContacts = 2
	store := newFakeStore()
	store.live["1001"] = []branch.RegistrationBinding{
		{SIPUser: "1001", CallID: "call-a", ExpiresAt: fixedNow().Add(time.Minute)},
		{SIPUser: "1001", CallID: "call-b", ExpiresAt: fixedNow().Add(time.Minute)},
	}
	uc, _ := newRegisterUC(store, b)

	// A THIRD distinct contact is rejected and NOT stored.
	res, _ := uc.Execute(authedRequest(b, "call-c", "sip:1001@x", "203.0.113.7:5060", 60))
	if res.Action != branch.RegisterForbidden {
		t.Fatalf("3rd contact action = %q, want forbidden", res.Action)
	}
	if len(store.upserts) != 0 {
		t.Fatalf("rejected contact was still upserted: %+v", store.upserts)
	}

	// Refreshing an EXISTING contact (same call_id) stays allowed even at the cap.
	res, _ = uc.Execute(authedRequest(b, "call-a", "sip:1001@x", "203.0.113.7:5060", 60))
	if res.Action != branch.RegisterOK {
		t.Fatalf("refresh of existing contact action = %q, want ok", res.Action)
	}
}
