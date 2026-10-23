package conversation_usecase

import (
	"errors"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type StoredWhatsAppLookup interface {
	GetByWhatsAppMessageID(wamid string) (*conversation.Message, error)
}

type StoredInboundLookup interface {
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
}

func StoredWhatsAppMessageID(messages StoredWhatsAppLookup, wamid string) string {
	wamid = strings.TrimSpace(wamid)
	if messages == nil || wamid == "" {
		return ""
	}
	stored, err := messages.GetByWhatsAppMessageID(wamid)
	return storedRowID(storedRow(stored, err, wamid))
}

func StoredInbound(messages StoredInboundLookup, entryType shared.EntryType, entryID, providerMessageID string) *conversation.Message {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if messages == nil || providerMessageID == "" || strings.TrimSpace(entryID) == "" {
		return nil
	}
	stored, err := messages.GetByEntryAndExternalMessageID(entryType, entryID, providerMessageID)
	return storedRow(stored, err, providerMessageID)
}

func StoredInboundMessageID(messages StoredInboundLookup, entryType shared.EntryType, entryID, providerMessageID string) string {
	return storedRowID(StoredInbound(messages, entryType, entryID, providerMessageID))
}

func storedRow(stored *conversation.Message, err error, providerID string) *conversation.Message {
	if err != nil {
		if !errors.Is(err, conversation.ErrMessageNotFound) {
			log.Printf("[workflow-trigger] stored inbound message %s unreadable, the run carries no trigger message: %v", providerID, err)
		}
		return nil
	}
	return stored
}

func storedRowID(stored *conversation.Message) string {
	if stored == nil {
		return ""
	}
	return stored.ID
}
