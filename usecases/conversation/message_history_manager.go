package conversation_usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type messageHistoryManager struct {
	repo  conversation.MessageRepository
	hub   conversation.EventBroadcaster
	wamid singleflight.Group
}

func NewMessageHistoryManager(repo conversation.MessageRepository) conversation.MessageHistoryManager {
	if repo == nil {
		return nil
	}
	return &messageHistoryManager{repo: repo}
}

func NewMessageHistoryManagerWithHub(repo conversation.MessageRepository, hub conversation.EventBroadcaster) conversation.MessageHistoryManager {
	if repo == nil {
		return nil
	}
	return &messageHistoryManager{repo: repo, hub: hub}
}

func (m *messageHistoryManager) Record(_ context.Context, record conversation.MessageHistoryRecord) error {
	if m == nil {
		return nil
	}

	entryID := record.GetEntryID()
	message := record.Message()
	message.ID = ensureMessageID(record.MessageID)
	entryType := message.EntryType

	providerID := strings.TrimSpace(record.ProviderMessageID)
	dedupID := providerID
	if providerID == "" {
		dedupID = strings.TrimSpace(record.MessageID)
	}

	if quoted := strings.TrimSpace(record.ReplyToWAMessageID); quoted != "" {
		if id := quotedMessageID(m.repo, entryType, entryID, quoted); id != "" {
			message.ReplyToMessageID = &id
		}
	}

	message.Normalize()

	if err := message.Validate(); err != nil {
		return err
	}

	if dedupID == "" {
		return m.persist(message, entryID, entryType, record.Silent)
	}

	_, err, _ := m.wamid.Do(string(entryType)+":"+entryID+":"+dedupID, func() (interface{}, error) {
		var (
			existing *conversation.Message
			err      error
		)
		if providerID != "" {
			existing, err = m.repo.GetByEntryAndExternalMessageID(entryType, entryID, providerID)
		} else {
			existing, err = m.repo.GetByWhatsAppMessageID(dedupID)
		}
		switch {
		case err == nil && existing != nil && message.SentBy.Claims(existing.SentBy):
			_, claimErr := m.repo.ClaimExternalEcho(message)
			return nil, claimErr
		case err == nil && existing != nil:
			log.Printf("[MessageHistoryManager] duplicate message ignored: id=%s entry=%s:%s", dedupID, entryType, entryID)
			return nil, nil
		case err != nil && !errors.Is(err, conversation.ErrMessageNotFound):
			log.Printf("[MessageHistoryManager] failed to check existing message by id %s: %v", dedupID, err)

		}
		return nil, m.persist(message, entryID, entryType, record.Silent)
	})
	return err
}

func quotedMessageID(repo conversation.MessageRepository, entryType shared.EntryType, entryID, providerID string) string {
	if repo == nil || strings.TrimSpace(providerID) == "" {
		return ""
	}
	if existing, err := repo.GetByEntryAndExternalMessageID(entryType, entryID, providerID); err == nil && existing != nil {
		return existing.ID
	}
	if existing, err := repo.GetByWhatsAppMessageID(providerID); err == nil && existing != nil {
		return existing.ID
	}
	return ""
}

func (m *messageHistoryManager) persist(message *conversation.Message, entryID string, entryType shared.EntryType, silent bool) error {
	if err := m.repo.Create(message); err != nil {
		return err
	}

	if m.hub != nil && entryID != "" && !silent {
		log.Printf("[MessageHistoryManager] Broadcasting message to entry %s:%s", entryType, entryID)
		m.hub.BroadcastNewMessage(entryID, string(entryType), message)
	}

	return nil
}

func ensureMessageID(_ string) string {
	return uuid.NewString()
}
