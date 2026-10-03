package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

type memoryReports struct {
	byID   map[string]*ads.SavedReport
	opened map[string]time.Time
}

func (m *memoryReports) Create(_ context.Context, r *ads.SavedReport) error {
	r.ID = "r-1"
	clone := *r
	m.byID[r.ID] = &clone
	return nil
}

func (m *memoryReports) Find(_ context.Context, ws, id string) (*ads.SavedReport, error) {
	r, ok := m.byID[id]
	if !ok || r.WorkspaceID != ws {
		return nil, ads.ErrReportNotFound
	}
	clone := *r
	return &clone, nil
}

func (m *memoryReports) Save(_ context.Context, r *ads.SavedReport) error {
	clone := *r
	m.byID[r.ID] = &clone
	return nil
}

func (m *memoryReports) MarkOpened(_ context.Context, _, id string, at time.Time) error {
	m.opened[id] = at
	return nil
}

func (m *memoryReports) Delete(_ context.Context, _, id string) error {
	delete(m.byID, id)
	return nil
}

func (m *memoryReports) ListByWorkspace(context.Context, string) ([]*ads.SavedReport, error) {
	return nil, nil
}

func newReportsFixture() (*SavedReportsUseCase, *memoryReports) {
	accounts := &fakeAccounts{byID: map[string]*ads.AdAccount{
		"acc-1": {ID: "acc-1", WorkspaceID: "ws"},
		"acc-x": {ID: "acc-x", WorkspaceID: "other"},
	}, connections: map[string]ads.Connection{}}
	reports := &memoryReports{byID: map[string]*ads.SavedReport{}, opened: map[string]time.Time{}}
	uc := NewSavedReportsUseCase(reports, accounts)
	uc.now = func() time.Time { return testNow }
	return uc, reports
}

func monthly(account string) SavedReportInput {
	return SavedReportInput{Name: "Mensal", AdAccountID: account, Definition: ads.ReportTemplates()[0].Definition}
}

func TestSavedReportsBelongToAnAccountOfTheWorkspace(t *testing.T) {
	uc, _ := newReportsFixture()
	if _, err := uc.Create(context.Background(), "ws", "u-1", monthly("acc-x")); !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestOpeningAReportStampsIt(t *testing.T) {
	uc, reports := newReportsFixture()
	ctx := context.Background()
	created, err := uc.Create(ctx, "ws", "u-1", monthly("acc-1"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := uc.Open(ctx, "ws", created.ID)
	if err != nil || !opened.LastOpenedAt.Equal(testNow) || !reports.opened[created.ID].Equal(testNow) {
		t.Fatalf("got %+v %v", opened, err)
	}
}

func TestUpdateValidatesTheDefinition(t *testing.T) {
	uc, _ := newReportsFixture()
	ctx := context.Background()
	created, _ := uc.Create(ctx, "ws", "u-1", monthly("acc-1"))
	broken := monthly("acc-1")
	broken.Definition.Metrics = nil
	if _, err := uc.Update(ctx, "ws", created.ID, broken); err == nil {
		t.Fatal("a definition without metrics must be refused")
	}
	renamed := monthly("acc-1")
	renamed.Name = "Trimestral"
	updated, err := uc.Update(ctx, "ws", created.ID, renamed)
	if err != nil || updated.Name != "Trimestral" {
		t.Fatalf("got %+v %v", updated, err)
	}
}
