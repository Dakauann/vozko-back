package conversation

import "strings"

// Meta's own answer to the question this platform now has to ask: what did that
// message cost us.
//
// It arrives on the status webhook we already handle, and until now it was read
// once to pick a refund category and then discarded. Persisting it is what
// eventually lets the service message report stop being an upper bound: our own
// rule counts every outbound non-template message, while Meta knows which ones
// fell inside the 72 hour free entry point and cost nothing.

// MetaPricingCategoryService is the category Meta stamps on a non-template
// message sent by a person or a third-party AI. From 1 October 2026 it is
// billable.
const MetaPricingCategoryService = "service"

// MetaOriginFreeEntryPoint marks a conversation opened from a Click to WhatsApp
// ad or a Facebook call-to-action. Delivery inside its 72 hour window stays
// free, which is the one exemption our own counting rule cannot see.
const MetaOriginFreeEntryPoint = "referral_conversion"

// MetaPricing is the pricing object from a WhatsApp status webhook.
type MetaPricing struct {
	// Category is service, utility, marketing or authentication.
	Category string
	// Billable is Meta's own verdict. Before 1 October 2026 service messages
	// arrive with it false.
	Billable bool
	// Model is Meta's pricing model name, kept for provenance when a rate has to
	// be explained later.
	Model string
}

// NormalizedCategory is the lowercase, trimmed category.
//
// Meta writes it lowercase on the webhook and uppercase in the Pricing
// Analytics API. One stable form is persisted so a query never has to know
// which surface a given row came from.
func (p MetaPricing) NormalizedCategory() string {
	return strings.ToLower(strings.TrimSpace(p.Category))
}

// Known reports whether Meta has actually told us anything.
//
// It is deliberately not the same as "not billable". A row Meta has not spoken
// about yet must not be counted as a confirmed zero, or the report would claim
// certainty it does not have.
func (p MetaPricing) Known() bool {
	return p.NormalizedCategory() != ""
}

// IsBillableService reports a message Meta charges us for as a service message.
// Both halves matter: the category says which rate card applies, and Billable
// says whether this particular delivery was charged at all.
func (p MetaPricing) IsBillableService() bool {
	return p.Billable && p.NormalizedCategory() == MetaPricingCategoryService
}

// DeliveryReceipt is everything one status webhook tells us about one message.
//
// It exists so the delivery status, the failure reason and Meta's pricing reach
// the database in a single update. They arrive together, they describe the same
// event, and writing them separately would mean two updates of the same row on
// the busiest webhook path the platform has.
type DeliveryReceipt struct {
	Status       DeliveryStatus
	ErrorCode    int
	ErrorMessage string

	Pricing MetaPricing

	// ConversationOrigin is the origin type of the conversation the message
	// belongs to, which is how a free entry point delivery is recognised.
	ConversationOrigin string
}

// HasPricing reports whether this receipt carries anything worth persisting
// beyond the status.
//
// Guarding on it keeps an ordinary receipt from writing empty strings over
// pricing an earlier webhook already recorded for the same message.
func (r DeliveryReceipt) HasPricing() bool {
	return r.Pricing.Known() || strings.TrimSpace(r.ConversationOrigin) != ""
}

// NormalizedOrigin is the lowercase, trimmed conversation origin.
func (r DeliveryReceipt) NormalizedOrigin() string {
	return strings.ToLower(strings.TrimSpace(r.ConversationOrigin))
}

// IsFreeEntryPoint reports a delivery inside the 72 hour free entry point
// window, which Meta does not charge for whatever the category says.
func (r DeliveryReceipt) IsFreeEntryPoint() bool {
	return r.NormalizedOrigin() == MetaOriginFreeEntryPoint
}
