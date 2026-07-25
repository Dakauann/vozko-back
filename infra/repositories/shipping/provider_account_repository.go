package shipping_repository

import (
	"encoding/json"
	"errors"
	"time"

	"vozko/domain/shipping"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type ProviderAccountRepository struct {
	db *gorm.DB
}

func NewProviderAccountRepository(db *gorm.DB) shipping.ProviderAccountRepository {
	return &ProviderAccountRepository{db: db}
}

func (r *ProviderAccountRepository) Create(account *shipping.ProviderAccount) error {
	dbAccount, err := toDBAccount(account)
	if err != nil {
		return err
	}
	return r.db.Create(dbAccount).Error
}

func (r *ProviderAccountRepository) Update(account *shipping.ProviderAccount) error {
	dbAccount, err := toDBAccount(account)
	if err != nil {
		return err
	}
	return r.db.Model(&schema.ShippingProviderAccount{}).Where("id = ?", dbAccount.ID).Updates(dbAccount).Error
}

func (r *ProviderAccountRepository) FindByID(id string) (*shipping.ProviderAccount, error) {
	var dbAccount schema.ShippingProviderAccount
	if err := r.db.Where("id = ?", id).First(&dbAccount).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, shipping.ErrAccountNotFound
		}
		return nil, err
	}
	return toDomainAccount(&dbAccount)
}

func (r *ProviderAccountRepository) ListByWorkspace(userID string, provider shipping.Provider) ([]*shipping.ProviderAccount, error) {
	var dbAccounts []schema.ShippingProviderAccount
	if err := r.db.Where("user_id = ? AND provider = ?", userID, string(provider)).Find(&dbAccounts).Error; err != nil {
		return nil, err
	}
	result := make([]*shipping.ProviderAccount, 0, len(dbAccounts))
	for i := range dbAccounts {
		domainAccount, err := toDomainAccount(&dbAccounts[i])
		if err != nil {
			return nil, err
		}
		result = append(result, domainAccount)
	}
	return result, nil
}

func toDBAccount(account *shipping.ProviderAccount) (*schema.ShippingProviderAccount, error) {
	if account == nil {
		return nil, errors.New("shipping provider account is nil")
	}
	scopes, err := json.Marshal(account.Token.Scopes)
	if err != nil {
		return nil, err
	}
	expiresAt := (*time.Time)(nil)
	if !account.Token.ExpiresAt.IsZero() {
		value := account.Token.ExpiresAt
		expiresAt = &value
	}
	dbAccount := &schema.ShippingProviderAccount{
		ID:           account.ID,
		UserID:       account.UserID,
		Provider:     string(account.Provider),
		ExternalID:   account.ExternalID,
		Label:        account.Label,
		AccessToken:  account.Token.AccessToken,
		RefreshToken: account.Token.RefreshToken,
		TokenType:    account.Token.TokenType,
		Scopes:       scopes,
		ExpiresAt:    expiresAt,
		AppSettings:  account.AppSettings,
		CreatedAt:    account.CreatedAt,
		UpdatedAt:    account.UpdatedAt,
	}
	return dbAccount, nil
}

func toDomainAccount(dbAccount *schema.ShippingProviderAccount) (*shipping.ProviderAccount, error) {
	if dbAccount == nil {
		return nil, errors.New("shipping provider account is nil")
	}
	var scopes []string
	if len(dbAccount.Scopes) > 0 {
		if err := json.Unmarshal(dbAccount.Scopes, &scopes); err != nil {
			return nil, err
		}
	}
	expiresAt := time.Time{}
	if dbAccount.ExpiresAt != nil {
		expiresAt = *dbAccount.ExpiresAt
	}
	account := &shipping.ProviderAccount{
		ID:         dbAccount.ID,
		UserID:     dbAccount.UserID,
		Provider:   shipping.Provider(dbAccount.Provider),
		ExternalID: dbAccount.ExternalID,
		Label:      dbAccount.Label,
		Token: shipping.ProviderToken{
			AccessToken:  dbAccount.AccessToken,
			RefreshToken: dbAccount.RefreshToken,
			TokenType:    dbAccount.TokenType,
			Scopes:       scopes,
			ExpiresAt:    expiresAt,
		},
		AppSettings: dbAccount.AppSettings,
		CreatedAt:   dbAccount.CreatedAt,
		UpdatedAt:   dbAccount.UpdatedAt,
	}
	return account, nil
}
