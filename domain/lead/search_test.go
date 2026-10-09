package lead

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func termTexts(s Search) []string {
	out := make([]string, 0, len(s.Terms))
	for _, term := range s.Terms {
		if term.IsPhone() {
			out = append(out, "#"+term.Digits)
			continue
		}
		out = append(out, term.Text)
	}
	return out
}

func TestParseSearchSplitsFoldsAndKeepsEveryWord(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"Santo Antônio", []string{"santo", "antonio"}},
		{"  JOÃO   da  Silva ", []string{"joao", "da", "silva"}},
		{"maria, natal.", []string{"maria", "natal"}},
		{"Maria maria", []string{"maria"}},
		{"a maria", []string{"maria"}},
		{"50% d_1", []string{"50%", "d_1"}},
	}
	for _, tc := range cases {
		got, err := ParseSearch(tc.raw)
		if err != nil {
			t.Fatalf("ParseSearch(%q) error = %v", tc.raw, err)
		}
		if !reflect.DeepEqual(termTexts(got), tc.want) {
			t.Errorf("ParseSearch(%q) = %v, want %v", tc.raw, termTexts(got), tc.want)
		}
	}
}

func TestParseSearchKeepsAtMostEightWords(t *testing.T) {
	got, err := ParseSearch("um dois tres quatro cinco seis sete oito nove dez")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"um", "dois", "tres", "quatro", "cinco", "seis", "sete", "oito"}
	if !reflect.DeepEqual(termTexts(got), want) {
		t.Fatalf("terms = %v, want %v", termTexts(got), want)
	}
}

func TestParseSearchRefusesAQueryWithoutAUsableWord(t *testing.T) {
	for _, raw := range []string{"", "   ", "a", "a b c", "é", "- , ."} {
		_, err := ParseSearch(raw)
		if !errors.Is(err, ErrLeadSearchTooShort) || !errors.Is(err, ErrLeadFilterInvalid) {
			t.Errorf("ParseSearch(%q) = %v, want ErrLeadSearchTooShort under ErrLeadFilterInvalid", raw, err)
		}
		if code := ErrorCode(err); code != "lead_search_too_short" {
			t.Errorf("ParseSearch(%q) code = %q", raw, code)
		}
	}
}

func TestParseSearchReadsAWholePhoneAsOneNumber(t *testing.T) {
	got, err := ParseSearch("(84) 99999-1234")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Terms) != 1 || got.Terms[0].Digits != "84999991234" {
		t.Fatalf("terms = %+v, want one phone 84999991234", got.Terms)
	}
	want := []string{"5584999991234", "558499991234"}
	if !reflect.DeepEqual(got.Terms[0].Numbers, want) {
		t.Fatalf("numbers = %v, want both ninth digit forms %v", got.Terms[0].Numbers, want)
	}
}

func TestParseSearchMatchesAPartialNumberByDigits(t *testing.T) {
	got, err := ParseSearch("maria 9999-1234")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(termTexts(got), []string{"maria", "#99991234"}) {
		t.Fatalf("terms = %v", termTexts(got))
	}
	if got.Terms[1].Numbers != nil {
		t.Fatalf("a partial number has no exact forms, got %v", got.Terms[1].Numbers)
	}
}

func TestParseSearchTreatsShortNumbersAsWords(t *testing.T) {
	got, err := ParseSearch("rua 12")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(termTexts(got), []string{"rua", "12"}) {
		t.Fatalf("terms = %v", termTexts(got))
	}
}

func TestSearchTermsMatchWordStartsBelowThreeCharacters(t *testing.T) {
	got, err := ParseSearch("jd maria da")
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]SearchTerm{}
	for _, term := range got.Terms {
		byText[term.Text] = term
	}
	if !byText["da"].WordStartOnly() || byText["maria"].WordStartOnly() {
		t.Fatalf("only words under three characters match word starts: %+v", got.Terms)
	}
	jd := byText["jd"]
	if jd.Place != "jardim" || jd.PlaceWordStartOnly() || !jd.WordStartOnly() {
		t.Fatalf("a bairro abbreviation expands for places only: %+v", jd)
	}
}

func TestSearchRankTextJoinsTheWords(t *testing.T) {
	got, err := ParseSearch("Maria  9999-1234 Natal")
	if err != nil {
		t.Fatal(err)
	}
	if got.RankText() != "maria natal" {
		t.Fatalf("rank text = %q", got.RankText())
	}
	phone, _ := ParseSearch("84 99999 1234")
	if phone.RankText() != "" {
		t.Fatalf("a phone search has no rank text, got %q", phone.RankText())
	}
}

func TestParsePlacePrefixFoldsLikeTheBairroKey(t *testing.T) {
	got, err := ParsePlacePrefix("  Sto Antô ")
	if err != nil || got != "santo anto" {
		t.Fatalf("ParsePlacePrefix = %q, %v", got, err)
	}
	if _, err := ParsePlacePrefix("a"); !errors.Is(err, ErrLeadSearchTooShort) {
		t.Fatalf("a one letter prefix = %v", err)
	}
	long, err := ParsePlacePrefix(strings.Repeat("b", 200))
	if err != nil || len(long) != MaxPlacePrefixLength {
		t.Fatalf("a long prefix is cut to %d, got %d %v", MaxPlacePrefixLength, len(long), err)
	}
}

func TestValidateFilterRefusesAnUnusableSearch(t *testing.T) {
	err := ValidateFilter(leadFilter(crmfilter.FieldQuery, crmfilter.OpContains, "a"))
	if !errors.Is(err, ErrLeadSearchTooShort) {
		t.Fatalf("ValidateFilter = %v, want ErrLeadSearchTooShort", err)
	}
	if err := ValidateFilter(leadFilter(crmfilter.FieldQuery, crmfilter.OpContains, "santo antonio")); err != nil {
		t.Fatalf("a usable search = %v", err)
	}
}

func TestPlaceSuggestionsAreCappedAtEightEach(t *testing.T) {
	cities, districts := SectionQuery{PlacePrefix: "santo"}.PlaceLimits()
	if cities != MaxPlaceSuggestions || districts != MaxPlaceSuggestions || MaxPlaceSuggestions != 8 {
		t.Fatalf("suggestion limits = %d, %d", cities, districts)
	}
	cities, districts = SectionQuery{}.PlaceLimits()
	if cities != MaxPlaceCities || districts != MaxPlaceDistricts {
		t.Fatalf("section limits = %d, %d", cities, districts)
	}
}
