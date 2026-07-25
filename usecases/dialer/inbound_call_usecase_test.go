package dialer_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/dialer"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
)

type inboundTestSession struct {
	id, userID, workspaceID string
	mu                      sync.Mutex
	active                  bool
	reserved                string
	notifyCh                chan dialer.DialerControlMessage
}

func newInboundTestSession(id, userID, workspaceID string) *inboundTestSession {
	return &inboundTestSession{id: id, userID: userID, workspaceID: workspaceID, notifyCh: make(chan dialer.DialerControlMessage, 4)}
}

func (s *inboundTestSession) ID() string          { return s.id }
func (s *inboundTestSession) UserID() string      { return s.userID }
func (s *inboundTestSession) WorkspaceID() string { return s.workspaceID }
func (s *inboundTestSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active || s.reserved != ""
}
func (s *inboundTestSession) ActiveCallID() string { return "" }
func (s *inboundTestSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || s.active || s.reserved != "" {
		return false
	}
	s.reserved = token
	return true
}
func (s *inboundTestSession) Release(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved == token {
		s.reserved = ""
	}
}
func (s *inboundTestSession) Notify(msg dialer.DialerControlMessage) error {
	s.notifyCh <- msg
	return nil
}

type inboundTestRegistry struct {
	available []dialer.DialerSession
}

func (r *inboundTestRegistry) Register(dialer.DialerSession) (func(), error) { return func() {}, nil }
func (r *inboundTestRegistry) FindByUser(string, string) (dialer.DialerSession, bool) {
	return nil, false
}
func (r *inboundTestRegistry) FindSessionsByUser(string, string) []dialer.DialerSession { return nil }
func (r *inboundTestRegistry) FindByID(string) (dialer.DialerSession, bool)             { return nil, false }
func (r *inboundTestRegistry) ListAvailable(string) []dialer.DialerSession { return r.available }
func (r *inboundTestRegistry) ListAll(string) []dialer.DialerSession       { return r.available }
func (r *inboundTestRegistry) ListPresence(string) []dialer.MemberPresence { return nil }
func (r *inboundTestRegistry) ListBrowserSessions(string) []dialer.DialerSession {
	return nil
}
func (r *inboundTestRegistry) SetPresenceListener(dialer.PresenceListener) {}
func (r *inboundTestRegistry) NotifyPresenceChanged(string)                {}

type inboundTestAdmission struct {
	lease        *dialer.CallAdmissionLease
	acquireCalls int
	releaseCalls int
}

func (a *inboundTestAdmission) Acquire(context.Context, dialer.CallAdmissionInput) (*dialer.CallAdmissionLease, error) {
	a.acquireCalls++
	if a.lease == nil {
		a.lease = &dialer.CallAdmissionLease{WorkspaceID: "ws-1", SlotAcquired: true}
	}
	return a.lease, nil
}
func (a *inboundTestAdmission) Refresh(*dialer.CallAdmissionLease, time.Duration) error { return nil }
func (a *inboundTestAdmission) Release(*dialer.CallAdmissionLease) error {
	a.releaseCalls++
	return nil
}

type inboundTestDialog struct {
	id, from, to string
	trying       int
	ringing      int
	answered     int
	hungup       int
}

func (d *inboundTestDialog) ID() string       { return d.id }
func (d *inboundTestDialog) FromUser() string { return d.from }
func (d *inboundTestDialog) ToUser() string   { return d.to }
func (d *inboundTestDialog) Trying() error {
	d.trying++
	return nil
}
func (d *inboundTestDialog) Ringing() error {
	d.ringing++
	return nil
}
func (d *inboundTestDialog) Answer(_ context.Context, _ *voip.RecordingMeta) (sip_trunk.TrunkCallSession, error) {
	d.answered++
	return sip_trunk.TrunkCallSession{
		ID:          d.id,
		TrunkID:     "trunk-1",
		PhoneNumber: d.from,
		Direction:   "inbound",
		StartedAt:   time.Now(),
	}, nil
}
func (d *inboundTestDialog) Hangup(context.Context) error {
	d.hungup++
	return nil
}

