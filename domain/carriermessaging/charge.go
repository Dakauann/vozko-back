package carriermessaging

import (
	"errors"
	"strings"

	"vozko/domain/rcs"
)

var ErrPriceUnavailable = errors.New("carrier messaging: this message has no price configured")

type ChargeKind string

const (
	ChargeRCSBasic        ChargeKind = "rcs_basic"
	ChargeRCSSingle       ChargeKind = "rcs_single"
	ChargeRCSConversation ChargeKind = "rcs_conversation"
	ChargeRCSInbound      ChargeKind = "rcs_inbound"
	ChargeSMSSegment      ChargeKind = "sms_segment"
	ChargeSMSInbound      ChargeKind = "sms_inbound"
)

const (
	replyFloorMessages  = 100
	replyFloorSpendPart = 10
)

var chargeMetrics = map[ChargeKind]string{
	ChargeRCSBasic:        "per_message",
	ChargeRCSSingle:       "per_message",
	ChargeRCSConversation: "per_conversation",
	ChargeRCSInbound:      "per_message",
	ChargeSMSSegment:      "per_segment",
	ChargeSMSInbound:      "per_message",
}

func AllChargeKinds() []ChargeKind {
	return []ChargeKind{ChargeRCSBasic, ChargeRCSSingle, ChargeRCSConversation, ChargeRCSInbound, ChargeSMSSegment, ChargeSMSInbound}
}

func ChargeKindsOf(c Channel) []ChargeKind {
	var out []ChargeKind
	for _, k := range AllChargeKinds() {
		if k.Channel() == c {
			out = append(out, k)
		}
	}
	return out
}

func ChargeForClass(class rcs.BillingClass) ChargeKind {
	if class == rcs.ClassBasic {
		return ChargeRCSBasic
	}
	return ChargeRCSSingle
}

func (k ChargeKind) known() bool {
	_, ok := chargeMetrics[k]
	return ok
}

func (k ChargeKind) Channel() Channel {
	if !k.known() {
		return ""
	}
	channel, _, _ := strings.Cut(string(k), "_")
	return Channel(channel)
}

func (k ChargeKind) PricingService() string {
	if !k.known() {
		return ""
	}
	_, service, _ := strings.Cut(string(k), "_")
	return service
}

func (k ChargeKind) PricingMetric() string {
	return chargeMetrics[k]
}

func ChargeReference(kind ChargeKind, key string) string {
	return "carrier:" + string(kind) + ":" + key
}

func ReplyFloor(spendLast30DaysMicros, inboundPriceMicros int64) int64 {
	return max(spendLast30DaysMicros/replyFloorSpendPart, replyFloorMessages*inboundPriceMicros)
}

func AffordsSend(balanceMicros, priceMicros, floorMicros int64) bool {
	return priceMicros > 0 && balanceMicros-priceMicros >= floorMicros
}

func Shortfall(balanceMicros, neededMicros, floorMicros int64) int64 {
	return max(0, neededMicros+floorMicros-balanceMicros)
}
