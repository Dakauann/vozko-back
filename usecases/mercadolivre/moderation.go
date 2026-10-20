package mercadolivre

import (
	"encoding/json"
	"strings"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
)

const ModerationMetadataKey = "mercadolivre_moderation"

const moderationRejected = "rejected"

func moderationValue(status mldomain.MessageStatus, moderationStatus, reason string) map[string]string {
	value := map[string]string{"status": string(status)}
	if s := strings.TrimSpace(moderationStatus); s != "" {
		value["moderation"] = s
	}
	if r := strings.TrimSpace(reason); r != "" {
		value["reason"] = r
	}
	return value
}

func moderationFailed(status mldomain.MessageStatus, moderationStatus string) bool {
	return status == mldomain.MessageModerated || status == mldomain.MessageRejected || strings.EqualFold(moderationStatus, moderationRejected)
}

func storedModeration(m *conversation.Message) map[string]string {
	if m == nil || len(m.Metadata) == 0 {
		return nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(m.Metadata, &meta); err != nil {
		return nil
	}
	raw, ok := meta[ModerationMetadataKey]
	if !ok {
		return nil
	}
	var value map[string]string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func sameModeration(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
