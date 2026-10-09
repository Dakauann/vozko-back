package georef

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"vozko/domain/address"
	"vozko/domain/billing"
	"vozko/domain/geo"
)

const (
	MinLeadPositions  = 5
	MinLeadWorkspaces = 2
	RefineBatch       = 2000
	RefineBudget      = 30 * time.Minute
	refineNightStart  = 3
	refineNightHours  = 3
)

type LocatedAddress struct {
	ID          string
	WorkspaceID string
	Postal      address.Postal
	Point       geo.Point
	Precision   geo.Precision
	Source      geo.FixSource
}

func RefiningSources() []geo.FixSource {
	return []geo.FixSource{geo.SourceManual, geo.SourceLeadPin, geo.SourceImport, geo.SourceProvider}
}

func RefiningPrecisions() []geo.Precision {
	return geo.PinningPrecisions()
}

func (a LocatedAddress) Refines() bool {
	return strings.TrimSpace(a.WorkspaceID) != "" && a.Precision.PinsAHouse() && slices.Contains(RefiningSources(), a.Source) && a.Point.InBrazil()
}

type RefineStats struct {
	Addresses int64
	Used      int64
	Skipped   int64
}

type workspacePositions struct {
	seen  map[geo.Point]struct{}
	order []geo.Point
}

type leadDistrictTally struct {
	workspaces map[string]*workspacePositions
	spellings  map[string]int64
}

func (t *leadDistrictTally) balanced() (sample, bool) {
	if len(t.workspaces) < MinLeadWorkspaces {
		return sample{}, false
	}
	sizes := make([]int, 0, len(t.workspaces))
	for _, w := range t.workspaces {
		sizes = append(sizes, len(w.order))
	}
	slices.SortFunc(sizes, func(a, b int) int { return cmp.Compare(b, a) })
	share := sizes[1]
	var s sample
	for _, w := range t.workspaces {
		for _, p := range w.order[:min(share, len(w.order))] {
			s.keep(p)
		}
	}
	return s, s.seen >= MinLeadPositions
}

type DistrictRefiner struct {
	limit   int
	tallies map[address.Place]*leadDistrictTally
	stats   RefineStats
}

func NewDistrictRefiner(limits Limits) *DistrictRefiner {
	limit := limits.DistrictSample
	if limit <= 0 {
		limit = DefaultLimits().DistrictSample
	}
	return &DistrictRefiner{limit: limit, tallies: map[address.Place]*leadDistrictTally{}}
}

func (r *DistrictRefiner) Add(cityCode string, a LocatedAddress) bool {
	r.stats.Addresses++
	name, key := districtName(a.Postal.District)
	if !a.Refines() || !address.ValidCityCode(cityCode) || key == "" {
		r.stats.Skipped++
		return false
	}
	r.stats.Used++
	place := address.Place{CityCode: cityCode, DistrictKey: key}
	tally := r.tallies[place]
	if tally == nil {
		tally = &leadDistrictTally{workspaces: map[string]*workspacePositions{}, spellings: map[string]int64{}}
		r.tallies[place] = tally
	}
	tally.spellings[name]++
	positions := tally.workspaces[a.WorkspaceID]
	if positions == nil {
		positions = &workspacePositions{seen: map[geo.Point]struct{}{}}
		tally.workspaces[a.WorkspaceID] = positions
	}
	if _, seen := positions.seen[a.Point]; seen || len(positions.order) >= r.limit {
		return true
	}
	positions.seen[a.Point] = struct{}{}
	positions.order = append(positions.order, a.Point)
	return true
}

func (r *DistrictRefiner) Stats() RefineStats { return r.stats }

func (r *DistrictRefiner) Points() []DistrictPoint {
	var out []DistrictPoint
	for place, tally := range r.tallies {
		s, ok := tally.balanced()
		if !ok {
			continue
		}
		center, spread := s.summary()
		out = append(out, DistrictPoint{
			CityCode: place.CityCode, DistrictKey: place.DistrictKey, Name: mostCommon(tally.spellings),
			Point: center, SpreadM: spread, SampleCount: s.seen, Bounds: s.bounds(),
		})
	}
	slices.SortFunc(out, func(a, b DistrictPoint) int {
		return cmp.Or(cmp.Compare(a.CityCode, b.CityCode), cmp.Compare(a.DistrictKey, b.DistrictKey))
	})
	return out
}

func RefineDueAt(now, last time.Time) bool {
	local := now.In(billing.LocationBRT())
	if local.Hour() < refineNightStart || local.Hour() >= refineNightStart+refineNightHours {
		return false
	}
	if last.IsZero() {
		return true
	}
	previous := last.In(billing.LocationBRT())
	return previous.Year() != local.Year() || previous.YearDay() != local.YearDay()
}
