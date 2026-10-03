package metachannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
)

type MessageStore interface {
	GetByExternalMessageID(entryType shared.EntryType, externalID string) (*conversation.Message, error)
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
	Update(messageID string, message *conversation.Message) error
	Delete(messageID string) error
}

type EntryUpdateBroadcaster interface {
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

type FetchFunc func(ctx context.Context, url string) ([]byte, string, error)

type Transcript struct {
	EntryType   shared.EntryType
	Channel     conversation.MessageChannel
	Prefix      string
	History     conversation.MessageHistoryManager
	Messages    MessageStore
	Media       conversation.MediaStore
	Ads         conversation.AdOriginRecorder
	Fetch       FetchFunc
	Broadcaster EntryUpdateBroadcaster
}

type Party struct {
	From         string
	To           string
	SenderName   string
	SenderAvatar string
}

type HistoryInput struct {
	MessageType       conversation.MessageType
	ProviderMessageID string
	Text              string
	Timestamp         time.Time
	MediaID           string
	MediaType         conversation.MediaType
	MediaURL          string
	Metadata          json.RawMessage
	AdReferral        *conversation.AdReferral
}

func (t *Transcript) Record(ctx context.Context, entryID string, sentBy conversation.SentBy, party Party, in HistoryInput) error {
	if t.History == nil {
		return nil
	}
	return t.History.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:            sentBy,
		EntryID:           entryID,
		EntryType:         t.EntryType,
		Channel:           t.Channel,
		MessageType:       in.MessageType,
		ProviderMessageID: in.ProviderMessageID,
		From:              party.From,
		To:                party.To,
		Text:              in.Text,
		Timestamp:         in.Timestamp,
		MediaID:           in.MediaID,
		MediaType:         in.MediaType,
		MediaURL:          in.MediaURL,
		Metadata:          in.Metadata,
		SenderName:        party.SenderName,
		SenderAvatar:      party.SenderAvatar,
		AdReferral:        in.AdReferral,
	})
}

func (t *Transcript) adPlatform() conversation.AdPlatform {
	if t.EntryType == shared.EntryTypeInstagram {
		return conversation.AdPlatformInstagram
	}
	return conversation.AdPlatformFacebook
}

func (t *Transcript) RecordReferral(ctx context.Context, entryID string, r *mm.Referral) {
	if t.Ads == nil {
		return
	}
	t.Ads.Record(ctx, entryID, t.EntryType, AdReferralOf(r, t.adPlatform()))
}

func AdReferralOf(r *mm.Referral, platform conversation.AdPlatform) *conversation.AdReferral {
	if r == nil || !strings.EqualFold(strings.TrimSpace(r.Source), "ADS") {
		return nil
	}
	ad := &conversation.AdReferral{AdID: strings.TrimSpace(r.AdID), Platform: platform, SourceURL: strings.TrimSpace(r.RefererURI)}
	if r.AdsContextData != nil {
		ad.Title = strings.TrimSpace(r.AdsContextData.AdTitle)
		ad.ImageURL = strings.TrimSpace(r.AdsContextData.PhotoURL)
	}
	if !ad.Usable() {
		return nil
	}
	return ad
}

type RecordOption func(*recordOptions)

type recordOptions struct {
	plainType conversation.MessageType
	metadata  map[string]any
}

func AsType(t conversation.MessageType) RecordOption {
	return func(o *recordOptions) { o.plainType = t }
}

func WithMetadata(values map[string]any) RecordOption {
	return func(o *recordOptions) { o.metadata = values }
}

func (t *Transcript) RecordMessage(ctx context.Context, entryID string, sentBy conversation.SentBy, msg *mm.Message, at time.Time, party Party, opts ...RecordOption) (string, error) {
	var o recordOptions
	for _, opt := range opts {
		opt(&o)
	}
	msgType, metadata := Classify(msg, t.Prefix)
	if o.plainType != "" && msgType == conversation.MessageTypeUserMessage {
		msgType = o.plainType
	}
	if len(o.metadata) > 0 {
		metadata = MergeMetadata(metadata, o.metadata)
	}
	text := strings.TrimSpace(msg.Text)
	ad := AdReferralOf(msg.Referral, t.adPlatform())
	firstAd := func() *conversation.AdReferral {
		current := ad
		ad = nil
		return current
	}

	stored := t.storeAttachments(ctx, entryID, msg)

	if text == "" && len(stored) == 0 {
		if msgType == conversation.MessageTypeUserMessage {
			msgType = conversation.MessageTypeUnsupported
		}
		placeholder := ""
		if msgType == conversation.MessageTypeUnsupported {
			placeholder = UnsupportedPlaceholder(msg)
		}
		return text, t.Record(ctx, entryID, sentBy, party, HistoryInput{
			MessageType: msgType, ProviderMessageID: msg.MID, Text: placeholder, Timestamp: at, Metadata: metadata, AdReferral: firstAd(),
		})
	}

	if text != "" || len(stored) == 0 {
		if err := t.Record(ctx, entryID, sentBy, party, HistoryInput{
			MessageType: msgType, ProviderMessageID: msg.MID, Text: text, Timestamp: at, Metadata: metadata, AdReferral: firstAd(),
		}); err != nil {
			return text, err
		}
	}

	for i, item := range stored {
		providerID := msg.MID
		if len(stored) > 1 || text != "" {
			providerID = fmt.Sprintf("%s:att%d", msg.MID, i)
		}
		if err := t.Record(ctx, entryID, sentBy, party, HistoryInput{
			MessageType:       MediaMessageType(msgType, item.Type),
			ProviderMessageID: providerID,
			Timestamp:         at,
			MediaID:           item.ID,
			MediaType:         item.Type,
			MediaURL:          item.URL,
			Metadata:          metadata,
			AdReferral:        firstAd(),
		}); err != nil {
			return text, err
		}
	}
	return text, nil
}

