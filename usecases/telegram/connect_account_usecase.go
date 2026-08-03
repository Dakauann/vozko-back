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

// ConnectAccountUseCase attaches a Telegram bot to a workspace.
//
// The whole flow is two round-trips, getMe, then setWebhook, with no OAuth, no
// callback, no state parameter and no token refresh. That is not an accident of
// this implementation: a Telegram bot token is minted by the customer in
// BotFather and never expires, so there is no authorization dance to run.
type ConnectAccountUseCase struct {
	accounts tgdomain.AccountRepository
	api      tgdomain.BotAPI
	// webhookBaseURL is the public origin Telegram will POST to. Validated at
	// boot, because a wrong scheme or port produces silence rather than an error.
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

// ConnectInput is one connect request.
type ConnectInput struct {
	WorkspaceID  string
	DepartmentID *string
	BotToken     string
}

// Execute validates the token, registers the webhook and persists the account.
func (uc *ConnectAccountUseCase) Execute(ctx context.Context, in ConnectInput) (*tgdomain.Account, error) {
	token := strings.TrimSpace(in.BotToken)
	if token == "" {
		return nil, tgdomain.ErrBotTokenRequired
	}
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, tgdomain.ErrWorkspaceIDRequired
	}

	// getMe both proves the token works and tells us who the bot is. Doing it
	// first means an invalid paste fails immediately with a clear message rather
	// than creating a dead row.
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

	// The webhook is registered AFTER the row exists, because the URL contains
	// the account id. Registering first would mean minting an id we might not
	// persist.
	if err := uc.registerWebhook(ctx, account); err != nil {
		// The account is kept rather than rolled back: the credentials are valid,
		// and the operator can retry registration from the UI. Deleting it would
		// force them to find the token again.
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

// upsert creates the account, or restores and updates a previously disconnected
// one. Mirrors the Instagram onboarding: reconnecting a bot must not collide
// with the unique index left behind by a soft delete.
func (uc *ConnectAccountUseCase) upsert(
	ctx context.Context,
	in ConnectInput,
	profile *tgdomain.BotProfile,
	token, secret string,
) (*tgdomain.Account, error) {
	existing, err := uc.accounts.FindByBotUserIDUnscoped(ctx, profile.BotUserID)
	switch {
	case err == nil:
		// A bot already connected to a DIFFERENT workspace is refused. The bot is
		// a single identity with a single webhook URL; letting two tenants claim
		// it would silently redirect one tenant's messages to the other.
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

// registerWebhook points Telegram at us and confirms it took.
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

	// setWebhook answering true is not proof of a working endpoint: Telegram
	// accepts the registration and only then discovers it cannot reach us.
	// getWebhookInfo is the confirmation, and the URL comparison catches a
	// misconfigured base URL at connect time instead of as silence in production.
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

// ReregisterWebhookUseCase re-points Telegram at us.
//
// This is the recovery action for the channel's worst failure mode. Undelivered
// updates are discarded after 24 hours and there is no history API, so a webhook
// that has been failing is losing messages permanently, and the fix has to be
// one button, not a support ticket.
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

	// The secret is rotated on re-registration. It costs nothing, and it closes
	// the window where an old secret leaked from a misconfigured proxy would
	// still be accepted.
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
