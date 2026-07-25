package workspacephoneaccess

import (
	phoneaccessdomain "vozko/domain/workspace_phone_access"
)

type PhoneAccessResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	PhoneID     string `json:"phoneId"`
	GrantedBy   string `json:"grantedBy"`
	CreatedAt   string `json:"createdAt"`
}

type GrantPhoneAccessRequest struct {
	WorkspaceID string `json:"workspaceId"`
	PhoneID     string `json:"phoneId"`
}

type RevokePhoneAccessRequest struct {
	WorkspaceID string `json:"workspaceId"`
	PhoneID     string `json:"phoneId"`
}

func mapPhoneAccessToResponse(a *phoneaccessdomain.WorkspacePhoneAccess) PhoneAccessResponse {
	return PhoneAccessResponse{
		ID:          a.ID,
		WorkspaceID: a.WorkspaceID,
		PhoneID:     a.PhoneID,
		GrantedBy:   a.GrantedBy,
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
