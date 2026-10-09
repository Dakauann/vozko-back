package lead

import (
	"slices"
	"testing"
)

func TestAnOptOutWithoutASourceIsTheOperatorsAction(t *testing.T) {
	cases := []struct {
		raw  OptOutSource
		want OptOutSource
	}{
		{raw: "", want: OptOutOperator},
		{raw: "   ", want: OptOutOperator},
		{raw: "operator", want: OptOutOperator},
		{raw: " lead_request ", want: OptOutLeadRequest},
		{raw: "rumour", want: "rumour"},
	}
	for _, tc := range cases {
		if got := tc.raw.OrDefault(); got != tc.want {
			t.Fatalf("OptOutSource(%q).OrDefault() = %q, want %q", tc.raw, got, tc.want)
		}
	}
	if OptOutSource("rumour").OrDefault().Valid() {
		t.Fatal("an unknown source must stay unknown so it is refused")
	}
}

func TestIDsOfListsEveryResolvedLeadOnce(t *testing.T) {
	leads := map[string]*Lead{
		"5511999990001": {ID: "l-1"},
		"5511999990002": {ID: "l-2"},
		"5511999990003": nil,
	}
	got := IDsOf(leads)
	slices.Sort(got)
	if !slices.Equal(got, []string{"l-1", "l-2"}) {
		t.Fatalf("IDsOf() = %v, want [l-1 l-2]", got)
	}
	if got := IDsOf(nil); len(got) != 0 {
		t.Fatalf("IDsOf(nil) = %v, want none", got)
	}
}
