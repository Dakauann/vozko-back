package handlers

import "vozko/domain/conversation"

type EntryUpdateBroadcaster interface {
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}
