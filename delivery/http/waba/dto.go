package waba

import (
	"time"

	"vozko/delivery/http/response"
	wabadomain "vozko/domain/whatsapp/waba"
)

type WABAResponse struct {
	ID                         string    `json:"id" example:"waba_a1b2c3"`
	Provider                   string    `json:"provider,omitempty" example:"meta"`
	MetaWABAId                 string    `json:"metaWabaId" example:"102290129340398"`
	Name                       string    `json:"name,omitempty" example:"Clínica Sorriso"`
	BusinessPortfolioID        string    `json:"businessPortfolioId,omitempty" example:"178451236789012"`
	Dialog360ClientID          string    `json:"dialog360ClientId,omitempty" example:"abcd1234efgh5678"`
	AccountReviewStatus        string    `json:"accountReviewStatus,omitempty" example:"APPROVED"`
	BusinessVerificationStatus string    `json:"businessVerificationStatus,omitempty" example:"verified"`
	OwnershipType              string    `json:"ownershipType,omitempty" example:"CLIENT"`
	MessagingLimitTier         string    `json:"messagingLimitTier,omitempty" example:"TIER_1K"`
	Currency                   string    `json:"currency,omitempty" example:"BRL"`
	TimezoneId                 string    `json:"timezoneId,omitempty" example:"America/Sao_Paulo"`
	MessageTemplateNamespace   string    `json:"messageTemplateNamespace,omitempty" example:"a1b2c3d4_namespace"`
	Status                     string    `json:"status,omitempty" example:"CONNECTED"`
	Country                    string    `json:"country,omitempty" example:"BR"`
	PhoneCount                 int       `json:"phoneCount" example:"2"`
	TemplateCount              int       `json:"templateCount" example:"14"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

type WABAListResponse struct {
	Data []WABAResponse          `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

func toWABAResponse(a *wabadomain.WhatsAppBusinessAccount) *WABAResponse {
	if a == nil {
		return nil
	}
	return &WABAResponse{
		ID:                         a.ID,
		Provider:                   a.Provider,
		MetaWABAId:                 a.MetaWABAId,
		Name:                       a.Name,
		BusinessPortfolioID:        a.BusinessPortfolioID,
		Dialog360ClientID:          a.Dialog360ClientID,
		AccountReviewStatus:        a.AccountReviewStatus,
		BusinessVerificationStatus: a.BusinessVerificationStatus,
		OwnershipType:              a.OwnershipType,
		MessagingLimitTier:         a.MessagingLimitTier,
		Currency:                   a.Currency,
		TimezoneId:                 a.TimezoneId,
		MessageTemplateNamespace:   a.MessageTemplateNamespace,
		Status:                     a.Status,
		Country:                    a.Country,
		PhoneCount:                 a.PhoneCount,
		TemplateCount:              a.TemplateCount,
		CreatedAt:                  a.CreatedAt,
		UpdatedAt:                  a.UpdatedAt,
	}
}

func toWABAResponses(items []*wabadomain.WhatsAppBusinessAccount) []*WABAResponse {
	if items == nil {
		return nil
	}
	out := make([]*WABAResponse, len(items))
	for i, it := range items {
		out[i] = toWABAResponse(it)
	}
	return out
}
