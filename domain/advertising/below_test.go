package advertising

import "testing"

func TestBelowQueriesFollowTheLevel(t *testing.T) {
	campaign := &Object{MetaID: "c1", WorkspaceID: "ws", AdAccountID: "acc", Level: LevelCampaign}
	q := campaign.BelowQueries()
	if len(q) != 2 || q[0].Level != LevelAdSet || q[1].Level != LevelAd || q[0].CampaignIDs[0] != "c1" || q[1].CampaignIDs[0] != "c1" || q[0].WorkspaceID != "ws" || q[1].AdAccountID != "acc" {
		t.Fatalf("campaign queries %+v", q)
	}
	adSet := &Object{MetaID: "s1", WorkspaceID: "ws", AdAccountID: "acc", Level: LevelAdSet}
	if q := adSet.BelowQueries(); len(q) != 1 || q[0].Level != LevelAd || q[0].AdSetIDs[0] != "s1" {
		t.Fatalf("ad set queries %+v", q)
	}
	if q := (&Object{Level: LevelAd}).BelowQueries(); len(q) != 0 {
		t.Fatal("an ad has nothing below it")
	}
}

func TestOffBelowListsPausedAdSetsBeforeTheirAdsAndSkipsTheParent(t *testing.T) {
	objects := []*Object{
		{MetaID: "a1", Level: LevelAd, Status: StatusPaused},
		{MetaID: "c1", Level: LevelCampaign, Status: StatusPaused},
		{MetaID: "s1", Level: LevelAdSet, Status: StatusPaused},
		{MetaID: "a2", Level: LevelAd, Status: StatusActive},
		{MetaID: "a3", Level: LevelAd, Status: StatusArchived},
	}
	off := OffBelow(objects, "c1")
	if len(off) != 2 || off[0].MetaID != "s1" || off[1].MetaID != "a1" {
		t.Fatalf("off %+v", off)
	}
}
