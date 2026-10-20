package messagestate

import (
	"errors"
	"time"

	"vozko/domain/conversation"
)

var ErrNoStore = errors.New("messagestate: no message store to save the change")

type Store interface {
	Update(messageID string, message *conversation.Message) error
}

type Announcer interface {
	BroadcastMessageUpdated(entryID, entryType string, message *conversation.Message)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

type Writer struct {
	Messages  Store
	Announcer Announcer
}

func (w Writer) Save(message *conversation.Message) error {
	if w.Messages == nil {
		return ErrNoStore
	}
	if message == nil {
		return conversation.ErrMessageNotFound
	}
	if err := w.Messages.Update(message.ID, message); err != nil {
		return err
	}
	if w.Announcer != nil {
		entryType := string(message.EntryType)
		w.Announcer.BroadcastMessageUpdated(message.EntryID, entryType, message)
		w.Announcer.BroadcastEntryUpdate(message.EntryID, entryType, nil)
	}
	return nil
}

func Stamp(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
