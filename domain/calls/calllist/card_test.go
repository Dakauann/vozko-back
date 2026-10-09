package calllist

import (
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func TestTheWorkerSeesOnlyTheFixedProjectionOfTheLead(t *testing.T) {
	birth := shared.Date{Year: 1980, Month: time.May, Day: 3}
	l := &lead.Lead{
		ID: "lead-1", Number: "5511987654321", Name: "Maria Souza", Email: "maria@example.com", BirthDate: &birth,
		CustomFields: map[string]any{"classificacao": "apoiador"}, RelativesCount: 3,
		Addresses: []lead.Address{
			{Primary: false, Postal: address.Postal{Street: "Rua A", Number: "10", District: "Centro", City: "Santos", State: "SP"}},
			{Primary: true, Postal: address.Postal{Street: "Rua B", Number: "20", District: "Vila Mariana", City: "São Paulo", State: "SP"}},
		},
	}
	card := CardOf(l)
	want := LeadCard{ID: "lead-1", Name: "Maria Souza", District: "Vila Mariana", City: "São Paulo", FamilyCount: 3}
	if card != want {
		t.Fatalf("card = %+v, want %+v", card, want)
	}
}

func TestACardWithoutAPrimaryAddressOrRealNameLeavesThemEmpty(t *testing.T) {
	l := &lead.Lead{ID: "lead-2", Number: "5511987654321", Name: "5511987654321",
		Addresses: []lead.Address{{Postal: address.Postal{District: "Centro", City: "Santos"}}}}
	if card := CardOf(l); card != (LeadCard{ID: "lead-2"}) {
		t.Fatalf("card = %+v", card)
	}
	if card := CardOf(nil); card != (LeadCard{}) {
		t.Fatalf("card of nothing = %+v", card)
	}
}
