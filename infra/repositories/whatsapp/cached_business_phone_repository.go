package whatsapp_repository

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

const businessPhoneCacheTTL = 60 * time.Second

type CachedBusinessPhoneRepository struct {
	inner  businessphone.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedBusinessPhoneRepository(inner businessphone.Repository, shared cache.SharedState) businessphone.Repository {
	return &CachedBusinessPhoneRepository{inner: inner, shared: shared, ttl: businessPhoneCacheTTL}
}

func bphoneIDKey(id string) string       { return "wa:bphone:id:" + id }
func bphoneMetaKey(metaID string) string { return "wa:bphone:meta:" + metaID }

func (r *CachedBusinessPhoneRepository) invalidateBoth(id, metaID string) {
	if r.shared == nil {
		return
	}
	keys := make([]string, 0, 2)
	if strings.TrimSpace(id) != "" {
		keys = append(keys, bphoneIDKey(id))
	}
	if strings.TrimSpace(metaID) != "" {
		keys = append(keys, bphoneMetaKey(metaID))
	}
	if len(keys) > 0 {
		_ = r.shared.Del(keys...)
	}
}

func (r *CachedBusinessPhoneRepository) invalidateByID(id string) {
	if r.shared == nil || strings.TrimSpace(id) == "" {
		return
	}
	metaID := ""
	if existing, err := r.inner.FindByID(id); err == nil && existing != nil {
		metaID = existing.MetaPhoneNumberID
	}
	r.invalidateBoth(id, metaID)
}

func (r *CachedBusinessPhoneRepository) readCached(key string) *businessphone.WhatsAppBusinessPhoneNumber {
	if r.shared == nil {
		return nil
	}
	raw, err := r.shared.GetString(key)
	if err != nil || raw == "" {
		return nil
	}
	var dto cachedPhoneDTO
	if json.Unmarshal([]byte(raw), &dto) != nil || dto.ID == "" {
		return nil
	}
	return dto.toEntity()
}

func (r *CachedBusinessPhoneRepository) writeCached(p *businessphone.WhatsAppBusinessPhoneNumber) {
	if r.shared == nil || p == nil {
		return
	}
	dto := newCachedPhoneDTO(p)
	data, err := json.Marshal(dto)
	if err != nil {
		return
	}
	if p.ID != "" {
		_ = r.shared.SetString(bphoneIDKey(p.ID), string(data), r.ttl)
	}
	if p.MetaPhoneNumberID != "" {
		_ = r.shared.SetString(bphoneMetaKey(p.MetaPhoneNumberID), string(data), r.ttl)
	}
}

// cachedPhoneDTO mirrors the phone for Redis. The Dialog360APIKey is cached the
// same way the Meta AccessToken already is (the factory needs the credential to
// build a client on a cache hit). It is encrypted at rest in Postgres via
// piigorm.EncryptedString; the short lived 60s Redis copy mirrors AccessToken.
type cachedPhoneDTO struct {
	ID                         string                        `json:"id"`
	Provider                   businessphone.Provider        `json:"provider,omitempty"`
	MetaPhoneNumberID          string                        `json:"metaPhoneNumberId"`
	Dialog360ChannelID         string                        `json:"dialog360ChannelId,omitempty"`
	Dialog360APIKey            string                        `json:"dialog360ApiKey,omitempty"`
	OnboardingError            string                        `json:"onboardingError,omitempty"`
	WABAId                     string                        `json:"wabaId"`
	OwnerWorkspaceID           string                        `json:"ownerWorkspaceId,omitempty"`
	OwnerAssignedBy            string                        `json:"ownerAssignedBy,omitempty"`
	OwnerAssignedAt            *time.Time                    `json:"ownerAssignedAt,omitempty"`
	BusinessPortfolioID        string                        `json:"businessPortfolioId,omitempty"`
	DisplayPhoneNumber         string                        `json:"displayPhoneNumber"`
	VerifiedName               string                        `json:"verifiedName"`
	Status                     businessphone.Status          `json:"status"`
	QualityRating              businessphone.QualityRating   `json:"qualityRating"`
	NameStatus                 businessphone.NameStatus      `json:"nameStatus"`
	CodeVerificationStatus     string                        `json:"codeVerificationStatus"`
	IsOfficialBusiness         bool                          `json:"isOfficialBusiness"`
	BusinessProfile            businessphone.BusinessProfile `json:"businessProfile,omitempty"`
	AccessToken                string                        `json:"accessToken,omitempty"`
	WABAName                   string                        `json:"wabaName,omitempty"`
	AccountReviewStatus        string                        `json:"accountReviewStatus,omitempty"`
	BusinessVerificationStatus string                        `json:"businessVerificationStatus,omitempty"`
	OwnershipType              string                        `json:"ownershipType,omitempty"`
	MessagingLimitTier         string                        `json:"messagingLimitTier,omitempty"`
	CreatedAt                  time.Time                     `json:"createdAt"`
	UpdatedAt                  time.Time                     `json:"updatedAt"`
}

func newCachedPhoneDTO(p *businessphone.WhatsAppBusinessPhoneNumber) cachedPhoneDTO {
	return cachedPhoneDTO{
		ID:                         p.ID,
		Provider:                   p.Provider.Normalized(),
		MetaPhoneNumberID:          p.MetaPhoneNumberID,
		Dialog360ChannelID:         p.Dialog360ChannelID,
		Dialog360APIKey:            p.Dialog360APIKey,
		OnboardingError:            p.OnboardingError,
		WABAId:                     p.WABAId,
		OwnerWorkspaceID:           p.OwnerWorkspaceID,
		OwnerAssignedBy:            p.OwnerAssignedBy,
		OwnerAssignedAt:            p.OwnerAssignedAt,
		BusinessPortfolioID:        p.BusinessPortfolioID,
		DisplayPhoneNumber:         p.DisplayPhoneNumber,
		VerifiedName:               p.VerifiedName,
		Status:                     p.Status,
		QualityRating:              p.QualityRating,
		NameStatus:                 p.NameStatus,
		CodeVerificationStatus:     p.CodeVerificationStatus,
		IsOfficialBusiness:         p.IsOfficialBusiness,
		BusinessProfile:            p.BusinessProfile,
		AccessToken:                p.AccessToken,
		WABAName:                   p.WABAName,
		AccountReviewStatus:        p.AccountReviewStatus,
		BusinessVerificationStatus: p.BusinessVerificationStatus,
		OwnershipType:              p.OwnershipType,
		MessagingLimitTier:         p.MessagingLimitTier,
		CreatedAt:                  p.CreatedAt,
		UpdatedAt:                  p.UpdatedAt,
	}
}

func (d cachedPhoneDTO) toEntity() *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                         d.ID,
		Provider:                   d.Provider.Normalized(),
		MetaPhoneNumberID:          d.MetaPhoneNumberID,
		Dialog360ChannelID:         d.Dialog360ChannelID,
		Dialog360APIKey:            d.Dialog360APIKey,
		OnboardingError:            d.OnboardingError,
		WABAId:                     d.WABAId,
		OwnerWorkspaceID:           d.OwnerWorkspaceID,
		OwnerAssignedBy:            d.OwnerAssignedBy,
		OwnerAssignedAt:            d.OwnerAssignedAt,
		BusinessPortfolioID:        d.BusinessPortfolioID,
		DisplayPhoneNumber:         d.DisplayPhoneNumber,
		VerifiedName:               d.VerifiedName,
		Status:                     d.Status,
		QualityRating:              d.QualityRating,
		NameStatus:                 d.NameStatus,
		CodeVerificationStatus:     d.CodeVerificationStatus,
		IsOfficialBusiness:         d.IsOfficialBusiness,
		BusinessProfile:            d.BusinessProfile,
		AccessToken:                d.AccessToken,
		WABAName:                   d.WABAName,
		AccountReviewStatus:        d.AccountReviewStatus,
		BusinessVerificationStatus: d.BusinessVerificationStatus,
		OwnershipType:              d.OwnershipType,
		MessagingLimitTier:         d.MessagingLimitTier,
		CreatedAt:                  d.CreatedAt,
		UpdatedAt:                  d.UpdatedAt,
	}
}

