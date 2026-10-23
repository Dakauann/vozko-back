package selectionstore

import (
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

func TestASelectionSurvivesTheStoredJSON(t *testing.T) {
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldName, Operator: crmfilter.OpContains, Values: []string{"Ana"}}}}}}
	in := selection.Selection{
		Mode: selection.ModeFirstN, Filter: &filter, Sort: []crmfilter.Sort{{Field: "created_at"}}, Limit: 10, ExcludeIDs: []string{"x"},
		ExpectedCount: 10, Fingerprint: "f", Require: &filter,
	}
	raw, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip = %+v\nwant %+v", out, in)
	}
}

func TestTheEmailSelectionsStoredBeforeKeepTheirShape(t *testing.T) {
	raw, err := Encode(selection.Selection{Mode: selection.ModeIDs, IDs: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"mode":"ids","ids":["a","b"]}` {
		t.Fatalf("encoded = %s", raw)
	}
	old := []byte(`{"mode":"all_matching","exclude_ids":["z"],"limit":0}`)
	got, err := Decode(old)
	if err != nil || got.Mode != selection.ModeAllMatching || !reflect.DeepEqual(got.ExcludeIDs, []string{"z"}) {
		t.Fatalf("decoded = %+v, %v", got, err)
	}
}

func TestDecodeOfNothingIsTheZeroSelection(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null")} {
		if got, err := Decode(raw); err != nil || !reflect.DeepEqual(got, selection.Selection{}) {
			t.Fatalf("Decode(%q) = %+v, %v", raw, got, err)
		}
	}
	if _, err := Decode([]byte("{")); err == nil {
		t.Fatal("broken JSON decoded")
	}
}
