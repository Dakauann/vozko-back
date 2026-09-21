package instagram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "webhooks", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func decodeFirstEntry(t *testing.T, name string) *EntryEnvelope {
	t.Helper()
	envelopes, err := DecodeEnvelope(loadFixture(t, name))
	if err != nil {
		t.Fatalf("DecodeEnvelope(%s): %v", name, err)
	}
	entries := SplitEntries(envelopes)
	if len(entries) == 0 {
		t.Fatalf("SplitEntries(%s) returned no entries", name)
	}
	return entries[0]
}

func normalizeFixture(t *testing.T, name string) []*Event {
	t.Helper()
	return NormalizeEntry(decodeFirstEntry(t, name))
}

func firstEvent(t *testing.T, name string) *Event {
	t.Helper()
	events := normalizeFixture(t, name)
	if len(events) == 0 {
		t.Fatalf("NormalizeEntry(%s) produced no events", name)
	}
	return events[0]
}

func TestDecodeEnvelope_AcceptsBothTopLevelShapes(t *testing.T) {
	for _, name := range []string{"text_dm.json", "top_level_array.json"} {
		envelopes, err := DecodeEnvelope(loadFixture(t, name))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if len(envelopes) != 1 {
			t.Fatalf("%s: got %d envelopes, want 1", name, len(envelopes))
		}
		if envelopes[0].Object != "instagram" {
			t.Errorf("%s: object = %q, want instagram", name, envelopes[0].Object)
		}
		if len(envelopes[0].Entry) != 1 {
			t.Errorf("%s: got %d entries, want 1", name, len(envelopes[0].Entry))
		}
	}
}

func TestDecodeEnvelope_RejectsGarbage(t *testing.T) {
	for _, body := range []string{"", "   ", "not json", "42"} {
		if _, err := DecodeEnvelope([]byte(body)); err == nil {
			t.Errorf("DecodeEnvelope(%q) = nil error, want failure", body)
		}
	}
}

func TestNormalizeEntry_TextDM(t *testing.T) {
	ev := firstEvent(t, "text_dm.json")

	if ev.Kind != EventInboundMessage {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventInboundMessage)
	}
	if ev.ContactIGSID != "IGSID" {
		t.Errorf("contact = %q, want IGSID", ev.ContactIGSID)
	}
	if ev.IGAccountExternalID != "IGID" {
		t.Errorf("account = %q, want IGID", ev.IGAccountExternalID)
	}
	if ev.Message == nil || ev.Message.Text != "MESSAGE-TEXT" {
		t.Errorf("message text not decoded: %+v", ev.Message)
	}
	if ev.IdempotencyKey != "ig:IGID:messages:MESSAGE-ID" {
		t.Errorf("idempotency key = %q", ev.IdempotencyKey)
	}
	if ev.Timestamp.UnixMilli() != 1569262485349 {
		t.Errorf("timestamp = %d, want 1569262485349", ev.Timestamp.UnixMilli())
	}
}

func TestUnixTimestampToTimeAcceptsSecondsAndMilliseconds(t *testing.T) {
	want := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, value := range []int64{want.Unix(), want.UnixMilli()} {
		if got := unixTimestampToTime(value); !got.Equal(want) {
			t.Errorf("unixTimestampToTime(%d) = %v, want %v", value, got, want)
		}
	}
}

func TestNormalizeChange_UsesSecondsForCurrentMetaPayloads(t *testing.T) {
	want := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	event := normalizeChange(&Entry{ID: "account-1", Time: want.Unix()}, "comments", json.RawMessage(`{
		"id":"comment-1","text":"oi","media":{"id":"media-1"}
	}`))
	if event == nil {
		t.Fatal("comment webhook was not normalized")
	}
	if !event.Timestamp.Equal(want) {
		t.Fatalf("event timestamp = %v, want %v", event.Timestamp, want)
	}
}

func TestNormalizeEntry_EchoIsDistinguished(t *testing.T) {
	ev := firstEvent(t, "echo_outbound.json")

	if ev.Kind != EventEchoMessage {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventEchoMessage)
	}
	if !ev.IsOutbound() {
		t.Error("IsOutbound() = false, want true")
	}
	if ev.ContactIGSID != "IGSID" {
		t.Errorf("contact = %q, want IGSID", ev.ContactIGSID)
	}
	if ev.IdempotencyKey != "ig:IGID:messages:MESSAGE-ID" {
		t.Errorf("idempotency key = %q, want the same key as the inbound message", ev.IdempotencyKey)
	}
}

