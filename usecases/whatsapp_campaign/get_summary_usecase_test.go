package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type waSummaryAggStub struct {
	counts    *wce.StatusCounts
	byCat     map[string]int64
	err       error
	catErr    error
	gotFilter wce.WorkspaceSummaryFilter
}

func (s *waSummaryAggStub) CountByStatusForWorkspace(f wce.WorkspaceSummaryFilter) (*wce.StatusCounts, error) {
	s.gotFilter = f
	return s.counts, s.err
}

func (s *waSummaryAggStub) CountDispatchesByCategoryForWorkspace(f wce.WorkspaceSummaryFilter) (map[string]int64, error) {
	if s.catErr != nil {
		return nil, s.catErr
	}
	return s.byCat, nil
}

type waChargeAggStub struct {
	stats *balance.WhatsAppChargeStats
	err   error
	got   balance.WhatsAppChargeFilter
}

func (s *waChargeAggStub) AggregateWhatsAppTemplateCharges(f balance.WhatsAppChargeFilter) (*balance.WhatsAppChargeStats, error) {
	s.got = f
	return s.stats, s.err
}

func TestGetSummaryBuildsBilledMetricsAndPassesFilter(t *testing.T) {
	agg := &waSummaryAggStub{
		counts: &wce.StatusCounts{
			Total: 50, Pending: 5, Sent: 30, Delivered: 5, Read: 2, Failed: 4, NotEligiblePossibleSpam: 4,
		},
		byCat: map[string]int64{
			"MARKETING":      20,
			"UTILITY":        15,
			"AUTHENTICATION": 2,
		},
	}
	uc := NewGetSummaryUseCase(agg, nil)

	filter := wce.WorkspaceSummaryFilter{WorkspaceID: "ws-1", Type: "standard"}
	m, err := uc.Execute(filter)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Without charge aggregator: billed from entry status = 37.
	if got, want := m.Dispatches, int64(37); got != want {
		t.Errorf("Dispatches = %d, want %d", got, want)
	}
	if m.ByCategory == nil {
		t.Fatal("expected ByCategory volume split")
	}
	if got, want := m.ByCategory.Marketing, int64(20); got != want {
		t.Errorf("Marketing = %d, want %d", got, want)
	}
	if agg.gotFilter.WorkspaceID != "ws-1" || agg.gotFilter.Type != "standard" {
		t.Errorf("filter not passed through: %+v", agg.gotFilter)
	}
}

func TestGetSummaryPrefersLedgerCharges(t *testing.T) {
	agg := &waSummaryAggStub{
		counts: &wce.StatusCounts{
			Total: 10, Pending: 8, Sent: 1, Delivered: 0, Read: 1, Failed: 0,
		},
	}
	charges := &waChargeAggStub{
		stats: &balance.WhatsAppChargeStats{
			NetDispatches:  99,
			Marketing:      70,
			Utility:        25,
			Authentication: 4,
		},
	}
	uc := NewGetSummaryUseCase(agg, charges)

	m, err := uc.Execute(wce.WorkspaceSummaryFilter{WorkspaceID: "ws-1", Type: "standard"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	// Ledger overrides entry-status dispatches (entry would have been 2).
	if got, want := m.Dispatches, int64(99); got != want {
		t.Errorf("Dispatches from ledger = %d, want %d", got, want)
	}
	if m.ByCategory == nil || m.ByCategory.Marketing != 70 || m.ByCategory.Utility != 25 {
		t.Errorf("ByCategory from ledger unexpected: %+v", m.ByCategory)
	}
	// Delivery funnel still entry-based.
	if got, want := m.Read, int64(1); got != want {
		t.Errorf("Read should stay entry-based, got %d", got)
	}
	if charges.got.WorkspaceID != "ws-1" || charges.got.CampaignType != "standard" {
		t.Errorf("charge filter not passed: %+v", charges.got)
	}
}

func TestGetSummaryPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewGetSummaryUseCase(&waSummaryAggStub{err: sentinel}, nil)
	if _, err := uc.Execute(wce.WorkspaceSummaryFilter{}); !errors.Is(err, sentinel) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
}

func TestGetSummaryCategoryErrorDoesNotFail(t *testing.T) {
	agg := &waSummaryAggStub{
		counts: &wce.StatusCounts{Total: 10, Sent: 10},
		catErr: errors.New("category join failed"),
	}
	uc := NewGetSummaryUseCase(agg, nil)
	m, err := uc.Execute(wce.WorkspaceSummaryFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("category error must not fail summary: %v", err)
	}
	if m.Dispatches != 10 {
		t.Errorf("Dispatches = %d, want 10", m.Dispatches)
	}
	if m.ByCategory != nil {
		t.Errorf("ByCategory should be nil when category query fails")
	}
}

func TestGetSummaryLedgerErrorFallsBackToEntries(t *testing.T) {
	agg := &waSummaryAggStub{
		counts: &wce.StatusCounts{Total: 5, Sent: 5},
		byCat:  map[string]int64{"UTILITY": 5},
	}
	charges := &waChargeAggStub{err: errors.New("ledger down")}
	uc := NewGetSummaryUseCase(agg, charges)
	m, err := uc.Execute(wce.WorkspaceSummaryFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ledger error must not fail summary: %v", err)
	}
	if m.Dispatches != 5 {
		t.Errorf("fallback Dispatches = %d, want 5", m.Dispatches)
	}
	if m.ByCategory == nil || m.ByCategory.Utility != 5 {
		t.Errorf("fallback ByCategory unexpected: %+v", m.ByCategory)
	}
}
