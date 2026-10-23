package carriercampaign

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

func smsVariant(name, text string, bindings ...campaign.VariableBinding) Variant {
	return Variant{Name: name, Text: text, Bindings: bindings}
}

func testRoute() sms.Route {
	return sms.Route{ID: "r1", WorkspaceID: "w1", BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive}
}

func smsCampaign() Campaign {
	c := validCampaign()
	c.Channel = carriermessaging.ChannelSMS
	c.SenderID = "r1"
	c.Variants = []Variant{smsVariant("", "Oi {{1}}, temos novidades", firstName())}
	return c
}

func TestSMSCampaignValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *Campaign)
		err    error
	}{
		{"valid", func(c *Campaign) {}, nil},
		{"ab test of texts", func(c *Campaign) {
			c.Variants = []Variant{smsVariant("A", "Oferta {{1}}", firstName()), smsVariant("B", "Desconto hoje")}
		}, nil},
		{"text required", func(c *Campaign) { c.Variants = []Variant{smsVariant("", "  ")} }, sms.ErrTextEmpty},
		{"over eight parts", func(c *Campaign) { c.Variants = []Variant{smsVariant("", strings.Repeat("a", 8*153+1))} }, sms.ErrTextTooLong},
		{"bindings counted on the text", func(c *Campaign) { c.Variants = []Variant{smsVariant("", "Oi {{1}} {{2}}", firstName())} }, campaign.ErrBindingsMismatch},
		{"rich content refused", func(c *Campaign) {
			c.Variants = []Variant{{Text: "oi", Content: rcs.Content{Text: "oi"}}}
		}, ErrVariantChannelMismatch},
		{"buttons refused", func(c *Campaign) {
			c.Variants = []Variant{{Text: "oi", Content: rcs.Content{Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Sim"}}}}}
		}, ErrVariantChannelMismatch},
		{"route required", func(c *Campaign) { c.SenderID = "" }, ErrSenderRequired},
		{"whatsapp fallback refused", func(c *Campaign) {
			c.Fallback.WhatsApp = &WhatsAppFallback{BusinessPhoneID: "p", TemplateID: "t"}
		}, ErrFallbackNotAllowed},
		{"sms fallback refused", func(c *Campaign) {
			c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: "oi"}
		}, ErrFallbackNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := smsCampaign()
			tc.mutate(&c)
			if err := c.Validate(testNow, nil); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestRCSVariantNeedsContent(t *testing.T) {
	c := validCampaign()
	c.Variants = []Variant{smsVariant("", "oi")}
	if err := c.Validate(testNow, nil); !errors.Is(err, ErrVariantChannelMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestSMSRouteID(t *testing.T) {
	withFallback := validCampaign()
	withFallback.Fallback.SMS = &SMSFallback{RouteID: "r9", Text: "oi"}
	cases := []struct {
		name string
		c    Campaign
		want string
	}{
		{"sms campaign uses its sender", smsCampaign(), "r1"},
		{"rcs campaign without sms", validCampaign(), ""},
		{"rcs campaign with sms fallback", withFallback, "r9"},
	}
	for _, tc := range cases {
		if got := tc.c.SMSRouteID(); got != tc.want {
			t.Errorf("%s: got %q", tc.name, got)
		}
	}
}

func TestValidateSMSRoute(t *testing.T) {
	long := strings.Repeat("a", 8*153-3)
	cases := []struct {
		name  string
		c     func() Campaign
		route sms.Route
		err   error
	}{
		{"sms campaign on its route", smsCampaign, testRoute(), nil},
		{"another route", smsCampaign, func() sms.Route { r := testRoute(); r.ID = "r2"; return r }(), ErrSMSRouteMismatch},
		{"invalid route", smsCampaign, func() sms.Route { r := testRoute(); r.BrandPrefix = "Lojá"; return r }(), sms.ErrRouteInvalid},
		{"prefix pushes the text past eight parts", func() Campaign {
			c := smsCampaign()
			c.Variants = []Variant{smsVariant("", long)}
			return c
		}, testRoute(), sms.ErrTextTooLong},
		{"prefix pushes the fallback past eight parts", func() Campaign {
			c := validCampaign()
			c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: long}
			return c
		}, testRoute(), sms.ErrTextTooLong},
		{"rcs fallback on its route", func() Campaign {
			c := validCampaign()
			c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: "Oi"}
			return c
		}, testRoute(), nil},
		{"campaign without sms takes no route", validCampaign, testRoute(), ErrSMSRouteMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c().ValidateSMSRoute(tc.route); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestWorstCaseSegments(t *testing.T) {
	if WorstCasePlaceholderRunes != 30 {
		t.Fatalf("placeholder = %d, want 30", WorstCasePlaceholderRunes)
	}
	route := testRoute()
	cases := []struct {
		name     string
		template string
		want     int
	}{
		{"short text", "Oi", 1},
		{"one variable fits one part", "Oi {{1}}, oferta", 1},
		{"prefix and placeholder still fit", strings.Repeat("a", 120) + " {{1}}", 1},
		{"two placeholders need two parts", strings.Repeat("a", 120) + " {{1}} {{2}}", 2},
		{"repeated variable counts each time", strings.Repeat("a", 120) + " {{1}} {{1}}", 2},
		{"prefix alone tips the text over", strings.Repeat("a", 155), 2},
		{"unicode template uses the 70 limit", "Ação " + strings.Repeat("b", 30) + " {{1}}", 2},
		{"unicode template that fits", "Promoção {{1}}", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorstCaseSegments(route, tc.template); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWorstCasePlaceholderFollowsEncoding(t *testing.T) {
	route := testRoute()
	gsm := WorstCaseText(route, "Oi {{1}}")
	if sms.EncodingOf(gsm) != sms.GSM7 || !strings.HasPrefix(gsm, "Loja: Oi ") || sms.Measure(gsm).Units != 6+3+30 {
		t.Fatalf("gsm worst case %q", gsm)
	}
	ucs := WorstCaseText(route, "Ação {{1}}")
	if sms.EncodingOf(ucs) != sms.UCS2 || strings.Contains(ucs, strings.Repeat("x", WorstCasePlaceholderRunes)) {
		t.Fatalf("ucs2 worst case %q", ucs)
	}
}

func TestFallbackSpec(t *testing.T) {
	wa := &WhatsAppFallback{BusinessPhoneID: "p1", TemplateID: "t1", BodyBindings: []campaign.VariableBinding{firstName()}}
	smsFallback := &SMSFallback{RouteID: "r1", Text: "Oi {{1}}, oferta", Bindings: []campaign.VariableBinding{firstName()}}

	got, err := Fallback{WhatsApp: wa, SMS: smsFallback}.Spec("u1", FallbackValues{WhatsAppBody: []string{"Ana"}, SMS: []string{"Ana"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.WhatsApp == nil || got.WhatsApp.ActorID != "u1" || got.WhatsApp.BusinessPhoneID != "p1" || got.WhatsApp.TemplateID != "t1" ||
		!slices.Equal(got.WhatsApp.BodyParams, []string{"Ana"}) || len(got.WhatsApp.HeaderParams) != 0 {
		t.Fatalf("whatsapp spec %+v", got.WhatsApp)
	}
	if got.SMSRouteID != "r1" || got.SMSText != "Oi Ana, oferta" {
		t.Fatalf("sms spec %q %q", got.SMSRouteID, got.SMSText)
	}
	if plan := got.Plan(); !plan.WhatsApp || !plan.SMS {
		t.Fatalf("plan %+v", plan)
	}

	none, err := Fallback{}.Spec("", FallbackValues{})
	if err != nil || none.WhatsApp != nil || none.SMSRouteID != "" || none.SMSText != "" {
		t.Fatalf("empty spec %+v, %v", none, err)
	}

	errCases := []struct {
		name   string
		f      Fallback
		actor  string
		values FallbackValues
		err    error
	}{
		{"whatsapp needs the person", Fallback{WhatsApp: wa}, " ", FallbackValues{WhatsAppBody: []string{"Ana"}}, ErrWhatsAppActorRequired},
		{"whatsapp body values", Fallback{WhatsApp: wa}, "u1", FallbackValues{}, campaign.ErrBindingsMismatch},
		{"whatsapp header values", Fallback{WhatsApp: wa}, "u1", FallbackValues{WhatsAppBody: []string{"Ana"}, WhatsAppHeader: []string{"x"}}, campaign.ErrBindingsMismatch},
		{"sms values", Fallback{SMS: smsFallback}, "", FallbackValues{SMS: []string{"Ana", "extra"}}, campaign.ErrBindingsMismatch},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.f.Spec(tc.actor, tc.values); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}
