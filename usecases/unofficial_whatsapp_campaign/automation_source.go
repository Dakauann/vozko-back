package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	convuc "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type AutomationSource struct {
	entries   uwc.EntryRepository
	campaigns uwc.Repository
}

func NewAutomationSource(entries uwc.EntryRepository, campaigns uwc.Repository) *AutomationSource {
	return &AutomationSource{entries: entries, campaigns: campaigns}
}

func (s *AutomationSource) AutomationForConversation(conversationID string) (*uwuc.CampaignAutomation, bool) {
	if s == nil || s.entries == nil || s.campaigns == nil {
		return nil, false
	}
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return nil, false
	}

	entry, err := s.entries.FindLatestByConversationID(id)
	if err != nil {
		if !errors.Is(err, uwc.ErrEntryNotFound) {
			log.Printf("[unofficial-whatsapp-campaign] automation lookup failed for conversation %s: %v", id, err)
		}
		return nil, false
	}

	camp, err := s.campaigns.FindByID(entry.CampaignID)
	if err != nil || camp == nil {
		if err != nil && !errors.Is(err, uwc.ErrCampaignNotFound) {
			log.Printf("[unofficial-whatsapp-campaign] automation lookup failed for campaign %s: %v", entry.CampaignID, err)
		}
		return nil, false
	}

	return &uwuc.CampaignAutomation{
		CampaignID:        camp.ID,
		EnableAnalysis:    camp.EnableAnalysis,
		EnableAutoStaging: camp.EnableAutoStaging,
		EnableAutoMemory:  camp.EnableAutoMemory,
		Automation: campaign.Automation{
			AgentID:              camp.AgentID,
			WorkflowID:           camp.WorkflowID,
			EnableAgentResponses: camp.EnableAgentResponses,
			EnableWorkflow:       camp.EnableWorkflow,
		},
	}, true
}

func (s *AutomationSource) AnalysisResolver(base convuc.AnalysisSubjectResolver) convuc.AnalysisSubjectResolver {
	return func(ctx context.Context, entryID string) (*convuc.AnalysisSubject, error) {
		subject, err := base(ctx, entryID)
		if err != nil || subject == nil || s == nil || s.entries == nil || s.campaigns == nil {
			return subject, err
		}
		entry, err := s.entries.FindLatestByConversationID(entryID)
		if errors.Is(err, uwc.ErrEntryNotFound) {
			return subject, nil
		}
		if err != nil {
			return nil, err
		}
		camp, err := s.campaigns.FindByID(entry.CampaignID)
		if errors.Is(err, uwc.ErrCampaignNotFound) {
			return subject, nil
		}
		if err != nil {
			return nil, err
		}
		if camp == nil {
			return subject, nil
		}
		overridden := *subject
		overridden.ContainerID, overridden.ContainerName = camp.ID, camp.Name
		overridden.AgentID, overridden.AIModel = camp.AgentID, camp.AiModel
		overridden.EnableAnalysis = camp.EnableAnalysis
		overridden.EnableAutoStaging = camp.EnableAutoStaging
		overridden.EnableAutoMemory = camp.EnableAutoMemory
		return &overridden, nil
	}
}
