package campaignaudience_usecase

import (
	"context"
	"fmt"

	"vozko/domain/campaignaudience"
	"vozko/domain/leadmap"
	"vozko/domain/selection"
)

const mapPrefix = "campaign-audience:map:"

type Places interface {
	Sample(ctx context.Context, scope selection.Scope, s selection.Selection, limit int) (leadmap.Sample, error)
}

func (s *Service) Map(ctx context.Context, a Actor, ch campaignaudience.Channel, spec campaignaudience.Spec) (leadmap.Sample, error) {
	if _, err := s.channel(ch); err != nil {
		return leadmap.Sample{}, err
	}
	spec = spec.Normalized()
	if err := spec.Validate(); err != nil {
		return leadmap.Sample{}, err
	}
	if spec.Source != campaignaudience.SourceLeads {
		return leadmap.Sample{}, campaignaudience.ErrSourceUnsupported
	}
	if s.deps.Places == nil {
		return leadmap.Sample{}, fmt.Errorf("%w: lead positions", campaignaudience.ErrUnavailable)
	}
	sel := *spec.Selection
	key, err := sharedKey(mapPrefix, struct {
		Actor     Actor
		Channel   campaignaudience.Channel
		Selection selection.Selection
	}{a, ch, sel})
	if err != nil {
		return leadmap.Sample{}, err
	}
	return sharedRun(ctx, s, key, func(ctx context.Context) (leadmap.Sample, error) {
		return s.deps.Places.Sample(ctx, scopeOf(a), sel, leadmap.MaxSamplePoints)
	})
}
