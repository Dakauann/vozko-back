package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
)

type dialLeadBook struct {
	leads       map[string]*lead.Lead
	loadErr     error
	holders     []*lead.Lead
	holdersErr  error
	lookedUpFor []string
}

func (b *dialLeadBook) LoadForDial(_ context.Context, workspaceID, leadID string) (*lead.Lead, error) {
	if b.loadErr != nil {
		return nil, b.loadErr
	}
	l, ok := b.leads[leadID]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	return l, nil
}

func (b *dialLeadBook) FindIdentities(_ context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	b.lookedUpFor = append(b.lookedUpFor, numbers...)
	if b.holdersErr != nil {
		return nil, b.holdersErr
	}
	var out []*lead.Lead
	for _, l := range b.holders {
		if l.WorkspaceID == workspaceID {
			out = append(out, l)
		}
	}
	return out, nil
}

type stubPlanner struct {
	plan   *sip_trunk.CallPlan
	err    error
	errFor map[string]error
	asked  []sip_trunk.CallPlanInput
	lined  []sip_trunk.CallPlanInput
}

func (p *stubPlanner) Lines(_ context.Context, input sip_trunk.CallPlanInput) ([]sip_trunk.TrunkChoice, error) {
	p.lined = append(p.lined, input)
	if p.err != nil || p.plan == nil {
		return nil, p.err
	}
	return p.plan.Trunks, nil
}

func (p *stubPlanner) Plan(_ context.Context, input sip_trunk.CallPlanInput) (*sip_trunk.CallPlan, error) {
	p.asked = append(p.asked, input)
	if err, refused := p.errFor[input.PhoneNumber]; refused {
		return nil, err
	}
	return p.plan, p.err
}

func dialMaria() *lead.Lead {
	return &lead.Lead{
		ID: "lead-1", WorkspaceID: cmdWorkspace, Number: "558494409684", Name: "Maria",
		Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}},
	}
}

func dialTargetsWith(t *testing.T, book *dialLeadBook, perms fakePermissions, planner *stubPlanner) *DialTargets {
	t.Helper()
	targets, err := NewDialTargets(DialTargetDeps{Leads: book, Permissions: perms, Planner: planner, Lines: planner})
	if err != nil {
		t.Fatal(err)
	}
	return targets
}

func refusalOf(err error) lead.DialRefusalReason {
	var refusal *lead.DialRefusal
	if errors.As(err, &refusal) {
		return refusal.Reason
	}
	return ""
}

func TestDialTargetsRefuseToBuildWithoutTheirPorts(t *testing.T) {
	full := DialTargetDeps{Leads: &dialLeadBook{}, Permissions: fakePermissions{}, Planner: &stubPlanner{}, Lines: &stubPlanner{}}
	for name, deps := range map[string]DialTargetDeps{
		"no leads":       {Permissions: full.Permissions, Planner: full.Planner, Lines: full.Lines},
		"no permissions": {Leads: full.Leads, Planner: full.Planner, Lines: full.Lines},
		"no planner":     {Leads: full.Leads, Permissions: full.Permissions, Lines: full.Lines},
		"no lines":       {Leads: full.Leads, Permissions: full.Permissions, Planner: full.Planner},
	} {
		if _, err := NewDialTargets(deps); err == nil {
			t.Errorf("%s: built without a port", name)
		}
	}
}

