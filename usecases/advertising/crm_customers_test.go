package advertising

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/address"
	ads "vozko/domain/advertising"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
)

type pagedSelection struct {
	pages     [][]string
	afters    []string
	scopes    []selection.Scope
	selection []selection.Selection
	limits    []int
	snapshots []string
}

func (p *pagedSelection) Snapshot(_ context.Context, _, snapshotID, after string, limit int) ([]string, error) {
	p.snapshots = append(p.snapshots, snapshotID)
	p.afters = append(p.afters, after)
	p.limits = append(p.limits, limit)
	index := len(p.afters) - 1
	if index >= len(p.pages) {
		return nil, nil
	}
	return p.pages[index], nil
}

func (p *pagedSelection) Resolve(_ context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error) {
	p.afters = append(p.afters, after)
	p.scopes = append(p.scopes, scope)
	p.selection = append(p.selection, s)
	p.limits = append(p.limits, limit)
	index := len(p.afters) - 1
	if index >= len(p.pages) {
		return nil, nil
	}
	refs := make([]selection.Ref, 0, len(p.pages[index]))
	for _, id := range p.pages[index] {
		refs = append(refs, selection.Ref{ID: id, Type: lead.SelectionRefType})
	}
	return refs, nil
}

type leadsByID map[string]*lead.Lead

func (l leadsByID) FindByIDs(_ string, ids []string) ([]*lead.Lead, error) {
	out := make([]*lead.Lead, 0, len(ids))
	for _, id := range ids {
		if found, ok := l[id]; ok {
			copied := *found
			out = append(out, &copied)
		}
	}
	return out, nil
}

type contactsOf map[string][]lead.Address

func (c contactsOf) AttachContactDetails(_ context.Context, _ string, leads []*lead.Lead) error {
	for _, l := range leads {
		l.Addresses = c[l.ID]
	}
	return nil
}

type definitionsOf []*customfield.Definition

func (d definitionsOf) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return d, nil
}

type permissionsOf map[string]bool

func (p permissionsOf) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return p[resource+":"+action]
}

var requester = Requester{WorkspaceID: "ws-1", UserID: "u-1"}

func customersFixture(pages [][]string, perms permissionsOf) (CustomerDirectory, *pagedSelection) {
	sel := &pagedSelection{pages: pages}
	home := []lead.Address{{Primary: true, Postal: address.Postal{ZipCode: "30140071", City: "Belo Horizonte", State: "MG", District: "Centro"}}}
	directory, err := NewCRMCustomers(CRMCustomerDeps{
		Selection: sel,
		Leads: leadsByID{
			"l-1": {ID: "l-1", Number: "5511987654321", Name: "+55 11 98765-4321"},
			"l-2": {ID: "l-2", Number: "5511912345678", Name: "Maria  Souza Lima"},
			"l-3": {ID: "l-3", Number: "5511900000000", Name: "Bloqueada", Blocked: true},
			"l-4": {ID: "l-4", Name: "Sem número", Phones: []lead.ContactPhone{{Number: "553133334444", Label: lead.PhoneLandline}}},
		},
		Contacts:    contactsOf{"l-2": home},
		Definitions: definitionsOf{{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"Positivo"}, Sensitive: true}},
		Permissions: perms,
	})
	if err != nil {
		panic(err)
	}
	return directory, sel
}

