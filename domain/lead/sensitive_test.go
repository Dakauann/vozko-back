package lead

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/recordevent"
)

func leadFieldDefs() []*customfield.Definition {
	return []*customfield.Definition{
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"positivo", "negativo"}, Sensitive: true, LegalBasis: "consentimento"},
	}
}

func recordWithEverything() *Lead {
	return &Lead{
		ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Maria", Version: 4,
		CustomFields: map[string]any{"interesse": "alto", "classificacao": "positivo", "removida": "x"},
		Phones:       []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline}},
		Addresses:    []Address{storedHome(&cepFix, GeoApproximate)},
		Relations:    []Relation{},
	}
}

func fullReader() Viewer {
	return Viewer{ReadsLeads: true, ReadsAddresses: true, Fields: customfield.Viewer{ReadsSensitive: true}, Definitions: leadFieldDefs()}
}

func TestVisibleFieldsForAFullReaderKeepsEverythingWithAField(t *testing.T) {
	l := recordWithEverything()
	got := VisibleFields(l, fullReader())
	if !reflect.DeepEqual(got.CustomFields, map[string]any{"interesse": "alto", "classificacao": "positivo"}) {
		t.Fatalf("custom fields = %v; values without a field never leave the server", got.CustomFields)
	}
	if !reflect.DeepEqual(got.Addresses, l.Addresses) || got.Number != l.Number || got.Name != "Maria" {
		t.Fatalf("a full reader sees the whole record: %+v", got)
	}
	if got == l {
		t.Fatal("the projection must be a copy")
	}
}

func TestVisibleFieldsHidesSensitiveValuesWithoutThePermission(t *testing.T) {
	v := fullReader()
	v.Fields = customfield.Viewer{}
	got := VisibleFields(recordWithEverything(), v)
	if !reflect.DeepEqual(got.CustomFields, map[string]any{"interesse": "alto"}) {
		t.Fatalf("custom fields = %v", got.CustomFields)
	}
}

func TestVisibleFieldsShowsOnlyTheAreaOfAnAddressWithoutTheAddressPermission(t *testing.T) {
	v := fullReader()
	v.ReadsAddresses = false
	l := recordWithEverything()
	got := VisibleFields(l, v)
	want := Address{ID: "a-1", Label: AddressHome, Primary: true, GeoStatus: GeoApproximate,
		Postal: address.Postal{District: "Bela Vista", City: "São Paulo", State: "SP"}}
	if len(got.Addresses) != 1 || !reflect.DeepEqual(got.Addresses[0], want) {
		t.Fatalf("addresses = %+v, want %+v", got.Addresses, want)
	}
	if l.Addresses[0].Postal.Street == "" || l.Addresses[0].Fix == nil {
		t.Fatal("the projection must not change the stored record")
	}
}

func TestVisibleFieldsWithoutDefinitionsHidesEveryCustomValue(t *testing.T) {
	v := fullReader()
	v.Definitions = nil
	if got := VisibleFields(recordWithEverything(), v); len(got.CustomFields) != 0 {
		t.Fatalf("custom fields = %v", got.CustomFields)
	}
}

func TestVisibleToSomeoneWhoCannotReadLeadsShowsOnlyTheCustomKeysTheySent(t *testing.T) {
	v := fullReader()
	v.ReadsLeads = false
	v.ReadsAddresses = false
	got := recordWithEverything().VisibleTo(v, []string{CustomFieldName("interesse"), FieldAddresses})
	if !reflect.DeepEqual(got.CustomFields, map[string]any{"interesse": "alto"}) {
		t.Fatalf("custom fields = %v", got.CustomFields)
	}
	if len(got.Addresses) != 1 || got.Addresses[0].Postal.Street != "" {
		t.Fatalf("the address rule applies to what was sent too: %+v", got.Addresses)
	}
	if got.Number != "" || got.Phones != nil {
		t.Fatalf("fields that were not sent leaked: %+v", got)
	}
}

func TestCustomFieldsGoThroughTheirOwnStep(t *testing.T) {
	if _, err := New("ws-1", Draft{Name: "Ana", CustomFields: map[string]any{"interesse": "alto"}}, recordNow); !errors.Is(err, ErrCustomFieldsUnchecked) {
		t.Fatalf("New = %v, want %v", err, ErrCustomFieldsUnchecked)
	}
	l := recordWithEverything()
	if err := l.ApplyEdit(Edit{CustomFields: map[string]any{"interesse": "baixo"}}, recordNow); !errors.Is(err, ErrCustomFieldsUnchecked) {
		t.Fatalf("ApplyEdit = %v, want %v", err, ErrCustomFieldsUnchecked)
	}
}

func TestSetCustomFieldsMergesAndKeepsWhatTheCallerCannotSee(t *testing.T) {
	l := recordWithEverything()
	if err := l.SetCustomFields(map[string]any{"interesse": "baixo"}, leadFieldDefs(), customfield.Viewer{}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"interesse": "baixo", "classificacao": "positivo", "removida": "x"}
	if !reflect.DeepEqual(l.CustomFields, want) {
		t.Fatalf("custom fields = %v, want %v", l.CustomFields, want)
	}
	err := l.SetCustomFields(map[string]any{"classificacao": "negativo"}, leadFieldDefs(), customfield.Viewer{})
	if !errors.Is(err, customfield.ErrValueForbidden) || l.CustomFields["classificacao"] != "positivo" {
		t.Fatalf("err = %v, values = %v", err, l.CustomFields)
	}
}

func TestEditFieldsNameEachCustomKey(t *testing.T) {
	e := Edit{Name: text("Ana"), CustomFields: map[string]any{"interesse": "alto", "classificacao": nil}}
	want := []string{FieldName, CustomFieldName("classificacao"), CustomFieldName("interesse")}
	if got := e.Fields(); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

func TestChangesRecordCustomFieldsAndRedactSensitiveOnes(t *testing.T) {
	before := recordWithEverything()
	after := recordWithEverything()
	after.CustomFields = map[string]any{"interesse": "baixo", "classificacao": "negativo", "removida": "y"}
	event := Changes(EventUpdated, "u-1", before, after, leadFieldDefs())
	want := []recordevent.Change{
		{Field: CustomFieldName("classificacao"), Redacted: true},
		{Field: CustomFieldName("interesse"), Before: "alto", After: "baixo"},
		{Field: CustomFieldName("removida"), Redacted: true},
	}
	if !reflect.DeepEqual(event.Changes, want) {
		t.Fatalf("changes = %+v, want %+v", event.Changes, want)
	}
}

func TestChangesWithoutDefinitionsRedactEveryCustomValue(t *testing.T) {
	before := recordWithEverything()
	after := recordWithEverything()
	after.CustomFields = map[string]any{"interesse": "baixo", "classificacao": "positivo", "removida": "x"}
	event := Changes(EventMerged, "system", before, after, nil)
	if len(event.Changes) != 1 || !event.Changes[0].Redacted || event.Changes[0].Before != nil {
		t.Fatalf("changes = %+v", event.Changes)
	}
}
