package carriercampaign

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/sms"
)

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func textVariant(name, text string, bindings ...campaign.VariableBinding) Variant {
	return Variant{Name: name, Content: rcs.Content{Text: text}, Bindings: bindings}
}

func cardVariant(name string) Variant {
	return Variant{Name: name, Content: rcs.Content{Cards: []rcs.Card{{
		Title:       "Oferta",
		Description: "Confira",
		Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestOpenURL, Label: "Ver", URL: "https://loja.example.com"}},
	}}}}
}

func firstName() campaign.VariableBinding {
	return campaign.VariableBinding{Source: campaign.BindFirstName}
}

func validCampaign() Campaign {
	return Campaign{
		ID:          "c1",
		WorkspaceID: "w1",
		Name:        "Black Friday",
		SenderID:    "s1",
		Channel:     carriermessaging.ChannelRCS,
		Status:      StatusDraft,
		Kind:        KindMarketing,
		Variants:    []Variant{textVariant("", "Oi {{1}}, temos novidades", firstName())},
		Audience:    selection.Selection{Mode: selection.ModeIDs, IDs: []string{"lead-1"}},
		CreatedBy:   "u1",
	}
}

func TestKindValid(t *testing.T) {
	cases := []struct {
		kind      Kind
		valid     bool
		marketing bool
	}{
		{KindMarketing, true, true},
		{KindTransactional, true, false},
		{"", false, true},
		{"promo", false, true},
	}
	for _, tc := range cases {
		if tc.kind.Valid() != tc.valid {
			t.Errorf("%q valid = %v", tc.kind, !tc.valid)
		}
		if tc.kind.ObeysMarketingRules() != tc.marketing {
			t.Errorf("%q marketing rules = %v", tc.kind, !tc.marketing)
		}
	}
}

