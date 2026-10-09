package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/actor"
	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/lead_message_window"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace"
)

var (
	ErrConversationNotVisible = errors.New("lead history: the conversation is not visible to this member")
	errHistoryIncomplete      = errors.New("lead history: a required dependency is missing")
)

type LeadFinder interface {
	FindByID(workspaceID, id string) (*lead.Lead, error)
	Load(ctx context.Context, workspaceID, id string) (*lead.Lead, error)
}

type RelativeLister interface {
	ListRelatives(ctx context.Context, workspaceID string, q lead.RelativesQuery) (lead.RelativesPage, error)
}

type EntryLister interface {
	ListByLeadID(leadID string) ([]wce.WhatsAppCampaignEntry, error)
	FindByID(id string) (*wce.WhatsAppCampaignEntry, error)
}

type MessageLister interface {
	ListByEntry(entryID string, entryType shared.EntryType) ([]*conversation.Message, error)
}

type AnalysisReader interface {
	LatestByEntries(ctx context.Context, workspaceID string, source ca.Source, entryIDs []string) (map[string]*ca.Analysis, error)
}

type EntryAccessResolver interface {
	EntryVisibilityFor(userID, workspaceID string, isAdmin bool) conversation.EntryVisibility
}

type WindowReader interface {
	FindAllByLead(leadID string) ([]*lead_message_window.LeadMessageWindow, error)
}

type CampaignNameResolver interface {
	ResolveCampaignNames(campaignIDs []string) map[string]string
}

type HistoryDeps struct {
	Leads         LeadFinder
	Entries       EntryLister
	Messages      MessageLister
	Analyses      AnalysisReader
	Access        EntryAccessResolver
	Windows       WindowReader
	CampaignNames CampaignNameResolver
	Permissions   Permissions
	Definitions   DefinitionSource
	Relatives     RelativeLister
	EntryLeads    lead.EntryLeads
	Owners        actor.NameResolver
}

type History struct {
	deps    HistoryDeps
	viewers viewers
}

func NewHistory(deps HistoryDeps) (*History, error) {
	missing := map[string]bool{
		"leads":          deps.Leads == nil,
		"entries":        deps.Entries == nil,
		"messages":       deps.Messages == nil,
		"analyses":       deps.Analyses == nil,
		"access":         deps.Access == nil,
		"windows":        deps.Windows == nil,
		"campaign names": deps.CampaignNames == nil,
		"permissions":    deps.Permissions == nil,
		"definitions":    deps.Definitions == nil,
		"relatives":      deps.Relatives == nil,
		"entry leads":    deps.EntryLeads == nil,
		"owner names":    deps.Owners == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errHistoryIncomplete, name)
		}
	}
	return &History{deps: deps, viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions}}, nil
}

type EntryConversation struct {
	EntryID    string
	EntryType  shared.EntryType
	LeadID     string
	CampaignID string
	Status     string
	Messages   []*conversation.Message
}

type CampaignHistory struct {
	CampaignID     string
	CampaignName   string
	Entries        []wce.WhatsAppCampaignEntry
	LastActivityAt time.Time
}

type LeadDetail struct {
	Lead      *lead.Lead
	Campaigns []CampaignHistory
	Summary   lead.LeadSummary
	OwnerName string
}

func (h *History) visibility(v conversation.Viewer, refs []shared.EntryRef) (map[shared.EntryRef]bool, error) {
	return visibleEntries(h.deps.Access, v, refs)
}

func visibleEntries(resolver EntryAccessResolver, v conversation.Viewer, refs []shared.EntryRef) (map[shared.EntryRef]bool, error) {
	if len(refs) == 0 {
		return map[shared.EntryRef]bool{}, nil
	}
	access := resolver.EntryVisibilityFor(v.UserID, v.WorkspaceID, v.IsAdmin)
	if access == nil {
		return nil, errHistoryIncomplete
	}
	visible, err := access.VisibleEntries(refs)
	if err != nil {
		return nil, fmt.Errorf("conversation access: %w", err)
	}
	return visible, nil
}

func requireLeadRef(v conversation.Viewer, leadID string) error {
	return requireRecordRef(v.WorkspaceID, leadID)
}

func (h *History) lead(v conversation.Viewer, leadID string) (*lead.Lead, error) {
	if err := requireLeadRef(v, leadID); err != nil {
		return nil, err
	}
	return found(h.deps.Leads.FindByID(v.WorkspaceID, leadID))
}

func (h *History) Entries(v conversation.Viewer, leadID string) (*lead.Lead, []wce.WhatsAppCampaignEntry, error) {
	l, err := h.lead(v, leadID)
	if err != nil {
		return nil, nil, err
	}
	entries, err := h.visible(v, l.ID, "")
	if err != nil {
		return nil, nil, err
	}
	return l, entries, nil
}

