package metachannel

import (
	"encoding/json"
	"mime"
	"path"
	"strings"

	"vozko/domain/conversation"
	mm "vozko/domain/metamessaging"
	"vozko/domain/workflow"
)

func Classify(msg *mm.Message, prefix string) (conversation.MessageType, json.RawMessage) {
	meta := map[string]any{}
	key := func(name string) string { return prefix + "_" + name }
	msgType := conversation.MessageTypeUserMessage

	if msg.ReplyTo != nil {
		switch {
		case msg.ReplyTo.Story != nil:
			msgType = conversation.MessageTypeStoryReply
			meta[key("story_id")] = msg.ReplyTo.Story.ID
			meta[key("story_url")] = msg.ReplyTo.Story.URL
			if msg.ReplyTo.Story.LinkStickerURL != "" {
				meta[key("story_link_sticker_url")] = msg.ReplyTo.Story.LinkStickerURL
			}
		case msg.ReplyTo.MID != "":
			meta[key("reply_to_mid")] = msg.ReplyTo.MID
		}
	}

	for _, att := range msg.Attachments {
		if att == nil {
			continue
		}
		switch att.Type {
		case "story_mention":
			msgType = conversation.MessageTypeStoryMention
			if att.Payload != nil {
				meta[key("story_mention_url")] = att.Payload.URL
			}
		case "share", "post", "ig_post":
			msgType = conversation.MessageTypePostShare
			if att.Payload != nil {
				if att.Payload.ID != "" {
					meta[key("shared_post_id")] = att.Payload.ID
				}
				if att.Payload.URL != "" {
					meta[key("shared_post_url")] = att.Payload.URL
				}
				if att.Payload.Title != "" {
					meta[key("shared_post_title")] = att.Payload.Title
				}
			}
		case "sticker":
			msgType = conversation.MessageTypeSticker
			if att.Payload != nil && att.Payload.StickerID.String() != "" {
				meta[key("sticker_id")] = att.Payload.StickerID.String()
			}
		case "fallback":
			msgType = conversation.MessageTypeLinkShare
			if att.Payload != nil {
				meta[key("link_url")] = att.Payload.URL
				meta[key("link_title")] = att.Payload.Title
			}
		case "ephemeral":
			meta[key("ephemeral")] = true
		}
	}

	if msg.Referral != nil {
		addReferral(meta, prefix, msg.Referral)
	}
	if msg.QuickReply != nil && msg.QuickReply.Payload != "" {
		meta[key("quick_reply_payload")] = msg.QuickReply.Payload
	}
	if msg.Unsupported() {
		msgType = conversation.MessageTypeUnsupported
		meta[key("unsupported")] = true
	}
	if msg.MID != "" {
		meta[key("mid")] = msg.MID
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return msgType, nil
	}
	return msgType, raw
}

func ReferralMetadata(prefix string, r *mm.Referral) map[string]any {
	meta := map[string]any{}
	addReferral(meta, prefix, r)
	return meta
}

func addReferral(meta map[string]any, prefix string, r *mm.Referral) {
	if r == nil {
		return
	}
	set := func(name, value string) {
		if value != "" {
			meta[prefix+"_referral_"+name] = value
		}
	}
	set("ref", r.Ref)
	set("source", r.Source)
	set("type", r.Type)
	set("ad_id", r.AdID)
	if r.AdsContextData != nil {
		set("ad_title", r.AdsContextData.AdTitle)
		set("ad_photo_url", r.AdsContextData.PhotoURL)
		set("ad_post_id", r.AdsContextData.PostID)
	}
}

func StorableKind(att *mm.Attachment) string {
	if att == nil {
		return ""
	}
	if att.Type == "sticker" {
		return "image"
	}
	return mm.MediaKindForAttachment(att.Type)
}

func MediaMessageType(base conversation.MessageType, mediaType conversation.MediaType) conversation.MessageType {
	switch base {
	case conversation.MessageTypeStoryReply, conversation.MessageTypeStoryMention,
		conversation.MessageTypePostShare, conversation.MessageTypeUnsupported,
		conversation.MessageTypeSticker, conversation.MessageTypeLinkShare:
		return base
	}
	if mediaType == conversation.MediaTypeAudio {
		return conversation.MessageTypeAudio
	}
	return conversation.MessageTypeMedia
}

func ConversationMediaType(kind string) conversation.MediaType {
	switch kind {
	case "image":
		return conversation.MediaTypeImage
	case "video":
		return conversation.MediaTypeVideo
	case "audio":
		return conversation.MediaTypeAudio
	default:
		return conversation.MediaTypeDocument
	}
}

func UnsupportedPlaceholder(msg *mm.Message) string {
	for _, att := range msg.Attachments {
		if att != nil && att.Type == "ephemeral" {
			return "[disappearing media]"
		}
	}
	return "[unsupported message]"
}

func QuickReplySelection(msg *mm.Message) *workflow.OptionSelection {
	if msg == nil || msg.QuickReply == nil || msg.QuickReply.Payload == "" {
		return nil
	}
	return &workflow.OptionSelection{ID: msg.QuickReply.Payload, Title: msg.Text, Kind: "quick_reply"}
}

func PostbackSelection(p *mm.Postback) *workflow.OptionSelection {
	if p == nil || p.Payload == "" {
		return nil
	}
	return &workflow.OptionSelection{ID: p.Payload, Title: p.Title, Kind: "postback"}
}

func ExtensionFor(contentType, rawURL string) string {
	if contentType != "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return exts[0]
		}
	}
	if ext := path.Ext(strings.SplitN(rawURL, "?", 2)[0]); ext != "" {
		return ext
	}
	return ""
}

func MergeMetadata(existing json.RawMessage, updates map[string]any) json.RawMessage {
	merged := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &merged)
	}
	for k, v := range updates {
		merged[k] = v
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return existing
	}
	return raw
}
