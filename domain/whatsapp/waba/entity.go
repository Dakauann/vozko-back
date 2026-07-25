package waba

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrWABANotFound       = errors.New("whatsapp business account not found")
	ErrWABAAlreadyExists  = errors.New("whatsapp business account already exists")
	ErrMetaWABAIDRequired = errors.New("meta WABA ID is required")
)

type WhatsAppBusinessAccount struct {
	ID                         string    `json:"id"`
	Provider                   string    `json:"provider,omitempty"`
	MetaWABAId                 string    `json:"metaWabaId"`
	Name                       string    `json:"name,omitempty"`
	BusinessPortfolioID        string    `json:"businessPortfolioId,omitempty"`
	Dialog360ClientID          string    `json:"dialog360ClientId,omitempty"`
	AccessToken                string    `json:"-"`
	AccountReviewStatus        string    `json:"accountReviewStatus,omitempty"`
	BusinessVerificationStatus string    `json:"businessVerificationStatus,omitempty"`
	OwnershipType              string    `json:"ownershipType,omitempty"`
	MessagingLimitTier         string    `json:"messagingLimitTier,omitempty"`
	Currency                   string    `json:"currency,omitempty"`
	TimezoneId                 string    `json:"timezoneId,omitempty"`
	MessageTemplateNamespace   string    `json:"messageTemplateNamespace,omitempty"`
	Status                     string    `json:"status,omitempty"`
	Country                    string    `json:"country,omitempty"`
	PhoneCount                 int       `json:"phoneCount"`
	TemplateCount              int       `json:"templateCount"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

func NewWhatsAppBusinessAccount(metaWABAId, name string) (*WhatsAppBusinessAccount, error) {
	metaWABAId = strings.TrimSpace(metaWABAId)
	if metaWABAId == "" {
		return nil, ErrMetaWABAIDRequired
	}
	return &WhatsAppBusinessAccount{
		// Provider mirrors businessphone.ProviderMeta. Historical accounts and
		// any created without an explicit provider default to Meta.
		Provider:   "meta",
		MetaWABAId: metaWABAId,
		Name:       strings.TrimSpace(name),
	}, nil
}
