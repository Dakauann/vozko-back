package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/actor"
	"vozko/domain/address"
	"vozko/domain/cep"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

type fakeCEPSearch struct {
	infos map[string]cep.CEPInfo
	err   error
	asked []string
}

func (f *fakeCEPSearch) Execute(_ context.Context, raw string) (*cep.CEPInfo, error) {
	f.asked = append(f.asked, raw)
	code, err := cep.Parse(raw)
	if err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	info, ok := f.infos[code]
	if !ok {
		return nil, cep.ErrNotFound
	}
	return &info, nil
}

func paulistaLookup() *fakeCEPSearch {
	return &fakeCEPSearch{infos: map[string]cep.CEPInfo{
		"01310100": {Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"},
	}}
}

type profileFixture struct {
	store    *fakeStore
	notifier *fakeNotifier
	fields   *fakeDefinitions
	lookup   *fakeCEPSearch
	profiles *Profiles
}

func newProfileFixture(t *testing.T, leads ...*lead.Lead) *profileFixture {
	t.Helper()
	f := &profileFixture{store: newFakeStore(leads...), notifier: &fakeNotifier{}, fields: &fakeDefinitions{defs: leadDefinitions()}, lookup: paulistaLookup()}
	profiles, err := NewProfiles(ProfileDeps{
		Store: f.store, Notifier: f.notifier, Definitions: f.fields, CEP: f.lookup,
		EntryLeads: historyEntryLeads{"e-1": "l-1"},
		Now:        func() time.Time { return cmdNow },
	})
	if err != nil {
		t.Fatalf("NewProfiles: %v", err)
	}
	f.profiles = profiles
	return f
}

func conversationUpdate(p lead.Profile) ProfileUpdate {
	return ProfileUpdate{WorkspaceID: cmdWorkspace, LeadID: "l-1", Actor: cmdAgent, Source: lead.ProfileFromConversation, Profile: p}
}

func TestNewProfilesRefusesAMissingDependency(t *testing.T) {
	complete := func() ProfileDeps {
		return ProfileDeps{Store: newFakeStore(), Notifier: &fakeNotifier{}, Definitions: &fakeDefinitions{}, EntryLeads: historyEntryLeads{}, CEP: paulistaLookup()}
	}
	for name, strip := range map[string]func(*ProfileDeps){
		"store":       func(d *ProfileDeps) { d.Store = nil },
		"notifier":    func(d *ProfileDeps) { d.Notifier = nil },
		"definitions": func(d *ProfileDeps) { d.Definitions = nil },
		"entry leads": func(d *ProfileDeps) { d.EntryLeads = nil },
		"cep":         func(d *ProfileDeps) { d.CEP = nil },
	} {
		t.Run(name, func(t *testing.T) {
			deps := complete()
			strip(&deps)
			if _, err := NewProfiles(deps); err == nil {
				t.Fatalf("profiles without %s must not be built", name)
			}
		})
	}
	if _, err := NewProfiles(complete()); err != nil {
		t.Fatalf("complete deps: %v", err)
	}
}

func TestUpdateProfileCompletesTheCEPAndWritesAVersionedEvent(t *testing.T) {
	f := newProfileFixture(t, storedLead())

	got, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{
		Address:      address.Postal{ZipCode: "01310-100", Number: "1000"},
		BirthDate:    "15/03/1980",
		CustomFields: map[string]string{"interesse": "alto"},
	}))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantChanged := []string{lead.FieldAddresses, lead.FieldBirthDate, lead.CustomFieldName("interesse")}
	if got.LeadID != "l-1" || got.Version != 4 || !reflect.DeepEqual(got.Changed, wantChanged) || got.Conflicts != nil {
		t.Fatalf("result = %+v", got)
	}
	saved := f.store.leads["l-1"]
	primary := saved.PrimaryAddress()
	if primary == nil || primary.Postal != (address.Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP", CityCode: "3550308"}) || primary.GeoStatus != lead.GeoPending {
		t.Fatalf("primary = %+v", primary)
	}
	if len(f.store.saves) != 1 || f.store.saves[0].expected != 3 || len(f.store.saves[0].events) != 1 {
		t.Fatalf("saves = %+v", f.store.saves)
	}
	event := f.store.saves[0].events[0]
	if event.Kind != lead.ProfileFromConversation.EventKind() || event.Actor != cmdAgent || !reflect.DeepEqual(event.Fields(), wantChanged) {
		t.Fatalf("event = %+v", event)
	}
	if len(f.notifier.changes) != 1 || f.notifier.changes[0].Version != 4 || !reflect.DeepEqual(f.notifier.changes[0].Fields, wantChanged) {
		t.Fatalf("notified = %+v", f.notifier.changes)
	}
	if !reflect.DeepEqual(f.lookup.asked, []string{"01310100"}) {
		t.Fatalf("lookups = %v", f.lookup.asked)
	}
}

