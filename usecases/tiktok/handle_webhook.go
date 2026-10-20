package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/messagestate"
)

const (
	PlaceholderUnsupported = "[unsupported message]"
	PlaceholderMedia       = "[media unavailable]"
	metadataSentVia        = "tiktok_sent_via"
	metadataSource         = "tiktok_source"
	metadataAutoMessage    = "tiktok_auto_message"
	metadataSharedVideo    = "tiktok_shared_video_id"
	metadataSharedURL      = "tiktok_shared_url"
	metadataButton         = "tiktok_button_id"
	metadataReplyTo        = "tiktok_reply_to"
)

type CommentSink interface {
	Forget(ctx context.Context, account *ttdomain.Account, commentID string)
}

type HandleWebhookDeps struct {
	Repos       Repositories
	Tokens      *TokenSource
	Messaging   ttdomain.MessagingService
	Errors      ttdomain.ErrorClassifier
	History     conversation.MessageHistoryManager
	Messages    MessageStore
	Announcer   messagestate.Announcer
	Watermarks  conversation.DeliveryWatermarkRepository
	Media       conversation.MediaStore
	Images      *ImageStore
	Assignments AssignmentService
	Automation  InboundDispatcher
	Comments    CommentSink
	Side        *SideMessages
	Metrics     Metrics
}

type HandleWebhook struct {
	d   HandleWebhookDeps
	now Clock
}

func NewHandleWebhook(d HandleWebhookDeps) *HandleWebhook {
	if d.Repos.Accounts == nil || d.Repos.Contacts == nil || d.Repos.Conversations == nil || d.Repos.Comments == nil ||
		d.Tokens == nil || d.Messaging == nil || d.Errors == nil || d.History == nil || d.Messages == nil || d.Announcer == nil ||
		d.Watermarks == nil || d.Media == nil || d.Images == nil || d.Assignments == nil || d.Automation == nil || d.Comments == nil || d.Side == nil || d.Metrics == nil {
		panic("tiktok: the webhook handler requires every repository, tokens, messaging, an error classifier, history, messages, announcer, watermarks, media, images, assignments, automation, the comment sink and metrics")
	}
	return &HandleWebhook{d: d, now: utcNow}
}

func (h *HandleWebhook) Handle(ctx context.Context, ev ttdomain.Event) error {
	account, err := h.d.Repos.Accounts.FindLiveByOpenID(ctx, ev.AccountOpenID)
	if errors.Is(err, ttdomain.ErrAccountNotFound) {
		h.d.Metrics.AddTikTokEvent("consume", "unknown_account")
		return ErrUnknownAccount
	}
	if err != nil {
		return err
	}
	switch ev.Kind {
	case ttdomain.EventMessageReceived:
		return h.whenMessaging(account, func() error { return h.inbound(ctx, account, ev.Message) })
	case ttdomain.EventMessageSent:
		return h.whenMessaging(account, func() error { return h.echo(ctx, account, ev.Message) })
	case ttdomain.EventReferral:
		return h.whenMessaging(account, func() error { return h.referral(ctx, account, ev.Referral) })
	case ttdomain.EventRead:
		return h.whenMessaging(account, func() error { return h.read(ctx, account, ev.Read) })
	case ttdomain.EventComment:
		return h.comment(ctx, account, ev)
	case ttdomain.EventMessageReceivedEU:
		h.d.Metrics.AddTikTokEvent("message", "eu_without_content")
		log.Printf("[tiktok] account %s received a message from an EEA, Swiss or UK user; TikTok withholds its content", account.ID)
		return nil
	case ttdomain.EventAutoMessageConfig, ttdomain.EventAutoMessageAudit:
		h.d.Metrics.AddTikTokEvent("auto_message", string(ev.Kind))
		return nil
	case ttdomain.EventHighIntentComment:
		h.d.Metrics.AddTikTokEvent("comment", "high_intent_ignored")
		return nil
	}
	return fmt.Errorf("%w: %s", ttdomain.ErrUnknownEvent, ev.Kind)
}

func (h *HandleWebhook) whenMessaging(account *ttdomain.Account, run func() error) error {
	if !account.CanReceiveMessagesNow() {
		h.d.Metrics.AddTikTokEvent("message", "account_unavailable")
		return fmt.Errorf("%w: %s", ErrAccountUnavailable, account.Status)
	}
	return run()
}

