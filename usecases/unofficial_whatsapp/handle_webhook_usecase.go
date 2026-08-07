package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/workflow"
)

// profileTTL bounds how stale a cached contact profile may get before the next
// inbound message refreshes it. Enrichment is lazy because profile reads compete
// with the send budget on the same instance.
const profileTTL = 7 * 24 * time.Hour

// AssignmentService is the round-robin port. Narrow by design so this package
// does not depend on the whole conversation usecase package.
//
// The third argument is the channel account id — here the instance — which is
// what keeps each connected number's round-robin pool separate.
type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

// AIReplier lets an AI agent attend this channel. A nil message with a nil error
// means "deliberately not answered" (automation off, empty body), which is a
// normal outcome rather than a failure.
type AIReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

// WorkflowTrigger fires workflow triggers. The event is channel-neutral, so
// every node that keys on (entry_id, entry_type) works here unchanged.
type WorkflowTrigger interface {
	Evaluate(event workflow.TriggerEvent)
}

// AnalysisScheduler stamps a conversation for deferred AI analysis.
type AnalysisScheduler interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

// LeadLinker resolves a phone number to a CRM lead, creating one if needed.
//
// This is the port that makes the channel first-class: unlike Instagram's IGSID
// or Telegram's user id, every contact here IS a phone number, so the dialer,
// boletos, opportunities and export all address the same person the inbox does.
type LeadLinker interface {
	EnsureLeadForPhone(ctx context.Context, workspaceID, phone, name string) (string, error)
}

// HandleWebhookUseCase turns one queued webhook body into CRM state.
type HandleWebhookUseCase struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	messaging     uw.MessagingAPI

	history conversation.MessageHistoryManager
	// messages is the conversation message store, needed to advance a row's
	// delivery status when the provider reports Sent -> Delivered -> Read.
	messages    conversation.MessageRepository
	convMedia   conversation.ConversationMediaRepository
	fileStorage media.FileStorage
	broadcaster conversation.EventBroadcaster
	assignments AssignmentService
	aiReply     AIReplier
	workflows   WorkflowTrigger
	leads       LeadLinker
	analysis    AnalysisScheduler
	sync        sessionSync
}

// HandleWebhookDeps groups the dependencies so the constructor stays readable.
type HandleWebhookDeps struct {
	Instances     uw.InstanceRepository
	Servers       uw.ServerRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Messaging     uw.MessagingAPI

	History     conversation.MessageHistoryManager
	Messages    conversation.MessageRepository
	ConvMedia   conversation.ConversationMediaRepository
	FileStorage media.FileStorage
	Broadcaster conversation.EventBroadcaster
	Assignments AssignmentService
	AIReply     AIReplier
	Workflows   WorkflowTrigger
	Leads       LeadLinker
	Analysis    AnalysisScheduler
}

func NewHandleWebhookUseCase(d HandleWebhookDeps) *HandleWebhookUseCase {
	return &HandleWebhookUseCase{
		instances:     d.Instances,
		servers:       d.Servers,
		contacts:      d.Contacts,
		conversations: d.Conversations,
		messaging:     d.Messaging,
		history:       d.History,
		messages:      d.Messages,
		convMedia:     d.ConvMedia,
		fileStorage:   d.FileStorage,
		broadcaster:   d.Broadcaster,
		assignments:   d.Assignments,
		aiReply:       d.AIReply,
		workflows:     d.Workflows,
		leads:         d.Leads,
		analysis:      d.Analysis,
		sync:          sessionSync{instances: d.Instances},
	}
}

