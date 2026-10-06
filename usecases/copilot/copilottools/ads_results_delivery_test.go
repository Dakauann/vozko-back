package copilottools

import (
	"testing"
	"time"

	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

func deliveryOf(t *testing.T, start, now time.Time) string {
	t.Helper()
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	report := &adsuc.Report{
		Account: &advertising.AdAccount{Currency: "BRL"},
		Range:   advertising.DateRange{Since: day.AddDate(0, 0, -29), Until: day},
		Rows: []adsuc.ReportRow{{Object: &advertising.Object{
			MetaID: "120250", Name: "Cadastros", Level: advertising.LevelCampaign, Status: advertising.StatusActive,
			EffectiveStatus: advertising.EffectiveActive, StartTime: &start,
		}}},
	}
	items := reportData(report, now)["rows"].([]map[string]interface{})
	return items[0]["delivery"].(string)
}

func TestDeliveryIsJudgedAtTheCurrentTimeNotTheEndOfTheReport(t *testing.T) {
	startedToday := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	if got := deliveryOf(t, startedToday, startedToday.Add(4*time.Hour)); got != string(advertising.DeliveryActive) {
		t.Fatalf("a campaign that started today is delivering, got %s", got)
	}
	if got := deliveryOf(t, startedToday.AddDate(0, 0, 2), startedToday); got != string(advertising.DeliveryScheduled) {
		t.Fatalf("a future start is scheduled, got %s", got)
	}
}
