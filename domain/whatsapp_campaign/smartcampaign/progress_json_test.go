package smartcampaign

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/campaign"
)

func TestASendingWaveCarriesNoNextWaveTime(t *testing.T) {
	sending := Describe(campaign.StatusRunning, State{}, &Wave{Number: 1, Size: 500, State: Enqueued})

	raw, err := json.Marshal(sending)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "until") || strings.Contains(string(raw), "0001-01-01") {
		t.Fatalf("an empty time must not reach the screen: %s", raw)
	}
}
