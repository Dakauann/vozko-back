package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type recordingAttendance struct {
	replies []aa.StartInput
	ids     []string
}

func (r *recordingAttendance) RecordAIReply(in aa.StartInput, messageID string) {
	r.replies = append(r.replies, in)
	r.ids = append(r.ids, messageID)
}

func continuationTurn() AgentContinuationTurn {
	return AgentContinuationTurn{
		WorkspaceID: "ws-1", EntryID: "conv-1", EntryType: shared.EntryTypeInstagram, AgentID: "agent-1",
		Note: `O fluxo "Matrícula" que você executou retornou agora.`,
	}
}

func continuationHistory() stubMessageRepo {
	return stubMessageRepo{newestFirst: []*conversation.Message{
		{MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByAI("agent-1"), Text: "Vou verificar sua matrícula."},
		{MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("ig-1"), Text: "meu CPF é 123"},
	}}
}

func readyToContinue(h *textRulesHarness) {
	h.svc.messages = continuationHistory()
	h.svc.SetEntryLeads(&entryLeadsStub{err: lead.ErrLeadNotFound})
}

func TestAChannelContinuationWithoutALeadStoreSendsNothing(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()

	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err == nil || len(h.sender.sent) != 0 {
		t.Fatalf("err = %v, sent = %v; without the lead its consent cannot be checked", err, h.sender.sent)
	}
}

func TestAContinuationTurnRepliesWithoutAnInboundMessageAndWithTheTriggerNote(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Sua matrícula foi concluída!")
	readyToContinue(h)
	h.svc.SetAssembler(newAssembledService(t).assembler)
	attendance := &recordingAttendance{}
	h.svc.SetAIAttendance(attendance)

	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err != nil {
		t.Fatal(err)
	}

	if len(h.sender.sent) != 1 || h.sender.sent[0] != "Sua matrícula foi concluída!" {
		t.Fatalf("sent = %v", h.sender.sent)
	}
	in := h.ai.inputs[0]
	for _, m := range in.Messages[:len(in.Messages)-1] {
		if m.Role == ai.RoleUser && strings.TrimSpace(m.Content) == "" {
			t.Fatalf("a continuation must not invent an empty customer message: %+v", in.Messages)
		}
	}
	if last := in.Messages[len(in.Messages)-2]; last.Content != "Vou verificar sua matrícula." {
		t.Fatalf("the history must end where the conversation is, got %+v", last)
	}
	if note := in.Messages[len(in.Messages)-1]; !strings.Contains(note.Content, "retornou agora") {
		t.Fatalf("note = %q", note.Content)
	}
	if len(attendance.replies) != 1 || attendance.replies[0].AgentID != "agent-1" || attendance.replies[0].EntryID != "conv-1" || attendance.ids[0] != "msg-1" {
		t.Fatalf("attendance = %+v %v, the turn must count as a normal AI reply", attendance.replies, attendance.ids)
	}
}

func TestAnInboundReplyIsAlsoRecordedAsAnAIReply(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Em 3 dias.")
	attendance := &recordingAttendance{}
	h.svc.SetAIAttendance(attendance)

	if _, err := h.svc.Reply(context.Background(), textRulesRequest()); err != nil {
		t.Fatal(err)
	}
	if len(attendance.replies) != 1 || attendance.replies[0].Channel != string(shared.EntryTypeInstagram) {
		t.Fatalf("attendance = %+v", attendance.replies)
	}
}

func TestTheChannelWindowDecidesWhetherAContinuationMaySend(t *testing.T) {
	for name, tc := range map[string]struct {
		adapter *fakeWindowAdapter
		open    bool
		fails   bool
	}{
		"open":       {&fakeWindowAdapter{entryType: shared.EntryTypeInstagram, open: true}, true, false},
		"closed":     {&fakeWindowAdapter{entryType: shared.EntryTypeInstagram}, false, false},
		"unreadable": {&fakeWindowAdapter{entryType: shared.EntryTypeInstagram, resolveErr: errors.New("api down")}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newTextRulesHarness(t, channel.Capabilities{})
			h.svc.adapters = conversation.NewAdapterRegistry(tc.adapter)
			open, err := h.svc.ContinuationWindowOpen(context.Background(), continuationTurn())
			if open != tc.open || (err != nil) != tc.fails {
				t.Fatalf("open = %v, err = %v", open, err)
			}
		})
	}
}

func TestAnInboundReplyWaitsForAContinuationOnTheSameConversation(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Em 3 dias.")
	state := newLockState()
	lock := NewAgentTurnLock(state)
	h.svc.SetAgentTurnLock(lock)
	release, _ := lock.Acquire(context.Background(), "conv-1", shared.EntryTypeInstagram)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = h.svc.Reply(ctx, textRulesRequest())
	release()

	if len(h.ai.inputs) != 0 {
		t.Fatal("the inbound reply generated while a continuation held the conversation")
	}
}

type entryLeadsStub struct {
	leadID  string
	err     error
	asked   []shared.EntryRef
	contact *lead.Lead
	readErr error
}

func (s *entryLeadsStub) LeadOfEntry(_ context.Context, workspaceID string, ref shared.EntryRef) (string, error) {
	s.asked = append(s.asked, ref)
	return s.leadID, s.err
}

func (s *entryLeadsStub) FindByID(workspaceID, id string) (*lead.Lead, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	if s.contact != nil {
		return s.contact, nil
	}
	return &lead.Lead{ID: id, WorkspaceID: workspaceID}, nil
}

