package conversation

import "testing"

// Meta tells us what it charged for, on the same status webhook that carries
// the delivery receipt, and until now we read the category to pick a refund
// and then threw the whole thing away.
//
// Keeping it matters because our own rule cannot see one thing Meta can: a
// message inside the 72 hour free entry point is free, and looks identical to
// a billed one from where we stand. Until this is persisted every service
// message figure we report is an upper bound.

func TestServiceCategoryIsRecognisedWhateverTheCase(t *testing.T) {
	// Meta documents the category lowercase on the webhook and uppercase in the
	// Pricing Analytics API, and our own refund path already upper-cases it
	// before comparing. Accepting one spelling would silently classify half the
	// traffic as "not a service message".
	for _, raw := range []string{"service", "SERVICE", " Service "} {
		pricing := MetaPricing{Category: raw, Billable: true}
		if !pricing.IsBillableService() {
			t.Errorf("category %q not recognised as a billed service message", raw)
		}
	}
}

// Billable is the whole point. Before 1 October 2026 Meta sends the service
// category with billable false, and counting those would report a cost that
// does not exist yet.
func TestUnbillableServiceIsNotCounted(t *testing.T) {
	pricing := MetaPricing{Category: "service", Billable: false}
	if pricing.IsBillableService() {
		t.Error("a service message Meta says is not billable must not be counted")
	}
}

// A billed template is not a service message. It is already charged to the
// customer as a campaign send, and counting it here would double count the same
// message on both sides of the comparison the report exists to make.
func TestOtherBilledCategoriesAreNotServiceMessages(t *testing.T) {
	for _, category := range []string{"utility", "marketing", "authentication"} {
		pricing := MetaPricing{Category: category, Billable: true}
		if pricing.IsBillableService() {
			t.Errorf("category %q counted as a service message", category)
		}
	}
}

// An empty pricing object is "Meta has not told us", which is not the same as
// "Meta told us it was free". The report has to be able to tell those apart, or
// every message we have not heard about yet reads as a confirmed zero.
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

// The free entry point is the one thing our own rule cannot see. Recording the
// origin is what eventually lets the report stop being an upper bound, so the
// predicate has to name it rather than leave the caller to compare strings.
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

// A receipt with nothing but a status is the ordinary case, and it must not
// look like it carries pricing. Otherwise the update would write empty strings
// over whatever Meta told us on an earlier webhook for the same message.
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

	// The origin alone is worth persisting even with no pricing block, because
	// it is what identifies a free entry point conversation.
	originOnly := DeliveryReceipt{
		Status:             DeliveryStatusDelivered,
		ConversationOrigin: "referral_conversion",
	}
	if !originOnly.HasPricing() {
		t.Error("an origin with no pricing block is still worth recording")
	}
}

// Normalized is what reaches the database, so it has to be the stable lowercase
// form rather than whichever spelling that particular webhook used.
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
