package container

import (
	"context"

	crmboard_usecase "vozko/usecases/crmboard"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type crmBoardTargetResolver struct {
	board *crmboard_usecase.Service
}

func (r crmBoardTargetResolver) ResolveTargets(
	_ context.Context,
	q crmbulk_usecase.TargetQuery,
) ([]crmbulk_usecase.EntryRef, int64, error) {
	entries, total, err := r.board.GetEntries(crmboard_usecase.EntriesInput{
		WorkspaceID:          q.WorkspaceID,
		UserID:               q.ActorID,
		IsAdmin:              q.IsAdmin,
		SelectedDepartmentID: q.SelectedDepartmentID,
		Filter:               q.Filter,
		Page:                 1,
		PageSize:             q.Limit,
	})
	if err != nil {
		return nil, 0, err
	}

	refs := make([]crmbulk_usecase.EntryRef, 0, len(entries))
	for i := range entries {
		if entries[i].EntryID == "" {
			continue
		}
		refs = append(refs, crmbulk_usecase.EntryRef{
			EntryID:   entries[i].EntryID,
			EntryType: string(entries[i].EntryType),
		})
	}
	return refs, total, nil
}
