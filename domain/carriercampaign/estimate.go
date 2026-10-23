package carriercampaign

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

const WorstCasePlaceholderRunes = 30

const (
	gsmPlaceholderRune  = 'x'
	ucs2PlaceholderRune = 'ã'
)

var ErrRecipientsInvalid = errors.New("carriercampaign: the recipient count cannot be negative")

type Prices map[carriermessaging.ChargeKind]int64

func (p Prices) Of(kind carriermessaging.ChargeKind) (int64, error) {
	price := p[kind]
	if price <= 0 {
		return 0, fmt.Errorf("%w: %s", carriermessaging.ErrPriceUnavailable, kind)
	}
	return price, nil
}

type EstimateInput struct {
	Recipients int
	Prices     Prices
	SMSRoute   *sms.Route
}

type Estimate struct {
	Channel            carriermessaging.Channel `json:"channel"`
	Messages           int64                    `json:"messages"`
	WorstCaseSegments  int                      `json:"worst_case_segments"`
	WorstCaseMicros    int64                    `json:"worst_case_micros"`
	ExpectedMicros     int64                    `json:"expected_micros"`
	PerRecipientMicros int64                    `json:"per_recipient_micros"`
}

func WorstCaseClass(variants []Variant) rcs.BillingClass {
	for _, v := range variants {
		if v.Content.BillingClass() == rcs.ClassSingle {
			return rcs.ClassSingle
		}
	}
	return rcs.ClassBasic
}

func WorstCaseText(route sms.Route, template string) string {
	placeholder := gsmPlaceholderRune
	if sms.EncodingOf(template) == sms.UCS2 {
		placeholder = ucs2PlaceholderRune
	}
	slots := shared.HighestPositionalParameter([]string{template})
	values := make([]string, slots)
	for i := range values {
		values[i] = strings.Repeat(string(placeholder), WorstCasePlaceholderRunes)
	}
	return route.Compose(shared.RenderPositional(template, values))
}

func WorstCaseSegments(route sms.Route, template string) int {
	return sms.Measure(WorstCaseText(route, template)).Segments
}

func (c Campaign) Estimate(in EstimateInput) (Estimate, error) {
	switch {
	case !c.Channel.Valid():
		return Estimate{}, ErrChannelInvalid
	case in.Recipients < 0:
		return Estimate{}, ErrRecipientsInvalid
	case len(c.Variants) == 0:
		return Estimate{}, ErrVariantsRequired
	}
	if err := c.checkSMSRoute(in.SMSRoute); err != nil {
		return Estimate{}, err
	}
	var (
		e   Estimate
		err error
	)
	if c.Channel == carriermessaging.ChannelSMS {
		e, err = c.smsEstimate(int64(in.Recipients), in.Prices, *in.SMSRoute)
	} else {
		e, err = c.rcsEstimate(int64(in.Recipients), in.Prices, in.SMSRoute)
	}
	if err != nil {
		return Estimate{}, err
	}
	e.Channel = c.Channel
	e.Messages = int64(in.Recipients)
	e.WorstCaseMicros = e.PerRecipientMicros * e.Messages
	return e, nil
}

func (c Campaign) rcsEstimate(recipients int64, prices Prices, route *sms.Route) (Estimate, error) {
	unit, err := prices.Of(carriermessaging.ChargeForClass(WorstCaseClass(c.Variants)))
	if err != nil {
		return Estimate{}, err
	}
	var segments int
	if c.Fallback.SMS != nil {
		segmentPrice, err := prices.Of(carriermessaging.ChargeSMSSegment)
		if err != nil {
			return Estimate{}, err
		}
		segments = WorstCaseSegments(*route, c.Fallback.SMS.Text)
		unit = max(unit, int64(segments)*segmentPrice)
	}
	expected, err := splitEvenly(c.Variants, recipients, func(v Variant) (int64, error) {
		return prices.Of(carriermessaging.ChargeForClass(v.Content.BillingClass()))
	})
	if err != nil {
		return Estimate{}, err
	}
	return Estimate{WorstCaseSegments: segments, ExpectedMicros: expected, PerRecipientMicros: unit}, nil
}

func (c Campaign) smsEstimate(recipients int64, prices Prices, route sms.Route) (Estimate, error) {
	segmentPrice, err := prices.Of(carriermessaging.ChargeSMSSegment)
	if err != nil {
		return Estimate{}, err
	}
	worst := 0
	for _, v := range c.Variants {
		worst = max(worst, WorstCaseSegments(route, v.Text))
	}
	expected, err := splitEvenly(c.Variants, recipients, func(v Variant) (int64, error) {
		return int64(WorstCaseSegments(route, v.Text)) * segmentPrice, nil
	})
	if err != nil {
		return Estimate{}, err
	}
	return Estimate{WorstCaseSegments: worst, ExpectedMicros: expected, PerRecipientMicros: int64(worst) * segmentPrice}, nil
}

func splitEvenly(variants []Variant, recipients int64, unitOf func(Variant) (int64, error)) (int64, error) {
	n := int64(len(variants))
	base, extra := recipients/n, recipients%n
	var total int64
	for i, v := range variants {
		unit, err := unitOf(v)
		if err != nil {
			return 0, err
		}
		share := base
		if int64(i) < extra {
			share++
		}
		total += share * unit
	}
	return total, nil
}

func (e Estimate) Shortfall(balanceMicros, spendLast30DaysMicros int64, prices Prices) (int64, error) {
	if !e.Channel.Valid() {
		return 0, ErrChannelInvalid
	}
	inbound, err := prices.Of(e.Channel.InboundCharge())
	if err != nil {
		return 0, err
	}
	return carriermessaging.Shortfall(balanceMicros, e.WorstCaseMicros, carriermessaging.ReplyFloor(spendLast30DaysMicros, inbound)), nil
}
