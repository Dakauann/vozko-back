package conversation_usecase

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

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

func (m *messageHistoryManager) Record(_ context.Context, direction conversation.MessageHistoryDirection, record conversation.MessageHistoryRecord) error {
	if m == nil {
		return nil
	}

	timestamp := record.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	entryID := record.GetEntryID()

	entryType := record.EntryType
	if !entryType.Valid() {
		entryType = shared.EntryTypeWhatsApp
	}

	channel := record.Channel
	if !channel.Valid() {
		channel = conversation.MessageChannelWhatsApp
	}

	msgType := record.MessageType
	if !msgType.Valid() {
		if direction == conversation.MessageDirectionInbound {
			msgType = conversation.MessageTypeUserMessage
		} else {
			msgType = conversation.MessageTypeAIResponse
		}
	}

	var mediaID *string
	var mediaType conversation.MediaType
	if record.MediaID != "" {
		mediaID = &record.MediaID
		mediaType = record.MediaType
	}

	message := &conversation.Message{
		ID:          ensureMessageID(record.MessageID),
		EntryID:     strings.TrimSpace(entryID),
		EntryType:   entryType,
		Channel:     channel,
		MessageType: msgType,
		From:        strings.TrimSpace(record.From),
		To:          strings.TrimSpace(record.To),
		Text:        strings.TrimSpace(record.Text),
		MediaID:     mediaID,
		MediaType:   mediaType,
		Metadata:    record.Metadata,
		// Transient: carried to the broadcast, dropped by the repository. When
		// the producer knew the sender, the hub does no lookup at all.
		SenderName:   strings.TrimSpace(record.SenderName),
		SenderAvatar: strings.TrimSpace(record.SenderAvatar),
		CreatedAt:    timestamp,
		UpdatedAt:    timestamp,
	}

	// Provider message id. Channels that set ProviderMessageID (Instagram
	// onward) go to the generic external_message_id column; WhatsApp keeps
	// writing whatsapp_message_id from MessageID. Both share the same
	// singleflight + read-before-insert dedup below.
	providerID := strings.TrimSpace(record.ProviderMessageID)
	wamid := strings.TrimSpace(record.MessageID)

	dedupID := providerID
	if providerID != "" {
		message.ExternalMessageID = &providerID
	} else if wamid != "" {
		message.WhatsAppMessageID = &wamid
		dedupID = wamid
	}

	message.Normalize()

	if err := message.Validate(); err != nil {
		return err
	}

	if dedupID == "" {
		return m.persist(message, entryID, entryType)
	}

	// Key the singleflight by entry type too, so ids from different channels can
	// never collide in the in-flight map.
	_, err, _ := m.wamid.Do(string(entryType)+":"+dedupID, func() (interface{}, error) {
		var (
			existing *conversation.Message
			err      error
		)
		if providerID != "" {
			existing, err = m.repo.GetByExternalMessageID(entryType, providerID)
		} else {
			existing, err = m.repo.GetByWhatsAppMessageID(dedupID)
		}
		switch {
		case err == nil && existing != nil:
			log.Printf("[MessageHistoryManager] duplicate message ignored: id=%s entry=%s:%s", dedupID, entryType, entryID)
			return nil, nil
		case err != nil && !errors.Is(err, conversation.ErrMessageNotFound):
			log.Printf("[MessageHistoryManager] failed to check existing message by id %s: %v", dedupID, err)

		}
		return nil, m.persist(message, entryID, entryType)
	})
	return err
}

func (m *messageHistoryManager) persist(message *conversation.Message, entryID string, entryType shared.EntryType) error {
	if err := m.repo.Create(message); err != nil {
		return err
	}

	if m.hub != nil && entryID != "" {
		log.Printf("[MessageHistoryManager] Broadcasting message to entry %s:%s", entryType, entryID)
		m.hub.BroadcastNewMessage(entryID, string(entryType), message)
	}

	return nil
}

func ensureMessageID(_ string) string {
	return uuid.NewString()
}
