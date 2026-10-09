package crmfilter

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLeadFields_AcceptTheirValues(t *testing.T) {
	cases := []struct {
		name string
		p    Predicate
	}{
		{"id set", pred(FieldID, OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f")},
		{"any phone in either ninth digit format", pred(FieldPhoneAny, OpIn, "+55 (11) 98765-4321", "551187654321")},
		{"email", pred(FieldEmail, OpContains, "gmail")},
		{"nickname", pred(FieldNickname, OpContains, "zé")},
		{"birthday today", pred(FieldBirthday, OpEquals, BirthdayToday)},
		{"birthday this week or month", pred(FieldBirthday, OpIn, BirthdayThisWeek, BirthdayThisMonth)},
		{"birth date range", pred(FieldBirthDate, OpBetween, "1980-01-01", "1990-12-31")},
		{"source", pred(FieldSource, OpIn, "manual")},
		{"zip with mask", pred(FieldZip, OpIn, "01310-100")},
		{"state", pred(FieldState, OpIn, "sp", "MG")},
		{"city key", pred(FieldCity, OpIn, "sp:sao paulo")},
		{"district pair", pred(FieldDistrict, OpIn, "sp:sao paulo/jardim paulista", "mg:contagem/centro")},
		{"geo precision", pred(FieldGeoPrecision, OpIn, "address")},
		{"geo status", pred(FieldGeoStatus, OpEquals, "pending")},
		{"has address", pred(FieldHasAddress, OpIsTrue)},
		{"has identity", pred(FieldHasIdentity, OpIsFalse)},
		{"opted out", pred(FieldOptedOut, OpIsTrue)},
		{"whatsapp opt in", pred(FieldWhatsAppOptIn, OpEquals, "true")},
		{"relation kind", pred(FieldRelationKind, OpIn, "parent", "sibling")},
		{"relatives count", pred(FieldRelativesCount, OpGreaterEq, "2")},
		{"referred count", pred(FieldReferredCount, OpBetween, "1", "10")},
		{"referred by", pred(FieldReferredBy, OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f")},
		{"every geo placement", pred(FieldGeoPlacement, OpIn, "on_map", "approximate", "without_address", "not_found", "pending", "quota_exceeded", "refused")},
		{"a geo placement left out", pred(FieldGeoPlacement, OpNotEquals, "on_map")},
		{"approximate positions inside an area", pred(FieldAreaApproximate, OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := filterOf(tc.p).Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestLeadFields_RefuseValuesThatCannotMatch(t *testing.T) {
	tooManyPairs := make([]string, MaxDistrictPairs+1)
	for i := range tooManyPairs {
		tooManyPairs[i] = "sp:sao paulo/bairro " + strings.Repeat("x", i%7+1) + string(rune('a'+i%26))
	}
	cases := []struct {
		name string
		p    Predicate
		want error
	}{
		{"an id that is not a uuid", pred(FieldID, OpIn, "1 OR 1=1"), ErrInvalidValue},
		{"a referrer that is not a uuid", pred(FieldReferredBy, OpEquals, "abc"), ErrInvalidValue},
		{"a phone that is not a phone", pred(FieldPhoneAny, OpIn, "12"), ErrInvalidValue},
		{"an unknown birthday window", pred(FieldBirthday, OpEquals, "next_year"), ErrInvalidValue},
		{"a negated birthday window", pred(FieldBirthday, OpNotEquals, BirthdayToday), ErrUnsupportedOp},
		{"a bare bairro without its city", pred(FieldDistrict, OpIn, "centro"), ErrInvalidValue},
		{"a pair with an empty bairro", pred(FieldDistrict, OpIn, "sp:sao paulo/"), ErrInvalidValue},
		{"a pair with two separators", pred(FieldDistrict, OpIn, "sp:sao paulo/centro/norte"), ErrInvalidValue},
		{"more pairs than one predicate may bind", pred(FieldDistrict, OpIn, tooManyPairs...), ErrTooManyValues},
		{"a zip with letters", pred(FieldZip, OpIn, "0131O100"), ErrInvalidValue},
		{"a state with three letters", pred(FieldState, OpIn, "SPX"), ErrInvalidValue},
		{"a city name instead of a city key", pred(FieldCity, OpIn, "Sao Paulo"), ErrInvalidValue},
		{"a city key in an unknown state", pred(FieldCity, OpIn, "xx:sao paulo"), ErrInvalidValue},
		{"more zips than one predicate may bind", pred(FieldZip, OpIn, manyZips(MaxZipValues+1)...), ErrTooManyValues},
		{"a count that is not a number", pred(FieldRelativesCount, OpGreaterEq, "dois"), ErrInvalidNumber},
		{"an id with a presence operator", pred(FieldID, OpIsSet), ErrUnsupportedOp},
		{"an unknown geo placement", pred(FieldGeoPlacement, OpIn, "somewhere"), ErrInvalidValue},
		{"a geo placement in another case", pred(FieldGeoPlacement, OpIn, "ON_MAP"), ErrInvalidValue},
		{"a geo placement presence test", pred(FieldGeoPlacement, OpIsSet), ErrUnsupportedOp},
		{"an approximate area that is not a uuid", pred(FieldAreaApproximate, OpIn, "north"), ErrInvalidValue},
		{"an approximate area excluded", pred(FieldAreaApproximate, OpNotIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"), ErrUnsupportedOp},
		{"more approximate areas than one predicate may bind", pred(FieldAreaApproximate, OpIn, manyAreaIDs(MaxAreas+1)...), ErrTooManyValues},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := filterOf(tc.p).Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseCityKey_GivesTheStoredKeyOrRefuses(t *testing.T) {
	if got, err := ParseCityKey(" SP:São Paulo "); err != nil || got != "sp:sao paulo" {
		t.Fatalf("ParseCityKey() = %q, %v", got, err)
	}
	if _, err := ParseCityKey("Sao Paulo"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("a bare name = %v, want ErrInvalidValue", err)
	}
}

func manyAreaIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("0b7c3f1e-2d4a-4c5b-9e8f-%012d", i)
	}
	return out
}

func TestGeoPlacementsNameEverySummaryBucketOnce(t *testing.T) {
	want := []GeoPlacement{PlacementOnMap, PlacementApproximate, PlacementWithoutAddress, PlacementNotFound, PlacementPending, PlacementQuotaExceeded, PlacementRefused}
	if got := GeoPlacements(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GeoPlacements() = %v, want %v", got, want)
	}
	for _, p := range want {
		if !p.Valid() {
			t.Fatalf("%q must be valid", p)
		}
	}
	if GeoPlacement("located").Valid() || GeoPlacement("").Valid() {
		t.Fatal("only the summary buckets are placements")
	}
}

func manyZips(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%08d", 1310100+i)
	}
	return out
}

func TestPhoneAnyNumbers_ExpandsBothNinthDigitFormatsOnce(t *testing.T) {
	got, err := PhoneAnyNumbers([]string{"+55 11 98765-4321", "5511987654321", " "})
	if err != nil {
		t.Fatalf("PhoneAnyNumbers() error = %v", err)
	}
	want := []string{"5511987654321", "551187654321"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PhoneAnyNumbers() = %v, want %v", got, want)
	}
	if _, err := PhoneAnyNumbers([]string{"999"}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("an unreadable phone must refuse, got %v", err)
	}
}

func TestParseDistrictPair(t *testing.T) {
	city, district, err := ParseDistrictPair(" sp:sao paulo/jardim paulista ")
	if err != nil || city != "sp:sao paulo" || district != "jardim paulista" {
		t.Fatalf("ParseDistrictPair() = %q, %q, %v", city, district, err)
	}
	if got := DistrictPair("sp:sao paulo", "centro"); got != "sp:sao paulo/centro" {
		t.Fatalf("DistrictPair() = %q", got)
	}
	city, district, err = ParseDistrictPair("SP:São Paulo/Jd. Paulista")
	if err != nil || city != "sp:sao paulo" || district != "jardim paulista" {
		t.Fatalf("ParseDistrictPair() must give the stored keys, got %q, %q, %v", city, district, err)
	}
	for _, bad := range []string{"", "centro", "/centro", "sp:sao paulo/", "a/b/c", "saopaulo/centro", "xx:sao paulo/centro"} {
		if _, _, err := ParseDistrictPair(bad); !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("ParseDistrictPair(%q) = %v, want ErrInvalidValue", bad, err)
		}
	}
}

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 15, 0, 0, 0, time.UTC)
}

func TestBirthdayRanges(t *testing.T) {
	cases := []struct {
		name   string
		window string
		today  time.Time
		want   []MonthDayRange
	}{
		{"today", BirthdayToday, day(2026, time.October, 8),
			[]MonthDayRange{{From: MonthDay{10, 8}, To: MonthDay{10, 8}}}},
		{"today on the last day of a short february also greets the 29th", BirthdayToday, day(2026, time.February, 28),
			[]MonthDayRange{{From: MonthDay{2, 28}, To: MonthDay{2, 29}}}},
		{"today on the 28th of a leap february is only the 28th", BirthdayToday, day(2028, time.February, 28),
			[]MonthDayRange{{From: MonthDay{2, 28}, To: MonthDay{2, 28}}}},
		{"this week runs sunday to saturday", BirthdayThisWeek, day(2026, time.October, 8),
			[]MonthDayRange{{From: MonthDay{10, 4}, To: MonthDay{10, 10}}}},
		{"this week across the year end splits in two ranges", BirthdayThisWeek, day(2026, time.December, 30),
			[]MonthDayRange{{From: MonthDay{12, 27}, To: MonthDay{12, 31}}, {From: MonthDay{1, 1}, To: MonthDay{1, 2}}}},
		{"this month", BirthdayThisMonth, day(2026, time.February, 10),
			[]MonthDayRange{{From: MonthDay{2, 1}, To: MonthDay{2, 31}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BirthdayRanges([]string{tc.window}, tc.today)
			if err != nil {
				t.Fatalf("BirthdayRanges() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("BirthdayRanges() = %v, want %v", got, tc.want)
			}
		})
	}
	if _, err := BirthdayRanges([]string{"tomorrow"}, day(2026, 1, 1)); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("an unknown window must refuse, got %v", err)
	}
	if _, err := BirthdayRanges([]string{BirthdayToday}, time.Time{}); !errors.Is(err, ErrBirthdayClockMissing) {
		t.Fatalf("a birthday window without the workspace day must refuse, got %v", err)
	}
}

func TestValidateForSelection_RefusesAGroupWithoutAConjunction(t *testing.T) {
	ambiguous := Filter{Groups: []Group{{Predicates: []Predicate{
		pred(FieldBlocked, OpIsTrue),
		pred(FieldOptedOut, OpIsTrue),
	}}}}
	if err := ambiguous.Validate(); err != nil {
		t.Fatalf("a list read keeps accepting the legacy shape, got %v", err)
	}
	if err := ambiguous.ValidateForSelection(); !errors.Is(err, ErrConjunctionRequired) {
		t.Fatalf("ValidateForSelection() = %v, want ErrConjunctionRequired", err)
	}

	single := Filter{Groups: []Group{{Predicates: []Predicate{pred(FieldBlocked, OpIsTrue)}}}}
	if err := single.ValidateForSelection(); !errors.Is(err, ErrConjunctionRequired) {
		t.Fatalf("even one predicate must state its conjunction, got %v", err)
	}

	unknown := Filter{Groups: []Group{{Conjunction: "xor", Predicates: []Predicate{pred(FieldBlocked, OpIsTrue)}}}}
	if err := unknown.ValidateForSelection(); !errors.Is(err, ErrConjunctionRequired) {
		t.Fatalf("an unknown conjunction must refuse, got %v", err)
	}

	stated := Filter{Groups: []Group{
		{Conjunction: And, Predicates: []Predicate{pred(FieldBlocked, OpIsTrue), pred(FieldOptedOut, OpIsTrue)}},
		{Conjunction: Or, Predicates: []Predicate{pred(FieldState, OpIn, "SP")}},
		{},
	}}
	if err := stated.ValidateForSelection(); err != nil {
		t.Fatalf("ValidateForSelection() = %v, want nil", err)
	}

	broken := Filter{Groups: []Group{{Conjunction: And, Predicates: []Predicate{pred(FieldID, OpIn, "nope")}}}}
	if err := broken.ValidateForSelection(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("selection validation still runs the predicate rules, got %v", err)
	}
}

func TestUsesBirthday(t *testing.T) {
	if (filterOf(pred(FieldBlocked, OpIsTrue))).UsesField(FieldBirthday) {
		t.Fatal("a filter without a birthday predicate does not use it")
	}
	if !(filterOf(pred(FieldBirthday, OpEquals, BirthdayToday))).UsesField(FieldBirthday) {
		t.Fatal("a birthday predicate is a use of the field")
	}
}
