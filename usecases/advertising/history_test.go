package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

func TestHistoryReadsTheObjectInTheAccountsDaysAndLanguage(t *testing.T) {
	w := newWorld()
	w.objects.byID["set-1"] = &ads.Object{MetaID: "set-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", Level: ads.LevelAdSet}
	older := ads.AdActivity{EventType: "create_ad_set", At: time.Date(2026, 10, 3, 23, 27, 0, 0, time.UTC)}
	newer := ads.AdActivity{EventType: "update_ad_set_run_status", At: time.Date(2026, 10, 3, 23, 33, 0, 0, time.UTC)}
	w.gateway.activities = []ads.AdActivity{older, newer}
	uc := NewHistoryUseCase(w.sync, w.gateway)
	day, _ := ads.ParseDay("2026-10-03")
	got, err := uc.List(context.Background(), "ws-1", "set-1", ads.DateRange{Since: day, Until: day}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].EventType != "update_ad_set_run_status" {
		t.Fatalf("got %+v", got)
	}
	q := w.gateway.activityQuery
	if q.ObjectID != "set-1" || q.Locale != "en_US" || q.Since.UTC().Format(time.RFC3339) != "2026-10-03T03:00:00Z" || !q.Until.Equal(q.Since.Add(24*time.Hour)) {
		t.Fatalf("query %+v", q)
	}
}

func TestHistoryOfAnotherWorkspacesObjectIsRefusedBeforeAskingMeta(t *testing.T) {
	w := newWorld()
	w.objects.byID["set-1"] = &ads.Object{MetaID: "set-1", WorkspaceID: "ws-2", AdAccountID: "acc-1", Level: ads.LevelAdSet}
	_, err := NewHistoryUseCase(w.sync, w.gateway).List(context.Background(), "ws-1", "set-1", ads.DateRange{}, "pt")
	if !errors.Is(err, ads.ErrObjectNotFound) || len(w.gateway.calls) != 0 {
		t.Fatalf("err %v calls %v", err, w.gateway.calls)
	}
}

func TestHistoryWithoutAPeriodCoversTheLastThirtyDays(t *testing.T) {
	w := newWorld()
	w.objects.byID["set-1"] = &ads.Object{MetaID: "set-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", Level: ads.LevelAdSet}
	if _, err := NewHistoryUseCase(w.sync, w.gateway).List(context.Background(), "ws-1", "set-1", ads.DateRange{}, "pt"); err != nil {
		t.Fatal(err)
	}
	q := w.gateway.activityQuery
	if days := q.Until.Sub(q.Since).Hours() / 24; days != DefaultReportDays || q.Until.Before(testNow) {
		t.Fatalf("query %+v", q)
	}
}