func TestCheckLeadAppliesTheDialRule(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	quiet := dialMaria()
	quiet.ID, quiet.OptedOutAt = "lead-2", &optedOut
	book := &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria(), "lead-2": quiet}}
	blockedOwner := &lead.Lead{ID: "lead-5", WorkspaceID: cmdWorkspace, Number: "551133334444", Blocked: true}
	sharedPhone := &dialLeadBook{leads: book.leads, holders: []*lead.Lead{blockedOwner}}
	cases := []struct {
		name string
		book *dialLeadBook
		dial callsession.LeadDial
		want lead.DialRefusalReason
		err  bool
	}{
		{name: "the identity", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialDirect}},
		{name: "a contact phone", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "551133334444", Purpose: lead.DialDirect}},
		{name: "another number", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "5511987654321", Purpose: lead.DialDirect}, want: lead.DialRefusedNumberNotHeld},
		{name: "a lead of another workspace", book: book, dial: callsession.LeadDial{WorkspaceID: "ws-2", LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialDirect}, want: lead.DialRefusedNotFound},
		{name: "an unknown lead", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "nope", Number: "5584994409684", Purpose: lead.DialDirect}, want: lead.DialRefusedNotFound},
		{name: "an opted out lead takes a direct call", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-2", Number: "5584994409684", Purpose: lead.DialDirect}},
		{name: "an opted out lead is not called from a call list", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-2", Number: "5584994409684", Purpose: lead.DialCallList}, want: lead.DialRefusedOptedOut},
		{name: "a call list calls a lead without recorded consent", book: book, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialCallList}},
		{name: "an unreadable lead refuses", book: &dialLeadBook{loadErr: errors.New("database down")}, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialDirect}, err: true},
		{name: "a contact phone that is a blocked lead's WhatsApp", book: sharedPhone, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "551133334444", Purpose: lead.DialDirect}, want: lead.DialRefusedBlocked},
		{name: "an unreadable identity lookup refuses", book: &dialLeadBook{leads: book.leads, holdersErr: errors.New("database down")}, dial: callsession.LeadDial{WorkspaceID: cmdWorkspace, LeadID: "lead-1", Number: "551133334444", Purpose: lead.DialDirect}, err: true},
		{name: "no workspace refuses", book: book, dial: callsession.LeadDial{LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialDirect}, err: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := dialTargetsWith(t, tc.book, fakePermissions{}, &stubPlanner{}).CheckLead(context.Background(), tc.dial)
			switch {
			case tc.err:
				if err == nil || refusalOf(err) != "" {
					t.Fatalf("CheckLead = %v, want a plain error", err)
				}
			case tc.want == "":
				if err != nil {
					t.Fatalf("CheckLead = %v, want allowed", err)
				}
			default:
				if refusalOf(err) != tc.want {
					t.Fatalf("CheckLead = %v, want %q", err, tc.want)
				}
			}
		})
	}
}

func TestIdentityLeadNamesTheWhatsAppOwnerAndRefusesItWhenBlocked(t *testing.T) {
	owner := dialMaria()
	relative := &lead.Lead{ID: "lead-3", WorkspaceID: cmdWorkspace, Number: "5511987654321", Phones: []lead.ContactPhone{{Number: "5584994409684", Label: lead.PhoneMobile}}}
	blocked := dialMaria()
	blocked.Blocked = true

	book := &dialLeadBook{holders: []*lead.Lead{relative, owner}}
	id, err := dialTargetsWith(t, book, fakePermissions{}, &stubPlanner{}).IdentityLead(context.Background(), cmdWorkspace, "5584994409684")
	if err != nil || id != "lead-1" {
		t.Fatalf("IdentityLead = %q, %v, want the identity holder", id, err)
	}
	if !reflect.DeepEqual(book.lookedUpFor, []string{"5584994409684"}) {
		t.Fatalf("looked up %v", book.lookedUpFor)
	}

	id, err = dialTargetsWith(t, &dialLeadBook{holders: []*lead.Lead{relative}}, fakePermissions{}, &stubPlanner{}).IdentityLead(context.Background(), cmdWorkspace, "5584994409684")
	if err != nil || id != "" {
		t.Fatalf("a contact phone alone names nobody, got %q, %v", id, err)
	}

	_, err = dialTargetsWith(t, &dialLeadBook{holders: []*lead.Lead{blocked}}, fakePermissions{}, &stubPlanner{}).IdentityLead(context.Background(), cmdWorkspace, "5584994409684")
	if !errors.Is(err, lead.ErrLeadDialBlocked) {
		t.Fatalf("IdentityLead = %v, want the blocked refusal", err)
	}

	_, err = dialTargetsWith(t, &dialLeadBook{holdersErr: errors.New("database down")}, fakePermissions{}, &stubPlanner{}).IdentityLead(context.Background(), cmdWorkspace, "5584994409684")
	if err == nil {
		t.Fatal("a failed lookup must refuse the call, not dial past the block")
	}

	quiet := dialMaria()
	quiet.OptedOutAt = &time.Time{}
	id, err = dialTargetsWith(t, &dialLeadBook{holders: []*lead.Lead{quiet}}, fakePermissions{}, &stubPlanner{}).IdentityLead(context.Background(), cmdWorkspace, "5584994409684")
	if err != nil || id != quiet.ID {
		t.Fatalf("IdentityLead = (%q, %v), an opted out lead takes a direct call", id, err)
	}
}

