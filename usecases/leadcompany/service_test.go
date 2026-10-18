package leadcompany_usecase

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/media"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const (
	workspaceID = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	userID      = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	companyID   = "11111111-1111-4111-8111-111111111111"
	imageID     = "5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5b"
	pdfID       = "5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5c"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

type permissions map[string]bool

func (p permissions) HasWorkspacePermission(user, ws, resource, action string, _ bool) bool {
	return user != "" && ws == workspaceID && p[resource+":"+action]
}

func managerPermissions() permissions {
	return permissions{
		"lead_companies:read": true, "lead_companies:create": true, "lead_companies:update": true, "lead_companies:delete": true,
	}
}

type memoryCompanies struct {
	byID     map[string]leadcompany.Company
	released []leadcompany.ReleasedLead
	deleted  []recordevent.Event
	listed   []leadcompany.ListQuery
}

func newMemoryCompanies(companies ...leadcompany.Company) *memoryCompanies {
	m := &memoryCompanies{byID: map[string]leadcompany.Company{}}
	for _, c := range companies {
		m.byID[c.ID] = c
	}
	return m
}

func (m *memoryCompanies) CreateCompany(_ context.Context, c leadcompany.Company) error {
	m.byID[c.ID] = c
	return nil
}

func (m *memoryCompanies) UpdateCompany(_ context.Context, c leadcompany.Company) error {
	if _, ok := m.byID[c.ID]; !ok {
		return leadcompany.ErrNotFound
	}
	m.byID[c.ID] = c
	return nil
}

func (m *memoryCompanies) GetCompany(_ context.Context, ws, id string) (leadcompany.Company, error) {
	c, ok := m.byID[id]
	if !ok || c.WorkspaceID != ws {
		return leadcompany.Company{}, leadcompany.ErrNotFound
	}
	return c, nil
}

func (m *memoryCompanies) ListCompanies(_ context.Context, ws string, q leadcompany.ListQuery) (*shared.PaginatedResult[leadcompany.Company], error) {
	m.listed = append(m.listed, q)
	var items []leadcompany.Company
	for _, c := range m.byID {
		if c.WorkspaceID == ws {
			items = append(items, c)
		}
	}
	return shared.NewPaginatedResult(items, q.Pagination(), int64(len(items))), nil
}

func (m *memoryCompanies) DeleteCompany(_ context.Context, ws, id string, release recordevent.Event, _ time.Time) ([]leadcompany.ReleasedLead, error) {
	if c, ok := m.byID[id]; !ok || c.WorkspaceID != ws {
		return nil, leadcompany.ErrNotFound
	}
	delete(m.byID, id)
	m.deleted = append(m.deleted, release)
	return m.released, nil
}

type images map[string]*media.Media

