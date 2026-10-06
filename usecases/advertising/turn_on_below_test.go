package advertising

import (
	"context"
	"testing"

	ads "vozko/domain/advertising"
)

func pausedBelowCampaign(w *world) {
	seedStructure(w)
	for _, id := range []string{"s-1", "a-1"} {
		w.objects.byID[id].Status = ads.StatusPaused
	}
	w.objects.byID["a-2"].Status = ads.StatusActive
}

func TestOffBelowListsWhatStaysOffUnderACampaign(t *testing.T) {
	w := newWorld()
	pausedBelowCampaign(w)
	off, err := w.manager().OffBelow(context.Background(), "ws-1", "c-1")
	if err != nil || len(off) != 2 || off[0].MetaID != "s-1" || off[1].MetaID != "a-1" {
		t.Fatalf("off %+v err %v", off, err)
	}
	if _, err := w.manager().OffBelow(context.Background(), "ws-2", "c-1"); err == nil {
		t.Fatal("another workspace read the structure")
	}
}

func TestTurningOnACampaignAloneReportsWhatStaysOff(t *testing.T) {
	w := newWorld()
	pausedBelowCampaign(w)
	_, stillOff, err := w.manager().TurnOn(context.Background(), "ws-1", "c-1", false)
	if err != nil || len(stillOff) != 2 {
		t.Fatalf("still off %+v err %v", stillOff, err)
	}
	if _, touched := w.gateway.statuses["s-1"]; touched {
		t.Fatal("turned on an ad set without being asked")
	}
}

func TestTurningOnACampaignWithEverythingBelowTurnsTheAdSetBeforeItsAds(t *testing.T) {
	w := newWorld()
	pausedBelowCampaign(w)
	w.gateway.objects[ads.LevelAdSet] = []*ads.Object{{MetaID: "s-1", CampaignMetaID: "c-1", Status: ads.StatusActive}}
	w.gateway.objects[ads.LevelAd] = []*ads.Object{{MetaID: "a-1", CampaignMetaID: "c-1", AdSetMetaID: "s-1", Status: ads.StatusActive}}
	_, stillOff, err := w.manager().TurnOn(context.Background(), "ws-1", "c-1", true)
	if err != nil || len(stillOff) != 0 {
		t.Fatalf("still off %+v err %v", stillOff, err)
	}
	order := []string{}
	for _, call := range w.gateway.calls {
		if call == "status:c-1" || call == "status:s-1" || call == "status:a-1" {
			order = append(order, call)
		}
	}
	if len(order) != 3 || order[0] != "status:c-1" || order[1] != "status:s-1" || order[2] != "status:a-1" {
		t.Fatalf("order %v", order)
	}
}
