package imagegen

import (
	"context"
	"strings"
)

type Model struct {
	ID   string
	Name string
}

type ModelCatalog interface {
	ImageModels(ctx context.Context) ([]Model, error)
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
