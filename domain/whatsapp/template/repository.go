package template

import "vozko/domain/shared"

type Repository interface {
	Create(template *Template) error

	Update(templateID string, template *Template) error

	Delete(templateID string) error

	FindByID(templateID string) (*Template, error)

	FindByExternalID(externalID string) (*Template, error)

	FindByExternalIDAndWABA(externalID string, wabaID string) (*Template, error)

	BatchFindByExternalIDs(externalIDs []string) ([]*Template, error)

	FindByName(name, language string) (*Template, error)

	FindByNameAndWABA(name, language, wabaID string) (*Template, error)

	List(input ListInput) (*shared.PaginatedResult[*Template], error)

	UpdateStatus(templateID string, status TemplateStatus) error

	UpdateHeaderMediaURL(templateID string, headerMediaURL *string) error

	UpdateHeaderMedia(templateID string, headerMediaURL *string, headerMediaID *string) error

	SyncFromExternal(template *Template) error
}

type ListInput struct {
	TemplateIDs []string
	WABAId      string
	Status      TemplateStatus
	Category    TemplateCategory
	Language    string
	Search      string
	Options     shared.QueryOptions
}
