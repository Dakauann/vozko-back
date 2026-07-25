package workspace

import (
	workspacedomain "vozko/domain/workspace"
)

type CreateWorkspaceRequest struct {
	Name         string `json:"name" example:"Minha Empresa"`
	ReferralCode string `json:"referralCode" example:"AFIL123"`
}

type UpdateWorkspaceRequest struct {
	Name *string `json:"name,omitempty" example:"Minha Empresa"`
}

type InviteMemberRequest struct {
	Email         string   `json:"email"`
	Role          string   `json:"role"`
	RoleID        string   `json:"roleId,omitempty"`
	DepartmentIDs []string `json:"departmentIds,omitempty"`
}

type AcceptInviteRequest struct {
	Token string `json:"token" example:"inv_tok_a1b2c3"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role" example:"admin"`
}

type AssignCustomRoleRequest struct {
	RoleID string `json:"roleId" example:"role_a1b2c3"`
}

func toWorkspaceRole(role string) workspacedomain.Role {
	return workspacedomain.Role(role)
}
