package callsession_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
	lead_usecase "vozko/usecases/lead"
)

type stubDialTargets struct {
	checked    []callsession.LeadDial
	checkErr   error
	identities map[string]string
	identityOf []string
	identErr   error
}

func (s *stubDialTargets) CheckLead(_ context.Context, dial callsession.LeadDial) error {
	s.checked = append(s.checked, dial)
	return s.checkErr
}

func (s *stubDialTargets) IdentityLead(_ context.Context, workspaceID, number string) (string, error) {
	s.identityOf = append(s.identityOf, workspaceID+"|"+number)
	if s.identErr != nil {
		return "", s.identErr
	}
	return s.identities[number], nil
}

type stubCallListItems struct {
	checked  []callsession.CallListItemDial
	checkErr error
	stamped  int
}

func (s *stubCallListItems) CheckItemDial(_ context.Context, dial callsession.CallListItemDial) error {
	s.checked = append(s.checked, dial)
	return s.checkErr
}

func (s *stubCallListItems) StampLastCall(context.Context, callsession.CallListItemStamp) error {
	s.stamped++
	return nil
}

type countingAdmission struct {
	stubAdmission
	acquired int
}

func (s *countingAdmission) Acquire(ctx context.Context, input callsession.CallAdmissionInput) (*callsession.CallAdmissionLease, error) {
	s.acquired++
	return s.stubAdmission.Acquire(ctx, input)
}

func newCountingAdmission() *countingAdmission {
	return &countingAdmission{stubAdmission: stubAdmission{lease: &callsession.CallAdmissionLease{WorkspaceID: "ws-1"}}}
}

func TestACallToALeadIsCheckedAgainstTheLeadAndCarriesItsID(t *testing.T) {
	targets := &stubDialTargets{}
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, newCountingAdmission(), targets, nil)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "558494409684", TrunkID: "trunk-1", LeadID: " lead-1 ",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := callsession.LeadDial{WorkspaceID: "ws-1", LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialDirect}
	if len(targets.checked) != 1 || targets.checked[0] != want {
		t.Fatalf("checked = %+v, want %+v", targets.checked, want)
	}
	if len(targets.identityOf) != 0 {
		t.Fatal("a call that names its lead must not be resolved by number")
	}
	if res.LeadID != "lead-1" || res.TrunkID != "trunk-1" || res.CallListItemID != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestARefusedLeadIsNeverAdmittedNorDialed(t *testing.T) {
	for name, refusal := range map[string]error{
		"blocked":          &lead.DialRefusal{Reason: lead.DialRefusedBlocked},
		"number not held":  &lead.DialRefusal{Reason: lead.DialRefusedNumberNotHeld},
		"lookup failed":    errors.New("database down"),
		"lead not visible": &lead.DialRefusal{Reason: lead.DialRefusedNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			admission := newCountingAdmission()
			source := &stubCallSource{call: &stubCRMCall{}}
			uc := NewStartOutboundCallUseCase(source, admission, &stubDialTargets{checkErr: refusal}, nil)
			_, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
				WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1",
			})
			if !errors.Is(err, refusal) {
				t.Fatalf("Execute = %v, want %v", err, refusal)
			}
			if admission.acquired != 0 || source.lastIn.PhoneNumber != "" {
				t.Fatalf("acquired %d, dialed %+v; a refused lead must cost nothing", admission.acquired, source.lastIn)
			}
		})
	}
}

