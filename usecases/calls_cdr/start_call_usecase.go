package calls_cdr_usecase

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calls/cdr"
)

type startCallUseCase struct {
	repo cdr.Repository
}

func NewStartCallUseCase(repo cdr.Repository) cdr.StartCallUseCase {
	return &startCallUseCase{repo: repo}
}

func (uc *startCallUseCase) Execute(input cdr.StartCallInput) (*cdr.Call, error) {
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	call := &cdr.Call{
		ID:             uuid.New().String(),
		CallID:         strings.TrimSpace(input.CallID),
		WorkspaceID:    strings.TrimSpace(input.WorkspaceID),
		Type:           input.Type,
		Direction:      input.Direction,
		Source:         input.Source,
		Status:         cdr.StatusInProgress,
		AgentID:        input.AgentID,
		ConversationID: input.ConversationID,
		LeadID:         input.LeadID,
		PhoneFrom:      strings.TrimSpace(input.PhoneFrom),
		PhoneTo:        strings.TrimSpace(input.PhoneTo),
		TrunkID:        input.TrunkID,
		ParentCallID:   input.ParentCallID,
		StartedAt:      startedAt,
	}

	if err := call.Validate(); err != nil {
		return nil, err
	}

	if existing, _ := uc.repo.GetByCallID(call.CallID); existing != nil {
		return existing, nil
	}

	if err := uc.repo.Create(call); err != nil {
		return nil, err
	}
	return call, nil
}
