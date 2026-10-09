package leadaction

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workspace"
)

func callListParams() *CallListParams {
	return &CallListParams{Name: "Retorno", AssigneeIDs: []string{"worker-1"}, PhoneSource: "identity"}
}

func TestTheCallListActionCreatesAListFromTheSelection(t *testing.T) {
	if !ActionCallList.Known() || ActionCallList.Runs() {
		t.Fatal("call_list is a known action whose downstream is the call list, not a lead action run")
	}
	got := ActionCallList.Requirements(Params{CallList: callListParams()}, false)
	if !reflect.DeepEqual(got, []workspace.CapabilityKey{CapabilityManageCallLists}) {
		t.Fatalf("Requirements = %v", got)
	}
	if _, ok := workspace.CapabilityRequires(CapabilityManageCallLists); !ok {
		t.Fatalf("%s is not in the access catalog", CapabilityManageCallLists)
	}
}

func TestCallListParamsBelongToTheCallListActionOnly(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		params Params
		want   error
	}{
		{"a call list", ActionCallList, Params{CallList: callListParams()}, nil},
		{"a call list without its parameters", ActionCallList, Params{}, ErrCallListRequired},
		{"a call list with a key", ActionCallList, Params{CallList: callListParams(), Key: "k"}, ErrParamsAmbiguous},
		{"a call list with an owner", ActionCallList, Params{CallList: callListParams(), OwnerID: owner("u")}, ErrParamsAmbiguous},
		{"a call list with an audience name", ActionCallList, Params{CallList: callListParams(), Name: "Base"}, ErrParamsAmbiguous},
		{"a call list with export columns", ActionCallList, Params{CallList: callListParams(), Addresses: true}, ErrParamsAmbiguous},
		{"a classification carrying call list parameters", ActionClassify, Params{Key: "k", Value: json.RawMessage(`1`), CallList: callListParams()}, ErrParamsAmbiguous},
		{"an export carrying call list parameters", ActionExport, Params{CallList: callListParams()}, ErrParamsAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params.Validate(tc.action); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCallListParamsAreTrimmed(t *testing.T) {
	p := Params{CallList: &CallListParams{Name: "  Lista ", PhoneSource: " contact ", PhoneLabel: " landline "}}.Normalized()
	if p.CallList.Name != "Lista" || p.CallList.PhoneSource != "contact" || p.CallList.PhoneLabel != "landline" {
		t.Fatalf("normalized = %+v", p.CallList)
	}
}
