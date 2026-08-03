package whatsapp_repository

import (
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

func encryptedOrNull(s string) piigorm.EncryptedString {
	if s == "" {
		return piigorm.Null()
	}
	return piigorm.NewEncrypted(s)
}

type businessPhoneRepository struct {
	db *gorm.DB
}

func NewBusinessPhoneRepository(db *gorm.DB) businessphone.Repository {
	return &businessPhoneRepository{db: db}
}

func (r *businessPhoneRepository) Create(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	record := mapPhoneToSchema(phone)
	if err := r.db.Create(&record).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return businessphone.ErrPhoneNumberAlreadyExists
		}
		return err
	}
	phone.ID = record.ID
	return nil
}

func (r *businessPhoneRepository) Update(phoneID string, phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	update := map[string]interface{}{
		"verified_name":            phone.VerifiedName,
		"status":                   string(phone.Status),
		"quality_rating":           string(phone.QualityRating),
		"name_status":              string(phone.NameStatus),
		"code_verification_status": phone.CodeVerificationStatus,
		"is_official_business":     phone.IsOfficialBusiness,
		"business_profile":         mapBusinessProfileToSchema(phone.BusinessProfile),
		"business_portfolio_id":    phone.BusinessPortfolioID,
		// Always written so a successful retry can clear a prior failure reason.
		"onboarding_error": phone.OnboardingError,
	}

	if phone.OwnerWorkspaceID != "" {
		update["owner_workspace_id"] = phone.OwnerWorkspaceID
	} else {
		update["owner_workspace_id"] = gorm.Expr("NULL")
	}
	if phone.OwnerAssignedBy != "" {
		update["owner_assigned_by"] = phone.OwnerAssignedBy
	} else {
		update["owner_assigned_by"] = gorm.Expr("NULL")
	}
	update["owner_assigned_at"] = phone.OwnerAssignedAt
	if phone.AccessToken != "" {
		update["access_token"] = phone.AccessToken
	}
	if phone.Provider.Normalized() != businessphone.ProviderMeta {
		update["provider"] = string(phone.Provider.Normalized())
	}
	if phone.Dialog360ChannelID != "" {
		update["dialog360_channel_id"] = phone.Dialog360ChannelID
	}
	if phone.Dialog360APIKey != "" {
		update["dialog360_api_key"] = piigorm.NewEncrypted(phone.Dialog360APIKey)
	}
	// Persist the display number when set. Written conditionally (like the other
	// vendor-sourced fields below) so an Update that doesn't carry it can't blank it.
	// Without this, Finalize/reconcile set phone.DisplayPhoneNumber in memory but it was
	// never written, dialog360 numbers connected showing no phone number.
	if phone.DisplayPhoneNumber != "" {
		update["display_phone_number"] = phone.DisplayPhoneNumber
	}
	if phone.WABAName != "" {
		update["waba_name"] = phone.WABAName
	}
	if phone.AccountReviewStatus != "" {
		update["account_review_status"] = phone.AccountReviewStatus
	}
	if phone.BusinessVerificationStatus != "" {
		update["business_verification_status"] = phone.BusinessVerificationStatus
	}
	if phone.OwnershipType != "" {
		update["ownership_type"] = phone.OwnershipType
	}
	if phone.MessagingLimitTier != "" {
		update["messaging_limit_tier"] = phone.MessagingLimitTier
	}
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", phoneID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) Delete(phoneID string) error {
	result := r.db.Delete(&schema.WhatsAppBusinessPhoneNumber{}, "id = ?", phoneID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) UpdateCallsEnabled(phoneID string, enabled bool) error {
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", phoneID).Update("calls_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) ClearAccessToken(phoneID string) error {
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", phoneID).Update("access_token", "")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

// ClearOwner detaches a phone from its owning workspace, returning it to the
// unassigned pool. It nulls only the ownership columns and leaves the Meta
// registration and connection state untouched (reversible, the number can be
// re-assigned afterwards).
func (r *businessPhoneRepository) ClearOwner(phoneID string) error {
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", phoneID).Updates(map[string]interface{}{
		"owner_workspace_id": gorm.Expr("NULL"),
		"owner_assigned_by":  gorm.Expr("NULL"),
		"owner_assigned_at":  gorm.Expr("NULL"),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) FindByID(phoneID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var record schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Where("id = ?", phoneID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, businessphone.ErrPhoneNumberNotFound
		}
		return nil, err
	}
	return mapPhoneToDomain(&record), nil
}

func (r *businessPhoneRepository) FindByMetaPhoneNumberID(metaPhoneNumberID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var record schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Where("meta_phone_number_id = ?", metaPhoneNumberID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, businessphone.ErrPhoneNumberNotFound
		}
		return nil, err
	}
	return mapPhoneToDomain(&record), nil
}

