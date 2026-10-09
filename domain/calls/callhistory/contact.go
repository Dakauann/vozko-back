package callhistory

import "vozko/domain/lead"

func ContactFor(number string, leads []*lead.Lead) Contact {
	contact := Contact{Number: number}
	var only, owner *lead.Lead
	for _, l := range leads {
		if l == nil || !l.HoldsNumber(number) {
			continue
		}
		contact.Holders++
		only = l
		if l.HoldsIdentity(number) {
			owner = l
		}
	}
	chosen := owner
	if chosen == nil && contact.Holders == 1 {
		chosen = only
	}
	if chosen != nil {
		contact.LeadID, contact.Name = chosen.ID, chosen.RealName()
	}
	return contact
}

func LinkedContact(number string, linked *lead.Lead, holders []*lead.Lead) Contact {
	contact := ContactFor(number, holders)
	if linked != nil {
		contact.LeadID, contact.Name = linked.ID, linked.RealName()
	}
	return contact
}
