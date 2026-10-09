package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
)

type fakeDefinitions struct {
	defs  []*customfield.Definition
	err   error
	calls int
}

func (d *fakeDefinitions) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	d.calls++
	if objectType != customfield.ObjectLead {
		return nil, errors.New("only lead fields are asked for")
	}
	return d.defs, d.err
}

func leadDefinitions() []*customfield.Definition {
	return []*customfield.Definition{
		{Key: "cor", ObjectType: customfield.ObjectLead, Type: customfield.TypeText},
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"positivo", "negativo"}, Sensitive: true, LegalBasis: "consentimento"},
	}
}

type fakeAnonymizer struct {
	store   *fakeStore
	erasure lead.Erasure
	err     error
	calls   []string
}

func (a *fakeAnonymizer) Anonymize(_ context.Context, workspaceID, leadID, actorID string, at time.Time) (lead.Erasure, error) {
	a.calls = append(a.calls, workspaceID+"/"+leadID+"/"+actorID+"/"+at.Format(time.RFC3339))
	if a.store != nil {
		if _, err := a.store.FindByID(workspaceID, leadID); err != nil {
			return lead.Erasure{}, err
		}
	}
	if a.err != nil {
		return lead.Erasure{}, a.err
	}
	erasure := a.erasure
	erasure.LeadID, erasure.At = leadID, at
	return erasure, nil
}

func classifiedLead() *lead.Lead {
	l := storedLead()
	l.CustomFields = map[string]any{"cor": "azul", "classificacao": "positivo"}
	l.Addresses = []lead.Address{{ID: "a-1", Label: lead.AddressHome, Primary: true, Postal: familyHome, GeoStatus: lead.GeoPending}}
	return l
}

func withSensitive(p fakePermissions) fakePermissions {
	p["leads:read_sensitive"] = true
	return p
}

func withAddresses(p fakePermissions) fakePermissions {
	p["leads:read_addresses"] = true
	return p
}

func TestNewCommandsRefusesMissingFieldsAndAnonymizer(t *testing.T) {
	full := completeCommandDeps()
	for name, drop := range map[string]func(d *CommandDeps){
		"definitions": func(d *CommandDeps) { d.Definitions = nil },
		"anonymizer":  func(d *CommandDeps) { d.Anonymizer = nil },
	} {
		deps := full
		drop(&deps)
		if _, err := NewCommands(deps); err == nil {
			t.Fatalf("commands without %s must not be built", name)
		}
	}
}

func TestCreateChecksAndStoresCustomFields(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	got, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", CustomFields: map[string]any{"interesse": "alto"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.store.inserted[0].CustomFields["interesse"] != "alto" || got.Lead.CustomFields["interesse"] != "alto" {
		t.Fatalf("stored = %v, answered = %v", f.store.inserted[0].CustomFields, got.Lead.CustomFields)
	}
	event := f.store.saves[0].events[0]
	if !reflect.DeepEqual(event.Fields(), []string{lead.CustomFieldName("interesse"), lead.FieldName}) {
		t.Fatalf("event fields = %v", event.Fields())
	}
}

func TestCreateRefusesBadCustomFieldsBeforeWriting(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
		want   error
	}{
		{"an unknown key", map[string]any{"cpf": "1"}, customfield.ErrUnknownKey},
		{"an option the field does not have", map[string]any{"interesse": "medio"}, customfield.ErrValueNotInOptions},
		{"a sensitive field without permission to read it", map[string]any{"classificacao": "positivo"}, customfield.ErrValueForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommandsFixture(t, allPermissions())
			if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", CustomFields: tc.values}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.store.inserted) != 0 || len(f.notifier.changes) != 0 {
				t.Fatal("a refused creation must not write or notify")
			}
		})
	}
}

func TestEveryCommandFailsClosedWhenTheLeadFieldsCannotBeRead(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	f.fields.err = errors.New("db down")
	ctx := context.Background()
	calls := map[string]func() error{
		"create": func() error { _, err := f.cmds.Create(ctx, operator(), lead.Draft{Name: "Maria"}); return err },
		"update": func() error {
			_, err := f.cmds.Update(ctx, operator(), "l-1", version(3), lead.Edit{Nickname: text("Aninha")})
			return err
		},
		"block":     func() error { _, err := f.cmds.Block(ctx, operator(), "l-1", BlockInput{Blocked: true}); return err },
		"set owner": func() error { _, err := f.cmds.SetOwner(ctx, operator(), "l-1", cmdUser); return err },
		"opt out":   func() error { _, err := f.cmds.OptOut(ctx, operator(), "l-1", lead.OptOutLeadRequest); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("a command must not answer without knowing which fields the caller may see")
			}
		})
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("nothing may be written when the fields cannot be read")
	}
}

func TestUpdateMergesCustomFieldsAndKeepsTheSensitiveOnesTheCallerCannotSee(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), classifiedLead())
	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{CustomFields: map[string]any{"interesse": "baixo", "cor": nil}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	stored := f.store.leads["l-1"].CustomFields
	if !reflect.DeepEqual(stored, map[string]any{"interesse": "baixo", "classificacao": "positivo"}) {
		t.Fatalf("stored = %v", stored)
	}
	if !reflect.DeepEqual(got.CustomFields, map[string]any{"interesse": "baixo"}) {
		t.Fatalf("answered = %v; the sensitive value must not reach a caller who cannot read it", got.CustomFields)
	}
}

