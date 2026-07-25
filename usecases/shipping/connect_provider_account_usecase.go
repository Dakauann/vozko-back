package shipping_usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shipping"
)

type ConnectProviderAccount struct {
	repo     shipping.ProviderAccountRepository
	gateways map[shipping.Provider]shipping.ProviderGateway
	now      func() time.Time
}

func NewConnectProviderAccount(repo shipping.ProviderAccountRepository, gateways map[shipping.Provider]shipping.ProviderGateway) shipping.ConnectProviderAccountUseCase {
	return &ConnectProviderAccount{
		repo:     repo,
		gateways: gateways,
		now:      time.Now,
	}
}

func (uc *ConnectProviderAccount) Execute(ctx context.Context, input shipping.ConnectProviderAccountInput) (*shipping.ProviderAccount, error) {
	gateway, ok := uc.gateways[input.Provider]
	if !ok || gateway == nil {
		return nil, shipping.ErrProviderNotConfigured
	}
	tokenResult, err := gateway.ExchangeAuthorizationCode(ctx, shipping.AuthorizationCodeParams{
		Code:        input.Code,
		RedirectURI: input.RedirectURI,
		Scopes:      input.Scopes,
	})
	if err != nil {
		return nil, err
	}
	if tokenResult == nil {
		return nil, shipping.ErrAuthorizationFailed
	}
	expiresAt := time.Time{}
	if tokenResult.ExpiresIn > 0 {
		expiresAt = uc.now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second)
	}
	appSettings, err := gateway.FetchAppSettings(ctx, tokenResult.AccessToken)
	if err != nil {
		return nil, err
	}
	now := uc.now()
	accountID := input.AccountID
	if accountID == "" {
		accountID = uuid.NewString()
	}
	if input.AccountID != "" {
		existing, err := uc.repo.FindByID(input.AccountID)
		if err != nil {
			return nil, err
		}
		if !existing.BelongsTo(input.UserID) {
			return nil, shipping.ErrAccountOwnership
		}
		existing.Token = shipping.ProviderToken{
			AccessToken:  tokenResult.AccessToken,
			RefreshToken: tokenResult.RefreshToken,
			TokenType:    tokenResult.TokenType,
			Scopes:       tokenResult.Scopes,
			ExpiresAt:    expiresAt,
		}
		if tokenResult.ExternalID != "" {
			existing.ExternalID = tokenResult.ExternalID
		}
		if tokenResult.Label != "" {
			existing.Label = tokenResult.Label
		}
		existing.AppSettings = appSettings
		existing.UpdatedAt = now
		if err := uc.repo.Update(existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	label := tokenResult.Label
	if label == "" {
		label = string(input.Provider)
	}
	account := &shipping.ProviderAccount{
		ID:         accountID,
		UserID:     input.UserID,
		Provider:   input.Provider,
		ExternalID: tokenResult.ExternalID,
		Label:      label,
		Token: shipping.ProviderToken{
			AccessToken:  tokenResult.AccessToken,
			RefreshToken: tokenResult.RefreshToken,
			TokenType:    tokenResult.TokenType,
			Scopes:       tokenResult.Scopes,
			ExpiresAt:    expiresAt,
		},
		AppSettings: appSettings,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := uc.repo.Create(account); err != nil {
		return nil, err
	}
	return account, nil
}
