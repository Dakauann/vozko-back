package lead

import (
	"time"

	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/unofficial_whatsapp"
)

type OptOutLeadRequest struct {
	Source string `json:"source,omitempty" enums:"operator,lead_request" example:"operator"`
}

type BlockLeadRequest struct {
	Blocked         *bool  `json:"blocked" example:"true"`
	BusinessPhoneID string `json:"businessPhoneId,omitempty" example:"bp_a1b2c3"`
}

type BlockLeadResponse struct {
	LeadID      string `json:"leadId"`
	Blocked     bool   `json:"blocked"`
	MetaApplied bool   `json:"metaApplied"`
	Version     int64  `json:"version" example:"4"`
}

type EntryResponse struct {
	ID         string           `json:"id"`
	CampaignID string           `json:"campaignId"`
	EntryType  shared.EntryType `json:"entryType"`
	Status     string           `json:"status"`
	CreatedAt  string           `json:"createdAt"`
}

type LeadListResponseItem struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspaceId"`
	Number            string  `json:"number"`
	Name              string  `json:"name,omitempty"`
	RealName          string  `json:"realName,omitempty" example:"Maria Souza"`
	ProfilePictureURL string  `json:"profilePictureUrl,omitempty"`
	Age               *int    `json:"age,omitempty"`
	Blocked           bool    `json:"blocked"`
	BlockedAt         *string `json:"blockedAt,omitempty"`
	Version           int64   `json:"version" example:"3"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`

	WhatsAppCampaigns  int     `json:"whatsappCampaigns"`
	TotalCampaigns     int     `json:"totalCampaigns"`
	LastActivityAt     *string `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool    `json:"whatsappWindowOpen"`
	WindowExpiresAt    *string `json:"windowExpiresAt,omitempty"`

	Memories     int     `json:"memories"`
	LastMemoryAt *string `json:"lastMemoryAt,omitempty"`

	Owner          string                 `json:"owner,omitempty"`
	OwnerName      string                 `json:"ownerName,omitempty" example:"Marina Costa"`
	Phones         []ContactPhoneResponse `json:"phones"`
	PrimaryAddress *AddressResponse       `json:"primaryAddress,omitempty"`
	CustomFields   map[string]any         `json:"customFields,omitempty"`
	RelativesCount int                    `json:"relativesCount"`
	ReferredCount  int                    `json:"referredCount"`
}

type LeadConsentResponse struct {
	GrantedAt string `json:"grantedAt"`
	Source    string `json:"source" example:"manual"`
	Purpose   string `json:"purpose,omitempty" example:"Avisos sobre a matrícula"`
}

type LeadRecordResponse struct {
	ID                string                 `json:"id"`
	WorkspaceID       string                 `json:"workspaceId"`
	Number            string                 `json:"number"`
	Name              string                 `json:"name,omitempty"`
	RealName          string                 `json:"realName,omitempty" example:"Maria Souza"`
	NameSource        string                 `json:"nameSource,omitempty" example:"manual"`
	Nickname          string                 `json:"nickname,omitempty"`
	Email             string                 `json:"email,omitempty"`
	BirthDate         string                 `json:"birthDate,omitempty" example:"1990-04-21"`
	Age               *int                   `json:"age,omitempty"`
	Source            string                 `json:"source,omitempty" example:"manual"`
	Owner             string                 `json:"owner,omitempty"`
	WhatsAppOptIn     *LeadConsentResponse   `json:"whatsappOptIn,omitempty"`
	OptedOutAt        *string                `json:"optedOutAt,omitempty"`
	OptOutSource      string                 `json:"optOutSource,omitempty" enums:"operator,lead_request" example:"operator"`
	Blocked           bool                   `json:"blocked"`
	BlockedAt         *string                `json:"blockedAt,omitempty"`
	BlockedBy         *string                `json:"blockedBy,omitempty"`
	ProfilePictureURL string                 `json:"profilePictureUrl,omitempty"`
	Phones            []ContactPhoneResponse `json:"phones,omitempty"`
	Addresses         []AddressResponse      `json:"addresses,omitempty"`
	CustomFields      map[string]any         `json:"customFields,omitempty"`
	RelativesCount    int                    `json:"relativesCount"`
	ReferredCount     int                    `json:"referredCount"`
	Version           int64                  `json:"version" example:"3"`
	CreatedAt         string                 `json:"createdAt,omitempty"`
	UpdatedAt         string                 `json:"updatedAt,omitempty"`
}

type LeadDetailResponse struct {
	LeadRecordResponse
	OwnerName          string                `json:"ownerName,omitempty" example:"Marina Costa"`
	WhatsAppCampaigns  int                   `json:"whatsappCampaigns"`
	TotalCampaigns     int                   `json:"totalCampaigns"`
	LastActivityAt     *string               `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool                  `json:"whatsappWindowOpen"`
	WindowExpiresAt    *string               `json:"windowExpiresAt,omitempty"`
	Campaigns          []CampaignHistoryItem `json:"campaigns"`
}

