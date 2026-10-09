package database

import (
	"reflect"
	"testing"
)

func TestUUIDArray(t *testing.T) {
	const a = "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b"
	const b = "0d4c1f3e-2b5a-4c6d-8e9f-a1b2c3d4e5f6"
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"keeps valid ids in order", []string{a, b}, []string{a, b}},
		{"drops duplicates", []string{a, a, b}, []string{a, b}},
		{"canonicalizes case and spaces", []string{" 7F9C2BA4-E88F-4D0B-A7A2-1C2F3E4D5A6B "}, []string{a}},
		{"drops ids that cannot exist", []string{"", "x", "'; drop table leads; --", a}, []string{a}},
		{"empty input", nil, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UUIDArray(tc.in); !reflect.DeepEqual([]string(got), tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