func (h *History) Detail(ctx context.Context, v conversation.Viewer, leadID string) (LeadDetail, error) {
	if err := requireLeadRef(v, leadID); err != nil {
		return LeadDetail{}, err
	}
	reader, err := h.viewers.of(v)
	if err != nil {
		return LeadDetail{}, err
	}
	if !reader.ReadsLeads {
		return LeadDetail{}, lead.ErrLeadForbidden
	}
	l, err := found(h.deps.Leads.Load(ctx, v.WorkspaceID, leadID))
	if err != nil {
		return LeadDetail{}, err
	}
	entries, err := h.visible(v, l.ID, "")
	if err != nil {
		return LeadDetail{}, err
	}
	windows, err := h.deps.Windows.FindAllByLead(l.ID)
	if err != nil {
		return LeadDetail{}, fmt.Errorf("message windows of lead %s: %w", l.ID, err)
	}
	ownerName, err := h.ownerName(l.Owner)
	if err != nil {
		return LeadDetail{}, err
	}
	campaigns := h.groupByCampaign(entries)
	return LeadDetail{Lead: lead.VisibleFields(l, reader), Campaigns: campaigns, Summary: summarize(campaigns, windows), OwnerName: ownerName}, nil
}

func (h *History) ownerName(owner string) (string, error) {
	if strings.TrimSpace(owner) == "" {
		return "", nil
	}
	names, err := h.deps.Owners.ResolveNames(owner)
	if err != nil {
		return "", fmt.Errorf("owner name: %w", err)
	}
	return names[owner], nil
}

func (h *History) Relatives(ctx context.Context, v conversation.Viewer, q lead.RelativesQuery) (lead.RelativesPage, error) {
	if err := requireLeadRef(v, q.LeadID); err != nil {
		return lead.RelativesPage{}, err
	}
	if !h.viewers.allowed(v, workspace.ActionRead) {
		return lead.RelativesPage{}, lead.ErrLeadForbidden
	}
	if _, err := h.lead(v, q.LeadID); err != nil {
		return lead.RelativesPage{}, err
	}
	return h.deps.Relatives.ListRelatives(ctx, v.WorkspaceID, q)
}

func (h *History) EntryLead(ctx context.Context, v conversation.Viewer, entryID string, entryType shared.EntryType) (lead.Card, error) {
	ref, err := h.visibleEntry(v, entryID, entryType)
	if err != nil {
		return lead.Card{}, err
	}
	reader, err := h.viewers.of(v)
	if err != nil {
		return lead.Card{}, err
	}
	leadID, err := h.deps.EntryLeads.LeadOfEntry(ctx, v.WorkspaceID, ref)
	if err != nil {
		return lead.Card{}, err
	}
	l, err := found(h.deps.Leads.Load(ctx, v.WorkspaceID, leadID))
	if err != nil {
		return lead.Card{}, err
	}
	card := lead.CardOf(l, reader)
	if card.OwnerName, err = h.ownerName(l.Owner); err != nil {
		return lead.Card{}, err
	}
	return card, nil
}

func (h *History) groupByCampaign(entries []wce.WhatsAppCampaignEntry) []CampaignHistory {
	index := map[string]int{}
	campaigns := make([]CampaignHistory, 0)
	for _, e := range entries {
		i, ok := index[e.CampaignID]
		if !ok {
			i = len(campaigns)
			index[e.CampaignID] = i
			campaigns = append(campaigns, CampaignHistory{CampaignID: e.CampaignID})
		}
		campaigns[i].Entries = append(campaigns[i].Entries, e)
		if e.UpdatedAt.After(campaigns[i].LastActivityAt) {
			campaigns[i].LastActivityAt = e.UpdatedAt
		}
	}
	if len(campaigns) == 0 {
		return campaigns
	}
	ids := make([]string, len(campaigns))
	for i := range campaigns {
		ids[i] = campaigns[i].CampaignID
	}
	names := h.deps.CampaignNames.ResolveCampaignNames(ids)
	for i := range campaigns {
		campaigns[i].CampaignName = names["whatsapp:"+campaigns[i].CampaignID]
	}
	return campaigns
}

func summarize(campaigns []CampaignHistory, windows []*lead_message_window.LeadMessageWindow) lead.LeadSummary {
	var summary lead.LeadSummary
	latest := func(at time.Time) {
		if summary.LastActivityAt == nil || at.After(*summary.LastActivityAt) {
			summary.LastActivityAt = &at
		}
	}
	for _, window := range windows {
		if window == nil {
			continue
		}
		if window.IsWindowOpen() {
			summary.WhatsAppWindowOpen = true
			expires := window.WindowExpiresAt()
			summary.WindowExpiresAt = &expires
		}
		latest(window.LastMessageAt)
	}
	for _, c := range campaigns {
		summary.WhatsAppCampaigns += len(c.Entries)
		latest(c.LastActivityAt)
	}
	summary.TotalCampaigns = summary.WhatsAppCampaigns
	return summary
}