func TestCampaignValidate(t *testing.T) {
	future := testNow.Add(time.Hour)
	past := testNow.Add(-time.Minute)
	defs := []*customfield.Definition{{Key: "plano"}, {Key: "cpf", Sensitive: true}}

	cases := []struct {
		name   string
		mutate func(c *Campaign)
		err    error
	}{
		{"valid", func(c *Campaign) {}, nil},
		{"name required", func(c *Campaign) { c.Name = "  " }, ErrNameRequired},
		{"name too long", func(c *Campaign) { c.Name = strings.Repeat("a", MaxNameRunes+1) }, ErrNameTooLong},
		{"sender required", func(c *Campaign) { c.SenderID = "" }, ErrSenderRequired},
		{"unknown kind", func(c *Campaign) { c.Kind = "promo" }, ErrKindInvalid},
		{"unknown status", func(c *Campaign) { c.Status = "held" }, ErrStatusInvalid},
		{"no variants", func(c *Campaign) { c.Variants = nil }, ErrVariantsRequired},
		{"five variants", func(c *Campaign) {
			c.Variants = []Variant{textVariant("A", "a"), textVariant("B", "b"), textVariant("C", "c"), textVariant("D", "d"), textVariant("E", "e")}
		}, ErrTooManyVariants},
		{"four named variants", func(c *Campaign) {
			c.Variants = []Variant{textVariant("A", "a"), textVariant("B", "b"), textVariant("C", "c"), cardVariant("D")}
		}, nil},
		{"ab variant needs a name", func(c *Campaign) {
			c.Variants = []Variant{textVariant("A", "a"), textVariant(" ", "b")}
		}, ErrVariantNameRequired},
		{"ab variant names unique", func(c *Campaign) {
			c.Variants = []Variant{textVariant("Curta", "a"), textVariant("curta ", "b")}
		}, ErrVariantNameDuplicate},
		{"invalid content", func(c *Campaign) { c.Variants = []Variant{textVariant("", "")} }, rcs.ErrContentEmpty},
		{"missing binding", func(c *Campaign) { c.Variants = []Variant{textVariant("", "Oi {{1}} {{2}}", firstName())} }, campaign.ErrBindingsMismatch},
		{"extra binding", func(c *Campaign) { c.Variants = []Variant{textVariant("", "Oi", firstName())} }, campaign.ErrBindingsMismatch},
		{"custom field binding", func(c *Campaign) {
			c.Variants = []Variant{textVariant("", "Plano {{1}}", campaign.VariableBinding{Source: campaign.CustomBinding("plano")})}
		}, nil},
		{"unknown custom field", func(c *Campaign) {
			c.Variants = []Variant{textVariant("", "Plano {{1}}", campaign.VariableBinding{Source: campaign.CustomBinding("nada")})}
		}, campaign.ErrBindingFieldUnknown},
		{"sensitive custom field", func(c *Campaign) {
			c.Variants = []Variant{textVariant("", "CPF {{1}}", campaign.VariableBinding{Source: campaign.CustomBinding("cpf")})}
		}, campaign.ErrBindingSensitive},
		{"whatsapp fallback", func(c *Campaign) {
			c.Fallback.WhatsApp = &WhatsAppFallback{BusinessPhoneID: "p1", TemplateID: "t1", BodyBindings: []campaign.VariableBinding{firstName()}}
		}, nil},
		{"whatsapp fallback without template", func(c *Campaign) {
			c.Fallback.WhatsApp = &WhatsAppFallback{BusinessPhoneID: "p1"}
		}, ErrWhatsAppFallbackIncomplete},
		{"whatsapp fallback without phone", func(c *Campaign) {
			c.Fallback.WhatsApp = &WhatsAppFallback{TemplateID: "t1"}
		}, ErrWhatsAppFallbackIncomplete},
		{"whatsapp fallback bad binding", func(c *Campaign) {
			c.Fallback.WhatsApp = &WhatsAppFallback{BusinessPhoneID: "p1", TemplateID: "t1", HeaderBindings: []campaign.VariableBinding{{Source: "lead.cpf"}}}
		}, campaign.ErrBindingUnknown},
		{"sms fallback", func(c *Campaign) {
			c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: "Oi {{1}}, veja a oferta", Bindings: []campaign.VariableBinding{firstName()}}
		}, nil},
		{"sms fallback needs a route", func(c *Campaign) { c.Fallback.SMS = &SMSFallback{Text: "oi"} }, ErrSMSRouteRequired},
		{"sms fallback empty", func(c *Campaign) { c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: " "} }, sms.ErrTextEmpty},
		{"sms fallback over eight parts", func(c *Campaign) {
			c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: strings.Repeat("a", 8*153+1)}
		}, sms.ErrTextTooLong},
		{"sms fallback binding mismatch", func(c *Campaign) { c.Fallback.SMS = &SMSFallback{RouteID: "r1", Text: "Oi {{1}}"} }, campaign.ErrBindingsMismatch},
		{"unknown channel", func(c *Campaign) { c.Channel = "email" }, ErrChannelInvalid},
		{"no channel", func(c *Campaign) { c.Channel = "" }, ErrChannelInvalid},
		{"rcs variant with sms text", func(c *Campaign) { c.Variants[0].Text = "oi" }, ErrVariantChannelMismatch},
		{"schedule in the future", func(c *Campaign) { c.ScheduledAt = &future }, nil},
		{"schedule in the past", func(c *Campaign) { c.ScheduledAt = &past }, ErrScheduleInPast},
		{"schedule at now", func(c *Campaign) { now := testNow; c.ScheduledAt = &now }, ErrScheduleInPast},
		{"past schedule of a launched campaign", func(c *Campaign) { c.Status = StatusRunning; c.ScheduledAt = &past }, nil},
		{"scheduled without a time", func(c *Campaign) { c.Status = StatusScheduled }, ErrScheduleMissing},
		{"invalid recurrence", func(c *Campaign) {
			c.Recurrence = &Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Timezone: saoPaulo}
		}, ErrRecurrenceWeekdays},
		{"ended recurrence", func(c *Campaign) {
			c.Recurrence = &Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo, Until: until(t, "2026-10-20")}
		}, ErrRecurrenceEnded},
		{"invalid audience", func(c *Campaign) {
			c.Audience = selection.Selection{Mode: selection.ModeIDs}
		}, ErrAudienceInvalid},
		{"no audience yet", func(c *Campaign) { c.Audience = selection.Selection{} }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCampaign()
			tc.mutate(&c)
			if err := c.Validate(testNow, defs); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestFallbackPlan(t *testing.T) {
	wa := &WhatsAppFallback{BusinessPhoneID: "p", TemplateID: "t"}
	sms := &SMSFallback{RouteID: "r1", Text: "oi"}
	cases := []struct {
		name string
		f    Fallback
		want carriermessaging.FallbackPlan
	}{
		{"none", Fallback{}, carriermessaging.FallbackPlan{}},
		{"whatsapp", Fallback{WhatsApp: wa}, carriermessaging.FallbackPlan{WhatsApp: true}},
		{"sms", Fallback{SMS: sms}, carriermessaging.FallbackPlan{SMS: true}},
		{"both", Fallback{WhatsApp: wa, SMS: sms}, carriermessaging.FallbackPlan{WhatsApp: true, SMS: true}},
	}
	for _, tc := range cases {
		if got := tc.f.Plan(); got != tc.want {
			t.Errorf("%s: got %+v", tc.name, got)
		}
	}
}

func TestWhatsAppFallbackPlanBindings(t *testing.T) {
	wa := WhatsAppFallback{BusinessPhoneID: "p", TemplateID: "t", BodyBindings: []campaign.VariableBinding{firstName()}}
	if _, _, err := wa.PlanBindings(1, 0, nil); err != nil {
		t.Fatalf("matching template: %v", err)
	}
	if _, _, err := wa.PlanBindings(2, 0, nil); !errors.Is(err, campaign.ErrBindingsMismatch) {
		t.Fatalf("body mismatch err = %v", err)
	}
	if _, _, err := wa.PlanBindings(1, 1, nil); !errors.Is(err, campaign.ErrBindingsMismatch) {
		t.Fatalf("header mismatch err = %v", err)
	}
}

func TestCampaignABTest(t *testing.T) {
	c := validCampaign()
	if c.ABTest() {
		t.Fatal("one variant is not a test")
	}
	c.Variants = append(c.Variants, cardVariant("B"))
	if !c.ABTest() {
		t.Fatal("two variants are a test")
	}
}

func TestCampaignReadyToLaunch(t *testing.T) {
	c := validCampaign()
	if err := c.ReadyToLaunch(testNow, nil); err != nil {
		t.Fatalf("ready: %v", err)
	}
	c.Audience = selection.Selection{}
	if err := c.ReadyToLaunch(testNow, nil); !errors.Is(err, ErrAudienceRequired) {
		t.Fatalf("no audience err = %v", err)
	}
	c = validCampaign()
	c.Name = ""
	if err := c.ReadyToLaunch(testNow, nil); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("invalid err = %v", err)
	}
}

