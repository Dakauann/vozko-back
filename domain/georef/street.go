package georef

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"vozko/domain/address"
	"vozko/domain/shared"
)

const maxStreetNameRunes = 200

var unnamedStreets = map[string]bool{
	"sem denominacao": true,
	"sem nome":        true,
}

var lowercaseConnectors = map[string]bool{"de": true, "da": true, "do": true, "das": true, "dos": true}

var romanNumeral = regexp.MustCompile(`^M{0,3}(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)

type Street struct {
	ZipCode      string
	CityCode     string
	Name         string
	Key          string
	District     string
	DistrictKey  string
	AddressCount int64
}

type streetPlace struct {
	zip, city, key, district string
}

type streetTally struct {
	addresses int64
	names     map[string]int64
	districts map[string]int64
}

func StreetNameOf(kind, title, name string) (string, string, bool) {
	name = collapseSpaces(name)
	if name == "" || unnamedStreets[shared.FoldForMatch(name)] || utf8.RuneCountInString(name) > maxStreetNameRunes {
		return "", "", false
	}
	titled := collapseSpaces(title + " " + name)
	key := address.DistrictKey(titled)
	if key == "" {
		return "", "", false
	}
	display := PlaceDisplay(collapseSpaces(kind + " " + titled))
	if utf8.RuneCountInString(display) > maxStreetNameRunes {
		return "", "", false
	}
	return display, key, true
}

func PlaceDisplay(raw string) string {
	words := strings.Fields(raw)
	for i, word := range words {
		words[i] = displayWord(word, i == 0)
	}
	return strings.Join(words, " ")
}

func displayWord(word string, first bool) string {
	upper := strings.ToUpper(word)
	if utf8.RuneCountInString(word) >= 2 && romanNumeral.MatchString(upper) {
		return upper
	}
	lower := strings.ToLower(word)
	if !first && lowercaseConnectors[lower] {
		return lower
	}
	r, size := utf8.DecodeRuneInString(lower)
	return string(unicode.ToUpper(r)) + lower[size:]
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (b *Builder) addStreet(r Record, zip string) {
	name, key, ok := StreetNameOf(r.StreetKind, r.StreetTitle, r.StreetName)
	if !ok {
		return
	}
	district, districtKey := districtName(r.Locality)
	place := streetPlace{zip: zip, city: r.CityCode, key: key, district: districtKey}
	tally := b.streets[place]
	if tally == nil {
		tally = &streetTally{names: map[string]int64{}, districts: map[string]int64{}}
		b.streets[place] = tally
	}
	tally.addresses++
	tally.names[name]++
	if district != "" {
		tally.districts[district]++
	}
}

func (b *Builder) Streets() []Street {
	out := make([]Street, 0, len(b.streets))
	for place, tally := range b.streets {
		out = append(out, Street{
			ZipCode: place.zip, CityCode: place.city, Name: mostCommon(tally.names), Key: place.key,
			District: PlaceDisplay(mostCommon(tally.districts)), DistrictKey: place.district, AddressCount: tally.addresses,
		})
	}
	slices.SortFunc(out, func(a, b Street) int {
		return cmp.Or(cmp.Compare(a.ZipCode, b.ZipCode), cmp.Compare(a.Key, b.Key), cmp.Compare(a.DistrictKey, b.DistrictKey))
	})
	return out
}
