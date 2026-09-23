package report

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Kind string

const (
	KindAttendanceOverview  Kind = "attendance_overview"
	KindConversationEntries Kind = "conversation_entries"
	KindBalanceTransactions Kind = "balance_transactions"
	KindOpportunities       Kind = "opportunities"
)

type Format string

const (
	FormatCSV  Format = "csv"
	FormatXLSX Format = "xlsx"
	FormatPDF  Format = "pdf"
)

func (f Format) Valid() bool {
	switch f {
	case FormatCSV, FormatXLSX, FormatPDF:
		return true
	}
	return false
}

func (f Format) ContentType() string {
	switch f {
	case FormatXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case FormatPDF:
		return "application/pdf"
	default:
		return "text/csv; charset=utf-8"
	}
}

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusExpired Status = "expired"
)

func (s Status) Terminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusExpired
}

type FailureCode string

const (
	FailureUnknownKind    FailureCode = "unknown_kind"
	FailureUnsupported    FailureCode = "unsupported_format"
	FailureTooManyRows    FailureCode = "too_many_rows"
	FailureRenderFailed   FailureCode = "render_failed"
	FailureUploadFailed   FailureCode = "upload_failed"
	FailureNoRenderer     FailureCode = "renderer_not_configured"
	FailureSourceFailed   FailureCode = "source_unavailable"
	FailureCancelled      FailureCode = "cancelled"
	FailureWorkspaceEmpty FailureCode = "workspace_required"
	FailureEmptyResult    FailureCode = "empty_result"
)

const (
	DefaultRetention = 7 * 24 * time.Hour
	MaxParamsBytes   = 64 * 1024
	IdempotencyGrace = 2 * time.Minute
)

var (
	ErrWorkspaceRequired = errors.New("report: workspace is required")
	ErrKindRequired      = errors.New("report: kind is required")
	ErrUnknownKind       = errors.New("report: unknown kind")
	ErrInvalidFormat     = errors.New("report: invalid format")
	ErrFormatUnsupported = errors.New("report: this kind does not support that format")
	ErrParamsTooLarge    = errors.New("report: parameters are too large")
	ErrNotFound          = errors.New("report: not found")
	ErrNotReady          = errors.New("report: the file is not ready yet")
	ErrExpired           = errors.New("report: the file has expired")
	ErrNoRenderer        = errors.New("report: no renderer is registered for this kind")
	ErrEmptyResult       = errors.New("report: the filters matched no rows")
	ErrForbidden         = errors.New("report: this report belongs to another workspace")
)

type Job struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	RequestedBy string          `json:"requestedBy,omitempty"`
	Kind        Kind            `json:"kind"`
	Format      Format          `json:"format"`
	Locale      string          `json:"locale,omitempty"`
	Params      json.RawMessage `json:"params,omitempty" swaggertype:"object"`
	Fingerprint string          `json:"-"`

	Status      Status      `json:"status"`
	Progress    int         `json:"progress"`
	FailureCode FailureCode `json:"failureCode,omitempty"`

	ObjectKey string `json:"-"`
	Filename  string `json:"filename,omitempty"`
	SizeBytes int64  `json:"sizeBytes"`
	RowCount  int64  `json:"rowCount"`

	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func (j *Job) Normalize() {
	j.ID = strings.TrimSpace(j.ID)
	j.WorkspaceID = strings.TrimSpace(j.WorkspaceID)
	j.RequestedBy = strings.TrimSpace(j.RequestedBy)
	j.Locale = strings.ToLower(strings.TrimSpace(j.Locale))
	if j.Format == "" {
		j.Format = FormatCSV
	}
	if j.Status == "" {
		j.Status = StatusQueued
	}
	if len(j.Params) == 0 {
		j.Params = json.RawMessage("{}")
	}
}

func (j *Job) Validate(known func(Kind) (Renderer, bool)) error {
	if j.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if j.Kind == "" {
		return ErrKindRequired
	}
	if !j.Format.Valid() {
		return ErrInvalidFormat
	}
	if len(j.Params) > MaxParamsBytes {
		return ErrParamsTooLarge
	}

	renderer, found := known(j.Kind)
	if !found {
		return ErrUnknownKind
	}
	for _, supported := range renderer.Formats() {
		if supported == j.Format {
			return nil
		}
	}
	return ErrFormatUnsupported
}

func (j *Job) IsExpired(now time.Time) bool {
	return j.ExpiresAt != nil && now.After(*j.ExpiresAt)
}

func (j *Job) EffectiveStatus(now time.Time) Status {
	if j.Status == StatusDone && j.IsExpired(now) {
		return StatusExpired
	}
	return j.Status
}

type Artifact struct {
	Data        []byte
	ContentType string
	Filename    string
	RowCount    int64
}

type ProgressFunc func(percent int)

type Renderer interface {
	Kind() Kind
	Formats() []Format
	Render(ctx context.Context, job Job, progress ProgressFunc) (Artifact, error)
}

type ListQuery struct {
	WorkspaceID string
	Kinds       []Kind
	Statuses    []Status
	RequestedBy string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Limit       int
	Offset      int
}

const (
	DefaultListLimit = 25
	MaxListLimit     = 100
)

func (q *ListQuery) Normalize() {
	q.WorkspaceID = strings.TrimSpace(q.WorkspaceID)
	q.RequestedBy = strings.TrimSpace(q.RequestedBy)
	if q.Limit <= 0 {
		q.Limit = DefaultListLimit
	}
	if q.Limit > MaxListLimit {
		q.Limit = MaxListLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
}

type ListPage struct {
	Jobs  []Job `json:"reports"`
	Total int64 `json:"total"`
}

type Repository interface {
	Create(job *Job) error
	GetByID(workspaceID, id string) (*Job, error)
	FindReusable(workspaceID, fingerprint string, since time.Time) (*Job, error)
	List(query ListQuery) (ListPage, error)
	MarkRunning(id string, at time.Time) error
	MarkDone(id string, objectKey, filename string, size, rows int64, at time.Time, expiresAt *time.Time) error
	MarkFailed(id string, failureCode FailureCode, at time.Time) error
	UpdateProgress(id string, percent int) error
	ExpireBefore(now time.Time, limit int) (int64, error)
}

type Storage interface {
	Upload(key string, data []byte, contentType string) error
	Download(ctx context.Context, key string) ([]byte, string, error)
}

type File struct {
	Data        []byte
	ContentType string
	Filename    string
}
