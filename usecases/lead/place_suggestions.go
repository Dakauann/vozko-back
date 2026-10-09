package lead_usecase

import (
	"context"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

func (s *Sections) PlaceSuggestions(ctx context.Context, a Actor, f crmfilter.Filter, prefix string) (*lead.PlacesSection, error) {
	folded, err := lead.ParsePlacePrefix(prefix)
	if err != nil {
		return nil, err
	}
	q, key, err := s.query(ctx, a, lead.SectionPlaces, f)
	if err != nil {
		return nil, err
	}
	q.PlacePrefix = folded
	key.Parts = append(key.Parts, "place:"+folded)
	return memoSection(ctx, s.deps.Caching, sectionCachePrefix, a.WorkspaceID, key, func(ctx context.Context) (*lead.PlacesSection, error) {
		return s.deps.Reader.ReadPlaces(ctx, q)
	})
}
