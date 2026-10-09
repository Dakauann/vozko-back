package lead

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"vozko/domain/address"
	"vozko/domain/shared"
)

const (
	MaxSearchTerms       = 8
	MinSearchTermLength  = 2
	MinSearchDigits      = 4
	MaxPlacePrefixLength = 60
	minSubstringLength   = 3
)

var ErrLeadSearchTooShort = fmt.Errorf("%w: the search needs a word of at least %d characters", ErrLeadFilterInvalid, MinSearchTermLength)

const (
	wordEdgePunctuation = ",.;:!?\"'()[]{}"
	phoneQueryRunes     = "0123456789 +()-./"
)

type SearchTerm struct {
	Text    string
	Place   string
	Digits  string
	Numbers []string
}

func (t SearchTerm) IsPhone() bool { return t.Digits != "" }

func (t SearchTerm) WordStartOnly() bool { return utf8.RuneCountInString(t.Text) < minSubstringLength }

func (t SearchTerm) PlaceWordStartOnly() bool {
	return utf8.RuneCountInString(t.Place) < minSubstringLength
}

type Search struct {
	Terms []SearchTerm
}

func (s Search) RankText() string {
	words := make([]string, 0, len(s.Terms))
	for _, term := range s.Terms {
		if !term.IsPhone() {
			words = append(words, term.Text)
		}
	}
	return strings.Join(words, " ")
}

func ParseSearch(raw string) (Search, error) {
	trimmed := strings.TrimSpace(raw)
	if isPhoneQuery(trimmed) {
		return Search{Terms: []SearchTerm{phoneTerm(shared.DigitsOf(trimmed))}}, nil
	}
	var terms []SearchTerm
	seen := map[string]bool{}
	for _, word := range strings.Fields(trimmed) {
		term, ok := searchTerm(word)
		key := term.Text + "#" + term.Digits
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		terms = append(terms, term)
		if len(terms) == MaxSearchTerms {
			break
		}
	}
	if len(terms) == 0 {
		return Search{}, fmt.Errorf("%w: %q", ErrLeadSearchTooShort, raw)
	}
	return Search{Terms: terms}, nil
}

func ParsePlacePrefix(raw string) (string, error) {
	prefix := address.DistrictKey(raw)
	if utf8.RuneCountInString(prefix) < MinSearchTermLength {
		return "", fmt.Errorf("%w: place %q", ErrLeadSearchTooShort, raw)
	}
	if runes := []rune(prefix); len(runes) > MaxPlacePrefixLength {
		prefix = strings.TrimSpace(string(runes[:MaxPlacePrefixLength]))
	}
	return prefix, nil
}

func searchTerm(word string) (SearchTerm, bool) {
	if digits := strings.TrimPrefix(shared.CompactPhone(word), "+"); len(digits) >= MinSearchDigits && shared.AllDigits(digits) {
		return phoneTerm(digits), true
	}
	text := strings.Trim(shared.FoldForMatch(word), wordEdgePunctuation)
	if utf8.RuneCountInString(text) < MinSearchTermLength {
		return SearchTerm{}, false
	}
	return SearchTerm{Text: text, Place: address.DistrictKey(text)}, true
}

func phoneTerm(digits string) SearchTerm {
	term := SearchTerm{Digits: digits}
	if number, err := shared.ParsePhone(digits); err == nil {
		term.Numbers = shared.NinthDigitVariants(number)
	}
	return term
}

func isPhoneQuery(query string) bool {
	if query == "" || strings.Trim(query, phoneQueryRunes) != "" {
		return false
	}
	return len(shared.DigitsOf(query)) >= MinSearchDigits
}

const MaxPlaceSuggestions = 8

func (q SectionQuery) PlaceLimits() (cities, districts int) {
	if q.PlacePrefix != "" {
		return MaxPlaceSuggestions, MaxPlaceSuggestions
	}
	return MaxPlaceCities, MaxPlaceDistricts
}