func (h *HandleWebhook) resolve(ctx context.Context, account *ttdomain.Account, m *ttdomain.Message) (*ttdomain.Contact, *ttdomain.Conversation, error) {
	contact, err := h.d.Repos.Contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, UniqueIdentifier: m.PersonID, Username: m.PersonUsername, IsFollower: m.IsFollower,
	})
	if err != nil {
		return nil, nil, err
	}
	conv, _, err := h.d.Repos.Conversations.FindOrCreate(ctx, ttdomain.FindOrCreateConversationInput{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, ContactID: contact.ID, ConversationID: m.ConversationID,
	})
	if err != nil {
		return nil, nil, err
	}
	return contact, conv, nil
}

func (h *HandleWebhook) inbound(ctx context.Context, account *ttdomain.Account, m *ttdomain.Message) error {
	contact, conv, err := h.resolve(ctx, account, m)
	if err != nil {
		return err
	}
	if m.Type == ttdomain.MessageReaction {
		return h.reaction(ctx, conv, m)
	}
	if err := h.d.Repos.Conversations.RecordInbound(ctx, conv.ID, m.SentAt); err != nil {
		return err
	}
	h.d.Assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeTikTok), account.ID)
	contact = h.refreshProfile(ctx, account, contact, conv)

	text, err := h.record(ctx, account, conv, conversation.SentByContact(contact.UniqueIdentifier), m, recordParty{
		from: contact.UniqueIdentifier, to: account.OpenID, name: contact.Name(), avatar: h.d.Images.URL(contact.AvatarKey),
	})
	if err != nil {
		return err
	}
	h.d.Metrics.AddTikTokEvent("message", "received")
	return h.d.Automation.Dispatch(ctx, conversation_usecase.InboundAutomationInput{
		WorkspaceID:          account.WorkspaceID,
		EntryID:              conv.ID,
		EntryType:            shared.EntryTypeTikTok,
		ContactRef:           contact.UniqueIdentifier,
		ProviderMessageID:    m.MessageID,
		ReceivedAt:           m.SentAt,
		LeadID:               contact.LeadID,
		Text:                 text,
		Selection:            buttonSelection(m),
		ConversationOverride: conv.AutomationEnabled,
		Config:               account.Automation(),
	})
}

func buttonSelection(m *ttdomain.Message) *workflow.OptionSelection {
	if m == nil || strings.TrimSpace(m.ButtonID) == "" {
		return nil
	}
	return &workflow.OptionSelection{ID: m.ButtonID, Title: m.Text, Kind: "quick_reply"}
}

func (h *HandleWebhook) echo(ctx context.Context, account *ttdomain.Account, m *ttdomain.Message) error {
	_, conv, err := h.resolve(ctx, account, m)
	if err != nil {
		return err
	}
	if err := h.d.Repos.Conversations.RecordOutbound(ctx, conv.ID, m.SentAt); err != nil {
		return err
	}
	if m.Source == ttdomain.SourceAPI && h.d.Side.Seen(m.MessageID) {
		h.d.Metrics.AddTikTokEvent("echo", "side_message")
		return nil
	}
	if m.Source != ttdomain.SourceAPI {
		if err := h.d.Repos.Conversations.CountOutbound(ctx, conv.ID); err != nil {
			return err
		}
	}
	via := sentVia(m)
	_, err = h.record(ctx, account, conv, conversation.SentExternally(), m, recordParty{
		from: account.OpenID, to: m.PersonID, plainType: conversation.MessageTypeOperator,
		metadata: map[string]any{metadataSentVia: via},
	})
	if err == nil {
		h.d.Metrics.AddTikTokEvent("echo", via)
	}
	return err
}

func sentVia(m *ttdomain.Message) string {
	switch {
	case m.AutoMessageType != "":
		return "tiktok_automation"
	case m.Source == ttdomain.SourceAPI:
		return "api"
	case m.Source == ttdomain.SourceApp:
		return "tiktok_app"
	case m.Source == ttdomain.SourceWeb:
		return "tiktok_web"
	}
	return "tiktok_other"
}

func (h *HandleWebhook) reaction(ctx context.Context, conv *ttdomain.Conversation, m *ttdomain.Message) error {
	for _, r := range m.Reactions {
		target := strings.TrimSpace(r.OriginalMessageID)
		if target == "" {
			continue
		}
		existing, err := h.d.Messages.GetByEntryAndExternalMessageID(shared.EntryTypeTikTok, conv.ID, target)
		if err != nil || existing == nil {
			h.d.Metrics.AddTikTokEvent("reaction", "target_unknown")
			continue
		}
		emoji := strings.TrimSpace(r.Emoji)
		if strings.EqualFold(r.Operation, "REMOVE") {
			emoji = ""
		}
		if err := existing.React(conversation.Reaction{Emoji: emoji, Direction: conversation.ReactionInbound, ActorID: m.PersonID, At: messagestate.Stamp(r.ReactedAt)}); err != nil {
			return err
		}
		if err := (messagestate.Writer{Messages: h.d.Messages, Announcer: h.d.Announcer}).Save(existing); err != nil {
			return err
		}
	}
	return nil
}

