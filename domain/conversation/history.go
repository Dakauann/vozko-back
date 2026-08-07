package conversation

import (
	"context"
	"encoding/json"
	"time"

	"vozko/domain/shared"
)

// MessageHistoryDirection is who a message came from: the contact, or us.
//
// It is a fact about the message that no other field carries. It is emphatically
// NOT the message TYPE — the provider's own schema is explicit that its
// messageType is the "tipo de conteúdo da mensagem", the kind of content, while
// its fromMe flag is what says who sent it. Deriving one from the other forces a
// channel to choose which of the two truths to keep, and every channel that
// tried chose differently:
//
//   - Telegram and Instagram store an operator's reply as MessageTypeOperator
//     whatever it contained, so direction survives and the content type is lost:
//     a photo sent from the Instagram app reads back as a plain operator note.
//   - Unofficial WhatsApp keeps the honest content type, so an owner answering
//     on their own phone was stored as MessageTypeUserMessage — indistinguishable
//     from the customer writing, and rendered as exactly that.
//
// Persisting it removes the choice. A channel states both, and neither is
// inferred from the other again.
type MessageHistoryDirection string

const (
	MessageDirectionInbound  MessageHistoryDirection = "INBOUND"
	MessageDirectionOutbound MessageHistoryDirection = "OUTBOUND"
	// MessageDirectionUnknown is the zero value, and it means "this row predates
	// the column" — not "neither". Readers fall back to the old type-based
	// inference for it rather than guessing, so a backfill that misses a row
	// degrades to previous behaviour instead of flipping a message to the wrong
	// side of the thread.
	MessageDirectionUnknown MessageHistoryDirection = ""
)

// Valid reports whether a direction was actually stated.
func (d MessageHistoryDirection) Valid() bool {
	return d == MessageDirectionInbound || d == MessageDirectionOutbound
}

// IsOutbound reports whether we sent this, by any route: an operator in the CRM,
// an agent, a workflow, or the owner typing on their own phone.
func (d MessageHistoryDirection) IsOutbound() bool {
	return d == MessageDirectionOutbound
}

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
