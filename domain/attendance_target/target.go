package attendance_target

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"vozko/domain/attendance"
)

type Scope string

const (
	ScopeWorkspace  Scope = "workspace"
	ScopeDepartment Scope = "department"
	ScopeMember     Scope = "member"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopeWorkspace, ScopeDepartment, ScopeMember:
		return true
	}
	return false
}

func (s Scope) NeedsScopeID() bool {
	return s == ScopeDepartment || s == ScopeMember
}

var (
	ErrWorkspaceRequired = errors.New("attendance target: workspace is required")
	ErrInvalidScope      = errors.New("attendance target: invalid scope")
	ErrScopeIDRequired   = errors.New("attendance target: this scope requires a scope id")
	ErrScopeIDForbidden  = errors.New("attendance target: the workspace scope takes no scope id")
	ErrUnknownMetric     = errors.New("attendance target: unknown metric")
	ErrMetricNotTargetab = errors.New("attendance target: metric cannot carry a target")
	ErrInvalidValue      = errors.New("attendance target: value must be a finite non-negative number")
	ErrCurrencyRequired  = errors.New("attendance target: a money target requires a currency")
	ErrCurrencyForbidden = errors.New("attendance target: only a money target carries a currency")
	ErrPeriodRequired    = errors.New("attendance target: period is required")
	ErrPeriodClosed      = errors.New("attendance target: the period has already closed")
	ErrNotFound          = errors.New("attendance target: not found")
)

type Target struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Scope       Scope     `json:"scope"`
	ScopeID     string    `json:"scopeId,omitempty"`
	MetricKey   string    `json:"metricKey"`
	PeriodStart time.Time `json:"periodStart"`
	Value       float64   `json:"value"`
	Currency    string    `json:"currency,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Month struct {
	Year  int
	Month time.Month
}

const MonthLayout = "2006-01"

func ParseMonth(raw string) (Month, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Month{}, true
	}
	parsed, err := time.Parse(MonthLayout, trimmed)
	if err != nil {
		return Month{}, false
	}
	return Month{Year: parsed.Year(), Month: parsed.Month()}, true
}

func MonthAt(instant time.Time, loc *time.Location) Month {
	if loc == nil {
		loc = time.UTC
	}
	local := instant.In(loc)
	return Month{Year: local.Year(), Month: local.Month()}
}

func (m Month) IsZero() bool { return m.Year == 0 }

func (m Month) Start(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, loc)
}

func (m Month) String() string {
	if m.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month))
}

func NormalizePeriodStart(at time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := at.In(loc)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
}

func (t *Target) Normalize(loc *time.Location) {
	t.ID = strings.TrimSpace(t.ID)
	t.WorkspaceID = strings.TrimSpace(t.WorkspaceID)
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.MetricKey = strings.ToLower(strings.TrimSpace(t.MetricKey))
	t.Currency = strings.ToUpper(strings.TrimSpace(t.Currency))
	t.CreatedBy = strings.TrimSpace(t.CreatedBy)
	if t.Scope == "" {
		t.Scope = ScopeWorkspace
	}
	if t.Scope == ScopeWorkspace {
		t.ScopeID = ""
	}
	if !t.PeriodStart.IsZero() {
		t.PeriodStart = NormalizePeriodStart(t.PeriodStart, loc)
	}
}

func (t *Target) Validate() error {
	if t.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if !t.Scope.Valid() {
		return ErrInvalidScope
	}
	if t.Scope.NeedsScopeID() && t.ScopeID == "" {
		return ErrScopeIDRequired
	}
	if t.Scope == ScopeWorkspace && t.ScopeID != "" {
		return ErrScopeIDForbidden
	}
	if t.PeriodStart.IsZero() {
		return ErrPeriodRequired
	}

	spec, found := attendance.Metric(t.MetricKey)
	if !found {
		return ErrUnknownMetric
	}
	if !spec.Targetable {
		return ErrMetricNotTargetab
	}
	if math.IsNaN(t.Value) || math.IsInf(t.Value, 0) || t.Value < 0 {
		return ErrInvalidValue
	}
	if spec.Kind == attendance.MetricKindMoney && t.Currency == "" {
		return ErrCurrencyRequired
	}
	if spec.Kind != attendance.MetricKindMoney && t.Currency != "" {
		return ErrCurrencyForbidden
	}
	return nil
}

func (t *Target) PeriodEnd() time.Time {
	return t.PeriodStart.AddDate(0, 1, 0)
}

func (t *Target) PeriodHasClosed(now time.Time) bool {
	return !now.Before(t.PeriodEnd())
}

type Repository interface {
	GetByID(workspaceID, id string) (*Target, error)
	ListForPeriod(workspaceID string, periodStart time.Time) ([]Target, error)
	ListRange(workspaceID string, from, to time.Time) ([]Target, error)
	Upsert(target Target) (*Target, error)
	Delete(workspaceID, id string) error
}
