package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

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

func TestOwnerReplyFromTheirPhoneMovesConversationToOngoing(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusNew,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusOngoing {
		t.Fatalf("status = %q, want ongoing; the queue still shows an answered conversation",
			repo.status)
	}
}

func TestCustomerMessageWithTheSameTypeStillOpensIt(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionInbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusNew {
		t.Fatalf("status = %q, want new; a finished conversation must reopen", repo.status)
	}
}

func TestOutboundDoesNotReopenAFinishedConversation(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusFinished {
		t.Errorf("status = %q, want it left finished", repo.status)
	}
}

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

func TestEveryRealAnswerClearsTheQueue(t *testing.T) {
	for _, msgType := range []conversation.MessageType{
		conversation.MessageTypeOperator,
		conversation.MessageTypeAIResponse,
		conversation.MessageTypeTemplate,
		conversation.MessageTypeUserMessage,
		conversation.MessageTypeMedia,
		conversation.MessageTypeAudio,
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

func TestUnstatedDirectionKeepsTheOldBehaviour(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusFinished,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionUnknown)
	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusNew {
		t.Errorf("status = %q, want new", repo.status)
	}

	repo = transitionWith(t, conversation.ConversationStatusNew,
		conversation.MessageTypeOperator, conversation.MessageDirectionUnknown)
	if conversation.ConversationStatus(repo.status) != conversation.ConversationStatusOngoing {
		t.Errorf("status = %q, want ongoing", repo.status)
	}
}

func TestOngoingConversationIsNotRewritten(t *testing.T) {
	repo := transitionWith(t, conversation.ConversationStatusOngoing,
		conversation.MessageTypeUserMessage, conversation.MessageDirectionOutbound)

	if len(repo.updated) != 0 {
		t.Errorf("wrote %v to an already-ongoing conversation", repo.updated)
	}
}