func TestPlanListsTheLeadNumbersAndTheLinesThatCanCallThem(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	quiet := dialMaria()
	quiet.OptedOutAt = &optedOut
	planner := &stubPlanner{plan: &sip_trunk.CallPlan{PhoneNumber: "5584994409684", Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}}
	readers := fakePermissions{"leads:read": true}
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}

	plan, err := dialTargetsWith(t, &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria()}}, readers, planner).Plan(context.Background(), actor, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := &LeadDialPlan{
		LeadID: "lead-1",
		DialPlan: lead.DialPlan{
			Numbers: []lead.PlannedDialNumber{
				{DialNumber: lead.DialNumber{Number: "5584994409684", Identity: true}},
				{DialNumber: lead.DialNumber{Number: "551133334444", Label: lead.PhoneLandline}},
			},
			Callable: "5584994409684",
		},
		Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v, want %+v", plan, want)
	}
	if len(planner.asked) != 1 || planner.asked[0] != (sip_trunk.CallPlanInput{WorkspaceID: cmdWorkspace, UserID: cmdUser, PhoneNumber: "5584994409684"}) {
		t.Fatalf("planner asked %+v", planner.asked)
	}

	plan, err = dialTargetsWith(t, &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": quiet}}, readers, &stubPlanner{plan: planner.plan}).Plan(context.Background(), actor, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Refusal != "" || plan.Callable != "5584994409684" || len(plan.Trunks) != 1 {
		t.Fatalf("plan = %+v, an opted out lead takes a direct call", plan)
	}

	sharedPhone := &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria()}, holders: []*lead.Lead{dialMaria()}}
	plan, err = dialTargetsWith(t, sharedPhone, readers, &stubPlanner{plan: planner.plan}).Plan(context.Background(), actor, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Numbers[0].Refusal != "" || plan.Callable != "5584994409684" {
		t.Fatalf("plan = %+v, the lead holds its own WhatsApp", plan)
	}
	if !reflect.DeepEqual(sharedPhone.lookedUpFor, []string{"5584994409684", "551133334444"}) {
		t.Fatalf("identities looked up for %v, want every number of the lead in one lookup", sharedPhone.lookedUpFor)
	}
}

func TestPlanRefusesAContactPhoneThatIsABlockedLeadsWhatsApp(t *testing.T) {
	blockedOwner := &lead.Lead{ID: "lead-5", WorkspaceID: cmdWorkspace, Number: "551133334444", Blocked: true}
	book := &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria()}, holders: []*lead.Lead{blockedOwner}}
	planner := &stubPlanner{plan: &sip_trunk.CallPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}}
	plan, err := dialTargetsWith(t, book, fakePermissions{"leads:read": true}, planner).Plan(context.Background(), Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Numbers[1].Refusal != lead.DialRefusedBlocked || plan.Numbers[0].Refusal != "" || plan.Refusal != "" {
		t.Fatalf("plan = %+v, want the shared landline refused as blocked", plan)
	}
}

func TestPlanSkipsANumberNoLineCanDial(t *testing.T) {
	readers := fakePermissions{"leads:read": true}
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}
	book := &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria()}}
	trunks := &sip_trunk.CallPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}

	planner := &stubPlanner{plan: trunks, errFor: map[string]error{"5584994409684": sip_trunk.ErrInvalidPhoneNumber}}
	plan, err := dialTargetsWith(t, book, readers, planner).Plan(context.Background(), actor, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Numbers[0].Refusal != lead.DialRefusedInvalidNumber || plan.Callable != "551133334444" || plan.Refusal != "" || len(plan.Trunks) != 1 {
		t.Fatalf("plan = %+v, want the landline planned after the invalid number", plan)
	}
	if len(planner.asked) != 2 || planner.asked[1].PhoneNumber != "551133334444" {
		t.Fatalf("planner asked %+v", planner.asked)
	}

	planner = &stubPlanner{err: sip_trunk.ErrInvalidPhoneNumber}
	plan, err = dialTargetsWith(t, book, readers, planner).Plan(context.Background(), actor, "lead-1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Refusal != lead.DialRefusedInvalidNumber || plan.Callable != "" || plan.TrunkRefusal != "" || len(plan.Trunks) != 0 || len(planner.asked) != 2 {
		t.Fatalf("plan = %+v after %d plans, want every number refused as invalid", plan, len(planner.asked))
	}
}

