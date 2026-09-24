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

// ConversationCampaigns reads the campaign a conversation was opened for ("" for
// a campaign-less one). The unofficial conversation repository implements it.
type ConversationCampaigns interface {
	CampaignIDForEntry(ctx context.Context, entryID string) (string, error)
}

// AutomationSource answers which campaign's automation a conversation follows:
// the campaign it was opened for, known from the moment it exists.
type AutomationSource struct {
	conversations ConversationCampaigns
	campaigns     uwc.Repository
}

func NewAutomationSource(conversations ConversationCampaigns, campaigns uwc.Repository) *AutomationSource {
	return &AutomationSource{conversations: conversations, campaigns: campaigns}
}

// campaignFor is the campaign a conversation was opened for; nil for a
// campaign-less conversation or a campaign that no longer exists.
func (s *AutomationSource) campaignFor(ctx context.Context, conversationID string) (*uwc.Campaign, error) {
	campaignID, err := s.conversations.CampaignIDForEntry(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if campaignID == "" {
		return nil, nil
	}
	camp, err := s.campaigns.FindByID(campaignID)
	if errors.Is(err, uwc.ErrCampaignNotFound) {
		return nil, nil
	}
	return camp, err
}

func (s *AutomationSource) AutomationForConversation(conversationID string) (*uwuc.CampaignAutomation, bool) {
	if s == nil || s.conversations == nil || s.campaigns == nil {
		return nil, false
	}
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return nil, false
	}

	camp, err := s.campaignFor(context.Background(), id)
	if err != nil {
		// Reporting "no campaign" would run the instance's AI on a conversation
		// that may belong to a campaign without one. Run nothing instead.
		log.Printf("[unofficial-whatsapp-campaign] automation lookup failed for conversation %s, running no automation: %v", id, err)
		return &uwuc.CampaignAutomation{}, true
	}
	if camp == nil {
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
		if err != nil || subject == nil || s == nil || s.conversations == nil || s.campaigns == nil {
			return subject, err
		}
		camp, err := s.campaignFor(ctx, entryID)
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
