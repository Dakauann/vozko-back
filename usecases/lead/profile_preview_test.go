package lead_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
)

func workflowUpdate(p lead.Profile) ProfileUpdate {
	return ProfileUpdate{WorkspaceID: cmdWorkspace, Actor: "workflow:wf-1", Source: lead.ProfileFromWorkflow, Profile: p}
}

func TestAPreviewSaysWhichLeadFieldsWouldChangeAndToWhatWithoutWriting(t *testing.T) {
	f := newProfileFixture(t, storedLead())

	got, err := f.profiles.Preview(workflowUpdate(lead.Profile{
		Address:      address.Postal{ZipCode: "01310-100", City: "São Paulo", State: "SP"},
		BirthDate:    "15/03/1980",
		CustomFields: map[string]string{"interesse": "alto"},
	}))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	want := []string{lead.FieldAddresses, lead.FieldBirthDate, lead.CustomFieldName("interesse")}
	if !reflect.DeepEqual(got.Changed, want) {
		t.Fatalf("changed = %v, want %v", got.Changed, want)
	}
	if got.Values[lead.CustomFieldName("interesse")] != "alto" || got.Values[lead.FieldBirthDate] != "1980-03-15" {
		t.Fatalf("values = %v", got.Values)
	}
	if postal, ok := got.Values[lead.FieldAddresses].(address.Postal); !ok || postal.ZipCode != "01310100" {
		t.Fatalf("address = %#v", got.Values[lead.FieldAddresses])
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 || len(f.lookup.asked) != 0 {
		t.Fatalf("a preview wrote or looked up: saves %d, notified %d, lookups %v", len(f.store.saves), len(f.notifier.changes), f.lookup.asked)
	}
}

func TestAPreviewRefusesWhatTheUpdateWouldRefuse(t *testing.T) {
	cases := []struct {
		name    string
		profile lead.Profile
		update  func(*ProfileUpdate)
		defErr  error
		want    error
	}{
		{"no workspace", lead.Profile{BirthDate: "15/03/1980"}, func(u *ProfileUpdate) { u.WorkspaceID = "" }, nil, lead.ErrLeadWorkspaceRequired},
		{"an unknown source", lead.Profile{BirthDate: "15/03/1980"}, func(u *ProfileUpdate) { u.Source = "manual" }, nil, lead.ErrProfileSourceInvalid},
		{"an empty profile", lead.Profile{BirthDate: " "}, nil, nil, lead.ErrProfileEmpty},
		{"an unknown field", lead.Profile{CustomFields: map[string]string{"turno": "noite"}}, nil, nil, customfield.ErrUnknownKey},
		{"a sensitive field", lead.Profile{CustomFields: map[string]string{"classificacao": "positivo"}}, nil, nil, customfield.ErrValueForbidden},
		{"a malformed CEP", lead.Profile{Address: address.Postal{ZipCode: "12"}}, nil, nil, address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}},
		{"a birth date in the future", lead.Profile{BirthDate: "15/03/2999"}, nil, nil, lead.ErrLeadBirthDateInvalid},
		{"lead fields that cannot be read", lead.Profile{BirthDate: "15/03/1980"}, nil, errors.New("database down"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newProfileFixture(t)
			f.fields.err = tc.defErr
			in := workflowUpdate(tc.profile)
			if tc.update != nil {
				tc.update(&in)
			}
			_, err := f.profiles.Preview(in)
			if err == nil {
				t.Fatal("the preview must refuse what the update refuses")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
