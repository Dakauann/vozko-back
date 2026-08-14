// Package lead_memory_usecase orchestrates the per-lead memory.
//
// Every writer, the AI tool and the operator HTTP handlers alike, goes through these
// use cases, so caps, dedup, attribution, and timeline events cannot diverge
// between actors. That single-write-model property is the feature's core
// invariant; do not add a second write path around it.
package lead_memory_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/shared"
)

const fullUUIDLen = 36

type createUseCase struct {
	repo   leadmemory.Repository
	events leadmemory.TimelineLogger
}

// NewCreateUseCase builds the create path. events is optional (best-effort
// telemetry); repo is not.
func NewCreateUseCase(repo leadmemory.Repository, events leadmemory.TimelineLogger) (leadmemory.CreateUseCase, error) {
	if repo == nil {
		return nil, fmt.Errorf("lead memory create use case: missing repository")
	}
	return &createUseCase{repo: repo, events: events}, nil
}

// Execute remembers one fact. A create whose normalized content already exists
// for the lead is idempotent success: the existing memory comes back with
// Deduplicated set, nothing is written, no event is emitted. That is what lets
// a looping model or a double-submitting client hammer this safely.
func (uc *createUseCase) Execute(_ context.Context, in leadmemory.CreateInput) (*leadmemory.CreateResult, error) {
	m := &leadmemory.LeadMemory{
		WorkspaceID:     in.WorkspaceID,
		LeadID:          in.LeadID,
		Category:        in.Category,
		Content:         in.Content,
		ActorKind:       in.Actor.Kind,
		ActorID:         in.Actor.ID,
		SourceEntryID:   in.SourceEntryID,
		SourceEntryType: in.SourceEntryType,
	}
	m.Normalize()
	if err := m.Validate(); err != nil {
		return nil, err
	}

	count, err := uc.repo.CountByLead(m.WorkspaceID, m.LeadID)
	if err != nil {
		return nil, err
	}
	if count >= leadmemory.MaxActiveMemoriesPerLead {
		return nil, leadmemory.ErrLimitReached
	}

	norm := leadmemory.NormalizeContent(m.Content)
	if existing, err := uc.repo.FindByNormalizedContent(m.WorkspaceID, m.LeadID, norm); err == nil {
		return &leadmemory.CreateResult{Memory: existing, Deduplicated: true}, nil
	} else if !errors.Is(err, leadmemory.ErrNotFound) {
		return nil, err
	}

	if err := uc.repo.Create(m); err != nil {
		if errors.Is(err, leadmemory.ErrDuplicate) {
			// Lost a race with an identical write. The unique index decided;
			// re-read the winner and report it as the deduplicated result.
			if existing, ferr := uc.repo.FindByNormalizedContent(m.WorkspaceID, m.LeadID, norm); ferr == nil {
				return &leadmemory.CreateResult{Memory: existing, Deduplicated: true}, nil
			}
		}
		return nil, err
	}

	logTimelineEvent(uc.events, ce.EventLeadMemoryCreated, m)
	return &leadmemory.CreateResult{Memory: m}, nil
}

type updateUseCase struct {
	repo   leadmemory.Repository
	events leadmemory.TimelineLogger
}

func NewUpdateUseCase(repo leadmemory.Repository, events leadmemory.TimelineLogger) (leadmemory.UpdateUseCase, error) {
	if repo == nil {
		return nil, fmt.Errorf("lead memory update use case: missing repository")
	}
	return &updateUseCase{repo: repo, events: events}, nil
}

// Execute edits content and/or category. The actor columns become the last
// writer: an operator correcting an AI memory owns the correction, and the
// original authorship stays on the created timeline event.
func (uc *updateUseCase) Execute(_ context.Context, in leadmemory.UpdateInput) (*leadmemory.LeadMemory, error) {
	m, err := resolveRef(uc.repo, in.WorkspaceID, in.LeadID, in.MemoryRef)
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(in.Content)
	if content == "" && in.Category == "" {
		// Nothing to change: idempotent no-op, no event.
		return m, nil
	}
	if content != "" {
		m.Content = content
	}
	if in.Category != "" {
		m.Category = in.Category
	}
	m.SourceEntryID = in.SourceEntryID
	m.SourceEntryType = in.SourceEntryType
	m.ActorKind = in.Actor.Kind
	m.ActorID = in.Actor.ID
	m.Normalize()
	if err := m.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(m); err != nil {
		return nil, err
	}
	logTimelineEvent(uc.events, ce.EventLeadMemoryUpdated, m)
	return m, nil
}

type deleteUseCase struct {
	repo   leadmemory.Repository
	events leadmemory.TimelineLogger
}

func NewDeleteUseCase(repo leadmemory.Repository, events leadmemory.TimelineLogger) (leadmemory.DeleteUseCase, error) {
	if repo == nil {
		return nil, fmt.Errorf("lead memory delete use case: missing repository")
	}
	return &deleteUseCase{repo: repo, events: events}, nil
}

func (uc *deleteUseCase) Execute(_ context.Context, in leadmemory.DeleteInput) error {
	m, err := resolveRef(uc.repo, in.WorkspaceID, in.LeadID, in.MemoryRef)
	if err != nil {
		return err
	}
	if err := uc.repo.SoftDelete(m.WorkspaceID, m.ID); err != nil {
		return err
	}
	m.SourceEntryID = in.SourceEntryID
	m.SourceEntryType = in.SourceEntryType
	m.ActorKind = in.Actor.Kind
	m.ActorID = in.Actor.ID
	m.Normalize()
	logTimelineEvent(uc.events, ce.EventLeadMemoryDeleted, m)
	return nil
}

