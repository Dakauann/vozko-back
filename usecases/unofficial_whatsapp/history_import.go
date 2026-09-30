package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

var (
	errAlreadyStored          = errors.New("unofficial whatsapp: message already stored")
	errHistoryBeforeConnected = errors.New("unofficial whatsapp: history arrived before the session was recorded as connected")
)

type historyImport struct {
	pass          uw.HistoryPass
	reachedWindow bool
	firstFailure  error
}

type importedConversation struct {
	chatID    string
	contactID string
	leadID    string
	imported  int
	oldest    time.Time
	newest    time.Time
}

type historyTally struct {
	result        historyImport
	conversations map[string]*importedConversation
	subjects      map[string]*uw.Contact
	order         []string
	unattributed  int
	outsideScope  int
	olderThanWin  int
	notAMessage   int
}

func (uc *HandleWebhookUseCase) ingestHistoryBatch(ctx context.Context, instance *uw.Instance, env *uw.Envelope) error {
	if !uw.HasItems(env.Data) {
		return nil
	}
	if uc.historyOff || !instance.ImportHistory {
		log.Printf("[unofficial-whatsapp][history] instance %s: history batch %d/%d dropped, import is disabled",
			instance.ID, env.Batch.Number, env.Batch.Total)
		return nil
	}
	if !instance.SessionLive() {
		return fmt.Errorf("%w: instance %s is %s", errHistoryBeforeConnected, instance.ID, instance.Status)
	}
	if err := uc.handover.Adopt(ctx, instance); err != nil {
		return err
	}

	result := uc.importHistory(ctx, instance, env, uc.historyWindowStart())
	log.Printf("[unofficial-whatsapp][history] instance %s: webhook batch %d/%d (%s) done: seen=%d imported=%d duplicate=%d skipped=%d failed=%d",
		instance.ID, env.Batch.Number, env.Batch.Total, env.Batch.Status,
		result.pass.Seen, result.pass.Imported, result.pass.Duplicate, result.pass.Skipped, result.pass.Failed)
	if result.pass.Failed > 0 {
		return fmt.Errorf("unofficial whatsapp: %d history messages failed to save: %w", result.pass.Failed, result.firstFailure)
	}
	return nil
}

func (uc *HandleWebhookUseCase) importHistory(
	ctx context.Context,
	instance *uw.Instance,
	env *uw.Envelope,
	windowFrom time.Time,
) historyImport {
	tally := &historyTally{
		conversations: map[string]*importedConversation{},
		subjects:      map[string]*uw.Contact{},
	}

	events := uw.NormalizeEnvelope(instance.ID, env)
	if len(events) == 0 && uw.HasItems(env.Data) {
		log.Printf("[unofficial-whatsapp][history] instance %s: %d provider items normalised to zero messages; first item keys: %v",
			instance.ID, uw.ItemCount(env.Data), uw.DescribeUnknownBody(uw.FirstItem(env.Data)))
	}

	for _, ev := range events {
		if ev == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			tally.fail(err)
			break
		}
		tally.result.pass.Seen++
		uc.importHistoricalEvent(ctx, instance, ev, windowFrom, tally)
	}

	uc.reportHistoryImport(instance, env, tally)
	uc.refreshImportedProfiles(ctx, instance, tally)
	for _, conversationID := range tally.order {
		uc.broadcastEntryUpdate(conversationID)
	}
	return tally.result
}

func (uc *HandleWebhookUseCase) importHistoricalEvent(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
	windowFrom time.Time,
	tally *historyTally,
) {
	switch {
	case !ev.IsHistoryImport():
		tally.notAMessage++
		tally.result.pass.Skipped++
		return
	case ev.SubjectJID() == "":
		tally.unattributed++
		tally.result.pass.Skipped++
		return
	case ev.IsGroup && !instance.HandleGroups:
		tally.outsideScope++
		tally.result.pass.Skipped++
		return
	case !windowFrom.IsZero() && ev.Timestamp.Before(windowFrom):
		tally.olderThanWin++
		tally.result.pass.Skipped++
		tally.result.reachedWindow = true
		return
	}

	sub, err := uc.storeMessage(ctx, instance, ev)
	switch {
	case err == nil:
		tally.imported(sub, ev)
		tally.seen(sub)
	case errors.Is(err, errAlreadyStored):
		tally.result.pass.Duplicate++
		tally.seen(sub)
	case errors.Is(err, errUnattributableEvent):
		tally.unattributed++
		tally.result.pass.Skipped++
	default:
		log.Printf("[unofficial-whatsapp][history] instance %s: message %s in chat %s failed to import: %v",
			instance.ID, ev.ProviderMessageID, ev.ChatID, err)
		tally.fail(err)
	}
}

