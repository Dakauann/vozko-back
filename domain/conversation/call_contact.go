package conversation

import "strings"

type CallContact struct {
	BusinessPhoneID string
	ContactNumber   string
}

func (c CallContact) Known() bool {
	return strings.TrimSpace(c.BusinessPhoneID) != "" && strings.TrimSpace(c.ContactNumber) != ""
}

type ContactCall interface {
	Contact() (CallContact, bool)
}

func ContactOf(call CRMCall) (CallContact, bool) {
	contactCall, ok := call.(ContactCall)
	if !ok {
		return CallContact{}, false
	}
	contact, known := contactCall.Contact()
	return contact, known && contact.Known()
}
