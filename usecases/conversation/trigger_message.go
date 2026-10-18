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
	return storedRowID(stored, err, wamid)
}

func StoredInboundMessageID(messages StoredInboundLookup, entryType shared.EntryType, entryID, providerMessageID string) string {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if messages == nil || providerMessageID == "" || strings.TrimSpace(entryID) == "" {
		return ""
	}
	stored, err := messages.GetByEntryAndExternalMessageID(entryType, entryID, providerMessageID)
	return storedRowID(stored, err, providerMessageID)
}

func storedRowID(stored *conversation.Message, err error, providerID string) string {
	if err != nil {
		if !errors.Is(err, conversation.ErrMessageNotFound) {
			log.Printf("[workflow-trigger] stored inbound message %s unreadable, the run carries no trigger message: %v", providerID, err)
		}
		return ""
	}
	if stored == nil {
		return ""
	}
	return stored.ID
}
