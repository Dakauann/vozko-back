package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/calls/recordings"
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/lead"
	calls_usecase "vozko/usecases/calls"
)

type recordedCallStarts struct {
	mu     sync.Mutex
	inputs []cdr.StartCallInput
}

func (r *recordedCallStarts) Execute(input cdr.StartCallInput) (*cdr.Call, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, input)
	return &cdr.Call{ID: "record-1", CallID: input.CallID}, nil
}

func (r *recordedCallStarts) snapshot() []cdr.StartCallInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]cdr.StartCallInput(nil), r.inputs...)
}

func TestStartCallCarriesTheLeadAndTheCallListItemToTheUseCase(t *testing.T) {
	call := newFakeCallSessionCRMCall("sip-out-lead")
	call.closeOnHangup = true
	h := newCallSessionTestHarness(t, call)
	c := h.dial(t)

	payload, _ := json.Marshal(map[string]string{
		"phone_number": "5584994409684", "trunk_id": "trunk-1", "lead_id": "lead-1", "call_list_item_id": "item-1",
		"entry_id": "ignored", "entry_type": "whatsapp", "request_id": "req-1",
	})
	if err := c.WriteJSON(WSIncomingMessage{Type: WSEventStartCall, Payload: payload}); err != nil {
		t.Fatalf("write start_call: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return len(h.startUC.inputs()) == 1 }) {
		t.Fatal("start_call never reached the use case")
	}
	got := h.startUC.inputs()[0]
	if got.LeadID != "lead-1" || got.CallListItemID != "item-1" || got.TrunkID != "trunk-1" || got.TargetPhone != "5584994409684" {
		t.Fatalf("use case input = %+v", got)
	}
	call.closeDoneOnce()
}

func TestTheLeadOfAStartedCallReachesItsCallRecord(t *testing.T) {
	call := newFakeCallSessionCRMCall("sip-out-record")
	call.closeOnHangup = true
	h := newCallSessionTestHarness(t, call)
	starts := &recordedCallStarts{}
	h.lifecycle.SetCDRStart(starts)
	h.startUC.linked = callsession_domain.StartOutboundCallResult{LeadID: "lead-1", TrunkID: "trunk-1"}

	c := h.dial(t)
	h.startCall(t, c)
	call.closeDoneOnce()

	if !waitFor(t, 2*time.Second, func() bool { return len(starts.snapshot()) == 1 }) {
		t.Fatal("no call record was started")
	}
	record := starts.snapshot()[0]
	if record.LeadID == nil || *record.LeadID != "lead-1" || record.TrunkID == nil || *record.TrunkID != "trunk-1" {
		t.Fatalf("call record = %+v, want the lead and the trunk", record)
	}
}

func TestBuildLiveCallKeepsTheLinksOfTheCall(t *testing.T) {
	lc := buildLiveCall(callAttachInput{
		Call: newFakeCallSessionCRMCall("sip-out-links"), LeadID: "lead-1", TrunkID: "trunk-1", CallListItemID: "item-1", Direction: cdr.DirectionOutbound,
	})
	if lc.leadID != "lead-1" || lc.trunkID != "trunk-1" || lc.callListItemID != "item-1" {
		t.Fatalf("live call = %+v", lc)
	}
}

func TestStartCallRefusalsAnswerWithTheirCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&lead.DialRefusal{Reason: lead.DialRefusedBlocked}, "lead_blocked"},
		{fmt.Errorf("dial: %w", &lead.DialRefusal{Reason: lead.DialRefusedBlocked}), "lead_blocked"},
		{&lead.DialRefusal{Reason: lead.DialRefusedNumberNotHeld}, "lead_not_dialable"},
		{&lead.DialRefusal{Reason: lead.DialRefusedOptedOut}, "lead_not_dialable"},
		{&lead.DialRefusal{Reason: lead.DialRefusedNotFound}, "lead_not_dialable"},
		{callsession_domain.ErrCallListItemNeedsLead, "missing_fields"},
		{callsession_domain.ErrCallListsNotConfigured, "not_configured"},
		{callsession_domain.ErrLeadDialTargetsNotConfigured, "not_configured"},
		{errors.New("database down"), "dial_failed"},
	}
	h := NewCallSessionWSHandler(&fakeStartUseCase{}, &fakeEndUseCase{}, nil, allowAllAuthorizer{}, log.Default(), noopWSMetricsRecorder{})
	for _, tc := range cases {
		var sent []*WSOutgoingMessage
		h.sendStartCallError(func(m *WSOutgoingMessage) { sent = append(sent, m) }, tc.err)
		if len(sent) != 1 {
			t.Fatalf("%v: sent %d messages", tc.err, len(sent))
		}
		payload, ok := sent[0].Payload.(ErrorPayload)
		if !ok || sent[0].Type != WSEventError || payload.Code != tc.code {
			t.Errorf("%v: sent %+v, want code %q", tc.err, sent[0], tc.code)
		}
	}
}

type unknownLeads struct{}

func (unknownLeads) CheckLead(context.Context, callsession_domain.LeadDial) error {
	return &lead.DialRefusal{Reason: lead.DialRefusedNotFound}
}

func (unknownLeads) IdentityLead(context.Context, string, string) (string, error) { return "", nil }

type capturedRecordingEvents struct {
	events chan recordings.RecordingUploadEvent
}

func (c capturedRecordingEvents) Publish(topic string, message []byte) error {
	if topic != recordings.TopicRecordingUpload {
		return nil
	}
	var event recordings.RecordingUploadEvent
	if err := json.Unmarshal(message, &event); err != nil {
		return err
	}
	c.events <- event
	return nil
}

func (c capturedRecordingEvents) PublishWithDelay(topic string, message []byte, _ time.Duration) error {
	return c.Publish(topic, message)
}

func (capturedRecordingEvents) ValidateConnection() error { return nil }

type discardedRecordings struct{}

func (discardedRecordings) UploadFile(string, []byte, string) error { return nil }
func (discardedRecordings) GetFileURL(key string) string            { return "https://r2.example/" + key }

func TestTheLeadOfACallReachesItsRecording(t *testing.T) {
	published := capturedRecordingEvents{events: make(chan recordings.RecordingUploadEvent, 1)}
	pool := calls_usecase.NewRecordingUploadPool(published, discardedRecordings{}, t.TempDir(), 1, log.Default())
	defer pool.Shutdown()

	call := newFakeCallSessionCRMCall("sip-in-recorded")
	lc := buildLiveCall(callAttachInput{
		Call: call, RecordingPool: pool, WorkspaceID: "ws-1", LeadID: "lead-1", Direction: cdr.DirectionInbound,
	})
	for i := 0; i < 10; i++ {
		_ = lc.call.SendAudio(make([]byte, 320))
	}
	call.closeDoneOnce()

	select {
	case event := <-published.events:
		if event.LeadID != "lead-1" || event.WorkspaceID != "ws-1" || event.CallID != "sip-in-recorded" || event.EntryID != "" {
			t.Fatalf("recording event = %+v, want the lead of the call and no entry", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the recording was never published")
	}
}
