package branchinfra

import (
	"sync"
	"testing"

	"vozko/domain/branch"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/voip"
	dialer_infra "vozko/infra/dialer"
)

// fakeBridge records the branch consult media-bridge calls without any real SIP/RTP.
type fakeBridge struct {
	mu             sync.Mutex
	consultStarted bool
	consultStops   int
	cancels        int
	onPhoneGone    func()
}

func (b *fakeBridge) StartBridge(string, string, voip.MediaSession, func()) (func(), error) {
	return func() {}, nil
}
func (b *fakeBridge) StartConsultBridge(_ string, _ branch.ConsultPeer, onPhoneGone func()) (func(), error) {
	b.mu.Lock()
	b.consultStarted = true
	b.onPhoneGone = onPhoneGone
	b.mu.Unlock()
	return func() { b.mu.Lock(); b.consultStops++; b.mu.Unlock() }, nil
}
func (b *fakeBridge) CancelConsult(string) { b.mu.Lock(); b.cancels++; b.mu.Unlock() }

func (b *fakeBridge) snapshot() (bool, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.consultStarted, b.consultStops, b.cancels
}

type fakeRing struct {
	mu      sync.Mutex
	calls   []branch.BranchRingRequest
	cancels []branch.BranchRingRequest
}

func (r *fakeRing) Ring(req branch.BranchRingRequest) error {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	return nil
}

func (r *fakeRing) CancelRing(req branch.BranchRingRequest) error {
	r.mu.Lock()
	r.cancels = append(r.cancels, req)
	r.mu.Unlock()
	return nil
}

func (r *fakeRing) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func regBranch() branch.RegisteredBranch {
	return branch.RegisteredBranch{BranchID: "b1", SIPUser: "1001", WorkspaceID: "ws-1", UserID: "user-1"}
}

func offer(callID, transferID string) dialer_domain.DialerControlMessage {
	return dialer_domain.DialerControlMessage{
		Type:    "transfer:offer",
		Payload: map[string]any{"call_id": callID, "transfer_id": transferID},
	}
}

func TestBranchSession_ReservationContract(t *testing.T) {
	s := newBranchSession(regBranch(), &fakeRing{}, nil, nil)

	if s.HasActiveCall() {
		t.Fatal("fresh session must be idle")
	}
	if !s.Reserve("t1") {
		t.Fatal("Reserve on idle must succeed")
	}
	if !s.HasActiveCall() {
		t.Fatal("a reserved branch must read busy so it is not double-rung")
	}
	if s.ActiveCallID() != "" {
		t.Fatal("reserved-only must have no active call id")
	}
	if s.Reserve("t2") {
		t.Fatal("a different token must not steal a live reservation")
	}
	if !s.Reserve("t1") {
		t.Fatal("same-token reserve must be idempotent")
	}
	s.Release("t1")
	if s.HasActiveCall() {
		t.Fatal("released branch must be idle again")
	}
}

func TestBranchSession_MarkActiveConsumesReservation(t *testing.T) {
	s := newBranchSession(regBranch(), &fakeRing{}, nil, nil)
	s.Reserve("t1")
	s.markActive("call-9")

	if s.ActiveCallID() != "call-9" || !s.HasActiveCall() {
		t.Fatal("markActive must set the active call")
	}
	if s.Reserve("t2") {
		t.Fatal("a session on a call must not be reservable")
	}
	// Stale release of the consumed token must not free an active session.
	s.Release("t1")
	if !s.HasActiveCall() {
		t.Fatal("active session must stay busy after a stale release")
	}
	s.clearActive()
	if s.HasActiveCall() {
		t.Fatal("cleared session must be idle")
	}
}

func TestBranchSession_NotifyOfferRings(t *testing.T) {
	ring := &fakeRing{}
	s := newBranchSession(regBranch(), ring, nil, nil)

	if err := s.Notify(offer("c1", "tr1")); err != nil {
		t.Fatal(err)
	}
	if ring.count() != 1 {
		t.Fatalf("offer must ring the phone once, got %d", ring.count())
	}
	got := ring.calls[0]
	if got.CallID != "c1" || got.TransferID != "tr1" || got.SIPUser != "1001" || got.WorkspaceID != "ws-1" {
		t.Fatalf("ring request wrong: %+v", got)
	}

	// A non-offer control message must NOT ring a phone.
	if err := s.Notify(dialer_domain.DialerControlMessage{Type: "presence:update"}); err != nil {
		t.Fatal(err)
	}
	if ring.count() != 1 {
		t.Fatalf("non-offer message rang the phone, count=%d", ring.count())
	}
}