// Execute processes one queued webhook body.
//
// A body can normalize to several events (a history replay is many messages in
// one delivery), and one failing event must not discard the rest: the provider
// has no replay endpoint, so a dropped sibling is permanently lost.
func (uc *HandleWebhookUseCase) Execute(ctx context.Context, q *QueuedEvent) error {
	if q == nil || len(q.Body) == 0 {
		return uw.ErrInvalidEvent
	}

	instance, err := uc.instances.FindByID(ctx, q.InstanceID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnknownInstance, q.InstanceID)
	}

	env, err := uw.DecodeEnvelope(q.Body)
	if err != nil {
		return err
	}

	var firstErr error
	for _, ev := range uw.NormalizeEnvelope(instance.ID, env) {
		if ev == nil {
			continue
		}
		if err := uc.handleEvent(ctx, instance, ev); err != nil {
			log.Printf("[unofficial-whatsapp] instance %s: event %s failed: %v",
				instance.ID, ev.Kind, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (uc *HandleWebhookUseCase) handleEvent(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	switch ev.Kind {
	case uw.EventInboundMessage:
		return uc.handleInbound(ctx, instance, ev)
	case uw.EventOutboundEcho, uw.EventOutboundFromDevice:
		return uc.handleOutbound(ctx, instance, ev)
	case uw.EventMessageStatus, uw.EventMessageEdited, uw.EventMessageDeleted:
		return uc.handleMessageUpdate(ctx, instance, ev)
	case uw.EventReaction:
		return uc.handleReaction(ctx, instance, ev)
	case uw.EventConnection:
		return uc.handleConnection(ctx, instance, ev)
	case uw.EventBlockToggle:
		return uc.handleBlockToggle(ctx, instance, ev)
	case uw.EventContactUpdate:
		return uc.handleContactUpdate(ctx, instance, ev)
	case uw.EventCall:
		return uc.handleCall(ctx, instance, ev)
	case uw.EventIgnored:
		return nil
	default:
		// Never dropped silently: this provider ships new event kinds without
		// notice, and an unlogged drop is indistinguishable from a working
		// integration.
		log.Printf("[unofficial-whatsapp] instance %s: unhandled provider event %q: %s",
			instance.ID, ev.ProviderEvent, truncateRaw(ev.Raw, 400))
		return nil
	}
}

// ---------------------------------------------------------------- inbound

func (uc *HandleWebhookUseCase) handleInbound(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, conv, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, conv, contact, ev, conversation.MessageDirectionInbound); err != nil {
		return err
	}

	// Everything below is ATTENDANCE, and a backfilled message must not trigger
	// any of it: a connect replays up to seven days at once, and assigning,
	// answering and analysing that burst would bury an operator under
	// conversations nobody has triaged. The transcript above is still complete.
	if !ev.RunsAutomation() {
		uc.broadcastEntryUpdate(conv.ID)
		return nil
	}

	uc.ensureAssignment(conv, instance)
	uc.fireWorkflowTriggers(instance, conv, ev)
	uc.maybeReplyWithAgent(ctx, instance, conv, ev)
	uc.scheduleAnalysis(instance, conv)
	uc.broadcastEntryUpdate(conv.ID)
	return nil
}

// handleOutbound records a message that left from this number.
//
// Both kinds land here, and the difference is what happens next rather than what
// is stored: an ECHO reconciles against the row the send path already wrote
// (MessageHistoryManager matches on the provider id), while a message the OWNER
// typed on their phone is genuinely new. Neither may trigger automation — the
// first would have the AI answer itself, the second answer a colleague.
func (uc *HandleWebhookUseCase) handleOutbound(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, conv, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordOutbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, conv, contact, ev, conversation.MessageDirectionOutbound); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(conv.ID)
	return nil
}

// resolveContext resolves the contact and conversation an event belongs to,
// bridging the contact to a CRM lead on the way.
//
// The lead bridge is what separates this channel from Instagram's and
// Telegram's: their contacts are opaque provider ids that no other subsystem can
// address, while this one is an E.164 number the whole CRM already keys on.
func (uc *HandleWebhookUseCase) resolveContext(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
) (*uw.Contact, *uw.Conversation, error) {
	contact, err := uc.contacts.FindOrCreate(ctx, uw.FindOrCreateContactInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		JID:         ev.SenderJID,
		LID:         ev.SenderLID,
		PhoneNumber: ev.SenderPhone,
		Name:        ev.SenderName,
	})
	if err != nil {
		return nil, nil, err
	}

	uc.bridgeLead(ctx, instance, contact, ev)
	uc.enrichContact(ctx, contact, ev)

	conv, err := uc.conversations.FindOrCreate(ctx, uw.FindOrCreateConversationInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		ContactID:   contact.ID,
		ChatID:      ev.ChatID,
		IsGroup:     ev.IsGroup,
	})
	if err != nil {
		return nil, nil, err
	}
	return contact, conv, nil
}

