package dialer_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/dialer"
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
	lease        *dialer.CallAdmissionLease
	acquireErr   error
	releaseCalls int
}

func (s *stubAdmission) Acquire(_ context.Context, _ dialer.CallAdmissionInput) (*dialer.CallAdmissionLease, error) {
	if s.acquireErr != nil {
		return nil, s.acquireErr
	}
	return s.lease, nil
}
func (s *stubAdmission) Refresh(_ *dialer.CallAdmissionLease, _ time.Duration) error { return nil }
func (s *stubAdmission) Release(_ *dialer.CallAdmissionLease) error {
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
	admission := &stubAdmission{lease: &dialer.CallAdmissionLease{
		WorkspaceID:         "ws-1",
		ReservedMicros:      50_000,
		PerMinuteCostMicros: 50_000,
		SlotAcquired:        true,
	}}
	callSource := &stubCallSource{call: call}
	uc := NewStartOutboundCallUseCase(callSource, nil, admission)

	res, err := uc.Execute(context.Background(), dialer.StartOutboundCallInput{
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
	if callSource.lastIn.EntryType != "sip" {
		t.Fatalf("EntryType = %q, want %q", callSource.lastIn.EntryType, "sip")
	}
}

func TestStartOutboundCallUseCaseRequiresAdmissionCoordinator(t *testing.T) {
	uc := NewStartOutboundCallUseCase(&stubCallSource{call: &stubCRMCall{}}, nil, nil)
	_, err := uc.Execute(context.Background(), dialer.StartOutboundCallInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		TargetPhone: "5511999999999",
	})
	if !errors.Is(err, dialer.ErrAdmissionDependenciesMissing) {
		t.Fatalf("Execute() error = %v, want ErrAdmissionDependenciesMissing", err)
	}
}

func TestStartOutboundCallUseCaseReleasesAdmissionOnDialFailure(t *testing.T) {
	admission := &stubAdmission{lease: &dialer.CallAdmissionLease{WorkspaceID: "ws-1"}}
	uc := NewStartOutboundCallUseCase(&stubCallSource{err: errors.New("dial failed")}, nil, admission)

	_, err := uc.Execute(context.Background(), dialer.StartOutboundCallInput{
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

	err := uc.Execute(context.Background(), dialer.EndOutboundCallInput{
		Call:             call,
		Admission:        &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
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