func (h *HandleWebhook) referral(ctx context.Context, account *ttdomain.Account, r *ttdomain.ReferralEvent) error {
	_, conv, err := h.resolve(ctx, account, &ttdomain.Message{ConversationID: r.ConversationID, PersonID: r.PersonID, PersonUsername: r.PersonUsername, IsFollower: r.IsFollower})
	if err != nil {
		return err
	}
	h.d.Metrics.AddTikTokEvent("referral", r.Referral.Source)
	return h.d.Repos.Conversations.SetReferral(ctx, conv.ID, r.Referral)
}

func (h *HandleWebhook) read(ctx context.Context, account *ttdomain.Account, r *ttdomain.ReadEvent) error {
	conv, err := h.d.Repos.Conversations.FindByConversationID(ctx, account.ID, r.ConversationID)
	if errors.Is(err, ttdomain.ErrConversationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = h.d.Watermarks.MarkOutboundStatusUpTo(conv.ID, shared.EntryTypeTikTok, conversation.DeliveryStatusRead, r.ReadUpTo)
	return err
}

func (h *HandleWebhook) comment(ctx context.Context, account *ttdomain.Account, ev ttdomain.Event) error {
	if !account.CanReadCommentsNow() {
		h.d.Metrics.AddTikTokEvent("comment", "account_unavailable")
		return fmt.Errorf("%w: %s", ErrAccountUnavailable, account.Status)
	}
	c := ev.Comment
	write := ttdomain.CommentEventWrite{WorkspaceID: account.WorkspaceID, AccountID: account.ID, Event: *c, OccurredAt: ev.OccurredAt}
	if c.Action == ttdomain.CommentInserted && c.PersonID != "" {
		if account.UniqueIdentifier != "" && account.UniqueIdentifier == c.PersonID {
			write.KnownOurs = true
		} else if contact, err := h.d.Repos.Contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: account.WorkspaceID, AccountID: account.ID, UniqueIdentifier: c.PersonID}); err == nil {
			write.ContactID = &contact.ID
		} else {
			return err
		}
	}
	applied, err := h.d.Repos.Comments.ApplyEvent(ctx, write)
	if err != nil {
		return err
	}
	if !applied {
		h.d.Metrics.AddTikTokEvent("comment", "stale_event")
		return nil
	}
	if c.Action == ttdomain.CommentDeleted {
		h.d.Comments.Forget(ctx, account, c.CommentID)
	}
	h.d.Metrics.AddTikTokEvent("comment", string(c.Action))
	return nil
}

type recordParty struct {
	from      string
	to        string
	name      string
	avatar    string
	plainType conversation.MessageType
	metadata  map[string]any
}

