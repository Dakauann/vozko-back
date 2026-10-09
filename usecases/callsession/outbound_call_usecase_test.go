package callsession_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/conversation"
)

type stubCallSource struct {
	call   conversation.CRMCall
	err    error
	lastIn conversation.CallDialInput
}

func (s *stubCallSource) Dial(_ context.Context, input conversation.CallDialInput) (conversation.CRMCall, error) {
	s.lastIn = input
	if s.err != nil {
		return nil, s.err
	}
	return s.call, nil
}

func (s *stubCallSource) Name() string { return "stub" }

type stubAdmission struct {
	lease        *callsession.CallAdmissionLease
	acquireErr   error
	releaseCalls int
}

func (s *stubAdmission) Acquire(_ context.Context, _ callsession.CallAdmissionInput) (*callsession.CallAdmissionLease, error) {
	if s.acquireErr != nil {
		return nil, s.acquireErr
	}
	return s.lease, nil
}
func (s *stubAdmission) Refresh(_ *callsession.CallAdmissionLease, _ time.Duration) error { return nil }
func (s *stubAdmission) Release(_ *callsession.CallAdmissionLease) error {
	s.releaseCalls++
	return nil
}

type stubCRMCall struct {
	hangupCalls int
}

func (c *stubCRMCall) ID() string                 { return "call-1" }
func (c *stubCRMCall) SendAudio([]byte) error     { return nil }
func (c *stubCRMCall) AudioStream() <-chan []byte { return make(chan []byte) }
func (c *stubCRMCall) Events() <-chan conversation.CallEvent {
	return make(chan conversation.CallEvent)
}
func (c *stubCRMCall) Hangup() error {
	c.hangupCalls++
	return nil
}
func (c *stubCRMCall) Done() <-chan struct{} { return make(chan struct{}) }

func TestStartOutboundCallUseCaseSuccessWithTargetPhone(t *testing.T) {
	call := &stubCRMCall{}
	admission := &stubAdmission{lease: &callsession.CallAdmissionLease{
		WorkspaceID:         "ws-1",
		ReservedMicros:      50_000,
		PerMinuteCostMicros: 50_000,
		SlotAcquired:        true,
	}}
	callSource := &stubCallSource{call: call}
	uc := NewStartOutboundCallUseCase(callSource, admission, &stubDialTargets{}, nil)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: "(11) 99999-0000",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if res == nil || res.Call == nil {
		t.Fatal("expected started call result")
	}
	if res.ReservedMicros != 50_000 {
		t.Fatalf("ReservedMicros = %d, want 50000", res.ReservedMicros)
	}
	if callSource.lastIn.PhoneNumber == "" {
		t.Fatal("expected dial input to contain normalized phone")
	}
}

func TestStartOutboundCallUseCaseRequiresAdmissionCoordinator(t *testing.T) {
	uc := NewStartOutboundCallUseCase(&stubCallSource{call: &stubCRMCall{}}, nil, &stubDialTargets{}, nil)
	_, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: "5511999999999",
	})
	if !errors.Is(err, callsession.ErrAdmissionDependenciesMissing) {
		t.Fatalf("Execute() error = %v, want ErrAdmissionDependenciesMissing", err)
	}
}

func TestStartOutboundCallUseCaseReleasesAdmissionOnDialFailure(t *testing.T) {
	admission := &stubAdmission{lease: &callsession.CallAdmissionLease{WorkspaceID: "ws-1"}}
	uc := NewStartOutboundCallUseCase(&stubCallSource{err: errors.New("dial failed")}, admission, &stubDialTargets{}, nil)

	_, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: "5511999999999",
	})
	if err == nil {
		t.Fatal("expected dial failure error")
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("releaseCalls = %d, want 1", admission.releaseCalls)
	}
}

func TestEndOutboundCallUseCaseHangupAndRelease(t *testing.T) {
	admission := &stubAdmission{}
	call := &stubCRMCall{}
	uc := NewEndOutboundCallUseCase(admission)

	err := uc.Execute(context.Background(), callsession.EndOutboundCallInput{
		Call:             call,
		Admission:        &callsession.CallAdmissionLease{WorkspaceID: "ws-1"},
		Hangup:           true,
		ReleaseAdmission: true,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if call.hangupCalls != 1 {
		t.Fatalf("hangupCalls = %d, want 1", call.hangupCalls)
	}
	if admission.releaseCalls != 1 {
		t.Fatalf("releaseCalls = %d, want 1", admission.releaseCalls)
	}
}

type channelRecordingAdmission struct {
	stubAdmission
	channel string
}

func (s *channelRecordingAdmission) Acquire(ctx context.Context, input callsession.CallAdmissionInput) (*callsession.CallAdmissionLease, error) {
	s.channel = input.CallChannel
	return s.stubAdmission.Acquire(ctx, input)
}

func TestTrunkCallsKeepTheTypedNumberAndArePricedAsSIP(t *testing.T) {
	admission := &channelRecordingAdmission{stubAdmission: stubAdmission{lease: &callsession.CallAdmissionLease{WorkspaceID: "ws-1"}}}
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, admission, &stubDialTargets{}, nil)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: " *100# ",
		TrunkID:     "trunk-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if admission.channel != "sip" {
		t.Fatalf("admission channel = %q, want sip", admission.channel)
	}
	if source.lastIn.TrunkID != "trunk-1" || source.lastIn.PhoneNumber != "*100#" || res.PhoneNumber != "*100#" {
		t.Fatalf("dial input = %+v, want the trunk and the typed dial string untouched", source.lastIn)
	}
}

func TestTrunkCallsDialAndReportTheNumberAnatelDefines(t *testing.T) {
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, &stubAdmission{lease: &callsession.CallAdmissionLease{WorkspaceID: "ws-1"}}, &stubDialTargets{}, nil)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: "558494409684",
		TrunkID:     "trunk-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if source.lastIn.PhoneNumber != "5584994409684" || res.PhoneNumber != "5584994409684" {
		t.Fatalf("dialed %q and reported %q, want the ninth digit restored", source.lastIn.PhoneNumber, res.PhoneNumber)
	}
}

func TestTrunkCallsRequireANumber(t *testing.T) {
	uc := NewStartOutboundCallUseCase(&stubCallSource{}, &stubAdmission{lease: &callsession.CallAdmissionLease{}}, &stubDialTargets{}, nil)
	_, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{WorkspaceID: "ws-1", UserID: "u", TrunkID: "trunk-1"})
	if !errors.Is(err, callsession.ErrTargetPhoneRequired) {
		t.Fatalf("Execute() error = %v, want ErrTargetPhoneRequired", err)
	}
}
