package livedecisions_usecase

import (
	"context"
	"log"

	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
)

func (s *Service) StageNeedsReview(ctx context.Context, target Trigger) bool {
	if !s.Acts(ctx, target.WorkspaceID) {
		return true
	}
	read, err := s.deps.Reads.Get(ctx, target.WorkspaceID, target.EntryID, string(target.EntryType))
	if err != nil {
		log.Printf("[live-decisions] reading the live stage of entry %s failed, leaving it to the LLM: %v", target.EntryID, err)
		return true
	}
	return read == nil || !read.StageSettled
}

func (s *Service) QuietGate(ctx context.Context, target Trigger, wantMemory, wantDeals bool, memory, deals string) ld.QuietGate {
	everything := ld.QuietGate{NeedsMemory: wantMemory, NeedsDeals: wantDeals}
	if (!wantMemory && !wantDeals) || !s.Acts(ctx, target.WorkspaceID) || !s.deps.Funds.Allow(target.WorkspaceID) {
		return everything
	}
	snapshot, err := s.deps.Snapshots.Snapshot(ctx, target, false)
	if err != nil {
		log.Printf("[live-decisions] quiet gate for entry %s could not read the conversation: %v", target.EntryID, err)
		return everything
	}
	state := snapshot.State()
	if wantMemory {
		state["memoria"] = memory
	}
	if wantDeals {
		state["oportunidades"] = deals
	}
	record := ld.Record{WorkspaceID: target.WorkspaceID, EntryID: target.EntryID, EntryType: string(target.EntryType), Purpose: ld.PurposeQuietGate}
	result, err := s.decide(ctx, &record, decision.Request{
		WorkspaceID: target.WorkspaceID,
		Purpose:     ld.PurposeQuietGate,
		ReferenceID: target.EntryID,
		State:       state,
		Questions:   ld.QuietQuestions(wantMemory, wantDeals),
	})
	if err != nil {
		return everything
	}
	gate := ld.InterpretQuiet(result, wantMemory, wantDeals, s.deps.Policy)
	record.Effects = gate.Effects(wantMemory, wantDeals)
	s.append(ctx, record)
	return gate
}

func (s *Service) decide(ctx context.Context, record *ld.Record, request decision.Request) (decision.Result, error) {
	started := s.deps.Clock()
	result, err := s.deps.Model.Decide(ctx, request)
	record.LatencyMillis = s.deps.Clock().Sub(started).Milliseconds()
	if err != nil {
		s.logFailure(ctx, *record, err)
		return decision.Result{}, err
	}
	record.Model, record.InputTokens, record.CostMicros, record.Answers = result.Model, result.InputTokens, result.CostMicros, result.Answers
	return result, nil
}