func TestBranchSession_NotifyWithoutRingErrors(t *testing.T) {
	s := newBranchSession(regBranch(), nil, nil, nil)
	if err := s.Notify(offer("c1", "tr1")); err == nil {
		t.Fatal("an offer with no ring gateway must error")
	}
}

// A ramal now hosts an attended consult: AttachConsult starts the transcode
// relay; DetachConsult stops it (keeping the phone for completion); AbortConsult
// stops it and hangs the phone up (cancel).
func TestBranchSession_AttendedConsult_Lifecycle(t *testing.T) {
	bridge := &fakeBridge{}
	s := newBranchSession(regBranch(), &fakeRing{}, bridge, nil)
	endpoint := dialer_infra.NewConsultBridge(0).B()

	if err := s.AttachConsult(endpoint); err != nil {
		t.Fatalf("AttachConsult: %v", err)
	}
	if started, _, _ := bridge.snapshot(); !started {
		t.Fatal("AttachConsult must start the consult relay")
	}

	// Complete path: DetachConsult stops the relay WITHOUT hanging up the phone.
	s.DetachConsult()
	if _, stops, cancels := bridge.snapshot(); stops != 1 || cancels != 0 {
		t.Fatalf("DetachConsult: stops=%d cancels=%d, want 1/0 (keep the phone for completion)", stops, cancels)
	}

	// A following AbortConsult (defensive on complete) does not double-stop; on a
	// real cancel it BYEs the phone.
	s.AbortConsult()
	if _, stops, cancels := bridge.snapshot(); stops != 1 || cancels != 1 {
		t.Fatalf("AbortConsult: stops=%d cancels=%d, want 1/1", stops, cancels)
	}
}

// AbortConsult straight after AttachConsult (cancel before any DetachConsult)
// stops the relay AND hangs the phone up.
func TestBranchSession_AttendedConsult_AbortStopsAndHangsUp(t *testing.T) {
	bridge := &fakeBridge{}
	s := newBranchSession(regBranch(), &fakeRing{}, bridge, nil)
	if err := s.AttachConsult(dialer_infra.NewConsultBridge(0).B()); err != nil {
		t.Fatalf("AttachConsult: %v", err)
	}
	s.AbortConsult()
	if _, stops, cancels := bridge.snapshot(); stops != 1 || cancels != 1 {
		t.Fatalf("AbortConsult: stops=%d cancels=%d, want 1/1", stops, cancels)
	}
}

// A phone hangup DURING the consult drives the target-disconnect abort so the
// caller resumes with the agent (mirrors a browser target's socket drop).
func TestBranchSession_AttendedConsult_PhoneGoneAborts(t *testing.T) {
	bridge := &fakeBridge{}
	s := newBranchSession(regBranch(), &fakeRing{}, bridge, nil)

	var gotTID, gotReason string
	s.SetTransferContext("tr-9", func(transferID, reason string) {
		gotTID, gotReason = transferID, reason
	})
	if err := s.AttachConsult(dialer_infra.NewConsultBridge(0).B()); err != nil {
		t.Fatalf("AttachConsult: %v", err)
	}

	// The registrar's relay fires onPhoneGone when the phone hangs up.
	bridge.mu.Lock()
	onGone := bridge.onPhoneGone
	bridge.mu.Unlock()
	if onGone == nil {
		t.Fatal("AttachConsult must pass an onPhoneGone hook")
	}
	onGone()

	if gotTID != "tr-9" || gotReason != string(dialer_domain.TransferReasonTargetDisconnect) {
		t.Fatalf("phone-gone abort = (%q,%q), want (tr-9, target_disconnect)", gotTID, gotReason)
	}

	// After the context is cleared, a stray phone-gone is a no-op.
	s.ClearTransferContext()
	gotTID = ""
	onGone()
	if gotTID != "" {
		t.Fatal("cleared transfer context must not abort")
	}
}