func (r *CachedBusinessPhoneRepository) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if strings.TrimSpace(id) == "" {
		return r.inner.FindByID(id)
	}
	if cached := r.readCached(bphoneIDKey(id)); cached != nil {
		return cached, nil
	}
	p, err := r.inner.FindByID(id)
	if err != nil || p == nil {
		return p, err
	}
	r.writeCached(p)
	return p, nil
}

func (r *CachedBusinessPhoneRepository) FindByMetaPhoneNumberID(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if strings.TrimSpace(metaID) == "" {
		return r.inner.FindByMetaPhoneNumberID(metaID)
	}
	if cached := r.readCached(bphoneMetaKey(metaID)); cached != nil {
		return cached, nil
	}
	p, err := r.inner.FindByMetaPhoneNumberID(metaID)
	if err != nil || p == nil {
		return p, err
	}
	r.writeCached(p)
	return p, nil
}

func (r *CachedBusinessPhoneRepository) FindByMetaPhoneNumberIDUnscoped(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.inner.FindByMetaPhoneNumberIDUnscoped(metaID)
}

func (r *CachedBusinessPhoneRepository) FindByDisplayPhoneNumber(displayPhoneNumber string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.inner.FindByDisplayPhoneNumber(displayPhoneNumber)
}

func (r *CachedBusinessPhoneRepository) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.inner.FindByWABAId(wabaID)
}

