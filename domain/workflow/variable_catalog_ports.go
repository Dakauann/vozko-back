package workflow

import (
	"context"
	"errors"
)

var ErrVariablesUnavailable = errors.New("workflow: the variables available at this node could not be read")

type SecretNameReader interface {
	SecretNames(ctx context.Context, workspaceID string) ([]string, error)
}
