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

// The single-target template sender used to live here as SendTemplateMessageInput
// + SendTemplateMessageUseCase, with billing behind `if WorkspaceID != ""`. It
// has been replaced by BilledTemplateSendUseCase (billed_sender.go), whose input
// makes the workspace and the idempotency key required fields rather than
// optional ones — so an unbilled send is not expressible.

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

// SetTemplateHeaderMediaUseCase resolves the header media for a media-header
// template: it downloads the given public URL, uploads it to the channel's
// WhatsApp /media endpoint to mint the media id that sends attach by, and links
// both URL and id to the template. A nil/empty URL clears the header media.
// It is invoked when a media-header template is first created and by the PATCH
// /whatsapp/templates/{id}/header-media endpoint, so both paths share one
// download/upload/link implementation.
type SetTemplateHeaderMediaUseCase interface {
	Execute(input SetTemplateHeaderMediaInput) error
}

type DeleteTemplateInput struct {
	TemplateID string
}

type DeleteTemplateUseCase interface {
	Execute(input DeleteTemplateInput) error
}
