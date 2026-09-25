package template

import (
	"errors"

	"vozko/domain/shared"
)

var (
	ErrTemplateAccessDenied  = errors.New("whatsapp template: the workspace has no access to this template")
	ErrPhoneOutsideWorkspace = errors.New("whatsapp template: the business phone does not belong to this workspace")
)

type WorkspaceTemplatesUseCase interface {
	List(workspaceID string, input ListInput) (*shared.PaginatedResult[*Template], error)
	Get(workspaceID, templateID string) (*Template, error)
	Create(workspaceID, grantedBy string, input CreateTemplateInput) (*CreateTemplateOutput, error)
}
