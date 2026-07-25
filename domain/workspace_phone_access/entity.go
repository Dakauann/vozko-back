package workspace_phone_access

import (
	"errors"
	"time"
)

var (
	ErrAccessNotFound      = errors.New("phone access not found")
	ErrAccessAlreadyExists = errors.New("workspace already has access to this phone number")
	ErrNoAccess            = errors.New("workspace does not have access to this phone number")
)

type WorkspacePhoneAccess struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	PhoneID     string    `json:"phoneId"`
	GrantedBy   string    `json:"grantedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

func NewWorkspacePhoneAccess(id, workspaceID, phoneID, grantedBy string) *WorkspacePhoneAccess {
	return &WorkspacePhoneAccess{
		ID:          id,
		WorkspaceID: workspaceID,
		PhoneID:     phoneID,
		GrantedBy:   grantedBy,
		CreatedAt:   time.Now(),
	}
}