func TestUpdateRefusesASensitiveFieldTheCallerCannotRead(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), classifiedLead())
	_, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{CustomFields: map[string]any{"classificacao": nil}})
	if !errors.Is(err, customfield.ErrValueForbidden) || len(f.store.saves) != 0 {
		t.Fatalf("err = %v, saves = %d", err, len(f.store.saves))
	}
}

func TestASensitiveChangeIsRecordedWithoutItsValue(t *testing.T) {
	f := newCommandsFixture(t, withSensitive(allPermissions()), classifiedLead())
	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{CustomFields: map[string]any{"classificacao": "negativo"}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.CustomFields["classificacao"] != "negativo" {
		t.Fatalf("a reader of sensitive data sees it: %v", got.CustomFields)
	}
	change := f.store.saves[0].events[0].Changes[0]
	if change.Field != lead.CustomFieldName("classificacao") || !change.Redacted || change.Before != nil || change.After != nil {
		t.Fatalf("recorded change = %+v", change)
	}
	if !reflect.DeepEqual(f.notifier.changes[0].Fields, []string{lead.CustomFieldName("classificacao")}) {
		t.Fatalf("notified fields = %v", f.notifier.changes[0].Fields)
	}
}

func TestAddressesNeedTheAddressPermissionToChangeAndToReadInFull(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), classifiedLead())
	addresses := []lead.AddressInput{}
	_, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Addresses: &addresses})
	if !errors.Is(err, lead.ErrAddressesForbidden) || len(f.store.saves) != 0 {
		t.Fatalf("err = %v, saves = %d", err, len(f.store.saves))
	}

	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Nickname: text("Aninha")})
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Addresses[0]; a.Postal.Street != "" || a.Postal.ZipCode != "" || a.Postal.District != "Bela Vista" || a.Postal.City != "São Paulo" {
		t.Fatalf("a caller without the address permission sees only bairro and city: %+v", a.Postal)
	}

	full := newCommandsFixture(t, withAddresses(allPermissions()), classifiedLead())
	got, err = full.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Addresses: &addresses})
	if err != nil || len(got.Addresses) != 0 {
		t.Fatalf("err = %v, addresses = %+v", err, got.Addresses)
	}
}

func TestTheConflictBodyHidesSensitiveValues(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), classifiedLead())
	_, err := f.cmds.Update(context.Background(), operator(), "l-1", version(2), lead.Edit{Nickname: text("Aninha")})
	var conflict *lead.VersionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v", err)
	}
	if _, leaked := conflict.Current.CustomFields["classificacao"]; leaked || conflict.Current.Addresses[0].Postal.Street != "" {
		t.Fatalf("current = %+v", conflict.Current)
	}
}

func TestAnonymizeErasesThroughTheAnonymizerAndAnnouncesEveryTouchedLead(t *testing.T) {
	perms := allPermissions()
	perms["leads:anonymize"] = true
	f := newCommandsFixture(t, perms, storedLead(), joao())
	f.anonymizer.erasure = lead.Erasure{Version: 5, Counterparts: map[string]lead.RelationTally{"joao": {Version: 8}}, Rows: map[lead.ErasureTarget]int64{lead.ErasureRecord: 1}}

	got, err := f.cmds.Anonymize(context.Background(), operator(), "l-1")
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if got.LeadID != "l-1" || !got.At.Equal(cmdNow) || got.Rows[lead.ErasureRecord] != 1 {
		t.Fatalf("erasure = %+v", got)
	}
	if want := []string{cmdWorkspace + "/l-1/" + cmdUser + "/" + cmdNow.Format(time.RFC3339)}; !reflect.DeepEqual(f.anonymizer.calls, want) {
		t.Fatalf("calls = %v", f.anonymizer.calls)
	}
	if len(f.notifier.changes) != 2 ||
		f.notifier.changes[0].LeadID != "l-1" || f.notifier.changes[0].Version != 5 || !reflect.DeepEqual(f.notifier.changes[0].Fields, []string{lead.FieldAnonymized}) ||
		f.notifier.changes[1].LeadID != "joao" || f.notifier.changes[1].Version != 8 || !reflect.DeepEqual(f.notifier.changes[1].Fields, []string{lead.FieldRelations}) {
		t.Fatalf("changes = %+v", f.notifier.changes)
	}
}

func TestAnonymizeNeedsItsOwnPermissionNotTheLegacyDelete(t *testing.T) {
	perms := allPermissions()
	perms["leads:delete"] = true
	f := newCommandsFixture(t, perms, storedLead())
	if _, err := f.cmds.Anonymize(context.Background(), operator(), "l-1"); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("err = %v", err)
	}
	if len(f.anonymizer.calls) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("a refused anonymization must not erase or notify")
	}
}

func TestAnonymizeAnswersNotFoundAndPassesFailuresOn(t *testing.T) {
	perms := allPermissions()
	perms["leads:anonymize"] = true
	f := newCommandsFixture(t, perms, storedLead())
	if _, err := f.cmds.Anonymize(context.Background(), operator(), "ghost"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v", err)
	}
	f.anonymizer.err = errors.New("db down")
	if _, err := f.cmds.Anonymize(context.Background(), operator(), "l-1"); err == nil || len(f.notifier.changes) != 0 {
		t.Fatalf("err = %v, changes = %+v", err, f.notifier.changes)
	}
}
