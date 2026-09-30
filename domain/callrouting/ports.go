package callrouting

import (
	"context"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
)

type RoutedCall interface {
	ID() string
	WorkspaceID() string
	RemoteNumber() string
	Channel() string
	OwnerSession() (callsession.CallSession, bool)
	Hold(music []byte) error
	StopHold()
	Connect(session callsession.CallSession) error
	SendAudio(pcm []byte) error
	Done() <-chan struct{}
	Hangup() error
}

type CallDirectory interface {
	Find(workspaceID, callID string) (RoutedCall, bool)
}

type AgentActivity interface {
	Stats(workspaceID string, userIDs []string) []AgentStats
	CallEnded(workspaceID, userID string, at time.Time)
	CallAnswered(workspaceID, userID string)
}

type QueueRepository interface {
	Create(ctx context.Context, queue *Queue) error
	Update(ctx context.Context, queue *Queue) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindInWorkspace(ctx context.Context, workspaceID, id string) (*Queue, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*Queue, error)
}

type TransferLog interface {
	Record(ctx context.Context, record TransferRecord) error
	Finish(ctx context.Context, workspaceID, id string, outcome TransferOutcome, answeredBy string, at time.Time) error
}

type QueueMembers interface {
	UserIDs(ctx context.Context, queue Queue) ([]string, error)
}

type QueueEntry struct {
	WorkspaceID string
	QueueID     string
	Notes       string
	From        string
	Call        RoutedCall
}

type QueueEntrance interface {
	EnterQueue(ctx context.Context, entry QueueEntry) (TransferOutcome, error)
}

type AnswerPermission interface {
	MayAnswerCalls(userID, workspaceID, channel string) bool
}

type MemberNames interface {
	ResolveUsernames(userIDs []string) map[string]string
}

type ParkInput struct {
	Call        conversation.CRMCall
	Admission   *callsession.CallAdmissionLease
	WorkspaceID string
	Phone       string
	Direction   cdr.Direction
	StartedAt   time.Time
}

type CallParking interface {
	Park(ctx context.Context, input ParkInput) (RoutedCall, error)
}