func (r *businessPhoneRepository) FindByMetaPhoneNumberIDUnscoped(metaPhoneNumberID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var record schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Unscoped().Where("meta_phone_number_id = ?", metaPhoneNumberID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, businessphone.ErrPhoneNumberNotFound
		}
		return nil, err
	}
	return mapPhoneToDomain(&record), nil
}

func (r *businessPhoneRepository) Restore(id string) error {
	result := r.db.Unscoped().Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", id).Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) FindByDisplayPhoneNumber(displayPhoneNumber string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var record schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Where("display_phone_number = ?", displayPhoneNumber).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, businessphone.ErrPhoneNumberNotFound
		}
		return nil, err
	}
	return mapPhoneToDomain(&record), nil
}

func (r *businessPhoneRepository) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var records []schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Where("waba_id = ?", wabaID).Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0, len(records))
	for i := range records {
		result = append(result, mapPhoneToDomain(&records[i]))
	}
	return result, nil
}

func (r *businessPhoneRepository) List(input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{})
	countQuery = applyPhoneFilters(countQuery, input)
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	// Aggregate the business (WABA) name in the query layer: a single LEFT JOIN on the
	// indexed whatsapp_business_accounts.meta_waba_id, so the client renders the name
	// inline and never refetches/joins WABAs itself. joined_waba_name (authoritative,
	// from the WABA record) overrides the phone's own stale waba_name denormalization.
	dataQuery := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Select(phoneTable + ".*, w.name AS joined_waba_name").
		Joins("LEFT JOIN whatsapp_business_accounts w ON w.meta_waba_id = " + phoneTable + ".waba_id AND w.deleted_at IS NULL").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)
	dataQuery = applyPhoneFilters(dataQuery, input)
	dataQuery = applyPhoneSorts(dataQuery, input.Options.Sorts)

	var records []phoneWithWABA
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0, len(records))
	for i := range records {
		item := mapPhoneToDomain(&records[i].WhatsAppBusinessPhoneNumber)
		if records[i].JoinedWABAName != "" {
			item.WABAName = records[i].JoinedWABAName
		}
		items = append(items, item)
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

// phoneWithWABA is the List scan target: the phone row plus the business name joined
// from whatsapp_business_accounts in the same query.
type phoneWithWABA struct {
	schema.WhatsAppBusinessPhoneNumber
	JoinedWABAName string `gorm:"column:joined_waba_name"`
}

func (r *businessPhoneRepository) UpdateStatus(phoneID string, status businessphone.Status) error {
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Where("id = ?", phoneID).
		Update("status", string(status))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) UpdateBusinessProfile(phoneID string, profile businessphone.BusinessProfile) error {
	result := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Where("id = ?", phoneID).
		Update("business_profile", mapBusinessProfileToSchema(profile))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return businessphone.ErrPhoneNumberNotFound
	}
	return nil
}

