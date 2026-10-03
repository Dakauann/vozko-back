package whatsapp_campaign

import (
	"encoding/json"
	"strings"
	"testing"
)

// The front reads campaign flags by camelCase key. A PascalCase tag still
// accepts writes, because Go matches field names case-insensitively on
// unmarshal, but it serialises under the wrong key and the toggle renders off
// while the database says on.
func TestCampaignJSON_FlagsAreCamelCase(t *testing.T) {
	raw, err := json.Marshal(&Campaign{EnableAutoStaging: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, wrong := payload["EnableAutoStaging"]; wrong {
		t.Fatal("EnableAutoStaging serialised in PascalCase; the front reads enableAutoStaging and would show the toggle off")
	}
	got, ok := payload["enableAutoStaging"]
	if !ok {
		t.Fatalf("enableAutoStaging missing from the payload: %s", raw)
	}
	if got != true {
		t.Fatalf("enableAutoStaging = %v, want true", got)
	}
}

func TestCampaignJSON_NoFlagUsesPascalCase(t *testing.T) {
	raw, err := json.Marshal(&Campaign{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for key := range payload {
		if key == "" {
			continue
		}
		if strings.ToUpper(key[:1]) == key[:1] {
			t.Errorf("key %q is not camelCase; the front reads camelCase and would silently miss it", key)
		}
	}
}
