package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
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
	// profiles fills the name and avatar the CRM shows for a conversation. Not
	// a job — see subject_profile.go for the per-message call budget.
	profiles subjectProfile
	// groups keeps a group chat's subject, roster and admin rules current.
	groups groupMetadata
}

// HandleWebhookDeps groups the dependencies so the constructor stays readable.
type HandleWebhookDeps struct {
	Instances     uw.InstanceRepository
	Servers       uw.ServerRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Groups        uw.GroupRepository
	Messaging     uw.MessagingAPI
	GroupAPI      uw.GroupAPI
	// Assets downloads a provider-hosted profile picture so it can be re-hosted
	// on our own storage. Optional: without it the channel stores names and
	// falls back to initials, which is a degraded avatar rather than a broken
	// inbox.
	Assets uw.RemoteAssetFetcher

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
	profiles := newSubjectProfile(d)
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
		profiles:      profiles,
		groups:        newGroupMetadata(d, profiles),
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

// errUnattributableEvent marks an event that names no chat and no sender.
//
// Terminal by nature: the consumer retries errors, and no number of retries will
// give an event an identity it never carried. handleEvent swallows it so a
// sibling event in the same delivery is not failed alongside it.
var errUnattributableEvent = errors.New("unofficial whatsapp: event identifies no chat")

func (uc *HandleWebhookUseCase) handleEvent(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	err := uc.dispatch(ctx, instance, ev)
	if errors.Is(err, errUnattributableEvent) {
		// Already logged with its payload shape at the point of detection.
		return nil
	}
	return err
}

func (uc *HandleWebhookUseCase) dispatch(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
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
	case uw.EventGroupChanged:
		return uc.handleGroupChanged(ctx, instance, ev)
	case uw.EventCall:
		return uc.handleCall(ctx, instance, ev)
	case uw.EventIgnored:
		return nil
	default:
		// Never dropped silently: this provider ships new event kinds without
		// notice, and an unlogged drop is indistinguishable from a working
		// integration.
		//
		// KEYS ONLY for the payload, never values. An unreadable body is usually
		// a real customer message, and logging its values would put message text
		// and phone numbers into the log sink — the precise data this channel
		// exists to protect. The key names alone identify a shape change, which
		// is the only reason to look.
		log.Printf("[unofficial-whatsapp] instance %s: unhandled provider event %q, payload keys: %v",
			instance.ID, ev.ProviderEvent, uw.DescribeUnknownBody(ev.Raw))
		return nil
	}
}

// ---------------------------------------------------------------- inbound

func (uc *HandleWebhookUseCase) handleInbound(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordInbound(ctx, sub.conversation.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, sub, ev, conversation.MessageDirectionInbound); err != nil {
		return err
	}

	// Everything below is ATTENDANCE, and two things must not trigger any of it.
	//
	// A backfilled message: a connect replays up to seven days at once, and
	// assigning, answering and analysing that burst would bury an operator under
	// conversations nobody has triaged.
	//
	// A group whose instance has not opted in: it stays visible and repliable —
	// sending is gated by the session and the block, never by this — but it does
	// not enter anyone's queue. Auto-assigning group threads to a random agent
	// is almost never what a workspace wants, which is why HandleGroups exists.
	//
	// The group half of that decision used to live on the EVENT, where it ran
	// before the instance was ever consulted and made HandleGroups unreachable.
	// The transcript above is written either way.
	if !ev.RunsAutomation() || !sub.conversation.InScope(instance.HandleGroups) {
		uc.broadcastEntryUpdate(sub.conversation.ID)
		return nil
	}

	uc.ensureAssignment(sub.conversation, instance)
	uc.fireWorkflowTriggers(instance, sub.conversation, ev)
	uc.maybeReplyWithAgent(ctx, instance, sub.subject, sub.conversation, ev)
	uc.scheduleAnalysis(instance, sub.conversation)
	uc.broadcastEntryUpdate(sub.conversation.ID)
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
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordOutbound(ctx, sub.conversation.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, sub, ev, conversation.MessageDirectionOutbound); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(sub.conversation.ID)
	return nil
}

