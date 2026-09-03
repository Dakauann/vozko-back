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
		// Stated, not derived. Every producer already passes a direction to
		// Record and it was previously used only as a fallback for an invalid
		// message type — so a channel that named its content honestly (an
		// unofficial WhatsApp text is MessageTypeUserMessage whoever sent it)
		// silently lost the one fact that says which side of the thread it
		// belongs on. Persisting it here covers every channel at once.
		Direction: direction,
		From:      strings.TrimSpace(record.From),
		To:        strings.TrimSpace(record.To),
		Text:      strings.TrimSpace(record.Text),
		MediaID:   mediaID,
		MediaType: mediaType,
		Metadata:  record.Metadata,
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

	// A quoted reply, translated from the provider's id to ours.
	//
	// Every channel already fills ReplyToWAMessageID on the record, and this is
	// where it was being dropped: the field was read by nobody, so an INBOUND
	// quote never reached the database on ANY channel — seven days of traffic
	// held 1739 quotes and every one of them was outbound, written by the
	// operator send path that bypasses this manager.
	//
	// Translation is required, not cosmetic: the transcript resolves a quote by
	// matching reply_to_message_id against a message's OWN id, so storing the
	// provider's id would satisfy the column and still render nothing.
	if quoted := strings.TrimSpace(record.ReplyToWAMessageID); quoted != "" {
		if id := m.resolveQuotedMessageID(entryType, entryID, quoted); id != "" {
			message.ReplyToMessageID = &id
		}
	}

	message.Normalize()

	if err := message.Validate(); err != nil {
		return err
	}

	if dedupID == "" {
		return m.persist(message, entryID, entryType)
	}

	// Key the singleflight by entry as well as channel. Two entries can hold the
	// same provider id when both ends of the chat are accounts we host, and a
	// key without the entry would make the inbound copy wait on the outbound
	// one and then be dropped as its duplicate.
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

// resolveQuotedMessageID maps a provider's message id onto the row we hold for
// it, in this entry.
//
// Both columns are consulted because the channels disagree on which one they
// fill: official WhatsApp writes whatsapp_message_id, everything adapter-backed
// writes external_message_id. The entry-scoped lookup goes first — it is the
// precise one, and the same id can legitimately exist on another entry when both
// ends of a chat are hosted here.
//
// A miss is normal and silent: quoting a message older than our history, or one
// we never received, leaves the reply as an ordinary message rather than a
// dangling reference the transcript could not render anyway.
func (m *messageHistoryManager) resolveQuotedMessageID(entryType shared.EntryType, entryID, providerID string) string {
	if existing, err := m.repo.GetByEntryAndExternalMessageID(entryType, entryID, providerID); err == nil && existing != nil {
		return existing.ID
	}
	if existing, err := m.repo.GetByWhatsAppMessageID(providerID); err == nil && existing != nil {
		return existing.ID
	}
	return ""
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