func TestCampaignSchedule(t *testing.T) {
	sp := mustLocation(t, saoPaulo)
	inTwoHours := testNow.Add(2 * time.Hour)
	startNextMonth := time.Date(2026, 11, 10, 0, 0, 0, 0, sp)
	daily := &Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo}

	cases := []struct {
		name   string
		mutate func(c *Campaign)
		want   time.Time
		err    error
	}{
		{"one off at its time", func(c *Campaign) { c.ScheduledAt = &inTwoHours }, inTwoHours, nil},
		{"one off needs a time", func(c *Campaign) {}, time.Time{}, ErrScheduleMissing},
		{"recurrence picks the first occurrence", func(c *Campaign) { c.Recurrence = daily }, time.Date(2026, 10, 22, 9, 0, 0, 0, sp), nil},
		{"recurrence starts at a chosen date", func(c *Campaign) { c.Recurrence = daily; c.ScheduledAt = &startNextMonth }, time.Date(2026, 11, 10, 9, 0, 0, 0, sp), nil},
		{"recurrence start on an occurrence keeps it", func(c *Campaign) {
			first := time.Date(2026, 11, 10, 9, 0, 0, 0, sp)
			c.Recurrence = daily
			c.ScheduledAt = &first
		}, time.Date(2026, 11, 10, 9, 0, 0, 0, sp), nil},
		{"running cannot schedule", func(c *Campaign) { c.Status = StatusRunning; c.ScheduledAt = &inTwoHours }, time.Time{}, ErrNotEditable},
		{"no audience", func(c *Campaign) { c.ScheduledAt = &inTwoHours; c.Audience = selection.Selection{} }, time.Time{}, ErrAudienceRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCampaign()
			tc.mutate(&c)
			got, err := c.Schedule(testNow, nil)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if got.Status != StatusScheduled || got.ScheduledAt == nil || !got.ScheduledAt.Equal(tc.want) {
				t.Fatalf("got %s at %v, want %s", got.Status, got.ScheduledAt, tc.want)
			}
		})
	}
}