// chatContext is everything one event needs resolving to, and the reason it is a
// struct is the distinction it carries.
//
// The SUBJECT is who the conversation is with; the AUTHOR is who spoke. In a
// private chat they are the same person and every downstream branch collapses.
// In a group they are not: the subject is the group, the author is a
// participant, and conflating them is what forked one group thread into one CRM
// conversation per member — each labelled with whichever member happened to
// speak first, with the delivery-receipt lookup then picking one of them at
// random.
type chatContext struct {
	// subject is the conversation's identity: the person, or the group. It is
	// the row the inbox renders and the row the avatar hangs off.
	subject *uw.Contact
	// authorName / authorHandle label the individual MESSAGE. In a group they
	// name the participant so the transcript reads like the chat does on a
	// phone, rather than attributing every line to the group itself.
	authorName   string
	authorHandle string
	// authorAvatar is the picture shown beside the bubble.
	authorAvatar string
	// author is the participant's own contact row in a group, and nil in a
	// private chat where the subject already IS the author. It is what lets the
	// history read resolve who spoke after a reload, when the live push's
	// SenderName is long gone.
	author *uw.Contact

	conversation *uw.Conversation
	// group is the cached metadata when this is a group chat, nil otherwise.
	group *uw.Group
}

// resolveContext resolves the subject, the author and the conversation an event
// belongs to, bridging the subject to a CRM lead on the way.
//
// The lead bridge is what separates this channel from Instagram's and
// Telegram's: their contacts are opaque provider ids that no other subsystem can
// address, while this one is an E.164 number the whole CRM already keys on.
func (uc *HandleWebhookUseCase) resolveContext(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
) (*chatContext, error) {
	// SubjectJID, not SenderJID: in a group the sender is a participant and the
	// subject is the chat.
	subjectJID := ev.SubjectJID()

	// An event that identifies nobody cannot be filed, and must not be filed
	// anyway.
	//
	// This is the guard behind a live symptom. Contact identity is uniquely
	// (instance, jid), so an event with no chat and no sender resolved to a
	// contact with an EMPTY jid — and because that row is unique, every later
	// unattributable event resolved to the same one. The result was a single
	// catch-all conversation per connected number, sitting in the operator's
	// inbox titled "unofficial_whatsapp" (the last-resort label for a contact
	// with no name and no handle to show) and filling up with
	// "[mensagem sem conteúdo]".
	//
	// The check lives here rather than in the normalizer on purpose: decoding
	// and attributing are different jobs, and a normalizer that refused to
	// classify an incomplete payload would also stop reporting what shape it
	// arrived in.
	//
	// Returned as nil, not an error: the consumer retries errors, and no number
	// of retries will give this event an identity.
	if subjectJID == "" {
		log.Printf("[unofficial-whatsapp] instance %s: %s event names no chat and no sender, "+
			"dropping it rather than filing it under a nameless contact; payload keys: %v",
			instance.ID, ev.ProviderEvent, uw.DescribeUnknownBody(ev.Raw))
		return nil, errUnattributableEvent
	}

	subject, err := uc.contacts.FindOrCreate(ctx, uw.FindOrCreateContactInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		JID:         subjectJID,
		LID:         subjectLID(ev),
		PhoneNumber: subjectPhone(ev),
		Name:        subjectSeedName(ev),
		IsGroup:     ev.IsGroup,
	})
	if err != nil {
		return nil, err
	}

	conv, err := uc.conversations.FindOrCreate(ctx, uw.FindOrCreateConversationInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		ContactID:   subject.ID,
		ChatID:      ev.ChatID,
		IsGroup:     ev.IsGroup,
	})
	if err != nil {
		return nil, err
	}

	out := &chatContext{subject: subject, conversation: conv}
	uc.enrich(ctx, instance, ev, out)
	uc.resolveAuthor(ctx, instance, ev, out)
	return out, nil
}

