package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/address"
	"vozko/domain/lead"
)

type fakeEntries struct {
	used  map[string]bool
	calls int
	err   error
}

func (e *fakeEntries) HasEntries(_ context.Context, _, leadID string) (bool, error) {
	e.calls++
	return e.used[leadID], e.err
}

type fakeDuplicates struct {
	found        []*lead.Lead
	err          error
	numbers      []string
	fingerprints []string
}

func (d *fakeDuplicates) FindByNumbersOrAddresses(_ context.Context, _ string, numbers, fingerprints []string) ([]*lead.Lead, error) {
	d.numbers, d.fingerprints = numbers, fingerprints
	return d.found, d.err
}

var familyHome = address.Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP"}

func maria() *lead.Lead {
	return &lead.Lead{ID: "maria", WorkspaceID: cmdWorkspace, Number: "5511987654321", Name: "Maria", Version: 2,
		Addresses: []lead.Address{{ID: "a-1", Label: lead.AddressHome, Primary: true, Postal: familyHome, GeoStatus: lead.GeoPending}}}
}

func joao() *lead.Lead {
	return &lead.Lead{ID: "joao", WorkspaceID: cmdWorkspace, Number: "5511912345678", Name: "João", Version: 5}
}

func TestNewCommandsRefusesMissingCollectionPorts(t *testing.T) {
	full := completeCommandDeps()
	for name, drop := range map[string]func(d *CommandDeps){
		"entries":    func(d *CommandDeps) { d.Entries = nil },
		"relations":  func(d *CommandDeps) { d.Relations = nil },
		"duplicates": func(d *CommandDeps) { d.Duplicates = nil },
	} {
		deps := full
		drop(&deps)
		if _, err := NewCommands(deps); err == nil {
			t.Fatalf("commands without %s must not be built", name)
		}
	}
}

func TestCreateWithPhonesAndAddressesWarnsAboutLikelyDuplicates(t *testing.T) {
	f := newCommandsFixture(t, withAddresses(allPermissions()))
	f.duplicates.found = []*lead.Lead{{ID: "x", WorkspaceID: cmdWorkspace, Name: "Zé", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}}}

	got, err := f.cmds.Create(context.Background(), operator(), lead.Draft{
		Name:      "Maria",
		Phones:    []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}},
		Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: familyHome}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(got.Lead.Phones) != 1 || got.Lead.Phones[0].ID == "" || len(got.Lead.Addresses) != 1 {
		t.Fatalf("created = %+v", got.Lead)
	}
	if !reflect.DeepEqual(f.duplicates.numbers, []string{"551133334444"}) || len(f.duplicates.fingerprints) != 1 {
		t.Fatalf("lookup = %v %v", f.duplicates.numbers, f.duplicates.fingerprints)
	}
	if len(got.Duplicates) != 1 || got.Duplicates[0].LeadID != "x" || got.Duplicates[0].Lead == nil || got.Duplicates[0].Lead.Name != "Zé" {
		t.Fatalf("duplicates = %+v", got.Duplicates)
	}
	if fields := f.notifier.changes[0].Fields; !reflect.DeepEqual(fields, []string{lead.FieldAddresses, lead.FieldName, lead.FieldPhones}) {
		t.Fatalf("fields = %v", fields)
	}
}

func TestDuplicateWarningsNeverRevealAnAddressTheCallerCannotRead(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	f.duplicates.found = []*lead.Lead{{ID: "neighbour", WorkspaceID: cmdWorkspace, Name: "Maria",
		Addresses: []lead.Address{{ID: "a-9", Label: lead.AddressHome, Primary: true, Postal: familyHome}}}}
	got, err := f.cmds.Create(context.Background(), operator(), lead.Draft{
		Name:      "Maria",
		Phones:    []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}},
		Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: familyHome}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.duplicates.fingerprints) != 0 {
		t.Fatalf("a caller without leads:read_addresses must not search by address, got %v", f.duplicates.fingerprints)
	}
	if len(got.Duplicates) != 0 {
		t.Fatalf("the same name at a guessed address must not name anyone, got %+v", got.Duplicates)
	}
}

