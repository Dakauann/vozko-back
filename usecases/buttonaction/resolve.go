package buttonaction_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type TemplateKey struct {
	WorkspaceID  string
	TemplateID   string
	TemplateName string
	Language     string
}

type TapInput struct {
	TemplateKey
	CampaignID string
	Payload    string
	Text       string
}

type ResolveInput struct {
	TemplateKey
	CampaignID  string
	ButtonIndex int
}

type TemplateExit struct {
	Template     buttonaction.TemplateRef
	Words        []string
	Confirmation string
}

func (s *Service) ResolveTap(ctx context.Context, in TapInput) (buttonaction.Resolution, error) {
	tmpl, reason, err := s.templateFor(in.TemplateKey)
	if err != nil || tmpl == nil {
		return buttonaction.Resolution{Reason: reason}, err
	}
	index, ok := buttonaction.MatchButton(tmpl, in.Payload, in.Text)
	if !ok {
		return buttonaction.Resolution{Template: buttonaction.RefOf(tmpl), Reason: buttonaction.ReasonNoMatch}, nil
	}
	return s.resolve(ctx, in.WorkspaceID, tmpl, in.CampaignID, index)
}

func (s *Service) ResolveAction(ctx context.Context, in ResolveInput) (buttonaction.Resolution, error) {
	tmpl, reason, err := s.templateFor(in.TemplateKey)
	if err != nil || tmpl == nil {
		return buttonaction.Resolution{Reason: reason, ButtonIndex: in.ButtonIndex}, err
	}
	return s.resolve(ctx, in.WorkspaceID, tmpl, in.CampaignID, in.ButtonIndex)
}

func (s *Service) TemplateExit(ctx context.Context, key TemplateKey) (*TemplateExit, error) {
	tmpl, _, err := s.templateFor(key)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, buttonaction.ErrTemplateNotFound
	}
	ref := buttonaction.RefOf(tmpl)
	settings, err := s.cachedSettings(ctx, key.WorkspaceID, ref)
	if err != nil {
		return nil, err
	}
	return &TemplateExit{Template: ref, Words: buttonaction.ShownExitWords(tmpl), Confirmation: settings.ExitConfirmation}, nil
}

func (s *Service) resolve(ctx context.Context, workspaceID string, tmpl *template.Template, campaignID string, index int) (buttonaction.Resolution, error) {
	settings, err := s.cachedSettings(ctx, workspaceID, buttonaction.RefOf(tmpl))
	if err != nil {
		return buttonaction.Resolution{}, err
	}
	routes, err := s.cachedRoutes(ctx, workspaceID, campaignID)
	if err != nil {
		return buttonaction.Resolution{}, err
	}
	return buttonaction.Resolve(tmpl, settings, routes, index), nil
}

func (s *Service) templateFor(key TemplateKey) (*template.Template, buttonaction.Reason, error) {
	if strings.TrimSpace(key.WorkspaceID) == "" {
		return nil, "", buttonaction.ErrWorkspaceRequired
	}
	if strings.TrimSpace(key.TemplateID) != "" {
		tmpl, err := s.template(key.WorkspaceID, key.TemplateID)
		if errors.Is(err, buttonaction.ErrTemplateNotFound) {
			return nil, buttonaction.ReasonTemplateNotFound, nil
		}
		return tmpl, "", err
	}
	return s.templateByName(key)
}

func (s *Service) templateByName(key TemplateKey) (*template.Template, buttonaction.Reason, error) {
	ref := buttonaction.NewTemplateRef(key.TemplateName, key.Language)
	if ref.Empty() {
		return nil, buttonaction.ReasonTemplateNotFound, nil
	}
	found, err := s.deps.Templates.FindByName(key.WorkspaceID, ref.Name, ref.Language)
	if err != nil {
		return nil, "", err
	}
	var matches []*template.Template
	for _, candidate := range found {
		if candidate != nil && buttonaction.RefOf(candidate) == ref {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return nil, buttonaction.ReasonTemplateNotFound, nil
	}
	if !sameQuickReplies(matches) {
		return nil, buttonaction.ReasonTemplateAmbiguous, nil
	}
	return matches[0], "", nil
}

func sameQuickReplies(templates []*template.Template) bool {
	first := templates[0].QuickReplies()
	for _, other := range templates[1:] {
		replies := other.QuickReplies()
		if len(replies) != len(first) {
			return false
		}
		for i := range replies {
			if replies[i].Index != first[i].Index || shared.FoldForMatch(replies[i].Text) != shared.FoldForMatch(first[i].Text) {
				return false
			}
		}
	}
	return true
}
