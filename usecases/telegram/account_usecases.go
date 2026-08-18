package telegram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

// asAPIError narrows an error to Telegram's structured form. Declared once here
// so every usecase classifies failures the same way.
func asAPIError(err error) (*tgdomain.APIError, bool) {
	var apiErr *tgdomain.APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// ListAccountsUseCase lists a workspace's connected bots.
type ListAccountsUseCase struct {
	accounts tgdomain.AccountRepository
}

func NewListAccountsUseCase(accounts tgdomain.AccountRepository) *ListAccountsUseCase {
	return &ListAccountsUseCase{accounts: accounts}
}

func (uc *ListAccountsUseCase) Execute(ctx context.Context, in tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, tgdomain.ErrWorkspaceIDRequired
	}
	return uc.accounts.ListByWorkspace(ctx, in)
}

// GetAccountUseCase reads one bot, scoped to the caller's workspace.
type GetAccountUseCase struct {
	accounts tgdomain.AccountRepository
}

func NewGetAccountUseCase(accounts tgdomain.AccountRepository) *GetAccountUseCase {
	return &GetAccountUseCase{accounts: accounts}
}

func (uc *GetAccountUseCase) Execute(ctx context.Context, workspaceID, accountID string) (*tgdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Tenant scoping is enforced here rather than in the query so a mismatch is
	// indistinguishable from "not found" to the caller.
	if account.WorkspaceID != workspaceID {
		return nil, tgdomain.ErrAccountNotFound
	}
	return account, nil
}

// UpdateAccountConfigInput carries the automation settings an operator may
// change. Credentials are deliberately absent: rotating a token is a reconnect,
// not a config edit.
type UpdateAccountConfigInput struct {
	DepartmentID         *string
	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses *bool
	EnableWorkflow       *bool
	EnableAnalysis       *bool
	EnableAutoStaging    *bool
	EnableAutoMemory     *bool
}

// UpdateAccountConfigUseCase edits a bot's automation configuration.
type UpdateAccountConfigUseCase struct {
	accounts tgdomain.AccountRepository
}

func NewUpdateAccountConfigUseCase(accounts tgdomain.AccountRepository) *UpdateAccountConfigUseCase {
	return &UpdateAccountConfigUseCase{accounts: accounts}
}

func (uc *UpdateAccountConfigUseCase) Execute(ctx context.Context, workspaceID, accountID string, in UpdateAccountConfigInput) (*tgdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, tgdomain.ErrAccountNotFound
	}

	// Every field is a pointer so "not supplied" is distinguishable from
	// "cleared", otherwise a partial form submission would silently unset the
	// agent.
	if in.DepartmentID != nil {
		account.DepartmentID = emptyToNil(in.DepartmentID)
	}
	if in.AgentID != nil {
		account.AgentID = emptyToNil(in.AgentID)
	}
	if in.WorkflowID != nil {
		account.WorkflowID = emptyToNil(in.WorkflowID)
	}
	if in.PipelineID != nil {
		account.PipelineID = emptyToNil(in.PipelineID)
	}
	if in.EnableAgentResponses != nil {
		account.EnableAgentResponses = *in.EnableAgentResponses
	}
	if in.EnableWorkflow != nil {
		account.EnableWorkflow = *in.EnableWorkflow
	}
	if in.EnableAnalysis != nil {
		account.EnableAnalysis = *in.EnableAnalysis
	}
	if in.EnableAutoStaging != nil {
		account.EnableAutoStaging = *in.EnableAutoStaging
	}
	if in.EnableAutoMemory != nil {
		account.EnableAutoMemory = *in.EnableAutoMemory
	}

	account.Normalize()
	if err := account.Validate(); err != nil {
		return nil, err
	}
	if err := uc.accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	return &trimmed
}

// DisconnectAccountUseCase removes a bot from a workspace.
type DisconnectAccountUseCase struct {
	accounts tgdomain.AccountRepository
	api      tgdomain.BotAPI
}

func NewDisconnectAccountUseCase(accounts tgdomain.AccountRepository, api tgdomain.BotAPI) *DisconnectAccountUseCase {
	return &DisconnectAccountUseCase{accounts: accounts, api: api}
}

func (uc *DisconnectAccountUseCase) Execute(ctx context.Context, workspaceID, accountID string) error {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.WorkspaceID != workspaceID {
		return tgdomain.ErrAccountNotFound
	}

	// Unregistering is best effort. If it fails the row still goes away, and the
	// webhook handler answers 401 for an unknown account, so no traffic is
	// accepted either way. Blocking the disconnect on a Telegram outage would
	// leave the operator unable to remove a bot they no longer control.
	if err := uc.api.DeleteWebhook(ctx, account.BotToken, false); err != nil {
		log.Printf("[telegram] deleteWebhook failed for @%s during disconnect (continuing): %v",
			account.BotUsername, err)
	}

	return uc.accounts.Delete(ctx, accountID)
}

