package sip_trunk_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/sip_trunk"
	callsession_usecase "vozko/usecases/callsession"
)

type memberSession struct {
	id, userID, workspaceID string
	mu                      sync.Mutex
	reserved                string
	offers                  chan callsession.InboundCallOffer
	withdrawn               atomic.Int32
}

func newMemberSession(userID, workspaceID string) *memberSession {
	return &memberSession{id: "session-" + userID, userID: userID, workspaceID: workspaceID, offers: make(chan callsession.InboundCallOffer, 4)}
}

func (s *memberSession) ID() string           { return s.id }
func (s *memberSession) UserID() string       { return s.userID }
func (s *memberSession) WorkspaceID() string  { return s.workspaceID }
func (s *memberSession) ActiveCallID() string { return "" }
func (s *memberSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserved != ""
}
func (s *memberSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved != "" {
		return false
	}
	s.reserved = token
	return true
}
func (s *memberSession) Release(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved == token {
		s.reserved = ""
	}
}
func (s *memberSession) Notify(msg callsession.CallSessionControlMessage) error {
	if offer, ok := msg.Payload.(callsession.InboundCallOffer); ok {
		s.offers <- offer
		return nil
	}
	s.withdrawn.Add(1)
	return nil
}

type workspaceSessions struct {
	callsession.CallSessionRegistry
	byWorkspace map[string][]callsession.CallSession
}

func (r workspaceSessions) ListAvailable(workspaceID string) []callsession.CallSession {
	return r.byWorkspace[workspaceID]
}

type countingAdmission struct {
	acquireErr error
	acquired   atomic.Int32
	released   atomic.Int32
	channel    atomic.Value
}

func (a *countingAdmission) Acquire(_ context.Context, input callsession.CallAdmissionInput) (*callsession.CallAdmissionLease, error) {
	a.channel.Store(input.CallChannel)
	if a.acquireErr != nil {
		return nil, a.acquireErr
	}
	a.acquired.Add(1)
	return &callsession.CallAdmissionLease{WorkspaceID: input.WorkspaceID, SlotAcquired: true}, nil
}
func (a *countingAdmission) Refresh(*callsession.CallAdmissionLease, time.Duration) error { return nil }
func (a *countingAdmission) Release(*callsession.CallAdmissionLease) error {
	a.released.Add(1)
	return nil
}

type attachingExecutor struct {
	mu       sync.Mutex
	attached []callsession.AttachInboundCRMCallInput
	err      error
}

func (e *attachingExecutor) AttachInboundCRMCall(_ context.Context, input callsession.AttachInboundCRMCallInput) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.attached = append(e.attached, input)
	if starter, ok := input.Call.(interface{ Start() }); ok {
		starter.Start()
	}
	return nil
}

type ringingDialog struct {
	answered atomic.Int32
	ringing  atomic.Int32
	hungUp   atomic.Int32
	gone     chan struct{}
	audio    *fakePCM
}

func newRingingDialog() *ringingDialog {
	return &ringingDialog{gone: make(chan struct{}), audio: newFakePCM()}
}

func (d *ringingDialog) ID() string       { return "dialog-1" }
func (d *ringingDialog) FromUser() string { return "+5511988887777" }
func (d *ringingDialog) ToUser() string   { return "4000" }
func (d *ringingDialog) Trying() error    { return nil }
func (d *ringingDialog) Ringing() error {
	d.ringing.Add(1)
	return nil
}
func (d *ringingDialog) Answer(context.Context) (sip_trunk.TrunkCallSession, error) {
	d.answered.Add(1)
	return sip_trunk.TrunkCallSession{ID: "dialog-1", TrunkID: "trunk-owned", Direction: sip_trunk.CallDirectionInbound, Audio: d.audio}, nil
}
func (d *ringingDialog) Hangup(context.Context) error {
	d.hungUp.Add(1)
	return nil
}
func (d *ringingDialog) Done() <-chan struct{} { return d.gone }