type inboundTestCall struct {
	done chan struct{}
	once sync.Once
}

func newInboundTestCall() *inboundTestCall        { return &inboundTestCall{done: make(chan struct{})} }
func (c *inboundTestCall) ID() string             { return "call-inbound-1" }
func (c *inboundTestCall) SendAudio([]byte) error { return nil }
func (c *inboundTestCall) AudioStream() <-chan []byte {
	return make(chan []byte)
}
func (c *inboundTestCall) Events() <-chan conversation.CallEvent {
	return make(chan conversation.CallEvent)
}
func (c *inboundTestCall) Hangup() error {
	c.once.Do(func() { close(c.done) })
	return nil
}
func (c *inboundTestCall) Done() <-chan struct{} { return c.done }
func (c *inboundTestCall) close()                { c.once.Do(func() { close(c.done) }) }

type inboundTestExecutor struct {
	call      *inboundTestCall
	attachCh  chan dialer.AttachInboundCallInput
	attachErr error
}

func (e *inboundTestExecutor) AttachInboundCall(_ context.Context, input dialer.AttachInboundCallInput) (conversation.CRMCall, error) {
	if e.attachCh != nil {
		e.attachCh <- input
	}
	if e.attachErr != nil {
		return nil, e.attachErr
	}
	return e.call, nil
}

type inboundTestFailingDialog struct {
	inboundTestDialog
	answerErr error
}

func (d *inboundTestFailingDialog) Answer(_ context.Context, _ *voip.RecordingMeta) (sip_trunk.TrunkCallSession, error) {
	d.answered++
	if d.answerErr != nil {
		return sip_trunk.TrunkCallSession{}, d.answerErr
	}
	return sip_trunk.TrunkCallSession{
		ID:          d.id,
		TrunkID:     "trunk-1",
		PhoneNumber: d.from,
		Direction:   "inbound",
		StartedAt:   time.Now(),
	}, nil
}

type inboundTestFailingSession struct {
	inboundTestSession
	notifyErr error
}

func (s *inboundTestFailingSession) Notify(msg dialer.DialerControlMessage) error {
	if s.notifyErr != nil {
		return s.notifyErr
	}
	return s.inboundTestSession.Notify(msg)
}

func TestInboundCallUseCaseOffersFirstAvailableSession(t *testing.T) {
	first := newInboundTestSession("s-1", "u-1", "ws-1")
	second := newInboundTestSession("s-2", "u-2", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{first, second}}, &inboundTestAdmission{}, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, first)
	if msg.Type != dialer.DialerControlInboundCall {
		t.Fatalf("offer type = %q, want %q", msg.Type, dialer.DialerControlInboundCall)
	}
	select {
	case <-second.notifyCh:
		t.Fatal("second session received offer; first connected idle session should be selected")
	default:
	}

	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
}

func TestInboundCallUseCaseAcceptAnswersAndAttaches(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	call := newInboundTestCall()
	executor := &inboundTestExecutor{call: call, attachCh: make(chan dialer.AttachInboundCallInput, 1)}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, executor)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var attached dialer.AttachInboundCallInput
	select {
	case attached = <-executor.attachCh:
	case <-time.After(time.Second):
		t.Fatal("AttachInboundCall was not called")
	}
	if dialog.trying != 1 || dialog.ringing != 1 || dialog.answered != 1 {
		t.Fatalf("dialog state trying=%d ringing=%d answered=%d", dialog.trying, dialog.ringing, dialog.answered)
	}
	if attached.Session != session || attached.PhoneNumber != "+5511999999999" || attached.SIPTrunkID != "trunk-1" {
		t.Fatalf("unexpected attach input: %+v", attached)
	}
	if attached.Admission != admission.lease {
		t.Fatal("attach did not receive admission lease")
	}

	call.close()
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
}

