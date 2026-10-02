package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

func TestDisconnectOnlyTouchesTheWorkspacesOwnAccount(t *testing.T) {
	w := newWorld()
	uc := NewAccountsUseCase(w.accounts)
	if err := uc.Disconnect(context.Background(), "ws-2", "acc-1"); !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("foreign disconnect err %v", err)
	}
	if err := uc.Disconnect(context.Background(), "ws-1", "acc-1"); err != nil || w.accounts.connections["acc-1"] != ads.ConnectionDisconnected {
		t.Fatalf("err %v connection %s", err, w.accounts.connections["acc-1"])
	}
}

func TestReportWithoutAPeriodUsesTheLast30DaysInTheAccountTimezone(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	uc := reporter(w)
	uc.now = func() time.Time { return testNow }
	report, err := uc.Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelCampaign})
	if err != nil {
		t.Fatal(err)
	}
	if report.Range.Days() != 30 || report.Range.Until.Format(ads.DayLayout) != "2026-10-01" {
		t.Fatalf("range %+v", report.Range)
	}
}
