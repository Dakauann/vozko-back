package ai

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrModelWithoutTools       = errors.New("ai: the model does not confirm tool use")
	ErrModelCatalogUnavailable = errors.New("ai: the model catalog could not be read")
)

const (
	RefusalModelWithoutTools       = "model_without_tools"
	RefusalModelCatalogUnavailable = "model_catalog_unavailable"
	RefusalReasoningUnsupported    = "reasoning_level_unsupported"
)

const (
	refusalMessageWithoutTools       = "Este modelo não usa ferramentas, e este assistente precisa delas para trabalhar. Escolha no seletor um modelo com ferramentas."
	refusalMessageCatalogUnavailable = "Não foi possível conferir a lista de modelos agora. Tente de novo em instantes."
	refusalMessageReasoning          = "Este modelo não oferece o nível de raciocínio escolhido. Escolha outro nível ou outro modelo."
)

type ModelCatalog interface {
	GetModelsWithPricing(ctx context.Context) ([]ModelInfo, error)
}

type ModelRefusal struct {
	Code    string
	Message string
}

func ResolveAgentModel(ctx context.Context, catalog ModelCatalog, modelID string, level ReasoningLevel) (ModelInfo, error) {
	if catalog == nil {
		return ModelInfo{ID: modelID}, ErrModelCatalogUnavailable
	}
	models, err := catalog.GetModelsWithPricing(ctx)
	if err != nil {
		return ModelInfo{ID: modelID}, fmt.Errorf("%w: %w", ErrModelCatalogUnavailable, err)
	}
	m, ok := FindModel(models, modelID)
	if !ok || !m.Has(CapabilityTools) {
		return ModelInfo{ID: modelID}, ErrModelWithoutTools
	}
	if err := m.AcceptsReasoning(level); err != nil {
		return m, err
	}
	return m, nil
}

func RefusalOf(err error) (ModelRefusal, bool) {
	switch {
	case err == nil:
		return ModelRefusal{}, false
	case errors.Is(err, ErrModelCatalogUnavailable):
		return ModelRefusal{Code: RefusalModelCatalogUnavailable, Message: refusalMessageCatalogUnavailable}, true
	case errors.Is(err, ErrModelWithoutTools):
		return ModelRefusal{Code: RefusalModelWithoutTools, Message: refusalMessageWithoutTools}, true
	case errors.Is(err, ErrReasoningLevelUnsupported):
		return ModelRefusal{Code: RefusalReasoningUnsupported, Message: refusalMessageReasoning}, true
	}
	return ModelRefusal{}, false
}
