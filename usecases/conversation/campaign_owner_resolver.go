package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wc_domain "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type EntryOwnerResolver interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
}

type EntryCampaignResolver interface {
	CampaignIDForEntry(ctx context.Context, entryID string) (string, error)
}

type instagramEntryResolver = EntryOwnerResolver

type campaignWorkspaceResolver struct {
	wcCampaignRepo wc_domain.Repository
	waEntryRepo    wce.Repository
	entryResolvers map[shared.EntryType]EntryOwnerResolver
}

func NewCampaignWorkspaceResolver(
	wcCampaignRepo wc_domain.Repository,
	waEntryRepo wce.Repository,
) conversation.CampaignWorkspaceResolver {
	return &campaignWorkspaceResolver{
		wcCampaignRepo: wcCampaignRepo,
		waEntryRepo:    waEntryRepo,
		entryResolvers: make(map[shared.EntryType]EntryOwnerResolver, 2),
	}
}

func (r *campaignWorkspaceResolver) SetEntryOwnerResolver(entryType shared.EntryType, repo EntryOwnerResolver) {
	if r == nil || repo == nil || entryType == "" {
		return
	}
	if r.entryResolvers == nil {
		r.entryResolvers = make(map[shared.EntryType]EntryOwnerResolver, 2)
	}
	r.entryResolvers[entryType] = repo
}

func (r *campaignWorkspaceResolver) SetInstagramEntryResolver(repo EntryOwnerResolver) {
	r.SetEntryOwnerResolver(shared.EntryTypeInstagram, repo)
}

func (r *campaignWorkspaceResolver) resolverFor(entryType string) (EntryOwnerResolver, bool) {
	if r == nil || r.entryResolvers == nil {
		return nil, false
	}
	resolver, ok := r.entryResolvers[shared.EntryType(entryType)]
	return resolver, ok && resolver != nil
}

func (r *campaignWorkspaceResolver) GetCampaignWorkspaceID(campaignID, campaignType string) (string, error) {
	if campaignType != string(shared.EntryTypeWhatsApp) {
		return "", fmt.Errorf("unknown campaign type: %s", campaignType)
	}
	c, err := r.wcCampaignRepo.FindByID(campaignID)
	if err != nil {
		return "", fmt.Errorf("find whatsapp campaign: %w", err)
	}
	return c.WorkspaceID, nil
}

func (r *campaignWorkspaceResolver) GetCampaignDepartmentID(campaignID, campaignType string) (string, error) {
	if campaignType == string(shared.EntryTypeWhatsApp) {
		c, err := r.wcCampaignRepo.FindByID(campaignID)
		if err != nil {
			return "", fmt.Errorf("find whatsapp campaign: %w", err)
		}
		return c.DepartmentID, nil
	}
	if shared.EntryType(campaignType).IsKnown() {
		return "", nil
	}
	return "", fmt.Errorf("unknown campaign type: %s", campaignType)
}

func (r *campaignWorkspaceResolver) GetEntryWorkspaceID(entryID, entryType string) (string, error) {
	if resolver, ok := r.resolverFor(entryType); ok {
		return resolver.WorkspaceIDForEntry(context.Background(), entryID)
	}
	if entryType != string(shared.EntryTypeWhatsApp) {
		return "", fmt.Errorf("unknown entry type: %s", entryType)
	}
	info, err := r.waEntryRepo.GetCampaignForEntry(entryID)
	if err != nil {
		return "", fmt.Errorf("get campaign for whatsapp entry: %w", err)
	}
	return r.GetCampaignWorkspaceID(info.CampaignID, string(shared.EntryTypeWhatsApp))
}

func (r *campaignWorkspaceResolver) GetEntryDepartmentID(entryID, entryType string) (string, error) {
	campaignID, err := r.GetEntryCampaignID(entryID, entryType)
	if err != nil {
		return "", err
	}

	if entryType == string(shared.EntryTypeWhatsApp) {
		return r.GetCampaignDepartmentID(campaignID, string(shared.EntryTypeWhatsApp))
	}
	if resolver, ok := r.resolverFor(entryType); ok {
		return resolver.DepartmentIDForEntry(context.Background(), entryID)
	}
	if shared.EntryType(entryType).IsKnown() {
		return "", nil
	}
	return "", fmt.Errorf("unknown entry type: %s", entryType)
}

func (r *campaignWorkspaceResolver) GetEntryCampaignID(entryID, entryType string) (string, error) {
	if entryType == string(shared.EntryTypeWhatsApp) {
		info, err := r.waEntryRepo.GetCampaignForEntry(entryID)
		if err != nil {
			return "", fmt.Errorf("get campaign for whatsapp entry: %w", err)
		}
		return info.CampaignID, nil
	}
	if resolver, ok := r.resolverFor(entryType); ok {
		if withCampaign, ok := resolver.(EntryCampaignResolver); ok {
			return withCampaign.CampaignIDForEntry(context.Background(), entryID)
		}
	}
	if shared.EntryType(entryType).IsKnown() {
		return "", nil
	}
	return "", fmt.Errorf("unknown entry type: %s", entryType)
}
