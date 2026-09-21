package conversation

import "strings"

const MetaPricingCategoryService = "service"

const MetaOriginFreeEntryPoint = "referral_conversion"

type MetaPricing struct {
	Category string
	Billable bool
	Model    string
}

func (p MetaPricing) NormalizedCategory() string {
	return strings.ToLower(strings.TrimSpace(p.Category))
}

func (p MetaPricing) Known() bool {
	return p.NormalizedCategory() != ""
}

func (p MetaPricing) IsBillableService() bool {
	return p.Billable && p.NormalizedCategory() == MetaPricingCategoryService
}

type DeliveryReceipt struct {
	Status       DeliveryStatus
	ErrorCode    int
	ErrorMessage string

	Pricing MetaPricing

	ConversationOrigin string
}

func (r DeliveryReceipt) HasPricing() bool {
	return r.Pricing.Known() || strings.TrimSpace(r.ConversationOrigin) != ""
}

func (r DeliveryReceipt) NormalizedOrigin() string {
	return strings.ToLower(strings.TrimSpace(r.ConversationOrigin))
}

func (r DeliveryReceipt) IsFreeEntryPoint() bool {
	return r.NormalizedOrigin() == MetaOriginFreeEntryPoint
}

type ServiceMessageBilling interface {
	AllowSend(workspaceID string) error

	ShouldCharge(receipt DeliveryReceipt) bool

	ChargeDelivered(workspaceID string, receipt DeliveryReceipt, providerMessageID string) error
}
