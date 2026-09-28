package facebook

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

type PageUseCases struct {
	pages        fbdomain.PageRepository
	grants       fbdomain.GrantRepository
	subscription fbdomain.SubscriptionService
	now          func() time.Time
}

func NewPageUseCases(pages fbdomain.PageRepository, grants fbdomain.GrantRepository, subscription fbdomain.SubscriptionService) *PageUseCases {
	return &PageUseCases{pages: pages, grants: grants, subscription: subscription, now: func() time.Time { return time.Now().UTC() }}
}

func (uc *PageUseCases) List(ctx context.Context, in fbdomain.ListPagesInput) (*shared.PaginatedResult[*fbdomain.Page], error) {
	return uc.pages.ListByWorkspace(ctx, in)
}

func (uc *PageUseCases) Get(ctx context.Context, workspaceID, id string) (*fbdomain.Page, error) {
	page, err := uc.pages.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if page.WorkspaceID != workspaceID {
		return nil, fbdomain.ErrPageNotFound
	}
	return page, nil
}

type UpdatePageConfigInput struct {
	WorkspaceID          string
	ID                   string
	DepartmentID         *string
	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses bool
	EnableWorkflow       bool
	EnableAnalysis       bool
	EnableAutoStaging    bool
	EnableAutoMemory     bool
	AutomationDisclosure string
}

func (uc *PageUseCases) UpdateConfig(ctx context.Context, in UpdatePageConfigInput) (*fbdomain.Page, error) {
	page, err := uc.Get(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	page.DepartmentID = blankToNil(in.DepartmentID)
	page.AgentID = blankToNil(in.AgentID)
	page.WorkflowID = blankToNil(in.WorkflowID)
	page.PipelineID = blankToNil(in.PipelineID)
	page.EnableAgentResponses = in.EnableAgentResponses
	page.EnableWorkflow = in.EnableWorkflow
	page.EnableAnalysis = in.EnableAnalysis
	page.EnableAutoStaging = in.EnableAutoStaging
	page.EnableAutoMemory = in.EnableAutoMemory
	page.AutomationDisclosure = strings.TrimSpace(in.AutomationDisclosure)
	if err := uc.pages.UpdateConfig(ctx, page); err != nil {
		return nil, err
	}
	return uc.pages.FindByID(ctx, page.ID)
}

func (uc *PageUseCases) Disconnect(ctx context.Context, workspaceID, id string) (warning string, err error) {
	page, err := uc.Get(ctx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if page.PageToken != "" {
		if err := uc.subscription.Unsubscribe(ctx, page.FBPageID, page.PageToken); err != nil {
			log.Printf("[facebook] unsubscribe failed while disconnecting page %s: %v", page.FBPageID, err)
			warning = "webhook_unsubscribe_failed"
		}
	}
	if page.Status.CanTransitionTo(fbdomain.StatusDisconnected) {
		if err := uc.pages.UpdateStatus(ctx, page.ID, fbdomain.StatusDisconnected, "disconnected by the workspace"); err != nil {
			return warning, err
		}
	}
	if err := uc.pages.Delete(ctx, page.ID); err != nil {
		return warning, err
	}
	if err := uc.releaseGrantIfUnused(ctx, page.GrantID); err != nil {
		log.Printf("[facebook] could not release grant %s: %v", page.GrantID, err)
	}
	return warning, nil
}

func (uc *PageUseCases) releaseGrantIfUnused(ctx context.Context, grantID string) error {
	if grantID == "" {
		return nil
	}
	remaining, err := uc.pages.ListByGrant(ctx, grantID)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return nil
	}
	if err := uc.grants.Revoke(ctx, grantID, uc.now()); err != nil {
		if errors.Is(err, fbdomain.ErrGrantNotFound) {
			return nil
		}
		return err
	}
	return uc.grants.EraseToken(ctx, grantID)
}

func blankToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	return &trimmed
}
