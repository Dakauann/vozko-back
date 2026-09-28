package metaplatform

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnknownApp              = errors.New("meta platform: unknown app")
	ErrRequestNotFound         = errors.New("meta platform: deletion request not found")
	ErrAppScopedUserIDRequired = errors.New("meta platform: app scoped user id is required")
)

type App string

const (
	AppMeta      App = "meta"
	AppInstagram App = "instagram"
)

func (a App) Valid() bool { return a == AppMeta || a == AppInstagram }

type DeletionStatus string

const (
	DeletionReceived  DeletionStatus = "RECEIVED"
	DeletionCompleted DeletionStatus = "COMPLETED"
	DeletionFailed    DeletionStatus = "FAILED"
)

type DeletionRequest struct {
	Code            string
	App             App
	AppScopedUserID string
	Status          DeletionStatus
	Detail          string
	RequestedAt     time.Time
	CompletedAt     *time.Time
}

type DeletionRequestRepository interface {
	Create(ctx context.Context, r *DeletionRequest) error
	FindByCode(ctx context.Context, code string) (*DeletionRequest, error)
	Finish(ctx context.Context, code string, status DeletionStatus, detail string, at time.Time) error
}

type AppUserHandler interface {
	RevokeAppUser(ctx context.Context, appScopedUserID string) error
	EraseAppUser(ctx context.Context, appScopedUserID string) error
}