func TestUpdateProfileWithoutACEPNeverAsksTheLookup(t *testing.T) {
	f := newProfileFixture(t, storedLead())
	if _, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{Address: address.Postal{District: "Centro", City: "Santos", State: "SP"}})); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(f.lookup.asked) != 0 {
		t.Fatalf("lookups = %v", f.lookup.asked)
	}
}

func TestUpdateProfileRefusesACEPItCannotTrust(t *testing.T) {
	cases := []struct {
		name    string
		postal  address.Postal
		outage  error
		wantErr error
	}{
		{"a CEP that does not exist", address.Postal{ZipCode: "99999-999"}, nil, lead.ErrProfileCEPUnknown},
		{"a CEP the lookup cannot check now", address.Postal{ZipCode: "01310-100"}, cep.ErrUnavailable, lead.ErrProfileCEPUnchecked},
		{"a CEP with the wrong length", address.Postal{ZipCode: "0131"}, nil, address.ErrInvalidAddress},
		{"a city that is not the CEP's", address.Postal{ZipCode: "01310100", City: "Campinas"}, nil, address.ErrCEPMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newProfileFixture(t, storedLead())
			f.lookup.err = tc.outage
			if _, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{Address: tc.postal})); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
				t.Fatalf("nothing may be written: saves %d, notified %d", len(f.store.saves), len(f.notifier.changes))
			}
		})
	}
}

func TestUpdateProfileThatOnlyConflictsWritesNothing(t *testing.T) {
	f := newProfileFixture(t, storedLead())
	got, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{CustomFields: map[string]string{"cor": "verde"}}))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Version != 3 || got.Changed != nil || !reflect.DeepEqual(got.Conflicts, []string{lead.CustomFieldName("cor")}) {
		t.Fatalf("result = %+v", got)
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
		t.Fatalf("a manual value is never replaced: saves %d, notified %d", len(f.store.saves), len(f.notifier.changes))
	}
}

func TestUpdateProfileRetriesAfterLosingARace(t *testing.T) {
	f := newProfileFixture(t, storedLead())
	f.store.raceOnce = true
	got, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{BirthDate: "1980-03-15"}))
	if err != nil || got.Version != 5 || !reflect.DeepEqual(got.Changed, []string{lead.FieldBirthDate}) {
		t.Fatalf("after a race = %+v, %v", got, err)
	}
}

