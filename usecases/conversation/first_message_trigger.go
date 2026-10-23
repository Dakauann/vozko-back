package conversation_usecase

import (
	"log"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func IsFirstInbound(counter InboundCounter, event workflow.TriggerEvent) bool {
	if counter == nil {
		return false
	}
	count, err := counter.CountInboundByEntry(event.EntryID, shared.EntryType(event.EntryType))
	if err != nil {
		log.Printf("[inbound-automation] could not count inbound messages for %s %s: %v", event.EntryType, event.EntryID, err)
		return false
	}
	return count == 1
}
