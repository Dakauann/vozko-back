package affiliate

import (
	"time"

	"vozko/delivery/http/response"
	affiliatedomain "vozko/domain/affiliate"
)

type RegisterAffiliateRequest struct {
	AsaasWalletID string `json:"asaasWalletId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	BrandName     string `json:"brandName" example:"Minha Marca"`
	BrandLogoURL  string `json:"brandLogoUrl" example:"https://cdn.exemplo.com/logo.png"`
	Code          string `json:"code" example:"MINHA-MARCA"`
}

type UpdateAffiliateRequest struct {
	AsaasWalletID *string `json:"asaasWalletId,omitempty" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	BrandName     *string `json:"brandName,omitempty" example:"Minha Marca"`
	BrandLogoURL  *string `json:"brandLogoUrl,omitempty" example:"https://cdn.exemplo.com/logo.png"`
}

type AdminUpdateAffiliateRequest struct {
	CommissionPct *float64 `json:"commissionPct,omitempty" example:"0.05"`
	Active        *bool    `json:"active,omitempty" example:"true"`
	Tier          *string  `json:"tier,omitempty" example:"affiliate"`
}

type AffiliateResponse struct {
	ID            string    `json:"id" example:"aff_a1b2c3"`
	UserID        string    `json:"userId" example:"usr_a1b2c3"`
	Code          string    `json:"code" example:"MINHA-MARCA"`
	BrandName     string    `json:"brandName" example:"Minha Marca"`
	BrandLogoURL  string    `json:"brandLogoUrl" example:"https://cdn.exemplo.com/logo.png"`
	AsaasWalletID string    `json:"asaasWalletId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	CommissionPct float64   `json:"commissionPct" example:"0.05"`
	Tier          string    `json:"tier" example:"affiliate"`
	Active        bool      `json:"active" example:"true"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type StatsResponse struct {
	TotalReferrals     int64 `json:"totalReferrals" example:"42"`
	TotalEarningMicros int64 `json:"totalEarningMicros" example:"1500000"`
	MonthEarningMicros int64 `json:"monthEarningMicros" example:"250000"`
}

type AffiliateProfileResponse struct {
	Affiliate *AffiliateResponse `json:"affiliate"`
	Stats     *StatsResponse     `json:"stats"`
}

type ReferralResponse struct {
	ID          string    `json:"id" example:"ref_a1b2c3"`
	AffiliateID string    `json:"affiliateId" example:"aff_a1b2c3"`
	WorkspaceID string    `json:"workspaceId" example:"ws_a1b2c3"`
	ReferredAt  time.Time `json:"referredAt"`
}

type EarningResponse struct {
	ID                 string    `json:"id" example:"earn_a1b2c3"`
	AffiliateID        string    `json:"affiliateId" example:"aff_a1b2c3"`
	InvoiceID          string    `json:"invoiceId" example:"inv_a1b2c3"`
	WorkspaceID        string    `json:"workspaceId" example:"ws_a1b2c3"`
	AmountMicros       int64     `json:"amountMicros" example:"1500000"`
	ExchangeRateMicros int64     `json:"exchangeRateMicros" example:"5200000"`
	Purpose            string    `json:"purpose" example:"subscription"`
	Status             string    `json:"status" example:"pending"`
	CreatedAt          time.Time `json:"createdAt"`
}

type ReferralValidationResponse struct {
	Valid        bool   `json:"valid" example:"true"`
	Code         string `json:"code,omitempty" example:"MINHA-MARCA"`
	BrandName    string `json:"brandName,omitempty" example:"Minha Marca"`
	BrandLogoURL string `json:"brandLogoUrl,omitempty" example:"https://cdn.exemplo.com/logo.png"`
}

type ReferralListResponse struct {
	Data []ReferralResponse      `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

type EarningListResponse struct {
	Data []EarningResponse       `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

func toAffiliateResponse(a *affiliatedomain.Affiliate) *AffiliateResponse {
	if a == nil {
		return nil
	}
	return &AffiliateResponse{
		ID:            a.ID,
		UserID:        a.UserID,
		Code:          a.Code,
		BrandName:     a.BrandName,
		BrandLogoURL:  a.BrandLogoURL,
		AsaasWalletID: a.AsaasWalletID,
		CommissionPct: a.CommissionPct,
		Tier:          string(a.Tier),
		Active:        a.Active,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

func toStatsResponse(s *affiliatedomain.Stats) *StatsResponse {
	if s == nil {
		return nil
	}
	return &StatsResponse{
		TotalReferrals:     s.TotalReferrals,
		TotalEarningMicros: s.TotalEarningMicros,
		MonthEarningMicros: s.MonthEarningMicros,
	}
}

func toAffiliateProfileResponse(p *affiliatedomain.AffiliateProfileWithStats) *AffiliateProfileResponse {
	if p == nil {
		return nil
	}
	return &AffiliateProfileResponse{
		Affiliate: toAffiliateResponse(p.Affiliate),
		Stats:     toStatsResponse(p.Stats),
	}
}

func toReferralValidationResponse(res *affiliatedomain.ReferralValidationResult) *ReferralValidationResponse {
	if res == nil {
		return nil
	}
	return &ReferralValidationResponse{
		Valid:        res.Valid,
		Code:         res.Code,
		BrandName:    res.BrandName,
		BrandLogoURL: res.BrandLogoURL,
	}
}

func toReferralResponses(items []affiliatedomain.Referral) []ReferralResponse {
	if items == nil {
		return nil
	}
	out := make([]ReferralResponse, len(items))
	for i, it := range items {
		out[i] = ReferralResponse{
			ID:          it.ID,
			AffiliateID: it.AffiliateID,
			WorkspaceID: it.WorkspaceID,
			ReferredAt:  it.ReferredAt,
		}
	}
	return out
}

func toEarningResponses(items []affiliatedomain.Earning) []EarningResponse {
	if items == nil {
		return nil
	}
	out := make([]EarningResponse, len(items))
	for i, it := range items {
		out[i] = EarningResponse{
			ID:                 it.ID,
			AffiliateID:        it.AffiliateID,
			InvoiceID:          it.InvoiceID,
			WorkspaceID:        it.WorkspaceID,
			AmountMicros:       it.AmountMicros,
			ExchangeRateMicros: it.ExchangeRateMicros,
			Purpose:            it.Purpose,
			Status:             it.Status,
			CreatedAt:          it.CreatedAt,
		}
	}
	return out
}

func toAffiliateResponses(items []affiliatedomain.Affiliate) []AffiliateResponse {
	if items == nil {
		return nil
	}
	out := make([]AffiliateResponse, len(items))
	for i, it := range items {
		aff := it
		out[i] = *toAffiliateResponse(&aff)
	}
	return out
}
