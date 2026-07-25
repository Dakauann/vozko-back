package shipping

import "context"

type AuthorizationURLParams struct {
	RedirectURI string
	Scopes      []string
	State       string
}

type AuthorizationURLBuilder interface {
	BuildAuthorizationURL(ctx context.Context, params AuthorizationURLParams) (string, error)
}

type AuthorizationCodeParams struct {
	Code        string
	RedirectURI string
	Scopes      []string
}

type TokenExchangeResult struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int
	Scopes       []string
	ExternalID   string
	Label        string
}

type ProviderGateway interface {
	AuthorizationURLBuilder
	ExchangeAuthorizationCode(ctx context.Context, params AuthorizationCodeParams) (*TokenExchangeResult, error)
	RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenExchangeResult, error)
	FetchAppSettings(ctx context.Context, accessToken string) ([]byte, error)
	CalculateFreight(ctx context.Context, accessToken string, request FreightCalculationRequest) ([]FreightQuote, error)
}