func TestCRMCustomersNeverSendANumberAsAName(t *testing.T) {
	directory, _ := customersFixture([][]string{{"l-1", "l-2"}}, permissionsOf{"leads:read": true})
	got, err := directory.Customers(context.Background(), requester, CustomerQuery{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got[0][ads.MatchFirstName] != "" || got[0][ads.MatchLastName] != "" || got[0][ads.MatchPhone] != "5511987654321" {
		t.Fatalf("customer = %v", got[0])
	}
	if got[1][ads.MatchFirstName] != "Maria" || got[1][ads.MatchLastName] != "Souza Lima" {
		t.Fatalf("customer = %v", got[1])
	}
}

func TestCRMCustomersPageByKeysetPastAHundred(t *testing.T) {
	directory, sel := customersFixture([][]string{{"l-1"}, {"l-2", "l-3"}, {"l-4"}}, permissionsOf{"leads:read": true})
	directory.(*crmCustomers).page = 1
	got, err := directory.Customers(context.Background(), requester, CustomerQuery{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("customers = %v", got)
	}
	if sel.afters[1] != "l-1" || sel.afters[2] != "l-3" || sel.scopes[0].ActorID != "u-1" || sel.selection[0].Mode != selection.ModeEveryone {
		t.Fatalf("paging = %v, scope %+v, mode %s", sel.afters, sel.scopes[0], sel.selection[0].Mode)
	}
	if !sel.selection[0].Require.UsesField(crmfilter.FieldBlocked) {
		t.Fatalf("blocked leads are not skipped by the selection: %+v", sel.selection[0].Require)
	}
	if got[3][ads.MatchPhone] != "553133334444" {
		t.Fatalf("a lead without an identity is matched by its first phone: %v", got[2])
	}
}

func TestCRMCustomersCarryTheAreaAndTheCEPOnlyForAddressReaders(t *testing.T) {
	directory, _ := customersFixture([][]string{{"l-2"}}, permissionsOf{"leads:read": true})
	got, _ := directory.Customers(context.Background(), requester, CustomerQuery{}, 10)
	if got[0][ads.MatchCity] != "Belo Horizonte" || got[0][ads.MatchState] != "MG" || got[0][ads.MatchZip] != "" {
		t.Fatalf("customer = %v", got[0])
	}
	full, _ := customersFixture([][]string{{"l-2"}}, permissionsOf{"leads:read": true, "leads:read_addresses": true})
	got, _ = full.Customers(context.Background(), requester, CustomerQuery{}, 10)
	if got[0][ads.MatchZip] != "30140071" {
		t.Fatalf("an address reader sends the CEP too: %v", got[0])
	}
}

func TestCRMCustomersRefuseAFilterOnASensitiveField(t *testing.T) {
	directory, _ := customersFixture(nil, permissionsOf{"leads:read": true, "leads:read_sensitive": true})
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"Positivo"}},
	}}}}
	if _, err := directory.Customers(context.Background(), requester, CustomerQuery{Filter: filter}, 10); !errors.Is(err, ads.ErrSensitiveAudience) {
		t.Fatalf("a sensitive audience = %v", err)
	}
}

func TestCRMCustomersNeedToReadLeads(t *testing.T) {
	directory, _ := customersFixture(nil, permissionsOf{})
	if _, err := directory.Customers(context.Background(), requester, CustomerQuery{}, 10); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("an audience without leads:read = %v", err)
	}
	if _, err := NewCRMCustomers(CRMCustomerDeps{}); err == nil {
		t.Fatal("a directory without its ports must not be built")
	}
}

func TestAudienceFilterForASelection(t *testing.T) {
	picked, err := AudienceFilter(selection.Selection{Mode: selection.ModeIDs, IDs: []string{"l-1", "l-2"}})
	if err != nil || len(picked.Groups) != 1 || picked.Groups[0].Predicates[0].Field != crmfilter.FieldID || len(picked.Groups[0].Predicates[0].Values) != 2 {
		t.Fatalf("picked = %+v, %v", picked, err)
	}
	base := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	matching, err := AudienceFilter(selection.Selection{Mode: selection.ModeAllMatching, Filter: &base, ExcludeIDs: []string{"l-9"}})
	if err != nil || len(matching.Groups) != 2 || matching.Groups[1].Predicates[0].Operator != crmfilter.OpNotIn {
		t.Fatalf("matching = %+v, %v", matching, err)
	}
	everyone, err := AudienceFilter(selection.Selection{Mode: selection.ModeEveryone})
	if err != nil || !everyone.IsEmpty() {
		t.Fatalf("everyone = %+v, %v", everyone, err)
	}
	if _, err := AudienceFilter(selection.Selection{Mode: selection.ModeFirstN, Filter: &base, Limit: 10}); !errors.Is(err, selection.ErrModeUnsupported) {
		t.Fatalf("first n = %v", err)
	}
}

func TestCRMCustomersPageAFrozenSetWhenGivenOne(t *testing.T) {
	directory, sel := customersFixture([][]string{{"l-1"}, {"l-2"}}, permissionsOf{"leads:read": true})
	directory.(*crmCustomers).page = 1
	got, err := directory.Customers(context.Background(), requester, CustomerQuery{SnapshotID: "snap-1"}, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("customers = %v, %v", got, err)
	}
	if len(sel.selection) != 0 || sel.snapshots[0] != "snap-1" || sel.afters[1] != "l-1" {
		t.Fatalf("a frozen audience read the live selection: %+v", sel)
	}
}

func TestAudienceSelectionSkipsBlockedLeads(t *testing.T) {
	s, err := AudienceSelection(selection.Selection{Mode: selection.ModeEveryone})
	if err != nil || s.Require == nil || !s.Require.UsesField(crmfilter.FieldBlocked) {
		t.Fatalf("selection = %+v, %v", s, err)
	}
	if _, err := AudienceSelection(selection.Selection{Mode: selection.ModeFirstN}); !errors.Is(err, selection.ErrModeUnsupported) {
		t.Fatalf("first n = %v", err)
	}
}
