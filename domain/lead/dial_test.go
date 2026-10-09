package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func dialableLead() *Lead {
	return &Lead{
		ID: "lead-1", WorkspaceID: "ws-1", Number: "558494409684", Name: "Maria",
		Phones: []ContactPhone{
			{Number: "551133334444", Label: PhoneLandline},
			{Number: "5584991112222", Label: PhoneWork},
		},
	}
}

func TestCheckDial(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	direct := DialContext{Purpose: DialDirect}
	callList := DialContext{Purpose: DialCallList}
	cases := []struct {
		name   string
		lead   func() *Lead
		number string
		ctx    DialContext
		want   DialRefusalReason
	}{
		{name: "the identity number in its dialable form", lead: dialableLead, number: "5584994409684", ctx: direct},
		{name: "the identity number without the ninth digit", lead: dialableLead, number: "558494409684", ctx: direct},
		{name: "a contact landline", lead: dialableLead, number: "551133334444", ctx: direct},
		{name: "a contact mobile in the other ninth digit form", lead: dialableLead, number: "558491112222", ctx: direct},
		{name: "a lead that does not exist", lead: func() *Lead { return nil }, number: "5584994409684", ctx: direct, want: DialRefusedNotFound},
		{name: "a number the lead does not hold", lead: dialableLead, number: "5511987654321", ctx: direct, want: DialRefusedNumberNotHeld},
		{name: "an empty number", lead: dialableLead, number: " ", ctx: direct, want: DialRefusedNumberNotHeld},
		{name: "an extension is never a lead number", lead: dialableLead, number: "100", ctx: direct, want: DialRefusedNumberNotHeld},
		{name: "a blocked lead is refused before the number is looked at", lead: func() *Lead {
			l := dialableLead()
			l.Blocked = true
			return l
		}, number: "5511987654321", ctx: direct, want: DialRefusedBlocked},
		{name: "a purpose nobody declared refuses", lead: dialableLead, number: "5584994409684", ctx: DialContext{}, want: DialRefusedUnknownPurpose},
		{name: "an opted out lead can still take a direct call", lead: func() *Lead {
			l := dialableLead()
			l.OptedOutAt = &optedOut
			return l
		}, number: "5584994409684", ctx: direct},
		{name: "an opted out lead is never called from a call list", lead: func() *Lead {
			l := dialableLead()
			l.OptedOutAt = &optedOut
			return l
		}, number: "5584994409684", ctx: callList, want: DialRefusedOptedOut},
		{name: "a call list calls a lead without recorded consent", lead: dialableLead, number: "5584994409684", ctx: callList},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckDial(tc.lead(), tc.number, tc.ctx)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckDial = %v, want allowed", err)
				}
				return
			}
			var refusal *DialRefusal
			if !errors.As(err, &refusal) || refusal.Reason != tc.want {
				t.Fatalf("CheckDial = %v, want refusal %q", err, tc.want)
			}
		})
	}
}

func TestDialRefusalsUnwrapToTheirSocketCodes(t *testing.T) {
	blocked := &DialRefusal{Reason: DialRefusedBlocked}
	if !errors.Is(blocked, ErrLeadDialBlocked) || errors.Is(blocked, ErrLeadNotDialable) {
		t.Fatalf("blocked must read as blocked only: %v", blocked)
	}
	for _, reason := range []DialRefusalReason{DialRefusedNotFound, DialRefusedNumberNotHeld, DialRefusedOptedOut, DialRefusedUnknownPurpose, DialRefusedNoNumber, DialRefusedInvalidNumber} {
		refusal := &DialRefusal{Reason: reason}
		if !errors.Is(refusal, ErrLeadNotDialable) || errors.Is(refusal, ErrLeadDialBlocked) {
			t.Errorf("%s must read as not dialable", reason)
		}
	}
	if ErrorCode(blocked) != "lead_blocked" || ErrorCode(&DialRefusal{Reason: DialRefusedOptedOut}) != "lead_not_dialable" {
		t.Fatalf("codes = %q / %q", ErrorCode(blocked), ErrorCode(&DialRefusal{Reason: DialRefusedOptedOut}))
	}
}