// enrich fills in whatever the event did not already carry.
//
// Skipped entirely for a backfill. A connect replays up to seven days of history
// in one burst, and a profile read per replayed message would be hundreds of
// provider calls in a few seconds on a number that has just come online — the
// most automated-looking thing this channel could possibly do.
func (uc *HandleWebhookUseCase) enrich(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
	out *chatContext,
) {
	if ev.Backfill {
		return
	}

	if ev.IsGroup {
		// One call at most, and only when the group is unknown or its metadata
		// was invalidated. The subject's name and avatar are refreshed inside,
		// so a rename lands in the inbox and the group panel together.
		out.group = uc.groups.ensureFresh(ctx, instance, ev.ChatID)
		return
	}

	uc.bridgeLead(ctx, instance, out.subject)
	// Free: the name rode in on the event itself.
	uc.profiles.applyEventName(ctx, out.subject, ev.SenderName)
	// TTL-gated: zero calls for a subject we already have a picture for, which
	// is almost all traffic.
	uc.profiles.refresh(ctx, instance, out.subject, false)
}

// resolveAuthor decides how the individual message is labelled.
//
// In a group this resolves — and PERSISTS — the participant as a contact of
// their own. That is a change from naming them off the event alone, and the
// reason is that the event's name only ever reached the live websocket push:
// SenderName is deliberately never stored on a message row (a frozen name goes
// stale after a rename), so on reload the reader fell back to the conversation's
// subject and every bubble in a group was labelled with the GROUP.
//
// The cost is bounded by who TALKS, not by who is a member. A two-hundred-person
// group where five people speak resolves five contacts, each enriched at most
// once a week and behind the same per-instance burst gate as everyone else. That
// is a very different bill from enriching a roster, which is what the earlier
// decision was avoiding.
//
// These rows are never bridged to a CRM lead: bridgeLead runs on the SUBJECT and
// a group's subject is the group, so a member of a customer's group does not
// silently become a lead in their pipeline.
func (uc *HandleWebhookUseCase) resolveAuthor(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
	out *chatContext,
) {
	if !ev.IsGroup {
		// One person, one label. Everything downstream stays branch-free.
		out.authorName = out.subject.DisplayName()
		out.authorHandle = out.subject.Handle()
		out.authorAvatar = out.subject.PictureURL
		return
	}

	// Fall back to the event before anything else, so a failed lookup still
	// names the person rather than the group.
	out.authorHandle = ev.SenderPhone
	if out.authorHandle != "" {
		out.authorHandle = "+" + out.authorHandle
	} else {
		out.authorHandle = ev.SenderJID
	}
	out.authorName = firstNonEmpty(ev.SenderName, out.authorHandle)

	if ev.SenderJID == "" || ev.SenderJID == ev.ChatID {
		// No participant to resolve — an outbound message, or a payload that
		// named no sender. The group is the right label for the first and the
		// only one available for the second.
		return
	}

	author, err := uc.contacts.FindOrCreate(ctx, uw.FindOrCreateContactInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		JID:         ev.SenderJID,
		LID:         ev.SenderLID,
		PhoneNumber: ev.SenderPhone,
		Name:        ev.SenderName,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] could not resolve group author %s: %v", ev.SenderJID, err)
		return
	}

	if !ev.Backfill {
		uc.profiles.applyEventName(ctx, author, ev.SenderName)
		uc.profiles.refresh(ctx, instance, author, false)
	}

	out.author = author
	out.authorName = author.DisplayName()
	out.authorHandle = author.Handle()
	out.authorAvatar = author.PictureURL
}

// subjectSeedName is the name to create a NEW subject with.
//
// A group's is left empty on purpose: the push name on a group message belongs
// to whoever spoke, and seeding the group with it would name the chat after its
// most recent talker until the metadata read lands.
func subjectSeedName(ev *uw.Event) string {
	if ev.IsGroup {
		return ""
	}
	return ev.SenderName
}

