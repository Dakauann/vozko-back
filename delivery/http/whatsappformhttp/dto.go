package whatsappformhttp

import (
	"time"

	"vozko/domain/whatsappform"
)

type CreateRequest struct {
	Name        string `json:"name" example:"Captação de lead"`
	Description string `json:"description,omitempty" example:"Formulário dos anúncios de outubro"`
	Starter     string `json:"starter,omitempty" enums:"lead_capture,appointment,nps,sign_up,contact_us,quote,documents" example:"lead_capture"`
}

type RenameRequest struct {
	Name        string `json:"name" example:"Captação de lead"`
	Description string `json:"description,omitempty"`
}

type DraftRequest struct {
	BaseVersionID string                `json:"baseVersionId" example:"5d1c2b3a-4e5f-4a6b-8c7d-9e0f1a2b3c4d"`
	Document      whatsappform.Document `json:"document"`
}

type CheckRequest struct {
	Document whatsappform.Document `json:"document"`
}

type SendRequest struct {
	FormID string                `json:"formId" example:"0f8c2d1e-5b6a-4c3d-9e8f-7a6b5c4d3e2f"`
	Copy   whatsappform.SendCopy `json:"copy"`
}

type BindingOptionResponse struct {
	Target     whatsappform.BindingTarget `json:"target" example:"email"`
	FieldKinds []whatsappform.FieldKind   `json:"fieldKinds"`
}

type CustomFieldOptionResponse struct {
	Key        string                   `json:"key" example:"plano"`
	Label      string                   `json:"label" example:"Plano"`
	Type       string                   `json:"type" example:"select"`
	Options    []string                 `json:"options,omitempty"`
	FieldKinds []whatsappform.FieldKind `json:"fieldKinds"`
}

type OptionsResponse struct {
	FieldKinds      []whatsappform.FieldKind    `json:"fieldKinds"`
	DisplayKinds    []whatsappform.DisplayKind  `json:"displayKinds"`
	Categories      []whatsappform.Category     `json:"categories"`
	Limits          whatsappform.Limits         `json:"limits"`
	Bindings        []BindingOptionResponse     `json:"bindings"`
	CustomFields    []CustomFieldOptionResponse `json:"customFields"`
	Starters        []whatsappform.Starter      `json:"starters"`
	FlowJSONVersion string                      `json:"flowJsonVersion" example:"7.3"`
}

type FormListResponse struct {
	Items []whatsappform.FormSummary `json:"items"`
}

type FormDetailResponse struct {
	Form         whatsappform.Form          `json:"form"`
	Draft        whatsappform.Version       `json:"draft"`
	Published    *whatsappform.Version      `json:"published,omitempty"`
	Publications []whatsappform.Publication `json:"publications"`
}

type DraftResponse struct {
	Draft whatsappform.Version `json:"draft"`
}

type ProblemsResponse struct {
	Problems whatsappform.Problems `json:"problems"`
}

type OutcomeResponse struct {
	MetaWABAID string                         `json:"metaWabaId" example:"104857629384756"`
	Status     whatsappform.PublicationStatus `json:"status,omitempty" example:"published"`
	Problems   whatsappform.Problems          `json:"problems,omitempty"`
	Error      string                         `json:"error,omitempty"`
	Code       string                         `json:"code,omitempty" example:"whatsapp_form_provider_unsupported"`
}

type PublishResponse struct {
	Outcomes []OutcomeResponse  `json:"outcomes"`
	Form     FormDetailResponse `json:"form"`
}

type PreviewResponse struct {
	PreviewURL string     `json:"previewUrl" example:"https://business.facebook.com/wa/manage/flows/123/preview"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type VersionsResponse struct {
	Items []whatsappform.Version `json:"items"`
}

type SendableFormResponse struct {
	FormID      string `json:"formId"`
	Name        string `json:"name"`
	VersionID   string `json:"versionId"`
	MetaFlowID  string `json:"metaFlowId"`
	FirstScreen string `json:"firstScreen" example:"SCREEN_A"`
}

type SendableResponse struct {
	Items []SendableFormResponse `json:"items"`
}

type SendsResponse struct {
	Items []whatsappform.Send `json:"items"`
	Next  string              `json:"next,omitempty"`
}

type ResponseRowResponse struct {
	whatsappform.Response
	LeadName string `json:"leadName,omitempty" example:"Ana Souza"`
}

type ResponsesResponse struct {
	Items []ResponseRowResponse `json:"items"`
	Next  string                `json:"next,omitempty"`
}

type FormRefResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ResponseDetailResponse struct {
	Response whatsappform.Response `json:"response"`
	Form     *FormRefResponse      `json:"form,omitempty"`
	Version  *whatsappform.Version `json:"version,omitempty"`
	LeadName string                `json:"leadName,omitempty"`
	Revealed bool                  `json:"revealed"`
}

type DistributionResponse struct {
	Counts map[string]int64 `json:"counts"`
}

type AutomationResponse struct {
	WorkflowID string `json:"workflowId"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	Use        string `json:"use" enums:"trigger,node"`
}

type AutomationsResponse struct {
	Items []AutomationResponse `json:"items"`
}

type AutomationCreatedResponse struct {
	WorkflowID string `json:"workflowId"`
}