func TestDialNumbersListTheIdentityFirstInTheDialableForm(t *testing.T) {
	l := dialableLead()
	l.Phones[0].ID, l.Phones[1].ID = "phone-landline", "phone-work"
	l.Phones = append(l.Phones, ContactPhone{ID: "phone-same-as-whatsapp", Number: "5584994409684", Label: PhoneOther})
	want := []DialNumber{
		{Number: "5584994409684", Identity: true},
		{Number: "551133334444", Label: PhoneLandline, PhoneID: "phone-landline"},
		{Number: "5584991112222", Label: PhoneWork, PhoneID: "phone-work"},
	}
	if got := l.DialNumbers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DialNumbers = %+v, want %+v", got, want)
	}
	if got := (&Lead{Name: "Sem telefone"}).DialNumbers(); len(got) != 0 {
		t.Fatalf("a lead without numbers has nothing to dial, got %+v", got)
	}
}

func TestCheckLeadDialAlsoAppliesTheRuleToTheNumberIdentityHolder(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	relative := func() *Lead {
		return &Lead{ID: "lead-2", WorkspaceID: "ws-1", Number: "5511987654321", Phones: []ContactPhone{{Number: "5584994409684", Label: PhoneMobile}}}
	}
	blockedOwner := dialableLead()
	blockedOwner.Blocked = true
	quietOwner := dialableLead()
	quietOwner.OptedOutAt = &optedOut
	cases := []struct {
		name       string
		lead       *Lead
		number     string
		identities []*Lead
		ctx        DialContext
		want       DialRefusalReason
	}{
		{name: "a contact phone nobody else holds as WhatsApp", lead: relative(), number: "5584994409684", ctx: DialContext{Purpose: DialDirect}},
		{name: "a contact phone that is a free lead's WhatsApp", lead: relative(), number: "5584994409684", identities: []*Lead{dialableLead()}, ctx: DialContext{Purpose: DialDirect}},
		{name: "a contact phone that is a blocked lead's WhatsApp", lead: relative(), number: "5584994409684", identities: []*Lead{blockedOwner}, ctx: DialContext{Purpose: DialDirect}, want: DialRefusedBlocked},
		{name: "a contact phone of an opted out WhatsApp takes a direct call", lead: relative(), number: "558494409684", identities: []*Lead{quietOwner}, ctx: DialContext{Purpose: DialDirect}},
		{name: "a contact phone of an opted out WhatsApp is not called from a call list", lead: relative(), number: "558494409684", identities: []*Lead{quietOwner}, ctx: DialContext{Purpose: DialCallList}, want: DialRefusedOptedOut},
		{name: "the lead's own WhatsApp is checked once", lead: dialableLead(), number: "5584994409684", identities: []*Lead{dialableLead()}, ctx: DialContext{Purpose: DialDirect}},
		{name: "the named lead is refused before its holder is looked at", lead: relative(), number: "5500000000000", identities: []*Lead{blockedOwner}, ctx: DialContext{Purpose: DialDirect}, want: DialRefusedNumberNotHeld},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckLeadDial(tc.lead, tc.number, tc.identities, tc.ctx)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckLeadDial = %v, want allowed", err)
				}
				return
			}
			var refusal *DialRefusal
			if !errors.As(err, &refusal) || refusal.Reason != tc.want {
				t.Fatalf("CheckLeadDial = %v, want refusal %q", err, tc.want)
			}
		})
	}
}

func TestCheckNumberDialNamesTheIdentityHolderOnlyWhenItMayBeCalled(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	relative := &Lead{ID: "lead-2", WorkspaceID: "ws-1", Number: "5511987654321", Phones: []ContactPhone{{Number: "5584994409684", Label: PhoneMobile}}}
	blocked := dialableLead()
	blocked.Blocked = true
	quiet := dialableLead()
	quiet.OptedOutAt = &optedOut
	direct := DialContext{Purpose: DialDirect}
	cases := []struct {
		name       string
		identities []*Lead
		ctx        DialContext
		holder     string
		want       DialRefusalReason
	}{
		{name: "the WhatsApp owner", identities: []*Lead{relative, dialableLead()}, ctx: direct, holder: "lead-1"},
		{name: "a contact phone alone names nobody", identities: []*Lead{relative}, ctx: direct},
		{name: "no lead at all", ctx: direct},
		{name: "a blocked owner", identities: []*Lead{blocked}, ctx: direct, want: DialRefusedBlocked},
		{name: "an opted out owner takes a direct call", identities: []*Lead{quiet}, ctx: direct, holder: "lead-1"},
		{name: "an opted out owner is not called from a call list", identities: []*Lead{quiet}, ctx: DialContext{Purpose: DialCallList}, want: DialRefusedOptedOut},
		{name: "an undeclared purpose", identities: []*Lead{dialableLead()}, ctx: DialContext{}, want: DialRefusedUnknownPurpose},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			holder, err := CheckNumberDial("5584994409684", tc.identities, tc.ctx)
			if tc.want != "" {
				var refusal *DialRefusal
				if holder != nil || !errors.As(err, &refusal) || refusal.Reason != tc.want {
					t.Fatalf("CheckNumberDial = %+v, %v, want refusal %q", holder, err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckNumberDial = %v, want allowed", err)
			}
			got := ""
			if holder != nil {
				got = holder.ID
			}
			if got != tc.holder {
				t.Fatalf("holder = %q, want %q", got, tc.holder)
			}
		})
	}
}