func TestWithoutDialTargetsNoCallIsPlaced(t *testing.T) {
	for name, input := range map[string]callsession.StartOutboundCallInput{
		"with a lead":    {WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1"},
		"without a lead": {WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1"},
	} {
		t.Run(name, func(t *testing.T) {
			admission := newCountingAdmission()
			source := &stubCallSource{call: &stubCRMCall{}}
			_, err := NewStartOutboundCallUseCase(source, admission, nil, nil).Execute(context.Background(), input)
			if !errors.Is(err, callsession.ErrLeadDialTargetsNotConfigured) {
				t.Fatalf("Execute = %v, want ErrLeadDialTargetsNotConfigured", err)
			}
			if admission.acquired != 0 || source.lastIn.PhoneNumber != "" {
				t.Fatal("nothing may be admitted or dialed without the lead check")
			}
		})
	}
}

func TestATypedNumberIsResolvedToItsIdentityLead(t *testing.T) {
	targets := &stubDialTargets{identities: map[string]string{"5584994409684": "lead-9"}}
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, newCountingAdmission(), targets, nil)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "(84) 9440-9684", WhatsAppPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(targets.identityOf) != 1 || targets.identityOf[0] != "ws-1|5584994409684" {
		t.Fatalf("identity lookups = %v, want the dialable number in the workspace", targets.identityOf)
	}
	if len(targets.checked) != 0 {
		t.Fatal("a typed number is not checked as a named lead")
	}
	if res.LeadID != "lead-9" || res.TrunkID != "" {
		t.Fatalf("result = %+v, want the identity lead and no trunk on a WhatsApp call", res)
	}
}

func TestATypedNumberOfABlockedIdentityIsStillRefused(t *testing.T) {
	blocked := &lead.DialRefusal{Reason: lead.DialRefusedBlocked}
	admission := newCountingAdmission()
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, admission, &stubDialTargets{identErr: blocked}, nil)

	_, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1",
	})
	if !errors.Is(err, lead.ErrLeadDialBlocked) {
		t.Fatalf("Execute = %v, want the blocked refusal", err)
	}
	if admission.acquired != 0 || source.lastIn.PhoneNumber != "" {
		t.Fatal("a blocked identity must not be admitted or dialed")
	}
}

func TestANumberWithoutALeadIsDialedWithoutOne(t *testing.T) {
	targets := &stubDialTargets{}
	source := &stubCallSource{call: &stubCRMCall{}}
	res, err := NewStartOutboundCallUseCase(source, newCountingAdmission(), targets, nil).Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "*100#", TrunkID: "trunk-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.LeadID != "" || source.lastIn.PhoneNumber != "*100#" {
		t.Fatalf("result = %+v, dialed %q", res, source.lastIn.PhoneNumber)
	}
}

func TestACallListItemIsAdmittedThenItsLeadCheckedForTheList(t *testing.T) {
	targets := &stubDialTargets{}
	items := &stubCallListItems{}
	source := &stubCallSource{call: &stubCRMCall{}}
	uc := NewStartOutboundCallUseCase(source, newCountingAdmission(), targets, items)

	res, err := uc.Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1", CallListItemID: "item-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantItem := callsession.CallListItemDial{WorkspaceID: "ws-1", UserID: "user-1", ItemID: "item-1", LeadID: "lead-1", Number: "5584994409684"}
	if len(items.checked) != 1 || items.checked[0] != wantItem {
		t.Fatalf("checked = %+v, want %+v", items.checked, wantItem)
	}
	wantLead := callsession.LeadDial{WorkspaceID: "ws-1", LeadID: "lead-1", Number: "5584994409684", Purpose: lead.DialCallList}
	if len(targets.checked) != 1 || targets.checked[0] != wantLead {
		t.Fatalf("checked = %+v, want %+v", targets.checked, wantLead)
	}
	if res.CallListItemID != "item-1" || res.LeadID != "lead-1" {
		t.Fatalf("result = %+v", res)
	}
}

