package livedecision

import (
	"context"
	"time"

	"vozko/domain/audience"
	"vozko/domain/decision"
	"vozko/domain/shared"
)

type LiveRead struct {
	WorkspaceID       string
	EntryID           string
	EntryType         string
	Interest          audience.Interest
	Disposition       audience.Disposition
	Sentiment         shared.Sentiment
	Qualification     audience.Qualification
	NextAction        audience.NextAction
	Language          string
	AttendanceQuality int
	Certainty         map[string]float64
	StageSettled      bool
	DecidedThrough    time.Time
	DecidedAt         time.Time
}

func NewLiveRead(s Snapshot, outcome Outcome, now time.Time) (LiveRead, bool) {
	stageAsked := outcome.StageSettled || outcome.StageUncertain
	if outcome.Reading == nil && !stageAsked {
		return LiveRead{}, false
	}
	read := LiveRead{
		WorkspaceID:    s.WorkspaceID,
		EntryID:        s.EntryID,
		EntryType:      s.EntryType,
		StageSettled:   outcome.StageSettled,
		DecidedThrough: s.LastMessageAt(),
		DecidedAt:      now,
		Certainty:      map[string]float64{},
	}
	if outcome.Reading != nil {
		c := outcome.Reading.Classification
		read.Interest = c.Interest
		read.Disposition = c.Disposition
		read.Sentiment = c.Sentiment
		read.Qualification = c.Qualification
		read.NextAction = c.NextAction
		read.Language = c.Language
		read.AttendanceQuality = c.Quality.Score()
		read.Certainty = outcome.Reading.Certainty
	}
	return read, true
}

func (r LiveRead) HasLabels() bool { return r.Qualification != "" }

func (r LiveRead) Supersedes(previous LiveRead) bool {
	return !r.DecidedThrough.Before(previous.DecidedThrough)
}

const (
	EffectStageMoved       = "stage_moved"
	EffectStageUncertain   = "stage_uncertain"
	EffectMemoryReviewSkip = "memory_review_skipped"
	EffectDealReviewSkip   = "deal_review_skipped"
)

func (g QuietGate) Effects(wantMemory, wantDeals bool) []string {
	var effects []string
	if wantMemory && !g.NeedsMemory {
		effects = append(effects, EffectMemoryReviewSkip)
	}
	if wantDeals && !g.NeedsDeals {
		effects = append(effects, EffectDealReviewSkip)
	}
	return effects
}

func (o Outcome) Effects() []string {
	var effects []string
	if o.MoveStageTo != "" {
		effects = append(effects, EffectStageMoved)
	}
	if o.StageUncertain {
		effects = append(effects, EffectStageUncertain)
	}
	return effects
}

type Record struct {
	ID            string
	WorkspaceID   string
	EntryID       string
	EntryType     string
	Purpose       string
	Answers       map[string]decision.Answer
	Effects       []string
	Failure       string
	Model         string
	InputTokens   int
	CostMicros    int64
	LatencyMillis int64
	CreatedAt     time.Time
}

type Summary struct {
	WorkspaceID   string
	WorkspaceName string
	Decisions     int
	Failures      int
	CostMicros    int64
	AvgLatency    time.Duration
	Effects       map[string]int
}

type EntryRef struct {
	EntryID   string
	EntryType string
}

type ReadStore interface {
	Save(ctx context.Context, read LiveRead) (bool, error)
	Get(ctx context.Context, workspaceID, entryID, entryType string) (*LiveRead, error)
	ForEntries(ctx context.Context, workspaceID string, refs []EntryRef) (map[EntryRef]LiveRead, error)
}

type Log interface {
	Append(ctx context.Context, record Record) error
	Summarize(ctx context.Context, since time.Time) ([]Summary, error)
}

type LiveReadView struct {
	Interest          string             `json:"interest,omitempty"`
	Disposition       string             `json:"disposition,omitempty"`
	Sentiment         string             `json:"sentiment,omitempty"`
	Qualification     string             `json:"qualification,omitempty"`
	NextAction        string             `json:"nextAction,omitempty"`
	Language          string             `json:"language,omitempty"`
	AttendanceQuality int                `json:"attendanceQuality"`
	Certainty         map[string]float64 `json:"certainty,omitempty"`
	DecidedAt         time.Time          `json:"decidedAt"`
}

func (r LiveRead) View() *LiveReadView {
	if !r.HasLabels() {
		return nil
	}
	return &LiveReadView{
		Interest:          string(r.Interest),
		Disposition:       string(r.Disposition),
		Sentiment:         string(r.Sentiment),
		Qualification:     string(r.Qualification),
		NextAction:        string(r.NextAction),
		Language:          r.Language,
		AttendanceQuality: r.AttendanceQuality,
		Certainty:         r.Certainty,
		DecidedAt:         r.DecidedAt,
	}
}
