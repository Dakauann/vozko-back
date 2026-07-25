package conversation_usecase

import (
	"fmt"
	"log"

	conversation "vozko/domain/conversation"
	"vozko/domain/workspace"
)

type inboxService struct {
	historyProvider      conversation.HistoryProvider
	StageProvider        conversation.StageProvider
	labelProvider        conversation.LabelProvider
	workspaceResolver    conversation.CampaignWorkspaceResolver
	InitialStageAssigner conversation.InitialStageAssigner
	authorizer           conversation.ConversationAuthorizer
	templateSender       conversation.TemplateSender
	analysisProvider     conversation.AnalysisProvider
	statusProvider       conversation.ConversationStatusUpdater
}

func NewInboxService(
	historyProvider conversation.HistoryProvider,
	StageProvider conversation.StageProvider,
	labelProvider conversation.LabelProvider,
	workspaceResolver conversation.CampaignWorkspaceResolver,
	InitialStageAssigner conversation.InitialStageAssigner,
	authorizer conversation.ConversationAuthorizer,
	analysisProvider conversation.AnalysisProvider,
	statusProvider conversation.ConversationStatusUpdater,
) conversation.InboxService {
	return &inboxService{
		historyProvider:      historyProvider,
		StageProvider:        StageProvider,
		labelProvider:        labelProvider,
		workspaceResolver:    workspaceResolver,
		InitialStageAssigner: InitialStageAssigner,
		authorizer:           authorizer,
		analysisProvider:     analysisProvider,
		statusProvider:       statusProvider,
	}
}

func (s *inboxService) SetTemplateSender(sender conversation.TemplateSender) {
	s.templateSender = sender
}

func (s *inboxService) GetInboxPage(userID, campaignID, campaignType, campaignWorkspaceID string, page, pageSize int, isAdmin bool) ([]conversation.InboxEntry, int64, error) {
	if s.historyProvider == nil {
		return nil, 0, nil
	}

	return s.SearchInbox(userID, conversation.SearchInboxInput{
		CampaignID:     campaignID,
		CampaignType:   campaignType,
		WorkspaceID:    campaignWorkspaceID,
		AssignedUserID: userID,
		IsAdmin:        isAdmin,
		Page:           page,
		PageSize:       pageSize,
	})
}

func (s *inboxService) GetInboxPageByWorkspace(userID, workspaceID, entryTypeFilter, whatsappCampaignType string, page, pageSize int, isAdmin bool) ([]conversation.InboxEntry, int64, error) {
	if s.historyProvider == nil {
		return nil, 0, nil
	}

	return s.SearchInbox(userID, conversation.SearchInboxInput{
		CampaignType:         entryTypeFilter,
		WhatsAppCampaignType: whatsappCampaignType,
		WorkspaceID:          workspaceID,
		AssignedUserID:       userID,
		IsAdmin:              isAdmin,
		Page:                 page,
		PageSize:             pageSize,
	})
}