func (h *History) EntriesInCampaign(v conversation.Viewer, leadID, campaignID string, entryType shared.EntryType) ([]wce.WhatsAppCampaignEntry, error) {
	l, err := h.lead(v, leadID)
	if err != nil {
		return nil, err
	}
	if entryType.Valid() && entryType != shared.EntryTypeWhatsApp {
		return []wce.WhatsAppCampaignEntry{}, nil
	}
	return h.visible(v, l.ID, campaignID)
}

func (h *History) visible(v conversation.Viewer, leadID, campaignID string) ([]wce.WhatsAppCampaignEntry, error) {
	all, err := h.deps.Entries.ListByLeadID(leadID)
	if err != nil {
		return nil, fmt.Errorf("entries of lead %s: %w", leadID, err)
	}
	candidates := make([]wce.WhatsAppCampaignEntry, 0, len(all))
	refs := make([]shared.EntryRef, 0, len(all))
	for _, e := range all {
		if campaignID != "" && e.CampaignID != campaignID {
			continue
		}
		candidates = append(candidates, e)
		refs = append(refs, shared.EntryRef{EntryID: e.ID, EntryType: shared.EntryTypeWhatsApp})
	}
	visible, err := h.visibility(v, refs)
	if err != nil {
		return nil, err
	}
	out := make([]wce.WhatsAppCampaignEntry, 0, len(candidates))
	for i, e := range candidates {
		if visible[refs[i]] {
			out = append(out, e)
		}
	}
	return out, nil
}

func (h *History) Analyses(ctx context.Context, v conversation.Viewer, leadID, campaignID string, entryType shared.EntryType) (*lead.Lead, []*ca.Analysis, error) {
	l, err := h.lead(v, leadID)
	if err != nil {
		return nil, nil, err
	}
	if entryType.Valid() && entryType != shared.EntryTypeWhatsApp {
		return l, []*ca.Analysis{}, nil
	}
	entries, err := h.visible(v, l.ID, campaignID)
	if err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		return l, []*ca.Analysis{}, nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	latest, err := h.deps.Analyses.LatestByEntries(ctx, v.WorkspaceID, ca.SourceWhatsApp, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("analyses of lead %s: %w", l.ID, err)
	}
	out := make([]*ca.Analysis, 0, len(ids))
	for _, id := range ids {
		if a, ok := latest[id]; ok && a != nil {
			out = append(out, a)
		}
	}
	return l, out, nil
}

func (h *History) visibleEntry(v conversation.Viewer, entryID string, entryType shared.EntryType) (shared.EntryRef, error) {
	if !entryType.Valid() {
		entryType = shared.EntryTypeWhatsApp
	}
	if strings.TrimSpace(v.WorkspaceID) == "" {
		return shared.EntryRef{}, lead.ErrLeadWorkspaceRequired
	}
	ref := shared.EntryRef{EntryID: strings.TrimSpace(entryID), EntryType: entryType}
	if ref.EntryID == "" {
		return shared.EntryRef{}, ErrConversationNotVisible
	}
	visible, err := h.visibility(v, []shared.EntryRef{ref})
	if err != nil {
		return shared.EntryRef{}, err
	}
	if !visible[ref] {
		return shared.EntryRef{}, ErrConversationNotVisible
	}
	return ref, nil
}

func (h *History) EntryConversation(v conversation.Viewer, entryID string, entryType shared.EntryType) (EntryConversation, error) {
	ref, err := h.visibleEntry(v, entryID, entryType)
	if err != nil {
		return EntryConversation{}, err
	}
	entryType = ref.EntryType
	messages, err := h.deps.Messages.ListByEntry(ref.EntryID, entryType)
	if err != nil {
		return EntryConversation{}, fmt.Errorf("messages of entry %s: %w", ref.EntryID, err)
	}
	out := EntryConversation{EntryID: ref.EntryID, EntryType: entryType, Messages: messages}
	if entryType != shared.EntryTypeWhatsApp {
		return out, nil
	}
	entry, err := h.deps.Entries.FindByID(ref.EntryID)
	if err != nil {
		return EntryConversation{}, fmt.Errorf("entry %s: %w", ref.EntryID, err)
	}
	if entry == nil {
		return EntryConversation{}, ErrConversationNotVisible
	}
	out.LeadID, out.CampaignID, out.Status = entry.LeadID, entry.CampaignID, string(entry.Status)
	return out, nil
}
