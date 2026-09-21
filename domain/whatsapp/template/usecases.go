package template

import "vozko/domain/shared"

type ListUseCase interface {
	Execute(input ListInput) (*shared.PaginatedResult[*Template], error)
}

type GetUseCase interface {
	Execute(templateID string) (*Template, error)
}

type SyncTemplatesInput struct {
	BusinessPhoneID string
	PageSize        int
}

type SyncTemplatesUseCase interface {
	Execute(input SyncTemplatesInput) ([]*Template, error)
}

type ReconcileTemplatesUseCase interface {
	Execute() error
}

type SyncTemplateInput struct {
	TemplateID string
}

type SyncTemplateUseCase interface {
	Execute(input SyncTemplateInput) (*Template, error)
}

type CreateTemplateInput struct {
	BusinessPhoneID string
	Name            string
	Language        string
	Category        TemplateCategory
	ParameterFormat string
	Components      []TemplateComponent
	HeaderMediaURL  *string
}

type CreateTemplateOutput struct {
	ID             string         `json:"id"`
	ExternalID     string         `json:"externalId"`
	Name           string         `json:"name"`
	Status         TemplateStatus `json:"status"`
	RejectedReason string         `json:"rejectedReason,omitempty"`
}

type CreateTemplateUseCase interface {
	Execute(input CreateTemplateInput) (*CreateTemplateOutput, error)
}

type ReplicateTemplateInput struct {
	TemplateID            string
	TargetBusinessPhoneID string
}

type ReplicateTemplateUseCase interface {
	Execute(input ReplicateTemplateInput) (*CreateTemplateOutput, error)
}

type SetTemplateHeaderMediaInput struct {
	TemplateID     string
	HeaderMediaURL *string
}

type SetTemplateHeaderMediaUseCase interface {
	Execute(input SetTemplateHeaderMediaInput) error
}

type DeleteTemplateInput struct {
	TemplateID string
}

type DeleteTemplateUseCase interface {
	Execute(input DeleteTemplateInput) error
}
