package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type pageQueries struct {
	items []*lead.LeadWithSummary
	seen  *[]lead.ListLeadsInput
}

func (q pageQueries) List(in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	if q.seen != nil {
		*q.seen = append(*q.seen, in)
	}
	return &shared.PaginatedResult[*lead.LeadWithSummary]{Items: q.items, TotalItems: int64(len(q.items))}, nil
}

func (q pageQueries) Facets(lead.ListLeadsInput) (*lead.LeadFacets, error) {
	return &lead.LeadFacets{}, nil
}

func (q pageQueries) Get(string, string) (*lead.Lead, error) { return nil, lead.ErrLeadNotFound }

func (q pageQueries) GetByNumber(string, string) (*lead.Lead, error) {
	return nil, lead.ErrLeadNotFound
}

type pageContacts struct {
	err   error
	calls int
}

func (c *pageContacts) AttachContactDetails(_ context.Context, _ string, leads []*lead.Lead) error {
	c.calls++
	if c.err != nil {
		return c.err
	}
	for _, l := range leads {
		l.Phones = []lead.ContactPhone{{ID: "p-1", Number: "551133334444", Label: lead.PhoneLandline}}
		l.Addresses = []lead.Address{{ID: "a-1", Label: lead.AddressHome, Primary: true, Postal: familyHome}}
	}
	return nil
}

func leadPages(t *testing.T, perms fakePermissions, contacts *pageContacts) *Pages {
	t.Helper()
	row := &lead.LeadWithSummary{
		Lead:    &lead.Lead{ID: "l-1", WorkspaceID: cmdWorkspace, Name: "Ana", Owner: cmdUser, RelativesCount: 2, CustomFields: map[string]any{"cor": "azul", "classificacao": "positivo"}},
		Summary: &lead.LeadSummary{},
	}
	pages, err := NewPages(PageDeps{Leads: pageQueries{items: []*lead.LeadWithSummary{row}}, Contacts: contacts, Permissions: perms, Definitions: &fakeDefinitions{defs: leadDefinitions()}, Names: namesOf{cmdUser: "Marina"}, Zones: fixedZones{loc: time.UTC}})
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

func TestTheLeadPageCarriesTheNewColumnsUnderTheFieldRules(t *testing.T) {
	in := lead.ListLeadsInput{WorkspaceID: cmdWorkspace}
	page, err := leadPages(t, fakePermissions{"leads:read": true}, &pageContacts{}).List(context.Background(), operator(), in)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	row := page.Items[0].Lead
	if len(row.Phones) != 1 || row.Owner != cmdUser || row.RelativesCount != 2 {
		t.Fatalf("row = %+v", row)
	}
	if primary := row.PrimaryAddress(); primary == nil || primary.Postal.Street != "" || primary.Postal.District != familyHome.District {
		t.Fatalf("without leads:read_addresses the row shows only the area, got %+v", row.Addresses)
	}
	if _, sensitive := row.CustomFields["classificacao"]; sensitive || row.CustomFields["cor"] != "azul" {
		t.Fatalf("custom fields = %v", row.CustomFields)
	}

	full, err := leadPages(t, withSensitive(withAddresses(fakePermissions{"leads:read": true})), &pageContacts{}).List(context.Background(), operator(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got := full.Items[0].Lead; got.PrimaryAddress().Postal.Street != familyHome.Street || got.CustomFields["classificacao"] != "positivo" {
		t.Fatalf("a full reader sees everything, got %+v", got)
	}
}

func TestTheLeadPageRefusesInsteadOfShowingTooMuchOrTooLittle(t *testing.T) {
	in := lead.ListLeadsInput{WorkspaceID: cmdWorkspace}
	if _, err := leadPages(t, fakePermissions{}, &pageContacts{}).List(context.Background(), operator(), in); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:read = %v", err)
	}
	if _, err := leadPages(t, fakePermissions{"leads:read": true}, &pageContacts{err: errors.New("database down")}).List(context.Background(), operator(), in); err == nil {
		t.Fatal("a page whose phones and addresses cannot be read must fail")
	}
	other := lead.ListLeadsInput{WorkspaceID: "ws-2"}
	if _, err := leadPages(t, fakePermissions{"leads:read": true}, &pageContacts{}).List(context.Background(), operator(), other); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("a page of another workspace = %v", err)
	}
	if _, err := NewPages(PageDeps{}); err == nil {
		t.Fatal("pages without their ports must not be built")
	}
}

func TestTheLeadPageNamesOwnersBindsCustomFieldsAndKnowsTheWorkspaceDay(t *testing.T) {
	var seen []lead.ListLeadsInput
	row := &lead.LeadWithSummary{Lead: &lead.Lead{ID: "l-1", WorkspaceID: cmdWorkspace, Owner: cmdUser}, Summary: &lead.LeadSummary{}}
	pages, err := NewPages(PageDeps{
		Leads: pageQueries{items: []*lead.LeadWithSummary{row}, seen: &seen}, Contacts: &pageContacts{},
		Permissions: fakePermissions{"leads:read": true}, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Names: namesOf{cmdUser: "Marina"}, Zones: fixedZones{loc: time.UTC},
		Now: func() time.Time { return time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	in := lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}},
		{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpEquals, Values: []string{crmfilter.BirthdayToday}},
	}}}}}
	page, err := pages.List(context.Background(), operator(), in)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Items[0].OwnerName != "Marina" {
		t.Fatalf("owner name = %q, want it resolved for the page", page.Items[0].OwnerName)
	}
	if len(seen) != 1 {
		t.Fatalf("queries = %d", len(seen))
	}
	if kind, bound := seen[0].Filter.Groups[0].Predicates[0].BoundKind(); !bound || kind != crmfilter.KindEnum {
		t.Fatal("the custom predicate must reach the repository bound to its definition")
	}
	if seen[0].Today.IsZero() {
		t.Fatal("a birthday filter needs the workspace day")
	}

	sensitive := lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}},
	}}}}}
	if _, err := pages.List(context.Background(), operator(), sensitive); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("a sensitive filter without the permission = %v, want ErrFilterSensitive", err)
	}
	if len(seen) != 1 {
		t.Fatal("a refused filter must not reach the repository")
	}
}

func TestAZipFilterNeedsTheFullAddressReader(t *testing.T) {
	zip := lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldID, Operator: crmfilter.OpEquals, Values: []string{"0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"}},
		{Field: crmfilter.FieldZip, Operator: crmfilter.OpIn, Values: []string{"01310-100"}},
	}}}}}
	if _, err := leadPages(t, fakePermissions{"leads:read": true}, &pageContacts{}).List(context.Background(), operator(), zip); !errors.Is(err, lead.ErrLeadFilterAddressForbidden) {
		t.Fatalf("a zip filter without leads:read_addresses = %v, want ErrLeadFilterAddressForbidden", err)
	}
	if _, err := leadPages(t, fakePermissions{"leads:read": true, "leads:read_addresses": true}, &pageContacts{}).List(context.Background(), operator(), zip); err != nil {
		t.Fatalf("a zip filter for an address reader = %v", err)
	}
}
