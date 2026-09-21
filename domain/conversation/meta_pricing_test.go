package conversation

import "testing"

func TestServiceCategoryIsRecognisedWhateverTheCase(t *testing.T) {
	for _, raw := range []string{"service", "SERVICE", " Service "} {
		pricing := MetaPricing{Category: raw, Billable: true}
		if !pricing.IsBillableService() {
			t.Errorf("category %q not recognised as a billed service message", raw)
		}
	}
}

func TestUnbillableServiceIsNotCounted(t *testing.T) {
	pricing := MetaPricing{Category: "service", Billable: false}
	if pricing.IsBillableService() {
		t.Error("a service message Meta says is not billable must not be counted")
	}
}

func TestOtherBilledCategoriesAreNotServiceMessages(t *testing.T) {
	for _, category := range []string{"utility", "marketing", "authentication"} {
		pricing := MetaPricing{Category: category, Billable: true}
		if pricing.IsBillableService() {
			t.Errorf("category %q counted as a service message", category)
		}
	}
}

func TestEmptyPricingIsUnknownNotFree(t *testing.T) {
	var pricing MetaPricing
	if pricing.Known() {
		t.Error("an empty pricing object must not claim to be known")
	}
	if pricing.IsBillableService() {
		t.Error("an unknown pricing must not be counted as billable")
	}

	stated := MetaPricing{Category: "service", Billable: false}
	if !stated.Known() {
		t.Error("a stated category is known even when it is not billable")
	}
}

func TestFreeEntryPointOriginIsRecognised(t *testing.T) {
	for _, origin := range []string{
		"referral_conversion",
		"REFERRAL_CONVERSION",
	} {
		receipt := DeliveryReceipt{ConversationOrigin: origin}
		if !receipt.IsFreeEntryPoint() {
			t.Errorf("origin %q not recognised as the free entry point", origin)
		}
	}

	for _, origin := range []string{"", "service", "utility", "marketing"} {
		receipt := DeliveryReceipt{ConversationOrigin: origin}
		if receipt.IsFreeEntryPoint() {
			t.Errorf("origin %q wrongly treated as the free entry point", origin)
		}
	}
}

func TestReceiptWithoutPricingCarriesNothingToWrite(t *testing.T) {
	receipt := DeliveryReceipt{Status: DeliveryStatusDelivered}
	if receipt.HasPricing() {
		t.Error("a bare status receipt must not claim to carry pricing")
	}

	withPricing := DeliveryReceipt{
		Status:  DeliveryStatusDelivered,
		Pricing: MetaPricing{Category: "service", Billable: true},
	}
	if !withPricing.HasPricing() {
		t.Error("a receipt carrying a category must say so")
	}

	originOnly := DeliveryReceipt{
		Status:             DeliveryStatusDelivered,
		ConversationOrigin: "referral_conversion",
	}
	if !originOnly.HasPricing() {
		t.Error("an origin with no pricing block is still worth recording")
	}
}

func TestNormalizedCategoryIsStable(t *testing.T) {
	cases := map[string]string{
		"SERVICE":  "service",
		" service": "service",
		"Utility":  "utility",
		"":         "",
		"   ":      "",
	}
	for raw, want := range cases {
		got := MetaPricing{Category: raw}.NormalizedCategory()
		if got != want {
			t.Errorf("NormalizedCategory(%q) = %q, want %q", raw, got, want)
		}
	}
}