func TestNormalizeEntry_PresenceOnlyBooleans(t *testing.T) {
	plain := firstEvent(t, "text_dm.json")
	if plain.Message.IsEcho != nil {
		t.Error("is_echo should be nil when absent")
	}
	if plain.Message.IsDeleted != nil {
		t.Error("is_deleted should be nil when absent")
	}

	deleted := firstEvent(t, "deleted_message.json")
	if deleted.Kind != EventDeletedMessage {
		t.Fatalf("kind = %s, want %s", deleted.Kind, EventDeletedMessage)
	}
	if deleted.Message.IsDeleted == nil || !*deleted.Message.IsDeleted {
		t.Error("is_deleted should decode to true")
	}
	if deleted.IdempotencyKey != "ig:IGID:deleted:MESSAGE-ID" {
		t.Errorf("idempotency key = %q, want a deleted-scoped key", deleted.IdempotencyKey)
	}
}

func TestNormalizeEntry_ReplyToIsAUnion(t *testing.T) {
	story := firstEvent(t, "story_reply.json")
	if story.Message.ReplyTo == nil || story.Message.ReplyTo.Story == nil {
		t.Fatalf("story reply not decoded: %+v", story.Message.ReplyTo)
	}
	if story.Message.ReplyTo.Story.ID != "STORY-ID" {
		t.Errorf("story id = %q", story.Message.ReplyTo.Story.ID)
	}
	if story.Message.ReplyTo.MID != "" {
		t.Error("a story reply must not populate reply_to.mid")
	}

	inline := firstEvent(t, "inline_reply.json")
	if inline.Message.ReplyTo == nil || inline.Message.ReplyTo.Story != nil {
		t.Fatalf("inline reply should not carry a story: %+v", inline.Message.ReplyTo)
	}
	if inline.Message.ReplyTo.MID == "" {
		t.Error("inline reply must populate reply_to.mid")
	}
}

func TestNormalizeEntry_StoryMentionIsAnAttachment(t *testing.T) {
	ev := firstEvent(t, "story_mention.json")
	if len(ev.Message.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(ev.Message.Attachments))
	}
	if ev.Message.Attachments[0].Type != "story_mention" {
		t.Errorf("attachment type = %q, want story_mention", ev.Message.Attachments[0].Type)
	}
	if ev.Message.ReplyTo != nil {
		t.Error("story mention must not populate reply_to")
	}
}

func TestNormalizeEntry_MultipleAttachments(t *testing.T) {
	ev := firstEvent(t, "media_dm_multi_attachment.json")
	if len(ev.Message.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(ev.Message.Attachments))
	}
	if got := MediaKindForAttachment(ev.Message.Attachments[0].Type); got != "image" {
		t.Errorf("first attachment kind = %q, want image", got)
	}
	if got := MediaKindForAttachment(ev.Message.Attachments[1].Type); got != "video" {
		t.Errorf("second attachment kind = %q, want video", got)
	}
}

func TestNormalizeEntry_EphemeralHasNoPayload(t *testing.T) {
	ev := firstEvent(t, "ephemeral_no_url.json")
	if len(ev.Message.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(ev.Message.Attachments))
	}
	att := ev.Message.Attachments[0]
	if att.Type != "ephemeral" {
		t.Errorf("type = %q, want ephemeral", att.Type)
	}
	if att.Payload != nil {
		t.Errorf("ephemeral payload = %+v, want nil", att.Payload)
	}
	if got := MediaKindForAttachment(att.Type); got != "" {
		t.Errorf("MediaKindForAttachment(ephemeral) = %q, want empty", got)
	}
}

func TestNormalizeEntry_Reaction(t *testing.T) {
	react := firstEvent(t, "reaction_react.json")
	if react.Kind != EventReaction {
		t.Fatalf("kind = %s, want %s", react.Kind, EventReaction)
	}
	if react.Reaction.Action != "react" || react.Reaction.Reaction != "love" {
		t.Errorf("reaction = %+v", react.Reaction)
	}
	if react.Reaction.Emoji == "" {
		t.Error("emoji should be preserved verbatim")
	}
	unreact := firstEvent(t, "reaction_unreact.json")
	if react.IdempotencyKey == unreact.IdempotencyKey {
		t.Errorf("react and unreact share an idempotency key: %q", react.IdempotencyKey)
	}
	if unreact.Reaction.Reaction != "" {
		t.Error("unreact should not carry a reaction value")
	}
}

func TestNormalizeEntry_ReadUsesMidNotWatermark(t *testing.T) {
	ev := firstEvent(t, "read_receipt.json")
	if ev.Kind != EventRead {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventRead)
	}
	if ev.Read == nil || ev.Read.MID != "MESSAGE-ID" {
		t.Errorf("read = %+v, want mid MESSAGE-ID", ev.Read)
	}
}

func TestNormalizeEntry_EditNumEditIsAString(t *testing.T) {
	ev := firstEvent(t, "edited_message.json")
	if ev.Kind != EventEditedMessage {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventEditedMessage)
	}
	if ev.Edit.Text != "the edited text" {
		t.Errorf("edited text = %q", ev.Edit.Text)
	}
	if ev.Edit.NumEdit.String() != "2" {
		t.Errorf("num_edit = %q, want 2", ev.Edit.NumEdit.String())
	}
	if ev.IdempotencyKey != "ig:IGID:edit:MESSAGE-ID:2" {
		t.Errorf("idempotency key = %q", ev.IdempotencyKey)
	}
}

