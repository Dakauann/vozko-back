package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/conversation"
	wc_domain "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// instagramEntryResolver is a narrow port for resolving the tenant and department
// of an Instagram conversation. Declared here so this resolver does not depend on
// the Instagram package.
type instagramEntryResolver interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
}

type campaignWorkspaceResolver struct {
	wcCampaignRepo     wc_domain.Repository
	waEntryRepo        wce.Repository
	instagramEntryRepo instagramEntryResolver
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

// SetInstagramEntryResolver registers the Instagram lookup. Optional so the
// resolver still constructs when the channel is disabled.
func (r *campaignWorkspaceResolver) SetInstagramEntryResolver(repo instagramEntryResolver) {
	r.instagramEntryRepo = repo
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

	case "support", "instagram":
		return "", nil

	default:
		return "", fmt.Errorf("unknown campaign type: %s", campaignType)
	}
}

func (r *campaignWorkspaceResolver) GetEntryWorkspaceID(entryID, entryType string) (string, error) {
	// Instagram conversations carry workspace_id on the row, so there is no
	// campaign to walk through — the whole reason WhatsApp needs the indirection.
	if entryType == "instagram" {
		if r.instagramEntryRepo == nil {
			return "", fmt.Errorf("instagram entry resolver not configured")
		}
		return r.instagramEntryRepo.WorkspaceIDForEntry(context.Background(), entryID)
	}
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
	case "instagram":
		if r.instagramEntryRepo == nil {
			return "", nil
		}
		return r.instagramEntryRepo.DepartmentIDForEntry(context.Background(), entryID)
	case "support":
		return "", nil
	default:
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
}

func (r *campaignWorkspaceResolver) GetEntryCampaignID(entryID, entryType string) (string, error) {
	// Instagram has no campaign; the entry itself is the unit and callers that
	// need the container use the account id instead.
	if entryType == "instagram" {
		return "", nil
	}
	if entryType != "whatsapp" {
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
	info, err := r.waEntryRepo.GetCampaignForEntry(entryID)
	if err != nil {
		return "", fmt.Errorf("get campaign for whatsapp entry: %w", err)
	}
	return info.CampaignID, nil
}