func (s *inboxService) SearchInbox(userID string, input conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	if s.historyProvider == nil {
		return nil, 0, nil
	}

	workspaceForOwnerCheck := input.WorkspaceID
	if workspaceForOwnerCheck == "" && input.CampaignID != "" && s.workspaceResolver != nil {
		workspaceForOwnerCheck, _ = s.workspaceResolver.GetCampaignWorkspaceID(input.CampaignID, input.CampaignType)
	}

	if input.WorkspaceID == "" {
		input.WorkspaceID = workspaceForOwnerCheck
	}
	if input.CampaignID != "" && input.CampaignType != "" && s.authorizer != nil {
		if !s.authorizer.CanAccessCampaign(userID, workspaceForOwnerCheck, input.CampaignID, input.CampaignType, input.IsAdmin) {
			return nil, 0, fmt.Errorf("unauthorized: you don't have access to this inbox")
		}
	}
	if s.authorizer != nil && input.WorkspaceID != "" {
		scope, allowed := s.authorizer.GetDepartmentScope(userID, input.WorkspaceID, input.IsAdmin)
		if !allowed {
			return nil, 0, fmt.Errorf("unauthorized: you don't have access to this inbox")
		}
		input.DepartmentIDs = scope.DepartmentIDs
		input.RestrictDepartments = scope.Restrict

		if input.SelectedDepartmentID != "" && (scope.Restrict || scope.WorkspaceHasDepartments) {
			if scope.Restrict {

				found := false
				for _, id := range scope.DepartmentIDs {
					if id == input.SelectedDepartmentID {
						found = true
						break
					}
				}
				if !found {
					return nil, 0, fmt.Errorf("unauthorized: you don't have access to this department")
				}
			}
			input.DepartmentIDs = []string{input.SelectedDepartmentID}
			input.RestrictDepartments = true
		}
	}
	if input.IsAdmin {
		input.AssignedUserID = ""
	} else if s.authorizer != nil && workspaceForOwnerCheck != "" && s.authorizer.IsWorkspaceOwnerOrAdmin(userID, workspaceForOwnerCheck) {
		input.AssignedUserID = ""
	} else if s.canViewOthers(userID, workspaceForOwnerCheck) {
		input.AssignedUserID = ""
	}

	if !input.IsAdmin && input.RestrictDepartments {
		input.AssigneeOverrideUserID = userID
	}

	var stageWorkspaceID string
	if input.WorkspaceID != "" {
		stageWorkspaceID = input.WorkspaceID
	} else if s.workspaceResolver != nil && input.CampaignID != "" {
		stageWorkspaceID, _ = s.workspaceResolver.GetCampaignWorkspaceID(input.CampaignID, input.CampaignType)
	}

	if s.StageProvider != nil && (input.StageID != "" || input.StageName != "" || len(input.StageIDs) > 0) {
		if input.StageID != "" {

			input.StageIDs = []string{input.StageID}
		} else if input.StageName != "" {
			resolved, err := s.StageProvider.FindStageIDsByName(stageWorkspaceID, input.StageName)
			if err != nil {
				return nil, 0, nil
			}
			input.StageIDs = resolved
		}

	}

	input.StageWorkspaceID = stageWorkspaceID

	entries, totalItems, err := s.historyProvider.SearchInboxEntries(input)
	if err != nil {
		return nil, 0, err
	}

	s.assignInitialStages(entries, input.WorkspaceID)

	entryTypeGroups := make(map[string][]int)
	for i := range entries {
		if entries[i].EntryID == "" || entries[i].EntryType == "" {
			continue
		}
		entryTypeGroups[entries[i].EntryType] = append(entryTypeGroups[entries[i].EntryType], i)
	}

	if s.StageProvider != nil && len(entries) > 0 {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchStages, err := s.StageProvider.GetBatchEntryStages(entryIDs, entryType, stageWorkspaceID)
			if err == nil {
				for _, idx := range indices {
					if tag, ok := batchStages[entries[idx].EntryID]; ok {
						entries[idx].Stage = tag
					}
				}
			}
		}
	}

	if s.labelProvider != nil && len(entries) > 0 {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchLabels, err := s.labelProvider.GetBatchEntryLabels(entryIDs, entryType, stageWorkspaceID)
			if err == nil {
				for _, idx := range indices {
					if labels, ok := batchLabels[entries[idx].EntryID]; ok {
						entries[idx].Labels = labels
					}
				}
			}
		}
	}

	if len(entries) > 0 && stageWorkspaceID != "" {
		campaignIDSet := make(map[string]struct{})
		for i := range entries {
			if entries[i].CampaignID != "" {
				campaignIDSet[entries[i].CampaignID] = struct{}{}
			}
		}
		campaignIDs := make([]string, 0, len(campaignIDSet))
		for id := range campaignIDSet {
			campaignIDs = append(campaignIDs, id)
		}
		if len(campaignIDs) > 0 && s.StageProvider != nil {
			availStages, err := s.StageProvider.GetAvailableStageByCampaigns(stageWorkspaceID, campaignIDs)
			if err == nil {
				for i := range entries {
					if tags, ok := availStages[entries[i].CampaignID]; ok {
						entries[i].AvailableStages = tags
					}
				}
			}
		}
	}

	if s.analysisProvider != nil && len(entries) > 0 {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchAnalysis, err := s.analysisProvider.GetBatchLatestAnalysis(entryIDs, entryType)
			if err == nil {
				for _, idx := range indices {
					if a, ok := batchAnalysis[entries[idx].EntryID]; ok {
						entries[idx].LatestAnalysis = a
					}
				}
			}
		}
	}

	return entries, totalItems, nil
}

// BuildInboxEntry builds a single fully-enriched inbox entry (same shape the
// list produces) by composing the base entry from the history provider with the
// shared enrichEntries pass. Used for entry_update broadcasts so the delivery
// layer never re-implements enrichment.
func (s *inboxService) BuildInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	if s.historyProvider == nil {
		return nil, fmt.Errorf("history provider not configured")
	}
	entry, err := s.historyProvider.GetInboxEntry(entryID, entryType)
	if err != nil || entry == nil {
		return entry, err
	}
	workspaceID := ""
	if s.workspaceResolver != nil {
		workspaceID, _ = s.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	}
	batch := []conversation.InboxEntry{*entry}
	s.enrichEntries(batch, workspaceID)
	*entry = batch[0]
	return entry, nil
}

func (s *inboxService) SendTemplateForEntry(entryID, entryType, templateID string, parameters []string, userID, workspaceID string, isAdmin bool) (string, error) {
	if s.templateSender == nil {
		return "", fmt.Errorf("template sender not configured")
	}
	if s.authorizer != nil && !s.authorizer.CanAccessEntry(userID, workspaceID, entryID, entryType, isAdmin) {
		return "", fmt.Errorf("unauthorized: you don't have access to this entry")
	}
	return s.templateSender.SendTemplate(entryID, entryType, templateID, parameters, userID, workspaceID)
}

