package database

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
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

func TestAllUUIDs(t *testing.T) {
	const a = "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b"
	cases := []struct {
		name string
		in   []string
		want bool
	}{
		{"every id is a uuid", []string{a, a}, true},
		{"one id is not a uuid", []string{a, "x"}, false},
		{"an empty id is not a uuid", []string{""}, false},
		{"nothing to check", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AllUUIDs(tc.in...); got != tc.want {
				t.Fatalf("AllUUIDs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestUUIDsCarryTheSameIDsAsUUIDArrayAndTheSameTextValue(t *testing.T) {
	const a = "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b"
	const b = "0d4c1f3e-2b5a-4c6d-8e9f-a1b2c3d4e5f6"
	in := []string{" 7F9C2BA4-E88F-4D0B-A7A2-1C2F3E4D5A6B ", "x", b, a}
	got := UUIDsOf(in)
	if !reflect.DeepEqual(got.Strings(), []string(UUIDArray(in))) {
		t.Fatalf("UUIDsOf = %v, UUIDArray = %v", got, UUIDArray(in))
	}
	value, err := got.Value()
	if err != nil {
		t.Fatal(err)
	}
	want, err := UUIDArray(in).Value()
	if err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("value %v, want %v", value, want)
	}
}

func TestUUIDsAreEncodedByPgxAsANativeUUIDArray(t *testing.T) {
	ids := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		ids = append(ids, fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
	}
	list := UUIDsOf(ids)
	m := pgtype.NewMap()
	encoded, err := m.Encode(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, list, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []string
	if err := m.Scan(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, ids) {
		t.Fatalf("decoded %d ids, first %v", len(decoded), decoded[:1])
	}
	allocs := testing.AllocsPerRun(5, func() {
		if _, err := m.Encode(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, list, nil); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > float64(3*len(ids)) {
		t.Fatalf("encoding 5000 ids took %.0f allocations, it went through the text value", allocs)
	}
}

func TestTextsAreEncodedByPgxAsANativeTextArrayWithThePqTextValue(t *testing.T) {
	words := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		words = append(words, fmt.Sprintf("lead%d@escola.example", i))
	}
	value, err := Texts(words).Value()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := pq.StringArray(words).Value()
	if value != want {
		t.Fatal("the text value differs from pq")
	}
	m := pgtype.NewMap()
	encoded, err := m.Encode(pgtype.TextArrayOID, pgtype.BinaryFormatCode, Texts(words), nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []string
	if err := m.Scan(pgtype.TextArrayOID, pgtype.BinaryFormatCode, encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, words) {
		t.Fatalf("decoded %d words, %v", len(decoded), err)
	}
	allocs := testing.AllocsPerRun(5, func() {
		if _, err := m.Encode(pgtype.TextArrayOID, pgtype.BinaryFormatCode, Texts(words), nil); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > float64(3*len(words)) {
		t.Fatalf("encoding 5000 texts took %.0f allocations, it went through the text value", allocs)
	}
}
