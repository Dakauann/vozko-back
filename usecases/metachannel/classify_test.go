package metachannel

import (
	"encoding/json"
	"testing"

	"vozko/domain/conversation"
	mm "vozko/domain/metamessaging"
)

func decode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestClassifyKeepsTheChannelPrefix(t *testing.T) {
	msgType, raw := Classify(&mm.Message{MID: "m1", ReplyTo: &mm.ReplyTo{MID: "m0"}}, "instagram")
	meta := decode(t, raw)
	if msgType != conversation.MessageTypeUserMessage || meta["instagram_reply_to_mid"] != "m0" || meta["instagram_mid"] != "m1" {
		t.Fatalf("type=%s meta=%v", msgType, meta)
	}
}

func TestClassifyStoryReplyAndMention(t *testing.T) {
	msgType, raw := Classify(&mm.Message{ReplyTo: &mm.ReplyTo{Story: &mm.Story{ID: "s", URL: "https://s"}}}, "instagram")
	if msgType != conversation.MessageTypeStoryReply || decode(t, raw)["instagram_story_url"] != "https://s" {
		t.Fatalf("story reply = %s %s", msgType, raw)
	}
	msgType, raw = Classify(&mm.Message{Attachments: []*mm.Attachment{{Type: "story_mention", Payload: &mm.AttachmentPayload{URL: "https://m"}}}}, "instagram")
	if msgType != conversation.MessageTypeStoryMention || decode(t, raw)["instagram_story_mention_url"] != "https://m" {
		t.Fatalf("story mention = %s %s", msgType, raw)
	}
}

func TestClassifyMessengerAttachments(t *testing.T) {
	cases := []struct {
		name    string
		att     *mm.Attachment
		want    conversation.MessageType
		metaKey string
		metaVal any
	}{
		{"sticker", &mm.Attachment{Type: "sticker", Payload: &mm.AttachmentPayload{URL: "https://st", StickerID: "369239263222822"}}, conversation.MessageTypeSticker, "facebook_sticker_id", "369239263222822"},
		{"fallback link", &mm.Attachment{Type: "fallback", Payload: &mm.AttachmentPayload{URL: "https://yt", Title: "Video"}}, conversation.MessageTypeLinkShare, "facebook_link_url", "https://yt"},
		{"fallback without payload", &mm.Attachment{Type: "fallback"}, conversation.MessageTypeLinkShare, "facebook_link_url", nil},
		{"post share", &mm.Attachment{Type: "post", Payload: &mm.AttachmentPayload{URL: "https://p", Title: "T", ID: "9"}}, conversation.MessageTypePostShare, "facebook_shared_post_id", "9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgType, raw := Classify(&mm.Message{Attachments: []*mm.Attachment{tc.att}}, "facebook")
			if msgType != tc.want {
				t.Fatalf("type = %s", msgType)
			}
			if got := decode(t, raw)[tc.metaKey]; got != tc.metaVal {
				t.Fatalf("%s = %v, want %v", tc.metaKey, got, tc.metaVal)
			}
		})
	}
}

func TestClassifyAdReferralInsideAMessage(t *testing.T) {
	_, raw := Classify(&mm.Message{Referral: &mm.Referral{Ref: "promo", Source: "ADS", AdID: "42", AdsContextData: &mm.AdsContextData{AdTitle: "Sale"}}}, "facebook")
	meta := decode(t, raw)
	if meta["facebook_referral_ref"] != "promo" || meta["facebook_referral_ad_id"] != "42" || meta["facebook_referral_ad_title"] != "Sale" {
		t.Fatalf("meta = %v", meta)
	}
}

func TestClassifyUnsupportedWins(t *testing.T) {
	yes := true
	msgType, _ := Classify(&mm.Message{IsUnsupported: &yes, Attachments: []*mm.Attachment{{Type: "sticker"}}}, "facebook")
	if msgType != conversation.MessageTypeUnsupported {
		t.Fatalf("type = %s", msgType)
	}
}

func TestMediaMessageTypeKeepsSpecialTypes(t *testing.T) {
	if MediaMessageType(conversation.MessageTypeSticker, conversation.MediaTypeImage) != conversation.MessageTypeSticker {
		t.Fatal("sticker must stay a sticker")
	}
	if MediaMessageType(conversation.MessageTypeUserMessage, conversation.MediaTypeAudio) != conversation.MessageTypeAudio {
		t.Fatal("audio attachment must be audio")
	}
	if MediaMessageType(conversation.MessageTypeUserMessage, conversation.MediaTypeImage) != conversation.MessageTypeMedia {
		t.Fatal("image attachment must be media")
	}
}

func TestStoredMediaKinds(t *testing.T) {
	if StorableKind(&mm.Attachment{Type: "sticker"}) != "image" {
		t.Fatal("stickers are stored as images")
	}
	if StorableKind(&mm.Attachment{Type: "fallback"}) != "" {
		t.Fatal("fallback links are not downloaded")
	}
	if StorableKind(&mm.Attachment{Type: "file"}) != "document" {
		t.Fatal("files are documents")
	}
}

func TestMergeMetadata(t *testing.T) {
	merged := MergeMetadata(json.RawMessage(`{"a":1,"b":2}`), map[string]any{"b": 3, "c": 4})
	m := decode(t, merged)
	if m["a"] != float64(1) || m["b"] != float64(3) || m["c"] != float64(4) {
		t.Fatalf("merged = %v", m)
	}
}
