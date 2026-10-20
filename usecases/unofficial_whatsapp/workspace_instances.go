package unofficial_whatsapp

import (
	"context"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type WorkspaceInstanceLister interface {
	ListByWorkspace(ctx context.Context, in uw.ListInstancesInput) (*shared.PaginatedResult[*uw.Instance], error)
}

func AllWorkspaceInstances(ctx context.Context, lister WorkspaceInstanceLister, workspaceID string) ([]*uw.Instance, error) {
	var all []*uw.Instance
	for page := 1; ; page++ {
		result, err := lister.ListByWorkspace(ctx, uw.ListInstancesInput{
			WorkspaceID: workspaceID,
			Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: shared.MaxPageSize}},
		})
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if page >= result.TotalPages || len(result.Items) == 0 {
			return all, nil
		}
	}
}