func (s *inboxService) enrichEntries(entries []conversation.InboxEntry, campaignWorkspaceID string) {
	if len(entries) == 0 {
		return
	}

	s.assignInitialStages(entries, campaignWorkspaceID)

	entryTypeGroups := make(map[string][]int)
	for i := range entries {
		if entries[i].EntryID == "" || entries[i].EntryType == "" {
			continue
		}
		entryTypeGroups[entries[i].EntryType] = append(entryTypeGroups[entries[i].EntryType], i)
	}

	if s.StageProvider != nil && campaignWorkspaceID != "" {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchStages, err := s.StageProvider.GetBatchEntryStages(entryIDs, entryType, campaignWorkspaceID)
			if err == nil {
				for _, idx := range indices {
					if tag, ok := batchStages[entries[idx].EntryID]; ok {
						entries[idx].Stage = tag
					}
				}
			} else {
				log.Printf("[InboxService] Error batch-fetching tags for type %s: %v", entryType, err)
			}
		}
	}

	if s.labelProvider != nil && campaignWorkspaceID != "" {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchLabels, err := s.labelProvider.GetBatchEntryLabels(entryIDs, entryType, campaignWorkspaceID)
			if err == nil {
				for _, idx := range indices {
					if labels, ok := batchLabels[entries[idx].EntryID]; ok {
						entries[idx].Labels = labels
					}
				}
			} else {
				log.Printf("[InboxService] Error batch-fetching labels for type %s: %v", entryType, err)
			}
		}
	}

	if len(entries) > 0 && campaignWorkspaceID != "" {
		campaignIDSet := make(map[string]struct{})
		for i := range entries {
			if entries[i].CampaignID != "" {
				campaignIDSet[entries[i].CampaignID] = struct{}{}
			}
		}
		campaignIDs := make([]string, 0, len(campaignIDSet))
		for id := range campaignIDSet {
			campaignIDs = append(campaignIDs, id)
		}
		if len(campaignIDs) > 0 && s.StageProvider != nil {
			availStages, err := s.StageProvider.GetAvailableStageByCampaigns(campaignWorkspaceID, campaignIDs)
			if err == nil {
				for i := range entries {
					if tags, ok := availStages[entries[i].CampaignID]; ok {
						entries[i].AvailableStages = tags
					}
				}
			} else {
				log.Printf("[InboxService] Error batch-fetching available tags: %v", err)
			}
		}
	}

	if s.analysisProvider != nil {
		for entryType, indices := range entryTypeGroups {
			entryIDs := make([]string, len(indices))
			for j, idx := range indices {
				entryIDs[j] = entries[idx].EntryID
			}
			batchAnalysis, err := s.analysisProvider.GetBatchLatestAnalysis(entryIDs, entryType)
			if err == nil {
				for _, idx := range indices {
					if a, ok := batchAnalysis[entries[idx].EntryID]; ok {
						entries[idx].LatestAnalysis = a
					}
				}
			} else {
				log.Printf("[InboxService] Error batch-fetching analysis for type %s: %v", entryType, err)
			}
		}
	}
}

func (s *inboxService) assignInitialStages(entries []conversation.InboxEntry, campaignWorkspaceID string) {
	if s.InitialStageAssigner == nil || campaignWorkspaceID == "" {
		return
	}

	for i := range entries {
		if entries[i].EntryID == "" || entries[i].EntryType == "" || entries[i].CampaignID == "" {
			continue
		}
		s.InitialStageAssigner.AutoAssignInitialStage(campaignWorkspaceID, entries[i].CampaignID, entries[i].EntryType, entries[i].EntryID, entries[i].EntryType)
	}
}

func (s *inboxService) GetInboxStageCounts(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	if s.StageProvider == nil {
		return nil, nil
	}
	if campaignID != "" {
		return s.StageProvider.GetStageCountsForCampaign(workspaceID, campaignID, entryType)
	}
	return s.StageProvider.GetStageCountsForWorkspace(workspaceID, entryType)
}

func (s *inboxService) GetConversationStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	if s.statusProvider == nil {
		return nil, nil
	}
	return s.statusProvider.GetStatusCounts(workspaceID, campaignID, entryType)
}

func (s *inboxService) GetAvailableLabels(workspaceID string) ([]conversation.InboxEntryLabel, error) {
	if s.labelProvider == nil {
		return nil, nil
	}
	return s.labelProvider.GetAvailableLabels(workspaceID)
}

func (s *inboxService) canViewOthers(userID, workspaceID string) bool {
	if s.authorizer == nil || workspaceID == "" {
		return false
	}
	return s.authorizer.HasWorkspacePermission(userID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionViewOthers), false)
}
