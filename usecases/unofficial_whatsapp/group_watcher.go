package unofficial_whatsapp

import (
	"context"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

type GroupWatcher interface {
	GroupMessage(ctx context.Context, instance *uw.Instance, groupJID string, hasText bool)
	GroupChanged(ctx context.Context, instance *uw.Instance, groupJID string, change uw.GroupParticipantChange)
	SeatSynced(ctx context.Context, instance *uw.Instance, seat *uw.Group)
}

func (uc *HandleWebhookUseCase) SetGroupWatcher(w GroupWatcher) {
	uc.groupWatcher = w
	uc.groups.watcher = w
}

func (uc *HandleWebhookUseCase) watchGroupMessage(ctx context.Context, instance *uw.Instance, sub *chatContext, ev *uw.Event) {
	if uc.groupWatcher == nil || ev.Backfill || sub == nil || sub.conversation == nil || !sub.conversation.IsGroup {
		return
	}
	uc.groupWatcher.GroupMessage(ctx, instance, sub.conversation.ChatID, strings.TrimSpace(ev.Text) != "")
}

func (uc *HandleWebhookUseCase) watchGroupChange(ctx context.Context, instance *uw.Instance, groupJID string, change uw.GroupParticipantChange) {
	if uc.groupWatcher == nil || !uw.IsGroupJID(groupJID) {
		return
	}
	uc.groupWatcher.GroupChanged(ctx, instance, groupJID, change)
}
