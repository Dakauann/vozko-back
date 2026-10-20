package tiktok

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/usecases/shared/automationrefs"
)

const disconnectReason = "disconnected by the workspace"

const MaxDisclosureRunes = 500

var ErrDisclosureTooLong = errors.New("tiktok: the automation disclosure is limited to 500 characters")

type AccountsDeps struct {
	Accounts   ttdomain.AccountRepository
	OAuth      ttdomain.OAuthService
	References automationrefs.Finders
	Alerts     AccountAlerts
	Metrics    Metrics
}

type Accounts struct {
	d   AccountsDeps
	now Clock
}

func NewAccounts(d AccountsDeps) *Accounts {
	if d.Accounts == nil || d.OAuth == nil || d.Alerts == nil || d.Metrics == nil ||
		d.References.Agents == nil || d.References.Workflows == nil || d.References.Pipelines == nil || d.References.Departments == nil {
		panic("tiktok: accounts require the repository, oauth, every reference finder, alerts and metrics")
	}
	return &Accounts{d: d, now: utcNow}
}

func (uc *Accounts) List(ctx context.Context, in ttdomain.ListAccountsInput) (*shared.PaginatedResult[*ttdomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, ttdomain.ErrWorkspaceIDRequired
	}
	return uc.d.Accounts.ListByWorkspace(ctx, in)
}

func (uc *Accounts) Get(ctx context.Context, workspaceID, id string) (*ttdomain.Account, error) {
	return uc.d.Accounts.FindByIDInWorkspace(ctx, workspaceID, id)
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
	EnableAnalysis       *bool
	EnableAutoStaging    *bool
	EnableAutoMemory     *bool
	AutomationDisclosure *string
}

func (uc *Accounts) Update(ctx context.Context, in UpdateAccountInput) (*ttdomain.Account, error) {
	account, err := uc.d.Accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	applyOptionalID(&account.DepartmentID, in.DepartmentID)
	applyOptionalID(&account.AgentID, in.AgentID)
	applyOptionalID(&account.WorkflowID, in.WorkflowID)
	applyOptionalID(&account.PipelineID, in.PipelineID)
	applyBool(&account.EnableAgentResponses, in.EnableAgentResponses)
	applyBool(&account.EnableWorkflow, in.EnableWorkflow)
	applyBool(&account.EnableAnalysis, in.EnableAnalysis)
	applyBool(&account.EnableAutoStaging, in.EnableAutoStaging)
	applyBool(&account.EnableAutoMemory, in.EnableAutoMemory)
	if in.AutomationDisclosure != nil {
		disclosure := strings.TrimSpace(*in.AutomationDisclosure)
		if utf8.RuneCountInString(disclosure) > MaxDisclosureRunes {
			return nil, ErrDisclosureTooLong
		}
		account.AutomationDisclosure = disclosure
	}
	if err := uc.d.References.OwnedBy(ctx, account.WorkspaceID, automationrefs.Refs{
		AgentID: account.AgentID, WorkflowID: account.WorkflowID, PipelineID: account.PipelineID, DepartmentID: account.DepartmentID,
	}); err != nil {
		return nil, err
	}
	if err := uc.d.Accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	return uc.d.Accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.ID)
}

func applyOptionalID(target **string, value *string) {
	if value != nil {
		*target = shared.OptionalID(value)
	}
}

func applyBool(target *bool, value *bool) {
	if value != nil {
		*target = *value
	}
}

type DisconnectOutput struct {
	PurgeAfter time.Time
	Warning    string
}

func (uc *Accounts) Disconnect(ctx context.Context, workspaceID, id string) (*DisconnectOutput, error) {
	account, err := uc.d.Accounts.FindByIDInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	token := account.AccessToken
	purgeAfter := ttdomain.PurgeScheduleFrom(uc.now())
	if err := uc.d.Accounts.Disconnect(ctx, account.ID, ttdomain.StatusDisconnected, disconnectReason, purgeAfter); err != nil {
		return nil, err
	}
	uc.d.Metrics.AddTikTokEvent("account", "disconnected")
	out := &DisconnectOutput{PurgeAfter: purgeAfter}
	if strings.TrimSpace(token) != "" {
		if err := uc.d.OAuth.Revoke(ctx, token); err != nil {
			log.Printf("[tiktok] revoke at TikTok failed for %s, the tokens are already wiped: %v", account.ID, err)
			out.Warning = "revoke_failed"
		}
	}
	if err := uc.d.Alerts.Healthy(ctx, account); err != nil {
		log.Printf("[tiktok] alerts of %s not resolved: %v", account.ID, err)
	}
	return out, nil
}
