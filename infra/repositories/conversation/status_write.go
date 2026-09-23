package conversation_repository

import (
	"vozko/domain/conversation"
)

func StatusUpdates(write conversation.StatusWrite) map[string]any {
	updates := map[string]any{
		"conversation_status": string(write.Status),
	}
	switch {
	case write.ClearCloseMeta:
		updates["close_source"] = ""
		updates["close_reason"] = ""
		updates["close_outcome"] = ""
		updates["closed_at"] = nil
	case write.SetCloseMeta:
		updates["close_source"] = string(write.CloseSource)
		updates["close_reason"] = string(write.CloseReason)
		updates["close_outcome"] = write.CloseOutcome
		closedAt := write.ClosedAt
		updates["closed_at"] = &closedAt
	default:
		updates["close_source"] = ""
		updates["close_reason"] = ""
		updates["close_outcome"] = ""
		updates["closed_at"] = nil
	}
	return updates
}