func TestNormalizeEntry_PostbackIdentifiesBusinessByRecipient(t *testing.T) {
	ev := firstEvent(t, "postback.json")
	if ev.Kind != EventPostback {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventPostback)
	}
	if ev.IGAccountExternalID != "IGID" {
		t.Errorf("account = %q, want IGID (from recipient.id, not entry.id)", ev.IGAccountExternalID)
	}
	if ev.Postback.Payload != "ICEBREAKER_1" {
		t.Errorf("payload = %q", ev.Postback.Payload)
	}
}

func TestNormalizeEntry_Unsupported(t *testing.T) {
	ev := firstEvent(t, "unsupported_message.json")
	if ev.Message.IsUnsupported == nil || !*ev.Message.IsUnsupported {
		t.Fatal("is_unsupported should decode to true")
	}
}

func TestNormalizeEntry_CommentBothLoginShapes(t *testing.T) {
	igLogin := firstEvent(t, "comment_ig_login.json")
	if igLogin.Kind != EventComment {
		t.Fatalf("instagram-login: kind = %s, want %s", igLogin.Kind, EventComment)
	}
	if got := igLogin.Comment.ResolvedCommentID(); got != "COMMENT_ID" {
		t.Errorf("instagram-login: comment id = %q (value.id form)", got)
	}
	if igLogin.ContactIGSID != "IGSID" {
		t.Errorf("instagram-login: commenter = %q", igLogin.ContactIGSID)
	}

	fbLogin := firstEvent(t, "comment_fb_login.json")
	if fbLogin.Kind != EventComment {
		t.Fatalf("facebook-login: kind = %s, want %s", fbLogin.Kind, EventComment)
	}
	if got := fbLogin.Comment.ResolvedCommentID(); got != "COMMENT_ID" {
		t.Errorf("facebook-login: comment id = %q (value.comment_id form)", got)
	}
	if fbLogin.Comment.ParentID != "PARENT_COMMENT_ID" {
		t.Errorf("facebook-login: parent id = %q", fbLogin.Comment.ParentID)
	}
}

func TestNormalizeEntry_Standby(t *testing.T) {
	ev := firstEvent(t, "standby.json")
	if ev.Kind != EventStandby {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventStandby)
	}
}

func TestNormalizeEntry_UnknownFieldIsNotDropped(t *testing.T) {
	ev := firstEvent(t, "unknown_field.json")
	if ev.Kind != EventUnknown {
		t.Fatalf("kind = %s, want %s", ev.Kind, EventUnknown)
	}
	if ev.RawField != "messaging_policy_enforcement" {
		t.Errorf("raw field = %q", ev.RawField)
	}
	if len(ev.RawValue) == 0 {
		t.Error("raw value should be preserved for logging")
	}
	var parsed map[string]any
	if err := json.Unmarshal(ev.RawValue, &parsed); err != nil {
		t.Errorf("raw value is not valid json: %v", err)
	}
}

func TestSplitEntries_MultiAccountBatch(t *testing.T) {
	envelopes, err := DecodeEnvelope(loadFixture(t, "multi_account_batch.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries := SplitEntries(envelopes)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		events := NormalizeEntry(entry)
		if len(events) != 1 {
			t.Fatalf("entry %s: got %d events, want 1", entry.Entry.ID, len(events))
		}
		seen[events[0].IGAccountExternalID] = true
	}
	for _, want := range []string{"IGID_A", "IGID_B"} {
		if !seen[want] {
			t.Errorf("account %s missing from split batch", want)
		}
	}
}

func TestNormalizeEntry_SortsByEventTimestamp(t *testing.T) {
	events := normalizeFixture(t, "out_of_order_batch.json")
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	want := []string{"FIRST", "SECOND", "THIRD"}
	for i, ev := range events {
		if ev.Message.MID != want[i] {
			t.Errorf("position %d: mid = %q, want %q", i, ev.Message.MID, want[i])
		}
		if i > 0 && ev.Timestamp.Before(events[i-1].Timestamp) {
			t.Errorf("position %d is out of chronological order", i)
		}
	}
}

func TestIdempotencyKeys_DistinguishEventKindsOnTheSameMid(t *testing.T) {
	fixtures := []string{
		"text_dm.json",
		"deleted_message.json",
		"edited_message.json",
		"read_receipt.json",
		"reaction_react.json",
		"reaction_unreact.json",
	}

	keys := map[string]string{}
	for _, name := range fixtures {
		ev := firstEvent(t, name)
		if ev.IdempotencyKey == "" {
			t.Fatalf("%s: empty idempotency key", name)
		}
		if prev, dup := keys[ev.IdempotencyKey]; dup {
			t.Errorf("%s and %s produced the same key %q", name, prev, ev.IdempotencyKey)
		}
		keys[ev.IdempotencyKey] = name
	}
}
