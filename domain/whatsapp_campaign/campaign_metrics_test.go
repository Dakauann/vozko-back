package whatsapp_campaign

import (
	"testing"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestNewCampaignMetricsNil(t *testing.T) {
	m := NewCampaignMetrics(nil)
	if m == nil {
		t.Fatal("NewCampaignMetrics(nil) returned nil")
	}
	if m.TotalNumbers != 0 || m.Dispatches != 0 || m.Processed != 0 ||
		m.CompletionRate != 0 || m.SuccessRate != 0 {
		t.Errorf("expected fully zeroed metrics for nil counts, got %+v", m)
	}
}

func TestNewCampaignMetricsDispatchesAndRates(t *testing.T) {
	counts := &wce.StatusCounts{
		Total:                   100,
		Pending:                 10,
		Sent:                    30,
		Delivered:               25,
		Read:                    15,
		Failed:                  12,
		NotEligiblePossibleSpam: 8,
	}

	m := NewCampaignMetrics(counts)

	if got, want := m.Dispatches, int64(70); got != want { // SENT + DELIVERED + READ
		t.Errorf("Dispatches = %d, want %d", got, want)
	}
	if got, want := m.Processed, int64(90); got != want {
		t.Errorf("Processed = %d, want %d", got, want)
	}
	if got, want := m.NotEligiblePossibleSpam, int64(8); got != want {
		t.Errorf("NotEligiblePossibleSpam = %d, want %d", got, want)
	}
	if got, want := m.CompletionRate, 90.0; got != want { // 90 / 100 * 100
		t.Errorf("CompletionRate = %v, want %v", got, want)
	}
	// SuccessRate = dispatches / processed * 100 = 70 / 90 * 100 = 77.78
	if m.SuccessRate < 77.7 || m.SuccessRate > 77.8 {
		t.Errorf("SuccessRate = %v, want ~77.78", m.SuccessRate)
	}
}

func TestNewCampaignMetricsRatesZeroWhenEmpty(t *testing.T) {
	m := NewCampaignMetrics(&wce.StatusCounts{Total: 0})
	if m.CompletionRate != 0 || m.SuccessRate != 0 {
		t.Errorf("rates should be 0 when there are no entries, got completion=%v success=%v",
			m.CompletionRate, m.SuccessRate)
	}
}
