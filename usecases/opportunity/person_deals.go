package opportunity_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

type DepartmentScoper interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type personDeals struct {
	svc    *Service
	access shared.EntryAccessChecker
	scoper DepartmentScoper
}

func NewPersonDeals(svc *Service, access shared.EntryAccessChecker, scoper DepartmentScoper) opportunity.PersonDealsUseCase {
	return &personDeals{svc: svc, access: access, scoper: scoper}
}

func (uc *personDeals) Create(by shared.Person, workspaceID string, d opportunity.DealDraft) (*opportunity.Opportunity, error) {
	if d.EntryID != "" && !by.MayActOn(uc.access, workspaceID, d.EntryID, d.EntryType) {
		return nil, opportunity.ErrEntryAccess
	}
	return uc.svc.Create(workspaceID, CreateInput{
		LeadID:        d.LeadID,
		PipelineID:    d.PipelineID,
		StageID:       d.StageID,
		Title:         d.Title,
		ValueCents:    d.ValueCents,
		Currency:      d.Currency,
		LinkEntryID:   d.EntryID,
		LinkEntryType: d.EntryType,
		Actor:         by.UserID,
	})
}

func (uc *personDeals) Move(by shared.Person, workspaceID, dealID, stageID string) (*opportunity.Opportunity, error) {
	return uc.svc.MoveStage(workspaceID, dealID, MoveStageInput{StageID: stageID}, by.UserID)
}

func (uc *personDeals) Link(by shared.Person, workspaceID, dealID, entryID, entryType string) error {
	if !by.MayActOn(uc.access, workspaceID, entryID, entryType) {
		return opportunity.ErrEntryAccess
	}
	return uc.svc.LinkConversation(workspaceID, dealID, entryID, entryType, by.UserID)
}

func (uc *personDeals) Scope(by shared.Person, workspaceID string) (opportunity.DealScope, error) {
	if uc.scoper == nil {
		return opportunity.DealScope{}, opportunity.ErrScopeDenied
	}
	scope, allowed := uc.scoper.GetDepartmentScope(by.UserID, workspaceID, by.SystemAdmin)
	if !allowed {
		return opportunity.DealScope{}, opportunity.ErrScopeDenied
	}
	out := opportunity.DealScope{DepartmentIDs: scope.DepartmentIDs, Restrict: scope.Restrict}
	if !by.SystemAdmin && scope.Restrict {
		out.AssigneeOverride = by.UserID
	}
	return out, nil
}

func (uc *personDeals) Get(by shared.Person, workspaceID, dealID string) (*opportunity.Opportunity, error) {
	if _, err := uc.Scope(by, workspaceID); err != nil {
		return nil, err
	}
	return uc.svc.Get(workspaceID, dealID)
}

func (uc *personDeals) ListByPipeline(by shared.Person, workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	scope, err := uc.Scope(by, workspaceID)
	if err != nil {
		return nil, err
	}
	return uc.svc.ListByPipelineScoped(workspaceID, pipelineID, scope.DepartmentIDs, scope.Restrict, scope.AssigneeOverride)
}
