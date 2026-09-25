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

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/workflow"
)

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type AIReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

type WorkflowTrigger interface {
	Evaluate(event workflow.TriggerEvent)
}

type AnalysisScheduler interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

type LeadLinker interface {
	EnsureLeadForPhone(ctx context.Context, workspaceID, phone, name string) (string, error)
}

type CampaignDeliverySink interface {
	AdvanceFromDelivery(providerMessageID string, status uw.DeliveryStatus)
}

type CampaignAutomation struct {
	CampaignID        string
	Automation        campaign.Automation
	EnableAnalysis    bool
	EnableAutoStaging bool
	EnableAutoMemory  bool
}

type CampaignAutomationSource interface {
	AutomationForConversation(conversationID string) (*CampaignAutomation, bool)
}

type HandleWebhookUseCase struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	messaging     uw.MessagingAPI

	history            conversation.MessageHistoryManager
	messages           conversation.MessageRepository
	convMedia          conversation.ConversationMediaRepository
	fileStorage        media.FileStorage
	broadcaster        conversation.EventBroadcaster
	campaignStatus     CampaignDeliverySink
	campaignAutomation CampaignAutomationSource
	assignments        AssignmentService
	aiReply            AIReplier
	workflows          WorkflowTrigger
	leads              LeadLinker
	analysis           AnalysisScheduler
	sync               sessionSync
	profiles           subjectProfile
	groups             groupMetadata
}

type HandleWebhookDeps struct {
	Instances     uw.InstanceRepository
	Servers       uw.ServerRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Groups        uw.GroupRepository
	Messaging     uw.MessagingAPI
	GroupAPI      uw.GroupAPI
	Assets        uw.RemoteAssetFetcher

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

var errUnattributableEvent = errors.New("unofficial whatsapp: event identifies no chat")

func (uc *HandleWebhookUseCase) handleEvent(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	err := uc.dispatch(ctx, instance, ev)
	if errors.Is(err, errUnattributableEvent) {
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
		log.Printf("[unofficial-whatsapp] instance %s: unhandled provider event %q, payload keys: %v",
			instance.ID, ev.ProviderEvent, uw.DescribeUnknownBody(ev.Raw))
		return nil
	}
}

func (uc *HandleWebhookUseCase) handleInbound(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordInbound(ctx, sub.conversation.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, sub, ev, conversation.SentByContact(sub.authorHandle)); err != nil {
		return err
	}

	if !ev.RunsAutomation() || !sub.conversation.InScope(instance.HandleGroups) {
		uc.broadcastEntryUpdate(sub.conversation.ID)
		return nil
	}

	auto := uc.automationFor(sub.conversation.ID)

	uc.ensureAssignment(sub.conversation, instance)
	uc.fireWorkflowTriggers(instance, sub.conversation, ev, auto)
	uc.maybeReplyWithAgent(ctx, instance, sub.subject, sub.conversation, ev, auto)
	uc.scheduleAnalysis(instance, sub.conversation, auto)
	uc.broadcastEntryUpdate(sub.conversation.ID)
	return nil
}

func (uc *HandleWebhookUseCase) handleOutbound(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}

	if err := uc.conversations.RecordOutbound(ctx, sub.conversation.ID, ev.Timestamp); err != nil {
		return err
	}
	if err := uc.recordMessage(ctx, instance, sub, ev, conversation.SentExternally()); err != nil {
		return err
	}
	if ev.Kind == uw.EventOutboundFromDevice && !ev.Backfill && sub.conversation.InScope(instance.HandleGroups) {
		uc.scheduleAnalysis(instance, sub.conversation, uc.automationFor(sub.conversation.ID))
	}
	uc.broadcastEntryUpdate(sub.conversation.ID)
	return nil
}

type chatContext struct {
	subject      *uw.Contact
	authorName   string
	authorHandle string
	authorAvatar string
	author       *uw.Contact

	conversation *uw.Conversation
	group        *uw.Group
}

func (uc *HandleWebhookUseCase) resolveContext(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
) (*chatContext, error) {
	subjectJID := ev.SubjectJID()

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
		out.group = uc.groups.ensureFresh(ctx, instance, ev.ChatID)
		return
	}

	if !ev.Outbound() {
		uc.profiles.applyEventName(ctx, out.subject, ev.SenderName)
	}
	uc.bridgeLead(ctx, instance, out.subject)
	uc.profiles.refresh(ctx, instance, out.subject, false)
}