// bridgeLead attaches the CRM lead this contact is.
//
// Best-effort: a failure here must never drop a customer's message. A contact
// without a lead still renders (the identity lookup covers it) and the next
// inbound message retries the bridge.
func (uc *HandleWebhookUseCase) bridgeLead(ctx context.Context, instance *uw.Instance, contact *uw.Contact, ev *uw.Event) {
	if uc.leads == nil || contact.LeadID != nil || contact.PhoneNumber == "" {
		return
	}
	// A group's chat id is not a person's number, so there is no lead to bridge
	// to; linking one would attach the group's transcript to a random contact.
	if ev.IsGroup {
		return
	}

	leadID, err := uc.leads.EnsureLeadForPhone(ctx, instance.WorkspaceID, contact.PhoneNumber, contact.DisplayName())
	if err != nil || leadID == "" {
		if err != nil {
			log.Printf("[unofficial-whatsapp] lead bridge failed for contact %s: %v", contact.ID, err)
		}
		return
	}
	if err := uc.contacts.LinkLead(ctx, contact.ID, leadID); err != nil {
		log.Printf("[unofficial-whatsapp] lead link failed for contact %s: %v", contact.ID, err)
		return
	}
	contact.LeadID = &leadID
}

// enrichContact refreshes a stale profile from what the event already carried.
//
// Free enrichment only: the event brought a display name, so no extra provider
// call is made. Anything richer waits for the profile fetch the send path can
// afford.
func (uc *HandleWebhookUseCase) enrichContact(ctx context.Context, contact *uw.Contact, ev *uw.Event) {
	name := strings.TrimSpace(ev.SenderName)
	if name == "" || name == contact.Name {
		return
	}
	if !contact.ProfileIsStale(time.Now().UTC(), profileTTL) && contact.Name != "" {
		return
	}
	err := uc.contacts.UpdateProfile(ctx, contact.ID, uw.ContactProfile{
		Name:      name,
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] profile refresh failed for contact %s: %v", contact.ID, err)
		return
	}
	contact.Name = name
}

// ---------------------------------------------------------------- messages

// recordMessage persists one message through the shared history manager.
//
// Always the shared manager, never a direct write: it owns dedup, persistence
// and websocket fan-out, and a channel that wrote conversation_messages itself
// would silently opt out of all three.
func (uc *HandleWebhookUseCase) recordMessage(
	ctx context.Context,
	instance *uw.Instance,
	conv *uw.Conversation,
	contact *uw.Contact,
	ev *uw.Event,
	direction conversation.MessageHistoryDirection,
) error {
	if uc.history == nil {
		return nil
	}

	record := conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeUnofficialWhatsApp,
		Channel:           conversation.MessageChannelUnofficialWhatsApp,
		MessageType:       messageTypeFor(ev),
		ProviderMessageID: ev.ProviderMessageID,
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
		SenderName:        contact.DisplayName(),
		SenderAvatar:      contact.PictureURL,
	}
	if direction == conversation.MessageDirectionInbound {
		record.From, record.To = contact.Handle(), instance.Label()
	} else {
		record.From, record.To = instance.Label(), contact.Handle()
	}
	if ev.QuotedProviderMessageID != "" {
		record.ReplyToWAMessageID = ev.QuotedProviderMessageID
	}

	if attachment := uc.storeAttachment(ctx, instance, conv, ev); attachment != nil {
		record.MediaID = attachment.mediaID
		record.MediaType = attachment.mediaType
		record.MediaURL = attachment.url
	}

	// Nothing is ever dropped for being empty.
	//
	// Persistence rejects a message with neither text nor media, and the consumer
	// treats that as retryable — so an event that can never gain content is
	// retried to exhaustion and then lost. That is reachable in two ways: an
	// attachment whose download failed (storeAttachment degrades by design), and
	// any message type this normalizer has no text for yet. A placeholder naming
	// what arrived keeps the conversation honest; a missing turn does not.
	if strings.TrimSpace(record.Text) == "" && record.MediaID == "" {
		record.Text = placeholderForEmptyMessage(ev)
	}
	return uc.history.Record(ctx, direction, record)
}

