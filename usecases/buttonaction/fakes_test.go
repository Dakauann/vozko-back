package buttonaction_usecase

import (
	"context"
	"strconv"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	ws         = "ws-1"
	userID     = "user-1"
	templateID = "tmpl-1"
	campaignID = "camp-1"
)

type fakeTemplates struct {
	byID      map[string]*template.Template
	created   []template.CreateTemplateInput
	createErr error
}

func (f *fakeTemplates) List(workspaceID string, input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	out := &shared.PaginatedResult[*template.Template]{}
	for _, t := range f.byID {
		if workspaceID == ws {
			out.Items = append(out.Items, t)
		}
	}
	return out, nil
}

func (f *fakeTemplates) Get(workspaceID, id string) (*template.Template, error) {
	t, ok := f.byID[id]
	if !ok || workspaceID != ws {
		return nil, template.ErrTemplateAccessDenied
	}
	return t, nil
}

func (f *fakeTemplates) Create(workspaceID, grantedBy string, input template.CreateTemplateInput) (*template.CreateTemplateOutput, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, input)
	return &template.CreateTemplateOutput{ID: "new", Name: input.Name}, nil
}

type fakeCampaigns struct {
	byID map[string]*wc.Campaign
}

func (f *fakeCampaigns) Owned(workspaceID string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	c, ok := f.byID[id]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}

type fakeSettings struct {
	rows  map[string]buttonaction.TemplateSettings
	saves int
}

func settingsKey(workspaceID string, ref buttonaction.TemplateRef) string {
	return workspaceID + "|" + ref.Name + "|" + ref.Language
}

func (f *fakeSettings) Get(_ context.Context, workspaceID string, ref buttonaction.TemplateRef) (buttonaction.TemplateSettings, error) {
	s, ok := f.rows[settingsKey(workspaceID, ref)]
	if !ok {
		s = buttonaction.TemplateSettings{WorkspaceID: workspaceID, Template: ref}
	}
	return s.Normalized(), nil
}

func (f *fakeSettings) Save(_ context.Context, s *buttonaction.TemplateSettings, expected int64) error {
	current, _ := f.Get(context.Background(), s.WorkspaceID, s.Template)
	if current.Version != expected {
		return buttonaction.ErrVersionConflict
	}
	s.Version = expected + 1
	f.rows[settingsKey(s.WorkspaceID, s.Template)] = *s
	f.saves++
	return nil
}

type fakeRoutes struct {
	rows map[string]buttonaction.CampaignRoutes
}

func (f *fakeRoutes) Get(_ context.Context, workspaceID, id string) (buttonaction.CampaignRoutes, error) {
	r, ok := f.rows[workspaceID+"|"+id]
	if !ok {
		r = buttonaction.CampaignRoutes{WorkspaceID: workspaceID, CampaignID: id}
	}
	return r.Normalized(), nil
}

func (f *fakeRoutes) Save(_ context.Context, r *buttonaction.CampaignRoutes, expected int64) error {
	current, _ := f.Get(context.Background(), r.WorkspaceID, r.CampaignID)
	if current.Version != expected {
		return buttonaction.ErrVersionConflict
	}
	if r.ExitRisk == nil {
		r.ExitRisk = current.ExitRisk
	}
	r.Version = expected + 1
	f.rows[r.WorkspaceID+"|"+r.CampaignID] = *r
	return nil
}

type fakeOwnership struct {
	refuse map[buttonaction.Kind]bool
	checks int
}

func (f *fakeOwnership) Check(_ context.Context, _ string, kind buttonaction.Kind, _ buttonaction.Params) error {
	f.checks++
	if f.refuse[kind] {
		return buttonaction.ErrParamsNotInWorkspace
	}
	return nil
}

type fakePermissions struct {
	granted map[string]bool
}

func (f fakePermissions) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return f.granted[resource+":"+action]
}

type fakeMemo struct{ values map[string][]byte }

func (m *fakeMemo) Remember(ctx context.Context, key string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	v, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	m.values[key] = v
	return v, nil
}

type fakeVersions struct{ versions map[string]int }

func (v *fakeVersions) Version(scope string) (string, error) {
	return strconv.Itoa(v.versions[scope]), nil
}

func (v *fakeVersions) Bump(scope string) error {
	v.versions[scope]++
	return nil
}

type fixture struct {
	svc        *Service
	templates  *fakeTemplates
	settings   *fakeSettings
	routes     *fakeRoutes
	ownership  *fakeOwnership
	permission fakePermissions
}

func promoTemplate() *template.Template {
	return &template.Template{
		ID: templateID, Name: "promo_outubro", Language: "pt_BR", Category: template.TemplateCategoryMarketing, Status: template.TemplateStatusApproved,
		Components: []template.TemplateComponent{
			{Type: "BODY", Text: "Ofertas da semana."},
			{Type: "BUTTONS", Buttons: []template.TemplateButton{
				{Type: "QUICK_REPLY", Text: "Falar com atendente"},
				{Type: "QUICK_REPLY", Text: "SAIR"},
				{Type: "URL", Text: "Ver loja", URL: "https://loja.exemplo.com"},
			}},
		},
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		templates: &fakeTemplates{byID: map[string]*template.Template{templateID: promoTemplate()}},
		settings:  &fakeSettings{rows: map[string]buttonaction.TemplateSettings{}},
		routes:    &fakeRoutes{rows: map[string]buttonaction.CampaignRoutes{}},
		ownership: &fakeOwnership{refuse: map[buttonaction.Kind]bool{}},
		permission: fakePermissions{granted: map[string]bool{
			"leads:read": true, "leads:assign": true, "members:read": true, "leads:update": true,
		}},
	}
	svc, err := NewService(Deps{
		Templates:   f.templates,
		Campaigns:   &fakeCampaigns{byID: map[string]*wc.Campaign{campaignID: {ID: campaignID, WorkspaceID: ws, TemplateID: templateID}}},
		Settings:    f.settings,
		Routes:      f.routes,
		Ownership:   f.ownership,
		Permissions: f.permission,
		Memo:        &fakeMemo{values: map[string][]byte{}},
		Versions:    &fakeVersions{versions: map[string]int{}},
		Now:         func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func actor() Actor {
	return Actor{WorkspaceID: ws, UserID: userID}
}
