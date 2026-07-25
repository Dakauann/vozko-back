package workspacetemplateaccess

import (
	workspacetemplateaccessdomain "vozko/domain/workspace_template_access"
)

type AccessResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	TemplateID  string `json:"templateId"`
	GrantedBy   string `json:"grantedBy"`
	CreatedAt   string `json:"createdAt"`
}

type GrantAccessRequest struct {
	WorkspaceID string `json:"workspaceId"`
	TemplateID  string `json:"templateId"`
}

type RevokeAccessRequest struct {
	WorkspaceID string `json:"workspaceId"`
	TemplateID  string `json:"templateId"`
}

func toAccessResponse(a *workspacetemplateaccessdomain.WorkspaceTemplateAccess) AccessResponse {
	return AccessResponse{
		ID:          a.ID,
		WorkspaceID: a.WorkspaceID,
		TemplateID:  a.TemplateID,
		GrantedBy:   a.GrantedBy,
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
