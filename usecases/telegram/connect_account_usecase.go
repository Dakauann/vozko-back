package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	tgdomain "vozko/domain/telegram"
)

type ConnectAccountUseCase struct {
	accounts       tgdomain.AccountRepository
	api            tgdomain.BotAPI
	webhookBaseURL string
}

func NewConnectAccountUseCase(
	accounts tgdomain.AccountRepository,
	api tgdomain.BotAPI,
	webhookBaseURL string,
) *ConnectAccountUseCase {
	return &ConnectAccountUseCase{
		accounts:       accounts,
		api:            api,
		webhookBaseURL: strings.TrimRight(strings.TrimSpace(webhookBaseURL), "/"),
	}
}

type ConnectInput struct {
	WorkspaceID  string
	DepartmentID *string
	BotToken     string
}

func (uc *ConnectAccountUseCase) Execute(ctx context.Context, in ConnectInput) (*tgdomain.Account, error) {
	token := strings.TrimSpace(in.BotToken)
	if token == "" {
		return nil, tgdomain.ErrBotTokenRequired
	}
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, tgdomain.ErrWorkspaceIDRequired
	}

	profile, err := uc.api.GetMe(ctx, token)
	if err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.NeedsReconnect() {
			return nil, fmt.Errorf("%w: BotFather rejected this token", tgdomain.ErrBotTokenInvalid)
		}
		return nil, fmt.Errorf("telegram: validate token: %w", err)
	}

	secret, err := tgdomain.GenerateWebhookSecret()
	if err != nil {
		return nil, err
	}

	account, err := uc.upsert(ctx, in, profile, token, secret)
	if err != nil {
		return nil, err
	}

	if err := uc.registerWebhook(ctx, account); err != nil {
		_ = uc.accounts.UpdateStatus(ctx, account.ID, tgdomain.StatusWebhookFailing,
			"webhook registration failed: "+err.Error())
		account.Status = tgdomain.StatusWebhookFailing
		account.StatusReason = err.Error()
		return account, fmt.Errorf("telegram: register webhook: %w", err)
	}

	if err := uc.accounts.UpdateStatus(ctx, account.ID, tgdomain.StatusActive, ""); err != nil {
		return nil, err
	}
	account.Status = tgdomain.StatusActive
	account.StatusReason = ""

	log.Printf("[telegram] connected bot @%s (id=%d) to workspace %s, webhook=%s",
		account.BotUsername, account.BotUserID, account.WorkspaceID,
		tgdomain.WebhookURLFor(uc.webhookBaseURL, account.ID))

	return account, nil
}

func (uc *ConnectAccountUseCase) upsert(
	ctx context.Context,
	in ConnectInput,
	profile *tgdomain.BotProfile,
	token, secret string,
) (*tgdomain.Account, error) {
	existing, err := uc.accounts.FindByBotUserIDUnscoped(ctx, profile.BotUserID)
	switch {
	case err == nil:
		if existing.WorkspaceID != in.WorkspaceID {
			return nil, tgdomain.ErrAccountAlreadyLinked
		}
		if err := uc.accounts.Restore(ctx, existing.ID); err != nil {
			return nil, err
		}

		existing.DepartmentID = in.DepartmentID
		existing.BotUsername = profile.Username
		existing.BotName = profile.FirstName
		existing.CanConnectToBusiness = profile.CanConnectToBusiness
		existing.BotToken = token
		existing.WebhookSecret = secret
		existing.Status = tgdomain.StatusPending
		existing.StatusReason = ""
		existing.Normalize()
		if err := existing.Validate(); err != nil {
			return nil, err
		}
		if err := uc.accounts.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil

	case errors.Is(err, tgdomain.ErrAccountNotFound):
		account := &tgdomain.Account{
			WorkspaceID:          in.WorkspaceID,
			DepartmentID:         in.DepartmentID,
			Mode:                 tgdomain.ModeBot,
			BotUserID:            profile.BotUserID,
			BotUsername:          profile.Username,
			BotName:              profile.FirstName,
			CanConnectToBusiness: profile.CanConnectToBusiness,
			BotToken:             token,
			WebhookSecret:        secret,
			Status:               tgdomain.StatusPending,
		}
		account.Normalize()
		if err := account.Validate(); err != nil {
			return nil, err
		}
		if err := uc.accounts.Create(ctx, account); err != nil {
			return nil, err
		}
		return account, nil

	default:
		return nil, err
	}
}

func (uc *ConnectAccountUseCase) registerWebhook(ctx context.Context, account *tgdomain.Account) error {
	url := tgdomain.WebhookURLFor(uc.webhookBaseURL, account.ID)

	if err := uc.api.SetWebhook(ctx, account.BotToken, tgdomain.WebhookConfig{
		URL:            url,
		SecretToken:    account.WebhookSecret,
		MaxConnections: tgdomain.DefaultMaxConnections,
		AllowedUpdates: tgdomain.AllowedUpdates(),
	}); err != nil {
		return err
	}

	info, err := uc.api.GetWebhookInfo(ctx, account.BotToken)
	if err != nil {
		return err
	}
	if info.URL != url {
		return fmt.Errorf("telegram reports webhook %q but we registered %q", info.URL, url)
	}
	if info.LastErrorMessage != "" {
		return fmt.Errorf("telegram cannot reach the webhook: %s", info.LastErrorMessage)
	}

	return uc.accounts.SetWebhookRegistered(ctx, account.ID, time.Now().UTC())
}

type ReregisterWebhookUseCase struct {
	accounts       tgdomain.AccountRepository
	api            tgdomain.BotAPI
	webhookBaseURL string
}

func NewReregisterWebhookUseCase(
	accounts tgdomain.AccountRepository,
	api tgdomain.BotAPI,
	webhookBaseURL string,
) *ReregisterWebhookUseCase {
	return &ReregisterWebhookUseCase{
		accounts:       accounts,
		api:            api,
		webhookBaseURL: strings.TrimRight(strings.TrimSpace(webhookBaseURL), "/"),
	}
}

func (uc *ReregisterWebhookUseCase) Execute(ctx context.Context, workspaceID, accountID string) (*tgdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, tgdomain.ErrAccountNotFound
	}

	secret, err := tgdomain.GenerateWebhookSecret()
	if err != nil {
		return nil, err
	}
	account.WebhookSecret = secret
	if err := uc.accounts.Update(ctx, account); err != nil {
		return nil, err
	}

	connect := &ConnectAccountUseCase{accounts: uc.accounts, api: uc.api, webhookBaseURL: uc.webhookBaseURL}
	if err := connect.registerWebhook(ctx, account); err != nil {
		_ = uc.accounts.UpdateStatus(ctx, account.ID, tgdomain.StatusWebhookFailing, err.Error())
		return nil, err
	}

	if account.Status.CanTransitionTo(tgdomain.StatusActive) {
		if err := uc.accounts.UpdateStatus(ctx, account.ID, tgdomain.StatusActive, ""); err != nil {
			return nil, err
		}
		account.Status = tgdomain.StatusActive
		account.StatusReason = ""
	}
	return account, nil
}
