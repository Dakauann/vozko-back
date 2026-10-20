package whatsapp_campaign

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAnUnscheduledCampaignSendsNoScheduledStart(t *testing.T) {
	raw, err := json.Marshal(Campaign{ID: "camp-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "scheduledStart") {
		t.Fatalf("an unscheduled campaign serialized %s", raw)
	}

	start := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	raw, err = json.Marshal(Campaign{ID: "camp-1", ScheduledStart: start})
	if err != nil {
		t.Fatal(err)
	}
	var back Campaign
	if err := json.Unmarshal(raw, &back); err != nil || !back.ScheduledStart.Equal(start) {
		t.Fatalf("round trip = %v, %v from %s", back.ScheduledStart, err, raw)
	}
}
