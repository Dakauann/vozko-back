package conversation

import "testing"

type contactlessCall struct{ CRMCall }

type whatsappLikeCall struct {
	CRMCall
	contact CallContact
}

func (c whatsappLikeCall) Contact() (CallContact, bool) { return c.contact, true }

func TestOnlyCallsThatKnowTheirContactHaveOne(t *testing.T) {
	if _, ok := ContactOf(contactlessCall{}); ok {
		t.Fatal("a call without a contact reported one")
	}
	if _, ok := ContactOf(whatsappLikeCall{contact: CallContact{BusinessPhoneID: "bp1"}}); ok {
		t.Fatal("a contact without a number was accepted")
	}
	contact, ok := ContactOf(whatsappLikeCall{contact: CallContact{BusinessPhoneID: "bp1", ContactNumber: "5584994409684"}})
	if !ok || contact.BusinessPhoneID != "bp1" {
		t.Fatalf("contact = %+v, %v", contact, ok)
	}
}
