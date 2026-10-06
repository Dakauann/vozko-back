package openrouter

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/ai"
	"vozko/domain/mediagen"
)

var ErrMediaCatalogUnavailable = errors.New("openrouter: the media model catalog is unavailable")

type MediaModelCatalog struct {
	images *modelCatalogFetcher
	audio  *modelCatalogFetcher
}

var _ mediagen.ModelCatalog = (*MediaModelCatalog)(nil)

func NewMediaModelCatalog(apiKey string) *MediaModelCatalog {
	return newMediaModelCatalog(apiKey, openRouterDefaultBaseURL)
}

func newMediaModelCatalog(apiKey, baseURL string) *MediaModelCatalog {
	return &MediaModelCatalog{
		images: newCatalogFetcher(apiKey, baseURL, outputModelsQuery(imageOutputModalities)),
		audio:  newCatalogFetcher(apiKey, baseURL, outputModelsQuery(audioOutputModalities)),
	}
}

func (c *MediaModelCatalog) Models(ctx context.Context, kind mediagen.Kind) ([]mediagen.Model, error) {
	switch kind {
	case mediagen.KindImage:
		return fetched(ctx, c.images, func(ai.ModelInfo) bool { return true })
	case mediagen.KindMusic:
		return fetched(ctx, c.audio, func(m ai.ModelInfo) bool { return !m.HearsAudio })
	case mediagen.KindVoice:
		return fetched(ctx, c.audio, func(m ai.ModelInfo) bool { return m.HearsAudio })
	}
	return nil, fmt.Errorf("openrouter: no model catalog for %q", kind)
}

func fetched(ctx context.Context, fetcher *modelCatalogFetcher, keep func(ai.ModelInfo) bool) ([]mediagen.Model, error) {
	models, ok := fetcher.FetchModelsWithPricing(ctx)
	if !ok {
		return nil, ErrMediaCatalogUnavailable
	}
	out := make([]mediagen.Model, 0, len(models))
	for _, model := range models {
		if keep(model) {
			out = append(out, mediagen.Model{ID: model.ID, Name: model.Name})
		}
	}
	return out, nil
}
