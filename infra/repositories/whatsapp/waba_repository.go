package whatsapp_repository

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/shared"
	"vozko/domain/whatsapp/waba"
	"vozko/infra/database/schema"
)

type wabaRepository struct {
	db *gorm.DB
}

func NewWABARepository(db *gorm.DB) waba.Repository {
	return &wabaRepository{db: db}
}

func (r *wabaRepository) Create(account *waba.WhatsAppBusinessAccount) error {
	record := mapWABAToSchema(account)
	if err := r.db.Create(&record).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return waba.ErrWABAAlreadyExists
		}
		return err
	}
	account.ID = record.ID
	return nil
}

func (r *wabaRepository) Update(id string, account *waba.WhatsAppBusinessAccount) error {
	update := map[string]interface{}{
		"name":                         account.Name,
		"business_portfolio_id":        account.BusinessPortfolioID,
		"account_review_status":        account.AccountReviewStatus,
		"business_verification_status": account.BusinessVerificationStatus,
		"ownership_type":               account.OwnershipType,
		"messaging_limit_tier":         account.MessagingLimitTier,
		"currency":                     account.Currency,
		"timezone_id":                  account.TimezoneId,
		"message_template_namespace":   account.MessageTemplateNamespace,
		"status":                       account.Status,
		"country":                      account.Country,
	}
	if account.AccessToken != "" {
		update["access_token"] = account.AccessToken
	}
	if strings.TrimSpace(account.Provider) != "" && account.Provider != "meta" {
		update["provider"] = account.Provider
	}
	if account.Dialog360ClientID != "" {
		update["dialog360_client_id"] = account.Dialog360ClientID
	}
	result := r.db.Model(&schema.WhatsAppBusinessAccount{}).Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return waba.ErrWABANotFound
	}
	return nil
}

func (r *wabaRepository) Delete(id string) error {
	result := r.db.Delete(&schema.WhatsAppBusinessAccount{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return waba.ErrWABANotFound
	}
	return nil
}

func (r *wabaRepository) ClearAccessToken(id string) error {
	result := r.db.Model(&schema.WhatsAppBusinessAccount{}).Where("id = ?", id).Update("access_token", "")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return waba.ErrWABANotFound
	}
	return nil
}

func (r *wabaRepository) FindByID(id string) (*waba.WhatsAppBusinessAccount, error) {
	var record schema.WhatsAppBusinessAccount
	if err := r.db.Where("id = ?", id).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, waba.ErrWABANotFound
		}
		return nil, err
	}
	return r.enrichWithCounts(mapWABAToDomain(&record))
}

func (r *wabaRepository) FindByMetaWABAId(metaWABAId string) (*waba.WhatsAppBusinessAccount, error) {
	var record schema.WhatsAppBusinessAccount
	if err := r.db.Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).Where("meta_waba_id = ?", metaWABAId).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, waba.ErrWABANotFound
		}
		return nil, err
	}
	return r.enrichWithCounts(mapWABAToDomain(&record))
}

func (r *wabaRepository) List(input waba.ListInput) (*shared.PaginatedResult[*waba.WhatsAppBusinessAccount], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	var total int64
	if err := r.db.Model(&schema.WhatsAppBusinessAccount{}).Count(&total).Error; err != nil {
		return nil, err
	}

	var records []schema.WhatsAppBusinessAccount
	if err := r.db.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*waba.WhatsAppBusinessAccount, 0, len(records))
	for i := range records {
		item := mapWABAToDomain(&records[i])
		item, _ = r.enrichWithCounts(item)
		items = append(items, item)
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *wabaRepository) enrichWithCounts(account *waba.WhatsAppBusinessAccount) (*waba.WhatsAppBusinessAccount, error) {
	var phoneCount int64
	r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Where("waba_id = ?", account.MetaWABAId).
		Count(&phoneCount)
	account.PhoneCount = int(phoneCount)

	var templateCount int64
	r.db.Model(&schema.WhatsAppTemplate{}).
		Where("waba_id = ?", account.MetaWABAId).
		Count(&templateCount)
	account.TemplateCount = int(templateCount)

	return account, nil
}

func mapWABAToSchema(account *waba.WhatsAppBusinessAccount) *schema.WhatsAppBusinessAccount {
	provider := account.Provider
	if strings.TrimSpace(provider) == "" {
		provider = "meta"
	}
	return &schema.WhatsAppBusinessAccount{
		ID:                         account.ID,
		Provider:                   provider,
		MetaWABAId:                 account.MetaWABAId,
		Name:                       account.Name,
		BusinessPortfolioID:        account.BusinessPortfolioID,
		Dialog360ClientID:          account.Dialog360ClientID,
		AccessToken:                account.AccessToken,
		AccountReviewStatus:        account.AccountReviewStatus,
		BusinessVerificationStatus: account.BusinessVerificationStatus,
		OwnershipType:              account.OwnershipType,
		MessagingLimitTier:         account.MessagingLimitTier,
		Currency:                   account.Currency,
		TimezoneId:                 account.TimezoneId,
		MessageTemplateNamespace:   account.MessageTemplateNamespace,
		Status:                     account.Status,
		Country:                    account.Country,
	}
}

func mapWABAToDomain(record *schema.WhatsAppBusinessAccount) *waba.WhatsAppBusinessAccount {
	provider := record.Provider
	if strings.TrimSpace(provider) == "" {
		provider = "meta"
	}
	return &waba.WhatsAppBusinessAccount{
		ID:                         record.ID,
		Provider:                   provider,
		MetaWABAId:                 record.MetaWABAId,
		Name:                       record.Name,
		BusinessPortfolioID:        record.BusinessPortfolioID,
		Dialog360ClientID:          record.Dialog360ClientID,
		AccessToken:                record.AccessToken,
		AccountReviewStatus:        record.AccountReviewStatus,
		BusinessVerificationStatus: record.BusinessVerificationStatus,
		OwnershipType:              record.OwnershipType,
		MessagingLimitTier:         record.MessagingLimitTier,
		Currency:                   record.Currency,
		TimezoneId:                 record.TimezoneId,
		MessageTemplateNamespace:   record.MessageTemplateNamespace,
		Status:                     record.Status,
		Country:                    record.Country,
		CreatedAt:                  record.CreatedAt,
		UpdatedAt:                  record.UpdatedAt,
	}
}
