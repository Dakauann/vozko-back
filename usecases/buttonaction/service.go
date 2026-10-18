package buttonaction_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/cache"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

const settingsCacheTTL = 10 * time.Minute

type ParamOwnership interface {
	Check(ctx context.Context, workspaceID string, kind buttonaction.Kind, params buttonaction.Params) error
}

type Deps struct {
	Templates   template.WorkspaceTemplatesUseCase
	Campaigns   wc.CampaignAccessUseCase
	Settings    buttonaction.TemplateSettingsStore
	Routes      buttonaction.CampaignRoutesStore
	Ownership   ParamOwnership
	Permissions workspace.PermissionChecker
	Memo        cache.Memo
	Versions    cache.Versions
	Now         func() time.Time
}

type Service struct {
	deps   Deps
	editor template.ContentEditor
}

type Actor struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool
	Departments *wd.DepartmentFilter
}

func NewService(deps Deps) (*Service, error) {
	if deps.Templates == nil || deps.Campaigns == nil || deps.Settings == nil || deps.Routes == nil ||
		deps.Ownership == nil || deps.Permissions == nil || deps.Memo == nil || deps.Versions == nil {
		return nil, buttonaction.ErrUnavailable
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{deps: deps}, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func requireActor(a Actor) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return buttonaction.ErrWorkspaceRequired
	}
	if strings.TrimSpace(a.UserID) == "" {
		return buttonaction.ErrActorRequired
	}
	return nil
}

func (s *Service) template(workspaceID, templateID string) (*template.Template, error) {
	tmpl, err := s.deps.Templates.Get(workspaceID, templateID)
	if errors.Is(err, template.ErrTemplateAccessDenied) || errors.Is(err, template.ErrTemplateNotFound) || (err == nil && tmpl == nil) {
		return nil, buttonaction.ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load template %s: %w", templateID, err)
	}
	return tmpl, nil
}

func (s *Service) campaign(a Actor, campaignID string) (*wc.Campaign, error) {
	c, err := s.deps.Campaigns.Owned(a.WorkspaceID, a.Departments, campaignID)
	if errors.Is(err, wc.ErrCampaignNotFound) || (err == nil && c == nil) {
		return nil, buttonaction.ErrCampaignNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load campaign %s: %w", campaignID, err)
	}
	if strings.TrimSpace(c.TemplateID) == "" {
		return nil, buttonaction.ErrCampaignNoTemplate
	}
	return c, nil
}

func (s *Service) requireKind(a Actor, kind buttonaction.Kind) error {
	requirements, err := buttonaction.RequirementsOf(kind)
	if err != nil {
		return err
	}
	entries, err := requirements.Entries()
	if err != nil {
		return fmt.Errorf("%w: %v", buttonaction.ErrForbidden, err)
	}
	if len(entries) == 0 {
		return nil
	}
	if !workspace.HoldsAll(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, entries) {
		return fmt.Errorf("%w: %s", buttonaction.ErrForbidden, kind)
	}
	return nil
}

func (s *Service) buildActions(ctx context.Context, a Actor, tmpl *template.Template, inputs []buttonaction.Input, current []buttonaction.ButtonAction) ([]buttonaction.ButtonAction, error) {
	actions := make([]buttonaction.ButtonAction, 0, len(inputs))
	for _, in := range inputs {
		action, err := buttonaction.BindButtonAction(tmpl, in, current)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	if err := buttonaction.EnsureOnePerButton(actions); err != nil {
		return nil, err
	}
	for _, action := range changedActions(current, actions) {
		if err := s.requireKind(a, action.Kind); err != nil {
			return nil, err
		}
	}
	for _, action := range actions {
		if sameAction(current, action) {
			continue
		}
		if err := s.deps.Ownership.Check(ctx, a.WorkspaceID, action.Kind, action.Params); err != nil {
			return nil, err
		}
	}
	return actions, nil
}

func changedActions(current, next []buttonaction.ButtonAction) []buttonaction.ButtonAction {
	var changed []buttonaction.ButtonAction
	for _, action := range next {
		if !sameAction(current, action) {
			changed = append(changed, action)
		}
	}
	for _, action := range current {
		if !sameAction(next, action) {
			changed = append(changed, action)
		}
	}
	return changed
}

func sameAction(list []buttonaction.ButtonAction, action buttonaction.ButtonAction) bool {
	for _, other := range list {
		if other.ButtonIndex == action.ButtonIndex && other.Kind == action.Kind && other.Params == action.Params && other.Confirmation == action.Confirmation {
			return true
		}
	}
	return false
}

func templateScope(workspaceID string, ref buttonaction.TemplateRef) string {
	return "tmpl:" + workspaceID + ":" + ref.Name + ":" + ref.Language
}

func campaignScope(workspaceID, campaignID string) string {
	return "camp:" + workspaceID + ":" + campaignID
}

func (s *Service) remembered(scope string) (string, bool) {
	version, err := s.deps.Versions.Version(scope)
	if err != nil {
		return "", false
	}
	return "buttonactions:" + scope + ":" + version, true
}

func (s *Service) cachedSettings(ctx context.Context, workspaceID string, ref buttonaction.TemplateRef) (buttonaction.TemplateSettings, error) {
	read := func(ctx context.Context) (buttonaction.TemplateSettings, error) {
		return s.deps.Settings.Get(ctx, workspaceID, ref)
	}
	key, ok := s.remembered(templateScope(workspaceID, ref))
	if !ok {
		return read(ctx)
	}
	return cache.Remember(ctx, s.deps.Memo, key, settingsCacheTTL, read)
}

func (s *Service) cachedRoutes(ctx context.Context, workspaceID, campaignID string) (buttonaction.CampaignRoutes, error) {
	if strings.TrimSpace(campaignID) == "" {
		return buttonaction.CampaignRoutes{}, nil
	}
	read := func(ctx context.Context) (buttonaction.CampaignRoutes, error) {
		return s.deps.Routes.Get(ctx, workspaceID, campaignID)
	}
	key, ok := s.remembered(campaignScope(workspaceID, campaignID))
	if !ok {
		return read(ctx)
	}
	return cache.Remember(ctx, s.deps.Memo, key, settingsCacheTTL, read)
}

func (s *Service) forget(scope string) error {
	if err := s.deps.Versions.Bump(scope); err != nil {
		return fmt.Errorf("saved, but the cached button actions could not be refreshed: %w", err)
	}
	return nil
}
