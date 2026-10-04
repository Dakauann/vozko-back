package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

type fakeGate struct {
	held map[string]bool
	err  error
}

func (g *fakeGate) SetNX(key, _ string, _ time.Duration) (bool, error) {
	if g.err != nil {
		return false, g.err
	}
	if g.held == nil {
		g.held = map[string]bool{}
	}
	if g.held[key] {
		return false, nil
	}
	g.held[key] = true
	return true, nil
}

func eventsWorld() (*world, *AccountEventsUseCase, *fakeGate) {
	w := newWorld()
	gate := &fakeGate{}
	uc := NewAccountEventsUseCase(w.sync, w.gateway, gate)
	return w, uc, gate
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

func TestAPreciseEventRefreshesOnlyThatObject(t *testing.T) {
	w, uc, _ := eventsWorld()
	w.gateway.object = &ads.Object{MetaID: "ad-9", Name: "Anúncio", EffectiveStatus: ads.EffectiveStatus("PAUSED")}
	change := ads.AdAccountChange{AccountMetaID: "act_111", Field: "field_changed", Objects: []ads.ObjectRef{{MetaID: "ad-9", Level: ads.LevelAd}}}
	if err := uc.Handle(context.Background(), []ads.AdAccountChange{change}); err != nil {
		t.Fatal(err)
	}
	stored := w.objects.byID["ad-9"]
	if stored == nil || stored.WorkspaceID != "ws-1" || stored.AdAccountID != "acc-1" || stored.Level != ads.LevelAd || stored.EffectiveStatus != "PAUSED" {
		t.Fatalf("stored %+v", stored)
	}
	if countCalls(w.gateway.calls, "get_object") != 1 || countCalls(w.gateway.calls, "list_campaign") != 0 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestEventsWithoutAnObjectRefreshTheAccountOncePerWindow(t *testing.T) {
	w, uc, _ := eventsWorld()
	vague := []ads.AdAccountChange{{AccountMetaID: "act_111", Field: "with_issues_ad_objects"}, {AccountMetaID: "111", Field: "in_process_ad_objects", Unresolved: true}}
	for range 3 {
		if err := uc.Handle(context.Background(), vague); err != nil {
			t.Fatal(err)
		}
	}
	if countCalls(w.gateway.calls, "list_campaign") != 1 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestFieldsVozkoDoesNotUseCostNoMetaCall(t *testing.T) {
	w, uc, _ := eventsWorld()
	unused := []ads.AdAccountChange{
		{AccountMetaID: "act_111", Field: "creative_fatigue"},
		{AccountMetaID: "act_111", Field: "ad_recommendations"},
		{AccountMetaID: "act_111", Field: "product_set_issue"},
		{AccountMetaID: "act_111", Field: "ads_async_creation_request"},
	}
	if err := uc.Handle(context.Background(), unused); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestEventsOfAnAccountNoWorkspaceConnectedAreIgnored(t *testing.T) {
	w, uc, _ := eventsWorld()
	if err := uc.Handle(context.Background(), []ads.AdAccountChange{{AccountMetaID: "999", Field: "with_issues_ad_objects"}}); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestAnObjectMetaNoLongerReturnsRefreshesTheAccountInstead(t *testing.T) {
	w, uc, _ := eventsWorld()
	w.gateway.failOn, w.gateway.failWith = "get_object", &ads.RemoteError{Kind: ads.FailureRejected, Code: 100, Message: "Unsupported get request"}
	change := ads.AdAccountChange{AccountMetaID: "act_111", Field: "in_process_ad_objects", Objects: []ads.ObjectRef{{MetaID: "ad-9", Level: ads.LevelAd}}}
	if err := uc.Handle(context.Background(), []ads.AdAccountChange{change}); err != nil {
		t.Fatal(err)
	}
	if countCalls(w.gateway.calls, "list_campaign") != 1 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestAMetaOutageIsRetriedNotSwallowed(t *testing.T) {
	w, uc, _ := eventsWorld()
	w.gateway.failOn, w.gateway.failWith = "get_object", &ads.RemoteError{Kind: ads.FailureRetryable, Code: 2, Message: "Service temporarily unavailable"}
	change := ads.AdAccountChange{AccountMetaID: "act_111", Field: "field_changed", Objects: []ads.ObjectRef{{MetaID: "ad-9", Level: ads.LevelAd}}}
	if err := uc.Handle(context.Background(), []ads.AdAccountChange{change}); err == nil {
		t.Fatal("an outage was treated as done")
	}
}

func TestALockOutageIsRetriedInsteadOfSkippingTheRefresh(t *testing.T) {
	w, uc, gate := eventsWorld()
	gate.err = errors.New("redis down")
	if err := uc.Handle(context.Background(), []ads.AdAccountChange{{AccountMetaID: "act_111", Field: "with_issues_ad_objects"}}); err == nil {
		t.Fatal("a lock outage was treated as done")
	}
	if countCalls(w.gateway.calls, "list_campaign") != 0 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}
