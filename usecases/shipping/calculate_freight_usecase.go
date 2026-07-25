package shipping_usecase

import (
	"context"
	"time"

	"vozko/domain/shipping"
)

type CalculateFreight struct {
	repo             shipping.ProviderAccountRepository
	gateways         map[shipping.Provider]shipping.ProviderGateway
	now              func() time.Time
	refreshTolerance time.Duration
}

func NewCalculateFreight(repo shipping.ProviderAccountRepository, gateways map[shipping.Provider]shipping.ProviderGateway, refreshTolerance time.Duration) shipping.CalculateFreightUseCase {
	if refreshTolerance <= 0 {
		refreshTolerance = time.Minute
	}
	return &CalculateFreight{
		repo:             repo,
		gateways:         gateways,
		now:              time.Now,
		refreshTolerance: refreshTolerance,
	}
}

func (uc *CalculateFreight) Execute(ctx context.Context, input shipping.CalculateFreightInput) ([]shipping.FreightQuote, error) {
	gateway, ok := uc.gateways[input.Provider]
	if !ok || gateway == nil {
		return nil, shipping.ErrProviderNotConfigured
	}
	account, err := uc.repo.FindByID(input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, shipping.ErrAccountNotFound
	}
	if account.Provider != input.Provider {
		return nil, shipping.ErrAccountOwnership
	}
	if !account.BelongsTo(input.UserID) {
		return nil, shipping.ErrAccountOwnership
	}
	now := uc.now()
	if account.NeedsRefresh(now, uc.refreshTolerance) {
		if account.Token.RefreshToken == "" {
			return nil, shipping.ErrTokenRefreshFailed
		}
		refreshResult, err := gateway.RefreshAccessToken(ctx, account.Token.RefreshToken)
		if err != nil {
			return nil, err
		}
		if refreshResult == nil {
			return nil, shipping.ErrTokenRefreshFailed
		}
		expiresAt := time.Time{}
		if refreshResult.ExpiresIn > 0 {
			expiresAt = now.Add(time.Duration(refreshResult.ExpiresIn) * time.Second)
		}
		account.Token = shipping.ProviderToken{
			AccessToken:  refreshResult.AccessToken,
			RefreshToken: refreshResult.RefreshToken,
			TokenType:    refreshResult.TokenType,
			Scopes:       refreshResult.Scopes,
			ExpiresAt:    expiresAt,
		}
		if refreshResult.ExternalID != "" {
			account.ExternalID = refreshResult.ExternalID
		}
		if refreshResult.Label != "" {
			account.Label = refreshResult.Label
		}
		settings, err := gateway.FetchAppSettings(ctx, refreshResult.AccessToken)
		if err != nil {
			return nil, err
		}
		account.AppSettings = settings
		account.UpdatedAt = now
		if err := uc.repo.Update(account); err != nil {
			return nil, err
		}
	}
	quotes, err := gateway.CalculateFreight(ctx, account.Token.AccessToken, input.Request)
	if err != nil {
		return nil, err
	}
	return quotes, nil
}