func (r *businessPhoneRepository) SyncFromMeta(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	var existing schema.WhatsAppBusinessPhoneNumber
	err := r.db.Where("meta_phone_number_id = ?", phone.MetaPhoneNumberID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.Create(phone)
	}
	if err != nil {
		return err
	}
	phone.ID = existing.ID
	return r.Update(existing.ID, phone)
}

func (r *businessPhoneRepository) BatchUpdate(phones []*businessphone.WhatsAppBusinessPhoneNumber) error {
	if len(phones) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, phone := range phones {
			update := map[string]interface{}{
				"verified_name":            phone.VerifiedName,
				"status":                   string(phone.Status),
				"quality_rating":           string(phone.QualityRating),
				"name_status":              string(phone.NameStatus),
				"code_verification_status": phone.CodeVerificationStatus,
				"is_official_business":     phone.IsOfficialBusiness,
			}
			if err := tx.Model(&schema.WhatsAppBusinessPhoneNumber{}).Where("id = ?", phone.ID).Updates(update).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *businessPhoneRepository) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var records []schema.WhatsAppBusinessPhoneNumber
	if err := r.db.Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0, len(records))
	for i := range records {
		items = append(items, mapPhoneToDomain(&records[i]))
	}
	return items, nil
}

// phoneTable qualifies columns so the filters/sorts stay unambiguous once List LEFT
// JOINs whatsapp_business_accounts (both tables carry id/created_at/updated_at).
const phoneTable = "whatsapp_business_phone_numbers"

func applyPhoneFilters(query *gorm.DB, input businessphone.ListInput) *gorm.DB {
	if input.Status != "" {
		query = query.Where(phoneTable+".status = ?", string(input.Status))
	}
	if input.OwnerWorkspaceID != "" && len(input.AccessPhoneIDs) > 0 {
		query = query.Where(
			query.Where(phoneTable+".owner_workspace_id = ?", input.OwnerWorkspaceID).Or(phoneTable+".id IN ?", input.AccessPhoneIDs),
		)
	} else if input.OwnerWorkspaceID != "" {
		query = query.Where(phoneTable+".owner_workspace_id = ?", input.OwnerWorkspaceID)
	} else if len(input.AccessPhoneIDs) > 0 {
		query = query.Where(phoneTable+".id IN ?", input.AccessPhoneIDs)
	}
	if input.WABAId != "" {
		query = query.Where(phoneTable+".waba_id = ?", input.WABAId)
	}
	if input.QualityRating != "" {
		query = query.Where(phoneTable+".quality_rating = ?", string(input.QualityRating))
	}
	if input.Search != "" {
		like := "%" + input.Search + "%"
		query = query.Where(phoneTable+".display_phone_number ILIKE ? OR "+phoneTable+".verified_name ILIKE ?", like, like)
	}
	return query
}

func applyPhoneSorts(query *gorm.DB, sorts []shared.Sort) *gorm.DB {
	if len(sorts) == 0 {
		return query.Order(phoneTable + ".created_at DESC")
	}
	for _, sort := range sorts {
		field := strings.ToLower(sort.Field)
		direction := "ASC"
		if strings.ToUpper(string(sort.Direction)) == "DESC" {
			direction = "DESC"
		}
		switch field {
		case "display_phone_number", "verified_name", "status", "quality_rating", "created_at", "updated_at":
			query = query.Order(phoneTable + "." + field + " " + direction)
		}
	}
	return query
}