func TestCampaignAfterRun(t *testing.T) {
	sp := mustLocation(t, saoPaulo)
	occurrence := time.Date(2026, 10, 22, 9, 0, 0, 0, sp)
	daily := &Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo}

	cases := []struct {
		name       string
		recurrence *Recurrence
		status     Status
		now        time.Time
		wantStatus Status
		wantNext   *time.Time
		err        error
	}{
		{"one off completes", nil, StatusRunning, occurrence.Add(time.Hour), StatusCompleted, nil, nil},
		{"recurring waits for the next day", daily, StatusRunning, occurrence.Add(time.Hour), StatusScheduled, ptr(time.Date(2026, 10, 23, 9, 0, 0, 0, sp)), nil},
		{"recurring skips occurrences missed while running", daily, StatusRunning, time.Date(2026, 10, 23, 10, 0, 0, 0, sp), StatusScheduled, ptr(time.Date(2026, 10, 24, 9, 0, 0, 0, sp)), nil},
		{"recurrence past until completes", &Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo, Until: until(t, "2026-10-22")}, StatusRunning, occurrence.Add(time.Hour), StatusCompleted, nil, nil},
		{"paused campaign cannot finish a run", daily, StatusPaused, occurrence.Add(time.Hour), "", nil, campaign.ErrNotRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCampaign()
			c.Status = tc.status
			c.Recurrence = tc.recurrence
			got, err := c.AfterRun(occurrence, tc.now)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status %s, want %s", got.Status, tc.wantStatus)
			}
			if (got.ScheduledAt == nil) != (tc.wantNext == nil) || (tc.wantNext != nil && !got.ScheduledAt.Equal(*tc.wantNext)) {
				t.Fatalf("next %v, want %v", got.ScheduledAt, tc.wantNext)
			}
		})
	}
}

func TestPickVariant(t *testing.T) {
	if got := PickVariant("lead-1", 1); got != 0 {
		t.Fatalf("single variant = %d", got)
	}
	if got := PickVariant("lead-1", 0); got != 0 {
		t.Fatalf("no variants = %d", got)
	}
	seen := map[int]int{}
	for i := range 4000 {
		key := "lead-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + time.Duration(i).String()
		idx := PickVariant(key, 4)
		if idx < 0 || idx > 3 {
			t.Fatalf("index %d out of range", idx)
		}
		if PickVariant(key, 4) != idx {
			t.Fatal("pick must be stable")
		}
		seen[idx]++
	}
	for i := range 4 {
		if seen[i] < 700 {
			t.Fatalf("variant %d got %d of 4000, split is not even", i, seen[i])
		}
	}
	c := validCampaign()
	c.Variants = append(c.Variants, cardVariant("B"))
	if c.VariantFor("lead-9") != PickVariant("lead-9", 2) {
		t.Fatal("campaign pick must use the shared split")
	}
}

func ptr[T any](v T) *T { return &v }
