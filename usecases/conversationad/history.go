package conversationad

import (
	"context"

	"vozko/domain/conversation"
)

type adAwareHistory struct {
	inner    conversation.MessageHistoryManager
	recorder *Recorder
}

func WithAdOrigins(inner conversation.MessageHistoryManager, recorder *Recorder) conversation.MessageHistoryManager {
	if inner == nil || recorder == nil {
		return inner
	}
	return &adAwareHistory{inner: inner, recorder: recorder}
}

func (h *adAwareHistory) Record(ctx context.Context, record conversation.MessageHistoryRecord) error {
	if err := h.inner.Record(ctx, record); err != nil {
		return err
	}
	if record.AdReferral != nil && record.SentBy.IsContact() {
		h.recorder.Record(ctx, record.GetEntryID(), record.EntryType, record.AdReferral)
	}
	return nil
}