func (t *Transcript) storeAttachments(ctx context.Context, entryID string, msg *mm.Message) []*conversation.ConversationMedia {
	if t.Media == nil || t.Fetch == nil || len(msg.Attachments) == 0 {
		return nil
	}
	out := make([]*conversation.ConversationMedia, 0, len(msg.Attachments))
	for _, att := range msg.Attachments {
		kind := StorableKind(att)
		if kind == "" || att.Payload == nil || att.Payload.URL == "" {
			continue
		}
		data, contentType, err := t.Fetch(ctx, att.Payload.URL)
		if err != nil {
			log.Printf("[%s] attachment download failed type=%s: %v", t.EntryType, att.Type, err)
			continue
		}
		mediaID := uuid.NewString()
		stored, err := t.Media.Store(conversation.StoreMediaInput{
			ID:        mediaID,
			Key:       fmt.Sprintf("conversations/%s/%s/%s%s", t.EntryType, entryID, mediaID, ExtensionFor(contentType, att.Payload.URL)),
			EntryID:   entryID,
			EntryType: t.EntryType,
			Type:      ConversationMediaType(kind),
			MimeType:  contentType,
			Data:      data,
		})
		if err != nil {
			log.Printf("[%s] attachment not stored type=%s: %v", t.EntryType, att.Type, err)
			continue
		}
		out = append(out, stored)
	}
	return out
}

func (t *Transcript) Find(entryID, mid string) (*conversation.Message, error) {
	if t.Messages == nil {
		return nil, conversation.ErrMessageNotFound
	}
	if entryID != "" {
		return t.Messages.GetByEntryAndExternalMessageID(t.EntryType, entryID, mid)
	}
	return t.Messages.GetByExternalMessageID(t.EntryType, mid)
}

func (t *Transcript) Tombstone(entryID, mid string) error {
	return t.mutate(entryID, mid, func(m *conversation.Message) (bool, error) {
		return false, t.Messages.Delete(m.ID)
	})
}

func (t *Transcript) ApplyEdit(entryID string, edit *mm.MessageEdit) error {
	return t.mutate(entryID, edit.MID, func(m *conversation.Message) (bool, error) {
		m.Text = strings.TrimSpace(edit.Text)
		m.Metadata = MergeMetadata(m.Metadata, map[string]any{
			t.Prefix + "_edited":     true,
			t.Prefix + "_edit_count": edit.NumEdit.String(),
		})
		return true, nil
	})
}

func (t *Transcript) ApplyReaction(entryID string, r *mm.Reaction, at time.Time) error {
	return t.mutate(entryID, r.MID, func(m *conversation.Message) (bool, error) {
		payload := map[string]any{
			t.Prefix + "_reaction_action": r.Action,
			t.Prefix + "_reaction":        r.Reaction,
			t.Prefix + "_reaction_emoji":  r.Emoji,
			t.Prefix + "_reaction_at":     at.UTC().Format(time.RFC3339),
		}
		if r.Action == "unreact" {
			payload[t.Prefix+"_reaction"] = ""
			payload[t.Prefix+"_reaction_emoji"] = ""
		}
		m.Metadata = MergeMetadata(m.Metadata, payload)
		return true, nil
	})
}

func (t *Transcript) MarkRead(entryID, mid string) error {
	return t.mutate(entryID, mid, func(m *conversation.Message) (bool, error) {
		if m.DeliveryStatus == conversation.DeliveryStatusRead {
			return false, nil
		}
		m.DeliveryStatus = conversation.DeliveryStatusRead
		return true, nil
	})
}

func (t *Transcript) mutate(entryID, mid string, change func(*conversation.Message) (bool, error)) error {
	if t.Messages == nil || mid == "" {
		return nil
	}
	existing, err := t.Find(entryID, mid)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}
	save, err := change(existing)
	if err != nil {
		return err
	}
	if save {
		if err := t.Messages.Update(existing.ID, existing); err != nil {
			return err
		}
	}
	t.BroadcastEntryUpdate(existing.EntryID)
	return nil
}

func (t *Transcript) BroadcastEntryUpdate(entryID string) {
	if t.Broadcaster == nil || entryID == "" {
		return
	}
	t.Broadcaster.BroadcastEntryUpdate(entryID, string(t.EntryType), nil)
}
