package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/metachannel"
)

var ErrUnknownPage = errors.New("facebook: webhook for an unknown page")

const MetadataPrefix = "facebook"

var messengerDialect = mm.Dialect{Prefix: "fb", ClassifyStandby: true}

func EntryDedupKey(env *mm.EntryEnvelope) string { return messengerDialect.EntryDedupKey(env) }

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type ProfileReader interface {
	GetProfile(ctx context.Context, pageToken, psid string) (*fbdomain.ProfileResult, error)
}

type AvatarStore interface {
	StoreContactAvatar(ctx context.Context, contactID, url string) (string, error)
}

type HandleMessagingDeps struct {
	Pages         fbdomain.PageRepository
	Contacts      fbdomain.ContactRepository
	Conversations fbdomain.ConversationRepository
	Profiles      ProfileReader
	Avatars       AvatarStore
	Transcript    *metachannel.Transcript
	Watermarks    conversation.DeliveryWatermarkRepository
	Assignments   AssignmentService
	Automation    *conversation_usecase.InboundAutomation
	OurAppID      string
}

type HandleMessagingUseCase struct {
	d   HandleMessagingDeps
	now func() time.Time
}

func NewHandleMessagingUseCase(d HandleMessagingDeps) *HandleMessagingUseCase {
	return &HandleMessagingUseCase{d: d, now: func() time.Time { return time.Now().UTC() }}
}

