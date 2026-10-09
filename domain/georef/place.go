package georef

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/address"
	"vozko/domain/cep"
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

type PlaceKind string

const (
	PlaceCity     PlaceKind = "city"
	PlaceDistrict PlaceKind = "district"
	PlaceStreet   PlaceKind = "street"
	PlaceCEP      PlaceKind = "cep"
)

const (
	PlaceLimit         = 10
	PlaceMinRunes      = 2
	PlaceMaxRunes      = 60
	cepPrefixMinDigits = 5
	cepDigits          = 8
	placeCachePrefix   = "georef:places"
	mixedPlaceKinds    = "all"
	generationLength   = 16
)

var (
	ErrPlaceQueryInvalid = errors.New("georef: a place search takes 2 to 60 characters of a known kind, or 5 to 8 digits of a CEP")
	ErrPlaceScopeInvalid = errors.New("georef: a place search scope is a known state and a valid city code of that state; bairros and streets need the city")
)

var placeKindOrder = []PlaceKind{PlaceCity, PlaceDistrict, PlaceStreet, PlaceCEP}

var namedPlaceKinds = []PlaceKind{PlaceCity, PlaceDistrict, PlaceStreet}

var streetKindWords = map[string]bool{
	"rua": true, "r": true, "avenida": true, "av": true, "ave": true, "travessa": true, "tv": true, "trav": true,
	"estrada": true, "estr": true, "est": true, "rodovia": true, "rod": true, "alameda": true, "al": true,
	"praca": true, "pc": true, "pca": true, "largo": true, "lg": true, "ladeira": true, "ld": true, "beco": true,
	"viela": true, "acesso": true, "caminho": true, "quadra": true, "qd": true, "passagem": true, "servidao": true,
}

var cepSeparators = strings.NewReplacer("-", "", ".", "", " ", "")

func (k PlaceKind) Known() bool { return slices.Contains(placeKindOrder, k) }

type PlaceQuery struct {
	Kinds     []PlaceKind
	Mixed     bool
	Key       string
	StreetKey string
	ZipPrefix string
	State     string
	CityCode  string
}

func ParsePlaceQuery(kind, text, state, cityCode string) (PlaceQuery, error) {
	text = strings.TrimSpace(text)
	if runes := utf8.RuneCountInString(text); runes < PlaceMinRunes || runes > PlaceMaxRunes {
		return PlaceQuery{}, ErrPlaceQueryInvalid
	}
	q, err := placeScope(state, cityCode)
	if err != nil {
		return PlaceQuery{}, err
	}
	wanted := PlaceKind(strings.ToLower(strings.TrimSpace(kind)))
	digits, numeric := cepPrefix(text)
	switch {
	case wanted == "" && numeric && len(digits) >= cepPrefixMinDigits:
		return q.forCEP(digits)
	case wanted == "":
		q.Mixed = true
		return q.named(namedPlaceKinds, text)
	case wanted == PlaceCEP:
		if !numeric {
			return PlaceQuery{}, ErrPlaceQueryInvalid
		}
		return q.forCEP(digits)
	case wanted == PlaceCity:
		return q.named([]PlaceKind{PlaceCity}, text)
	case wanted == PlaceDistrict, wanted == PlaceStreet:
		if q.CityCode == "" {
			return PlaceQuery{}, ErrPlaceScopeInvalid
		}
		return q.named([]PlaceKind{wanted}, text)
	}
	return PlaceQuery{}, ErrPlaceQueryInvalid
}

func placeScope(state, cityCode string) (PlaceQuery, error) {
	var q PlaceQuery
	if raw := strings.TrimSpace(state); raw != "" {
		code, ok := address.StateCode(raw)
		if !ok {
			return PlaceQuery{}, ErrPlaceScopeInvalid
		}
		q.State = code
	}
	code := strings.TrimSpace(cityCode)
	if code == "" {
		return q, nil
	}
	cityState, ok := address.StateOfCityCode(code)
	if !ok || (q.State != "" && q.State != cityState) {
		return PlaceQuery{}, ErrPlaceScopeInvalid
	}
	q.CityCode, q.State = code, cityState
	return q, nil
}

func cepPrefix(text string) (string, bool) {
	digits := cepSeparators.Replace(text)
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return digits, digits != ""
}

func (q PlaceQuery) forCEP(digits string) (PlaceQuery, error) {
	if len(digits) < cepPrefixMinDigits || len(digits) > cepDigits {
		return PlaceQuery{}, ErrPlaceQueryInvalid
	}
	q.Kinds, q.ZipPrefix = []PlaceKind{PlaceCEP}, digits
	return q, nil
}

func (q PlaceQuery) named(kinds []PlaceKind, text string) (PlaceQuery, error) {
	key := address.DistrictKey(text)
	if key == "" {
		return PlaceQuery{}, ErrPlaceQueryInvalid
	}
	q.Kinds, q.Key, q.StreetKey = kinds, key, streetKeyOf(key)
	return q, nil
}

func streetKeyOf(key string) string {
	first, rest, found := strings.Cut(key, " ")
	if found && streetKindWords[first] && strings.TrimSpace(rest) != "" {
		return strings.TrimSpace(rest)
	}
	return key
}

func (q PlaceQuery) Wants(kind PlaceKind) bool { return slices.Contains(q.Kinds, kind) }

