package recordevent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	cases := []struct {
		name   string
		before map[string]any
		after  map[string]any
		want   []Change
	}{
		{
			name:   "nothing changed",
			before: map[string]any{"name": "Ana", "blocked": false},
			after:  map[string]any{"name": "Ana", "blocked": false},
			want:   nil,
		},
		{
			name:   "changed fields are listed in field order",
			before: map[string]any{"name": "Ana", "email": "", "blocked": false},
			after:  map[string]any{"name": "Ana Maria", "email": "ana@exemplo.com.br", "blocked": false},
			want: []Change{
				{Field: "email", Before: "", After: "ana@exemplo.com.br"},
				{Field: "name", Before: "Ana", After: "Ana Maria"},
			},
		},
		{
			name:   "a field only on one side is a change",
			before: map[string]any{},
			after:  map[string]any{"owner": "u-1"},
			want:   []Change{{Field: "owner", Before: nil, After: "u-1"}},
		},
		{
			name:   "a creation has no before",
			before: nil,
			after:  map[string]any{"name": "Ana"},
			want:   []Change{{Field: "name", Before: nil, After: "Ana"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Diff(tc.before, tc.after); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Diff = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestEventFieldsNamesEachChangedFieldOnce(t *testing.T) {
	e := Event{Kind: "updated", Changes: []Change{{Field: "name"}, {Field: "email"}, {Field: "name"}}}
	if got := e.Fields(); !reflect.DeepEqual(got, []string{"email", "name"}) {
		t.Fatalf("Fields = %v", got)
	}
}

func TestFieldsOfManyEventsAreMerged(t *testing.T) {
	events := []Event{
		{Kind: "renamed", Changes: []Change{{Field: "name"}}},
		{Kind: "blocked", Changes: []Change{{Field: "blocked"}}},
	}
	if got := FieldsOf(events); !reflect.DeepEqual(got, []string{"blocked", "name"}) {
		t.Fatalf("FieldsOf = %v", got)
	}
}

func TestEventEmpty(t *testing.T) {
	if !(Event{Kind: "updated"}).Empty() {
		t.Fatal("an event without changes is empty")
	}
	if (Event{Kind: "updated", Changes: []Change{{Field: "name"}}}).Empty() {
		t.Fatal("an event with a change is not empty")
	}
}

func TestRedactKeepsTheFieldAndDropsBothValues(t *testing.T) {
	e := Event{Actor: "u-1", Kind: "updated", Changes: Diff(
		map[string]any{"name": "Ana", "customFields.classificacao": "positivo"},
		map[string]any{"name": "Ana Souza", "customFields.classificacao": "negativo"},
	)}
	got := e.Redact(func(field string) bool { return field == "customFields.classificacao" })
	want := []Change{
		{Field: "customFields.classificacao", Redacted: true},
		{Field: "name", Before: "Ana", After: "Ana Souza"},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("changes = %+v, want %+v", got.Changes, want)
	}
	if e.Changes[0].Before != "positivo" {
		t.Fatal("redacting must not change the original event")
	}
	if got.Actor != "u-1" || got.Kind != "updated" {
		t.Fatalf("the event identity changed: %+v", got)
	}
}

func TestARedactedChangeSaysSoInJSON(t *testing.T) {
	raw, err := json.Marshal(Change{Field: "customFields.classificacao", Redacted: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"field":"customFields.classificacao","before":null,"after":null,"redacted":true}` {
		t.Fatalf("json = %s", raw)
	}
}