func (uc *HandleMessagingUseCase) Execute(ctx context.Context, env *mm.EntryEnvelope) error {
	events := mm.NormalizeMessaging(env, messengerDialect)
	if len(events) == 0 {
		return nil
	}
	page, err := uc.d.Pages.FindByFBPageID(ctx, env.Entry.ID)
	if errors.Is(err, fbdomain.ErrPageNotFound) {
		return fmt.Errorf("%w: %s", ErrUnknownPage, env.Entry.ID)
	}
	if err != nil {
		return err
	}
	if page.Status == fbdomain.StatusDisconnected {
		log.Printf("[facebook] ignoring %d event(s) for disconnected page %s", len(events), page.FBPageID)
		return nil
	}

	var errs []error
	for _, ev := range events {
		if err := uc.handle(ctx, page, ev); err != nil {
			log.Printf("[facebook] event %s for page %s failed: %v", ev.Kind, page.FBPageID, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (uc *HandleMessagingUseCase) handle(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	switch ev.Kind {
	case mm.EventInboundMessage:
		return uc.inbound(ctx, page, ev)
	case mm.EventEchoMessage:
		return uc.echo(ctx, page, ev)
	case mm.EventEditedMessage:
		return uc.withKnownConversation(ctx, page, ev, func(conv *fbdomain.Conversation) error {
			return uc.d.Transcript.ApplyEdit(conv.ID, ev.Edit)
		})
	case mm.EventReaction:
		return uc.reaction(ctx, page, ev)
	case mm.EventDelivery:
		return uc.watermark(ctx, page, ev, fbdomain.WatermarkDelivered, conversation.DeliveryStatusDelivered, ev.Delivery.WatermarkTime())
	case mm.EventRead:
		return uc.watermark(ctx, page, ev, fbdomain.WatermarkRead, conversation.DeliveryStatusRead, ev.Read.WatermarkTime())
	case mm.EventPostback:
		return uc.postback(ctx, page, ev)
	case mm.EventReferral:
		return uc.referral(ctx, page, ev)
	case mm.EventThreadControl:
		return uc.threadControl(ctx, page, ev)
	case mm.EventPolicyEnforcement:
		return uc.policy(ctx, page, ev)
	case mm.EventOptin:
		log.Printf("[facebook] optin page=%s psid=%s type=%s status=%s", page.FBPageID, ev.ContactExternalID, ev.Optin.Type, ev.Optin.Status)
	}
	return nil
}

func (uc *HandleMessagingUseCase) inbound(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	contact, conv, err := uc.resolve(ctx, page, ev.ContactExternalID)
	if err != nil {
		return err
	}
	if contact.Blocked {
		log.Printf("[facebook] dropping message from blocked contact %s", contact.PSID)
		return nil
	}
	if err := uc.d.Conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.observeOwner(ctx, conv, ev)
	uc.ensureAssignment(conv, page)

	text, err := uc.d.Transcript.RecordMessage(ctx, conv.ID, conversation.SentByContact(contact.PSID), ev.Message, ev.Timestamp, uc.party(page, contact))
	if err != nil {
		return err
	}
	uc.enrich(ctx, page, contact)

	if ev.Standby || conv.OwnedByAnotherApp(uc.d.OurAppID) {
		return nil
	}
	uc.d.Automation.Dispatch(ctx, automationInput(page, contact, conv, text, metachannel.QuickReplySelection(ev.Message)))
	return nil
}

func (uc *HandleMessagingUseCase) echo(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	_, conv, err := uc.resolve(ctx, page, ev.ContactExternalID)
	if err != nil {
		return err
	}
	if err := uc.d.Conversations.RecordOutbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	appID := ev.Message.AppID.String()
	if appID == uc.d.OurAppID && ev.Message.Metadata == fbdomain.CaptionMetadata {
		return nil
	}
	if appID != "" && appID != uc.d.OurAppID {
		owner := appID
		if fbdomain.IsPageInboxApp(appID) {
			owner = fbdomain.PageInboxAppID
		}
		uc.setOwner(ctx, conv, owner)
	}
	_, err = uc.d.Transcript.RecordMessage(ctx, conv.ID, conversation.SentExternally(), ev.Message, ev.Timestamp,
		metachannel.Party{From: page.FBPageID, To: ev.ContactExternalID},
		metachannel.AsType(conversation.MessageTypeOperator),
		metachannel.WithMetadata(map[string]any{MetadataPrefix + "_sent_via": sentVia(appID, uc.d.OurAppID)}))
	return err
}

func (uc *HandleMessagingUseCase) reaction(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	return uc.withKnownConversation(ctx, page, ev, func(conv *fbdomain.Conversation) error {
		if ev.Reaction.Action == "react" {
			if err := uc.d.Conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
				return err
			}
		}
		return uc.d.Transcript.ApplyReaction(conv.ID, ev.Reaction, ev.Timestamp)
	})
}

func (uc *HandleMessagingUseCase) watermark(ctx context.Context, page *fbdomain.Page, ev *mm.Event, kind fbdomain.WatermarkKind, status conversation.DeliveryStatus, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	return uc.withKnownConversation(ctx, page, ev, func(conv *fbdomain.Conversation) error {
		if !conv.WatermarkAdvances(kind, at) {
			return nil
		}
		if err := uc.d.Conversations.AdvanceWatermark(ctx, conv.ID, kind, at); err != nil {
			return err
		}
		if uc.d.Watermarks == nil {
			return nil
		}
		updated, err := uc.d.Watermarks.MarkOutboundStatusUpTo(conv.ID, shared.EntryTypeFacebook, status, at)
		if err != nil {
			return err
		}
		if updated > 0 {
			uc.d.Transcript.BroadcastEntryUpdate(conv.ID)
		}
		return nil
	})
}

func (uc *HandleMessagingUseCase) postback(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	contact, conv, err := uc.resolve(ctx, page, ev.ContactExternalID)
	if err != nil {
		return err
	}
	if err := uc.d.Conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.observeOwner(ctx, conv, ev)
	uc.ensureAssignment(conv, page)
	uc.d.Transcript.RecordReferral(ctx, conv.ID, ev.Postback.Referral)

	metadata := metachannel.MergeMetadata(nil, map[string]any{
		MetadataPrefix + "_postback_payload": ev.Postback.Payload,
		MetadataPrefix + "_postback_title":   ev.Postback.Title,
	})
	if err := uc.d.Transcript.Record(ctx, conv.ID, conversation.SentByContact(contact.PSID), uc.party(page, contact), metachannel.HistoryInput{
		MessageType:       conversation.MessageTypeUserMessage,
		ProviderMessageID: ev.Postback.MID,
		Text:              ev.Postback.Title,
		Timestamp:         ev.Timestamp,
		Metadata:          metadata,
	}); err != nil {
		return err
	}
	uc.enrich(ctx, page, contact)
	if ev.Standby || conv.OwnedByAnotherApp(uc.d.OurAppID) {
		return nil
	}
	uc.d.Automation.Dispatch(ctx, automationInput(page, contact, conv, ev.Postback.Title, metachannel.PostbackSelection(ev.Postback)))
	return nil
}

func (uc *HandleMessagingUseCase) referral(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	_, conv, err := uc.resolve(ctx, page, ev.ContactExternalID)
	if err != nil {
		return err
	}
	if err := uc.d.Conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.d.Transcript.RecordReferral(ctx, conv.ID, ev.Referral)
	return nil
}

func (uc *HandleMessagingUseCase) threadControl(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	_, conv, err := uc.resolve(ctx, page, ev.ContactExternalID)
	if err != nil {
		return err
	}
	owner := ev.ThreadControl.NewOwnerAppID.String()
	if fbdomain.IsPageInboxApp(owner) {
		owner = fbdomain.PageInboxAppID
	}
	if owner == uc.d.OurAppID {
		owner = ""
	}
	uc.setOwner(ctx, conv, owner)
	return nil
}

func (uc *HandleMessagingUseCase) policy(ctx context.Context, page *fbdomain.Page, ev *mm.Event) error {
	if err := uc.d.Pages.UpdatePolicy(ctx, page.ID, ev.Policy.Action, ev.Policy.Reason, uc.now()); err != nil {
		return err
	}
	log.Printf("[facebook] policy %s for page %s: %s", ev.Policy.Action, page.FBPageID, ev.Policy.Reason)
	switch ev.Policy.Action {
	case "block":
		if page.Status.CanTransitionTo(fbdomain.StatusRestricted) {
			return uc.d.Pages.UpdateStatus(ctx, page.ID, fbdomain.StatusRestricted, "blocked by Meta: "+ev.Policy.Reason)
		}
	case "unblock":
		if page.Status == fbdomain.StatusRestricted {
			return uc.d.Pages.UpdateStatus(ctx, page.ID, fbdomain.StatusConnected, "")
		}
	}
	return nil
}

func (uc *HandleMessagingUseCase) withKnownConversation(ctx context.Context, page *fbdomain.Page, ev *mm.Event, do func(*fbdomain.Conversation) error) error {
	contact, err := uc.d.Contacts.FindByPSID(ctx, page.ID, ev.ContactExternalID)
	if errors.Is(err, fbdomain.ErrContactNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	conv, err := uc.d.Conversations.FindByContact(ctx, page.ID, contact.ID)
	if errors.Is(err, fbdomain.ErrConversationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return do(conv)
}

func (uc *HandleMessagingUseCase) resolve(ctx context.Context, page *fbdomain.Page, psid string) (*fbdomain.Contact, *fbdomain.Conversation, error) {
	if psid == "" {
		return nil, nil, fmt.Errorf("facebook: event has no customer id")
	}
	contact, err := uc.d.Contacts.FindOrCreate(ctx, page.WorkspaceID, page.ID, psid)
	if err != nil {
		return nil, nil, err
	}
	conv, err := uc.d.Conversations.FindOrCreate(ctx, page.WorkspaceID, page.ID, contact.ID)
	if err != nil {
		return nil, nil, err
	}
	return contact, conv, nil
}

func (uc *HandleMessagingUseCase) observeOwner(ctx context.Context, conv *fbdomain.Conversation, ev *mm.Event) {
	if ev.Standby {
		uc.setOwner(ctx, conv, fbdomain.OtherAppOwner)
		return
	}
	uc.setOwner(ctx, conv, "")
}

func (uc *HandleMessagingUseCase) setOwner(ctx context.Context, conv *fbdomain.Conversation, owner string) {
	if conv.ThreadOwnerAppID == owner {
		return
	}
	if err := uc.d.Conversations.SetThreadOwner(ctx, conv.ID, owner, uc.now()); err != nil {
		log.Printf("[facebook] thread owner update failed conversation=%s: %v", conv.ID, err)
		return
	}
	conv.ThreadOwnerAppID = owner
	uc.d.Transcript.BroadcastEntryUpdate(conv.ID)
}

func (uc *HandleMessagingUseCase) ensureAssignment(conv *fbdomain.Conversation, page *fbdomain.Page) {
	if uc.d.Assignments == nil {
		return
	}
	uc.d.Assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeFacebook), page.ID)
}

func (uc *HandleMessagingUseCase) party(page *fbdomain.Page, contact *fbdomain.Contact) metachannel.Party {
	return metachannel.Party{From: contact.PSID, To: page.FBPageID, SenderName: contact.DisplayName()}
}

func (uc *HandleMessagingUseCase) enrich(ctx context.Context, page *fbdomain.Page, contact *fbdomain.Contact) {
	if uc.d.Profiles == nil || !contact.ProfileIsStale(uc.now()) {
		return
	}
	profile, err := uc.d.Profiles.GetProfile(ctx, page.PageToken, contact.PSID)
	if err != nil {
		status, known := profileFailure(err)
		if !known {
			log.Printf("[facebook] profile fetch failed psid=%s: %v", contact.PSID, err)
			return
		}
		uc.saveProfile(ctx, contact, fbdomain.ContactProfile{Status: status, FetchedAt: uc.now()})
		return
	}
	update := fbdomain.ContactProfile{
		Name: profile.Name, FirstName: profile.FirstName, LastName: profile.LastName,
		Status: fbdomain.ProfileAvailable, FetchedAt: uc.now(),
	}
	if profile.PictureURL != "" && uc.d.Avatars != nil {
		if key, err := uc.d.Avatars.StoreContactAvatar(ctx, contact.ID, profile.PictureURL); err == nil {
			update.AvatarStorageKey = key
		} else {
			log.Printf("[facebook] avatar not stored psid=%s: %v", contact.PSID, err)
		}
	}
	uc.saveProfile(ctx, contact, update)
}

func (uc *HandleMessagingUseCase) saveProfile(ctx context.Context, contact *fbdomain.Contact, p fbdomain.ContactProfile) {
	if err := uc.d.Contacts.UpdateProfile(ctx, contact.ID, p); err != nil {
		log.Printf("[facebook] profile update failed psid=%s: %v", contact.PSID, err)
	}
}

func profileFailure(err error) (fbdomain.ProfileStatus, bool) {
	switch {
	case fbdomain.HasCode(err, fbdomain.CodeNoProfile):
		return fbdomain.ProfileNone, true
	case fbdomain.HasCode(err, fbdomain.CodeProfilePermission), fbdomain.Classify(err) == fbdomain.FailureAccessLevel:
		return fbdomain.ProfileDenied, true
	}
	return "", false
}

func sentVia(appID, ourAppID string) string {
	switch {
	case appID == ourAppID:
		return "vozko"
	case fbdomain.IsPageInboxApp(appID):
		return "meta_business_suite"
	case appID == "":
		return "unknown"
	}
	return "external_app"
}

func automationInput(page *fbdomain.Page, contact *fbdomain.Contact, conv *fbdomain.Conversation, text string, sel *workflow.OptionSelection) conversation_usecase.InboundAutomationInput {

	return conversation_usecase.InboundAutomationInput{
		WorkspaceID:          page.WorkspaceID,
		EntryID:              conv.ID,
		EntryType:            shared.EntryTypeFacebook,
		ContactRef:           contact.PSID,
		LeadID:               contact.LeadID,
		Text:                 text,
		Selection:            sel,
		ConversationOverride: conv.AutomationEnabled,
		Config:               page.Automation(),
	}
}
