package lead

import (
	"errors"
	"strings"

	"vozko/domain/cache"
)

var errGenerationsUnavailable = errors.New("lead aggregate generations: no shared state")

type aggregateGenerations struct {
	agg *aggregateCache
}

func NewAggregateGenerations(state cache.SharedState) cache.Versions {
	return aggregateGenerations{agg: newAggregateCache(state)}
}

func (g aggregateGenerations) Version(workspaceID string) (string, error) {
	if !g.agg.enabled() {
		return "", errGenerationsUnavailable
	}
	value, err := g.agg.state.GetString(g.agg.generationKey(workspaceID))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "0", nil
	}
	return value, nil
}

func (g aggregateGenerations) Bump(workspaceID string) error {
	if !g.agg.enabled() {
		return errGenerationsUnavailable
	}
	g.agg.bump(workspaceID)
	return nil
}