func (i images) GetMedia(ws, id string) (*media.Media, error) {
	m, ok := i[id]
	if !ok || m.WorkspaceID != ws {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

type brokenImages struct{}

func (brokenImages) GetMedia(string, string) (*media.Media, error) {
	return nil, errors.New("database down")
}

type notifier struct{ changes []lead.Change }

func (n *notifier) LeadChanged(c lead.Change) { n.changes = append(n.changes, c) }

type fixture struct {
	companies *memoryCompanies
	notifier  *notifier
	service   *Service
}

func library() images {
	return images{
		imageID: {ID: imageID, WorkspaceID: workspaceID, Type: media.MediaTypeProductImage, URL: "https://cdn/logo.png", PreviewURL: "https://cdn/logo-preview.jpg"},
		pdfID:   {ID: pdfID, WorkspaceID: workspaceID, Type: media.MediaTypeDocumentPdf, URL: "https://cdn/x.pdf"},
	}
}

func newFixture(t *testing.T, perms permissions, companies ...leadcompany.Company) *fixture {
	t.Helper()
	f := &fixture{companies: newMemoryCompanies(companies...), notifier: &notifier{}}
	service, err := New(Deps{
		Companies:   f.companies,
		Permissions: perms,
		Images:      library(),
		Notifier:    f.notifier,
		Now:         func() time.Time { return now },
		NewID:       func() string { return companyID },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.service = service
	return f
}

func someone() conversation.Viewer {
	return conversation.Viewer{UserID: userID, WorkspaceID: workspaceID}
}

func acme() leadcompany.Company {
	return leadcompany.Company{
		ID: companyID, WorkspaceID: workspaceID, Name: "Acme", AcceptsLeads: true, MembersCount: 7,
		ImageMediaID: imageID, Image: &leadcompany.Image{MediaID: imageID, URL: "https://cdn/logo.png"},
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
}

func text(s string) *string { return &s }

func flag(b bool) *bool { return &b }

func TestNewRefusesAMissingDependency(t *testing.T) {
	full := Deps{Companies: newMemoryCompanies(), Permissions: permissions{}, Images: images{}, Notifier: &notifier{}}
	drops := map[string]func(*Deps){
		"companies":   func(d *Deps) { d.Companies = nil },
		"permissions": func(d *Deps) { d.Permissions = nil },
		"images":      func(d *Deps) { d.Images = nil },
		"notifier":    func(d *Deps) { d.Notifier = nil },
	}
	for name, drop := range drops {
		t.Run(name, func(t *testing.T) {
			deps := full
			drop(&deps)
			if _, err := New(deps); err == nil {
				t.Fatalf("a service without %s must not be built", name)
			}
		})
	}
}

func TestEveryOperationNeedsItsOwnCapability(t *testing.T) {
	ctx := context.Background()
	calls := map[string]struct {
		granted permissions
		call    func(*Service) error
	}{
		"list": {permissions{}, func(s *Service) error {
			_, err := s.List(ctx, someone(), leadcompany.ListQuery{})
			return err
		}},
		"get": {permissions{}, func(s *Service) error { _, err := s.Get(ctx, someone(), companyID); return err }},
		"create": {permissions{"lead_companies:read": true}, func(s *Service) error {
			_, err := s.Create(ctx, someone(), leadcompany.Draft{Name: "Acme"})
			return err
		}},
		"update": {permissions{"lead_companies:read": true, "lead_companies:create": true}, func(s *Service) error {
			_, err := s.Update(ctx, someone(), companyID, leadcompany.Patch{Name: text("Globex")})
			return err
		}},
		"delete": {permissions{"lead_companies:read": true, "lead_companies:update": true}, func(s *Service) error {
			return s.Delete(ctx, someone(), companyID)
		}},
	}
	for name, tc := range calls {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, tc.granted, acme())
			if err := tc.call(f.service); !errors.Is(err, leadcompany.ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
			if got := f.companies.byID[companyID]; !reflect.DeepEqual(got, acme()) || len(f.notifier.changes) != 0 {
				t.Fatal("a refused operation must not write or notify")
			}
		})
	}
}

func TestCreateStoresTheCompanyWithItsResolvedImage(t *testing.T) {
	f := newFixture(t, managerPermissions())
	got, err := f.service.Create(context.Background(), someone(), leadcompany.Draft{Name: " Acme ", AcceptsLeads: true, ImageMediaID: imageID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := leadcompany.Image{MediaID: imageID, URL: "https://cdn/logo.png", PreviewURL: "https://cdn/logo-preview.jpg"}
	if got.ID != companyID || got.Name != "Acme" || got.CreatedBy != userID || got.Image == nil || *got.Image != want {
		t.Fatalf("created = %+v", got)
	}
	if stored := f.companies.byID[companyID]; stored.ImageMediaID != imageID || !stored.AcceptsLeads {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestAnImageMustBeAnImageOfTheWorkspace(t *testing.T) {
	for name, id := range map[string]string{"unknown media": "5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5d", "a pdf": pdfID} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, managerPermissions())
			if _, err := f.service.Create(context.Background(), someone(), leadcompany.Draft{Name: "Acme", ImageMediaID: id}); !errors.Is(err, leadcompany.ErrImageInvalid) {
				t.Fatalf("err = %v, want ErrImageInvalid", err)
			}
			if len(f.companies.byID) != 0 {
				t.Fatal("a refused image must not create the company")
			}
		})
	}
}

func TestAMediaLookupFailureIsAFailureNotAnAcceptedImage(t *testing.T) {
	f := newFixture(t, managerPermissions())
	f.service.deps.Images = brokenImages{}
	_, err := f.service.Create(context.Background(), someone(), leadcompany.Draft{Name: "Acme", ImageMediaID: imageID})
	if err == nil || errors.Is(err, leadcompany.ErrImageInvalid) || len(f.companies.byID) != 0 {
		t.Fatalf("err = %v, stored = %d", err, len(f.companies.byID))
	}
}

func TestUpdateKeepsTheImageAndTheMembersItDidNotTouch(t *testing.T) {
	f := newFixture(t, managerPermissions(), acme())
	got, err := f.service.Update(context.Background(), someone(), companyID, leadcompany.Patch{AcceptsLeads: flag(false)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.AcceptsLeads || got.MembersCount != 7 || got.Image == nil || got.ImageMediaID != imageID || !got.UpdatedAt.Equal(now) {
		t.Fatalf("updated = %+v", got)
	}
}

func TestUpdateClearsOrReplacesTheImage(t *testing.T) {
	f := newFixture(t, managerPermissions(), acme())
	cleared, err := f.service.Update(context.Background(), someone(), companyID, leadcompany.Patch{ImageMediaID: text("")})
	if err != nil || cleared.Image != nil || f.companies.byID[companyID].ImageMediaID != "" {
		t.Fatalf("cleared = %+v, err = %v", cleared, err)
	}
	if _, err := f.service.Update(context.Background(), someone(), companyID, leadcompany.Patch{ImageMediaID: text(pdfID)}); !errors.Is(err, leadcompany.ErrImageInvalid) {
		t.Fatalf("err = %v, want ErrImageInvalid", err)
	}
}

func TestUpdateOfAMissingCompanyIsNotFound(t *testing.T) {
	f := newFixture(t, managerPermissions())
	if _, err := f.service.Update(context.Background(), someone(), companyID, leadcompany.Patch{Name: text("Acme")}); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListNormalizesTheQueryBeforeReading(t *testing.T) {
	f := newFixture(t, permissions{"lead_companies:read": true}, acme())
	page, err := f.service.List(context.Background(), someone(), leadcompany.ListQuery{Search: "  ac  "})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if q := f.companies.listed[0]; q.Search != "ac" || q.Page != 1 || q.PageSize != leadcompany.DefaultPageSize {
		t.Fatalf("query = %+v", q)
	}
	if _, err := f.service.List(context.Background(), someone(), leadcompany.ListQuery{Page: -2}); !errors.Is(err, leadcompany.ErrListQueryInvalid) {
		t.Fatalf("err = %v, want ErrListQueryInvalid", err)
	}
}

func TestDeleteReleasesTheMembersAndAnnouncesEachOfThem(t *testing.T) {
	f := newFixture(t, managerPermissions(), acme())
	f.companies.released = []leadcompany.ReleasedLead{{LeadID: "l-2", Version: 4}, {LeadID: "l-1", Version: 9}}
	if err := f.service.Delete(context.Background(), someone(), companyID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if want := lead.CompanyReleaseEvent(userID, companyID); !reflect.DeepEqual(f.companies.deleted, []recordevent.Event{want}) {
		t.Fatalf("release event = %+v, want %+v", f.companies.deleted, want)
	}
	sort.Slice(f.notifier.changes, func(i, j int) bool { return f.notifier.changes[i].LeadID < f.notifier.changes[j].LeadID })
	want := []lead.Change{
		{WorkspaceID: workspaceID, LeadID: "l-1", Version: 9, Fields: []string{lead.FieldCompany}},
		{WorkspaceID: workspaceID, LeadID: "l-2", Version: 4, Fields: []string{lead.FieldCompany}},
	}
	if !reflect.DeepEqual(f.notifier.changes, want) {
		t.Fatalf("changes = %+v, want %+v", f.notifier.changes, want)
	}
}

func TestDeleteOfAMissingCompanyIsNotFound(t *testing.T) {
	f := newFixture(t, managerPermissions())
	if err := f.service.Delete(context.Background(), someone(), companyID); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
