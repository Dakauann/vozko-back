package opportunity

import "testing"

type fixedNames map[string]string

func (f fixedNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := f[id]; ok {
			out[id] = name
		}
	}
	return out
}

func TestOwnerCreatorAndCloserAreNamedIncludingAutomation(t *testing.T) {
	deal := &Opportunity{OwnerID: "ai:a1", CreatedBy: "workflow:w1", ClosedBy: "u1"}
	unowned := &Opportunity{CreatedBy: "system"}

	NameParticipants(fixedNames{"u1": "Ana", "ai:a1": "Sofia", "workflow:w1": "Boas-vindas"}, deal, unowned, nil)

	if deal.OwnerName != "Sofia" || deal.CreatedByName != "Boas-vindas" || deal.ClosedByName != "Ana" {
		t.Fatalf("deal = %+v", deal)
	}
	if unowned.OwnerName != "" || unowned.CreatedByName != "" {
		t.Fatalf("unowned = %+v", unowned)
	}
}

func TestEventActorsAreNamed(t *testing.T) {
	events := []Event{{ActorID: "workflow:w1"}, {ActorID: "system"}}

	NameEventActors(fixedNames{"workflow:w1": "Boas-vindas"}, events)

	if events[0].ActorName != "Boas-vindas" || events[1].ActorName != "" {
		t.Fatalf("events = %+v", events)
	}
}

func TestWithoutANamerNothingIsNamed(t *testing.T) {
	o := &Opportunity{OwnerID: "u1"}
	NameParticipants(nil, o)
	NameEventActors(nil, []Event{{ActorID: "u1"}})
	if o.OwnerName != "" {
		t.Fatal("no namer, no name")
	}
}
