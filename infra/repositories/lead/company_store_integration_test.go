package lead

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type companyWorld struct {
	repo      *repository
	db        *gorm.DB
	workspace string
	actor     string
}

func newCompanyWorld(t *testing.T) companyWorld {
	t.Helper()
	db := repotest.IsolatedDB(t, "lead_companies")
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadCompany{}, &schema.Media{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
	return companyWorld{repo: &repository{db: db, agg: newAggregateCache(nil)}, db: db, workspace: uuid.NewString(), actor: uuid.NewString()}
}

func (w companyWorld) company(t *testing.T, name string, open bool, imageID string) leadcompany.Company {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := leadcompany.Company{ID: uuid.NewString(), WorkspaceID: w.workspace, Name: name, AcceptsLeads: open, ImageMediaID: imageID, CreatedBy: w.actor, CreatedAt: now, UpdatedAt: now}
	if err := w.repo.CreateCompany(context.Background(), c); err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func (w companyWorld) lead(t *testing.T, name string) *lead.Lead {
	t.Helper()
	l := &lead.Lead{ID: uuid.NewString(), WorkspaceID: w.workspace, Name: name}
	if err := w.repo.Insert(context.Background(), l, nil); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return l
}

func (w companyWorld) join(t *testing.T, l *lead.Lead, companyID string) error {
	t.Helper()
	current, err := w.repo.Load(context.Background(), w.workspace, l.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	next := *current
	next.CompanyID = companyID
	event := lead.Changes(lead.EventCompanyChanged, w.actor, current, &next, nil)
	return w.repo.SaveMembership(context.Background(), &next, current.Version, []recordevent.Event{event})
}

func TestCompaniesAgainstPostgres(t *testing.T) {
	w := newCompanyWorld(t)
	ctx := context.Background()
	imageID := uuid.NewString()
	if err := w.db.Exec(`INSERT INTO medias (id, workspace_id, url, preview_url, type, created_at) VALUES (?, ?, 'https://cdn/logo.png', 'https://cdn/logo-preview.jpg', 'image', now())`, imageID, w.workspace).Error; err != nil {
		t.Fatal(err)
	}
	acme := w.company(t, "Ação Indústria", true, imageID)
	closed := w.company(t, "Fechada", false, "")
	ana, bia := w.lead(t, "Ana"), w.lead(t, "Bia")

	if err := w.join(t, ana, acme.ID); err != nil {
		t.Fatalf("join an open company: %v", err)
	}
	if err := w.join(t, bia, acme.ID); err != nil {
		t.Fatalf("join an open company: %v", err)
	}
	if err := w.join(t, ana, closed.ID); !errors.Is(err, leadcompany.ErrClosed) {
		t.Fatalf("join a closed company = %v, want ErrClosed", err)
	}
	if err := w.join(t, ana, uuid.NewString()); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("join a missing company = %v, want ErrNotFound", err)
	}

	stored, err := w.repo.Load(ctx, w.workspace, ana.ID)
	if err != nil || stored.CompanyID != acme.ID || stored.Version != 2 {
		t.Fatalf("stored = %+v, err = %v", stored, err)
	}
	stored.Name = "Ana Souza"
	if err := w.repo.Save(ctx, stored, stored.Version, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if again, _ := w.repo.Load(ctx, w.workspace, ana.ID); again.CompanyID != acme.ID {
		t.Fatalf("a general save must keep the company, got %q", again.CompanyID)
	}

	got, err := w.repo.GetCompany(ctx, w.workspace, acme.ID)
	if err != nil || got.MembersCount != 2 || got.Image == nil || got.Image.PreviewURL != "https://cdn/logo-preview.jpg" {
		t.Fatalf("company = %+v, err = %v", got, err)
	}
	page, err := w.repo.ListCompanies(ctx, w.workspace, leadcompany.ListQuery{Search: "acao", Page: 1, PageSize: 10})
	if err != nil || page.TotalItems != 1 || len(page.Items) != 1 || page.Items[0].ID != acme.ID {
		t.Fatalf("search without accents = %+v, err = %v", page, err)
	}
	if _, err := w.repo.GetCompany(ctx, uuid.NewString(), acme.ID); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("another workspace reads the company: %v", err)
	}

	released, err := w.repo.DeleteCompany(ctx, w.workspace, acme.ID, lead.CompanyReleaseEvent(w.actor, acme.ID), time.Now().UTC())
	if err != nil || len(released) != 2 {
		t.Fatalf("released = %+v, err = %v", released, err)
	}
	if after, _ := w.repo.Load(ctx, w.workspace, bia.ID); after.CompanyID != "" || after.Version != 3 {
		t.Fatalf("bia after the delete = %+v", after)
	}
	var events int64
	if err := w.db.Raw(`SELECT COUNT(*) FROM lead_events WHERE lead_id = ? AND kind = ?`, bia.ID, string(lead.EventCompanyChanged)).Scan(&events).Error; err != nil || events != 2 {
		t.Fatalf("bia has %d company events, want the join and the release: %v", events, err)
	}
	if _, err := w.repo.GetCompany(ctx, w.workspace, acme.ID); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("a deleted company is still read: %v", err)
	}
	if err := w.join(t, bia, acme.ID); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("joining a deleted company = %v, want ErrNotFound", err)
	}
}
