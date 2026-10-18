package buttonaction

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/whatsapp/template"
)

const FirstVersion int64 = 1

type TemplateSettings struct {
	WorkspaceID      string         `json:"workspaceId"`
	Template         TemplateRef    `json:"template"`
	Actions          []ButtonAction `json:"actions"`
	ExitConfirmation string         `json:"exitConfirmation,omitempty"`
	Version          int64          `json:"version"`
	UpdatedBy        string         `json:"updatedBy,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type CampaignRoutes struct {
	WorkspaceID string           `json:"workspaceId"`
	CampaignID  string           `json:"campaignId"`
	Template    TemplateRef      `json:"template"`
	Routes      []ButtonAction   `json:"routes"`
	ExitRisk    *Acknowledgement `json:"exitRiskAcknowledgement,omitempty"`
	Version     int64            `json:"version"`
	UpdatedBy   string           `json:"updatedBy,omitempty"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

type CampaignAdmission struct {
	Template TemplateRef
	ExitRisk *Acknowledgement
}

func (r CampaignRoutes) Admit(a CampaignAdmission) (CampaignRoutes, bool) {
	stored := r.Version > FirstVersion
	changed := stored && r.Template != a.Template
	if !changed && a.ExitRisk == nil {
		return r, false
	}
	next := r
	if r.Template != a.Template {
		next.Template, next.Routes = a.Template, nil
	}
	next.ExitRisk = a.ExitRisk
	if a.ExitRisk != nil {
		next.UpdatedBy = a.ExitRisk.By
	}
	return next, true
}

func NewExitConfirmation(confirmation string) (string, error) {
	confirmation = strings.TrimSpace(confirmation)
	if utf8.RuneCountInString(confirmation) > MaxConfirmationLength {
		return "", ErrExitConfirmationLong
	}
	return confirmation, nil
}

func (s TemplateSettings) ActionAt(index int) (ButtonAction, bool) {
	for _, action := range s.Actions {
		if action.ButtonIndex == index {
			return action, true
		}
	}
	return ButtonAction{}, false
}

func (r CampaignRoutes) RouteAt(index int) (ButtonAction, bool) {
	for _, route := range r.Routes {
		if route.ButtonIndex == index {
			return route, true
		}
	}
	return ButtonAction{}, false
}

func (r CampaignRoutes) appliesTo(t *template.Template) bool {
	return r.CampaignID != "" && r.Template == RefOf(t)
}

func (s TemplateSettings) Normalized() TemplateSettings {
	if s.Version < FirstVersion {
		s.Version = FirstVersion
	}
	if s.Actions == nil {
		s.Actions = []ButtonAction{}
	}
	return s
}

func (r CampaignRoutes) Normalized() CampaignRoutes {
	if r.Version < FirstVersion {
		r.Version = FirstVersion
	}
	if r.Routes == nil {
		r.Routes = []ButtonAction{}
	}
	return r
}

type Acknowledgement struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

func (a *Acknowledgement) Given() bool {
	return a != nil && strings.TrimSpace(a.By) != "" && !a.At.IsZero()
}

func RequireExitRiskAcknowledged(verdict Verdict, ack *Acknowledgement) error {
	if verdict.State == VerdictOK || ack.Given() {
		return nil
	}
	return ErrExitRiskNotAcknowledged
}

type ExitGates struct {
	TemplateExitRequired            bool
	CampaignAcknowledgementRequired bool
}

func ExitGatesOf(category template.TemplateCategory, verdict Verdict) ExitGates {
	missing := verdict.State != VerdictOK
	return ExitGates{TemplateExitRequired: missing && isMarketing(category), CampaignAcknowledgementRequired: missing}
}

func RequireTemplateExit(category template.TemplateCategory, verdict Verdict) error {
	if ExitGatesOf(category, verdict).TemplateExitRequired {
		return ErrMarketingNeedsExit
	}
	return nil
}

func isMarketing(category template.TemplateCategory) bool {
	return template.TemplateCategory(strings.ToUpper(strings.TrimSpace(string(category)))) == template.TemplateCategoryMarketing
}
