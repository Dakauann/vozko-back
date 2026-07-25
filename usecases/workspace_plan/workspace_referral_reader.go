package workspace_plan_usecase

import (
	"context"
	"errors"

	"vozko/domain/affiliate"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type WorkspaceOwnerReader interface {
	GetOwnerUserID(workspaceID string) (string, error)
}

type workspaceReferralReaderAdapter struct {
	affiliates affiliate.Repository
	workspaces WorkspaceOwnerReader
}

func NewWorkspaceReferralReader(
	affiliates affiliate.Repository,
	workspaces WorkspaceOwnerReader,
) workspace_plan.WorkspaceReferralReader {
	return &workspaceReferralReaderAdapter{
		affiliates: affiliates,
		workspaces: workspaces,
	}
}

func (a *workspaceReferralReaderAdapter) GetAffiliateIDByWorkspaceID(workspaceID string) (string, error) {
	if a == nil || a.affiliates == nil || workspaceID == "" {
		return "", nil
	}

	ref, err := a.affiliates.GetReferralByWorkspaceID(context.Background(), workspaceID)
	if err != nil && !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		return "", err
	}
	if ref != nil && ref.AffiliateID != "" {
		return ref.AffiliateID, nil
	}

	if a.workspaces == nil {
		return "", nil
	}
	ownerID, err := a.workspaces.GetOwnerUserID(workspaceID)
	if err != nil {
		return "", err
	}
	if ownerID == "" {
		return "", nil
	}
	owner, err := a.affiliates.GetByUserID(context.Background(), ownerID)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return "", nil
		}
		return "", err
	}
	if owner == nil || !owner.Active || owner.Tier != affiliate.TierReseller {
		return "", nil
	}
	return owner.ID, nil
}
