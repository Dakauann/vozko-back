package lead_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/lead"
)

type OwnerReach struct {
	owners     actor.OwnerDirectory
	visibility MemberVisibility
}

func NewOwnerReach(owners actor.OwnerDirectory, visibility MemberVisibility) (*OwnerReach, error) {
	if owners == nil || visibility == nil {
		return nil, fmt.Errorf("%w: owner reach", errCommandsIncomplete)
	}
	return &OwnerReach{owners: owners, visibility: visibility}, nil
}

func (r *OwnerReach) CheckOwner(a Actor, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil
	}
	if err := lead.ValidateOwner(owner); err != nil {
		return err
	}
	belongs, err := r.owners.Belongs(a.WorkspaceID, owner)
	if err != nil {
		return err
	}
	if !belongs {
		return lead.ErrLeadOwnerOutsideWorkspace
	}
	if actor.KindOf(owner) != actor.KindHuman {
		return nil
	}
	visible, err := r.visibility.CanView(a.UserID, owner, a.WorkspaceID, a.IsAdmin)
	if err != nil {
		return err
	}
	if !visible {
		return lead.ErrLeadOwnerOutOfReach
	}
	return nil
}
