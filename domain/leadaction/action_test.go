package leadaction

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workspace"
)

func blocked(v bool) *bool { return &v }

func owner(v string) *string { return &v }

func TestActionKinds(t *testing.T) {
	cases := []struct {
		action Action
		known  bool
		runs   bool
	}{
		{ActionClassify, true, true},
		{ActionAssignOwner, true, true},
		{ActionBlock, true, true},
		{ActionExport, true, false},
		{ActionMetaAudience, true, false},
		{ActionSendTemplate, true, false},
		{ActionSendUnofficial, true, false},
		{"send_sms", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		if tc.action.Known() != tc.known || tc.action.Runs() != tc.runs {
			t.Errorf("%q known = %v runs = %v, want %v %v", tc.action, tc.action.Known(), tc.action.Runs(), tc.known, tc.runs)
		}
	}
}

func TestParamsValidate(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		params Params
		want   error
	}{
		{"classify sets a value", ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, nil},
		{"classify clears with an explicit null", ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`null`)}, nil},
		{"classify without a value is refused", ActionClassify, Params{Key: "classificacao"}, ErrValueRequired},
		{"classify without a key is refused", ActionClassify, Params{Value: json.RawMessage(`"x"`)}, ErrKeyRequired},
		{"classify with an owner is ambiguous", ActionClassify, Params{Key: "k", Value: json.RawMessage(`1`), OwnerID: owner("u-1")}, ErrParamsAmbiguous},
		{"assign sets an owner", ActionAssignOwner, Params{OwnerID: owner("ai:agent-1")}, nil},
		{"assign clears the owner", ActionAssignOwner, Params{OwnerID: owner("")}, nil},
		{"assign without the owner key is refused", ActionAssignOwner, Params{}, ErrOwnerRequired},
		{"assign with a value is ambiguous", ActionAssignOwner, Params{OwnerID: owner(""), Value: json.RawMessage(`1`)}, ErrParamsAmbiguous},
		{"block", ActionBlock, Params{Blocked: blocked(true), BusinessPhoneID: "phone-1"}, nil},
		{"unblock", ActionBlock, Params{Blocked: blocked(false)}, nil},
		{"block without the flag is refused", ActionBlock, Params{}, ErrBlockedRequired},
		{"block with a key is ambiguous", ActionBlock, Params{Blocked: blocked(true), Key: "k"}, ErrParamsAmbiguous},
		{"export", ActionExport, Params{Format: "csv", Addresses: true}, nil},
		{"export picks csv when no format is sent", ActionExport, Params{}, nil},
		{"export refuses an unknown format", ActionExport, Params{Format: "pdf"}, ErrFormatUnsupported},
		{"export with a key is ambiguous", ActionExport, Params{Key: "k"}, ErrParamsAmbiguous},
		{"meta audience", ActionMetaAudience, Params{AdAccountID: "acc-1", Name: "Base"}, nil},
		{"meta audience without an account", ActionMetaAudience, Params{Name: "Base"}, ErrAdAccountRequired},
		{"meta audience without a name", ActionMetaAudience, Params{AdAccountID: "acc-1"}, ErrNameRequired},
		{"meta audience with blocked is ambiguous", ActionMetaAudience, Params{AdAccountID: "a", Name: "n", Blocked: blocked(true)}, ErrParamsAmbiguous},
		{"unknown action", "send_sms", Params{}, ErrUnknownAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params.Validate(tc.action); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRequirementsNameCapabilitiesOfTheCatalog(t *testing.T) {
	cases := []struct {
		name      string
		action    Action
		params    Params
		sensitive bool
		want      []workspace.CapabilityKey
	}{
		{"classify", ActionClassify, Params{Key: "k"}, false, []workspace.CapabilityKey{CapabilityBulkEdit}},
		{"classify a sensitive field", ActionClassify, Params{Key: "k"}, true, []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityReadSensitive}},
		{"assign", ActionAssignOwner, Params{OwnerID: owner("")}, false, []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityAssign}},
		{"block", ActionBlock, Params{Blocked: blocked(true)}, false, []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityBlock}},
		{"export", ActionExport, Params{}, false, []workspace.CapabilityKey{CapabilityExport}},
		{"export with every tier", ActionExport, Params{Addresses: true, Sensitive: true}, false, []workspace.CapabilityKey{CapabilityExport, CapabilityReadAddresses, CapabilityReadSensitive}},
		{"meta audience", ActionMetaAudience, Params{}, false, []workspace.CapabilityKey{CapabilityMetaAudience}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.action.Requirements(tc.params, tc.sensitive)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Requirements = %v, want %v", got, tc.want)
			}
			for _, key := range got {
				if _, ok := workspace.CapabilityRequires(key); !ok {
					t.Fatalf("%s is not in the access catalog", key)
				}
			}
		})
	}
	if got := Action("nope").Requirements(Params{}, false); got != nil {
		t.Fatalf("an unknown action requires %v", got)
	}
}

func TestPermissionsOfRequirements(t *testing.T) {
	got, err := PermissionsOf([]workspace.CapabilityKey{CapabilityBulkEdit, CapabilityBlock})
	if err != nil {
		t.Fatal(err)
	}
	want := map[workspace.PermissionEntry]bool{
		{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}:       true,
		{Resource: workspace.ResourceLeads, Action: workspace.ActionUpdate}:     true,
		{Resource: workspace.ResourceLeads, Action: workspace.ActionBulkUpdate}: true,
		{Resource: workspace.ResourceLeads, Action: workspace.ActionBlock}:      true,
	}
	if len(got) != len(want) {
		t.Fatalf("PermissionsOf = %v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("PermissionsOf listed %s", p.Key())
		}
	}
	if _, err := PermissionsOf([]workspace.CapabilityKey{"leads.nothing"}); !errors.Is(err, ErrRequirementUnknown) {
		t.Fatalf("an unknown capability was accepted: %v", err)
	}
}

func TestRedactedHidesTheValueOfASensitiveField(t *testing.T) {
	p := Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}
	if got := p.Redacted(true); got.Value != nil || !got.ValueRedacted || got.Key != "classificacao" {
		t.Fatalf("Redacted(true) = %+v", got)
	}
	if got := p.Redacted(false); string(got.Value) != `"Positivo"` || got.ValueRedacted {
		t.Fatalf("Redacted(false) = %+v", got)
	}
}
