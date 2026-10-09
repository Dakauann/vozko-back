package leadaction

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/selection"
)

func classification(sensitive bool) []*customfield.Definition {
	return []*customfield.Definition{{
		Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect,
		Options: []string{"Positivo", "Negativo"}, Sensitive: sensitive,
	}}
}

func TestEditForClassify(t *testing.T) {
	edit, err := EditFor(ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, classification(false), customfield.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	want := Edit{Kind: EditCustomField, Field: lead.CustomFieldName("classificacao"), Key: "classificacao", Value: "Positivo", Recorded: true, EventKind: lead.EventUpdated}
	if !reflect.DeepEqual(edit, want) {
		t.Fatalf("edit = %#v, want %#v", edit, want)
	}

	cleared, err := EditFor(ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`null`)}, classification(false), customfield.Viewer{})
	if err != nil || cleared.Value != nil {
		t.Fatalf("a clear = %#v, %v", cleared, err)
	}
}

func TestEditForClassifyRefusals(t *testing.T) {
	cases := []struct {
		name   string
		params Params
		defs   []*customfield.Definition
		viewer customfield.Viewer
		want   error
	}{
		{"a value outside the options", Params{Key: "classificacao", Value: json.RawMessage(`"Talvez"`)}, classification(false), customfield.Viewer{}, customfield.ErrValueNotInOptions},
		{"a value of the wrong type", Params{Key: "classificacao", Value: json.RawMessage(`3`)}, classification(false), customfield.Viewer{}, customfield.ErrValueType},
		{"an unknown field", Params{Key: "cor", Value: json.RawMessage(`"azul"`)}, classification(false), customfield.Viewer{}, customfield.ErrUnknownKey},
		{"a sensitive field without the permission", Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, classification(true), customfield.Viewer{}, customfield.ErrValueForbidden},
		{"a value that is not json", Params{Key: "classificacao", Value: json.RawMessage(`{`)}, classification(false), customfield.Viewer{}, ErrValueRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EditFor(ActionClassify, tc.params, tc.defs, tc.viewer); !errors.Is(err, tc.want) {
				t.Fatalf("EditFor = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestASensitiveClassificationIsNotRecorded(t *testing.T) {
	edit, err := EditFor(ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, classification(true), customfield.Viewer{ReadsSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	event := edit.Event("user-1", "Negativo")
	want := recordevent.Event{Actor: "user-1", Kind: lead.EventUpdated, Changes: []recordevent.Change{{Field: lead.CustomFieldName("classificacao"), Redacted: true}}}
	if !reflect.DeepEqual(event, want) {
		t.Fatalf("event = %#v", event)
	}
}

func TestEditEvents(t *testing.T) {
	block, _ := EditFor(ActionBlock, Params{Blocked: blocked(true)}, nil, customfield.Viewer{})
	unblock, _ := EditFor(ActionBlock, Params{Blocked: blocked(false)}, nil, customfield.Viewer{})
	assign, _ := EditFor(ActionAssignOwner, Params{OwnerID: owner("ai:agent-1")}, nil, customfield.Viewer{})
	unassign, _ := EditFor(ActionAssignOwner, Params{OwnerID: owner("")}, nil, customfield.Viewer{})
	classify, _ := EditFor(ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, classification(false), customfield.Viewer{})
	cases := []struct {
		name   string
		edit   Edit
		before any
		want   recordevent.Event
	}{
		{"block", block, nil, recordevent.Event{Actor: "u", Kind: lead.EventBlocked, Changes: []recordevent.Change{{Field: lead.FieldBlocked, After: true}}}},
		{"unblock", unblock, nil, recordevent.Event{Actor: "u", Kind: lead.EventUnblocked, Changes: []recordevent.Change{{Field: lead.FieldBlocked, Before: true}}}},
		{"assign", assign, "user-9", recordevent.Event{Actor: "u", Kind: lead.EventOwnerChange, Changes: []recordevent.Change{{Field: lead.FieldOwner, Before: "user-9", After: "ai:agent-1"}}}},
		{"assign from nobody", assign, "", recordevent.Event{Actor: "u", Kind: lead.EventOwnerChange, Changes: []recordevent.Change{{Field: lead.FieldOwner, After: "ai:agent-1"}}}},
		{"unassign", unassign, "user-9", recordevent.Event{Actor: "u", Kind: lead.EventOwnerChange, Changes: []recordevent.Change{{Field: lead.FieldOwner, Before: "user-9"}}}},
		{"classify", classify, "Negativo", recordevent.Event{Actor: "u", Kind: lead.EventUpdated, Changes: []recordevent.Change{{Field: lead.CustomFieldName("classificacao"), Before: "Negativo", After: "Positivo"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.edit.Event("u", tc.before); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("event = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestEditForAssignRefusesAMalformedOwner(t *testing.T) {
	if _, err := EditFor(ActionAssignOwner, Params{OwnerID: owner("ai:")}, nil, customfield.Viewer{}); !errors.Is(err, lead.ErrLeadOwnerInvalid) {
		t.Fatalf("a malformed owner was accepted: %v", err)
	}
}

func TestEditForRefusesActionsThatDoNotEdit(t *testing.T) {
	if _, err := EditFor(ActionExport, Params{}, nil, customfield.Viewer{}); !errors.Is(err, ErrNotARun) {
		t.Fatalf("an export became an edit: %v", err)
	}
}

func TestAQuantitySelectionSkipsUnchangedLeadsBeforeItsLimit(t *testing.T) {
	block, _ := EditFor(ActionBlock, Params{Blocked: blocked(true)}, nil, customfield.Viewer{})
	assign, _ := EditFor(ActionAssignOwner, Params{OwnerID: owner("ai:agent-1")}, nil, customfield.Viewer{})
	classify, _ := EditFor(ActionClassify, Params{Key: "classificacao", Value: json.RawMessage(`"Positivo"`)}, classification(false), customfield.Viewer{})
	firstN := selection.Selection{Mode: selection.ModeFirstN, Limit: 10}
	cases := []struct {
		name string
		edit Edit
		s    selection.Selection
		want *lead.Assignment
	}{
		{"block over the first n", block, firstN, &lead.Assignment{Kind: lead.AssignBlocked, Value: true}},
		{"assign over the first n", assign, firstN, &lead.Assignment{Kind: lead.AssignOwner, Value: "ai:agent-1"}},
		{"classify over the first n", classify, firstN, &lead.Assignment{Kind: lead.AssignCustomField, Key: "classificacao", Value: "Positivo"}},
		{"a filter keeps its unchanged leads to count them", block, selection.Selection{Mode: selection.ModeAllMatching}, nil},
		{"picked ids keep theirs", block, selection.Selection{Mode: selection.ModeIDs}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.edit.PendingFor(tc.s); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pending = %#v, want %#v", got, tc.want)
			}
		})
	}
}
