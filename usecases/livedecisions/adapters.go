package livedecisions_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/actor"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
	"vozko/domain/stage"
	stage_usecase "vozko/usecases/stage"
)

type MessageLister interface {
	ListByEntryPaginated(input conversation.ListMessagesInput) ([]*conversation.Message, error)
}

type StageLookup interface {
	GetEntryStage(entryID, entryType, workspaceID string) (*stage.EntryStage, error)
	FindByID(id string) (*stage.Stage, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
	ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error)
}

type CampaignLookup interface {
	GetEntryCampaignID(entryID, entryType string) (string, error)
}

type ConversationSnapshots struct {
	Messages  MessageLister
	Stages    StageLookup
	Campaigns CampaignLookup
}

func (c ConversationSnapshots) Snapshot(_ context.Context, trigger Trigger, withStages bool) (ld.Snapshot, error) {
	messages, err := c.Messages.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   trigger.EntryID,
		EntryType: trigger.EntryType,
		Limit:     ld.MaxTurns,
	})
	if err != nil {
		return ld.Snapshot{}, err
	}
	snapshot := ld.Snapshot{
		WorkspaceID: trigger.WorkspaceID,
		EntryID:     trigger.EntryID,
		EntryType:   string(trigger.EntryType),
		Turns:       turnsOldestFirst(messages),
	}
	if withStages {
		snapshot.CurrentStageID, snapshot.Stages = c.stages(trigger)
	}
	return snapshot, nil
}

func turnsOldestFirst(newestFirst []*conversation.Message) []ld.Turn {
	turns := make([]ld.Turn, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		m := newestFirst[i]
		if m == nil || !m.Transcribable() {
			continue
		}
		role := ld.RoleAgent
		if m.FromCustomer() {
			role = ld.RoleCustomer
		}
		turns = append(turns, ld.Turn{Role: role, Text: m.Text, At: m.CreatedAt})
	}
	return turns
}

func (c ConversationSnapshots) stages(trigger Trigger) (string, []ld.StageOption) {
	placement, err := c.placement(trigger)
	if err != nil {
		log.Printf("[live-decisions] locating the funnel of entry %s failed, asking no stage: %v", trigger.EntryID, err)
		return "", nil
	}
	stages, err := stage_usecase.NewEntryFunnel(c.Stages).Stages(trigger.WorkspaceID, placement)
	if err != nil {
		log.Printf("[live-decisions] reading the funnel of entry %s failed, asking no stage: %v", trigger.EntryID, err)
		return "", nil
	}
	options := make([]ld.StageOption, 0, len(stages))
	for _, s := range stages {
		if s != nil {
			options = append(options, ld.StageOption{ID: s.ID, Name: s.Name, Description: s.Description})
		}
	}
	return placement.CurrentStageID, options
}

func (c ConversationSnapshots) placement(trigger Trigger) (stage_usecase.Placement, error) {
	entryType := string(trigger.EntryType)
	placement := stage_usecase.Placement{CampaignType: entryType}
	current, err := c.Stages.GetEntryStage(trigger.EntryID, entryType, trigger.WorkspaceID)
	if err != nil {
		return placement, err
	}
	if current != nil && current.StageID != "" {
		placement.CurrentStageID = current.StageID
		return placement, nil
	}
	if c.Campaigns == nil {
		return placement, errors.New("no campaign lookup to find the funnel of a conversation without a stage")
	}
	placement.CampaignID, err = c.Campaigns.GetEntryCampaignID(trigger.EntryID, entryType)
	return placement, err
}

type StageAssigner interface {
	Execute(workspaceID string, input stage.AssignEntryStageInput) (*stage.EntryStage, error)
}

type StageBroadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
}

type StageEffects struct {
	Assign StageAssigner
	Hub    StageBroadcaster
}

func (s StageEffects) MoveStage(_ context.Context, workspaceID, entryID string, entryType shared.EntryType, stageID string, certainty float64) error {
	if _, err := s.Assign.Execute(workspaceID, stage.AssignEntryStageInput{
		StageID:    stageID,
		EntryID:    entryID,
		EntryType:  string(entryType),
		ActorID:    actor.PlatformAI,
		Confidence: certainty,
	}); err != nil {
		return err
	}
	go s.Hub.BroadcastStageUpdate(workspaceID, entryID, string(entryType))
	return nil
}

type HourlyLimiter struct {
	State cache.SharedState
	Max   int64
}

func (l HourlyLimiter) AllowDecision(workspaceID, entryID string) bool {
	count, err := l.State.IncrWithTTL("live:decide:rate:"+workspaceID+":"+entryID, time.Hour)
	if err != nil {
		log.Printf("[live-decisions] rate counter for entry %s unavailable, refusing: %v", entryID, err)
		return false
	}
	return count <= l.Max
}

type InboxReads struct {
	Reads ld.ReadStore
}

func (i InboxReads) LiveReadViews(workspaceID string, entryIDs []string, entryType string) (map[string]*ld.LiveReadView, error) {
	refs := make([]ld.EntryRef, 0, len(entryIDs))
	for _, id := range entryIDs {
		refs = append(refs, ld.EntryRef{EntryID: id, EntryType: entryType})
	}
	reads, err := i.Reads.ForEntries(context.Background(), workspaceID, refs)
	if err != nil {
		return nil, err
	}
	views := make(map[string]*ld.LiveReadView, len(reads))
	for ref, read := range reads {
		if view := read.View(); view != nil {
			views[ref.EntryID] = view
		}
	}
	return views, nil
}
