package label_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/label"
)

type labelProviderService struct {
	repo label.Repository
}

func NewLabelProviderService(repo label.Repository) conversation.LabelProvider {
	return &labelProviderService{repo: repo}
}

func (s *labelProviderService) GetEntryLabels(entryID, entryType, workspaceID string) ([]conversation.InboxEntryLabel, error) {
	entryLabels, err := s.repo.GetEntryLabels(entryID, entryType, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]conversation.InboxEntryLabel, len(entryLabels))
	for i, el := range entryLabels {
		result[i] = conversation.InboxEntryLabel{
			LabelID: el.LabelID,
			Name:    el.LabelName,
			Color:   el.LabelColor,
		}
	}
	return result, nil
}

func (s *labelProviderService) GetBatchEntryLabels(entryIDs []string, entryType, workspaceID string) (map[string][]conversation.InboxEntryLabel, error) {
	batchLabels, err := s.repo.GetBatchEntryLabels(entryIDs, entryType, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]conversation.InboxEntryLabel, len(batchLabels))
	for entryID, labels := range batchLabels {
		mapped := make([]conversation.InboxEntryLabel, len(labels))
		for i, el := range labels {
			mapped[i] = conversation.InboxEntryLabel{
				LabelID: el.LabelID,
				Name:    el.LabelName,
				Color:   el.LabelColor,
			}
		}
		result[entryID] = mapped
	}
	return result, nil
}

func (s *labelProviderService) GetEntriesByLabelID(labelID, workspaceID string) ([]string, error) {
	entryLabels, err := s.repo.GetEntriesByLabel(labelID, workspaceID)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(entryLabels))
	for i, el := range entryLabels {
		ids[i] = el.EntryID
	}
	return ids, nil
}

func (s *labelProviderService) GetAvailableLabels(workspaceID string) ([]conversation.InboxEntryLabel, error) {
	labels, err := s.repo.ListByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]conversation.InboxEntryLabel, len(labels))
	for i, l := range labels {
		result[i] = conversation.InboxEntryLabel{
			LabelID: l.ID,
			Name:    l.Name,
			Color:   l.Color,
		}
	}
	return result, nil
}
