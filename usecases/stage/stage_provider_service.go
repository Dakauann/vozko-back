package stage_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/stage"
)

type tagProviderService struct {
	repo stage.Repository
}

func NewStageProviderService(repo stage.Repository) conversation.StageProvider {
	return &tagProviderService{repo: repo}
}

func (s *tagProviderService) GetEntryStage(entryID, entryType, workspaceID string) (*conversation.InboxEntryStage, error) {
	et, err := s.repo.GetEntryStage(entryID, entryType, workspaceID)
	if err != nil {
		return nil, err
	}
	if et == nil {
		return nil, nil
	}
	return &conversation.InboxEntryStage{
		StageID: et.StageID,
		Name:    et.StageName,
		Color:   et.StageColor,
	}, nil
}

func (s *tagProviderService) GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*conversation.InboxEntryStage, error) {
	batchStages, err := s.repo.GetBatchEntryStages(entryIDs, entryType, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*conversation.InboxEntryStage, len(batchStages))
	for entryID, et := range batchStages {
		result[entryID] = &conversation.InboxEntryStage{
			StageID: et.StageID,
			Name:    et.StageName,
			Color:   et.StageColor,
		}
	}
	return result, nil
}

func (s *tagProviderService) GetEntriesByTagID(StageID, workspaceID string) ([]string, error) {
	entryTags, err := s.repo.GetEntriesByStage(StageID, workspaceID)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(entryTags))
	for i, et := range entryTags {
		ids[i] = et.EntryID
	}
	return ids, nil
}

func (s *tagProviderService) FindStageIDsByName(workspaceID, name string) ([]string, error) {
	ids, err := s.repo.FindIDsByName(workspaceID, name)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, stage.ErrTagNotFound
	}
	return ids, nil
}

func (s *tagProviderService) GetStageCountsForCampaign(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	return s.repo.GetStageCountsForCampaign(workspaceID, campaignID, entryType)
}

func (s *tagProviderService) GetStageCountsForWorkspace(workspaceID, entryType string) (map[string]int64, error) {
	return s.repo.GetStageCountsForWorkspace(workspaceID, entryType)
}

func (s *tagProviderService) GetAvailableStages(workspaceID string, placements []conversation.StagePlacement) ([][]conversation.InboxEntryStage, error) {
	funnel := NewEntryFunnel(s.repo)
	out := make([][]conversation.InboxEntryStage, len(placements))
	for i, p := range placements {
		stages, err := funnel.Stages(workspaceID, Placement{CurrentStageID: p.CurrentStageID, CampaignID: p.CampaignID, CampaignType: p.CampaignType})
		if err != nil {
			return nil, err
		}
		mapped := make([]conversation.InboxEntryStage, 0, len(stages))
		for _, st := range stages {
			if st != nil {
				mapped = append(mapped, conversation.InboxEntryStage{StageID: st.ID, Name: st.Name, Color: st.Color})
			}
		}
		out[i] = mapped
	}
	return out, nil
}
