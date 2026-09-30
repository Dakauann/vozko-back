package unofficial_whatsapp

import (
	"errors"
	"time"
)

var (
	ErrHistorySyncNotFound  = errors.New("unofficial whatsapp: no history import for this number")
	ErrHistorySyncActive    = errors.New("unofficial whatsapp: a history import is already running for this number")
	ErrHistorySyncLeaseLost = errors.New("unofficial whatsapp: history import was claimed by another worker")
	ErrHistorySyncCooldown  = errors.New("unofficial whatsapp: history was imported recently; try again later")
	ErrHistorySyncTrigger   = errors.New("unofficial whatsapp: unknown history import trigger")
	ErrHistoryImportOff     = errors.New("unofficial whatsapp: history import is switched off for this number")
)

type HistorySyncStatus string

const (
	HistorySyncQueued    HistorySyncStatus = "QUEUED"
	HistorySyncRunning   HistorySyncStatus = "RUNNING"
	HistorySyncCompleted HistorySyncStatus = "COMPLETED"
	HistorySyncPartial   HistorySyncStatus = "PARTIAL"
	HistorySyncFailed    HistorySyncStatus = "FAILED"
	HistorySyncCancelled HistorySyncStatus = "CANCELLED"
)

func (s HistorySyncStatus) Active() bool {
	return s == HistorySyncQueued || s == HistorySyncRunning
}

func ActiveHistorySyncStatuses() []HistorySyncStatus {
	return []HistorySyncStatus{HistorySyncQueued, HistorySyncRunning}
}

type HistorySyncTrigger string

const (
	HistorySyncTriggerConnect HistorySyncTrigger = "CONNECT"
	HistorySyncTriggerManual  HistorySyncTrigger = "MANUAL"
)

func (t HistorySyncTrigger) Valid() bool {
	return t == HistorySyncTriggerConnect || t == HistorySyncTriggerManual
}

type HistorySyncPolicy struct {
	Window         time.Duration
	PollWindow     time.Duration
	SettleAfter    time.Duration
	QuietPasses    int
	MaxMessages    int
	MaxAttempts    int
	PauseRetry     time.Duration
	PauseLimit     time.Duration
	ManualCooldown time.Duration
}

func DefaultHistorySyncPolicy() HistorySyncPolicy {
	return HistorySyncPolicy{
		Window:         30 * 24 * time.Hour,
		PollWindow:     2 * time.Hour,
		SettleAfter:    10 * time.Minute,
		QuietPasses:    2,
		MaxMessages:    100_000,
		MaxAttempts:    5,
		PauseRetry:     5 * time.Minute,
		PauseLimit:     24 * time.Hour,
		ManualCooldown: time.Hour,
	}
}

