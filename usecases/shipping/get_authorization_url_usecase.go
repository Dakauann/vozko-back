package shipping_usecase

import (
	"context"

	"vozko/domain/shipping"
)

type GetAuthorizationURL struct {
	gateways map[shipping.Provider]shipping.ProviderGateway
}

func NewGetAuthorizationURL(gateways map[shipping.Provider]shipping.ProviderGateway) shipping.GetAuthorizationURLUseCase {
	return &GetAuthorizationURL{gateways: gateways}
}

func (uc *GetAuthorizationURL) Execute(ctx context.Context, input shipping.GetAuthorizationURLInput) (string, error) {
	gateway, ok := uc.gateways[input.Provider]
	if !ok || gateway == nil {
		return "", shipping.ErrProviderNotConfigured
	}
	return gateway.BuildAuthorizationURL(ctx, shipping.AuthorizationURLParams{
		RedirectURI: input.RedirectURI,
		Scopes:      input.Scopes,
		State:       input.State,
	})
}
