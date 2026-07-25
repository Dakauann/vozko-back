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

	MediaID   string
	MediaType MediaType
	MediaURL  string

	Metadata json.RawMessage
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
