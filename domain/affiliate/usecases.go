package affiliate

import "context"

type RegisterAffiliateInput struct {
	UserID        string
	Code          string
	BrandName     string
	BrandLogoURL  string
	AsaasWalletID string
}

type UpdateAffiliateProfileInput struct {
	UserID        string
	BrandName     *string
	BrandLogoURL  *string
	AsaasWalletID *string
}

type AdminUpdateAffiliateInput struct {
	ID            string
	CommissionPct *float64
	Active        *bool

	Tier *Tier
}

type TrackReferralInput struct {
	Code                 string
	WorkspaceID          string
	WorkspaceOwnerUserID string
}

type RecordEarningInput struct {
	WorkspaceID        string
	InvoiceID          string
	AmountUSDMicros    int64
	ExchangeRateMicros int64
	Purpose            string
}

type ReferralValidationResult struct {
	Valid        bool   `json:"valid"`
	Code         string `json:"code,omitempty"`
	BrandName    string `json:"brandName,omitempty"`
	BrandLogoURL string `json:"brandLogoUrl,omitempty"`
}

type AffiliateProfileWithStats struct {
	Affiliate *Affiliate `json:"affiliate"`
	Stats     *Stats     `json:"stats"`
}

type RegisterAffiliateUseCase interface {
	Execute(ctx context.Context, input RegisterAffiliateInput) (*Affiliate, error)
}

type GetMyAffiliateUseCase interface {
	Execute(ctx context.Context, userID string) (*AffiliateProfileWithStats, error)
}

type UpdateMyAffiliateUseCase interface {
	Execute(ctx context.Context, input UpdateAffiliateProfileInput) (*Affiliate, error)
}

type ListReferralsUseCase interface {
	Execute(ctx context.Context, userID string, page, pageSize int) ([]Referral, int64, error)
}

type ListEarningsUseCase interface {
	Execute(ctx context.Context, userID string, page, pageSize int) ([]Earning, int64, error)
}

type GetAffiliateStatsUseCase interface {
	Execute(ctx context.Context, affiliateID string) (*Stats, error)
}

type ValidateReferralCodeUseCase interface {
	Execute(ctx context.Context, code string) (*ReferralValidationResult, error)
}

type TrackReferralUseCase interface {
	Execute(ctx context.Context, input TrackReferralInput) (*Referral, error)
}

type RecordEarningUseCase interface {
	Execute(ctx context.Context, input RecordEarningInput) (*Earning, error)
}

type AdminListAffiliatesUseCase interface {
	Execute(ctx context.Context, page, pageSize int) ([]Affiliate, int64, error)
}

type AdminGetAffiliateUseCase interface {
	Execute(ctx context.Context, id string) (*AffiliateProfileWithStats, error)
}

type AdminUpdateAffiliateUseCase interface {
	Execute(ctx context.Context, input AdminUpdateAffiliateInput) (*Affiliate, error)
}
