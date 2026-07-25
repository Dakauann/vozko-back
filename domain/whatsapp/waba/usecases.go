package waba

import "vozko/domain/shared"

type ListUseCase interface {
	Execute(input ListInput) (*shared.PaginatedResult[*WhatsAppBusinessAccount], error)
}

type GetUseCase interface {
	Execute(id string) (*WhatsAppBusinessAccount, error)
}
