package campaign

import (
	"time"

	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
)

func RecipientRefusal(contact *lead.Lead) error {
	return refusalOf(LeadRefusal(LeadFactsOf(contact)))
}

func ContactRefusal(contact *lead.Lead) error {
	if contact == nil {
		return nil
	}
	facts := LeadFactsOf(contact)
	facts.HasIdentity = true
	return refusalOf(LeadRefusal(facts))
}

func ReplyRefusal(contact *lead.Lead, lastInbound *time.Time) error {
	return replyRefusalOf(contact, LeadRefusal(LeadFactsOf(contact)), lastInbound)
}

func ContactReplyRefusal(contact *lead.Lead, lastInbound *time.Time) error {
	if contact == nil {
		return nil
	}
	facts := LeadFactsOf(contact)
	facts.HasIdentity = true
	return replyRefusalOf(contact, LeadRefusal(facts), lastInbound)
}

func replyRefusalOf(contact *lead.Lead, reason SkipReason, lastInbound *time.Time) error {
	if reason == SkipOptedOut && contact.WroteAfterOptOut(lastInbound) {
		return nil
	}
	return refusalOf(reason)
}

func refusalOf(reason SkipReason) error {
	switch reason {
	case "":
		return nil
	case SkipBlocked:
		return wo.ErrLeadBlocked
	case SkipOptedOut:
		return wo.ErrLeadOptedOut
	}
	return wo.ErrInvalidPhone
}
