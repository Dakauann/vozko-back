package container

import (
	"context"

	crmboard_usecase "vozko/usecases/crmboard"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

// crmBoardTargetResolver lets a bulk action address "every conversation this
// filter matches" by expanding the filter through the CRM board's own read path.
//
// It lives in the composition root, not in either usecase, because it is pure
// wiring: crmbulk must not depend on crmboard (a write fan-out has no business
// importing a read model), and crmboard must not know bulk exists. The port is
// declared by the consumer — crmbulk_usecase.TargetResolver — and satisfied here,
// which is the only place allowed to know both halves.
//
// Reusing GetEntries rather than issuing a query of its own is the load-bearing
// part. The department scope, the workspace scope and the filter compilation all
// come from the same code that rendered the operator's table, so the set they
// saw is exactly the set that changes. A second query here would be a second
// definition of "what this filter means", free to drift from the first.
type crmBoardTargetResolver struct {
	board *crmboard_usecase.Service
}

func (r crmBoardTargetResolver) ResolveTargets(
	_ context.Context,
	q crmbulk_usecase.TargetQuery,
) ([]crmbulk_usecase.EntryRef, int64, error) {
	// Page 1 at Limit: the caller's cap IS the page size, so an over-large match
	// comes back truncated with the true total alongside it, and the service can
	// report "n of total" instead of pretending it applied to everything.
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
