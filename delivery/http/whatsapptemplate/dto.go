package whatsapptemplate

import (
	"time"

	whatsapptemplatedomain "vozko/domain/whatsapp/template"
)

type ListTemplatesRequest struct {
	Status          string `json:"status" example:"APPROVED"`
	Category        string `json:"category" example:"MARKETING"`
	Language        string `json:"language" example:"pt_BR"`
	BusinessPhoneID string `json:"businessPhoneId" example:"phone_a1b2c3"`
	WABAId          string `json:"wabaId" example:"waba_a1b2c3"`
	Page            int    `json:"page" example:"1"`
	PageSize        int    `json:"pageSize" example:"20"`
}

type UpdateHeaderMediaURLRequest struct {
	HeaderMediaURL *string `json:"headerMediaUrl" example:"https://cdn.exemplo.com/cabecalho.jpg"`
}

type SendTemplateRequest struct {
	To               string   `json:"to" example:"5511987654321"`
	Template         string   `json:"template" example:"boas_vindas"`
	Language         string   `json:"language" example:"pt_BR"`
	Parameters       []string `json:"parameters"`
	HeaderParameters []string `json:"headerParameters"`
	BusinessPhoneID  string   `json:"businessPhoneId" example:"phone_a1b2c3"`
}

type CreateTemplateRequest struct {
	BusinessPhoneID string                     `json:"businessPhoneId" example:"phone_a1b2c3"`
	Name            string                     `json:"name" example:"boas_vindas"`
	Language        string                     `json:"language" example:"pt_BR"`
	Category        string                     `json:"category" example:"MARKETING"`
	ParameterFormat string                     `json:"parameterFormat" example:"POSITIONAL"`
	Components      []TemplateComponentRequest `json:"components"`
	HeaderMediaURL  *string                    `json:"headerMediaUrl,omitempty" example:"https://cdn.exemplo.com/cabecalho.jpg"`
}

type TemplateComponentRequest struct {
	Type    string                  `json:"type" example:"BODY"`
	Format  string                  `json:"format" example:"TEXT"`
	Text    string                  `json:"text" example:"Olá {{1}}, seja bem-vindo!"`
	Buttons []TemplateButtonRequest `json:"buttons"`
	Example *TemplateExampleRequest `json:"example"`
}

type TemplateButtonRequest struct {
	Type        string `json:"type" example:"URL"`
	Text        string `json:"text" example:"Acessar site"`
	URL         string `json:"url" example:"https://exemplo.com.br"`
	PhoneNumber string `json:"phone_number" example:"5511987654321"`
	Example     string `json:"example" example:"https://exemplo.com.br/promo"`
}

type TemplateExampleRequest struct {
	HeaderText      []string                   `json:"header_text"`
	HeaderHandle    []string                   `json:"header_handle"`
	BodyText        [][]string                 `json:"body_text"`
	BodyTextNamed   []NamedParamExampleRequest `json:"body_text_named_params"`
	HeaderTextNamed []NamedParamExampleRequest `json:"header_text_named_params"`
}

type NamedParamExampleRequest struct {
	ParamName string `json:"param_name" example:"nome"`
	Example   string `json:"example" example:"Maria"`
}

type ReplicateTemplateRequest struct {
	TargetBusinessPhoneID string `json:"targetBusinessPhoneId" example:"phone_d4e5f6"`
}

type MessageResponse struct {
	Message string `json:"message" example:"Template deleted successfully"`
}

type templateResponse struct {
	ID               string                                     `json:"id"`
	ExternalID       string                                     `json:"externalId"`
	WABAId           string                                     `json:"wabaId"`
	WABAName         string                                     `json:"wabaName,omitempty"`
	Name             string                                     `json:"name"`
	Language         string                                     `json:"language"`
	Category         whatsapptemplatedomain.TemplateCategory    `json:"category"`
	Status           whatsapptemplatedomain.TemplateStatus      `json:"status"`
	ParameterFormat  whatsapptemplatedomain.ParameterFormat     `json:"parameterFormat"`
	Components       []whatsapptemplatedomain.TemplateComponent `json:"components"`
	HeaderMediaURL   *string                                    `json:"headerMediaUrl,omitempty"`
	HeaderMediaID    *string                                    `json:"headerMediaId,omitempty"`
	UsabilityStatus  whatsapptemplatedomain.UsabilityStatus     `json:"usabilityStatus"`
	UsabilityMessage string                                     `json:"usabilityMessage,omitempty"`
	CreatedAt        time.Time                                  `json:"createdAt"`
	UpdatedAt        time.Time                                  `json:"updatedAt"`
}

func toTemplateResponse(t *whatsapptemplatedomain.Template) templateResponse {
	return templateResponse{
		ID:               t.ID,
		ExternalID:       t.ExternalID,
		WABAId:           t.WABAId,
		WABAName:         t.WABAName,
		Name:             t.Name,
		Language:         t.Language,
		Category:         t.Category,
		Status:           t.Status,
		ParameterFormat:  t.GetEffectiveParameterFormat(),
		Components:       t.Components,
		HeaderMediaURL:   t.HeaderMediaURL,
		HeaderMediaID:    t.HeaderMediaID,
		UsabilityStatus:  t.GetUsabilityStatus(),
		UsabilityMessage: t.GetUsabilityMessage(),
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
	}
}

func toTemplateResponses(templates []*whatsapptemplatedomain.Template) []templateResponse {
	responses := make([]templateResponse, len(templates))
	for i, t := range templates {
		responses[i] = toTemplateResponse(t)
	}
	return responses
}
