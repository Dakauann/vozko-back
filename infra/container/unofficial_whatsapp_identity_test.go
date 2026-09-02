package container

import (
	"context"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

// identityContacts answers only the one method the inbox hydration calls. The
// embedded nil interface makes anything else a loud panic rather than a silent
// zero value.
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

// The inbox asks for a contact using whatever rode the lead slot, and on this
// channel that is the CRM lead id once the contact has resolved to one. Keying
// the answer only by contact id meant every linked row came back unmatched:
// name, avatar, handle and the group flag all silently absent.
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

	// Still keyed by contact id too: a group and a contact that has not resolved
	// yet are addressed that way, and both share this one lookup.
	if _, ok := got["contact-1"]; !ok {
		t.Errorf("contact id key dropped; groups and unlinked contacts would stop hydrating: %+v", got)
	}
}

// A contact with no lead — every group, and anyone whose first message has not
// been bridged yet — must not add an empty key that swallows unrelated rows.
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