func mapPhoneToSchema(phone *businessphone.WhatsAppBusinessPhoneNumber) *schema.WhatsAppBusinessPhoneNumber {
	return &schema.WhatsAppBusinessPhoneNumber{
		ID:                         phone.ID,
		Provider:                   string(phone.Provider.Normalized()),
		MetaPhoneNumberID:          phone.MetaPhoneNumberID,
		WABAId:                     phone.WABAId,
		OwnerWorkspaceID:           phone.OwnerWorkspaceID,
		OwnerAssignedBy:            phone.OwnerAssignedBy,
		OwnerAssignedAt:            phone.OwnerAssignedAt,
		BusinessPortfolioID:        phone.BusinessPortfolioID,
		DisplayPhoneNumber:         phone.DisplayPhoneNumber,
		VerifiedName:               phone.VerifiedName,
		Status:                     string(phone.Status),
		QualityRating:              string(phone.QualityRating),
		NameStatus:                 string(phone.NameStatus),
		CodeVerificationStatus:     phone.CodeVerificationStatus,
		IsOfficialBusiness:         phone.IsOfficialBusiness,
		BusinessProfile:            mapBusinessProfileToSchema(phone.BusinessProfile),
		AccessToken:                phone.AccessToken,
		Dialog360ChannelID:         phone.Dialog360ChannelID,
		Dialog360APIKey:            encryptedOrNull(phone.Dialog360APIKey),
		OnboardingError:            phone.OnboardingError,
		WABAName:                   phone.WABAName,
		AccountReviewStatus:        phone.AccountReviewStatus,
		BusinessVerificationStatus: phone.BusinessVerificationStatus,
		OwnershipType:              phone.OwnershipType,
		MessagingLimitTier:         phone.MessagingLimitTier,
	}
}

func mapBusinessProfileToSchema(profile businessphone.BusinessProfile) schema.WhatsAppBusinessProfileJSON {
	return schema.WhatsAppBusinessProfileJSON{
		About:             profile.About,
		Address:           profile.Address,
		Description:       profile.Description,
		Email:             profile.Email,
		ProfilePictureURL: profile.ProfilePictureURL,
		Websites:          profile.Websites,
		Vertical:          string(profile.Vertical),
	}
}

func mapPhoneToDomain(record *schema.WhatsAppBusinessPhoneNumber) *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                     record.ID,
		Provider:               businessphone.Provider(record.Provider).Normalized(),
		MetaPhoneNumberID:      record.MetaPhoneNumberID,
		WABAId:                 record.WABAId,
		OwnerWorkspaceID:       record.OwnerWorkspaceID,
		OwnerAssignedBy:        record.OwnerAssignedBy,
		OwnerAssignedAt:        record.OwnerAssignedAt,
		BusinessPortfolioID:    record.BusinessPortfolioID,
		DisplayPhoneNumber:     record.DisplayPhoneNumber,
		VerifiedName:           record.VerifiedName,
		Status:                 businessphone.Status(record.Status),
		QualityRating:          businessphone.QualityRating(record.QualityRating),
		NameStatus:             businessphone.NameStatus(record.NameStatus),
		CodeVerificationStatus: record.CodeVerificationStatus,
		IsOfficialBusiness:     record.IsOfficialBusiness,
		BusinessProfile: businessphone.BusinessProfile{
			About:             record.BusinessProfile.About,
			Address:           record.BusinessProfile.Address,
			Description:       record.BusinessProfile.Description,
			Email:             record.BusinessProfile.Email,
			ProfilePictureURL: record.BusinessProfile.ProfilePictureURL,
			Websites:          record.BusinessProfile.Websites,
			Vertical:          businessphone.BusinessVertical(record.BusinessProfile.Vertical),
		},
		AccessToken:                record.AccessToken,
		Dialog360ChannelID:         record.Dialog360ChannelID,
		Dialog360APIKey:            record.Dialog360APIKey.String(),
		OnboardingError:            record.OnboardingError,
		WABAName:                   record.WABAName,
		AccountReviewStatus:        record.AccountReviewStatus,
		BusinessVerificationStatus: record.BusinessVerificationStatus,
		OwnershipType:              record.OwnershipType,
		MessagingLimitTier:         record.MessagingLimitTier,
		CallsEnabled:               record.CallsEnabled,
		CreatedAt:                  record.CreatedAt,
		UpdatedAt:                  record.UpdatedAt,
	}
}
