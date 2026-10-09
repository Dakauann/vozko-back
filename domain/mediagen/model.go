package mediagen

import (
	"context"
	"strings"
)

type Model struct {
	ID   string
	Name string
}

type ModelCatalog interface {
	Models(ctx context.Context, kind Kind) ([]Model, error)
}

func DefaultModel(models []Model) (Model, bool) {
	if len(models) == 0 {
		return Model{}, false
	}
	return models[0], true
}

func Supported(models []Model, id string) error {
	wanted := strings.TrimSpace(id)
	for _, model := range models {
		if model.ID == wanted {
			return nil
		}
	}
	return &ValidationError{Issues: []FieldIssue{{Field: FieldModel, Code: CodeUnknown}}}
}
