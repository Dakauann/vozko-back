package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// fakeAdapter is a ChannelAdapter whose window and send outcome are dictated by
// the test.
type fakeAdapter struct {
	entryType  shared.EntryType
	windowOpen bool
	sendErr    error
	sentBody   string
	sendCalls  int
}

func (a *fakeAdapter) EntryType() shared.EntryType { return a.entryType }

func (a *fakeAdapter) ResolveEntry(_ context.Context, entryID string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{
		EntryID:    entryID,
		EntryType:  a.entryType,
		AccountID:  "acct-1",
		ContactRef: "contact-ref",
	}, nil
}

func (a *fakeAdapter) WindowState(_ context.Context, _ *conversation.EntryContext) (bool, *time.Time, error) {
	return a.windowOpen, nil, nil
}

func (a *fakeAdapter) SendText(_ context.Context, _ *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	a.sendCalls++
	if a.sendErr != nil {
		return nil, a.sendErr
	}
	a.sentBody = req.Body
	return &conversation.SendOutcome{ProviderMessageID: "provider-1"}, nil
}

func (a *fakeAdapter) SendMedia(_ context.Context, _ *conversation.EntryContext, _ conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

// recordingHistory captures what the sender persisted.
type recordingHistory struct {
	records []conversation.MessageHistoryRecord
	err     error
}

func (h *recordingHistory) Record(_ context.Context, _ conversation.MessageHistoryDirection, rec conversation.MessageHistoryRecord) error {
	if h.err != nil {
		return h.err
	}
	h.records = append(h.records, rec)
	return nil
}

func igRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID:          "run-1",
		EntryID:     "conv-1",
		EntryType:   string(shared.EntryTypeInstagram),
		WorkspaceID: "ws-1",
	}
}

func TestChannelSenderSendsThroughAdapter(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	history := &recordingHistory{}
	sender := &channelSender{
		adapters: conversation.NewAdapterRegistry(adapter),
		history:  history,
	}

	sent, err := sender.SendText(context.Background(), igRun(), "olá!", conversation.MessageTypeAIResponse)
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if sent == nil {
		t.Fatal("expected a send result")
	}
	if adapter.sentBody != "olá!" {
		t.Errorf("adapter received %q", adapter.sentBody)
	}
	if sent.ProviderMessageID != "provider-1" || sent.AccountID != "acct-1" {
		t.Errorf("unexpected result: %+v", sent)
	}

	// The transcript must be written through the shared history manager, with the
	// run's own channel, otherwise the message is delivered but invisible.
	if len(history.records) != 1 {
		t.Fatalf("expected one history record, got %d", len(history.records))
	}
	rec := history.records[0]
	if rec.EntryType != shared.EntryTypeInstagram || rec.Channel != conversation.MessageChannel("instagram") {
		t.Errorf("record has the wrong channel: %+v", rec)
	}
	if rec.ProviderMessageID != "provider-1" || rec.Text != "olá!" {
		t.Errorf("record content wrong: %+v", rec)
	}
}

// A closed window is normal on Instagram, not an error: the workflow must
// continue with sent=false rather than failing the run.
func TestChannelSenderWithheldWhenWindowClosed(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: false}
	history := &recordingHistory{}
	sender := &channelSender{
		adapters: conversation.NewAdapterRegistry(adapter),
		history:  history,
	}

	sent, err := sender.SendText(context.Background(), igRun(), "olá!", conversation.MessageTypeAIResponse)
	if err != nil {
		t.Fatalf("a closed window must not be an error, got %v", err)
	}
	if sent != nil {
		t.Errorf("nothing should have been sent, got %+v", sent)
	}
	if adapter.sendCalls != 0 {
		t.Error("the provider must not be called when the window is closed")
	}
	if len(history.records) != 0 {
		t.Error("nothing may be recorded when nothing was sent")
	}
}

func TestChannelSenderPropagatesProviderFailure(t *testing.T) {
	adapter := &fakeAdapter{
		entryType:  shared.EntryTypeInstagram,
		windowOpen: true,
		sendErr:    errors.New("meta rejected the message"),
	}
	history := &recordingHistory{}
	sender := &channelSender{
		adapters: conversation.NewAdapterRegistry(adapter),
		history:  history,
	}

	if _, err := sender.SendText(context.Background(), igRun(), "olá!", conversation.MessageTypeAIResponse); err == nil {
		t.Error("a provider failure must surface, not be swallowed")
	}
	if len(history.records) != 0 {
		t.Error("a failed send must not be recorded as delivered")
	}
}

// WhatsApp must never be served by the adapter path: it keeps its dedicated
// sender, which resolves lead numbers, phone selection and the lead window.
func TestChannelSenderKeepsWhatsAppOnItsOwnPath(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	sender := &channelSender{adapters: conversation.NewAdapterRegistry(adapter)}

	waRun := &workflow.WorkflowRun{ID: "run-2", EntryID: "wa-1", EntryType: string(shared.EntryTypeWhatsApp)}

	if sender.Supports(waRun) {
		t.Error("without a WhatsApp sender, a WhatsApp run must not be supported")
	}
	sent, err := sender.SendText(context.Background(), waRun, "hi", conversation.MessageTypeAIResponse)
	if err != nil || sent != nil {
		t.Errorf("WhatsApp must not fall through to an adapter (sent=%v err=%v)", sent, err)
	}
	if adapter.sendCalls != 0 {
		t.Error("the Instagram adapter was called for a WhatsApp run")
	}
}

func TestChannelSenderSupports(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	sender := &channelSender{adapters: conversation.NewAdapterRegistry(adapter)}

	if !sender.Supports(igRun()) {
		t.Error("a registered channel must be supported")
	}
	// A channel with no adapter, the state before a channel is wired, must
	// report unsupported so nodes skip instead of pretending to send.
	unknown := &workflow.WorkflowRun{ID: "r", EntryID: "e", EntryType: "telegram"}
	if sender.Supports(unknown) {
		t.Error("an unregistered channel must not be supported")
	}
	// Nil-safety: executors call this before every send.
	if (*channelSender)(nil).Supports(igRun()) {
		t.Error("a nil sender supports nothing")
	}
	if sender.Supports(nil) {
		t.Error("a nil run supports nothing")
	}
}

// A history failure must fail the node: the message went out, and silently
// losing it from the transcript would leave the operator blind.
func TestChannelSenderFailsWhenTranscriptCannotBeWritten(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	sender := &channelSender{
		adapters: conversation.NewAdapterRegistry(adapter),
		history:  &recordingHistory{err: errors.New("db down")},
	}

	if _, err := sender.SendText(context.Background(), igRun(), "olá!", conversation.MessageTypeAIResponse); err == nil {
		t.Error("expected the history failure to surface")
	}
}
