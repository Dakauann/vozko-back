package lead

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func TestSelectionOrder(t *testing.T) {
	cases := []struct {
		name  string
		sorts []crmfilter.Sort
		want  []shared.Sort
		err   error
	}{
		{"no sort keeps the list default", nil, []shared.Sort{DefaultSort}, nil},
		{"a list sort key is kept with its direction", []crmfilter.Sort{{Field: "name"}, {Field: "createdAt", Desc: true}},
			[]shared.Sort{{Field: string(SortName), Direction: shared.SortAsc}, {Field: string(SortCreatedAt), Direction: shared.SortDesc}}, nil},
		{"keys are read in any case", []crmfilter.Sort{{Field: "RELATIVESCOUNT", Desc: true}},
			[]shared.Sort{{Field: string(SortRelatives), Direction: shared.SortDesc}}, nil},
		{"an unknown key is refused", []crmfilter.Sort{{Field: "phone"}}, nil, selection.ErrUnknownSort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectionOrder(tc.sorts)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("order = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSelectionIDs(t *testing.T) {
	a, b := "1b4e28ba-2fa1-11d2-883f-0016d3cca427", "6fa459ea-ee8a-3ca4-894e-db77e160355e"
	cases := []struct {
		name string
		ids  []string
		want []string
		err  error
	}{
		{"ids are trimmed, lowered and kept once", []string{" " + a + " ", "6FA459EA-EE8A-3CA4-894E-DB77E160355E", a}, []string{a, b}, nil},
		{"no ids is nothing", nil, []string{}, nil},
		{"a malformed id refuses the whole list", []string{a, "not-an-id"}, nil, selection.ErrInvalidIDs},
		{"a blank id refuses the whole list", []string{a, " "}, nil, selection.ErrInvalidIDs},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectionIDs(tc.ids)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectionQueryShape(t *testing.T) {
	q := SelectionQuery{WorkspaceID: "ws-1", Limit: 10}
	if !q.Ordered() {
		t.Fatal("a limited selection is read in its chosen order")
	}
	if (SelectionQuery{WorkspaceID: "ws-1"}).Ordered() {
		t.Fatal("an unlimited selection is read in id order")
	}
	if err := (SelectionQuery{}).Validate(); !errors.Is(err, ErrLeadWorkspaceRequired) {
		t.Fatalf("a query without a workspace was accepted: %v", err)
	}
	if err := (SelectionQuery{WorkspaceID: "ws-1", Limit: -1}).Validate(); !errors.Is(err, selection.ErrLimitRequired) {
		t.Fatalf("a negative limit was accepted: %v", err)
	}
	if err := (SelectionQuery{WorkspaceID: "ws-1", Limit: 5, IDs: []string{"1b4e28ba-2fa1-11d2-883f-0016d3cca427"}}).Validate(); !errors.Is(err, selection.ErrLimitRequired) {
		t.Fatalf("a limit over picked ids was accepted: %v", err)
	}
}

func TestSelectionQueryRefusesAnUnknownPendingAssignment(t *testing.T) {
	cases := []struct {
		name    string
		pending *Assignment
		want    error
	}{
		{"no pending assignment", nil, nil},
		{"a custom field", &Assignment{Kind: AssignCustomField, Key: "interesse", Value: "alto"}, nil},
		{"an owner", &Assignment{Kind: AssignOwner, Value: ""}, nil},
		{"a block", &Assignment{Kind: AssignBlocked, Value: true}, nil},
		{"a custom field without its key", &Assignment{Kind: AssignCustomField, Value: "alto"}, ErrAssignmentInvalid},
		{"a block without a flag", &Assignment{Kind: AssignBlocked, Value: "yes"}, ErrAssignmentInvalid},
		{"an unknown kind", &Assignment{Kind: "stage"}, ErrAssignmentInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (SelectionQuery{WorkspaceID: "ws-1", Pending: tc.pending}).Validate()
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}