// subjectLID and subjectPhone carry the sender's identifiers only when the
// sender IS the subject. A group has neither, and attaching a participant's to
// it would make one member's LID resolve to the whole group.
func subjectLID(ev *uw.Event) string {
	if ev.IsGroup {
		return ""
	}
	return ev.SenderLID
}

func subjectPhone(ev *uw.Event) string {
	if ev.IsGroup {
		return ""
	}
	return ev.SenderPhone
}

// bridgeLead attaches the CRM lead this subject is.
//
// Best-effort: a failure here must never drop a customer's message. A subject
// without a lead still renders (the identity lookup covers it) and the next
// inbound message retries the bridge.
//
// Never called for a group, and never for a group's participants. A group has no
// number to bridge to, and auto-creating a lead for every member of a
// two-hundred-person thread would flood the CRM with people who have never
// contacted the business.
func (uc *HandleWebhookUseCase) bridgeLead(ctx context.Context, instance *uw.Instance, subject *uw.Contact) {
	if uc.leads == nil || subject.IsGroup || subject.LeadID != nil || subject.PhoneNumber == "" {
		return
	}

	leadID, err := uc.leads.EnsureLeadForPhone(ctx, instance.WorkspaceID, subject.PhoneNumber, subject.DisplayName())
	if err != nil || leadID == "" {
		if err != nil {
			log.Printf("[unofficial-whatsapp] lead bridge failed for contact %s: %v", subject.ID, err)
		}
		return
	}
	if err := uc.contacts.LinkLead(ctx, subject.ID, leadID); err != nil {
		log.Printf("[unofficial-whatsapp] lead link failed for contact %s: %v", subject.ID, err)
		return
	}
	subject.LeadID = &leadID
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
	sub *chatContext,
	ev *uw.Event,
	direction conversation.MessageHistoryDirection,
) error {
	if uc.history == nil {
		return nil
	}

	conv := sub.conversation
	record := conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeUnofficialWhatsApp,
		Channel:           conversation.MessageChannelUnofficialWhatsApp,
		MessageType:       messageTypeFor(ev),
		ProviderMessageID: ev.ProviderMessageID,
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
		// The AUTHOR, not the subject. In a group these differ, and labelling
		// every bubble with the subject would attribute the whole thread to the
		// group instead of to the people in it.
		SenderName:   sub.authorName,
		SenderAvatar: sub.authorAvatar,
	}
	if direction == conversation.MessageDirectionInbound {
		record.From, record.To = sub.authorHandle, instance.Label()
	} else {
		record.From, record.To = instance.Label(), sub.subject.Handle()
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
			// The id is minted HERE, as Telegram, Instagram and the upload
			// endpoint all do. The repository maps this value onto a separate
			// schema struct and the database hook stamps its id onto THAT copy,
			// so a row created without one leaves this object's ID empty — and
			// the message below then links to "" . The bytes upload, the row is
			// written, and the CRM still renders a bare placeholder next to an
			// object nothing can reference.
			ID:               uuid.NewString(),
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
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}
	if uc.history == nil {
		return nil
	}
	err = uc.history.Record(ctx, conversation.MessageDirectionInbound, conversation.MessageHistoryRecord{
		EntryID:           sub.conversation.ID,
		EntryType:         shared.EntryTypeUnofficialWhatsApp,
		Channel:           conversation.MessageChannelUnofficialWhatsApp,
		MessageType:       conversation.MessageTypeReaction,
		ProviderMessageID: ev.ProviderMessageID,
		Text:              ev.Emoji,
		From:              sub.authorHandle,
		To:                instance.Label(),
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
		SenderName:        sub.authorName,
	})
	if err != nil {
		return err
	}
	uc.broadcastEntryUpdate(sub.conversation.ID)
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

