package affiliate

import "context"

type Repository interface {
	Create(ctx context.Context, affiliate *Affiliate) error
	GetByID(ctx context.Context, id string) (*Affiliate, error)
	GetByUserID(ctx context.Context, userID string) (*Affiliate, error)
	GetByCode(ctx context.Context, code string) (*Affiliate, error)
	Update(ctx context.Context, affiliate *Affiliate) error
	List(ctx context.Context, page, pageSize int) ([]Affiliate, int64, error)

	CreateReferral(ctx context.Context, referral *Referral) error
	GetReferralByWorkspaceID(ctx context.Context, workspaceID string) (*Referral, error)
	ListReferralsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]Referral, int64, error)
	CountReferralsByAffiliateID(ctx context.Context, affiliateID string) (int64, error)

	CreateEarning(ctx context.Context, earning *Earning) error
	GetEarningByInvoiceID(ctx context.Context, invoiceID string) (*Earning, error)
	ListEarningsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]Earning, int64, error)
	SumEarningsByAffiliateID(ctx context.Context, affiliateID string) (int64, error)
	SumEarningsSince(ctx context.Context, affiliateID string, sinceUnix int64) (int64, error)
}