func TestDuplicateWarningsHideTheOtherLeadFromSomeoneWhoCannotReadLeads(t *testing.T) {
	f := newCommandsFixture(t, fakePermissions{"leads:create": true})
	f.duplicates.found = []*lead.Lead{{ID: "x", WorkspaceID: cmdWorkspace, Name: "Zé", Phones: []lead.ContactPhone{{Number: "551133334444"}}}}
	got, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Duplicates) != 1 || got.Duplicates[0].Lead != nil {
		t.Fatalf("duplicates = %+v", got.Duplicates)
	}
}

func TestCreateNamesTheLeadThatHoldsTheNumber(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	f.duplicates.found = []*lead.Lead{{ID: "holder", WorkspaceID: cmdWorkspace, Number: "551187654321"}}
	_, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Number: "5511987654321"})
	var taken *lead.IdentityTaken
	if !errors.As(err, &taken) || taken.LeadID != "holder" || !errors.Is(err, lead.ErrLeadDuplicate) {
		t.Fatalf("err = %#v, want the identity taken by holder", err)
	}
	if len(f.store.inserted) != 0 {
		t.Fatal("nothing may be inserted")
	}

	blind := newCommandsFixture(t, fakePermissions{"leads:create": true})
	blind.duplicates.found = f.duplicates.found
	_, err = blind.cmds.Create(context.Background(), operator(), lead.Draft{Number: "5511987654321"})
	if !errors.As(err, &taken) || taken.LeadID != "" {
		t.Fatalf("someone who cannot read leads learns only that the number is taken, got %#v", err)
	}
}

func TestCreateRefusesWhenTheDuplicateLookupFails(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	f.duplicates.err = errors.New("database down")
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}}); err == nil {
		t.Fatal("a failed duplicate lookup must refuse the creation")
	}
	if len(f.store.inserted) != 0 {
		t.Fatal("nothing may be inserted")
	}
}

func TestUpdateReplacesPhonesAndAddressesOfTheAggregate(t *testing.T) {
	f := newCommandsFixture(t, withAddresses(allPermissions()), maria())
	phones := []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	addresses := []lead.AddressInput{}
	got, err := f.cmds.Update(context.Background(), operator(), "maria", version(2), lead.Edit{Phones: &phones, Addresses: &addresses})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(got.Phones) != 1 || got.Phones[0].ID == "" || len(got.Addresses) != 0 || got.Version != 3 {
		t.Fatalf("updated = %+v", got)
	}
	if fields := f.notifier.changes[0].Fields; !reflect.DeepEqual(fields, []string{lead.FieldAddresses, lead.FieldPhones}) {
		t.Fatalf("fields = %v", fields)
	}
}

func TestUpdateChangesTheIdentityOnlyWithoutConversations(t *testing.T) {
	cases := []struct {
		name      string
		used      bool
		lookupErr error
		number    string
		wantErr   error
		wantCalls int
		want      string
	}{
		{name: "a lead without conversations", number: "5521998765432", wantCalls: 1, want: "5521998765432"},
		{name: "a lead with conversations", used: true, number: "5521998765432", wantErr: lead.ErrIdentityInUse, wantCalls: 1, want: "5511987654321"},
		{name: "the same number in another format asks nothing", used: true, number: "551187654321", wantCalls: 0, want: "5511987654321"},
		{name: "a failed lookup refuses", lookupErr: errors.New("database down"), number: "5521998765432", wantErr: errors.New("any"), wantCalls: 1, want: "5511987654321"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommandsFixture(t, allPermissions(), maria())
			f.entries.used = map[string]bool{"maria": tc.used}
			f.entries.err = tc.lookupErr
			number := tc.number
			_, err := f.cmds.Update(context.Background(), operator(), "maria", version(2), lead.Edit{Number: &number})
			if (err != nil) != (tc.wantErr != nil) || (tc.lookupErr == nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if f.entries.calls != tc.wantCalls {
				t.Fatalf("entry lookups = %d, want %d", f.entries.calls, tc.wantCalls)
			}
			if stored := f.store.leads["maria"]; stored.Number != tc.want {
				t.Fatalf("number = %q, want %q", stored.Number, tc.want)
			}
		})
	}
}

