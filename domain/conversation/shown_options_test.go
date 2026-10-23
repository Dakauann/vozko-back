package conversation

import (
	"reflect"
	"testing"
)

func TestAnOutcomeNamesTheOptionsTheChannelShowed(t *testing.T) {
	out := ShowingOptions(&SendOutcome{ProviderMessageID: "m1"}, []InteractiveOption{{ID: "a"}, {ID: "b"}})
	if !reflect.DeepEqual(out.ShownOptionIDs, []string{"a", "b"}) || out.ProviderMessageID != "m1" {
		t.Fatalf("outcome = %+v", out)
	}
	if ShowingOptions(nil, []InteractiveOption{{ID: "a"}}) != nil {
		t.Fatal("no outcome stays no outcome")
	}
	if got := ShowingOptions(&SendOutcome{}, nil); got.ShownOptionIDs == nil || len(got.ShownOptionIDs) != 0 {
		t.Fatalf("a channel that showed nothing reports an empty list, got %#v", got.ShownOptionIDs)
	}
}
