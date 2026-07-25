package branch_usecase

import (
	"errors"
	"strings"

	"vozko/domain/branch"
)

type routeToBranchUseCase struct {
	repo  branch.Repository
	store branch.BindingStore
}

// NewRouteToBranchUseCase resolves a branch to the live contacts an INVITE should
// fork to, applying enabled/DND and cross-workspace isolation. Busy/double-ring
// is handled by the DialerSession Reserve/Release gate, not here.
func NewRouteToBranchUseCase(repo branch.Repository, store branch.BindingStore) branch.RouteToBranchUseCase {
	return &routeToBranchUseCase{repo: repo, store: store}
}

func (uc *routeToBranchUseCase) Execute(workspaceID, sipUser string) (branch.RouteResult, error) {
	su := strings.ToLower(strings.TrimSpace(sipUser))
	if su == "" {
		return branch.RouteResult{Reason: branch.RouteBranchNotFound}, nil
	}

	b, err := uc.repo.FindByGlobalSIPUser(su)
	if err != nil {
		if errors.Is(err, branch.ErrBranchNotFound) || errors.Is(err, branch.ErrBranchSIPUserTaken) {
			return branch.RouteResult{Reason: branch.RouteBranchNotFound}, nil
		}
		return branch.RouteResult{}, err
	}

	// Cross-workspace isolation: a transfer within workspace W must never resolve
	// a branch owned by another workspace.
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" && !b.IsOwnedBy(workspaceID) {
		return branch.RouteResult{Reason: branch.RouteBranchNotFound}, nil
	}
	if !b.Enabled {
		return branch.RouteResult{Reason: branch.RouteDisabled, Branch: b}, nil
	}
	if b.DND {
		return branch.RouteResult{Reason: branch.RouteDND, Branch: b}, nil
	}

	live, err := uc.store.ListLive(su)
	if err != nil {
		return branch.RouteResult{}, err
	}
	if len(live) == 0 {
		return branch.RouteResult{Reason: branch.RouteOffline, Branch: b}, nil
	}
	return branch.RouteResult{Reason: branch.RouteOK, Branch: b, Contacts: live}, nil
}