func (q PlaceQuery) kindLabel() string {
	if q.Mixed {
		return mixedPlaceKinds
	}
	labels := make([]string, len(q.Kinds))
	for i, k := range q.Kinds {
		labels[i] = string(k)
	}
	return strings.Join(labels, ",")
}

func (q PlaceQuery) CacheKey(generation string) string {
	prefix := q.Key
	if q.ZipPrefix != "" {
		prefix = q.ZipPrefix
	}
	return strings.Join([]string{placeCachePrefix, generation, q.kindLabel(), q.State, q.CityCode, prefix}, ":")
}

func (q PlaceQuery) CoveredBy(coverage geo.Coverage) error {
	if len(CoveredStates(coverage)) == 0 {
		return fmt.Errorf("%w: no state is loaded", geo.ErrReferenceNotLoaded)
	}
	if q.State != "" && !coverage.States[q.State] {
		return fmt.Errorf("%w: %s", geo.ErrReferenceNotLoaded, q.State)
	}
	return nil
}

func CoveredStates(coverage geo.Coverage) []string {
	states := make([]string, 0, len(coverage.States))
	for state, loaded := range coverage.States {
		if loaded {
			states = append(states, state)
		}
	}
	slices.Sort(states)
	return states
}

type Place struct {
	Kind         PlaceKind
	Key          string
	Name         string
	ZipCode      string
	Street       string
	District     string
	City         string
	CityCode     string
	State        string
	Point        geo.Point
	Precision    geo.Precision
	Bounds       *geo.BBox
	AddressCount int64
	ZipCount     int64
}

func BestPoint(fixes ...geo.Fix) (geo.Point, geo.Precision, bool) {
	best, ok := geo.Choose(nil, fixes)
	if !ok {
		return geo.Point{}, "", false
	}
	return best.Point, best.Precision, true
}

func (p Place) LabelText() string {
	var parts []string
	switch p.Kind {
	case PlaceCEP:
		parts = []string{cep.Format(p.ZipCode), p.Street, p.District, p.City, p.State}
	case PlaceStreet:
		parts = []string{cmp.Or(p.Street, p.Name), p.District, p.City, p.State}
	case PlaceDistrict:
		parts = []string{cmp.Or(p.District, p.Name), p.City, p.State}
	default:
		parts = []string{cmp.Or(p.City, p.Name), p.State}
	}
	kept := parts[:0]
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ", ")
}

func (q PlaceQuery) exact(p Place) bool {
	switch p.Kind {
	case PlaceStreet:
		return p.Key == q.StreetKey
	case PlaceCEP:
		return p.ZipCode == q.ZipPrefix
	}
	return p.Key == q.Key
}

func (q PlaceQuery) Rank(places []Place) []Place {
	ranked := slices.Clone(places)
	slices.SortStableFunc(ranked, func(a, b Place) int {
		ea, eb := q.exact(a), q.exact(b)
		if ea != eb {
			if ea {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.AddressCount, a.AddressCount), cmp.Compare(a.LabelText(), b.LabelText()))
	})
	ranked = ranked[:min(len(ranked), PlaceLimit)]
	slices.SortStableFunc(ranked, func(a, b Place) int {
		return cmp.Compare(slices.Index(placeKindOrder, a.Kind), slices.Index(placeKindOrder, b.Kind))
	})
	return ranked
}

type LoadStamp struct {
	State   string
	BuiltAt time.Time
}

type ReferenceLoads struct {
	Coverage   geo.Coverage
	Generation string
}

func ReferenceLoadsOf(stamps []LoadStamp) ReferenceLoads {
	sorted := slices.Clone(stamps)
	slices.SortFunc(sorted, func(a, b LoadStamp) int { return cmp.Compare(a.State, b.State) })
	coverage := geo.Coverage{States: make(map[string]bool, len(sorted))}
	var b strings.Builder
	for _, s := range sorted {
		coverage.States[s.State] = true
		b.WriteString(s.State + "@" + strconv.FormatInt(s.BuiltAt.UnixNano(), 10) + ";")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return ReferenceLoads{Coverage: coverage, Generation: hex.EncodeToString(sum[:])[:generationLength]}
}

type CEPStreetRow struct {
	Street       string
	District     string
	CityCode     string
	City         string
	State        string
	AddressCount int64
}

func CEPInfoFromStreets(zip string, rows []CEPStreetRow) (cep.CEPInfo, bool) {
	if len(rows) == 0 {
		return cep.CEPInfo{}, false
	}
	byCity := map[string]int64{}
	for _, r := range rows {
		byCity[r.CityCode] += r.AddressCount
	}
	cityCode := mostCommon(byCity)
	info := cep.CEPInfo{Cep: zip, IBGE: cityCode}
	streets, districts := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		if r.CityCode != cityCode {
			continue
		}
		info.Localidade, info.Uf = r.City, r.State
		streets[r.Street] = true
		if r.District != "" {
			districts[r.District] = true
		}
	}
	if len(byCity) == 1 {
		info.Logradouro = onlyKey(streets)
		info.Bairro = onlyKey(districts)
	}
	return info, info.Localidade != "" && info.Uf != ""
}

func onlyKey(set map[string]bool) string {
	if len(set) != 1 {
		return ""
	}
	for key := range set {
		return key
	}
	return ""
}

func (p Place) CityKey() string {
	return address.CityKey(p.City, p.State)
}

func (p Place) DistrictPair() string {
	city, district := p.CityKey(), address.DistrictKey(p.District)
	if city == "" || district == "" {
		return ""
	}
	return crmfilter.DistrictPair(city, district)
}