func TestChangeIdentityIsAVersionedEdit(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), maria())
	if _, err := f.cmds.ChangeIdentity(context.Background(), operator(), "maria", nil, "5521998765432"); err == nil {
		t.Fatal("a change without the version that was read must be refused")
	}
	got, err := f.cmds.ChangeIdentity(context.Background(), operator(), "maria", version(2), "5521998765432")
	if err != nil || got.Number != "5521998765432" || f.store.saves[0].events[0].Kind != lead.EventUpdated {
		t.Fatalf("ChangeIdentity = %+v, %v", got, err)
	}
}

func TestAddRelativeCreatesTheRelativeAndTheRelationTogether(t *testing.T) {
	f := newCommandsFixture(t, withAddresses(allPermissions()), maria())
	f.duplicates.found = []*lead.Lead{maria()}
	got, err := f.cmds.AddRelative(context.Background(), operator(), "maria", AddRelativeInput{
		Kind:               lead.KindChild,
		Relative:           lead.Draft{Name: "Pedro", Phones: []lead.ContactPhone{{Number: "5511987654321", Label: lead.PhoneMobile}}},
		CopyPrimaryAddress: true,
	})
	if err != nil {
		t.Fatalf("AddRelative: %v", err)
	}
	relative := got.Relative
	if relative.ID == "" || relative.Name != "Pedro" || relative.RelativesCount != 1 {
		t.Fatalf("relative = %+v", relative)
	}
	if len(relative.Addresses) != 1 || relative.Addresses[0].Postal != familyHome || !relative.Addresses[0].Primary {
		t.Fatalf("the relative gets the primary address, got %+v", relative.Addresses)
	}
	if got.Relation.LeadID != "maria" || got.Relation.OtherLeadID != relative.ID || got.Relation.Kind != lead.KindChild || got.Relation.ID == "" {
		t.Fatalf("relation = %+v, want maria holding her child", got.Relation)
	}
	if got.Lead.ID != "maria" || got.Lead.RelativesCount != 1 || got.Lead.Version != 3 {
		t.Fatalf("anchor = %+v", got.Lead)
	}
	if len(f.store.inserted) != 1 || len(f.store.saves) != 1 {
		t.Fatalf("one insert carries the relative and the relation, got %d inserts %d writes", len(f.store.inserted), len(f.store.saves))
	}
	kinds := []string{}
	for _, e := range f.store.saves[0].events {
		kinds = append(kinds, string(e.Kind))
	}
	if !reflect.DeepEqual(kinds, []string{string(lead.EventCreated), string(lead.EventRelationAdded)}) {
		t.Fatalf("events = %v", kinds)
	}
	if len(got.Duplicates) != 0 {
		t.Fatalf("the lead the relative is added to is never a duplicate warning, got %+v", got.Duplicates)
	}
	notified := map[string]bool{}
	for _, c := range f.notifier.changes {
		notified[c.LeadID] = true
	}
	if !notified["maria"] || !notified[relative.ID] {
		t.Fatalf("both leads are announced, got %+v", f.notifier.changes)
	}
}