func (r *CachedBusinessPhoneRepository) List(input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return r.inner.List(input)
}

func (r *CachedBusinessPhoneRepository) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.inner.ListAll()
}

func (r *CachedBusinessPhoneRepository) Create(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	err := r.inner.Create(phone)
	if err == nil && phone != nil {
		r.invalidateBoth(phone.ID, phone.MetaPhoneNumberID)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) Update(id string, phone *businessphone.WhatsAppBusinessPhoneNumber) error {

	prevMeta := ""
	if existing, ferr := r.inner.FindByID(id); ferr == nil && existing != nil {
		prevMeta = existing.MetaPhoneNumberID
	}
	err := r.inner.Update(id, phone)
	if err == nil {
		newMeta := ""
		if phone != nil {
			newMeta = phone.MetaPhoneNumberID
		}
		r.invalidateBoth(id, prevMeta)
		if newMeta != "" && newMeta != prevMeta {
			r.invalidateBoth("", newMeta)
		}
	}
	return err
}

func (r *CachedBusinessPhoneRepository) Delete(id string) error {

	metaID := ""
	if existing, ferr := r.inner.FindByID(id); ferr == nil && existing != nil {
		metaID = existing.MetaPhoneNumberID
	}
	err := r.inner.Delete(id)
	if err == nil {
		r.invalidateBoth(id, metaID)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) BatchUpdate(phones []*businessphone.WhatsAppBusinessPhoneNumber) error {
	err := r.inner.BatchUpdate(phones)
	if err == nil {
		for _, p := range phones {
			if p == nil {
				continue
			}
			r.invalidateBoth(p.ID, p.MetaPhoneNumberID)
		}
	}
	return err
}

func (r *CachedBusinessPhoneRepository) UpdateStatus(id string, status businessphone.Status) error {
	err := r.inner.UpdateStatus(id, status)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) UpdateCallsEnabled(id string, enabled bool) error {
	err := r.inner.UpdateCallsEnabled(id, enabled)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) UpdateBusinessProfile(id string, profile businessphone.BusinessProfile) error {
	err := r.inner.UpdateBusinessProfile(id, profile)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) SyncFromMeta(phone *businessphone.WhatsAppBusinessPhoneNumber) error {

	prevMeta := ""
	if phone != nil && strings.TrimSpace(phone.MetaPhoneNumberID) != "" {
		if existing, ferr := r.inner.FindByMetaPhoneNumberID(phone.MetaPhoneNumberID); ferr == nil && existing != nil {
			prevMeta = existing.MetaPhoneNumberID
		}
	}
	err := r.inner.SyncFromMeta(phone)
	if err == nil && phone != nil {
		r.invalidateBoth(phone.ID, phone.MetaPhoneNumberID)
		if prevMeta != "" && prevMeta != phone.MetaPhoneNumberID {
			r.invalidateBoth("", prevMeta)
		}
	}
	return err
}

func (r *CachedBusinessPhoneRepository) ClearAccessToken(id string) error {
	err := r.inner.ClearAccessToken(id)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) ClearOwner(id string) error {
	err := r.inner.ClearOwner(id)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedBusinessPhoneRepository) Restore(id string) error {
	err := r.inner.Restore(id)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}