func TestAChannelContinuationNeverWritesToALeadWhoOptedOut(t *testing.T) {
	optedOut := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	h := newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()
	h.svc.SetEntryLeads(&entryLeadsStub{leadID: "lead-1", contact: &lead.Lead{ID: "lead-1", OptedOutAt: &optedOut}})

	err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn())

	if reason, skipped := replySkipReason(err); !skipped || reason != "lead_opted_out" {
		t.Fatalf("err = %v, want the opted-out skip", err)
	}
	if len(h.sender.sent) != 0 || len(h.ai.inputs) != 0 {
		t.Fatalf("sent = %v, generated = %d", h.sender.sent, len(h.ai.inputs))
	}
}

func TestAChannelContinuationWhoseLeadCannotBeReadSendsNothing(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()
	h.svc.SetEntryLeads(&entryLeadsStub{leadID: "lead-1", readErr: errors.New("db down")})

	err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn())
	if err == nil || len(h.sender.sent) != 0 {
		t.Fatalf("err = %v, sent = %v; an unreadable consent must stop the turn", err, h.sender.sent)
	}
	if !errors.Is(err, conversation.ErrAgentTurnNotStarted) {
		t.Fatalf("err = %v; a failed read before the model ran may be retried", err)
	}
}

func TestAChannelContinuationWithoutALeadStoreIsNotRetried(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()

	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); conversation.TurnRetryable(err) {
		t.Fatalf("err = %v; a missing store is wiring, retrying cannot fix it", err)
	}
}

type disclosingAdapter struct {
	describedWindowAdapter
	disclosure string
	err        error
	asked      []string
}

func (a *disclosingAdapter) AutomationDisclosure(_ context.Context, ec *conversation.EntryContext) (string, error) {
	a.asked = append(a.asked, ec.EntryID)
	return a.disclosure, a.err
}

func withDisclosingAdapter(h *textRulesHarness, adapter *disclosingAdapter) {
	adapter.fakeWindowAdapter = fakeWindowAdapter{entryType: shared.EntryTypeInstagram, open: true}
	h.svc.adapters = conversation.NewAdapterRegistry(adapter)
}

func TestAChannelContinuationCarriesTheAutomationDisclosureLikeAnInboundReply(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Sua matrícula foi concluída!")
	readyToContinue(h)
	adapter := &disclosingAdapter{disclosure: "Você está falando com um assistente virtual."}
	withDisclosingAdapter(h, adapter)

	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err != nil {
		t.Fatal(err)
	}

	if len(adapter.asked) != 1 || adapter.asked[0] != "conv-1" {
		t.Fatalf("disclosure asked for %v", adapter.asked)
	}
	if len(h.sender.sent) != 1 || h.sender.sent[0] != "Você está falando com um assistente virtual.\n\nSua matrícula foi concluída!" {
		t.Fatalf("sent = %q", h.sender.sent)
	}
}

func TestAChannelContinuationWhoseDisclosureCannotBeReadSendsNothing(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Sua matrícula foi concluída!")
	readyToContinue(h)
	withDisclosingAdapter(h, &disclosingAdapter{err: errors.New("db down")})

	err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn())
	if err == nil || len(h.sender.sent) != 0 {
		t.Fatalf("err = %v, sent = %v; a reply without the configured disclosure must not go out", err, h.sender.sent)
	}
	if !errors.Is(err, conversation.ErrAgentTurnNotStarted) {
		t.Fatalf("err = %v; a failed read before the model ran may be retried", err)
	}
}

func TestAChannelContinuationThatProducesNothingIsASkipNotASend(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{})
	readyToContinue(h)

	err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn())

	if reason, skipped := replySkipReason(err); !skipped || reason != replySkipEmptyReply {
		t.Fatalf("err = %v, want the empty reply skip", err)
	}
	if _, err := h.svc.Reply(context.Background(), textRulesRequest()); err != nil {
		t.Fatalf("an inbound reply with nothing to say stays quiet without an error, got %v", err)
	}
}

func TestAContinuationOnAnotherChannelCarriesTheLeadMemory(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Sua matrícula foi concluída!")
	h.svc.messages = continuationHistory()
	h.svc.SetAssembler(newAssembledServiceWithMemories(t).assembler)
	leads := &entryLeadsStub{leadID: "lead-1"}
	h.svc.SetEntryLeads(leads)

	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err != nil {
		t.Fatal(err)
	}
	if len(leads.asked) != 1 || leads.asked[0].EntryID != "conv-1" || leads.asked[0].EntryType != shared.EntryTypeInstagram {
		t.Fatalf("asked = %+v", leads.asked)
	}
	if !strings.Contains(h.ai.inputs[0].SystemPrompt, "Prefere boleto a PIX.") {
		t.Fatalf("the continuation turn ran without the lead memory:\n%s", h.ai.inputs[0].SystemPrompt)
	}
}

func TestAContinuationWithoutALeadStillRepliesButAnUnreadableLeadStopsIt(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()
	h.svc.SetEntryLeads(&entryLeadsStub{err: lead.ErrLeadNotFound})
	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err != nil || len(h.sender.sent) != 1 {
		t.Fatalf("a conversation without a lead must still continue: err = %v, sent = %v", err, h.sender.sent)
	}

	h = newTextRulesHarness(t, channel.Capabilities{}, "Pronto!")
	h.svc.messages = continuationHistory()
	h.svc.SetEntryLeads(&entryLeadsStub{err: errors.New("db down")})
	if err := h.svc.ContinueAgentTurn(context.Background(), continuationTurn()); err == nil || len(h.sender.sent) != 0 {
		t.Fatalf("an unreadable lead must stop the turn: err = %v, sent = %v", err, h.sender.sent)
	}
}