type inboundFixture struct {
	handler   *InboundCallUseCase
	broker    *callsession_usecase.InboundOfferBroker
	admission *countingAdmission
	executor  *attachingExecutor
	engine    *fakeEngine
}

func newInboundFixture(sessions map[string][]callsession.CallSession, granted grantedCallers) inboundFixture {
	broker := callsession_usecase.NewInboundOfferBroker()
	f := inboundFixture{broker: broker, admission: &countingAdmission{}, executor: &attachingExecutor{}, engine: newFakeEngine()}
	f.handler = NewInboundCallUseCase(InboundCallConfig{
		Sessions:    workspaceSessions{byWorkspace: sessions},
		Admission:   f.admission,
		Ringer:      callsession_usecase.NewInboundRinger(broker),
		Executor:    f.executor,
		Permissions: granted,
		Engine:      f.engine,
		RingWindow:  400 * time.Millisecond,
		PerMember:   200 * time.Millisecond,
	})
	return f
}

func invite(dialog *ringingDialog) sip_trunk.InboundInvite {
	return sip_trunk.InboundInvite{ID: "dialog-1", TrunkID: "trunk-owned", WorkspaceID: ownerWorkspace, FromNumber: "+5511988887777", ToNumber: "4000", Dialog: dialog}
}

func (f inboundFixture) accept(t *testing.T, member *memberSession) callsession.InboundCallOffer {
	t.Helper()
	select {
	case offer := <-member.offers:
		if err := f.broker.Accept(context.Background(), callsession.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: offer.WorkspaceID, UserID: member.userID, SessionID: member.id}); err != nil {
			t.Fatalf("accept: %v", err)
		}
		return offer
	case <-time.After(2 * time.Second):
		t.Fatal("the member was never rung")
	}
	return callsession.InboundCallOffer{}
}

func TestInboundCallRingsOnlyPermittedMembersOfTheTrunkWorkspace(t *testing.T) {
	permitted := newMemberSession("agent", ownerWorkspace)
	notPermitted := newMemberSession("viewer", ownerWorkspace)
	stranger := newMemberSession("stranger", strangerWorkspace)
	f := newInboundFixture(map[string][]callsession.CallSession{
		ownerWorkspace:    {notPermitted, permitted},
		strangerWorkspace: {stranger},
	}, grantedCallers{"agent|" + ownerWorkspace: true, "stranger|" + strangerWorkspace: true})
	dialog := newRingingDialog()

	done := make(chan error, 1)
	go func() { done <- f.handler.HandleInboundInvite(context.Background(), invite(dialog)) }()
	offer := f.accept(t, permitted)

	if offer.Channel != "sip" || offer.WorkspaceID != ownerWorkspace || offer.FromNumber != "+5511988887777" {
		t.Fatalf("offer = %+v, want a sip offer in the trunk workspace", offer)
	}
	select {
	case <-notPermitted.offers:
		t.Fatal("a member without sip_trunks:call was rung")
	case <-stranger.offers:
		t.Fatal("a member of another workspace was rung")
	default:
	}
	waitFor(t, func() bool {
		f.executor.mu.Lock()
		defer f.executor.mu.Unlock()
		return len(f.executor.attached) == 1
	})
	f.executor.mu.Lock()
	attached := f.executor.attached[0]
	f.executor.mu.Unlock()
	if attached.UserID != "agent" || attached.OfferID != offer.OfferID || !strings.HasPrefix(attached.Call.ID(), "sip-in-") || attached.Admission == nil {
		t.Fatalf("attached = %+v, want the accepting member with the admitted sip-in call", attached)
	}
	if got := f.admission.channel.Load(); got != "sip" {
		t.Fatalf("admission channel = %v, want sip", got)
	}
	select {
	case <-done:
		t.Fatal("the handler returned while the call was up; returning hangs the caller up")
	default:
	}
	_ = dialog.audio.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleInboundInvite() = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the handler did not return after the call ended")
	}
	if f.admission.released.Load() != 0 {
		t.Fatal("the lease was released by the handler although the attached call owns it")
	}
}

