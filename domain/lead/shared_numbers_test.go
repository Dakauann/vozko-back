package lead

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestSharedNumbersNameTheOtherHoldersOfEachNumber(t *testing.T) {
	self := &Lead{ID: "l-1", Number: "5511987654321", Phones: []ContactPhone{
		{ID: "p-1", Number: "551133334444", Label: PhoneLandline},
		{ID: "p-2", Number: "5511912345678", Label: PhoneMobile},
	}}
	sister := &Lead{ID: "l-2", Name: "Joana Souza", Number: "5511955554444", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	father := &Lead{ID: "l-3", Name: "Pedro Souza", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}, {Number: "5511987654321", Label: PhoneMobile}}}
	ninthDigitTwin := &Lead{ID: "l-4", Number: "551187654321"}
	stranger := &Lead{ID: "l-5", Name: "Outra", Number: "5521999998888"}

	cases := []struct {
		name   string
		self   *Lead
		others []*Lead
		want   []SharedNumber
	}{
		{
			name:   "each number lists who else holds it, identity first and holders by name",
			self:   self,
			others: []*Lead{sister, father, stranger},
			want: []SharedNumber{
				{Number: "5511987654321", Holders: []NumberHolder{{LeadID: "l-3", Name: "Pedro Souza"}}},
				{Number: "551133334444", Holders: []NumberHolder{{LeadID: "l-2", Name: "Joana Souza", Number: "5511955554444"}, {LeadID: "l-3", Name: "Pedro Souza"}}},
			},
		},
		{
			name:   "the other ninth digit form of a number is the same number",
			self:   self,
			others: []*Lead{ninthDigitTwin},
			want:   []SharedNumber{{Number: "5511987654321", Holders: []NumberHolder{{LeadID: "l-4", Number: "551187654321"}}}},
		},
		{
			name:   "the lead itself and repeated holders are never listed",
			self:   self,
			others: []*Lead{self, sister, sister, nil},
			want:   []SharedNumber{{Number: "551133334444", Holders: []NumberHolder{{LeadID: "l-2", Name: "Joana Souza", Number: "5511955554444"}}}},
		},
		{
			name:   "nobody else holds a number",
			self:   self,
			others: []*Lead{stranger},
			want:   []SharedNumber{},
		},
		{
			name:   "a lead without numbers shares nothing",
			self:   &Lead{ID: "l-9"},
			others: []*Lead{sister},
			want:   []SharedNumber{},
		},
		{
			name:   "no lead",
			self:   nil,
			others: []*Lead{sister},
			want:   []SharedNumber{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SharedNumbersOf(tc.self, tc.others)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("shared numbers = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestSharedNumbersStopAtTheHolderLimitAndSayThereAreMore(t *testing.T) {
	self := &Lead{ID: "l-0", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	others := make([]*Lead, 0, SharedNumberHolderLimit+2)
	for i := 0; i < SharedNumberHolderLimit+2; i++ {
		others = append(others, &Lead{ID: "l-" + strconv.Itoa(i+1), Name: "Pessoa " + strconv.Itoa(i+1), Phones: []ContactPhone{{Number: "551133334444"}}})
	}
	got := SharedNumbersOf(self, others)
	if len(got) != 1 || len(got[0].Holders) != SharedNumberHolderLimit || !got[0].More {
		t.Fatalf("shared numbers = %+v, want %d holders and more", got, SharedNumberHolderLimit)
	}
	exact := SharedNumbersOf(self, others[:SharedNumberHolderLimit])
	if len(exact) != 1 || len(exact[0].Holders) != SharedNumberHolderLimit || exact[0].More {
		t.Fatalf("shared numbers = %+v, want exactly the limit and no more", exact)
	}
}

func TestCardOfCarriesTheOptOut(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	card := CardOf(&Lead{ID: "l-1", OptedOutAt: &at, OptOutSource: OptOutLeadRequest}, Viewer{})
	if card.OptedOutAt == nil || !card.OptedOutAt.Equal(at) || card.OptOutSource != OptOutLeadRequest {
		t.Fatalf("card = %+v, want the opt-out", card)
	}
	if fresh := CardOf(&Lead{ID: "l-2"}, Viewer{}); fresh.OptedOutAt != nil || fresh.OptOutSource != "" {
		t.Fatalf("card = %+v, want no opt-out", fresh)
	}
}

func TestSharedNumberHoldersSortByNameIgnoringAccents(t *testing.T) {
	self := &Lead{ID: "l-0", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	holder := func(id, name string) *Lead {
		return &Lead{ID: id, Name: name, Phones: []ContactPhone{{Number: "551133334444"}}}
	}
	others := []*Lead{holder("l-1", "Zé Carlos"), holder("l-2", "Érica"), holder("l-3", "Ângela"), holder("l-4", "bruno"), holder("l-5", "")}

	got := SharedNumbersOf(self, others)

	var order []string
	for _, h := range got[0].Holders {
		order = append(order, h.LeadID)
	}
	if want := []string{"l-3", "l-4", "l-2", "l-1", "l-5"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("holders = %v, want %v (accents fold into their letter, unnamed last)", order, want)
	}
}