func TestPlanExplainsWhyALeadCannotBeCalled(t *testing.T) {
	readers := fakePermissions{"leads:read": true}
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}
	blocked := dialMaria()
	blocked.Blocked = true
	silent := &lead.Lead{ID: "lead-4", WorkspaceID: cmdWorkspace, Name: "Sem telefone"}
	cases := []struct {
		name          string
		lead          *lead.Lead
		planner       *stubPlanner
		refusal       lead.DialRefusalReason
		trunkRefusal  TrunkRefusal
		plannerCalled bool
	}{
		{name: "blocked", lead: blocked, planner: &stubPlanner{}, refusal: lead.DialRefusedBlocked},
		{name: "no numbers", lead: silent, planner: &stubPlanner{}, refusal: lead.DialRefusedNoNumber},
		{name: "no permission to call", lead: dialMaria(), planner: &stubPlanner{err: sip_trunk.ErrCallNotPermitted}, trunkRefusal: TrunkRefusedNotPermitted, plannerCalled: true},
		{name: "no line connected", lead: dialMaria(), planner: &stubPlanner{err: sip_trunk.ErrNoDialableTrunk}, trunkRefusal: TrunkRefusedNoneDialable, plannerCalled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			book := &dialLeadBook{leads: map[string]*lead.Lead{tc.lead.ID: tc.lead}}
			plan, err := dialTargetsWith(t, book, readers, tc.planner).Plan(context.Background(), actor, tc.lead.ID)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if plan.Refusal != tc.refusal || plan.TrunkRefusal != tc.trunkRefusal || len(plan.Trunks) != 0 {
				t.Fatalf("plan = %+v", plan)
			}
			if (len(tc.planner.asked) > 0) != tc.plannerCalled {
				t.Fatalf("planner asked %d times", len(tc.planner.asked))
			}
		})
	}
}

func TestPlanRefusesWhatTheCallerMayNotRead(t *testing.T) {
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}
	book := &dialLeadBook{leads: map[string]*lead.Lead{"lead-1": dialMaria()}}
	planner := &stubPlanner{plan: &sip_trunk.CallPlan{}}

	if _, err := dialTargetsWith(t, book, fakePermissions{}, planner).Plan(context.Background(), actor, "lead-1"); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("Plan without leads:read = %v, want forbidden", err)
	}
	readers := fakePermissions{"leads:read": true}
	if _, err := dialTargetsWith(t, book, readers, planner).Plan(context.Background(), Actor{UserID: cmdUser, WorkspaceID: "ws-2"}, "lead-1"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("Plan in another workspace = %v, want not found", err)
	}
	if _, err := dialTargetsWith(t, book, readers, &stubPlanner{err: errors.New("trunks down")}).Plan(context.Background(), actor, "lead-1"); err == nil {
		t.Fatal("a failed line lookup must fail the plan")
	}
	if len(planner.asked) != 0 {
		t.Fatal("no line may be planned for a refused caller")
	}
}

func TestLinesOfferTheTrunksThatCanDialWhenNoLeadIsNamed(t *testing.T) {
	planner := &stubPlanner{plan: &sip_trunk.CallPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}, {ID: "trunk-2", Name: "Filial"}}}}
	book := &dialLeadBook{}
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace, IsAdmin: true}
	plan, err := dialTargetsWith(t, book, fakePermissions{}, planner).Lines(context.Background(), actor)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if plan.LeadID != "" || len(plan.Numbers) != 0 || plan.Refusal != "" || plan.TrunkRefusal != "" || !reflect.DeepEqual(plan.Trunks, planner.plan.Trunks) {
		t.Fatalf("plan = %+v, want only the lines", plan)
	}
	if !reflect.DeepEqual(planner.lined, []sip_trunk.CallPlanInput{{WorkspaceID: cmdWorkspace, UserID: cmdUser, IsAdmin: true}}) || len(planner.asked) != 0 || len(book.lookedUpFor) != 0 {
		t.Fatalf("lines asked %+v, plans asked %+v, leads looked up %v", planner.lined, planner.asked, book.lookedUpFor)
	}
}

func TestLinesExplainWhyNoLineIsOffered(t *testing.T) {
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}
	cases := []struct {
		name    string
		err     error
		refusal TrunkRefusal
	}{
		{"no permission to call", sip_trunk.ErrCallNotPermitted, TrunkRefusedNotPermitted},
		{"no line connected", sip_trunk.ErrNoDialableTrunk, TrunkRefusedNoneDialable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := dialTargetsWith(t, &dialLeadBook{}, fakePermissions{}, &stubPlanner{err: tc.err}).Lines(context.Background(), actor)
			if err != nil || plan.TrunkRefusal != tc.refusal || len(plan.Trunks) != 0 {
				t.Fatalf("plan = %+v, err = %v", plan, err)
			}
		})
	}
	if _, err := dialTargetsWith(t, &dialLeadBook{}, fakePermissions{}, &stubPlanner{err: errors.New("trunks down")}).Lines(context.Background(), actor); err == nil {
		t.Fatal("a failed trunk lookup must not read as no line")
	}
	if _, err := dialTargetsWith(t, &dialLeadBook{}, fakePermissions{}, &stubPlanner{}).Lines(context.Background(), Actor{UserID: cmdUser}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("err = %v, want the workspace required", err)
	}
}
