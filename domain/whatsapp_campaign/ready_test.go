package whatsapp_campaign

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/campaign"
)

func readyOf(t *testing.T, c *Campaign) (bool, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	ready, ok := payload["readyToStart"].(bool)
	if !ok {
		t.Fatalf("readyToStart missing: %s", raw)
	}
	return ready, payload
}

func TestANeverStartedCampaignIsShownReady(t *testing.T) {
	c := &Campaign{ID: "c1", Name: "Natal", Status: CampaignStatusStopped, Metrics: &campaign.Metrics{TotalNumbers: 5, Pending: 5}}
	ready, payload := readyOf(t, c)
	if !ready {
		t.Fatal("a created, never started campaign is ready")
	}
	if payload["id"] != "c1" || payload["name"] != "Natal" || payload["status"] != "STOPPED" {
		t.Fatalf("the campaign fields changed: %v", payload)
	}
}

func TestAStoppedAfterSendingOrScheduledCampaignIsNotReady(t *testing.T) {
	sent := &Campaign{Status: CampaignStatusStopped, Metrics: &campaign.Metrics{TotalNumbers: 5, Pending: 3, Sent: 2}}
	if ready, _ := readyOf(t, sent); ready {
		t.Fatal("a campaign that already sent is stopped, not ready")
	}
	scheduled := &Campaign{Status: CampaignStatusStopped, ScheduledStart: time.Now().Add(time.Hour), Metrics: &campaign.Metrics{TotalNumbers: 5, Pending: 5}}
	if ready, _ := readyOf(t, scheduled); ready {
		t.Fatal("a scheduled campaign is shown as scheduled, not ready")
	}
	if ready, _ := readyOf(t, &Campaign{Status: CampaignStatusStopped}); ready {
		t.Fatal("a campaign without metrics is not ready")
	}
}
