package leadaction

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	StaleAfter        = 2 * time.Minute
	MaxAttempts       = 5
	MaxIdempotencyKey = 128
	BatchSize         = 500
	SnapshotRetention = 24 * time.Hour
)

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

func (s Status) Terminal() bool {
	return s == StatusDone || s == StatusFailed
}

type Phase string

const (
	PhaseEdit Phase = "edit"
	PhaseMeta Phase = "meta"
)

type FailureCode string

const (
	FailureStalled      FailureCode = "stalled"
	FailureForbidden    FailureCode = "forbidden"
	FailureInvalid      FailureCode = "invalid"
	FailureInternal     FailureCode = "internal"
	FailureSnapshotLost FailureCode = "snapshot_lost"
)

type SkipReason string

const (
	SkipUnchanged SkipReason = "unchanged"
	SkipGone      SkipReason = "gone"
)

type Result struct {
	Matched         int                `json:"matched"`
	Selected        int                `json:"selected"`
	Processed       int                `json:"processed"`
	Changed         int                `json:"changed"`
	Skipped         map[SkipReason]int `json:"skipped"`
	MetaApplied     int                `json:"metaApplied,omitempty"`
	MetaFailed      int                `json:"metaFailed,omitempty"`
	MetaUnavailable bool               `json:"metaUnavailable,omitempty"`
}

type Run struct {
	ID                 string
	WorkspaceID        string
	ActorID            string
	IsAdmin            bool
	DepartmentID       string
	Action             Action
	Params             Params
	IdempotencyKey     string
	RequestFingerprint string
	Status             Status
	Phase              Phase
	Cursor             string
	Result             Result
	FailureCode        FailureCode
	Attempts           int
	Claim              string
	HeartbeatAt        *time.Time
	NotBefore          *time.Time
	StartedAt          *time.Time
	FinishedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type RunRequest struct {
	ID                 string
	WorkspaceID        string
	ActorID            string
	IsAdmin            bool
	DepartmentID       string
	Action             Action
	Params             Params
	IdempotencyKey     string
	RequestFingerprint string
	Matched            int
	Selected           int
}

func ValidKey(key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && len(key) <= MaxIdempotencyKey
}

func NewRun(r RunRequest, now time.Time) (*Run, error) {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(r.ActorID) == "" {
		return nil, ErrActorRequired
	}
	if !ValidKey(r.IdempotencyKey) {
		return nil, ErrIdempotencyKeyRequired
	}
	if !r.Action.Runs() {
		return nil, ErrNotARun
	}
	if err := r.Params.Validate(r.Action); err != nil {
		return nil, err
	}
	at := now.UTC()
	return &Run{
		ID: r.ID, WorkspaceID: strings.TrimSpace(r.WorkspaceID), ActorID: strings.TrimSpace(r.ActorID), IsAdmin: r.IsAdmin,
		DepartmentID: strings.TrimSpace(r.DepartmentID), Action: r.Action, Params: r.Params.Normalized(),
		IdempotencyKey: strings.TrimSpace(r.IdempotencyKey), RequestFingerprint: r.RequestFingerprint,
		Status: StatusQueued, Phase: PhaseEdit,
		Result:    Result{Matched: r.Matched, Selected: r.Selected, Skipped: map[SkipReason]int{}},
		CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (r Run) Claimable(now time.Time) bool {
	switch r.Status {
	case StatusQueued:
		return r.NotBefore == nil || !r.NotBefore.After(now)
	case StatusRunning:
		return r.Lease().Claimable(now, StaleAfter, MaxAttempts)
	}
	return false
}

func (r Run) Lease() shared.Lease {
	return shared.Lease{Claim: r.Claim, HeartbeatAt: r.HeartbeatAt, Attempts: r.Attempts}
}

func (r *Run) Start(claim string, now time.Time) error {
	claim = strings.TrimSpace(claim)
	if claim == "" || !r.Claimable(now) {
		return ErrClaimLost
	}
	at := now.UTC()
	r.Status, r.Claim, r.HeartbeatAt, r.NotBefore = StatusRunning, claim, &at, nil
	r.Attempts++
	if r.StartedAt == nil {
		r.StartedAt = &at
	}
	r.UpdatedAt = at
	return nil
}

type BatchOutcome struct {
	Cursor    string
	Processed int
	Changed   int
	Skipped   map[SkipReason]int
}

func (r *Run) Advance(b BatchOutcome, now time.Time) {
	at := now.UTC()
	r.Cursor = b.Cursor
	r.Result.Processed += b.Processed
	r.Result.Changed += b.Changed
	if r.Result.Skipped == nil {
		r.Result.Skipped = map[SkipReason]int{}
	}
	for reason, n := range b.Skipped {
		r.Result.Skipped[reason] += n
	}
	r.HeartbeatAt, r.UpdatedAt = &at, at
}

func (r Run) NeedsMeta() bool {
	return r.Action == ActionBlock && r.Params.Blocked != nil && strings.TrimSpace(r.Params.BusinessPhoneID) != ""
}

func (r *Run) EnterMeta(now time.Time) {
	at := now.UTC()
	r.Phase, r.Cursor, r.UpdatedAt = PhaseMeta, "", at
}

func (r *Run) Applied(applied, failed int) {
	r.Result.MetaApplied += applied
	r.Result.MetaFailed += failed
}

func (r *Run) Defer(until, now time.Time) {
	due := until.UTC()
	r.Status, r.Claim, r.HeartbeatAt, r.NotBefore, r.Attempts, r.UpdatedAt = StatusQueued, "", nil, &due, 0, now.UTC()
}

func (r *Run) Release(now time.Time) {
	r.Status, r.Claim, r.HeartbeatAt, r.UpdatedAt = StatusQueued, "", nil, now.UTC()
}

func (r *Run) Finish(now time.Time) {
	at := now.UTC()
	r.Status, r.Claim, r.FinishedAt, r.UpdatedAt = StatusDone, "", &at, at
}

func (r *Run) Fail(code FailureCode, now time.Time) {
	at := now.UTC()
	r.Status, r.FailureCode, r.Claim, r.FinishedAt, r.UpdatedAt = StatusFailed, code, "", &at, at
}

func (r Run) VisibleTo(userID string, holdsCapability bool) bool {
	return holdsCapability || (strings.TrimSpace(userID) != "" && userID == r.ActorID)
}
