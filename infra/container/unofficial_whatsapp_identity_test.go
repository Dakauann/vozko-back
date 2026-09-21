package container

import (
	"context"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

type identityContacts struct {
	uw.ContactRepository

	asked    []string
	contacts []*uw.Contact
}

func (c *identityContacts) FindByIDs(_ context.Context, ids []string) ([]*uw.Contact, error) {
	c.asked = append(c.asked, ids...)
	return c.contacts, nil
}

func strptr(s string) *string { return &s }

func TestUnofficialWhatsAppContactIdentity_KeyedByBothIDs(t *testing.T) {
	contacts := &identityContacts{contacts: []*uw.Contact{
		{ID: "contact-1", LeadID: strptr("lead-1"), Name: "Dakauann", PictureURL: "pic"},
	}}
	lookup := unofficialWhatsAppContactIdentity(&unofficialWhatsAppBundle{Contacts: contacts})

	got, err := lookup.ContactsByIDs(context.Background(), []string{"lead-1"})
	if err != nil {
		t.Fatalf("ContactsByIDs: %v", err)
	}

	byLead, ok := got["lead-1"]
	if !ok {
		t.Fatalf("a contact asked for by its lead id was not returned under it: %+v", got)
	}
	if byLead.PictureURL != "pic" {
		t.Errorf("PictureURL = %q; a linked row would render with no avatar", byLead.PictureURL)
	}

	if _, ok := got["contact-1"]; !ok {
		t.Errorf("contact id key dropped; groups and unlinked contacts would stop hydrating: %+v", got)
	}
}

func TestUnofficialWhatsAppContactIdentity_NoLeadAddsNoBlankKey(t *testing.T) {
	contacts := &identityContacts{contacts: []*uw.Contact{
		{ID: "group-1", IsGroup: true, Name: "Teste grupos"},
		{ID: "contact-2", LeadID: strptr(""), Name: "Sem lead"},
	}}
	lookup := unofficialWhatsAppContactIdentity(&unofficialWhatsAppBundle{Contacts: contacts})

	got, err := lookup.ContactsByIDs(context.Background(), []string{"group-1", "contact-2"})
	if err != nil {
		t.Fatalf("ContactsByIDs: %v", err)
	}
	if _, ok := got[""]; ok {
		t.Fatal(`an empty lead id was used as a map key; it would collide across contacts`)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if !got["group-1"].IsGroup {
		t.Error("the group flag must survive; it is what suppresses lead-only affordances")
	}
}