type LeadDetailSummaryResponse struct {
	DealsCount    *int                   `json:"dealsCount,omitempty" example:"2"`
	MemoriesCount int                    `json:"memoriesCount" example:"5"`
	SharedNumbers []SharedNumberResponse `json:"sharedNumbers"`
}

type SharedNumberResponse struct {
	Number  string                 `json:"number" example:"551133334444"`
	Holders []NumberHolderResponse `json:"holders"`
	More    bool                   `json:"more"`
}

type NumberHolderResponse struct {
	LeadID string `json:"leadId"`
	Name   string `json:"name,omitempty" example:"Joana Souza"`
	Number string `json:"number,omitempty" example:"5511912345678"`
}

func sharedNumberResponses(shared []leaddomain.SharedNumber) []SharedNumberResponse {
	out := make([]SharedNumberResponse, 0, len(shared))
	for _, s := range shared {
		holders := make([]NumberHolderResponse, 0, len(s.Holders))
		for _, h := range s.Holders {
			holders = append(holders, NumberHolderResponse{LeadID: h.LeadID, Name: h.Name, Number: h.Number})
		}
		out = append(out, SharedNumberResponse{Number: s.Number, Holders: holders, More: s.More})
	}
	return out
}

type CampaignHistoryItem struct {
	CampaignID   string              `json:"campaignId"`
	CampaignName string              `json:"campaignName"`
	Type         string              `json:"type"`
	Entries      []CampaignEntryItem `json:"entries"`
}

type CampaignEntryItem struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type CreateLeadRequest struct {
	Number        string                `json:"number,omitempty" example:"5511987654321"`
	Name          string                `json:"name,omitempty" example:"Maria Souza"`
	Nickname      string                `json:"nickname,omitempty" example:"Mari"`
	Email         string                `json:"email,omitempty" example:"maria@exemplo.com.br"`
	BirthDate     string                `json:"birthDate,omitempty" example:"1990-04-21"`
	WhatsAppOptIn *bool                 `json:"whatsappOptIn,omitempty"`
	Phones        []ContactPhoneRequest `json:"phones,omitempty"`
	Addresses     []AddressRequest      `json:"addresses,omitempty"`
	CustomFields  map[string]any        `json:"customFields,omitempty"`
}

func (r CreateLeadRequest) toDomain() leaddomain.Draft {
	return leaddomain.Draft{Number: r.Number, Name: r.Name, Nickname: r.Nickname, Email: r.Email, BirthDate: r.BirthDate, WhatsAppOptIn: r.WhatsAppOptIn,
		Phones: phoneInputs(r.Phones), Addresses: addressInputs(r.Addresses), CustomFields: r.CustomFields}
}

type UpdateLeadRequest struct {
	Number        *string                `json:"number,omitempty" example:"5511987654321"`
	Name          *string                `json:"name,omitempty" example:"Maria Souza"`
	Nickname      *string                `json:"nickname,omitempty" example:"Mari"`
	Email         *string                `json:"email,omitempty" example:"maria@exemplo.com.br"`
	BirthDate     *string                `json:"birthDate,omitempty" example:"1990-04-21"`
	WhatsAppOptIn *bool                  `json:"whatsappOptIn,omitempty"`
	Phones        *[]ContactPhoneRequest `json:"phones,omitempty"`
	Addresses     *[]AddressRequest      `json:"addresses,omitempty"`
	CustomFields  map[string]any         `json:"customFields,omitempty"`
}

func (r UpdateLeadRequest) toDomain() leaddomain.Edit {
	e := leaddomain.Edit{Number: r.Number, Name: r.Name, Nickname: r.Nickname, Email: r.Email, BirthDate: r.BirthDate, WhatsAppOptIn: r.WhatsAppOptIn, CustomFields: r.CustomFields}
	if r.Phones != nil {
		phones := phoneInputs(*r.Phones)
		e.Phones = &phones
	}
	if r.Addresses != nil {
		addresses := addressInputs(*r.Addresses)
		e.Addresses = &addresses
	}
	return e
}

type RenameLeadRequest struct {
	Name *string `json:"name"`
}

type SetLeadOwnerRequest struct {
	OwnerID *string `json:"ownerId" example:"4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"`
}

type LeadVersionConflictResponse struct {
	Code    string             `json:"code" example:"version_conflict"`
	Message string             `json:"message"`
	Current LeadRecordResponse `json:"current"`
}

func fmtRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtRFC3339(*t)
	return &s
}

func fmtNonZero(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmtRFC3339(t)
}

