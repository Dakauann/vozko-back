package conversation

import (
	"context"
	"encoding/json"
	"time"

	"vozko/domain/shared"
)

type MessageHistoryDirection string

const (
	MessageDirectionInbound  MessageHistoryDirection = "INBOUND"
	MessageDirectionOutbound MessageHistoryDirection = "OUTBOUND"
)

type MessageHistoryRecord struct {
	EntryID     string
	EntryType   shared.EntryType
	Channel     MessageChannel
	MessageType MessageType

	// TODO: Remove after all usecases are migrated
	ConversationID string

	MessageID          string
	ReplyToWAMessageID string
	From               string
	To                 string
	Text               string
	Timestamp          time.Time

	// ProviderMessageID is the channel-agnostic provider id. When set it is
	// written to conversation_messages.external_message_id and used as the dedup
	// key, instead of the WhatsApp-specific column. Channels added from Instagram
	// onward set this; WhatsApp keeps using MessageID.
	ProviderMessageID string

	MediaID   string
	MediaType MediaType
	MediaURL  string

	Metadata json.RawMessage

	// SenderName and SenderAvatar are the display identity for the live
	// broadcast, and are never persisted, a message row that froze the name it
	// was sent under would show a stale name forever after a rename.
	//
	// Inbound handlers set them because they have just loaded the contact or
	// lead anyway, which makes the broadcast free. Leaving them empty is valid:
	// the hub resolves the identity itself, at the cost of a lookup.
	SenderName   string
	SenderAvatar string
}

func (r MessageHistoryRecord) GetEntryID() string {
	if r.EntryID != "" {
		return r.EntryID
	}
	return r.ConversationID
}

type MessageHistoryManager interface {
	Record(ctx context.Context, direction MessageHistoryDirection, record MessageHistoryRecord) error
}
