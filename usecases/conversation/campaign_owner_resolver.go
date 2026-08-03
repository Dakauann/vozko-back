package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wc_domain "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// EntryOwnerResolver is a narrow port for resolving the tenant and department of
// one channel's conversation. Declared here so this resolver depends on no
// channel package.
//
// Channels that carry workspace_id on the conversation row itself implement it
// directly; WhatsApp is the exception, walking entry → campaign → workspace,
// which is why it keeps its own branch below.
type EntryOwnerResolver interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
}

// instagramEntryResolver is the previous name of EntryOwnerResolver.
//
// Deprecated: use EntryOwnerResolver.
type instagramEntryResolver = EntryOwnerResolver

type campaignWorkspaceResolver struct {
	wcCampaignRepo wc_domain.Repository
	waEntryRepo    wce.Repository
	// entryResolvers is keyed by entry type so registering one channel never
	// displaces another. It replaces a single Instagram-shaped field, which a
	// second channel would have had to either overwrite or duplicate.
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

// SetEntryOwnerResolver registers a channel's tenant lookup. Optional so the
// resolver still constructs when a channel is disabled.
func (r *campaignWorkspaceResolver) SetEntryOwnerResolver(entryType shared.EntryType, repo EntryOwnerResolver) {
	if r == nil || repo == nil || entryType == "" {
		return
	}
	if r.entryResolvers == nil {
		r.entryResolvers = make(map[shared.EntryType]EntryOwnerResolver, 2)
	}
	r.entryResolvers[entryType] = repo
}

// SetInstagramEntryResolver registers the Instagram lookup.
//
// Deprecated: use SetEntryOwnerResolver(shared.EntryTypeInstagram, repo).
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
	// Channels with no campaign concept have no campaign-level department; the
	// department lives on the account and is read through GetEntryDepartmentID.
	if shared.EntryType(campaignType).IsKnown() {
		return "", nil
	}
	return "", fmt.Errorf("unknown campaign type: %s", campaignType)
}

func (r *campaignWorkspaceResolver) GetEntryWorkspaceID(entryID, entryType string) (string, error) {
	// Channels that carry workspace_id on the conversation row need no campaign
	// walk, the indirection exists only because a WhatsApp entry does not know
	// its own tenant.
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
	// A known channel with no registered resolver (support, or a channel whose
	// bundle is switched off) simply has no department scope.
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
	// Channels with no campaign concept return empty; callers that need the
	// container use the account id instead.
	if shared.EntryType(entryType).IsKnown() {
		return "", nil
	}
	return "", fmt.Errorf("unknown entry type: %s", entryType)
}