func TestInboundCallWithNobodyToRingIsNotAdmittedNorAnswered(t *testing.T) {
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {newMemberSession("viewer", ownerWorkspace)}}, grantedCallers{})
	dialog := newRingingDialog()
	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); !errors.Is(err, ErrNoMemberAvailable) {
		t.Fatalf("HandleInboundInvite() = %v, want ErrNoMemberAvailable", err)
	}
	if f.admission.acquired.Load() != 0 || dialog.answered.Load() != 0 {
		t.Fatal("a call nobody could take was admitted or answered")
	}
}

func TestInboundCallRefusedByAdmissionIsNeverAnswered(t *testing.T) {
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {newMemberSession("agent", ownerWorkspace)}}, grantedCallers{"agent|" + ownerWorkspace: true})
	f.admission.acquireErr = callsession.ErrNoCallSlotsAvailable
	dialog := newRingingDialog()
	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); !errors.Is(err, callsession.ErrNoCallSlotsAvailable) {
		t.Fatalf("HandleInboundInvite() = %v, want the admission error", err)
	}
	if dialog.answered.Load() != 0 || dialog.ringing.Load() != 0 {
		t.Fatal("a call without a slot was rung or answered")
	}
}

func TestUnansweredInboundCallReleasesItsLeaseAndStaysUnanswered(t *testing.T) {
	member := newMemberSession("agent", ownerWorkspace)
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {member}}, grantedCallers{"agent|" + ownerWorkspace: true})
	dialog := newRingingDialog()
	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); err != nil {
		t.Fatalf("HandleInboundInvite() = %v", err)
	}
	if dialog.ringing.Load() != 1 || dialog.answered.Load() != 0 {
		t.Fatalf("ringing=%d answered=%d, want a ringing, unanswered call", dialog.ringing.Load(), dialog.answered.Load())
	}
	if f.admission.released.Load() != 1 {
		t.Fatalf("lease releases = %d, want 1", f.admission.released.Load())
	}
	if member.HasActiveCall() {
		t.Fatal("the member stayed reserved after the ring window")
	}
	if member.withdrawn.Load() != 1 {
		t.Fatalf("withdrawn notices = %d, want the expired offer withdrawn from the member", member.withdrawn.Load())
	}
}

func TestACallerWhoHangsUpWhileRingingFreesEverything(t *testing.T) {
	member := newMemberSession("agent", ownerWorkspace)
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {member}}, grantedCallers{"agent|" + ownerWorkspace: true})
	dialog := newRingingDialog()
	done := make(chan error, 1)
	go func() { done <- f.handler.HandleInboundInvite(context.Background(), invite(dialog)) }()
	<-member.offers
	close(dialog.gone)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the handler kept ringing after the caller left")
	}
	if dialog.answered.Load() != 0 || f.admission.released.Load() != 1 || member.HasActiveCall() {
		t.Fatalf("answered=%d releases=%d reserved=%v, want nothing held", dialog.answered.Load(), f.admission.released.Load(), member.HasActiveCall())
	}
}

func TestAFailedAttachHangsTheAnsweredCallUp(t *testing.T) {
	member := newMemberSession("agent", ownerWorkspace)
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {member}}, grantedCallers{"agent|" + ownerWorkspace: true})
	f.executor.err = callsession.ErrSessionBusy
	dialog := newRingingDialog()
	done := make(chan error, 1)
	go func() { done <- f.handler.HandleInboundInvite(context.Background(), invite(dialog)) }()
	f.accept(t, member)
	select {
	case err := <-done:
		if !errors.Is(err, callsession.ErrSessionBusy) {
			t.Fatalf("HandleInboundInvite() = %v, want the attach error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the handler hung after a failed attach")
	}
	if f.engine.hangupCount() != 1 {
		t.Fatalf("engine hangups = %d, want the answered leg hung up", f.engine.hangupCount())
	}
	if member.HasActiveCall() {
		t.Fatal("the member stayed reserved after a failed attach")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
