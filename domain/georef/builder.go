package georef

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"vozko/domain/address"
	"vozko/domain/cep"
	"vozko/domain/geo"
)

const (
	SourceCNEFE      = "cnefe"
	SourceLeads      = "leads"
	Attribution      = "IBGE, CNEFE 2022"
	spreadRank       = 0.9
	boundsTrim       = 0.02
	minBoundsSpanDeg = 0.004
	defaultCEP       = 2048
	defaultPlace     = 4096
	defaultCity      = 8192
)

type Level int

func (l Level) Known() bool { return l >= 1 && l <= 6 }

func (l Level) FeedsPoints() bool { return l >= 1 && l <= 4 }

type Record struct {
	CityCode    string
	ZipCode     string
	Locality    string
	StreetKind  string
	StreetTitle string
	StreetName  string
	Point       geo.Point
	Level       Level
}

type Limits struct {
	CEPSample      int
	DistrictSample int
	CitySample     int
}

func DefaultLimits() Limits {
	return Limits{CEPSample: defaultCEP, DistrictSample: defaultPlace, CitySample: defaultCity}
}

type Stats struct {
	Rows      int64
	Pinned    int64
	CountOnly int64
	Skipped   int
}

type CEPPoint struct {
	ZipCode      string
	Point        geo.Point
	SpreadM      float64
	AddressCount int64
	SampleCount  int64
	CityCode     string
}

type DistrictPoint struct {
	CityCode    string
	DistrictKey string
	Name        string
	Point       geo.Point
	SpreadM     float64
	SampleCount int64
	Bounds      geo.BBox
}

type CityPoint struct {
	CityCode    string
	Point       geo.Point
	SpreadM     float64
	SampleCount int64
	Bounds      geo.BBox
}

type sample struct {
	lat, lng []float32
	seen     int64
}

func (s *sample) keep(p geo.Point) {
	s.seen++
	s.lat = append(s.lat, float32(p.Lat))
	s.lng = append(s.lng, float32(p.Lng))
}

func (s *sample) add(p geo.Point, limit int, rng *rand.Rand) {
	if len(s.lat) < limit {
		s.keep(p)
		return
	}
	s.seen++
	if j := rng.Int64N(s.seen); j < int64(limit) {
		s.lat[j], s.lng[j] = float32(p.Lat), float32(p.Lng)
	}
}

func (s *sample) summary() (geo.Point, float64) {
	if len(s.lat) == 0 {
		return geo.Point{}, 0
	}
	center := geo.Point{Lat: median(s.lat), Lng: median(s.lng)}
	distances := make([]float64, len(s.lat))
	for i := range s.lat {
		distances[i] = geo.DistanceMeters(center, geo.Point{Lat: float64(s.lat[i]), Lng: float64(s.lng[i])})
	}
	slices.Sort(distances)
	rank := int(math.Ceil(spreadRank*float64(len(distances)))) - 1
	return center, distances[max(rank, 0)]
}

func (s *sample) bounds() geo.BBox {
	if len(s.lat) == 0 {
		return geo.BBox{}
	}
	south, north := percentileRange(s.lat)
	west, east := percentileRange(s.lng)
	south, north = widen(south, north)
	west, east = widen(west, east)
	return geo.BBox{South: south, West: west, North: north, East: east}
}

func percentileRange(values []float32) (float64, float64) {
	sorted := make([]float64, len(values))
	for i, v := range values {
		sorted[i] = float64(v)
	}
	slices.Sort(sorted)
	trim := int(math.Floor(boundsTrim * float64(len(sorted))))
	return sorted[trim], sorted[len(sorted)-1-trim]
}

func widen(low, high float64) (float64, float64) {
	if missing := minBoundsSpanDeg - (high - low); missing > 0 {
		return low - missing/2, high + missing/2
	}
	return low, high
}

