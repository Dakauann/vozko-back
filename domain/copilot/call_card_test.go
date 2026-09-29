package copilot

import (
	"testing"

	"vozko/domain/sip_trunk"
)

func TestCallCardCarriesTheChosenTrunk(t *testing.T) {
	card := NewCallCard(sip_trunk.CallPlan{
		PhoneNumber: "+5584999990000",
		Trunks:      []sip_trunk.TrunkChoice{{ID: "t1", Name: "Principal"}},
	})
	if card.Kind != ActionPlaceCall || card.Call == nil {
		t.Fatalf("unexpected card %+v", card)
	}
	if *card.Call != (CallIntent{PhoneNumber: "+5584999990000", TrunkID: "t1", TrunkName: "Principal"}) {
		t.Fatalf("call = %+v", *card.Call)
	}
}

func TestCallCardLeavesTheTrunkToTheMemberWhenThereIsAChoice(t *testing.T) {
	card := NewCallCard(sip_trunk.CallPlan{
		PhoneNumber: "100",
		Trunks:      []sip_trunk.TrunkChoice{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	})
	if card.Call.TrunkID != "" || card.Call.TrunkName != "" || card.Call.PhoneNumber != "100" {
		t.Fatalf("call = %+v", *card.Call)
	}
}

func TestCallCardIsNotAnOfferableAction(t *testing.T) {
	if ActionPlaceCall.Valid() {
		t.Fatal("place_call must come only from the call tool, never from offer_action")
	}
}
