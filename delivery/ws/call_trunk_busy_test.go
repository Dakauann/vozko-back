package ws

import (
	"testing"
	"time"

	dialer_domain "vozko/domain/dialer"
)

func TestBuildCallTrunkBusyMessage_NilReturnsNil(t *testing.T) {
	if got := buildCallTrunkBusyMessage(nil, callTrunkBusyContext{}); got != nil {
		t.Fatalf("buildCallTrunkBusyMessage(nil) = %v, want nil", got)
	}
}

func TestBuildCallTrunkBusyMessage_PopulatesAllFields(t *testing.T) {
	busy := &dialer_domain.ErrTrunkBusy{
		TrunkID:    "trunk-1",
		Reason:     dialer_domain.TrunkBusyReasonReconciling,
		RetryAfter: 2 * time.Second,
	}
	ctx := callTrunkBusyContext{
		EntryID:     "entry-1",
		EntryType:   "sip",
		PhoneNumber: "+5511999999999",
		RequestID:   "req-1",
	}

	msg := buildCallTrunkBusyMessage(busy, ctx)
	if msg == nil {
		t.Fatal("buildCallTrunkBusyMessage = nil")
	}
	if msg.Type != WSEventCallTrunkBusy {
		t.Fatalf("Type = %q, want %q", msg.Type, WSEventCallTrunkBusy)
	}
	payload, ok := msg.Payload.(CallTrunkBusyPayload)
	if !ok {
		t.Fatalf("Payload is %T, want CallTrunkBusyPayload", msg.Payload)
	}
	if payload.TrunkID != "trunk-1" {
		t.Errorf("TrunkID = %q, want trunk-1", payload.TrunkID)
	}
	if payload.RetryAfterMs != 2000 {
		t.Errorf("RetryAfterMs = %d, want 2000", payload.RetryAfterMs)
	}
	if payload.Reason != dialer_domain.TrunkBusyReasonReconciling {
		t.Errorf("Reason = %q, want %q", payload.Reason, dialer_domain.TrunkBusyReasonReconciling)
	}
	if payload.EntryID != "entry-1" || payload.EntryType != "sip" {
		t.Errorf("UI context not populated: %+v", payload)
	}
	if payload.Phone != "+5511999999999" || payload.RequestID != "req-1" {
		t.Errorf("UI context not populated: %+v", payload)
	}
}

func TestBuildCallTrunkBusyMessage_DefaultsZeroRetry(t *testing.T) {
	busy := &dialer_domain.ErrTrunkBusy{
		TrunkID:    "trunk-1",
		Reason:     dialer_domain.TrunkBusyReasonReconciling,
		RetryAfter: 0,
	}
	msg := buildCallTrunkBusyMessage(busy, callTrunkBusyContext{})
	payload := msg.Payload.(CallTrunkBusyPayload)
	if payload.RetryAfterMs != 3000 {
		t.Fatalf("expected default 3000ms, got %d", payload.RetryAfterMs)
	}
}

func TestBuildCallTrunkBusyMessage_DefaultsEmptyReason(t *testing.T) {
	busy := &dialer_domain.ErrTrunkBusy{
		TrunkID:    "trunk-1",
		RetryAfter: time.Second,
	}
	msg := buildCallTrunkBusyMessage(busy, callTrunkBusyContext{})
	payload := msg.Payload.(CallTrunkBusyPayload)
	if payload.Reason != dialer_domain.TrunkBusyReasonNotOwned {
		t.Fatalf("Reason = %q, want default %q", payload.Reason, dialer_domain.TrunkBusyReasonNotOwned)
	}
}

func TestBuildCallTrunkBusyMessage_ZeroContextLeavesUIFieldsEmpty(t *testing.T) {
	busy := &dialer_domain.ErrTrunkBusy{
		TrunkID:    "trunk-1",
		Reason:     dialer_domain.TrunkBusyReasonRegistering,
		RetryAfter: time.Second,
	}
	msg := buildCallTrunkBusyMessage(busy, callTrunkBusyContext{})
	payload := msg.Payload.(CallTrunkBusyPayload)
	if payload.EntryID != "" || payload.EntryType != "" || payload.Phone != "" || payload.RequestID != "" {
		t.Fatalf("UI fields must be empty: %+v", payload)
	}
}
