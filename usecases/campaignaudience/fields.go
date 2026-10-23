package campaignaudience_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/customfield"
	"vozko/domain/whatsapp/template"
)

type FieldKind string

const (
	FieldLead   FieldKind = "lead"
	FieldCustom FieldKind = "custom"
)

type Field struct {
	Source campaign.BindingSource `json:"source"`
	Kind   FieldKind              `json:"kind"`
	Column string                 `json:"column"`
	Key    string                 `json:"key,omitempty"`
	Label  string                 `json:"label,omitempty"`
	Type   customfield.FieldType  `json:"type,omitempty"`
}

func (s *Service) definitions(workspaceID string) ([]*customfield.Definition, error) {
	defs, err := s.deps.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return defs, nil
}

func (s *Service) Fields(_ context.Context, a Actor) ([]Field, error) {
	defs, err := s.definitions(a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	fields := make([]Field, 0, len(campaign.BindableSources()))
	for _, source := range campaign.BindableSources() {
		fields = append(fields, Field{Source: source, Kind: FieldLead, Column: campaign.FieldColumn(source, nil)})
	}
	for _, def := range campaign.BindableCustomFields(defs) {
		source := campaign.CustomBinding(def.Key)
		fields = append(fields, Field{Source: source, Kind: FieldCustom, Column: campaign.FieldColumn(source, defs), Key: def.Key, Label: def.Label, Type: def.Type})
	}
	return fields, nil
}

type TemplateReader interface {
	Get(workspaceID, templateID string) (*template.Template, error)
}

type TemplateSuggestions struct {
	TemplateID  string                        `json:"templateId"`
	Suggestions []campaignaudience.Suggestion `json:"suggestions"`
}

func (s *Service) suggestionTemplate(a Actor, templateID string) (*template.Template, error) {
	if s.deps.Templates == nil || s.deps.Suggestions == nil {
		return nil, campaignaudience.ErrUnavailable
	}
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return nil, campaignaudience.ErrTemplateRequired
	}
	tmpl, err := s.deps.Templates.Get(a.WorkspaceID, templateID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, template.ErrTemplateNotFound
	}
	return tmpl, nil
}

func (s *Service) TemplateSuggestions(ctx context.Context, a Actor, templateID string) (TemplateSuggestions, error) {
	tmpl, err := s.suggestionTemplate(a, templateID)
	if err != nil {
		return TemplateSuggestions{}, err
	}
	stored, err := s.deps.Suggestions.Get(ctx, a.WorkspaceID, tmpl.ID)
	if err != nil {
		return TemplateSuggestions{}, err
	}
	if stored == nil {
		stored = []campaignaudience.Suggestion{}
	}
	return TemplateSuggestions{TemplateID: tmpl.ID, Suggestions: stored}, nil
}

func (s *Service) SaveTemplateSuggestions(ctx context.Context, a Actor, templateID string, suggestions []campaignaudience.Suggestion) (TemplateSuggestions, error) {
	tmpl, err := s.suggestionTemplate(a, templateID)
	if err != nil {
		return TemplateSuggestions{}, err
	}
	defs, err := s.definitions(a.WorkspaceID)
	if err != nil {
		return TemplateSuggestions{}, err
	}
	valid, err := campaignaudience.ValidateSuggestions(suggestions, tmpl.ParameterCount(), defs)
	if err != nil {
		return TemplateSuggestions{}, err
	}
	if err := s.deps.Suggestions.Replace(ctx, a.WorkspaceID, tmpl.ID, a.UserID, valid); err != nil {
		return TemplateSuggestions{}, err
	}
	return TemplateSuggestions{TemplateID: tmpl.ID, Suggestions: valid}, nil
}
