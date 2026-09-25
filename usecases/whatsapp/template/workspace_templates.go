package template_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	domain "vozko/domain/whatsapp/template"
	"vozko/domain/workspace_template_access"
)

type TemplateAccess interface {
	GetTemplateIDsForWorkspace(workspaceID string) ([]string, error)
	HasAccess(workspaceID, templateID string) (bool, error)
}

type TemplateGrants interface {
	Create(access *workspace_template_access.WorkspaceTemplateAccess) error
}

type PhoneOwners interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type WorkspaceTemplatesDeps struct {
	Access TemplateAccess
	Grants TemplateGrants
	Phones PhoneOwners
	List   domain.ListUseCase
	Get    domain.GetUseCase
	Create domain.CreateTemplateUseCase
}

type workspaceTemplates struct {
	access TemplateAccess
	grants TemplateGrants
	phones PhoneOwners
	list   domain.ListUseCase
	get    domain.GetUseCase
	create domain.CreateTemplateUseCase
}

func NewWorkspaceTemplatesUseCase(deps WorkspaceTemplatesDeps) domain.WorkspaceTemplatesUseCase {
	return &workspaceTemplates{access: deps.Access, grants: deps.Grants, phones: deps.Phones, list: deps.List, get: deps.Get, create: deps.Create}
}

func (uc *workspaceTemplates) Create(workspaceID, grantedBy string, input domain.CreateTemplateInput) (*domain.CreateTemplateOutput, error) {
	if strings.TrimSpace(workspaceID) == "" || uc.phones == nil || uc.grants == nil || uc.create == nil {
		return nil, domain.ErrPhoneOutsideWorkspace
	}
	phone, err := uc.phones.FindByID(input.BusinessPhoneID)
	if err != nil || phone == nil || !phone.BelongsToWorkspace(workspaceID) {
		return nil, domain.ErrPhoneOutsideWorkspace
	}
	created, err := uc.create.Execute(input)
	if err != nil {
		return nil, err
	}
	if err := uc.grants.Create(workspace_template_access.NewWorkspaceTemplateAccess("", workspaceID, created.ID, grantedBy)); err != nil {
		return nil, fmt.Errorf("grant template %s to %s: %w", created.ID, workspaceID, err)
	}
	return created, nil
}

func (uc *workspaceTemplates) List(workspaceID string, input domain.ListInput) (*shared.PaginatedResult[*domain.Template], error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, domain.ErrTemplateAccessDenied
	}
	ids, err := uc.access.GetTemplateIDsForWorkspace(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("template access of %s: %w", workspaceID, err)
	}
	if len(ids) == 0 {
		return &shared.PaginatedResult[*domain.Template]{Items: []*domain.Template{}, Page: 1, PageSize: input.Options.Pagination.PageSize}, nil
	}
	input.TemplateIDs = ids
	return uc.list.Execute(input)
}

func (uc *workspaceTemplates) Get(workspaceID, templateID string) (*domain.Template, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, domain.ErrTemplateAccessDenied
	}
	granted, err := uc.access.HasAccess(workspaceID, templateID)
	if err != nil {
		return nil, fmt.Errorf("template access of %s: %w", workspaceID, err)
	}
	if !granted {
		return nil, domain.ErrTemplateAccessDenied
	}
	return uc.get.Execute(templateID)
}
