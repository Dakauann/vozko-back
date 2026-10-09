package customfield

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workspace"
)

func TestRequiredPermissions_MapsEachObjectToItsOwnResource(t *testing.T) {
	leads := func(action workspace.Action) workspace.PermissionEntry {
		return workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: action}
	}
	conversations := func(action workspace.Action) workspace.PermissionEntry {
		return workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: action}
	}
	configure, readSensitive := leads(workspace.ActionConfigure), leads(workspace.ActionReadSensitive)
	cases := []struct {
		name      string
		object    ObjectType
		op        Operation
		sensitive bool
		want      []workspace.PermissionEntry
	}{
		{"read a lead field", ObjectLead, OperationRead, false, []workspace.PermissionEntry{leads(workspace.ActionRead)}},
		{"read a sensitive lead field", ObjectLead, OperationRead, true, []workspace.PermissionEntry{leads(workspace.ActionRead)}},
		{"create a lead field", ObjectLead, OperationCreate, false, []workspace.PermissionEntry{configure}},
		{"update a lead field", ObjectLead, OperationUpdate, false, []workspace.PermissionEntry{configure}},
		{"delete a lead field", ObjectLead, OperationDelete, false, []workspace.PermissionEntry{configure}},
		{"create a sensitive lead field", ObjectLead, OperationCreate, true, []workspace.PermissionEntry{configure, readSensitive}},
		{"update a sensitive lead field", ObjectLead, OperationUpdate, true, []workspace.PermissionEntry{configure, readSensitive}},
		{"delete a sensitive lead field", ObjectLead, OperationDelete, true, []workspace.PermissionEntry{configure, readSensitive}},
		{"read an opportunity field", ObjectOpportunity, OperationRead, false, []workspace.PermissionEntry{conversations(workspace.ActionRead)}},
		{"create an opportunity field", ObjectOpportunity, OperationCreate, false, []workspace.PermissionEntry{conversations(workspace.ActionCreate)}},
		{"update an opportunity field", ObjectOpportunity, OperationUpdate, false, []workspace.PermissionEntry{conversations(workspace.ActionUpdate)}},
		{"delete an opportunity field", ObjectOpportunity, OperationDelete, false, []workspace.PermissionEntry{conversations(workspace.ActionDelete)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RequiredPermissions(tc.object, tc.op, tc.sensitive)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("RequiredPermissions = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	if _, err := RequiredPermissions("contract", OperationRead, false); !errors.Is(err, ErrInvalidObjectType) {
		t.Fatalf("an unknown object = %v, want ErrInvalidObjectType", err)
	}
	if _, err := RequiredPermissions(ObjectLead, "rename", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an unknown operation = %v, want ErrForbidden", err)
	}
}

func TestTouchesSensitiveData_WhenTheFieldIsSensitiveBeforeOrAfter(t *testing.T) {
	plain := &Definition{ObjectType: ObjectLead}
	sensitive := &Definition{ObjectType: ObjectLead, Sensitive: true}
	cases := []struct {
		name          string
		before, after *Definition
		want          bool
	}{
		{"a plain field stays plain", plain, plain, false},
		{"a sensitive field loses the flag", sensitive, plain, true},
		{"a plain field gains the flag", plain, sensitive, true},
		{"a sensitive field stays sensitive", sensitive, sensitive, true},
		{"a new sensitive field", nil, sensitive, true},
		{"a deleted sensitive field", sensitive, nil, true},
		{"nothing at all", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TouchesSensitiveData(tc.before, tc.after); got != tc.want {
				t.Fatalf("TouchesSensitiveData = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreationTouchesSensitiveData_WhenTheKeyOnceHeldSensitiveValues(t *testing.T) {
	plain := &Definition{ObjectType: ObjectLead, Key: "classificacao"}
	sensitive := &Definition{ObjectType: ObjectLead, Key: "classificacao", Sensitive: true}
	retiredPlain := &Definition{ObjectType: ObjectLead, Key: "classificacao"}
	retiredSensitive := &Definition{ObjectType: ObjectLead, Key: "classificacao", Sensitive: true}
	cases := []struct {
		name    string
		retired []*Definition
		created *Definition
		want    bool
	}{
		{"a fresh plain key", nil, plain, false},
		{"a fresh sensitive key", nil, sensitive, true},
		{"a plain key reused after plain deletions", []*Definition{retiredPlain, retiredPlain}, plain, false},
		{"a key reused after a sensitive deletion", []*Definition{retiredSensitive}, plain, true},
		{"a sensitive deletion among plain ones", []*Definition{retiredPlain, retiredSensitive, retiredPlain}, plain, true},
		{"a nil entry in the history", []*Definition{nil}, plain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CreationTouchesSensitiveData(tc.retired, tc.created); got != tc.want {
				t.Fatalf("CreationTouchesSensitiveData = %v, want %v", got, tc.want)
			}
		})
	}
}
