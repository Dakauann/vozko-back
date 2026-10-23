package buttonactionhttp

import (
	"time"

	"vozko/delivery/http/httpx"
	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type SaveTemplateActionsRequest struct {
	Actions          []httpx.ButtonActionRequest `json:"actions"`
	ExitConfirmation string                      `json:"exitConfirmation,omitempty" example:"Pronto, você não receberá mais mensagens."`
}

type DraftButtonRequest struct {
	Type string `json:"type" example:"QUICK_REPLY"`
	Text string `json:"text" example:"SAIR"`
}

type DraftExitVerdictRequest struct {
	Category      string                      `json:"category" example:"MARKETING"`
	Header        string                      `json:"header" example:"Ofertas de outubro"`
	Body          string                      `json:"body" example:"Olá {{1}}, temos novidades para você."`
	Footer        string                      `json:"footer" example:"Responda /sair para não receber mais"`
	Buttons       []DraftButtonRequest        `json:"buttons"`
	ButtonActions []httpx.ButtonActionRequest `json:"buttonActions"`
}

func (r DraftExitVerdictRequest) draft() buttonaction_usecase.DraftTemplate {
	buttons := make([]template.TemplateButton, 0, len(r.Buttons))
	for _, b := range r.Buttons {
		buttons = append(buttons, template.TemplateButton{Type: b.Type, Text: b.Text})
	}
	return buttonaction_usecase.DraftTemplate{
		Category: template.TemplateCategory(r.Category),
		Header:   r.Header,
		Body:     r.Body,
		Footer:   r.Footer,
		Buttons:  buttons,
		Actions:  httpx.ButtonActionInputs(r.ButtonActions),
	}
}

type DraftExitVerdictResponse struct {
	Verdict ExitVerdictResponse `json:"verdict"`
}

type AllowedKindsResponse struct {
	AllowedKinds []string `json:"allowedKinds" example:"opt_out,hand_off"`
}

type SaveCampaignRoutesRequest struct {
	Routes []httpx.ButtonActionRequest `json:"routes"`
}

type TemplateRefResponse struct {
	Name     string `json:"name" example:"promo_outubro"`
	Language string `json:"language" example:"pt_BR"`
}

type ActionResponse struct {
	ButtonIndex  int                      `json:"buttonIndex" example:"1"`
	ButtonText   string                   `json:"buttonText" example:"SAIR"`
	Kind         string                   `json:"kind" example:"opt_out"`
	Params       httpx.ButtonActionParams `json:"params"`
	Confirmation string                   `json:"confirmation"`
	Stale        bool                     `json:"stale"`
	Version      int64                    `json:"version" example:"2"`
	UpdatedBy    string                   `json:"updatedBy,omitempty"`
	UpdatedAt    *time.Time               `json:"updatedAt,omitempty"`
}

type ButtonResponse struct {
	Index  int             `json:"index" example:"1"`
	Text   string          `json:"text" example:"SAIR"`
	Action *ActionResponse `json:"action"`
	Source string          `json:"source,omitempty" example:"template" enums:"template,campaign"`
	Stale  bool            `json:"stale"`
}

type AcknowledgementResponse struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

type TemplateActionsResponse struct {
	TemplateID       string              `json:"templateId"`
	Template         TemplateRefResponse `json:"template"`
	Version          int64               `json:"version" example:"2"`
	UpdatedBy        string              `json:"updatedBy,omitempty"`
	UpdatedAt        *time.Time          `json:"updatedAt,omitempty"`
	Buttons          []ButtonResponse    `json:"buttons"`
	Actions          []ActionResponse    `json:"actions"`
	ExitConfirmation string              `json:"exitConfirmation"`
	AllowedKinds     []string            `json:"allowedKinds" example:"opt_out,hand_off"`
}

type CampaignRoutesResponse struct {
	CampaignID              string                   `json:"campaignId"`
	TemplateID              string                   `json:"templateId"`
	Template                TemplateRefResponse      `json:"template"`
	Version                 int64                    `json:"version" example:"2"`
	UpdatedBy               string                   `json:"updatedBy,omitempty"`
	UpdatedAt               *time.Time               `json:"updatedAt,omitempty"`
	Routes                  []ActionResponse         `json:"routes"`
	Buttons                 []ButtonResponse         `json:"buttons"`
	ExitRiskAcknowledgement *AcknowledgementResponse `json:"exitRiskAcknowledgement"`
}

type ExitVerdictResponse struct {
	State                           string           `json:"state" example:"ok" enums:"ok,none"`
	Buttons                         []ButtonResponse `json:"buttons"`
	ExitWords                       []string         `json:"exitWords" example:"sair"`
	TemplateExitRequired            bool             `json:"templateExitRequired" example:"false"`
	CampaignAcknowledgementRequired bool             `json:"campaignAcknowledgementRequired" example:"false"`
}

func presentRef(ref buttonaction.TemplateRef) TemplateRefResponse {
	return TemplateRefResponse{Name: ref.Name, Language: ref.Language}
}

func presentAction(a buttonaction.ButtonAction, tmpl *template.Template) ActionResponse {
	return ActionResponse{
		ButtonIndex: a.ButtonIndex, ButtonText: a.ButtonText, Kind: string(a.Kind), Params: httpx.ButtonActionParams(a.Params), Confirmation: a.Confirmation,
		Stale: a.StaleAgainst(tmpl), Version: a.Version, UpdatedBy: a.UpdatedBy, UpdatedAt: optionalTime(a.UpdatedAt),
	}
}

func presentActions(actions []buttonaction.ButtonAction, tmpl *template.Template) []ActionResponse {
	out := make([]ActionResponse, 0, len(actions))
	for _, a := range actions {
		out = append(out, presentAction(a, tmpl))
	}
	return out
}

func presentButtons(buttons []buttonaction.ButtonRoute, tmpl *template.Template) []ButtonResponse {
	out := make([]ButtonResponse, 0, len(buttons))
	for _, b := range buttons {
		button := ButtonResponse{Index: b.Index, Text: b.Text, Source: string(b.Source), Stale: b.Stale}
		if b.Action != nil {
			action := presentAction(*b.Action, tmpl)
			button.Action = &action
		}
		out = append(out, button)
	}
	return out
}

func presentAcknowledgement(ack *buttonaction.Acknowledgement) *AcknowledgementResponse {
	if !ack.Given() {
		return nil
	}
	return &AcknowledgementResponse{By: ack.By, At: ack.At}
}

func presentTemplateActions(v *buttonaction_usecase.TemplateView) TemplateActionsResponse {
	s := v.Settings
	return TemplateActionsResponse{
		TemplateID: v.TemplateID, Template: presentRef(s.Template), Version: s.Version, UpdatedBy: s.UpdatedBy, UpdatedAt: optionalTime(s.UpdatedAt),
		Buttons: presentButtons(v.Buttons, v.Template), Actions: presentActions(s.Actions, v.Template),
		ExitConfirmation: s.ExitConfirmation, AllowedKinds: presentKinds(v.AllowedKinds),
	}
}

func presentKinds(kinds []buttonaction.Kind) []string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

func presentCampaignRoutes(v *buttonaction_usecase.CampaignView) CampaignRoutesResponse {
	r := v.Routes
	return CampaignRoutesResponse{
		CampaignID: v.CampaignID, TemplateID: v.TemplateID, Template: presentRef(buttonaction.RefOf(v.Template)), Version: r.Version,
		UpdatedBy: r.UpdatedBy, UpdatedAt: optionalTime(r.UpdatedAt), Routes: presentActions(r.Routes, v.Template),
		Buttons: presentButtons(v.Buttons, v.Template), ExitRiskAcknowledgement: presentAcknowledgement(r.ExitRisk),
	}
}

func presentVerdict(v *buttonaction.Verdict, gates buttonaction.ExitGates, tmpl *template.Template) ExitVerdictResponse {
	return ExitVerdictResponse{
		State: string(v.State), Buttons: presentButtons(v.Buttons, tmpl), ExitWords: nonNilWords(v.ExitWords),
		TemplateExitRequired: gates.TemplateExitRequired, CampaignAcknowledgementRequired: gates.CampaignAcknowledgementRequired,
	}
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nonNilWords(words []string) []string {
	if words == nil {
		return []string{}
	}
	return words
}