func TestACallListItemIsRefusedWhenItCannotBeVouchedFor(t *testing.T) {
	notYours := errors.New("item reserved by someone else")
	cases := []struct {
		name  string
		input callsession.StartOutboundCallInput
		items callsession.CallListItems
		want  error
	}{
		{
			name:  "an item without its lead",
			input: callsession.StartOutboundCallInput{WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", CallListItemID: "item-1"},
			items: &stubCallListItems{},
			want:  callsession.ErrCallListItemNeedsLead,
		},
		{
			name:  "no call lists on this server",
			input: callsession.StartOutboundCallInput{WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1", CallListItemID: "item-1"},
			want:  callsession.ErrCallListsNotConfigured,
		},
		{
			name:  "an item the caller may not work",
			input: callsession.StartOutboundCallInput{WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1", CallListItemID: "item-1"},
			items: &stubCallListItems{checkErr: notYours},
			want:  notYours,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets := &stubDialTargets{}
			admission := newCountingAdmission()
			source := &stubCallSource{call: &stubCRMCall{}}
			_, err := NewStartOutboundCallUseCase(source, admission, targets, tc.items).Execute(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Execute = %v, want %v", err, tc.want)
			}
			if len(targets.checked) != 0 || admission.acquired != 0 || source.lastIn.PhoneNumber != "" {
				t.Fatal("a refused item must stop before the lead check, admission and dialing")
			}
		})
	}
}

func TestACallListItemWhoseLeadIsRefusedIsLeftAsItWas(t *testing.T) {
	items := &stubCallListItems{}
	admission := newCountingAdmission()
	source := &stubCallSource{call: &stubCRMCall{}}
	refused := &lead.DialRefusal{Reason: lead.DialRefusedOptedOut}
	_, err := NewStartOutboundCallUseCase(source, admission, &stubDialTargets{checkErr: refused}, items).Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1", LeadID: "lead-1", CallListItemID: "item-1",
	})
	if !errors.Is(err, lead.ErrLeadNotDialable) {
		t.Fatalf("Execute = %v, want the lead refusal", err)
	}
	if len(items.checked) != 1 || items.stamped != 0 || admission.acquired != 0 || source.lastIn.PhoneNumber != "" {
		t.Fatalf("checked %d, stamped %d, acquired %d, dialed %q; the item check reads only", len(items.checked), items.stamped, admission.acquired, source.lastIn.PhoneNumber)
	}
}

type identityDirectory struct{ identity *lead.Lead }

func (d identityDirectory) LoadForDial(context.Context, string, string) (*lead.Lead, error) {
	return nil, lead.ErrLeadNotFound
}

func (d identityDirectory) FindIdentities(_ context.Context, workspaceID string, _ []string) ([]*lead.Lead, error) {
	if d.identity.WorkspaceID != workspaceID {
		return nil, nil
	}
	return []*lead.Lead{d.identity}, nil
}

type everyPermission struct{}

func (everyPermission) HasWorkspacePermission(string, string, string, string, bool) bool { return true }

type noLines struct{}

func (noLines) Plan(context.Context, sip_trunk.CallPlanInput) (*sip_trunk.CallPlan, error) {
	return nil, sip_trunk.ErrNoDialableTrunk
}

func TestATypedNumberOfAnOptedOutIdentityTakesADirectCall(t *testing.T) {
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	identity := &lead.Lead{ID: "lead-9", WorkspaceID: "ws-1", Number: "558494409684", OptedOutAt: &optedOut}
	targets, err := lead_usecase.NewDialTargets(lead_usecase.DialTargetDeps{
		Leads: identityDirectory{identity: identity}, Permissions: everyPermission{}, Planner: noLines{}, Lines: noLines{},
	})
	if err != nil {
		t.Fatal(err)
	}
	admission := newCountingAdmission()
	source := &stubCallSource{call: &stubCRMCall{}}
	res, err := NewStartOutboundCallUseCase(source, admission, targets, nil).Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", TargetPhone: "5584994409684", TrunkID: "trunk-1",
	})
	if err != nil {
		t.Fatalf("Execute = %v, a direct call to an opted out lead is allowed", err)
	}
	if res.LeadID != "lead-9" || admission.acquired != 1 || source.lastIn.PhoneNumber != "5584994409684" {
		t.Fatalf("result %+v, acquired %d, dialed %q, want the identity named and dialed", res, admission.acquired, source.lastIn.PhoneNumber)
	}
}

func TestEveryCallNeedsANumber(t *testing.T) {

	_, err := NewStartOutboundCallUseCase(&stubCallSource{}, newCountingAdmission(), &stubDialTargets{}, nil).Execute(context.Background(), callsession.StartOutboundCallInput{
		WorkspaceID: "ws-1", UserID: "user-1", WhatsAppPhoneID: "phone-1", LeadID: "lead-1",
	})
	if !errors.Is(err, callsession.ErrTargetPhoneRequired) {
		t.Fatalf("Execute = %v, want ErrTargetPhoneRequired", err)
	}
}

func (noLines) Lines(context.Context, sip_trunk.CallPlanInput) ([]sip_trunk.TrunkChoice, error) {
	return nil, sip_trunk.ErrNoDialableTrunk
}
