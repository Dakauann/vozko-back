package unofficial_whatsapp_group_usecase

import (
	"context"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func TestDashboardZeroFillsItsSeriesInTheWorkspaceZone(t *testing.T) {
	w := newWorld()
	w.instances.instances[1].Status = uw.StatusDisconnected

	d, err := w.svc.Dashboard(context.Background(), everyone())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.DailyVolume) != uwg.DashboardWindowDays || len(d.ResponseTime) != uwg.DashboardWindowDays || len(d.Growth) != uwg.GrowthWindowDays {
		t.Fatalf("series lengths = %d / %d / %d", len(d.DailyVolume), len(d.ResponseTime), len(d.Growth))
	}
	var messages int64
	for _, p := range d.DailyVolume {
		messages += p.Messages
	}
	if messages != 40 {
		t.Fatalf("daily volume total = %d", messages)
	}
	if d.Connections.Connected != 1 || d.Connections.Total != 2 {
		t.Fatalf("connections = %+v", d.Connections)
	}
	if d.Sentiment.Negative != 5 || d.Groups.Followed != 2 || d.People.Unique != 7 {
		t.Fatalf("dashboard = %+v", d)
	}
}

func TestDashboardCountsOnlyTheCallersNumbers(t *testing.T) {
	w := newWorld()
	d, err := w.svc.Dashboard(context.Background(), salesOnly())
	if err != nil {
		t.Fatal(err)
	}
	if d.Connections.Total != 1 {
		t.Fatalf("connections = %+v, want only the sales number", d.Connections)
	}
}

func TestMembershipTableRefusesAnUnknownPeriod(t *testing.T) {
	w := newWorld()
	if _, err := w.svc.MembershipTable(context.Background(), everyone(), MembershipTableInput{Period: "year"}); err != uwg.ErrPeriodInvalid {
		t.Fatalf("err = %v, want ErrPeriodInvalid", err)
	}
}