func TestInboundCallUseCaseDeclineRejectsAndReleasesAdmission(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1", admission.releaseCalls)
	}
	if dialog.answered != 0 || dialog.hungup != 1 {
		t.Fatalf("dialog answered=%d hungup=%d, want answered=0 hungup=1", dialog.answered, dialog.hungup)
	}
}

func TestInboundCallUseCaseAttachErrorReleasesAdmissionAndHangsUp(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	executor := &inboundTestExecutor{
		call:      newInboundTestCall(),
		attachErr: errors.New("session busy"),
	}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, executor)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := <-done; err == nil {
		t.Fatal("expected error from AttachInboundCall failure")
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1 ( AttachInboundCall error must release lease )", admission.releaseCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 ( AttachInboundCall error must hang up dialog )", dialog.hungup)
	}
}

func TestInboundCallUseCaseAnswerErrorReleasesAdmissionAndHangsUp(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)

	dialog := &inboundTestFailingDialog{
		inboundTestDialog: inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"},
		answerErr:         errors.New("sip answer failed"),
	}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := <-done; err == nil {
		t.Fatal("expected error from Answer failure")
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1 (Answer error must release lease)", admission.releaseCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 (Answer error must hang up dialog)", dialog.hungup)
	}
}

func TestInboundCallUseCaseNotifyErrorReleasesAdmissionAndHangsUp(t *testing.T) {
	session := &inboundTestFailingSession{
		inboundTestSession: *newInboundTestSession("s-1", "u-1", "ws-1"),
		notifyErr:          errors.New("ws closed"),
	}
	admission := &inboundTestAdmission{}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	if err := <-done; err == nil {
		t.Fatal("expected error from Notify failure")
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1 (Notify error must release lease)", admission.releaseCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 (Notify error must hang up dialog)", dialog.hungup)
	}
}

func TestInboundCallUseCaseOfferTimeoutReleasesAdmissionAndHangsUp(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}

	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)
	uc.offerTTL = 50 * time.Millisecond
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	if err := <-done; !errors.Is(err, dialer.ErrInboundOfferNotFound) {
		t.Fatalf("expected ErrInboundOfferNotFound, got %v", err)
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1 (offer timeout must release lease)", admission.releaseCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 (offer timeout must hang up dialog)", dialog.hungup)
	}
}

func TestInboundCallUseCaseContextCancelDuringRetryReleasesAdmissionAndHangsUp(t *testing.T) {

	uc, _ := NewInboundCallUseCase(InboundCallUseCaseConfig{
		Sessions:  &inboundTestRegistry{available: []dialer.DialerSession{}},
		Admission: &inboundTestAdmission{},
		RetryWait: 30 * time.Second,
		OfferTTL:  time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}

	done := make(chan error, 1)
	go func() { done <- uc.HandleInboundInvite(ctx, inboundInvite(dialog)) }()

	time.Sleep(10 * time.Millisecond)
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 (ctx cancel must hang up dialog)", dialog.hungup)
	}
}

func TestInboundCallUseCaseNoExecutorReleasesAdmissionAndHangsUp(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}

	uc, _ := NewInboundCallUseCase(InboundCallUseCaseConfig{
		Sessions:  &inboundTestRegistry{available: []dialer.DialerSession{session}},
		Admission: admission,
		Executor:  &inboundTestExecutor{call: newInboundTestCall()},
		OfferTTL:  time.Second,
	})
	uc.SetExecutor(nil)

	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := <-done; !errors.Is(err, dialer.ErrInboundExecutorNotConfigured) {
		t.Fatalf("expected ErrInboundExecutorNotConfigured, got %v", err)
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("Release calls = %d, want 1 (nil executor must release lease)", admission.releaseCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1 (nil executor must hang up dialog)", dialog.hungup)
	}
}

func TestInboundCallUseCaseAcceptSuccessDoesNotReleaseLease(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	call := newInboundTestCall()
	executor := &inboundTestExecutor{call: call, attachCh: make(chan dialer.AttachInboundCallInput, 1)}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, executor)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	<-executor.attachCh
	if admission.releaseCalls != 0 {
		t.Fatalf("Release calls = %d, want 0 (success must transfer lease to lifecycle, not release)", admission.releaseCalls)
	}

	call.close()
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
}

