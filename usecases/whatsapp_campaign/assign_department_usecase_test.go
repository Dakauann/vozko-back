package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
)

type wcAssignRepoMock struct {
	campaigns map[string]*wc.Campaign
	updateErr error
}

func newWCAssignRepoMock() *wcAssignRepoMock {
	return &wcAssignRepoMock{campaigns: make(map[string]*wc.Campaign)}
}

func (m *wcAssignRepoMock) Create(*wc.Campaign) error { return nil }

func (m *wcAssignRepoMock) Update(campaignID string, value *wc.Campaign) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	copyValue := *value
	m.campaigns[campaignID] = &copyValue
	return nil
}

func (m *wcAssignRepoMock) Delete(string) error { return nil }

func (m *wcAssignRepoMock) FindByID(campaignID string) (*wc.Campaign, error) {
	value, ok := m.campaigns[campaignID]
	if !ok {
		return nil, wc.ErrCampaignNotFound
	}
	copyValue := *value
	return &copyValue, nil
}

func (m *wcAssignRepoMock) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	return nil, wc.ErrCampaignNotFound
}

func (m *wcAssignRepoMock) List(wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	return &shared.PaginatedResult[*wc.Campaign]{Items: []*wc.Campaign{}}, nil
}

func (m *wcAssignRepoMock) ListByStatus(wc.Status) ([]*wc.Campaign, error) {
	return nil, nil
}

func (m *wcAssignRepoMock) ListScheduledToStart(time.Time, int) ([]*wc.Campaign, error) {
	return nil, nil
}

func (m *wcAssignRepoMock) UpdateStatus(string, wc.Status, ...wc.Status) (bool, error) {
	return false, nil
}

func (m *wcAssignRepoMock) UpdateResetCode(string, string) error { return nil }

func (m *wcAssignRepoMock) UpdateClearCode(string, string) error { return nil }

type wcDepartmentResolverStub struct {
	departmentID string
	err          error
	workspaceID  string
}

func (s *wcDepartmentResolverStub) Resolve(_ context.Context, workspaceID string) (string, error) {
	s.workspaceID = workspaceID
	if s.err != nil {
		return "", s.err
	}
	return s.departmentID, nil
}

func TestAssignDepartmentUseCase_AssignsResolvedDepartment(t *testing.T) {
	repo := newWCAssignRepoMock()
	repo.campaigns["campaign-1"] = &wc.Campaign{ID: "campaign-1", WorkspaceID: "ws-1"}
	resolver := &wcDepartmentResolverStub{departmentID: "dept-1"}

	uc := NewAssignDepartmentUseCase(repo, resolver)
	updated, err := uc.Execute(context.Background(), "campaign-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolver.workspaceID != "ws-1" {
		t.Fatalf("expected resolver workspace ws-1, got %s", resolver.workspaceID)
	}
	if updated.DepartmentID != "dept-1" {
		t.Fatalf("expected assigned department dept-1, got %s", updated.DepartmentID)
	}
	if repo.campaigns["campaign-1"].DepartmentID != "dept-1" {
		t.Fatalf("expected repository to persist dept-1, got %s", repo.campaigns["campaign-1"].DepartmentID)
	}
}

func TestAssignDepartmentUseCase_ReturnsResolverError(t *testing.T) {
	repo := newWCAssignRepoMock()
	repo.campaigns["campaign-1"] = &wc.Campaign{ID: "campaign-1", WorkspaceID: "ws-1"}
	resolver := &wcDepartmentResolverStub{err: errors.New("resolver failed")}

	uc := NewAssignDepartmentUseCase(repo, resolver)
	_, err := uc.Execute(context.Background(), "campaign-1")
	if err == nil || err.Error() != "resolver failed" {
		t.Fatalf("expected resolver error, got %v", err)
	}
	if repo.campaigns["campaign-1"].DepartmentID != "" {
		t.Fatalf("expected repository department to remain empty, got %s", repo.campaigns["campaign-1"].DepartmentID)
	}
}
