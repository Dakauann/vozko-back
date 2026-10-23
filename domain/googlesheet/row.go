package googlesheet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	MaxIdempotencyKeyLength = 200
	MaxBackoff              = 64 * time.Second
	MaxTransientAttempts    = 8
	DefaultBatchRows        = 500
	DefaultBatchBytes       = 1536 << 10
)

type RowStatus string

const (
	RowPending  RowStatus = "pending"
	RowInFlight RowStatus = "in_flight"
	RowWritten  RowStatus = "written"
	RowHeld     RowStatus = "held"
	RowFailed   RowStatus = "failed"
)

type OriginKind string

const (
	OriginAgent    OriginKind = "agent"
	OriginWorkflow OriginKind = "workflow"
	OriginCopilot  OriginKind = "copilot"
	OriginManual   OriginKind = "manual"
)

func (k OriginKind) Valid() bool {
	switch k {
	case OriginAgent, OriginWorkflow, OriginCopilot, OriginManual:
		return true
	}
	return false
}

type Origin struct {
	Kind       OriginKind
	AgentID    string
	WorkflowID string
	RunID      string
	ActorID    string
	LeadID     string
	EntryID    string
	EntryType  string
}

type Row struct {
	ID             string
	WorkspaceID    string
	SheetID        string
	IdempotencyKey string
	Cells          Cells
	SheetVersion   int
	Origin         Origin
	Status         RowStatus
	Attempts       int
	NextAttemptAt  time.Time
	BatchID        string
	RowNumber      int
	ErrorCode      string
	ErrorDetail    string
	CreatedAt      time.Time
	WrittenAt      *time.Time
}

func NewRow(s *Sheet, cells Cells, idempotencyKey string, origin Origin, now time.Time) (*Row, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if !origin.Kind.Valid() {
		return nil, ErrOriginRequired
	}
	return &Row{
		WorkspaceID:    s.WorkspaceID,
		SheetID:        s.ID,
		IdempotencyKey: boundKey(idempotencyKey),
		Cells:          cells,
		SheetVersion:   s.Version,
		Origin:         origin,
		Status:         RowPending,
		NextAttemptAt:  now,
		CreatedAt:      now,
	}, nil
}

func KeyForTool(entryID, callID string, values map[string]any) string {
	canonical, _ := json.Marshal(values)
	sum := sha256.Sum256(canonical)
	return boundKey("ai:" + entryID + ":" + callID + ":" + hex.EncodeToString(sum[:])[:16])
}

func KeyForNode(runID, nodeVisit string) string {
	return boundKey("wf:" + runID + ":" + nodeVisit)
}

func KeyForCopilot(actionID string) string {
	return boundKey("elo:" + actionID)
}

func KeyForManual(userID, clientKey string) string {
	return boundKey("manual:" + userID + ":" + strings.TrimSpace(clientKey))
}

func boundKey(key string) string {
	if len(key) <= MaxIdempotencyKeyLength {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return key[:MaxIdempotencyKeyLength-33] + ":" + hex.EncodeToString(sum[:])[:32]
}

func Backoff(attempt int, jitter time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		return MaxBackoff
	}
	wait := time.Duration(1<<attempt)*time.Second + jitter
	if wait > MaxBackoff {
		return MaxBackoff
	}
	return wait
}

func PlanBatch(sizes []int, maxRows, maxBytes int) int {
	total := 0
	for i, size := range sizes {
		if i >= maxRows {
			return i
		}
		if i > 0 && total+size > maxBytes {
			return i
		}
		total += size
	}
	return len(sizes)
}

type FailureKind string

const (
	FailureAuth        FailureKind = "auth"
	FailurePermission  FailureKind = "permission"
	FailureNotFound    FailureKind = "not_found"
	FailureRateLimited FailureKind = "rate_limited"
	FailureInvalid     FailureKind = "invalid"
	FailureTransient   FailureKind = "transient"
)

type Failure struct {
	Kind       FailureKind
	RetryAfter time.Duration
	Detail     string
}

type Outcome string

const (
	OutcomeRetry          Outcome = "retry"
	OutcomeHold           Outcome = "hold"
	OutcomeIsolate        Outcome = "isolate"
	OutcomeWaitForAccount Outcome = "wait_for_account"
)

func Decide(f Failure, attempts int) Outcome {
	switch f.Kind {
	case FailureRateLimited:
		return OutcomeRetry
	case FailureAuth:
		return OutcomeWaitForAccount
	case FailurePermission, FailureNotFound:
		return OutcomeHold
	case FailureInvalid:
		return OutcomeIsolate
	}
	if attempts >= MaxTransientAttempts {
		return OutcomeHold
	}
	return OutcomeRetry
}

func (f Failure) AttentionReason() AttentionReason {
	switch f.Kind {
	case FailurePermission:
		return ReasonAccessLost
	case FailureNotFound:
		return ReasonSpreadsheetGone
	}
	return ReasonGoogleUnavailable
}

const (
	CodeWriteRateLimited        = "google_sheet_write_rate_limited"
	CodeWriteUnavailable        = "google_sheet_write_google_unavailable"
	CodeWriteAccessLost         = "google_sheet_write_access_lost"
	CodeWriteSpreadsheetMissing = "google_sheet_write_spreadsheet_missing"
	CodeWriteAccountRefused     = "google_sheet_write_account_refused"
	CodeWriteRefusedByGoogle    = "google_sheet_write_refused_by_google"
)

var failureCodes = map[FailureKind]string{
	FailureRateLimited: CodeWriteRateLimited,
	FailureTransient:   CodeWriteUnavailable,
	FailurePermission:  CodeWriteAccessLost,
	FailureNotFound:    CodeWriteSpreadsheetMissing,
	FailureAuth:        CodeWriteAccountRefused,
	FailureInvalid:     CodeWriteRefusedByGoogle,
}

func (f Failure) Code() string {
	if code, ok := failureCodes[f.Kind]; ok {
		return code
	}
	return CodeWriteUnavailable
}

func RowErrorCodes() []string {
	return []string{
		CodeWriteRateLimited, CodeWriteUnavailable, CodeWriteAccessLost, CodeWriteSpreadsheetMissing,
		CodeWriteAccountRefused, CodeWriteRefusedByGoogle, CodeInvalidValue,
	}
}

func AttentionReasons() []AttentionReason {
	return []AttentionReason{
		ReasonHeaderMismatch, ReasonAccessLost, ReasonSpreadsheetGone, ReasonSheetFull, ReasonGoogleUnavailable, ReasonAccountChanged,
	}
}
