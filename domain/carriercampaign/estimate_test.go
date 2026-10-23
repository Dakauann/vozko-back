package carriercampaign

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

func testPrices() Prices {
	return Prices{
		carriermessaging.ChargeRCSBasic:        10_000,
		carriermessaging.ChargeRCSSingle:       25_000,
		carriermessaging.ChargeRCSConversation: 50_000,
		carriermessaging.ChargeRCSInbound:      10_000,
		carriermessaging.ChargeSMSSegment:      40_000,
		carriermessaging.ChargeSMSInbound:      8_000,
	}
}

func TestPricesOf(t *testing.T) {
	p := Prices{carriermessaging.ChargeRCSBasic: 10, carriermessaging.ChargeRCSSingle: 0}
	if got, err := p.Of(carriermessaging.ChargeRCSBasic); err != nil || got != 10 {
		t.Fatalf("basic = %d, %v", got, err)
	}
	for _, kind := range []carriermessaging.ChargeKind{carriermessaging.ChargeRCSSingle, carriermessaging.ChargeSMSSegment} {
		if _, err := p.Of(kind); !errors.Is(err, carriermessaging.ErrPriceUnavailable) {
			t.Fatalf("%s err = %v", kind, err)
		}
	}
}

func TestRCSCampaignEstimate(t *testing.T) {
	basic := textVariant("Curta", "Oi {{1}}", firstName())
	single := cardVariant("Cartão")
	smsOnly := Fallback{SMS: &SMSFallback{RouteID: "r1", Text: "oi"}}
	longSMS := Fallback{SMS: &SMSFallback{RouteID: "r1", Text: strings.Repeat("a", 200)}}
	cheapSMS := testPrices()
	cheapSMS[carriermessaging.ChargeSMSSegment] = 5_000
	route := testRoute()
	otherRoute := testRoute()
	otherRoute.ID = "r2"
	rcsEstimate := func(messages, worst, expected, unit int64, segments int) Estimate {
		return Estimate{Channel: carriermessaging.ChannelRCS, Messages: messages, WorstCaseSegments: segments, WorstCaseMicros: worst, ExpectedMicros: expected, PerRecipientMicros: unit}
	}

	cases := []struct {
		name       string
		variants   []Variant
		fallback   Fallback
		prices     Prices
		route      *sms.Route
		recipients int
		want       Estimate
		err        error
	}{
		{"all basic", []Variant{basic}, Fallback{}, testPrices(), nil, 100, rcsEstimate(100, 1_000_000, 1_000_000, 10_000, 0), nil},
		{"any single makes the worst case single", []Variant{basic, single}, Fallback{}, testPrices(), nil, 100, rcsEstimate(100, 2_500_000, 50*10_000+50*25_000, 25_000, 0), nil},
		{"uneven split gives the remainder to the first variants", []Variant{single, basic, basic}, Fallback{}, testPrices(), nil, 10, rcsEstimate(10, 250_000, 4*25_000+3*10_000+3*10_000, 25_000, 0), nil},
		{"sms fallback dearer than rcs raises the worst case", []Variant{basic}, smsOnly, testPrices(), &route, 10, rcsEstimate(10, 400_000, 100_000, 40_000, 1), nil},
		{"long sms fallback counts its parts", []Variant{basic}, longSMS, testPrices(), &route, 10, rcsEstimate(10, 800_000, 100_000, 80_000, 2), nil},
		{"cheaper sms keeps the rcs worst case", []Variant{single}, smsOnly, cheapSMS, &route, 10, rcsEstimate(10, 250_000, 250_000, 25_000, 1), nil},
		{"whatsapp fallback is billed by whatsapp", []Variant{basic}, Fallback{WhatsApp: &WhatsAppFallback{BusinessPhoneID: "p", TemplateID: "t"}}, testPrices(), nil, 10, rcsEstimate(10, 100_000, 100_000, 10_000, 0), nil},
		{"no recipients", []Variant{basic}, Fallback{}, testPrices(), nil, 0, rcsEstimate(0, 0, 0, 10_000, 0), nil},
		{"sms fallback needs its route", []Variant{basic}, smsOnly, testPrices(), nil, 10, Estimate{}, ErrSMSRouteRequired},
		{"sms fallback on another route", []Variant{basic}, smsOnly, testPrices(), &otherRoute, 10, Estimate{}, ErrSMSRouteMismatch},
		{"route given without sms", []Variant{basic}, Fallback{}, testPrices(), &route, 10, Estimate{}, ErrSMSRouteMismatch},
		{"negative recipients", []Variant{basic}, Fallback{}, testPrices(), nil, -1, Estimate{}, ErrRecipientsInvalid},
		{"no variants", nil, Fallback{}, testPrices(), nil, 10, Estimate{}, ErrVariantsRequired},
		{"missing single price", []Variant{single}, Fallback{}, Prices{carriermessaging.ChargeRCSBasic: 1}, nil, 10, Estimate{}, carriermessaging.ErrPriceUnavailable},
		{"missing sms price", []Variant{basic}, smsOnly, Prices{carriermessaging.ChargeRCSBasic: 1}, &route, 10, Estimate{}, carriermessaging.ErrPriceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCampaign()
			c.Variants = tc.variants
			c.Fallback = tc.fallback
			got, err := c.Estimate(EstimateInput{Recipients: tc.recipients, Prices: tc.prices, SMSRoute: tc.route})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSMSCampaignEstimate(t *testing.T) {
	short := smsVariant("A", "Oi {{1}}", firstName())
	twoParts := smsVariant("B", strings.Repeat("a", 140)+" {{1}}", firstName())
	route := testRoute()
	smsEstimate := func(messages, worst, expected, unit int64, segments int) Estimate {
		return Estimate{Channel: carriermessaging.ChannelSMS, Messages: messages, WorstCaseSegments: segments, WorstCaseMicros: worst, ExpectedMicros: expected, PerRecipientMicros: unit}
	}
	cases := []struct {
		name       string
		variants   []Variant
		prices     Prices
		route      *sms.Route
		recipients int
		want       Estimate
		err        error
	}{
		{"one part", []Variant{short}, testPrices(), &route, 10, smsEstimate(10, 400_000, 400_000, 40_000, 1), nil},
		{"worst variant sets the worst case", []Variant{short, twoParts}, testPrices(), &route, 3, smsEstimate(3, 3*2*40_000, 2*1*40_000+1*2*40_000, 80_000, 2), nil},
		{"rcs prices are not needed", []Variant{short}, Prices{carriermessaging.ChargeSMSSegment: 40_000}, &route, 1, smsEstimate(1, 40_000, 40_000, 40_000, 1), nil},
		{"missing segment price", []Variant{short}, Prices{carriermessaging.ChargeRCSBasic: 1}, &route, 10, Estimate{}, carriermessaging.ErrPriceUnavailable},
		{"route required", []Variant{short}, testPrices(), nil, 10, Estimate{}, ErrSMSRouteRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := smsCampaign()
			c.Variants = tc.variants
			got, err := c.Estimate(EstimateInput{Recipients: tc.recipients, Prices: tc.prices, SMSRoute: tc.route})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEstimateRefusesUnknownChannel(t *testing.T) {
	c := validCampaign()
	c.Channel = "email"
	if _, err := c.Estimate(EstimateInput{Recipients: 1, Prices: testPrices()}); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestWorstCaseClass(t *testing.T) {
	cases := []struct {
		name     string
		variants []Variant
		want     rcs.BillingClass
	}{
		{"basic only", []Variant{textVariant("", "oi")}, rcs.ClassBasic},
		{"single only", []Variant{cardVariant("A")}, rcs.ClassSingle},
		{"mixed", []Variant{textVariant("A", "oi"), cardVariant("B")}, rcs.ClassSingle},
	}
	for _, tc := range cases {
		if got := WorstCaseClass(tc.variants); got != tc.want {
			t.Errorf("%s: got %s", tc.name, got)
		}
	}
}

func TestEstimateShortfall(t *testing.T) {
	rcsEstimate := Estimate{Channel: carriermessaging.ChannelRCS, WorstCaseMicros: 1_000_000}
	smsEstimate := Estimate{Channel: carriermessaging.ChannelSMS, WorstCaseMicros: 1_000_000}
	cases := []struct {
		name    string
		e       Estimate
		balance int64
		spend   int64
		prices  Prices
		want    int64
		err     error
	}{
		{"rcs floor from the rcs reply price", rcsEstimate, 1_500_000, 0, testPrices(), 1_000_000 + 100*10_000 - 1_500_000, nil},
		{"sms floor from the sms reply price", smsEstimate, 1_500_000, 0, testPrices(), 1_000_000 + 100*8_000 - 1_500_000, nil},
		{"floor from recent spend", rcsEstimate, 1_500_000, 30_000_000, testPrices(), 1_000_000 + 3_000_000 - 1_500_000, nil},
		{"enough balance", rcsEstimate, 10_000_000, 0, testPrices(), 0, nil},
		{"no reply price", smsEstimate, 10_000_000, 0, Prices{carriermessaging.ChargeSMSSegment: 1}, 0, carriermessaging.ErrPriceUnavailable},
		{"unknown channel", Estimate{WorstCaseMicros: 1}, 10_000_000, 0, testPrices(), 0, ErrChannelInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.e.Shortfall(tc.balance, tc.spend, tc.prices)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
