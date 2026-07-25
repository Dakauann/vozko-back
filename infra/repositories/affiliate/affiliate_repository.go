package affiliate_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/affiliate"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) affiliate.Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, a *affiliate.Affiliate) error {
	row := toAffiliateSchema(a)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	*a = *toAffiliateDomain(row)
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*affiliate.Affiliate, error) {
	var row schema.Affiliate
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	return firstAffiliate(&row, err)
}

func (r *Repository) GetByUserID(ctx context.Context, userID string) (*affiliate.Affiliate, error) {
	var row schema.Affiliate
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error
	return firstAffiliate(&row, err)
}

func (r *Repository) GetByCode(ctx context.Context, code string) (*affiliate.Affiliate, error) {
	var row schema.Affiliate
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&row).Error
	return firstAffiliate(&row, err)
}

func (r *Repository) Update(ctx context.Context, a *affiliate.Affiliate) error {
	row := toAffiliateSchema(a)
	result := r.db.WithContext(ctx).Model(&schema.Affiliate{}).Where("id = ?", a.ID).Updates(map[string]interface{}{
		"code":            row.Code,
		"brand_name":      row.BrandName,
		"brand_logo_url":  row.BrandLogoURL,
		"asaas_wallet_id": row.AsaasWalletID,
		"commission_pct":  row.CommissionPct,
		"tier":            row.Tier,
		"active":          row.Active,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return affiliate.ErrAffiliateNotFound
	}
	return nil
}

func (r *Repository) List(ctx context.Context, page, pageSize int) ([]affiliate.Affiliate, int64, error) {
	pag := shared.NormalizePagination(shared.Pagination{Page: page, PageSize: pageSize})

	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.Affiliate{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []schema.Affiliate
	if err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(pag.PageSize).
		Offset(pag.Offset()).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	result := make([]affiliate.Affiliate, 0, len(rows))
	for i := range rows {
		result = append(result, *toAffiliateDomain(&rows[i]))
	}
	return result, total, nil
}

func (r *Repository) CreateReferral(ctx context.Context, ref *affiliate.Referral) error {
	row := toReferralSchema(ref)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	*ref = *toReferralDomain(row)
	return nil
}

func (r *Repository) GetReferralByWorkspaceID(ctx context.Context, workspaceID string) (*affiliate.Referral, error) {
	var row schema.AffiliateReferral
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toReferralDomain(&row), nil
}

func (r *Repository) ListReferralsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Referral, int64, error) {
	pag := shared.NormalizePagination(shared.Pagination{Page: page, PageSize: pageSize})

	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.AffiliateReferral{}).
		Where("affiliate_id = ?", affiliateID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []schema.AffiliateReferral
	if err := r.db.WithContext(ctx).
		Where("affiliate_id = ?", affiliateID).
		Order("referred_at DESC").
		Limit(pag.PageSize).
		Offset(pag.Offset()).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	result := make([]affiliate.Referral, 0, len(rows))
	for i := range rows {
		result = append(result, *toReferralDomain(&rows[i]))
	}
	return result, total, nil
}

func (r *Repository) CountReferralsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&schema.AffiliateReferral{}).
		Where("affiliate_id = ?", affiliateID).
		Count(&total).Error
	return total, err
}

func (r *Repository) CreateEarning(ctx context.Context, e *affiliate.Earning) error {
	row := toEarningSchema(e)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	*e = *toEarningDomain(row)
	return nil
}

func (r *Repository) GetEarningByInvoiceID(ctx context.Context, invoiceID string) (*affiliate.Earning, error) {
	var row schema.AffiliateEarning
	err := r.db.WithContext(ctx).Where("invoice_id = ?", invoiceID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toEarningDomain(&row), nil
}

func (r *Repository) ListEarningsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Earning, int64, error) {
	pag := shared.NormalizePagination(shared.Pagination{Page: page, PageSize: pageSize})

	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.AffiliateEarning{}).
		Where("affiliate_id = ?", affiliateID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []schema.AffiliateEarning
	if err := r.db.WithContext(ctx).
		Where("affiliate_id = ?", affiliateID).
		Order("created_at DESC").
		Limit(pag.PageSize).
		Offset(pag.Offset()).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	result := make([]affiliate.Earning, 0, len(rows))
	for i := range rows {
		result = append(result, *toEarningDomain(&rows[i]))
	}
	return result, total, nil
}

