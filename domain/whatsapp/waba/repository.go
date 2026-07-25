package waba

import "vozko/domain/shared"

type ListInput struct {
	Options shared.QueryOptions
}

type Repository interface {
	Create(account *WhatsAppBusinessAccount) error
	Update(id string, account *WhatsAppBusinessAccount) error
	Delete(id string) error
	FindByID(id string) (*WhatsAppBusinessAccount, error)
	FindByMetaWABAId(metaWABAId string) (*WhatsAppBusinessAccount, error)
	List(input ListInput) (*shared.PaginatedResult[*WhatsAppBusinessAccount], error)
	ClearAccessToken(id string) error
}