func TestInboundCallUseCaseRejectsForeignTrunk(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	invite := inboundInvite(dialog)

	invite.Trunk.WorkspaceID = "ws-foreign"

	err := uc.HandleInboundInvite(context.Background(), invite)
	if !errors.Is(err, dialer.ErrInboundTrunkUnsupported) {
		t.Fatalf("HandleInboundInvite error = %v, want ErrInboundTrunkUnsupported", err)
	}
	if admission.acquireCalls != 0 {
		t.Fatalf("Acquire calls = %d, want 0", admission.acquireCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1", dialog.hungup)
	}
}

func TestInboundCallUseCaseRejectsGlobalTrunk(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, admission, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	invite := inboundInvite(dialog)

	invite.Trunk.IsGloballyVisible = true

	err := uc.HandleInboundInvite(context.Background(), invite)
	if !errors.Is(err, dialer.ErrInboundTrunkUnsupported) {
		t.Fatalf("HandleInboundInvite error = %v, want ErrInboundTrunkUnsupported", err)
	}
	if admission.acquireCalls != 0 {
		t.Fatalf("Acquire calls = %d, want 0", admission.acquireCalls)
	}
	if dialog.hungup != 1 {
		t.Fatalf("dialog hungup = %d, want 1", dialog.hungup)
	}
}

func TestInboundSelectAndReserveClaimsAgentAtMostOnce(t *testing.T) {
	// Two concurrent inbound legs contend for a single available agent: the first
	// claims it via the reservation CAS; the second sees no free agent and so can
	// never double-ring the same person.
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)

	if got := uc.selectAndReserve("ws-1", "offer-A"); got == nil || got.ID() != "s-1" {
		t.Fatalf("first selectAndReserve must claim the agent, got %v", got)
	}
	if got := uc.selectAndReserve("ws-1", "offer-B"); got != nil {
		t.Fatalf("second selectAndReserve must find no free agent (already ringing), got %v", got.ID())
	}
}

func TestInboundDeclineReleasesReservation(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	msg := waitInboundOffer(t, session)
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
	if session.reserved != "" {
		t.Fatalf("decline must release the reservation, still %q", session.reserved)
	}
}

func TestInboundOfferTimeoutReleasesReservation(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	uc.offerTTL = 40 * time.Millisecond
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	_ = waitInboundOffer(t, session)
	if err := <-done; !errors.Is(err, dialer.ErrInboundOfferNotFound) {
		t.Fatalf("expected ErrInboundOfferNotFound, got %v", err)
	}
	if session.reserved != "" {
		t.Fatalf("offer timeout must release the reservation, still %q", session.reserved)
	}
}

func TestInboundNotifyFailureReleasesReservation(t *testing.T) {
	session := &inboundTestFailingSession{
		inboundTestSession: *newInboundTestSession("s-1", "u-1", "ws-1"),
		notifyErr:          errors.New("ws closed"),
	}
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := runInboundInvite(t, uc, dialog)

	if err := <-done; err == nil {
		t.Fatal("expected error from Notify failure")
	}
	if session.reserved != "" {
		t.Fatalf("notify failure must release the reservation, still %q", session.reserved)
	}
}

func TestInboundContextCancelReleasesReservation(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- uc.HandleInboundInvite(ctx, inboundInvite(dialog)) }()

	_ = waitInboundOffer(t, session)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if session.reserved != "" {
		t.Fatalf("context cancel mid-ring must release the reservation, still %q", session.reserved)
	}
}

func newInboundTestUseCase(t *testing.T, registry dialer.DialerSessionRegistry, admission dialer.CallAdmissionCoordinator, executor dialer.DialerInboundExecutor) *InboundCallUseCase {
	t.Helper()
	uc, err := NewInboundCallUseCase(InboundCallUseCaseConfig{Sessions: registry, Admission: admission, Executor: executor, OfferTTL: time.Second})
	if err != nil {
		t.Fatalf("NewInboundCallUseCase: %v", err)
	}
	return uc
}

