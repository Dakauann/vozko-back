package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

type lastMessageWindow struct {
	last *time.Time
	err  error
}

func (w lastMessageWindow) IsWindowOpen(string, string) (bool, error) { return true, nil }

func (w lastMessageWindow) GetLastMessageTime(string, string) (*time.Time, error) {
	return w.last, w.err
}

func repliedAt(offset time.Duration) *time.Time {
	at := optedOutLead().OptedOutAt.Add(offset)
	return &at
}

func replyingSender(window lastMessageWindow, client *countingWhatsAppClient) *whatsappSender {
	sender := guardedSender(optedOutLead(), client, &recordingHistory{})
	sender.deps.MessageWindowRepo = window
	return sender
}

func TestALeadWhoWritesAfterOptingOutGetsTheReply(t *testing.T) {
	for name, send := range everyWhatsAppSend(t) {
		t.Run(name, func(t *testing.T) {
			client := &countingWhatsAppClient{}
			if err := send(replyingSender(lastMessageWindow{last: repliedAt(time.Minute)}, client)); err != nil {
				t.Fatalf("a reply to a lead who wrote after opting out must go: %v", err)
			}
		})
	}
}

func TestTheOptOutMessageItselfGetsNoReply(t *testing.T) {
	for name, send := range everyWhatsAppSend(t) {
		t.Run(name, func(t *testing.T) {
			client := &countingWhatsAppClient{}
			err := send(replyingSender(lastMessageWindow{last: repliedAt(-time.Second)}, client))
			if !errors.Is(err, wo.ErrLeadOptedOut) || client.sends != 0 {
				t.Fatalf("the message that opted out is not answered: err %v sends %d", err, client.sends)
			}
		})
	}
}

func TestAnUnreadableLastMessageRefusesTheReply(t *testing.T) {
	client := &countingWhatsAppClient{}
	sender := replyingSender(lastMessageWindow{err: errors.New("db down")}, client)

	_, _, err := sender.SendText(context.Background(), waMediaRun(), "Oi", "ai_response")

	if !errors.Is(err, errRecipientRefused) || client.sends != 0 {
		t.Fatalf("err %v sends %d", err, client.sends)
	}
}

func TestATemplateStaysRefusedEvenAfterTheLeadWrites(t *testing.T) {
	sends := &recordingTemplateSends{}
	sender := senderWithLeads(&knownLeadRepo{byID: optedOutLead()}, sends, &recordingHistory{})
	sender.deps.MessageWindowRepo = lastMessageWindow{last: repliedAt(time.Minute)}

	_, _, err := sender.SendTemplate(context.Background(), waMediaRun(), "t1", "", nil, nil)

	if !errors.Is(err, errRecipientRefused) || len(sends.sends) != 0 {
		t.Fatalf("templates are what an opt-out stops: err %v sends %d", err, len(sends.sends))
	}
}

func TestCheckReplyAnswersBeforeAnyModelIsCalled(t *testing.T) {
	if err := replyingSender(lastMessageWindow{last: repliedAt(-time.Second)}, &countingWhatsAppClient{}).CheckReply(waMediaRun()); !failsTheStep(err) {
		t.Fatalf("the opt-out message must be refused up front, got %v", err)
	}
	if err := replyingSender(lastMessageWindow{last: repliedAt(time.Minute)}, &countingWhatsAppClient{}).CheckReply(waMediaRun()); err != nil {
		t.Fatalf("a later message may be answered, got %v", err)
	}
}

func TestTheAgentNodeDoesNotPayForAReplyItCannotSend(t *testing.T) {
	service := &catalogAgentAI{}
	sender := replyingSender(lastMessageWindow{last: repliedAt(-time.Second)}, &countingWhatsAppClient{})
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, sender, nil)
	state := workflow.NewRunState()
	ctx := agentNodeContext(&state, nil)
	ctx.Run = refusedRunAt("ai-1")

	_, err := executor.Execute(ctx)

	if !failsTheStep(err) || len(service.inputs) != 0 {
		t.Fatalf("err %v, model calls %d; the model must not be called for a reply that is refused", err, len(service.inputs))
	}
}