// placeholderForEmptyMessage names what arrived when there is nothing to show.
//
// Kept deliberately plain and in the operator's reading language-neutral form:
// this is a last-resort marker for a message the channel could not render, not
// a feature. It names the media kind when there is one, because "the customer
// sent a photo we could not fetch" and "the customer sent something we did not
// understand" are different support conversations.
func placeholderForEmptyMessage(ev *uw.Event) string {
	switch ev.Media {
	case uw.MediaImage:
		return "[imagem]"
	case uw.MediaVideo:
		return "[vídeo]"
	case uw.MediaAudio, uw.MediaVoice:
		return "[áudio]"
	case uw.MediaDocument:
		return "[documento]"
	case uw.MediaSticker:
		return "[figurinha]"
	}
	if ev.FileName != "" {
		return "[" + ev.FileName + "]"
	}
	return "[mensagem sem conteúdo]"
}

// storedAttachment is a downloaded attachment persisted to object storage.
type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

// storeAttachment downloads an inbound attachment and persists it.
//
// Failures degrade rather than abort: the message row is still written, so the
// transcript shows that something arrived even when the bytes could not be
// fetched. An invisible gap is far worse than a bubble with no preview.
func (uc *HandleWebhookUseCase) storeAttachment(
	ctx context.Context,
	instance *uw.Instance,
	conv *uw.Conversation,
	ev *uw.Event,
) *storedAttachment {
	if ev.Media == uw.MediaNone || uc.messaging == nil || uc.fileStorage == nil {
		return nil
	}

	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		log.Printf("[unofficial-whatsapp] media skipped, server unavailable: %v", err)
		return nil
	}

	remote, err := uc.messaging.DownloadMedia(ctx, uw.RefFor(server, instance), ev.ProviderMessageID)
	if err != nil || remote == nil || len(remote.Data) == 0 {
		log.Printf("[unofficial-whatsapp] media download failed for message %s: %v", ev.ProviderMessageID, err)
		return nil
	}

	mimeType := firstNonEmpty(ev.MIMEType, remote.MIMEType)
	objectKey := path.Join("conversations", "unofficial_whatsapp", conv.ID,
		ev.ProviderMessageID+extensionFor(mimeType, ev.FileName))

	// The real content type is passed rather than left empty: the stored value
	// becomes the Content-Type the CDN serves, and providers that fetch these
	// URLs decide from that header alone whether the asset is sendable.
	if err := uc.fileStorage.UploadFile(objectKey, remote.Data, mimeType); err != nil {
		log.Printf("[unofficial-whatsapp] media upload failed for message %s: %v", ev.ProviderMessageID, err)
		return nil
	}
	url := uc.fileStorage.GetFileURL(objectKey)

	mediaType := conversationMediaType(ev.Media)
	stored := &storedAttachment{mediaType: mediaType, url: url}
	if uc.convMedia != nil {
		row := &conversation.ConversationMedia{
			EntryID:          conv.ID,
			EntryType:        shared.EntryTypeUnofficialWhatsApp,
			Type:             mediaType,
			MimeType:         mimeType,
			OriginalFilename: firstNonEmpty(ev.FileName, path.Base(objectKey)),
			SizeBytes:        int64(len(remote.Data)),
			URL:              url,
		}
		if err := uc.convMedia.Create(row); err != nil {
			log.Printf("[unofficial-whatsapp] media row failed for message %s: %v", ev.ProviderMessageID, err)
		} else {
			stored.mediaID = row.ID
		}
	}
	return stored
}

