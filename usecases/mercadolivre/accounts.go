package mercadolivre

import (
	"context"
	"log"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/usecases/shared/automationrefs"
)

const disconnectReason = "disconnected by the workspace"

type AccountsDeps struct {
	Accounts   mldomain.AccountRepository
	OAuth      mldomain.OAuthService
	References automationrefs.Finders
	Alerts     AccountAlerts
	Metrics    Metrics
}

type Accounts struct {
	accounts mldomain.AccountRepository
	oauth    mldomain.OAuthService
	refs     automationrefs.Finders
	alerts   AccountAlerts
	metrics  Metrics
	now      Clock
}

func NewAccounts(d AccountsDeps) *Accounts {
	if d.Accounts == nil || d.OAuth == nil || d.Alerts == nil || d.Metrics == nil ||
		d.References.Agents == nil || d.References.Workflows == nil || d.References.Pipelines == nil || d.References.Departments == nil {
		panic("mercadolivre: accounts require the repository, oauth, every reference finder, alerts and metrics")
	}
	return &Accounts{accounts: d.Accounts, oauth: d.OAuth, refs: d.References, alerts: d.Alerts, metrics: d.Metrics, now: utcNow}
}

func (uc *Accounts) List(ctx context.Context, in mldomain.ListAccountsInput) (*shared.PaginatedResult[*mldomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, mldomain.ErrWorkspaceIDRequired
	}
	return uc.accounts.ListByWorkspace(ctx, in)
}

func (uc *Accounts) Get(ctx context.Context, workspaceID, id string) (*mldomain.Account, error) {
	return uc.accounts.FindByIDInWorkspace(ctx, workspaceID, id)
}

type UpdateAccountInput struct {
	WorkspaceID string
	ID          string

	DepartmentID         *string
	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses *bool
	EnableWorkflow       *bool
	AutomationDisclosure *string
}

func (uc *Accounts) Update(ctx context.Context, in UpdateAccountInput) (*mldomain.Account, error) {
	account, err := uc.accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	if in.DepartmentID != nil {
		account.DepartmentID = shared.OptionalID(in.DepartmentID)
	}
	if in.AgentID != nil {
		account.AgentID = shared.OptionalID(in.AgentID)
	}
	if in.WorkflowID != nil {
		account.WorkflowID = shared.OptionalID(in.WorkflowID)
	}
	if in.PipelineID != nil {
		account.PipelineID = shared.OptionalID(in.PipelineID)
	}
	if in.EnableAgentResponses != nil {
		account.EnableAgentResponses = *in.EnableAgentResponses
	}
	if in.EnableWorkflow != nil {
		account.EnableWorkflow = *in.EnableWorkflow
	}
	if in.AutomationDisclosure != nil {
		disclosure := strings.TrimSpace(*in.AutomationDisclosure)
		if disclosure != "" {
			if err := mldomain.ValidateMessageText(disclosure); err != nil {
				return nil, err
			}
		}
		account.AutomationDisclosure = disclosure
	}
	if err := uc.refs.OwnedBy(ctx, account.WorkspaceID, automationrefs.Refs{
		AgentID: account.AgentID, WorkflowID: account.WorkflowID, PipelineID: account.PipelineID, DepartmentID: account.DepartmentID,
	}); err != nil {
		return nil, err
	}
	if err := uc.accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	return uc.accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.ID)
}

type DisconnectOutput struct {
	PurgeAfter time.Time
	Warning    string
}

func (uc *Accounts) Disconnect(ctx context.Context, workspaceID, id string) (*DisconnectOutput, error) {
	account, err := uc.accounts.FindByIDInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	creds := account.Credentials()
	purgeAfter := mldomain.PurgeScheduleFrom(uc.now())
	if err := uc.accounts.Disconnect(ctx, account.ID, mldomain.StatusDisconnected, disconnectReason, purgeAfter); err != nil {
		return nil, err
	}
	uc.metrics.AddMercadoLivreEvent("account", "disconnected")
	out := &DisconnectOutput{PurgeAfter: purgeAfter}
	if creds.Validate() == nil {
		if err := uc.oauth.Revoke(ctx, creds); err != nil {
			log.Printf("[mercadolivre] revoke at Mercado Livre failed for %s, the tokens are already wiped: %v", account.ID, err)
			out.Warning = "revoke_failed"
		}
	}
	if err := uc.alerts.Reconnected(ctx, account); err != nil {
		log.Printf("[mercadolivre] reconnect alert of %s not resolved: %v", account.ID, err)
	}
	return out, nil
}