func toLeadRecord(l *leaddomain.Lead, now time.Time) LeadRecordResponse {
	out := LeadRecordResponse{
		ID:                l.ID,
		WorkspaceID:       l.WorkspaceID,
		Number:            l.Number,
		Name:              l.Name,
		RealName:          l.RealName(),
		NameSource:        string(l.NameSource),
		Nickname:          l.Nickname,
		Email:             l.Email,
		Age:               l.Age(now),
		Source:            string(l.Source),
		Owner:             l.Owner,
		OptedOutAt:        fmtTimePtr(l.OptedOutAt),
		OptOutSource:      string(l.OptOutSource),
		Blocked:           l.Blocked,
		BlockedBy:         l.BlockedBy,
		ProfilePictureURL: l.ProfilePictureURL,
		Phones:            phoneResponses(l.Phones),
		Addresses:         addressResponses(l.Addresses),
		CustomFields:      l.CustomFields,
		RelativesCount:    l.RelativesCount,
		ReferredCount:     l.ReferredCount,
		Version:           l.Version,
		CreatedAt:         fmtNonZero(l.CreatedAt),
		UpdatedAt:         fmtNonZero(l.UpdatedAt),
	}
	if l.BirthDate != nil {
		out.BirthDate = l.BirthDate.String()
	}
	if l.WhatsAppOptIn != nil {
		out.WhatsAppOptIn = &LeadConsentResponse{GrantedAt: fmtRFC3339(l.WhatsAppOptIn.GrantedAt), Source: string(l.WhatsAppOptIn.Source), Purpose: l.WhatsAppOptIn.Purpose}
	}
	if l.Blocked && !l.BlockedAt.IsZero() {
		blockedAt := fmtRFC3339(l.BlockedAt)
		out.BlockedAt = &blockedAt
	}
	return out
}

func toLeadListItem(lws *leaddomain.LeadWithSummary, now time.Time) LeadListResponseItem {
	l := lws.Lead
	s := lws.Summary
	item := LeadListResponseItem{
		ID:                 l.ID,
		WorkspaceID:        l.WorkspaceID,
		Number:             l.Number,
		Name:               l.Name,
		RealName:           l.RealName(),
		ProfilePictureURL:  l.ProfilePictureURL,
		Age:                l.Age(now),
		Blocked:            l.Blocked,
		Version:            l.Version,
		CreatedAt:          fmtRFC3339(l.CreatedAt),
		UpdatedAt:          fmtRFC3339(l.UpdatedAt),
		WhatsAppCampaigns:  s.WhatsAppCampaigns,
		TotalCampaigns:     s.TotalCampaigns,
		LastActivityAt:     fmtTimePtr(s.LastActivityAt),
		WhatsAppWindowOpen: s.WhatsAppWindowOpen,
		WindowExpiresAt:    fmtTimePtr(s.WindowExpiresAt),
		Memories:           s.Memories,
		LastMemoryAt:       fmtTimePtr(s.LastMemoryAt),
		Owner:              l.Owner,
		OwnerName:          lws.OwnerName,
		Phones:             phoneResponses(l.Phones),
		CustomFields:       l.CustomFields,
		RelativesCount:     l.RelativesCount,
		ReferredCount:      l.ReferredCount,
	}
	if item.Phones == nil {
		item.Phones = []ContactPhoneResponse{}
	}
	if primary := l.PrimaryAddress(); primary != nil {
		item.PrimaryAddress = &addressResponses([]leaddomain.Address{*primary})[0]
	}
	if l.Blocked && !l.BlockedAt.IsZero() {
		blockedAt := fmtRFC3339(l.BlockedAt)
		item.BlockedAt = &blockedAt
	}
	return item
}

type SeedConversationsSpec struct {
	Bodies      []string            `json:"bodies"`
	MaxMessages int                 `json:"maxMessages" example:"4"`
	Context     string              `json:"context,omitempty"`
	Attachment  *SeedAttachmentSpec `json:"attachment,omitempty"`
}

type SeedAttachmentSpec struct {
	MediaID string `json:"mediaId"`
	Kind    string `json:"kind" example:"image"`
}

func (a *SeedAttachmentSpec) toDomain() *unofficial_whatsapp.SeedAttachment {
	if a == nil {
		return nil
	}
	return &unofficial_whatsapp.SeedAttachment{
		MediaID: a.MediaID,
		Kind:    unofficial_whatsapp.MediaKind(a.Kind),
	}
}

func (s *SeedConversationsSpec) toDomain() *unofficial_whatsapp.SeedScript {
	if s == nil {
		return nil
	}
	return &unofficial_whatsapp.SeedScript{
		Bodies:      s.Bodies,
		MaxMessages: s.MaxMessages,
		Context:     s.Context,
		Attachment:  s.Attachment.toDomain(),
	}
}

type AnonymizeLeadResponse struct {
	LeadID       string           `json:"leadId"`
	Version      int64            `json:"version" example:"6"`
	AnonymizedAt string           `json:"anonymizedAt" example:"2026-10-08T12:00:00Z"`
	Erased       map[string]int64 `json:"erased"`
}

func toAnonymizeResponse(e leaddomain.Erasure) AnonymizeLeadResponse {
	erased := make(map[string]int64, len(e.Rows))
	for target, rows := range e.Rows {
		erased[string(target)] = rows
	}
	return AnonymizeLeadResponse{LeadID: e.LeadID, Version: e.Version, AnonymizedAt: fmtRFC3339(e.At), Erased: erased}
}