func TestPlanDialGivesEachNumberItsVerdictAndPicksTheFirstCallable(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	direct := DialContext{Purpose: DialDirect}
	blockedOther := &Lead{ID: "lead-7", WorkspaceID: "ws-1", Number: "551133334444", Blocked: true}
	quiet := dialableLead()
	quiet.OptedOutAt = &optedOut
	blocked := dialableLead()
	blocked.Blocked = true
	numbers := func(refusals ...DialRefusalReason) []PlannedDialNumber {
		all := []PlannedDialNumber{
			{DialNumber: DialNumber{Number: "5584994409684", Identity: true}},
			{DialNumber: DialNumber{Number: "551133334444", Label: PhoneLandline}},
			{DialNumber: DialNumber{Number: "5584991112222", Label: PhoneWork}},
		}
		for i, reason := range refusals {
			all[i].Refusal = reason
		}
		return all
	}
	cases := []struct {
		name       string
		lead       *Lead
		identities []*Lead
		ctx        DialContext
		want       DialPlan
	}{
		{name: "every number callable", lead: dialableLead(), ctx: direct, want: DialPlan{Numbers: numbers(), Callable: "5584994409684"}},
		{name: "a contact phone held as a blocked lead's WhatsApp", lead: dialableLead(), identities: []*Lead{blockedOther}, ctx: direct, want: DialPlan{Numbers: numbers("", DialRefusedBlocked), Callable: "5584994409684"}},
		{name: "a blocked lead", lead: blocked, ctx: direct, want: DialPlan{Numbers: numbers(DialRefusedBlocked, DialRefusedBlocked, DialRefusedBlocked), Refusal: DialRefusedBlocked}},
		{name: "opted out on a direct call", lead: quiet, ctx: direct, want: DialPlan{Numbers: numbers(), Callable: "5584994409684"}},
		{name: "opted out on a call list", lead: quiet, ctx: DialContext{Purpose: DialCallList}, want: DialPlan{Numbers: numbers(DialRefusedOptedOut, DialRefusedOptedOut, DialRefusedOptedOut), Refusal: DialRefusedOptedOut}},
		{name: "no numbers", lead: &Lead{ID: "lead-4", WorkspaceID: "ws-1"}, ctx: direct, want: DialPlan{Numbers: []PlannedDialNumber{}, Refusal: DialRefusedNoNumber}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lead.PlanDial(tc.identities, tc.ctx); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("PlanDial = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestARefusedNumberHandsTheCallToTheNextOne(t *testing.T) {
	plan := dialableLead().PlanDial(nil, DialContext{Purpose: DialDirect})
	plan.Refuse("5584994409684", DialRefusedInvalidNumber)
	if plan.Callable != "551133334444" || plan.Refusal != "" || plan.Numbers[0].Refusal != DialRefusedInvalidNumber {
		t.Fatalf("plan = %+v, want the landline next", plan)
	}
	plan.Refuse("551133334444", DialRefusedInvalidNumber)
	plan.Refuse("5584991112222", DialRefusedInvalidNumber)
	if plan.Callable != "" || plan.Refusal != DialRefusedInvalidNumber {
		t.Fatalf("plan = %+v, want nothing callable and the first number's reason", plan)
	}
	plan.Refuse("5584994409684", DialRefusedBlocked)
	if plan.Numbers[0].Refusal != DialRefusedInvalidNumber {
		t.Fatal("a number keeps its first refusal")
	}
}
