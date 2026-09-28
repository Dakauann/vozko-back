package unofficial_whatsapp

import (
	"context"

	"vozko/domain/shared"
)

type ListInstancesUseCase interface {
	Execute(ctx context.Context, in ListInstancesInput) (*shared.PaginatedResult[*Instance], error)
}