// ---------------------------------------------------------------- health

// webhookHealthLead is how stale a probe may be before the cron re-runs it.
const webhookHealthLead = time.Hour

// pendingUpdateAlarm is the backlog at which an account is marked unhealthy.
//
// Deliberately low. Telegram discards undelivered updates after 24 hours and has
// no history API, so a backlog is not a statistic, it is a countdown to
// permanent message loss.
const pendingUpdateAlarm = 20

// CheckWebhookHealthUseCase probes every account's webhook.
//
// This is the channel's equivalent of Instagram's token-refresh cron, and it
// matters for the same reason: it is the only thing standing between a silent
// infrastructure change and a tenant quietly losing conversations.
type CheckWebhookHealthUseCase struct {
	accounts tgdomain.AccountRepository
	api      tgdomain.BotAPI
	batch    int
}

func NewCheckWebhookHealthUseCase(accounts tgdomain.AccountRepository, api tgdomain.BotAPI) *CheckWebhookHealthUseCase {
	return &CheckWebhookHealthUseCase{accounts: accounts, api: api, batch: 100}
}

func (uc *CheckWebhookHealthUseCase) Execute(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-webhookHealthLead)
	accounts, err := uc.accounts.ListForHealthCheck(ctx, cutoff, uc.batch)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return nil
	}

	log.Printf("[telegram] probing webhook health for %d account(s)", len(accounts))
	for _, account := range accounts {
		// One tenant's failure must never abort the loop, the same isolation the
		// Instagram refresh cron enforces.
		uc.probe(ctx, account)
	}
	return nil
}

func (uc *CheckWebhookHealthUseCase) probe(ctx context.Context, account *tgdomain.Account) {
	now := time.Now().UTC()

	info, err := uc.api.GetWebhookInfo(ctx, account.BotToken)
	if err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.NeedsReconnect() {
			// 401 is the only way a Telegram token dies: it was revoked in
			// BotFather. Nothing recovers it but a new token, so the account is
			// marked for reconnection rather than retried forever.
			uc.transition(ctx, account, tgdomain.StatusTokenInvalid,
				"the bot token was revoked in BotFather; reconnect with a new token")
			return
		}
		log.Printf("[telegram] webhook probe failed for @%s: %v", account.BotUsername, err)
		_ = uc.accounts.UpdateWebhookHealth(ctx, account.ID, tgdomain.WebhookHealth{
			PendingCount: account.WebhookPendingCount,
			LastError:    err.Error(),
			LastErrorAt:  &now,
			CheckedAt:    now,
		})
		return
	}

	health := tgdomain.WebhookHealth{
		PendingCount: info.PendingCount,
		LastError:    info.LastErrorMessage,
		LastErrorAt:  info.LastErrorDate,
		CheckedAt:    now,
	}
	if err := uc.accounts.UpdateWebhookHealth(ctx, account.ID, health); err != nil {
		log.Printf("[telegram] failed to record webhook health for @%s: %v", account.BotUsername, err)
		return
	}

	account.WebhookPendingCount = info.PendingCount
	account.WebhookLastError = info.LastErrorMessage

	if account.WebhookUnhealthy(pendingUpdateAlarm) {
		log.Printf("[telegram] ALARM: webhook unhealthy for @%s (pending=%d, error=%q), "+
			"undelivered updates are discarded after 24h and cannot be recovered",
			account.BotUsername, info.PendingCount, info.LastErrorMessage)
		uc.transition(ctx, account, tgdomain.StatusWebhookFailing, info.LastErrorMessage)
		return
	}

	// Recovered: a previously failing webhook that now reports clean goes back to
	// active without operator action.
	if account.Status == tgdomain.StatusWebhookFailing {
		uc.transition(ctx, account, tgdomain.StatusActive, "")
	}
}

func (uc *CheckWebhookHealthUseCase) transition(ctx context.Context, account *tgdomain.Account, next tgdomain.Status, reason string) {
	if account.Status == next {
		return
	}
	if !account.Status.CanTransitionTo(next) {
		return
	}
	if err := uc.accounts.UpdateStatus(ctx, account.ID, next, reason); err != nil {
		log.Printf("[telegram] failed to set status %s for @%s: %v", next, account.BotUsername, err)
	}
}

// PurgeProcessedEventsUseCase trims the durable dedup table.
type PurgeProcessedEventsUseCase struct {
	events    tgdomain.ProcessedEventRepository
	retention time.Duration
}

func NewPurgeProcessedEventsUseCase(events tgdomain.ProcessedEventRepository, retention time.Duration) *PurgeProcessedEventsUseCase {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &PurgeProcessedEventsUseCase{events: events, retention: retention}
}

func (uc *PurgeProcessedEventsUseCase) Execute(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-uc.retention)
	removed, err := uc.events.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	if removed > 0 {
		log.Printf("[telegram] purged %d processed webhook event(s) older than %s", removed, uc.retention)
	}
	return nil
}