func TestAddRelativeNeedsCreateAndUpdate(t *testing.T) {
	for name, perms := range map[string]fakePermissions{
		"without create": {"leads:read": true, "leads:update": true},
		"without update": {"leads:read": true, "leads:create": true},
	} {
		f := newCommandsFixture(t, perms, maria())
		_, err := f.cmds.AddRelative(context.Background(), operator(), "maria", AddRelativeInput{Kind: lead.KindChild, Relative: lead.Draft{Name: "Pedro"}})
		if !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestAddRelativeRefusals(t *testing.T) {
	cases := []struct {
		name string
		id   string
		in   AddRelativeInput
		want error
	}{
		{"an unknown kind", "maria", AddRelativeInput{Kind: "friend", Relative: lead.Draft{Name: "Pedro"}}, lead.ErrRelationKindInvalid},
		{"a lead of another workspace", "foreign", AddRelativeInput{Kind: lead.KindChild, Relative: lead.Draft{Name: "Pedro"}}, lead.ErrLeadNotFound},
		{"a relative without name or number", "maria", AddRelativeInput{Kind: lead.KindChild}, lead.ErrLeadIdentityRequired},
		{"copying an address the lead does not have", "joao", AddRelativeInput{Kind: lead.KindChild, Relative: lead.Draft{Name: "Pedro"}, CopyPrimaryAddress: true}, lead.ErrNoPrimaryAddress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			foreign := joao()
			foreign.ID, foreign.WorkspaceID = "foreign", "ws-2"
			f := newCommandsFixture(t, allPermissions(), maria(), joao(), foreign)
			if _, err := f.cmds.AddRelative(context.Background(), operator(), tc.id, tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.store.inserted) != 0 {
				t.Fatal("nothing may be inserted")
			}
		})
	}
}

func TestLinkRelationRelatesTwoLeadsAndCountsOnBothSides(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), maria(), joao())
	got, err := f.cmds.LinkRelation(context.Background(), operator(), "maria", "joao", lead.KindReferredBy)
	if err != nil {
		t.Fatalf("LinkRelation: %v", err)
	}
	if got.Relation.ID == "" || got.Relation.LeadID != "joao" || got.Relation.Kind != lead.KindReferred || got.Relation.CreatedBy != cmdUser {
		t.Fatalf("relation = %+v, want joao holding the referral, stamped by the caller", got.Relation)
	}
	if got.Lead.Version != 3 || got.Lead.ReferredCount != 0 {
		t.Fatalf("lead = %+v", got.Lead)
	}
	if stored := f.store.leads["joao"]; stored.ReferredCount != 1 || stored.Version != 6 {
		t.Fatalf("the referrer counts the referral in the same write, got %+v", stored)
	}
	if len(f.store.saves) != 0 || len(f.store.added) != 1 {
		t.Fatalf("a link writes through the relation store only, got %d saves and %d links", len(f.store.saves), len(f.store.added))
	}
	changed := map[string]int64{}
	for _, c := range f.notifier.changes {
		changed[c.LeadID] = c.Version
	}
	if changed["maria"] != 3 || changed["joao"] != 6 {
		t.Fatalf("both leads are announced with their versions, got %+v", f.notifier.changes)
	}

	if _, err := f.cmds.LinkRelation(context.Background(), operator(), "joao", "maria", lead.KindReferred); !errors.Is(err, lead.ErrRelationExists) {
		t.Fatalf("a second referral for the pair = %v, want %v", err, lead.ErrRelationExists)
	}
}

func TestLinkRelationRefusals(t *testing.T) {
	foreign := joao()
	foreign.ID, foreign.WorkspaceID = "foreign", "ws-2"
	cases := []struct {
		name, other string
		kind        lead.RelationKind
		want        error
	}{
		{"a relative of another workspace", "foreign", lead.KindSibling, lead.ErrRelativeNotFound},
		{"a relative that does not exist", "ghost", lead.KindSibling, lead.ErrRelativeNotFound},
		{"the lead itself", "maria", lead.KindSibling, lead.ErrRelationSelf},
		{"an unknown kind", "joao", "friend", lead.ErrRelationKindInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommandsFixture(t, allPermissions(), maria(), joao(), foreign)
			if _, err := f.cmds.LinkRelation(context.Background(), operator(), "maria", tc.other, tc.kind); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.store.added) != 0 || len(f.notifier.changes) != 0 {
				t.Fatal("nothing may be written or announced")
			}
		})
	}
	f := newCommandsFixture(t, fakePermissions{"leads:read": true}, maria(), joao())
	if _, err := f.cmds.LinkRelation(context.Background(), operator(), "maria", "joao", lead.KindSibling); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:update = %v", err)
	}
}

