package dialer_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"vozko/domain/dialer"
)

type startOutboundSessionUseCase struct {
	repo dialer.Repository
}

func NewStartOutboundSessionUseCase(repo dialer.Repository) dialer.StartOutboundSessionUseCase {
	return &startOutboundSessionUseCase{repo: repo}
}

func (uc *startOutboundSessionUseCase) Execute(_ context.Context, input dialer.StartOutboundSessionInput) (*dialer.Session, error) {
	if uc.repo == nil {
		return nil, fmt.Errorf("dialer repository is required")
	}

	if existing, err := uc.repo.FindActiveByOwnerConnection(input.WorkspaceID, input.OwnerConnectionID); err == nil && existing != nil {
		return nil, dialer.ErrSessionAlreadyActive
	} else if err != nil && !errors.Is(err, dialer.ErrSessionNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	session := &dialer.Session{
		ID:                uuid.New().String(),
		WorkspaceID:       input.WorkspaceID,
		OwnerUserID:       input.OwnerUserID,
		OwnerConnectionID: input.OwnerConnectionID,
		EntryID:           input.EntryID,
		EntryType:         input.EntryType,
		TargetPhone:       input.TargetPhone,
		Status:            dialer.SessionStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	session.Normalize()
	if err := session.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Create(session); err != nil {
		return nil, err
	}
	return session, nil
}
