package callsession

import (
	"context"
	"time"

	"vozko/domain/conversation"
	workspace_domain "vozko/domain/workspace"
)

type CallAdmissionInput struct {
	WorkspaceID      string
	GlobalMax        int64
	SlotPollInterval time.Duration
	SlotPollTimeout  time.Duration
	ReservationTTL   time.Duration
	OnWaitingForSlot func()

	CallChannel string
}

type CallAdmissionLease struct {
	WorkspaceID         string
	ReservedMicros      int64
	PerMinuteCostMicros int64
	CallChannel         string
	SlotAcquired        bool
	AcquiredAt          time.Time
}

type CallSlotGate interface {
	Acquire(workspaceID string, globalMax int64) (workspace_domain.AcquireResult, bool)
	Release(workspaceID string)
}

type CallAdmissionCoordinator interface {
	Acquire(ctx context.Context, input CallAdmissionInput) (*CallAdmissionLease, error)
	Refresh(lease *CallAdmissionLease, ttl time.Duration) error
	Release(lease *CallAdmissionLease) error
}

type StartOutboundCallInput struct {
	WorkspaceID      string
	UserID           string
	IsAdmin          bool
	TargetPhone      string
	WhatsAppPhoneID  string
	TrunkID          string
	LeadID           string
	CallListItemID   string
	OnWaitingForSlot func()
}

type StartOutboundCallResult struct {
	Call                conversation.CRMCall
	PhoneNumber         string
	PerMinuteCostMicros int64
	ReservedMicros      int64
	Admission           *CallAdmissionLease
	LeadID              string
	TrunkID             string
	CallListItemID      string
}

type StartOutboundCallUseCase interface {
	Execute(ctx context.Context, input StartOutboundCallInput) (*StartOutboundCallResult, error)
}

type EndOutboundCallInput struct {
	Call             conversation.CRMCall
	Admission        *CallAdmissionLease
	Hangup           bool
	ReleaseAdmission bool
}

type EndOutboundCallUseCase interface {
	Execute(ctx context.Context, input EndOutboundCallInput) error
}
