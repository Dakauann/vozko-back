package audiencex

import (
	"vozko/delivery/http/httpx"
	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
)

type VariableRequest struct {
	Key     string `json:"key" example:"1"`
	Source  string `json:"source,omitempty" example:"lead.first_name"`
	Value   string `json:"value,omitempty" example:"1º ano"`
	Missing string `json:"missing,omitempty" enums:"text,exclude" example:"text"`
	Text    string `json:"text,omitempty" example:"cliente"`
}

type AudienceRequest struct {
	Source    string                      `json:"source" enums:"leads,list" example:"leads"`
	Selection *httpx.LeadSelectionRequest `json:"selection,omitempty"`
	ListID    string                      `json:"listId,omitempty"`
	Variables []VariableRequest           `json:"variables,omitempty"`
}

func Variables(in []VariableRequest) []audience.Variable {
	out := make([]audience.Variable, 0, len(in))
	for _, v := range in {
		out = append(out, audience.Variable{Key: v.Key, Source: campaign.BindingSource(v.Source), Value: v.Value, Missing: audience.MissingPolicy(v.Missing), Text: v.Text})
	}
	return out
}

func (r AudienceRequest) Spec() audience.Spec {
	spec := audience.Spec{Source: audience.Source(r.Source), ListID: r.ListID, Variables: Variables(r.Variables)}
	if r.Selection != nil {
		s := r.Selection.Selection()
		spec.Selection = &s
	}
	return spec
}

func (r *AudienceRequest) Request(userID string, isAdmin bool) *audience.Request {
	if r == nil {
		return nil
	}
	return &audience.Request{Spec: r.Spec(), UserID: userID, IsAdmin: isAdmin}
}