// handleMessageUpdate advances a message's delivery track, or tombstones it.
//
// This is the channel's advantage over Telegram: real Sent → Delivered → Read
// callbacks, so the status ticks the CRM renders are honest.
func (uc *HandleWebhookUseCase) handleMessageUpdate(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	// The conversation is looked up for the LIVE PUSH only, and it is allowed to
	// be missing.
	//
	// Status callbacks arrive with an empty chatid — the provider identifies the
	// message, not the chat — so resolving the conversation first and returning
	// on failure threw every delivered/read receipt away, which is exactly what
	// it did until this comment existed. The row update below is keyed by the
	// provider's message id and never needed the conversation at all.
	conv, convErr := uc.conversations.FindByChatID(ctx, instance.ID, ev.ChatID)

	entryID := ""
	if convErr == nil && conv != nil {
		entryID = conv.ID
	}

	uc.advanceDeliveryStatus(ctx, entryID, ev)
	if entryID != "" {
		uc.broadcastEntryUpdate(entryID)
	}
	return nil
}

// advanceDeliveryStatus writes the provider's status onto the message row and
// pushes it to open inboxes.
//
// Without this the callbacks were received, classified, and thrown away: the
// ticks an operator reads in the CRM stayed on "sent" forever no matter how
// many times the customer opened the chat. Doing the write AND the live push
// together matters because they answer different questions — the write is what
// a reopened conversation shows, the push is what an operator watching right
// now sees.
func (uc *HandleWebhookUseCase) advanceDeliveryStatus(
	ctx context.Context,
	entryID string,
	ev *uw.Event,
) {
	if uc.messages == nil || ev.DeliveryStatus == uw.DeliveryUnknown {
		return
	}
	target := firstNonEmpty(ev.TargetProviderMessageID, ev.ProviderMessageID)
	if target == "" {
		return
	}

	status := crmDeliveryStatus(ev.DeliveryStatus)
	if status == conversation.DeliveryStatusNone {
		return
	}
	if err := uc.messages.UpdateDeliveryStatus(target, status); err != nil {
		// Best-effort by design: a status that could not be written is a
		// cosmetic tick, never a reason to fail (and so retry) a delivery that
		// already succeeded.
		log.Printf("[unofficial-whatsapp] could not advance status for message %s: %v", target, err)
		return
	}
	// The push is skipped when the conversation could not be resolved; the
	// persisted status above is what a reopened conversation shows either way.
	if uc.broadcaster != nil && entryID != "" {
		uc.broadcaster.BroadcastMessageStatus(
			entryID, string(shared.EntryTypeUnofficialWhatsApp), target, status)
	}
}

// crmDeliveryStatus maps the channel's status onto the CRM's.
//
// Deletion is deliberately absent: a tombstone is a message-content change, not
// a delivery state, and handleEvent routes it separately.
func crmDeliveryStatus(status uw.DeliveryStatus) conversation.DeliveryStatus {
	switch status {
	case uw.DeliveryQueued, uw.DeliverySent:
		return conversation.DeliveryStatusSent
	case uw.DeliveryDelivered:
		return conversation.DeliveryStatusDelivered
	case uw.DeliveryRead:
		return conversation.DeliveryStatusRead
	case uw.DeliveryFailed:
		return conversation.DeliveryStatusFailed
	}
	return conversation.DeliveryStatusNone
}

