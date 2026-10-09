package ws

import (
	"encoding/json"
	"testing"
)

func TestTheSubscribedPayloadAlwaysCarriesTheLeadIdentityFields(t *testing.T) {
	raw, err := json.Marshal(SubscribedPayload{EntryID: "e-1", EntryType: "whatsapp"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"lead_name", "lead_number", "lead_version", "lead_picture"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("%s missing from %s; a cleared value would leave the stale one on screen", key, raw)
		}
	}
}