func runInboundInvite(t *testing.T, uc *InboundCallUseCase, dialog sip_trunk.InboundDialog) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- uc.HandleInboundInvite(context.Background(), inboundInvite(dialog)) }()
	return done
}

func waitInboundOffer(t *testing.T, session *inboundTestSession) dialer.DialerControlMessage {
	t.Helper()
	select {
	case msg := <-session.notifyCh:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound offer")
	}
	return dialer.DialerControlMessage{}
}

func inboundInvite(dialog sip_trunk.InboundDialog) sip_trunk.InboundInvite {
	return sip_trunk.InboundInvite{
		ID:          dialog.ID(),
		TrunkID:     "trunk-1",
		WorkspaceID: "ws-1",
		FromNumber:  dialog.FromUser(),
		ToNumber:    dialog.ToUser(),
		ReceivedAt:  time.Now(),
		Trunk: &sip_trunk.SIPTrunk{
			ID:          "trunk-1",
			WorkspaceID: "ws-1",
			Enabled:     true,
		},
		Dialog: dialog,
	}
}

type inboundTestReceptive struct {
	handled bool
	err     error
	calls   int
}

func (r *inboundTestReceptive) TryHandleInbound(_ context.Context, _ sip_trunk.InboundInvite) (bool, error) {
	r.calls++
	return r.handled, r.err
}

// When a receptive campaign handles the call, the human-roulette path must not
// run: no admission slot, no Trying/Ringing, no offer to a human session.
func TestInboundReceptiveHandlerTakesPrecedence(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	receptive := &inboundTestReceptive{handled: true}
	uc, err := NewInboundCallUseCase(InboundCallUseCaseConfig{
		Sessions:  &inboundTestRegistry{available: []dialer.DialerSession{session}},
		Admission: admission,
		Executor:  &inboundTestExecutor{call: newInboundTestCall()},
		Receptive: receptive,
		OfferTTL:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewInboundCallUseCase: %v", err)
	}
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}

	if herr := uc.HandleInboundInvite(context.Background(), inboundInvite(dialog)); herr != nil {
		t.Fatalf("HandleInboundInvite returned %v, want nil", herr)
	}
	if receptive.calls != 1 {
		t.Fatalf("receptive consulted %d times, want 1", receptive.calls)
	}
	if admission.acquireCalls != 0 {
		t.Fatalf("human path acquired admission %d times, want 0", admission.acquireCalls)
	}
	if dialog.trying != 0 || dialog.ringing != 0 {
		t.Fatalf("human signaling leaked: trying=%d ringing=%d, want 0/0", dialog.trying, dialog.ringing)
	}
	select {
	case <-session.notifyCh:
		t.Fatal("human session received an offer; receptive campaign should have owned the call")
	default:
	}
}

// When no receptive campaign applies (handled=false), the call falls through to
// the existing human-roulette path with normal Trying/Ringing and an offer.
func TestInboundReceptiveFallsBackToHumanWhenNotHandled(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	admission := &inboundTestAdmission{}
	receptive := &inboundTestReceptive{handled: false}
	uc, err := NewInboundCallUseCase(InboundCallUseCaseConfig{
		Sessions:  &inboundTestRegistry{available: []dialer.DialerSession{session}},
		Admission: admission,
		Receptive: receptive,
		OfferTTL:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewInboundCallUseCase: %v", err)
	}
	dialog := &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"}
	done := make(chan error, 1)
	go func() { done <- uc.HandleInboundInvite(context.Background(), inboundInvite(dialog)) }()

	msg := waitInboundOffer(t, session)
	if receptive.calls != 1 {
		t.Fatalf("receptive consulted %d times, want 1", receptive.calls)
	}
	if dialog.trying != 1 || dialog.ringing != 1 {
		t.Fatalf("human signaling not sent after fallback: trying=%d ringing=%d, want 1/1", dialog.trying, dialog.ringing)
	}
	offer := msg.Payload.(dialer.InboundCallOffer)
	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
}