func median(values []float32) float64 {
	sorted := make([]float64, len(values))
	for i, v := range values {
		sorted[i] = float64(v)
	}
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

type cepTally struct {
	sample
	addresses int64
	cities    map[string]int64
}

type districtTally struct {
	sample
	spellings map[string]int64
}

type Builder struct {
	limits    Limits
	rng       *rand.Rand
	ceps      map[string]*cepTally
	districts map[address.Place]*districtTally
	cities    map[string]*sample
	streets   map[streetPlace]*streetTally
	stats     Stats
}

func NewBuilder(limits Limits, seed uint64) *Builder {
	defaults := DefaultLimits()
	if limits.CEPSample <= 0 {
		limits.CEPSample = defaults.CEPSample
	}
	if limits.DistrictSample <= 0 {
		limits.DistrictSample = defaults.DistrictSample
	}
	if limits.CitySample <= 0 {
		limits.CitySample = defaults.CitySample
	}
	return &Builder{
		limits:    limits,
		rng:       rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		ceps:      map[string]*cepTally{},
		districts: map[address.Place]*districtTally{},
		cities:    map[string]*sample{},
		streets:   map[streetPlace]*streetTally{},
	}
}

func (b *Builder) Add(r Record) bool {
	zip, err := cep.Parse(r.ZipCode)
	if err != nil || !address.ValidCityCode(r.CityCode) || !r.Level.Known() {
		b.stats.Skipped++
		return false
	}
	if r.Level.FeedsPoints() && !r.Point.InBrazil() {
		b.stats.Skipped++
		return false
	}
	b.stats.Rows++
	tally := b.ceps[zip]
	if tally == nil {
		tally = &cepTally{cities: map[string]int64{}}
		b.ceps[zip] = tally
	}
	tally.addresses++
	tally.cities[r.CityCode]++
	b.addStreet(r, zip)
	if !r.Level.FeedsPoints() {
		b.stats.CountOnly++
		return true
	}
	b.stats.Pinned++
	tally.add(r.Point, b.limits.CEPSample, b.rng)
	b.addDistrict(r)
	city := b.cities[r.CityCode]
	if city == nil {
		city = &sample{}
		b.cities[r.CityCode] = city
	}
	city.add(r.Point, b.limits.CitySample, b.rng)
	return true
}

func districtName(raw string) (string, string) {
	name := strings.Join(strings.Fields(raw), " ")
	return name, address.DistrictKey(name)
}

func DistrictSourcesReplacedBy(source string) []string {
	switch source {
	case SourceCNEFE:
		return []string{SourceCNEFE, SourceLeads}
	case SourceLeads:
		return []string{SourceLeads}
	}
	return nil
}

func (b *Builder) addDistrict(r Record) {
	name, key := districtName(r.Locality)
	if key == "" {
		return
	}
	place := address.Place{CityCode: r.CityCode, DistrictKey: key}
	tally := b.districts[place]
	if tally == nil {
		tally = &districtTally{spellings: map[string]int64{}}
		b.districts[place] = tally
	}
	tally.spellings[name]++
	tally.add(r.Point, b.limits.DistrictSample, b.rng)
}

func (b *Builder) Stats() Stats { return b.stats }

func (b *Builder) CEPPoints() []CEPPoint {
	out := make([]CEPPoint, 0, len(b.ceps))
	for zip, tally := range b.ceps {
		if tally.seen == 0 {
			continue
		}
		center, spread := tally.summary()
		out = append(out, CEPPoint{
			ZipCode: zip, Point: center, SpreadM: spread,
			AddressCount: tally.addresses, SampleCount: tally.seen, CityCode: mostCommon(tally.cities),
		})
	}
	slices.SortFunc(out, func(a, b CEPPoint) int { return cmp.Compare(a.ZipCode, b.ZipCode) })
	return out
}

func (b *Builder) DistrictPoints() []DistrictPoint {
	out := make([]DistrictPoint, 0, len(b.districts))
	for place, tally := range b.districts {
		center, spread := tally.summary()
		out = append(out, DistrictPoint{
			CityCode: place.CityCode, DistrictKey: place.DistrictKey, Name: mostCommon(tally.spellings),
			Point: center, SpreadM: spread, SampleCount: tally.seen, Bounds: tally.bounds(),
		})
	}
	slices.SortFunc(out, func(a, b DistrictPoint) int {
		return cmp.Or(cmp.Compare(a.CityCode, b.CityCode), cmp.Compare(a.DistrictKey, b.DistrictKey))
	})
	return out
}

func (b *Builder) CityPoints() []CityPoint {
	out := make([]CityPoint, 0, len(b.cities))
	for code, s := range b.cities {
		center, spread := s.summary()
		out = append(out, CityPoint{CityCode: code, Point: center, SpreadM: spread, SampleCount: s.seen, Bounds: s.bounds()})
	}
	slices.SortFunc(out, func(a, b CityPoint) int { return cmp.Compare(a.CityCode, b.CityCode) })
	return out
}

func mostCommon(counts map[string]int64) string {
	best, bestCount := "", int64(-1)
	for value, count := range counts {
		if count > bestCount || (count == bestCount && value < best) {
			best, bestCount = value, count
		}
	}
	return best
}
