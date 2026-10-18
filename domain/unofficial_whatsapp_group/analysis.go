package unofficial_whatsapp_group

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrAnalysisNotFound       = errors.New("analysis not found")
	ErrAnalysisNameRequired   = errors.New("an analysis name is required")
	ErrAnalysisPromptRequired = errors.New("a monitoring prompt is required")
	ErrAnalysisGroupRequired  = errors.New("an analysis needs a group to monitor")
	ErrMessagesPerCycleRange  = errors.New("messages per cycle must be between 1 and 1000")
	ErrScheduleInvalid        = errors.New("the schedule must be a time between 00:00 and 23:59")
	ErrTriggerInvalid         = errors.New("trigger must be message_count or scheduled")
	ErrAnalysisStatusInvalid  = errors.New("status must be active, paused or archived")
	ErrPromptTooLong          = errors.New("the monitoring prompt exceeds 8000 characters")
	ErrTrackedFieldKey        = errors.New("a tracked field key uses lowercase letters, digits and underscores, starting with a letter")
	ErrTrackedFieldDuplicate  = errors.New("tracked field keys must be unique")
	ErrTrackedFieldType       = errors.New("a tracked field type must be text, number, percentage or boolean")
	ErrTrackedFieldsTooMany   = errors.New("an analysis tracks at most 20 fields")
	ErrAnalysisBusy           = errors.New("this analysis is already running")
	ErrNothingToAnalyze       = errors.New("there are no new messages to analyze")
)

const (
	DefaultMessagesPerCycle = 50
	MaxMessagesPerCycle     = 1000
	MaxPromptRunes          = 8000
	MaxTrackedFields        = 20
	MaxTrackedFieldKeyRunes = 40
	ArrivalGrace            = time.Minute
	MaxConsecutiveFailures  = 5
	FirstFailureBackoff     = 15 * time.Minute
)

type PausedReason string

const PausedAfterFailures PausedReason = "repeated_failures"

type Cursor struct {
	At time.Time
	ID string
}

type AnalysisStatus string

const (
	AnalysisActive   AnalysisStatus = "active"
	AnalysisPaused   AnalysisStatus = "paused"
	AnalysisArchived AnalysisStatus = "archived"
)

func (s AnalysisStatus) Valid() bool {
	return s == AnalysisActive || s == AnalysisPaused || s == AnalysisArchived
}

type Trigger string

const (
	TriggerMessageCount Trigger = "message_count"
	TriggerScheduled    Trigger = "scheduled"
)

func (t Trigger) Valid() bool {
	return t == TriggerMessageCount || t == TriggerScheduled
}

type FieldType string

const (
	FieldText       FieldType = "text"
	FieldNumber     FieldType = "number"
	FieldPercentage FieldType = "percentage"
	FieldBoolean    FieldType = "boolean"
)

func (f FieldType) Valid() bool {
	switch f {
	case FieldText, FieldNumber, FieldPercentage, FieldBoolean:
		return true
	}
	return false
}

type TrackedField struct {
	Key  string    `json:"key"`
	Type FieldType `json:"type"`
}

var trackedFieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func ValidateTrackedFields(fields []TrackedField) error {
	if len(fields) > MaxTrackedFields {
		return ErrTrackedFieldsTooMany
	}
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if !trackedFieldKeyPattern.MatchString(f.Key) || len([]rune(f.Key)) > MaxTrackedFieldKeyRunes {
			return fmt.Errorf("%w: %q", ErrTrackedFieldKey, f.Key)
		}
		if !f.Type.Valid() {
			return fmt.Errorf("%w: %q", ErrTrackedFieldType, f.Type)
		}
		if _, dup := seen[f.Key]; dup {
			return fmt.Errorf("%w: %q", ErrTrackedFieldDuplicate, f.Key)
		}
		seen[f.Key] = struct{}{}
	}
	return nil
}

