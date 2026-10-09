package leadimport

import (
	"reflect"
	"testing"
)

func TestPlanContactLookupsBoundsEveryLoad(t *testing.T) {
	cases := []struct {
		name      string
		phones    [][]string
		counts    map[string]int
		limit     int
		ambiguous map[int]bool
		groups    [][]int
	}{
		{
			name:      "phones held by few leads are loaded together",
			phones:    [][]string{{"551133330000"}, {"551144440000", "5511955550000"}},
			counts:    map[string]int{"551133330000": 2, "551144440000": 1},
			limit:     10,
			ambiguous: map[int]bool{},
			groups:    [][]int{{0, 1}},
		},
		{
			name:      "a phone held past the limit makes its rows ambiguous without loading anything",
			phones:    [][]string{{"551133330000"}, {"551144440000"}, {"551144440000", "551133330000"}},
			counts:    map[string]int{"551133330000": 11, "551144440000": 3},
			limit:     10,
			ambiguous: map[int]bool{0: true, 2: true},
			groups:    [][]int{{1}},
		},
		{
			name:      "a row whose phones together go past the limit is ambiguous",
			phones:    [][]string{{"551133330000", "551144440000"}, {"551133330000"}},
			counts:    map[string]int{"551133330000": 6, "551144440000": 6},
			limit:     10,
			ambiguous: map[int]bool{0: true},
			groups:    [][]int{{1}},
		},
		{
			name:      "loads are split when the next row would go past the limit",
			phones:    [][]string{{"551133330000"}, {"551144440000"}, {"551133330000"}, {"551155550000"}},
			counts:    map[string]int{"551133330000": 6, "551144440000": 4, "551155550000": 3},
			limit:     10,
			ambiguous: map[int]bool{},
			groups:    [][]int{{0, 1, 2}, {3}},
		},
		{
			name:      "both ninth digit formats of a phone count",
			phones:    [][]string{{"5511987650000"}},
			counts:    map[string]int{"5511987650000": 6, "551187650000": 6},
			limit:     10,
			ambiguous: map[int]bool{0: true},
			groups:    nil,
		},
		{
			name:      "rows without contact phones are left out",
			phones:    [][]string{nil, {"551133330000"}, {}},
			counts:    map[string]int{},
			limit:     10,
			ambiguous: map[int]bool{},
			groups:    [][]int{{1}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanContactLookups(tc.phones, tc.counts, tc.limit)
			if !reflect.DeepEqual(got.Ambiguous, tc.ambiguous) {
				t.Fatalf("ambiguous = %v, want %v", got.Ambiguous, tc.ambiguous)
			}
			if !reflect.DeepEqual(got.Groups, tc.groups) {
				t.Fatalf("groups = %v, want %v", got.Groups, tc.groups)
			}
		})
	}
}

func TestPlanContactLookupsNamesThePhonesOfEachGroup(t *testing.T) {
	phones := [][]string{{"551133330000"}, {"551133330000", "551144440000"}}
	plan := PlanContactLookups(phones, map[string]int{}, 10)
	if got := plan.PhonesOf(phones, 0); !reflect.DeepEqual(got, []string{"551133330000", "551144440000"}) {
		t.Fatalf("phones = %v", got)
	}
}