// handleReaction records a reaction against the message it applies to.
//
// An empty emoji is a REMOVAL, not a missing field, and both are recorded so the
// UI can drop the reaction rather than leaving a stale one on screen.
func (uc *HandleWebhookUseCase) handleReaction(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, conv, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}
	if uc.history == nil {
		return nil
	}
	err = uc.history.Record(ctx, conversation.MessageDirectionInbound, conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeUnofficialWhatsApp,
		Channel:           conversation.MessageChannelUnofficialWhatsApp,
		MessageType:       conversation.MessageTypeReaction,
		ProviderMessageID: ev.ProviderMessageID,
		Text:              ev.Emoji,
		From:              contact.Handle(),
		To:                instance.Label(),
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
		SenderName:        contact.DisplayName(),
	})
	if err != nil {
		return err
	}
	uc.broadcastEntryUpdate(conv.ID)
	return nil
}

// ---------------------------------------------------------------- instance

// handleConnection reconciles a pushed session-state change.
//
// It goes through the SAME sessionSync the poll and the connect flow use, which
// is what makes the health backstop skip this instance automatically: the sync
// stamps the last-signal clock, and the backstop selects on it. Writing the
// status directly here would silently cost that backoff.
func (uc *HandleWebhookUseCase) handleConnection(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	_, err := uc.sync.apply(ctx, instance, &uw.Session{
		State:     ev.SessionState,
		Connected: strings.EqualFold(ev.SessionState, "connected"),
	})
	return err
}

func (uc *HandleWebhookUseCase) handleBlockToggle(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, err := uc.contacts.FindByJID(ctx, instance.ID, ev.ChatID)
	if err != nil {
		return nil
	}
	return uc.contacts.SetBlocked(ctx, contact.ID, ev.Blocked, time.Now().UTC())
}

func (uc *HandleWebhookUseCase) handleContactUpdate(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, err := uc.contacts.FindByJID(ctx, instance.ID, ev.ChatID)
	if err != nil || strings.TrimSpace(ev.SenderName) == "" {
		return nil
	}
	return uc.contacts.UpdateProfile(ctx, contact.ID, uw.ContactProfile{
		ContactName: ev.SenderName,
		FetchedAt:   time.Now().UTC(),
	})
}

// handleCall records an inbound call as a timeline marker.
//
// A marker, never a conversational turn: the product does not do AI voice, and a
// "call received" line that read as something the customer said would poison the
// agent's context.
func (uc *HandleWebhookUseCase) handleCall(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	conv, err := uc.conversations.FindByChatID(ctx, instance.ID, ev.ChatID)
	if err != nil || uc.history == nil {
		return nil
	}
	err = uc.history.Record(ctx, conversation.MessageDirectionInbound, conversation.MessageHistoryRecord{
		EntryID:     conv.ID,
		EntryType:   shared.EntryTypeUnofficialWhatsApp,
		Channel:     conversation.MessageChannelUnofficialWhatsApp,
		MessageType: conversation.MessageTypeCallReceived,
		Timestamp:   ev.Timestamp,
		Metadata:    inboundMetadata(ev),
	})
	if err != nil {
		return err
	}
	uc.broadcastEntryUpdate(conv.ID)
	return nil
}

// ---------------------------------------------------------------- attendance

func (uc *HandleWebhookUseCase) ensureAssignment(conv *uw.Conversation, instance *uw.Instance) {
	if uc.assignments == nil {
		return
	}
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeUnofficialWhatsApp), instance.ID)
}

func (uc *HandleWebhookUseCase) scheduleAnalysis(instance *uw.Instance, conv *uw.Conversation) {
	if uc.analysis == nil || (!instance.EnableAnalysis && !instance.EnableAutoStaging) {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeUnofficialWhatsApp)
}

// fireWorkflowTriggers evaluates message and first-message triggers.
//
// A tapped button carries its OPTION ID, which is what an interactive-prompt
// node branches on. Sending only the visible label would route every press down
// the no-match branch and read as "the customer typed something unexpected".
func (uc *HandleWebhookUseCase) fireWorkflowTriggers(instance *uw.Instance, conv *uw.Conversation, ev *uw.Event) {
	if uc.workflows == nil || !instance.EnableWorkflow {
		return
	}
	if !conv.RunsAutomation(instance.HandleGroups) {
		return
	}

	data := map[string]any{"text": ev.Text}
	if ev.OptionID != "" {
		data[workflow.DataKeySelectedOptionID] = ev.OptionID
		data[workflow.DataKeySelectedOptionTitle] = ev.Text
	}

	uc.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: instance.WorkspaceID,
		EntryID:     conv.ID,
		EntryType:   string(shared.EntryTypeUnofficialWhatsApp),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})
}