func TestRemoveRelation(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), maria(), joao())
	linked, err := f.cmds.LinkRelation(context.Background(), operator(), "joao", "maria", lead.KindParent)
	if err != nil {
		t.Fatal(err)
	}
	f.notifier.changes = nil

	removed, err := f.cmds.RemoveRelation(context.Background(), operator(), linked.Relation.ID)
	if err != nil || removed.ID != linked.Relation.ID {
		t.Fatalf("RemoveRelation = %+v, %v", removed, err)
	}
	if f.store.leads["maria"].RelativesCount != 0 || f.store.leads["joao"].RelativesCount != 0 {
		t.Fatalf("counts after removal: maria %d joao %d", f.store.leads["maria"].RelativesCount, f.store.leads["joao"].RelativesCount)
	}
	if len(f.store.removedBy) != 1 || f.store.removedBy[0] != cmdUser {
		t.Fatalf("the removal is recorded for the caller, got %v", f.store.removedBy)
	}
	if len(f.notifier.changes) != 2 {
		t.Fatalf("both leads are announced, got %+v", f.notifier.changes)
	}
	if _, err := f.cmds.RemoveRelation(context.Background(), operator(), linked.Relation.ID); !errors.Is(err, lead.ErrRelationNotFound) {
		t.Fatalf("removing twice = %v, want %v", err, lead.ErrRelationNotFound)
	}

	blind := newCommandsFixture(t, fakePermissions{"leads:read": true}, maria())
	if _, err := blind.cmds.RemoveRelation(context.Background(), operator(), "r-1"); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:update = %v", err)
	}
}

func TestRemoveRelationStillWorksWhenTheHolderWasDeleted(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), maria(), joao())
	linked, err := f.cmds.LinkRelation(context.Background(), operator(), "joao", "maria", lead.KindParent)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Relation.LeadID != "maria" {
		t.Fatalf("maria holds the relation, got %+v", linked.Relation)
	}
	delete(f.store.leads, "maria")
	f.notifier.changes = nil

	if _, err := f.cmds.RemoveRelation(context.Background(), operator(), linked.Relation.ID); err != nil {
		t.Fatalf("a relation whose holder is gone must still be removable from the live side, got %v", err)
	}
	if f.store.leads["joao"].RelativesCount != 0 {
		t.Fatalf("the live side stops counting it, got %d", f.store.leads["joao"].RelativesCount)
	}
	if len(f.notifier.changes) != 1 || f.notifier.changes[0].LeadID != "joao" {
		t.Fatalf("only the live side is announced, got %+v", f.notifier.changes)
	}
}

func TestSetAreaEditsOnlyTheBairroAndCityOfThePrimaryAddress(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), maria())
	got, err := f.cmds.SetArea(context.Background(), operator(), "maria", lead.Area{District: "Jardim Paulista", City: "São Paulo", State: "SP"})
	if err != nil {
		t.Fatalf("SetArea: %v", err)
	}
	stored := f.store.leads["maria"].Addresses[0]
	if stored.Postal.District != "Jardim Paulista" || stored.Postal.Street != familyHome.Street || stored.ID != "a-1" {
		t.Fatalf("stored = %+v", stored)
	}
	if len(got.Addresses) != 1 || got.Addresses[0].Postal.Street != "" || got.Addresses[0].Postal.District != "Jardim Paulista" {
		t.Fatalf("without leads:read_addresses the answer shows only the area, got %+v", got.Addresses)
	}
	if c := f.notifier.changes[0]; c.LeadID != "maria" || !reflect.DeepEqual(c.Fields, []string{lead.FieldAddresses}) {
		t.Fatalf("change = %+v", c)
	}
	if _, err := f.cmds.SetArea(context.Background(), operator(), "maria", lead.Area{District: "Jardim Paulista", City: "São Paulo", State: "SP"}); err != nil || len(f.store.saves) != 1 {
		t.Fatalf("the same area again writes nothing, got %d saves, %v", len(f.store.saves), err)
	}
	blind := newCommandsFixture(t, fakePermissions{"leads:read": true}, maria())
	if _, err := blind.cmds.SetArea(context.Background(), operator(), "maria", lead.Area{District: "Centro", City: "São Paulo", State: "SP"}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:update = %v", err)
	}
}
