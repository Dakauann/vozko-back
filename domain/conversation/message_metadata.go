package conversation

import (
	"bytes"
	"encoding/json"
)

func metadataFields(raw json.RawMessage) map[string]json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

func metadataString(fields map[string]json.RawMessage, key string) string {
	var value string
	if err := json.Unmarshal(fields[key], &value); err != nil {
		return ""
	}
	return value
}

func withMetadataValue(raw json.RawMessage, key string, value any) (json.RawMessage, error) {
	fields := metadataFields(raw)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw, err
	}
	fields[key] = encoded
	return json.Marshal(fields)
}
