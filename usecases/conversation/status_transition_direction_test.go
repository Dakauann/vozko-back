package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// What a message does to a conversation's status depends on WHO sent it, and
// until direction was stored the service asked the message TYPE instead.
//
// That answered wrongly for exactly one case, and it is the case a customer
// notices: the owner picks up their own phone and replies. The message is an
// ordinary text, the type says "inbound", and the conversation stayed in NEW —
// so the queue kept surfacing a conversation that had already been answered,
// and no amount of answering it from the phone would clear it.

// statusEntryRepo is the minimum of wce.Repository the status service touches:
// read a status, write a status.
type statusEntryRepo struct {
	wce.Repository
	status  string
	updated []string
}

func (r *statusEntryRepo) FindByID(string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: "entry-1", ConversationStatus: r.status}, nil
}

func (r *statusEntryRepo) UpdateConversationStatus(_ string, write wce.ConversationStatusWrite) error {
	r.updated = append(r.updated, write.Status)
	r.status = write.Status
	return nil
}

func transitionWith(
	t *testing.T,
	from conversation.ConversationStatus,
	msgType conversation.MessageType,
	direction conversation.MessageHistoryDirection,
) *statusEntryRepo {
	t.Helper()
	repo := &statusEntryRepo{status: string(from)}
	svc := NewConversationStatusService(repo)
	if err := svc.TransitionOnMessage("entry-1", string(shared.EntryTypeWhatsApp), msgType, direction); err != nil {
		t.Fatalf("TransitionOnMessage: %v", err)
	}
	return repo
}

// The bug: a reply typed on the owner's own WhatsApp app clears the queue.
func TestOwnerReplyFromTheirPhoneMovesConversationToOngoing(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusNew,
		// An ordinary text — what the provider actually sends for one.
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusOngoing {
		t.Fatalf("status = %q, want ongoing; the queue still shows an answered conversation",
			repo.status)
	}
}

// The customer writing must still open the conversation, which is the same
// message type as above and the reason type alone could never decide this.
func TestCustomerMessageWithTheSameTypeStillOpensIt(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionInbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusNew {
		t.Fatalf("status = %q, want new; a finished conversation must reopen", repo.status)
	}
}

// An outbound message does NOT reopen a finished conversation. Answering a
// closed thread from your phone is a parting word, not a reopening — only the
// customer coming back is.
func TestOutboundDoesNotReopenAFinishedConversation(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusFinished {
		t.Errorf("status = %q, want it left finished", repo.status)
	}
}

// Automation talking to itself must not mark a conversation handled. A workflow
// logging a tool call is outbound, but the customer is still waiting.
func TestToolAndSystemTrafficDoesNotClearTheQueue(t *testing.T) {
	for _, msgType := range []conversation.MessageType{
		conversation.MessageTypeToolCall,
		conversation.MessageTypeToolResult,
		conversation.MessageTypeSystem,
	} {
		t.Run(string(msgType), func(t *testing.T) {
			repo := transitionWith(t, conversation.ConversationStatusNew,
				msgType, conversation.MessageDirectionOutbound)
			if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusNew {
				t.Errorf("status = %q; %s marked a waiting conversation as handled",
					repo.status, msgType)
			}
		})
	}
}

// Every way of actually answering counts, whichever route it took.
func TestEveryRealAnswerClearsTheQueue(t *testing.T) {
	for _, msgType := range []conversation.MessageType{
		conversation.MessageTypeOperator,    // typed in the CRM
		conversation.MessageTypeAIResponse,  // the agent
		conversation.MessageTypeTemplate,    // a template send
		conversation.MessageTypeUserMessage, // typed on the owner's phone
		conversation.MessageTypeMedia,       // a photo sent from the phone
		conversation.MessageTypeAudio,       // a voice note from the phone
	} {
		t.Run(string(msgType), func(t *testing.T) {
			repo := transitionWith(t, conversation.ConversationStatusNew,
				msgType, conversation.MessageDirectionOutbound)
			if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusOngoing {
				t.Errorf("status = %q, want ongoing", repo.status)
			}
		})
	}
}

// Rows written before the column carry no direction, and must behave exactly as
// they did — otherwise this change rewrites the status of historical
// conversations on the next message they receive.
func TestUnstatedDirectionKeepsTheOldBehaviour(t *testing.T) {
	// Old reading: user_message is inbound, so a finished conversation reopens.
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionUnknown)
	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusNew {
		t.Errorf("status = %q, want new", repo.status)
	}

	// Old reading: operator is outbound, so a new conversation becomes ongoing.
	repo = transitionWith(t, conversation.ConversationStatusNew,
		conversation.MessageTypeOperator, conversation.MessageDirectionUnknown)
	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusOngoing {
		t.Errorf("status = %q, want ongoing", repo.status)
	}
}

// An already-ongoing conversation is left alone: applyStatus is a write, and
// rewriting the same value on every message would churn the entry's clocks and
// the timeline for nothing.
func TestOngoingConversationIsNotRewritten(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusOngoing,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if len(repo.updated) != 0 {
		t.Errorf("wrote %v to an already-ongoing conversation", repo.updated)
	}
}
