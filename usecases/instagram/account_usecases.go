package instagram

import (
	"context"
	"log"
	"strings"

	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

type ListAccountsUseCase struct {
	accounts igdomain.AccountRepository
}

func NewListAccountsUseCase(accounts igdomain.AccountRepository) *ListAccountsUseCase {
	return &ListAccountsUseCase{accounts: accounts}
}

func (uc *ListAccountsUseCase) Execute(ctx context.Context, in igdomain.ListAccountsInput) (*shared.PaginatedResult[*igdomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, igdomain.ErrWorkspaceIDRequired
	}
	return uc.accounts.ListByWorkspace(ctx, in)
}

type GetAccountUseCase struct {
	accounts igdomain.AccountRepository
}

func NewGetAccountUseCase(accounts igdomain.AccountRepository) *GetAccountUseCase {
	return &GetAccountUseCase{accounts: accounts}
}

func (uc *GetAccountUseCase) Execute(ctx context.Context, workspaceID, id string) (*igdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, igdomain.ErrAccountNotFound
	}
	return account, nil
}

type UpdateAccountConfigInput struct {
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
}

type UpdateAccountConfigUseCase struct {
	accounts igdomain.AccountRepository
}

func NewUpdateAccountConfigUseCase(accounts igdomain.AccountRepository) *UpdateAccountConfigUseCase {
	return &UpdateAccountConfigUseCase{accounts: accounts}
}

func (uc *UpdateAccountConfigUseCase) Execute(ctx context.Context, in UpdateAccountConfigInput) (*igdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != in.WorkspaceID {
		return nil, igdomain.ErrAccountNotFound
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
	account.AccessToken = ""
	if err := uc.accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	return uc.accounts.FindByID(ctx, in.ID)
}

type DisconnectAccountUseCase struct {
	accounts     igdomain.AccountRepository
	subscription igdomain.SubscriptionService
}

func NewDisconnectAccountUseCase(
	accounts igdomain.AccountRepository,
	subscription igdomain.SubscriptionService,
) *DisconnectAccountUseCase {
	return &DisconnectAccountUseCase{accounts: accounts, subscription: subscription}
}

func (uc *DisconnectAccountUseCase) Execute(ctx context.Context, workspaceID, id string) error {
	account, err := uc.accounts.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if account.WorkspaceID != workspaceID {
		return igdomain.ErrAccountNotFound
	}

	if uc.subscription != nil && account.AccessToken != "" {
		if err := uc.subscription.Unsubscribe(ctx, account.IGUserID, account.AccessToken); err != nil {
			log.Printf("[instagram] unsubscribe failed on disconnect account=%s: %v", account.IGUserID, err)
		}
	}

	if err := uc.accounts.UpdateStatus(ctx, id, igdomain.StatusRevoked, "disconnected by user"); err != nil {
		log.Printf("[instagram] status update failed on disconnect account=%s: %v", account.IGUserID, err)
	}
	return uc.accounts.Delete(ctx, id)
}
