package workspace

type Actor struct {
	UserID        string
	Role          Role
	PlatformAdmin bool
}

func (a Actor) effectiveRole() Role {
	if a.PlatformAdmin {
		return RoleOwner
	}
	return a.Role
}

func (a Actor) ManagesMembers() error {
	if a.PlatformAdmin || a.Role.CanManageMembers() {
		return nil
	}
	return ErrInsufficientPermissions
}

func (a Actor) CanReassign(target *Member) error {
	if target.Role == RoleOwner {
		return ErrCannotChangeOwnerRole
	}
	if target.Role == RoleAdmin && a.effectiveRole() != RoleOwner {
		return ErrInsufficientPermissions
	}
	return nil
}

func (a Actor) CanInviteAs(role Role) error {
	if role == RoleOwner {
		return ErrInvalidRole
	}
	if role == RoleAdmin {
		return a.ManagesMembers()
	}
	return nil
}

func (a Actor) CanRemove(target *Member) error {
	if target.Role == RoleOwner {
		return ErrCannotRemoveOwner
	}
	if a.UserID != "" && a.UserID == target.UserID {
		return nil
	}
	if target.Role == RoleAdmin && a.effectiveRole() != RoleOwner {
		return ErrInsufficientPermissions
	}
	return nil
}

func CanEditPermissions(actorUserID string, target *Member) error {
	if actorUserID == target.UserID {
		return ErrCannotModifySelf
	}
	if target.Role == RoleOwner || target.Role == RoleAdmin {
		return ErrCannotChangeOwnerRole
	}
	return nil
}
