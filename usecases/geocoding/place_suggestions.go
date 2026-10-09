package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

const DefaultPlaceSuggestionTTL = 12 * time.Hour

var errPlaceSuggestionsIncomplete = errors.New("place suggestions: a required dependency is missing")

type PlaceSuggestionDeps struct {
	Index georef.PlaceIndex
	Memo  cache.Memo
	Gate  cache.Gate
	TTL   time.Duration
}

type PlaceSuggestions struct {
	deps PlaceSuggestionDeps
}

type PlaceAnswer struct {
	Places        []georef.Place
	CoveredStates []string
}

func NewPlaceSuggestions(deps PlaceSuggestionDeps) (*PlaceSuggestions, error) {
	for name, absent := range map[string]bool{"index": deps.Index == nil, "memo": deps.Memo == nil, "gate": deps.Gate == nil} {
		if absent {
			return nil, fmt.Errorf("%w: %s", errPlaceSuggestionsIncomplete, name)
		}
	}
	if deps.TTL <= 0 {
		deps.TTL = DefaultPlaceSuggestionTTL
	}
	return &PlaceSuggestions{deps: deps}, nil
}

func (s *PlaceSuggestions) Suggest(ctx context.Context, kind, text, state, cityCode string) (PlaceAnswer, error) {
	q, err := georef.ParsePlaceQuery(kind, text, state, cityCode)
	if err != nil {
		return PlaceAnswer{}, err
	}
	stamps, err := s.deps.Index.Loads(ctx)
	if err != nil {
		return PlaceAnswer{}, fmt.Errorf("%w: %w", geo.ErrReferenceUnavailable, err)
	}
	loads := georef.ReferenceLoadsOf(stamps)
	answer := PlaceAnswer{CoveredStates: georef.CoveredStates(loads.Coverage)}
	if err := q.CoveredBy(loads.Coverage); err != nil {
		return answer, err
	}
	places, err := cache.Remember(ctx, s.deps.Memo, q.CacheKey(loads.Generation), s.deps.TTL, func(ctx context.Context) ([]georef.Place, error) {
		var found []georef.Place
		err := cache.Gated(ctx, s.deps.Gate, func(ctx context.Context) error {
			var err error
			found, err = s.read(ctx, q)
			return err
		})
		return found, err
	})
	if err != nil {
		return answer, err
	}
	answer.Places = places
	return answer, nil
}

func (s *PlaceSuggestions) read(ctx context.Context, q georef.PlaceQuery) ([]georef.Place, error) {
	readers := []struct {
		kind georef.PlaceKind
		read func(context.Context, georef.PlaceQuery, int) ([]georef.Place, error)
	}{
		{georef.PlaceCity, s.deps.Index.Cities},
		{georef.PlaceDistrict, s.deps.Index.Districts},
		{georef.PlaceStreet, s.deps.Index.Streets},
		{georef.PlaceCEP, s.deps.Index.CEPs},
	}
	found := []georef.Place{}
	for _, r := range readers {
		if !q.Wants(r.kind) {
			continue
		}
		places, err := r.read(ctx, q, georef.PlaceLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", geo.ErrReferenceUnavailable, r.kind, err)
		}
		found = append(found, places...)
	}
	return q.Rank(found), nil
}
