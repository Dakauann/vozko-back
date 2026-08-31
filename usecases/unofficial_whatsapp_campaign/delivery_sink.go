package unofficial_whatsapp_campaign

import (
	"log"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// DeliverySink advances a campaign target when WhatsApp reports a delivery.
//
// It is what makes the entries table say DELIVERED and READ rather than sitting
// on SENT forever. The provider's callbacks carry only a message id, which is
// why the entry stores one.
type DeliverySink struct {
	entries uwc.EntryRepository
}

func NewDeliverySink(entries uwc.EntryRepository) *DeliverySink {
	return &DeliverySink{entries: entries}
}

// AdvanceFromDelivery maps the channel's delivery state onto a send status.
//
// Only the three lifecycle states advance an entry. A provider "failed" is
// deliberately NOT written here: by the time a receipt says failed the message
// has already left and been charged for elsewhere, and the send path has its own
// far richer failure handling. Letting a receipt overwrite that would replace a
// specific reason with a generic one.
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

	// The repository refuses a backwards move, because WhatsApp's callbacks are
	// not ordered and a late DELIVERED arriving after a READ would otherwise
	// make the entries table disagree with the transcript on screen.
	if err := s.entries.UpdateStatusByProviderMessageID(providerMessageID, next); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not advance delivery for %s: %v",
			providerMessageID, err)
	}
}
