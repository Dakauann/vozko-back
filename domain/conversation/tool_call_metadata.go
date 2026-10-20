package conversation

import (
	"encoding/json"
	"strings"
)

const ToolCallIDMetadataKey = "tool_call_id"

func ToolCallMetadata(toolCallID string) json.RawMessage {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return nil
	}
	raw, err := withMetadataValue(nil, ToolCallIDMetadataKey, toolCallID)
	if err != nil {
		return nil
	}
	return raw
}