func (uc *HandleWebhookUseCase) reportHistoryImport(instance *uw.Instance, env *uw.Envelope, tally *historyTally) {
	for _, conversationID := range tally.order {
		c := tally.conversations[conversationID]
		log.Printf("[unofficial-whatsapp][history] instance %s: conversation %s (chat %s, contact %s, lead %s) +%d messages from %s to %s",
			instance.ID, conversationID, c.chatID, c.contactID, orNone(c.leadID), c.imported,
			c.oldest.Format(time.RFC3339), c.newest.Format(time.RFC3339))
	}
	if tally.unattributed > 0 {
		log.Printf("[unofficial-whatsapp][history] instance %s: %d messages named no chat and were dropped; first item keys: %v",
			instance.ID, tally.unattributed, uw.DescribeUnknownBody(uw.FirstItem(env.Data)))
	}
	if tally.outsideScope > 0 {
		log.Printf("[unofficial-whatsapp][history] instance %s: %d group messages skipped because this number does not handle groups",
			instance.ID, tally.outsideScope)
	}
	if tally.olderThanWin > 0 {
		log.Printf("[unofficial-whatsapp][history] instance %s: %d messages older than the import window skipped",
			instance.ID, tally.olderThanWin)
	}
	if tally.notAMessage > 0 {
		log.Printf("[unofficial-whatsapp][history] instance %s: %d reactions or receipts in history skipped",
			instance.ID, tally.notAMessage)
	}
}

func (t *historyTally) imported(sub *chatContext, ev *uw.Event) {
	t.result.pass.Imported++
	t.result.pass.Oldest = earlierOf(t.result.pass.Oldest, ev.Timestamp)
	t.result.pass.Newest = laterOf(t.result.pass.Newest, ev.Timestamp)

	conversationID := sub.conversation.ID
	c, ok := t.conversations[conversationID]
	if !ok {
		c = &importedConversation{
			chatID:    sub.conversation.ChatID,
			contactID: sub.subject.ID,
			oldest:    ev.Timestamp,
			newest:    ev.Timestamp,
		}
		t.conversations[conversationID] = c
		t.order = append(t.order, conversationID)
	}
	if sub.subject.LeadID != nil {
		c.leadID = *sub.subject.LeadID
	}
	c.imported++
	if ev.Timestamp.Before(c.oldest) {
		c.oldest = ev.Timestamp
	}
	if ev.Timestamp.After(c.newest) {
		c.newest = ev.Timestamp
	}
}

func (t *historyTally) fail(err error) {
	t.result.pass.Failed++
	if t.result.firstFailure == nil {
		t.result.firstFailure = err
	}
}

func (uc *HandleWebhookUseCase) storeMessage(ctx context.Context, instance *uw.Instance, ev *uw.Event) (*chatContext, error) {
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return nil, err
	}

	if ev.Backfill {
		stored, err := uc.alreadyStored(ctx, instance, sub, ev.ProviderMessageID)
		if err != nil {
			return nil, err
		}
		if stored {
			return sub, errAlreadyStored
		}
	}

	if ev.Outbound() {
		err = uc.conversations.RecordOutbound(ctx, sub.conversation.ID, ev.Timestamp)
	} else {
		err = uc.conversations.RecordInbound(ctx, sub.conversation.ID, ev.Timestamp)
	}
	if err != nil {
		return nil, err
	}
	if err := uc.recordMessage(ctx, instance, sub, ev, senderFor(ev, sub)); err != nil {
		return nil, err
	}
	return sub, nil
}

func (uc *HandleWebhookUseCase) alreadyStored(ctx context.Context, instance *uw.Instance, sub *chatContext, providerMessageID string) (bool, error) {
	if uc.messages == nil || providerMessageID == "" {
		return false, nil
	}
	entryIDs, err := uc.conversations.EntryIDsForContact(ctx, instance.ID, sub.conversation.ContactID)
	if err != nil {
		return false, fmt.Errorf("unofficial whatsapp: list conversations of contact %s: %w", sub.conversation.ContactID, err)
	}
	if !containsString(entryIDs, sub.conversation.ID) {
		entryIDs = append(entryIDs, sub.conversation.ID)
	}

	for _, entryID := range entryIDs {
		existing, err := uc.messages.GetByEntryAndExternalMessageID(
			shared.EntryTypeUnofficialWhatsApp, entryID, providerMessageID)
		switch {
		case err == nil && existing != nil:
			return true, nil
		case err == nil, errors.Is(err, conversation.ErrMessageNotFound):
			continue
		default:
			return false, fmt.Errorf("unofficial whatsapp: duplicate check for %s: %w", providerMessageID, err)
		}
	}
	return false, nil
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func senderFor(ev *uw.Event, sub *chatContext) conversation.SentBy {
	if ev.Outbound() {
		return conversation.SentExternally()
	}
	return conversation.SentByContact(sub.authorHandle)
}

func earlierOf(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.Before(*current) {
		return &candidate
	}
	return current
}

func laterOf(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.After(*current) {
		return &candidate
	}
	return current
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func (uc *HandleWebhookUseCase) refreshImportedProfiles(ctx context.Context, instance *uw.Instance, tally *historyTally) {
	for _, subject := range tally.subjects {
		uc.profiles.refresh(ctx, instance, subject, false)
	}
}

func (t *historyTally) seen(sub *chatContext) {
	if sub == nil || sub.subject == nil || sub.subject.IsGroup {
		return
	}
	t.subjects[sub.subject.ID] = sub.subject
}

func (uc *HandleWebhookUseCase) historyWindowStart() time.Time {
	if uc.historyWindow <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(-uc.historyWindow)
}
