package callhistory

import (
	"testing"

	"vozko/domain/lead"
)

func TestContactForPicksWhoHoldsTheNumber(t *testing.T) {
	mae := &lead.Lead{ID: "mae", Number: "5511999991234", Name: "Mãe"}
	filho := &lead.Lead{ID: "filho", Name: "Filho", Phones: []lead.ContactPhone{{Number: "5511999991234"}, {Number: "551133334444"}}}
	irma := &lead.Lead{ID: "irma", Name: "Irmã", Phones: []lead.ContactPhone{{Number: "551133334444"}}}
	vizinho := &lead.Lead{ID: "vizinho", Name: "Vizinho", Phones: []lead.ContactPhone{{Number: "551122220000"}}}
	everyone := []*lead.Lead{mae, filho, irma, vizinho}
	cases := []struct {
		name   string
		number string
		want   Contact
	}{
		{"the WhatsApp owner wins over a relative who lists the number", "551199991234", Contact{Number: "551199991234", LeadID: "mae", Name: "Mãe", Holders: 2}},
		{"the only lead that keeps the number as a contact phone", "551122220000", Contact{Number: "551122220000", LeadID: "vizinho", Name: "Vizinho", Holders: 1}},
		{"a shared landline names nobody and says how many hold it", "551133334444", Contact{Number: "551133334444", Holders: 2}},
		{"an unknown number", "551100000000", Contact{Number: "551100000000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContactFor(tc.number, everyone); got != tc.want {
				t.Fatalf("ContactFor = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLinkedContactNamesTheLeadTheCallWasPlacedFor(t *testing.T) {
	mae := &lead.Lead{ID: "mae", Number: "5511999991234", Name: "Mãe"}
	filho := &lead.Lead{ID: "filho", Name: "Filho", Phones: []lead.ContactPhone{{Number: "5511999991234"}, {Number: "551133334444"}}}
	irma := &lead.Lead{ID: "irma", Name: "Irmã", Phones: []lead.ContactPhone{{Number: "551133334444"}}}
	holders := []*lead.Lead{mae, filho, irma}
	cases := []struct {
		name   string
		number string
		linked *lead.Lead
		want   Contact
	}{
		{"the linked lead wins over the WhatsApp owner of the number", "5511999991234", filho, Contact{Number: "5511999991234", LeadID: "filho", Name: "Filho", Holders: 2}},
		{"the linked lead names a shared landline", "551133334444", irma, Contact{Number: "551133334444", LeadID: "irma", Name: "Irmã", Holders: 2}},
		{"an old call without a link falls back to the number", "5511999991234", nil, Contact{Number: "5511999991234", LeadID: "mae", Name: "Mãe", Holders: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LinkedContact(tc.number, tc.linked, holders); got != tc.want {
				t.Fatalf("LinkedContact = %+v, want %+v", got, tc.want)
			}
		})
	}
}
