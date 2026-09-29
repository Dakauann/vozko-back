package stage_usecase

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/stage"
)

var ErrStageOutsideWorkspace = errors.New("stage: the conversation's stage belongs to another workspace")

type FunnelLookup interface {
	FindByID(id string) (*stage.Stage, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
	ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error)
}

type Placement struct {
	CurrentStageID string
	CampaignID     string
	CampaignType   string
}

type home struct {
	pipelineID   string
	campaignID   string
	campaignType string
}

type EntryFunnel struct {
	lookup     FunnelLookup
	homes      map[string]home
	byPipeline map[string][]*stage.Stage
	byCampaign map[string][]*stage.Stage
}

func NewEntryFunnel(lookup FunnelLookup) *EntryFunnel {
	return &EntryFunnel{
		lookup:     lookup,
		homes:      map[string]home{},
		byPipeline: map[string][]*stage.Stage{},
		byCampaign: map[string][]*stage.Stage{},
	}
}

func (f *EntryFunnel) Stages(workspaceID string, p Placement) ([]*stage.Stage, error) {
	current, err := f.homeOf(workspaceID, strings.TrimSpace(p.CurrentStageID))
	if err != nil {
		return nil, err
	}
	switch {
	case current.pipelineID != "":
		return f.pipelineStages(workspaceID, current.pipelineID)
	case current.campaignID != "":
		return f.campaignStages(workspaceID, current.campaignID, current.campaignType)
	}
	return f.campaignStages(workspaceID, strings.TrimSpace(p.CampaignID), strings.TrimSpace(p.CampaignType))
}

func (f *EntryFunnel) homeOf(workspaceID, stageID string) (home, error) {
	if stageID == "" {
		return home{}, nil
	}
	if found, ok := f.homes[stageID]; ok {
		return found, nil
	}
	current, err := f.lookup.FindByID(stageID)
	if err != nil {
		return home{}, fmt.Errorf("stage: reading the conversation's stage %s: %w", stageID, err)
	}
	var found home
	if current != nil {
		if current.WorkspaceID != workspaceID {
			return home{}, ErrStageOutsideWorkspace
		}
		found = home{
			pipelineID:   strings.TrimSpace(current.PipelineID),
			campaignID:   strings.TrimSpace(current.CampaignID),
			campaignType: strings.TrimSpace(current.CampaignType),
		}
	}
	f.homes[stageID] = found
	return found, nil
}

func (f *EntryFunnel) pipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if stages, ok := f.byPipeline[pipelineID]; ok {
		return stages, nil
	}
	stages, err := f.lookup.ListByPipeline(workspaceID, pipelineID)
	if err != nil {
		return nil, err
	}
	f.byPipeline[pipelineID] = stages
	return stages, nil
}

func (f *EntryFunnel) campaignStages(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	key := campaignType + ":" + campaignID
	if stages, ok := f.byCampaign[key]; ok {
		return stages, nil
	}
	stages, err := f.lookup.ListByCampaign(workspaceID, campaignID, campaignType)
	if err != nil {
		return nil, err
	}
	f.byCampaign[key] = stages
	return stages, nil
}

type EntryStageReader interface {
	FunnelLookup
	GetEntryStage(entryID, entryType, workspaceID string) (*stage.EntryStage, error)
}

type EntryRef struct {
	EntryID      string
	EntryType    string
	CampaignID   string
	CampaignType string
}

type EntryStages struct {
	Current *stage.EntryStage
	Stages  []*stage.Stage
}

func StagesForEntry(reader EntryStageReader, workspaceID string, ref EntryRef) (EntryStages, error) {
	var out EntryStages
	placement := Placement{CampaignID: ref.CampaignID, CampaignType: ref.CampaignType}
	if ref.EntryID != "" && ref.EntryType != "" {
		current, err := reader.GetEntryStage(ref.EntryID, ref.EntryType, workspaceID)
		if err != nil {
			return out, fmt.Errorf("stage: reading the stage of entry %s: %w", ref.EntryID, err)
		}
		out.Current = current
	}
	if out.Current != nil {
		placement.CurrentStageID = out.Current.StageID
	}
	stages, err := NewEntryFunnel(reader).Stages(workspaceID, placement)
	if err != nil {
		return EntryStages{}, err
	}
	out.Stages = stages
	return out, nil
}