func (h *HandleWebhook) record(ctx context.Context, account *ttdomain.Account, conv *ttdomain.Conversation, sentBy conversation.SentBy, m *ttdomain.Message, party recordParty) (string, error) {
	meta := map[string]any{metadataSource: string(m.Source)}
	for k, v := range party.metadata {
		meta[k] = v
	}
	if m.AutoMessageType != "" {
		meta[metadataAutoMessage] = m.AutoMessageType
	}
	if m.ButtonID != "" {
		meta[metadataButton] = m.ButtonID
	}
	if m.ReferencedMessageID != "" {
		meta[metadataReplyTo] = m.ReferencedMessageID
	}
	plain := party.plainType
	if plain == "" {
		plain = conversation.MessageTypeUserMessage
	}
	rec := conversation.MessageHistoryRecord{
		SentBy:            sentBy,
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeTikTok,
		Channel:           conversation.MessageChannelTikTok,
		ProviderMessageID: m.MessageID,
		From:              party.from,
		To:                party.to,
		Timestamp:         m.SentAt,
		SenderName:        party.name,
		SenderAvatar:      party.avatar,
	}
	text := strings.TrimSpace(m.Text)
	switch m.Type {
	case ttdomain.MessageText:
		rec.MessageType, rec.Text = plain, text
	case ttdomain.MessageImage, ttdomain.MessageVideo:
		stored := h.storeMedia(ctx, account, conv, m)
		if stored == nil {
			rec.MessageType, rec.Text = conversation.MessageTypeUnsupported, PlaceholderMedia
			break
		}
		rec.MessageType = conversation.MessageTypeMedia
		rec.MediaID, rec.MediaType, rec.MediaURL, rec.Text = stored.ID, stored.Type, stored.URL, text
	case ttdomain.MessageSharePost:
		rec.MessageType, rec.Text = conversation.MessageTypePostShare, strings.TrimSpace(m.SharedEmbedURL)
		meta[metadataSharedVideo], meta[metadataSharedURL] = m.SharedVideoID, m.SharedEmbedURL
	case ttdomain.MessageSticker, ttdomain.MessageEmoji:
		source := m.StickerURL
		if m.Type == ttdomain.MessageEmoji {
			source = m.EmojiURL
		}
		rec.MessageType, rec.MediaType, rec.MediaURL = conversation.MessageTypeSticker, conversation.MediaTypeSticker, strings.TrimSpace(source)
	case ttdomain.MessageTemplate:
		rec.MessageType, rec.Text = plain, cardText(m.Card)
	default:
		rec.MessageType, rec.Text = conversation.MessageTypeUnsupported, PlaceholderUnsupported
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	rec.Metadata = raw
	if err := h.d.History.Record(ctx, rec); err != nil {
		return "", err
	}
	if rec.MessageType == conversation.MessageTypeUnsupported {
		return "", nil
	}
	return rec.Text, nil
}

func cardText(card *ttdomain.Card) string {
	if card == nil {
		return PlaceholderUnsupported
	}
	parts := []string{strings.TrimSpace(card.Title)}
	for _, b := range card.Buttons {
		parts = append(parts, "- "+strings.TrimSpace(b.Text))
	}
	return strings.Join(parts, "\n")
}

func (h *HandleWebhook) storeMedia(ctx context.Context, account *ttdomain.Account, conv *ttdomain.Conversation, m *ttdomain.Message) *conversation.ConversationMedia {
	if strings.TrimSpace(m.MediaID) == "" {
		return nil
	}
	creds, err := h.d.Tokens.Credentials(ctx, account)
	if err != nil {
		h.d.Metrics.AddTikTokEvent("media", "no_credentials")
		return nil
	}
	kind, mediaType := ttdomain.MediaKindImage, conversation.MediaTypeImage
	if m.Type == ttdomain.MessageVideo {
		kind, mediaType = ttdomain.MediaKindVideo, conversation.MediaTypeVideo
	}
	downloaded, err := h.d.Messaging.DownloadMedia(ctx, creds, ttdomain.MediaRef{ConversationID: m.ConversationID, MessageID: m.MessageID, MediaID: m.MediaID, Kind: kind})
	if err != nil {
		h.d.Metrics.AddTikTokEvent("media", "download_failed")
		log.Printf("[tiktok] media of message %s not downloaded: %v", m.MessageID, err)
		return nil
	}
	id := uuid.NewString()
	stored, err := h.d.Media.Store(conversation.StoreMediaInput{
		ID:        id,
		Key:       "tiktok/" + conv.ID + "/" + id,
		EntryID:   conv.ID,
		EntryType: shared.EntryTypeTikTok,
		Type:      mediaType,
		MimeType:  downloaded.MimeType,
		Data:      downloaded.Data,
	})
	if err != nil {
		h.d.Metrics.AddTikTokEvent("media", "store_failed")
		log.Printf("[tiktok] media of message %s not stored: %v", m.MessageID, err)
		return nil
	}
	return stored
}

func (h *HandleWebhook) refreshProfile(ctx context.Context, account *ttdomain.Account, contact *ttdomain.Contact, conv *ttdomain.Conversation) *ttdomain.Contact {
	now := h.now()
	if !contact.ProfileIsStale(now) {
		return contact
	}
	creds, err := h.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return contact
	}
	people, err := h.d.Messaging.Participants(ttdomain.WithBackground(ctx), creds, conv.ConversationID)
	if err != nil {
		h.d.Metrics.AddTikTokEvent("profile", "unavailable")
		return contact
	}
	for _, p := range people {
		if p.PersonID != contact.UniqueIdentifier {
			continue
		}
		profile := ttdomain.ContactProfile{Username: p.Username, DisplayName: p.DisplayName}
		if key, err := h.d.Images.Rehost(ctx, ContactAvatarKey(account.ID, contact.ID), p.AvatarURL); err == nil {
			profile.AvatarKey = key
		}
		if err := h.d.Repos.Contacts.UpdateProfile(ctx, contact.ID, profile, now); err != nil {
			log.Printf("[tiktok] profile of contact %s not saved: %v", contact.ID, err)
			return contact
		}
		refreshed := *contact
		if profile.DisplayName != "" {
			refreshed.DisplayName = profile.DisplayName
		}
		if profile.Username != "" {
			refreshed.Username = profile.Username
		}
		if profile.AvatarKey != "" {
			refreshed.AvatarKey = profile.AvatarKey
		}
		refreshed.ProfileRefreshedAt = &now
		return &refreshed
	}
	return contact
}