func TestUpdateProfileThatKeepsLosingIsAConflict(t *testing.T) {
	profiles, err := NewProfiles(ProfileDeps{Store: alwaysConflicting{newFakeStore(storedLead())}, Notifier: &fakeNotifier{}, Definitions: &fakeDefinitions{defs: leadDefinitions()}, EntryLeads: historyEntryLeads{}, CEP: paulistaLookup()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Update(context.Background(), conversationUpdate(lead.Profile{BirthDate: "1980-03-15"})); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("err = %v, want a version conflict", err)
	}
}

func TestUpdateProfileStampsTheSystemWhenNoActorIsKnown(t *testing.T) {
	f := newProfileFixture(t, storedLead())
	in := conversationUpdate(lead.Profile{BirthDate: "1980-03-15"})
	in.Actor, in.Source = " ", lead.ProfileFromForm
	if _, err := f.profiles.Update(context.Background(), in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if event := f.store.saves[0].events[0]; event.Actor != actor.SystemID || event.Kind != lead.ProfileFromForm.EventKind() {
		t.Fatalf("event = %+v", event)
	}
}

func TestUpdateProfileRefuses(t *testing.T) {
	cases := []struct {
		name   string
		update func(*ProfileUpdate)
		defErr error
		want   error
	}{
		{"no workspace", func(u *ProfileUpdate) { u.WorkspaceID = "" }, nil, lead.ErrLeadWorkspaceRequired},
		{"no lead", func(u *ProfileUpdate) { u.LeadID = "" }, nil, lead.ErrLeadRequired},
		{"a lead of another workspace", func(u *ProfileUpdate) { u.WorkspaceID = "ws-2" }, nil, lead.ErrLeadNotFound},
		{"a lead that is gone", func(u *ProfileUpdate) { u.LeadID = "l-gone" }, nil, lead.ErrLeadNotFound},
		{"an unknown source", func(u *ProfileUpdate) { u.Source = "manual" }, nil, lead.ErrProfileSourceInvalid},
		{"an empty profile", func(u *ProfileUpdate) { u.Profile = lead.Profile{BirthDate: " "} }, nil, lead.ErrProfileEmpty},
		{"a sensitive field", func(u *ProfileUpdate) {
			u.Profile = lead.Profile{CustomFields: map[string]string{"classificacao": "positivo"}}
		}, nil, customfield.ErrValueForbidden},
		{"lead fields that cannot be read", func(*ProfileUpdate) {}, errors.New("database down"), errors.New("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newProfileFixture(t, storedLead())
			f.fields.err = tc.defErr
			in := conversationUpdate(lead.Profile{CustomFields: map[string]string{"interesse": "alto"}})
			tc.update(&in)
			_, err := f.profiles.Update(context.Background(), in)
			if err == nil {
				t.Fatal("the update must be refused")
			}
			if tc.defErr == nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
				t.Fatalf("nothing may be written: saves %d, notified %d", len(f.store.saves), len(f.notifier.changes))
			}
		})
	}
}

func TestUpdateProfileOfEntryResolvesTheLeadOfTheConversation(t *testing.T) {
	f := newProfileFixture(t, storedLead())
	in := ProfileUpdate{WorkspaceID: cmdWorkspace, Actor: "workflow:wf-1", Source: lead.ProfileFromWorkflow, Profile: lead.Profile{BirthDate: "1980-03-15"}}

	got, err := f.profiles.UpdateOfEntry(context.Background(), shared.EntryRef{EntryID: "e-1", EntryType: shared.EntryTypeWhatsApp}, in)
	if err != nil || got.LeadID != "l-1" || !reflect.DeepEqual(got.Changed, []string{lead.FieldBirthDate}) {
		t.Fatalf("result = %+v, %v", got, err)
	}
	if kind := f.store.saves[0].events[0].Kind; kind != recordevent.Kind("profile_from_workflow") {
		t.Fatalf("event kind = %q", kind)
	}

	if _, err := f.profiles.UpdateOfEntry(context.Background(), shared.EntryRef{EntryID: "e-without-lead", EntryType: shared.EntryTypeWhatsApp}, in); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a conversation without a lead: err = %v", err)
	}
	if _, err := f.profiles.UpdateOfEntry(context.Background(), shared.EntryRef{EntryType: shared.EntryTypeWhatsApp}, in); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("no conversation: err = %v", err)
	}
}

func TestWritableFieldsNeverListASensitiveField(t *testing.T) {
	f := newProfileFixture(t)
	got, err := f.profiles.WritableFields(cmdWorkspace)
	if err != nil {
		t.Fatalf("WritableFields: %v", err)
	}
	keys := make([]string, 0, len(got))
	for _, def := range got {
		keys = append(keys, def.Key)
	}
	if !reflect.DeepEqual(keys, []string{"cor", "interesse"}) {
		t.Fatalf("keys = %v", keys)
	}
	if _, err := f.profiles.WritableFields(""); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("no workspace: err = %v", err)
	}
}
