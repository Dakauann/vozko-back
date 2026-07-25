package shipping

import "context"

type GetAuthorizationURLInput struct {
	Provider    Provider
	RedirectURI string
	Scopes      []string
	State       string
}

type GetAuthorizationURLUseCase interface {
	Execute(ctx context.Context, input GetAuthorizationURLInput) (string, error)
}

type ConnectProviderAccountInput struct {
	Provider    Provider
	UserID      string
	Code        string
	RedirectURI string
	Scopes      []string
	AccountID   string
}

type ConnectProviderAccountUseCase interface {
	Execute(ctx context.Context, input ConnectProviderAccountInput) (*ProviderAccount, error)
}

type CalculateFreightInput struct {
	Provider  Provider
	UserID    string
	AccountID string
	Request   FreightCalculationRequest
}

type CalculateFreightUseCase interface {
	Execute(ctx context.Context, input CalculateFreightInput) ([]FreightQuote, error)
}

type ListProviderAccountsInput struct {
	Provider Provider
	UserID   string
}

type ListProviderAccountsUseCase interface {
	Execute(ctx context.Context, input ListProviderAccountsInput) ([]*ProviderAccount, error)
}
