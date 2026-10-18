package campaign

import (
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