// maybeReplyWithAgent hands the turn to the AI, honouring both gates.
//
// The per-conversation override wins over the instance switch, which is what
// lets an operator take one conversation over without silencing the agent
// everywhere.
func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	instance *uw.Instance,
	conv *uw.Conversation,
	ev *uw.Event,
) {
	if uc.aiReply == nil || instance.AgentID == nil {
		return
	}
	if !conv.RunsAutomation(instance.HandleGroups) {
		return
	}

	_, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           instance.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeUnofficialWhatsApp,
		AgentID:               *instance.AgentID,
		AgentResponsesEnabled: instance.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  ev.Text,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] AI reply failed for entry %s: %v", conv.ID, err)
	}
}

func (uc *HandleWebhookUseCase) broadcastEntryUpdate(entryID string) {
	if uc.broadcaster == nil || entryID == "" {
		return
	}
	uc.broadcaster.BroadcastEntryUpdate(entryID, string(shared.EntryTypeUnofficialWhatsApp), nil)
}

// ---------------------------------------------------------------- helpers

// messageTypeFor maps an event onto the CRM's message vocabulary.
func messageTypeFor(ev *uw.Event) conversation.MessageType {
	if ev.Outbound() {
		return conversation.MessageTypeOperator
	}
	switch ev.Media {
	case uw.MediaVoice, uw.MediaAudio:
		// Audio, not media: the audio type is what routes a voice note into the
		// speech-to-text path.
		return conversation.MessageTypeAudio
	case uw.MediaNone:
		return conversation.MessageTypeUserMessage
	default:
		return conversation.MessageTypeMedia
	}
}

func conversationMediaType(kind uw.MediaKind) conversation.MediaType {
	switch kind {
	case uw.MediaImage:
		return conversation.MediaTypeImage
	case uw.MediaVideo:
		return conversation.MediaTypeVideo
	case uw.MediaAudio, uw.MediaVoice:
		return conversation.MediaTypeAudio
	case uw.MediaSticker:
		return conversation.MediaTypeSticker
	default:
		return conversation.MediaTypeDocument
	}
}

// inboundMetadata preserves the facts that have no column of their own.
//
// The option id and the group flag matter downstream (workflow branching,
// automation gating) and would otherwise be unrecoverable from the stored row.
func inboundMetadata(ev *uw.Event) json.RawMessage {
	meta := map[string]any{"providerEvent": ev.ProviderEvent}
	if ev.OptionID != "" {
		meta["selectedOptionId"] = ev.OptionID
	}
	if ev.IsGroup {
		meta["isGroup"] = true
	}
	if ev.Backfill {
		meta["backfill"] = true
	}
	if ev.TrackID != "" {
		meta["trackId"] = ev.TrackID
	}
	if ev.DeliveryStatus != uw.DeliveryUnknown {
		meta["deliveryStatus"] = string(ev.DeliveryStatus)
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return encoded
}

func extensionFor(mimeType, fileName string) string {
	if ext := path.Ext(fileName); ext != "" {
		return ext
	}
	switch {
	case strings.Contains(mimeType, "jpeg"):
		return ".jpg"
	case strings.Contains(mimeType, "png"):
		return ".png"
	case strings.Contains(mimeType, "webp"):
		return ".webp"
	case strings.Contains(mimeType, "mp4"):
		return ".mp4"
	case strings.Contains(mimeType, "ogg"):
		return ".ogg"
	case strings.Contains(mimeType, "mpeg"):
		return ".mp3"
	case strings.Contains(mimeType, "pdf"):
		return ".pdf"
	}
	return ".bin"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRaw(raw json.RawMessage, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}
