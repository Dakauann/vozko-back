package customerportfolio

import (
	"errors"
	"testing"

	"vozko/domain/shared"
)

func TestSubjectIsExactlyOneLeadOrOneChannelContact(t *testing.T) {
	cases := []struct {
		name    string
		subject Subject
		valid   bool
	}{
		{"a lead", LeadSubject("lead-1"), true},
		{"an instagram contact found by its handle", ContactSubject(shared.EntryTypeInstagram, "ig-1"), true},
		{"a telegram contact that never shared a phone", ContactSubject(shared.EntryTypeTelegram, "tg-1"), true},
		{"a facebook contact", ContactSubject(shared.EntryTypeFacebook, "fb-1"), true},
		{"a webchat visitor", ContactSubject(shared.EntryTypeWebchat, "wc-1"), true},
		{"an unofficial whatsapp contact", ContactSubject(shared.EntryTypeUnofficialWhatsApp, "uw-1"), true},
		{"official whatsapp has no contact table, only leads", ContactSubject(shared.EntryTypeWhatsApp, "x"), false},
		{"nothing at all", Subject{}, false},
		{"both a lead and a contact", Subject{LeadID: "lead-1", ContactType: shared.EntryTypeInstagram, ContactID: "ig-1"}, false},
		{"a contact without its channel", Subject{ContactID: "ig-1"}, false},
		{"a channel without its contact", Subject{ContactType: shared.EntryTypeInstagram}, false},
		{"blank lead id", LeadSubject("   "), false},
		{"blank contact id", Subject{ContactType: shared.EntryTypeInstagram, ContactID: "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.subject.Validate()
			if c.valid && err != nil {
				t.Fatalf("Validate() = %v, want valid", err)
			}
			if !c.valid && !errors.Is(err, ErrSubjectInvalid) {
				t.Fatalf("Validate() = %v, want ErrSubjectInvalid", err)
			}
		})
	}
}

func TestSubjectConstructorsTrimIds(t *testing.T) {
	if got := LeadSubject(" lead-1 "); got.LeadID != "lead-1" {
		t.Fatalf("lead id = %q", got.LeadID)
	}
	if got := ContactSubject(shared.EntryTypeInstagram, " ig-1 "); got.ContactID != "ig-1" {
		t.Fatalf("contact id = %q", got.ContactID)
	}
}

func TestSubjectKind(t *testing.T) {
	if got := LeadSubject("lead-1").Kind(); got != SubjectLead {
		t.Fatalf("lead kind = %q", got)
	}
	if got := ContactSubject(shared.EntryTypeInstagram, "ig-1").Kind(); got != SubjectContact {
		t.Fatalf("contact kind = %q", got)
	}
}
