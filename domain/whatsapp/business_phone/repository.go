package businessphone

import "vozko/domain/shared"

type Repository interface {
	Create(phoneNumber *WhatsAppBusinessPhoneNumber) error
	Update(id string, phoneNumber *WhatsAppBusinessPhoneNumber) error
	Delete(id string) error
	FindByID(id string) (*WhatsAppBusinessPhoneNumber, error)
	FindByMetaPhoneNumberID(metaPhoneNumberID string) (*WhatsAppBusinessPhoneNumber, error)
	FindByMetaPhoneNumberIDUnscoped(metaPhoneNumberID string) (*WhatsAppBusinessPhoneNumber, error)
	FindByDisplayPhoneNumber(displayPhoneNumber string) (*WhatsAppBusinessPhoneNumber, error)
	FindByWABAId(wabaID string) ([]*WhatsAppBusinessPhoneNumber, error)
	List(input ListInput) (*shared.PaginatedResult[*WhatsAppBusinessPhoneNumber], error)
	ListAll() ([]*WhatsAppBusinessPhoneNumber, error)
	BatchUpdate(phones []*WhatsAppBusinessPhoneNumber) error
	UpdateStatus(id string, status Status) error
	UpdateCallsEnabled(id string, enabled bool) error
	UpdateBusinessProfile(id string, profile BusinessProfile) error
	SyncFromMeta(phoneNumber *WhatsAppBusinessPhoneNumber) error
	ClearAccessToken(id string) error
	ClearOwner(id string) error
	Restore(id string) error
}
