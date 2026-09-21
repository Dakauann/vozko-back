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
	MessageDirectionUnknown  MessageHistoryDirection = ""
)

func (d MessageHistoryDirection) Valid() bool {
	return d == MessageDirectionInbound || d == MessageDirectionOutbound
}

func (d MessageHistoryDirection) IsOutbound() bool {
	return d == MessageDirectionOutbound
}

type MessageHistoryRecord struct {
	EntryID     string
	EntryType   shared.EntryType
	Channel     MessageChannel
	MessageType MessageType

	ConversationID string

	MessageID          string
	ReplyToWAMessageID string
	From               string
	To                 string
	Text               string
	Timestamp          time.Time

	ProviderMessageID string

	MediaID   string
	MediaType MediaType
	MediaURL  string

	Metadata json.RawMessage

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