// handleContactUpdate consumes a pushed profile change.
//
// This is the cheapest enrichment path there is: the `chats` and `contacts`
// events carry the vendor's whole Chat object — saved name AND picture — so a
// customer changing their photo or their display name reaches the CRM as a PUSH,
// with no call back to WhatsApp at all. The picture is re-hosted only when its
// source url actually changed, so an event that merely re-states what we already
// have costs one string comparison.
func (uc *HandleWebhookUseCase) handleContactUpdate(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	contact, err := uc.contacts.FindByJID(ctx, instance.ID, ev.ChatID)
	if err != nil {
		// A chat we have never opened. Creating a contact from a directory sync
		// would fill the CRM with every number in the owner's address book.
		return nil
	}

	if name := strings.TrimSpace(ev.SenderName); name != "" {
		// FetchedAt is deliberately left zero. The provider pushed a name, which
		// is not a profile read: stamping the clock here would consume the
		// subject's weekly refresh budget and suppress the picture fetch that is
		// due.
		field := uw.ContactProfile{ContactName: name}
		if contact.IsGroup {
			// A group's identity is its subject, not a saved-contact name.
			field = uw.ContactProfile{Name: name}
		}
		if err := uc.contacts.UpdateProfile(ctx, contact.ID, field); err != nil {
			return err
		}
	}

	uc.profiles.applyPushedPicture(ctx, contact, ev.PictureURL)
	uc.broadcastSubjectUpdate(ctx, instance, contact)
	return nil
}

// broadcastSubjectUpdate pushes a refreshed identity to open inboxes.
//
// Without it a renamed contact or a new profile picture only appears on the next
// reload, which reads as "the CRM did not notice" — the same complaint that
// produced this whole path.
func (uc *HandleWebhookUseCase) broadcastSubjectUpdate(ctx context.Context, instance *uw.Instance, subject *uw.Contact) {
	if uc.broadcaster == nil || uc.conversations == nil {
		return
	}
	conv, err := uc.conversations.FindByChatID(ctx, instance.ID, subject.JID)
	if err != nil || conv == nil {
		return
	}
	uc.broadcastEntryUpdate(conv.ID)
}

// handleGroupChanged consumes a `groups` delivery as an INVALIDATION.
//
// It marks the cached row stale and stops. It does not parse the payload, does
// not re-read the group, and does not touch the roster — deliberately, for two
// separate reasons:
//
//   - The provider specifies no schema for this event, so any field we read is a
//     guess. A guess that is wrong writes a roster nobody can tell is wrong,
//     which is worse than one that is briefly stale.
//   - Re-reading here would put a provider call on an event we do not control
//     the rate of. WhatsApp emits these for every group the number is in,
//     including ones nobody in the CRM has ever opened. Marking stale costs one
//     UPDATE, and the re-read happens on the next message in that group — so a
//     group nobody talks in is never read at all.
func (uc *HandleWebhookUseCase) handleGroupChanged(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	if err := uc.groups.markStale(ctx, instance.ID, ev.ChatID); err != nil {
		log.Printf("[unofficial-whatsapp] could not flag group %s as stale: %v", ev.ChatID, err)
	}
	return nil
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
	contact *uw.Contact,
	conv *uw.Conversation,
	ev *uw.Event,
) {
	if uc.aiReply == nil || instance.AgentID == nil {
		return
	}
	if !conv.RunsAutomation(instance.HandleGroups) {
		return
	}

	// The subject's CRM lead, resolved by bridgeLead on the way in. Always nil
	// for a group, which keeps lead-scoped features (memory) inert there.
	var leadID *string
	if contact != nil {
		leadID = contact.LeadID
	}
	_, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           instance.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeUnofficialWhatsApp,
		AgentID:               *instance.AgentID,
		AgentResponsesEnabled: instance.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  ev.Text,
		LeadID:                leadID,
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
//
// The type describes CONTENT, never direction. Direction is its own column and
// is set explicitly on every message this channel records, so encoding it in the
// type buys nothing and costs the content: an outbound branch here returned
// `operator` for everything, so a photo the owner sent from their phone read
// back as a plain note, and a voice note lost the audio type that routes it into
// speech-to-text. Telegram and Instagram still make that trade; this channel
// keeps both facts.
func messageTypeFor(ev *uw.Event) conversation.MessageType {
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
