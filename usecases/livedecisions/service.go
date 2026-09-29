package livedecisions_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

var (
	ErrInsufficientFunds = errors.New("live decisions: balance below the AI floor")
	ErrRateLimited       = errors.New("live decisions: too many decisions for this conversation")
)

const FailureStale = "stale"

type Trigger struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
	Features    ld.Features
}

type Subjects interface {
	Subject(ctx context.Context, ref ld.EntryRef) (Trigger, bool, error)
}

type SnapshotReader interface {
	Snapshot(ctx context.Context, trigger Trigger, withStages bool) (ld.Snapshot, error)
}

type StageMover interface {
	MoveStage(ctx context.Context, workspaceID, entryID string, entryType shared.EntryType, stageID string, certainty float64) error
}

type LiveBroadcaster interface {
	LiveReadChanged(read ld.LiveRead)
}

type FundsGuard interface {
	Allow(workspaceID string) bool
}

type DecisionLimiter interface {
	AllowDecision(workspaceID, entryID string) bool
}

type Deps struct {
	Subjects  Subjects
	Model     decision.Model
	Policy    decision.Policy
	Snapshots SnapshotReader
	Reads     ld.ReadStore
	Log       ld.Log
	Stages    StageMover
	Live      LiveBroadcaster
	Funds     FundsGuard
	Limiter   DecisionLimiter
	Clock     func() time.Time
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.Policy == nil {
		deps.Policy = decision.DefaultPolicy()
	}
	return &Service{deps: deps}
}

func (s *Service) Acts(context.Context, string) bool {
	return s.wired()
}

func (s *Service) wired() bool {
	return s != nil && s.deps.Model != nil && s.deps.Subjects != nil
}

func (s *Service) Decide(ctx context.Context, trigger Trigger) (ld.Outcome, error) {
	if !s.Acts(ctx, trigger.WorkspaceID) {
		return ld.Outcome{}, decision.ErrDisabled
	}
	if !trigger.Features.Any() {
		return ld.Outcome{}, nil
	}
	if !s.deps.Limiter.AllowDecision(trigger.WorkspaceID, trigger.EntryID) {
		return ld.Outcome{}, ErrRateLimited
	}
	if !s.deps.Funds.Allow(trigger.WorkspaceID) {
		return ld.Outcome{}, ErrInsufficientFunds
	}
	snapshot, err := s.deps.Snapshots.Snapshot(ctx, trigger, trigger.Features.Staging)
	if err != nil {
		return ld.Outcome{}, fmt.Errorf("live decisions: reading the conversation: %w", err)
	}
	questions := ld.Questions(snapshot, trigger.Features)
	if len(questions) == 0 {
		return ld.Outcome{}, nil
	}

	record := ld.Record{
		WorkspaceID: trigger.WorkspaceID,
		EntryID:     trigger.EntryID,
		EntryType:   string(trigger.EntryType),
		Purpose:     ld.PurposeLive,
	}
	result, err := s.decide(ctx, &record, decision.Request{
		WorkspaceID: trigger.WorkspaceID,
		Purpose:     ld.PurposeLive,
		ReferenceID: trigger.EntryID,
		State:       snapshot.State(),
		Questions:   questions,
	})
	if err != nil {
		return ld.Outcome{}, err
	}

	outcome, err := ld.Interpret(snapshot, trigger.Features, result, s.deps.Policy)
	if err != nil {
		s.logFailure(ctx, record, err)
		return ld.Outcome{}, err
	}
	if read, ok := ld.NewLiveRead(snapshot, outcome, s.deps.Clock()); ok {
		saved, err := s.deps.Reads.Save(ctx, read)
		if err != nil {
			s.logFailure(ctx, record, fmt.Errorf("saving the live read: %w", err))
			return ld.Outcome{}, err
		}
		if !saved {
			record.Failure = FailureStale
			s.append(ctx, record)
			return ld.Outcome{}, nil
		}
		if read.HasLabels() {
			s.deps.Live.LiveReadChanged(read)
		}
	}

	applied := s.apply(ctx, trigger, outcome)
	record.Effects = applied.Effects()
	s.append(ctx, record)
	return applied, nil
}

func (s *Service) DecideLater(ctx context.Context, ref ld.EntryRef) {
	trigger, found, err := s.deps.Subjects.Subject(ctx, ref)
	if err != nil {
		log.Printf("[live-decisions] reading the settings of entry %s failed, deciding nothing: %v", ref.EntryID, err)
		return
	}
	if !found {
		return
	}
	if _, err := s.Decide(ctx, trigger); err != nil && !errors.Is(err, decision.ErrDisabled) {
		log.Printf("[live-decisions] deciding entry %s failed: %v", ref.EntryID, err)
	}
}

func (s *Service) apply(ctx context.Context, trigger Trigger, outcome ld.Outcome) ld.Outcome {
	applied := outcome
	if outcome.MoveStageTo != "" {
		if err := s.deps.Stages.MoveStage(ctx, trigger.WorkspaceID, trigger.EntryID, trigger.EntryType, outcome.MoveStageTo, outcome.MoveCertainty); err != nil {
			log.Printf("[live-decisions] moving entry %s to stage %s failed: %v", trigger.EntryID, outcome.MoveStageTo, err)
			applied.MoveStageTo = ""
		}
	}
	return applied
}

func (s *Service) logFailure(ctx context.Context, record ld.Record, err error) {
	record.Failure = err.Error()
	s.append(ctx, record)
}

func (s *Service) append(ctx context.Context, record ld.Record) {
	record.CreatedAt = s.deps.Clock()
	if err := s.deps.Log.Append(ctx, record); err != nil {
		log.Printf("[live-decisions] logging the decision for entry %s failed: %v", record.EntryID, err)
	}
}