type Analysis struct {
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspaceId"`
	GroupID          string         `json:"groupId"`
	Name             string         `json:"name"`
	Prompt           string         `json:"prompt"`
	Status           AnalysisStatus `json:"status"`
	Trigger          Trigger        `json:"trigger"`
	MessagesPerCycle int            `json:"messagesPerCycle"`
	ScheduleHour     int            `json:"scheduleHour"`
	ScheduleMinute   int            `json:"scheduleMinute"`
	TrackedFields    []TrackedField `json:"trackedFields"`
	PendingMessages  int            `json:"pendingMessages"`
	AnalyzedUntil    *time.Time     `json:"analyzedUntil,omitempty"`
	AnalyzedUntilID  string         `json:"-"`
	Failures         int            `json:"failures"`
	PausedReason     PausedReason   `json:"pausedReason,omitempty"`
	LastRunAt        *time.Time     `json:"lastRunAt,omitempty"`
	NextRunAt        *time.Time     `json:"nextRunAt,omitempty"`
	CreatedByID      string         `json:"createdById,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

func (a *Analysis) Normalize() {
	a.Name = strings.TrimSpace(a.Name)
	a.Prompt = strings.TrimSpace(a.Prompt)
	a.GroupID = strings.TrimSpace(a.GroupID)
	if a.Status == "" {
		a.Status = AnalysisActive
	}
	if a.Trigger == "" {
		a.Trigger = TriggerMessageCount
	}
	if a.MessagesPerCycle == 0 {
		a.MessagesPerCycle = DefaultMessagesPerCycle
	}
	if a.TrackedFields == nil {
		a.TrackedFields = []TrackedField{}
	}
	for i := range a.TrackedFields {
		a.TrackedFields[i].Key = strings.TrimSpace(a.TrackedFields[i].Key)
	}
}

func (a Analysis) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if a.Name == "" {
		return ErrAnalysisNameRequired
	}
	if a.Prompt == "" {
		return ErrAnalysisPromptRequired
	}
	if len([]rune(a.Prompt)) > MaxPromptRunes {
		return ErrPromptTooLong
	}
	if a.GroupID == "" {
		return ErrAnalysisGroupRequired
	}
	if !a.Status.Valid() {
		return ErrAnalysisStatusInvalid
	}
	if !a.Trigger.Valid() {
		return ErrTriggerInvalid
	}
	if a.MessagesPerCycle < 1 || a.MessagesPerCycle > MaxMessagesPerCycle {
		return ErrMessagesPerCycleRange
	}
	if a.ScheduleHour < 0 || a.ScheduleHour > 23 || a.ScheduleMinute < 0 || a.ScheduleMinute > 59 {
		return ErrScheduleInvalid
	}
	return ValidateTrackedFields(a.TrackedFields)
}

func (a Analysis) scheduleOn(day time.Time, loc *time.Location) time.Time {
	local := day.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), a.ScheduleHour, a.ScheduleMinute, 0, 0, loc)
}

func (a Analysis) Cursor() Cursor {
	if a.AnalyzedUntil == nil {
		return Cursor{At: a.CreatedAt}
	}
	return Cursor{At: *a.AnalyzedUntil, ID: a.AnalyzedUntilID}
}

func (a *Analysis) Advance(c Cursor) {
	at := c.At
	a.AnalyzedUntil, a.AnalyzedUntilID = &at, c.ID
}

func (a *Analysis) RecordFailure(now time.Time) {
	a.Failures++
	if a.Failures >= MaxConsecutiveFailures {
		a.Status, a.PausedReason, a.NextRunAt = AnalysisPaused, PausedAfterFailures, nil
		return
	}
	retry := now.Add(FirstFailureBackoff << (a.Failures - 1))
	a.NextRunAt = &retry
}

func (a *Analysis) RecordSuccess() {
	a.Failures, a.PausedReason = 0, ""
}

func (a *Analysis) SetStatus(status AnalysisStatus) {
	if status == AnalysisActive && a.Status != AnalysisActive {
		a.Failures, a.PausedReason = 0, ""
	}
	a.Status = status
}

func (a Analysis) RunKind() RunKind {
	if a.Trigger == TriggerScheduled {
		return RunDaily
	}
	return RunCycle
}

func (a Analysis) NextRun(now time.Time, loc *time.Location) time.Time {
	today := a.scheduleOn(now, loc)
	if now.Before(today) {
		return today
	}
	return a.scheduleOn(now.In(loc).AddDate(0, 0, 1), loc)
}

func SettledLines(lines []TranscriptLine, now time.Time) ([]TranscriptLine, bool) {
	until := now.Add(-ArrivalGrace)
	for i, line := range lines {
		if line.At.After(until) {
			return lines[:i], true
		}
	}
	return lines, false
}
