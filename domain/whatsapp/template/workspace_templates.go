package template

import (
	"errors"
	"strings"

	"vozko/domain/shared"
)

var (
	ErrTemplateAccessDenied      = errors.New("whatsapp template: the workspace has no access to this template")
	ErrPhoneOutsideWorkspace     = errors.New("whatsapp template: the business phone does not belong to this workspace")
	ErrTemplateLookupUnavailable = errors.New("whatsapp template: the lookup by name is not available on this server")
)

type WorkspaceTemplatesUseCase interface {
	List(workspaceID string, input ListInput) (*shared.PaginatedResult[*Template], error)
	Get(workspaceID, templateID string) (*Template, error)
	FindByName(workspaceID, name, language string) ([]*Template, error)
	Create(workspaceID, grantedBy string, input CreateTemplateInput) (*CreateTemplateOutput, error)
}

type GrantedTemplateNames interface {
	FindGrantedByName(workspaceID, name, language string) ([]*Template, error)
}

func LookupKey(name, language string) (string, string) {
	return strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(language)
}
