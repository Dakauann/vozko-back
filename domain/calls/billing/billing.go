package billing

import (
	"errors"
	"time"

	"vozko/domain/shared"
)

const (
	Exchange = "call_events_exchange"

	TopicCallCompleted = "call.billing.completed"
)

type BillingStatus string

const (
	StatusInProgress   BillingStatus = "in_progress"
	StatusCompleted    BillingStatus = "completed"
	StatusCharged      BillingStatus = "charged"
	StatusChargeFailed BillingStatus = "charge_failed"
	StatusOrphaned     BillingStatus = "orphaned"
)

type CallSource string

const (
	CallSourceSIP       CallSource = "sip"
	CallSourceWebSocket CallSource = "websocket"
)

var (
	ErrBillingRecordNotFound = errors.New("billing record not found")
	ErrAlreadyCharged        = errors.New("call already charged")
)

type CallBillingRecord struct {
	ID          string        `json:"id"`
	CallID      string        `json:"callId"`
	WorkspaceID string        `json:"workspaceId"`
	AgentID     *string       `json:"agentId,omitempty"`
	LeadID      *string       `json:"leadId,omitempty"`
	CallSource  CallSource    `json:"callSource"`
	Status      BillingStatus `json:"status"`

	CallStart   time.Time `json:"callStart"`
	CallEnd     time.Time `json:"callEnd"`
	DurationSec int       `json:"durationSec"`

	TelephonyRevenueMicros int64 `json:"telephonyRevenueMicros"`
	TotalRevenueMicros     int64 `json:"totalRevenueMicros"`

	TelephonyCostMicros int64 `json:"telephonyCostMicros"`
	TotalCostMicros     int64 `json:"totalCostMicros"`

	TelephonyProfitMicros int64 `json:"telephonyProfitMicros"`
	TotalProfitMicros     int64 `json:"totalProfitMicros"`

	TransactionID *string `json:"transactionId,omitempty"`

	RecordingURL *string `json:"recordingUrl,omitempty"`

	CallRecordID *string `json:"callRecordId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ListBillingInput struct {
	WorkspaceID string
	Status      *BillingStatus
	CallSource  *CallSource
	StartDate   *time.Time
	EndDate     *time.Time
	shared.QueryOptions
}

type ListBillingRecordsUseCase interface {
	Execute(input ListBillingInput) (*shared.PaginatedResult[*CallBillingRecord], error)
}

type Repository interface {
	Save(record *CallBillingRecord) error
	GetByCallID(callID string) (*CallBillingRecord, error)
	GetByCallIDs(callIDs []string) (map[string]*CallBillingRecord, error)
	Update(record *CallBillingRecord) error
	FindOrphaned(olderThan time.Duration) ([]*CallBillingRecord, error)
	UpdateStatus(callID string, status BillingStatus) error
	ListByWorkspaceID(input ListBillingInput) (*shared.PaginatedResult[*CallBillingRecord], error)
}
