package smartcampaign

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotSmart        = errors.New("smart campaign: the campaign has no wave policy")
	ErrNothingToClaim  = errors.New("smart campaign: no pending contact left to claim")
	ErrStillUnhealthy  = errors.New("smart campaign: a health signal still blocks sending")
	ErrPolicyLocked    = errors.New("smart campaign: the wave policy cannot change while the campaign is running")
	ErrCampaignBusy    = errors.New("smart campaign: another decision for this campaign is in progress")
	ErrHealthUnhandled = errors.New("smart campaign: health signal for an unknown template or number")
)

type Recipient struct {
	EntryID     string
	PhoneNumber string
}

type Hold struct {
	Decision Decision  `json:"decision"`
	At       time.Time `json:"at"`
}

type State struct {
	CampaignID       string    `json:"campaignId"`
	Hold             *Hold     `json:"hold,omitempty"`
	Waiting          *Decision `json:"waiting,omitempty"`
	ResumedAfterHold bool      `json:"resumedAfterHold"`
	UnreadSince      time.Time `json:"unreadSince,omitzero"`
	DueAt            time.Time `json:"dueAt,omitzero"`
}

func (s State) History(currentSize, slowStreak int) History {
	return History{CurrentSize: currentSize, SlowStreakAtFloor: slowStreak, ResumedAfterHold: s.ResumedAfterHold, UnreadSince: s.UnreadSince}
}

func (s *State) TrackHealthReads(unread bool, now time.Time) {
	if !unread {
		s.UnreadSince = time.Time{}
		return
	}
	if s.UnreadSince.IsZero() {
		s.UnreadSince = now
	}
}

func (s *State) Held(d Decision, at time.Time) {
	s.Hold = &Hold{Decision: d, At: at}
	s.Waiting = nil
	s.DueAt = time.Time{}
}

func (s *State) Resumed(now time.Time) {
	s.ResumedAfterHold = s.Hold != nil
	s.Hold = nil
	s.Waiting = nil
	s.UnreadSince = time.Time{}
	s.DueAt = now
}

func (s *State) WaveReleased(due time.Time) {
	s.ResumedAfterHold = false
	s.Waiting = nil
	s.DueAt = due
}

func (s *State) WaitFor(d Decision, due time.Time) {
	s.Waiting = &d
	s.DueAt = due
}

type PolicyRepository interface {
	FindPolicy(campaignID string) (*Policy, error)
	SavePolicy(campaignID string, policy Policy) error
	DeletePolicy(campaignID string) error
}

type StateRepository interface {
	FindState(campaignID string) (State, error)
	SaveState(state State) error
	DueCampaigns(now time.Time, limit int) ([]string, error)
	RunningUsing(templateID, phoneID string) ([]string, error)
}

type WaveRepository interface {
	CurrentWave(campaignID string) (*Wave, error)
	ListWaves(campaignID string) ([]Wave, error)
	ClaimWave(campaignID string, number, size int, at time.Time) (Wave, error)
	SaveWave(wave Wave) error
	ReturnToPool(campaignID string, number int) (int, error)
	ReleasedSince(campaignID string, since time.Time) (int, error)
	PendingInWave(campaignID string, number int) (int, error)
	WaveOutcome(campaignID string, number int) (Outcome, error)
	WaveRecipients(campaignID string, number int) ([]Recipient, error)
}

type HealthReader interface {
	Template(ctx context.Context, templateID string) (TemplateState, error)
	Phone(ctx context.Context, phoneID string, fresh bool) (PhoneState, error)
}
