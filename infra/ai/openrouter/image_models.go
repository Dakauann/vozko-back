package openrouter

import (
	"context"
	"errors"

	"vozko/domain/imagegen"
)

var ErrImageCatalogUnavailable = errors.New("openrouter: the image model catalog is unavailable")

type ImageModelCatalog struct {
	fetcher *modelCatalogFetcher
}

func NewImageModelCatalog(apiKey string) *ImageModelCatalog {
	return newImageModelCatalog(apiKey, openRouterDefaultBaseURL)
}

func newImageModelCatalog(apiKey, baseURL string) *ImageModelCatalog {
	return &ImageModelCatalog{fetcher: newCatalogFetcher(apiKey, baseURL, imageModelsQuery())}
}

func (c *ImageModelCatalog) ImageModels(ctx context.Context) ([]imagegen.Model, error) {
	models, ok := c.fetcher.FetchModelsWithPricing(ctx)
	if !ok {
		return nil, ErrImageCatalogUnavailable
	}
	out := make([]imagegen.Model, 0, len(models))
	for _, model := range models {
		out = append(out, imagegen.Model{ID: model.ID, Name: model.Name})
	}
	return out, nil
}
