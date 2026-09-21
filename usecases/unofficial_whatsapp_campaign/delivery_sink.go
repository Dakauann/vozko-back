package unofficial_whatsapp_campaign

import (
	"log"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type DeliverySink struct {
	entries uwc.EntryRepository
}

func NewDeliverySink(entries uwc.EntryRepository) *DeliverySink {
	return &DeliverySink{entries: entries}
}

func (s *DeliverySink) AdvanceFromDelivery(providerMessageID string, status uw.DeliveryStatus) {
	if s == nil || s.entries == nil || providerMessageID == "" {
		return
	}

	var next campaign.SendStatus
	switch status {
	case uw.DeliverySent, uw.DeliveryQueued:
		next = campaign.SendStatusSent
	case uw.DeliveryDelivered:
		next = campaign.SendStatusDelivered
	case uw.DeliveryRead:
		next = campaign.SendStatusRead
	default:
		return
	}

	if err := s.entries.UpdateStatusByProviderMessageID(providerMessageID, next); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not advance delivery for %s: %v",
			providerMessageID, err)
	}
}
