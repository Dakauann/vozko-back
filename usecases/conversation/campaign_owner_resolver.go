package conversation_usecase

import (
	"fmt"

	"vozko/domain/conversation"
	wc_domain "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type campaignWorkspaceResolver struct {
	wcCampaignRepo wc_domain.Repository
	waEntryRepo    wce.Repository
}

func NewCampaignWorkspaceResolver(
	wcCampaignRepo wc_domain.Repository,
	waEntryRepo wce.Repository,
) conversation.CampaignWorkspaceResolver {
	return &campaignWorkspaceResolver{
		wcCampaignRepo: wcCampaignRepo,
		waEntryRepo:    waEntryRepo,
	}
}

func (r *campaignWorkspaceResolver) GetCampaignWorkspaceID(campaignID, campaignType string) (string, error) {
	if campaignType != "whatsapp" {
		return "", fmt.Errorf("unknown campaign type: %s", campaignType)
	}
	c, err := r.wcCampaignRepo.FindByID(campaignID)
	if err != nil {
		return "", fmt.Errorf("find whatsapp campaign: %w", err)
	}
	return c.WorkspaceID, nil
}

func (r *campaignWorkspaceResolver) GetCampaignDepartmentID(campaignID, campaignType string) (string, error) {
	switch campaignType {
	case "whatsapp":
		c, err := r.wcCampaignRepo.FindByID(campaignID)
		if err != nil {
			return "", fmt.Errorf("find whatsapp campaign: %w", err)
		}
		return c.DepartmentID, nil

	case "support":
		return "", nil

	default:
		return "", fmt.Errorf("unknown campaign type: %s", campaignType)
	}
}

func (r *campaignWorkspaceResolver) GetEntryWorkspaceID(entryID, entryType string) (string, error) {
	if entryType != "whatsapp" {
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
	info, err := r.waEntryRepo.GetCampaignForEntry(entryID)
	if err != nil {
		return "", fmt.Errorf("get campaign for whatsapp entry: %w", err)
	}
	return r.GetCampaignWorkspaceID(info.CampaignID, "whatsapp")
}

func (r *campaignWorkspaceResolver) GetEntryDepartmentID(entryID, entryType string) (string, error) {
	campaignID, err := r.GetEntryCampaignID(entryID, entryType)
	if err != nil {
		return "", err
	}

	switch entryType {
	case "whatsapp":
		return r.GetCampaignDepartmentID(campaignID, "whatsapp")
	case "support":
		return "", nil
	default:
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
}

func (r *campaignWorkspaceResolver) GetEntryCampaignID(entryID, entryType string) (string, error) {
	if entryType != "whatsapp" {
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
	info, err := r.waEntryRepo.GetCampaignForEntry(entryID)
	if err != nil {
		return "", fmt.Errorf("get campaign for whatsapp entry: %w", err)
	}
	return info.CampaignID, nil
}
