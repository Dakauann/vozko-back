package conversation_repository

import (
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func (r *repository) MarkOutboundStatusUpTo(entryID string, entryType shared.EntryType, status conversation.DeliveryStatus, upTo time.Time) (int64, error) {
	previous := status.Supersedes()
	if len(previous) == 0 {
		return 0, nil
	}
	lower := make([]string, 0, len(previous))
	for _, s := range previous {
		lower = append(lower, string(s))
	}
	result := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ? AND direction = ? AND created_at <= ? AND delivery_status IN ?",
			entryID, string(entryType), string(conversation.MessageDirectionOutbound), upTo, lower).
		Updates(map[string]interface{}{
			"delivery_status": string(status),
			"updated_at":      time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}

var _ conversation.DeliveryWatermarkRepository = (*repository)(nil)
