package workspace_template_access

import (
	"errors"
	"time"
)

var (
	ErrAccessNotFound      = errors.New("template access not found")
	ErrAccessAlreadyExists = errors.New("workspace already has access to this template")
	ErrNoAccess            = errors.New("workspace does not have access to this template")
)

type WorkspaceTemplateAccess struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	TemplateID  string    `json:"templateId"`
	GrantedBy   string    `json:"grantedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

func NewWorkspaceTemplateAccess(id, workspaceID, templateID, grantedBy string) *WorkspaceTemplateAccess {
	return &WorkspaceTemplateAccess{
		ID:          id,
		WorkspaceID: workspaceID,
		TemplateID:  templateID,
		GrantedBy:   grantedBy,
		CreatedAt:   time.Now(),
	}
}
