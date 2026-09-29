package sip_trunk

import "context"

type EngineStore interface {
	FindEnabled(ctx context.Context) ([]*SIPTrunk, error)
	UpdateStatus(ctx context.Context, id string, status RegistrationStatus, lastError string) error
}

type Repository interface {
	EngineStore
	Create(ctx context.Context, trunk *SIPTrunk) error
	Update(ctx context.Context, trunk *SIPTrunk) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindInWorkspace(ctx context.Context, workspaceID, id string) (*SIPTrunk, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*SIPTrunk, error)
}