func (r *Repository) SumEarningsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	var total *int64
	err := r.db.WithContext(ctx).Model(&schema.AffiliateEarning{}).
		Where("affiliate_id = ?", affiliateID).
		Select("COALESCE(SUM(amount_micros), 0)").
		Row().Scan(&total)
	if err != nil {
		return 0, err
	}
	if total == nil {
		return 0, nil
	}
	return *total, nil
}

func (r *Repository) SumEarningsSince(ctx context.Context, affiliateID string, sinceUnix int64) (int64, error) {
	var total *int64
	err := r.db.WithContext(ctx).Model(&schema.AffiliateEarning{}).
		Where("affiliate_id = ? AND extract(epoch from created_at) >= ?", affiliateID, sinceUnix).
		Select("COALESCE(SUM(amount_micros), 0)").
		Row().Scan(&total)
	if err != nil {
		return 0, err
	}
	if total == nil {
		return 0, nil
	}
	return *total, nil
}

func firstAffiliate(row *schema.Affiliate, err error) (*affiliate.Affiliate, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, affiliate.ErrAffiliateNotFound
		}
		return nil, err
	}
	return toAffiliateDomain(row), nil
}

func toAffiliateSchema(a *affiliate.Affiliate) *schema.Affiliate {
	tier := string(a.Tier)
	if tier == "" {
		tier = string(affiliate.TierAffiliate)
	}
	return &schema.Affiliate{
		ID:            a.ID,
		UserID:        a.UserID,
		Code:          a.Code,
		BrandName:     a.BrandName,
		BrandLogoURL:  a.BrandLogoURL,
		AsaasWalletID: a.AsaasWalletID,
		CommissionPct: a.CommissionPct,
		Tier:          tier,
		Active:        a.Active,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

func toAffiliateDomain(row *schema.Affiliate) *affiliate.Affiliate {
	tier := affiliate.Tier(row.Tier)
	if tier == "" {
		tier = affiliate.TierAffiliate
	}
	return &affiliate.Affiliate{
		ID:            row.ID,
		UserID:        row.UserID,
		Code:          row.Code,
		BrandName:     row.BrandName,
		BrandLogoURL:  row.BrandLogoURL,
		AsaasWalletID: row.AsaasWalletID,
		CommissionPct: row.CommissionPct,
		Tier:          tier,
		Active:        row.Active,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func toReferralSchema(r *affiliate.Referral) *schema.AffiliateReferral {
	return &schema.AffiliateReferral{
		ID:          r.ID,
		AffiliateID: r.AffiliateID,
		WorkspaceID: r.WorkspaceID,
		ReferredAt:  r.ReferredAt,
	}
}

func toReferralDomain(row *schema.AffiliateReferral) *affiliate.Referral {
	return &affiliate.Referral{
		ID:          row.ID,
		AffiliateID: row.AffiliateID,
		WorkspaceID: row.WorkspaceID,
		ReferredAt:  row.ReferredAt,
	}
}

func toEarningSchema(e *affiliate.Earning) *schema.AffiliateEarning {
	return &schema.AffiliateEarning{
		ID:                 e.ID,
		AffiliateID:        e.AffiliateID,
		InvoiceID:          e.InvoiceID,
		WorkspaceID:        e.WorkspaceID,
		AmountMicros:       e.AmountMicros,
		ExchangeRateMicros: e.ExchangeRateMicros,
		Purpose:            e.Purpose,
		Status:             e.Status,
		CreatedAt:          e.CreatedAt,
	}
}

func toEarningDomain(row *schema.AffiliateEarning) *affiliate.Earning {
	return &affiliate.Earning{
		ID:                 row.ID,
		AffiliateID:        row.AffiliateID,
		InvoiceID:          row.InvoiceID,
		WorkspaceID:        row.WorkspaceID,
		AmountMicros:       row.AmountMicros,
		ExchangeRateMicros: row.ExchangeRateMicros,
		Purpose:            row.Purpose,
		Status:             row.Status,
		CreatedAt:          row.CreatedAt,
	}
}