func (uc *HandleWebhookUseCase) resolveAuthor(
	ctx context.Context,
	instance *uw.Instance,
	ev *uw.Event,
	out *chatContext,
) {
	if !ev.IsGroup {
		out.authorName = out.subject.DisplayName()
		out.authorHandle = out.subject.Handle()
		out.authorAvatar = out.subject.PictureURL
		return
	}

	out.authorHandle = ev.SenderPhone
	if out.authorHandle != "" {
		out.authorHandle = "+" + out.authorHandle
	} else {
		out.authorHandle = ev.SenderJID
	}
	out.authorName = firstNonEmpty(ev.SenderName, out.authorHandle)

	if ev.SenderJID == "" || ev.SenderJID == ev.ChatID {
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

func subjectSeedName(ev *uw.Event) string {
	if ev.IsGroup {
		return ""
	}
	if ev.Outbound() {
		return ""
	}
	return ev.SenderName
}

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

func (uc *HandleWebhookUseCase) recordMessage(
	ctx context.Context,
	instance *uw.Instance,
	sub *chatContext,
	ev *uw.Event,
	sentBy conversation.SentBy,
) error {
	if uc.history == nil {
		return nil
	}

	conv := sub.conversation
	record := conversation.MessageHistoryRecord{
		SentBy:            sentBy,
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeUnofficialWhatsApp,
		Channel:           conversation.MessageChannelUnofficialWhatsApp,
		MessageType:       messageTypeFor(ev),
		ProviderMessageID: ev.ProviderMessageID,
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
		SenderName:        sub.authorName,
		SenderAvatar:      sub.authorAvatar,
	}
	if sentBy.IsContact() {
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

	if strings.TrimSpace(record.Text) == "" && record.MediaID == "" {
		record.Text = placeholderForEmptyMessage(ev)
	}
	return uc.history.Record(ctx, record)
}

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

type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

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

	if err := uc.fileStorage.UploadFile(objectKey, remote.Data, mimeType); err != nil {
		log.Printf("[unofficial-whatsapp] media upload failed for message %s: %v", ev.ProviderMessageID, err)
		return nil
	}
	url := uc.fileStorage.GetFileURL(objectKey)

	mediaType := conversationMediaType(ev.Media)
	stored := &storedAttachment{mediaType: mediaType, url: url}
	if uc.convMedia != nil {
		row := &conversation.ConversationMedia{
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

func (uc *HandleWebhookUseCase) handleMessageUpdate(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	entryID := ""
	if strings.TrimSpace(ev.ChatID) != "" {
		if conv, err := uc.conversations.FindByChatID(ctx, instance.ID, ev.ChatID); err == nil && conv != nil {
			entryID = conv.ID
		}
	}

	uc.advanceDeliveryStatus(ctx, entryID, ev)
	if entryID != "" {
		uc.broadcastEntryUpdate(entryID)
	}
	return nil
}

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
	if uc.campaignStatus != nil {
		uc.campaignStatus.AdvanceFromDelivery(target, ev.DeliveryStatus)
	}

	if err := uc.messages.UpdateDeliveryStatus(target, status); err != nil {
		log.Printf("[unofficial-whatsapp] could not advance status for message %s: %v", target, err)
		return
	}
	if uc.broadcaster != nil && entryID != "" {
		uc.broadcaster.BroadcastMessageStatus(
			entryID, string(shared.EntryTypeUnofficialWhatsApp), target, status)
	}
}

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

func (uc *HandleWebhookUseCase) handleReaction(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	sub, err := uc.resolveContext(ctx, instance, ev)
	if err != nil {
		return err
	}
	if uc.history == nil {
		return nil
	}
	err = uc.history.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:            reactionSender(ev, sub),
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
	if err != nil {
		return nil
	}

	if name := strings.TrimSpace(ev.SenderName); name != "" {
		field := uw.ContactProfile{ContactName: name}
		if contact.IsGroup {
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

func (uc *HandleWebhookUseCase) handleGroupChanged(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	if err := uc.groups.markStale(ctx, instance.ID, ev.ChatID); err != nil {
		log.Printf("[unofficial-whatsapp] could not flag group %s as stale: %v", ev.ChatID, err)
	}
	return nil
}

func reactionSender(ev *uw.Event, sub *chatContext) conversation.SentBy {
	if ev.FromMe {
		return conversation.SentExternally()
	}
	return conversation.SentByContact(sub.authorHandle)
}

func (uc *HandleWebhookUseCase) handleCall(ctx context.Context, instance *uw.Instance, ev *uw.Event) error {
	conv, err := uc.conversations.FindByChatID(ctx, instance.ID, ev.ChatID)
	if err != nil || uc.history == nil {
		return nil
	}
	err = uc.history.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:      conversation.SentByContact(ev.ChatID),
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

func (uc *HandleWebhookUseCase) ensureAssignment(conv *uw.Conversation, instance *uw.Instance) {
	if uc.assignments == nil {
		return
	}
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeUnofficialWhatsApp), instance.ID)
}

func (uc *HandleWebhookUseCase) scheduleAnalysis(instance *uw.Instance, conv *uw.Conversation, auto *CampaignAutomation) {
	analysis, staging, memory := instance.EnableAnalysis, instance.EnableAutoStaging, instance.EnableAutoMemory
	if auto != nil {
		analysis, staging, memory = auto.EnableAnalysis, auto.EnableAutoStaging, auto.EnableAutoMemory
	}
	if uc.analysis == nil || (!analysis && !staging && !memory) {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeUnofficialWhatsApp)
}

func (uc *HandleWebhookUseCase) fireWorkflowTriggers(
	instance *uw.Instance,
	conv *uw.Conversation,
	ev *uw.Event,
	auto *CampaignAutomation,
) {
	if uc.workflows == nil {
		return
	}
	if !conv.RunsAutomation(instance.HandleGroups) {
		return
	}

	scopedWorkflowID := ""
	if auto != nil {
		if !auto.Automation.RunsWorkflow(conv.AutomationEnabled) {
			return
		}
		scopedWorkflowID = auto.Automation.WorkflowID
	} else {
		if !instance.EnableWorkflow {
			return
		}
		if instance.WorkflowID != nil {
			scopedWorkflowID = strings.TrimSpace(*instance.WorkflowID)
		}
	}

	data := map[string]any{"text": ev.Text}

	if scopedWorkflowID != "" {
		if auto != nil {
			data["campaign_id"] = auto.CampaignID
			data["campaign_workflow_id"] = scopedWorkflowID
		} else {
			data["account_workflow_id"] = scopedWorkflowID
		}
	}
	if ev.OptionID != "" {
		data[workflow.DataKeySelectedOptionID] = ev.OptionID
		data[workflow.DataKeySelectedOptionTitle] = ev.Text
	}
	workflow.ApplyContactNumber(data, uw.PhoneFromJID(conv.ChatID))

	uc.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: instance.WorkspaceID,
		EntryID:     conv.ID,
		EntryType:   string(shared.EntryTypeUnofficialWhatsApp),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})
}

func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	instance *uw.Instance,
	contact *uw.Contact,
	conv *uw.Conversation,
	ev *uw.Event,
	auto *CampaignAutomation,
) {
	if uc.aiReply == nil {
		return
	}
	if !conv.RunsAutomation(instance.HandleGroups) {
		return
	}

	agentID := ""
	agentEnabled := false
	if auto != nil {
		if !auto.Automation.RunsAgent(conv.AutomationEnabled) {
			return
		}
		agentID, agentEnabled = auto.Automation.AgentID, true
	} else {
		if instance.AgentID == nil {
			return
		}
		agentID, agentEnabled = *instance.AgentID, instance.EnableAgentResponses
	}

	var leadID *string
	if contact != nil {
		leadID = contact.LeadID
	}
	_, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           instance.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeUnofficialWhatsApp,
		AgentID:               agentID,
		AgentResponsesEnabled: agentEnabled,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  ev.Text,
		LeadID:                leadID,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] AI reply failed for entry %s: %v", conv.ID, err)
	}
}

func (uc *HandleWebhookUseCase) SetCampaignDeliverySink(sink CampaignDeliverySink) {
	uc.campaignStatus = sink
}

func (uc *HandleWebhookUseCase) SetCampaignAutomationSource(src CampaignAutomationSource) {
	uc.campaignAutomation = src
}

func (uc *HandleWebhookUseCase) automationFor(conversationID string) *CampaignAutomation {
	if uc.campaignAutomation == nil {
		return nil
	}
	found, ok := uc.campaignAutomation.AutomationForConversation(conversationID)
	if !ok {
		return nil
	}
	return found
}

func (uc *HandleWebhookUseCase) broadcastEntryUpdate(entryID string) {
	if uc.broadcaster == nil || entryID == "" {
		return
	}
	uc.broadcaster.BroadcastEntryUpdate(entryID, string(shared.EntryTypeUnofficialWhatsApp), nil)
}

func messageTypeFor(ev *uw.Event) conversation.MessageType {
	switch ev.Media {
	case uw.MediaVoice, uw.MediaAudio:
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