func (p HistorySyncPolicy) PollDelay(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < 10*time.Minute:
		return 30 * time.Second
	case elapsed < 30*time.Minute:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

func (p HistorySyncPolicy) retryDelay(attempt int) time.Duration {
	delay := time.Minute
	for i := 1; i < attempt && delay < 15*time.Minute; i++ {
		delay *= 2
	}
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	return delay
}

type HistorySync struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspaceId"`
	InstanceID  string             `json:"instanceId"`
	Status      HistorySyncStatus  `json:"status"`
	Trigger     HistorySyncTrigger `json:"trigger"`

	WindowFrom      time.Time  `json:"windowFrom"`
	OldestMessageAt *time.Time `json:"oldestMessageAt,omitempty"`
	NewestMessageAt *time.Time `json:"newestMessageAt,omitempty"`

	Passes            int `json:"passes"`
	QuietPasses       int `json:"quietPasses"`
	MessagesSeen      int `json:"messagesSeen"`
	MessagesImported  int `json:"messagesImported"`
	MessagesDuplicate int `json:"messagesDuplicate"`
	MessagesSkipped   int `json:"messagesSkipped"`
	MessagesFailed    int `json:"messagesFailed"`

	Attempts    int        `json:"attempts"`
	NextPollAt  time.Time  `json:"nextPollAt"`
	PollUntil   time.Time  `json:"pollUntil"`
	PausedSince *time.Time `json:"pausedSince,omitempty"`

	LeaseOwner string     `json:"-"`
	LeaseUntil *time.Time `json:"-"`

	Reason    string `json:"reason,omitempty"`
	LastError string `json:"lastError,omitempty"`

	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type HistoryPass struct {
	Seen       int
	Imported   int
	Duplicate  int
	Skipped    int
	Failed     int
	Pages      int
	Oldest     *time.Time
	Newest     *time.Time
	CapReached bool
}

func NewHistorySync(instance *Instance, trigger HistorySyncTrigger, now time.Time, policy HistorySyncPolicy) (*HistorySync, error) {
	if instance == nil || instance.ID == "" {
		return nil, ErrInstanceNotFound
	}
	if instance.WorkspaceID == "" {
		return nil, ErrWorkspaceIDRequired
	}
	if !trigger.Valid() {
		return nil, ErrHistorySyncTrigger
	}
	return &HistorySync{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		Status:      HistorySyncQueued,
		Trigger:     trigger,
		WindowFrom:  now.Add(-policy.Window),
		NextPollAt:  now,
		PollUntil:   now.Add(policy.PollWindow),
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (p *HistoryPass) Add(other HistoryPass) {
	p.Seen += other.Seen
	p.Imported += other.Imported
	p.Duplicate += other.Duplicate
	p.Skipped += other.Skipped
	p.Failed += other.Failed
	p.Pages += other.Pages
	p.Oldest = earliest(p.Oldest, other.Oldest)
	p.Newest = latest(p.Newest, other.Newest)
	p.CapReached = p.CapReached || other.CapReached
}

func (s *HistorySync) absorb(pass HistoryPass) {
	s.MessagesSeen += pass.Seen
	s.MessagesImported += pass.Imported
	s.MessagesDuplicate += pass.Duplicate
	s.MessagesSkipped += pass.Skipped
	s.MessagesFailed += pass.Failed
	s.OldestMessageAt = earliest(s.OldestMessageAt, pass.Oldest)
	s.NewestMessageAt = latest(s.NewestMessageAt, pass.Newest)
}

func (s *HistorySync) FailPass(pass HistoryPass, reason string, retryable bool, now time.Time, policy HistorySyncPolicy) {
	s.absorb(pass)
	s.FailAttempt(reason, retryable, now, policy)
}

func (s *HistorySync) CompletePass(pass HistoryPass, now time.Time, policy HistorySyncPolicy) {
	s.Passes++
	s.absorb(pass)
	s.Attempts = 0
	s.LastError = ""
	s.PausedSince = nil
	s.UpdatedAt = now

	if pass.Imported == 0 && pass.Failed == 0 {
		s.QuietPasses++
	} else {
		s.QuietPasses = 0
	}

	elapsed := now.Sub(s.startedOrCreated())
	switch {
	case pass.CapReached || s.MessagesImported >= policy.MaxMessages:
		s.finish(HistorySyncPartial, "limite de mensagens por importação atingido", now)
	case !now.Before(s.PollUntil):
		s.finish(HistorySyncCompleted, "", now)
	case s.QuietPasses >= policy.QuietPasses && elapsed >= policy.SettleAfter:
		s.finish(HistorySyncCompleted, "", now)
	default:
		s.Status = HistorySyncRunning
		s.NextPollAt = now.Add(policy.PollDelay(elapsed))
	}
}

func (s *HistorySync) FailAttempt(reason string, retryable bool, now time.Time, policy HistorySyncPolicy) {
	s.Attempts++
	s.LastError = reason
	s.UpdatedAt = now
	if !retryable || s.Attempts >= policy.MaxAttempts {
		s.finish(HistorySyncFailed, reason, now)
		return
	}
	s.Status = HistorySyncRunning
	s.NextPollAt = now.Add(policy.retryDelay(s.Attempts))
}

func (s *HistorySync) Pause(now time.Time, policy HistorySyncPolicy) {
	s.UpdatedAt = now
	if s.PausedSince == nil {
		paused := now
		s.PausedSince = &paused
	}
	if now.Sub(*s.PausedSince) >= policy.PauseLimit {
		s.finish(HistorySyncPartial, "o número ficou desconectado antes de concluir a importação", now)
		return
	}
	s.NextPollAt = now.Add(policy.PauseRetry)
}

func (s *HistorySync) Resume(now time.Time, policy HistorySyncPolicy) {
	s.PausedSince = nil
	s.NextPollAt = now
	if until := now.Add(policy.PollWindow); until.After(s.PollUntil) {
		s.PollUntil = until
	}
	s.UpdatedAt = now
}

func (s *HistorySync) Stop(status HistorySyncStatus, reason string, now time.Time) {
	s.finish(status, reason, now)
}

func (s *HistorySync) AllowsManualRetryAt(now time.Time, policy HistorySyncPolicy) bool {
	if s.Status.Active() {
		return true
	}
	if s.FinishedAt == nil {
		return true
	}
	return !now.Before(s.FinishedAt.Add(policy.ManualCooldown))
}

func (s *HistorySync) finish(status HistorySyncStatus, reason string, now time.Time) {
	finished := now
	s.Status = status
	s.Reason = reason
	s.FinishedAt = &finished
	s.UpdatedAt = now
}

func (s *HistorySync) startedOrCreated() time.Time {
	if s.StartedAt != nil {
		return *s.StartedAt
	}
	return s.CreatedAt
}

func earliest(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.Before(*current) {
		value := *candidate
		return &value
	}
	return current
}

func latest(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.After(*current) {
		value := *candidate
		return &value
	}
	return current
}