type listUseCase struct {
	repo   leadmemory.Repository
	agents leadmemory.AgentNameFinder
	users  leadmemory.UserNameFinder
}

// NewListUseCase builds the read path. agents and users are optional: without
// them the listing still works and ActorLabel stays empty (the UI falls back
// to the actor kind).
func NewListUseCase(repo leadmemory.Repository, agents leadmemory.AgentNameFinder, users leadmemory.UserNameFinder) (leadmemory.ListUseCase, error) {
	if repo == nil {
		return nil, fmt.Errorf("lead memory list use case: missing repository")
	}
	return &listUseCase{repo: repo, agents: agents, users: users}, nil
}

func (uc *listUseCase) Execute(_ context.Context, in leadmemory.ListInput) (*leadmemory.ListResult, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, leadmemory.ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.LeadID) == "" {
		return nil, leadmemory.ErrLeadRequired
	}

	items, total, err := uc.repo.ListByLead(in.WorkspaceID, in.LeadID, in.Query)
	if err != nil {
		return nil, err
	}

	labels := uc.resolveActorLabels(items)
	views := make([]leadmemory.MemoryView, len(items))
	for i, m := range items {
		views[i] = leadmemory.MemoryView{LeadMemory: m, ActorLabel: labels[m.ActorID]}
	}
	return &leadmemory.ListResult{Items: views, Total: total}, nil
}

// resolveActorLabels maps actor ids to display names, best-effort: one batch
// query per kind, failures logged and swallowed. A missing label is a
// cosmetic degradation, never a reason to hide memories.
func (uc *listUseCase) resolveActorLabels(items []*leadmemory.LeadMemory) map[string]string {
	agentIDs := map[string]bool{}
	userIDs := map[string]bool{}
	for _, m := range items {
		switch m.ActorKind {
		case actor.KindAI:
			if id := actor.ParseAI(m.ActorID); id != "" {
				agentIDs[id] = true
			}
		case actor.KindHuman:
			if m.ActorID != "" {
				userIDs[m.ActorID] = true
			}
		}
	}

	labels := map[string]string{}
	if uc.agents != nil && len(agentIDs) > 0 {
		if found, err := uc.agents.FindByIDs(keys(agentIDs)); err != nil {
			log.Printf("[lead_memory] could not resolve agent names: %v", err)
		} else {
			for _, a := range found {
				labels[actor.FormatAI(a.ID)] = a.Name
			}
		}
	}
	if uc.users != nil && len(userIDs) > 0 {
		if found, err := uc.users.FindByIDs(keys(userIDs)); err != nil {
			log.Printf("[lead_memory] could not resolve user names: %v", err)
		} else {
			for _, u := range found {
				labels[u.ID] = u.Username
			}
		}
	}
	return labels
}

// resolveRef turns a memory reference, full UUID or the ≥8-char prefix the
// prompt block shows, into the owned row. A full id from another lead still
// resolves only when LeadID matches: the AI tool must never reach another
// lead's memory by guessing ids, and for the tool LeadID is always set.
func resolveRef(repo leadmemory.Repository, workspaceID, leadID, ref string) (*leadmemory.LeadMemory, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case len(ref) == fullUUIDLen:
		m, err := repo.FindByID(workspaceID, ref)
		if err != nil {
			return nil, err
		}
		if leadID != "" && m.LeadID != leadID {
			return nil, leadmemory.ErrNotFound
		}
		return m, nil
	case len(ref) >= leadmemory.MinIDPrefixLen && strings.TrimSpace(leadID) != "":
		return repo.FindByIDPrefix(workspaceID, leadID, ref)
	default:
		return nil, leadmemory.ErrNotFound
	}
}

// logTimelineEvent records the mutation on the conversation timeline when the
// write happened inside a conversation. Lead-page edits carry no entry and
// stay off the timeline; the row itself is their record.
func logTimelineEvent(events leadmemory.TimelineLogger, eventType ce.EventType, m *leadmemory.LeadMemory) {
	if events == nil || m.SourceEntryID == nil || m.SourceEntryType == nil {
		return
	}
	b := ce.New(m.WorkspaceID, *m.SourceEntryID, *m.SourceEntryType, eventType).
		WithChannel(shared.EntryType(*m.SourceEntryType).EventChannel()).
		WithCorrelation(m.ID).
		WithDetails(map[string]string{
			"memory_id": m.ID,
			"category":  string(m.Category),
		})
	switch m.ActorKind {
	case actor.KindAI:
		b = b.WithActorAI(actor.ParseAI(m.ActorID))
	case actor.KindHuman:
		b = b.WithActorHuman(m.ActorID)
	default:
		b = b.WithActorSystem()
	}
	events.ConversationEvent(b.Build())
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

var (
	_ leadmemory.CreateUseCase = (*createUseCase)(nil)
	_ leadmemory.UpdateUseCase = (*updateUseCase)(nil)
	_ leadmemory.DeleteUseCase = (*deleteUseCase)(nil)
	_ leadmemory.ListUseCase   = (*listUseCase)(nil)
)
