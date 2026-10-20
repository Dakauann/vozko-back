package ai_attendance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAToolCallNamesItsToolCallIDForTheTimelineAndOmitsItWhenOld(t *testing.T) {
	withID, _ := json.Marshal(ToolCall{ID: "row-1", ToolCallID: "call_1"})
	if !strings.Contains(string(withID), `"toolCallId":"call_1"`) {
		t.Fatalf("json = %s", withID)
	}
	old, _ := json.Marshal(ToolCall{ID: "row-2"})
	if strings.Contains(string(old), "toolCallId") {
		t.Fatalf("an old record must not carry an empty id: %s", old)
	}
}
