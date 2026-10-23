package campaignaudiencehttp

import (
	"encoding/json"
	"strings"

	"vozko/delivery/http/audiencex"
	"vozko/delivery/http/unofficial_whatsapp"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

type ChannelFields struct {
	TemplateID      string                              `json:"templateId,omitempty" example:"6f1d1c1e-7a1b-4c2d-9e3f-111111111111"`
	BusinessPhoneID string                              `json:"businessPhoneId,omitempty"`
	InstanceID      string                              `json:"instanceId,omitempty"`
	Message         *unofficial_whatsapp.MessageSpecDTO `json:"message,omitempty"`
	Subject         string                              `json:"subject,omitempty" example:"Oferta para {{first_name|cliente}}"`
	Document        json.RawMessage                     `json:"document,omitempty" swaggertype:"object"`
	Kind            string                              `json:"kind,omitempty" enums:"marketing,transactional"`
	WorkflowID      string                              `json:"workflowId,omitempty" example:"0d6c2f1a-4b5e-4c7d-8e9f-222222222222"`
	AgentID         string                              `json:"agentId,omitempty" example:"0d6c2f1a-4b5e-4c7d-8e9f-333333333333"`
}

func (f ChannelFields) params() (campaignaudience_usecase.Params, error) {
	p := campaignaudience_usecase.Params{
		TemplateID: strings.TrimSpace(f.TemplateID), BusinessPhoneID: strings.TrimSpace(f.BusinessPhoneID),
		InstanceID: strings.TrimSpace(f.InstanceID), Subject: f.Subject, Kind: strings.TrimSpace(f.Kind),
		Automation: campaign.Automation{WorkflowID: strings.TrimSpace(f.WorkflowID), AgentID: strings.TrimSpace(f.AgentID)},
	}
	if f.Message != nil {
		message := f.Message.ToDomain()
		p.Message = &message
	}
	if len(f.Document) > 0 && string(f.Document) != "null" {
		doc, err := emaildesign.Decode(f.Document)
		if err != nil {
			return campaignaudience_usecase.Params{}, err
		}
		p.Document = &doc
	}
	return p, nil
}

type CountRequest struct {
	audiencex.AudienceRequest
	ChannelFields
}

type LeadSearchRequest struct {
	CountRequest
	Search      string `json:"search,omitempty" example:"ana"`
	Status      string `json:"status,omitempty" enums:"ready,out,undecided"`
	Reason      string `json:"reason,omitempty" example:"no_email"`
	Variable    string `json:"variable,omitempty" example:"2"`
	MetadataKey string `json:"metadataKey,omitempty" example:"turma"`
	Page        int    `json:"page" example:"1"`
	Limit       int    `json:"limit" example:"50"`
}

func (r LeadSearchRequest) query() campaignaudience_usecase.LeadsQuery {
	return campaignaudience_usecase.LeadsQuery{Page: r.Page, Limit: r.Limit, Filter: campaignaudience.LeadFilter{
		Status: campaignaudience.LeadStatus(strings.TrimSpace(r.Status)), Reason: strings.TrimSpace(r.Reason), Variable: strings.TrimSpace(r.Variable),
		MetadataKey: strings.TrimSpace(r.MetadataKey), Search: r.Search,
	}}
}

type ListContext struct {
	ChannelFields
	Variables []audiencex.VariableRequest `json:"variables,omitempty"`
}

func (c ListContext) view() (campaignaudience_usecase.View, error) {
	params, err := c.params()
	if err != nil {
		return campaignaudience_usecase.View{}, err
	}
	return campaignaudience_usecase.View{Params: params, Variables: audiencex.Variables(c.Variables)}, nil
}

type UploadTextRequest struct {
	ListContext
	FileName string `json:"fileName,omitempty" example:"colado.csv"`
	Text     string `json:"text,omitempty" example:"numero;nome\n5511999990000;Maria"`
}

type RowSearchRequest struct {
	ListContext
	Search      string `json:"search,omitempty" example:"maria"`
	MissingOnly bool   `json:"missingOnly,omitempty"`
	Page        int    `json:"page" example:"1"`
	Limit       int    `json:"limit" example:"50"`
}

type RowRequest struct {
	Contact   string         `json:"contact" example:"(11) 90001-0142"`
	Name      string         `json:"name,omitempty" example:"Maria"`
	Variables []string       `json:"variables,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (r RowRequest) input() campaignaudience_usecase.RowInput {
	return campaignaudience_usecase.RowInput{Contact: r.Contact, Name: r.Name, Variables: r.Variables, Metadata: r.Metadata}
}

type RowResponse struct {
	ID        string         `json:"id"`
	Line      int            `json:"line"`
	Contact   string         `json:"contact" example:"5511900010142"`
	Name      string         `json:"name"`
	Variables []string       `json:"variables"`
	Metadata  map[string]any `json:"metadata"`
}

func rowResponse(r campaignaudience.Row) RowResponse {
	out := RowResponse{ID: r.ID, Line: r.Line, Contact: r.Contact, Name: r.Name, Variables: r.Variables, Metadata: r.Metadata}
	if out.Variables == nil {
		out.Variables = []string{}
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	return out
}

type SampleRequest struct {
	ChannelFields
	Variables []audiencex.VariableRequest `json:"variables,omitempty"`
	Format    string                      `json:"format,omitempty" enums:"csv,xlsx" example:"csv"`
}

type SuggestionsRequest struct {
	Suggestions []campaignaudience.Suggestion `json:"suggestions"`
}

type DeletedResponse struct {
	ID string `json:"id"`
}
